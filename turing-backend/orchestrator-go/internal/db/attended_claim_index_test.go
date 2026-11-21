package db

import (
	"context"
	"strings"
	"testing"
)

// TUR-027: the attended probe's index is partial on attended pending jobs,
// so a backlog of unattended work never grows what it walks, and it carries
// the claim index's own key, so the planner weighs the two alike and takes
// the smaller. The flag it filters on is exactly 0 or 1.
func TestAttendedClaimMigrationAddsTheFlagAndItsPartialIndex(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	assertIndexColumns(t, ctx, database, "idx_jobs_attended_claim", []indexColumn{
		{name: "agent_id"}, {name: "status"}, {name: "created_at_ns"}, {name: "id"},
	})
	var definition string
	if err := database.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_jobs_attended_claim' AND tbl_name = 'jobs'`).
		Scan(&definition); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(definition, "WHERE status = 'pending' AND unattended = 0") {
		t.Fatalf("idx_jobs_attended_claim = %q, want a partial index on attended pending jobs", definition)
	}

	runID := seedDelegationRun(t, ctx, database, "sess_flag", "", "chat")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO jobs (id, run_id, agent_id, status, payload_json, created_at, created_at_ns)
		VALUES ('job_flag', ?, 'general_assistant', 'pending', '{}', '2026-11-01T00:00:00.000000000Z', 1)`, runID); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{2, nil} {
		if _, err := database.ExecContext(ctx, `UPDATE jobs SET unattended = ? WHERE id = 'job_flag'`, value); err == nil {
			t.Fatalf("jobs.unattended = %v was accepted, want only 0 or 1", value)
		}
	}
}
