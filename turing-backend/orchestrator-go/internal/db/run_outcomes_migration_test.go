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

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runcorrelation"
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

// TestRunOutcomeMigrationAllowsNullAndSingleMismatchedLegacyCorrelation pins the
// approved fallback for a link that cannot prove ownership: it is neutral, not
// fatal. Only a duplicate is unknowable enough to abort, because only a
// duplicate leaves two candidate claimants and no defensible way to choose. A
// single mismatch — including a mutual pair that disagrees about role or
// session — has exactly one claimant and one honest reading: the link is
// unusable, so nothing is adopted from it and nothing is rewritten.
func TestRunOutcomeMigrationAllowsNullAndSingleMismatchedLegacyCorrelation(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "legacy-correlation.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_legacy_link")
	seedLegacySession(t, ctx, database, "sess_other_half")
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
	// Mutually named, but the message is a tool turn rather than the assistant
	// turn. Nothing else claims either row, so this is unusable, not contested.
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_tool_role_link", sessionID: "sess_legacy_link", status: "completed",
		assistantMessageID: "msg_tool_role", assistantContent: "tool output",
		assistantRole: "tool",
	})
	// Mutually named, but the two rows disagree about which session the turn
	// belongs to. Again exactly one claimant on each side.
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_cross_session_link", sessionID: "sess_legacy_link", status: "completed",
		assistantMessageID: "msg_cross_session", assistantContent: "answer filed elsewhere",
	})
	if _, err := database.ExecContext(ctx,
		`UPDATE messages SET session_id = 'sess_other_half' WHERE id = 'msg_cross_session'`); err != nil {
		t.Fatal(err)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatalf("ApplyMigrations rejected neutral legacy correlation: %v", err)
	}
	emptyDigest := runoutcome.ContentSHA256("")
	for _, want := range []struct {
		runID     string
		lifecycle string
		reason    string
	}{
		{runID: "run_null_link", lifecycle: "failed", reason: "provider_failure"},
		// completed_no_content rather than none is the load-bearing half of
		// this assertion: the stored message has content, and the run only
		// reads as contentless because the unusable link was not followed.
		{runID: "run_mismatched_link", lifecycle: "completed", reason: "completed_no_content"},
		{runID: "run_tool_role_link", lifecycle: "completed", reason: "completed_no_content"},
		{runID: "run_cross_session_link", lifecycle: "completed", reason: "completed_no_content"},
	} {
		got := readMigratedRunState(t, ctx, database, want.runID)
		if got.StateVersion != 1 {
			t.Fatalf("%s state_version = %d, want 1", want.runID, got.StateVersion)
		}
		if got.Lifecycle != want.lifecycle || got.OutcomeReason != want.reason {
			t.Fatalf("%s = %s/%s, want %s/%s", want.runID, got.Lifecycle, got.OutcomeReason,
				want.lifecycle, want.reason)
		}
		// An unusable link proves nothing about ownership, so no content is
		// adopted from it: the identity stays the empty-content digest.
		if got.ContentSHA256 != emptyDigest {
			t.Fatalf("%s adopted content from an unusable legacy link", want.runID)
		}
	}
	// The rows themselves are left exactly as they were found. A migration that
	// "repaired" a legacy half-link would be inventing the ownership fact it
	// just refused to infer.
	for _, check := range []struct {
		query string
		want  string
	}{
		{query: `SELECT run_id FROM messages WHERE id = 'msg_mismatched'`, want: "run_somewhere_else"},
		{query: `SELECT role FROM messages WHERE id = 'msg_tool_role'`, want: "tool"},
		{query: `SELECT session_id FROM messages WHERE id = 'msg_cross_session'`, want: "sess_other_half"},
	} {
		var got string
		if err := database.QueryRowContext(ctx, check.query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != check.want {
			t.Fatalf("%s = %q, want %q left untouched", check.query, got, check.want)
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

// batchesForScan filters the recorded batches down to those from one named
// scan, so each keyset pass's row/byte bounds can be asserted independently.
func batchesForScan(batches []runOutcomeBatch, scan string) []runOutcomeBatch {
	filtered := make([]runOutcomeBatch, 0, len(batches))
	for _, batch := range batches {
		if batch.Scan == scan {
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
	got := batchesForScan(*batches, runOutcomesRunScan)
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
		id := fmt.Sprintf("run_%03d", index)
		seedLegacyRun(t, ctx, database, legacyRun{
			id:                 id,
			sessionID:          "sess_rows",
			status:             "queued",
			assistantMessageID: id + "_assistant",
		})
	}

	batches := recordRunOutcomeBatches(t)
	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	var gotRows []int
	for _, batch := range batchesForScan(*batches, runOutcomesRunScan) {
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
	got := batchesForScan(*batches, runOutcomesRunScan)
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
		{table: "sandbox_artifacts", query: `SELECT COUNT(*) FROM sandbox_artifacts WHERE run_id = 'run_children'`},
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

type egressDecisionRow struct {
	DecisionID                string
	DecisionVersion           int
	RunID                     string
	ChallengeNonce            string
	ChallengeFingerprint      string
	RequestDigest             string
	Provider                  string
	ModelName                 string
	ExternalAgentID           sql.NullString
	ExternalCredentialRefHash string
	Endpoint                  string
	EndpointHost              string
	DataCategoriesJSON        string
	SelectedToolsJSON         string
	SkillSnapshotFingerprint  string
	RecallApplicable          int
	MemoryProfileApplicable   int
	ConsentGrantedAt          string
	RemoteMCPServersJSON      string
}

func readEgressDecisionRow(t *testing.T, ctx context.Context, database *DB, runID string) egressDecisionRow {
	t.Helper()
	var row egressDecisionRow
	if err := database.QueryRowContext(ctx, `
		SELECT decision_id, decision_version, run_id, challenge_nonce,
			challenge_fingerprint, request_digest, provider, model_name,
			external_agent_id, external_credential_ref_hash, endpoint, endpoint_host,
			data_categories_json, selected_tools_json, skill_snapshot_fingerprint,
			recall_applicable, memory_profile_applicable, consent_granted_at,
			remote_mcp_servers_json
		FROM run_egress_decisions
		WHERE run_id = ?
	`, runID).Scan(
		&row.DecisionID, &row.DecisionVersion, &row.RunID, &row.ChallengeNonce,
		&row.ChallengeFingerprint, &row.RequestDigest, &row.Provider, &row.ModelName,
		&row.ExternalAgentID, &row.ExternalCredentialRefHash, &row.Endpoint, &row.EndpointHost,
		&row.DataCategoriesJSON, &row.SelectedToolsJSON, &row.SkillSnapshotFingerprint,
		&row.RecallApplicable, &row.MemoryProfileApplicable, &row.ConsentGrantedAt,
		&row.RemoteMCPServersJSON,
	); err != nil {
		t.Fatal(err)
	}
	return row
}

type idempotencyReplayRow struct {
	SessionID          string
	RequestFingerprint string
	UserMessageID      string
	AssistantMessageID string
	RunID              string
	JobID              string
	TraceID            string
	QueuedSequence     int64
}

func readIdempotencyReplayRow(t *testing.T, ctx context.Context, database *DB, key string) idempotencyReplayRow {
	t.Helper()
	var row idempotencyReplayRow
	if err := database.QueryRowContext(ctx, `
		SELECT session_id, request_fingerprint, user_message_id, assistant_message_id,
			run_id, job_id, trace_id, queued_event_sequence
		FROM send_message_idempotency
		WHERE idempotency_key = ?
	`, key).Scan(
		&row.SessionID, &row.RequestFingerprint, &row.UserMessageID, &row.AssistantMessageID,
		&row.RunID, &row.JobID, &row.TraceID, &row.QueuedSequence,
	); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestRunOutcomeMigrationComposesWithRemoteEgressHistory(t *testing.T) {
	ctx := context.Background()
	database := databaseBeforeMigration(t, ctx, "0014_run_egress_decisions.sql")

	seedLegacySession(t, ctx, database, "sess_egress_denied")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_egress_denied", sessionID: "sess_egress_denied", status: "queued",
		assistantMessageID: "msg_egress_denied",
	})
	if _, err := database.ExecContext(ctx, `
		UPDATE agent_runs
		SET model_provider = 'openai_compatible', model_name = 'gpt-5-mini'
		WHERE id = 'run_egress_denied';
		INSERT INTO jobs (
			id, run_id, agent_id, status, payload_json, created_at, created_at_ns
		) VALUES (
			'job_egress_denied', 'run_egress_denied', 'general_assistant', 'pending',
			'{"secret":"must be scrubbed"}', '2026-01-01T00:00:00.000000000Z', 1
		);
		INSERT INTO send_message_idempotency (
			idempotency_key, session_id, request_fingerprint, user_message_id,
			assistant_message_id, run_id, job_id, trace_id, queued_event_sequence, created_at
		) VALUES (
			'idem_egress_denied', 'sess_egress_denied', 'fingerprint_denied',
			'run_egress_denied_user', 'msg_egress_denied', 'run_egress_denied',
			'job_egress_denied', 'trace_run_egress_denied', 1,
			'2026-01-01T00:00:00.000000000Z'
		)
	`); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"0014_run_egress_decisions.sql",
		"0014_session_deletion_withdrawal.sql",
		"0015_session_lifecycle.sql",
		"0016_mcp_registry.sql",
	} {
		applyMigration(t, ctx, database, name)
	}

	seedLegacySession(t, ctx, database, "sess_egress_preserved")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_egress_preserved", sessionID: "sess_egress_preserved", status: "completed",
		assistantMessageID: "msg_egress_preserved", assistantContent: "kept response",
		finishedAt: "2026-01-01T00:00:02.000000000Z",
	})
	if _, err := database.ExecContext(ctx, `
		UPDATE agent_runs
		SET model_provider = 'openai_compatible', model_name = 'gpt-5-mini'
		WHERE id = 'run_egress_preserved';
		INSERT INTO jobs (
			id, run_id, agent_id, status, payload_json, created_at, created_at_ns
		) VALUES (
			'job_egress_preserved', 'run_egress_preserved', 'general_assistant', 'completed',
			'{}', '2026-01-01T00:00:00.000000000Z', 2
		);
		INSERT INTO send_message_idempotency (
			idempotency_key, session_id, request_fingerprint, user_message_id,
			assistant_message_id, run_id, job_id, trace_id, queued_event_sequence, created_at
		) VALUES (
			'idem_egress_preserved', 'sess_egress_preserved', 'fingerprint_preserved',
			'run_egress_preserved_user', 'msg_egress_preserved', 'run_egress_preserved',
			'job_egress_preserved', 'trace_run_egress_preserved', 7,
			'2026-01-01T00:00:00.000000000Z'
		);
		INSERT INTO run_egress_decisions (
			decision_id, decision_version, run_id, challenge_nonce,
			challenge_fingerprint, request_digest, provider, model_name,
			external_agent_id, external_credential_ref_hash, endpoint, endpoint_host,
			data_categories_json, selected_tools_json, skill_snapshot_fingerprint,
			recall_applicable, memory_profile_applicable, consent_granted_at,
			remote_mcp_servers_json
		) VALUES (
			'decision_preserved', 3, 'run_egress_preserved', 'nonce_preserved',
			'sha256:challenge', 'sha256:request', 'openai_compatible', 'gpt-5-mini',
			NULL, '', 'https://api.example.test/v1', 'api.example.test',
			'["EGRESS_DATA_CATEGORY_CURRENT_MESSAGE"]',
			'["system/system.time"]', 'sha256:skills', 1, 0,
			'2026-01-01T00:00:01.000000000Z',
			'[{"serverName":"remote","endpoint":"https://mcp.example.test","endpointHost":"mcp.example.test"}]'
		)
	`); err != nil {
		t.Fatal(err)
	}

	wantDecision := readEgressDecisionRow(t, ctx, database, "run_egress_preserved")
	wantReplay := readIdempotencyReplayRow(t, ctx, database, "idem_egress_preserved")

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	if got := readEgressDecisionRow(t, ctx, database, "run_egress_preserved"); !reflect.DeepEqual(got, wantDecision) {
		t.Fatalf("egress decision after migration = %#v, want exact preservation %#v", got, wantDecision)
	}
	if got := readIdempotencyReplayRow(t, ctx, database, "idem_egress_preserved"); got != wantReplay {
		t.Fatalf("idempotency replay row after migration = %#v, want %#v", got, wantReplay)
	}

	var (
		runStatus     string
		outcomeReason string
		runCode       sql.NullString
		runMessage    sql.NullString
		stateVersion  int64
		jobStatus     string
		jobCode       sql.NullString
		jobMessage    sql.NullString
		payloadJSON   string
	)
	if err := database.QueryRowContext(ctx, `
		SELECT status, outcome_reason, error_code, error_message, state_version
		FROM agent_runs
		WHERE id = 'run_egress_denied'
	`).Scan(&runStatus, &outcomeReason, &runCode, &runMessage, &stateVersion); err != nil {
		t.Fatal(err)
	}
	if runStatus != "failed" || outcomeReason != "policy_denied" || stateVersion != 1 {
		t.Fatalf("egress-denied run = %s/%s version %d, want failed/policy_denied version 1",
			runStatus, outcomeReason, stateVersion)
	}
	if !runCode.Valid || runCode.String != "egress_decision_required" || runMessage.Valid {
		t.Fatalf("egress-denied run diagnostics = code %v message %v, want fixed code and no message",
			runCode, runMessage)
	}
	if err := database.QueryRowContext(ctx, `
		SELECT status, error_code, error_message
		FROM jobs
		WHERE id = 'job_egress_denied'
	`).Scan(&jobStatus, &jobCode, &jobMessage); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "failed" || !jobCode.Valid || jobCode.String != "egress_decision_required" || jobMessage.Valid {
		t.Fatalf("egress-denied job = %s code %v message %v, want terminal fixed code and no message",
			jobStatus, jobCode, jobMessage)
	}
	if err := database.QueryRowContext(ctx, `
		SELECT payload_json
		FROM events
		WHERE id = 'evt_egress_required_run_egress_denied'
	`).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"egress_decision_required",
		"remote run was queued before explicit egress consent",
		"secret",
	} {
		if strings.Contains(payloadJSON, forbidden) {
			t.Fatalf("rewritten egress-denied payload leaked %q: %s", forbidden, payloadJSON)
		}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	runState, ok := payload["runState"].(map[string]any)
	if len(payload) != 1 || !ok || runState["lifecycle"] != "failed" || runState["outcomeReason"] != "policy_denied" {
		t.Fatalf("rewritten egress-denied payload = %#v, want only failed/policy_denied runState", payload)
	}

	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatalf("idempotent migration replay: %v", err)
	}
	if got := readEgressDecisionRow(t, ctx, database, "run_egress_preserved"); !reflect.DeepEqual(got, wantDecision) {
		t.Fatalf("egress decision after migration replay = %#v, want %#v", got, wantDecision)
	}
	if got := readIdempotencyReplayRow(t, ctx, database, "idem_egress_preserved"); got != wantReplay {
		t.Fatalf("idempotency row after migration replay = %#v, want %#v", got, wantReplay)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT status, state_version FROM agent_runs WHERE id = 'run_egress_denied'`,
	).Scan(&runStatus, &stateVersion); err != nil {
		t.Fatal(err)
	}
	if runStatus != "failed" || stateVersion != 1 {
		t.Fatalf("migration replay revived terminal run as %s version %d", runStatus, stateVersion)
	}
	assertNoForeignKeyViolations(t, ctx, database)
}

func TestRunOutcomeMigrationPreservesPreexistingEgressDecisionInvalidAsPolicyDenial(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, ":memory:")
	defer database.Close()

	seedLegacySession(t, ctx, database, "sess_egress_invalid")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_egress_invalid", sessionID: "sess_egress_invalid", status: "failed",
		assistantMessageID: "msg_egress_invalid", errorCode: "egress_decision_invalid",
		errorMessage: "remote endpoint and consent record did not match",
		finishedAt:   "2026-01-01T00:00:01.000000000Z",
	})
	if _, err := database.ExecContext(ctx, `
		INSERT INTO jobs (
			id, run_id, agent_id, status, payload_json, error_code, error_message,
			created_at, created_at_ns, finished_at
		) VALUES (
			'job_egress_invalid', 'run_egress_invalid', 'general_assistant', 'failed',
			'{}', 'egress_decision_invalid', 'remote endpoint and consent record did not match',
			'2026-01-01T00:00:00.000000000Z', 1, '2026-01-01T00:00:01.000000000Z'
		)
	`); err != nil {
		t.Fatal(err)
	}
	seedLegacyEvent(t, ctx, database, "sess_egress_invalid", "run_egress_invalid", 1, "agent.run.failed",
		`{"code":"egress_decision_invalid","message":"remote endpoint and consent record did not match"}`)

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}

	var runCode, runMessage, outcomeReason, jobCode, jobMessage sql.NullString
	if err := database.QueryRowContext(ctx, `
		SELECT r.error_code, r.error_message, r.outcome_reason, j.error_code, j.error_message
		FROM agent_runs r
		JOIN jobs j ON j.run_id = r.id
		WHERE r.id = 'run_egress_invalid'
	`).Scan(&runCode, &runMessage, &outcomeReason, &jobCode, &jobMessage); err != nil {
		t.Fatal(err)
	}
	if !runCode.Valid || runCode.String != "egress_decision_invalid" ||
		!jobCode.Valid || jobCode.String != "egress_decision_invalid" ||
		!outcomeReason.Valid || outcomeReason.String != "policy_denied" ||
		runMessage.Valid || jobMessage.Valid {
		t.Fatalf("migrated egress mismatch = run %v/%v/%v job %v/%v, want fixed codes, policy denial, and no messages",
			runCode, runMessage, outcomeReason, jobCode, jobMessage)
	}
	var payload string
	if err := database.QueryRowContext(ctx,
		`SELECT payload_json FROM events WHERE run_id = 'run_egress_invalid'`,
	).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "egress_decision_invalid") ||
		strings.Contains(payload, "remote endpoint and consent record did not match") ||
		!strings.Contains(payload, `"outcomeReason":"policy_denied"`) {
		t.Fatalf("migrated failure payload = %s, want redacted policy-denied run state", payload)
	}
	assertNoForeignKeyViolations(t, ctx, database)
}

func TestRunOutcomeMigrationNormalizesLegacyApprovalDenialAsPolicyDenied(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, ":memory:")
	defer database.Close()

	seedLegacySession(t, ctx, database, "sess_policy_legacy")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_policy_legacy", sessionID: "sess_policy_legacy", status: "failed",
		assistantMessageID: "msg_policy_legacy", errorCode: "approval_denied",
		errorMessage: "private denial rationale",
		finishedAt:   "2026-01-01T00:00:01.000000000Z",
	})
	seedLegacyEvent(t, ctx, database, "sess_policy_legacy", "run_policy_legacy", 1, "agent.run.failed",
		`{"code":"approval_denied","message":"private denial rationale"}`)

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}

	var outcomeReason, errorMessage string
	if err := database.QueryRowContext(ctx, `
		SELECT outcome_reason, COALESCE(error_message, '')
		FROM agent_runs
		WHERE id = 'run_policy_legacy'
	`).Scan(&outcomeReason, &errorMessage); err != nil {
		t.Fatal(err)
	}
	if outcomeReason != "policy_denied" {
		t.Fatalf("approval-denied outcome = %q, want policy_denied", outcomeReason)
	}
	if errorMessage != "" {
		t.Fatalf("approval-denied diagnostic survived migration: %q", errorMessage)
	}
	var payload string
	if err := database.QueryRowContext(ctx,
		`SELECT payload_json FROM events WHERE run_id = 'run_policy_legacy'`,
	).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "approval_denied") ||
		strings.Contains(payload, "private denial rationale") ||
		!strings.Contains(payload, `"outcomeReason":"policy_denied"`) {
		t.Fatalf("migrated approval-denied payload = %s, want redacted policy-denied run state", payload)
	}
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
		INSERT INTO sandbox_artifacts (
			id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at)
		VALUES (
			'artifact_child', ?, ?, 'sha256:artifact-child', 'artifact-child.txt',
			'ready', 'delete_on_session_delete', 0, '2026-01-01T00:00:00.000000000Z');
		INSERT INTO send_message_idempotency (
			idempotency_key, session_id, request_fingerprint, user_message_id, assistant_message_id,
			run_id, job_id, trace_id, queued_event_sequence, created_at)
		VALUES ('idem_child', ?, 'fingerprint', ?, ?, ?, 'job_child', ?, 1, '2026-01-01T00:00:00.000000000Z');
	`, runID, runID, runID, runID, runID, sessionID, runID,
		sessionID, runID+"_user", assistantMessageID, runID, "trace_"+runID); err != nil {
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
		{table: "sandbox_artifacts", query: `SELECT COUNT(*) FROM sandbox_artifacts WHERE run_id = ? AND id = 'artifact_child'`},
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

func TestRunOutcomeMigrationRestoresForeignKeysAfterPanic(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, ":memory:")
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_fk_panic")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_fk_panic", sessionID: "sess_fk_panic", status: "completed",
		assistantMessageID: "msg_fk_panic", assistantContent: "answer",
	})
	seedRunOwnedChildren(t, ctx, database, "sess_fk_panic", "run_fk_panic", "msg_fk_panic")
	migrationPhaseHook = func(_ context.Context, _ string, phase string, _ *sql.Tx) error {
		if phase == migrationPhaseAfterIndexes {
			panic("injected migration panic")
		}
		return nil
	}
	t.Cleanup(func() { migrationPhaseHook = nil })

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = applyRunOutcomesMigration(t, ctx, database)
	}()
	if recovered == nil {
		t.Fatal("ApplyMigrations did not propagate the injected panic")
	}
	assertNoForeignKeyViolations(t, ctx, database)
	assertRunOwnedChildrenIntact(t, ctx, database, "run_fk_panic")
}

// mutateAtMigrationPhase installs a phase observer that runs one statement in
// the migration's own transaction at the named seam. Foreign keys are off on
// that pinned connection, which is exactly why the statement can create the
// parent/child inconsistency the precommit check exists to catch: no immediate
// constraint stands in its way.
func mutateAtMigrationPhase(t *testing.T, target string, statement string) {
	t.Helper()
	mutateAtMigrationPhaseStatements(t, target, statement)
}

// mutateAtMigrationPhaseStatements is the multi-statement form, for seams that
// need to disarm a guard before writing the row it would have rejected. The
// statements run in order inside the live migration transaction.
func mutateAtMigrationPhaseStatements(t *testing.T, target string, statements ...string) {
	t.Helper()
	migrationPhaseHook = func(ctx context.Context, _ string, phase string, tx *sql.Tx) error {
		if phase != target {
			return nil
		}
		for _, statement := range statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("seed inconsistency at %s: %w", phase, err)
			}
		}
		return nil
	}
	t.Cleanup(func() { migrationPhaseHook = nil })
}

// assertPreMigrationRunShape proves the database still has exactly its
// pre-migration shape: not the rebuilt table, not the widened status set, not
// the scrub, not the indexes, not the scratch table, and not the migration
// record. Callers pass either a reopened database, after proving a rollback
// survived a restart, or a live one that the migration refused before writing
// anything.
func assertPreMigrationRunShape(
	t *testing.T,
	ctx context.Context,
	database *DB,
	runID string,
	wantErrorCode string,
	wantErrorMessage string,
) {
	t.Helper()
	for _, column := range []string{"state_version", "state_updated_at", "outcome_reason", "assistant_content_sha256"} {
		if hasColumn(t, ctx, database, "agent_runs", column) {
			t.Fatalf("agent_runs.%s survived a rolled-back migration", column)
		}
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE agent_runs SET status = 'recovering' WHERE id = ?`, runID); err == nil {
		t.Fatal("the widened status CHECK survived a rolled-back migration")
	}
	var errorCode, errorMessage string
	if err := database.QueryRowContext(ctx,
		`SELECT error_code, error_message FROM agent_runs WHERE id = ?`, runID).
		Scan(&errorCode, &errorMessage); err != nil {
		t.Fatal(err)
	}
	if errorCode != wantErrorCode || errorMessage != wantErrorMessage {
		t.Fatalf("run diagnostics = %q/%q, want the pre-migration values %q/%q",
			errorCode, errorMessage, wantErrorCode, wantErrorMessage)
	}
	var jobMessage, toolMessage string
	if err := database.QueryRowContext(ctx,
		`SELECT error_message FROM jobs WHERE id = 'job_child'`).Scan(&jobMessage); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT error_message FROM tool_calls WHERE id = 'call_child'`).Scan(&toolMessage); err != nil {
		t.Fatal(err)
	}
	if jobMessage != "provider text" || toolMessage != "tool text" {
		t.Fatalf("child diagnostics = %q/%q, want the pre-migration values", jobMessage, toolMessage)
	}
	for _, name := range []string{"idx_runs_assistant_message_unique", "idx_messages_assistant_run_unique"} {
		if sqliteObjectExists(t, ctx, database, "index", name) {
			t.Fatalf("index %q survived a rolled-back migration", name)
		}
	}
	if sqliteObjectExists(t, ctx, database, "table", runOutcomesBackfillTable) {
		t.Fatalf("%s survived a rolled-back migration", runOutcomesBackfillTable)
	}
	var recorded int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
		runOutcomesMigrationVersion).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 0 {
		t.Fatalf("migration record count = %d, want 0", recorded)
	}
}

// TestRunOutcomeMigrationFailsClosedOnPrecommitForeignKeyViolation exercises the
// last gate before commit. The rebuild deliberately runs with foreign keys off,
// so nothing refuses a statement that separates a child row from its parent;
// PRAGMA foreign_key_check, run with the rebuilt parent and every child already
// in place, is the only thing that can still catch it.
func TestRunOutcomeMigrationFailsClosedOnPrecommitForeignKeyViolation(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		statement string
	}{
		{
			name:      "the parent is gone and its children are orphans",
			statement: `DELETE FROM agent_runs WHERE id = 'run_fk_gate'`,
		},
		{
			name: "a child names a parent that never existed",
			statement: `INSERT INTO agent_run_steps (id, run_id, step_index, kind, status, summary, created_at)
				VALUES ('step_orphan', 'run_never_existed', 2, 'model', 'completed', 'orphan', '2026-01-01T00:00:00.000000000Z')`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "foreign-key-gate.db")
			database := openMigratedThroughLegacy(t, ctx, path)
			seedLegacySession(t, ctx, database, "sess_fk_gate")
			seedLegacyRun(t, ctx, database, legacyRun{
				id: "run_fk_gate", sessionID: "sess_fk_gate", status: "failed",
				errorCode: "weird_legacy_code", errorMessage: "provider stack trace",
				assistantMessageID: "msg_fk_gate", assistantContent: "partial answer",
				finishedAt: "2026-01-01T00:00:07.000000000Z",
			})
			seedRunOwnedChildren(t, ctx, database, "sess_fk_gate", "run_fk_gate", "msg_fk_gate")
			mutateAtMigrationPhase(t, migrationPhaseAfterIndexes, testCase.statement)

			err := applyRunOutcomesMigration(t, ctx, database)
			if !errors.Is(err, errForeignKeyViolation) {
				t.Fatalf("ApplyMigrations error = %v, want the foreign key violation sentinel", err)
			}
			// The check counts violations rather than returning them: a table,
			// a row ID, or a path has no place in a migration error.
			if got := err.Error(); got != "run outcome migration foreign key violation" {
				t.Fatalf("error = %q, want exactly the value-free sentinel", got)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			assertPreMigrationRunShape(t, ctx, reopened, "run_fk_gate", "weird_legacy_code", "provider stack trace")
			assertRunOwnedChildrenIntact(t, ctx, reopened, "run_fk_gate")
			// Nothing the injected statement wrote survived either, and the
			// connection the migration weakened left referential integrity on.
			if sqliteObjectExists(t, ctx, reopened, "table", "agent_run_steps") {
				var orphan int
				if err := reopened.QueryRowContext(ctx,
					`SELECT COUNT(*) FROM agent_run_steps WHERE id = 'step_orphan'`).Scan(&orphan); err != nil {
					t.Fatal(err)
				}
				if orphan != 0 {
					t.Fatalf("orphan step rows = %d, want 0", orphan)
				}
			}
			assertNoForeignKeyViolations(t, ctx, reopened)
		})
	}
}

// TestRunOutcomeMigrationAfterHookValidatesCanonicalStateWithoutTheSchemaCheck
// proves the After hook's canonical validation is load-bearing rather than
// decorative. In normal operation the rebuilt table's CHECK constraints reject
// the same values first, which is exactly why a reviewer can mistake the Go
// pass for dead code. Here the CHECKs are switched off for the duration of one
// injected write — the situation a future ALTER, a relaxed constraint, or a
// direct table edit would create — and the migration must still refuse to
// commit state its readers were promised.
func TestRunOutcomeMigrationAfterHookValidatesCanonicalStateWithoutTheSchemaCheck(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		column string
		value  string
	}{
		{name: "outcome outside the closed vocabulary", column: "outcome_reason", value: `'legacy_unknown'`},
		{name: "version below the first stored one", column: "state_version", value: `0`},
		{name: "variable-width state timestamp", column: "state_updated_at", value: `'2026-01-01T00:00:00Z'`},
		// This value is exactly the canonical 30-character width — the length
		// check alone would accept it — but month 13 is not a valid calendar
		// month, so only a real parse of the value catches it.
		{name: "thirty-character noncanonical state timestamp", column: "state_updated_at", value: `'2026-13-01T00:00:00.000000000Z'`},
		{name: "truncated content digest", column: "assistant_content_sha256", value: `'deadbeef'`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "canonical-gate.db")
			database := openMigratedThroughLegacy(t, ctx, path)
			seedLegacySession(t, ctx, database, "sess_canonical_gate")
			seedLegacyRun(t, ctx, database, legacyRun{
				id: "run_canonical_gate", sessionID: "sess_canonical_gate", status: "failed",
				errorCode: "weird_legacy_code", errorMessage: "provider stack trace",
				assistantMessageID: "msg_canonical_gate", assistantContent: "partial answer",
				finishedAt: "2026-01-01T00:00:07.000000000Z",
			})
			seedRunOwnedChildren(t, ctx, database, "sess_canonical_gate", "run_canonical_gate", "msg_canonical_gate")
			mutateAtMigrationPhaseStatements(t, migrationPhaseAfterIndexes,
				`PRAGMA ignore_check_constraints = ON`,
				`UPDATE agent_runs SET `+testCase.column+` = `+testCase.value+
					` WHERE id = 'run_canonical_gate'`,
				`PRAGMA ignore_check_constraints = OFF`,
			)

			err := applyRunOutcomesMigration(t, ctx, database)
			if !errors.Is(err, errRunOutcomeCanonicalFields) {
				t.Fatalf("ApplyMigrations error = %v, want the canonical state sentinel", err)
			}
			if got := err.Error(); got != "run outcome migration produced invalid canonical state" {
				t.Fatalf("error = %q, want exactly the value-free sentinel", got)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			assertPreMigrationRunShape(t, ctx, reopened, "run_canonical_gate",
				"weird_legacy_code", "provider stack trace")
			assertRunOwnedChildrenIntact(t, ctx, reopened, "run_canonical_gate")
			assertNoForeignKeyViolations(t, ctx, reopened)
		})
	}
}

// TestRunOutcomeMigrationSplitsStateTimestampScanAtOneHundredTwentyEightRows
// proves the After hook's canonical-timestamp pass is itself a bounded keyset
// scan over the rebuilt table, not an unbounded read of it: it is a third,
// independently written loop, alongside the run and event backfill passes,
// so its row bound is proven separately rather than assumed to be inherited.
func TestRunOutcomeMigrationSplitsStateTimestampScanAtOneHundredTwentyEightRows(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "state-timestamp-row-split.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_state_ts_rows")
	for index := 0; index < 300; index++ {
		id := fmt.Sprintf("run_ts_%03d", index)
		seedLegacyRun(t, ctx, database, legacyRun{
			id:                 id,
			sessionID:          "sess_state_ts_rows",
			status:             "queued",
			assistantMessageID: id + "_assistant",
		})
	}

	batches := recordRunOutcomeBatches(t)
	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	var gotRows []int
	for _, batch := range batchesForScan(*batches, runOutcomesStateTimestampScan) {
		gotRows = append(gotRows, batch.Rows)
	}
	if want := []int{128, 128, 44}; !reflect.DeepEqual(gotRows, want) {
		t.Fatalf("state-timestamp batch sizes = %v, want %v", gotRows, want)
	}
}

// TestRunOutcomeMigrationRejectsOneOversizedStateTimestampValueFree proves the
// canonical-timestamp pass measures each row's selected bytes before it is
// read, the same defense in depth every other keyset scan in this migration
// applies. A state_updated_at wide enough to defeat the CHECK constraint is
// only reachable the same way the canonical-gate test reaches one: by
// disabling checks for one injected write, the situation a future ALTER, a
// relaxed constraint, or a direct table edit would create. That row must be
// refused without ever being read into Go, and the whole migration must roll
// back rather than leave a partially rebuilt table behind.
func TestRunOutcomeMigrationRejectsOneOversizedStateTimestampValueFree(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state-timestamp-oversized.db")
	database := openMigratedThroughLegacy(t, ctx, path)
	seedLegacySession(t, ctx, database, "sess_state_ts_oversized")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_ts_oversized", sessionID: "sess_state_ts_oversized", status: "failed",
		errorCode: "weird_legacy_code", errorMessage: "provider stack trace",
		assistantMessageID: "msg_ts_oversized", assistantContent: "partial answer",
		finishedAt: "2026-01-01T00:00:07.000000000Z",
	})
	seedRunOwnedChildren(t, ctx, database, "sess_state_ts_oversized", "run_ts_oversized", "msg_ts_oversized")
	oversized := strings.Repeat("9", int(runOutcomesByteBudget+1))
	migrationPhaseHook = func(ctx context.Context, _ string, phase string, tx *sql.Tx) error {
		if phase != migrationPhaseAfterIndexes {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `PRAGMA ignore_check_constraints = ON`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE agent_runs SET state_updated_at = ? WHERE id = 'run_ts_oversized'`, oversized); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `PRAGMA ignore_check_constraints = OFF`)
		return err
	}
	t.Cleanup(func() { migrationPhaseHook = nil })

	err := applyRunOutcomesMigration(t, ctx, database)
	if err == nil {
		t.Fatal("ApplyMigrations accepted a state_updated_at above the byte budget")
	}
	if got := err.Error(); got != "run outcome migration row exceeds byte limit" {
		t.Fatalf("error = %q, want exactly the value-free byte-limit sentinel", got)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertPreMigrationRunShape(t, ctx, reopened, "run_ts_oversized",
		"weird_legacy_code", "provider stack trace")
	assertRunOwnedChildrenIntact(t, ctx, reopened, "run_ts_oversized")
	assertNoForeignKeyViolations(t, ctx, reopened)
}

func TestRunOutcomeSchemaRejectsNoncanonicalStateWrites(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, ":memory:")
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_schema_guards")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_schema_guards", sessionID: "sess_schema_guards", status: "completed",
		assistantMessageID: "msg_schema_guards",
		finishedAt:         "2026-01-01T00:00:01.000000000Z",
	})
	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name   string
		column string
		value  string
	}{
		{name: "zero version", column: "state_version", value: "0"},
		{name: "negative version", column: "state_version", value: "-1"},
		{name: "variable-width timestamp", column: "state_updated_at", value: "'2026-01-01T00:00:00Z'"},
		{name: "short content digest", column: "assistant_content_sha256", value: "'deadbeef'"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := database.ExecContext(ctx,
				`UPDATE agent_runs SET `+testCase.column+` = `+testCase.value+
					` WHERE id = 'run_schema_guards'`)
			if err == nil {
				t.Fatalf("noncanonical %s write succeeded", testCase.column)
			}
		})
	}
}

// TestRunOutcomeMigrationAfterHookValidatesCorrelationWithoutTheUniqueIndex is
// the same argument for the rebuilt correlation. The partial unique indexes
// created moments earlier would normally reject a second claimant, so the
// injected statement drops the one that would have fired and then creates the
// ambiguity by hand. The After hook is the remaining guard, and it has to fail
// closed with the same value-free conflict the preflight uses.
func TestRunOutcomeMigrationAfterHookValidatesCorrelationWithoutTheUniqueIndex(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		index      string
		statements []string
	}{
		{
			name:  "two runs claim one assistant message",
			index: "idx_runs_assistant_message_unique",
			statements: []string{
				`UPDATE agent_runs SET assistant_message_id = 'msg_correlation_gate'
					WHERE id = 'run_correlation_second'`,
			},
		},
		{
			name:  "two assistant messages claim one run",
			index: "idx_messages_assistant_run_unique",
			statements: []string{
				`INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
					VALUES ('msg_correlation_extra', 'sess_correlation_gate', 'run_correlation_gate',
						'assistant', 'second answer', 'text', 99, '2026-01-01T00:00:00.000000000Z')`,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "correlation-gate.db")
			database := openMigratedThroughLegacy(t, ctx, path)
			seedLegacySession(t, ctx, database, "sess_correlation_gate")
			seedLegacyRun(t, ctx, database, legacyRun{
				id: "run_correlation_gate", sessionID: "sess_correlation_gate", status: "failed",
				errorCode: "weird_legacy_code", errorMessage: "provider stack trace",
				assistantMessageID: "msg_correlation_gate", assistantContent: "partial answer",
				finishedAt: "2026-01-01T00:00:07.000000000Z",
			})
			seedLegacyRun(t, ctx, database, legacyRun{
				id: "run_correlation_second", sessionID: "sess_correlation_gate", status: "completed",
			})
			seedRunOwnedChildren(t, ctx, database, "sess_correlation_gate", "run_correlation_gate",
				"msg_correlation_gate")
			mutateAtMigrationPhaseStatements(t, migrationPhaseAfterIndexes,
				append([]string{`DROP INDEX ` + testCase.index}, testCase.statements...)...)

			err := applyRunOutcomesMigration(t, ctx, database)
			if !errors.Is(err, runcorrelation.ErrConflict) {
				t.Fatalf("ApplyMigrations error = %v, want the correlation conflict sentinel", err)
			}
			if got := err.Error(); got != "run/message correlation conflict" {
				t.Fatalf("error = %q, want exactly the value-free sentinel", got)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			assertPreMigrationRunShape(t, ctx, reopened, "run_correlation_gate",
				"weird_legacy_code", "provider stack trace")
			assertRunOwnedChildrenIntact(t, ctx, reopened, "run_correlation_gate")
			assertNoForeignKeyViolations(t, ctx, reopened)
		})
	}
}

// TestMigrationSectionsSkipStatementFreeSlices covers the other half of the
// marker contract. Rejecting a bad marker sequence is tested above; this is the
// good sequence with nothing between two markers, which the runner must hand to
// the phase seam without handing an empty string to the driver.
func TestMigrationSectionsSkipStatementFreeSlices(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		section string
		want    bool
	}{
		{name: "blank", section: "\n  \n\t\n", want: true},
		{name: "whole-line comments only", section: "-- rebuilt above\n\n--   trailing note\n", want: true},
		{name: "empty", section: "", want: true},
		{name: "a statement", section: "\n-- explains the next line\nUPDATE agent_runs SET error_message = NULL;\n", want: false},
		{name: "a statement with a trailing comment", section: "SELECT 1; -- not a whole-line comment\n", want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := sqlSectionIsEmpty(testCase.section); got != testCase.want {
				t.Fatalf("sqlSectionIsEmpty(%q) = %v, want %v", testCase.section, got, testCase.want)
			}
		})
	}

	// A well-formed file whose middle section carries nothing still produces
	// all three markers, so every rollback seam stays reachable.
	sections, err := migrationSections(runOutcomesMigrationVersion,
		"SELECT 1;\n-- marker: after-rebuild\n-- nothing to scrub here\n-- marker: after-scrub\nSELECT 2;\n-- marker: after-indexes\n")
	if err != nil {
		t.Fatalf("migrationSections: %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(sections))
	}
	if !sqlSectionIsEmpty(sections[1].SQL) {
		t.Fatalf("middle section %q, want it recognized as statement-free", sections[1].SQL)
	}
	if sections[1].Marker != migrationPhaseAfterScrub {
		t.Fatalf("middle marker = %q, want %q", sections[1].Marker, migrationPhaseAfterScrub)
	}
}

// TestRunOutcomeMigrationRefusesAConnectionThatNeverEnforcedForeignKeys pins the
// entry gate. The migration turns foreign keys off and promises to put them
// back; starting from a connection that never had them on would make that
// promise a claim about a state the migration never observed. The pool is
// opened without the enforcement pragma, so every connection it hands out is
// unenforced — no reliance on which connection gets reused.
func TestRunOutcomeMigrationRefusesAConnectionThatNeverEnforcedForeignKeys(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "foreign-keys-off.db")
	legacy := openMigratedThroughLegacy(t, ctx, path)
	seedLegacySession(t, ctx, legacy, "sess_fk_off")
	seedLegacyRun(t, ctx, legacy, legacyRun{
		id: "run_fk_off", sessionID: "sess_fk_off", status: "failed",
		errorCode: "weird_legacy_code", errorMessage: "provider stack trace",
		assistantMessageID: "msg_fk_off", assistantContent: "partial answer",
		finishedAt: "2026-01-01T00:00:07.000000000Z",
	})
	seedRunOwnedChildren(t, ctx, legacy, "sess_fk_off", "run_fk_off", "msg_fk_off")
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	unenforced, err := sql.Open(sqliteDriverName, "file:"+path+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	unenforced.SetMaxOpenConns(1)
	database := &DB{DB: unenforced}
	defer database.Close()
	var enabled int
	if err := database.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 0 for this fixture", enabled)
	}

	err = applyRunOutcomesMigration(t, ctx, database)
	if !errors.Is(err, errForeignKeysNotEnforced) {
		t.Fatalf("ApplyMigrations error = %v, want the unenforced foreign keys sentinel", err)
	}
	if got := err.Error(); got != "run outcome migration requires enforced foreign keys" {
		t.Fatalf("error = %q, want exactly the value-free sentinel", got)
	}
	// This is a refusal, not the fatal restoration path: nothing was weakened,
	// so the database is left open and unchanged.
	assertPreMigrationRunShape(t, ctx, database, "run_fk_off", "weird_legacy_code", "provider stack trace")
	assertRunOwnedChildrenIntact(t, ctx, database, "run_fk_off")
}

// TestRunOutcomeMigrationClosesTheDatabaseWhenForeignKeysCannotBeProvenRestored
// covers the fatal path. The pinned connection goes back to the pool after this
// migration, so a connection left with cascades disarmed would silently weaken
// every later statement in the process. Both cases below are real ways the
// proof fails rather than simulated ones: a connection that has gone away, and
// a restoration that SQLite accepts and then ignores.
func TestRunOutcomeMigrationClosesTheDatabaseWhenForeignKeysCannotBeProvenRestored(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		disturb func(t *testing.T, ctx context.Context, conn *sql.Conn)
	}{
		{
			name: "the connection is gone before the pragma runs",
			disturb: func(t *testing.T, _ context.Context, conn *sql.Conn) {
				if err := conn.Close(); err != nil {
					t.Errorf("close pinned connection: %v", err)
				}
			},
		},
		{
			name: "the pragma is accepted inside a transaction and does nothing",
			disturb: func(t *testing.T, ctx context.Context, conn *sql.Conn) {
				// PRAGMA foreign_keys is a documented no-op while a
				// transaction is open, so the restoration silently does not
				// take and only the re-read can notice.
				if _, err := conn.ExecContext(ctx, `BEGIN`); err != nil {
					t.Errorf("begin on pinned connection: %v", err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "foreign-keys-unrestorable.db")
			database := openMigratedThroughLegacy(t, ctx, path)
			defer database.Close()
			seedLegacySession(t, ctx, database, "sess_fk_fatal")
			seedLegacyRun(t, ctx, database, legacyRun{
				id: "run_fk_fatal", sessionID: "sess_fk_fatal", status: "completed",
				assistantMessageID: "msg_fk_fatal", assistantContent: "answer",
			})
			seedRunOwnedChildren(t, ctx, database, "sess_fk_fatal", "run_fk_fatal", "msg_fk_fatal")
			disturb := testCase.disturb
			migrationPinnedConnectionHook = func(ctx context.Context, conn *sql.Conn) {
				disturb(t, ctx, conn)
			}
			t.Cleanup(func() { migrationPinnedConnectionHook = nil })

			err := applyRunOutcomesMigration(t, ctx, database)
			if !errors.Is(err, errForeignKeysUnrestorable) {
				t.Fatalf("ApplyMigrations error = %v, want the unrestorable foreign keys sentinel", err)
			}
			if got := err.Error(); got != "run outcome migration could not restore foreign keys" {
				t.Fatalf("error = %q, want exactly the value-free sentinel", got)
			}
			// Startup fails and the pool is gone: a cascade-disabled
			// connection is never handed back out.
			pingErr := database.PingContext(ctx)
			if pingErr == nil {
				t.Fatal("the database is still usable after an unprovable foreign key restoration")
			}
			if !strings.Contains(pingErr.Error(), "database is closed") {
				t.Fatalf("ping error = %v, want a closed database", pingErr)
			}
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
		// The nonterminal shapes carry a well-formed assistant link. A
		// nonterminal run is a promise of future work and every canonical
		// transition validates that link, so one without it does not migrate at
		// all; seeding it here would be asserting an outcome for a row the
		// migration refuses.
		{
			name:          "queued",
			run:           legacyRun{status: "queued", assistantMessageID: "msg"},
			wantLifecycle: "queued", wantReason: "none",
		},
		{
			name: "running with a live worker",
			run: legacyRun{
				status: "running", executionActive: 1, executionState: "delivered",
				assistantMessageID: "msg",
			},
			wantLifecycle: "running", wantReason: "none",
		},
		{
			name: "waiting approval with a live worker",
			run: legacyRun{
				status: "waiting_approval", executionActive: 1, executionState: "delivered",
				assistantMessageID: "msg",
			},
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
			// The one legacy shape whose stored status is not its honest
			// lifecycle: older call sites filed the client-disconnect signal
			// as a run failure.
			name:          "failed under the client cancellation transport code",
			run:           legacyRun{status: "failed", errorCode: "client_cancelled", errorMessage: "stream gone"},
			wantLifecycle: "cancelled", wantReason: "abandoned",
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

// TestRunOutcomeSchemaEnforcesTheOutcomeVocabularyAndNotThePairing pins exactly
// what the rebuilt table promises, because a reader who believes in a check
// that does not exist will stop writing the one that does. The closed
// outcome_reason vocabulary is a real column constraint. The lifecycle/outcome
// pairing is not: existing writers still terminalize a run without touching
// outcome_reason, so a cross-column check added here would reject writes this
// change does not own. When the versioned transitions take ownership of the
// pair, this test is the thing that changes with them.
func TestRunOutcomeSchemaEnforcesTheOutcomeVocabularyAndNotThePairing(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "outcome-vocabulary.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_vocabulary")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_vocabulary", sessionID: "sess_vocabulary", status: "completed",
		assistantMessageID: "msg_vocabulary", assistantContent: "answer",
	})
	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}

	if _, err := database.ExecContext(ctx,
		`UPDATE agent_runs SET outcome_reason = 'provider_said_something' WHERE id = 'run_vocabulary'`); err == nil {
		t.Fatal("the schema accepted an outcome_reason outside the closed vocabulary")
	}
	// In the vocabulary, but paired with a lifecycle the approved matrix would
	// never produce. The schema takes it, and saying so plainly is better than
	// a comment that implies a guarantee nothing is providing.
	if _, err := database.ExecContext(ctx,
		`UPDATE agent_runs SET outcome_reason = 'provider_failure' WHERE id = 'run_vocabulary'`); err != nil {
		t.Fatalf("the schema rejected a lifecycle/outcome pair it does not constrain: %v", err)
	}
	state := readMigratedRunState(t, ctx, database, "run_vocabulary")
	if state.Lifecycle != "completed" || state.OutcomeReason != "provider_failure" {
		t.Fatalf("run = %s/%s, want completed/provider_failure to persist unconstrained",
			state.Lifecycle, state.OutcomeReason)
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
		// Every row here is nonterminal, so each needs the well-formed
		// assistant link a nonterminal migration requires.
		seed.assistantMessageID = seed.id + "_assistant"
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

// TestRunOutcomeMigrationMapsLegacyClientCancelledToAbandoned locks the
// ambiguous-disconnect mapping in both of the shapes legacy rows recorded it.
// The current transport writes one client_cancelled signal for a deliberate
// stop and for an unkeyed transport loss, and older call sites filed that same
// signal as a failure rather than a cancellation. Neither shape can prove user
// intent, and neither is honestly an internal failure, so both canonicalize to
// cancelled/abandoned and USER_CANCELLED stays reserved.
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
	// Rows whose failure is a real failure, and rows whose freeform text merely
	// sounds like a cancellation. Neither may be dragged into the mapping: the
	// signal is the server-chosen code, never prose.
	for _, seed := range []legacyRun{
		{id: "run_failed_provider", status: "failed", errorCode: "model_error", errorMessage: "client_cancelled"},
		{id: "run_failed_freeform_cancel", status: "failed", errorMessage: "the user cancelled, I think"},
	} {
		seed.sessionID = "sess_cancel"
		seedLegacyRun(t, ctx, database, seed)
	}
	seedLegacyEvent(t, ctx, database, "sess_cancel", "run_failed_client_cancelled", 1, "agent.run.failed",
		`{"runId":"run_failed_client_cancelled","code":"client_cancelled","message":"stream gone"}`)

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for _, runID := range []string{
		"run_client_cancelled", "run_cancelled_freeform", "run_cancelled_no_reason",
		// Seeded as failed with the transport code. Canonicalizing it as
		// failed/internal_failure would report a system fault for a client
		// that simply went away.
		"run_failed_client_cancelled",
	} {
		got := readMigratedRunState(t, ctx, database, runID)
		if got.Lifecycle != "cancelled" || got.OutcomeReason != "abandoned" {
			t.Fatalf("%s = %s/%s, want cancelled/abandoned", runID, got.Lifecycle, got.OutcomeReason)
		}
	}
	for _, expectation := range []struct {
		runID  string
		reason string
	}{
		{runID: "run_failed_provider", reason: "provider_failure"},
		{runID: "run_failed_freeform_cancel", reason: "internal_failure"},
	} {
		got := readMigratedRunState(t, ctx, database, expectation.runID)
		if got.Lifecycle != "failed" || got.OutcomeReason != expectation.reason {
			t.Fatalf("%s = %s/%s, want failed/%s", expectation.runID, got.Lifecycle, got.OutcomeReason,
				expectation.reason)
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
	// The reclassification has to reach the durable event too. The run row and
	// the event payload are written by different passes, and a client reading
	// history sees the payload: leaving a failed/internal_failure projection
	// there would tell the user the system broke, whichever way the run row
	// reads.
	payload := readEventPayload(t, ctx, database, "sess_cancel", 1)
	state, ok := payload["runState"].(map[string]any)
	if !ok {
		t.Fatalf("runState = %#v, want the canonical projection", payload["runState"])
	}
	if state["lifecycle"] != "cancelled" || state["outcomeReason"] != "abandoned" {
		t.Fatalf("event runState = %v/%v, want cancelled/abandoned",
			state["lifecycle"], state["outcomeReason"])
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
			assistantMessageID: "msg_started_only",
		},
		{
			id: "run_created_only", status: "queued",
			createdAt:          "2026-01-02T03:04:05Z",
			assistantMessageID: "msg_created_only",
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

// TestRunOutcomeMigrationSelectsStateUpdatedAtByLifecyclePrecedence pins the
// approved rule as precedence, not recency. The two are indistinguishable on
// well-ordered rows, so every fixture here is deliberately skewed: the more
// authoritative field is the older one. Legacy rows were written by several
// call sites with no shared clock discipline, and "latest wins" would let a
// skewed started_at outrank the finish that actually ended the run.
func TestRunOutcomeMigrationSelectsStateUpdatedAtByLifecyclePrecedence(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "precedence.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_precedence")
	for _, seed := range []legacyRun{
		// finished_at outranks a started_at recorded later.
		{
			id: "run_finish_before_start", status: "completed",
			createdAt:  "2026-05-01T00:00:00.000000000Z",
			startedAt:  "2026-05-01T09:00:00.000000000Z",
			finishedAt: "2026-05-01T02:00:00.000000000Z",
		},
		// started_at outranks a created_at recorded later, with no finish.
		{
			id: "run_start_before_create", status: "running",
			executionActive: 1, executionState: "delivered",
			createdAt:          "2026-05-01T08:00:00.000000000Z",
			startedAt:          "2026-05-01T03:00:00.000000000Z",
			assistantMessageID: "msg_start_before_create",
		},
		// finished_at outranks both of the later timestamps beneath it.
		{
			id: "run_finish_before_both", status: "cancelled",
			createdAt:  "2026-05-01T07:00:00.000000000Z",
			startedAt:  "2026-05-01T10:00:00.000000000Z",
			finishedAt: "2026-05-01T01:00:00.000000000Z",
		},
	} {
		seed.sessionID = "sess_precedence"
		seedLegacyRun(t, ctx, database, seed)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for runID, want := range map[string]string{
		"run_finish_before_start": "2026-05-01T02:00:00.000000000Z",
		"run_start_before_create": "2026-05-01T03:00:00.000000000Z",
		"run_finish_before_both":  "2026-05-01T01:00:00.000000000Z",
	} {
		if got := readMigratedRunState(t, ctx, database, runID).StateUpdatedAt; got != want {
			t.Fatalf("%s state_updated_at = %q, want %q from lifecycle precedence rather than the latest value",
				runID, got, want)
		}
	}
}

// TestRunOutcomeMigrationIgnoresEmptyLifecycleTimestamps keeps precedence from
// being satisfied by a present-but-empty column. SQLite stores the empty string
// as a non-NULL TEXT value, so a legacy writer that cleared a field left
// something that is Valid and useless; selecting it would fail the parse
// instead of falling through to the field that actually holds the time.
func TestRunOutcomeMigrationIgnoresEmptyLifecycleTimestamps(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "empty-timestamps.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_empty_time")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_empty_time", sessionID: "sess_empty_time", status: "completed",
		createdAt: "2026-06-07T08:09:10.000000000Z",
		startedAt: "2026-06-07T08:09:11.000000000Z",
	})
	if _, err := database.ExecContext(ctx,
		`UPDATE agent_runs SET finished_at = '' WHERE id = 'run_empty_time'`); err != nil {
		t.Fatal(err)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	const want = "2026-06-07T08:09:11.000000000Z"
	if got := readMigratedRunState(t, ctx, database, "run_empty_time").StateUpdatedAt; got != want {
		t.Fatalf("state_updated_at = %q, want %q from the next field in precedence", got, want)
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
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_other", sessionID: "sess_unique", status: "queued",
		assistantMessageID: "msg_other",
	})

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

// TestRunOutcomeMigrationOmitsUnprovenAssistantIdentityFromRunState covers the
// run-terminal events whose whole payload is the canonical projection. The
// inventory test above only exercises a link that validates, so nothing there
// notices if the rewrite starts publishing an assistant message ID straight off
// the run row. These rows are mutually named — each side points at the other,
// so neither the duplicate preflight nor the partial unique indexes object —
// but they disagree about role or session, which means ownership was never
// proven. The safe projection therefore has to drop assistantMessageId while
// keeping every other canonical field it does know.
func TestRunOutcomeMigrationOmitsUnprovenAssistantIdentityFromRunState(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "event-correlation.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_event_link")
	seedLegacySession(t, ctx, database, "sess_event_elsewhere")
	// Mutually named, but the message is a tool turn rather than the assistant
	// turn this run would own.
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_role_link", sessionID: "sess_event_link", status: "failed",
		errorCode: "model_error", errorMessage: "provider said something rude",
		assistantMessageID: "msg_event_role", assistantContent: "tool output",
		assistantRole: "tool", finishedAt: "2026-01-01T00:00:07.000000000Z",
	})
	// Mutually named, but the two rows disagree about the session.
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_session_link", sessionID: "sess_event_link", status: "cancelled",
		cancellationReason: "client_cancelled",
		assistantMessageID: "msg_event_session", assistantContent: "answer filed elsewhere",
		finishedAt: "2026-01-01T00:00:09.000000000Z",
	})
	if _, err := database.ExecContext(ctx,
		`UPDATE messages SET session_id = 'sess_event_elsewhere' WHERE id = 'msg_event_session'`); err != nil {
		t.Fatal(err)
	}
	seedLegacyEvent(t, ctx, database, "sess_event_link", "run_role_link", 1, "agent.run.failed",
		`{"runId":"run_role_link","code":"model_error","message":"provider said something rude"}`)
	seedLegacyEvent(t, ctx, database, "sess_event_link", "run_session_link", 2, "agent.run.cancelled",
		`{"runId":"run_session_link","reason":"client went away mid-sentence"}`)

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for _, expectation := range []struct {
		sequence  int
		wantState map[string]any
	}{
		{
			sequence: 1,
			wantState: map[string]any{
				"runId":                 "run_role_link",
				"userMessageId":         "run_role_link_user",
				"lifecycle":             "failed",
				"outcomeReason":         "provider_failure",
				"stateVersion":          float64(1),
				"stateUpdatedAt":        "2026-01-01T00:00:07.000000000Z",
				"finishedAt":            "2026-01-01T00:00:07.000000000Z",
				"hasDisplayableContent": false,
			},
		},
		{
			sequence: 2,
			wantState: map[string]any{
				"runId":                 "run_session_link",
				"userMessageId":         "run_session_link_user",
				"lifecycle":             "cancelled",
				"outcomeReason":         "abandoned",
				"stateVersion":          float64(1),
				"stateUpdatedAt":        "2026-01-01T00:00:09.000000000Z",
				"finishedAt":            "2026-01-01T00:00:09.000000000Z",
				"hasDisplayableContent": false,
			},
		},
	} {
		payload := readEventPayload(t, ctx, database, "sess_event_link", expectation.sequence)
		if got := payloadKeys(payload); !reflect.DeepEqual(got, []string{"runState"}) {
			t.Fatalf("sequence %d payload keys = %v, want only runState", expectation.sequence, got)
		}
		state, ok := payload["runState"].(map[string]any)
		if !ok {
			t.Fatalf("sequence %d runState = %#v, want the canonical projection",
				expectation.sequence, payload["runState"])
		}
		// Named separately from the DeepEqual below so a regression that
		// publishes the unproven ID reports what it actually did.
		if _, published := state["assistantMessageId"]; published {
			t.Fatalf("sequence %d published assistantMessageId = %#v from an unproven link",
				expectation.sequence, state["assistantMessageId"])
		}
		if !reflect.DeepEqual(state, expectation.wantState) {
			t.Fatalf("sequence %d runState = %#v, want %#v", expectation.sequence, state, expectation.wantState)
		}
	}
	// The run rows keep their legacy pointer: refusing to publish an unproven
	// link is a statement about the projection, not a repair of the data.
	for _, check := range []struct {
		runID string
		want  string
	}{
		{runID: "run_role_link", want: "msg_event_role"},
		{runID: "run_session_link", want: "msg_event_session"},
	} {
		var got string
		if err := database.QueryRowContext(ctx,
			`SELECT assistant_message_id FROM agent_runs WHERE id = ?`, check.runID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != check.want {
			t.Fatalf("%s assistant_message_id = %q, want %q left untouched", check.runID, got, check.want)
		}
	}
}

// TestRunOutcomeMigrationCanonicalizesFinishedAtInRewrittenRunState covers the
// one timestamp the projection publishes straight off the legacy row.
// state_updated_at is derived and re-rendered at canonical fixed width, but
// finished_at is copied verbatim by the rebuild, so an offset-bearing or
// variable-fraction legacy value reaches the newly written public payload
// unchanged. That is text a client text-compares: two runs that finished at the
// same instant publish different strings, and one payload then carries two
// renderings of a single moment — a UTC nanosecond stateUpdatedAt next to a
// local-offset finishedAt that reads as hours apart. The rewritten payload is
// Task 3's deliverable, so it has to speak the canonical format even though the
// legacy column keeps its own text.
func TestRunOutcomeMigrationCanonicalizesFinishedAtInRewrittenRunState(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "finished-at.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_finished_at")
	// Both rows name the same instant. Only the rendering differs, which is
	// exactly what a text-compared public field must not preserve.
	for _, seed := range []legacyRun{
		{
			id: "run_offset_finish", status: "failed",
			errorCode: "model_error", errorMessage: "provider said something rude",
			createdAt: "2026-03-04T00:00:00.000000000Z", finishedAt: "2026-03-04T05:06:07.1+02:00",
		},
		{
			id: "run_short_fraction_finish", status: "cancelled",
			cancellationReason: "user pressed stop",
			createdAt:          "2026-03-04T00:00:00.000000000Z", finishedAt: "2026-03-04T03:06:07.100Z",
		},
	} {
		seed.sessionID = "sess_finished_at"
		seedLegacyRun(t, ctx, database, seed)
	}
	seedLegacyEvent(t, ctx, database, "sess_finished_at", "run_offset_finish", 1, "agent.run.failed",
		`{"runId":"run_offset_finish","code":"model_error","message":"provider said something rude"}`)
	seedLegacyEvent(t, ctx, database, "sess_finished_at", "run_short_fraction_finish", 2, "agent.run.cancelled",
		`{"runId":"run_short_fraction_finish","reason":"user pressed stop"}`)

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	const wantCanonical = "2026-03-04T03:06:07.100000000Z"
	published := make([]string, 0, 2)
	for _, sequence := range []int{1, 2} {
		payload := readEventPayload(t, ctx, database, "sess_finished_at", sequence)
		state, ok := payload["runState"].(map[string]any)
		if !ok {
			t.Fatalf("sequence %d runState = %#v, want the canonical projection", sequence, payload["runState"])
		}
		got, _ := state["finishedAt"].(string)
		if got != wantCanonical {
			t.Fatalf("sequence %d runState.finishedAt = %#v, want the canonical %q",
				sequence, state["finishedAt"], wantCanonical)
		}
		// The projection must not report one instant twice in two formats.
		if updated, _ := state["stateUpdatedAt"].(string); updated != got {
			t.Fatalf("sequence %d publishes stateUpdatedAt %q beside finishedAt %q for one instant",
				sequence, updated, got)
		}
		published = append(published, got)
	}
	if published[0] != published[1] {
		t.Fatalf("equal instants published as %q and %q", published[0], published[1])
	}
	// Legacy storage is deliberately untouched: canonicalizing the public
	// payload is a statement about what the migration newly writes, not a
	// rewrite of the column the rebuild copied verbatim.
	for runID, want := range map[string]string{
		"run_offset_finish":         "2026-03-04T05:06:07.1+02:00",
		"run_short_fraction_finish": "2026-03-04T03:06:07.100Z",
	} {
		var got string
		if err := database.QueryRowContext(ctx,
			`SELECT finished_at FROM agent_runs WHERE id = ?`, runID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s finished_at = %q, want the legacy text %q left in place", runID, got, want)
		}
	}
}

// TestRunOutcomeMigrationFailsClosedOnUncanonicalizableFinishedAt proves the
// projection's canonicalization is a gate rather than a best effort. The Before
// hook already rejects an unparseable finished_at, so the only way to reach the
// event pass with one is to write it after that hook ran — which is exactly the
// defense-in-depth case worth pinning: the rewrite must never fall back to
// publishing the raw text. The failure has to name the class without the value
// and take the whole transaction with it.
func TestRunOutcomeMigrationFailsClosedOnUncanonicalizableFinishedAt(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "finished-at-closed.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_finished_closed")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_finished_closed", sessionID: "sess_finished_closed", status: "failed",
		errorCode: "model_error", errorMessage: "provider said something rude",
		createdAt: "2026-01-01T00:00:00.000000000Z", finishedAt: "2026-01-01T00:00:05.000000000Z",
	})
	const legacyPayload = `{"runId":"run_finished_closed","code":"model_error","message":"provider said something rude"}`
	seedLegacyEvent(t, ctx, database, "sess_finished_closed", "run_finished_closed", 1,
		"agent.run.failed", legacyPayload)
	// After every SQL section, and therefore after the Before hook that would
	// otherwise have caught it.
	mutateAtMigrationPhase(t, migrationPhaseAfterIndexes,
		`UPDATE agent_runs SET finished_at = '2026-01-01 00:00:05' WHERE id = 'run_finished_closed'`)

	err := applyRunOutcomesMigration(t, ctx, database)
	if err == nil {
		t.Fatal("ApplyMigrations published a finished_at it could not canonicalize")
	}
	if got := err.Error(); got != "invalid persisted timestamp" {
		t.Fatalf("error = %q, want exactly the value-free timestamp sentinel", got)
	}
	var payload string
	if err := database.QueryRowContext(ctx,
		`SELECT payload_json FROM events WHERE id = 'event_sess_finished_closed_1'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != legacyPayload {
		t.Fatalf("event payload = %q, want the pre-migration text after a rolled-back migration", payload)
	}
	if hasColumn(t, ctx, database, "agent_runs", "outcome_reason") {
		t.Fatal("agent_runs.outcome_reason survived a rolled-back migration")
	}
	var recorded int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
		runOutcomesMigrationVersion).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 0 {
		t.Fatalf("migration record count = %d, want 0", recorded)
	}
}

// TestRunOutcomeMigrationOmitsStateVersionFromOrphanRunStepNotices pins the one
// number a rewritten notice must never invent. stateVersion is a protobuf
// int64: zero is absence, not version zero. A failure-like run-step whose run
// row is gone has no canonical version to cite, so the rewrite has to leave the
// field out rather than write the Go zero value and let a client read it as a
// real version older than every stored one.
func TestRunOutcomeMigrationOmitsStateVersionFromOrphanRunStepNotices(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "orphan-step.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_orphan_step")
	// No run row at all: the event outlived its run.
	seedLegacyEvent(t, ctx, database, "sess_orphan_step", "", 1, "agent.run.step",
		`{"note":"Retrying (attempt 2 of 3)","attempt":2,"maxAttempts":3,"reason":"model_error"}`)
	seedLegacyEvent(t, ctx, database, "sess_orphan_step", "", 2, "agent.run.step",
		`{"note":"Gave up after 3 attempts","attempts":3,"maxAttempts":3,"reason":"worker_unavailable"}`)
	// Counters an orphan cannot bound either: the category survives alone.
	seedLegacyEvent(t, ctx, database, "sess_orphan_step", "", 3, "agent.run.step",
		`{"note":"Retrying","attempt":1000000000000,"maxAttempts":1000000000000,"reason":"worker_unavailable"}`)

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for _, expectation := range []struct {
		sequence int
		want     map[string]any
	}{
		{
			sequence: 1,
			want: map[string]any{
				"category": "dispatch_retry", "attempt": float64(2), "maxAttempts": float64(3),
			},
		},
		{
			sequence: 2,
			want: map[string]any{
				"category": "recovery_exhausted", "attempt": float64(3), "maxAttempts": float64(3),
			},
		},
		{sequence: 3, want: map[string]any{"category": "recovery_retry"}},
	} {
		payload := readEventPayload(t, ctx, database, "sess_orphan_step", expectation.sequence)
		if _, published := payload["stateVersion"]; published {
			t.Fatalf("sequence %d published stateVersion = %#v with no run to cite",
				expectation.sequence, payload["stateVersion"])
		}
		if !reflect.DeepEqual(payload, expectation.want) {
			t.Fatalf("sequence %d payload = %#v, want %#v", expectation.sequence, payload, expectation.want)
		}
	}
}

// TestRunOutcomeMigrationKeepsStateVersionOnCorrelatedRunStepNotices is the
// other half of the pin above: omitting the version is only correct when there
// is no run, and a notice that does have one must still carry it.
func TestRunOutcomeMigrationKeepsStateVersionOnCorrelatedRunStepNotices(t *testing.T) {
	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "correlated-step.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_correlated_step")
	seedLegacyRun(t, ctx, database, legacyRun{
		id: "run_correlated_step", sessionID: "sess_correlated_step", status: "failed",
		errorCode: "retries_exhausted", finishedAt: "2026-01-01T00:00:03.000000000Z",
	})
	seedLegacyEvent(t, ctx, database, "sess_correlated_step", "run_correlated_step", 1, "agent.run.step",
		`{"note":"Retrying (attempt 2 of 3)","attempt":2,"maxAttempts":3,"reason":"model_error"}`)

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"stateVersion": float64(1), "category": "dispatch_retry",
		"attempt": float64(2), "maxAttempts": float64(3),
	}
	if got := readEventPayload(t, ctx, database, "sess_correlated_step", 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("correlated notice = %#v, want %#v", got, want)
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

// TestRunOutcomeMigrationFailsClosedOnMalformedRetryCounters covers a row the
// old truncate-or-fall-through rewrite mishandled two different ways: a
// counter stored as a string parsed as absent, which sent the whole notice
// down the "no attempt budget" path and left the row untouched — its raw
// note and reason durable forever — and a fractional counter silently
// truncated into a plausible-looking integer count. Both must instead resolve
// to a bounded, category-only notice: the reserved keys (attempt/attempts/
// maxAttempts) are what mark this row as a retry-notice attempt, so a
// malformed value must fail the notice closed rather than fall through to the
// raw payload.
func TestRunOutcomeMigrationFailsClosedOnMalformedRetryCounters(t *testing.T) {
	testCases := []struct {
		name    string
		payload string
		want    map[string]any
	}{
		{
			name: "string attempt counter",
			payload: `{"note":"retrying after connection refused by ollama at 127.0.0.1:11434",` +
				`"attempt":"two","maxAttempts":3,"reason":"model_error"}`,
			want: map[string]any{"category": "dispatch_retry"},
		},
		{
			name: "string attempts counter on a give-up notice",
			payload: `{"note":"giving up: connection refused by ollama at 127.0.0.1:11434",` +
				`"attempts":"three","maxAttempts":3,"reason":"runtime_error"}`,
			want: map[string]any{"category": "recovery_exhausted"},
		},
		{
			name: "string maxAttempts counter",
			payload: `{"note":"retrying","attempt":2,"maxAttempts":"three",` +
				`"reason":"worker_unavailable"}`,
			want: map[string]any{"category": "recovery_retry"},
		},
		{
			name:    "fractional attempt counter truncated toward a plausible count",
			payload: `{"note":"retrying","attempt":2.9,"maxAttempts":3,"reason":"model_error"}`,
			want:    map[string]any{"category": "dispatch_retry"},
		},
		{
			name:    "fractional maxAttempts counter",
			payload: `{"note":"retrying","attempt":2,"maxAttempts":3.5,"reason":"worker_unavailable"}`,
			want:    map[string]any{"category": "recovery_retry"},
		},
		{
			name:    "fractional attempts counter on a give-up notice",
			payload: `{"note":"giving up","attempts":2.5,"maxAttempts":3,"reason":"runtime_error"}`,
			want:    map[string]any{"category": "recovery_exhausted"},
		},
	}

	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "malformed-counters.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_malformed_counters")
	for index, testCase := range testCases {
		seedLegacyEvent(t, ctx, database, "sess_malformed_counters", "", index+1, "agent.run.step", testCase.payload)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := readEventPayload(t, ctx, database, "sess_malformed_counters", index+1)
			if !reflect.DeepEqual(payload, testCase.want) {
				t.Fatalf("payload = %#v, want %#v (row left carrying more than the bounded category)", payload, testCase.want)
			}
			if _, leaked := payload["note"]; leaked {
				t.Fatalf("payload retained the raw note: %#v", payload)
			}
			if _, leaked := payload["reason"]; leaked {
				t.Fatalf("payload retained the raw reason: %#v", payload)
			}
			if _, leaked := payload["attempt"]; leaked {
				t.Fatalf("payload published an attempt counter derived from a malformed value: %#v", payload)
			}
			if _, leaked := payload["attempts"]; leaked {
				t.Fatalf("payload published an attempts counter derived from a malformed value: %#v", payload)
			}
			if _, leaked := payload["maxAttempts"]; leaked {
				t.Fatalf("payload published a maxAttempts counter derived from a malformed value: %#v", payload)
			}
		})
	}
}

// TestRunOutcomeMigrationFailsClosedOnIncompleteRetryCounters covers a row
// that carries only half of a retry notice's counters: an attempt (or
// attempts) with no maxAttempts budget at all, or a maxAttempts budget with
// no attempt/attempts counter at all. Neither key is malformed here — one of
// them is simply absent. runoutcome.HasReservedRetryNoticeKey still recognizes
// the row as a retry-notice attempt because it is gated on presence of *any* reserved
// key, not on every counter being present together, so the row must still be
// rewritten to a bounded, category-only notice rather than left with its raw
// note and reason durable. The pre-fix rewrite required both a parsed
// attempt/attempts *and* a parsed maxAttempts before treating the row as
// failure-like at all; a row missing one of them fell through to the "not a
// retry notice" branch and was left completely untouched, note/reason and
// all. This is the case that gate closed, distinct from
// TestRunOutcomeMigrationFailsClosedOnMalformedRetryCounters above, which
// covers keys that are present but hold an untrustworthy value.
func TestRunOutcomeMigrationFailsClosedOnIncompleteRetryCounters(t *testing.T) {
	testCases := []struct {
		name    string
		payload string
		want    map[string]any
	}{
		{
			name:    "attempt present, maxAttempts absent",
			payload: `{"note":"retrying after connection refused by ollama at 127.0.0.1:11434","attempt":2,"reason":"model_error"}`,
			want:    map[string]any{"category": "dispatch_retry"},
		},
		{
			name:    "attempt present, maxAttempts absent, worker_unavailable reason",
			payload: `{"note":"retrying","attempt":2,"reason":"worker_unavailable"}`,
			want:    map[string]any{"category": "recovery_retry"},
		},
		{
			name:    "attempts present, maxAttempts absent, give-up notice",
			payload: `{"note":"giving up: connection refused by ollama at 127.0.0.1:11434","attempts":3,"reason":"runtime_error"}`,
			want:    map[string]any{"category": "recovery_exhausted"},
		},
		{
			name:    "maxAttempts present, attempt and attempts absent",
			payload: `{"note":"retrying","maxAttempts":3,"reason":"model_error"}`,
			want:    map[string]any{"category": "dispatch_retry"},
		},
		{
			name:    "maxAttempts present, attempt and attempts absent, worker_unavailable reason",
			payload: `{"note":"retrying","maxAttempts":3,"reason":"worker_unavailable"}`,
			want:    map[string]any{"category": "recovery_retry"},
		},
	}

	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "incomplete-counters.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_incomplete_counters")
	for index, testCase := range testCases {
		seedLegacyEvent(t, ctx, database, "sess_incomplete_counters", "", index+1, "agent.run.step", testCase.payload)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := readEventPayload(t, ctx, database, "sess_incomplete_counters", index+1)
			if !reflect.DeepEqual(payload, testCase.want) {
				t.Fatalf("payload = %#v, want %#v (row left carrying more than the bounded category)", payload, testCase.want)
			}
			if _, leaked := payload["note"]; leaked {
				t.Fatalf("payload retained the raw note: %#v", payload)
			}
			if _, leaked := payload["reason"]; leaked {
				t.Fatalf("payload retained the raw reason: %#v", payload)
			}
			if _, leaked := payload["attempt"]; leaked {
				t.Fatalf("payload published an attempt counter with no budget to bound it: %#v", payload)
			}
			if _, leaked := payload["attempts"]; leaked {
				t.Fatalf("payload published an attempts counter with no budget to bound it: %#v", payload)
			}
			if _, leaked := payload["maxAttempts"]; leaked {
				t.Fatalf("payload published a maxAttempts counter with no attempt count to pair it with: %#v", payload)
			}
		})
	}
}

// TestRunOutcomeMigrationFailsClosedOnCategoryOnlyNotice covers a row that
// carries only the "category" reserved key — no attempt, attempts, or
// maxAttempts at all — and whose category value is either unrecognized or the
// wrong JSON type. runoutcome.HasReservedRetryNoticeKey must still recognize
// this row as a retry-notice attempt from "category" alone, because the
// migration's rewrite never reads the raw category value anyway (it always
// derives the category itself from the reason/attempts shape); what matters
// here is that the row is not left untouched with its raw note and reason
// durable. This pins "category" specifically in the reserved-key list: a
// mutation that dropped "category" from that list (leaving only
// attempt/attempts/maxAttempts/stateVersion) would let this row fall through
// to the "not a retry notice" branch and keep its raw payload forever.
func TestRunOutcomeMigrationFailsClosedOnCategoryOnlyNotice(t *testing.T) {
	testCases := []struct {
		name    string
		payload string
		want    map[string]any
	}{
		{
			name:    "unrecognized category value, no counters",
			payload: `{"note":"retrying after connection refused by ollama at 127.0.0.1:11434","reason":"model_error","category":"not_a_real_category"}`,
			want:    map[string]any{"category": "dispatch_retry"},
		},
		{
			name:    "wrong-typed category value, no counters",
			payload: `{"note":"retrying","reason":"worker_unavailable","category":42}`,
			want:    map[string]any{"category": "recovery_retry"},
		},
	}

	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "category-only.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_category_only")
	for index, testCase := range testCases {
		seedLegacyEvent(t, ctx, database, "sess_category_only", "", index+1, "agent.run.step", testCase.payload)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := readEventPayload(t, ctx, database, "sess_category_only", index+1)
			if !reflect.DeepEqual(payload, testCase.want) {
				t.Fatalf("payload = %#v, want %#v (row left carrying more than the bounded category)", payload, testCase.want)
			}
			if _, leaked := payload["note"]; leaked {
				t.Fatalf("payload retained the raw note: %#v", payload)
			}
			if _, leaked := payload["reason"]; leaked {
				t.Fatalf("payload retained the raw reason: %#v", payload)
			}
			if _, leaked := payload["stateVersion"]; leaked {
				t.Fatalf("payload published stateVersion with no run to cite: %#v", payload)
			}
		})
	}
}

// TestRunOutcomeMigrationFailsClosedOnBareStateVersionNotice is the
// "stateVersion" counterpart to the category-only test above: a row that
// carries only the "stateVersion" reserved key — no category, attempt,
// attempts, or maxAttempts — with its raw note and reason still attached.
// The migration never reads this legacy stateVersion value (the rewritten
// stateVersion, when published at all, always comes from the row's own
// correlated run state), so a malformed or wrong-typed stored value must not
// matter to whether the row is recognized. This pins "stateVersion"
// specifically: a mutation that dropped it from the reserved-key list would
// leave this row unrecognized as a retry-notice attempt and keep its raw
// note/reason durable forever.
func TestRunOutcomeMigrationFailsClosedOnBareStateVersionNotice(t *testing.T) {
	testCases := []struct {
		name    string
		payload string
		want    map[string]any
	}{
		{
			name:    "numeric stateVersion, no counters or category",
			payload: `{"note":"retrying after connection refused by ollama at 127.0.0.1:11434","reason":"model_error","stateVersion":7}`,
			want:    map[string]any{"category": "dispatch_retry"},
		},
		{
			name:    "wrong-typed stateVersion, no counters or category",
			payload: `{"note":"retrying","reason":"worker_unavailable","stateVersion":"not-a-number"}`,
			want:    map[string]any{"category": "recovery_retry"},
		},
	}

	ctx := context.Background()
	database := openMigratedThroughLegacy(t, ctx, filepath.Join(t.TempDir(), "bare-state-version.db"))
	defer database.Close()
	seedLegacySession(t, ctx, database, "sess_bare_state_version")
	for index, testCase := range testCases {
		seedLegacyEvent(t, ctx, database, "sess_bare_state_version", "", index+1, "agent.run.step", testCase.payload)
	}

	if err := applyRunOutcomesMigration(t, ctx, database); err != nil {
		t.Fatal(err)
	}
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := readEventPayload(t, ctx, database, "sess_bare_state_version", index+1)
			if !reflect.DeepEqual(payload, testCase.want) {
				t.Fatalf("payload = %#v, want %#v (row left carrying more than the bounded category)", payload, testCase.want)
			}
			if _, leaked := payload["note"]; leaked {
				t.Fatalf("payload retained the raw note: %#v", payload)
			}
			if _, leaked := payload["reason"]; leaked {
				t.Fatalf("payload retained the raw reason: %#v", payload)
			}
			// The legacy row is unattached to any run, so the rewrite has no
			// correlated state to cite; a raw stateVersion the row itself
			// carried must not leak through as if it were that citation.
			if got, leaked := payload["stateVersion"]; leaked {
				t.Fatalf("payload published stateVersion = %#v derived from the raw stored value with no run to cite", got)
			}
		})
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

			// Old table shape, old diagnostics, absent indexes, absent scratch
			// table, absent migration record.
			assertPreMigrationRunShape(t, ctx, reopened, "run_rollback", "weird_legacy_code", "provider stack trace")
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
	for _, batch := range batchesForScan(*batches, runOutcomesEventScan) {
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
	for _, batch := range batchesForScan(*batches, runOutcomesEventScan) {
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

// -----------------------------------------------------------------------------
// Nonterminal legacy correlations that cannot progress
//
// The neutral fallback pinned by
// TestRunOutcomeMigrationAllowsNullAndSingleMismatchedLegacyCorrelation is a
// rule about HISTORY: a terminal row is immutable, so an unusable link costs
// nothing but the content it declines to adopt. A nonterminal row is a promise
// of future work, and that promise cannot be kept. Every canonical transition
// validates the same link before it writes, so such a run can never be started,
// completed, failed, cancelled, or requeued — and an invalid link also drops it
// out of the same-session ordering subquery, so later work in its session
// leapfrogs a run that is still, on paper, active.
//
// Migrating that row is therefore not a conservative choice. It is the creation
// of a permanently stuck run, and it fails closed here instead.
// -----------------------------------------------------------------------------

// legacyLifecycleFixture is one legacy row shape and the canonical nonterminal
// lifecycle the migration derives from it. The derived name is carried so a
// failure message says which lifecycle was accepted rather than which columns
// were seeded.
type legacyLifecycleFixture struct {
	name            string
	derived         string
	status          string
	executionActive int
	executionState  string
	startedAt       string
}

// nonterminalLegacyLifecycles crosses every legacy shape that derives a
// nonterminal canonical lifecycle, including both execution states that widen a
// dishonest running/waiting row into recovering.
var nonterminalLegacyLifecycles = []legacyLifecycleFixture{
	{name: "queued", derived: "queued", status: "queued", executionState: "none"},
	{
		name: "running", derived: "running", status: "running",
		executionActive: 1, executionState: "delivered", startedAt: "2026-01-01T00:00:01.000000000Z",
	},
	{
		name: "waiting approval", derived: "waiting_approval", status: "waiting_approval",
		executionActive: 1, executionState: "delivered", startedAt: "2026-01-01T00:00:01.000000000Z",
	},
	{
		name: "recovering from uncertain running", derived: "recovering", status: "running",
		executionActive: 1, executionState: "uncertain", startedAt: "2026-01-01T00:00:01.000000000Z",
	},
	{
		name: "recovering from fenced waiting approval", derived: "recovering", status: "waiting_approval",
		executionActive: 1, executionState: "fenced", startedAt: "2026-01-01T00:00:01.000000000Z",
	},
}

// legacyCorrelationCorruption is one way a legacy assistant link fails the
// shared rule while leaving exactly one claimant on each side — that is, every
// break the duplicate preflight deliberately does not catch.
type legacyCorrelationCorruption struct {
	name string
	// seed adjusts the fixture before it is written, for breaks that only exist
	// at insert time.
	seed func(legacyRun) legacyRun
	// mutate breaks an otherwise well-formed pair after both rows exist.
	mutate func(t *testing.T, ctx context.Context, database *DB, run legacyRun)
}

func nonterminalCorrelationCorruptions() []legacyCorrelationCorruption {
	return []legacyCorrelationCorruption{
		{
			name: "null in both directions",
			seed: func(run legacyRun) legacyRun {
				run.assistantMessageID = ""
				return run
			},
		},
		{
			name: "run names the message but the message names nobody",
			mutate: func(t *testing.T, ctx context.Context, database *DB, run legacyRun) {
				t.Helper()
				if _, err := database.ExecContext(ctx,
					`UPDATE messages SET run_id = NULL WHERE id = ?`, run.assistantMessageID); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "message names the run but the run names nobody",
			mutate: func(t *testing.T, ctx context.Context, database *DB, run legacyRun) {
				t.Helper()
				if _, err := database.ExecContext(ctx,
					`UPDATE agent_runs SET assistant_message_id = NULL WHERE id = ?`, run.id); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "linked message is not the assistant turn",
			seed: func(run legacyRun) legacyRun {
				run.assistantRole = "tool"
				return run
			},
		},
		{
			name: "linked message belongs to another session",
			mutate: func(t *testing.T, ctx context.Context, database *DB, run legacyRun) {
				t.Helper()
				if _, err := database.ExecContext(ctx,
					`UPDATE messages SET session_id = ? WHERE id = ?`,
					otherLegacySessionID, run.assistantMessageID); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
}

// otherLegacySessionID is the second session the cross-session break files its
// assistant message under. It exists because messages.session_id is a foreign
// key, so the break cannot be expressed with an invented session.
const otherLegacySessionID = "sess_nonterminal_other"

// legacyRowSnapshot is every legacy row a rejected migration must leave exactly
// as it found it, rendered as sorted column text so a comparison names the
// difference rather than a struct address.
type legacyRowSnapshot struct {
	runs     []string
	messages []string
	jobs     []string
	events   []string
}

func snapshotLegacyRows(t *testing.T, ctx context.Context, database *DB) legacyRowSnapshot {
	t.Helper()
	return legacyRowSnapshot{
		runs: scanRowText(t, ctx, database, `
			SELECT id, session_id, COALESCE(assistant_message_id, '<null>'), status,
				COALESCE(error_code, '<null>'), COALESCE(error_message, '<null>'),
				execution_active, execution_state, COALESCE(started_at, '<null>'),
				COALESCE(finished_at, '<null>')
			FROM agent_runs ORDER BY id`),
		messages: scanRowText(t, ctx, database, `
			SELECT id, session_id, COALESCE(run_id, '<null>'), role, content
			FROM messages ORDER BY id`),
		jobs: scanRowText(t, ctx, database, `
			SELECT id, run_id, status, COALESCE(error_code, '<null>'), COALESCE(error_message, '<null>')
			FROM jobs ORDER BY id`),
		events: scanRowText(t, ctx, database, `
			SELECT id, type, payload_json FROM events ORDER BY id`),
	}
}

// scanRowText renders a result set as one joined string per row, so a snapshot
// comparison works for any column list without a per-table struct.
func scanRowText(t *testing.T, ctx context.Context, database *DB, query string) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	rendered := make([]string, 0, 8)
	for rows.Next() {
		cells := make([]any, len(columns))
		targets := make([]any, len(columns))
		for i := range cells {
			targets[i] = &cells[i]
		}
		if err := rows.Scan(targets...); err != nil {
			t.Fatal(err)
		}
		parts := make([]string, 0, len(columns))
		for i, column := range columns {
			parts = append(parts, fmt.Sprintf("%s=%v", column, cells[i]))
		}
		rendered = append(rendered, strings.Join(parts, " "))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return rendered
}

// assertRunOutcomesMigrationLeftNoTrace proves the rejection rolled back
// everything the migration would otherwise have committed: the record, the
// widened schema, the correlation indexes, the temporary backfill table, the
// raw-diagnostic scrub, and the public event rewrite.
func assertRunOutcomesMigrationLeftNoTrace(
	t *testing.T,
	ctx context.Context,
	database *DB,
	before legacyRowSnapshot,
) {
	t.Helper()
	var recorded int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
		runOutcomesMigrationVersion).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 0 {
		t.Fatalf("migration record count = %d, want 0", recorded)
	}
	for _, column := range []string{"state_version", "state_updated_at", "outcome_reason", "assistant_content_sha256"} {
		if hasColumn(t, ctx, database, "agent_runs", column) {
			t.Fatalf("agent_runs.%s survived a rejected migration", column)
		}
	}
	assertLegacyRunStatusVocabulary(t, ctx, database)
	for _, name := range []string{"idx_runs_assistant_message_unique", "idx_messages_assistant_run_unique"} {
		if sqliteObjectExists(t, ctx, database, "index", name) {
			t.Fatalf("index %q survived a rejected migration", name)
		}
	}
	if sqliteObjectExists(t, ctx, database, "table", runOutcomesBackfillTable) {
		t.Fatalf("%s survived a rejected migration", runOutcomesBackfillTable)
	}
	after := snapshotLegacyRows(t, ctx, database)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("rejected migration changed legacy rows:\n after = %+v\n before = %+v", after, before)
	}
}

// legacyRunStatusCheck is the exact status vocabulary migration 0017 widens.
// The rollback proof matches it against the stored DDL instead of probing with
// a write, because any write — even one that matches no row — would either be
// vacuous against the CHECK or perturb the legacy snapshot compared below.
const legacyRunStatusCheck = `CHECK (status IN ('queued','running','waiting_approval','completed','failed','cancelled'))`

// assertLegacyRunStatusVocabulary reads agent_runs' stored CREATE TABLE text
// and proves it is still the pre-0017 one: it admits no 'recovering' and it
// still spells out every legacy status. Migration 0017's widened table fails
// both halves — it names 'recovering' and no longer carries the legacy check —
// so a surviving rebuild cannot pass by accident.
func assertLegacyRunStatusVocabulary(t *testing.T, ctx context.Context, database *DB) {
	t.Helper()
	var stored sql.NullString
	if err := database.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'agent_runs'`).Scan(&stored); err != nil {
		t.Fatalf("reading stored agent_runs DDL: %v", err)
	}
	if !stored.Valid || strings.TrimSpace(stored.String) == "" {
		t.Fatal("agent_runs has no stored DDL after a rejected migration")
	}
	// The rebuild lays the status check out across its own lines, so compare on
	// whitespace-normalized text rather than on the legacy file's formatting.
	ddl := strings.Join(strings.Fields(stored.String), " ")
	if strings.Contains(ddl, `'recovering'`) {
		t.Fatalf("rolled-back agent_runs DDL still admits 'recovering': %s", ddl)
	}
	if !strings.Contains(ddl, legacyRunStatusCheck) {
		t.Fatalf("agent_runs DDL lost the legacy status check %s: %s", legacyRunStatusCheck, ddl)
	}
}

// TestRunOutcomeMigrationRejectsEveryNonterminalBrokenCorrelationValueFree
// crosses every derived nonterminal lifecycle with every single-claimant way
// the assistant link can fail the shared rule. None of them may migrate, and
// none of them may say why in terms an operator could mistake for row content.
func TestRunOutcomeMigrationRejectsEveryNonterminalBrokenCorrelationValueFree(t *testing.T) {
	for _, lifecycle := range nonterminalLegacyLifecycles {
		for _, corruption := range nonterminalCorrelationCorruptions() {
			t.Run(lifecycle.name+"/"+corruption.name, func(t *testing.T) {
				ctx := context.Background()
				path := filepath.Join(t.TempDir(), "nonterminal-correlation.db")
				database := openMigratedThroughLegacy(t, ctx, path)
				seedLegacySession(t, ctx, database, "sess_nonterminal")
				seedLegacySession(t, ctx, database, otherLegacySessionID)
				run := legacyRun{
					id:                 "run_nonterminal",
					sessionID:          "sess_nonterminal",
					status:             lifecycle.status,
					executionActive:    lifecycle.executionActive,
					executionState:     lifecycle.executionState,
					startedAt:          lifecycle.startedAt,
					assistantMessageID: "msg_nonterminal",
					assistantContent:   "half-written answer",
				}
				if corruption.seed != nil {
					run = corruption.seed(run)
				}
				seedLegacyRun(t, ctx, database, run)
				// A job and a raw failure event, so the rollback proof covers
				// the diagnostic scrub and the public event rewrite rather than
				// only the run table.
				seedLegacyNonterminalJob(t, ctx, database, run.id)
				seedLegacyEvent(t, ctx, database, run.sessionID, run.id, 10, "tool.call.failed",
					`{"toolCallId":"call_nonterminal","toolName":"files.update","message":"provider stack trace"}`)
				if corruption.mutate != nil {
					corruption.mutate(t, ctx, database, run)
				}
				before := snapshotLegacyRows(t, ctx, database)

				err := applyRunOutcomesMigration(t, ctx, database)
				if !errors.Is(err, runcorrelation.ErrConflict) {
					t.Fatalf("ApplyMigrations error = %v, want runcorrelation.ErrConflict for a %s run",
						err, lifecycle.derived)
				}
				if got := err.Error(); got != "run/message correlation conflict" {
					t.Fatalf("error = %q, want exactly the value-free correlation sentinel", got)
				}
				assertRunOutcomesMigrationLeftNoTrace(t, ctx, database, before)

				// A restart must not turn the refusal into an acceptance: the
				// operator has to repair the row, and the second attempt says
				// exactly as little as the first.
				if err := database.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				retry := ApplyMigrations(ctx, reopened)
				if !errors.Is(retry, runcorrelation.ErrConflict) {
					t.Fatalf("reopened ApplyMigrations error = %v, want runcorrelation.ErrConflict", retry)
				}
				if got := retry.Error(); got != "run/message correlation conflict" {
					t.Fatalf("reopened error = %q, want exactly the value-free correlation sentinel", got)
				}
				assertRunOutcomesMigrationLeftNoTrace(t, ctx, reopened, before)
			})
		}
	}
}

// seedLegacyNonterminalJob writes the pending job a nonterminal run still owns,
// carrying the raw diagnostics the migration's scrub would otherwise clear.
func seedLegacyNonterminalJob(t *testing.T, ctx context.Context, database *DB, runID string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO jobs (id, run_id, agent_id, status, payload_json, error_code, error_message, created_at, created_at_ns)
		VALUES ('job_nonterminal', ?, 'general_assistant', 'pending', '{}', 'weird_legacy_code', 'provider text',
			'2026-01-01T00:00:00.000000000Z', 1)
	`, runID); err != nil {
		t.Fatal(err)
	}
}
