package db

import (
	"context"
	"testing"
	"time"
)

// A run that was already queued when 0020 arrived has been waiting since it was
// created, and the bound has to know that. Backfilling nothing would hand every
// pre-existing queued run a fresh, unbounded wait the moment the feature that
// bounds waiting shipped — and leaving the column null would make the accrued
// interval zero for as long as the run stayed queued.
func TestQueueWaitMigrationBackfillsTheOpenIntervalForQueuedRuns(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name == "0020_queue_wait.sql" {
			break
		}
		applyMigration(t, ctx, database, name)
	}

	const queuedAt = "2026-08-20T01:02:03.500000000Z"
	const terminalAt = "2026-08-20T01:02:04.000000000Z"
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, title, status, created_at, updated_at)
		VALUES ('sess_queue_wait', 'Queue wait', 'active', ?, ?);
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES
			('msg_qw_user', 'sess_queue_wait', 'user', 'waiting', 'text', 1, ?),
			('msg_qw_assistant', 'sess_queue_wait', 'assistant', '', 'text', 2, ?),
			('msg_qw_done_user', 'sess_queue_wait', 'user', 'done', 'text', 3, ?),
			('msg_qw_done_assistant', 'sess_queue_wait', 'assistant', 'answer', 'text', 4, ?);
		INSERT INTO agent_runs (
			id, session_id, user_message_id, assistant_message_id, agent_id,
			trace_id, status, model_provider, model_name, created_at,
			state_version, state_updated_at, outcome_reason, assistant_content_sha256
		) VALUES (
			'run_qw_queued', 'sess_queue_wait', 'msg_qw_user', 'msg_qw_assistant',
			'general_assistant', 'trace_qw_queued', 'queued', 'ollama', 'llama3.2', ?,
			1, ?, 'none', 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
		), (
			'run_qw_done', 'sess_queue_wait', 'msg_qw_done_user', 'msg_qw_done_assistant',
			'general_assistant', 'trace_qw_done', 'completed', 'ollama', 'llama3.2', ?,
			2, ?, 'none', 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
		);
	`, queuedAt, queuedAt, queuedAt, queuedAt, queuedAt, queuedAt, queuedAt, queuedAt, terminalAt, terminalAt); err != nil {
		t.Fatal(err)
	}

	applyMigration(t, ctx, database, "0020_queue_wait.sql")

	var queuedSince *int64
	var waited int64
	var reason string
	if err := database.QueryRowContext(ctx, `
		SELECT queued_since_ns, queue_waited_ns, queue_wait_reason
		FROM agent_runs WHERE id = 'run_qw_queued'
	`).Scan(&queuedSince, &waited, &reason); err != nil {
		t.Fatal(err)
	}
	want, err := time.Parse(time.RFC3339Nano, queuedAt)
	if err != nil {
		t.Fatal(err)
	}
	if queuedSince == nil || *queuedSince != want.UnixNano() {
		t.Fatalf("backfilled queued_since_ns = %v, want %d", queuedSince, want.UnixNano())
	}
	if waited != 0 {
		t.Fatalf("backfilled queue_waited_ns = %d, want 0 — nothing was banked before this migration", waited)
	}
	if reason != "none" {
		t.Fatalf("backfilled queue_wait_reason = %q, want none until an observer says otherwise", reason)
	}

	// A run that is not queued has no open interval to backfill; giving it one
	// would make a completed run look like it is still waiting.
	var terminalSince *int64
	if err := database.QueryRowContext(ctx,
		`SELECT queued_since_ns FROM agent_runs WHERE id = 'run_qw_done'`).Scan(&terminalSince); err != nil {
		t.Fatal(err)
	}
	if terminalSince != nil {
		t.Fatalf("a completed run was backfilled with an open queued interval: %v", terminalSince)
	}

	// The CHECK is the schema's half of the closed vocabulary.
	if _, err := database.ExecContext(ctx,
		`UPDATE agent_runs SET queue_wait_reason = 'whatever' WHERE id = 'run_qw_queued'`); err == nil {
		t.Fatal("an unknown queue wait reason was accepted")
	}
}
