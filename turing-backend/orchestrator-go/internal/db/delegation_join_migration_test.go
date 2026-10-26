package db

import (
	"context"
	"strings"
	"testing"
)

// A run continues at most one other run: a second continuation of the same
// run is refused by the database itself, whatever the code that inserts it
// believes.
func TestARunHasAtMostOneContinuation(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	parent := seedDelegationRun(t, ctx, database, "sess_parent", "", "chat")
	first := seedDelegationRun(t, ctx, database, "sess_first", "", "chat")
	second := seedDelegationRun(t, ctx, database, "sess_second", "", "chat")
	if _, err := database.ExecContext(ctx, `UPDATE agent_runs SET continues_run_id = ? WHERE id = ?`, parent, first); err != nil {
		t.Fatalf("first continuation: %v", err)
	}
	_, err := database.ExecContext(ctx, `UPDATE agent_runs SET continues_run_id = ? WHERE id = ?`, parent, second)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("second continuation of the same run: err = %v, want a uniqueness failure", err)
	}
}

// A continuation goes with the run it continues.
func TestAContinuationIsDeletedWithTheRunItContinues(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	parent := seedDelegationRun(t, ctx, database, "sess_parent", "", "chat")
	continuation := seedDelegationRun(t, ctx, database, "sess_continuation", "", "chat")
	if _, err := database.ExecContext(ctx, `UPDATE agent_runs SET continues_run_id = ? WHERE id = ?`, parent, continuation); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM agent_runs WHERE id = ?`, parent); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, ctx, database, `SELECT COUNT(*) FROM agent_runs WHERE id = ?`, continuation); got != 0 {
		t.Fatalf("continuations left after their run was deleted = %d, want 0", got)
	}
}

// Every run that existed before the upgrade continues nothing.
func TestDelegationJoinMigrationLeavesExistingRunsUncontinued(t *testing.T) {
	ctx := context.Background()
	database := databaseBeforeMigration(t, ctx, "0028_delegation_join.sql")
	run := seedDelegationRun(t, ctx, database, "sess_before", "", "chat")
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, ctx, database, `SELECT COUNT(*) FROM agent_runs WHERE id = ? AND continues_run_id IS NULL`, run); got != 1 {
		t.Fatalf("pre-upgrade runs continuing nothing = %d, want 1", got)
	}
}

// A delegation records the task it was asked to do, so a card rebuilt after a
// restart says what the specialist is working on. Delegations made before the
// column existed read empty.
func TestDelegationTaskMigrationLeavesExistingDelegationsEmpty(t *testing.T) {
	ctx := context.Background()
	database := databaseBeforeMigration(t, ctx, "0029_delegation_task.sql")
	parentRun := seedDelegationRun(t, ctx, database, "sess_parent", "", "chat")
	childRun := seedDelegationRun(t, ctx, database, "sess_child", "sess_parent", "delegation")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO delegations (id, parent_session_id, parent_run_id, parent_tool_call_id, child_session_id, child_run_id,
			profile_id, profile_revision, args_hash, created_at)
		VALUES ('dlg_before', 'sess_parent', ?, 'call_1', 'sess_child', ?, 'research', 'rev-1', 'sha256:x',
			'2026-10-05T00:00:00.000000000Z')`, parentRun, childRun); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, ctx, database, `SELECT COUNT(*) FROM delegations WHERE id = 'dlg_before' AND task = ''`); got != 1 {
		t.Fatalf("pre-upgrade delegations with an empty task = %d, want 1", got)
	}
}

// Delegations made before child egress consent needed none: they read as
// asking for no tools that leave the machine, with no consent pending, and the
// consent column takes only its four states.
func TestDelegationEgressMigrationLeavesExistingDelegationsWithoutConsent(t *testing.T) {
	ctx := context.Background()
	database := databaseBeforeMigration(t, ctx, "0030_delegation_egress.sql")
	parentRun := seedDelegationRun(t, ctx, database, "sess_parent", "", "chat")
	childRun := seedDelegationRun(t, ctx, database, "sess_child", "sess_parent", "delegation")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO delegations (id, parent_session_id, parent_run_id, parent_tool_call_id, child_session_id, child_run_id,
			profile_id, profile_revision, args_hash, created_at)
		VALUES ('dlg_before', 'sess_parent', ?, 'call_1', 'sess_child', ?, 'research', 'rev-1', 'sha256:x',
			'2026-10-05T00:00:00.000000000Z')`, parentRun, childRun); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, ctx, database, `SELECT COUNT(*) FROM delegations
		WHERE id = 'dlg_before' AND egress_tools_json = '[]' AND egress_consent = '' AND egress_required = 0`); got != 1 {
		t.Fatalf("pre-upgrade delegations without consent = %d, want 1", got)
	}
	for _, state := range []string{"pending", "granted", "declined", ""} {
		if _, err := database.ExecContext(ctx, `UPDATE delegations SET egress_consent = ? WHERE id = 'dlg_before'`, state); err != nil {
			t.Fatalf("consent %q refused: %v", state, err)
		}
	}
	if _, err := database.ExecContext(ctx, `UPDATE delegations SET egress_consent = 'maybe' WHERE id = 'dlg_before'`); err == nil {
		t.Fatal("an unknown consent state was stored")
	}
}
