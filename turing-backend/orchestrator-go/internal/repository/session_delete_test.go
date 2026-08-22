package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// seedDeletableSession builds a session that has produced one of everything the
// FK graph is supposed to cascade through, plus an audit row, so a deletion
// test can prove each table is actually emptied.
func seedDeletableSession(t *testing.T, repo *Repository, title string, content string) EnqueueUserMessageResult {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: content, AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-delete"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_" + enqueued.RunID, RunID: enqueued.RunID, ModelToolCallID: "model_" + enqueued.RunID,
		Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{"path":"secret.txt"}`, "sha256:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateApproval(ctx, enqueued.RunID, "call_"+enqueued.RunID, "general_assistant",
		"files.update", `{"path":"secret.txt"}`, "sha256:test", "2099-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// correlation_id is the run id at every production call site, which is the
	// only thing tying an audit row back to a session.
	if err := repo.RecordAudit(ctx, enqueued.RunID, "runtime", "", "tool.call.before",
		"call_"+enqueued.RunID, `{"args":{"path":"secret.txt"},"resultSummary":"private"}`); err != nil {
		t.Fatal(err)
	}
	return enqueued
}

// finishRun terminalizes the seeded run so deletion is permitted. Seeding
// deliberately leaves the run live (ClaimNextJob makes it running, CreateApproval
// moves it to waiting_approval), which is what the refusal test needs.
func finishRun(t *testing.T, repo *Repository, runID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := repo.db.ExecContext(ctx, `UPDATE approvals SET status = 'consumed' WHERE run_id = ?`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE jobs SET status = 'completed' WHERE run_id = ?`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE agent_runs SET status = 'completed', execution_active = 0 WHERE id = ?`, runID); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, repo *Repository, query string, args ...any) int {
	t.Helper()
	var count int
	if err := repo.db.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestBeginSessionDeletionMakesSessionUnreadable(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Withdraw immediately")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	if _, err := repo.GetSession(ctx, session.SessionID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetSession after deletion begins = %v, want sql.ErrNoRows", err)
	}
}

func TestBeginSessionDeletionTerminalizesRecoveringRuns(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _, _ := recoveringRun(t, repo, "worker-delete-recovering")

	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	if state.Lifecycle != lifecycleCancelled {
		t.Fatalf("recovering run lifecycle = %q, want cancelled", state.Lifecycle)
	}
}

func TestBeginSessionDeletionCommitsCanonicalCancellation(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Canonical deletion cancellation")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "delete this run", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	after, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Lifecycle != lifecycleCancelled || after.OutcomeReason != "abandoned" {
		t.Fatalf("deleted-session run state = %s/%s, want cancelled/abandoned",
			after.Lifecycle, after.OutcomeReason)
	}
	if after.StateVersion != before.StateVersion+1 {
		t.Fatalf("deleted-session run version = %d, want %d", after.StateVersion, before.StateVersion+1)
	}
	var cancelledEvents int
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM events
		WHERE run_id = ? AND type = 'agent.run.cancelled'
	`, enqueued.RunID).Scan(&cancelledEvents); err != nil {
		t.Fatal(err)
	}
	if cancelledEvents != 1 {
		t.Fatalf("agent.run.cancelled events = %d, want exactly one", cancelledEvents)
	}

	if _, err := repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatalf("idempotent BeginSessionDeletion: %v", err)
	}
	replayed, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.StateVersion != after.StateVersion {
		t.Fatalf("idempotent deletion changed run version from %d to %d",
			after.StateVersion, replayed.StateVersion)
	}
}

func TestBeginSessionDeletionRevokesApprovalsOnRecoveringRuns(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	const workerID = "worker-delete-approval"
	enqueued := enqueueRun(t, repo, "Delete recovering approval")
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", workerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_delete_recovering", RunID: enqueued.RunID,
		ModelToolCallID: "model_delete_recovering",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:delete-recovering"); err != nil {
		t.Fatalf("RecordToolCallBefore: %v", err)
	}
	approval, _, err := repo.CreateApprovalWithEvent(
		ctx,
		enqueued.RunID,
		"call_delete_recovering",
		"general_assistant",
		"files.update",
		`{"path":"note.txt"}`,
		"sha256:delete-recovering",
		"2099-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("CreateApprovalWithEvent: %v", err)
	}
	if _, err := repo.ApproveApprovalWithEvent(
		ctx,
		approval.ApprovalID,
		"approval-token",
		sql.NullString{},
		now(),
	); err != nil {
		t.Fatalf("ApproveApprovalWithEvent: %v", err)
	}
	waiting, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FenceRunOwnership(ctx, FenceRunOwnershipInput{
		RunID: enqueued.RunID, ExpectedStateVersion: waiting.StateVersion,
		WorkerID: workerID, AssignmentAttemptID: claimed.AssignmentAttemptID,
	}); err != nil {
		t.Fatalf("FenceRunOwnership: %v", err)
	}

	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	if _, err := repo.ConsumeApprovalWithEvent(ctx, approval.ApprovalID, ""); err == nil {
		t.Fatal("approval on a recovering deleted-session run remained consumable")
	}
	stored, err := repo.GetApproval(ctx, approval.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if stored.Status != "expired" {
		t.Fatalf("approval status = %q, want expired", stored.Status)
	}
}

func TestBeginSessionDeletionExcludesMessagesFromSearch(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Withdraw search")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "withdrawal search sentinel", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	found, err := repo.SearchMessages(ctx, "", "", "withdrawal", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("search returned deleting-session content: %+v", found)
	}
}

func TestBeginSessionDeletionRejectsNewAndIdempotentEnqueue(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Reject enqueue")
	if err != nil {
		t.Fatal(err)
	}
	input := EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "first withdrawal turn", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", IdempotencyKey: "delete-idempotency-key",
	}
	if _, err := repo.EnqueueUserMessage(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	if _, err := repo.EnqueueUserMessage(ctx, input); !errors.Is(err, ErrSessionDeleting) {
		t.Fatalf("idempotent enqueue after deletion begins = %v, want ErrSessionDeleting", err)
	}
	input.IdempotencyKey = ""
	input.Content = "new withdrawal turn"
	if _, err := repo.EnqueueUserMessage(ctx, input); !errors.Is(err, ErrSessionDeleting) {
		t.Fatalf("new enqueue after deletion begins = %v, want ErrSessionDeleting", err)
	}
}

func TestBeginSessionDeletionHidesListsMessagesAndEventReplay(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Hide every read", "withdraw read sentinel")

	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	sessions, err := repo.ListSessions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("ListSessions returned deleting session: %+v", sessions)
	}
	if _, err := repo.ListMessages(ctx, enqueued.SessionID, 10); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ListMessages after deletion begins = %v, want sql.ErrNoRows", err)
	}
	if _, err := repo.ListMessagesBefore(ctx, enqueued.SessionID, "msg_missing", 10); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ListMessagesBefore after deletion begins = %v, want sql.ErrNoRows", err)
	}
	if _, _, err := repo.ReplayEvents(ctx, enqueued.SessionID, 0, 10); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ReplayEvents after deletion begins = %v, want ErrSessionNotFound", err)
	}
}

func TestBeginSessionDeletionExcludesGlobalSessionUpdateSnapshot(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Derived title must hide")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "title disclosure sentinel", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}
	updates, err := repo.ListLatestSessionUpdatedEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 0 {
		t.Fatalf("global update snapshot includes deleting session: %+v", updates)
	}
}

func TestBeginSessionDeletionCancelsSessionWorkAndRevokesApprovals(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Fence work", "withdraw active work")

	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	var runStatus, jobStatus, approvalStatus, toolStatus string
	if err := repo.db.QueryRowContext(ctx, `SELECT status FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.QueryRowContext(ctx, `SELECT status FROM jobs WHERE run_id = ?`, enqueued.RunID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.QueryRowContext(ctx, `SELECT status FROM approvals WHERE run_id = ?`, enqueued.RunID).Scan(&approvalStatus); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE run_id = ?`, enqueued.RunID).Scan(&toolStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "cancelled" || jobStatus != "cancelled" || approvalStatus == "pending" || approvalStatus == "approved" || toolStatus == "approval_required" {
		t.Fatalf("deletion left active work: run=%q job=%q approval=%q tool=%q", runStatus, jobStatus, approvalStatus, toolStatus)
	}
}

func TestBeginSessionDeletionRejectsSessionRoutingReadsAndWrites(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Delete routing")
	if err != nil {
		t.Fatal(err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); !errors.Is(err, ErrSessionDeleting) {
		t.Fatalf("SetSessionAgent = %v, want ErrSessionDeleting", err)
	}
	if err := repo.ClearSessionAgent(ctx, session.SessionID); !errors.Is(err, ErrSessionDeleting) {
		t.Fatalf("ClearSessionAgent = %v, want ErrSessionDeleting", err)
	}
	if _, _, err := repo.GetSessionAgent(ctx, session.SessionID); !errors.Is(err, ErrSessionDeleting) {
		t.Fatalf("GetSessionAgent = %v, want ErrSessionDeleting", err)
	}
}

func TestBeginSessionDeletionImmediatelyScrubsAuditPayloads(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Scrub before finalization", "withdraw audit sentinel")

	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	var payload string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT payload_json
		FROM audit_logs
		WHERE correlation_id = ? AND action = 'tool.call.before'
	`, enqueued.RunID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != scrubbedAuditPayload {
		t.Fatalf("audit payload after deletion begins = %q, want %q", payload, scrubbedAuditPayload)
	}
}

func TestBeginSessionDeletionPreventsLateAuditPayload(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Block late audit", "withdraw late audit")

	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	inserted, err := repo.RecordAuditForExistingRun(
		ctx,
		enqueued.RunID,
		"runtime",
		"",
		"tool.call.after",
		"call_late",
		`{"resultSummary":"late secret"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("late audit payload was inserted for a deleting session")
	}
}

func TestAdvanceSessionDeletionWaitsForExecutionExitThenWithdrawsRows(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Wait for exit", "withdraw after acknowledgement")

	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	receipt, err := repo.AdvanceSessionDeletion(ctx, enqueued.SessionID)
	if err != nil {
		t.Fatalf("AdvanceSessionDeletion while active: %v", err)
	}
	if receipt.State != "quiescing" || !receipt.Retryable {
		t.Fatalf("active execution receipt = %+v, want retryable quiescing", receipt)
	}
	if err := repo.AcknowledgeExecutionExit(ctx, enqueued.RunID); err != nil {
		t.Fatalf("AcknowledgeExecutionExit: %v", err)
	}

	receipt, err = repo.AdvanceSessionDeletion(ctx, enqueued.SessionID)
	if err != nil {
		t.Fatalf("AdvanceSessionDeletion after acknowledgement: %v", err)
	}
	if receipt.State != "completed" || receipt.Retryable {
		t.Fatalf("completed receipt = %+v", receipt)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sessions WHERE id = ?`, enqueued.SessionID); got != 0 {
		t.Fatalf("session rows after completed withdrawal = %d, want 0", got)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH ?`, "acknowledgement"); got != 0 {
		t.Fatalf("withdrawn content remains in FTS: %d rows", got)
	}
}

func TestAdvanceSessionDeletionRetainsRetryableFailureForOwnedArtifact(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Artifact failure", "withdraw artifact")
	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AcknowledgeExecutionExit(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO sandbox_artifacts (
			id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at
		) VALUES (?, ?, ?, ?, ?, 'ready', 'delete_on_session_delete', 1, ?)
	`,
		"artifact_pending",
		enqueued.SessionID,
		enqueued.RunID,
		"sha256:artifact",
		"sessions/test/runs/test/files/secret.txt",
		now(),
	); err != nil {
		t.Fatal(err)
	}

	receipt, err := repo.AdvanceSessionDeletion(ctx, enqueued.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != "failed_external" || !receipt.Retryable || receipt.ErrorCode != "artifact_cleanup_pending" {
		t.Fatalf("artifact cleanup receipt = %+v", receipt)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sessions WHERE id = ?`, enqueued.SessionID); got != 1 {
		t.Fatalf("artifact cleanup failure deleted session rows = %d, want 1", got)
	}
}

func TestAdvanceSessionDeletionFailsClosedAfterExecutionDrainLeaseExpires(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Lease expiry", "withdraw after lease")
	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `
		UPDATE session_deletions
		SET quiesce_deadline_at = '2000-01-01T00:00:00.000000000Z'
		WHERE session_id = ?
	`, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	receipt, err := repo.AdvanceSessionDeletion(ctx, enqueued.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != "failed_external" || !receipt.Retryable || receipt.ErrorCode != "execution_unreconciled" {
		t.Fatalf("expired execution receipt = %+v", receipt)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sessions WHERE id = ?`, enqueued.SessionID); got != 1 {
		t.Fatalf("expired execution lease deleted session rows = %d, want 1", got)
	}
}

func TestPendingSessionDeletionIDsExcludeCompletedReceipts(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Resume deletion")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}
	ids, err := repo.PendingSessionDeletionIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != session.SessionID {
		t.Fatalf("pending deletion ids = %v, want [%s]", ids, session.SessionID)
	}
	if _, err := repo.AdvanceSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}
	ids, err = repo.PendingSessionDeletionIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("completed deletion ids = %v, want none", ids)
	}
}

func TestDeleteSessionRemovesEverythingItProduced(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Delete me", "remember the passphrase hunter2")
	finishRun(t, repo, enqueued.RunID)
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, enqueued.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO automation_runs (run_id, automation_id, automation_name, allowed_tools_json, fired_at)
		VALUES (?, 'automation-delete', 'Delete automation run', '[]', datetime('now'))
	`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteSession(ctx, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	for _, check := range []struct {
		table string
		query string
	}{
		{"sessions", `SELECT COUNT(*) FROM sessions WHERE id = ?`},
		{"messages", `SELECT COUNT(*) FROM messages WHERE session_id = ?`},
		{"agent_runs", `SELECT COUNT(*) FROM agent_runs WHERE session_id = ?`},
		{"events", `SELECT COUNT(*) FROM events WHERE session_id = ?`},
		{"session_external_agent", `SELECT COUNT(*) FROM session_external_agent WHERE session_id = ?`},
	} {
		if got := countRows(t, repo, check.query, enqueued.SessionID); got != 0 {
			t.Fatalf("%s still has %d rows for the deleted session", check.table, got)
		}
	}
	// These hang off agent_runs rather than sessions, so they prove the cascade
	// reaches a second level.
	for _, check := range []struct {
		table string
		query string
	}{
		{"jobs", `SELECT COUNT(*) FROM jobs WHERE run_id = ?`},
		{"tool_calls", `SELECT COUNT(*) FROM tool_calls WHERE run_id = ?`},
		{"approvals", `SELECT COUNT(*) FROM approvals WHERE run_id = ?`},
		{"automation_runs", `SELECT COUNT(*) FROM automation_runs WHERE run_id = ?`},
	} {
		if got := countRows(t, repo, check.query, enqueued.RunID); got != 0 {
			t.Fatalf("%s still has %d rows for the deleted run", check.table, got)
		}
	}
}

// The canary for soft deletion. A `deleted_at` flag would never fire the
// messages_fts AFTER DELETE trigger, so cross-session recall would keep
// surfacing content the user erased.
func TestDeleteSessionRemovesMessagesFromTheSearchIndex(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Delete me", "remember the passphrase hunter2")
	finishRun(t, repo, enqueued.RunID)

	found, err := repo.SearchMessages(ctx, "", "", "hunter2", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("precondition failed: the seeded message is not searchable")
	}

	if err := repo.DeleteSession(ctx, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	found, err = repo.SearchMessages(ctx, "", "", "hunter2", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("deleted content is still searchable: %+v", found)
	}

	// SearchMessages inner-joins messages, so it returns nothing once those rows
	// are gone whether or not messages_fts_ad fired. Query the index directly —
	// this is the assertion that actually catches a soft-delete or a dropped
	// trigger leaving deleted text in the index.
	if got := countRows(t, repo, `SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH ?`, "hunter2"); got != 0 {
		t.Fatalf("deleted content still occupies %d row(s) in the FTS index", got)
	}
}

func TestDeleteSessionKeepsAuditRowButScrubsItsPayload(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Delete me", "remember the passphrase hunter2")
	finishRun(t, repo, enqueued.RunID)

	if err := repo.DeleteSession(ctx, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	var action, actorType, target, payload string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT action, actor_type, COALESCE(target, ''), COALESCE(payload_json, '')
		FROM audit_logs WHERE correlation_id = ? AND action = 'tool.call.before'
	`, enqueued.RunID).Scan(&action, &actorType, &target, &payload); err != nil {
		t.Fatalf("audit row did not survive deletion: %v", err)
	}
	// Shape survives so the record is still evidence something happened.
	if action != "tool.call.before" || actorType != "runtime" || target != "call_"+enqueued.RunID {
		t.Fatalf("audit row lost its shape: action=%q actor=%q target=%q", action, actorType, target)
	}
	// Content does not.
	if payload == "" {
		t.Fatal("payload was nulled; want a tombstone so a reader can tell scrubbed from never-had-one")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("scrubbed payload %q is not valid JSON: %v", payload, err)
	}
	if decoded["scrubbed"] != true {
		t.Fatalf("payload = %q, want a scrubbed tombstone", payload)
	}
	if _, leaked := decoded["args"]; leaked {
		t.Fatalf("payload still carries user content: %q", payload)
	}

	// Deletion is itself an auditable action, and the row says how much went —
	// "something was deleted" is far weaker evidence than a count.
	var deletedTarget, deletedPayload string
	var deletedCorrelation any
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(target, ''), COALESCE(payload_json, ''), correlation_id
		FROM audit_logs WHERE action = 'session.deleted'
	`).Scan(&deletedTarget, &deletedPayload, &deletedCorrelation); err != nil {
		t.Fatalf("session.deleted audit row missing: %v", err)
	}
	if deletedTarget != enqueued.SessionID {
		t.Fatalf("session.deleted target = %q, want the session id", deletedTarget)
	}
	// correlation_id means "run id" at every other call site; reusing it for a
	// session id would conflate two kinds under one index.
	if deletedCorrelation != nil {
		t.Fatalf("session.deleted correlation_id = %v, want NULL", deletedCorrelation)
	}
	var counts struct {
		Runs     int `json:"runs"`
		Messages int `json:"messages"`
	}
	if err := json.Unmarshal([]byte(deletedPayload), &counts); err != nil {
		t.Fatalf("session.deleted payload %q: %v", deletedPayload, err)
	}
	if counts.Runs != 1 || counts.Messages == 0 {
		t.Fatalf("session.deleted counts = %+v, want 1 run and some messages", counts)
	}
}

// routeAndUnrouteSession points a conversation at a third party and then
// returns it to the local assistant, which is the only way to produce a real
// session.routed / session.unrouted pair. Both rows carry the session id as
// target and a NULL correlation_id, so neither is reachable from the run-id
// scrub — which is exactly what the deletion test below has to prove is no
// longer true.
func routeAndUnrouteSession(t *testing.T, repo *Repository, sessionID string, agent ExternalAgentInput) {
	t.Helper()
	ctx := context.Background()
	created := mustCreateAgent(t, ctx, repo, agent)
	if _, err := repo.SetSessionAgent(ctx, sessionID, created.AgentID); err != nil {
		t.Fatalf("route %s: %v", sessionID, err)
	}
	if err := repo.ClearSessionAgent(ctx, sessionID); err != nil {
		t.Fatalf("unroute %s: %v", sessionID, err)
	}
}

func routingAuditPayload(t *testing.T, repo *Repository, sessionID, action string) string {
	t.Helper()
	var payload string
	if err := repo.db.QueryRowContext(context.Background(), `
		SELECT COALESCE(payload_json, '') FROM audit_logs WHERE action = ? AND target = ?
	`, action, sessionID).Scan(&payload); err != nil {
		t.Fatalf("%s row for %s: %v", action, sessionID, err)
	}
	return payload
}

// Routing rows are the one audit shape whose sensitive content is not reachable
// from a run id: SetSessionAgent / ClearSessionAgent write correlation_id NULL
// and put the session id in target. Deleting the conversation therefore has to
// scrub them by target as well, or the third-party endpoint, model, and agent
// display name a user asked to forget outlive the session they belong to.
func TestDeleteSessionScrubsRoutingAuditRowsTargetingThatSession(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	const sentinelAgent = "SENTINEL-AGENT-a41f7c-do-not-leak"
	const sentinelHost = "sentinel-endpoint-a41f7c-do-not-leak.example.com"
	const sentinelModel = "sentinel-model-a41f7c-do-not-leak"

	enqueued := seedDeletableSession(t, repo, "Delete me", "routed content")
	doomedAgent := anthropicAgent()
	doomedAgent.DisplayName = sentinelAgent
	doomedAgent.BaseURL = "https://" + sentinelHost + "/v1"
	doomedAgent.Model = sentinelModel
	routeAndUnrouteSession(t, repo, enqueued.SessionID, doomedAgent)

	// A second conversation routed to its own destination: the scrub must be
	// targeted, not a blanket wipe of every routing row in the table.
	survivor := seedDeletableSession(t, repo, "Keep me", "surviving content")
	survivorAgent := anthropicAgent()
	survivorAgent.DisplayName = "Survivor Agent"
	survivorAgent.BaseURL = "https://survivor.example.com/v1"
	survivorAgent.Model = "survivor-model"
	survivorAgent.CredentialRef = "survivor"
	routeAndUnrouteSession(t, repo, survivor.SessionID, survivorAgent)

	// Precondition: the sensitive routing payload really is stored before the
	// delete, so a passing assertion afterwards is the scrub and not a fixture
	// that never carried the sentinel.
	before := routingAuditPayload(t, repo, enqueued.SessionID, "session.routed")
	for _, sentinel := range []string{sentinelAgent, sentinelHost, sentinelModel} {
		if !strings.Contains(before, sentinel) {
			t.Fatalf("precondition failed: session.routed payload %q is missing sentinel %q", before, sentinel)
		}
	}

	finishRun(t, repo, enqueued.RunID)
	if err := repo.DeleteSession(ctx, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	for _, action := range []string{"session.routed", "session.unrouted"} {
		got := routingAuditPayload(t, repo, enqueued.SessionID, action)
		if got != scrubbedAuditPayload {
			t.Fatalf("%s payload after delete = %q, want the exact tombstone %q", action, got, scrubbedAuditPayload)
		}
	}

	// The deletion's own row is inserted after the scrub and is the evidence the
	// deletion happened, so it must stay PRESENT with its counts.
	deleted := routingAuditPayload(t, repo, enqueued.SessionID, "session.deleted")
	if deleted == scrubbedAuditPayload || deleted == "" {
		t.Fatalf("session.deleted payload = %q, want the unscrubbed counts", deleted)
	}
	var counts struct {
		Runs     int `json:"runs"`
		Messages int `json:"messages"`
	}
	if err := json.Unmarshal([]byte(deleted), &counts); err != nil {
		t.Fatalf("session.deleted payload %q: %v", deleted, err)
	}
	if counts.Runs != 1 || counts.Messages == 0 {
		t.Fatalf("session.deleted counts = %+v, want 1 run and some messages", counts)
	}

	survivorRouted := routingAuditPayload(t, repo, survivor.SessionID, "session.routed")
	if survivorRouted == scrubbedAuditPayload || !strings.Contains(survivorRouted, "survivor.example.com") {
		t.Fatalf("an unrelated session's routing audit was scrubbed: %q", survivorRouted)
	}
}

func TestDeleteSessionAuditScrubUsesOwnershipIndexes(t *testing.T) {
	repo := New(openTestDB(t))
	rows, err := repo.db.QueryContext(
		context.Background(),
		"EXPLAIN QUERY PLAN\n"+scrubSessionAuditPayloadsSQL,
		scrubbedAuditPayload,
		"session_query_plan",
		"session_query_plan",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()

	var usesCorrelationIndex, usesTargetIndex bool
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "SCAN audit_logs") {
			t.Fatalf("audit scrub performs an unbounded table scan: %s", detail)
		}
		usesCorrelationIndex = usesCorrelationIndex || strings.Contains(detail, "idx_audit_correlation")
		usesTargetIndex = usesTargetIndex || strings.Contains(detail, "idx_audit_target")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !usesCorrelationIndex || !usesTargetIndex {
		t.Fatalf(
			"audit scrub plan uses correlation index=%t target index=%t, want both",
			usesCorrelationIndex,
			usesTargetIndex,
		)
	}
}

func TestDeleteSessionScrubsSessionTargetedRoutingAudit(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Delete routed session")
	if err != nil {
		t.Fatal(err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteSession(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}

	var payload string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(payload_json, '')
		FROM audit_logs
		WHERE action = 'session.routed' AND target = ?
	`, session.SessionID).Scan(&payload); err != nil {
		t.Fatalf("routing audit row did not survive deletion: %v", err)
	}
	if payload != scrubbedAuditPayload {
		t.Fatalf("routing audit payload = %q, want %q", payload, scrubbedAuditPayload)
	}
}

func TestDeleteSessionScrubsSessionTargetedUnroutingAudit(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Delete unrouted session")
	if err != nil {
		t.Fatal(err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClearSessionAgent(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}

	var payloadBefore string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(payload_json, '')
		FROM audit_logs
		WHERE action = 'session.unrouted' AND target = ?
	`, session.SessionID).Scan(&payloadBefore); err != nil {
		t.Fatal(err)
	}
	if payloadBefore != "{}" {
		t.Fatalf("unrouting audit payload before deletion = %q, want %q", payloadBefore, "{}")
	}

	if err := repo.DeleteSession(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}

	var payloadAfter string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(payload_json, '')
		FROM audit_logs
		WHERE action = 'session.unrouted' AND target = ?
	`, session.SessionID).Scan(&payloadAfter); err != nil {
		t.Fatalf("unrouting audit row did not survive deletion: %v", err)
	}
	if payloadAfter != scrubbedAuditPayload {
		t.Fatalf("unrouting audit payload after deletion = %q, want %q", payloadAfter, scrubbedAuditPayload)
	}
}

func TestDeleteSessionScrubsUncorrelatedSessionTargetedAuditActionsByDefault(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Delete future session action")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordAudit(
		ctx,
		"",
		"client",
		"",
		"conversation.renamed",
		session.SessionID,
		`{"title":"private title"}`,
	); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteSession(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}

	var payload string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(payload_json, '')
		FROM audit_logs
		WHERE action = 'conversation.renamed' AND target = ?
	`, session.SessionID).Scan(&payload); err != nil {
		t.Fatalf("session-targeted audit row did not survive deletion: %v", err)
	}
	if payload != scrubbedAuditPayload {
		t.Fatalf("session-targeted audit payload = %q, want %q", payload, scrubbedAuditPayload)
	}
}

func TestDeleteSessionScrubsLegacyEmptyCorrelationAudit(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Delete legacy audit session")
	if err != nil {
		t.Fatal(err)
	}
	const payloadBefore = `{"title":"legacy private title"}`
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO audit_logs (
			id, correlation_id, actor_type, action, target, payload_json, created_at
		)
		VALUES (
			'audit_legacy_empty_correlation', '', 'client', 'conversation.renamed', ?, ?, datetime('now')
		)
	`, session.SessionID, payloadBefore); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteSession(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}

	var payloadAfter string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(payload_json, '')
		FROM audit_logs
		WHERE id = 'audit_legacy_empty_correlation'
	`).Scan(&payloadAfter); err != nil {
		t.Fatal(err)
	}
	if payloadAfter != scrubbedAuditPayload {
		t.Fatalf("legacy empty-correlation payload after deletion = %q, want %q", payloadAfter, scrubbedAuditPayload)
	}
}

// The scrub is correlated, not global: another session's audit must be intact.
func TestDeleteSessionLeavesOtherSessionsAuditIntact(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	doomed := seedDeletableSession(t, repo, "Delete me", "doomed content")
	survivor := seedDeletableSession(t, repo, "Keep me", "surviving content")
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, doomed.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSessionAgent(ctx, survivor.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	var routingPayloadBefore string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(payload_json, '')
		FROM audit_logs
		WHERE action = 'session.routed' AND target = ?
	`, survivor.SessionID).Scan(&routingPayloadBefore); err != nil {
		t.Fatal(err)
	}
	const unrelatedPayload = `{"scope":"surviving run"}`
	if err := repo.RecordAudit(
		ctx,
		survivor.RunID,
		"runtime",
		"",
		"tool.call.shared-target",
		doomed.SessionID,
		unrelatedPayload,
	); err != nil {
		t.Fatal(err)
	}
	finishRun(t, repo, doomed.RunID)

	if err := repo.DeleteSession(ctx, doomed.SessionID); err != nil {
		t.Fatal(err)
	}

	var payload string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(payload_json, '') FROM audit_logs WHERE correlation_id = ? AND action = 'tool.call.before'
	`, survivor.RunID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatal(err)
	}
	if _, present := decoded["args"]; !present {
		t.Fatalf("an unrelated session's audit payload was scrubbed: %q", payload)
	}

	var routingPayloadAfter string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(payload_json, '')
		FROM audit_logs
		WHERE action = 'session.routed' AND target = ?
	`, survivor.SessionID).Scan(&routingPayloadAfter); err != nil {
		t.Fatal(err)
	}
	if routingPayloadAfter != routingPayloadBefore {
		t.Fatalf(
			"an unrelated session's routing audit payload changed from %q to %q",
			routingPayloadBefore,
			routingPayloadAfter,
		)
	}

	var unrelatedPayloadAfter string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(payload_json, '')
		FROM audit_logs
		WHERE action = 'tool.call.shared-target' AND correlation_id = ?
	`, survivor.RunID).Scan(&unrelatedPayloadAfter); err != nil {
		t.Fatal(err)
	}
	if unrelatedPayloadAfter != unrelatedPayload {
		t.Fatalf(
			"an unrelated audit payload sharing the deleted session target changed from %q to %q",
			unrelatedPayload,
			unrelatedPayloadAfter,
		)
	}
}

func TestDeleteSessionRejectsUnknownID(t *testing.T) {
	repo := New(openTestDB(t))
	err := repo.DeleteSession(context.Background(), "sess_does_not_exist")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("DeleteSession(unknown) = %v, want ErrSessionNotFound", err)
	}
}

// Deleting rows out from under a worker mid-execution would leave the runtime
// finishing a run whose rows no longer exist. Refuse, and mutate nothing.
func TestDeleteSessionRefusesWhileARunIsLive(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Busy", "in flight")

	err := repo.DeleteSession(ctx, enqueued.SessionID)
	if !errors.Is(err, ErrSessionHasActiveRun) {
		t.Fatalf("DeleteSession(live run) = %v, want ErrSessionHasActiveRun", err)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sessions WHERE id = ?`, enqueued.SessionID); got != 1 {
		t.Fatal("refused deletion still removed the session")
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM messages WHERE session_id = ?`, enqueued.SessionID); got == 0 {
		t.Fatal("refused deletion still removed messages")
	}
	// And the audit payload must not have been scrubbed on the refusal path.
	var payload string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(payload_json, '') FROM audit_logs WHERE correlation_id = ? AND action = 'tool.call.before'
	`, enqueued.RunID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload == "" || payload == `{"scrubbed":true}` {
		t.Fatalf("refused deletion scrubbed audit anyway: %q", payload)
	}
}

// A run can be terminally failed while its execution is still live:
// failRunWithEventTx(preserveExecution=true) sets status='failed' but leaves
// execution_active = 1 and execution_state = 'uncertain' (runs.go), and the
// recovery machinery still queries those rows (assignments.go). Status alone is
// therefore not enough to decide the run is finished — deleting here would
// cascade rows out from under a worker that has not acknowledged exit.
func TestDeleteSessionRefusesWhileExecutionIsStillActive(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Uncertain", "still executing")
	finishRun(t, repo, enqueued.RunID)

	// Terminal status, but the worker never acknowledged exit.
	if _, err := database.ExecContext(ctx, `
		UPDATE agent_runs
		SET status = 'failed', execution_active = 1, execution_state = 'uncertain'
		WHERE id = ?
	`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	err := repo.DeleteSession(ctx, enqueued.SessionID)
	if !errors.Is(err, ErrSessionHasActiveRun) {
		t.Fatalf("DeleteSession(uncertain execution) = %v, want ErrSessionHasActiveRun", err)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sessions WHERE id = ?`, enqueued.SessionID); got != 1 {
		t.Fatal("refused deletion still removed the session")
	}
}

// CancelRun is the user-reachable variant of the same hazard: it sets
// status='cancelled' but never touches execution_active (runs.go), so the
// worker may still be executing. Deleting here previously tore down the whole
// ConnectWorker stream, taking unrelated runs with it.
func TestDeleteSessionRefusesAfterCancelLeavesExecutionActive(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Cancelled", "cancel me")

	if _, err := cancelRunAtCurrentVersion(t, repo, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	var status string
	var active int
	if err := database.QueryRowContext(ctx,
		`SELECT status, execution_active FROM agent_runs WHERE id = ?`, enqueued.RunID,
	).Scan(&status, &active); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || active != 1 {
		t.Fatalf("precondition: run = status:%q execution_active:%d, want cancelled with execution still active", status, active)
	}

	if err := repo.DeleteSession(ctx, enqueued.SessionID); !errors.Is(err, ErrSessionHasActiveRun) {
		t.Fatalf("DeleteSession(cancelled, execution live) = %v, want ErrSessionHasActiveRun", err)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sessions WHERE id = ?`, enqueued.SessionID); got != 1 {
		t.Fatal("refused deletion still removed the session")
	}
}
