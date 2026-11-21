package db

import (
	"context"
	"strings"
	"testing"
)

// UX-002a: no automation had dismissed anything before the Needs-you queue
// existed, so every one reads as never dismissed after the upgrade.
func TestAttentionMigrationLeavesExistingAutomationsUndismissed(t *testing.T) {
	ctx := context.Background()
	database := databaseBeforeMigration(t, ctx, "0031_attention.sql")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO automations (id, name, prompt, schedule_kind, interval_seconds, enabled, created_at, updated_at)
		VALUES ('auto_before', 'Digest', 'x', 'interval', 300, 1,
			'2026-11-01T00:00:00.000000000Z', '2026-11-01T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, ctx, database,
		`SELECT COUNT(*) FROM automations WHERE id = 'auto_before' AND attention_dismissed_occurrence IS NULL`); got != 1 {
		t.Fatalf("pre-upgrade automations never dismissed = %d, want 1", got)
	}
}

// The queue's two bounded reads each get a partial index: pending consents,
// and each automation's remote-egress blocks, so one automation's lookup is
// one seek whatever another's history holds.
func TestAttentionMigrationAddsItsPartialIndexes(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	for _, test := range []struct {
		index   string
		table   string
		columns []indexColumn
		where   string
	}{
		{
			index: "idx_delegations_consent_pending", table: "delegations",
			columns: []indexColumn{{name: "created_at"}, {name: "id"}},
			where:   "WHERE egress_consent = 'pending'",
		},
		{
			index: "idx_audit_remote_egress_blocked", table: "audit_logs",
			columns: []indexColumn{{name: "actor_id"}, {name: "created_at"}},
			where:   "WHERE action = 'automation.remote_egress_blocked'",
		},
	} {
		assertIndexColumns(t, ctx, database, test.index, test.columns)
		var definition string
		if err := database.QueryRowContext(ctx,
			`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ? AND tbl_name = ?`,
			test.index, test.table).Scan(&definition); err != nil {
			t.Fatalf("%s: %v", test.index, err)
		}
		if !strings.Contains(definition, test.where) {
			t.Fatalf("%s = %q, want a partial index %s", test.index, definition, test.where)
		}
	}
}
