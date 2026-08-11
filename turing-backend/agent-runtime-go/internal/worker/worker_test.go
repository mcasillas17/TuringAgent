package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
)

type blockingProvider struct {
	started   chan struct{}
	cancelled chan struct{}
}

func (p *blockingProvider) ID() string { return "ollama" }

func (p *blockingProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	close(p.started)
	out := make(chan llm.StreamEvent)
	go func() {
		defer close(out)
		<-ctx.Done()
		close(p.cancelled)
	}()
	return out, nil
}

func TestWorkerCancelsActiveRunAndAcknowledges(t *testing.T) {
	provider := &blockingProvider{started: make(chan struct{}), cancelled: make(chan struct{})}
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-1", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, providerExecutor{provider: provider})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	ready := nextSent(t, stream)
	if ready.GetWorkerReady() == nil || ready.GetWorkerReady().WorkerId != "worker-1" {
		t.Fatalf("first update = %+v, want worker_ready", ready)
	}

	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_1", RunId: "run_1", Model: "llama3.2"}}}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}

	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunCancelled{RunCancelled: &turingv1.RuntimeRunCancelled{RunId: "run_1", Reason: "client_cancelled"}}}
	select {
	case <-provider.cancelled:
	case <-time.After(time.Second):
		t.Fatal("provider context was not cancelled")
	}
	ack := nextSent(t, stream)
	if ack.GetRunCancelledAck() == nil || ack.GetRunCancelledAck().RunId != "run_1" {
		t.Fatalf("cancel update = %+v, want run_cancelled_ack", ack)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
}

func TestWorkerCancelsApprovalDeniedRunAndAcknowledgesExit(t *testing.T) {
	executor := &approvalWaitingExecutor{waiting: make(chan struct{}), cancelled: make(chan struct{})}
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-approval", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, executor)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	_ = nextSent(t, stream)
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_1", RunId: "run_1", TraceId: "trace_1"}}}
	beacon := nextSent(t, stream).GetToolBeacon()
	if beacon == nil || beacon.GetToolCallId() != "call_approval" {
		t.Fatalf("tool beacon = %+v, want approval beacon", beacon)
	}
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ToolPolicyDecision{ToolPolicyDecision: &turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
		ToolCallId: beacon.GetToolCallId(),
		ApprovalId: "approval_1",
	}}}
	select {
	case <-executor.waiting:
	case <-time.After(time.Second):
		t.Fatal("executor did not wait for approval")
	}

	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ApprovalUpdated{ApprovalUpdated: &turingv1.RuntimeApprovalUpdated{
		ApprovalId: "approval_1",
		Status:     "denied",
	}}}
	select {
	case <-executor.cancelled:
	case <-time.After(time.Second):
		t.Fatal("approval denial did not cancel active run")
	}
	ack := nextSent(t, stream)
	if ack.GetRunCancelledAck() == nil || ack.GetRunCancelledAck().GetRunId() != "run_1" {
		t.Fatalf("approval cancellation update = %+v, want run_cancelled_ack", ack)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
}

func TestWorkerHonorsLocalConcurrencyLimit(t *testing.T) {
	executor := &blockingExecutor{started: make(chan string, 2)}
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-capacity", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, executor)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	_ = nextSent(t, stream)
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_1", RunId: "run_1"}}}
	select {
	case runID := <-executor.started:
		if runID != "run_1" {
			t.Fatalf("first started run = %q, want run_1", runID)
		}
	case <-time.After(time.Second):
		t.Fatal("first run did not start")
	}
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_2", RunId: "run_2"}}}
	select {
	case runID := <-executor.started:
		t.Fatalf("second run started while capacity was full: %q", runID)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
}

func TestWorkerReportsTerminalUpdateOnlyAfterExecutionExits(t *testing.T) {
	executor := &terminalBlockingExecutor{reported: make(chan struct{}), release: make(chan struct{})}
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-terminal", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, executor)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	_ = nextSent(t, stream)
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_1", RunId: "run_1", AssistantMessageId: "msg_1"}}}
	select {
	case <-executor.reported:
	case <-time.After(time.Second):
		t.Fatal("executor did not emit terminal update")
	}
	select {
	case update := <-stream.sent:
		t.Fatalf("terminal update sent before execution exited: %+v", update)
	case <-time.After(50 * time.Millisecond):
	}
	close(executor.release)
	update := nextSent(t, stream)
	if update.GetRunCompleted() == nil || update.GetRunCompleted().GetRunId() != "run_1" {
		t.Fatalf("terminal update = %+v, want run_completed", update)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
}

func TestWorkerAcknowledgesExternallyTerminalizedRunAfterExecutionExits(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-external-terminal", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, terminalizedExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	_ = nextSent(t, stream)
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_1", RunId: "run_1"}}}
	update := nextSent(t, stream)
	if update.GetRunCancelledAck() == nil || update.GetRunCancelledAck().GetRunId() != "run_1" {
		t.Fatalf("terminalized run update = %+v, want run_cancelled_ack", update)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
}

func TestWorkerDoesNotSendDerivedMessageCompletedEvent(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-1", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	_ = nextSent(t, stream)

	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_1", RunId: "run_1", AssistantMessageId: "msg_assistant"}}}
	update := nextSent(t, stream)
	if update.GetEvent() != nil && update.GetEvent().Type == turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED {
		t.Fatalf("worker sent derived message.completed event: %+v", update)
	}
	if update.GetRunCompleted() == nil {
		t.Fatalf("update = %+v, want run_completed", update)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
}

func TestPostToolBeaconMarksErrorAfterBeaconSent(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-1", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := worker.postToolBeacon(ctx, stream, &turingv1.ToolCallBeacon{
		RunId:      "run_1",
		TraceId:    "trace_1",
		ToolCallId: "call_1",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "system",
		ToolName:   "system.echo",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("postToolBeacon error = %v, want context.Canceled", err)
	}
	var posted interface{ BeaconPosted() bool }
	if !errors.As(err, &posted) || !posted.BeaconPosted() {
		t.Fatalf("postToolBeacon error = %T %[1]v, want BeaconPosted marker", err)
	}
	update := nextSent(t, stream)
	if update.GetToolBeacon() == nil || update.GetToolBeacon().ToolCallId != "call_1" {
		t.Fatalf("sent update = %+v, want before beacon", update)
	}
}

func TestDelayedDecisionFromCancelledAttemptCannotSatisfyRetry(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-1", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := worker.postToolBeacon(firstCtx, stream, &turingv1.ToolCallBeacon{ToolCallId: "call_first"})
		firstDone <- err
	}()
	_ = nextSent(t, stream)
	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled attempt error = %v, want context.Canceled", err)
	}

	retryDone := make(chan *turingv1.ToolPolicyDecision, 1)
	go func() {
		decision, _ := worker.postToolBeacon(context.Background(), stream, &turingv1.ToolCallBeacon{ToolCallId: "call_retry"})
		retryDone <- decision
	}()
	_ = nextSent(t, stream)

	worker.deliverDecision(&turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: "call_first",
	})
	select {
	case decision := <-retryDone:
		t.Fatalf("delayed cancelled-attempt decision satisfied retry: %+v", decision)
	case <-time.After(20 * time.Millisecond):
	}

	worker.deliverDecision(&turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: "call_retry",
	})
	select {
	case decision := <-retryDone:
		if decision.GetToolCallId() != "call_retry" {
			t.Fatalf("retry decision = %+v", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("matching retry decision was not delivered")
	}
}

func TestDelayedBeforeDecisionWithSameToolCallIDCannotSatisfyAfter(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-1", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	beforeCtx, cancelBefore := context.WithCancel(context.Background())
	defer cancelBefore()
	afterCtx, cancelAfter := context.WithCancel(context.Background())
	defer cancelAfter()
	type result struct {
		decision *turingv1.ToolPolicyDecision
		err      error
	}
	beforeDone := make(chan result, 1)
	afterDone := make(chan result, 1)

	go func() {
		decision, err := worker.postToolBeacon(beforeCtx, stream, &turingv1.ToolCallBeacon{
			ToolCallId: "call_same",
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		})
		beforeDone <- result{decision: decision, err: err}
	}()
	_ = nextSent(t, stream)
	go func() {
		decision, err := worker.postToolBeacon(afterCtx, stream, &turingv1.ToolCallBeacon{
			ToolCallId: "call_same",
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
		})
		afterDone <- result{decision: decision, err: err}
	}()
	_ = nextSent(t, stream)

	worker.deliverDecision(&turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: "call_same",
	})
	select {
	case result := <-afterDone:
		t.Fatalf("delayed BEFORE decision satisfied AFTER waiter: %+v", result)
	case result := <-beforeDone:
		if result.err != nil || result.decision.GetToolCallId() != "call_same" {
			t.Fatalf("BEFORE decision result = %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("delayed BEFORE decision did not satisfy BEFORE waiter")
	}
	select {
	case result := <-afterDone:
		t.Fatalf("AFTER waiter completed before its decision arrived: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}

	worker.deliverDecision(&turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: "call_same",
	})
	select {
	case result := <-afterDone:
		if result.err != nil || result.decision.GetToolCallId() != "call_same" {
			t.Fatalf("AFTER decision result = %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("matching AFTER decision was not delivered")
	}
}

type providerExecutor struct{ provider llm.Provider }

type approvalWaitingExecutor struct {
	poster    func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)
	waiting   chan struct{}
	cancelled chan struct{}
}

type blockingExecutor struct {
	started chan string
}

func (e *blockingExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, _ func(*turingv1.RuntimeUpdate) error) error {
	e.started <- job.GetRunId()
	<-ctx.Done()
	return ctx.Err()
}

type terminalBlockingExecutor struct {
	reported chan struct{}
	release  chan struct{}
}

type terminalizedExecutor struct{}

func (terminalizedExecutor) Execute(context.Context, *turingv1.AgentJob, func(*turingv1.RuntimeUpdate) error) error {
	return terminalizedWorkerError{}
}

type terminalizedWorkerError struct{}

func (terminalizedWorkerError) Error() string     { return "run already terminal" }
func (terminalizedWorkerError) RunTerminal() bool { return true }

func (e *terminalBlockingExecutor) Execute(_ context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error {
	if err := emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId: job.GetRunId(), AssistantMessageId: job.GetAssistantMessageId(), Content: "done",
	}}}); err != nil {
		return err
	}
	close(e.reported)
	<-e.release
	return nil
}

func (e *approvalWaitingExecutor) SetToolBeaconPoster(post func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)) {
	e.poster = post
}

func (e *approvalWaitingExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, _ func(*turingv1.RuntimeUpdate) error) error {
	if e.poster == nil {
		return errors.New("tool beacon poster is not configured")
	}
	decision, err := e.poster(ctx, &turingv1.ToolCallBeacon{
		RunId:      job.GetRunId(),
		TraceId:    job.GetTraceId(),
		ToolCallId: "call_approval",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	})
	if err != nil {
		return err
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED {
		return errors.New("approval decision is required")
	}
	close(e.waiting)
	<-ctx.Done()
	close(e.cancelled)
	return ctx.Err()
}

func (e providerExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error {
	events, err := e.provider.StreamChat(ctx, llm.ChatRequest{Model: job.Model})
	if err != nil {
		return err
	}
	for range events {
	}
	return ctx.Err()
}

type terminalExecutor struct{}

func (terminalExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error {
	if err := emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{RunId: job.GetRunId(), Type: turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED}}}); err != nil {
		return err
	}
	return emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{RunId: job.GetRunId(), AssistantMessageId: job.GetAssistantMessageId(), Content: "done"}}})
}

type fakeRuntimeClient struct{ stream *fakeStream }

func (c *fakeRuntimeClient) ConnectWorker(ctx context.Context) (RuntimeStream, error) {
	c.stream.ctx = ctx
	return c.stream, nil
}

type fakeStream struct {
	ctx  context.Context
	sent chan *turingv1.RuntimeUpdate
	recv chan *turingv1.RuntimeCommand
}

func newFakeStream() *fakeStream {
	return &fakeStream{sent: make(chan *turingv1.RuntimeUpdate, 8), recv: make(chan *turingv1.RuntimeCommand, 8)}
}

func (s *fakeStream) Send(update *turingv1.RuntimeUpdate) error {
	s.sent <- update
	return nil
}

func (s *fakeStream) Recv() (*turingv1.RuntimeCommand, error) {
	select {
	case cmd := <-s.recv:
		return cmd, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *fakeStream) CloseSend() error { return nil }

func nextSent(t *testing.T, stream *fakeStream) *turingv1.RuntimeUpdate {
	t.Helper()
	select {
	case update := <-stream.sent:
		return update
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime update")
	}
	return nil
}
