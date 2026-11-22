package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed schema/*.sql
var migrationFS embed.FS

// afterLegacySkillsExportHook is set only by the cross-system migration race
// regression, at the exact boundary between filesystem export and SQL commit.
var afterLegacySkillsExportHook func()

// afterRecoverySkillsExportHook is set only by the recovery root-swap
// regression, immediately after the export's final root-path verification.
var afterRecoverySkillsExportHook func()

const sessionLifecycleMigrationVersion = "0015_session_lifecycle"

// ErrDatabaseNewerThanBuild is the refusal ApplyMigrationsWithSkillsRoot
// returns before it writes anything, for a database this build cannot
// account for: its ledger records a migration this build does not have, or it
// holds tables beside a missing or empty ledger, which no Turing build leaves.
var ErrDatabaseNewerThanBuild = errors.New("database refused")

// databaseRecoveries is the whole recovery, stated once (spec section 17):
// there is no override, and deleting a ledger row reverses none of its
// migration's changes.
const databaseRecoveries = "restore the pre-upgrade backup (for a development branch, the backup taken before applying its migrations), " +
	"recreate a disposable development database, or run the matching newer build; " +
	"deleting a schema_migrations row does not undo a migration"

func ApplyMigrations(ctx context.Context, database *DB) error {
	return ApplyMigrationsWithSkillsRoot(ctx, database, "")
}

// ApplyMigrationsWithSkillsRoot exports legacy database-backed skills before
// the migration that removes their tables. The filesystem side effect happens
// first and is idempotent, so a crash before the database transaction commits
// can safely retry without duplicating or overwriting a skill. Ahead of both,
// it refuses a database it cannot account for (ErrDatabaseNewerThanBuild).
func ApplyMigrationsWithSkillsRoot(ctx context.Context, database *DB, skillsRoot string) error {
	if err := refuseDatabaseNewerThanBuild(ctx, database); err != nil {
		return err
	}
	if err := recoverPendingLegacySkills(ctx, database, skillsRoot); err != nil {
		return fmt.Errorf("recover legacy skill export: %w", err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	names, err := migrationNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var exists int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if version == "0011_file_skills" {
			if err := exportLegacySkills(ctx, database, skillsRoot); err != nil {
				return fmt.Errorf("%s export: %w", name, err)
			}
			if afterLegacySkillsExportHook != nil {
				afterLegacySkillsExportHook()
			}
		}
		sqlText, err := migrationFS.ReadFile("schema/" + name)
		if err != nil {
			return err
		}
		if hook, hooked := migrationHooks[version]; hooked {
			if err := applyHookedMigration(ctx, database, version, string(sqlText), hook); err != nil {
				return err
			}
			continue
		}
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if version == sessionLifecycleMigrationVersion {
			if err := normalizeSessionTimestamps(ctx, tx); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("%s: normalize session timestamps: %w", name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, string(sqlText)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))`, version); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if version == "0011_file_skills" {
			if err := dropEmptyLegacySkillRecovery(ctx, database); err != nil {
				return err
			}
		}
	}
	return nil
}

// refuseDatabaseNewerThanBuild reads before the runner writes anything: one
// sqlite_master read, then the ledger's versions, compared with this build's
// as whole names, because numeric prefixes repeat (three 0012_*). A ledger
// that is missing or holds no row means a fresh install only beside no other
// table; every migration commits its ledger row with its own changes, so no
// Turing build leaves tables without one.
func refuseDatabaseNewerThanBuild(ctx context.Context, database *DB) error {
	var ledgers, otherTables int
	if err := database.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE name = 'schema_migrations'),
			COUNT(*) FILTER (WHERE name <> 'schema_migrations')
		FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'
	`).Scan(&ledgers, &otherTables); err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	var applied []sql.NullString
	if ledgers > 0 {
		var err error
		if applied, err = appliedMigrationVersions(ctx, database); err != nil {
			return err
		}
	}
	if len(applied) == 0 {
		if otherTables == 0 {
			return nil
		}
		ledger := "no schema_migrations ledger"
		if ledgers > 0 {
			ledger = "an empty schema_migrations ledger"
		}
		return fmt.Errorf("%w: it holds tables but %s, which no Turing build leaves; %s",
			ErrDatabaseNewerThanBuild, ledger, databaseRecoveries)
	}
	known, err := knownMigrationVersions()
	if err != nil {
		return err
	}
	var unknown []string
	for _, version := range applied {
		if !version.Valid {
			unknown = append(unknown, "NULL")
			continue
		}
		if _, ok := known[version.String]; !ok {
			unknown = append(unknown, strconv.Quote(version.String))
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("%w: a newer Turing build applied migrations this build does not have (%s); %s",
			ErrDatabaseNewerThanBuild, strings.Join(unknown, ", "), databaseRecoveries)
	}
	return nil
}

// appliedMigrationVersions reads the ledger; a NULL version, which the
// primary key admits and no build writes, stays NULL.
func appliedMigrationVersions(ctx context.Context, database *DB) ([]sql.NullString, error) {
	rows, err := database.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var versions []sql.NullString
	for rows.Next() {
		var version sql.NullString
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("read schema_migrations: %w", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	return versions, nil
}

// knownMigrationVersions is every version this build embeds, read from the
// embedded schema directory so a new migration needs no edit here, plus the
// retired names below.
func knownMigrationVersions() (map[string]struct{}, error) {
	names, err := migrationNames()
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(names)+len(retiredMigrationVersions))
	for _, name := range names {
		known[strings.TrimSuffix(name, ".sql")] = struct{}{}
	}
	for version := range retiredMigrationVersions {
		known[version] = struct{}{}
	}
	return known, nil
}

// retiredMigrationVersions are the ledger names older Turing builds wrote for
// a migration this build embeds under another name, so this build knows their
// changes. The list is closed: both were written before the check existed,
// by builds whose successors promised to upgrade them. A branch migration
// renumbered before it merges now leaves its old name only in development
// databases, which the check refuses; recreate those instead of adding here.
var retiredMigrationVersions = map[string]struct{}{
	// The TypeScript orchestrator (#7) recorded file names, in the database
	// the Go orchestrator took over (#10); its 0001_initial.sql is the file
	// embedded here as 0001_initial, which the Go runner re-applies (every
	// statement is IF NOT EXISTS) under its own name.
	"0001_initial.sql": {},
	// #68's branch named its migration 0014_session_lifecycle until #67 took
	// 0014; 0015_session_lifecycle re-applies over it.
	"0014_session_lifecycle": {},
}

func recoverPendingLegacySkills(ctx context.Context, database *DB, skillsRoot string) error {
	var exists int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'legacy_skill_export_recovery'
	`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	var count int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM legacy_skill_export_recovery
	`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		if err := exportRecoverySkills(ctx, database, skillsRoot); err != nil {
			return err
		}
		if afterRecoverySkillsExportHook != nil {
			afterRecoverySkillsExportHook()
		}
		// There is no atomic commit boundary between this database and the
		// configured filesystem root. Nonempty recovery is therefore never
		// deleted by application code; cleanup is an offline operator action.
		return nil
	}
	_, err := database.ExecContext(ctx, `DROP TABLE legacy_skill_export_recovery`)
	return err
}

func dropEmptyLegacySkillRecovery(ctx context.Context, database *DB) error {
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM legacy_skill_export_recovery`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	_, err := database.ExecContext(ctx, `DROP TABLE legacy_skill_export_recovery`)
	return err
}

// LatestSchemaVersion reports the numeric version prefix from the latest
// embedded migration.
func LatestSchemaVersion() (string, error) {
	names, err := migrationNames()
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no schema migrations embedded")
	}
	version := strings.TrimSuffix(names[len(names)-1], ".sql")
	if separator := strings.IndexByte(version, '_'); separator >= 0 {
		version = version[:separator]
	}
	if version == "" {
		return "", fmt.Errorf("latest schema migration has no version")
	}
	return version, nil
}

func CurrentSchemaVersion() (string, error) {
	return LatestSchemaVersion()
}

func migrationNames() ([]string, error) {
	entries, err := migrationFS.ReadDir("schema")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
