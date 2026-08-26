package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
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

// Withdrawal error classes. Each one is an opaque class and nothing else: a
// receipt is read by a client and projected into an audit row, so it must never
// carry a path, a transport message, or anything else the failure knew.
const (
	// SessionDeletionArtifactCleanupPending is the one literal that says a
	// withdrawal is waiting on external files. Both manifests raise it, and the
	// dispatch gate matches it exactly — a second spelling for "there are still
	// files" is a withdrawal that never dispatches a cleaner and reports
	// completion with the user's notes still on disk.
	SessionDeletionArtifactCleanupPending = "artifact_cleanup_pending"
	// SessionDeletionSandboxCleanupFailed names a sandbox cleanup that could
	// not finish.
	SessionDeletionSandboxCleanupFailed = "artifact_cleanup_failed"
	// SessionDeletionVaultCleanupFailed names a vault cleanup that could not
	// finish. It is a separate class because the two failures are separately
	// retryable and land in separate manifests.
	SessionDeletionVaultCleanupFailed = "vault_artifact_cleanup_failed"
	// SessionDeletionArtifactManifestFinalizeFailed names a withdrawal whose
	// external files are gone and whose manifest rows could not be dropped.
	//
	// It is deliberately not one of the cleanup-failed classes. Those mean
	// "the file is still there", and they are recorded by marking every row
	// delete_failed with one audit row each — a per-file claim that Turing
	// could not remove a file it in fact removed. This class says the opposite
	// and truthful thing: the removal happened, the bookkeeping did not, and
	// the rows are being kept because the retry needs them.
	SessionDeletionArtifactManifestFinalizeFailed = "artifact_manifest_finalize_failed"
	// SessionDeletionUnsupportedArtifactScope names a cleaner that failed under
	// a scope this withdrawal has no manifest for. There is nothing to mark and
	// nothing to audit, so the receipt is the only place the failure can live —
	// and it has to live somewhere, because the alternative is handing back the
	// pending gate and waiting forever on a cleaner that already failed.
	SessionDeletionUnsupportedArtifactScope = "artifact_scope_unsupported"
	// SessionDeletionMemoryReconcileFailed names a withdrawal whose rows are
	// gone but whose on-disk completion could not be written.
	SessionDeletionMemoryReconcileFailed = "memory_reconcile_failed"
)

// SessionDeletionCompletion is the on-disk work a withdrawal must finish before
// its receipt may claim completion.
//
// It runs after the cascade has committed and before the receipt is marked
// completed, with no transaction open and no repository lock held. Both halves
// of that are load-bearing:
//
//   - After the cascade, because what it has to repair is a note citing a
//     conversation that no longer exists, and it cannot see that while the
//     session row is still there.
//   - Before the completion mark, because a receipt that says "completed" while
//     a belief in the user's vault still names the deleted conversation is a
//     withdrawal Turing reported and did not finish. A failure here leaves the
//     receipt retryable instead, and the rows stay gone — that half of the
//     promise is already kept and is never undone.
//
// It is not called inside the transaction. A vault pass walks the filesystem
// and takes a vault-wide lock; holding SQLite open across either is how a
// withdrawal and a reconcile wedge against each other.
type SessionDeletionCompletion func(context.Context) error

const sessionDeletionQuiesceLease = 5 * time.Minute

// scrubSessionAuditPayloadsSQL empties the content out of every audit row a
// withdrawn session left behind.
//
// session.deleted is excluded by name. It is the evidence that the withdrawal
// happened, its payload is a pair of counts rather than anything the user
// wrote, and a withdrawal is now advanced across more than one transaction — a
// retry after an unfinished completion re-runs this statement over a
// session.deleted row an earlier attempt already wrote. Without the exclusion
// that retry replaces the counts with the tombstone, and the record of how much
// was removed is lost to the act of finishing the removal.
const scrubSessionAuditPayloadsSQL = `
	UPDATE audit_logs SET payload_json = ?
	WHERE action <> 'session.deleted'
		AND (
			correlation_id IN (SELECT id FROM agent_runs WHERE session_id = ?)
			OR (
				target = ?
				AND (correlation_id IS NULL OR correlation_id = '')
			)
		)
`

// BeginSessionDeletion makes a session immediately unavailable to ordinary
// reads and later advances use the durable receipt rather than starting a
// second withdrawal.
//
// It takes the session's decision lock for exactly this transaction. A decision
// about one of the session's memory proposals holds that same lock across its
// whole mutation window — the file it moves and the profile it writes — so the
// two can never interleave: either the decision finishes and this withdraws
// what it produced, or this flips the state first and every decision after it
// is refused before it has touched anything. The lock is released here, before
// any cleaner or vault reconcile runs, because holding it across a filesystem
// walk is how a withdrawal and a pass wedge against each other.
func (r *Repository) BeginSessionDeletion(ctx context.Context, sessionID string) (SessionDeletionReceipt, error) {
	unlockSession, err := lockSessionDecision(ctx, sessionID)
	if err != nil {
		return SessionDeletionReceipt{}, err
	}
	defer unlockSession()

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
		WHERE session_id = ? AND status IN ('queued', 'running', 'waiting_approval', 'recovering')
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

	for _, runID := range runIDs {
		if _, err := cancelRunTx(ctx, tx, CancelRunInput{
			RunID:              runID,
			Cancellation:       runoutcome.AbandonedCancellation(),
			resolveVersionInTx: true,
		}); err != nil {
			return err
		}
	}
	return nil
}

// AdvanceSessionDeletion progresses a receipt without waiting for an active
// runtime. A caller can retry it after the runtime acknowledges its exit.
//
// completion is the on-disk work the withdrawal owes once its rows are gone.
// Pass nil when there is none.
func (r *Repository) AdvanceSessionDeletion(ctx context.Context, sessionID string, completion SessionDeletionCompletion) (SessionDeletionReceipt, error) {
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
	// Both manifests are counted, and counted separately. They answer different
	// retention questions over different roots — scratch output inside the tool
	// sandbox, and user-visible notes inside the vault the user opens — and a
	// session can own files in one and none in the other. Summing them into a
	// single query keyed on the sandbox's policy column would make a vault-only
	// session look drained and complete a withdrawal with the note still there.
	var pendingSandboxArtifacts int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sandbox_artifacts
		WHERE session_id = ? AND policy = ?
	`, sessionID, SandboxArtifactPolicyDeleteOnSessionDelete).Scan(&pendingSandboxArtifacts); err != nil {
		return SessionDeletionReceipt{}, err
	}
	// Every vault row counts, whatever state it is in. A `writing` reservation
	// names a path that may hold bytes a crash left behind, and a
	// `delete_failed` row names a file that is definitely still in the user's
	// vault; both are outstanding work, and neither drains until its file does.
	var pendingVaultArtifacts int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM vault_artifacts
		WHERE session_id = ?
	`, sessionID).Scan(&pendingVaultArtifacts); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if pendingSandboxArtifacts+pendingVaultArtifacts > 0 {
		receipt.State = "failed_external"
		receipt.Retryable = true
		receipt.ErrorCode = SessionDeletionArtifactCleanupPending
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
	// How many files the sandbox does not own is answered once, while the rows
	// describing them are still there, and never unanswered afterwards.
	//
	// A withdrawal now advances across more than one transaction: a completion
	// that could not finish leaves the receipt retryable with the session — and
	// therefore its whole sandbox manifest — already cascaded away. The retry
	// re-enters here and observes zero, because there is nothing left to count,
	// not because nothing was retained. Writing that zero over the earlier
	// answer turns "two files of yours are still on disk" into "nothing was
	// left behind", which is the one thing this number exists to say.
	//
	// So the observation only ever raises the count. `receipt` already carries
	// the persisted value read at the top of this function, and both it and the
	// stored column take the larger of the two.
	var observedLegacyArtifacts int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT physical_path)
		FROM sandbox_artifacts
		WHERE session_id = ? AND policy = ?
	`, sessionID, SandboxArtifactPolicyRetainLegacyUnowned).Scan(&observedLegacyArtifacts); err != nil {
		return SessionDeletionReceipt{}, err
	}
	if observedLegacyArtifacts > receipt.RetainedLegacyArtifactCount {
		receipt.RetainedLegacyArtifactCount = observedLegacyArtifacts
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_deletions
		SET retained_legacy_artifact_count = MAX(retained_legacy_artifact_count, ?)
		WHERE session_id = ?
	`, observedLegacyArtifacts, sessionID); err != nil {
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
	withdrawn, err := withdrawMemoryNotesLosingLastEvidenceTx(ctx, tx, sessionID)
	if err != nil {
		return SessionDeletionReceipt{}, err
	}
	if r.memoryDeletionWithdrawalBarrier != nil {
		if err := r.memoryDeletionWithdrawalBarrier(withdrawn); err != nil {
			return SessionDeletionReceipt{}, err
		}
	}
	// The cascade commits on its own, before the completion runs and before the
	// receipt is marked completed. Removing the rows is the half of the promise
	// the user asked for, and it is never held hostage to a file the vault
	// would not let go of; the receipt is what stays honest about the rest.
	//
	// A retry after a completion failure re-enters here with the session
	// already gone, which is why the audit row is written only when this
	// statement actually removed something. Recording it unconditionally would
	// stack one "session.deleted" row per retry — a count of attempts dressed
	// up as a count of deletions.
	deleted, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND deletion_state = 'deleting'`, sessionID)
	if err != nil {
		return SessionDeletionReceipt{}, err
	}
	removed, err := deleted.RowsAffected()
	if err != nil {
		return SessionDeletionReceipt{}, err
	}
	if removed > 0 {
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
	}
	if err := tx.Commit(); err != nil {
		return SessionDeletionReceipt{}, err
	}

	if completion != nil {
		if completionErr := completion(ctx); completionErr != nil {
			if err := r.markSessionDeletionCompletionFailure(ctx, sessionID, SessionDeletionMemoryReconcileFailed); err != nil {
				return SessionDeletionReceipt{}, err
			}
			receipt.State = "failed_external"
			receipt.Retryable = true
			receipt.ErrorCode = SessionDeletionMemoryReconcileFailed
			return receipt, nil
		}
	}

	completedAt := now()
	if _, err := r.db.ExecContext(ctx, `
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
	return receipt, nil
}

// markSessionDeletionCompletionFailure keeps a withdrawal whose rows are gone
// visibly unfinished. It touches neither artifact manifest: both drained before
// the cascade ran, and there is nothing left in either to mark.
func (r *Repository) markSessionDeletionCompletionFailure(ctx context.Context, sessionID string, errorCode string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE session_deletions
		SET state = 'failed_external', retryable = 1, error_code = ?
		WHERE session_id = ? AND state <> 'completed'
	`, errorCode, sessionID)
	return err
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

// MarkSessionDeletionReceiptFailure records an external failure that has no
// manifest to mark.
//
// Two failures look like this, and both would be misreported by either of the
// manifest markers. A cleanup that removed the files and could not drop the
// rows naming them has nothing to mark delete_failed — those rows describe
// files that are gone, and marking them files a per-file audit row claiming
// Turing could not remove a file it just removed. A cleaner failing under a
// scope with no manifest here has nothing of its own to mark at all, and
// marking one of the manifests we do know would attribute a stranger's failure
// to a store that did not fail.
//
// So this touches the receipt and only the receipt: the withdrawal stays
// visibly unfinished and retryable, and every manifest row is left exactly as
// the pass left it, which is what the retry needs to read.
func (r *Repository) MarkSessionDeletionReceiptFailure(ctx context.Context, sessionID string, errorCode string) error {
	if errorCode == "" {
		return errors.New("session deletion external failure code is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := markSessionDeletionFailedTx(ctx, tx, sessionID, errorCode); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkSessionDeletionSandboxFailure preserves only an opaque error class when a
// bounded sandbox cleanup attempt fails. The detail stays out of durable state
// because it can contain filesystem or transport information.
//
// It marks the sandbox manifest and only the sandbox manifest. A vault row
// belongs to a different cleaner over a different root, and marking it here
// would file an audit row saying Turing could not delete a note it never tried
// to delete — then send the next retry looking for it.
func (r *Repository) MarkSessionDeletionSandboxFailure(ctx context.Context, sessionID string, errorCode string) error {
	if errorCode == "" {
		return errors.New("session deletion external failure code is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	marked, err := markSessionDeletionFailedTx(ctx, tx, sessionID, errorCode)
	if err != nil {
		return err
	}
	if !marked {
		return tx.Commit()
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

// MarkSessionDeletionVaultFailure is the vault's half of the same promise: a
// cleanup that could not remove the notes a session left in the user's vault
// keeps the withdrawal retryable and records one redacted audit row per file
// still sitting there.
//
// It is the mirror image of the sandbox call and shares nothing with it but
// the receipt. It names vault_artifacts and only vault_artifacts, and it skips
// rows a partial pass already marked — those already carry their audit row, and
// filing a second one would inflate a single failure into two.
func (r *Repository) MarkSessionDeletionVaultFailure(ctx context.Context, sessionID string, errorCode string) error {
	if errorCode == "" {
		return errors.New("session deletion external failure code is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	marked, err := markSessionDeletionFailedTx(ctx, tx, sessionID, errorCode)
	if err != nil {
		return err
	}
	if !marked {
		return tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM vault_artifacts
		WHERE session_id = ? AND state <> ?
		ORDER BY id
	`, sessionID, VaultArtifactStateDeleteFailed)
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
		UPDATE vault_artifacts
		SET state = ?
		WHERE session_id = ? AND state <> ?
	`, VaultArtifactStateDeleteFailed, sessionID, VaultArtifactStateDeleteFailed); err != nil {
		return err
	}
	payloadBytes, err := json.Marshal(map[string]string{
		"state":     VaultArtifactStateDeleteFailed,
		"errorCode": errorCode,
	})
	if err != nil {
		return err
	}
	for _, artifactID := range artifactIDs {
		if err := recordAuditTx(ctx, tx, "", "system", "", vaultArtifactCleanupFailedAction, artifactID, string(payloadBytes)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// markSessionDeletionFailedTx moves a live receipt to a retryable external
// failure and reports whether the caller should go on to mark its own manifest.
// A receipt that is already a completed tombstone is left alone: it is not
// reopened, and no manifest is marked on the strength of it.
func markSessionDeletionFailedTx(ctx context.Context, tx *sql.Tx, sessionID string, errorCode string) (bool, error) {
	result, err := tx.ExecContext(ctx, `
		UPDATE session_deletions
		SET state = 'failed_external', retryable = 1, error_code = ?
		WHERE session_id = ? AND state <> 'completed'
	`, errorCode, sessionID)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed > 0 {
		return true, nil
	}
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM session_deletions WHERE session_id = ?`, sessionID).Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrSessionNotFound
		}
		return false, err
	}
	if state == "completed" {
		return false, nil
	}
	return false, ErrSessionNotFound
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
	// 'recovering' counts as active for the same reason the recovery scan sees
	// it: nobody has proven the worker is gone, so a worker may still be
	// holding these rows.
	//
	// Status alone is not enough — two paths leave a run terminal-by-status with
	// execution still live:
	//   - cancelRunTx leaves execution_active untouched. This is the
	//     user-reachable one.
	//   - failRunTx with PreserveExecution keeps execution_active = 1 and marks
	//     execution_state = 'uncertain'.
	// The recovery machinery treats both as in flight (assignments.go queries
	// execution_active = 1 alongside terminal statuses). Deleting then cascades
	// rows out from under a worker that has not acknowledged exit, and the
	// resulting unmapped sql.ErrNoRows tears down the whole ConnectWorker
	// stream — taking every other run that worker holds with it.
	var active int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agent_runs
		WHERE session_id = ?
			AND (status IN ('queued','running','waiting_approval','recovering') OR execution_active = 1)
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
	if _, err := withdrawMemoryNotesLosingLastEvidenceTx(ctx, tx, sessionID); err != nil {
		return err
	}
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
