package repository

import (
	"context"
	"testing"
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
	const want = "Retrying after the worker became unavailable"
	if got := runStepNote(t, notice); got != want {
		t.Fatalf("recovery note = %q, want %q", got, want)
	}
	if !notice.RunID.Valid || notice.RunID.String != enqueued.RunID {
		t.Fatalf("notice run_id = %+v, want %q (a client correlates by run)", notice.RunID, enqueued.RunID)
	}
	if notice.SessionID != enqueued.SessionID {
		t.Fatalf("notice session_id = %q, want %q", notice.SessionID, enqueued.SessionID)
	}
}

// A reconciliation that does not requeue must stay silent: a fenced or cleared
// assignment is not something the user needs to be told about, and a spurious
// notice would read as a retry that never happened.
func TestReconcileAssignmentWithoutRequeueEmitsNoNotice(t *testing.T) {
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
