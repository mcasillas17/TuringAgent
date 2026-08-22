package repository

import (
	"context"
	"errors"
	"testing"
)

func TestCancelRunTerminalizesPendingApprovalAndToolCall(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Cancel pending approval")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "cancel approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{ToolCallID: "call_cancel_approval", RunID: enqueued.RunID}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:cancel"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "call_cancel_approval", "general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:cancel", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cancelRunEvents(t, repo, enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	var approvalStatus, toolStatus, runStatus, jobStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM approvals WHERE id = ?`, approval.ApprovalID).Scan(&approvalStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = 'call_cancel_approval'`).Scan(&toolStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if approvalStatus == "pending" || toolStatus == "approval_required" || runStatus != "cancelled" || jobStatus != "cancelled" {
		t.Fatalf("cancellation left lifecycle open: approval=%q tool=%q run=%q job=%q", approvalStatus, toolStatus, runStatus, jobStatus)
	}
	if _, err := repo.RecordToolCallAfter(ctx, ToolCallAfterRecord{
		ToolCallID: "call_cancel_approval", RunID: enqueued.RunID, ServerName: "files", ToolName: "files.update",
		Status: "failed", ErrorCode: "cancelled", ErrorMessage: "client_cancelled",
	}); err != nil {
		t.Fatalf("late cancellation cleanup AFTER: %v", err)
	}
}

func TestCancelRunFencesPendingAssignmentBeforeDelivery(t *testing.T) {
	tests := map[string]func(*testing.T, *Repository, string) error{
		"at the run's current version": func(t *testing.T, repo *Repository, runID string) error {
			_, err := cancelRunAtCurrentVersion(t, repo, runID)
			return err
		},
		// The same transition read through the events its caller publishes, so
		// a fence that skipped the projection would fail here too.
		"through the events it appended": func(t *testing.T, repo *Repository, runID string) error {
			_, err := cancelRunEvents(t, repo, runID)
			return err
		},
	}
	for name, cancel := range tests {
		t.Run(name, func(t *testing.T) {
			database := openTestDB(t)
			repo := New(database)
			ctx := context.Background()
			session, err := repo.CreateSession(ctx, "Cancel pending assignment")
			if err != nil {
				t.Fatal(err)
			}
			enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
				SessionID: session.SessionID, Content: "cancel before send", AgentID: "general_assistant",
				ModelProvider: "ollama", Model: "llama3.2",
			})
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-cancel-pending")
			if err != nil {
				t.Fatal(err)
			}
			assignment := Assignment{
				JobID: claimed.JobID, RunID: claimed.RunID,
				WorkerID: "worker-cancel-pending", AttemptID: claimed.AssignmentAttemptID,
			}
			if err := cancel(t, repo, enqueued.RunID); err != nil {
				t.Fatal(err)
			}
			if err := repo.BeginAssignmentSend(ctx, assignment); !errors.Is(err, ErrAssignmentFenced) {
				t.Fatalf("BeginAssignmentSend after cancellation = %v, want ErrAssignmentFenced", err)
			}
			run, err := repo.GetRun(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != "cancelled" || !run.ExecutionActive || run.ExecutionState != "pending_send" {
				t.Fatalf("cancelled pending assignment = %+v, want retained pending-send containment", run)
			}
		})
	}
}

func TestPreservingFailureFencesActiveExecution(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Fence failed execution")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "fence this execution", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-fence"); err != nil {
		t.Fatal(err)
	}

	if _, err := failRunPreservingExecutionAtCurrentVersion(
		t, repo, enqueued.RunID, testFailure("approval_delivery_failed"),
	); err != nil {
		t.Fatalf("FailRunCanonical: %v", err)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.ExecutionActive || run.ExecutionState != "uncertain" {
		t.Fatalf("run = %+v, want active uncertain execution fence", run)
	}
}
