package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
)

// TestWorkerBusyRejectionRequeuesWithoutImmediateRedispatch proves the fix for
// the drain-loop cascade: when a worker rejects an assignment with a retryable
// worker_busy failure, the run is requeued (not permanently failed) and the
// orchestrator does not immediately hand the same still-busy worker another
// assignment.
func TestWorkerBusyRejectionRequeuesWithoutImmediateRedispatch(t *testing.T) {
	h := newHarness(t)
	h.createSessionAndRun(t, "busy worker")
	stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-busy", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()

	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId:               assigned.RunId,
		Code:                "worker_busy",
		FailureOrigin:       turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH,
		AutomaticRetryClass: turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
	}}}); err != nil {
		t.Fatal(err)
	}

	// The worker that just reported busy must NOT be handed another assignment
	// immediately; without suppression DispatchPending would re-dispatch the
	// requeued job straight back to the draining worker in a tight loop.
	if cmd := recvWithin(t, stream, 750*time.Millisecond); cmd != nil && cmd.GetRunAssigned() != nil {
		t.Fatalf("busy worker was immediately re-dispatched run %s", cmd.GetRunAssigned().RunId)
	}

	// The run must be requeued rather than permanently failed: with dispatch
	// suppressed it stays queued, waiting for a later trigger (a free worker,
	// a heartbeat, or the drain completing).
	run, err := h.repo.GetRun(context.Background(), assigned.RunId)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" {
		t.Fatalf("run %s status = %q, want queued (requeued, not failed)", assigned.RunId, run.Status)
	}
}

// recvWithin returns the next command received within timeout, or nil if none
// arrives before the timeout elapses.
func recvWithin(t *testing.T, stream turingv1.RuntimeService_ConnectWorkerClient, timeout time.Duration) *turingv1.RuntimeCommand {
	t.Helper()
	received := make(chan *turingv1.RuntimeCommand, 1)
	errs := make(chan error, 1)
	go func() {
		cmd, err := stream.Recv()
		if err != nil {
			errs <- err
			return
		}
		received <- cmd
	}()
	select {
	case cmd := <-received:
		return cmd
	case err := <-errs:
		t.Fatalf("stream recv: %v", err)
		return nil
	case <-time.After(timeout):
		return nil
	}
}

// Suppressing dispatch is only safe for worker_busy, where the worker is at
// capacity and its draining run will later emit RunCompleted — which does
// trigger DispatchPending.
//
// Every other retryable failure happens MID-RUN, after which the worker is
// idle. tool_discovery_failed is the common one: MCP is briefly unreachable and
// ToolDiscoveryRetryable defaults to true. Nothing else re-dispatches — a
// heartbeat only dispatches when a lease is reconciled or revived, which a
// healthy worker never is, and there is no periodic dispatcher. Suppressing
// here would strand the run in `queued` forever: the client saw
// agent.run.started and would then get silence, which is worse than the fast
// terminal failure this change replaced.
func TestIdleWorkerRetryableFailureIsRedispatched(t *testing.T) {
	h := newHarness(t)
	h.createSessionAndRun(t, "transient tool discovery failure")
	stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-idle", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()

	// The run failed partway through; the worker is now idle and will send
	// nothing further on its own.
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId:               assigned.RunId,
		Code:                "tool_discovery_failed",
		FailureOrigin:       turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_INFRASTRUCTURE,
		AutomaticRetryClass: turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
	}}}); err != nil {
		t.Fatal(err)
	}

	redispatched := recvWithin(t, stream, 3*time.Second)
	if redispatched == nil || redispatched.GetRunAssigned() == nil {
		t.Fatal("requeued run was never re-dispatched to the idle worker; it is stranded in queued")
	}
	if got := redispatched.GetRunAssigned().RunId; got != assigned.RunId {
		t.Fatalf("re-dispatched run %s, want the requeued run %s", got, assigned.RunId)
	}
}

// The suppression above is only defensible because a terminal update dispatches
// queued work. Nothing asserted that, so the suppression test alone would pass
// just as happily if the run were stranded forever — it proves the run is not
// re-dispatched IMMEDIATELY, not that it is re-dispatched at all.
//
// This pins the mechanism a suppressed dispatch relies on: when the in-flight
// run finishes, the next one goes out on the same stream. It does not stage a
// worker_busy rejection — the orchestrator's own capacity gate stops it offering
// a second assignment to a full worker, so worker_busy is a reconnect race,
// where the worker still holds a run the orchestrator has already released.
func TestTerminalUpdateDispatchesQueuedWork(t *testing.T) {
	h := newHarness(t)
	h.createSessionAndRun(t, "first run")
	stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-drain", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	inFlight := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()

	h.createSessionAndRun(t, "queued behind it")

	// The drain completes. This is the path a suppressed dispatch depends on.
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId: inFlight.RunId, AssistantMessageId: inFlight.AssistantMessageId, Content: "done",
	}}}); err != nil {
		t.Fatal(err)
	}

	next := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil && cmd.GetRunAssigned().RunId != inFlight.RunId
	})
	if next == nil || next.GetRunAssigned() == nil {
		t.Fatal("the queued run was never dispatched; a suppressed dispatch would strand it")
	}
}

// TestWorkerBusyRequeuePublishesRetryNotice proves the requeue notice reaches a
// client, not just the RetryDecision. The repository test can only observe
// decision.Events; the point of the notice is that it is published on the
// session's event stream, which is what the Flutter client subscribes to.
func TestWorkerBusyRequeuePublishesRetryNotice(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSessionAndRun(t, "notice me")
	stream, unsubscribe := h.bus.Subscribe(sessionID)
	defer unsubscribe()

	workerStream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerStream.CloseSend() }()
	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-notice", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()

	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId:               assigned.RunId,
		Code:                "worker_busy",
		FailureOrigin:       turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH,
		AutomaticRetryClass: turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
	}}}); err != nil {
		t.Fatal(err)
	}

	notice := recvBusEvent(t, stream, func(event events.Event) bool {
		return runNoticeIsRetry(event, runoutcome.NoticeDispatchRetry, 2, 3)
	})
	if notice.RunID != assigned.RunId {
		t.Fatalf("notice run_id = %q, want %q (a client correlates by run)", notice.RunID, assigned.RunId)
	}
}

// TestOrphanRecoveryPublishesRetryNotice is the reconciliation counterpart to
// TestWorkerBusyRequeuePublishesRetryNotice. The repository test can only see
// AssignmentReconciliation.Events; this proves the orchestrator's own recovery
// sweep publishes them, so a refactor that drops result.Events cannot silence
// the notice behind a green repository suite.
func TestOrphanRecoveryPublishesRetryNotice(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Orphan recovery")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "lose my worker", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := h.repo.ClaimNextJob(ctx, "general_assistant", "worker-orphaned")
	if err != nil {
		t.Fatal(err)
	}
	assignment := repository.Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-orphaned", AttemptID: claimed.AssignmentAttemptID,
	}
	if err := h.repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	// Expire the lease so the sweep treats the run as orphaned. No worker is
	// connected to this harness, so nothing can renew it.
	expired := time.Now().UTC().Add(-time.Minute)
	if _, err := h.database.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
		WHERE id = ?
	`, repository.FormatTimestamp(expired), expired.UnixNano(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	stream, unsubscribe := h.bus.Subscribe(session.SessionID)
	defer unsubscribe()
	if err := h.service.RecoverOrphanedAssignments(ctx); err != nil {
		t.Fatal(err)
	}

	notice := recvBusEvent(t, stream, func(event events.Event) bool {
		return runNoticeIsRetry(event, runoutcome.NoticeRecoveryRetry, 2, 3)
	})
	if notice.RunID != enqueued.RunID {
		t.Fatalf("notice run_id = %q, want %q", notice.RunID, enqueued.RunID)
	}
}

// The give-up notice is the last thing the user ever hears about a run that
// exhausted its retries, so proving it reaches the bus matters more than for
// the retry notice. The publish loops are generic over Events, but a future
// refactor that filtered by type could drop this silently.
func TestRetryExhaustionPublishesGiveUpNotice(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{MaxAttempts: 1})
	sessionID := h.createSessionAndRun(t, "give up on me")
	stream, unsubscribe := h.bus.Subscribe(sessionID)
	defer unsubscribe()

	workerStream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerStream.CloseSend() }()
	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-giveup", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, workerStream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()

	// MaxAttempts=1 means the first rejection exhausts the budget outright.
	if err := workerStream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId: assigned.RunId, Code: "worker_busy",
		FailureOrigin:       turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH,
		AutomaticRetryClass: turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
	}}}); err != nil {
		t.Fatal(err)
	}

	if notice := recvBusEvent(t, stream, func(event events.Event) bool {
		return runNoticeIsRetry(event, runoutcome.NoticeRecoveryExhausted, 1, 1)
	}); notice.RunID != assigned.RunId {
		t.Fatalf("give-up notice run_id = %q, want %q", notice.RunID, assigned.RunId)
	}
}

// runNoticeIsRetry matches a failure-like run-step notice by its allowlisted
// category and its two bounded numbers. The sentence these notices used to
// carry was assembled by string formatting and stored in the durable log; a
// client cannot localize that, so the category is what travels now.
func runNoticeIsRetry(event events.Event, category runoutcome.NoticeCategory, attempt int, maxAttempts int) bool {
	if event.Type != "agent.run.step" {
		return false
	}
	var payload struct {
		Category    string `json:"category"`
		Attempt     int    `json:"attempt"`
		MaxAttempts int    `json:"maxAttempts"`
	}
	if json.Unmarshal([]byte(event.PayloadJSON), &payload) != nil {
		return false
	}
	return payload.Category == string(category) && payload.Attempt == attempt && payload.MaxAttempts == maxAttempts
}
