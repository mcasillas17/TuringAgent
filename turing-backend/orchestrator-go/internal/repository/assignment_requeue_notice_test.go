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
	const want = "Retrying (attempt 2 of 3) after the worker became unavailable"
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
