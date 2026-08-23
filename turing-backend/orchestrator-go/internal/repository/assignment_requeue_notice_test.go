package repository

import (
	"context"
	"reflect"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// runningAssignment leaves a run running under worker, with its assignment
// delivered, so reconciliation treats it as work in flight.
func runningAssignment(t *testing.T, repo *Repository, worker string) (EnqueueUserMessageResult, Assignment) {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Requeue notice")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "lose my worker", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", worker)
	if err != nil {
		t.Fatal(err)
	}
	assignment := Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: worker, AttemptID: claimed.AssignmentAttemptID,
	}
	if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	return enqueued, assignment
}

// TestRecoverAssignmentRequeuePublishesNotice covers the requeue path users
// actually hit. worker_busy (RequeueOrFailRetryableRun) is a reconnect race the
// capacity gate mostly prevents; losing a worker mid-run — disconnect, lease
// expiry, orchestrator restart — all land here, and all were silent.
func TestRecoverAssignmentRequeuePublishesNotice(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, assignment := runningAssignment(t, repo, "worker-vanished")

	reconciliation, err := repo.RecoverAssignmentWithLimit(ctx, assignment, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reconciliation.Requeued {
		t.Fatalf("reconciliation = %+v, want requeued", reconciliation)
	}

	notice := onlyRunStepEvent(t, reconciliation.Events)
	requeued, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	assertStepNotice(t, notice, runoutcome.NoticeRecoveryRetry, 2, 3, requeued.StateVersion)
	if !notice.RunID.Valid || notice.RunID.String != enqueued.RunID {
		t.Fatalf("notice run_id = %+v, want %q (a client correlates by run)", notice.RunID, enqueued.RunID)
	}
	if notice.SessionID != enqueued.SessionID {
		t.Fatalf("notice session_id = %q, want %q", notice.SessionID, enqueued.SessionID)
	}
}

// Fencing must stay silent. A fenced assignment means a newer attempt already
// owns the run, so telling the user "retrying" would describe work that is not
// happening.
//
// Note this returns at the fence check, well before the requeue branch — it
// pins fencing, NOT the notice code. The requeue and give-up branches are
// covered by TestRecoverAssignmentRequeuePublishesNotice and by
// TestRecoverAssignmentAtCutoffFailsAtConfiguredMaximumAttempt respectively.
func TestFencedReconciliationEmitsNoNotice(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	_, assignment := runningAssignment(t, repo, "worker-live")

	// staleRecovery=false on a delivered, still-active assignment fences rather
	// than requeues.
	reconciliation, err := repo.ReconcileAssignmentWithLimit(ctx, assignment, 3)
	if err != nil {
		t.Fatal(err)
	}
	if reconciliation.Requeued {
		t.Fatalf("reconciliation = %+v, want no requeue for a live assignment", reconciliation)
	}
	for _, event := range reconciliation.Events {
		if event.Type == "agent.run.step" {
			t.Fatalf("non-requeue reconciliation emitted a notice: %+v", event)
		}
	}
}

// The give-up notice is inserted before failPendingApprovalLifecycleTx so the
// explanation precedes every consequence of it, matching the ordering in
// RequeueOrFailRetryableRun. Every other exhaustion test runs with no pending
// approval, where that cleanup returns nothing — so swapping the two calls
// would be invisible. This is the case where the ordering is observable.
func TestExhaustedRecoveryOrdersGiveUpBeforeApprovalCleanup(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, assignment := runningAssignment(t, repo, "worker-vanished")

	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_giveup", RunID: enqueued.RunID, ModelToolCallID: "model_giveup",
		Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateApproval(ctx, enqueued.RunID, "call_giveup", "general_assistant",
		"files.update", `{"path":"note.txt"}`, "sha256:test", "2099-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// CreateApproval parks the run in waiting_approval; put it back to running so
	// reconciliation takes the branch that terminalizes on exhausted attempts,
	// with the approval still pending underneath it.
	//
	// This is a shortcut to a REACHABLE state, not a manufactured one. A run can
	// hold several pending approvals — the unique index is on tool_call_id, not
	// one-per-run (0001_initial.sql) — and CreateApproval accepts a run that is
	// already 'running'. So: approval B is created (run -> waiting_approval),
	// then an earlier approval A is approved, which sets the run back to
	// 'running' and leaves B pending. Two SQL statements here stand in for that
	// interleaving. Using an already-'approved' approval instead would NOT
	// exercise this branch: terminalizeStaleApprovedAuthorizationTx intercepts
	// that case earlier in reconcileAssignment.
	if _, err := database.ExecContext(ctx, `UPDATE agent_runs SET status = 'running' WHERE id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	// maxAttempts=1 with the job on attempt 1 exhausts the budget immediately.
	beforeGiveUp, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	reconciliation, err := repo.RecoverAssignmentWithLimit(ctx, assignment, 1)
	if err != nil {
		t.Fatal(err)
	}
	var eventTypes []string
	for _, event := range reconciliation.Events {
		eventTypes = append(eventTypes, event.Type)
	}
	// approval.expired (not denied) is this path's pre-existing wording for a
	// pending approval abandoned by a timed-out job.
	want := []string{"agent.run.step", "approval.expired", "tool.call.failed", "agent.run.failed"}
	if !reflect.DeepEqual(eventTypes, want) {
		t.Fatalf("exhausted recovery events = %v, want %v", eventTypes, want)
	}
	assertStepNotice(t, reconciliation.Events[0], runoutcome.NoticeRecoveryExhausted, 1, 1, beforeGiveUp.StateVersion)
	// Sequence must agree with slice order — the client renders by sequence.
	for index := 1; index < len(reconciliation.Events); index++ {
		if reconciliation.Events[index].Sequence <= reconciliation.Events[index-1].Sequence {
			t.Fatalf("event %d (%s) has sequence %d, not after %d",
				index, eventTypes[index], reconciliation.Events[index].Sequence, reconciliation.Events[index-1].Sequence)
		}
	}
}
