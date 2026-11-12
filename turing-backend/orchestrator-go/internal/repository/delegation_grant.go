package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// MaxRevokedDelegationBatch is the most delegated tasks one revocation
// transaction withdraws, so revoking a profile with a long backlog never
// holds the single database connection for long.
const MaxRevokedDelegationBatch = 100

// delegationGrantHeldSQL is true while the delegation aliased alias may still
// run: its specialist's setting row is enabled and granted at the revision the
// task was created under. It is the claim fence and the revocation batch's
// predicate, stated once so the two cannot drift: a crash between batches
// resumes on the next tick with no marker, because the fence is the cursor.
//
// A re-grant writes only granted_revision, so a re-grant at another revision
// never requalifies a task; only a re-enable at its own revision does.
func delegationGrantHeldSQL(alias string) string {
	return `EXISTS (
				SELECT 1 FROM agent_profile_settings setting
				WHERE setting.profile_id = ` + alias + `.profile_id
					AND setting.enabled = 1
					AND setting.granted_revision = ` + alias + `.profile_revision
			)`
}

// revokedDelegationsQuery selects, oldest first, the queued tasks whose
// specialist no longer holds their grant, consent-held ones included. It
// starts from the queued runs (idx_runs_status), so its cost follows the
// queue, not every delegation ever made. A task a worker holds is never
// selected: a running task meets the revocation at no re-entry and finishes.
var revokedDelegationsQuery = `
	SELECT d.child_run_id
	FROM agent_runs run
	JOIN delegations d ON d.child_run_id = run.id
	WHERE run.status = 'queued' AND NOT ` + delegationGrantHeldSQL("d") + `
	ORDER BY run.created_at, run.id
	LIMIT ?`

// RevokedDelegationBatch is what one revocation transaction withdrew.
type RevokedDelegationBatch struct {
	// Cancelled are the withdrawn tasks' runs, oldest first.
	Cancelled []string
	// Events are every event the withdrawals appended, in any session.
	Events []Event
	// ContinuationRunIDs are the continuations the batch queued by finishing
	// a turn's last task. The caller should dispatch them.
	ContinuationRunIDs []string
}

// CancelRevokedDelegations withdraws, in one transaction, at most
// MaxRevokedDelegationBatch queued tasks whose specialist was disabled,
// re-granted at another revision, or lost its setting row. The setting is
// read again inside the transaction, so a task re-enabled at its own revision
// before the batch reaches it is left runnable. Each is cancelled as the
// orchestrator withdrawing it, through the same transition every other
// cancellation takes, so it reports and joins like one.
func (r *Repository) CancelRevokedDelegations(ctx context.Context) (RevokedDelegationBatch, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RevokedDelegationBatch{}, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, revokedDelegationsQuery, MaxRevokedDelegationBatch)
	if err != nil {
		return RevokedDelegationBatch{}, err
	}
	var runIDs []string
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			return RevokedDelegationBatch{}, errors.Join(err, rows.Close())
		}
		runIDs = append(runIDs, runID)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return RevokedDelegationBatch{}, err
	}
	var batch RevokedDelegationBatch
	for _, runID := range runIDs {
		result, err := cancelRunTx(ctx, tx, CancelRunInput{
			RunID: runID, Cancellation: runoutcome.ProfileGrantRevokedCancellation(), resolveVersionInTx: true,
		})
		if err != nil {
			return RevokedDelegationBatch{}, err
		}
		batch.Cancelled = append(batch.Cancelled, runID)
		batch.Events = append(batch.Events, result.Events...)
		if result.ContinuationRunID != "" {
			batch.ContinuationRunIDs = append(batch.ContinuationRunIDs, result.ContinuationRunID)
		}
	}
	if err := tx.Commit(); err != nil {
		return RevokedDelegationBatch{}, err
	}
	return batch, nil
}

// CancelUnsentRevokedDelegation withdraws a task the dispatcher has claimed
// but not sent, because its specialist's profile file no longer resolves as
// active at the revision the task was created under. The claim must still be
// exactly as the dispatcher holds it — this attempt, this worker, its send
// not begun — or the call is refused as fenced. Nothing ever ran, so the
// claim's execution is released in the same transaction as the cancellation.
func (r *Repository) CancelUnsentRevokedDelegation(ctx context.Context, assignment Assignment) (RunTransitionResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RunTransitionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var unsent bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM agent_runs run
			JOIN jobs job ON job.run_id = run.id
			JOIN delegations task ON task.child_run_id = run.id
			WHERE run.id = ? AND run.status = 'running' AND run.execution_active = 1
				AND run.execution_state = 'pending_send'
				AND run.execution_attempt_id = ? AND run.worker_id = ?
				AND job.id = ? AND job.status = 'in_progress' AND job.assignment_attempt_id = ?
		)`, assignment.RunID, assignment.AttemptID, assignment.WorkerID, assignment.JobID, assignment.AttemptID).Scan(&unsent)
	if err != nil {
		return RunTransitionResult{}, err
	}
	if !unsent {
		return RunTransitionResult{}, ErrAssignmentFenced
	}
	result, err := cancelRunTx(ctx, tx, CancelRunInput{
		RunID: assignment.RunID, Cancellation: runoutcome.ProfileGrantRevokedCancellation(), resolveVersionInTx: true,
	})
	if err != nil {
		return RunTransitionResult{}, err
	}
	if err := releaseUnsentExecutionTx(ctx, tx, assignment); err != nil {
		return RunTransitionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RunTransitionResult{}, err
	}
	return result, nil
}

func releaseUnsentExecutionTx(ctx context.Context, tx *sql.Tx, assignment Assignment) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_active = 0,
			execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
			execution_state = 'exited',
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL
		WHERE id = ? AND execution_attempt_id = ? AND execution_state = 'pending_send'`,
		now(), assignment.RunID, assignment.AttemptID)
	if err != nil {
		return err
	}
	return expectOneRowErr(result, ErrAssignmentFenced)
}
