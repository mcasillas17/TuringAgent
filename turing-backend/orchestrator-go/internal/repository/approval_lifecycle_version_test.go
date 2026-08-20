package repository

import (
	"context"
	"database/sql"
	"testing"
)

// The approval pair is the one lifecycle change a user actually participates
// in: the run stops to ask, and starts again because a person said yes. Both
// halves are real transitions, so both owe a version and a projection.
//
// They are also the two transitions with a lifecycle event of their own. That
// is exactly where a second, redundant state-changed event is easiest to leave
// lying beside the real one, and where a projection is easiest to forget
// entirely — the approval event was published for its own sake long before it
// carried any run state. These tests pin both: exactly one event, and that one
// event carrying the state the row committed.
//
// approval.requested and approval.approved are the temporary carriers at this
// task's boundary. When the typed readiness path replaces them, the assertions
// below have to move with it rather than be deleted: what is being pinned is
// "one transition, one version, one projection", not the event names.

// approvalPairFixture leaves a claimed, running run with a tool call recorded
// and ready to need authorization.
func approvalPairFixture(t *testing.T, repo *Repository, worker string) (EnqueueUserMessageResult, RunState) {
	t.Helper()
	ctx := context.Background()
	enqueued := enqueueRun(t, repo, "Approval lifecycle")
	if _, err := repo.ClaimNextJob(ctx, "general_assistant", worker); err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_approval_pair", RunID: enqueued.RunID, ModelToolCallID: "model_approval_pair",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:pair"); err != nil {
		t.Fatalf("RecordToolCallBefore: %v", err)
	}
	running, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	if running.Lifecycle != lifecycleRunning {
		t.Fatalf("fixture lifecycle = %q, want running", running.Lifecycle)
	}
	return enqueued, running
}

// assertDurableSnapshot re-reads an event from the log and checks the snapshot
// a reopening client would actually get, rather than only the copy handed back
// to the writer's caller.
func assertDurableSnapshot(t *testing.T, repo *Repository, event Event, state RunState) {
	t.Helper()
	var payloadJSON string
	if err := repo.db.QueryRowContext(context.Background(),
		`SELECT payload_json FROM events WHERE id = ?`, event.EventID).Scan(&payloadJSON); err != nil {
		t.Fatalf("read stored %s event: %v", event.Type, err)
	}
	assertSnapshotMatchesState(t, decodeRunStateSnapshot(t, Event{
		Type: event.Type, PayloadJSON: payloadJSON,
	}), state)
}

// TestApprovalRequestCommitsOneVersionAndOneProjection pins running ->
// waiting_approval. The run really did stop, so the version really did move,
// and approval.requested is the single event that says so.
func TestApprovalRequestCommitsOneVersionAndOneProjection(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, running := approvalPairFixture(t, repo, "worker-approval-request")
	events := countRunEvents(t, repo, enqueued.RunID)

	_, requested, err := repo.CreateApprovalWithEvent(ctx, enqueued.RunID, "call_approval_pair",
		"general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:pair", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("CreateApprovalWithEvent: %v", err)
	}

	waiting, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Lifecycle != lifecycleWaitingApproval || waiting.OutcomeReason != "none" {
		t.Fatalf("state = %s/%s, want waiting_approval/none", waiting.Lifecycle, waiting.OutcomeReason)
	}
	if waiting.StateVersion != running.StateVersion+1 {
		t.Fatalf("version = %d, want exactly one increment past %d", waiting.StateVersion, running.StateVersion)
	}
	// Waiting is not over. A finish time here would end the run in every client
	// that reads one.
	if waiting.FinishedAt.Valid || waiting.FinishedAt.String != "" {
		t.Fatalf("a waiting run carries a finish time: %+v", waiting.FinishedAt)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != events+1 {
		t.Fatalf("the approval request appended %d events, want exactly 1", after-events)
	}
	if requested.Type != "approval.requested" {
		t.Fatalf("request event = %q, want approval.requested", requested.Type)
	}
	// The request event IS the lifecycle projection, so no state-changed event
	// may be sitting beside it saying the same thing at the same version.
	var stateChanged int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE run_id = ? AND type = ?`,
		enqueued.RunID, runStateChangedEventType).Scan(&stateChanged); err != nil {
		t.Fatal(err)
	}
	if stateChanged != 0 {
		t.Fatalf("the approval request appended %d redundant %s events", stateChanged, runStateChangedEventType)
	}
	assertSnapshotMatchesState(t, decodeRunStateSnapshot(t, requested), waiting)
	assertDurableSnapshot(t, repo, requested, waiting)
}

// TestApprovalDecisionCommitsOneVersionAndOneProjection pins the other half.
// A user said yes, so the run resumes: one more version, and approval.approved
// carrying it.
func TestApprovalDecisionCommitsOneVersionAndOneProjection(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _ := approvalPairFixture(t, repo, "worker-approval-decision")
	approval, _, err := repo.CreateApprovalWithEvent(ctx, enqueued.RunID, "call_approval_pair",
		"general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:pair", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("CreateApprovalWithEvent: %v", err)
	}
	waiting, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	decided, err := repo.ApproveApprovalWithEvent(ctx, approval.ApprovalID, "approval-token", sql.NullString{}, now())
	if err != nil {
		t.Fatalf("ApproveApprovalWithEvent: %v", err)
	}
	if !decided.Changed {
		t.Fatal("the decision reported no change")
	}

	resumed, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Lifecycle != lifecycleRunning || resumed.OutcomeReason != "none" {
		t.Fatalf("state = %s/%s, want running/none", resumed.Lifecycle, resumed.OutcomeReason)
	}
	if resumed.StateVersion != waiting.StateVersion+1 {
		t.Fatalf("version = %d, want exactly one increment past %d", resumed.StateVersion, waiting.StateVersion)
	}
	if resumed.FinishedAt.Valid || resumed.FinishedAt.String != "" {
		t.Fatalf("a resumed run carries a finish time: %+v", resumed.FinishedAt)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != events+1 {
		t.Fatalf("the approval decision appended %d events, want exactly 1", after-events)
	}
	if decided.ApprovalEvent.Type != "approval.approved" {
		t.Fatalf("decision event = %q, want approval.approved", decided.ApprovalEvent.Type)
	}
	assertSnapshotMatchesState(t, decodeRunStateSnapshot(t, decided.ApprovalEvent), resumed)
	assertDurableSnapshot(t, repo, decided.ApprovalEvent, resumed)
}

// TestSecondApprovalDecisionOnAResumedRunStillProjectsIt covers the case a run
// with two pending authorizations creates. The first decision resumes the run,
// so the second one arrives at a run that is already running: its approval row
// really does change, but its lifecycle does not.
//
// The decision still has to be announced. Without an event the approval stays
// pending in every client that is watching, and the user is asked to authorize
// something they already authorized.
func TestSecondApprovalDecisionOnAResumedRunStillProjectsIt(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _ := approvalPairFixture(t, repo, "worker-two-approvals")
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_approval_second", RunID: enqueued.RunID, ModelToolCallID: "model_approval_second",
	}, "general_assistant", "files", "files.update", `{"path":"second.txt"}`, "sha256:second"); err != nil {
		t.Fatalf("RecordToolCallBefore: %v", err)
	}
	first, _, err := repo.CreateApprovalWithEvent(ctx, enqueued.RunID, "call_approval_pair",
		"general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:pair", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("CreateApprovalWithEvent: %v", err)
	}
	waiting, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	second, secondRequested, err := repo.CreateApprovalWithEvent(ctx, enqueued.RunID, "call_approval_second",
		"general_assistant", "files.update", `{"path":"second.txt"}`, "sha256:second", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("second CreateApprovalWithEvent: %v", err)
	}
	// The run was already waiting, so the second request is the same shape of
	// event as the second decision below: no lifecycle change, and a projection
	// of the state as it stands rather than no projection at all.
	if secondRequested.EventID == "" || secondRequested.Type != "approval.requested" {
		t.Fatalf("second request event = %+v, want a durable approval.requested", secondRequested)
	}
	stillWaiting, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if stillWaiting != waiting {
		t.Fatalf("the second request changed the run: %+v, want %+v", stillWaiting, waiting)
	}
	assertSnapshotMatchesState(t, decodeRunStateSnapshot(t, secondRequested), waiting)
	assertDurableSnapshot(t, repo, secondRequested, waiting)
	if _, err := repo.ApproveApprovalWithEvent(ctx, first.ApprovalID, "approval-token-1", sql.NullString{}, now()); err != nil {
		t.Fatalf("first ApproveApprovalWithEvent: %v", err)
	}
	resumed, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Lifecycle != lifecycleRunning {
		t.Fatalf("state after the first decision = %q, want running", resumed.Lifecycle)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	decided, err := repo.ApproveApprovalWithEvent(ctx, second.ApprovalID, "approval-token-2", sql.NullString{}, now())
	if err != nil {
		t.Fatalf("second ApproveApprovalWithEvent: %v", err)
	}
	if !decided.Changed || decided.Approval.Status != "approved" {
		t.Fatalf("second decision = %+v, want a committed approval", decided)
	}
	// A publisher only publishes what carries an ID, so an empty event is the
	// same as no event to every subscriber.
	if decided.ApprovalEvent.EventID == "" || decided.ApprovalEvent.Type != "approval.approved" {
		t.Fatalf("second decision event = %+v, want a durable approval.approved", decided.ApprovalEvent)
	}
	if after := countRunEvents(t, repo, enqueued.RunID); after != events+1 {
		t.Fatalf("the second decision appended %d events, want exactly 1", after-events)
	}
	// The run was already running: the decision changed an approval, not the
	// lifecycle, so nothing about the run may move.
	unchanged, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != resumed {
		t.Fatalf("the second decision changed the run: %+v, want %+v", unchanged, resumed)
	}
	var stateChanged int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE run_id = ? AND type = ?`,
		enqueued.RunID, runStateChangedEventType).Scan(&stateChanged); err != nil {
		t.Fatal(err)
	}
	if stateChanged != 0 {
		t.Fatalf("the second decision appended %d redundant %s events", stateChanged, runStateChangedEventType)
	}
	// The projection is the canonical state as it stands, so a client that
	// reopens on this event sees the run it is actually looking at.
	assertSnapshotMatchesState(t, decodeRunStateSnapshot(t, decided.ApprovalEvent), resumed)
	assertDurableSnapshot(t, repo, decided.ApprovalEvent, resumed)
}
