package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

var (
	ErrCancelKeyInvalid  = errors.New("invalid cancellation idempotency key")
	ErrCancelKeyConflict = errors.New("cancellation idempotency key conflict")
)

type RunCancellation struct {
	State           RunState
	ExecutionActive bool
}

type CancelUserRunResult struct {
	RunCancellation
	Accepted bool
	Events   []Event
	// StoppedTasks are the delegated runs cancelling this one stopped; see
	// RunTransitionResult.StoppedTasks.
	StoppedTasks []string
}

// ReadRunCancellation checks withdrawal and exact session ownership in the same
// snapshot as the state and execution observation. No receipt bypasses this gate.
func (r *Repository) ReadRunCancellation(ctx context.Context, sessionID, runID string) (RunCancellation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RunCancellation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	row, active, err := visibleCancellationRunTx(ctx, tx, sessionID, runID)
	if err != nil {
		return RunCancellation{}, err
	}
	if err := tx.Commit(); err != nil {
		return RunCancellation{}, err
	}
	return RunCancellation{State: row.state(), ExecutionActive: active}, nil
}

func visibleCancellationRunTx(ctx context.Context, tx *sql.Tx, sessionID, runID string) (runRow, bool, error) {
	var active bool
	err := tx.QueryRowContext(ctx, `
		SELECT r.execution_active FROM agent_runs r JOIN sessions s ON s.id = r.session_id
		WHERE r.id = ? AND r.session_id = ? AND s.deletion_state = 'active'`,
		runID, sessionID).Scan(&active)
	if err != nil {
		return runRow{}, false, err
	}
	row, err := readRunRow(ctx, tx, runID)
	return row, active, err
}

// CancelUserRun atomically records an explicit public operation and its exact
// canonical result. SQLite serializes competing writers; no version sampled
// outside this transaction can accidentally cancel a replacement attempt.
func (r *Repository) CancelUserRun(ctx context.Context, sessionID, runID, key string) (CancelUserRunResult, error) {
	if strings.TrimSpace(key) == "" || len(key) > 128 || !utf8.ValidString(key) {
		return CancelUserRunResult{}, ErrCancelKeyInvalid
	}
	unlock, err := lockSessionDecision(ctx, sessionID)
	if err != nil {
		return CancelUserRunResult{}, err
	}
	defer unlock()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return CancelUserRunResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	row, active, err := visibleCancellationRunTx(ctx, tx, sessionID, runID)
	if err != nil {
		return CancelUserRunResult{}, err
	}
	result := CancelUserRunResult{RunCancellation: RunCancellation{State: row.state(), ExecutionActive: active}}
	var storedSession, storedRun, stateJSON string
	err = tx.QueryRowContext(ctx, `SELECT session_id, run_id, accepted, run_state_json
		FROM run_cancellation_receipts WHERE idempotency_key = ?`, key).
		Scan(&storedSession, &storedRun, &result.Accepted, &stateJSON)
	switch {
	case err == nil:
		if storedSession != sessionID || storedRun != runID {
			return CancelUserRunResult{}, ErrCancelKeyConflict
		}
		if err := json.Unmarshal([]byte(stateJSON), &result.State); err != nil {
			return CancelUserRunResult{}, err
		}
	case errors.Is(err, sql.ErrNoRows):
		if !isTerminalLifecycle(row.lifecycle) {
			transition, err := cancelRunTx(ctx, tx, CancelRunInput{
				RunID: runID, AssistantMessageID: row.assistantMessageID,
				resolveVersionInTx: true, Cancellation: runoutcome.UserCancellation(),
			})
			if err != nil {
				return CancelUserRunResult{}, err
			}
			result.Accepted, result.State, result.Events = true, transition.State, transition.Events
			result.StoppedTasks = transition.StoppedTasks
			if err := recordAuditTx(ctx, tx, runID, "client", "", "run.cancel", runID, ""); err != nil {
				return CancelUserRunResult{}, err
			}
		}
		encoded, err := json.Marshal(result.State)
		if err != nil {
			return CancelUserRunResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_cancellation_receipts
			(idempotency_key, session_id, run_id, accepted, run_state_json) VALUES (?, ?, ?, ?, ?)`,
			key, sessionID, runID, result.Accepted, string(encoded)); err != nil {
			return CancelUserRunResult{}, err
		}
	default:
		return CancelUserRunResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CancelUserRunResult{}, err
	}
	return result, nil
}

// ApprovalResumeLostToCancellation recognizes a previously authorized Ready
// that lost to a committed stop, without granting permission to resume, and
// returns the stop's code to redeliver: the user's cancellation, or a task
// stopped because its turn ended. It returns "" for anything else. The stop
// has erased the unconsumed token, so the canonical event history supplies
// the decision and pre-cancellation state; current rows still fence worker and
// attempt identity.
func (r *Repository) ApprovalResumeLostToCancellation(ctx context.Context, input ResumeApprovedRunInput) (string, error) {
	if input.RunID == "" || input.ApprovalID == "" || input.WorkerID == "" ||
		input.AssignmentAttemptID == "" || input.ExpectedStateVersion < 1 {
		return "", nil
	}
	var outcomeReason string
	err := r.db.QueryRowContext(ctx, `
		SELECT r.outcome_reason FROM agent_runs r JOIN approvals a ON a.run_id = r.id
			WHERE r.id = ? AND a.id = ? AND r.worker_id = ? AND r.execution_attempt_id = ?
				AND r.status = 'cancelled' AND r.execution_active = 1
				AND (r.outcome_reason = ? OR COALESCE(r.cancellation_reason, '') = ?)
				AND (
					a.status = 'consumed' OR (
						a.status = 'expired' AND EXISTS (
							SELECT 1 FROM events e WHERE e.run_id = r.id AND e.type = 'approval.expired'
								AND e.created_at = r.state_updated_at AND json_valid(e.payload_json)
								AND json_extract(e.payload_json, ?) = a.id
						)
					)
				)
				AND EXISTS (
					SELECT 1 FROM events e WHERE e.run_id = r.id AND e.type = 'approval.approved'
						AND json_valid(e.payload_json) AND json_extract(e.payload_json, ?) = a.id
				)
				AND EXISTS (
					SELECT 1 FROM events e WHERE e.run_id = r.id AND json_valid(e.payload_json)
						AND json_extract(e.payload_json, ?) = r.state_version - 1
						AND (
							(e.type IN ('approval.requested', 'approval.approved')
								AND json_extract(e.payload_json, '$.runState.lifecycle') = 'waiting_approval'
								AND r.state_version - 1 = ?)
							OR (e.type = 'agent.run.state_changed'
								AND json_extract(e.payload_json, '$.runState.lifecycle') = 'running'
								AND json_extract(e.payload_json, ?) = a.id
								AND r.state_version - 2 = ?)
						)
				)
		`, input.RunID, input.ApprovalID, input.WorkerID, input.AssignmentAttemptID,
		string(runoutcome.ReasonUserCancelled), runoutcome.CodeParentRunEnded,
		approvalIdentityPayloadPath, approvalIdentityPayloadPath,
		runStateVersionPayloadPath, input.ExpectedStateVersion, approvalIdentityPayloadPath, input.ExpectedStateVersion,
	).Scan(&outcomeReason)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil
	case err != nil:
		return "", err
	case outcomeReason == string(runoutcome.ReasonUserCancelled):
		return runoutcome.CodeUserCancelled, nil
	default:
		return runoutcome.CodeParentRunEnded, nil
	}
}
