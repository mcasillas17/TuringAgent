package repository

import (
	"context"
	"errors"
	"testing"
)

func TestLateFailedAfterForTerminalSafeCallIsIdempotent(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Late safe after")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "safe tool", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-late-safe"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_late_safe", RunID: enqueued.RunID, Status: "allowed", ModelToolCallID: "model_late_safe",
	}, "general_assistant", "system", "system.time", `{"timezone":"UTC"}`, "sha256:late-safe"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CancelRunWithEvent(ctx, enqueued.RunID, "client_cancelled", `{"reason":"client_cancelled"}`); err != nil {
		t.Fatal(err)
	}

	record := ToolCallAfterRecord{
		ToolCallID: "call_late_safe", RunID: enqueued.RunID, ServerName: "system", ToolName: "system.time",
		ModelToolCallID: "model_late_safe", ArgsHash: "sha256:late-safe", WorkerID: "worker-late-safe",
		Status: "failed", ErrorCode: "cancelled", ErrorMessage: "worker cleanup after cancellation",
	}
	changed, event, err := repo.RecordToolCallAfterWithEvent(ctx, record, "tool.call.failed", `{"status":"failed"}`)
	if err != nil {
		t.Fatalf("late failed AFTER: %v", err)
	}
	if changed || event.EventID != "" {
		t.Fatalf("late failed AFTER changed=%v event=%+v, want idempotent no event", changed, event)
	}
	var failures int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'tool.call.failed'`, enqueued.RunID).Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if failures != 1 {
		t.Fatalf("tool.call.failed count = %d, want terminalization event only", failures)
	}
	record.ResultSummary = "conflicting result"
	if _, _, err := repo.RecordToolCallAfterWithEvent(ctx, record, "tool.call.failed", `{"status":"failed"}`); !errors.Is(err, ErrToolCallInvalidTransition) {
		t.Fatalf("conflicting late failed AFTER error = %v, want ErrToolCallInvalidTransition", err)
	}
}

func TestLateCompletedAfterCorrectsConsumedApprovalTerminalCleanup(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Late committed approval after")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "mutate file", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-late-committed"); err != nil {
		t.Fatal(err)
	}
	const toolCallID = "call_late_committed"
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: toolCallID, RunID: enqueued.RunID, Status: "approval_required", ModelToolCallID: "model_late_committed",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt","content":"changed"}`, "sha256:late-committed"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, toolCallID, "general_assistant", "files.update", `{"path":"note.txt","content":"changed"}`, "sha256:late-committed", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApproveApproval(ctx, approval.ApprovalID, "approval-token", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ConsumeApprovalWithEvent(ctx, approval.ApprovalID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CancelRunWithEvent(ctx, enqueued.RunID, "client_cancelled", `{"reason":"client_cancelled"}`); err != nil {
		t.Fatal(err)
	}

	changed, event, err := repo.RecordToolCallAfterWithEvent(ctx, ToolCallAfterRecord{
		ToolCallID: toolCallID, RunID: enqueued.RunID, ServerName: "files", ToolName: "files.update",
		ModelToolCallID: "model_late_committed", ArgsHash: "sha256:late-committed", WorkerID: "worker-late-committed",
		Status: "completed", ResultSummary: `{"updated":true}`, DurationMS: 12,
	}, "tool.call.completed", `{"toolCallId":"call_late_committed","toolName":"files.update","serverName":"files"}`)
	if err != nil {
		t.Fatalf("late completed AFTER: %v", err)
	}
	if !changed || event.Type != "tool.call.completed" {
		t.Fatalf("late completed AFTER changed=%v event=%+v, want completed correction", changed, event)
	}
	var toolStatus, resultSummary, runStatus string
	if err := database.QueryRowContext(ctx, `SELECT status, COALESCE(result_summary, '') FROM tool_calls WHERE id = ?`, toolCallID).Scan(&toolStatus, &resultSummary); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if toolStatus != "completed" || resultSummary != `{"updated":true}` || runStatus != "cancelled" {
		t.Fatalf("late correction tool=%q summary=%q run=%q", toolStatus, resultSummary, runStatus)
	}
}
