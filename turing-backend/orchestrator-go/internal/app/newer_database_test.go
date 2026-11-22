package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
)

// TUR-016a: a build refuses a database it cannot account for before it writes
// a row, a table or the skills root (spec section 17).

// refusalRecoveries are the three recoveries a refusal names, stated once in
// spec section 17; the second phrase is the pre-branch backup.
var refusalRecoveries = []string{
	"restore the pre-upgrade backup",
	"the backup taken before applying",
	"recreate a disposable development database",
	"run the matching newer build",
}

// refusalLedgerWarning is the only mention of removing anything a refusal may
// make: deleting a ledger row is not a recovery (rounds 27 and 28).
const refusalLedgerWarning = "deleting a schema_migrations row does not undo a migration"

// refusalMessageFaults lists what a refusal message gets wrong: a recovery or
// the warning it omits, or any other mention of removing something.
func refusalMessageFaults(message string) []string {
	text := strings.ToLower(message)
	var faults []string
	for _, required := range append(append([]string(nil), refusalRecoveries...), refusalLedgerWarning) {
		if !strings.Contains(text, required) {
			faults = append(faults, fmt.Sprintf("omits %q", required))
		}
	}
	rest := strings.ReplaceAll(text, refusalLedgerWarning, "")
	for _, removal := range []string{"delet", "remov", "drop", "truncat"} {
		if strings.Contains(rest, removal) {
			faults = append(faults, fmt.Sprintf("suggests removing something (%q)", removal))
		}
	}
	return faults
}

func TestNewRefusesADatabaseANewerBuildMigrated(t *testing.T) {
	ctx := context.Background()
	const newerVersion = "9999_from_a_newer_build"
	databasePath := filepath.Join(t.TempDir(), "turing.db")
	database := migratedDatabaseAt(t, databasePath)
	// What a newer build leaves: its migration's table and row and its ledger
	// row, beside a conversation and a legacy skill still pending export,
	// which the runner exports into the skills root before anything else.
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE newer_build_state (id TEXT PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO newer_build_state (id, value) VALUES ('state_kept', 'written by the newer build');
		INSERT INTO schema_migrations (version, applied_at) VALUES ('`+newerVersion+`', '2026-11-02 00:00:00');
		CREATE TABLE legacy_skill_export_recovery (id TEXT PRIMARY KEY, name TEXT NOT NULL, instructions TEXT NOT NULL);
		INSERT INTO legacy_skill_export_recovery (id, name, instructions)
		VALUES ('skill_pending', 'Pending', 'Exported only by a build that may migrate this database.');
	`); err != nil {
		t.Fatal(err)
	}
	before := databaseSnapshot(t, database)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	skillsRoot := t.TempDir()

	application, err := New(databaseStartConfig(databasePath, skillsRoot))
	if err == nil {
		application.Stop()
		t.Fatal("New started on a database a newer build migrated")
	}
	if !errors.Is(err, db.ErrDatabaseNewerThanBuild) {
		t.Fatalf("New error = %v, want ErrDatabaseNewerThanBuild", err)
	}
	message := err.Error()
	if !strings.Contains(message, newerVersion) {
		t.Errorf("refusal %q does not name %s", message, newerVersion)
	}
	if faults := refusalMessageFaults(message); len(faults) > 0 {
		t.Errorf("refusal %q: %s", message, strings.Join(faults, "; "))
	}
	// The check fails a message that omits any recovery or the warning, or
	// that suggests deleting a row.
	lower := strings.ToLower(message)
	broken := map[string]string{
		"suggests deleting the row": lower + "; or delete its schema_migrations row",
		"suggests removing the row": lower + "; or remove " + newerVersion + " from schema_migrations",
	}
	for _, phrase := range append(append([]string(nil), refusalRecoveries...), refusalLedgerWarning) {
		broken["omits "+phrase] = strings.ReplaceAll(lower, phrase, "")
	}
	for fault, variant := range broken {
		if len(refusalMessageFaults(variant)) == 0 {
			t.Errorf("message check accepted a refusal that %s: %q", fault, variant)
		}
	}
	assertDatabaseUnchanged(t, databasePath, before)
	if entries, err := os.ReadDir(skillsRoot); err != nil || len(entries) != 0 {
		t.Fatalf("skills root entries = %v (error %v), want it untouched", entries, err)
	}
}

func TestNewRefusesTablesBesideAMissingOrEmptyLedger(t *testing.T) {
	for _, testCase := range []struct {
		name string
		// damage is what a hand did to a migrated database's ledger.
		damage string
		// ledger is how the refusal names it.
		ledger string
	}{
		{name: "missing ledger", damage: `DROP TABLE schema_migrations`, ledger: "no schema_migrations ledger"},
		{name: "empty ledger", damage: `DELETE FROM schema_migrations`, ledger: "an empty schema_migrations ledger"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "turing.db")
			database := migratedDatabaseAt(t, databasePath)
			if _, err := database.ExecContext(context.Background(), testCase.damage); err != nil {
				t.Fatal(err)
			}
			before := databaseSnapshot(t, database)
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}

			application, err := New(databaseStartConfig(databasePath, t.TempDir()))
			if err == nil {
				application.Stop()
				t.Fatalf("New started on tables beside %s", testCase.ledger)
			}
			if !errors.Is(err, db.ErrDatabaseNewerThanBuild) {
				t.Fatalf("New error = %v, want ErrDatabaseNewerThanBuild", err)
			}
			if !strings.Contains(err.Error(), testCase.ledger) {
				t.Errorf("refusal %q does not name %s", err, testCase.ledger)
			}
			if faults := refusalMessageFaults(err.Error()); len(faults) > 0 {
				t.Errorf("refusal %q: %s", err, strings.Join(faults, "; "))
			}
			// Unchanged includes sqlite_master: no schema_migrations table
			// appears, and none of its rows.
			assertDatabaseUnchanged(t, databasePath, before)
		})
	}
}

func TestNewStartsAgainOnADatabaseItMigrated(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "turing.db")
	skillsRoot := t.TempDir()
	first, err := New(databaseStartConfig(databasePath, skillsRoot))
	if err != nil {
		t.Fatalf("first start on an empty database: %v", err)
	}
	first.Stop()
	database, err := db.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	// Three migrations share the prefix 0012; versions are compared as names.
	var sharedPrefix int
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM schema_migrations WHERE version LIKE '0012\_%' ESCAPE '\'
	`).Scan(&sharedPrefix); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if sharedPrefix != 3 {
		t.Fatalf("0012_* versions applied = %d, want 3", sharedPrefix)
	}

	second, err := New(databaseStartConfig(databasePath, skillsRoot))
	if err != nil {
		t.Fatalf("restart on a database whose versions are all embedded: %v", err)
	}
	second.Stop()
}

func databaseStartConfig(databasePath string, skillsRoot string) config.Config {
	return config.Config{
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret: "approval-secret",
		DatabasePath:      databasePath,
		SkillsRoot:        skillsRoot,
		LocalModel:        "llama3.2",
		OpenAIModel:       "gpt-4o-mini",
	}
}

// migratedDatabaseAt is a database this build migrated, holding one
// conversation, left open for a test to change before it starts the app.
func migratedDatabaseAt(t *testing.T, databasePath string) *db.DB {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrationsWithSkillsRoot(ctx, database, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, title, status, created_at, updated_at)
		VALUES ('session_kept', 'Kept', 'active', '2026-11-01T00:00:00Z', '2026-11-01T00:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}
	return database
}

// databaseSnapshot is every sqlite_master entry and every row of every table,
// sorted, so two snapshots are equal only if the refusal changed neither.
func databaseSnapshot(t *testing.T, database *db.DB) []string {
	t.Helper()
	ctx := context.Background()
	var snapshot, tables []string
	schemaRows, err := database.QueryContext(ctx, `
		SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_master ORDER BY type, name
	`)
	if err != nil {
		t.Fatal(err)
	}
	for schemaRows.Next() {
		var kind, name, table, definition string
		if err := schemaRows.Scan(&kind, &name, &table, &definition); err != nil {
			t.Fatal(err)
		}
		snapshot = append(snapshot, fmt.Sprintf("schema %s %s on %s: %s", kind, name, table, definition))
		if kind == "table" {
			tables = append(tables, name)
		}
	}
	if err := schemaRows.Err(); err != nil {
		t.Fatal(err)
	}
	// One pooled connection: each result set closes before the next query.
	_ = schemaRows.Close()
	for _, table := range tables {
		rows, err := database.QueryContext(ctx, `SELECT * FROM "`+table+`"`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var tableRows []string
		for rows.Next() {
			values := make([]any, len(columns))
			targets := make([]any, len(columns))
			for i := range values {
				targets[i] = &values[i]
			}
			if err := rows.Scan(targets...); err != nil {
				t.Fatal(err)
			}
			tableRows = append(tableRows, fmt.Sprintf("row %s %v", table, values))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		sort.Strings(tableRows)
		snapshot = append(snapshot, tableRows...)
	}
	return snapshot
}

func assertDatabaseUnchanged(t *testing.T, databasePath string, before []string) {
	t.Helper()
	database, err := db.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	after := databaseSnapshot(t, database)
	var gone, added []string
	seen := make(map[string]int, len(after))
	for _, entry := range after {
		seen[entry]++
	}
	for _, entry := range before {
		if seen[entry] == 0 {
			gone = append(gone, entry)
			continue
		}
		seen[entry]--
	}
	for entry, count := range seen {
		for ; count > 0; count-- {
			added = append(added, entry)
		}
	}
	if len(gone) > 0 || len(added) > 0 {
		sort.Strings(added)
		t.Fatalf("refused database changed:\nremoved %q\nadded %q", gone, added)
	}
}
