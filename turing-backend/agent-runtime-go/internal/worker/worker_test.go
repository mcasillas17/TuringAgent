package worker

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
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

func (p *blockingProvider) ContextWindowTokens() int { return llm.DefaultContextWindowTokens }
func (p *blockingProvider) MaxOutputTokens() int     { return llm.DefaultMaxOutputTokens }
func (p *blockingProvider) EstimateRequestTokens(req llm.ChatRequest) (int, error) {
	body, err := json.Marshal(req)
	return len(body), err
}

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

func TestWorkerEmitsPeriodicAuthenticatedHeartbeat(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{
		WorkerID: "worker-heartbeat", MaxConcurrentRuns: 1, HeartbeatInterval: time.Millisecond,
	}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	_ = nextSent(t, stream)

	update := nextSent(t, stream)
	heartbeat := update.GetHeartbeat()
	if heartbeat == nil || heartbeat.GetWorkerId() != "worker-heartbeat" {
		t.Fatalf("periodic update = %+v, want authenticated heartbeat", update)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Worker.Run returned %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Worker.Run did not stop after heartbeat test")
	}
}

func TestWorkerRejectsMaxConcurrentRunsAboveWireAndServerBound(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{
		WorkerID: "worker-overflow", MaxConcurrentRuns: 2147483648,
	}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})

	err := worker.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "max concurrent runs must be between 1 and 128") {
		t.Fatalf("Worker.Run max concurrent error = %v, want bounded validation", err)
	}
	// The sentinel is what a caller looping over Run keys on to stop rather than
	// retry a configuration that can never succeed.
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("validation error %v does not wrap ErrInvalidConfig", err)
	}
	select {
	case update := <-stream.sent:
		t.Fatalf("invalid max concurrent reached protobuf send: %+v", update)
	default:
	}
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
		Phase:      beacon.GetPhase(),
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
	defer worker.stopOutboundWriter()
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

func TestOutboundWriterCanRestartForASubsequentStream(t *testing.T) {
	firstStream := newFakeStream()
	worker := New(Options{WorkerID: "worker-reconnect", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: firstStream}, terminalExecutor{})
	worker.startOutboundWriter(firstStream)
	if err := worker.send(context.Background(), firstStream, &turingv1.RuntimeUpdate{}); err != nil {
		t.Fatalf("send on first stream: %v", err)
	}
	worker.stopOutboundWriter()

	secondStream := newFakeStream()
	worker.startOutboundWriter(secondStream)
	defer worker.stopOutboundWriter()
	if err := worker.send(context.Background(), secondStream, &turingv1.RuntimeUpdate{}); err != nil {
		t.Fatalf("send on second stream: %v", err)
	}
	select {
	case <-secondStream.sent:
	case <-time.After(time.Second):
		t.Fatal("second stream did not receive update")
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
	defer worker.stopOutboundWriter()
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
		Phase:      before.GetPhase(),
	})
	worker.deliverDecision(&turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: after.GetToolCallId(),
		Phase:      after.GetPhase(),
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
		worker.stopOutboundWriter()
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
	defer worker.stopOutboundWriter()

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
	defer worker.stopOutboundWriter()
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
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	})
	select {
	case result := <-afterDone:
		t.Fatalf("delayed BEFORE decision satisfied AFTER waiter: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}

	worker.deliverDecision(&turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: "call_same",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
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
		Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: before.GetToolCallId(), Phase: before.GetPhase(),
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
		Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: after.GetToolCallId(), Phase: after.GetPhase(),
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

type fakeRuntimeClient struct {
	stream *fakeStream
	// queued streams are handed out in order before falling back to stream, so a
	// test can drive more than one connection (see the reuse test).
	queued []*fakeStream
}

func (c *fakeRuntimeClient) ConnectWorker(ctx context.Context) (RuntimeStream, error) {
	if len(c.queued) > 0 {
		next := c.queued[0]
		c.queued = c.queued[1:]
		next.ctx = ctx
		return next, nil
	}
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
		return
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

// A worker that cannot take an assignment must say so. Silence is
// indistinguishable from "accepted and running", so the orchestrator waits on a
// run that will never start. Reachable once the runtime reconnects: a run still
// draining from the previous stream can outlive DisconnectCleanupTimeout and
// keep the concurrency slot occupied.
func TestWorkerRejectsAssignmentItCannotRun(t *testing.T) {
	provider := &blockingProvider{started: make(chan struct{}), cancelled: make(chan struct{})}
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-1", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, providerExecutor{provider: provider})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	nextSent(t, stream) // worker_ready

	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_1", RunId: "run_1", Model: "llama3.2"}}}
	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first run never started")
	}

	// The slot is full; this second assignment cannot be served.
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_2", RunId: "run_2", Model: "llama3.2"}}}

	update := nextSent(t, stream)
	failed := update.GetRunFailed()
	if failed == nil {
		t.Fatalf("update = %+v, want a run_failed rejecting the assignment", update)
	}
	if failed.RunId != "run_2" {
		t.Fatalf("rejected run_id = %q, want run_2", failed.RunId)
	}
	if !retryableFailure(failed) {
		t.Fatal("rejection must be retryable: the worker was busy, the run is not broken")
	}
	cancel()
	<-done
}

// The reconnect loop calls Run repeatedly on the SAME Worker, and Worker carries
// state across calls (active runs, the outbound writer, the fatal channel).
// Nothing else exercises a second Run, so nothing else would catch a change that
// breaks reuse — which is the entire premise of reconnecting.
func TestWorkerCanRunAgainAfterStreamLoss(t *testing.T) {
	provider := &blockingProvider{started: make(chan struct{}), cancelled: make(chan struct{})}
	first, second := newFakeStream(), newFakeStream()
	client := &fakeRuntimeClient{stream: second, queued: []*fakeStream{first}}
	worker := New(Options{WorkerID: "worker-1", MaxConcurrentRuns: 1, DisconnectCleanupTimeout: time.Second}, client, providerExecutor{provider: provider})

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(firstCtx) }()

	if ready := nextSent(t, first); ready.GetWorkerReady() == nil {
		t.Fatalf("first stream: update = %+v, want worker_ready", ready)
	}
	first.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_1", RunId: "run_1", Model: "llama3.2"}}}
	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("run never started on the first stream")
	}

	// Lose the connection with a run in flight — the case reconnect exists for.
	cancelFirst()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after the stream was lost")
	}

	// Same Worker, second stream.
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	go func() { done <- worker.Run(secondCtx) }()

	if ready := nextSent(t, second); ready.GetWorkerReady() == nil {
		t.Fatalf("second stream: update = %+v, want a fresh worker_ready", ready)
	}

	// And it must actually accept work: a stale entry left in w.active would make
	// the reconnected worker silently refuse every assignment.
	restarted := &blockingProvider{started: make(chan struct{}), cancelled: make(chan struct{})}
	worker.executor = providerExecutor{provider: restarted}
	second.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_2", RunId: "run_2", Model: "llama3.2"}}}
	select {
	case <-restarted.started:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnected worker did not accept a new run")
	}
	cancelSecond()
	<-done
}

// A redelivered assignment for a run already in flight is a duplicate, not a
// rejection. Acking it would terminally fail a healthy run that keeps executing
// and then reports a completion the orchestrator refuses. Silence is correct.
func TestWorkerSilentlyIgnoresDuplicateAssignment(t *testing.T) {
	provider := &blockingProvider{started: make(chan struct{}), cancelled: make(chan struct{})}
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-1", MaxConcurrentRuns: 2}, &fakeRuntimeClient{stream: stream}, providerExecutor{provider: provider})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	nextSent(t, stream) // worker_ready

	assign := &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{JobId: "job_1", RunId: "run_1", Model: "llama3.2"}}}
	stream.recv <- assign
	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("run never started")
	}

	// Redeliver the same assignment. There is capacity, so the only reason to
	// refuse is that it is already running.
	stream.recv <- assign

	select {
	case update := <-stream.sent:
		if failed := update.GetRunFailed(); failed != nil {
			t.Fatalf("duplicate assignment was failed (%s); it must be ignored", failed.Code)
		}
		t.Fatalf("unexpected update for a duplicate assignment: %+v", update)
	case <-time.After(300 * time.Millisecond):
		// No update: correct.
	}
	cancel()
	<-done
}

// The orchestrator persists this snapshot as its tool registry and derives every
// policy decision from it. Reporting must happen on EVERY connect, not just the
// first, or a reconnected worker leaves the orchestrator with a stale registry.
func TestWorkerReportsDiscoveredToolsOnEveryConnect(t *testing.T) {
	first, second := newFakeStream(), newFakeStream()
	client := &fakeRuntimeClient{stream: second, queued: []*fakeStream{first}}
	var calls int
	worker := New(Options{
		WorkerID: "worker-1", MaxConcurrentRuns: 1,
		DiscoverTools: func(context.Context) ([]*turingv1.DiscoveredTool, error) {
			calls++
			return []*turingv1.DiscoveredTool{{ServerName: "system", ToolName: "system.time"}}, nil
		},
	}, client, terminalExecutor{})

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(firstCtx) }()

	ready := nextSent(t, first).GetWorkerReady()
	if ready == nil {
		t.Fatal("no worker_ready on the first stream")
	}
	if got := ready.GetToolDiscoveryStatus(); got != turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE {
		t.Fatalf("status = %v, want COMPLETE", got)
	}
	if len(ready.GetTools()) != 1 || ready.GetTools()[0].GetToolName() != "system.time" {
		t.Fatalf("tools = %+v, want system.time", ready.GetTools())
	}
	cancelFirst()
	<-done

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	go func() { done <- worker.Run(secondCtx) }()
	if reReady := nextSent(t, second).GetWorkerReady(); reReady == nil || len(reReady.GetTools()) != 1 {
		t.Fatalf("reconnect did not re-report tools: %+v", reReady)
	}
	if calls != 2 {
		t.Fatalf("discovery ran %d times, want once per connect", calls)
	}
	cancelSecond()
	<-done
}

func TestWorkerReadyAdvertisesCompleteCapabilities(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{
		WorkerID:          "worker-capabilities",
		AgentID:           turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		MaxConcurrentRuns: 3,
		Models: []*turingv1.ModelCapability{{
			Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Model:            "qwen2.5:7b",
			MaxContextTokens: 32768,
		}},
		SupportsExternalAgents:      true,
		ExternalAgentCredentialRefs: []string{"openai", "claude"},
		NewRegistrationID:           func() (string, error) { return "registration-1", nil },
		DiscoverTools: func(context.Context) ([]*turingv1.DiscoveredTool, error) {
			return []*turingv1.DiscoveredTool{{ServerName: "system", ToolName: "system.time"}}, nil
		},
	}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	ready := nextSent(t, stream).GetWorkerReady()
	if ready.GetRegistrationId() != "registration-1" {
		t.Fatalf("registration_id = %q, want registration-1", ready.GetRegistrationId())
	}
	capabilities := ready.GetCapabilities()
	if capabilities == nil {
		t.Fatal("capabilities are missing")
	}
	if capabilities.GetMaxConcurrentRuns() != 3 || !capabilities.GetSupportsExternalAgents() {
		t.Fatalf("capacity/external support = %d/%v, want 3/true", capabilities.GetMaxConcurrentRuns(), capabilities.GetSupportsExternalAgents())
	}
	if len(capabilities.GetAgentIds()) != 1 || capabilities.GetAgentIds()[0] != turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT {
		t.Fatalf("agent_ids = %v, want general assistant", capabilities.GetAgentIds())
	}
	if len(capabilities.GetModels()) != 1 || capabilities.GetModels()[0].GetModel() != "qwen2.5:7b" || capabilities.GetModels()[0].GetMaxContextTokens() != 32768 {
		t.Fatalf("models = %+v, want qwen2.5:7b with 32768 tokens", capabilities.GetModels())
	}
	if len(capabilities.GetTools()) != 1 || capabilities.GetTools()[0].GetToolName() != "system.time" {
		t.Fatalf("tools = %+v, want system.time", capabilities.GetTools())
	}
	if got := capabilities.GetExternalAgentCredentialRefs(); !slices.Equal(got, []string{"claude", "openai"}) {
		t.Fatalf("external credential refs = %v, want sorted names only", got)
	}
	if ready.GetAgentId() != turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT ||
		ready.GetMaxConcurrentRuns() != 3 ||
		len(ready.GetTools()) != 1 {
		t.Fatalf("legacy ready fields are not mirrored: %+v", ready)
	}
	cancel()
	<-done
}

func TestWorkerReconnectUsesFreshRegistrationIdentity(t *testing.T) {
	first, second := newFakeStream(), newFakeStream()
	client := &fakeRuntimeClient{stream: second, queued: []*fakeStream{first}}
	registrationIDs := []string{"registration-1", "registration-2"}
	worker := New(Options{
		WorkerID:          "worker-reconnect-identity",
		MaxConcurrentRuns: 1,
		Models: []*turingv1.ModelCapability{{
			Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Model:    "qwen2.5:7b",
		}},
		NewRegistrationID: func() (string, error) {
			registrationID := registrationIDs[0]
			registrationIDs = registrationIDs[1:]
			return registrationID, nil
		},
	}, client, terminalExecutor{})

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(firstCtx) }()
	firstReady := nextSent(t, first).GetWorkerReady()
	cancelFirst()
	<-done

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	go func() { done <- worker.Run(secondCtx) }()
	secondReady := nextSent(t, second).GetWorkerReady()
	cancelSecond()
	<-done

	if firstReady.GetRegistrationId() != "registration-1" || secondReady.GetRegistrationId() != "registration-2" {
		t.Fatalf("registration IDs = %q, %q; want a fresh ID per stream", firstReady.GetRegistrationId(), secondReady.GetRegistrationId())
	}
}

// A worker that cannot enumerate its tools must say so rather than register a
// snapshot it does not have. The orchestrator rejects FAILED, and the reconnect
// loop retries — which is what should happen when MCP is briefly down.
func TestWorkerReportsFailedDiscovery(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{
		WorkerID: "worker-1", MaxConcurrentRuns: 1,
		DiscoverTools: func(context.Context) ([]*turingv1.DiscoveredTool, error) {
			return nil, errors.New("mcp unreachable")
		},
	}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	ready := nextSent(t, stream).GetWorkerReady()
	if ready == nil {
		t.Fatal("no worker_ready sent")
	}
	if got := ready.GetToolDiscoveryStatus(); got != turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_FAILED {
		t.Fatalf("status = %v, want FAILED", got)
	}
	if len(ready.GetTools()) != 0 {
		t.Fatalf("failed discovery reported %d tools; it must report none", len(ready.GetTools()))
	}
	cancel()
	<-done
}

// Without a discovery function the worker is a legacy reporter: the orchestrator
// falls back to its compatibility defaults, which UNSPECIFIED selects.
func TestWorkerWithoutDiscoveryReportsUnspecified(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-1", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	ready := nextSent(t, stream).GetWorkerReady()
	if ready == nil {
		t.Fatal("no worker_ready sent")
	}
	if got := ready.GetToolDiscoveryStatus(); got != turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_UNSPECIFIED {
		t.Fatalf("status = %v, want UNSPECIFIED", got)
	}
	cancel()
	<-done
}

func TestModernWorkerWithoutDiscoveryReportsAuthoritativeEmptyTools(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{
		WorkerID:          "worker-modern-no-tools",
		MaxConcurrentRuns: 1,
		Models: []*turingv1.ModelCapability{{
			Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Model:    "qwen2.5:7b",
		}},
		NewRegistrationID: func() (string, error) { return "registration-no-tools", nil },
	}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	ready := nextSent(t, stream).GetWorkerReady()
	if ready == nil {
		t.Fatal("no worker_ready sent")
	}
	if got := ready.GetToolDiscoveryStatus(); got != turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE {
		t.Fatalf("status = %v, want COMPLETE so older orchestrators do not synthesize legacy tools", got)
	}
	if len(ready.GetTools()) != 0 || len(ready.GetCapabilities().GetTools()) != 0 {
		t.Fatalf("modern no-discovery worker advertised tools: legacy=%+v capabilities=%+v", ready.GetTools(), ready.GetCapabilities().GetTools())
	}
	cancel()
	<-done
}

// ---------------------------------------------------------------------------
// Assignment version identity.
//
// The worker is the only thing that knows which state its report was computed
// against, so it retains the highest version the orchestrator has committed for
// the assignment it owns and echoes it back on every terminal report.
// ---------------------------------------------------------------------------

// scriptedExecutor runs whatever the test hands it, so version behaviour can be
// driven step by step instead of inferred from a real agent's timing.
type scriptedExecutor struct {
	started chan *turingv1.AgentJob
	release chan struct{}
	emitted chan func(*turingv1.RuntimeUpdate) error
	posters chan func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)
	run     func(job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error
}

func (e *scriptedExecutor) SetToolBeaconPoster(post func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)) {
	e.posters <- post
}

func newScriptedExecutor(run func(job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error) *scriptedExecutor {
	return &scriptedExecutor{
		started: make(chan *turingv1.AgentJob, 4),
		release: make(chan struct{}),
		emitted: make(chan func(*turingv1.RuntimeUpdate) error, 4),
		posters: make(chan func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error), 4),
		run:     run,
	}
}

func (e *scriptedExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error {
	e.started <- job
	if e.run != nil {
		return e.run(job, emit)
	}
	e.emitted <- emit
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func startScriptedWorker(t *testing.T, executor Executor, stream *fakeStream, adjust ...func(*Options)) (*Worker, func()) {
	t.Helper()
	options := Options{
		WorkerID:                 "worker-version",
		MaxConcurrentRuns:        1,
		UpdateSendTimeout:        time.Second,
		DisconnectCleanupTimeout: 50 * time.Millisecond,
	}
	for _, apply := range adjust {
		apply(&options)
	}
	worker := New(options, &fakeRuntimeClient{stream: stream}, executor)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	// The worker_ready registration is the first thing on the wire; drain it so
	// tests read only the updates they are about.
	_ = nextSent(t, stream)
	return worker, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for worker Run to return")
		}
		waitForOutboundWriterExit(t, worker)
	}
}

func assignJob(t *testing.T, stream *fakeStream, runID string, attemptID string, version int64) {
	t.Helper()
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{
		RunAssigned: &turingv1.AgentJob{
			JobId:                "job_" + runID,
			RunId:                runID,
			AssistantMessageId:   "msg_" + runID,
			AssignmentAttemptId:  attemptID,
			ExpectedStateVersion: version,
		},
	}}
}

func TestWorkerEchoesExpectedVersionOnCompletionAndFailure(t *testing.T) {
	for _, test := range []struct {
		name     string
		terminal func(runID string) *turingv1.RuntimeUpdate
		observed func(*turingv1.RuntimeUpdate) int64
	}{
		{
			name: "completion",
			terminal: func(runID string) *turingv1.RuntimeUpdate {
				return &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{
					RunCompleted: &turingv1.RuntimeRunCompleted{
						RunId: runID, AssistantMessageId: "msg_" + runID, Content: "done",
					},
				}}
			},
			observed: func(update *turingv1.RuntimeUpdate) int64 {
				return update.GetRunCompleted().GetExpectedStateVersion()
			},
		},
		{
			name: "failure",
			terminal: func(runID string) *turingv1.RuntimeUpdate {
				return &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{
					RunFailed: &turingv1.RuntimeRunFailed{RunId: runID, Code: "runtime_error"},
				}}
			},
			observed: func(update *turingv1.RuntimeUpdate) int64 {
				return update.GetRunFailed().GetExpectedStateVersion()
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			const runID = "run_echo_version"
			const assignedVersion = int64(7)
			stream := newFakeStream()
			executor := newScriptedExecutor(func(job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error {
				return emit(test.terminal(job.GetRunId()))
			})
			_, stop := startScriptedWorker(t, executor, stream)
			defer stop()

			assignJob(t, stream, runID, "attempt-echo", assignedVersion)
			update := nextSent(t, stream)
			if got := test.observed(update); got != assignedVersion {
				t.Fatalf("terminal report carried version %d, want the assigned %d", got, assignedVersion)
			}
		})
	}
}

func TestWorkerKeepsHighestAcceptedVersionPerAssignment(t *testing.T) {
	const runID = "run_highest_version"
	const attemptID = "attempt-highest"
	stream := newFakeStream()
	executor := newScriptedExecutor(nil)
	worker, stop := startScriptedWorker(t, executor, stream)
	defer stop()

	post := <-executor.posters
	assignJob(t, stream, runID, attemptID, 3)
	emit := <-executor.emitted

	// A same-attempt refresh at a newer version is the orchestrator committing
	// forward, and the worker follows it.
	assignJob(t, stream, runID, attemptID, 5)
	waitForWorkerVersion(t, worker, runID, 5)

	// A refresh computed against an older state cannot roll the worker back,
	// and one naming a different attempt is a fenced predecessor's command.
	assignJob(t, stream, runID, attemptID, 4)
	assignJob(t, stream, runID, "attempt-other", 9)
	// Both rejected commands are observed through a later accepted one, so the
	// assertion never races the receive loop.
	assignJob(t, stream, runID, attemptID, 5)
	waitForWorkerVersion(t, worker, runID, 5)

	// A beacon's own decision is the one reply that carries a committed version
	// back before tool work continues, so it moves the assignment forward.
	decided := make(chan *turingv1.ToolPolicyDecision, 1)
	go func() {
		decision, err := post(context.Background(), &turingv1.ToolCallBeacon{
			Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, ToolCallId: "call_current",
			RunId: runID, ToolName: "system.time",
		})
		if err != nil {
			decided <- nil
			return
		}
		decided <- decision
	}()
	if beacon := nextSent(t, stream); beacon.GetToolBeacon() == nil {
		t.Fatalf("worker sent %+v, want the tool beacon", beacon)
	}
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ToolPolicyDecision{
		ToolPolicyDecision: &turingv1.ToolPolicyDecision{
			Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: "call_current",
			Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, RunStateVersion: 6,
		},
	}}
	select {
	case decision := <-decided:
		if decision == nil {
			t.Fatal("tool beacon returned no decision")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the tool policy decision")
	}
	waitForWorkerVersion(t, worker, runID, 6)

	if err := emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{
		RunCompleted: &turingv1.RuntimeRunCompleted{RunId: runID, AssistantMessageId: "msg_" + runID, Content: "done"},
	}}); err != nil {
		t.Fatalf("emit terminal: %v", err)
	}
	close(executor.release)
	terminal := nextSent(t, stream)
	if got := terminal.GetRunCompleted().GetExpectedStateVersion(); got != 6 {
		t.Fatalf("terminal report carried version %d, want the highest accepted 6", got)
	}
}

func TestWorkerPausesOutboundRunUpdatesUntilSameAttemptRefresh(t *testing.T) {
	const runID = "run_paused_outbound"
	const attemptID = "attempt-paused"
	stream := newFakeStream()
	// A stalled send is the honest shape of stream loss here: the update may or
	// may not have arrived, so the run's ownership is exactly as uncertain as
	// the orchestrator's fence assumes. The stall is released rather than
	// failed, because a failed send stops the outbound writer for good and
	// there would be no stream left to resume onto.
	stall := make(chan struct{})
	stalled := make(chan struct{})
	var stallOnce sync.Once
	stalledOne := false
	stream.sendFn = func(update *turingv1.RuntimeUpdate) error {
		// Only the FIRST run-step stalls. Everything after it is forwarded, so
		// what does and does not reach the wire later is observable rather than
		// swallowed by the fixture.
		if !stalledOne && update.GetEvent().GetType() == turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			stalledOne = true
			stallOnce.Do(func() { close(stalled) })
			<-stall
			return nil
		}
		stream.sent <- update
		return nil
	}
	executor := newScriptedExecutor(nil)
	worker, stop := startScriptedWorker(t, executor, stream, func(options *Options) {
		options.UpdateSendTimeout = 50 * time.Millisecond
	})
	defer stop()

	assignJob(t, stream, runID, attemptID, 3)
	emit := <-executor.emitted

	lost := make(chan error, 1)
	go func() {
		lost <- emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
			RunId: runID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
		}}})
	}()
	<-stalled
	select {
	case err := <-lost:
		if err == nil {
			t.Fatal("stalled send reported success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the stalled send to give up")
	}
	close(stall)

	// Withheld while paused, and given a type nothing else in this test uses so
	// the wire can be read for its absence rather than timed for it.
	withheld := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId: runID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED,
	}}}
	if err := emit(withheld); err != nil {
		t.Fatalf("withheld update reported an error to the executor: %v", err)
	}

	assignJob(t, stream, runID, attemptID, 5)
	// A second assignment the worker has no capacity for. Its rejection is sent
	// synchronously from the same receive loop that just applied the refresh, so
	// reading it off the wire proves the refresh landed — no polling, and no
	// waiting out a duration to conclude nothing happened.
	assignJob(t, stream, "run_capacity_probe", "attempt-probe", 1)
	busy := nextSent(t, stream)
	if failed := busy.GetRunFailed(); failed == nil || failed.GetCode() != "worker_busy" {
		t.Fatalf("first update after the refresh = %+v, want the busy rejection; a withheld update reached the wire", busy)
	}
	if entry := worker.activeRun(runID); entry == nil || entry.expectedVersion() != 5 {
		t.Fatalf("worker version after the refresh = %+v, want 5", entry)
	}

	delta := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId: runID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
	}}}
	if err := emit(delta); err != nil {
		t.Fatalf("resumed update failed: %v", err)
	}
	// The writer sends in order, so the withheld update would have arrived
	// before this one had it been queued rather than dropped.
	if update := nextSent(t, stream); update.GetEvent().GetType() != turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA {
		t.Fatalf("resumed update = %+v, want the message delta", update)
	}
}

func waitForWorkerVersion(t *testing.T, worker *Worker, runID string, want int64) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		entry := worker.activeRun(runID)
		if entry != nil && entry.expectedVersion() == want {
			return
		}
		select {
		case <-deadline:
			got := int64(-1)
			if entry != nil {
				got = entry.expectedVersion()
			}
			t.Fatalf("worker version for %s = %d, want %d", runID, got, want)
		case <-time.After(time.Millisecond):
		}
	}
}

// The worker itself reports two failures: a run it cannot accept, and a run
// whose executor died. Both name a typed origin, so the orchestrator never has
// to read a Go error string to decide whether to requeue.
func TestWorkerTypedFailureOriginsForRuntimeAndDispatch(t *testing.T) {
	t.Run("busy_rejection_is_dispatch_and_transient", func(t *testing.T) {
		stream := newFakeStream()
		executor := newScriptedExecutor(nil)
		_, stop := startScriptedWorker(t, executor, stream)
		defer stop()

		assignJob(t, stream, "run_first", "attempt-first", 1)
		<-executor.emitted
		assignJob(t, stream, "run_second", "attempt-second", 1)

		failed := nextSent(t, stream).GetRunFailed()
		if failed == nil || failed.GetCode() != "worker_busy" {
			t.Fatalf("update = %+v, want worker_busy", failed)
		}
		if failed.GetFailureOrigin() != turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH {
			t.Fatalf("origin = %v, want dispatch", failed.GetFailureOrigin())
		}
		if failed.GetAutomaticRetryClass() != turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT {
			t.Fatalf("retry class = %v, want same-run transient", failed.GetAutomaticRetryClass())
		}
		if failed.GetMessage() != "" {
			t.Fatalf("busy rejection carried message %q, want none", failed.GetMessage())
		}
	})

	t.Run("executor_failure_is_worker_runtime_and_never", func(t *testing.T) {
		stream := newFakeStream()
		executor := newScriptedExecutor(func(*turingv1.AgentJob, func(*turingv1.RuntimeUpdate) error) error {
			return errors.New("model_quota_exceeded: tool_call_failed")
		})
		_, stop := startScriptedWorker(t, executor, stream)
		defer stop()

		assignJob(t, stream, "run_broken", "attempt-broken", 4)

		failed := nextSent(t, stream).GetRunFailed()
		if failed == nil || failed.GetCode() != "runtime_error" {
			t.Fatalf("update = %+v, want runtime_error", failed)
		}
		if failed.GetFailureOrigin() != turingv1.FailureOrigin_FAILURE_ORIGIN_WORKER_RUNTIME {
			t.Fatalf("origin = %v, want worker runtime", failed.GetFailureOrigin())
		}
		if failed.GetAutomaticRetryClass() != turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_NEVER {
			t.Fatalf("retry class = %v, want never", failed.GetAutomaticRetryClass())
		}
		if failed.GetMessage() != "" {
			t.Fatalf("executor error text crossed the boundary as %q", failed.GetMessage())
		}
	})
}

// retryableFailure reads the typed retry class the worker now reports.
func retryableFailure(failed *turingv1.RuntimeRunFailed) bool {
	return failed.GetAutomaticRetryClass() == turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT
}
