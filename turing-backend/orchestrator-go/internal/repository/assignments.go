package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type AssignmentReconciliation struct {
	Requeued       bool
	Cleared        bool
	Fenced         bool
	RunFailedEvent Event
}

func (r *Repository) ReconcileAssignment(ctx context.Context, assignment Assignment) (AssignmentReconciliation, error) {
	return r.reconcileAssignment(ctx, assignment, false)
}

func (r *Repository) reconcileAssignment(ctx context.Context, assignment Assignment, staleRecovery bool) (AssignmentReconciliation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AssignmentReconciliation{}, err
	}
	defer tx.Rollback()

	var runStatus, executionState, executionAttemptID, sessionID, traceID string
	var workerID sql.NullString
	var active int
	err = tx.QueryRowContext(ctx, `
		SELECT status, execution_state, COALESCE(execution_attempt_id, ''), worker_id, execution_active, session_id, trace_id
		FROM agent_runs
		WHERE id = ?
	`, assignment.RunID).Scan(&runStatus, &executionState, &executionAttemptID, &workerID, &active, &sessionID, &traceID)
	if err != nil {
		return AssignmentReconciliation{}, err
	}
	if assignment.AttemptID != "" && assignment.AttemptID != executionAttemptID {
		return AssignmentReconciliation{Fenced: true}, tx.Commit()
	}
	if assignment.WorkerID != "" && (!workerID.Valid || workerID.String != assignment.WorkerID) {
		return AssignmentReconciliation{Fenced: true}, tx.Commit()
	}

	switch runStatus {
	case "completed", "failed", "cancelled":
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_runs
			SET execution_active = 0,
				execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
				execution_state = 'exited',
				execution_lease_expires_at = NULL
			WHERE id = ?
		`, now(), assignment.RunID); err != nil {
			return AssignmentReconciliation{}, err
		}
		if err := tx.Commit(); err != nil {
			return AssignmentReconciliation{}, err
		}
		return AssignmentReconciliation{Cleared: true}, nil
	case "waiting_approval":
		reconciliation, err := reconcileWaitingApprovalTx(ctx, tx, assignment.RunID, sessionID, traceID)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		if err := tx.Commit(); err != nil {
			return AssignmentReconciliation{}, err
		}
		return reconciliation, nil
	case "running":
		if !staleRecovery && (executionState == "sending" || executionState == "uncertain") {
			if err := tx.Commit(); err != nil {
				return AssignmentReconciliation{}, err
			}
			return AssignmentReconciliation{Fenced: true}, nil
		}
		if active == 0 && !staleRecovery {
			if err := tx.Commit(); err != nil {
				return AssignmentReconciliation{}, err
			}
			return AssignmentReconciliation{Fenced: true}, nil
		}
		if err := requeueAssignmentTx(ctx, tx, assignment.RunID, assignment.JobID, executionAttemptID); err != nil {
			return AssignmentReconciliation{}, err
		}
		if err := tx.Commit(); err != nil {
			return AssignmentReconciliation{}, err
		}
		return AssignmentReconciliation{Requeued: true}, nil
	case "queued":
		if err := tx.Commit(); err != nil {
			return AssignmentReconciliation{}, err
		}
		return AssignmentReconciliation{}, nil
	default:
		return AssignmentReconciliation{}, ErrAssignmentFenced
	}
}

func requeueAssignmentTx(ctx context.Context, tx *sql.Tx, runID, jobID, attemptID string) error {
	if jobID == "" {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE run_id = ? AND status = 'in_progress'`, runID).Scan(&jobID); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'pending',
			lease_owner = NULL,
			lease_expires_at = NULL,
			picked_up_at = NULL,
			assignment_attempt_id = NULL,
			attempt = attempt + 1
		WHERE id = ?
			AND run_id = ?
			AND status = 'in_progress'
			AND (? = '' OR COALESCE(assignment_attempt_id, '') = ?)
	`, jobID, runID, attemptID, attemptID)
	if err != nil {
		return err
	}
	if err := expectOneRowErr(result, ErrAssignmentFenced); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET status = 'queued',
			started_at = NULL,
			worker_id = NULL,
			execution_active = 0,
			execution_exit_acknowledged_at = NULL,
			execution_attempt_id = NULL,
			execution_state = 'none',
			execution_lease_expires_at = NULL
		WHERE id = ?
			AND status = 'running'
			AND (? = '' OR COALESCE(execution_attempt_id, '') = ?)
	`, runID, attemptID, attemptID)
	if err != nil {
		return err
	}
	return expectOneRowErr(result, ErrAssignmentFenced)
}

func reconcileWaitingApprovalTx(ctx context.Context, tx *sql.Tx, runID, sessionID, traceID string) (AssignmentReconciliation, error) {
	const code = "approval_delivery_failed"
	const message = "Worker disconnected while waiting for approval"
	finishedAt := now()
	var approvalID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM approvals WHERE run_id = ? AND status = 'pending' ORDER BY created_at DESC, id DESC LIMIT 1`, runID).Scan(&approvalID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AssignmentReconciliation{}, err
	}
	if err == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE approvals SET status = 'denied', decided_at = ? WHERE id = ? AND status = 'pending'`, finishedAt, approvalID); err != nil {
			return AssignmentReconciliation{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tool_calls
		SET status = 'failed', error_code = ?, error_message = ?, completed_at = COALESCE(completed_at, ?)
		WHERE run_id = ? AND status IN ('requested', 'allowed', 'approval_required')
	`, code, message, finishedAt, runID); err != nil {
		return AssignmentReconciliation{}, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET status = 'failed',
			error_code = ?,
			error_message = ?,
			finished_at = ?,
			execution_active = 0,
			execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
			execution_state = 'exited',
			execution_lease_expires_at = NULL
		WHERE id = ? AND status = 'waiting_approval'
	`, code, message, finishedAt, finishedAt, runID)
	if err != nil {
		return AssignmentReconciliation{}, err
	}
	if err := expectOneRowErr(result, ErrAssignmentFenced); err != nil {
		return AssignmentReconciliation{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'failed',
			finished_at = ?,
			error_code = ?,
			error_message = ?,
			lease_owner = NULL,
			lease_expires_at = NULL
		WHERE run_id = ? AND status IN ('pending', 'in_progress')
	`, finishedAt, code, message, runID); err != nil {
		return AssignmentReconciliation{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"runId": runID, "code": code, "message": message, "retryable": false,
	})
	if err != nil {
		return AssignmentReconciliation{}, err
	}
	event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "agent.run.failed", string(payload), finishedAt)
	if err != nil {
		return AssignmentReconciliation{}, err
	}
	return AssignmentReconciliation{Cleared: true, RunFailedEvent: event}, nil
}

func (r *Repository) RecoverStaleAssignments(ctx context.Context, cutoff time.Time) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT r.id, COALESCE(j.id, ''), COALESCE(r.worker_id, ''), COALESCE(r.execution_attempt_id, '')
		FROM agent_runs r
		LEFT JOIN jobs j ON j.run_id = r.id AND j.status = 'in_progress'
		WHERE r.status IN ('running', 'waiting_approval')
			AND (
				r.execution_state = 'fenced'
				OR r.execution_lease_expires_at IS NULL
				OR r.execution_lease_expires_at <= ?
			)
		ORDER BY r.created_at, r.id
	`, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assignments []Assignment
	for rows.Next() {
		var assignment Assignment
		if err := rows.Scan(&assignment.RunID, &assignment.JobID, &assignment.WorkerID, &assignment.AttemptID); err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var events []Event
	for _, assignment := range assignments {
		reconciliation, err := r.reconcileAssignment(ctx, assignment, true)
		if err != nil {
			return events, err
		}
		if reconciliation.RunFailedEvent.EventID != "" {
			events = append(events, reconciliation.RunFailedEvent)
		}
	}
	return events, nil
}
