package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestTerminalRunRevokesApprovedToolLifecycle(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Terminal dependents")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "approve then cancel", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-terminal", 1, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_terminal", RunID: enqueued.RunID, Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{}`, "sha256:test"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "call_terminal", "general_assistant", "files.update", `{}`, "sha256:test", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApproveApproval(ctx, approval.ApprovalID, "issued-token", sql.NullString{}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CancelRunWithEvent(ctx, enqueued.RunID, "client_cancelled", `{"reason":"client_cancelled"}`); err != nil {
		t.Fatal(err)
	}

	var approvalStatus, toolStatus string
	var approvalToken sql.NullString
	if err := database.QueryRowContext(ctx, `SELECT status, approval_token FROM approvals WHERE id = ?`, approval.ApprovalID).Scan(&approvalStatus, &approvalToken); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = 'call_terminal'`).Scan(&toolStatus); err != nil {
		t.Fatal(err)
	}
	if approvalStatus != "expired" || approvalToken.Valid || toolStatus != "failed" {
		t.Fatalf("terminal cleanup approval=%s token=%q tool=%s", approvalStatus, approvalToken.String, toolStatus)
	}
	if _, err := repo.ConsumeApproval(ctx, approval.ApprovalID, ""); err == nil {
		t.Fatal("ConsumeApproval succeeded after terminal transition")
	}
}

func TestApproveApprovalRejectsDatabaseExpiryBoundary(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Approval boundary")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "boundary", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-boundary", 1, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_boundary", RunID: enqueued.RunID, Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{}`, "sha256:test"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "call_boundary", "general_assistant", "files.update", `{}`, "sha256:test", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE approvals SET expires_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?
	`, approval.ApprovalID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.ApproveApprovalWithEvent(ctx, approval.ApprovalID, "must-not-issue", sql.NullString{}, ""); err == nil {
		t.Fatal("ApproveApprovalWithEvent accepted an approval at its database expiry boundary")
	}
	current, err := repo.GetApproval(ctx, approval.ApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "pending" || current.ApprovalToken != "" {
		t.Fatalf("boundary approval = %+v, want unchanged pending state", current)
	}
}

func TestTerminalRunEmitsFailureEventForActiveSafeToolCall(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Safe tool terminal cleanup")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "safe tool", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-safe", 1, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_safe_active", RunID: enqueued.RunID,
		ModelToolCallID: "model_safe_active", Status: "allowed",
	}, "general_assistant", "system", "system.echo", `{"value":"hello"}`, "sha256:safe"); err != nil {
		t.Fatal(err)
	}

	events, err := repo.CancelRunWithEvent(ctx, enqueued.RunID, "client_cancelled", `{"reason":"client_cancelled"}`)
	if err != nil {
		t.Fatal(err)
	}
	terminal := events[len(events)-1]
	replayed, _, err := repo.ReplayEvents(ctx, enqueued.SessionID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var toolFailure Event
	for _, event := range replayed {
		if event.Type == "tool.call.failed" {
			toolFailure = event
		}
	}
	if toolFailure.EventID == "" || toolFailure.Sequence >= terminal.Sequence {
		t.Fatalf("safe tool failure event = %+v, terminal = %+v; want one ordered before terminal", toolFailure, terminal)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(toolFailure.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"toolCallId": "call_safe_active",
		"toolName":   "system.echo",
		"serverName": "system",
		// The cancellation's own outcome class. The transport cannot tell a
		// deliberate stop from a dropped connection, so it says abandoned
		// rather than claiming the user meant it.
		"category": "abandoned",
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("safe tool failure payload = %#v, want %#v", payload, want)
	}
}
