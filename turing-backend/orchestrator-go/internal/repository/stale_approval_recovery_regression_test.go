package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestRecoverStaleApprovedAuthorizationTerminalizesInsteadOfRequeueing(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Stale approval")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "approved stale work", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-stale-approved")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_stale_approved", RunID: enqueued.RunID, Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{}`, "sha256:stale-approved"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "call_stale_approved", "general_assistant", "files.update", `{}`, "sha256:stale-approved", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApproveApproval(ctx, approval.ApprovalID, "approved-token", ""); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	expired := cutoff.Add(-time.Second)
	if _, err := database.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
		WHERE id = ?
	`, FormatTimestamp(expired), expired.UnixNano(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	events, err := repo.RecoverStaleAssignments(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "agent.run.failed" {
		t.Fatalf("recovery events = %+v, want one run failure", events)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || run.ExecutionActive {
		t.Fatalf("stale approved run = %+v, want failed inactive", run)
	}
	current, err := repo.GetApproval(ctx, approval.ApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "expired" || current.ApprovalToken != "" {
		t.Fatalf("stale approved authorization = %+v, want revoked expired token", current)
	}
	if _, err := repo.ConsumeApproval(ctx, approval.ApprovalID, ""); err == nil {
		t.Fatal("old approved token was consumable after stale recovery")
	}
	var toolStatus, jobStatus string
	var jobCode sql.NullString
	if err := database.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = 'call_stale_approved'`).Scan(&toolStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT status, error_code FROM jobs WHERE id = ?`, claimed.JobID).Scan(&jobStatus, &jobCode); err != nil {
		t.Fatal(err)
	}
	if toolStatus != "failed" || jobStatus != "failed" || jobCode.String != "side_effect_uncertain" {
		t.Fatalf("stale approval terminal state tool=%q job=%q/%q", toolStatus, jobStatus, jobCode.String)
	}
}
