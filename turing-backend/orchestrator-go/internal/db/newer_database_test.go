package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The ledger's primary key admits NULL, which no build writes; a hand that
// wrote one gets the same refusal, recoveries included, not a scan error.
func TestApplyMigrationsRefusesANullLedgerVersion(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (NULL, datetime('now'))`); err != nil {
		t.Fatal(err)
	}

	err = ApplyMigrations(ctx, database)
	if !errors.Is(err, ErrDatabaseNewerThanBuild) {
		t.Fatalf("ApplyMigrations error = %v, want ErrDatabaseNewerThanBuild", err)
	}
	if !strings.Contains(err.Error(), "(NULL)") || !strings.Contains(err.Error(), databaseRecoveries) {
		t.Fatalf("refusal %q does not name the NULL version and the recoveries", err)
	}
}

// Prefixes repeat (three 0012_* are embedded), so a parallel branch's
// migration most likely shares one, and sorts below the newest version. Only
// a comparison of whole names refuses it.
func TestApplyMigrationsRefusesAnUnknownVersionThatSharesAPrefix(t *testing.T) {
	ctx := context.Background()
	const parallel = "0012_from_a_parallel_branch"
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))`, parallel); err != nil {
		t.Fatal(err)
	}

	err = ApplyMigrations(ctx, database)
	if !errors.Is(err, ErrDatabaseNewerThanBuild) {
		t.Fatalf("ApplyMigrations error = %v, want ErrDatabaseNewerThanBuild", err)
	}
	if !strings.Contains(err.Error(), `("`+parallel+`")`) {
		t.Fatalf("refusal %q does not name %s alone", err, parallel)
	}
}

// A database holding no table of its own is a fresh install, whatever else
// it holds.
func TestApplyMigrationsInstallsBesideNoTableOfItsOwn(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		setup string
	}{
		// A first start that stopped after creating the ledger, before its
		// first migration committed.
		{name: "empty ledger", setup: `CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`},
		// SQLite keeps sqlite_sequence after the last AUTOINCREMENT table goes.
		{name: "only internal tables", setup: `CREATE TABLE counter (id INTEGER PRIMARY KEY AUTOINCREMENT); DROP TABLE counter`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database, err := Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if _, err := database.ExecContext(ctx, testCase.setup); err != nil {
				t.Fatal(err)
			}

			if err := ApplyMigrations(ctx, database); err != nil {
				t.Fatalf("install beside %s: %v", testCase.name, err)
			}
			assertEveryEmbeddedMigrationApplied(t, ctx, database)
		})
	}
}

// The TypeScript orchestrator (#7) recorded its one migration under the file
// name, 0001_initial.sql, the same file this build embeds as 0001_initial, and
// the Go orchestrator took that database over (#10). Such a database was
// written by older builds only, so it must keep starting.
func TestApplyMigrationsUpgradesADatabaseTheTypeScriptOrchestratorStarted(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	initial, err := migrationFS.ReadFile("schema/0001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO schema_migrations (version, applied_at) VALUES ('0001_initial.sql', '2026-05-12T09:00:00.000Z');
		INSERT INTO sessions (id, title, status, created_at, updated_at)
		VALUES ('session_typescript', 'Kept', 'active', '2026-05-12T09:00:00.000Z', '2026-05-12T09:00:00.000Z');
	`); err != nil {
		t.Fatal(err)
	}

	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatalf("upgrade a database the TypeScript orchestrator started: %v", err)
	}
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatalf("restart on it: %v", err)
	}
	assertEveryEmbeddedMigrationApplied(t, ctx, database)
	var sessions int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE id = 'session_typescript'`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf("TypeScript-era sessions = %d, want the one kept", sessions)
	}
}

func assertEveryEmbeddedMigrationApplied(t *testing.T, ctx context.Context, database *DB) {
	t.Helper()
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		var applied int
		if err := database.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, name[:len(name)-len(".sql")]).Scan(&applied); err != nil {
			t.Fatal(err)
		}
		if applied != 1 {
			t.Errorf("%s applied %d times, want once", name, applied)
		}
	}
}
