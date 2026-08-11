package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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
	writerMu   sync.Mutex
	writer     *outboundWriter
}

type decisionWaiter struct {
	decision  chan *turingv1.ToolPolicyDecision
	tombstone bool
}

type activeRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	stop   bool
}

const terminalUpdateSendTimeout = 5 * time.Second

var errOutboundWriterStopped = errors.New("runtime outbound writer stopped")

type outboundWriter struct {
	stream RuntimeStream
	queue  chan *outboundRequest
	done   chan struct{}
	exited chan struct{}
	once   sync.Once
	mu     sync.Mutex
	err    error
}

type outboundRequest struct {
	ctx    context.Context
	update *turingv1.RuntimeUpdate
	result chan error
}

func newOutboundWriter(stream RuntimeStream) *outboundWriter {
	writer := &outboundWriter{
		stream: stream,
		queue:  make(chan *outboundRequest, 64),
		done:   make(chan struct{}),
		exited: make(chan struct{}),
	}
	go writer.run()
	return writer
}

func (w *outboundWriter) run() {
	defer close(w.exited)
	for {
		select {
		case <-w.done:
			return
		case request := <-w.queue:
			select {
			case <-w.done:
				return
			default:
			}
			if err := request.ctx.Err(); err != nil {
				request.complete(err)
				continue
			}
			err := w.stream.Send(request.update)
			request.complete(err)
			if err != nil {
				w.stop(err)
				return
			}
		}
	}
}

func (w *outboundWriter) send(ctx context.Context, update *turingv1.RuntimeUpdate) error {
	if ctx == nil {
		ctx = context.Background()
	}
	request := &outboundRequest{ctx: ctx, update: update, result: make(chan error, 1)}
	select {
	case <-w.done:
		return w.error()
	default:
	}
	select {
	case w.queue <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return w.error()
	}
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return w.error()
	}
}

func (w *outboundWriter) stop(err error) {
	if err == nil {
		err = errOutboundWriterStopped
	}
	w.once.Do(func() {
		w.mu.Lock()
		w.err = err
		w.mu.Unlock()
		close(w.done)
	})
}

func (w *outboundWriter) error() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err == nil {
		return errOutboundWriterStopped
	}
	return w.err
}

func (r *outboundRequest) complete(err error) {
	select {
	case r.result <- err:
	default:
	}
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
	w.startOutboundWriter(stream)
	defer func() {
		w.stopOutboundWriter(stream)
		_ = stream.CloseSend()
	}()
	if setter, ok := w.executor.(BeaconPosterSetter); ok {
		setter.SetToolBeaconPoster(func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			return w.postToolBeacon(ctx, stream, beacon)
		})
	}
	if err := w.send(ctx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: w.options.WorkerID, AgentId: w.options.AgentID, MaxConcurrentRuns: int32(w.options.MaxConcurrentRuns)}}}); err != nil {
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
			return w.send(runCtx, stream, update)
		})
		if entry.isStopping() {
			return
		}
		w.deleteActive(job.GetRunId())
		if terminal != nil {
			_ = w.sendTerminalUpdate(runCtx, stream, terminal)
			return
		}
		if runWasTerminalized(err) {
			_ = w.sendTerminalUpdate(runCtx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: job.GetRunId()}}})
			return
		}
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(runCtx.Err(), context.Canceled) {
			_ = w.sendTerminalUpdate(runCtx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{RunId: job.GetRunId(), Code: "runtime_error", Message: err.Error(), Retryable: false}}})
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
	return w.sendTerminalUpdate(ctx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: runID}}})
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

func (w *Worker) startOutboundWriter(stream RuntimeStream) {
	w.writerMu.Lock()
	defer w.writerMu.Unlock()
	if w.writer != nil {
		return
	}
	w.writer = newOutboundWriter(stream)
}

func (w *Worker) stopOutboundWriter(_ RuntimeStream) {
	w.writerMu.Lock()
	writer := w.writer
	w.writerMu.Unlock()
	if writer != nil {
		writer.stop(nil)
	}
}

func (w *Worker) send(ctx context.Context, _ RuntimeStream, update *turingv1.RuntimeUpdate) error {
	if update == nil {
		return fmt.Errorf("runtime update is required")
	}
	w.writerMu.Lock()
	writer := w.writer
	w.writerMu.Unlock()
	if writer == nil {
		return errors.New("runtime outbound writer is not initialized")
	}
	return writer.send(ctx, update)
}

func (w *Worker) sendTerminalUpdate(ctx context.Context, stream RuntimeStream, update *turingv1.RuntimeUpdate) error {
	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), terminalUpdateSendTimeout)
	defer cancel()
	return w.send(reportCtx, stream, update)
}

func (w *Worker) postToolBeacon(ctx context.Context, stream RuntimeStream, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
	if beacon == nil || beacon.ToolCallId == "" {
		return nil, errors.New("tool beacon with tool_call_id is required")
	}
	waiter := &decisionWaiter{decision: make(chan *turingv1.ToolPolicyDecision, 1)}
	sent := false
	w.decisionMu.Lock()
	w.decisions[beacon.ToolCallId] = append(w.decisions[beacon.ToolCallId], waiter)
	w.decisionMu.Unlock()
	defer func() {
		w.retireDecisionWaiter(beacon.ToolCallId, waiter, sent)
		w.mu.Lock()
		delete(w.toolCalls, beacon.ToolCallId)
		w.mu.Unlock()
	}()
	w.mu.Lock()
	w.toolCalls[beacon.ToolCallId] = beacon.RunId
	w.mu.Unlock()
	if err := w.send(ctx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: beacon}}); err != nil {
		return nil, err
	}
	sent = true
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
	tombstone := waiter.tombstone
	if len(waiters) == 1 {
		delete(w.decisions, decision.ToolCallId)
	} else {
		w.decisions[decision.ToolCallId] = waiters[1:]
	}
	w.decisionMu.Unlock()
	if tombstone {
		return
	}
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

func (w *Worker) retireDecisionWaiter(toolCallID string, target *decisionWaiter, sent bool) {
	w.decisionMu.Lock()
	defer w.decisionMu.Unlock()
	waiters := w.decisions[toolCallID]
	for index, waiter := range waiters {
		if waiter != target {
			continue
		}
		if sent {
			// Responses carry no phase or generation. Preserve this slot so a
			// delayed response cannot be delivered to a later same-ID request.
			waiter.tombstone = true
			return
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
