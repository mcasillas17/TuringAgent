package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// seedQueuedRun writes one run anchored on its first message in sessionID,
// as an enqueue would leave it, with its job in jobStatus.
func seedQueuedRun(t *testing.T, ctx context.Context, database *db.DB, sessionID, runID, jobStatus string, sequence int) {
	t.Helper()
	const at = "2026-11-01T00:00:00.000000000Z"
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT OR IGNORE INTO sessions (id, created_at, updated_at) VALUES (?, ?, ?)`, []any{sessionID, at, at}},
		{`INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
			VALUES (?, ?, 'user', 'hello', 'text', ?, ?)`, []any{"msg_" + runID, sessionID, sequence, at}},
		{`INSERT INTO agent_runs (id, session_id, user_message_id, agent_id, trace_id, status, model_provider,
				model_name, created_at, state_version, state_updated_at, outcome_reason, assistant_content_sha256)
			VALUES (?, ?, ?, 'general_assistant', 'trace', 'queued', 'ollama', 'llama3.2', ?, 1, ?, 'none', ?)`,
			[]any{runID, sessionID, "msg_" + runID, at, at, strings.Repeat("0", 64)}},
		{`INSERT INTO jobs (id, run_id, agent_id, status, payload_json, created_at, created_at_ns)
			VALUES (?, ?, 'general_assistant', ?, '{}', ?, ?)`, []any{"job_" + runID, runID, jobStatus, at, sequence}},
	} {
		if _, err := database.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
}

// TUR-027: the migration marks every job an automation already queued,
// whatever its state, so for history too the flag is what the one
// definition gives; an owner's turn, in an automation's conversation or not,
// stays attended.
func TestAttendedClaimMigrationBackfillsTheUnattendedFlag(t *testing.T) {
	ctx := context.Background()
	database := db.DatabaseBeforeMigration(t, ctx, "0032_attended_claim.sql")
	seedQueuedRun(t, ctx, database, "sess_owner", "run_owner", "pending", 1)
	seedQueuedRun(t, ctx, database, "sess_digest", "run_digest_done", "completed", 1)
	seedQueuedRun(t, ctx, database, "sess_digest", "run_digest", "pending", 2)
	seedQueuedRun(t, ctx, database, "sess_digest", "run_reply", "pending", 3)
	for _, runID := range []string{"run_digest_done", "run_digest"} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO automation_runs (run_id, automation_id, automation_name, allowed_tools_json, fired_at)
			VALUES (?, 'auto_digest', 'Digest', '[]', '2026-11-01T00:00:00Z')`, runID); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}

	repo := repository.New(database)
	for runID, want := range map[string]bool{
		"run_owner": false, "run_digest_done": true, "run_digest": true, "run_reply": false,
	} {
		var flag bool
		if err := database.QueryRowContext(ctx, `SELECT unattended FROM jobs WHERE run_id = ?`, runID).Scan(&flag); err != nil {
			t.Fatal(err)
		}
		unattended, err := repo.RunUnattended(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		if flag != unattended || flag != want {
			t.Fatalf("run %s: jobs.unattended = %v, RunUnattended = %v; want both %v", runID, flag, unattended, want)
		}
	}
}
