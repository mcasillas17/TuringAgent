package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ErrSessionNotFound reports a delete for a session id that does not exist, so
// the caller can answer NotFound rather than reporting a success that removed
// nothing.
var ErrSessionNotFound = errors.New("session not found")

// ErrSessionHasActiveRun reports a delete refused because the session still has
// work in flight. Removing the rows would leave the runtime finishing a run
// whose session, messages and job no longer exist, and reconciliation would
// then operate on nothing.
var ErrSessionHasActiveRun = errors.New("session has an active run")

// ErrSessionDeleting reports a read or mutation against a session whose
// withdrawal transaction has begun. Callers must not treat it as a successful
// lookup or retry the operation against the same logical session.
var ErrSessionDeleting = errors.New("session deletion is in progress")

// SessionDeletionReceipt is the content-free progress record for one
// idempotent session withdrawal. It intentionally contains no session title,
// message, path, tool arguments, result, or external error text.
type SessionDeletionReceipt struct {
	SessionID                   string
	LifecycleVersion            int64
	State                       string
	TerminalSequence            int64
	Retryable                   bool
	ErrorCode                   string
	RunCount                    int
	MessageCount                int
	RetainedLegacyArtifactCount int
}

// scrubbedAuditPayload replaces audit content on deletion. It is a tombstone
// rather than NULL so a reader can tell "scrubbed because the user deleted the
// session" from "never carried a payload".
const scrubbedAuditPayload = `{"scrubbed":true}`

const sessionDeletionQuiesceLease = 5 * time.Minute

const scrubSessionAuditPayloadsSQL = `
	UPDATE audit_logs SET payload_json = ?
	WHERE correlation_id IN (SELECT id FROM agent_runs WHERE session_id = ?)
		OR (
			target = ?
			AND (correlation_id IS NULL OR correlation_id = '')
		)
`

// BeginSessionDeletion makes a session immediately unavailable to ordinary
// reads and later advances use the durable receipt rather than starting a
// second withdrawal.
func (r *Repository) BeginSessionDeletion(ctx context.Context, sessionID string) (SessionDeletionReceipt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionDeletionReceipt{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var receipt SessionDeletionReceipt
	err = tx.QueryRowContext(ctx, `
		SELECT session_id, lifecycle_version, state, terminal_sequence, retryable,
			COALESCE(error_code, ''), run_count, message_count, retained_legacy_artifact_count
		FROM session_deletions
		WHERE session_id = ?
	`, sessionID).Scan(
		&receipt.SessionID,
		&receipt.LifecycleVersion,
		&receipt.State,
		&receipt.TerminalSequence,
		&receipt.Retryable,
		&receipt.ErrorCode,
		&receipt.RunCount,
		&receipt.MessageCount,
		&receipt.RetainedLegacyArtifactCount,
	)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return SessionDeletionReceipt{}, err
		}
		return receipt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SessionDeletionReceipt{}, err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET deletion_state = 'deleting'
		WHERE id = ? AND deletion_state = 'active'
	`, sessionID)
	if err != nil {
		return SessionDeletionReceipt{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return SessionDeletionReceipt{}, err
	}
	if changed == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID).Scan(&exists); err != nil {
			return SessionDeletionReceipt{}, err
		}
		if exists == 0 {
			return SessionDeletionReceipt{}, ErrSessionNotFound
		}
		return SessionDeletionReceipt{}, ErrSessionDeleting
	}

	receipt = SessionDeletionReceipt{
		SessionID:        sessionID,
		LifecycleVersion: 1,
		State:            "quiescing",
		Retryable:        true,
	}
	if _, err := tx.ExecContext(
		ctx,
		scrubSessionAuditPayloadsSQL,
		scrubbedAuditPayload,
		sessionID,
		sessionID,
	); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs WHERE session_id = ?`, sessionID).Scan(&receipt.RunCount); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&receipt.MessageCount); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if err := cancelSessionWorkTx(ctx, tx, sessionID); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM events WHERE session_id = ?`, sessionID).Scan(&receipt.TerminalSequence); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_deletions (
			session_id, lifecycle_version, state, quiesce_deadline_at, terminal_sequence,
			retryable, run_count, message_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		receipt.SessionID,
		receipt.LifecycleVersion,
		receipt.State,
		FormatTimestamp(time.Now().UTC().Add(sessionDeletionQuiesceLease)),
		receipt.TerminalSequence,
		receipt.Retryable,
		receipt.RunCount,
		receipt.MessageCount,
	); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionDeletionReceipt{}, err
	}
	return receipt, nil
}

func cancelSessionWorkTx(ctx context.Context, tx *sql.Tx, sessionID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM agent_runs
		WHERE session_id = ? AND status IN ('queued', 'running', 'waiting_approval')
		ORDER BY created_at, id
	`, sessionID)
	if err != nil {
		return err
	}
	var runIDs []string
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			return errors.Join(err, rows.Close())
		}
		runIDs = append(runIDs, runID)
	}
	if err := rows.Err(); err != nil {
		return errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return err
	}

	finishedAt := now()
	for _, runID := range runIDs {
		if _, err := failPendingApprovalLifecycleTx(ctx, tx, runID, "session_deleting", "Session deletion is in progress", finishedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE jobs
			SET status = 'cancelled',
				finished_at = ?,
				error_code = 'session_deleting',
				error_message = 'Session deletion is in progress',
				lease_owner = NULL,
				lease_expires_at = NULL,
				lease_expires_at_ns = NULL
			WHERE run_id = ? AND status IN ('pending', 'in_progress')
		`, finishedAt, runID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_runs
			SET status = 'cancelled',
				cancellation_reason = 'session_deleting',
				finished_at = ?
			WHERE id = ? AND status IN ('queued', 'running', 'waiting_approval')
		`, finishedAt, runID); err != nil {
			return err
		}
	}
	return nil
}

// AdvanceSessionDeletion progresses a receipt without waiting for an active
// runtime. A caller can retry it after the runtime acknowledges its exit.
func (r *Repository) AdvanceSessionDeletion(ctx context.Context, sessionID string) (SessionDeletionReceipt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionDeletionReceipt{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var receipt SessionDeletionReceipt
	if err := tx.QueryRowContext(ctx, `
		SELECT session_id, lifecycle_version, state, terminal_sequence, retryable,
			COALESCE(error_code, ''), run_count, message_count, retained_legacy_artifact_count
		FROM session_deletions
		WHERE session_id = ?
	`, sessionID).Scan(
		&receipt.SessionID,
		&receipt.LifecycleVersion,
		&receipt.State,
		&receipt.TerminalSequence,
		&receipt.Retryable,
		&receipt.ErrorCode,
		&receipt.RunCount,
		&receipt.MessageCount,
		&receipt.RetainedLegacyArtifactCount,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionDeletionReceipt{}, ErrSessionNotFound
		}
		return SessionDeletionReceipt{}, err
	}
	if receipt.State == "completed" {
		if err := tx.Commit(); err != nil {
			return SessionDeletionReceipt{}, err
		}
		return receipt, nil
	}

	var (
		activeExecutions int
		quiesceDeadline  string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM agent_runs
		WHERE session_id = ? AND execution_active = 1
	`, sessionID).Scan(&activeExecutions); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if activeExecutions > 0 {
		if err := tx.QueryRowContext(ctx, `
			SELECT quiesce_deadline_at
			FROM session_deletions
			WHERE session_id = ?
		`, sessionID).Scan(&quiesceDeadline); err != nil {
			return SessionDeletionReceipt{}, err
		}
		deadline, err := time.Parse(time.RFC3339Nano, quiesceDeadline)
		if err != nil {
			return SessionDeletionReceipt{}, err
		}
		if !time.Now().UTC().Before(deadline) {
			receipt.State = "failed_external"
			receipt.Retryable = true
			receipt.ErrorCode = "execution_unreconciled"
			if _, err := tx.ExecContext(ctx, `
				UPDATE session_deletions
				SET state = ?, retryable = 1, error_code = ?
				WHERE session_id = ?
			`, receipt.State, receipt.ErrorCode, sessionID); err != nil {
				return SessionDeletionReceipt{}, err
			}
			if err := tx.Commit(); err != nil {
				return SessionDeletionReceipt{}, err
			}
			return receipt, nil
		}
		receipt.State = "quiescing"
		receipt.Retryable = true
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_deletions
			SET state = ?, retryable = 1
			WHERE session_id = ?
		`, receipt.State, sessionID); err != nil {
			return SessionDeletionReceipt{}, err
		}
		if err := tx.Commit(); err != nil {
			return SessionDeletionReceipt{}, err
		}
		return receipt, nil
	}
	var pendingArtifacts int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sandbox_artifacts
		WHERE session_id = ? AND policy = 'delete_on_session_delete'
	`, sessionID).Scan(&pendingArtifacts); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if pendingArtifacts > 0 {
		receipt.State = "failed_external"
		receipt.Retryable = true
		receipt.ErrorCode = "artifact_cleanup_pending"
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_deletions
			SET state = ?, retryable = 1, error_code = ?
			WHERE session_id = ?
		`, receipt.State, receipt.ErrorCode, sessionID); err != nil {
			return SessionDeletionReceipt{}, err
		}
		if err := tx.Commit(); err != nil {
			return SessionDeletionReceipt{}, err
		}
		return receipt, nil
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT physical_path)
		FROM sandbox_artifacts
		WHERE session_id = ? AND policy = ?
	`, sessionID, SandboxArtifactPolicyRetainLegacyUnowned).Scan(&receipt.RetainedLegacyArtifactCount); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_deletions
		SET retained_legacy_artifact_count = ?
		WHERE session_id = ?
	`, receipt.RetainedLegacyArtifactCount, sessionID); err != nil {
		return SessionDeletionReceipt{}, err
	}

	if _, err := tx.ExecContext(
		ctx,
		scrubSessionAuditPayloadsSQL,
		scrubbedAuditPayload,
		sessionID,
		sessionID,
	); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND deletion_state = 'deleting'`, sessionID); err != nil {
		return SessionDeletionReceipt{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"runs":     receipt.RunCount,
		"messages": receipt.MessageCount,
	})
	if err != nil {
		return SessionDeletionReceipt{}, err
	}
	if err := recordAuditTx(ctx, tx, "", "client", "", "session.deleted", sessionID, string(payload)); err != nil {
		return SessionDeletionReceipt{}, err
	}
	completedAt := now()
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_deletions
		SET state = 'completed',
			terminal_at = ?,
			deleted_at = ?,
			error_code = NULL,
			retryable = 0
		WHERE session_id = ?
	`, completedAt, completedAt, sessionID); err != nil {
		return SessionDeletionReceipt{}, err
	}
	receipt.State = "completed"
	receipt.Retryable = false
	receipt.ErrorCode = ""
	if err := tx.Commit(); err != nil {
		return SessionDeletionReceipt{}, err
	}
	return receipt, nil
}

// SessionExecutionRunIDs returns only executions that must receive a runtime
// cancellation command while a deletion receipt is quiescing.
func (r *Repository) SessionExecutionRunIDs(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id
		FROM agent_runs
		WHERE session_id = ? AND execution_active = 1
		ORDER BY created_at, id
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var runIDs []string
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			return nil, err
		}
		runIDs = append(runIDs, runID)
	}
	return runIDs, rows.Err()
}

// MarkSessionDeletionExternalFailure preserves only an opaque error class when
// a bounded external cleanup attempt fails. The detail stays out of durable
// state because it can contain filesystem or transport information.
func (r *Repository) MarkSessionDeletionExternalFailure(ctx context.Context, sessionID string, errorCode string) error {
	if errorCode == "" {
		return errors.New("session deletion external failure code is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE session_deletions
		SET state = 'failed_external', retryable = 1, error_code = ?
		WHERE session_id = ? AND state <> 'completed'
	`, errorCode, sessionID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM session_deletions WHERE session_id = ?`, sessionID).Scan(&state); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrSessionNotFound
			}
			return err
		}
		if state == "completed" {
			return tx.Commit()
		}
		return ErrSessionNotFound
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM sandbox_artifacts
		WHERE session_id = ? AND policy = ? AND state <> ?
		ORDER BY id
	`, sessionID, SandboxArtifactPolicyDeleteOnSessionDelete, SandboxArtifactStateDeleteFailed)
	if err != nil {
		return err
	}
	var artifactIDs []string
	for rows.Next() {
		var artifactID string
		if err := rows.Scan(&artifactID); err != nil {
			return errors.Join(err, rows.Close())
		}
		artifactIDs = append(artifactIDs, artifactID)
	}
	if err := rows.Err(); err != nil {
		return errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE sandbox_artifacts
		SET state = ?
		WHERE session_id = ? AND policy = ? AND state <> ?
	`, SandboxArtifactStateDeleteFailed, sessionID, SandboxArtifactPolicyDeleteOnSessionDelete, SandboxArtifactStateDeleteFailed); err != nil {
		return err
	}
	payloadBytes, err := json.Marshal(map[string]string{
		"policy":    SandboxArtifactPolicyDeleteOnSessionDelete,
		"state":     SandboxArtifactStateDeleteFailed,
		"errorCode": errorCode,
	})
	if err != nil {
		return err
	}
	for _, artifactID := range artifactIDs {
		if err := recordAuditTx(ctx, tx, "", "system", "", "session.artifact.cleanup.failed", artifactID, string(payloadBytes)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PendingSessionDeletionIDs returns receipts that must be retried after a
// restart or transient external-cleanup failure. Completed receipts are
// durable tombstones, not reconciliation work.
func (r *Repository) PendingSessionDeletionIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT session_id
		FROM session_deletions
		WHERE state <> 'completed'
		ORDER BY session_id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var sessionIDs []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return nil, err
		}
		sessionIDs = append(sessionIDs, sessionID)
	}
	return sessionIDs, rows.Err()
}

// PendingSessionDeletionReceipts exposes only content-free retry state for
// client recovery. It intentionally does not join sessions, messages, events,
// or artifact paths.
func (r *Repository) PendingSessionDeletionReceipts(ctx context.Context) ([]SessionDeletionReceipt, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT session_id, lifecycle_version, state, terminal_sequence, retryable,
			COALESCE(error_code, ''), run_count, message_count, retained_legacy_artifact_count
		FROM session_deletions
		WHERE state <> 'completed'
		ORDER BY session_id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var receipts []SessionDeletionReceipt
	for rows.Next() {
		var receipt SessionDeletionReceipt
		if err := rows.Scan(
			&receipt.SessionID,
			&receipt.LifecycleVersion,
			&receipt.State,
			&receipt.TerminalSequence,
			&receipt.Retryable,
			&receipt.ErrorCode,
			&receipt.RunCount,
			&receipt.MessageCount,
			&receipt.RetainedLegacyArtifactCount,
		); err != nil {
			return nil, err
		}
		receipts = append(receipts, receipt)
	}
	return receipts, rows.Err()
}

// SessionDeletionReceipt returns one content-free receipt after its session
// root may already have been cascaded away.
func (r *Repository) SessionDeletionReceipt(ctx context.Context, sessionID string) (SessionDeletionReceipt, error) {
	var receipt SessionDeletionReceipt
	err := r.db.QueryRowContext(ctx, `
		SELECT session_id, lifecycle_version, state, terminal_sequence, retryable,
			COALESCE(error_code, ''), run_count, message_count, retained_legacy_artifact_count
		FROM session_deletions
		WHERE session_id = ?
	`, sessionID).Scan(
		&receipt.SessionID,
		&receipt.LifecycleVersion,
		&receipt.State,
		&receipt.TerminalSequence,
		&receipt.Retryable,
		&receipt.ErrorCode,
		&receipt.RunCount,
		&receipt.MessageCount,
		&receipt.RetainedLegacyArtifactCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionDeletionReceipt{}, ErrSessionNotFound
	}
	if err != nil {
		return SessionDeletionReceipt{}, err
	}
	return receipt, nil
}

// DeleteSession removes a session and everything it produced, and scrubs the
// content out of the audit rows it leaves behind.
//
// Two things about this are load-bearing and easy to get wrong:
//
// It is a real DELETE, never a soft-delete flag. `messages_fts_ad` is an AFTER
// DELETE trigger, so only a real delete removes the rows from the FTS index. A
// flag would leave every "deleted" message searchable, and therefore still
// reachable by cross-session recall — the system would keep remembering exactly
// what the user asked it to forget.
//
// The audit scrub runs BEFORE the cascade, as a single statement whose
// subquery resolves run ownership inline. Run-owned rows link through
// correlation_id; uncorrelated session-level actions, including routing rows,
// use the session as their target. Both links disappear or become unresolvable
// after deletion, so the update resolves them while the source rows still
// exist.
func (r *Repository) DeleteSession(ctx context.Context, sessionID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrSessionNotFound
	}

	// Refuse before mutating anything, so a rejected delete leaves no trace.
	//
	// Status alone is not enough — two paths leave a run terminal-by-status with
	// execution still live:
	//   - CancelRun / CancelRunWithEvent set status='cancelled' and never touch
	//     execution_active at all (runs.go). This is the user-reachable one.
	//   - failRunWithEventTx(preserveExecution=true) keeps execution_active = 1
	//     with execution_state = 'uncertain'.
	// The recovery machinery treats both as in flight (assignments.go queries
	// execution_active = 1 alongside terminal statuses). Deleting then cascades
	// rows out from under a worker that has not acknowledged exit, and the
	// resulting unmapped sql.ErrNoRows tears down the whole ConnectWorker
	// stream — taking every other run that worker holds with it.
	var active int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agent_runs
		WHERE session_id = ?
			AND (status IN ('queued','running','waiting_approval') OR execution_active = 1)
	`, sessionID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return ErrSessionHasActiveRun
	}

	var runCount, messageCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs WHERE session_id = ?`, sessionID).Scan(&runCount); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&messageCount); err != nil {
		return err
	}

	// Scrub both run-owned rows and every uncorrelated session-targeted action.
	// The latter includes routing payloads and future derived session actions.
	// session.deleted is inserted after this statement and deliberately remains
	// PRESENT as evidence of the deletion.
	if _, err := tx.ExecContext(
		ctx,
		scrubSessionAuditPayloadsSQL,
		scrubbedAuditPayload,
		sessionID,
		sessionID,
	); err != nil {
		return err
	}

	// Let the FK graph do the deleting. Foreign keys are enforced
	// (`_foreign_keys=on` in the DSN), so this cascades to messages, agent_runs,
	// and from there to jobs, events, tool_calls and approvals. Hand-deleting
	// those here would duplicate the schema in Go and drift from it.
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
		return err
	}

	// The deletion is itself auditable, and is not scrubbed. correlation_id is
	// left empty: every other call site puts a RUN id there, and reusing the
	// column for a session id would conflate two kinds under one index. The
	// session id is the target, which is what it means.
	//
	// Counts make the row evidence rather than a marker — "something was
	// deleted" is far less useful than how much.
	payload, err := json.Marshal(map[string]any{"runs": runCount, "messages": messageCount})
	if err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, "", "client", "", "session.deleted", sessionID, string(payload)); err != nil {
		return err
	}
	return tx.Commit()
}
