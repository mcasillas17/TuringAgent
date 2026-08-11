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
	decisionMu sync.Mutex
	decisions  map[string]chan *turingv1.ToolPolicyDecision
	sendMu     sync.Mutex
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
	return &Worker{options: options, client: client, executor: executor, active: map[string]*activeRun{}, decisions: map[string]chan *turingv1.ToolPolicyDecision{}}
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
		}
	}
}

func (w *Worker) startRun(parent context.Context, stream RuntimeStream, job *turingv1.AgentJob) {
	runCtx, cancel := context.WithCancel(parent)
	entry := &activeRun{cancel: cancel, done: make(chan struct{})}
	w.mu.Lock()
	w.active[job.GetRunId()] = entry
	w.mu.Unlock()
	go func() {
		defer close(entry.done)
		defer func() {
			if !entry.isStopping() {
				w.deleteActive(job.GetRunId())
			}
		}()
		err := w.executor.Execute(runCtx, job, func(update *turingv1.RuntimeUpdate) error {
			if isDerivedMessageCompleted(update) {
				return nil
			}
			return w.send(stream, update)
		})
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

func (w *Worker) deleteActive(runID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.active, runID)
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
	waiter := make(chan *turingv1.ToolPolicyDecision, 1)
	w.decisionMu.Lock()
	w.decisions[beacon.ToolCallId] = waiter
	w.decisionMu.Unlock()
	defer func() {
		w.decisionMu.Lock()
		delete(w.decisions, beacon.ToolCallId)
		w.decisionMu.Unlock()
	}()
	if err := w.send(stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: beacon}}); err != nil {
		return nil, err
	}
	select {
	case decision := <-waiter:
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
	waiter := w.decisions[decision.ToolCallId]
	w.decisionMu.Unlock()
	if waiter == nil {
		return
	}
	select {
	case waiter <- decision:
	default:
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
