package db

import (
	"context"
	"strings"
	"testing"
)

// A child's deletion receipt names its parent and keeps naming it after both
// session rows are gone: the column has no foreign key, so deleting either
// session neither removes the receipt nor fails.
func TestSessionDeletionReceiptsOutliveTheSessionsTheyName(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	seedDelegationRun(t, ctx, database, "sess_parent", "", "chat")
	seedDelegationRun(t, ctx, database, "sess_child", "sess_parent", "delegation")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO session_deletions (session_id, lifecycle_version, state, quiesce_deadline_at, terminal_sequence,
			retryable, run_count, message_count, parent_session_id)
		VALUES ('sess_child', 1, 'quiescing', '2026-10-05T00:00:00.000000000Z', 1, 1, 1, 1, 'sess_parent')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM sessions WHERE id = 'sess_parent'`); err != nil {
		t.Fatalf("deleting the parent with a child receipt naming it: %v", err)
	}
	if got := countRows(t, ctx, database, `SELECT COUNT(*) FROM session_deletions
		WHERE session_id = 'sess_child' AND parent_session_id = 'sess_parent'`); got != 1 {
		t.Fatalf("child receipts naming the deleted parent = %d, want 1", got)
	}
}

// The parent's deletion finds its children's receipts through an index.
func TestSessionDeletionsAreIndexedByParent(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	rows, err := database.QueryContext(ctx, `EXPLAIN QUERY PLAN
		SELECT session_id FROM session_deletions WHERE parent_session_id = ? AND state <> 'completed'`, "sess_parent")
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
	if joined := strings.Join(plan, "; "); !strings.Contains(joined, "idx_session_deletions_parent") {
		t.Fatalf("query plan = %q, want the parent lookup to use idx_session_deletions_parent", joined)
	}
}

// Every receipt written before the upgrade belongs to a chat session and has
// no parent.
func TestSessionDeletionParentMigrationLeavesExistingReceiptsParentless(t *testing.T) {
	ctx := context.Background()
	database := databaseBeforeMigration(t, ctx, "0027_session_deletion_parent.sql")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO session_deletions (session_id, lifecycle_version, state, quiesce_deadline_at, terminal_sequence,
			retryable, run_count, message_count)
		VALUES ('sess_before', 1, 'completed', '2026-10-05T00:00:00.000000000Z', 1, 0, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, ctx, database, `SELECT COUNT(*) FROM session_deletions
		WHERE session_id = 'sess_before' AND parent_session_id IS NULL`); got != 1 {
		t.Fatalf("pre-upgrade receipt with no parent = %d, want 1", got)
	}
}
