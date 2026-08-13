package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type AssignmentReconciliation struct {
	Requeued bool
	Cleared  bool
	Fenced   bool
	Events   []Event
}

const defaultAssignmentMaxAttempts = 3

func (r *Repository) RenewAssignments(ctx context.Context, assignments []Assignment, leaseExpires time.Time) ([]Assignment, error) {
	if len(assignments) == 0 {
		return nil, nil
	}
	leaseExpires = leaseExpires.UTC()
	leaseExpiresAt := FormatTimestamp(leaseExpires)
	leaseExpiresAtNanos := leaseExpires.UnixNano()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	renewed := make([]Assignment, 0, len(assignments))
	for _, assignment := range assignments {
		if assignment.JobID == "" || assignment.RunID == "" || assignment.WorkerID == "" || assignment.AttemptID == "" {
			return nil, errors.New("complete assignment identity is required for lease renewal")
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE agent_runs
			SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
			WHERE id = ?
				AND status IN ('running', 'waiting_approval')
				AND execution_active = 1
				AND worker_id = ?
				AND execution_attempt_id = ?
				AND EXISTS (
					SELECT 1 FROM jobs
					WHERE id = ? AND run_id = agent_runs.id
						AND status = 'in_progress'
						AND lease_owner = ?
						AND assignment_attempt_id = ?
				)
		`, leaseExpiresAt, leaseExpiresAtNanos, assignment.RunID, assignment.WorkerID,
			assignment.AttemptID, assignment.JobID, assignment.WorkerID, assignment.AttemptID)
		if err != nil {
			return nil, err
		}
		runRows, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if runRows == 0 {
			continue
		}
		result, err = tx.ExecContext(ctx, `
			UPDATE jobs
			SET lease_expires_at = ?, lease_expires_at_ns = ?
			WHERE id = ? AND run_id = ?
				AND status = 'in_progress'
				AND lease_owner = ?
				AND assignment_attempt_id = ?
		`, leaseExpiresAt, leaseExpiresAtNanos, assignment.JobID, assignment.RunID,
			assignment.WorkerID, assignment.AttemptID)
		if err != nil {
			return nil, err
		}
		jobRows, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if jobRows != 1 {
			return nil, fmt.Errorf("renew assignment %s: matching job disappeared", assignment.AttemptID)
		}
		renewed = append(renewed, assignment)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return renewed, nil
}

func (r *Repository) ReconcileAssignment(ctx context.Context, assignment Assignment) (AssignmentReconciliation, error) {
	return r.ReconcileAssignmentWithLimit(ctx, assignment, defaultAssignmentMaxAttempts)
}

func (r *Repository) ReconcileAssignmentWithLimit(ctx context.Context, assignment Assignment, maxAttempts int) (AssignmentReconciliation, error) {
	return r.reconcileAssignment(ctx, assignment, false, nil, maxAttempts)
}

func (r *Repository) RecoverAssignment(ctx context.Context, assignment Assignment) (AssignmentReconciliation, error) {
	return r.RecoverAssignmentWithLimit(ctx, assignment, defaultAssignmentMaxAttempts)
}

func (r *Repository) RecoverAssignmentWithLimit(ctx context.Context, assignment Assignment, maxAttempts int) (AssignmentReconciliation, error) {
	return r.reconcileAssignment(ctx, assignment, true, nil, maxAttempts)
}

func (r *Repository) RecoverAssignmentAtCutoff(ctx context.Context, assignment Assignment, cutoff time.Time) (AssignmentReconciliation, error) {
	return r.RecoverAssignmentAtCutoffWithLimit(ctx, assignment, cutoff, defaultAssignmentMaxAttempts)
}

func (r *Repository) RecoverAssignmentAtCutoffWithLimit(ctx context.Context, assignment Assignment, cutoff time.Time, maxAttempts int) (AssignmentReconciliation, error) {
	cutoff = cutoff.UTC()
	return r.reconcileAssignment(ctx, assignment, true, &cutoff, maxAttempts)
}

func (r *Repository) reconcileAssignment(ctx context.Context, assignment Assignment, staleRecovery bool, cutoff *time.Time, maxAttempts int) (AssignmentReconciliation, error) {
	if maxAttempts <= 0 {
		maxAttempts = defaultAssignmentMaxAttempts
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AssignmentReconciliation{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var runStatus, executionState, executionAttemptID, sessionID, traceID string
	var workerID sql.NullString
	var leaseExpiresAtNanos sql.NullInt64
	var active int
	err = tx.QueryRowContext(ctx, `
		SELECT status, execution_state, COALESCE(execution_attempt_id, ''), worker_id, execution_active, session_id, trace_id,
			COALESCE(execution_lease_expires_at_ns, `+sqliteTimestampNanos("execution_lease_expires_at")+`)
		FROM agent_runs
		WHERE id = ?
	`, assignment.RunID).Scan(
		&runStatus, &executionState, &executionAttemptID, &workerID, &active, &sessionID, &traceID, &leaseExpiresAtNanos,
	)
	if err != nil {
		return AssignmentReconciliation{}, err
	}
	if assignment.AttemptID != "" && assignment.AttemptID != executionAttemptID {
		return AssignmentReconciliation{Fenced: true}, tx.Commit()
	}
	if assignment.WorkerID != "" && (!workerID.Valid || workerID.String != assignment.WorkerID) {
		return AssignmentReconciliation{Fenced: true}, tx.Commit()
	}
	if cutoff != nil &&
		active == 1 &&
		(runStatus == "running" || runStatus == "waiting_approval") &&
		executionState != "fenced" &&
		leaseExpiresAtNanos.Valid &&
		leaseExpiresAtNanos.Int64 > cutoff.UnixNano() {
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
		if staleRecovery {
			reconciliation, terminalized, err := terminalizeStaleApprovedAuthorizationTx(ctx, tx, assignment.RunID, assignment.JobID, executionAttemptID, sessionID, traceID)
			if err != nil {
				return AssignmentReconciliation{}, err
			}
			if terminalized {
				if err := tx.Commit(); err != nil {
					return AssignmentReconciliation{}, err
				}
				return reconciliation, nil
			}
		}
		reconciliation, terminalized, attempt, err := terminalizeExhaustedAssignmentTx(
			ctx, tx, assignment.RunID, assignment.JobID, executionAttemptID, sessionID, traceID, maxAttempts,
		)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		if terminalized {
			if err := tx.Commit(); err != nil {
				return AssignmentReconciliation{}, err
			}
			return reconciliation, nil
		}
		if err := requeueAssignmentTx(ctx, tx, assignment.RunID, assignment.JobID, executionAttemptID); err != nil {
			return AssignmentReconciliation{}, err
		}
		// attempt is the one that just lost its worker, and requeueAssignmentTx has
		// just incremented the counter, so attempt+1 is the attempt the user is
		// about to wait through — same arithmetic as RequeueOrFailRetryableRun.
		// Two consecutive worker losses would otherwise render identical notices.
		notice, err := appendRunNoticeTx(ctx, tx, sessionID, assignment.RunID, traceID,
			fmt.Sprintf("Retrying (attempt %d of %d) after the worker became unavailable", attempt+1, maxAttempts),
			map[string]any{"attempt": attempt + 1, "maxAttempts": maxAttempts, "reason": "worker_unavailable"})
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		if err := tx.Commit(); err != nil {
			return AssignmentReconciliation{}, err
		}
		return AssignmentReconciliation{Requeued: true, Events: []Event{notice}}, nil
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

func terminalizeExhaustedAssignmentTx(
	ctx context.Context,
	tx *sql.Tx,
	runID string,
	jobID string,
	attemptID string,
	sessionID string,
	traceID string,
	maxAttempts int,
) (AssignmentReconciliation, bool, int, error) {
	var attempt int
	err := tx.QueryRowContext(ctx, `
		SELECT id, attempt
		FROM jobs
		WHERE run_id = ?
			AND status = 'in_progress'
			AND (? = '' OR id = ?)
			AND (? = '' OR COALESCE(assignment_attempt_id, '') = ?)
	`, runID, jobID, jobID, attemptID, attemptID).Scan(&jobID, &attempt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AssignmentReconciliation{}, false, attempt, ErrAssignmentFenced
		}
		return AssignmentReconciliation{}, false, attempt, err
	}
	if attempt < maxAttempts {
		return AssignmentReconciliation{}, false, attempt, nil
	}

	const code = "job_timeout"
	const message = "Job timed out"
	finishedAt := now()
	result, err := tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET status = 'failed',
			error_code = ?,
			error_message = ?,
			finished_at = ?,
			execution_active = 0,
			execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
			execution_state = 'exited',
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL
		WHERE id = ?
			AND status = 'running'
			AND (? = '' OR COALESCE(execution_attempt_id, '') = ?)
	`, code, message, finishedAt, finishedAt, runID, attemptID, attemptID)
	if err != nil {
		return AssignmentReconciliation{}, false, attempt, err
	}
	if err := expectOneRowErr(result, ErrAssignmentFenced); err != nil {
		return AssignmentReconciliation{}, false, attempt, err
	}
	events, err := failPendingApprovalLifecycleTx(ctx, tx, runID, code, message, finishedAt)
	if err != nil {
		return AssignmentReconciliation{}, false, attempt, err
	}
	// Ordered before the terminal agent.run.failed below. The requeue notice
	// tells the user we are retrying after losing the worker; without this, the
	// moment we stop retrying is silent, because the client has no
	// agent.run.failed case at all.
	giveUp, err := appendRunNoticeTx(ctx, tx, sessionID, runID, traceID,
		giveUpNote(attempt),
		map[string]any{"attempts": attempt, "maxAttempts": maxAttempts, "reason": code})
	if err != nil {
		return AssignmentReconciliation{}, false, attempt, err
	}
	events = append(events, giveUp)
	result, err = tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'failed',
			finished_at = ?,
			error_code = ?,
			error_message = ?,
			lease_owner = NULL,
			lease_expires_at = NULL,
			lease_expires_at_ns = NULL
		WHERE id = ?
			AND run_id = ?
			AND status = 'in_progress'
			AND (? = '' OR COALESCE(assignment_attempt_id, '') = ?)
	`, finishedAt, code, message, jobID, runID, attemptID, attemptID)
	if err != nil {
		return AssignmentReconciliation{}, false, attempt, err
	}
	if err := expectOneRowErr(result, ErrAssignmentFenced); err != nil {
		return AssignmentReconciliation{}, false, attempt, err
	}
	payloadJSON, err := json.Marshal(map[string]any{
		"runId": runID, "code": code, "message": message, "retryable": false,
	})
	if err != nil {
		return AssignmentReconciliation{}, false, attempt, err
	}
	event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "agent.run.failed", string(payloadJSON), finishedAt)
	if err != nil {
		return AssignmentReconciliation{}, false, attempt, err
	}
	events = append(events, event)
	return AssignmentReconciliation{Cleared: true, Events: events}, true, attempt, nil
}

func terminalizeStaleApprovedAuthorizationTx(
	ctx context.Context,
	tx *sql.Tx,
	runID string,
	jobID string,
	attemptID string,
	sessionID string,
	traceID string,
) (AssignmentReconciliation, bool, error) {
	type approvalAuthorization struct {
		id, toolCallID, toolName, modelToolCallID string
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT a.id, COALESCE(a.tool_call_id, ''), a.tool_name, COALESCE(tc.model_tool_call_id, '')
		FROM approvals a
		LEFT JOIN tool_calls tc ON tc.id = a.tool_call_id AND tc.run_id = a.run_id
		WHERE a.run_id = ? AND a.status IN ('approved', 'consumed')
		ORDER BY `+sqliteTimestampNanos("a.created_at")+`, a.id
	`, runID)
	if err != nil {
		return AssignmentReconciliation{}, false, err
	}
	var approvals []approvalAuthorization
	for rows.Next() {
		var approval approvalAuthorization
		if err := rows.Scan(&approval.id, &approval.toolCallID, &approval.toolName, &approval.modelToolCallID); err != nil {
			return AssignmentReconciliation{}, false, errors.Join(err, rows.Close())
		}
		approvals = append(approvals, approval)
	}
	if err := rows.Err(); err != nil {
		return AssignmentReconciliation{}, false, errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return AssignmentReconciliation{}, false, err
	}
	if len(approvals) == 0 {
		return AssignmentReconciliation{}, false, nil
	}

	type openToolCall struct {
		id, serverName, toolName string
	}
	toolRows, err := tx.QueryContext(ctx, `
		SELECT id, server_name, tool_name
		FROM tool_calls
		WHERE run_id = ? AND status IN ('requested', 'allowed', 'approval_required')
		ORDER BY `+sqliteTimestampNanos("created_at")+`, id
	`, runID)
	if err != nil {
		return AssignmentReconciliation{}, false, err
	}
	var toolCalls []openToolCall
	for toolRows.Next() {
		var toolCall openToolCall
		if err := toolRows.Scan(&toolCall.id, &toolCall.serverName, &toolCall.toolName); err != nil {
			return AssignmentReconciliation{}, false, errors.Join(err, toolRows.Close())
		}
		toolCalls = append(toolCalls, toolCall)
	}
	if err := toolRows.Err(); err != nil {
		return AssignmentReconciliation{}, false, errors.Join(err, toolRows.Close())
	}
	if err := toolRows.Close(); err != nil {
		return AssignmentReconciliation{}, false, err
	}

	const code = "side_effect_uncertain"
	const message = "Stale assignment may have executed an approved tool call"
	finishedAt := now()
	if _, err := tx.ExecContext(ctx, `
		UPDATE approvals
		SET status = 'expired', approval_jti = NULL, approval_token = NULL, decided_at = COALESCE(decided_at, ?)
		WHERE run_id = ? AND status IN ('approved', 'consumed')
	`, finishedAt, runID); err != nil {
		return AssignmentReconciliation{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tool_calls
		SET status = 'failed', error_code = ?, error_message = ?, completed_at = COALESCE(completed_at, ?)
		WHERE run_id = ? AND status IN ('requested', 'allowed', 'approval_required')
	`, code, message, finishedAt, runID); err != nil {
		return AssignmentReconciliation{}, false, err
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
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL
		WHERE id = ?
			AND status = 'running'
			AND (? = '' OR COALESCE(execution_attempt_id, '') = ?)
	`, code, message, finishedAt, finishedAt, runID, attemptID, attemptID)
	if err != nil {
		return AssignmentReconciliation{}, false, err
	}
	if err := expectOneRowErr(result, ErrAssignmentFenced); err != nil {
		return AssignmentReconciliation{}, false, err
	}
	if jobID == "" {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE run_id = ? AND status = 'in_progress'`, runID).Scan(&jobID); err != nil {
			return AssignmentReconciliation{}, false, err
		}
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'failed',
			finished_at = ?,
			error_code = ?,
			error_message = ?,
			lease_owner = NULL,
			lease_expires_at = NULL,
			lease_expires_at_ns = NULL
		WHERE id = ?
			AND run_id = ?
			AND status = 'in_progress'
			AND (? = '' OR COALESCE(assignment_attempt_id, '') = ?)
	`, finishedAt, code, message, jobID, runID, attemptID, attemptID)
	if err != nil {
		return AssignmentReconciliation{}, false, err
	}
	if err := expectOneRowErr(result, ErrAssignmentFenced); err != nil {
		return AssignmentReconciliation{}, false, err
	}
	events := make([]Event, 0, len(approvals)+len(toolCalls)+1)
	for _, approval := range approvals {
		payload := map[string]any{
			"approvalId": approval.id,
			"toolCallId": approval.toolCallID,
			"toolName":   approval.toolName,
			"reason":     code,
		}
		if approval.modelToolCallID != "" {
			payload["modelToolCallId"] = approval.modelToolCallID
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return AssignmentReconciliation{}, false, err
		}
		event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "approval.expired", string(payloadJSON), finishedAt)
		if err != nil {
			return AssignmentReconciliation{}, false, err
		}
		events = append(events, event)
	}
	for _, toolCall := range toolCalls {
		payloadJSON, err := marshalToolLifecyclePayload(toolCall.id, toolCall.serverName, toolCall.toolName, message)
		if err != nil {
			return AssignmentReconciliation{}, false, err
		}
		event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "tool.call.failed", payloadJSON, finishedAt)
		if err != nil {
			return AssignmentReconciliation{}, false, err
		}
		events = append(events, event)
	}
	payloadJSON, err := json.Marshal(map[string]any{
		"runId": runID, "code": code, "message": message, "retryable": false,
	})
	if err != nil {
		return AssignmentReconciliation{}, false, err
	}
	event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "agent.run.failed", string(payloadJSON), finishedAt)
	if err != nil {
		return AssignmentReconciliation{}, false, err
	}
	events = append(events, event)
	return AssignmentReconciliation{Cleared: true, Events: events}, true, nil
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
	var approval ApprovalRecord
	var approvalID string
	approvalQuery := `SELECT id FROM approvals WHERE run_id = ? AND status = 'pending' ORDER BY ` + sqliteTimestampNanos("created_at") + ` DESC, id DESC LIMIT 1`
	err := tx.QueryRowContext(ctx, approvalQuery, runID).Scan(&approvalID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AssignmentReconciliation{}, err
	}
	if err == nil {
		approval, err = approvalByID(ctx, tx, approvalID)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE approvals SET status = 'denied', decided_at = ? WHERE id = ? AND status = 'pending'`, finishedAt, approvalID); err != nil {
			return AssignmentReconciliation{}, err
		}
	}
	type openToolCall struct {
		id, serverName, toolName string
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, server_name, tool_name
		FROM tool_calls
		WHERE run_id = ? AND status IN ('requested', 'allowed', 'approval_required')
		ORDER BY `+sqliteTimestampNanos("created_at")+`, id
	`, runID)
	if err != nil {
		return AssignmentReconciliation{}, err
	}
	var toolCalls []openToolCall
	for rows.Next() {
		var toolCall openToolCall
		if err := rows.Scan(&toolCall.id, &toolCall.serverName, &toolCall.toolName); err != nil {
			return AssignmentReconciliation{}, errors.Join(err, rows.Close())
		}
		toolCalls = append(toolCalls, toolCall)
	}
	if err := rows.Err(); err != nil {
		return AssignmentReconciliation{}, errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return AssignmentReconciliation{}, err
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
	events := make([]Event, 0, len(toolCalls)+2)
	if approvalID != "" {
		approval.Status = "denied"
		event, err := appendApprovalLifecycleEventTx(ctx, tx, approval, "approval.denied", finishedAt)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		events = append(events, event)
	}
	for _, toolCall := range toolCalls {
		payloadJSON, err := marshalToolLifecyclePayload(toolCall.id, toolCall.serverName, toolCall.toolName, message)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "tool.call.failed", payloadJSON, finishedAt)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		events = append(events, event)
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
	events = append(events, event)
	return AssignmentReconciliation{Cleared: !preserveExecution, Fenced: preserveExecution, Events: events}, nil
}

func (r *Repository) RecoverStaleAssignments(ctx context.Context, cutoff time.Time) ([]Event, error) {
	return r.RecoverStaleAssignmentsWithLimit(ctx, cutoff, defaultAssignmentMaxAttempts)
}

func (r *Repository) RecoverStaleAssignmentsWithLimit(ctx context.Context, cutoff time.Time, maxAttempts int) ([]Event, error) {
	assignments, err := r.RecoverableAssignments(ctx, cutoff)
	if err != nil {
		return nil, err
	}
	return r.recoverAssignments(ctx, assignments, &cutoff, maxAttempts)
}

func (r *Repository) RecoverAllActiveAssignments(ctx context.Context) ([]Event, error) {
	return r.RecoverAllActiveAssignmentsWithLimit(ctx, defaultAssignmentMaxAttempts)
}

func (r *Repository) RecoverAllActiveAssignmentsWithLimit(ctx context.Context, maxAttempts int) ([]Event, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
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
	return r.recoverAssignments(ctx, assignments, nil, maxAttempts)
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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

func (r *Repository) recoverAssignments(ctx context.Context, assignments []Assignment, cutoff *time.Time, maxAttempts int) ([]Event, error) {
	var events []Event
	for _, assignment := range assignments {
		var reconciliation AssignmentReconciliation
		var err error
		if cutoff == nil {
			reconciliation, err = r.RecoverAssignmentWithLimit(ctx, assignment, maxAttempts)
		} else {
			reconciliation, err = r.RecoverAssignmentAtCutoffWithLimit(ctx, assignment, *cutoff, maxAttempts)
		}
		if err != nil {
			return events, err
		}
		events = append(events, reconciliation.Events...)
	}
	return events, nil
}
