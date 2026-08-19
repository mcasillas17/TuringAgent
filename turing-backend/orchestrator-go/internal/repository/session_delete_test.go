package repository

import (
	"context"
	"encoding/json"
	"errors"
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

func TestDeleteSessionRemovesEverythingItProduced(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedDeletableSession(t, repo, "Delete me", "remember the passphrase hunter2")
	finishRun(t, repo, enqueued.RunID)

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

// The scrub is correlated, not global: another session's audit must be intact.
func TestDeleteSessionLeavesOtherSessionsAuditIntact(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	doomed := seedDeletableSession(t, repo, "Delete me", "doomed content")
	survivor := seedDeletableSession(t, repo, "Keep me", "surviving content")
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

	if err := repo.CancelRun(ctx, enqueued.RunID, "user cancelled"); err != nil {
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
