package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// errInjectedPhase is the failure tests inject at a named migration seam. It
// is deliberately unrelated to any production sentinel so a test can prove the
// runner propagated exactly the error it was given.
var errInjectedPhase = errors.New("injected migration phase failure")

// recordMigrationPhases installs a phase observer that appends every phase the
// runner reaches, optionally running an inspection callback inside the live
// transaction. The observer is removed when the test ends.
func recordMigrationPhases(t *testing.T, inspect func(phase string, tx *sql.Tx)) *[]string {
	t.Helper()
	phases := make([]string, 0, 8)
	migrationPhaseHook = func(_ context.Context, version string, phase string, tx *sql.Tx) error {
		phases = append(phases, version+":"+phase)
		if inspect != nil {
			inspect(phase, tx)
		}
		return nil
	}
	t.Cleanup(func() { migrationPhaseHook = nil })
	return &phases
}

// failAtMigrationPhase installs a phase observer that fails exactly once, at
// the named seam, so a rollback boundary can be exercised in isolation.
func failAtMigrationPhase(t *testing.T, target string) {
	t.Helper()
	migrationPhaseHook = func(_ context.Context, _ string, phase string, _ *sql.Tx) error {
		if phase == target {
			return errInjectedPhase
		}
		return nil
	}
	t.Cleanup(func() { migrationPhaseHook = nil })
}

func TestRunOutcomeMigrationRunsBeforeSQLAfterSQLRecordAndCommitInOrder(t *testing.T) {
	ctx := context.Background()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	recordedAt := map[string]int{}
	phases := recordMigrationPhases(t, func(phase string, tx *sql.Tx) {
		var recorded int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
			runOutcomesMigrationVersion,
		).Scan(&recorded); err != nil {
			t.Fatalf("count migration record at %s: %v", phase, err)
		}
		recordedAt[phase] = recorded
		// The parent-table swap only survives because cascades are disarmed on
		// this one pinned connection for the length of the transaction.
		var foreignKeys int
		if err := tx.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			t.Fatalf("read foreign_keys at %s: %v", phase, err)
		}
		if foreignKeys != 0 {
			t.Fatalf("PRAGMA foreign_keys at %s = %d, want 0 inside the rebuild transaction", phase, foreignKeys)
		}
	})

	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}

	want := []string{
		runOutcomesMigrationVersion + ":" + migrationPhaseBeforeHook,
		runOutcomesMigrationVersion + ":" + migrationPhaseAfterRebuild,
		runOutcomesMigrationVersion + ":" + migrationPhaseAfterScrub,
		runOutcomesMigrationVersion + ":" + migrationPhaseAfterIndexes,
		runOutcomesMigrationVersion + ":" + migrationPhaseAfterHook,
		runOutcomesMigrationVersion + ":" + migrationPhaseBeforeRecord,
		runOutcomesMigrationVersion + ":" + migrationPhaseAfterRecord,
	}
	if !reflect.DeepEqual(*phases, want) {
		t.Fatalf("migration phases = %v, want %v", *phases, want)
	}
	for _, phase := range []string{
		migrationPhaseBeforeHook,
		migrationPhaseAfterRebuild,
		migrationPhaseAfterScrub,
		migrationPhaseAfterIndexes,
		migrationPhaseAfterHook,
		migrationPhaseBeforeRecord,
	} {
		if recordedAt[phase] != 0 {
			t.Fatalf("migration record count at %s = %d, want 0", phase, recordedAt[phase])
		}
	}
	if recordedAt[migrationPhaseAfterRecord] != 1 {
		t.Fatalf("migration record count at %s = %d, want 1", migrationPhaseAfterRecord, recordedAt[migrationPhaseAfterRecord])
	}

	var committed int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
		runOutcomesMigrationVersion,
	).Scan(&committed); err != nil {
		t.Fatal(err)
	}
	if committed != 1 {
		t.Fatalf("committed migration record count = %d, want 1", committed)
	}
}

func TestOrdinaryMigrationsKeepExistingExecutionPath(t *testing.T) {
	ctx := context.Background()
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrationHooks) != 1 {
		t.Fatalf("registered migration hooks = %d, want exactly the TUR-009 hook", len(migrationHooks))
	}
	if _, ok := migrationHooks[runOutcomesMigrationVersion]; !ok {
		t.Fatalf("migration hooks do not contain %q", runOutcomesMigrationVersion)
	}
	for _, name := range names {
		version := name[:len(name)-len(".sql")]
		wantPinned := version == runOutcomesMigrationVersion
		if got := migrationUsesPinnedConnection(version); got != wantPinned {
			t.Fatalf("migrationUsesPinnedConnection(%q) = %v, want %v", version, got, wantPinned)
		}
		if wantPinned {
			continue
		}
		// An ordinary file is executed by one ExecContext even if its text
		// happens to contain a marker comment.
		ordinary := "SELECT 1;\n-- marker: after-rebuild\nSELECT 2;\n"
		sections, err := migrationSections(version, ordinary)
		if err != nil {
			t.Fatalf("migrationSections(%q): %v", version, err)
		}
		if len(sections) != 1 || sections[0].SQL != ordinary || sections[0].Marker != "" {
			t.Fatalf("ordinary migration %q sections = %#v, want one unsplit section", version, sections)
		}
	}

	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	phases := recordMigrationPhases(t, nil)
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	for _, phase := range *phases {
		if got := phase[:len(runOutcomesMigrationVersion)+1]; got != runOutcomesMigrationVersion+":" {
			t.Fatalf("ordinary migration reached phase %q, want no phase outside %q", phase, runOutcomesMigrationVersion)
		}
	}
}

func TestRunOutcomeMigrationRecordIsAtomicWithHooksAndSQL(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "atomic.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	failAtMigrationPhase(t, migrationPhaseAfterRecord)
	err = ApplyMigrations(ctx, database)
	if !errors.Is(err, errInjectedPhase) {
		t.Fatalf("ApplyMigrations error = %v, want injected phase failure", err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var recorded int
	if err := reopened.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
		runOutcomesMigrationVersion,
	).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 0 {
		t.Fatalf("migration record count after rollback = %d, want 0", recorded)
	}
	if hasColumn(t, ctx, reopened, "agent_runs", "state_version") {
		t.Fatal("agent_runs.state_version survived a rolled-back migration")
	}
}

func hasColumn(t *testing.T, ctx context.Context, database *DB, table string, column string) bool {
	t.Helper()
	rows, err := database.QueryContext(ctx, "PRAGMA table_info("+quoteSQLiteIdentifier(table)+")")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var columnID, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&columnID, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return false
}

// legacyRun is one pre-TUR-009 agent_runs row plus the assistant message it
// claims, expressed the way the legacy schema stored it.
type legacyRun struct {
	id                 string
	sessionID          string
	status             string
	executionActive    int
	executionState     string
	errorCode          string
	errorMessage       string
	cancellationReason string
	createdAt          string
	startedAt          string
	finishedAt         string
	assistantMessageID string
	assistantContent   string
	// assistantRunID overrides the message's back-reference so a test can seed
	// a mismatched or duplicated legacy correlation.
	assistantRunID  string
	assistantRole   string
	externalAgent   string
	skipUserMessage bool
}

func openMigratedThroughLegacy(t *testing.T, ctx context.Context, path string) *DB {
	t.Helper()
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	applyMigrationsBeforeRunOutcomes(t, ctx, database)
	return database
}

func applyMigrationsBeforeRunOutcomes(t *testing.T, ctx context.Context, database *DB) {
	t.Helper()
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name == runOutcomesMigrationVersion+".sql" {
			return
		}
		applyMigration(t, ctx, database, name)
	}
	t.Fatalf("embedded migrations do not contain %q", runOutcomesMigrationVersion)
}

func seedLegacySession(t *testing.T, ctx context.Context, database *DB, sessionID string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, title, title_origin, status, created_at, updated_at)
		VALUES (?, 'Legacy', 'unset', 'active', '2026-01-01T00:00:00.000000000Z', '2026-01-01T00:00:00.000000000Z')
	`, sessionID); err != nil {
		t.Fatal(err)
	}
}

func seedLegacyRun(t *testing.T, ctx context.Context, database *DB, run legacyRun) {
	t.Helper()
	if run.createdAt == "" {
		run.createdAt = "2026-01-01T00:00:00.000000000Z"
	}
	if run.assistantRole == "" {
		run.assistantRole = "assistant"
	}
	userMessageID := run.id + "_user"
	if !run.skipUserMessage {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
			VALUES (?, ?, 'user', 'ask', 'text',
				(SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE session_id = ?), ?)
		`, userMessageID, run.sessionID, run.sessionID, run.createdAt); err != nil {
			t.Fatal(err)
		}
	}
	if run.assistantMessageID != "" {
		backReference := run.assistantRunID
		if backReference == "" {
			backReference = run.id
		}
		if _, err := database.ExecContext(ctx, `
			INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
			VALUES (?, ?, ?, ?, ?, 'text',
				(SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE session_id = ?), ?)
		`, run.assistantMessageID, run.sessionID, backReference, run.assistantRole, run.assistantContent,
			run.sessionID, run.createdAt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agent_runs (
			id, session_id, user_message_id, assistant_message_id, agent_id, trace_id, status,
			model_provider, model_name, error_code, error_message, cancellation_reason,
			execution_active, execution_state, external_agent_name,
			created_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, 'general_assistant', ?, ?, 'ollama', 'qwen2.5:7b', ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		run.id, run.sessionID, userMessageID, nullableText(run.assistantMessageID), "trace_"+run.id, run.status,
		nullableText(run.errorCode), nullableText(run.errorMessage), nullableText(run.cancellationReason),
		run.executionActive, defaultText(run.executionState, "none"), nullableText(run.externalAgent),
		run.createdAt, nullableText(run.startedAt), nullableText(run.finishedAt),
	); err != nil {
		t.Fatal(err)
	}
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func defaultText(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func applyRunOutcomesMigration(t *testing.T, ctx context.Context, database *DB) error {
	t.Helper()
	return ApplyMigrations(ctx, database)
}

func TestRunOutcomeMigrationRejectsDuplicateRunAssistantCorrelationValueFree(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "duplicate-run.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_dup_run")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_dup_a", sessionID: "sess_dup_run", status: "completed",
		assistantMessageID: "msg_shared", assistantContent: "shared answer",
	})
	// A second run claiming the same assistant message. Ownership is ambiguous,
	// so the migration must abort rather than pick one.
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_dup_b", sessionID: "sess_dup_run", status: "completed",
	})
	if _, err := database.ExecContext(ctx,
		`UPDATE agent_runs SET assistant_message_id = 'msg_shared' WHERE id = 'run_dup_b'`); err != nil {
		t.Fatal(err)
	}

	err := applyRunOutcomesMigration(t, ctx, database)
	if err == nil {
		t.Fatal("ApplyMigrations succeeded with a duplicated run/assistant correlation")
	}
	if got := err.Error(); got != "run/message correlation conflict" {
		t.Fatalf("error = %q, want exactly the value-free correlation sentinel", got)
	}
}

func TestRunOutcomeMigrationRejectsDuplicateMessageRunCorrelationValueFree(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "duplicate-message.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_dup_message")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_two_assistants", sessionID: "sess_dup_message", status: "completed",
		assistantMessageID: "msg_first", assistantContent: "first answer",
	})
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_second', 'sess_dup_message', 'run_two_assistants', 'assistant', 'second answer', 'text', 99,
			'2026-01-01T00:00:00.000000000Z')
	`); err != nil {
		t.Fatal(err)
	}

	err := applyRunOutcomesMigration(t, ctx, database)
	if err == nil {
		t.Fatal("ApplyMigrations succeeded with two assistant messages on one run")
	}
	if got := err.Error(); got != "run/message correlation conflict" {
		t.Fatalf("error = %q, want exactly the value-free correlation sentinel", got)
	}
}

func TestRunOutcomeMigrationAllowsNullAndSingleMismatchedLegacyCorrelation(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "legacy-correlation.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_legacy_link")
	// No assistant message at all.
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_null_link", sessionID: "sess_legacy_link", status: "failed",
		errorCode: "model_error", errorMessage: "provider said something",
		finishedAt: "2026-01-01T00:00:01.000000000Z",
	})
	// One assistant message whose back-reference names a different run.
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_mismatched_link", sessionID: "sess_legacy_link", status: "completed",
		assistantMessageID: "msg_mismatched", assistantContent: "orphan answer",
		assistantRunID: "run_somewhere_else",
	})

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatalf("ApplyMigrations rejected neutral legacy correlation: %v", err)
	}
	emptyDigest := runoutcome.ContentSHA256("")
	for _, runID := range []string{"run_null_link", "run_mismatched_link"} {
		got := readMigratedRunState(t, ctx, database, runID)
		if got.StateVersion != 1 {
			t.Fatalf("%s state_version = %d, want 1", runID, got.StateVersion)
		}
		// An unusable link proves nothing about ownership, so no content is
		// adopted from it: the identity stays the empty-content digest.
		if got.ContentSHA256 != emptyDigest {
			t.Fatalf("%s adopted content from an unusable legacy link", runID)
		}
	}
}

// recordRunOutcomeBatches captures every keyset batch the migration reads so
// the row and byte bounds can be asserted directly instead of inferred.
func recordRunOutcomeBatches(t *testing.T) *[]runOutcomeBatch {
	t.Helper()
	batches := make([]runOutcomeBatch, 0, 4)
	runOutcomesBatchObserver = func(scan string, rows int, bytes int64) {
		batches = append(batches, runOutcomeBatch{Scan: scan, Rows: rows, Bytes: bytes})
	}
	t.Cleanup(func() { runOutcomesBatchObserver = nil })
	return &batches
}

func runBatches(batches []runOutcomeBatch) []runOutcomeBatch {
	filtered := make([]runOutcomeBatch, 0, len(batches))
	for _, batch := range batches {
		if batch.Scan == runOutcomesRunScan {
			filtered = append(filtered, batch)
		}
	}
	return filtered
}

func TestRunOutcomeMigrationAllowsExactlySixteenMiBSelectedData(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "exact-limit.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_exact")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_exact", sessionID: "sess_exact", status: "completed",
		assistantMessageID: "msg_exact", assistantContent: "",
	})
	padAssistantContentToSelectedBytes(t, ctx, database, "run_exact", runOutcomesByteBudget)

	batches := recordRunOutcomeBatches(t)
	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatalf("ApplyMigrations rejected a batch of exactly the byte budget: %v", err)
	}
	got := runBatches(*batches)
	if len(got) != 1 || got[0].Rows != 1 || got[0].Bytes != runOutcomesByteBudget {
		t.Fatalf("run batches = %#v, want one batch of one row at exactly %d bytes", got, runOutcomesByteBudget)
	}
}

func TestRunOutcomeMigrationRejectsOneOversizedRowValueFree(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "oversized.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_oversized")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_oversized", sessionID: "sess_oversized", status: "completed",
		assistantMessageID: "msg_oversized", assistantContent: "",
	})
	padAssistantContentToSelectedBytes(t, ctx, database, "run_oversized", runOutcomesByteBudget+1)

	err := applyRunOutcomesMigration(t, ctx, database)
	if err == nil {
		t.Fatal("ApplyMigrations accepted a row above the byte budget")
	}
	if got := err.Error(); got != "run outcome migration row exceeds byte limit" {
		t.Fatalf("error = %q, want exactly the value-free byte-limit sentinel", got)
	}
}

func TestRunOutcomeMigrationSplitsAtOneHundredTwentyEightRows(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "row-split.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_rows")
	for index := 0; index < 300; index++ {
		seedLegacyRun(t, ctx, database, legacyRun{
			id:        fmt.Sprintf("run_%03d", index),
			sessionID: "sess_rows",
			status:    "queued",
		})
	}

	batches := recordRunOutcomeBatches(t)
	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	var gotRows []int
	for _, batch := range runBatches(*batches) {
		gotRows = append(gotRows, batch.Rows)
	}
	if want := []int{128, 128, 44}; !reflect.DeepEqual(gotRows, want) {
		t.Fatalf("run batch sizes = %v, want %v", gotRows, want)
	}
}

func TestRunOutcomeMigrationSplitsBeforeExceedingSixteenMiB(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "byte-split.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_bytes")
	const perRow = 6 << 20
	for index := 0; index < 3; index++ {
		id := fmt.Sprintf("run_big_%d", index)
		seedLegacyRun(t, ctx, database, legacyRun{
			id: id, sessionID: "sess_bytes", status: "completed",
			assistantMessageID: id + "_msg", assistantContent: "",
		})
		padAssistantContentToSelectedBytes(t, ctx, database, id, perRow)
	}

	batches := recordRunOutcomeBatches(t)
	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	got := runBatches(*batches)
	var gotRows []int
	for _, batch := range got {
		if batch.Bytes > runOutcomesByteBudget {
			t.Fatalf("batch selected %d bytes, want at most %d", batch.Bytes, runOutcomesByteBudget)
		}
		gotRows = append(gotRows, batch.Rows)
	}
	if want := []int{2, 1}; !reflect.DeepEqual(gotRows, want) {
		t.Fatalf("run batch sizes = %v, want %v", gotRows, want)
	}
}

// padAssistantContentToSelectedBytes grows one run's assistant content until
// the migration's own per-row selected-byte measurement hits target exactly.
func padAssistantContentToSelectedBytes(t *testing.T, ctx context.Context, database *DB, runID string, target int64) {
	t.Helper()
	var current int64
	if err := database.QueryRowContext(ctx, `
		SELECT `+runOutcomesRunBytesExpr+`
		FROM agent_runs r
		LEFT JOIN messages m ON m.id = r.assistant_message_id
		WHERE r.id = ?
	`, runID).Scan(&current); err != nil {
		t.Fatal(err)
	}
	padding := target - current
	if padding < 0 {
		t.Fatalf("run %s already selects %d bytes, above the %d byte target", runID, current, target)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE messages
		SET content = ?
		WHERE id = (SELECT assistant_message_id FROM agent_runs WHERE id = ?)
	`, strings.Repeat("x", int(padding)), runID); err != nil {
		t.Fatal(err)
	}
}

func TestRunOutcomeMigrationPreservesEveryRunOwnedChildRow(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "children.db")
	database := openMigratedThroughLegacy(t, ctx, path)
	seedLegacySession(t, ctx, database, "sess_children")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_children", sessionID: "sess_children", status: "completed",
		assistantMessageID: "msg_children", assistantContent: "kept",
		finishedAt: "2026-01-01T00:00:02.000000000Z",
	})
	seedRunOwnedChildren(t, ctx, database, "sess_children", "run_children", "msg_children")

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertRunOwnedChildrenIntact(t, ctx, reopened, "run_children")
	assertNoForeignKeyViolations(t, ctx, reopened)

	// The rebuilt parent must still own its children: a deleted session has to
	// take the whole conversation with it, which is the reason the swap runs
	// with cascades disarmed in the first place.
	if _, err := reopened.ExecContext(ctx, `DELETE FROM sessions WHERE id = 'sess_children'`); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		table string
		query string
	}{
		{table: "agent_runs", query: `SELECT COUNT(*) FROM agent_runs WHERE id = 'run_children'`},
		{table: "agent_run_steps", query: `SELECT COUNT(*) FROM agent_run_steps WHERE run_id = 'run_children'`},
		{table: "jobs", query: `SELECT COUNT(*) FROM jobs WHERE run_id = 'run_children'`},
		{table: "tool_calls", query: `SELECT COUNT(*) FROM tool_calls WHERE run_id = 'run_children'`},
		{table: "approvals", query: `SELECT COUNT(*) FROM approvals WHERE run_id = 'run_children'`},
		{table: "automation_runs", query: `SELECT COUNT(*) FROM automation_runs WHERE run_id = 'run_children'`},
		{table: "send_message_idempotency", query: `SELECT COUNT(*) FROM send_message_idempotency WHERE run_id = 'run_children'`},
		{table: "events", query: `SELECT COUNT(*) FROM events WHERE run_id = 'run_children'`},
	} {
		var remaining int
		if err := reopened.QueryRowContext(ctx, check.query).Scan(&remaining); err != nil {
			t.Fatalf("count %s after session delete: %v", check.table, err)
		}
		if remaining != 0 {
			t.Fatalf("%s rows surviving a session delete = %d, want 0", check.table, remaining)
		}
	}
	assertNoForeignKeyViolations(t, ctx, reopened)
}

// TestRunOutcomeMigrationRejectsMissingDuplicateOrReorderedMarkers guards the
// split itself. The markers decide where a rollback seam sits and which
// statements ran before it, so a file whose markers do not say exactly that is
// rejected rather than executed under a guessed boundary.
func TestRunOutcomeMigrationRejectsMissingDuplicateOrReorderedMarkers(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		sqlText string
	}{
		{name: "missing", sqlText: "SELECT 1;\n-- marker: after-rebuild\nSELECT 2;\n-- marker: after-scrub\n"},
		{name: "duplicate", sqlText: "-- marker: after-rebuild\n-- marker: after-rebuild\n-- marker: after-scrub\n-- marker: after-indexes\n"},
		{name: "reordered", sqlText: "-- marker: after-scrub\n-- marker: after-rebuild\n-- marker: after-indexes\n"},
		{name: "unknown name", sqlText: "-- marker: after-rebuild\n-- marker: after-vacuum\n-- marker: after-indexes\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := migrationSections(runOutcomesMigrationVersion, testCase.sqlText); !errors.Is(err, errMigrationMarkers) {
				t.Fatalf("migrationSections error = %v, want the marker sentinel", err)
			}
		})
	}

	sqlText, err := migrationFS.ReadFile("schema/" + runOutcomesMigrationVersion + ".sql")
	if err != nil {
		t.Fatal(err)
	}
	sections, err := migrationSections(runOutcomesMigrationVersion, string(sqlText))
	if err != nil {
		t.Fatalf("migrationSections on the embedded file: %v", err)
	}
	var markers []string
	for _, section := range sections {
		if section.Marker != "" {
			markers = append(markers, section.Marker)
		}
	}
	if want := []string{"after-rebuild", "after-scrub", "after-indexes"}; !reflect.DeepEqual(markers, want) {
		t.Fatalf("embedded markers = %v, want %v", markers, want)
	}
}

func seedRunOwnedChildren(t *testing.T, ctx context.Context, database *DB, sessionID string, runID string, assistantMessageID string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agent_run_steps (id, run_id, step_index, kind, status, summary, created_at)
		VALUES ('step_child', ?, 1, 'model', 'completed', 'kept step', '2026-01-01T00:00:00.000000000Z');
		INSERT INTO jobs (id, run_id, agent_id, status, payload_json, error_code, error_message, created_at, created_at_ns)
		VALUES ('job_child', ?, 'general_assistant', 'failed', '{}', 'model_error', 'provider text', '2026-01-01T00:00:00.000000000Z', 1);
		INSERT INTO tool_calls (id, run_id, agent_id, server_name, tool_name, args_json, args_hash, status, result_summary, error_code, error_message, created_at)
		VALUES ('call_child', ?, 'general_assistant', 'files', 'files.update', '{"path":"note.txt"}', 'sha256:args', 'failed', 'summary', 'mcp_call_failed', 'tool text', '2026-01-01T00:00:00.000000000Z');
		INSERT INTO approvals (id, run_id, tool_call_id, agent_id, tool_name, args_json, args_hash, status, approval_comment, denial_reason, expires_at, created_at)
		VALUES ('approval_child', ?, 'call_child', 'general_assistant', 'files.update', '{"path":"note.txt"}', 'sha256:args', 'denied', 'looked risky to me', 'I did not want that file touched', '2026-01-01T00:01:00.000000000Z', '2026-01-01T00:00:00.000000000Z');
		INSERT INTO automation_runs (run_id, automation_id, automation_name, allowed_tools_json, fired_at)
		VALUES (?, 'automation_child', 'Nightly', '[]', '2026-01-01T00:00:00.000000000Z');
		INSERT INTO send_message_idempotency (
			idempotency_key, session_id, request_fingerprint, user_message_id, assistant_message_id,
			run_id, job_id, trace_id, queued_event_sequence, created_at)
		VALUES ('idem_child', ?, 'fingerprint', ?, ?, ?, 'job_child', ?, 1, '2026-01-01T00:00:00.000000000Z');
	`, runID, runID, runID, runID, runID, sessionID, runID+"_user", assistantMessageID, runID, "trace_"+runID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO events (id, session_id, run_id, trace_id, sequence, type, payload_json, created_at)
		VALUES ('event_child', ?, ?, ?, 1, 'message.completed',
			'{"messageId":"' || ? || '","content":"kept"}', '2026-01-01T00:00:00.000000000Z')
	`, sessionID, runID, "trace_"+runID, assistantMessageID); err != nil {
		t.Fatal(err)
	}
}

func assertRunOwnedChildrenIntact(t *testing.T, ctx context.Context, database *DB, runID string) {
	t.Helper()
	for _, check := range []struct {
		table string
		query string
	}{
		{table: "agent_run_steps", query: `SELECT COUNT(*) FROM agent_run_steps WHERE run_id = ? AND summary = 'kept step'`},
		{table: "jobs", query: `SELECT COUNT(*) FROM jobs WHERE run_id = ? AND id = 'job_child'`},
		{table: "tool_calls", query: `SELECT COUNT(*) FROM tool_calls WHERE run_id = ? AND result_summary = 'summary'`},
		{table: "approvals", query: `SELECT COUNT(*) FROM approvals WHERE run_id = ? AND id = 'approval_child'`},
		{table: "automation_runs", query: `SELECT COUNT(*) FROM automation_runs WHERE run_id = ?`},
		{table: "send_message_idempotency", query: `SELECT COUNT(*) FROM send_message_idempotency WHERE run_id = ?`},
		{table: "events", query: `SELECT COUNT(*) FROM events WHERE run_id = ? AND id = 'event_child'`},
	} {
		var count int
		if err := database.QueryRowContext(ctx, check.query, runID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", check.table, err)
		}
		if count != 1 {
			t.Fatalf("%s rows for %s = %d, want 1", check.table, runID, count)
		}
	}
}

func assertNoForeignKeyViolations(t *testing.T, ctx context.Context, database *DB) {
	t.Helper()
	var enabled int
	if err := database.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1", enabled)
	}
	rows, err := database.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		t.Fatal("PRAGMA foreign_key_check reported a violation")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

type columnShape struct {
	Type       string
	NotNull    int
	Default    string
	PrimaryKey int
}

func tableColumns(t *testing.T, ctx context.Context, database *DB, table string) map[string]columnShape {
	t.Helper()
	rows, err := database.QueryContext(ctx, "PRAGMA table_info("+quoteSQLiteIdentifier(table)+")")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	columns := map[string]columnShape{}
	for rows.Next() {
		var columnID, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&columnID, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = columnShape{Type: columnType, NotNull: notNull, Default: defaultValue.String, PrimaryKey: primaryKey}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return columns
}

func tableForeignKeys(t *testing.T, ctx context.Context, database *DB, table string) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteSQLiteIdentifier(table)+")")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	keys := make([]string, 0, 4)
	for rows.Next() {
		var id, seq int
		var target, from, onUpdate, onDelete, match string
		var to sql.NullString
		if err := rows.Scan(&id, &seq, &target, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, from+"->"+target+"."+to.String+" delete="+onDelete+" update="+onUpdate)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(keys)
	return keys
}

func tableIndexes(t *testing.T, ctx context.Context, database *DB, table string) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, `
		SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND name IS NOT NULL
	`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	names := make([]string, 0, 8)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	return names
}

func TestRunOutcomeMigrationPreservesEveryExistingRunColumnAndForeignKey(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "shape.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_shape")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_shape", sessionID: "sess_shape", status: "running",
		assistantMessageID: "msg_shape", assistantContent: "",
	})
	beforeColumns := tableColumns(t, ctx, database, "agent_runs")
	beforeKeys := tableForeignKeys(t, ctx, database, "agent_runs")
	beforeIndexes := tableIndexes(t, ctx, database, "agent_runs")
	if _, err := database.ExecContext(ctx,
		`UPDATE agent_runs SET status = 'recovering' WHERE id = 'run_shape'`); err == nil {
		t.Fatal("legacy status CHECK already accepted 'recovering'")
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}

	afterColumns := tableColumns(t, ctx, database, "agent_runs")
	for name, shape := range beforeColumns {
		got, ok := afterColumns[name]
		if !ok {
			t.Fatalf("agent_runs.%s disappeared", name)
		}
		if got != shape {
			t.Fatalf("agent_runs.%s = %#v, want preserved %#v", name, got, shape)
		}
	}
	added := make([]string, 0, 4)
	for name := range afterColumns {
		if _, existed := beforeColumns[name]; !existed {
			added = append(added, name)
		}
	}
	sort.Strings(added)
	wantAdded := []string{"assistant_content_sha256", "outcome_reason", "state_updated_at", "state_version"}
	if !reflect.DeepEqual(added, wantAdded) {
		t.Fatalf("added agent_runs columns = %v, want %v", added, wantAdded)
	}
	if got := tableForeignKeys(t, ctx, database, "agent_runs"); !reflect.DeepEqual(got, beforeKeys) {
		t.Fatalf("agent_runs foreign keys = %v, want preserved %v", got, beforeKeys)
	}
	afterIndexes := tableIndexes(t, ctx, database, "agent_runs")
	for _, name := range beforeIndexes {
		if !containsString(afterIndexes, name) {
			t.Fatalf("agent_runs index %q was not recreated; got %v", name, afterIndexes)
		}
	}
	if !containsString(afterIndexes, "idx_runs_assistant_message_unique") {
		t.Fatalf("agent_runs indexes = %v, want the assistant-message correlation index", afterIndexes)
	}
	// The widened CHECK is the one approved constraint change.
	if _, err := database.ExecContext(ctx,
		`UPDATE agent_runs SET status = 'recovering' WHERE id = 'run_shape'`); err != nil {
		t.Fatalf("migrated status CHECK rejected 'recovering': %v", err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestRunOutcomeMigrationRestoresForeignKeysAfterSuccessAndRollback(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		failPhase string
	}{
		{name: "success"},
		{name: "rollback", failPhase: migrationPhaseAfterIndexes},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "foreign-keys.db")
			database := openMigratedThroughLegacy(t, ctx, path)
			defer database.Close()
			seedLegacySession(t, ctx, database, "sess_fk")
			seedLegacyRun(t, ctx, database, legacyRun{
				id: "run_fk", sessionID: "sess_fk", status: "completed",
				assistantMessageID: "msg_fk", assistantContent: "answer",
			})
			seedRunOwnedChildren(t, ctx, database, "sess_fk", "run_fk", "msg_fk")
			if testCase.failPhase != "" {
				failAtMigrationPhase(t, testCase.failPhase)
			}

			err := applyRunOutcomesMigration(t, ctx, database)
			if testCase.failPhase == "" && err != nil {
				t.Fatal(err)
			}
			if testCase.failPhase != "" && !errors.Is(err, errInjectedPhase) {
				t.Fatalf("ApplyMigrations error = %v, want injected phase failure", err)
			}
			assertNoForeignKeyViolations(t, ctx, database)
			assertRunOwnedChildrenIntact(t, ctx, database, "run_fk")
		})
	}
}

type migratedRunState struct {
	Lifecycle      string
	OutcomeReason  string
	StateVersion   int64
	StateUpdatedAt string
	ContentSHA256  string
}

func readMigratedRunState(t *testing.T, ctx context.Context, database *DB, runID string) migratedRunState {
	t.Helper()
	var state migratedRunState
	if err := database.QueryRowContext(ctx, `
		SELECT status, outcome_reason, state_version, state_updated_at, assistant_content_sha256
		FROM agent_runs WHERE id = ?
	`, runID).Scan(
		&state.Lifecycle, &state.OutcomeReason, &state.StateVersion, &state.StateUpdatedAt,
		&state.ContentSHA256,
	); err != nil {
		t.Fatalf("read migrated run %s: %v", runID, err)
	}
	return state
}

func TestRunOutcomeMigrationBackfillsEveryLifecycleAndOutcome(t *testing.T) {
	testCases := []struct {
		name          string
		run           legacyRun
		wantLifecycle string
		wantReason    string
	}{
		{
			name:          "queued",
			run:           legacyRun{status: "queued"},
			wantLifecycle: "queued", wantReason: "none",
		},
		{
			name:          "running with a live worker",
			run:           legacyRun{status: "running", executionActive: 1, executionState: "delivered"},
			wantLifecycle: "running", wantReason: "none",
		},
		{
			name:          "waiting approval with a live worker",
			run:           legacyRun{status: "waiting_approval", executionActive: 1, executionState: "delivered"},
			wantLifecycle: "waiting_approval", wantReason: "none",
		},
		{
			name:          "completed with displayable content",
			run:           legacyRun{status: "completed", assistantMessageID: "msg", assistantContent: "here you go"},
			wantLifecycle: "completed",
			wantReason:    "none",
		},
		{
			name:          "completed with empty content",
			run:           legacyRun{status: "completed", assistantMessageID: "msg", assistantContent: ""},
			wantLifecycle: "completed", wantReason: "completed_no_content",
		},
		{
			name:          "completed with whitespace-only content",
			run:           legacyRun{status: "completed", assistantMessageID: "msg", assistantContent: "\u00a0\u3000\n"},
			wantLifecycle: "completed", wantReason: "completed_no_content",
		},
		{
			name:          "failed on an expired approval",
			run:           legacyRun{status: "failed", errorCode: "approval_expired", errorMessage: "Approval expired"},
			wantLifecycle: "failed", wantReason: "expired",
		},
		{
			name:          "failed on the context budget",
			run:           legacyRun{status: "failed", errorCode: "context_budget_exceeded", errorMessage: "too long"},
			wantLifecycle: "failed", wantReason: "context_limit",
		},
		{
			name:          "failed on a provider error",
			run:           legacyRun{status: "failed", errorCode: "model_error", errorMessage: "provider said something"},
			wantLifecycle: "failed", wantReason: "provider_failure",
		},
		{
			name:          "failed on a tool call",
			run:           legacyRun{status: "failed", errorCode: "tool_call_failed", errorMessage: "tool text"},
			wantLifecycle: "failed", wantReason: "tool_failure",
		},
		{
			name:          "failed on automation policy",
			run:           legacyRun{status: "failed", errorCode: "automation_tool_not_allowlisted", errorMessage: "not allowed"},
			wantLifecycle: "failed", wantReason: "policy_denied",
		},
		{
			name:          "failed after exhausting retries",
			run:           legacyRun{status: "failed", errorCode: "retries_exhausted", errorMessage: "gave up"},
			wantLifecycle: "failed", wantReason: "retries_exhausted",
		},
		{
			name:          "failed on a job timeout",
			run:           legacyRun{status: "failed", errorCode: "job_timeout", errorMessage: "lease lost"},
			wantLifecycle: "failed", wantReason: "recovery_interrupted",
		},
		{
			name:          "failed with uncertain side effects",
			run:           legacyRun{status: "failed", errorCode: "side_effect_uncertain", errorMessage: "unknown"},
			wantLifecycle: "failed", wantReason: "side_effect_uncertain",
		},
		{
			name:          "failed delivering an approval decision",
			run:           legacyRun{status: "failed", errorCode: "approval_delivery_failed", errorMessage: "no worker"},
			wantLifecycle: "failed", wantReason: "approval_delivery_failed",
		},
		{
			name:          "failed inside the worker runtime",
			run:           legacyRun{status: "failed", errorCode: "runtime_error", errorMessage: "panic text"},
			wantLifecycle: "failed", wantReason: "internal_failure",
		},
		{
			name:          "failed with a code this build cannot interpret",
			run:           legacyRun{status: "failed", errorCode: "some_future_code", errorMessage: "future text"},
			wantLifecycle: "failed", wantReason: "internal_failure",
		},
		{
			name:          "cancelled",
			run:           legacyRun{status: "cancelled", cancellationReason: "client disconnected"},
			wantLifecycle: "cancelled", wantReason: "abandoned",
		},
	}

	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "lifecycles.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_lifecycles")
	for index, testCase := range testCases {
		run := testCase.run
		run.id = fmt.Sprintf("run_lifecycle_%02d", index)
		run.sessionID = "sess_lifecycles"
		if run.assistantMessageID != "" {
			run.assistantMessageID = run.id + "_" + run.assistantMessageID
		}
		seedLegacyRun(t, ctx, database, run)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			runID := fmt.Sprintf("run_lifecycle_%02d", index)
			got := readMigratedRunState(t, ctx, database, runID)
			if got.Lifecycle != testCase.wantLifecycle || got.OutcomeReason != testCase.wantReason {
				t.Fatalf("%s = %s/%s, want %s/%s", runID, got.Lifecycle, got.OutcomeReason,
					testCase.wantLifecycle, testCase.wantReason)
			}
			if got.StateVersion != 1 {
				t.Fatalf("%s state_version = %d, want 1", runID, got.StateVersion)
			}
			if len(got.StateUpdatedAt) != len("2026-01-01T00:00:00.000000000Z") {
				t.Fatalf("%s state_updated_at = %q, want the fixed-width canonical layout", runID, got.StateUpdatedAt)
			}
		})
	}
}

func TestRunOutcomeMigrationBackfillsUncertainOwnershipAsRecovering(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "recovering.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_recovering")
	for _, seed := range []legacyRun{
		{id: "run_running_uncertain", status: "running", executionActive: 1, executionState: "uncertain"},
		{id: "run_running_fenced", status: "running", executionActive: 1, executionState: "fenced"},
		{id: "run_waiting_uncertain", status: "waiting_approval", executionActive: 1, executionState: "uncertain"},
		{id: "run_running_owned", status: "running", executionActive: 1, executionState: "delivered"},
		{id: "run_running_exited", status: "running", executionActive: 0, executionState: "uncertain"},
	} {
		seed.sessionID = "sess_recovering"
		seedLegacyRun(t, ctx, database, seed)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for runID, wantLifecycle := range map[string]string{
		"run_running_uncertain": "recovering",
		"run_running_fenced":    "recovering",
		"run_waiting_uncertain": "recovering",
		"run_running_owned":     "running",
		"run_running_exited":    "running",
	} {
		got := readMigratedRunState(t, ctx, database, runID)
		if got.Lifecycle != wantLifecycle {
			t.Fatalf("%s lifecycle = %q, want %q", runID, got.Lifecycle, wantLifecycle)
		}
		if got.OutcomeReason != "none" {
			t.Fatalf("%s outcome_reason = %q, want none for a nonterminal lifecycle", runID, got.OutcomeReason)
		}
	}
}

func TestRunOutcomeMigrationMapsLegacyClientCancelledToAbandoned(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "abandoned.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_cancel")
	for _, seed := range []legacyRun{
		{id: "run_client_cancelled", status: "cancelled", cancellationReason: "client_cancelled"},
		{id: "run_cancelled_freeform", status: "cancelled", cancellationReason: "user closed the app, probably"},
		{id: "run_cancelled_no_reason", status: "cancelled"},
		{id: "run_failed_client_cancelled", status: "failed", errorCode: "client_cancelled", errorMessage: "stream gone"},
	} {
		seed.sessionID = "sess_cancel"
		seedLegacyRun(t, ctx, database, seed)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for _, runID := range []string{"run_client_cancelled", "run_cancelled_freeform", "run_cancelled_no_reason"} {
		got := readMigratedRunState(t, ctx, database, runID)
		if got.Lifecycle != "cancelled" || got.OutcomeReason != "abandoned" {
			t.Fatalf("%s = %s/%s, want cancelled/abandoned", runID, got.Lifecycle, got.OutcomeReason)
		}
	}
	var userCancelled int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_runs WHERE outcome_reason = 'user_cancelled'`).Scan(&userCancelled); err != nil {
		t.Fatal(err)
	}
	if userCancelled != 0 {
		t.Fatalf("migrated rows claiming user intent = %d, want 0", userCancelled)
	}
}

func TestRunOutcomeMigrationBackfillsContentIdentityAndPresence(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "content.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_content")
	const unicodeContent = "café — \U0001F600 done"
	for _, seed := range []legacyRun{
		{id: "run_unicode", status: "completed", assistantMessageID: "msg_unicode", assistantContent: unicodeContent},
		{id: "run_blank", status: "completed", assistantMessageID: "msg_blank", assistantContent: "   \t\n"},
		{id: "run_zero_width", status: "completed", assistantMessageID: "msg_zero_width", assistantContent: "\u200b"},
		{id: "run_no_message", status: "completed"},
	} {
		seed.sessionID = "sess_content"
		seedLegacyRun(t, ctx, database, seed)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	emptyDigest := runoutcome.ContentSHA256("")
	// Presence is observable through the completed outcome: a completed run
	// with displayable content reads none, and one without reads
	// completed_no_content.
	for _, expectation := range []struct {
		runID      string
		wantSHA    string
		wantReason string
	}{
		{runID: "run_unicode", wantSHA: runoutcome.ContentSHA256(unicodeContent), wantReason: "none"},
		{runID: "run_blank", wantSHA: runoutcome.ContentSHA256("   \t\n"), wantReason: "completed_no_content"},
		{runID: "run_zero_width", wantSHA: runoutcome.ContentSHA256("\u200b"), wantReason: "none"},
		{runID: "run_no_message", wantSHA: emptyDigest, wantReason: "completed_no_content"},
	} {
		got := readMigratedRunState(t, ctx, database, expectation.runID)
		if got.ContentSHA256 != expectation.wantSHA {
			t.Fatalf("%s sha = %q, want %q", expectation.runID, got.ContentSHA256, expectation.wantSHA)
		}
		if got.OutcomeReason != expectation.wantReason {
			t.Fatalf("%s outcome_reason = %q, want %q", expectation.runID, got.OutcomeReason, expectation.wantReason)
		}
	}
	// The exact original bytes survive; only the boolean is derived.
	var stored string
	if err := database.QueryRowContext(ctx,
		`SELECT content FROM messages WHERE id = 'msg_blank'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "   \t\n" {
		t.Fatalf("assistant content = %q, want the original bytes preserved", stored)
	}
}

func TestRunOutcomeMigrationCanonicalizesOffsetAndVariableFractionTimestamps(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "timestamps.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_time")
	for _, seed := range []legacyRun{
		{
			id: "run_offset_finished", status: "completed",
			createdAt: "2026-03-04T00:00:00Z", finishedAt: "2026-03-04T05:06:07.1+02:00",
		},
		{
			id: "run_started_only", status: "running", executionActive: 1, executionState: "delivered",
			createdAt: "2026-03-04T00:00:00.5Z", startedAt: "2026-03-04T01:02:03.000000004Z",
		},
		{
			id: "run_created_only", status: "queued",
			createdAt: "2026-01-02T03:04:05Z",
		},
	} {
		seed.sessionID = "sess_time"
		seedLegacyRun(t, ctx, database, seed)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for runID, want := range map[string]string{
		"run_offset_finished": "2026-03-04T03:06:07.100000000Z",
		"run_started_only":    "2026-03-04T01:02:03.000000004Z",
		"run_created_only":    "2026-01-02T03:04:05.000000000Z",
	} {
		if got := readMigratedRunState(t, ctx, database, runID).StateUpdatedAt; got != want {
			t.Fatalf("%s state_updated_at = %q, want %q", runID, got, want)
		}
	}
}

func TestRunOutcomeMigrationRejectsInvalidTimestampValueFree(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "bad-timestamp.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_bad_time")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_bad_time", sessionID: "sess_bad_time", status: "completed",
		createdAt: "2026-01-01T00:00:00.000000000Z", finishedAt: "2026-01-01 00:00:01",
	})

	err := applyRunOutcomesMigration(t, ctx, database)
	if err == nil {
		t.Fatal("ApplyMigrations accepted an unparseable legacy timestamp")
	}
	if got := err.Error(); got != "invalid persisted timestamp" {
		t.Fatalf("error = %q, want exactly the value-free timestamp sentinel", got)
	}
}

func TestRunOutcomeMigrationCreatesBidirectionalPartialUniqueIndexes(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "indexes.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_unique")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_unique", sessionID: "sess_unique", status: "completed",
		assistantMessageID: "msg_unique", assistantContent: "answer",
	})
	seedLegacyRun(t, ctx, database, legacyRun{id: "run_other", sessionID: "sess_unique", status: "queued"})

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE agent_runs SET assistant_message_id = 'msg_unique' WHERE id = 'run_other'`); err == nil {
		t.Fatal("a second run was allowed to claim one assistant message")
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_second_assistant', 'sess_unique', 'run_unique', 'assistant', 'again', 'text', 900,
			'2026-01-01T00:00:00.000000000Z')
	`); err == nil {
		t.Fatal("a second assistant message was allowed on one run")
	}
	// The index is partial: non-assistant rows and NULL run IDs stay unconstrained.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_tool_same_run', 'sess_unique', 'run_unique', 'tool', 'tool output', 'text', 901,
			'2026-01-01T00:00:00.000000000Z')
	`); err != nil {
		t.Fatalf("partial index rejected a non-assistant row on the same run: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_unlinked_a', 'sess_unique', 'assistant', 'a', 'text', 902, '2026-01-01T00:00:00.000000000Z'),
			('msg_unlinked_b', 'sess_unique', 'assistant', 'b', 'text', 903, '2026-01-01T00:00:00.000000000Z')
	`); err != nil {
		t.Fatalf("partial index rejected assistant rows with no run: %v", err)
	}
}

func TestRunOutcomeMigrationRecreatesEveryExistingRunIndex(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "run-indexes.db"))
	defer database.Close()

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	after := tableIndexes(t, ctx, database, "agent_runs")
	for _, name := range []string{
		"idx_runs_session_created",
		"idx_runs_status",
		"idx_runs_session_execution_active",
		"idx_runs_execution_recovery",
		"idx_runs_execution_recovery_ns",
	} {
		if !containsString(after, name) {
			t.Fatalf("index %q missing after the parent-table swap; got %v", name, after)
		}
	}
}

func TestRunOutcomeMigrationPreservesTerminalRowsWithoutTerminalEvents(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "no-events.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_no_events")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_no_terminal_event", sessionID: "sess_no_events", status: "failed",
		errorCode: "model_timeout", errorMessage: "provider text",
		assistantMessageID: "msg_no_terminal_event", assistantContent: "",
		finishedAt: "2026-01-01T00:00:09.000000000Z",
	})

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	got := readMigratedRunState(t, ctx, database, "run_no_terminal_event")
	if got.Lifecycle != "failed" || got.OutcomeReason != "provider_failure" {
		t.Fatalf("terminal row without events = %s/%s, want failed/provider_failure", got.Lifecycle, got.OutcomeReason)
	}
	if got.StateUpdatedAt != "2026-01-01T00:00:09.000000000Z" {
		t.Fatalf("state_updated_at = %q, want the row's own finished_at", got.StateUpdatedAt)
	}
	var eventCount int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE run_id = 'run_no_terminal_event'`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("migration created %d events for a row that had none, want 0", eventCount)
	}
}

func seedLegacyEvent(t *testing.T, ctx context.Context, database *DB, sessionID string, runID string, sequence int, eventType string, payloadJSON string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO events (id, session_id, run_id, trace_id, sequence, type, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '2026-01-01T00:00:00.000000000Z')
	`, fmt.Sprintf("event_%s_%d", sessionID, sequence), sessionID, nullableText(runID), "trace_"+runID,
		sequence, eventType, payloadJSON); err != nil {
		t.Fatal(err)
	}
}

func readEventPayload(t *testing.T, ctx context.Context, database *DB, sessionID string, sequence int) map[string]any {
	t.Helper()
	var payloadJSON string
	if err := database.QueryRowContext(ctx, `
		SELECT payload_json FROM events WHERE session_id = ? AND sequence = ?
	`, sessionID, sequence).Scan(&payloadJSON); err != nil {
		t.Fatalf("read event %s/%d: %v", sessionID, sequence, err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode event %s/%d payload %q: %v", sessionID, sequence, payloadJSON, err)
	}
	return payload
}

func payloadKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TestRunOutcomeMigrationRewritesThePublicFailureInventory drives every durable
// failure-like event type through the migration and pins the whole rewritten
// payload, not just the absence of one field: an assertion that only removes
// "message" would still pass if a raw reason string were moved to a new key.
func TestRunOutcomeMigrationRewritesThePublicFailureInventory(t *testing.T) {
	testCases := []struct {
		name      string
		eventType string
		payload   string
		wantKeys  []string
		want      map[string]any
	}{
		{
			name:      "agent.run.failed",
			eventType: "agent.run.failed",
			payload:   `{"runId":"run_inventory","code":"model_error","message":"provider said something rude","retryable":true}`,
			wantKeys:  []string{"runState"},
		},
		{
			name:      "agent.run.cancelled",
			eventType: "agent.run.cancelled",
			payload:   `{"runId":"run_inventory","reason":"client went away mid-sentence"}`,
			wantKeys:  []string{"runState"},
		},
		{
			name:      "agent.run.step dispatch retry",
			eventType: "agent.run.step",
			payload:   `{"note":"Retrying (attempt 2 of 3)","attempt":2,"maxAttempts":3,"reason":"model_error"}`,
			wantKeys:  []string{"attempt", "category", "maxAttempts", "stateVersion"},
			want:      map[string]any{"category": "dispatch_retry", "attempt": float64(2), "maxAttempts": float64(3), "stateVersion": float64(1)},
		},
		{
			name:      "agent.run.step recovery retry",
			eventType: "agent.run.step",
			payload:   `{"note":"Retrying (attempt 2 of 3) after the worker became unavailable","attempt":2,"maxAttempts":3,"reason":"worker_unavailable"}`,
			wantKeys:  []string{"attempt", "category", "maxAttempts", "stateVersion"},
			want:      map[string]any{"category": "recovery_retry", "attempt": float64(2), "maxAttempts": float64(3), "stateVersion": float64(1)},
		},
		{
			name:      "agent.run.step give-up notice",
			eventType: "agent.run.step",
			payload:   `{"note":"Gave up after 3 attempts","attempts":3,"maxAttempts":3,"reason":"runtime_error"}`,
			wantKeys:  []string{"attempt", "category", "maxAttempts", "stateVersion"},
			want:      map[string]any{"category": "recovery_exhausted", "attempt": float64(3), "maxAttempts": float64(3), "stateVersion": float64(1)},
		},
		{
			name:      "approval.denied",
			eventType: "approval.denied",
			payload:   `{"approvalId":"appr_1","toolCallId":"call_1","toolName":"files.update","runId":"run_inventory","traceId":"trace_run_inventory","modelToolCallId":"model_call_1","reason":"I did not want that file touched","message":"denied by user"}`,
			wantKeys:  []string{"approvalId", "category", "modelToolCallId", "runId", "toolCallId", "toolName", "traceId"},
			want:      map[string]any{"category": "policy_denied", "approvalId": "appr_1", "toolCallId": "call_1", "toolName": "files.update"},
		},
		{
			name:      "approval.expired",
			eventType: "approval.expired",
			payload:   `{"approvalId":"appr_1","toolName":"files.update","reason":"approval_expired","toolCallId":"call_1"}`,
			wantKeys:  []string{"approvalId", "category", "toolCallId", "toolName"},
			want:      map[string]any{"category": "expired", "approvalId": "appr_1", "toolCallId": "call_1", "toolName": "files.update"},
		},
		{
			name:      "tool.call.failed",
			eventType: "tool.call.failed",
			payload:   `{"toolCallId":"call_1","toolName":"files.update","serverName":"files","error":"EACCES: /Users/somebody/secret.txt"}`,
			wantKeys:  []string{"category", "serverName", "toolCallId", "toolName"},
			want:      map[string]any{"category": "tool_failure", "toolCallId": "call_1", "toolName": "files.update", "serverName": "files"},
		},
		{
			name:      "tool.call.denied",
			eventType: "tool.call.denied",
			payload:   `{"toolCallId":"call_1","toolName":"files.update","serverName":"files","error":"policy text a human wrote"}`,
			wantKeys:  []string{"category", "serverName", "toolCallId", "toolName"},
			want:      map[string]any{"category": "policy_denied", "toolCallId": "call_1", "toolName": "files.update", "serverName": "files"},
		},
	}

	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "inventory.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_inventory")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_inventory", sessionID: "sess_inventory", status: "failed",
		errorCode: "model_error", errorMessage: "provider said something rude",
		assistantMessageID: "msg_inventory", assistantContent: "",
		finishedAt: "2026-01-01T00:00:05.000000000Z",
	})
	for index, testCase := range testCases {
		seedLegacyEvent(t, ctx, database, "sess_inventory", "run_inventory", index+1, testCase.eventType, testCase.payload)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := readEventPayload(t, ctx, database, "sess_inventory", index+1)
			if got := payloadKeys(payload); !reflect.DeepEqual(got, testCase.wantKeys) {
				t.Fatalf("payload keys = %v, want %v", got, testCase.wantKeys)
			}
			for key, want := range testCase.want {
				if payload[key] != want {
					t.Fatalf("payload[%q] = %#v, want %#v", key, payload[key], want)
				}
			}
			if testCase.want != nil {
				return
			}
			state, ok := payload["runState"].(map[string]any)
			if !ok {
				t.Fatalf("runState = %#v, want the canonical projection", payload["runState"])
			}
			wantState := map[string]any{
				"runId":                 "run_inventory",
				"userMessageId":         "run_inventory_user",
				"assistantMessageId":    "msg_inventory",
				"lifecycle":             "failed",
				"outcomeReason":         "provider_failure",
				"stateVersion":          float64(1),
				"stateUpdatedAt":        "2026-01-01T00:00:05.000000000Z",
				"finishedAt":            "2026-01-01T00:00:05.000000000Z",
				"hasDisplayableContent": false,
			}
			if !reflect.DeepEqual(state, wantState) {
				t.Fatalf("runState = %#v, want %#v", state, wantState)
			}
		})
	}
}

func TestRunOutcomeMigrationPreservesNonfailureRunStepNotices(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "notices.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_notices")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_notices", sessionID: "sess_notices", status: "completed",
		assistantMessageID: "msg_notices", assistantContent: "answer",
	})
	const routingNotice = `{"note":"Sending to Remote Helper — this message leaves your machine","externalAgent":"Remote Helper","endpoint":"api.example.com","model":"gpt-4o-mini"}`
	const completedPayload = `{"messageId":"msg_notices","content":"answer"}`
	seedLegacyEvent(t, ctx, database, "sess_notices", "run_notices", 1, "agent.run.step", routingNotice)
	seedLegacyEvent(t, ctx, database, "sess_notices", "run_notices", 2, "message.completed", completedPayload)

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for _, expectation := range []struct {
		sequence int
		want     string
	}{
		{sequence: 1, want: routingNotice},
		{sequence: 2, want: completedPayload},
	} {
		var got string
		if err := database.QueryRowContext(ctx, `
			SELECT payload_json FROM events WHERE session_id = 'sess_notices' AND sequence = ?
		`, expectation.sequence).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != expectation.want {
			t.Fatalf("sequence %d payload = %q, want it preserved byte for byte as %q",
				expectation.sequence, got, expectation.want)
		}
	}
}

const legacyDenialRationale = "I did not want that file touched"
const legacyApprovalComment = "looked risky to me"

func seedLegacyApprovalRationale(t *testing.T, ctx context.Context, database *DB, sessionID string, runID string, sequence int) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO tool_calls (id, run_id, agent_id, server_name, tool_name, args_json, args_hash, status, error_code, error_message, created_at)
		VALUES ('call_rationale', ?, 'general_assistant', 'files', 'files.update', '{"path":"note.txt"}', 'sha256:args', 'denied', 'some_legacy_code', 'raw tool text', '2026-01-01T00:00:00.000000000Z');
		INSERT INTO approvals (id, run_id, tool_call_id, agent_id, tool_name, args_json, args_hash, status, approval_comment, denial_reason, expires_at, created_at)
		VALUES ('approval_rationale', ?, 'call_rationale', 'general_assistant', 'files.update', '{"path":"note.txt"}', 'sha256:args', 'denied', ?, ?, '2026-01-01T00:01:00.000000000Z', '2026-01-01T00:00:00.000000000Z');
		INSERT INTO audit_logs (id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at)
		VALUES ('audit_rationale', ?, 'client', 'actor_1', 'approval.denied', 'approval_rationale', ?, '2026-01-01T00:00:00.000000000Z');
	`, runID, runID, legacyApprovalComment, legacyDenialRationale, runID,
		`{"toolName":"files.update","denial_reason":"`+legacyDenialRationale+`"}`); err != nil {
		t.Fatal(err)
	}
	seedLegacyEvent(t, ctx, database, sessionID, runID, sequence, "approval.denied",
		`{"approvalId":"approval_rationale","toolCallId":"call_rationale","toolName":"files.update","runId":"`+runID+`","traceId":"trace_`+runID+`","reason":"`+legacyDenialRationale+`"}`)
}

func TestRunOutcomeMigrationPreservesApprovalRationaleOnlyInGovernedStorage(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "rationale.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_rationale")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_rationale", sessionID: "sess_rationale", status: "failed",
		errorCode: "approval_delivery_failed", errorMessage: "no worker",
		finishedAt: "2026-01-01T00:00:03.000000000Z",
	})
	seedLegacyApprovalRationale(t, ctx, database, "sess_rationale", "run_rationale", 1)

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	var comment, denial, auditPayload string
	if err := database.QueryRowContext(ctx, `
		SELECT approval_comment, denial_reason FROM approvals WHERE id = 'approval_rationale'
	`).Scan(&comment, &denial); err != nil {
		t.Fatal(err)
	}
	if comment != legacyApprovalComment || denial != legacyDenialRationale {
		t.Fatalf("approval rationale = %q/%q, want it untouched", comment, denial)
	}
	if err := database.QueryRowContext(ctx, `
		SELECT payload_json FROM audit_logs WHERE id = 'audit_rationale'
	`).Scan(&auditPayload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(auditPayload, legacyDenialRationale) {
		t.Fatalf("audit payload = %q, want the governed rationale preserved", auditPayload)
	}
	var argsJSON string
	if err := database.QueryRowContext(ctx, `
		SELECT args_json FROM approvals WHERE id = 'approval_rationale'
	`).Scan(&argsJSON); err != nil {
		t.Fatal(err)
	}
	if argsJSON != `{"path":"note.txt"}` {
		t.Fatalf("approval args = %q, want them untouched", argsJSON)
	}
}

func TestRunOutcomeMigrationNeverCopiesRationaleIntoRunStateOrEvents(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "no-rationale-leak.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_leak")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_leak", sessionID: "sess_leak", status: "failed",
		errorCode: "approval_delivery_failed", errorMessage: legacyDenialRationale,
		finishedAt: "2026-01-01T00:00:03.000000000Z",
	})
	seedLegacyApprovalRationale(t, ctx, database, "sess_leak", "run_leak", 1)

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	rows, err := database.QueryContext(ctx, `SELECT payload_json FROM events`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(payload, legacyDenialRationale) || strings.Contains(payload, legacyApprovalComment) {
			t.Fatalf("event payload %q carries human approval rationale", payload)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var leaked int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agent_runs
		WHERE COALESCE(error_code, '') LIKE '%' || ? || '%'
			OR COALESCE(error_message, '') LIKE '%' || ? || '%'
			OR COALESCE(cancellation_reason, '') LIKE '%' || ? || '%'
			OR outcome_reason LIKE '%' || ? || '%'
	`, legacyDenialRationale, legacyDenialRationale, legacyDenialRationale, legacyDenialRationale).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("agent_runs rows carrying human rationale = %d, want 0", leaked)
	}
}

func TestRunOutcomeMigrationScrubsAllRawDiagnosticColumns(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "scrub.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_scrub")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_allowlisted", sessionID: "sess_scrub", status: "failed",
		errorCode: "model_timeout", errorMessage: "provider stack trace",
		finishedAt: "2026-01-01T00:00:04.000000000Z",
	})
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_unknown_code", sessionID: "sess_scrub", status: "failed",
		errorCode: "leaked_/Users/somebody/secret.txt", errorMessage: "raw text",
		finishedAt: "2026-01-01T00:00:04.000000000Z",
	})
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_external_unknown_code", sessionID: "sess_scrub", status: "failed",
		errorCode: "vendor_specific_thing", errorMessage: "raw text",
		externalAgent: "Remote Helper", finishedAt: "2026-01-01T00:00:04.000000000Z",
	})
	if _, err := database.ExecContext(ctx, `
		INSERT INTO jobs (id, run_id, agent_id, status, payload_json, error_code, error_message, created_at, created_at_ns)
		VALUES ('job_scrub', 'run_unknown_code', 'general_assistant', 'failed', '{}', 'weird_legacy_code', 'job stack trace', '2026-01-01T00:00:00.000000000Z', 1);
		INSERT INTO tool_calls (id, run_id, agent_id, server_name, tool_name, args_json, args_hash, status, result_summary, error_code, error_message, created_at)
		VALUES
			('call_failed', 'run_unknown_code', 'general_assistant', 'files', 'files.update', '{"path":"note.txt"}', 'sha256:args', 'failed', 'kept summary', 'weird_legacy_code', 'tool stack trace', '2026-01-01T00:00:00.000000000Z'),
			('call_denied', 'run_unknown_code', 'general_assistant', 'files', 'files.update', '{"path":"note.txt"}', 'sha256:args', 'denied', 'kept summary', 'weird_legacy_code', 'policy text', '2026-01-01T00:00:00.000000000Z'),
			('call_known', 'run_unknown_code', 'general_assistant', 'files', 'files.update', '{"path":"note.txt"}', 'sha256:args', 'failed', 'kept summary', 'mcp_call_failed', 'tool stack trace', '2026-01-01T00:00:00.000000000Z');
	`); err != nil {
		t.Fatal(err)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"agent_runs", "jobs", "tool_calls"} {
		var remaining int
		if err := database.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM "+quoteSQLiteIdentifier(table)+" WHERE error_message IS NOT NULL").Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining != 0 {
			t.Fatalf("%s rows with a raw error_message = %d, want 0", table, remaining)
		}
	}
	for _, expectation := range []struct {
		query string
		want  string
	}{
		{query: `SELECT error_code FROM agent_runs WHERE id = 'run_allowlisted'`, want: "model_timeout"},
		{query: `SELECT error_code FROM agent_runs WHERE id = 'run_unknown_code'`, want: "internal_failure"},
		{query: `SELECT error_code FROM agent_runs WHERE id = 'run_external_unknown_code'`, want: "provider_failure"},
		{query: `SELECT error_code FROM jobs WHERE id = 'job_scrub'`, want: "internal_failure"},
		{query: `SELECT error_code FROM tool_calls WHERE id = 'call_failed'`, want: "tool_failure"},
		{query: `SELECT error_code FROM tool_calls WHERE id = 'call_denied'`, want: "policy_denied"},
		{query: `SELECT error_code FROM tool_calls WHERE id = 'call_known'`, want: "mcp_call_failed"},
	} {
		var got string
		if err := database.QueryRowContext(ctx, expectation.query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != expectation.want {
			t.Fatalf("%s = %q, want %q", expectation.query, got, expectation.want)
		}
	}
	// Operational state that is not a failure diagnostic stays put.
	var summary string
	if err := database.QueryRowContext(ctx,
		`SELECT result_summary FROM tool_calls WHERE id = 'call_failed'`).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "kept summary" {
		t.Fatalf("tool call result summary = %q, want it untouched", summary)
	}
}

// TestRunOutcomeMigrationRollsBackEveryPhase injects one failure at each seam
// the runner passes through and proves, from a reopened database, that nothing
// from that attempt survived: not the rebuilt table, not the scrub, not the
// indexes, not the rewritten events, and not the migration record.
func TestRunOutcomeMigrationRollsBackEveryPhase(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		phase string
	}{
		{name: "before SQL", phase: migrationPhaseBeforeHook},
		{name: "after table rebuild", phase: migrationPhaseAfterRebuild},
		{name: "after raw-field scrub", phase: migrationPhaseAfterScrub},
		{name: "after index creation", phase: migrationPhaseAfterIndexes},
		{name: "after event rewrite", phase: migrationPhaseAfterHook},
		{name: "before migration-record insert", phase: migrationPhaseBeforeRecord},
		{name: "after migration-record insert", phase: migrationPhaseAfterRecord},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "rollback.db")
			database := openMigratedThroughLegacy(t, ctx, path)
			seedLegacySession(t, ctx, database, "sess_rollback")
			seedLegacyRun(t, ctx, database, legacyRun{
				id: "run_rollback", sessionID: "sess_rollback", status: "failed",
				errorCode: "weird_legacy_code", errorMessage: "provider stack trace",
				assistantMessageID: "msg_rollback", assistantContent: "partial answer",
				finishedAt: "2026-01-01T00:00:07.000000000Z",
			})
			seedRunOwnedChildren(t, ctx, database, "sess_rollback", "run_rollback", "msg_rollback")
			seedLegacyApprovalRationale(t, ctx, database, "sess_rollback", "run_rollback", 3)
			const legacyFailedPayload = `{"runId":"run_rollback","code":"weird_legacy_code","message":"provider stack trace","retryable":true}`
			seedLegacyEvent(t, ctx, database, "sess_rollback", "run_rollback", 2, "agent.run.failed", legacyFailedPayload)

			failAtMigrationPhase(t, testCase.phase)
			err := applyRunOutcomesMigration(t, ctx, database)
			if !errors.Is(err, errInjectedPhase) {
				t.Fatalf("ApplyMigrations error = %v, want the injected phase failure", err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()

			// Old table shape.
			for _, column := range []string{"state_version", "state_updated_at", "outcome_reason", "assistant_content_sha256"} {
				if hasColumn(t, ctx, reopened, "agent_runs", column) {
					t.Fatalf("agent_runs.%s survived a rolled-back migration", column)
				}
			}
			if _, err := reopened.ExecContext(ctx,
				`UPDATE agent_runs SET status = 'recovering' WHERE id = 'run_rollback'`); err == nil {
				t.Fatal("the widened status CHECK survived a rolled-back migration")
			}
			// Old data.
			var errorCode, errorMessage string
			if err := reopened.QueryRowContext(ctx,
				`SELECT error_code, error_message FROM agent_runs WHERE id = 'run_rollback'`).
				Scan(&errorCode, &errorMessage); err != nil {
				t.Fatal(err)
			}
			if errorCode != "weird_legacy_code" || errorMessage != "provider stack trace" {
				t.Fatalf("run diagnostics = %q/%q, want the pre-migration values", errorCode, errorMessage)
			}
			var jobMessage, toolMessage string
			if err := reopened.QueryRowContext(ctx,
				`SELECT error_message FROM jobs WHERE id = 'job_child'`).Scan(&jobMessage); err != nil {
				t.Fatal(err)
			}
			if err := reopened.QueryRowContext(ctx,
				`SELECT error_message FROM tool_calls WHERE id = 'call_child'`).Scan(&toolMessage); err != nil {
				t.Fatal(err)
			}
			if jobMessage != "provider text" || toolMessage != "tool text" {
				t.Fatalf("child diagnostics = %q/%q, want the pre-migration values", jobMessage, toolMessage)
			}
			// Old event JSON.
			var payload string
			if err := reopened.QueryRowContext(ctx,
				`SELECT payload_json FROM events WHERE session_id = 'sess_rollback' AND sequence = 2`).
				Scan(&payload); err != nil {
				t.Fatal(err)
			}
			if payload != legacyFailedPayload {
				t.Fatalf("event payload = %q, want the pre-migration payload", payload)
			}
			// Absent indexes, absent migration row, absent scratch table.
			for _, name := range []string{"idx_runs_assistant_message_unique", "idx_messages_assistant_run_unique"} {
				if sqliteObjectExists(t, ctx, reopened, "index", name) {
					t.Fatalf("index %q survived a rolled-back migration", name)
				}
			}
			if sqliteObjectExists(t, ctx, reopened, "table", runOutcomesBackfillTable) {
				t.Fatalf("%s survived a rolled-back migration", runOutcomesBackfillTable)
			}
			var recorded int
			if err := reopened.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
				runOutcomesMigrationVersion).Scan(&recorded); err != nil {
				t.Fatal(err)
			}
			if recorded != 0 {
				t.Fatalf("migration record count = %d, want 0", recorded)
			}
			// Children, rationale, and referential integrity.
			assertRunOwnedChildrenIntact(t, ctx, reopened, "run_rollback")
			var comment, denial string
			if err := reopened.QueryRowContext(ctx,
				`SELECT approval_comment, denial_reason FROM approvals WHERE id = 'approval_rationale'`).
				Scan(&comment, &denial); err != nil {
				t.Fatal(err)
			}
			if comment != legacyApprovalComment || denial != legacyDenialRationale {
				t.Fatalf("approval rationale = %q/%q, want it untouched", comment, denial)
			}
			assertNoForeignKeyViolations(t, ctx, reopened)
		})
	}
}

func sqliteObjectExists(t *testing.T, ctx context.Context, database *DB, objectType string, name string) bool {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`, objectType, name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count > 0
}

func eventBatches(batches []runOutcomeBatch) []runOutcomeBatch {
	filtered := make([]runOutcomeBatch, 0, len(batches))
	for _, batch := range batches {
		if batch.Scan == runOutcomesEventScan {
			filtered = append(filtered, batch)
		}
	}
	return filtered
}

// legacyEventBytesExpr is the production event measurement minus the two
// columns the migration is about to add. It is therefore a lower bound on what
// the migration will measure, which is all the byte-bound tests need: padding a
// row to N bytes here guarantees the migration sees at least N.
const legacyEventBytesExpr = `
	COALESCE(length(CAST(e.id AS BLOB)), 0) +
	COALESCE(length(CAST(e.type AS BLOB)), 0) +
	COALESCE(length(CAST(e.payload_json AS BLOB)), 0) +
	COALESCE(length(CAST(e.run_id AS BLOB)), 0) +
	COALESCE(length(CAST(r.id AS BLOB)), 0) +
	COALESCE(length(CAST(r.session_id AS BLOB)), 0) +
	COALESCE(length(CAST(r.user_message_id AS BLOB)), 0) +
	COALESCE(length(CAST(r.assistant_message_id AS BLOB)), 0) +
	COALESCE(length(CAST(r.status AS BLOB)), 0) +
	COALESCE(length(CAST(r.finished_at AS BLOB)), 0) +
	COALESCE(length(CAST(m.id AS BLOB)), 0) +
	COALESCE(length(CAST(m.session_id AS BLOB)), 0) +
	COALESCE(length(CAST(m.run_id AS BLOB)), 0) +
	COALESCE(length(CAST(m.role AS BLOB)), 0)`

// seedFailureEventPayload writes one tool.call.failed event whose payload is
// padded until its selected size reaches target.
func seedFailureEventPayload(t *testing.T, ctx context.Context, database *DB, sessionID string, runID string, sequence int, target int64) {
	t.Helper()
	seedLegacyEvent(t, ctx, database, sessionID, runID, sequence, "tool.call.failed",
		`{"toolCallId":"call_1","toolName":"files.update","error":""}`)
	var current int64
	if err := database.QueryRowContext(ctx, `
		SELECT `+legacyEventBytesExpr+`
		FROM events e
		LEFT JOIN agent_runs r ON r.id = e.run_id
		LEFT JOIN messages m ON m.id = r.assistant_message_id
		WHERE e.session_id = ? AND e.sequence = ?
	`, sessionID, sequence).Scan(&current); err != nil {
		t.Fatal(err)
	}
	padding := target - current
	if padding < 0 {
		t.Fatalf("event %d already selects %d bytes, above the %d byte target", sequence, current, target)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE events SET payload_json = ? WHERE session_id = ? AND sequence = ?
	`, `{"toolCallId":"call_1","toolName":"files.update","error":"`+strings.Repeat("x", int(padding))+`"}`,
		sessionID, sequence); err != nil {
		t.Fatal(err)
	}
}

// The event pass is a second, independently written keyset loop over the
// largest table this migration touches, so its bounds are proven separately
// from the run pass rather than assumed to inherit them.
func TestRunOutcomeMigrationSplitsEventScanAtOneHundredTwentyEightRows(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "event-row-split.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_event_rows")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_event_rows", sessionID: "sess_event_rows", status: "failed",
		errorCode: "model_error", finishedAt: "2026-01-01T00:00:01.000000000Z",
	})
	for sequence := 1; sequence <= 300; sequence++ {
		seedLegacyEvent(t, ctx, database, "sess_event_rows", "run_event_rows", sequence, "tool.call.failed",
			`{"toolCallId":"call_1","toolName":"files.update","error":"raw text"}`)
	}

	batches := recordRunOutcomeBatches(t)
	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	var gotRows []int
	for _, batch := range eventBatches(*batches) {
		gotRows = append(gotRows, batch.Rows)
	}
	if want := []int{128, 128, 44}; !reflect.DeepEqual(gotRows, want) {
		t.Fatalf("event batch sizes = %v, want %v", gotRows, want)
	}
}

func TestRunOutcomeMigrationSplitsEventScanBeforeExceedingSixteenMiB(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "event-byte-split.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_event_bytes")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_event_bytes", sessionID: "sess_event_bytes", status: "failed",
		errorCode: "model_error", finishedAt: "2026-01-01T00:00:01.000000000Z",
	})
	for sequence := 1; sequence <= 3; sequence++ {
		seedFailureEventPayload(t, ctx, database, "sess_event_bytes", "run_event_bytes", sequence, 6<<20)
	}

	batches := recordRunOutcomeBatches(t)
	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	var gotRows []int
	for _, batch := range eventBatches(*batches) {
		if batch.Bytes > runOutcomesByteBudget {
			t.Fatalf("event batch selected %d bytes, want at most %d", batch.Bytes, runOutcomesByteBudget)
		}
		gotRows = append(gotRows, batch.Rows)
	}
	if want := []int{2, 1}; !reflect.DeepEqual(gotRows, want) {
		t.Fatalf("event batch sizes = %v, want %v", gotRows, want)
	}
}

func TestRunOutcomeMigrationRejectsOneOversizedEventRowValueFree(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "event-oversized.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_event_oversized")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_event_oversized", sessionID: "sess_event_oversized", status: "failed",
		errorCode: "model_error", finishedAt: "2026-01-01T00:00:01.000000000Z",
	})
	seedFailureEventPayload(t, ctx, database, "sess_event_oversized", "run_event_oversized", 1, runOutcomesByteBudget+1)

	err := applyRunOutcomesMigration(t, ctx, database)
	if err == nil {
		t.Fatal("ApplyMigrations accepted an event row above the byte budget")
	}
	if got := err.Error(); got != "run outcome migration row exceeds byte limit" {
		t.Fatalf("error = %q, want exactly the value-free byte-limit sentinel", got)
	}
}

// TestRunOutcomeMigrationRewritesFailureEventEdgePayloads covers the payload
// shapes no current writer produces but a legacy database may still hold.
func TestRunOutcomeMigrationRewritesFailureEventEdgePayloads(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "event-edges.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_event_edges")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_event_edges", sessionID: "sess_event_edges", status: "failed",
		errorCode: "model_error", finishedAt: "2026-01-01T00:00:01.000000000Z",
	})
	// An event with no run at all: there is no canonical state to project, and
	// the old code/message must still go.
	seedLegacyEvent(t, ctx, database, "sess_event_edges", "", 1, "agent.run.failed",
		`{"runId":"run_that_is_gone","code":"model_error","message":"provider stack trace"}`)
	// Payloads this build cannot parse.
	seedLegacyEvent(t, ctx, database, "sess_event_edges", "run_event_edges", 2, "tool.call.failed",
		`this is not json at all: /Users/somebody/secret.txt`)
	seedLegacyEvent(t, ctx, database, "sess_event_edges", "run_event_edges", 3, "agent.run.step",
		`also not json`)
	// A notice whose counters cannot be represented as a bounded attempt.
	seedLegacyEvent(t, ctx, database, "sess_event_edges", "run_event_edges", 4, "agent.run.step",
		`{"note":"Retrying","attempt":1000000000000,"maxAttempts":1000000000000,"reason":"model_error"}`)

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}

	if got := readEventPayload(t, ctx, database, "sess_event_edges", 1); len(got) != 0 {
		t.Fatalf("orphan failure payload = %#v, want an empty object", got)
	}
	if got := readEventPayload(t, ctx, database, "sess_event_edges", 2); !reflect.DeepEqual(
		got, map[string]any{"category": "tool_failure"}) {
		t.Fatalf("unparseable tool payload = %#v, want the bare safe category", got)
	}
	// An unparseable run-step payload cannot be classified as failure-like, so
	// it keeps the nonfailure-notice treatment and is left for the public read
	// boundary to sanitize. Pinned here so the asymmetry is a decision rather
	// than an accident.
	var stepPayload string
	if err := database.QueryRowContext(ctx,
		`SELECT payload_json FROM events WHERE session_id = 'sess_event_edges' AND sequence = 3`).
		Scan(&stepPayload); err != nil {
		t.Fatal(err)
	}
	if stepPayload != `also not json` {
		t.Fatalf("unparseable run-step payload = %q, want it left untouched", stepPayload)
	}
	if got := readEventPayload(t, ctx, database, "sess_event_edges", 4); !reflect.DeepEqual(
		got, map[string]any{"stateVersion": float64(1), "category": "dispatch_retry"}) {
		t.Fatalf("out-of-range notice = %#v, want the category with no counters", got)
	}
}
