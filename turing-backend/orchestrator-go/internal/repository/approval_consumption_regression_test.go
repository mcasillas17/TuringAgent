package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestConsumeApprovalAfterExpiryTerminalizesVerifiedAuthorization(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Consume expiry")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "approval boundary", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-consume-expiry"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_consume_expiry", RunID: enqueued.RunID, Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{}`, "sha256:consume-expiry"); err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Date(2030, 1, 1, 0, 0, 0, 100_000_000, time.UTC)
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "call_consume_expiry", "general_assistant", "files.update", `{}`, "sha256:consume-expiry", expiresAt.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApproveApprovalWithEvent(ctx, approval.ApprovalID, "verified-before-expiry", sql.NullString{}, FormatTimestamp(expiresAt.Add(-time.Nanosecond))); err != nil {
		t.Fatal(err)
	}

	transition, err := repo.ConsumeApprovalWithEvent(ctx, approval.ApprovalID, FormatTimestamp(expiresAt.Add(time.Nanosecond)))
	if !errors.Is(err, ErrApprovalExpired) {
		t.Fatalf("ConsumeApprovalWithEvent error = %v, want ErrApprovalExpired", err)
	}
	if transition.Approval.Status != "expired" || transition.Approval.ApprovalToken != "" {
		t.Fatalf("expired consumption transition = %+v", transition)
	}
	current, err := repo.GetApproval(ctx, approval.ApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "expired" || current.ApprovalToken != "" {
		t.Fatalf("stored approval after expiry = %+v", current)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
	var toolStatus, jobStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = 'call_consume_expiry'`).Scan(&toolStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if toolStatus != "failed" || jobStatus != "failed" {
		t.Fatalf("terminal states tool=%q job=%q, want failed", toolStatus, jobStatus)
	}
	rows, err := database.QueryContext(ctx, `
		SELECT type, payload_json
		FROM events
		WHERE run_id = ? AND type IN ('approval.expired', 'tool.call.failed', 'agent.run.failed')
		ORDER BY sequence
	`, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var eventTypes []string
	for rows.Next() {
		var eventType, payloadJSON string
		if err := rows.Scan(&eventType, &payloadJSON); err != nil {
			t.Fatal(err)
		}
		eventTypes = append(eventTypes, eventType)
		if eventType == "tool.call.failed" {
			var payload map[string]any
			if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"toolCallId": "call_consume_expiry",
				"toolName":   "files.update",
				"serverName": "files",
				"category":   "expired",
			}
			if !reflect.DeepEqual(payload, want) {
				t.Fatalf("tool.call.failed payload = %#v, want %#v", payload, want)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"approval.expired", "tool.call.failed", "agent.run.failed"}; !reflect.DeepEqual(eventTypes, want) {
		t.Fatalf("terminal event order = %v, want %v", eventTypes, want)
	}
	if _, err := repo.ConsumeApprovalWithEvent(ctx, approval.ApprovalID, FormatTimestamp(expiresAt.Add(2*time.Nanosecond))); err == nil {
		t.Fatal("expired approval was consumed after terminalization")
	}
}
