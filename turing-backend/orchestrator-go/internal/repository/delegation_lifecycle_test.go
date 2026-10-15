package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// claimChild claims the next job, which must be the delegation's child, and
// returns the claim.
func (f delegationFixture) claimChild(t *testing.T, delegation Delegation) Job {
	t.Helper()
	claimed, err := f.repo.ClaimNextCompatibleJobWithLimit(f.ctx, "general_assistant", "team-worker", 0, time.Hour,
		joinWorker(1), func(RoutingRequirements) bool { return true })
	if err != nil || claimed.RunID != delegation.ChildRunID {
		t.Fatalf("claim = %+v, %v; want the child %s", claimed, err, delegation.ChildRunID)
	}
	return claimed
}

// requestApproval leaves a running child waiting on one approval.
func (f delegationFixture) requestApproval(t *testing.T, delegation Delegation, toolCallID string) (ApprovalRecord, []Event) {
	t.Helper()
	if err := f.repo.RecordToolCallBefore(f.ctx, ToolCallRecord{
		ToolCallID: toolCallID, RunID: delegation.ChildRunID, ModelToolCallID: "model_" + toolCallID,
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:"+toolCallID); err != nil {
		t.Fatal(err)
	}
	approval, events, err := f.repo.CreateApprovalWithEvents(f.ctx, delegation.ChildRunID, toolCallID, "general_assistant",
		"files.update", `{"path":"note.txt"}`, "sha256:"+toolCallID, "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return approval, events
}

type delegationUpdate struct {
	Type, DelegationID, ChildRunID, State, ApprovalID, Summary string
	RunID, SessionID                                           string
}

func decodeUpdates(t *testing.T, events []Event) []delegationUpdate {
	t.Helper()
	var updates []delegationUpdate
	for _, event := range events {
		if event.Type != "delegation.updated" {
			continue
		}
		var payload struct {
			DelegationID string `json:"delegationId"`
			ChildRunID   string `json:"childRunId"`
			State        string `json:"state"`
			ApprovalID   string `json:"approvalId"`
			Summary      string `json:"summary"`
		}
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
			t.Fatal(err)
		}
		updates = append(updates, delegationUpdate{
			Type: event.Type, DelegationID: payload.DelegationID, ChildRunID: payload.ChildRunID, State: payload.State,
			ApprovalID: payload.ApprovalID, Summary: payload.Summary, RunID: event.RunID.String, SessionID: event.SessionID,
		})
	}
	return updates
}

// durableUpdates are the parent stream's delegation.updated events, in order.
func (f delegationFixture) durableUpdates(t *testing.T) []delegationUpdate {
	t.Helper()
	rows, err := f.repo.db.QueryContext(f.ctx, `
		SELECT id, session_id, run_id, type, payload_json FROM events
		WHERE session_id = ? AND type = 'delegation.updated' ORDER BY sequence`, f.parent.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var events []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.EventID, &event.SessionID, &event.RunID, &event.Type, &event.PayloadJSON); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return decodeUpdates(t, events)
}

func updateStates(updates []delegationUpdate) []string {
	states := make([]string, 0, len(updates))
	for _, update := range updates {
		states = append(states, update.State)
	}
	return states
}

// A task already waiting on one approval that asks for another shows the newer
// one on its card, though its state does not change, so its parent's stream is
// told which.
func TestASecondApprovalRequestReportsTheApprovalTheCardNowShows(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	f.claimChild(t, child)
	f.requestApproval(t, child, "call_first")
	second, requested := f.requestApproval(t, child, "call_second")
	updates := decodeUpdates(t, requested)
	if len(updates) != 1 || updates[0].State != "waiting_approval" || updates[0].ApprovalID != second.ApprovalID ||
		updates[0].RunID != f.parent.RunID {
		t.Fatalf("second approval request's delegation events = %+v, want the parent told it waits on %s", updates, second.ApprovalID)
	}
	if record, err := f.repo.GetDelegation(f.ctx, child.ID); err != nil || record.ApprovalID != second.ApprovalID {
		t.Fatalf("card = %+v, %v; want it waiting on %s", record, err, second.ApprovalID)
	}
}

// Approving the approval a task's card names leaves the task waiting for its
// worker to resume, with the card naming the next pending approval, or none;
// its parent's stream is told in the approval's transaction.
func TestApprovingATasksApprovalReportsWhatItsCardNowShows(t *testing.T) {
	for _, pending := range []int{1, 2} {
		t.Run(map[int]string{1: "sole approval", 2: "newer of two"}[pending], func(t *testing.T) {
			f := newDelegationFixture(t)
			child := f.delegate(t, "call_1")
			f.claimChild(t, child)
			older, _ := f.requestApproval(t, child, "call_first")
			newest := older
			want := ""
			if pending == 2 {
				newest, _ = f.requestApproval(t, child, "call_second")
				want = older.ApprovalID
			}
			approved, err := f.repo.ApproveApprovalWithEvent(f.ctx, newest.ApprovalID, "token", sql.NullString{}, now())
			if err != nil {
				t.Fatal(err)
			}
			updates := decodeUpdates(t, approved.DelegationEvents)
			if len(updates) != 1 || updates[0].State != "waiting_approval" || updates[0].ApprovalID != want ||
				updates[0].RunID != f.parent.RunID {
				t.Fatalf("approval's delegation events = %+v, want the task waiting on %q", updates, want)
			}
			if record, err := f.repo.GetDelegation(f.ctx, child.ID); err != nil || record.ApprovalID != want {
				t.Fatalf("card = %+v, %v; want it waiting on %q", record, err, want)
			}
		})
	}
}

// A task reports every change in what its card would show, on its parent's
// stream, as it happens: started, waiting for an approval (naming it), still
// waiting once it is approved (naming none), working again, and finished. Losing and regaining its worker is shown as working
// throughout, so it reports nothing; being queued again does report.
func TestATaskReportsEachChangeItsCardWouldShow(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")

	claimed := f.claimChild(t, child)
	if updates := decodeUpdates(t, claimed.DelegationEvents); len(updates) != 1 || updates[0].State != "running" {
		t.Fatalf("claim's delegation events = %+v, want the task running", updates)
	}
	approval, requested := f.requestApproval(t, child, "call_write")
	if updates := decodeUpdates(t, requested); len(updates) != 1 || updates[0].State != "waiting_approval" ||
		updates[0].ApprovalID != approval.ApprovalID {
		t.Fatalf("approval request's delegation events = %+v, want waiting on %s", updates, approval.ApprovalID)
	}
	if _, err := f.repo.ApproveApprovalWithEvent(f.ctx, approval.ApprovalID, "token", sql.NullString{}, now()); err != nil {
		t.Fatal(err)
	}
	resumed, err := f.repo.ResumeApprovedRun(f.ctx, ResumeApprovedRunInput{
		RunID: child.ChildRunID, ApprovalID: approval.ApprovalID, WorkerID: "team-worker",
		AssignmentAttemptID: claimed.AssignmentAttemptID, ExpectedStateVersion: f.stateVersion(t, child.ChildRunID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updates := decodeUpdates(t, resumed.Delegation); len(updates) != 1 || updates[0].State != "running" {
		t.Fatalf("resume's delegation events = %+v, want the task running again", updates)
	}
	fenced, err := f.repo.FenceRunOwnership(f.ctx, FenceRunOwnershipInput{
		RunID: child.ChildRunID, ExpectedStateVersion: f.stateVersion(t, child.ChildRunID),
		WorkerID: "team-worker", AssignmentAttemptID: claimed.AssignmentAttemptID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fenced.Delegation) != 0 {
		t.Fatalf("running → recovering reported %+v, want nothing", decodeUpdates(t, fenced.Delegation))
	}
	requeued, err := f.repo.RequeueRecoveringRun(f.ctx, RequeueRecoveringRunInput{
		RunID: child.ChildRunID, ExpectedStateVersion: f.stateVersion(t, child.ChildRunID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updates := decodeUpdates(t, requeued.Delegation); len(updates) != 1 || updates[0].State != "queued" {
		t.Fatalf("requeue's delegation events = %+v, want the task queued", updates)
	}
	// The recovery path that requeues a run releases its job in the same
	// pass; this test drives the run transition alone.
	f.exec(t, `UPDATE jobs SET status = 'pending', lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL
		WHERE run_id = ?`, child.ChildRunID)
	f.claimChild(t, child)
	completed := f.complete(t, child.ChildRunID, "Notes.")
	if updates := decodeUpdates(t, completed.Delegation); len(updates) != 1 || updates[0].State != "completed" ||
		updates[0].Summary != "🔬 Research completed" {
		t.Fatalf("completion's delegation events = %+v, want the task completed", updates)
	}

	durable := f.durableUpdates(t)
	// Approving reports the task still waiting, on no approval.
	want := []string{"running", "waiting_approval", "waiting_approval", "running", "queued", "running", "completed"}
	if !slices.Equal(updateStates(durable), want) || durable[2].ApprovalID != "" {
		t.Fatalf("parent stream updates = %+v, want %v", durable, want)
	}
	for _, update := range durable {
		if update.RunID != f.parent.RunID || update.SessionID != f.parent.SessionID ||
			update.DelegationID != child.ID || update.ChildRunID != child.ChildRunID || update.Summary == "" {
			t.Fatalf("update = %+v, want it on the parent's run, naming the delegation", update)
		}
	}
}

// Once the parent's conversation is being deleted, its stream takes no new
// event: its terminal sequence is frozen. A task changing state then reports
// nothing, and its own transition still commits.
func TestATaskOfAConversationBeingDeletedReportsNothing(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	f.complete(t, f.parent.RunID, "Asked Research.")
	if _, err := f.repo.BeginSessionDeletion(f.ctx, f.parent.SessionID); err != nil {
		t.Fatal(err)
	}
	before := len(f.durableUpdates(t))
	claimed := f.claimChild(t, child)
	if len(claimed.DelegationEvents) != 0 || len(f.durableUpdates(t)) != before {
		t.Fatalf("a task of a conversation being deleted reported %+v", decodeUpdates(t, claimed.DelegationEvents))
	}
}

// noticeMessages are the parent conversation's delegation notices.
func (f delegationFixture) noticeMessages(t *testing.T) []Message {
	t.Helper()
	rows, err := f.repo.db.QueryContext(f.ctx, `
		SELECT id, role, content, content_type, COALESCE(run_id, '') FROM messages
		WHERE session_id = ? AND content_type = 'delegation_notice' ORDER BY sequence`, f.parent.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var messages []Message
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.MessageID, &message.Role, &message.Content, &message.ContentType, &message.RunID); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
	return messages
}

func (f delegationFixture) wantStopped(t *testing.T, delegation Delegation) {
	t.Helper()
	var status, reason, code string
	if err := f.repo.db.QueryRowContext(f.ctx, `
		SELECT status, outcome_reason, COALESCE(cancellation_reason, '') FROM agent_runs WHERE id = ?`,
		delegation.ChildRunID).Scan(&status, &reason, &code); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || reason != string(runoutcome.ReasonAbandoned) || code != runoutcome.CodeParentRunEnded {
		t.Fatalf("task %s = %s/%s/%s, want cancelled, abandoned, parent_run_ended", delegation.ChildRunID, status, reason, code)
	}
}

// A turn that fails stops every task it started that has not finished —
// queued, working or waiting on an approval — as the orchestrator giving up
// on them, not as anything the user did. One notice in the conversation says
// whose work was stopped. Nothing is joined, so there is no continuation and
// no delegation.finished; each stopped task's last update says it was stopped.
// A task that already finished is left as it was.
func TestAFailedTurnStopsTheTasksItStarted(t *testing.T) {
	f := newDelegationFixture(t)
	done := f.delegate(t, "call_1")
	f.claimChild(t, done)
	f.complete(t, done.ChildRunID, "Done early.")
	working := f.delegate(t, "call_2")
	f.claimChild(t, working)
	waiting := f.delegate(t, "call_3")
	f.claimChild(t, waiting)
	approval, _ := f.requestApproval(t, waiting, "call_write")
	queued := f.delegate(t, "call_4")

	result := f.fail(t, f.parent.RunID, runoutcome.NormalizeFailure(runoutcome.OriginExternalProvider, "model_error", runoutcome.RetryClassNever))

	want := []string{working.ChildRunID, waiting.ChildRunID, queued.ChildRunID}
	if !slices.Equal(result.StoppedTasks, want) {
		t.Fatalf("stopped tasks = %v, want %v, in the order they were asked for", result.StoppedTasks, want)
	}
	for _, delegation := range []Delegation{working, waiting, queued} {
		f.wantStopped(t, delegation)
	}
	if status, err := f.repo.GetApproval(f.ctx, approval.ApprovalID); err != nil || status.Status == "pending" {
		t.Fatalf("the stopped task's approval = %+v, %v; want it closed", status, err)
	}
	notices := f.noticeMessages(t)
	if len(notices) != 1 || notices[0].Role != "system" || notices[0].RunID != "" ||
		strings.Count(notices[0].Content, "🔬 Research") != 1 || !strings.Contains(notices[0].Content, "stopped") {
		t.Fatalf("notices = %+v, want one system notice naming Research once", notices)
	}
	f.wantNoJoin(t)
	stoppedUpdates := decodeUpdates(t, result.Delegation)
	if len(stoppedUpdates) != 3 {
		t.Fatalf("the failure's delegation updates = %+v, want one per stopped task", stoppedUpdates)
	}
	for _, update := range stoppedUpdates {
		if update.State != "cancelled" || update.Summary != "🔬 Research was stopped because Turing's turn ended" {
			t.Fatalf("stopped update = %+v", update)
		}
	}
	if types := eventTypes(result.Events); !slices.Contains(types, "agent.run.cancelled") {
		t.Fatalf("the failure's events = %v, want the tasks' own cancellations among them for publishing", types)
	}
	var doneStatus string
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT status FROM agent_runs WHERE id = ?`, done.ChildRunID).Scan(&doneStatus); err != nil || doneStatus != "completed" {
		t.Fatalf("the finished task = %q, %v; want it left completed", doneStatus, err)
	}
}

// The user cancelling the turn stops its tasks the same way.
func TestACancelledTurnStopsTheTasksItStarted(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	result, err := f.repo.CancelUserRun(f.ctx, f.parent.SessionID, f.parent.RunID, "cancel-parent")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.StoppedTasks, []string{child.ChildRunID}) {
		t.Fatalf("stopped tasks = %v, want the one task", result.StoppedTasks)
	}
	f.wantStopped(t, child)
	if notices := f.noticeMessages(t); len(notices) != 1 {
		t.Fatalf("notices = %+v, want one", notices)
	}
}

// A turn whose tasks had all finished stops nothing and says nothing; a turn
// that completes stops nothing either, its tasks are joined instead.
func TestATurnWithNothingLeftToStopWritesNoNotice(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	f.claimChild(t, child)
	f.complete(t, child.ChildRunID, "Done.")
	result := f.fail(t, f.parent.RunID, runoutcome.NormalizeFailure(runoutcome.OriginExternalProvider, "model_error", runoutcome.RetryClassNever))
	if len(result.StoppedTasks) != 0 || len(f.noticeMessages(t)) != 0 {
		t.Fatalf("stopped %v with notices %+v, want neither", result.StoppedTasks, f.noticeMessages(t))
	}

	g := newDelegationFixture(t)
	running := g.delegate(t, "call_1")
	g.claimChild(t, running)
	completed := g.complete(t, g.parent.RunID, "Asked Research.")
	if len(completed.StoppedTasks) != 0 {
		t.Fatalf("a completed turn stopped %v", completed.StoppedTasks)
	}
	var status string
	if err := g.repo.db.QueryRowContext(g.ctx, `SELECT status FROM agent_runs WHERE id = ?`, running.ChildRunID).Scan(&status); err != nil || status != "running" {
		t.Fatalf("the task of a completed turn = %q, %v; want it still running", status, err)
	}
}

// Deleting a conversation whose turn is still running stops its tasks too, so
// nothing keeps working for a conversation that is going away; but the
// conversation takes no new message or event, so there is no notice and no
// update.
func TestDeletingARunningTurnsConversationStopsItsTasksSilently(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	before := len(f.durableUpdates(t))
	if _, err := f.repo.BeginSessionDeletion(f.ctx, f.parent.SessionID); err != nil {
		t.Fatal(err)
	}
	f.wantStopped(t, child)
	if notices := f.noticeMessages(t); len(notices) != 0 {
		t.Fatalf("notices = %+v, want none in a conversation being deleted", notices)
	}
	if after := len(f.durableUpdates(t)); after != before {
		t.Fatalf("updates went from %d to %d, want none added", before, after)
	}
}

// A delegation reads back as its card shows it: who, what they were asked,
// how it stands — waiting on which approval, how it ended — and, once its
// turn's tasks are joined, the continuation that answered from them.
func TestADelegationReadsBackAsItsCardShowsIt(t *testing.T) {
	f := newDelegationFixture(t)
	input := f.input("call_1")
	input.Task = "Gather the notes"
	created, err := f.repo.CreateDelegation(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	child := created.Delegation
	f.claimChild(t, child)
	approval, _ := f.requestApproval(t, child, "call_write")

	record, err := f.repo.GetDelegation(f.ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != child.ID || record.ParentSessionID != f.parent.SessionID || record.ParentRunID != f.parent.RunID ||
		record.ChildSessionID != child.ChildSessionID || record.ChildRunID != child.ChildRunID ||
		record.ProfileID != "research" || record.DisplayName != "Research" || record.Emoji != "🔬" ||
		record.Task != "Gather the notes" || record.State != "waiting_approval" || record.ApprovalID != approval.ApprovalID ||
		record.Summary != "🔬 Research is waiting for your approval" || record.CreatedAt == "" || record.FinishedAt != "" {
		t.Fatalf("record = %+v", record)
	}

	if _, err := f.repo.DenyApprovalWithEvent(f.ctx, approval.ApprovalID, sql.NullString{String: "no", Valid: true}, ""); err != nil {
		t.Fatal(err)
	}
	f.complete(t, f.parent.RunID, "Asked Research.")
	listed, err := f.repo.ListDelegations(f.ctx, f.parent.SessionID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListDelegations = %+v, %v; want the one", listed, err)
	}
	record = listed[0]
	if record.State != "failed" || record.ApprovalID != "" || record.ErrorCode == "" || !record.Joined ||
		record.ContinuationRunID != f.continuationRunID(t) || record.FinishedAt == "" ||
		!strings.Contains(record.Summary, "failed") {
		t.Fatalf("finished record = %+v", record)
	}
}

// Recovering is shown as running — even for a task that was waiting on an
// approval when its worker was lost, which names no approval while it is
// shown as running — and the list is in the order the tasks were asked for.
func TestListedDelegationsShowRecoveringAsRunningInOrder(t *testing.T) {
	f := newDelegationFixture(t)
	first, second := f.delegate(t, "call_1"), f.delegate(t, "call_2")
	claimed := f.claimChild(t, first)
	f.requestApproval(t, first, "call_write")
	if _, err := f.repo.FenceRunOwnership(f.ctx, FenceRunOwnershipInput{
		RunID: first.ChildRunID, ExpectedStateVersion: f.stateVersion(t, first.ChildRunID),
		WorkerID: "team-worker", AssignmentAttemptID: claimed.AssignmentAttemptID,
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := f.repo.ListDelegations(f.ctx, f.parent.SessionID)
	if err != nil || len(listed) != 2 || listed[0].ID != first.ID || listed[1].ID != second.ID {
		t.Fatalf("ListDelegations = %+v, %v; want both, in order", listed, err)
	}
	if listed[0].State != "running" || listed[0].ApprovalID != "" || listed[1].State != "queued" {
		t.Fatalf("records = %+v, %+v; want running (recovering) naming no approval, then queued", listed[0], listed[1])
	}
}

// A conversation being deleted, or one that does not exist, has no
// delegations to show, and neither does a specialist's own session.
func TestDelegationsOfAConversationThatIsNotThereAreNotFound(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	if _, err := f.repo.ListDelegations(f.ctx, child.ChildSessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ListDelegations(a specialist's session) = %v, want ErrSessionNotFound", err)
	}
	if _, err := f.repo.ListDelegations(f.ctx, "sess_missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ListDelegations(missing) = %v, want ErrSessionNotFound", err)
	}
	if _, err := f.repo.GetDelegation(f.ctx, "dlg_missing"); !errors.Is(err, ErrDelegationNotFound) {
		t.Fatalf("GetDelegation(missing) = %v, want ErrDelegationNotFound", err)
	}
	f.complete(t, f.parent.RunID, "Asked Research.")
	if _, err := f.repo.BeginSessionDeletion(f.ctx, f.parent.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ListDelegations(f.ctx, f.parent.SessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ListDelegations(deleting) = %v, want ErrSessionNotFound", err)
	}
	if _, err := f.repo.GetDelegation(f.ctx, child.ID); !errors.Is(err, ErrDelegationNotFound) {
		t.Fatalf("GetDelegation(deleting) = %v, want ErrDelegationNotFound", err)
	}
}

// The read itself shows nothing of a conversation being deleted, whatever
// was checked before it.
func TestTheDelegationReadShowsNothingOfAConversationBeingDeleted(t *testing.T) {
	f := newDelegationFixture(t)
	f.delegate(t, "call_1")
	f.complete(t, f.parent.RunID, "Asked Research.")
	if _, err := f.repo.BeginSessionDeletion(f.ctx, f.parent.SessionID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.repo.db.BeginTx(f.ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if records, err := activeDelegationsTx(f.ctx, tx, f.parent.SessionID); err != nil || len(records) != 0 {
		t.Fatalf("delegations of a conversation being deleted = %+v, %v; want none", records, err)
	}
}

// stoppedDeliveredRun is a run delivered to a worker and then stopped the way
// a failed turn stops its tasks: its worker has been told nothing yet.
func stoppedDeliveredRun(t *testing.T, repo *Repository) Assignment {
	t.Helper()
	ctx := context.Background()
	run, _ := queuedTerminalSource(t, repo)
	job, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-stopped", 0, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	assignment := Assignment{RunID: run.RunID, JobID: run.JobID, WorkerID: "worker-stopped", AttemptID: job.AssignmentAttemptID}
	if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	state, err := repo.GetRunState(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CancelRunCanonical(ctx, CancelRunInput{
		RunID: run.RunID, ExpectedStateVersion: state.StateVersion, Cancellation: runoutcome.ParentEndedCancellation(),
	}); err != nil {
		t.Fatal(err)
	}
	return assignment
}

// A stopped task's worker has to let it go before its execution counts as
// over, as for a user's cancellation: a heartbeat proves only that the worker
// is alive, and a lost stream proves nothing before its lease runs out.
func TestAStoppedTaskStaysContainedUntilItsWorkerLetsGo(t *testing.T) {
	ctx := context.Background()
	t.Run("heartbeat", func(t *testing.T) {
		repo := New(openTestDB(t))
		assignment := stoppedDeliveredRun(t, repo)
		result, err := repo.ReconcileHeartbeatAssignment(ctx, assignment, 3)
		if err != nil || !result.AwaitingExit || result.Cleared {
			t.Fatalf("heartbeat reconciliation = %+v, %v; want it awaiting the worker's exit", result, err)
		}
		if run, err := repo.GetRun(ctx, assignment.RunID); err != nil || !run.ExecutionActive {
			t.Fatalf("run = %+v, %v; want its execution still contained", run, err)
		}
	})
	t.Run("lost stream before the lease runs out", func(t *testing.T) {
		repo := New(openTestDB(t))
		assignment := stoppedDeliveredRun(t, repo)
		result, err := repo.RecoverAssignmentAtCutoffWithLimit(ctx, assignment, time.Now().UTC(), 3)
		if err != nil || !result.AwaitingExit || result.Cleared {
			t.Fatalf("recovery = %+v, %v; want it awaiting the worker's exit", result, err)
		}
		if run, err := repo.GetRun(ctx, assignment.RunID); err != nil || !run.ExecutionActive {
			t.Fatalf("run = %+v, %v; want its execution still contained", run, err)
		}
	})
}
