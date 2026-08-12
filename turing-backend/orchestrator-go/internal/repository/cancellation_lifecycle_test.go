package repository

import (
	"context"
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
	if _, err := repo.CancelRunWithEvent(ctx, enqueued.RunID, "client_cancelled", `{"reason":"client_cancelled"}`); err != nil {
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
