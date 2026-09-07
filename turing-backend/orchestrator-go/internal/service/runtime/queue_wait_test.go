package runtime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
)

// queueWaitDispatch is the policy the tests below run under unless they are
// about a different one: both bounds on, and short enough that the controlled
// clock only has to move minutes rather than hours.
func queueWaitDispatch(policy runoutcome.QueueTimeoutPolicy) DispatchConfig {
	return DispatchConfig{
		QueueWait: QueueWaitPolicy{
			MaxWait:         time.Hour,
			NoWorkerTimeout: 10 * time.Minute,
			Policy:          policy,
		},
	}
}

// queueClock is a controlled clock. Every deadline assertion in this file moves
// it deliberately instead of sleeping: a bound proven by waiting for it is a
// test that is both slow and, on a loaded machine, a coin flip.
// It is mutex-guarded because it is genuinely shared: the sweep also runs from
// worker-stream goroutines, so a test that advances time while one of those is
// reading is a real concurrent access, not an artifact of the detector.
type queueClock struct {
	mu  sync.Mutex
	now time.Time
}

// newQueueClock starts from the real clock rather than a fixed instant. The
// repository stamps its own transitions with the real one — a run's queued
// interval opens at the moment enqueue committed — so a sweep clock parked in
// another era would measure the distance between the two rather than the time
// a test meant to pass. Starting here and only ever moving forward keeps the
// two readings comparable while still making every advance deliberate.
func newQueueClock() *queueClock {
	return &queueClock{now: time.Now().UTC()}
}

func (c *queueClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *queueClock) advance(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(by)
}

// queueWaitState reads the durable queue columns a client never sees, so a test
// can assert on the clock the sweep actually measures rather than on the public
// projection alone.
func queueWaitState(t *testing.T, h *harness, runID string) (reason string, waitedNs int64, queuedSince, unroutableSince *int64) {
	t.Helper()
	var waited int64
	var since, unroutable *int64
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT queue_wait_reason, queue_waited_ns, queued_since_ns, queue_unroutable_since_ns
		FROM agent_runs WHERE id = ?
	`, runID).Scan(&reason, &waited, &since, &unroutable); err != nil {
		t.Fatalf("read queue wait state: %v", err)
	}
	return reason, waited, since, unroutable
}

// refreshQueue runs one queue-observation and expiry pass, the way the reaper
// tick does.
func refreshQueue(t *testing.T, h *harness) {
	t.Helper()
	if err := h.service.RefreshPendingRoutingState(context.Background(), "test sweep"); err != nil {
		t.Fatalf("RefreshPendingRoutingState: %v", err)
	}
}

// WorkerAccepted precedes registration-triggered dispatch. Observe an assignment
// occupying the single slot before enqueueing work that must remain queued.
func occupyQueueWorker(t *testing.T, h *harness, stream turingv1.RuntimeService_ConnectWorkerClient) repository.EnqueueUserMessageResult {
	t.Helper()
	if err := h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatal(err)
	}
	occupying := h.enqueueRun(t, "occupy worker")
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetRunId() != occupying.RunID {
		t.Fatalf("assignment = %q, want occupying run %q", assigned.GetRunId(), occupying.RunID)
	}
	return occupying
}

func assertQueuedPendingRun(t *testing.T, h *harness, runID string) {
	t.Helper()
	var runStatus, jobStatus string
	var executionActive bool
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT runs.status, jobs.status, runs.execution_active
		FROM agent_runs AS runs JOIN jobs ON jobs.run_id = runs.id
		WHERE runs.id = ?
	`, runID).Scan(&runStatus, &jobStatus, &executionActive); err != nil {
		t.Fatal(err)
	}
	if runStatus != "queued" || jobStatus != "pending" || executionActive {
		t.Fatalf("queue prerequisite: run=%q job=%q execution_active=%v, want queued/pending/false",
			runStatus, jobStatus, executionActive)
	}
	if state := h.runState(t, runID); state.Lifecycle != "queued" {
		t.Fatalf("published lifecycle = %q, want queued", state.Lifecycle)
	}
}

func TestQueuedRunLosingEveryEligibleWorkerGainsDurableQueueTruth(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, queueWaitDispatch(runoutcome.QueueTimeoutPolicyFail))
	h.service.queueNow = clock.read

	// Accepted while a compatible worker was live, which is the only way an
	// enqueue gets past routing validation in production.
	stream := connectWorkerCapabilities(t, h, "worker-losing", "registration-losing",
		modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1))
	occupying := occupyQueueWorker(t, h, stream)
	enqueued := h.enqueueRun(t, "answer me")
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertQueuedPendingRun(t, h, enqueued.RunID)
	refreshQueue(t, h)
	if reason, _, _, unroutable := queueWaitState(t, h, enqueued.RunID); reason != string(runoutcome.QueueWaitNone) || unroutable != nil {
		t.Fatalf("queue truth with a compatible worker = %q/%v, want none and no open interval", reason, unroutable)
	}

	// Every eligible worker goes away.
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	eventually(t, eventuallyTimeout, func() bool {
		return h.service.registeredWorker("worker-losing") == nil
	})
	h.service.WaitForWorkerStreams()
	refreshQueue(t, h)
	assertQueuedPendingRun(t, h, enqueued.RunID)

	// The delivered run is recovering, not queued. Full teardown must not make
	// it eligible for the queue observer or the no-worker deadline.
	recovering, err := h.repo.GetRun(context.Background(), occupying.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if recovering.Status != "recovering" || !recovering.ExecutionActive {
		t.Fatalf("delivered run after disconnect = %s/active=%v, want recovering/active=true",
			recovering.Status, recovering.ExecutionActive)
	}
	recoveringState := h.runState(t, occupying.RunID)
	if reason, _, since, unroutable := queueWaitState(t, h, occupying.RunID); reason != string(runoutcome.QueueWaitNone) || since != nil || unroutable != nil {
		t.Fatalf("recovering queue truth = %q/%v/%v, want none with no open intervals", reason, since, unroutable)
	}

	reason, _, _, unroutable := queueWaitState(t, h, enqueued.RunID)
	if reason != string(runoutcome.QueueWaitNoCompatibleWorker) {
		t.Fatalf("queue reason after losing every worker = %q, want %q", reason, runoutcome.QueueWaitNoCompatibleWorker)
	}
	if unroutable == nil || *unroutable != clock.read().UnixNano() {
		t.Fatalf("no-worker interval = %v, want it to open at the observation instant", unroutable)
	}
	// The public snapshot says the same thing, so a reopened conversation and a
	// live stream cannot disagree.
	if state := h.runState(t, enqueued.RunID); state.QueueWaitReason != string(runoutcome.QueueWaitNoCompatibleWorker) {
		t.Fatalf("published queue reason = %q, want %q", state.QueueWaitReason, runoutcome.QueueWaitNoCompatibleWorker)
	}

	// Still inside the bound: the run keeps waiting rather than being ended.
	clock.advance(9 * time.Minute)
	refreshQueue(t, h)
	assertQueuedPendingRun(t, h, enqueued.RunID)

	clock.advance(2 * time.Minute)
	refreshQueue(t, h)
	state := h.runState(t, enqueued.RunID)
	if state.Lifecycle != "failed" || state.OutcomeReason != string(runoutcome.ReasonExpired) {
		t.Fatalf("run past the no-worker bound = %s/%s, want failed/expired", state.Lifecycle, state.OutcomeReason)
	}
	if state.QueueWaitReason != string(runoutcome.QueueWaitNoCompatibleWorker) {
		t.Fatalf("terminal queue reason = %q, want the bound that ended it", state.QueueWaitReason)
	}
	if !state.FinishedAt.Valid {
		t.Fatal("a terminal run recorded no finish time")
	}
	if after := h.runState(t, occupying.RunID); after != recoveringState {
		t.Fatalf("queue expiry changed delivered/recovering work: %+v, want %+v", after, recoveringState)
	}
}

func TestIncompatibleLiveWorkerIsNotACompatibleOne(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, queueWaitDispatch(runoutcome.QueueTimeoutPolicyFail))
	h.service.queueNow = clock.read

	stream := connectWorkerCapabilities(t, h, "worker-swap", "registration-swap",
		modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1))
	defer func() { _ = stream.CloseSend() }()
	occupyQueueWorker(t, h, stream)
	enqueued := h.enqueueRun(t, "route me")
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertQueuedPendingRun(t, h, enqueued.RunID)

	// The worker stays connected and stays live; what it can do changes.
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerCapabilitiesUpdated{
		WorkerCapabilitiesUpdated: &turingv1.RuntimeWorkerCapabilitiesUpdated{
			WorkerId: "worker-swap", RegistrationId: "registration-swap",
			Capabilities: modelCapabilities(
				turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 8192, 1),
		},
	}}); err != nil {
		t.Fatal(err)
	}
	// Wait on the registry, then sweep explicitly. The capability-change path
	// refreshes queue state as an advisory best effort — a failure there is
	// logged rather than allowed to tear down a healthy worker — so the
	// guarantee this test is about belongs to the recovery tick, which is what
	// refreshQueue stands in for.
	eventually(t, eventuallyTimeout, func() bool {
		return h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
			AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		}) != nil
	})
	refreshQueue(t, h)
	assertQueuedPendingRun(t, h, enqueued.RunID)
	if reason, _, _, _ := queueWaitState(t, h, enqueued.RunID); reason != string(runoutcome.QueueWaitNoCompatibleWorker) {
		t.Fatalf("queue reason with only an incompatible live worker = %q, want %q",
			reason, runoutcome.QueueWaitNoCompatibleWorker)
	}

	clock.advance(11 * time.Minute)
	refreshQueue(t, h)
	if state := h.runState(t, enqueued.RunID); state.Lifecycle != "failed" {
		t.Fatalf("run with only an incompatible live worker = %q, want failed", state.Lifecycle)
	}
}

func TestBusyCompatibleWorkerNeverStartsTheNoWorkerBound(t *testing.T) {
	clock := newQueueClock()
	// The no-worker bound is short and the overall bound is off, so a run that
	// wrongly counted "busy" as "absent" would be terminalized and one that
	// treats them as different cannot be.
	h := newHarnessWithDispatch(t, DispatchConfig{
		MaxConcurrentRuns: 1,
		QueueWait: QueueWaitPolicy{
			NoWorkerTimeout: time.Minute,
			Policy:          runoutcome.QueueTimeoutPolicyFail,
		},
	})
	h.service.queueNow = clock.read

	occupying := h.enqueueRun(t, "take the slot")
	stream := connectWorkerCapabilities(t, h, "worker-busy", "registration-busy",
		modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1))
	defer func() { _ = stream.CloseSend() }()
	// The worker takes the first run and is then at its advertised capacity.
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetRunId() != occupying.RunID {
		t.Fatalf("dispatched %q, want the run meant to occupy the slot", assigned.GetRunId())
	}
	queuedBehind := h.enqueueRun(t, "wait your turn")

	clock.advance(10 * time.Minute)
	refreshQueue(t, h)

	reason, _, _, unroutable := queueWaitState(t, h, queuedBehind.RunID)
	if reason != string(runoutcome.QueueWaitNone) {
		t.Fatalf("queue reason behind a busy compatible worker = %q, want %q", reason, runoutcome.QueueWaitNone)
	}
	if unroutable != nil {
		t.Fatal("a busy compatible worker opened a no-worker interval")
	}
	if state := h.runState(t, queuedBehind.RunID); state.Lifecycle != "queued" {
		t.Fatalf("run behind a busy worker = %q, want it still queued", state.Lifecycle)
	}
}

func TestCapabilityRestorationBeforeExpiryClearsTheQueueTruth(t *testing.T) {
	clock := newQueueClock()
	dispatch := queueWaitDispatch(runoutcome.QueueTimeoutPolicyFail)
	// The disconnected occupant's execution fence keeps the global slot busy
	// even after a new compatible worker connects.
	dispatch.MaxConcurrentRuns = 1
	h := newHarnessWithDispatch(t, dispatch)
	h.service.queueNow = clock.read

	first := connectWorkerCapabilities(t, h, "worker-gone", "registration-gone",
		modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1))
	occupyQueueWorker(t, h, first)
	enqueued := h.enqueueRun(t, "come back")
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertQueuedPendingRun(t, h, enqueued.RunID)
	if err := first.CloseSend(); err != nil {
		t.Fatal(err)
	}
	eventually(t, eventuallyTimeout, func() bool {
		return h.service.registeredWorker("worker-gone") == nil
	})
	h.service.WaitForWorkerStreams()
	refreshQueue(t, h)
	assertQueuedPendingRun(t, h, enqueued.RunID)
	if reason, _, _, _ := queueWaitState(t, h, enqueued.RunID); reason != string(runoutcome.QueueWaitNoCompatibleWorker) {
		t.Fatalf("queue reason after the worker went = %q", reason)
	}

	clock.advance(5 * time.Minute)
	second := connectWorkerCapabilities(t, h, "worker-back", "registration-back",
		modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1))
	defer func() { _ = second.CloseSend() }()
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	refreshQueue(t, h)
	assertQueuedPendingRun(t, h, enqueued.RunID)

	reason, _, _, unroutable := queueWaitState(t, h, enqueued.RunID)
	if reason != string(runoutcome.QueueWaitNone) {
		t.Fatalf("queue reason after restoration = %q, want %q", reason, runoutcome.QueueWaitNone)
	}
	if unroutable != nil {
		t.Fatal("restoration left the no-worker interval open, so the run would still expire on it")
	}
	// Past what the ORIGINAL no-worker interval would have been: a cleared
	// interval must not be resumed from where it stopped.
	clock.advance(6 * time.Minute)
	refreshQueue(t, h)
	assertQueuedPendingRun(t, h, enqueued.RunID)
}

func TestRepeatedReconciliationOverAnUnchangedQueueWritesNothing(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, queueWaitDispatch(runoutcome.QueueTimeoutPolicyFail))
	h.service.queueNow = clock.read

	stream := connectWorkerCapabilities(t, h, "worker-idle", "registration-idle",
		modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1))
	occupyQueueWorker(t, h, stream)
	enqueued := h.enqueueRun(t, "reconcile me")
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertQueuedPendingRun(t, h, enqueued.RunID)
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	eventually(t, eventuallyTimeout, func() bool {
		return h.service.registeredWorker("worker-idle") == nil
	})
	h.service.WaitForWorkerStreams()
	refreshQueue(t, h)
	assertQueuedPendingRun(t, h, enqueued.RunID)
	if reason, _, _, unroutable := queueWaitState(t, h, enqueued.RunID); reason != string(runoutcome.QueueWaitNoCompatibleWorker) || unroutable == nil {
		t.Fatalf("queue truth before repeated reconciliation = %q/%v, want no compatible worker with an open interval",
			reason, unroutable)
	}
	afterFirst := h.runState(t, enqueued.RunID)
	events := countRunEvents(t, h, enqueued.RunID, "agent.run.state_changed")

	for range 5 {
		clock.advance(30 * time.Second)
		refreshQueue(t, h)
	}

	if after := h.runState(t, enqueued.RunID); after != afterFirst {
		t.Fatalf("repeated reconciliation moved the run: %+v, want %+v", after, afterFirst)
	}
	if got := countRunEvents(t, h, enqueued.RunID, "agent.run.state_changed"); got != events {
		t.Fatalf("repeated reconciliation appended %d projections, want none", got-events)
	}
}

func TestQueueExpiryLosesTheRaceToAnAssignment(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, queueWaitDispatch(runoutcome.QueueTimeoutPolicyFail))
	h.service.queueNow = clock.read

	h.enqueueRun(t, "claim me")
	stream := connectWorkerCapabilities(t, h, "worker-claiming", "registration-claiming",
		modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1))
	defer func() { _ = stream.CloseSend() }()
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()
	claimed := h.runState(t, assigned.GetRunId())
	if claimed.Lifecycle != "running" {
		t.Fatalf("claimed run = %q, want running", claimed.Lifecycle)
	}

	// The stale page a scan could still be holding: this run was queued when it
	// was read, and its bounds are long past.
	stale := repository.PendingRoutingWork{
		RunID: assigned.GetRunId(),
		Clock: repository.QueueWaitClock{WaitedNanos: (48 * time.Hour).Nanoseconds()},
	}
	expired, err := h.service.expireOverdueQueuedRun(context.Background(), stale, runoutcome.QueueWaitNoCompatibleWorker)
	if err != nil {
		t.Fatalf("expireOverdueQueuedRun: %v", err)
	}
	if expired {
		t.Fatal("a stale scan reported that it ended a run a worker is executing")
	}
	if after := h.runState(t, assigned.GetRunId()); after != claimed {
		t.Fatalf("a stale scan changed a claimed run: %+v, want %+v", after, claimed)
	}
}

func TestQueueExpiryLeavesACancelledRunAlone(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, queueWaitDispatch(runoutcome.QueueTimeoutPolicyFail))
	h.service.queueNow = clock.read

	enqueued := h.enqueueRun(t, "cancel me")
	before := h.runState(t, enqueued.RunID)
	if _, err := h.repo.CancelRunCanonical(context.Background(), repository.CancelRunInput{
		RunID:                enqueued.RunID,
		AssistantMessageID:   enqueued.AssistantMessageID,
		ExpectedStateVersion: before.StateVersion,
		Cancellation:         runoutcome.AbandonedCancellation(),
	}); err != nil {
		t.Fatalf("CancelRunCanonical: %v", err)
	}
	cancelled := h.runState(t, enqueued.RunID)

	stale := repository.PendingRoutingWork{
		RunID: enqueued.RunID,
		Clock: repository.QueueWaitClock{WaitedNanos: (48 * time.Hour).Nanoseconds()},
	}
	expired, err := h.service.expireOverdueQueuedRun(context.Background(), stale, runoutcome.QueueWaitNoCompatibleWorker)
	if err != nil {
		t.Fatalf("expireOverdueQueuedRun: %v", err)
	}
	if expired {
		t.Fatal("a queue bound reported that it ended an already cancelled run")
	}
	if after := h.runState(t, enqueued.RunID); after != cancelled {
		t.Fatalf("a queue bound overrode a cancellation: %+v, want %+v", after, cancelled)
	}
	// And the observation half is just as inert: a cancelled run is not queued
	// pending work, so nothing may be written onto it.
	if err := h.service.recordQueueWaitObservation(context.Background(), stale,
		runoutcome.QueueWaitNoCompatibleWorker); err != nil {
		t.Fatalf("recordQueueWaitObservation: %v", err)
	}
	if after := h.runState(t, enqueued.RunID); after != cancelled {
		t.Fatalf("the queue observer wrote onto a cancelled run: %+v", after)
	}
}

func TestQueueAgeSurvivesARequeueAndAnOrchestratorRestart(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, DispatchConfig{
		QueueWait: QueueWaitPolicy{MaxWait: 30 * time.Minute, Policy: runoutcome.QueueTimeoutPolicyFail},
	})
	h.service.queueNow = clock.read

	enqueued := h.enqueueRun(t, "requeue me")
	// Backdate the enqueue so the run has already banked most of its allowance
	// before anything below happens, without moving the clock the enqueue used.
	openedAt := clock.read().Add(-20 * time.Minute).UnixNano()
	if _, err := h.database.ExecContext(context.Background(),
		`UPDATE agent_runs SET queued_since_ns = ? WHERE id = ?`, openedAt, enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	// A dispatch and a requeue: the run leaves the queue and comes back, which
	// is the churn that must not restart the clock.
	stream := connectWorkerCapabilities(t, h, "worker-flap", "registration-flap",
		modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1))
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetRunId() != enqueued.RunID {
		t.Fatalf("dispatched %q, want the run under test", assigned.GetRunId())
	}
	// A same-run transient release: the owning attempt hands the run straight
	// back to the queue, which is the requeue edge that must not restart the
	// clock.
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId:                enqueued.RunID,
		Code:                 "worker_busy",
		FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH,
		AutomaticRetryClass:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
		ExpectedStateVersion: assigned.GetExpectedStateVersion(),
	}}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, eventuallyTimeout, func() bool {
		return h.runState(t, enqueued.RunID).Lifecycle == "queued"
	})
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}

	_, banked, since, _ := queueWaitState(t, h, enqueued.RunID)
	if banked < (20 * time.Minute).Nanoseconds() {
		t.Fatalf("banked queue age after a requeue = %d ns, want at least the 20 minutes already waited", banked)
	}
	if since == nil {
		t.Fatal("the requeued run opened no new queued interval")
	}

	// A restart: a brand new service over the same durable state, which is all
	// an orchestrator restart is from the queue's point of view.
	restarted := NewWithConfig(h.repo, h.bus, DispatchConfig{
		QueueWait: QueueWaitPolicy{MaxWait: 30 * time.Minute, Policy: runoutcome.QueueTimeoutPolicyFail},
	}, h.approvals)
	restarted.queueNow = clock.read
	if err := restarted.RefreshPendingRoutingState(context.Background(), "restart"); err != nil {
		t.Fatalf("RefreshPendingRoutingState after restart: %v", err)
	}
	if state := h.runState(t, enqueued.RunID); state.Lifecycle != "queued" {
		t.Fatalf("run just after a restart = %q, want still queued inside its bound", state.Lifecycle)
	}

	clock.advance(11 * time.Minute)
	if err := restarted.RefreshPendingRoutingState(context.Background(), "restart"); err != nil {
		t.Fatalf("RefreshPendingRoutingState after restart: %v", err)
	}
	state := h.runState(t, enqueued.RunID)
	if state.Lifecycle != "failed" || state.OutcomeReason != string(runoutcome.ReasonExpired) {
		t.Fatalf("run past its accumulated queue age = %s/%s, want failed/expired", state.Lifecycle, state.OutcomeReason)
	}
	if state.QueueWaitReason != string(runoutcome.QueueWaitQueueTimeout) {
		t.Fatalf("terminal queue reason = %q, want %q", state.QueueWaitReason, runoutcome.QueueWaitQueueTimeout)
	}
}

func TestConfiguredCancelPolicyEndsTheRunAsCancelled(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, queueWaitDispatch(runoutcome.QueueTimeoutPolicyCancel))
	h.service.queueNow = clock.read

	enqueued := h.enqueueRun(t, "cancel policy")
	refreshQueue(t, h)
	clock.advance(11 * time.Minute)
	refreshQueue(t, h)

	state := h.runState(t, enqueued.RunID)
	if state.Lifecycle != "cancelled" || state.OutcomeReason != string(runoutcome.ReasonAbandoned) {
		t.Fatalf("run under the cancel policy = %s/%s, want cancelled/abandoned", state.Lifecycle, state.OutcomeReason)
	}
	if state.QueueWaitReason != string(runoutcome.QueueWaitNoCompatibleWorker) {
		t.Fatalf("terminal queue reason = %q, want the bound that ended it", state.QueueWaitReason)
	}
}

func TestDisabledQueueBoundsLeaveRunsWaiting(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, DispatchConfig{})
	h.service.queueNow = clock.read

	enqueued := h.enqueueRun(t, "wait forever")
	refreshQueue(t, h)
	clock.advance(365 * 24 * time.Hour)
	refreshQueue(t, h)

	state := h.runState(t, enqueued.RunID)
	if state.Lifecycle != "queued" {
		t.Fatalf("run with both bounds disabled = %q, want queued", state.Lifecycle)
	}
	// The explanation is still durable even with no bound: the operator turned
	// off the deadline, not the truth.
	if state.QueueWaitReason != string(runoutcome.QueueWaitNoCompatibleWorker) {
		t.Fatalf("queue reason with bounds off = %q, want the observation to stand", state.QueueWaitReason)
	}
}

// One unwritable row must not stop the queue. The pages are keyset-ordered, so
// a run the guarded writer refuses sits at a fixed position: if it ended the
// pass, every run behind it would wait forever and TUR-018's loss and
// restoration notices would stop with it.
func TestOneUnwritableRunDoesNotStopTheSweep(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, queueWaitDispatch(runoutcome.QueueTimeoutPolicyFail))
	h.service.queueNow = clock.read

	poisoned := h.enqueueRun(t, "broken correlation")
	overdue := h.enqueueRun(t, "behind the broken one")
	// Break the first run's assistant correlation the way a restored or
	// hand-edited legacy database can. The guarded transition writer refuses to
	// commit anything for a run in this shape, which is exactly the failure
	// this test is about — and it is left status='queued' with a pending job,
	// so the scan keeps selecting it.
	if _, err := h.database.ExecContext(context.Background(),
		`UPDATE agent_runs SET assistant_message_id = NULL WHERE id = ?`, poisoned.RunID); err != nil {
		t.Fatal(err)
	}

	refreshQueue(t, h)
	clock.advance(11 * time.Minute)
	refreshQueue(t, h)

	if state := h.runState(t, overdue.RunID); state.Lifecycle != "failed" {
		t.Fatalf("run behind an unwritable one = %q, want it to have reached its bound", state.Lifecycle)
	}
	// The broken row is left exactly as it was rather than being forced.
	if state := h.runState(t, poisoned.RunID); state.Lifecycle != "queued" {
		t.Fatalf("unwritable run = %q, want it untouched", state.Lifecycle)
	}
}

// The page already carries the stored reason, so an unchanged queue must cost
// no transaction at all — this sweep runs on every enqueue and every worker
// event, on SQLite's single connection, not only on the reaper tick.
func TestAnUnchangedQueueObservationOpensNoTransaction(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{})
	h.service.queueNow = newQueueClock().read

	// A page whose stored reason already agrees with the observation. The run
	// ID is deliberately one no row has: reaching the repository at all would
	// fail, so a pass here proves nothing was opened.
	unchanged := repository.PendingRoutingWork{
		RunID: "run_does_not_exist",
		Clock: repository.QueueWaitClock{Reason: string(runoutcome.QueueWaitNoCompatibleWorker)},
	}
	if err := h.service.recordQueueWaitObservation(
		context.Background(), unchanged, runoutcome.QueueWaitNoCompatibleWorker); err != nil {
		t.Fatalf("an unchanged observation reached the database: %v", err)
	}
}

func TestQueueObservationPublishesItsSnapshotLive(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, queueWaitDispatch(runoutcome.QueueTimeoutPolicyFail))
	h.service.queueNow = clock.read

	enqueued := h.enqueueRun(t, "tell me why")
	published, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()
	refreshQueue(t, h)

	observation := recvBusEvent(t, published, func(event events.Event) bool {
		return event.Type == "agent.run.state_changed" && event.RunID == enqueued.RunID
	})
	var payload struct {
		RunState struct {
			Lifecycle       string `json:"lifecycle"`
			QueueWaitReason string `json:"queueWaitReason"`
			StateVersion    int64  `json:"stateVersion"`
		} `json:"runState"`
	}
	if err := json.Unmarshal([]byte(observation.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode observation payload: %v", err)
	}
	if payload.RunState.Lifecycle != "queued" {
		t.Fatalf("observation lifecycle = %q, want queued", payload.RunState.Lifecycle)
	}
	if payload.RunState.QueueWaitReason != string(runoutcome.QueueWaitNoCompatibleWorker) {
		t.Fatalf("observation queue reason = %q, want %q",
			payload.RunState.QueueWaitReason, runoutcome.QueueWaitNoCompatibleWorker)
	}
	if payload.RunState.StateVersion <= 1 {
		t.Fatalf("observation version = %d, want one increment past the enqueue", payload.RunState.StateVersion)
	}
}
