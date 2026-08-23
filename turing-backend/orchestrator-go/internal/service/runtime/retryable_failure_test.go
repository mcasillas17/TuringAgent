package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// TestRuntimeRetryableFailureUsesCurrentAttemptConfirmedRelease is the boundary
// half of the confirmed-release correction.
//
// The repository can commit the direct running-to-queued edge only if somebody
// hands it a proven owner, and the only place that proof exists is the
// authenticated worker stream: the server knows which worker is connected, and
// `beginUpdate` has already checked that this run is assigned to it. Dropping
// that identity on the floor and calling the retry path anonymously would make
// every worker rejection look like a lost run again.
//
// worker_busy is the failure used here on purpose: it is the one retryable code
// that suppresses re-dispatch, so the run stays queued and the versions this
// test reads cannot be moved underneath it by a redispatch race.
func TestRuntimeRetryableFailureUsesCurrentAttemptConfirmedRelease(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSessionAndRun(t, "confirmed release from the current attempt")
	published, unsubscribe := h.bus.Subscribe(sessionID)
	defer unsubscribe()

	stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-confirmed-release", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()

	running := h.runState(t, assigned.GetRunId())
	if running.Lifecycle != "running" || running.StateVersion != assigned.GetExpectedStateVersion() {
		t.Fatalf("assigned run = %s at version %d, want running at the dispatched version %d",
			running.Lifecycle, running.StateVersion, assigned.GetExpectedStateVersion())
	}
	if assigned.GetAssignmentAttemptId() == "" {
		t.Fatal("assignment carried no attempt ID, so no report could ever prove it owned the run")
	}

	// A worker-authored diagnostic rides along on the report. Nothing durable
	// and nothing public may repeat it.
	const rawDiagnostic = "dial tcp 10.4.7.9:11434: connection refused (worker pool draining)"
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId:                assigned.GetRunId(),
		Code:                 "worker_busy",
		Message:              rawDiagnostic,
		FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH,
		AutomaticRetryClass:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
		ExpectedStateVersion: assigned.GetExpectedStateVersion(),
	}}}); err != nil {
		t.Fatal(err)
	}

	// The queued projection is the deterministic barrier: it is published only
	// once the requeue has committed, so nothing below has to wait on a clock.
	queued := recvBusEvent(t, published, func(event events.Event) bool {
		return event.Type == "agent.run.state_changed" && runStateLifecycle(t, event.PayloadJSON) == "queued"
	})
	wantVersion := running.StateVersion + 1
	if got := runStateVersion(t, queued.PayloadJSON); got != wantVersion {
		t.Fatalf("queued projection version = %d, want exactly one increment to %d", got, wantVersion)
	}

	state := h.runState(t, assigned.GetRunId())
	if state.Lifecycle != "queued" || state.OutcomeReason != "none" || state.StateVersion != wantVersion {
		t.Fatalf("released run = %s/%s at version %d, want queued/none at %d",
			state.Lifecycle, state.OutcomeReason, state.StateVersion, wantVersion)
	}
	// The proof travelled: an anonymous retry would have fenced the run into
	// recovering first and committed two versions instead of one.
	for _, lifecycle := range runStateLifecycles(t, h, assigned.GetRunId()) {
		if lifecycle == "recovering" {
			t.Fatalf("a proven release published an uncertain phase: %v", runStateLifecycles(t, h, assigned.GetRunId()))
		}
	}
	assertNoRawDiagnostic(t, h, assigned.GetRunId(), rawDiagnostic)

	stateEvents := countRunEvents(t, h, assigned.GetRunId(), "agent.run.state_changed")
	beforeReplay := h.runState(t, assigned.GetRunId())

	// The same report again. That attempt released the run when it was
	// accepted, and the version it names is the version the run has already
	// left, so it is a claim about a state that no longer exists. It must be
	// refused without a write — the worker holds no assignment now, so nothing
	// its disconnect reconciles can hide a write behind.
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId:                assigned.GetRunId(),
		Code:                 "worker_busy",
		Message:              rawDiagnostic,
		FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH,
		AutomaticRetryClass:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
		ExpectedStateVersion: running.StateVersion,
	}}}); err != nil {
		t.Fatal(err)
	}
	// The stream ends on a refused report, and the handler finishes its
	// teardown before the client observes that end. Waiting for it is what
	// makes the assertions below a statement about a settled system rather
	// than a race.
	awaitStreamEnd(t, stream)

	if after := h.runState(t, assigned.GetRunId()); after != beforeReplay {
		t.Fatalf("stale report changed the run: %+v, want %+v", after, beforeReplay)
	}
	if got := countRunEvents(t, h, assigned.GetRunId(), "agent.run.state_changed"); got != stateEvents {
		t.Fatalf("stale report appended %d lifecycle projections", got-stateEvents)
	}
	assertNoRawDiagnostic(t, h, assigned.GetRunId(), rawDiagnostic)
}

// runStateLifecycle reads the lifecycle out of a published state payload.
func runStateLifecycle(t *testing.T, payloadJSON string) string {
	t.Helper()
	return decodePublishedRunState(t, payloadJSON).Lifecycle
}

func runStateVersion(t *testing.T, payloadJSON string) int64 {
	t.Helper()
	return decodePublishedRunState(t, payloadJSON).StateVersion
}

type publishedRunState struct {
	Lifecycle    string `json:"lifecycle"`
	StateVersion int64  `json:"stateVersion"`
}

func decodePublishedRunState(t *testing.T, payloadJSON string) publishedRunState {
	t.Helper()
	var payload struct {
		RunState *publishedRunState `json:"runState"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode run state payload %q: %v", payloadJSON, err)
	}
	if payload.RunState == nil {
		t.Fatalf("payload %q carries no runState snapshot", payloadJSON)
	}
	return *payload.RunState
}

// runStateLifecycles returns the durable lifecycle projections a run committed,
// in log order.
func runStateLifecycles(t *testing.T, h *harness, runID string) []string {
	t.Helper()
	rows, err := h.database.QueryContext(context.Background(), `
		SELECT payload_json FROM events WHERE run_id = ? AND type = 'agent.run.state_changed' ORDER BY sequence
	`, runID)
	if err != nil {
		t.Fatalf("read run state projections: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lifecycles []string
	for rows.Next() {
		var payloadJSON string
		if err := rows.Scan(&payloadJSON); err != nil {
			t.Fatalf("scan run state projection: %v", err)
		}
		lifecycles = append(lifecycles, runStateLifecycle(t, payloadJSON))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read run state projections: %v", err)
	}
	return lifecycles
}

// assertNoRawDiagnostic proves the worker's own sentence never became durable
// public truth, in either place it could have landed: the run row's diagnostic
// columns, or any event payload a client replays.
func assertNoRawDiagnostic(t *testing.T, h *harness, runID string, raw string) {
	t.Helper()
	_, message := h.runDiagnostics(t, runID)
	if strings.Contains(message.String, raw) {
		t.Fatalf("run row stored the worker's raw message: %q", message.String)
	}
	rows, err := h.database.QueryContext(context.Background(),
		`SELECT type, payload_json FROM events WHERE run_id = ?`, runID)
	if err != nil {
		t.Fatalf("read run events: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var eventType, payloadJSON string
		if err := rows.Scan(&eventType, &payloadJSON); err != nil {
			t.Fatalf("scan run event: %v", err)
		}
		if strings.Contains(payloadJSON, raw) {
			t.Fatalf("%s payload leaked the worker's raw message: %s", eventType, payloadJSON)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read run events: %v", err)
	}
}

// awaitStreamEnd blocks until the worker stream reports its end, which happens
// only after the handler's teardown has finished. Without it, a test reading
// the database after a refused report would be racing that teardown. It returns
// the error the stream ended with, which is how a caller reads the status a
// refused report was mapped to.
func awaitStreamEnd(t *testing.T, stream turingv1.RuntimeService_ConnectWorkerClient) error {
	t.Helper()
	ended := make(chan error, 1)
	go func() {
		for {
			if _, err := stream.Recv(); err != nil {
				ended <- err
				return
			}
		}
	}()
	select {
	case err := <-ended:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the refused report neither committed nor ended the worker stream")
		return nil
	}
}

// ---------------------------------------------------------------------------
// A stale version reported by the worker that still holds the run.
//
// The replay above is refused before it reaches the repository: the first
// accepted report released the assignment, so `beginUpdate` turns the second
// into "run is not assigned to worker". That proves the in-memory gate works
// and nothing else. The version fence is a different claim, and it only gets to
// speak when the worker still owns the run — a mid-run transient failure whose
// reported version the run has never held.
// ---------------------------------------------------------------------------

// staleVersionFailure builds a same-run-transient report whose reported version
// is a nonzero one the run does not hold. If terminalExpectation ignored the
// reported version and substituted the run's own, this report would be applied
// and the run would be requeued.
func staleVersionFailure(runID string, reported int64) *turingv1.RuntimeRunFailed {
	return &turingv1.RuntimeRunFailed{
		RunId:                runID,
		Code:                 "worker_busy",
		FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH,
		AutomaticRetryClass:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
		ExpectedStateVersion: reported,
	}
}

// runJobProgress is the pair a write-free refusal must leave untouched: the job
// is still in progress under its worker, and the attempt counter a requeue
// would have spent has not moved.
type runJobProgress struct {
	status  string
	attempt int
}

func readRunJobProgress(t *testing.T, h *harness, runID string) runJobProgress {
	t.Helper()
	var progress runJobProgress
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT status, attempt FROM jobs WHERE run_id = ?`, runID).Scan(&progress.status, &progress.attempt); err != nil {
		t.Fatalf("read job progress: %v", err)
	}
	return progress
}

func countAllRunEvents(t *testing.T, h *harness, runID string) int {
	t.Helper()
	var count int
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE run_id = ?`, runID).Scan(&count); err != nil {
		t.Fatalf("count run events: %v", err)
	}
	return count
}

// TestRuntimeRetryableFailureOnOwnedAssignmentHonoursStaleReportedVersion is the
// fence the in-memory gate was hiding.
//
// The owner here is real and current: it is the assignment snapshot
// `beginUpdate` hands down, taken from the live worker registration, so nothing
// about ownership can refuse this report. The only thing wrong with it is the
// version it names, and a version the run has never reached is a claim computed
// against a state that does not exist. It has to be refused by the repository —
// not applied against whatever version the row happens to hold now — and
// refused without touching the row, the job, or the log.
func TestRuntimeRetryableFailureOnOwnedAssignmentHonoursStaleReportedVersion(t *testing.T) {
	h := newHarness(t)
	h.createSessionAndRun(t, "stale version from the current owner")
	const workerID = "worker-stale-version-owner"
	_, assigned := h.connectAssignedWorker(t, workerID, "")
	runID := assigned.GetRunId()

	connected := h.service.workerForRun(runID)
	if connected == nil {
		t.Fatal("no live worker holds the assignment, so this test could not exercise an owned report")
	}
	owned, ok := connected.assignmentForRun(runID)
	if !ok || owned.attemptID == "" {
		t.Fatalf("worker holds no attempt for run %s, so no owner could be proven", runID)
	}

	before := h.runState(t, runID)
	beforeEvents := countAllRunEvents(t, h, runID)
	beforeStates := countRunEvents(t, h, runID, "agent.run.state_changed")
	beforeJob := readRunJobProgress(t, h, runID)

	// A version the run has never reached, reported by the worker that still
	// owns it. Ownership is proven; the premise is not.
	suppressDispatch, err := h.service.handleRunFailed(
		context.Background(),
		staleVersionFailure(runID, before.StateVersion+7),
		releasedBy(workerID, owned),
	)
	if suppressDispatch {
		t.Fatal("a refused report asked the caller to suppress its dispatch")
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale version report = %v (code %s), want FailedPrecondition", err, status.Code(err))
	}
	// The refusal is a fence, not a leak: nothing about the run's internals
	// travels back to the worker.
	if strings.Contains(status.Convert(err).Message(), runID) {
		t.Fatalf("refusal message leaked run identity: %q", status.Convert(err).Message())
	}

	if after := h.runState(t, runID); after != before {
		t.Fatalf("fenced report changed the run: %+v, want %+v", after, before)
	}
	if got := countAllRunEvents(t, h, runID); got != beforeEvents {
		t.Fatalf("fenced report appended %d events", got-beforeEvents)
	}
	if got := countRunEvents(t, h, runID, "agent.run.state_changed"); got != beforeStates {
		t.Fatalf("fenced report appended %d lifecycle projections", got-beforeStates)
	}
	if got := readRunJobProgress(t, h, runID); got != beforeJob {
		t.Fatalf("fenced report changed the job: %+v, want %+v", got, beforeJob)
	}
}

// TestRuntimeStaleVersionOnLiveAssignmentEndsStreamWithFailedPrecondition is the
// transport half of the same fence.
//
// It sends the report the worker would actually send, over a stream whose
// assignment is still live, and reads the status the worker is told. Two codes
// separate the two failure modes this test exists to tell apart: PermissionDenied
// is `beginUpdate` refusing a report about a run the worker no longer holds,
// and FailedPrecondition is the repository refusing a report whose version the
// run never held. Only the second one proves the version fence ran.
func TestRuntimeStaleVersionOnLiveAssignmentEndsStreamWithFailedPrecondition(t *testing.T) {
	h := newHarness(t)
	h.createSessionAndRun(t, "stale version over a live stream")
	stream, assigned := h.connectAssignedWorker(t, "worker-stale-version-stream", "")
	runID := assigned.GetRunId()
	running := h.runState(t, runID)

	if err := stream.Send(&turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: staleVersionFailure(runID, running.StateVersion+7)},
	}); err != nil {
		t.Fatalf("send run_failed: %v", err)
	}

	err := awaitStreamEnd(t, stream)
	if status.Code(err) == codes.PermissionDenied {
		t.Fatalf("the report never reached the version fence: %v", err)
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale version report ended the stream with %v (code %s), want FailedPrecondition",
			err, status.Code(err))
	}
}
