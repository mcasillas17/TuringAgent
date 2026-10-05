package db

import (
	"context"
	"strings"
	"testing"
)

// Every tool call reads its run's job to find the frozen selection, so the
// lookup by run is indexed rather than a scan of every job ever queued.
func TestJobsAreIndexedByRun(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	rows, err := database.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT payload_json FROM jobs WHERE run_id = ?`, "run_1")
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
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(plan, "; "); !strings.Contains(joined, "USING INDEX idx_jobs_run") {
		t.Fatalf("query plan = %q, want the run lookup to use idx_jobs_run", joined)
	}
}
