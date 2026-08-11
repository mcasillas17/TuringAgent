package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

type RuntimeStream interface {
	Send(*turingv1.RuntimeUpdate) error
	Recv() (*turingv1.RuntimeCommand, error)
	CloseSend() error
}

type RuntimeClient interface {
	ConnectWorker(ctx context.Context) (RuntimeStream, error)
}

type Executor interface {
	Execute(ctx context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error
}

type BeaconPosterSetter interface {
	SetToolBeaconPoster(func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error))
}

type Options struct {
	WorkerID          string
	AgentID           turingv1.AgentId
	MaxConcurrentRuns int
}

type Worker struct {
	options    Options
	client     RuntimeClient
	executor   Executor
	mu         sync.Mutex
	active     map[string]*activeRun
	approvals  map[string]string
	toolCalls  map[string]string
	decisionMu sync.Mutex
	decisions  map[string][]*decisionWaiter
	sendMu     sync.Mutex
}

type decisionWaiter struct {
	decision chan *turingv1.ToolPolicyDecision
}

type activeRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	stop   bool
}

func New(options Options, client RuntimeClient, executor Executor) *Worker {
	if options.WorkerID == "" {
		options.WorkerID = "worker-general-go"
	}
	if options.AgentID == turingv1.AgentId_AGENT_ID_UNSPECIFIED {
		options.AgentID = turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT
	}
	if options.MaxConcurrentRuns <= 0 {
		options.MaxConcurrentRuns = 1
	}
	return &Worker{options: options, client: client, executor: executor, active: map[string]*activeRun{}, approvals: map[string]string{}, toolCalls: map[string]string{}, decisions: map[string][]*decisionWaiter{}}
}

func (w *Worker) Run(ctx context.Context) error {
	if w.client == nil {
		return errors.New("runtime client is required")
	}
	if w.executor == nil {
		return errors.New("executor is required")
	}
	stream, err := w.client.ConnectWorker(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = stream.CloseSend() }()
	if setter, ok := w.executor.(BeaconPosterSetter); ok {
		setter.SetToolBeaconPoster(func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			return w.postToolBeacon(ctx, stream, beacon)
		})
	}
	if err := w.send(stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: w.options.WorkerID, AgentId: w.options.AgentID, MaxConcurrentRuns: int32(w.options.MaxConcurrentRuns)}}}); err != nil {
		return err
	}
	for {
		cmd, err := stream.Recv()
		if err != nil {
			return err
		}
		switch value := cmd.GetCommand().(type) {
		case *turingv1.RuntimeCommand_RunAssigned:
			if value.RunAssigned != nil {
				w.startRun(ctx, stream, value.RunAssigned)
			}
		case *turingv1.RuntimeCommand_RunCancelled:
			if value.RunCancelled != nil {
				if err := w.cancelRun(ctx, stream, value.RunCancelled.GetRunId()); err != nil {
					return err
				}
			}
		case *turingv1.RuntimeCommand_ShutdownRequested:
			return nil
		case *turingv1.RuntimeCommand_ToolPolicyDecision:
			w.deliverDecision(value.ToolPolicyDecision)
		case *turingv1.RuntimeCommand_ApprovalUpdated:
			if value.ApprovalUpdated != nil && (value.ApprovalUpdated.Status == "denied" || value.ApprovalUpdated.Status == "expired") {
				if err := w.cancelApprovalRun(ctx, stream, value.ApprovalUpdated.ApprovalId); err != nil {
					return err
				}
			}
		}
	}
}

func (w *Worker) startRun(parent context.Context, stream RuntimeStream, job *turingv1.AgentJob) {
	runCtx, cancel := context.WithCancel(parent)
	entry := &activeRun{cancel: cancel, done: make(chan struct{})}
	w.mu.Lock()
	if _, exists := w.active[job.GetRunId()]; exists || len(w.active) >= w.options.MaxConcurrentRuns {
		w.mu.Unlock()
		cancel()
		return
	}
	w.active[job.GetRunId()] = entry
	w.mu.Unlock()
	go func() {
		defer close(entry.done)
		var terminal *turingv1.RuntimeUpdate
		err := w.executor.Execute(runCtx, job, func(update *turingv1.RuntimeUpdate) error {
			if isDerivedMessageCompleted(update) {
				return nil
			}
			if isTerminalRunUpdate(update) {
				terminal = update
				return nil
			}
			return w.send(stream, update)
		})
		if entry.isStopping() {
			return
		}
		w.deleteActive(job.GetRunId())
		if terminal != nil {
			_ = w.send(stream, terminal)
			return
		}
		if runWasTerminalized(err) {
			_ = w.send(stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: job.GetRunId()}}})
			return
		}
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(runCtx.Err(), context.Canceled) {
			_ = w.send(stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{RunId: job.GetRunId(), Code: "runtime_error", Message: err.Error(), Retryable: false}}})
		}
	}()
}

func isDerivedMessageCompleted(update *turingv1.RuntimeUpdate) bool {
	event := update.GetEvent()
	// RuntimeService derives message.completed from run_completed.
	return event != nil && event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED
}

func isTerminalRunUpdate(update *turingv1.RuntimeUpdate) bool {
	return update != nil && (update.GetRunCompleted() != nil || update.GetRunFailed() != nil)
}

type terminalRunState interface {
	RunTerminal() bool
}

func runWasTerminalized(err error) bool {
	var terminal terminalRunState
	return errors.As(err, &terminal) && terminal.RunTerminal()
}

func (w *Worker) cancelRun(ctx context.Context, stream RuntimeStream, runID string) error {
	entry := w.activeRun(runID)
	if entry == nil {
		return nil
	}
	entry.markStopping()
	entry.cancel()
	select {
	case <-entry.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	w.deleteActive(runID)
	return w.send(stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: runID}}})
}

func (w *Worker) activeRun(runID string) *activeRun {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active[runID]
}

func (w *Worker) cancelApprovalRun(ctx context.Context, stream RuntimeStream, approvalID string) error {
	w.mu.Lock()
	runID := w.approvals[approvalID]
	w.mu.Unlock()
	if runID == "" {
		return nil
	}
	return w.cancelRun(ctx, stream, runID)
}

func (w *Worker) deleteActive(runID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.active, runID)
	for approvalID, approvalRunID := range w.approvals {
		if approvalRunID == runID {
			delete(w.approvals, approvalID)
		}
	}
	for toolCallID, toolCallRunID := range w.toolCalls {
		if toolCallRunID == runID {
			delete(w.toolCalls, toolCallID)
		}
	}
}

func (w *Worker) send(stream RuntimeStream, update *turingv1.RuntimeUpdate) error {
	if update == nil {
		return fmt.Errorf("runtime update is required")
	}
	w.sendMu.Lock()
	defer w.sendMu.Unlock()
	return stream.Send(update)
}

func (w *Worker) postToolBeacon(ctx context.Context, stream RuntimeStream, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
	if beacon == nil || beacon.ToolCallId == "" {
		return nil, errors.New("tool beacon with tool_call_id is required")
	}
	waiter := &decisionWaiter{decision: make(chan *turingv1.ToolPolicyDecision, 1)}
	w.decisionMu.Lock()
	w.decisions[beacon.ToolCallId] = append(w.decisions[beacon.ToolCallId], waiter)
	w.decisionMu.Unlock()
	defer func() {
		w.removeDecisionWaiter(beacon.ToolCallId, waiter)
		w.mu.Lock()
		delete(w.toolCalls, beacon.ToolCallId)
		w.mu.Unlock()
	}()
	w.mu.Lock()
	w.toolCalls[beacon.ToolCallId] = beacon.RunId
	w.mu.Unlock()
	if err := w.send(stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: beacon}}); err != nil {
		return nil, err
	}
	select {
	case decision := <-waiter.decision:
		if decision.GetDecision() == turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED && decision.GetApprovalId() != "" && beacon.GetRunId() != "" {
			w.mu.Lock()
			w.approvals[decision.GetApprovalId()] = beacon.GetRunId()
			w.mu.Unlock()
		}
		return decision, nil
	case <-ctx.Done():
		return nil, sentBeaconError{err: ctx.Err()}
	}
}

type sentBeaconError struct {
	err error
}

func (e sentBeaconError) Error() string { return e.err.Error() }
func (e sentBeaconError) Unwrap() error { return e.err }
func (e sentBeaconError) BeaconPosted() bool {
	return true
}

func (w *Worker) deliverDecision(decision *turingv1.ToolPolicyDecision) {
	if decision == nil || decision.ToolCallId == "" {
		return
	}
	w.decisionMu.Lock()
	waiters := w.decisions[decision.ToolCallId]
	if len(waiters) == 0 {
		w.decisionMu.Unlock()
		return
	}
	waiter := waiters[0]
	if len(waiters) == 1 {
		delete(w.decisions, decision.ToolCallId)
	} else {
		w.decisions[decision.ToolCallId] = waiters[1:]
	}
	w.decisionMu.Unlock()
	if decision.GetDecision() == turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED && decision.GetApprovalId() != "" {
		w.mu.Lock()
		if runID := w.toolCalls[decision.GetToolCallId()]; runID != "" {
			w.approvals[decision.GetApprovalId()] = runID
		}
		w.mu.Unlock()
	}
	select {
	case waiter.decision <- decision:
	default:
	}
}

func (w *Worker) removeDecisionWaiter(toolCallID string, target *decisionWaiter) {
	w.decisionMu.Lock()
	defer w.decisionMu.Unlock()
	waiters := w.decisions[toolCallID]
	for index, waiter := range waiters {
		if waiter != target {
			continue
		}
		waiters = append(waiters[:index], waiters[index+1:]...)
		if len(waiters) == 0 {
			delete(w.decisions, toolCallID)
		} else {
			w.decisions[toolCallID] = waiters
		}
		return
	}
}

func (r *activeRun) markStopping() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stop = true
}

func (r *activeRun) isStopping() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stop
}
