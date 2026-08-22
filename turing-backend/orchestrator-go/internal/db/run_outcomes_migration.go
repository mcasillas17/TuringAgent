package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runcorrelation"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// runOutcomesMigrationVersion is the one version that runs through the hooked,
// section-split, foreign-key-pinned path. Every other migration keeps the
// ordinary single-statement-batch execution below.
const runOutcomesMigrationVersion = "0017_run_outcomes"

// Named seams the runner passes through, in execution order. They are the
// rollback boundaries the migration tests inject at, and the phase names the
// SQL file's marker comments must use.
const (
	migrationPhaseBeforeHook   = "before-hook"
	migrationPhaseAfterRebuild = "after-rebuild"
	migrationPhaseAfterScrub   = "after-scrub"
	migrationPhaseAfterIndexes = "after-indexes"
	migrationPhaseAfterHook    = "after-hook"
	migrationPhaseBeforeRecord = "before-record"
	migrationPhaseAfterRecord  = "after-record"
)

// migrationHook is Go work that must commit or roll back with its migration's
// SQL. Both phases receive the migration's own transaction: while the pinned
// connection is held it is the only way to reach the database, because a
// database.Query/Exec call would wait forever for the one occupied connection.
type migrationHook struct {
	Before func(context.Context, *sql.Tx) error
	After  func(context.Context, *sql.Tx) error
}

var migrationHooks = map[string]migrationHook{
	runOutcomesMigrationVersion: {
		Before: runOutcomesBeforeHook,
		After:  runOutcomesAfterHook,
	},
}

// migrationPhaseHook is set only by migration tests, to observe phase ordering
// and to inject a failure at one exact seam. Production leaves it nil.
var migrationPhaseHook func(context.Context, string, string, *sql.Tx) error

// migrationPinnedConnectionHook is set only by migration tests, to disturb the
// pinned connection between the transaction and the foreign-key restoration
// proof. Nothing else can reach that connection — this function holds it and
// the pool has no second one — so without this seam the fatal
// cannot-prove-restoration path could not be exercised at all. It cannot
// fabricate an outcome: it returns nothing, and the restoration proof below is
// still the only thing that decides. Production leaves it nil.
var migrationPinnedConnectionHook func(context.Context, *sql.Conn)

var (
	// errForeignKeysNotEnforced refuses to start the rebuild from a connection
	// that never had referential integrity on: turning foreign keys "back on"
	// afterwards would be a claim the migration cannot support.
	errForeignKeysNotEnforced = errors.New("run outcome migration requires enforced foreign keys")
	// errForeignKeysUnrestorable is fatal. The pinned connection returns to the
	// pool after this migration, so a connection stuck with foreign keys off
	// would silently disable cascade integrity for the rest of the process.
	errForeignKeysUnrestorable = errors.New("run outcome migration could not restore foreign keys")
	// errForeignKeyViolation reports that the rebuilt parent table left a child
	// row orphaned. It names the class of problem and never a row value.
	errForeignKeyViolation = errors.New("run outcome migration foreign key violation")
	// errMigrationMarkers rejects a SQL file whose section markers are missing,
	// duplicated, unknown, or reordered rather than guessing the boundaries.
	errMigrationMarkers = errors.New("run outcome migration section markers are invalid")
)

// migrationUsesPinnedConnection reports whether a version needs the dedicated
// connection, foreign-key-off rebuild path. Only the TUR-009 migration does:
// every run-owned child table references agent_runs(id) ON DELETE CASCADE, so
// dropping and recreating that parent with foreign keys on would cascade the
// children away.
func migrationUsesPinnedConnection(version string) bool {
	_, hooked := migrationHooks[version]
	return hooked
}

// migrationSection is one executable slice of a migration file. Marker is the
// phase name that terminates it, or empty for a trailing or unsplit section.
type migrationSection struct {
	SQL    string
	Marker string
}

const migrationMarkerPrefix = "-- marker:"

// runOutcomesSectionMarkers is the exact ordered marker sequence the TUR-009
// file must contain.
var runOutcomesSectionMarkers = []string{
	migrationPhaseAfterRebuild,
	migrationPhaseAfterScrub,
	migrationPhaseAfterIndexes,
}

// migrationSections splits a hooked migration into its named sections and
// leaves every other migration exactly as it was: one section, executed by one
// ExecContext, marker comments or not.
func migrationSections(version string, sqlText string) ([]migrationSection, error) {
	if !migrationUsesPinnedConnection(version) {
		return []migrationSection{{SQL: sqlText}}, nil
	}
	var (
		sections []migrationSection
		markers  []string
		current  strings.Builder
	)
	for _, line := range strings.SplitAfter(sqlText, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, migrationMarkerPrefix) {
			current.WriteString(line)
			continue
		}
		marker := strings.TrimSpace(strings.TrimPrefix(trimmed, migrationMarkerPrefix))
		markers = append(markers, marker)
		sections = append(sections, migrationSection{SQL: current.String(), Marker: marker})
		current.Reset()
	}
	if trailing := current.String(); strings.TrimSpace(trailing) != "" {
		sections = append(sections, migrationSection{SQL: trailing})
	}
	if len(markers) != len(runOutcomesSectionMarkers) {
		return nil, errMigrationMarkers
	}
	for index, marker := range markers {
		if marker != runOutcomesSectionMarkers[index] {
			return nil, errMigrationMarkers
		}
	}
	return sections, nil
}

// applyHookedMigration runs one migration's Go hooks and SQL sections in a
// single transaction on a dedicated connection.
//
// The connection is pinned because the rebuild drops and recreates agent_runs,
// the parent of every run-owned child table. PRAGMA foreign_keys is a
// per-connection setting that cannot change inside a transaction, so it is
// turned off before BeginTx on the connection the whole migration then runs on.
// The existing single-connection pool means no other statement can observe the
// weakened connection while it is held.
//
// Errors are returned unwrapped. This migration's failure classes are
// deliberately value-free sentinels, and a decorated message is one more place
// a row value could leak into an operator log.
func applyHookedMigration(
	ctx context.Context,
	database *DB,
	version string,
	sqlText string,
	hook migrationHook,
) (pending error) {
	sections, err := migrationSections(version, sqlText)
	if err != nil {
		return err
	}
	conn, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if err := requireForeignKeysEnabled(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return restoreForeignKeys(ctx, database, conn, err)
	}
	defer func() {
		pending = restoreForeignKeys(ctx, database, conn, pending)
	}()
	pending = runHookedMigrationTx(ctx, conn, version, sections, hook)
	if migrationPinnedConnectionHook != nil {
		migrationPinnedConnectionHook(ctx, conn)
	}
	return pending
}

func runHookedMigrationTx(
	ctx context.Context,
	conn *sql.Conn,
	version string,
	sections []migrationSection,
	hook migrationHook,
) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if hook.Before != nil {
		if err := hook.Before(ctx, tx); err != nil {
			return err
		}
	}
	if err := migrationPhase(ctx, version, migrationPhaseBeforeHook, tx); err != nil {
		return err
	}
	for _, section := range sections {
		if !sqlSectionIsEmpty(section.SQL) {
			if _, err := tx.ExecContext(ctx, section.SQL); err != nil {
				return err
			}
		}
		if section.Marker == "" {
			continue
		}
		if err := migrationPhase(ctx, version, section.Marker, tx); err != nil {
			return err
		}
	}
	if hook.After != nil {
		if err := hook.After(ctx, tx); err != nil {
			return err
		}
	}
	if err := migrationPhase(ctx, version, migrationPhaseAfterHook, tx); err != nil {
		return err
	}
	// Run with the rebuilt parent and every child row already in place, so an
	// orphan created by the swap is caught before the transaction can commit.
	if err := requireEmptyForeignKeyCheck(ctx, tx); err != nil {
		return err
	}
	if err := migrationPhase(ctx, version, migrationPhaseBeforeRecord, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))`,
		version,
	); err != nil {
		return err
	}
	if err := migrationPhase(ctx, version, migrationPhaseAfterRecord, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func migrationPhase(ctx context.Context, version string, phase string, tx *sql.Tx) error {
	if migrationPhaseHook == nil {
		return nil
	}
	return migrationPhaseHook(ctx, version, phase, tx)
}

func requireForeignKeysEnabled(ctx context.Context, conn *sql.Conn) error {
	var enabled int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		return err
	}
	if enabled != 1 {
		return errForeignKeysNotEnforced
	}
	return nil
}

// restoreForeignKeys puts the pinned connection back the way it was found and
// proves it, on the success path and on every failure path alike. If the proof
// cannot be produced the database is closed: continuing would hand a
// cascade-disabled connection back to the pool, and a wrong-but-running process
// is worse here than a failed startup.
func restoreForeignKeys(ctx context.Context, database *DB, conn *sql.Conn, pending error) error {
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		_ = database.Close()
		return errForeignKeysUnrestorable
	}
	var enabled int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		_ = database.Close()
		return errForeignKeysUnrestorable
	}
	if enabled != 1 {
		_ = database.Close()
		return errForeignKeysUnrestorable
	}
	return pending
}

func requireEmptyForeignKeyCheck(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	violated := rows.Next()
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if violated {
		return errForeignKeyViolation
	}
	return nil
}

// sqlSectionIsEmpty reports whether a section carries no statement, so a
// comment-only or blank slice between markers is skipped rather than handed to
// the driver. It only inspects whole-line comments, which is all a section
// boundary can produce.
func sqlSectionIsEmpty(section string) bool {
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		return false
	}
	return true
}

// Bounds for every keyset scan this migration performs. A batch stops at the
// first of the two: 128 rows caps fixed Go per-row overhead, and 16 MiB caps
// the variable-width data actually selected, so one enormous assistant message
// cannot turn a bounded transaction into an unbounded allocation.
const (
	runOutcomesBatchRows        = 128
	runOutcomesByteBudget int64 = 16 << 20
	runOutcomesRunScan          = "runs"
	runOutcomesEventScan        = "events"
)

// runOutcomesRunBytesExpr measures one run's selected variable-width data. It
// is the single definition of "selected bytes" for the run pass: the length
// cursor sums it before any value is read, so an oversized row is rejected
// without ever materializing it in Go.
const runOutcomesRunBytesExpr = `
	COALESCE(length(CAST(r.id AS BLOB)), 0) +
	COALESCE(length(CAST(r.session_id AS BLOB)), 0) +
	COALESCE(length(CAST(r.user_message_id AS BLOB)), 0) +
	COALESCE(length(CAST(r.assistant_message_id AS BLOB)), 0) +
	COALESCE(length(CAST(r.status AS BLOB)), 0) +
	COALESCE(length(CAST(r.error_code AS BLOB)), 0) +
	COALESCE(length(CAST(r.cancellation_reason AS BLOB)), 0) +
	COALESCE(length(CAST(r.execution_state AS BLOB)), 0) +
	COALESCE(length(CAST(r.created_at AS BLOB)), 0) +
	COALESCE(length(CAST(r.started_at AS BLOB)), 0) +
	COALESCE(length(CAST(r.finished_at AS BLOB)), 0) +
	COALESCE(length(CAST(m.id AS BLOB)), 0) +
	COALESCE(length(CAST(m.session_id AS BLOB)), 0) +
	COALESCE(length(CAST(m.run_id AS BLOB)), 0) +
	COALESCE(length(CAST(m.role AS BLOB)), 0) +
	COALESCE(length(CAST(m.content AS BLOB)), 0)`

// runOutcomeBatch is one keyset batch the migration read.
type runOutcomeBatch struct {
	Scan  string
	Rows  int
	Bytes int64
}

// runOutcomesBatchObserver is set only by migration tests, to prove the scans
// actually split at their row and byte bounds. Production leaves it nil.
var runOutcomesBatchObserver func(scan string, rows int, bytes int64)

func observeRunOutcomeBatch(scan string, rows int, bytes int64) {
	if runOutcomesBatchObserver == nil {
		return
	}
	runOutcomesBatchObserver(scan, rows, bytes)
}

// runOutcomesBackfillTable holds the canonical values the SQL rebuild copies
// into the new agent_runs. It is created and dropped inside the migration
// transaction, so a rollback at any seam takes it with everything else.
const runOutcomesBackfillTable = "run_outcomes_backfill"

var (
	// errRunOutcomeRowTooLarge reports a single row the bounded scan refuses to
	// materialize. It names the class of problem and never the row.
	errRunOutcomeRowTooLarge = errors.New("run outcome migration row exceeds byte limit")
	// errRunOutcomeUnclassifiable reports a legacy status this migration has no
	// honest canonical lifecycle for, rather than guessing one.
	errRunOutcomeUnclassifiable = errors.New("run outcome migration cannot classify a legacy run")
)

func runOutcomesBeforeHook(ctx context.Context, tx *sql.Tx) error {
	if err := requireUniqueRunMessageCorrelation(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE `+runOutcomesBackfillTable+` (
		run_id TEXT PRIMARY KEY,
		lifecycle TEXT NOT NULL,
		outcome_reason TEXT NOT NULL,
		state_version INTEGER NOT NULL,
		state_updated_at TEXT NOT NULL,
		has_displayable_content INTEGER NOT NULL,
		assistant_content_sha256 TEXT NOT NULL
	)`); err != nil {
		return err
	}
	return backfillRunOutcomes(ctx, tx)
}

// requireUniqueRunMessageCorrelation aborts before anything is written if either
// direction of the run/assistant link is ambiguous. A duplicate makes ownership
// unknowable, and no automatic choice between two claimants is defensible, so
// the operator restores a consistent database instead. The queries return only
// existence: no ID, content, or path reaches the error or the log.
func requireUniqueRunMessageCorrelation(ctx context.Context, tx *sql.Tx) error {
	for _, query := range []string{
		`SELECT EXISTS (
			SELECT 1 FROM agent_runs
			WHERE assistant_message_id IS NOT NULL
			GROUP BY assistant_message_id
			HAVING COUNT(*) > 1
		)`,
		`SELECT EXISTS (
			SELECT 1 FROM messages
			WHERE run_id IS NOT NULL AND role = 'assistant'
			GROUP BY run_id
			HAVING COUNT(*) > 1
		)`,
	} {
		var duplicated int
		if err := tx.QueryRowContext(ctx, query).Scan(&duplicated); err != nil {
			return err
		}
		if duplicated != 0 {
			return runcorrelation.ErrConflict
		}
	}
	return nil
}

// legacyRunRow is one pre-TUR-009 run joined to the assistant message it claims.
type legacyRunRow struct {
	rowID              int64
	id                 string
	sessionID          string
	assistantMessageID sql.NullString
	status             string
	errorCode          sql.NullString
	executionActive    int64
	executionState     string
	createdAt          string
	startedAt          sql.NullString
	finishedAt         sql.NullString
	messageID          sql.NullString
	messageSessionID   sql.NullString
	messageRunID       sql.NullString
	messageRole        sql.NullString
	messageContent     sql.NullString
}

type runOutcomeBackfill struct {
	runID                 string
	lifecycle             string
	outcomeReason         string
	stateUpdatedAt        string
	hasDisplayableContent bool
	contentSHA256         string
}

// backfillRunOutcomes walks agent_runs by stable rowid keyset and writes one
// canonical row per run. Each batch is measured before it is read, and every
// cursor is closed before any write, so the transaction never holds a result
// set open across an insert.
func backfillRunOutcomes(ctx context.Context, tx *sql.Tx) error {
	var cursor int64
	for {
		lastRowID, batchRows, batchBytes, err := runOutcomeRunBatchBounds(ctx, tx, cursor)
		if err != nil {
			return err
		}
		if batchRows == 0 {
			return nil
		}
		observeRunOutcomeBatch(runOutcomesRunScan, batchRows, batchBytes)
		rows, err := readLegacyRunBatch(ctx, tx, cursor, lastRowID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			derived, err := deriveRunOutcome(row)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO `+runOutcomesBackfillTable+` (
				run_id, lifecycle, outcome_reason, state_version, state_updated_at,
				has_displayable_content, assistant_content_sha256
			) VALUES (?, ?, ?, 1, ?, ?, ?)`,
				derived.runID, derived.lifecycle, derived.outcomeReason, derived.stateUpdatedAt,
				boolToInt(derived.hasDisplayableContent), derived.contentSHA256,
			); err != nil {
				return err
			}
		}
		cursor = lastRowID
	}
}

// runOutcomeRunBatchBounds is the length-only cursor. It accounts for the byte
// length of every variable-width column the data pass will select and returns
// the last rowid that fits, so no value is materialized before it is known to
// be affordable.
func runOutcomeRunBatchBounds(ctx context.Context, tx *sql.Tx, cursor int64) (int64, int, int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT r.rowid, `+runOutcomesRunBytesExpr+`
		FROM agent_runs r
		LEFT JOIN messages m ON m.id = r.assistant_message_id
		WHERE r.rowid > ?
		ORDER BY r.rowid
		LIMIT ?
	`, cursor, runOutcomesBatchRows)
	if err != nil {
		return 0, 0, 0, err
	}
	lastRowID, count, total, err := accumulateBatchBounds(rows)
	if closeErr := rows.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, 0, 0, err
	}
	return lastRowID, count, total, nil
}

func accumulateBatchBounds(rows *sql.Rows) (int64, int, int64, error) {
	var (
		lastRowID int64
		count     int
		total     int64
	)
	for rows.Next() {
		var rowID, rowBytes int64
		if err := rows.Scan(&rowID, &rowBytes); err != nil {
			return 0, 0, 0, err
		}
		if rowBytes > runOutcomesByteBudget {
			return 0, 0, 0, errRunOutcomeRowTooLarge
		}
		if total+rowBytes > runOutcomesByteBudget {
			break
		}
		total += rowBytes
		lastRowID = rowID
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, err
	}
	return lastRowID, count, total, nil
}

func readLegacyRunBatch(ctx context.Context, tx *sql.Tx, cursor int64, lastRowID int64) ([]legacyRunRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT r.rowid, r.id, r.session_id, r.assistant_message_id, r.status, r.error_code,
			r.execution_active, r.execution_state, r.created_at, r.started_at, r.finished_at,
			m.id, m.session_id, m.run_id, m.role, m.content
		FROM agent_runs r
		LEFT JOIN messages m ON m.id = r.assistant_message_id
		WHERE r.rowid > ? AND r.rowid <= ?
		ORDER BY r.rowid
	`, cursor, lastRowID)
	if err != nil {
		return nil, err
	}
	batch := make([]legacyRunRow, 0, runOutcomesBatchRows)
	for rows.Next() {
		var row legacyRunRow
		if err := rows.Scan(
			&row.rowID, &row.id, &row.sessionID, &row.assistantMessageID, &row.status, &row.errorCode,
			&row.executionActive, &row.executionState, &row.createdAt, &row.startedAt, &row.finishedAt,
			&row.messageID, &row.messageSessionID, &row.messageRunID, &row.messageRole, &row.messageContent,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		batch = append(batch, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return batch, nil
}

// deriveRunOutcome turns one legacy row into canonical state. Content identity
// is adopted only from a link that passes the shared correlation rule: a run
// that names a message which does not name it back has not proven ownership,
// and inheriting that message's bytes would be a guess.
//
// What an unusable link costs depends entirely on whether the row is finished.
// Terminal history is immutable, so the link buys nothing but the content it
// declines to adopt, and the neutral fallback is honest. A nonterminal row is a
// promise of future work, and every canonical transition validates the same
// link before it writes — so migrating one would mint a run that can never be
// started, finished, or requeued, and whose session cannot move past it.
// Refusing is therefore the conservative choice, and it says only which class
// of problem occurred: the row, its session, and its content stay out of the
// error entirely.
func deriveRunOutcome(row legacyRunRow) (runOutcomeBackfill, error) {
	lifecycle, err := deriveRunLifecycle(row)
	if err != nil {
		return runOutcomeBackfill{}, err
	}
	content := ""
	if linkErr := runcorrelation.Validate(runcorrelation.Link{
		RunID:                 row.id,
		RunSessionID:          row.sessionID,
		RunAssistantMessageID: row.assistantMessageID.String,
		MessageID:             row.messageID.String,
		MessageSessionID:      row.messageSessionID.String,
		MessageRunID:          row.messageRunID.String,
		MessageRole:           row.messageRole.String,
	}); linkErr != nil {
		if !isTerminalLegacyLifecycle(lifecycle) {
			return runOutcomeBackfill{}, linkErr
		}
	} else {
		content = row.messageContent.String
	}
	hasContent := runoutcome.HasDisplayableContent(content)
	stateUpdatedAt, err := deriveStateUpdatedAt(row)
	if err != nil {
		return runOutcomeBackfill{}, err
	}
	return runOutcomeBackfill{
		runID:                 row.id,
		lifecycle:             lifecycle,
		outcomeReason:         string(deriveOutcomeReason(lifecycle, row.errorCode.String, hasContent)),
		stateUpdatedAt:        stateUpdatedAt,
		hasDisplayableContent: hasContent,
		contentSHA256:         runoutcome.ContentSHA256(content),
	}, nil
}

// isTerminalLegacyLifecycle names the canonical lifecycles a run can never
// leave. Only these may carry the neutral fallback for an unusable assistant
// link, because only these are done needing one.
func isTerminalLegacyLifecycle(lifecycle string) bool {
	switch lifecycle {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

// deriveRunLifecycle widens the legacy statuses that were never honest. A row
// stored as running or waiting-approval while its execution lease is uncertain
// or fenced has no worker known to be making progress, so it migrates to
// recovering rather than claiming forward motion.
//
// A row stored as failed under the client-cancellation transport code is
// reclassified for the opposite reason: it was never a failure. Older call
// sites filed the disconnect signal as a run failure, and migrating it as such
// would report a system fault to a user whose client simply went away.
func deriveRunLifecycle(row legacyRunRow) (string, error) {
	switch row.status {
	case "queued", "completed", "cancelled":
		return row.status, nil
	case "failed":
		if isLegacyTransportCancellation(row) {
			return "cancelled", nil
		}
		return row.status, nil
	case "running", "waiting_approval":
		if row.executionActive == 1 && (row.executionState == "uncertain" || row.executionState == "fenced") {
			return "recovering", nil
		}
		return row.status, nil
	default:
		return "", errRunOutcomeUnclassifiable
	}
}

// isLegacyTransportCancellation recognizes the one signal the transport writes
// when a client goes away. Only the exact server-chosen code counts. Freeform
// cancellation prose is never consulted: a human sentence cannot distinguish a
// deliberate stop from a dropped connection, and reading intent out of it would
// invent the very fact the abandoned outcome exists to avoid claiming.
func isLegacyTransportCancellation(row legacyRunRow) bool {
	return row.errorCode.Valid && row.errorCode.String == runoutcome.CodeClientCancelled
}

// deriveStateUpdatedAt takes the most authoritative lifecycle timestamp the row
// actually has — finished_at, else started_at, else created_at — and re-renders
// it at the canonical fixed width. The order is precedence, not recency: legacy
// rows were written by several call sites with no shared clock discipline, so a
// skewed started_at must not outrank the finish that ended the run. An empty
// string is treated as absent, because SQLite stores it as a non-NULL value a
// legacy writer left behind rather than as a time. A present but unparseable
// value fails the migration: writing a variable-width or offset-bearing string
// into a text-compared ordering column is the bug this format exists to
// prevent.
func deriveStateUpdatedAt(row legacyRunRow) (string, error) {
	source := row.createdAt
	if row.startedAt.Valid && row.startedAt.String != "" {
		source = row.startedAt.String
	}
	if row.finishedAt.Valid && row.finishedAt.String != "" {
		source = row.finishedAt.String
	}
	return canonicalLegacyTime(source)
}

// canonicalLegacyTime re-renders one legacy timestamp at the canonical fixed
// width. It is the single place a persisted time becomes public text in this
// migration, so a value published beside state_updated_at cannot acquire a
// second rendering rule. A present but unparseable value fails with the
// value-free sentinel rather than being published as it stands.
func canonicalLegacyTime(value string) (string, error) {
	parsed, err := persisttime.ParseLegacy(value)
	if err != nil {
		return "", err
	}
	return persisttime.Format(parsed), nil
}

// legacyRunFailureOrigins is the approved run-terminal mapping, keyed by the
// code the reporting call site wrote. The origin is not recoverable from the
// row, so it is restated here exactly as approved; a code outside this table is
// treated as unknown and fails closed to an internal failure.
var legacyRunFailureOrigins = map[string]runoutcome.Origin{
	"message_fetch_failed":            runoutcome.OriginContextAssembly,
	"external_agent_unavailable":      runoutcome.OriginExternalProvider,
	"model_provider_unavailable":      runoutcome.OriginProviderConfiguration,
	"tool_discovery_failed":           runoutcome.OriginToolInfrastructure,
	"context_budget_exceeded":         runoutcome.OriginContextAssembly,
	"model_timeout":                   runoutcome.OriginProviderTransport,
	"model_stream_failed":             runoutcome.OriginProviderTransport,
	"model_output_limit_exceeded":     runoutcome.OriginProviderOutputGuard,
	"model_unavailable":               runoutcome.OriginProviderProtocol,
	"model_auth_failed":               runoutcome.OriginProviderProtocol,
	"model_request_failed":            runoutcome.OriginProviderProtocol,
	"model_error":                     runoutcome.OriginExternalProvider,
	"model_quota_exceeded":            runoutcome.OriginProviderProtocol,
	"model_bad_chunk":                 runoutcome.OriginProviderProtocol,
	"model_stream_error":              runoutcome.OriginProviderTransport,
	"tool_call_failed":                runoutcome.OriginToolExecution,
	"tool_call_limit_exceeded":        runoutcome.OriginToolGuard,
	"tool_result_limit_exceeded":      runoutcome.OriginToolGuard,
	"runtime_error":                   runoutcome.OriginWorkerRuntime,
	"retries_exhausted":               runoutcome.OriginDispatch,
	"job_timeout":                     runoutcome.OriginRecovery,
	"side_effect_uncertain":           runoutcome.OriginRecovery,
	"approval_delivery_failed":        runoutcome.OriginApprovalTransport,
	"approval_expired":                runoutcome.OriginApprovalExpiry,
	"approval_denied":                 runoutcome.OriginToolPolicy,
	"automation_approval_failed":      runoutcome.OriginAutomationPolicy,
	"automation_tool_not_allowlisted": runoutcome.OriginAutomationPolicy,
	"egress_decision_required":        runoutcome.OriginToolPolicy,
	"egress_decision_invalid":         runoutcome.OriginToolPolicy,
}

var legacyRunFailureCodeAliases = map[string]string{
	"approval_denied": "tool_policy_decision_failed",
}

// failedOutcomeReasons is the closed set the approved matrix allows on a failed
// lifecycle. Anything a normalizer produces outside it is a category error
// rather than a public outcome, so it collapses to internal failure.
var failedOutcomeReasons = map[runoutcome.Reason]struct{}{
	runoutcome.ReasonExpired:                {},
	runoutcome.ReasonContextLimit:           {},
	runoutcome.ReasonProviderFailure:        {},
	runoutcome.ReasonToolFailure:            {},
	runoutcome.ReasonPolicyDenied:           {},
	runoutcome.ReasonRetriesExhausted:       {},
	runoutcome.ReasonRecoveryInterrupted:    {},
	runoutcome.ReasonSideEffectUncertain:    {},
	runoutcome.ReasonApprovalDeliveryFailed: {},
	runoutcome.ReasonInternalFailure:        {},
}

// deriveOutcomeReason applies the approved lifecycle/outcome matrix.
//
// Cancellation is always abandonment. The current transport uses one signal for
// a deliberate stop and for an unkeyed disconnect, and no legacy row can tell
// them apart, so claiming user intent here would be inventing a fact.
func deriveOutcomeReason(lifecycle string, errorCode string, hasContent bool) runoutcome.Reason {
	switch lifecycle {
	case "completed":
		if hasContent {
			return runoutcome.ReasonNone
		}
		return runoutcome.ReasonCompletedNoContent
	case "cancelled":
		return runoutcome.AbandonedCancellation().Reason()
	case "failed":
		origin, allowlisted := legacyRunFailureOrigins[errorCode]
		if !allowlisted {
			origin = runoutcome.OriginUnknown
		}
		normalizedCode := errorCode
		if alias, ok := legacyRunFailureCodeAliases[errorCode]; ok {
			normalizedCode = alias
		}
		reason := runoutcome.NormalizeFailure(origin, normalizedCode, runoutcome.RetryClassNever).Reason()
		if _, allowed := failedOutcomeReasons[reason]; !allowed {
			return runoutcome.ReasonInternalFailure
		}
		return reason
	default:
		return runoutcome.ReasonNone
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// runOutcomesEventBytesExpr measures one event row's selected variable-width
// data, on the same terms as the run pass.
const runOutcomesEventBytesExpr = `
	COALESCE(length(CAST(e.id AS BLOB)), 0) +
	COALESCE(length(CAST(e.type AS BLOB)), 0) +
	COALESCE(length(CAST(e.payload_json AS BLOB)), 0) +
	COALESCE(length(CAST(e.run_id AS BLOB)), 0) +
	COALESCE(length(CAST(r.id AS BLOB)), 0) +
	COALESCE(length(CAST(r.session_id AS BLOB)), 0) +
	COALESCE(length(CAST(r.user_message_id AS BLOB)), 0) +
	COALESCE(length(CAST(r.assistant_message_id AS BLOB)), 0) +
	COALESCE(length(CAST(r.status AS BLOB)), 0) +
	COALESCE(length(CAST(r.outcome_reason AS BLOB)), 0) +
	COALESCE(length(CAST(r.state_updated_at AS BLOB)), 0) +
	COALESCE(length(CAST(r.finished_at AS BLOB)), 0) +
	COALESCE(length(CAST(m.id AS BLOB)), 0) +
	COALESCE(length(CAST(m.session_id AS BLOB)), 0) +
	COALESCE(length(CAST(m.run_id AS BLOB)), 0) +
	COALESCE(length(CAST(m.role AS BLOB)), 0)`

// runOutcomesEventJoin is shared by the length cursor and the data pass so both
// measure and read exactly the same rows.
const runOutcomesEventJoin = `
	FROM events e
	LEFT JOIN agent_runs r ON r.id = e.run_id
	LEFT JOIN ` + runOutcomesBackfillTable + ` b ON b.run_id = r.id
	LEFT JOIN messages m ON m.id = r.assistant_message_id
	WHERE e.rowid > ? AND e.rowid <= ?
		AND e.type IN (
			'agent.run.failed','agent.run.cancelled','agent.run.step',
			'approval.denied','approval.expired','tool.call.failed','tool.call.denied'
		)`

// runOutcomesEventCountJoin is the same predicate with only the upper bound
// open, for the length cursor's LIMIT-bounded page.
const runOutcomesEventCountJoin = `
	FROM events e
	LEFT JOIN agent_runs r ON r.id = e.run_id
	LEFT JOIN ` + runOutcomesBackfillTable + ` b ON b.run_id = r.id
	LEFT JOIN messages m ON m.id = r.assistant_message_id
	WHERE e.rowid > ?
		AND e.type IN (
			'agent.run.failed','agent.run.cancelled','agent.run.step',
			'approval.denied','approval.expired','tool.call.failed','tool.call.denied'
		)`

var errRunOutcomeCanonicalFields = errors.New("run outcome migration produced invalid canonical state")

// runStateProjection is the durable public shape of a run snapshot. Only these
// fields are ever written into a failure event: no code, no message, no reason
// string, and nothing a provider, a tool, or a human reviewer authored.
type runStateProjection struct {
	RunID                 string `json:"runId"`
	UserMessageID         string `json:"userMessageId"`
	AssistantMessageID    string `json:"assistantMessageId,omitempty"`
	Lifecycle             string `json:"lifecycle"`
	OutcomeReason         string `json:"outcomeReason"`
	StateVersion          int64  `json:"stateVersion"`
	StateUpdatedAt        string `json:"stateUpdatedAt"`
	FinishedAt            string `json:"finishedAt,omitempty"`
	HasDisplayableContent bool   `json:"hasDisplayableContent"`
}

// legacyEventRow is one durable failure-like event joined to its run's freshly
// migrated canonical state.
type legacyEventRow struct {
	rowID       int64
	id          string
	eventType   string
	payloadJSON string
	runFound    bool
	state       runStateProjection
}

func runOutcomesAfterHook(ctx context.Context, tx *sql.Tx) error {
	if err := rewritePublicFailureEvents(ctx, tx); err != nil {
		return err
	}
	if err := requireUniqueRunMessageCorrelation(ctx, tx); err != nil {
		return err
	}
	if err := requireCanonicalRunState(ctx, tx); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DROP TABLE `+runOutcomesBackfillTable)
	return err
}

func rewritePublicFailureEvents(ctx context.Context, tx *sql.Tx) error {
	var cursor int64
	for {
		lastRowID, batchRows, batchBytes, err := runOutcomeEventBatchBounds(ctx, tx, cursor)
		if err != nil {
			return err
		}
		if batchRows == 0 {
			return nil
		}
		observeRunOutcomeBatch(runOutcomesEventScan, batchRows, batchBytes)
		rows, err := readLegacyEventBatch(ctx, tx, cursor, lastRowID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			payload, changed, err := rewriteFailureEventPayload(row)
			if err != nil {
				return err
			}
			if !changed {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE events SET payload_json = ? WHERE id = ?`, payload, row.id); err != nil {
				return err
			}
		}
		cursor = lastRowID
	}
}

func runOutcomeEventBatchBounds(ctx context.Context, tx *sql.Tx, cursor int64) (int64, int, int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT e.rowid, `+runOutcomesEventBytesExpr+runOutcomesEventCountJoin+`
		ORDER BY e.rowid
		LIMIT ?
	`, cursor, runOutcomesBatchRows)
	if err != nil {
		return 0, 0, 0, err
	}
	lastRowID, count, total, err := accumulateBatchBounds(rows)
	if closeErr := rows.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, 0, 0, err
	}
	return lastRowID, count, total, nil
}

func readLegacyEventBatch(ctx context.Context, tx *sql.Tx, cursor int64, lastRowID int64) ([]legacyEventRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT e.rowid, e.id, e.type, e.payload_json,
			r.id, r.session_id, r.user_message_id, r.assistant_message_id, r.status, r.outcome_reason,
			r.state_version, r.state_updated_at, r.finished_at, b.has_displayable_content,
			m.id, m.session_id, m.run_id, m.role
		`+runOutcomesEventJoin+`
		ORDER BY e.rowid
	`, cursor, lastRowID)
	if err != nil {
		return nil, err
	}
	batch := make([]legacyEventRow, 0, runOutcomesBatchRows)
	for rows.Next() {
		var (
			row                legacyEventRow
			runID              sql.NullString
			runSessionID       sql.NullString
			userMessageID      sql.NullString
			runAssistantID     sql.NullString
			status             sql.NullString
			outcomeReason      sql.NullString
			stateVersion       sql.NullInt64
			stateUpdatedAt     sql.NullString
			finishedAt         sql.NullString
			displayableContent sql.NullInt64
			messageID          sql.NullString
			messageSessionID   sql.NullString
			messageRunID       sql.NullString
			messageRole        sql.NullString
		)
		if err := rows.Scan(
			&row.rowID, &row.id, &row.eventType, &row.payloadJSON,
			&runID, &runSessionID, &userMessageID, &runAssistantID, &status, &outcomeReason,
			&stateVersion, &stateUpdatedAt, &finishedAt, &displayableContent,
			&messageID, &messageSessionID, &messageRunID, &messageRole,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		row.runFound = runID.Valid
		// finished_at is copied verbatim by the rebuild, so it still holds
		// whatever shape a legacy writer chose. The public payload this
		// migration newly writes is text clients compare, so it is re-rendered
		// at the canonical width here; the stored column keeps its own text.
		// An empty string is a value a legacy writer cleared, not a time.
		publishedFinishedAt := ""
		if finishedAt.Valid && finishedAt.String != "" {
			canonical, err := canonicalLegacyTime(finishedAt.String)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			publishedFinishedAt = canonical
		}
		row.state = runStateProjection{
			RunID:                 runID.String,
			UserMessageID:         userMessageID.String,
			Lifecycle:             status.String,
			OutcomeReason:         outcomeReason.String,
			StateVersion:          stateVersion.Int64,
			StateUpdatedAt:        stateUpdatedAt.String,
			FinishedAt:            publishedFinishedAt,
			HasDisplayableContent: displayableContent.Int64 == 1,
		}
		// The assistant message ID is published only when the link proves
		// itself in both directions; a half-written legacy link names nothing
		// the client could trust.
		if runcorrelation.Validate(runcorrelation.Link{
			RunID:                 runID.String,
			RunSessionID:          runSessionID.String,
			RunAssistantMessageID: runAssistantID.String,
			MessageID:             messageID.String,
			MessageSessionID:      messageSessionID.String,
			MessageRunID:          messageRunID.String,
			MessageRole:           messageRole.String,
		}) == nil {
			row.state.AssistantMessageID = runAssistantID.String
		}
		batch = append(batch, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return batch, nil
}

// approvalEventIdentityKeys and toolCallEventIdentityKeys are the only payload
// keys that survive a rewrite. Everything else — message, reason, error,
// arbitrary policy text — is dropped rather than renamed, so a rewritten event
// cannot smuggle its old diagnostic under a new name.
var approvalEventIdentityKeys = []string{"approvalId", "toolCallId", "toolName", "runId", "traceId", "modelToolCallId"}

var toolCallEventIdentityKeys = []string{"toolCallId", "toolName", "serverName", "modelToolCallId"}

// reservedRetryNoticeKeys are the payload keys only the repository's retry and
// recovery notice writer ever sets on an agent.run.step row (see
// repositoryAuthoredStepKeys in the events package's public-read boundary,
// which this list mirrors so the two boundaries answer the same question
// identically). Their presence — not whether their values still parse — is
// what marks a legacy row as a retry-notice attempt rather than a governed
// non-retry step, so a value this build cannot trust must still resolve to a
// bounded typed notice instead of being left as an unrewritten row that keeps
// republishing its raw note and reason forever.
var reservedRetryNoticeKeys = []string{"category", "attempt", "attempts", "maxAttempts", "stateVersion"}

func hasReservedRetryNoticeKey(payload map[string]any) bool {
	for _, key := range reservedRetryNoticeKeys {
		if _, ok := payload[key]; ok {
			return true
		}
	}
	return false
}

func rewriteFailureEventPayload(row legacyEventRow) (string, bool, error) {
	switch row.eventType {
	case "agent.run.failed", "agent.run.cancelled":
		if !row.runFound {
			return marshalEventPayload(map[string]any{})
		}
		return marshalEventPayload(map[string]any{"runState": row.state})
	case "agent.run.step":
		return rewriteRunStepNotice(row)
	case "approval.denied", "approval.expired":
		return rewriteIdentityEvent(row, approvalEventIdentityKeys, approvalEventCategory(row.eventType))
	case "tool.call.failed", "tool.call.denied":
		return rewriteIdentityEvent(row, toolCallEventIdentityKeys, toolCallEventCategory(row.eventType))
	default:
		return "", false, nil
	}
}

// approvalEventCategory and toolCallEventCategory read the category off the
// event type, which the server chose, rather than off the payload, which a
// failing provider or tool influenced.
//
// approvalEventCategory defers to the same rule the live writers use, so a
// rewritten approval failure and a freshly written one are indistinguishable.
// It is total where the shared rule is partial because it is only ever reached
// for the two approval failure types dispatched above.
func approvalEventCategory(eventType string) runoutcome.Reason {
	if category, ok := runoutcome.ApprovalFailureCategory(eventType); ok {
		return category
	}
	return runoutcome.ReasonPolicyDenied
}

func toolCallEventCategory(eventType string) runoutcome.Reason {
	if category, ok := runoutcome.ToolCallFailureCategory(eventType); ok {
		return category
	}
	return runoutcome.ReasonToolFailure
}

func rewriteIdentityEvent(row legacyEventRow, identityKeys []string, category runoutcome.Reason) (string, bool, error) {
	legacy, err := decodeEventPayload(row.payloadJSON)
	if err != nil {
		return "", false, err
	}
	rewritten := make(map[string]any, len(identityKeys)+1)
	for _, key := range identityKeys {
		value, present := legacy[key]
		text, isText := value.(string)
		if !present || !isText || text == "" {
			continue
		}
		rewritten[key] = text
	}
	rewritten["category"] = string(category)
	return marshalEventPayload(rewritten)
}

// rewriteRunStepNotice rewrites only the failure-like notices. A run-step
// payload that carries none of reservedRetryNoticeKeys is a redacted-egress or
// model-limit projection, which is governed elsewhere and is left exactly as
// it was.
//
// A payload that does carry one of those keys is claiming to be a
// repository-authored retry or recovery notice, and that claim is resolved to
// a bounded typed notice below whether or not the counters it carries still
// parse — gating on a successful parse instead would read a malformed counter
// as "this key was never here" and leave the row's raw note and reason
// unrewritten, which is exactly the row this rewrite exists to close.
func rewriteRunStepNotice(row legacyEventRow) (string, bool, error) {
	legacy, err := decodeEventPayload(row.payloadJSON)
	if err != nil {
		return "", false, err
	}
	if !hasReservedRetryNoticeKey(legacy) {
		return "", false, nil
	}
	maxAttempts, _ := payloadInt32(legacy, "maxAttempts")
	attempt, _ := payloadInt32(legacy, "attempt")
	attempts, _ := payloadInt32(legacy, "attempts")
	_, isGiveUp := legacy["attempts"]
	category := runoutcome.NoticeDispatchRetry
	switch {
	case isGiveUp:
		category = runoutcome.NoticeRecoveryExhausted
		attempt = attempts
	case payloadString(legacy, "reason") == "worker_unavailable":
		category = runoutcome.NoticeRecoveryRetry
	}
	rewritten := map[string]any{"category": string(category)}
	// The version is published only when a run row actually supplied one. On
	// the wire stateVersion is an int64 whose zero means absence, so writing
	// the Go zero value for an event whose run is gone would not read as "no
	// version known" — it would read as a real version older than every stored
	// one, and a client reconciling by version would treat the notice as stale
	// rather than as unattributed.
	if row.runFound {
		rewritten["stateVersion"] = row.state.StateVersion
	}
	// Counters are published only when they pass the notice constructor's
	// bounds. A legacy row with an impossible budget keeps its category and
	// loses the numbers rather than persisting an unbounded count.
	if notice, err := runoutcome.NewStepNotice(category, attempt, maxAttempts); err == nil {
		rewritten["attempt"] = notice.Attempt()
		rewritten["maxAttempts"] = notice.MaxAttempts()
	}
	return marshalEventPayload(rewritten)
}

func decodeEventPayload(payloadJSON string) (map[string]any, error) {
	payload := map[string]any{}
	if strings.TrimSpace(payloadJSON) == "" {
		return payload, nil
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		// A payload this build cannot parse is rewritten to the safe shape
		// anyway; returning the parser's message here would republish the very
		// bytes the rewrite exists to remove.
		return map[string]any{}, nil
	}
	return payload, nil
}

func marshalEventPayload(payload any) (string, bool, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", false, err
	}
	return string(encoded), true, nil
}

// payloadInt32 accepts only an exact integral value in the supported int32
// range. A stored counter this build ever wrote is always a whole number, so a
// fractional value (2.9) is not a counter that lost precision — it is a value
// no writer here produced, and truncating it would fabricate a
// plausible-looking count. Out of range and fractional are both reported as
// present-but-unusable (ok=true, value=0) rather than absent, so a caller can
// still tell "this key was never here" apart from "this key was here but
// broken".
func payloadInt32(payload map[string]any, key string) (int32, bool) {
	number, ok := payload[key].(float64)
	if !ok {
		return 0, false
	}
	if number != math.Trunc(number) {
		return 0, true
	}
	if number < math.MinInt32 || number > math.MaxInt32 {
		return 0, true
	}
	return int32(number), true
}

func payloadString(payload map[string]any, key string) string {
	text, _ := payload[key].(string)
	return text
}

// requireCanonicalRunState proves the rebuild produced state every later reader
// can rely on, before the transaction is allowed to commit. The check counts
// violations rather than returning them: a row value has no place in a
// migration error.
func requireCanonicalRunState(ctx context.Context, tx *sql.Tx) error {
	var invalid int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agent_runs
		WHERE state_version < 1
			OR length(state_updated_at) <> 30
			OR length(assistant_content_sha256) <> 64
			OR outcome_reason NOT IN (
				'none','completed_no_content','user_cancelled','abandoned','expired','context_limit',
				'provider_failure','tool_failure','policy_denied','retries_exhausted',
				'recovery_interrupted','side_effect_uncertain','approval_delivery_failed','internal_failure'
			)
	`).Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return errRunOutcomeCanonicalFields
	}
	// Correlation is deliberately not re-judged here beyond the duplicate
	// preflight above. A mutually named pair that disagrees about session or
	// role still has exactly one claimant on each side, so it has one honest
	// reading: the link is unusable. deriveRunOutcome already refused to adopt
	// its content on a terminal row, and already refused to migrate a
	// nonterminal one at all, and the two partial unique indexes still prevent
	// a second claimant from appearing. Aborting a whole migration over a
	// terminal row the fallback handles neutrally would contradict the
	// fallback.
	return nil
}
