package db

import (
	"context"
	"strings"
	"testing"
)

// seedDelegationRun inserts a session with one run anchored on its first
// message, as an enqueue would leave them.
func seedDelegationRun(t *testing.T, ctx context.Context, database *DB, sessionID, parentSessionID, kind string) string {
	t.Helper()
	runID := "run_" + sessionID
	messageID := "msg_" + sessionID
	var parent any
	if parentSessionID != "" {
		parent = parentSessionID
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO sessions (id, kind, parent_session_id, created_at, updated_at)
			VALUES (?, ?, ?, '2026-10-05T00:00:00.000000000Z', '2026-10-05T00:00:00.000000000Z')`,
			[]any{sessionID, kind, parent}},
		{`INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
			VALUES (?, ?, 'user', 'hello', 'text', 1, '2026-10-05T00:00:00.000000000Z')`,
			[]any{messageID, sessionID}},
		{`INSERT INTO agent_runs (
				id, session_id, user_message_id, agent_id, trace_id, status,
				model_provider, model_name, created_at, state_version, state_updated_at,
				outcome_reason, assistant_content_sha256
			) VALUES (?, ?, ?, 'general_assistant', 'trace', 'queued', 'ollama', 'llama3.2',
				'2026-10-05T00:00:00.000000000Z', 1, '2026-10-05T00:00:00.000000000Z', 'none', ?)`,
			[]any{runID, sessionID, messageID, strings.Repeat("0", 64)}},
	} {
		if _, err := database.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	return runID
}

func insertDelegation(ctx context.Context, database *DB, id, parentSession, parentRun, toolCall, childSession, childRun string) error {
	_, err := database.ExecContext(ctx, `
		INSERT INTO delegations (
			id, parent_session_id, parent_run_id, parent_tool_call_id, child_session_id, child_run_id,
			profile_id, profile_revision, args_hash, created_at
		) VALUES (?, ?, ?, ?, ?, ?, 'research', 'rev-1', 'sha256:x', '2026-10-05T00:00:00.000000000Z')`,
		id, parentSession, parentRun, toolCall, childSession, childRun)
	return err
}

func countRows(t *testing.T, ctx context.Context, database *DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// One tool call creates at most one delegation, and each child session and
// run belongs to exactly one.
func TestADelegationIsUniquePerToolCall(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	parentRun := seedDelegationRun(t, ctx, database, "sess_parent", "", "chat")
	firstChild := seedDelegationRun(t, ctx, database, "sess_child_1", "sess_parent", "delegation")
	secondChild := seedDelegationRun(t, ctx, database, "sess_child_2", "sess_parent", "delegation")

	if err := insertDelegation(ctx, database, "dlg_1", "sess_parent", parentRun, "call_1", "sess_child_1", firstChild); err != nil {
		t.Fatal(err)
	}
	if err := insertDelegation(ctx, database, "dlg_2", "sess_parent", parentRun, "call_1", "sess_child_2", secondChild); err == nil {
		t.Fatal("a second delegation for the same tool call was accepted")
	}
	if err := insertDelegation(ctx, database, "dlg_3", "sess_parent", parentRun, "call_2", "sess_child_1", secondChild); err == nil {
		t.Fatal("a child session was shared by two delegations")
	}
	thirdChild := seedDelegationRun(t, ctx, database, "sess_child_3", "sess_parent", "delegation")
	_ = thirdChild
	if err := insertDelegation(ctx, database, "dlg_5", "sess_parent", parentRun, "call_4", "sess_child_3", firstChild); err == nil {
		t.Fatal("a child run was shared by two delegations")
	}
	if err := insertDelegation(ctx, database, "dlg_4", "sess_parent", parentRun, "call_3", "sess_child_2", secondChild); err != nil {
		t.Fatalf("a second tool call was refused: %v", err)
	}
}

// Deleting any session looks up its children by parent, so the lookup is
// indexed rather than a scan of every session.
func TestSessionsAreIndexedByParent(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	rows, err := database.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT id FROM sessions WHERE parent_session_id = ?`, "sess_parent")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if joined := strings.Join(plan, "; "); !strings.Contains(joined, "USING COVERING INDEX idx_sessions_parent") &&
		!strings.Contains(joined, "USING INDEX idx_sessions_parent") {
		t.Fatalf("query plan = %q, want the parent lookup to use idx_sessions_parent", joined)
	}
}

// The foreign keys are a backstop: deleting the parent session, or the
// parent run, leaves no delegation pointing at a deleted row and raises no
// foreign-key error. A parent session's children go with it.
func TestDeletingAParentLeavesNoOrphanDelegation(t *testing.T) {
	for _, test := range []struct {
		name      string
		delete    string
		children  int
		delegated int
	}{
		{"the parent session", `DELETE FROM sessions WHERE id = 'sess_parent'`, 0, 0},
		{"the parent run", `DELETE FROM agent_runs WHERE id = 'run_sess_parent'`, 1, 0},
		{"the child session", `DELETE FROM sessions WHERE id = 'sess_child'`, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			database := openMigratedInvariantDB(t, ctx)
			parentRun := seedDelegationRun(t, ctx, database, "sess_parent", "", "chat")
			childRun := seedDelegationRun(t, ctx, database, "sess_child", "sess_parent", "delegation")
			if err := insertDelegation(ctx, database, "dlg_1", "sess_parent", parentRun, "call_1", "sess_child", childRun); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(ctx, test.delete); err != nil {
				t.Fatalf("delete: %v", err)
			}
			if got := countRows(t, ctx, database, `SELECT COUNT(*) FROM sessions WHERE kind = 'delegation'`); got != test.children {
				t.Fatalf("child sessions = %d, want %d", got, test.children)
			}
			if got := countRows(t, ctx, database, `SELECT COUNT(*) FROM delegations`); got != test.delegated {
				t.Fatalf("delegations = %d, want %d", got, test.delegated)
			}
		})
	}
}

// Every session that existed before the upgrade has no parent.
func TestDelegationsMigrationLeavesExistingSessionsParentless(t *testing.T) {
	ctx := context.Background()
	database := databaseBeforeMigration(t, ctx, "0026_delegations.sql")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, created_at, updated_at)
		VALUES ('sess_before', '2026-10-05T00:00:00.000000000Z', '2026-10-05T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, ctx, database, `SELECT COUNT(*) FROM sessions WHERE id = 'sess_before' AND parent_session_id IS NULL`); got != 1 {
		t.Fatalf("pre-upgrade session with no parent = %d, want 1", got)
	}
}
