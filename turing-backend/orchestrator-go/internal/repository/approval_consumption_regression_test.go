package repository

import (
	"context"
	"errors"
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
	if _, err := repo.ApproveApprovalWithEvent(ctx, approval.ApprovalID, "verified-before-expiry", FormatTimestamp(expiresAt.Add(-time.Nanosecond))); err != nil {
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
	if _, err := repo.ConsumeApprovalWithEvent(ctx, approval.ApprovalID, FormatTimestamp(expiresAt.Add(2*time.Nanosecond))); err == nil {
		t.Fatal("expired approval was consumed after terminalization")
	}
}
