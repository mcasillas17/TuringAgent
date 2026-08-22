package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// payloadCarriesRunStateKey reports whether a durable event's raw stored
// bytes contain the runState key at all — the writer-side question this
// package's read-side carrier gate exists to answer for. It is deliberately a
// raw map-key check rather than a decode into the typed snapshot: a writer
// that started merging a malformed or partial runState value would still be
// a writer that started merging one, and this guard has to catch that too.
func payloadCarriesRunStateKey(t *testing.T, eventType string, payloadJSON string) bool {
	t.Helper()
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payloadJSON), &decoded); err != nil {
		t.Fatalf("decode %s payload %q: %v", eventType, payloadJSON, err)
	}
	_, present := decoded[runStatePayloadKey]
	return present
}

// driftGuardRun is one fresh queued-then-claimed run, driven far enough for
// every writer this test exercises to have somewhere valid to write to.
type driftGuardRun struct {
	enqueued repository.EnqueueUserMessageResult
	job      repository.Job
	workerID string
}

// startDriftGuardRun enqueues and claims a run through the exact real public
// entry points a production caller uses, so the run this test's writers act
// on is not a fixture built by reaching into repository internals.
func startDriftGuardRun(t *testing.T, repo *repository.Repository, title string) driftGuardRun {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	workerID := "worker-" + title
	job, err := repo.ClaimNextJob(ctx, "general_assistant", workerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	return driftGuardRun{enqueued: enqueued, job: job, workerID: workerID}
}

// requestDriftGuardApproval drives RecordToolCallBefore and
// CreateApprovalWithEvent for one more tool call on an already-claimed run,
// exactly as a worker asking permission for a tool call would.
func requestDriftGuardApproval(t *testing.T, repo *repository.Repository, run driftGuardRun, toolCallID string) (repository.ApprovalRecord, repository.Event) {
	t.Helper()
	ctx := context.Background()
	if err := repo.RecordToolCallBefore(ctx, repository.ToolCallRecord{
		ToolCallID: toolCallID, RunID: run.enqueued.RunID, ModelToolCallID: "model_" + toolCallID,
	}, "general_assistant", "files", "files.update", `{"path":"`+toolCallID+`.txt"}`, "sha256:"+toolCallID); err != nil {
		t.Fatalf("RecordToolCallBefore(%s): %v", toolCallID, err)
	}
	approval, event, err := repo.CreateApprovalWithEvent(ctx, run.enqueued.RunID, toolCallID,
		"general_assistant", "files.update", `{"path":"`+toolCallID+`.txt"}`, "sha256:"+toolCallID, "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("CreateApprovalWithEvent(%s): %v", toolCallID, err)
	}
	return approval, event
}

// TestRunStateCarrierSetMatchesActualRepositoryWriters is the writer-side
// drift guard: it asserts the exact set of durable event types whose payload
// actually contains runState, as committed by the repository's real writers,
// equals this package's declared safe-payload carrier set.
//
// run_state_carrier_test.go's runStateCarrierEventTypes is an oracle written
// down by a human reading the repository's source. It is only honest if it is
// exactly the set of types the writers actually commit a snapshot under, and
// that fact can drift the moment either side changes without the other. This
// test does not re-read the source to check; it drives every one of those
// writers through its real public entry point — EnqueueUserMessage,
// ClaimNextJob, CreateApprovalWithEvent (both the transition-borne primary
// path and the already-waiting fallback path), ApproveApprovalWithEvent (the
// only writer approval.approved has), ResumeApprovedRun (the state_changed
// writer), and the three terminal transitions — and reads back the exact
// bytes each one committed to the database. It drives DenyApprovalWithEvent,
// ExpireApprovalWithEvent, and ConsumeApprovalWithEvent the same way and
// asserts their payloads carry no runState at all, so a change that started
// merging a snapshot into any of them — the exact regression this task's
// carrier gate exists to make harmless on the read side — fails here on the
// write side too, independent of isRunStateCarrier ever being touched.
//
// Every writer here is reachable through its ordinary public contract; none
// needed a repository-internal seam to exercise, so there is no "closest
// helper" fallback to document.
func TestRunStateCarrierSetMatchesActualRepositoryWriters(t *testing.T) {
	repo := repository.New(openEventTestDB(t))
	ctx := context.Background()
	observed := map[string]bool{}
	record := func(eventType string, payloadJSON string) {
		t.Helper()
		has := payloadCarriesRunStateKey(t, eventType, payloadJSON)
		if existing, seen := observed[eventType]; seen && existing != has {
			t.Fatalf("%s: two real writer samples disagree on whether the payload carries runState (%v vs %v)",
				eventType, existing, has)
		}
		observed[eventType] = has
	}

	// agent.run.queued: EnqueueUserMessage's own writer.
	primary := startDriftGuardRun(t, repo, "drift-primary")
	record(primary.enqueued.QueuedEvent.Type, primary.enqueued.QueuedEvent.PayloadJSON)
	// agent.run.started: ClaimNextJob's own writer.
	record(primary.job.StartedEvent.Type, primary.job.StartedEvent.PayloadJSON)

	// approval.requested, primary path: this request IS the running ->
	// waiting_approval transition, so its own event carries the transition's
	// committed state.
	first, requestedPrimary := requestDriftGuardApproval(t, repo, primary, "call_drift_a")
	record(requestedPrimary.Type, requestedPrimary.PayloadJSON)

	// approval.requested, fallback path: the run is already waiting on the
	// first request, so this second one cannot itself move the lifecycle, and
	// falls back to appendApprovalRunStateEventTx.
	second, requestedFallback := requestDriftGuardApproval(t, repo, primary, "call_drift_b")
	record(requestedFallback.Type, requestedFallback.PayloadJSON)
	if requestedPrimary.Type != requestedFallback.Type {
		t.Fatalf("primary and fallback approval requests disagree on event type: %q vs %q",
			requestedPrimary.Type, requestedFallback.Type)
	}

	// approval.approved: appendApprovalRunStateEventTx is this event's only
	// writer — approving a decision never itself moves a run's lifecycle.
	waiting, err := repo.GetRunState(ctx, primary.enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	decidedFirst, err := repo.ApproveApprovalWithEvent(ctx, first.ApprovalID, "token-drift-a", sql.NullString{}, "")
	if err != nil {
		t.Fatalf("ApproveApprovalWithEvent(first): %v", err)
	}
	record(decidedFirst.ApprovalEvent.Type, decidedFirst.ApprovalEvent.PayloadJSON)

	// agent.run.state_changed: ResumeApprovedRun is a real transition with no
	// lifecycle event of its own, so it falls through to the shared
	// state-changed projection.
	resumed, err := repo.ResumeApprovedRun(ctx, repository.ResumeApprovedRunInput{
		RunID: primary.enqueued.RunID, ApprovalID: first.ApprovalID,
		WorkerID: primary.workerID, AssignmentAttemptID: primary.job.AssignmentAttemptID,
		ExpectedStateVersion: waiting.StateVersion,
	})
	if err != nil {
		t.Fatalf("ResumeApprovedRun: %v", err)
	}
	if len(resumed.Events) == 0 {
		t.Fatal("ResumeApprovedRun committed no event")
	}
	stateChanged := resumed.Events[len(resumed.Events)-1]
	record(stateChanged.Type, stateChanged.PayloadJSON)

	// A second approval.approved sample, now that the run is running again:
	// still the same only writer, still no lifecycle move.
	decidedSecond, err := repo.ApproveApprovalWithEvent(ctx, second.ApprovalID, "token-drift-b", sql.NullString{}, "")
	if err != nil {
		t.Fatalf("ApproveApprovalWithEvent(second): %v", err)
	}
	record(decidedSecond.ApprovalEvent.Type, decidedSecond.ApprovalEvent.PayloadJSON)

	// agent.run.completed / agent.run.failed / agent.run.cancelled: each
	// terminalizes a run exactly once, so each gets its own fresh run.
	completedRun := startDriftGuardRun(t, repo, "drift-completed")
	completed, err := repo.CompleteRunCanonical(ctx, repository.CompleteRunInput{
		RunID: completedRun.enqueued.RunID, AssistantMessageID: completedRun.enqueued.AssistantMessageID,
		Content: "done", ExpectedStateVersion: completedRun.job.ExpectedStateVersion,
	})
	if err != nil {
		t.Fatalf("CompleteRunCanonical: %v", err)
	}
	record(lastEvent(t, completed.Events).Type, lastEvent(t, completed.Events).PayloadJSON)

	failedRun := startDriftGuardRun(t, repo, "drift-failed")
	failed, err := repo.FailRunCanonical(ctx, repository.FailRunInput{
		RunID: failedRun.enqueued.RunID, AssistantMessageID: failedRun.enqueued.AssistantMessageID,
		ExpectedStateVersion: failedRun.job.ExpectedStateVersion,
		Failure:              runoutcome.NormalizeFailure(runoutcome.OriginProviderTransport, "model_stream_failed", runoutcome.RetryClassNever),
	})
	if err != nil {
		t.Fatalf("FailRunCanonical: %v", err)
	}
	record(lastEvent(t, failed.Events).Type, lastEvent(t, failed.Events).PayloadJSON)

	cancelledRun := startDriftGuardRun(t, repo, "drift-cancelled")
	cancelled, err := repo.CancelRunCanonical(ctx, repository.CancelRunInput{
		RunID: cancelledRun.enqueued.RunID, AssistantMessageID: cancelledRun.enqueued.AssistantMessageID,
		ExpectedStateVersion: cancelledRun.job.ExpectedStateVersion,
		Cancellation:         runoutcome.AbandonedCancellation(),
	})
	if err != nil {
		t.Fatalf("CancelRunCanonical: %v", err)
	}
	record(lastEvent(t, cancelled.Events).Type, lastEvent(t, cancelled.Events).PayloadJSON)

	// approval.denied / approval.expired / approval.consumed: the three
	// non-carriers this task calls out by name. Each is driven through its
	// real terminal writer on its own fresh run, and each must come back with
	// no runState key at all — not an honest-unknown snapshot, nothing.
	deniedRun := startDriftGuardRun(t, repo, "drift-denied")
	deniedApproval, _ := requestDriftGuardApproval(t, repo, deniedRun, "call_drift_denied")
	denied, err := repo.DenyApprovalWithEvent(ctx, deniedApproval.ApprovalID, sql.NullString{String: "no", Valid: true}, "")
	if err != nil {
		t.Fatalf("DenyApprovalWithEvent: %v", err)
	}
	record(denied.ApprovalEvent.Type, denied.ApprovalEvent.PayloadJSON)
	// DenyApprovalWithEvent also fails the run it denied approval on. That is
	// a second real agent.run.failed sample, and it is asserted consistent
	// with the dedicated failedRun sample above through the same record call.
	if denied.RunFailedEvent.Type != "" {
		record(denied.RunFailedEvent.Type, denied.RunFailedEvent.PayloadJSON)
	}

	expiredRun := startDriftGuardRun(t, repo, "drift-expired")
	expiredApproval, _ := requestDriftGuardApproval(t, repo, expiredRun, "call_drift_expired")
	expired, err := repo.ExpireApprovalWithEvent(ctx, expiredApproval.ApprovalID, "")
	if err != nil {
		t.Fatalf("ExpireApprovalWithEvent: %v", err)
	}
	record(expired.ApprovalEvent.Type, expired.ApprovalEvent.PayloadJSON)

	consumedRun := startDriftGuardRun(t, repo, "drift-consumed")
	consumedApproval, _ := requestDriftGuardApproval(t, repo, consumedRun, "call_drift_consumed")
	approvedForConsume, err := repo.ApproveApprovalWithEvent(ctx, consumedApproval.ApprovalID, "token-drift-consume", sql.NullString{}, "")
	if err != nil {
		t.Fatalf("ApproveApprovalWithEvent(consume fixture): %v", err)
	}
	record(approvedForConsume.ApprovalEvent.Type, approvedForConsume.ApprovalEvent.PayloadJSON)
	consumed, err := repo.ConsumeApprovalWithEvent(ctx, consumedApproval.ApprovalID, "")
	if err != nil {
		t.Fatalf("ConsumeApprovalWithEvent: %v", err)
	}
	record(consumed.ApprovalEvent.Type, consumed.ApprovalEvent.PayloadJSON)

	// The exact set this task asks for: every type actually observed to carry
	// runState must be exactly this package's declared carrier set, and every
	// type observed NOT to carry it must be declared a non-carrier. Comparing
	// against isRunStateCarrier — the same predicate Decode gates on — rather
	// than re-listing the eight names here keeps this test and the production
	// gate reading off one shared source, so a future edit to the carrier set
	// cannot go stale against a second copy living in this file.
	for eventType, hasState := range observed {
		if hasState != isRunStateCarrier(eventType) {
			t.Errorf("%s: real writer committed a payload with runState=%v, but isRunStateCarrier(%q)=%v",
				eventType, hasState, eventType, isRunStateCarrier(eventType))
		}
	}
	wantObserved := []string{
		"agent.run.queued", "agent.run.started", "agent.run.state_changed",
		"agent.run.completed", "agent.run.failed", "agent.run.cancelled",
		"approval.requested", "approval.approved",
		"approval.denied", "approval.expired", "approval.consumed",
	}
	if len(observed) != len(wantObserved) {
		t.Fatalf("observed %d distinct writer-produced event types %v, want exactly %d: %v",
			len(observed), observed, len(wantObserved), wantObserved)
	}
	for _, want := range wantObserved {
		if _, ok := observed[want]; !ok {
			t.Fatalf("never drove a real writer that produces %q", want)
		}
	}
	observedCarriers := 0
	for eventType, hasState := range observed {
		if hasState {
			observedCarriers++
			_ = eventType
		}
	}
	if observedCarriers != len(runStateCarrierTypes) {
		t.Fatalf("%d observed writer-produced types actually carry runState, want exactly the declared carrier set's %d members",
			observedCarriers, len(runStateCarrierTypes))
	}
}

// lastEvent returns a terminal transition's own canonical projection, which
// applyRunTransitionTx always appends last after any subsidiary events a
// terminalization's own side effects may have written.
func lastEvent(t *testing.T, events []repository.Event) repository.Event {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("transition committed no event at all")
	}
	return events[len(events)-1]
}
