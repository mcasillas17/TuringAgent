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

	found, err := repo.SearchMessages(ctx, "", "hunter2", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("precondition failed: the seeded message is not searchable")
	}

	if err := repo.DeleteSession(ctx, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	found, err = repo.SearchMessages(ctx, "", "hunter2", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("deleted content is still searchable: %+v", found)
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

	// Deletion is itself an auditable action.
	if got := countRows(t, repo, `SELECT COUNT(*) FROM audit_logs WHERE action = 'session.deleted'`); got != 1 {
		t.Fatalf("session.deleted audit rows = %d, want 1", got)
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
	// ClaimNextJob in the seed already moved the run to running.

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
