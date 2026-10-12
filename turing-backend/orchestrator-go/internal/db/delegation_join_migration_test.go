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
