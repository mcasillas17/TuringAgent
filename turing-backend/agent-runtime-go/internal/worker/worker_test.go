package worker

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
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
	waitForOutboundWriterExit(t, worker)
}

func TestWorkerCancelsApprovalDeniedRunAndAcknowledgesExit(t *testing.T) {
	executor := &approvalWaitingExecutor{waiting: make(chan struct{}), cancelled: make(chan struct{}), cause: make(chan error, 1)}
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
	select {
	case cause := <-executor.cause:
		var terminal interface{ TerminalApproval() bool }
		if !errors.As(cause, &terminal) || !terminal.TerminalApproval() || cause.Error() != "approval denied" {
			t.Fatalf("approval cancellation cause = %T %v, want terminal approval denial", cause, cause)
		}
	case <-time.After(time.Second):
		t.Fatal("approval denial did not preserve a terminal cancellation cause")
	}
	ack := nextSent(t, stream)
	if ack.GetRunCancelledAck() == nil || ack.GetRunCancelledAck().GetRunId() != "run_1" {
		t.Fatalf("approval cancellation update = %+v, want run_cancelled_ack", ack)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
	waitForOutboundWriterExit(t, worker)
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
	waitForOutboundWriterExit(t, worker)
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
	waitForOutboundWriterExit(t, worker)
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

func TestPostToolBeaconDoesNotMarkQueueRejectedBeaconPosted(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-1", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	worker.startOutboundWriter(stream)
	defer worker.stopOutboundWriter(stream)
	worker.writerMu.Lock()
	writer := worker.writer
	worker.writerMu.Unlock()
	writer.stop(errors.New("queue rejected"))
	waitForOutboundWriterExit(t, worker)

	_, err := worker.postToolBeacon(context.Background(), stream, &turingv1.ToolCallBeacon{
		RunId:      "run_1",
		TraceId:    "trace_1",
		ToolCallId: "call_1",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "system",
		ToolName:   "system.echo",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	})

	if err == nil || !strings.Contains(err.Error(), "queue rejected") {
		t.Fatalf("postToolBeacon error = %v, want queue rejection", err)
	}
	if tools.BeaconWasPosted(err) {
		t.Fatalf("postToolBeacon error = %T %[1]v, must not claim queue-rejected beacon was sent", err)
	}
	select {
	case update := <-stream.sent:
		t.Fatalf("queue-rejected beacon reached Send: %+v", update)
	default:
	}
}

func TestWorkerMarksStartedBlockedBeaconAndAttemptsFailedAfter(t *testing.T) {
	stream := newFakeStream()
	blockedSend := make(chan struct{})
	releaseSend := make(chan struct{})
	var inFlight, maxInFlight atomic.Int32
	stream.sendFn = func(update *turingv1.RuntimeUpdate) error {
		current := inFlight.Add(1)
		for {
			previous := maxInFlight.Load()
			if current <= previous || maxInFlight.CompareAndSwap(previous, current) {
				break
			}
		}
		defer inFlight.Add(-1)
		if beacon := update.GetToolBeacon(); beacon != nil && beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
			select {
			case <-blockedSend:
			default:
				close(blockedSend)
			}
			<-releaseSend
		}
		stream.sent <- update
		return nil
	}
	worker := New(Options{WorkerID: "worker-blocked-send", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	worker.startOutboundWriter(stream)
	defer worker.stopOutboundWriter(stream)
	runner := &tools.Runner{PostBeacon: func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		return worker.postToolBeacon(ctx, stream, beacon)
	}}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan struct {
		err     error
		elapsed time.Duration
	}, 1)
	started := time.Now()
	go func() {
		_, err := runner.Run(runCtx, tools.RunInput{
			AgentID:      turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			RunID:        "run_1",
			TraceID:      "trace_1",
			ServerName:   "system",
			ToolName:     "system.echo",
			MCPClient:    workerMCPClient{},
			Timeout:      100 * time.Millisecond,
			TotalTimeout: 500 * time.Millisecond,
		})
		result <- struct {
			err     error
			elapsed time.Duration
		}{err: err, elapsed: time.Since(started)}
	}()
	select {
	case <-blockedSend:
	case <-time.After(time.Second):
		t.Fatal("tool beacon send did not block")
	}
	cancel()
	waitForQueuedOutboundRequest(t, worker, 1)
	close(releaseSend)
	before := nextSent(t, stream).GetToolBeacon()
	if before == nil || before.GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
		t.Fatalf("started beacon = %+v, want delivered BEFORE", before)
	}
	after := nextSent(t, stream).GetToolBeacon()
	if after == nil || after.GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER ||
		after.GetStatus() != turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED ||
		after.GetError().GetCode() != "tool_policy_decision_failed" {
		t.Fatalf("cleanup beacon = %+v, want failed AFTER", after)
	}
	worker.deliverDecision(&turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: before.GetToolCallId(),
	})
	worker.deliverDecision(&turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: after.GetToolCallId(),
	})
	got := <-result
	if !errors.Is(got.err, context.Canceled) || !tools.BeaconWasPosted(got.err) {
		t.Fatalf("blocked send result = %T %v, want delivery-unknown cancellation", got.err, got.err)
	}
	if got.elapsed > time.Second {
		t.Fatalf("blocked send operation took %v, want bounded cleanup", got.elapsed)
	}
	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("concurrent stream sends = %d, want 1", got)
	}
}

func TestWorkerBoundsCleanupBehindBlockedStartedBeacon(t *testing.T) {
	stream := newFakeStream()
	blockedSend := make(chan struct{})
	releaseSend := make(chan struct{})
	released := false
	stream.sendFn = func(update *turingv1.RuntimeUpdate) error {
		if beacon := update.GetToolBeacon(); beacon != nil && beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
			close(blockedSend)
			<-releaseSend
		}
		stream.sent <- update
		return nil
	}
	worker := New(Options{WorkerID: "worker-blocked-cleanup", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	worker.startOutboundWriter(stream)
	defer func() {
		if !released {
			close(releaseSend)
		}
		worker.stopOutboundWriter(stream)
		waitForOutboundWriterExit(t, worker)
	}()
	runner := &tools.Runner{PostBeacon: func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		return worker.postToolBeacon(ctx, stream, beacon)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := runner.Run(ctx, tools.RunInput{
			AgentID:      turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			RunID:        "run_1",
			TraceID:      "trace_1",
			ServerName:   "system",
			ToolName:     "system.echo",
			MCPClient:    workerMCPClient{},
			Timeout:      20 * time.Millisecond,
			TotalTimeout: 100 * time.Millisecond,
		})
		result <- err
	}()
	select {
	case <-blockedSend:
	case <-time.After(time.Second):
		t.Fatal("tool beacon send did not block")
	}
	cancel()
	waitForQueuedOutboundRequest(t, worker, 1)
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) ||
			!tools.BeaconWasPosted(err) || !tools.ReportingFailed(err) {
			t.Fatalf("blocked cleanup result = %T %v, want combined delivery-unknown/reporting failure", err, err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("blocked cleanup did not respect reporting timeout")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("blocked cleanup took %v, want bounded reporting timeout", elapsed)
	}
	close(releaseSend)
	released = true
}

func TestWorkerAttemptsFailedAfterWhenAcceptedBeforeResponseTimesOut(t *testing.T) {
	stream := newFakeStream()
	executor := newTimeoutBeaconExecutor(20*time.Millisecond, 40*time.Millisecond)
	worker := New(Options{WorkerID: "worker-blocked-response", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, executor)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	_ = nextSent(t, stream)
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{
		JobId: "job_1", RunId: "run_1", TraceId: "trace_1",
	}}}
	waitForStartedRun(t, executor.started, "run_1")
	before := nextSent(t, stream).GetToolBeacon()
	if before == nil || before.GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
		t.Fatalf("first beacon = %+v, want accepted BEFORE beacon", before)
	}
	after := nextSent(t, stream).GetToolBeacon()
	if after == nil || after.GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER ||
		after.GetStatus() != turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED ||
		after.GetError().GetCode() != "tool_policy_decision_failed" {
		t.Fatalf("cleanup beacon = %+v, want failed AFTER beacon", after)
	}
	result := waitForRunnerResult(t, executor.results)
	if !errors.Is(result.err, context.DeadlineExceeded) || !tools.BeaconWasPosted(result.err) || !tools.ReportingFailed(result.err) {
		t.Fatalf("blocked response result = %T %v, want bounded combined reporting failure", result.err, result.err)
	}
	if result.elapsed > 500*time.Millisecond {
		t.Fatalf("blocked response operation took %v, want bounded cleanup", result.elapsed)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
	waitForOutboundWriterExit(t, worker)
}

func TestDelayedDecisionFromCancelledAttemptCannotSatisfyRetry(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-1", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	worker.startOutboundWriter(stream)
	defer worker.stopOutboundWriter(stream)

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
	worker.startOutboundWriter(stream)
	defer worker.stopOutboundWriter(stream)
	beforeCtx, cancelBefore := context.WithCancel(context.Background())
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
	cancelBefore()
	select {
	case result := <-beforeDone:
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("cancelled BEFORE result = %+v, want context.Canceled", result)
		}

	case <-time.After(time.Second):
		t.Fatal("BEFORE waiter did not cancel")
	}

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

func TestWorkerContinuesReceivingCleanupDecisionWhileCancellationWaitsForExecutor(t *testing.T) {
	executor := &cancelCleanupExecutor{
		waitingForCancel: make(chan struct{}),
		cleanupStarted:   make(chan struct{}),
		cleanupFinished:  make(chan struct{}),
	}
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-cancel-cleanup", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, executor)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	_ = nextSent(t, stream)
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{
		JobId: "job_1", RunId: "run_1", TraceId: "trace_1",
	}}}
	before := nextSent(t, stream).GetToolBeacon()
	if before == nil || before.GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
		t.Fatalf("first update = %+v, want BEFORE beacon", before)
	}
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ToolPolicyDecision{ToolPolicyDecision: &turingv1.ToolPolicyDecision{
		Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: before.GetToolCallId(),
	}}}
	select {
	case <-executor.waitingForCancel:
	case <-time.After(time.Second):
		t.Fatal("executor did not begin waiting for cancellation")
	}

	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunCancelled{RunCancelled: &turingv1.RuntimeRunCancelled{
		RunId: "run_1", Reason: "client_cancelled",
	}}}
	after := nextSent(t, stream).GetToolBeacon()
	if after == nil || after.GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
		t.Fatalf("cleanup update = %+v, want AFTER beacon", after)
	}
	select {
	case <-executor.cleanupStarted:
	case <-time.After(time.Second):
		t.Fatal("executor did not wait for cleanup decision")
	}
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ToolPolicyDecision{ToolPolicyDecision: &turingv1.ToolPolicyDecision{
		Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: after.GetToolCallId(),
	}}}
	select {
	case <-executor.cleanupFinished:
	case <-time.After(time.Second):
		t.Fatal("receive loop did not deliver cleanup decision during cancellation")
	}
	ack := nextSent(t, stream)
	if ack.GetRunCancelledAck() == nil || ack.GetRunCancelledAck().GetRunId() != "run_1" {
		t.Fatalf("terminal update = %+v, want cancellation acknowledgement", ack)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}
	waitForOutboundWriterExit(t, worker)
}

type providerExecutor struct{ provider llm.Provider }

type approvalWaitingExecutor struct {
	poster    func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)
	waiting   chan struct{}
	cancelled chan struct{}
	cause     chan error
}

type cancelCleanupExecutor struct {
	poster           func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)
	waitingForCancel chan struct{}
	cleanupStarted   chan struct{}
	cleanupFinished  chan struct{}
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

func (e *cancelCleanupExecutor) SetToolBeaconPoster(post func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)) {
	e.poster = post
}

func (e *cancelCleanupExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, _ func(*turingv1.RuntimeUpdate) error) error {
	if e.poster == nil {
		return errors.New("tool beacon poster is not configured")
	}
	before, err := e.poster(ctx, &turingv1.ToolCallBeacon{
		RunId: job.GetRunId(), TraceId: job.GetTraceId(), ToolCallId: "call_cancel_cleanup",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "system", ToolName: "system.time",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	})
	if err != nil {
		return err
	}
	if before.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		return errors.New("before decision was not allow")
	}
	close(e.waitingForCancel)
	<-ctx.Done()
	close(e.cleanupStarted)
	cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = e.poster(cleanupCtx, &turingv1.ToolCallBeacon{
		RunId: job.GetRunId(), TraceId: job.GetTraceId(), ToolCallId: "call_cancel_cleanup",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "system", ToolName: "system.time",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER, Status: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
		Error: &turingv1.ToolCallError{Code: "cancelled", Message: "cancelled"},
	})
	if err != nil {
		return err
	}
	close(e.cleanupFinished)
	return terminalizedWorkerError{}
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
	if e.cause != nil {
		e.cause <- context.Cause(ctx)
	}
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

type timeoutBeaconExecutor struct {
	poster  func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)
	started chan string
	results chan runnerResult
	timeout time.Duration
	total   time.Duration
}

type runnerResult struct {
	err     error
	elapsed time.Duration
}

func newTimeoutBeaconExecutor(timeout time.Duration, total time.Duration) *timeoutBeaconExecutor {
	return &timeoutBeaconExecutor{
		started: make(chan string, 2),
		results: make(chan runnerResult, 1),
		timeout: timeout,
		total:   total,
	}
}

func (e *timeoutBeaconExecutor) SetToolBeaconPoster(post func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)) {
	e.poster = post
}

func (e *timeoutBeaconExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, _ func(*turingv1.RuntimeUpdate) error) error {
	e.started <- job.GetRunId()
	if job.GetRunId() != "run_1" {
		<-ctx.Done()
		return ctx.Err()
	}
	started := time.Now()
	runner := &tools.Runner{PostBeacon: e.poster}
	_, err := runner.Run(ctx, tools.RunInput{
		AgentID:      turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		RunID:        job.GetRunId(),
		TraceID:      job.GetTraceId(),
		ServerName:   "system",
		ToolName:     "system.echo",
		MCPClient:    workerMCPClient{},
		Timeout:      e.timeout,
		TotalTimeout: e.total,
	})
	e.results <- runnerResult{err: err, elapsed: time.Since(started)}
	return err
}

type workerMCPClient struct{}

func (workerMCPClient) CallTool(context.Context, string, map[string]any, ...string) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

type fakeRuntimeClient struct{ stream *fakeStream }

func (c *fakeRuntimeClient) ConnectWorker(ctx context.Context) (RuntimeStream, error) {
	c.stream.ctx = ctx
	return c.stream, nil
}

type fakeStream struct {
	ctx    context.Context
	sent   chan *turingv1.RuntimeUpdate
	recv   chan *turingv1.RuntimeCommand
	sendFn func(*turingv1.RuntimeUpdate) error
}

func newFakeStream() *fakeStream {
	return &fakeStream{sent: make(chan *turingv1.RuntimeUpdate, 8), recv: make(chan *turingv1.RuntimeCommand, 8)}
}

func (s *fakeStream) Send(update *turingv1.RuntimeUpdate) error {
	if s.sendFn != nil {
		return s.sendFn(update)
	}
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

func waitForStartedRun(t *testing.T, started <-chan string, runID string) {
	t.Helper()
	select {
	case got := <-started:
		if got != runID {
			t.Fatalf("started run = %q, want %q", got, runID)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for run %q to start", runID)
	}
}

func waitForRunnerResult(t *testing.T, results <-chan runnerResult) runnerResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bounded runner result")
	}
	return runnerResult{}
}

func waitForInactiveRun(t *testing.T, worker *Worker, runID string) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if worker.activeRun(runID) == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("run %q remained active after executor exit", runID)
		case <-ticker.C:
		}
	}
}

func waitForOutboundWriterExit(t *testing.T, worker *Worker) {
	t.Helper()
	worker.writerMu.Lock()
	writer := worker.writer
	worker.writerMu.Unlock()
	if writer == nil {
		t.Fatal("worker outbound writer was not initialized")
	}
	select {
	case <-writer.exited:
	case <-time.After(time.Second):
		t.Fatal("worker outbound writer did not exit")
	}
}

func waitForQueuedOutboundRequest(t *testing.T, worker *Worker, want int) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		worker.writerMu.Lock()
		writer := worker.writer
		queued := 0
		if writer != nil {
			queued = len(writer.queue)
		}
		worker.writerMu.Unlock()
		if queued >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("outbound queue length remained %d, want at least %d", queued, want)
		case <-ticker.C:
		}
	}
}
