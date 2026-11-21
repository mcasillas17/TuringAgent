package db

import (
	"context"
	"testing"
)

// TUR-028a: the owner's quiet switch is compare-and-set on a revision kept
// beside its value. The migration gives every settings row one, starting at
// 0, so a row written before it (memory_enabled) reads as never revised, and
// a revision is a count: never negative, never absent.
func TestUnattendedQuietMigrationAddsASettingsRevision(t *testing.T) {
	ctx := context.Background()
	database := databaseBeforeMigration(t, ctx, "0033_unattended_quiet.sql")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO settings (key, value_json, updated_at) VALUES ('memory_enabled', 'false', '2026-11-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	readRevision := func(key string) int64 {
		t.Helper()
		var revision int64
		if err := database.QueryRowContext(ctx, `SELECT revision FROM settings WHERE key = ?`, key).Scan(&revision); err != nil {
			t.Fatal(err)
		}
		return revision
	}
	if got := readRevision("memory_enabled"); got != 0 {
		t.Fatalf("revision of a row written before the migration = %d, want 0", got)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO settings (key, value_json, updated_at) VALUES ('unattended_quiet', 'true', '2026-11-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if got := readRevision("unattended_quiet"); got != 0 {
		t.Fatalf("revision of a row inserted without one = %d, want the default 0", got)
	}
	if _, err := database.ExecContext(ctx, `UPDATE settings SET revision = 7 WHERE key = 'unattended_quiet'`); err != nil {
		t.Fatalf("revision = 7 was refused: %v", err)
	}
	if got := readRevision("unattended_quiet"); got != 7 {
		t.Fatalf("revision = %d after setting 7, want 7", got)
	}
	for _, value := range []any{-1, nil} {
		if _, err := database.ExecContext(ctx, `UPDATE settings SET revision = ? WHERE key = 'unattended_quiet'`, value); err == nil {
			t.Fatalf("revision = %v was accepted, want a count that is never negative or absent", value)
		}
	}
}
