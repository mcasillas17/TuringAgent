package db

import (
	"context"
	"embed"
	"fmt"
	"sort"
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

func ApplyMigrations(ctx context.Context, database *DB) error {
	return ApplyMigrationsWithSkillsRoot(ctx, database, "")
}

// ApplyMigrationsWithSkillsRoot exports legacy database-backed skills before
// the migration that removes their tables. The filesystem side effect happens
// first and is idempotent, so a crash before the database transaction commits
// can safely retry without duplicating or overwriting a skill.
func ApplyMigrationsWithSkillsRoot(ctx context.Context, database *DB, skillsRoot string) error {
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
