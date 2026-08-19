package db

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsMigrationExportsLegacyRowsBeforeDroppingTheirTables(t *testing.T) {
	ctx := context.Background()
	database := databaseThrough0010(t, ctx)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO skills (id, name, instructions, created_at, updated_at)
		VALUES ('skill_legacy', 'Careful: "Tone"', ?, datetime('now'), datetime('now'))`,
		"Keep the first line.\n\nKeep the second too."); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()

	if err := ApplyMigrationsWithSkillsRoot(ctx, database, root); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(root, "imported", "skill_legacy", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{
		"name: 'Careful: \"Tone\"'",
		"description: Imported from the previous TuringAgent skill library.",
		"Keep the first line.\n\nKeep the second too.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("exported file = %q, want %q", text, want)
		}
	}
	for _, table := range []string{"skills", "session_skills"} {
		var exists int
		if err := database.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != 0 {
			t.Fatalf("legacy table %s still exists", table)
		}
	}
	var recoveryRows int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM legacy_skill_export_recovery`).Scan(&recoveryRows); err != nil {
		t.Fatal(err)
	}
	if recoveryRows != 1 {
		t.Fatalf("legacy recovery rows = %d, want one", recoveryRows)
	}
}

func TestSkillsMigrationExportIsIdempotent(t *testing.T) {
	ctx := context.Background()
	database := databaseThrough0010(t, ctx)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO skills (id, name, instructions, created_at, updated_at)
		VALUES ('skill_repeat', 'Repeatable', 'Do not duplicate me.', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()

	if err := ApplyMigrationsWithSkillsRoot(ctx, database, root); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrationsWithSkillsRoot(ctx, database, root); err != nil {
		t.Fatal(err)
	}
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.Name() == "SKILL.md" {
			files = append(files, path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("exported files = %v, want exactly one", files)
	}
}

func TestRecoveryExportsUnsafeLegacyIDToDocumentedHashedFolder(t *testing.T) {
	ctx := context.Background()
	database := databaseThrough0010(t, ctx)
	const legacyID = "../unsafe legacy"
	const documentedFolder = "skill-4821d585f7aedcd6"
	if _, err := database.ExecContext(ctx, `
		INSERT INTO skills (id, name, instructions, created_at, updated_at)
		VALUES (?, 'Unsafe ID', 'Recover the unsafe identifier.', datetime('now'), datetime('now'))`, legacyID); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()

	if err := ApplyMigrationsWithSkillsRoot(ctx, database, root); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "imported", documentedFolder, "SKILL.md")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("documented hashed export path: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrationsWithSkillsRoot(ctx, database, root); err != nil {
		t.Fatalf("recover unsafe legacy id: %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read recovered unsafe-id skill: %v", err)
	}
	if !strings.Contains(string(content), "Recover the unsafe identifier.") {
		t.Fatalf("recovered unsafe-id skill = %q", content)
	}
}

func TestSkillsMigrationRefusesToOverwriteAConflictingExport(t *testing.T) {
	ctx := context.Background()
	database := databaseThrough0010(t, ctx)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO skills (id, name, instructions, created_at, updated_at)
		VALUES ('skill_collision', 'Collision', 'Database copy.', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	target := filepath.Join(root, "imported", "skill_collision", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("user-owned different content"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ApplyMigrationsWithSkillsRoot(ctx, database, root)
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("error = %v, want conflicting export refusal", err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "user-owned different content" {
		t.Fatalf("conflicting file was overwritten: %q", content)
	}
	var exists int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'skills'`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != 1 {
		t.Fatal("legacy table was dropped even though export failed")
	}
}

func TestSkillsMigrationRejectsSymlinkedExportWithoutDroppingLegacyRows(t *testing.T) {
	ctx := context.Background()
	database := databaseThrough0010(t, ctx)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO skills (id, name, instructions, created_at, updated_at)
		VALUES ('skill_link', 'Linked', 'Database copy.', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	target := filepath.Join(root, "imported", "skill_link", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	wantContent, err := marshalLegacySkill(legacySkill{ID: "skill_link", Name: "Linked", Instructions: "Database copy."})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, wantContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}

	err = ApplyMigrationsWithSkillsRoot(ctx, database, root)
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("error = %v, want symlink refusal", err)
	}
	content, readErr := os.ReadFile(outside)
	if readErr != nil || !bytes.Equal(content, wantContent) {
		t.Fatalf("outside target = %q, error=%v", content, readErr)
	}
	var exists int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'skills'`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != 1 {
		t.Fatal("legacy table was dropped after symlink refusal")
	}
}

func TestSkillsMigrationRejectsParentDirectorySwapWithoutWritingOutsideRoot(t *testing.T) {
	ctx := context.Background()
	database := databaseThrough0010(t, ctx)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO skills (id, name, instructions, created_at, updated_at)
		VALUES ('skill_parent_swap', 'Parent swap', 'Database copy.', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	targetFolder := filepath.Join(root, "imported", "skill_parent_swap")
	parkedFolder := filepath.Join(root, "imported", "skill_parent_swap-original")
	outside := t.TempDir()
	legacyExportBeforeInstallHook = func() {
		legacyExportBeforeInstallHook = nil
		if err := os.Rename(targetFolder, parkedFolder); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, targetFolder); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { legacyExportBeforeInstallHook = nil })

	err := ApplyMigrationsWithSkillsRoot(ctx, database, root)
	if err == nil {
		t.Fatal("migration succeeded after its export parent was swapped")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "SKILL.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("outside export exists or could not be inspected: %v", statErr)
	}
	var exists int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'skills'`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != 1 {
		t.Fatal("legacy table was dropped after parent directory swap")
	}
}

func TestSkillsMigrationRejectsRootSwapWithoutDroppingLegacyRows(t *testing.T) {
	ctx := context.Background()
	database := databaseThrough0010(t, ctx)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO skills (id, name, instructions, created_at, updated_at)
		VALUES ('skill_root_swap', 'Root swap', 'Database copy.', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "skills")
	parkedRoot := filepath.Join(parent, "skills-original")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyExportBeforeInstallHook = func() {
		legacyExportBeforeInstallHook = nil
		if err := os.Rename(root, parkedRoot); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { legacyExportBeforeInstallHook = nil })

	err := ApplyMigrationsWithSkillsRoot(ctx, database, root)
	if err == nil || !strings.Contains(err.Error(), "root changed") {
		t.Fatalf("error = %v, want root replacement rejection", err)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("replacement skills root contains export data: %+v", entries)
	}
	var exists int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'skills'`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != 1 {
		t.Fatal("legacy table was dropped after root directory swap")
	}
}

func TestSkillsMigrationRetainsRecoveryCopyWhenRootChangesAfterVerification(t *testing.T) {
	ctx := context.Background()
	database := databaseThrough0010(t, ctx)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO skills (id, name, instructions, created_at, updated_at)
		VALUES ('skill_commit_window', 'Commit window', 'Recover this copy.', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "skills")
	parkedRoot := filepath.Join(parent, "skills-exported")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	afterLegacySkillsExportHook = func() {
		afterLegacySkillsExportHook = nil
		if err := os.Rename(root, parkedRoot); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterLegacySkillsExportHook = nil })

	if err := ApplyMigrationsWithSkillsRoot(ctx, database, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parkedRoot, "imported", "skill_commit_window", "SKILL.md")); err != nil {
		t.Fatalf("parked export is not recoverable: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("replacement root unexpectedly contains export: %+v", entries)
	}
	var legacyTable int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'skills'`).Scan(&legacyTable); err != nil {
		t.Fatal(err)
	}
	if legacyTable != 0 {
		t.Fatal("legacy skills table still exists after migration")
	}

	// The next startup consumes the migration-only recovery rows into the
	// currently configured root, but deliberately retains them for offline
	// operator cleanup.
	if err := ApplyMigrationsWithSkillsRoot(ctx, database, root); err != nil {
		t.Fatalf("automatic recovery retry: %v", err)
	}
	recovered, err := os.ReadFile(filepath.Join(root, "imported", "skill_commit_window", "SKILL.md"))
	if err != nil {
		t.Fatalf("read recovered skill: %v", err)
	}
	if !strings.Contains(string(recovered), "Recover this copy.") {
		t.Fatalf("recovered skill = %q", recovered)
	}
	var recoveryTable int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'legacy_skill_export_recovery'
	`).Scan(&recoveryTable); err != nil {
		t.Fatal(err)
	}
	if recoveryTable != 1 {
		t.Fatal("migration-only recovery table disappeared during automatic retry")
	}

	// Swap the root after the recovery export's final verification. Automatic
	// startup must keep the database copy even at this exact cross-system
	// boundary, so a later startup can recover into the replacement root.
	confirmedRoot := filepath.Join(parent, "skills-confirmed")
	afterRecoverySkillsExportHook = func() {
		afterRecoverySkillsExportHook = nil
		if err := os.Rename(root, confirmedRoot); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterRecoverySkillsExportHook = nil })
	if err := ApplyMigrationsWithSkillsRoot(ctx, database, root); err != nil {
		t.Fatalf("root-swap recovery retry: %v", err)
	}
	entries, err = os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("replacement root unexpectedly contains export: %+v", entries)
	}
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'legacy_skill_export_recovery'
	`).Scan(&recoveryTable); err != nil {
		t.Fatal(err)
	}
	if recoveryTable != 1 {
		t.Fatal("automatic startup deleted recovery after final export verification")
	}

	if err := ApplyMigrationsWithSkillsRoot(ctx, database, root); err != nil {
		t.Fatalf("recovery into replacement root: %v", err)
	}
	finalCopy, err := os.ReadFile(filepath.Join(root, "imported", "skill_commit_window", "SKILL.md"))
	if err != nil || !strings.Contains(string(finalCopy), "Recover this copy.") {
		t.Fatalf("final recovered skill = %q, error=%v", finalCopy, err)
	}
}

func TestRecoveryConflictLeavesFileAndRowsIntactForRetry(t *testing.T) {
	ctx := context.Background()
	database := databaseThrough0010(t, ctx)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO skills (id, name, instructions, created_at, updated_at)
		VALUES ('skill_recovery_conflict', 'Recovery conflict', 'Database recovery.', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := ApplyMigrationsWithSkillsRoot(ctx, database, root); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "imported", "skill_recovery_conflict", "SKILL.md")
	conflict := []byte("user-owned replacement")
	if err := os.WriteFile(target, conflict, 0o600); err != nil {
		t.Fatal(err)
	}

	err := ApplyMigrationsWithSkillsRoot(ctx, database, root)
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("error = %v, want recovery conflict refusal", err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil || !bytes.Equal(content, conflict) {
		t.Fatalf("conflicting target = %q, error=%v", content, readErr)
	}
	var rows int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM legacy_skill_export_recovery
	`).Scan(&rows); err != nil {
		t.Fatalf("recovery rows missing after conflict: %v", err)
	}
	if rows != 1 {
		t.Fatalf("recovery rows after conflict = %d", rows)
	}
}

func databaseThrough0010(t *testing.T, ctx context.Context) *DB {
	t.Helper()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	for _, name := range []string{
		"0001_initial.sql",
		"0002_go_runtime.sql",
		"0003_messages_fts.sql",
		"0003_tool_call_model_identity.sql",
		"0004_execution_exit_gate.sql",
		"0005_timestamp_ordering.sql",
		"0006_skills.sql",
		"0007_agents.sql",
		"0008_integrations.sql",
		"0009_automations.sql",
		"0010_telemetry.sql",
	} {
		applyMigration(t, ctx, database, name)
	}
	return database
}
