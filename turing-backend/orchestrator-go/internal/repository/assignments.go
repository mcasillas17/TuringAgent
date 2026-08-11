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

func (r *Repository) RecoverAssignment(ctx context.Context, assignment Assignment) (AssignmentReconciliation, error) {
	return r.reconcileAssignment(ctx, assignment, true)
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
		if !staleRecovery && active == 1 {
			if err := fenceExecutionTx(ctx, tx, assignment.RunID); err != nil {
				return AssignmentReconciliation{}, err
			}
			if err := tx.Commit(); err != nil {
				return AssignmentReconciliation{}, err
			}
			return AssignmentReconciliation{Fenced: true}, nil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_runs
			SET execution_active = 0,
				execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
				execution_state = 'exited',
				execution_lease_expires_at = NULL,
				execution_lease_expires_at_ns = NULL
			WHERE id = ?
		`, now(), assignment.RunID); err != nil {
			return AssignmentReconciliation{}, err
		}
		if err := tx.Commit(); err != nil {
			return AssignmentReconciliation{}, err
		}
		return AssignmentReconciliation{Cleared: true}, nil
	case "waiting_approval":
		reconciliation, err := reconcileWaitingApprovalTx(ctx, tx, assignment.RunID, sessionID, traceID, !staleRecovery && active == 1)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		if err := tx.Commit(); err != nil {
			return AssignmentReconciliation{}, err
		}
		return reconciliation, nil
	case "running":
		if !staleRecovery && executionState != "pending_send" {
			if err := fenceExecutionTx(ctx, tx, assignment.RunID); err != nil {
				return AssignmentReconciliation{}, err
			}
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
		if active == 1 {
			if _, err := tx.ExecContext(ctx, `
				UPDATE agent_runs
				SET execution_active = 0,
					execution_exit_acknowledged_at = NULL,
					execution_attempt_id = NULL,
					execution_state = 'none',
					execution_lease_expires_at = NULL,
					execution_lease_expires_at_ns = NULL
				WHERE id = ? AND status = 'queued' AND execution_active = 1
			`, assignment.RunID); err != nil {
				return AssignmentReconciliation{}, err
			}
			if err := tx.Commit(); err != nil {
				return AssignmentReconciliation{}, err
			}
			return AssignmentReconciliation{Cleared: true}, nil
		}
		if err := tx.Commit(); err != nil {
			return AssignmentReconciliation{}, err
		}
		return AssignmentReconciliation{}, nil
	default:
		return AssignmentReconciliation{}, ErrAssignmentFenced
	}
}

func fenceExecutionTx(ctx context.Context, tx *sql.Tx, runID string) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_state = 'uncertain'
		WHERE id = ? AND execution_active = 1
	`, runID)
	if err != nil {
		return err
	}
	return expectOneRowErr(result, ErrAssignmentFenced)
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
			lease_expires_at_ns = NULL,
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
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL
		WHERE id = ?
			AND status = 'running'
			AND (? = '' OR COALESCE(execution_attempt_id, '') = ?)
	`, runID, attemptID, attemptID)
	if err != nil {
		return err
	}
	return expectOneRowErr(result, ErrAssignmentFenced)
}

func reconcileWaitingApprovalTx(ctx context.Context, tx *sql.Tx, runID, sessionID, traceID string, preserveExecution bool) (AssignmentReconciliation, error) {
	const code = "approval_delivery_failed"
	const message = "Worker disconnected while waiting for approval"
	finishedAt := now()
	var approvalID string
	approvalQuery := `SELECT id FROM approvals WHERE run_id = ? AND status = 'pending' ORDER BY ` + sqliteTimestampNanos("created_at") + ` DESC, id DESC LIMIT 1`
	err := tx.QueryRowContext(ctx, approvalQuery, runID).Scan(&approvalID)
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
	runUpdate := `
		UPDATE agent_runs
		SET status = 'failed',
			error_code = ?,
			error_message = ?,
			finished_at = ?`
	args := []any{code, message, finishedAt}
	if preserveExecution {
		runUpdate += `,
			execution_state = 'uncertain'`
	} else {
		runUpdate += `,
			execution_active = 0,
			execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
			execution_state = 'exited',
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL`
		args = append(args, finishedAt)
	}
	runUpdate += `
		WHERE id = ? AND status = 'waiting_approval'`
	args = append(args, runID)
	result, err := tx.ExecContext(ctx, runUpdate, args...)
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
			lease_expires_at = NULL,
			lease_expires_at_ns = NULL
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
	return AssignmentReconciliation{Cleared: !preserveExecution, Fenced: preserveExecution, RunFailedEvent: event}, nil
}

func (r *Repository) RecoverStaleAssignments(ctx context.Context, cutoff time.Time) ([]Event, error) {
	assignments, err := r.RecoverableAssignments(ctx, cutoff)
	if err != nil {
		return nil, err
	}
	return r.recoverAssignments(ctx, assignments)
}

func (r *Repository) RecoverAllActiveAssignments(ctx context.Context) ([]Event, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_state = 'fenced'
		WHERE status IN ('running', 'waiting_approval')
	`); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	assignments, err := r.startupRecoveryAssignments(ctx)
	if err != nil {
		return nil, err
	}
	return r.recoverAssignments(ctx, assignments)
}

func (r *Repository) RecoverableAssignments(ctx context.Context, cutoff time.Time) ([]Assignment, error) {
	query := `
		SELECT r.id, COALESCE(j.id, ''), COALESCE(r.worker_id, ''), COALESCE(r.execution_attempt_id, '')
		FROM agent_runs r
		LEFT JOIN jobs j ON j.run_id = r.id AND j.status = 'in_progress'
		WHERE r.execution_active = 1
			AND (
				r.status = 'queued'
				OR (
					r.status IN ('completed', 'failed', 'cancelled')
					AND (
						r.execution_state != 'uncertain'
						OR r.execution_lease_expires_at IS NULL
						OR COALESCE(
							r.execution_lease_expires_at_ns,
							` + sqliteTimestampNanos("r.execution_lease_expires_at") + `
						) <= ?
					)
				)
				OR (
					r.status IN ('running', 'waiting_approval')
					AND (
						r.execution_state = 'fenced'
						OR r.execution_lease_expires_at IS NULL
						OR COALESCE(
							r.execution_lease_expires_at_ns,
							` + sqliteTimestampNanos("r.execution_lease_expires_at") + `
						) <= ?
					)
				)
			)
		ORDER BY ` + sqliteTimestampNanos("r.created_at") + `, r.id
	`
	rows, err := r.db.QueryContext(ctx, query, cutoff.UTC().UnixNano(), cutoff.UTC().UnixNano())
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
	return assignments, nil
}

func (r *Repository) startupRecoveryAssignments(ctx context.Context) ([]Assignment, error) {
	query := `
		SELECT r.id, COALESCE(j.id, ''), COALESCE(r.worker_id, ''), COALESCE(r.execution_attempt_id, '')
		FROM agent_runs r
		LEFT JOIN jobs j ON j.run_id = r.id AND j.status = 'in_progress'
		WHERE r.execution_active = 1 OR r.status IN ('running', 'waiting_approval')
		ORDER BY ` + sqliteTimestampNanos("r.created_at") + `, r.id
	`
	rows, err := r.db.QueryContext(ctx, query)
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
	return assignments, nil
}

func (r *Repository) recoverAssignments(ctx context.Context, assignments []Assignment) ([]Event, error) {
	var events []Event
	for _, assignment := range assignments {
		reconciliation, err := r.RecoverAssignment(ctx, assignment)
		if err != nil {
			return events, err
		}
		if reconciliation.RunFailedEvent.EventID != "" {
			events = append(events, reconciliation.RunFailedEvent)
		}
	}
	return events, nil
}
