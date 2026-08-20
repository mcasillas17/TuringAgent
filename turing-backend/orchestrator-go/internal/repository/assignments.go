package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
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
		(runStatus == lifecycleRunning || runStatus == lifecycleWaitingApproval || runStatus == lifecycleRecovering) &&
		executionState != "fenced" &&
		leaseExpiresAtNanos.Valid &&
		leaseExpiresAtNanos.Int64 > cutoff.UnixNano() {
		return AssignmentReconciliation{Fenced: true}, tx.Commit()
	}

	switch runStatus {
	case lifecycleCompleted, lifecycleFailed, lifecycleCancelled:
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
	case lifecycleWaitingApproval:
		reconciliation, err := reconcileWaitingApprovalTx(ctx, tx, assignment.RunID, sessionID, traceID, !staleRecovery && active == 1)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		if err := tx.Commit(); err != nil {
			return AssignmentReconciliation{}, err
		}
		return reconciliation, nil
	case lifecycleRunning, lifecycleRecovering:
		// Recovering shares this branch because it IS this branch's state: a
		// run whose worker ownership is already uncertain still has to be
		// terminalized or requeued. What it must not do is get fenced a second
		// time as though its ownership had just been lost, which is why the
		// fence below is a guarded transition rather than a blind write — a
		// repeat of the same fence is recognized and costs nothing.
		//
		// A run fenced out of waiting-approval keeps its pending approval, and
		// its lifecycle no longer says so, so the approval itself is what
		// decides which reconciliation this is.
		if runStatus == lifecycleRecovering {
			waiting, err := hasPendingApprovalTx(ctx, tx, assignment.RunID)
			if err != nil {
				return AssignmentReconciliation{}, err
			}
			if waiting {
				reconciliation, err := reconcileWaitingApprovalTx(ctx, tx, assignment.RunID, sessionID, traceID, !staleRecovery && active == 1)
				if err != nil {
					return AssignmentReconciliation{}, err
				}
				if err := tx.Commit(); err != nil {
					return AssignmentReconciliation{}, err
				}
				return reconciliation, nil
			}
		}
		if !staleRecovery && executionState != "pending_send" {
			if active != 1 {
				return AssignmentReconciliation{}, ErrAssignmentFenced
			}
			fenced, err := applyRunTransitionTx(ctx, tx, fenceOwnershipTransition(
				assignment.RunID, unresolvedStateVersion, assignmentIdentity(assignment), "uncertain"), nil)
			if err != nil {
				return AssignmentReconciliation{}, err
			}
			if err := tx.Commit(); err != nil {
				return AssignmentReconciliation{}, err
			}
			return AssignmentReconciliation{Fenced: true, Events: fenced.Events}, nil
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
		if executionState == "pending_send" {
			requeueEvents, err := requeueAssignmentTx(ctx, tx, assignment.RunID, assignment.JobID, executionAttemptID, false)
			if err != nil {
				return AssignmentReconciliation{}, err
			}
			if err := tx.Commit(); err != nil {
				return AssignmentReconciliation{}, err
			}
			return AssignmentReconciliation{Requeued: true, Events: requeueEvents}, nil
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
		requeueEvents, err := requeueAssignmentTx(ctx, tx, assignment.RunID, assignment.JobID, executionAttemptID, true)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		// attempt is the one that just lost its worker, and requeueAssignmentTx has
		// just incremented the counter, so attempt+1 is the attempt the user is
		// about to wait through — same arithmetic as RequeueOrFailRetryableRun.
		// The attempt < maxAttempts guard above makes "attempt 4 of 3" unreachable.
		retry, err := newRetryNotice(runoutcome.NoticeRecoveryRetry, attempt+1, maxAttempts)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		notice, err := appendStepNoticeTx(ctx, tx, sessionID, assignment.RunID, traceID, retry, now())
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		if err := tx.Commit(); err != nil {
			return AssignmentReconciliation{}, err
		}
		return AssignmentReconciliation{Requeued: true, Events: append(requeueEvents, notice)}, nil
	case lifecycleQueued:
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

	// Recovery gave up. The outcome says the recovery was interrupted, not what
	// the worker's last words were, because by definition there were none.
	failure := runoutcome.NormalizeFailure(runoutcome.OriginRecovery, "job_timeout", runoutcome.RetryClassNever)
	// Emitted first, ahead of both the approval cleanup and the terminal event,
	// so the explanation precedes every consequence of it — and so this path
	// orders identically to RequeueOrFailRetryableRun, which inserts its notice
	// before the terminal transition. The client renders a terminal failure card
	// for agent.run.failed, but that card explains the failure, not that
	// retries were attempted and exhausted; this notice carries the count.
	exhausted, err := newRetryNotice(runoutcome.NoticeRecoveryExhausted, attempt, maxAttempts)
	if err != nil {
		return AssignmentReconciliation{}, false, attempt, err
	}
	giveUp, err := appendStepNoticeTx(ctx, tx, sessionID, runID, traceID, exhausted, now())
	if err != nil {
		return AssignmentReconciliation{}, false, attempt, err
	}
	terminal, err := failRunTx(ctx, tx, FailRunInput{
		RunID:              runID,
		Failure:            failure,
		resolveVersionInTx: true,
	})
	if err != nil {
		if errors.Is(err, ErrRunNotFailable) || errors.Is(err, ErrRunTransitionConflict) {
			return AssignmentReconciliation{}, false, attempt, ErrAssignmentFenced
		}
		return AssignmentReconciliation{}, false, attempt, err
	}
	events := append([]Event{giveUp}, terminal.Events...)
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

	// The approved authorization may already have run. Nothing here can prove
	// it did or did not, and that uncertainty IS the outcome — it is not
	// downgraded to a generic tool failure.
	failure := runoutcome.NormalizeFailure(runoutcome.OriginRecovery, "side_effect_uncertain", runoutcome.RetryClassNever)
	category := failure.Reason()
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
		SET status = 'failed', error_code = ?, error_message = NULL, completed_at = COALESCE(completed_at, ?)
		WHERE run_id = ? AND status IN ('requested', 'allowed', 'approval_required')
	`, failure.Code(), finishedAt, runID); err != nil {
		return AssignmentReconciliation{}, false, err
	}
	events := make([]Event, 0, len(approvals)+len(toolCalls)+1)
	for _, approval := range approvals {
		payload := map[string]any{
			"approvalId": approval.id,
			"toolCallId": approval.toolCallID,
			"toolName":   approval.toolName,
			"category":   string(category),
		}
		if approval.modelToolCallID != "" {
			payload["modelToolCallId"] = approval.modelToolCallID
		}
		payloadJSON, err := marshalEventPayload(payload)
		if err != nil {
			return AssignmentReconciliation{}, false, err
		}
		event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "approval.expired", payloadJSON, finishedAt)
		if err != nil {
			return AssignmentReconciliation{}, false, err
		}
		events = append(events, event)
	}
	for _, toolCall := range toolCalls {
		payloadJSON, err := marshalToolLifecyclePayload(toolCall.id, toolCall.serverName, toolCall.toolName, category)
		if err != nil {
			return AssignmentReconciliation{}, false, err
		}
		event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "tool.call.failed", payloadJSON, finishedAt)
		if err != nil {
			return AssignmentReconciliation{}, false, err
		}
		events = append(events, event)
	}
	terminal, err := failRunTx(ctx, tx, FailRunInput{RunID: runID, Failure: failure, resolveVersionInTx: true})
	if err != nil {
		if errors.Is(err, ErrRunNotFailable) || errors.Is(err, ErrRunTransitionConflict) {
			return AssignmentReconciliation{}, false, ErrAssignmentFenced
		}
		return AssignmentReconciliation{}, false, err
	}
	events = append(events, terminal.Events...)
	return AssignmentReconciliation{Cleared: true, Events: events}, true, nil
}

// fenceExecutionTx marks execution containment uncertain WITHOUT changing the
// public lifecycle. It is used only on runs that already reached a terminal
// lifecycle: those rows are immutable, but execution containment is an internal
// detail, not a public phase, and reconciliation still has to record that the
// worker never acknowledged its exit.
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

// assignmentIdentity projects an assignment onto the trigger identity a guarded
// transition fences on.
func assignmentIdentity(assignment Assignment) runTransitionIdentity {
	return runTransitionIdentity{
		workerID:            assignment.WorkerID,
		assignmentAttemptID: assignment.AttemptID,
	}
}

// hasPendingApprovalTx reports whether a run still holds a pending approval.
func hasPendingApprovalTx(ctx context.Context, tx *sql.Tx, runID string) (bool, error) {
	var pending int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM approvals WHERE run_id = ? AND status = 'pending'`, runID).Scan(&pending); err != nil {
		return false, err
	}
	return pending > 0, nil
}

func requeueAssignmentTx(ctx context.Context, tx *sql.Tx, runID, jobID, attemptID string, incrementAttempt bool) ([]Event, error) {
	if jobID == "" {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE run_id = ? AND status = 'in_progress'`, runID).Scan(&jobID); err != nil {
			return nil, err
		}
	}
	events, err := requeueRunThroughRecoveryTx(ctx, tx, runID, jobID,
		runTransitionIdentity{assignmentAttemptID: attemptID}, incrementAttempt)
	if err != nil {
		if errors.Is(err, ErrRunTransitionConflict) {
			return nil, ErrAssignmentFenced
		}
		return nil, err
	}
	return events, nil
}

func reconcileWaitingApprovalTx(ctx context.Context, tx *sql.Tx, runID, sessionID, traceID string, preserveExecution bool) (AssignmentReconciliation, error) {
	// Losing the worker while a decision is pending means the decision can
	// never be delivered. The outcome names that, and nothing about it depends
	// on what the worker last said.
	failure := runoutcome.NormalizeFailure(runoutcome.OriginApprovalTransport, "approval_delivery_failed", runoutcome.RetryClassNever)
	category := failure.Reason()
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
		SET status = 'failed', error_code = ?, error_message = NULL, completed_at = COALESCE(completed_at, ?)
		WHERE run_id = ? AND status IN ('requested', 'allowed', 'approval_required')
	`, failure.Code(), finishedAt, runID); err != nil {
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
		payloadJSON, err := marshalToolLifecyclePayload(toolCall.id, toolCall.serverName, toolCall.toolName, category)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "tool.call.failed", payloadJSON, finishedAt)
		if err != nil {
			return AssignmentReconciliation{}, err
		}
		events = append(events, event)
	}
	// waiting_approval and recovering are both legal sources: the run may have
	// been fenced into recovering before this reconciliation reached it, and
	// its pending approval still has to be closed either way.
	terminal, err := failRunTx(ctx, tx, FailRunInput{
		RunID:              runID,
		Failure:            failure,
		PreserveExecution:  preserveExecution,
		allowedFrom:        []string{lifecycleWaitingApproval, lifecycleRecovering},
		resolveVersionInTx: true,
	})
	if err != nil {
		if errors.Is(err, ErrRunNotFailable) || errors.Is(err, ErrRunTransitionConflict) {
			return AssignmentReconciliation{}, ErrAssignmentFenced
		}
		return AssignmentReconciliation{}, err
	}
	events = append(events, terminal.Events...)
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
	// Recovering is included because a restart must re-fence a run that was
	// already uncertain: skipping it would leave the one lifecycle that exists
	// to mean "nobody owns this" outside the sweep that rescues it.
	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_state = 'fenced'
		WHERE status IN ('running', 'waiting_approval', 'recovering')
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
					r.status IN ('running', 'waiting_approval', 'recovering')
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
		WHERE r.execution_active = 1 OR r.status IN ('running', 'waiting_approval', 'recovering')
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
