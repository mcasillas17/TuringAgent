package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

var (
	ErrApprovalExpired         = errors.New("approval expired")
	ErrApprovalAlreadyConsumed = errors.New("approval already consumed")
)

type ApprovalRecord struct {
	ApprovalID      string
	RunID           string
	ToolCallID      string
	AgentID         string
	ServerName      string
	ToolName        string
	ArgsJSON        string
	ArgsHash        string
	Status          string
	ApprovalToken   string
	ApprovalComment sql.NullString
	DenialReason    sql.NullString
	ExpiresAt       string
	ModelToolCallID string
}

type ApprovalTerminalization struct {
	Approval       ApprovalRecord
	ApprovalEvent  Event
	ToolEvent      Event
	RunFailedEvent Event
	Changed        bool
}

func (r *Repository) CreateApproval(ctx context.Context, runID string, toolCallID string, agentID string, toolName string, argsJSON string, argsHash string, expiresAt string) (ApprovalRecord, error) {
	createdAt := now()
	record := ApprovalRecord{
		ApprovalID: ids.New("appr"),
		RunID:      runID,
		ToolCallID: toolCallID,
		AgentID:    agentID,
		ToolName:   toolName,
		ArgsJSON:   argsJSON,
		ArgsHash:   argsHash,
		Status:     "pending",
		ExpiresAt:  expiresAt,
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var nullableToolCallID any
	if toolCallID != "" {
		nullableToolCallID = toolCallID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO approvals (id, run_id, tool_call_id, agent_id, tool_name, args_json, args_hash, status, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`, record.ApprovalID, runID, nullableToolCallID, agentID, toolName, argsJSON, argsHash, expiresAt, createdAt); err != nil {
		return ApprovalRecord{}, err
	}
	if toolCallID != "" {
		result, err := tx.ExecContext(ctx, `UPDATE tool_calls SET approval_id = ?, status = 'approval_required' WHERE id = ? AND run_id = ?`, record.ApprovalID, toolCallID, runID)
		if err != nil {
			return ApprovalRecord{}, err
		}
		if err := expectOneRow(result, "tool call not found"); err != nil {
			return ApprovalRecord{}, err
		}
	}
	if _, err := awaitApprovalTransitionTx(ctx, tx, runID, record.ApprovalID); err != nil {
		return ApprovalRecord{}, err
	}
	record, err = approvalByID(ctx, tx, record.ApprovalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApprovalRecord{}, err
	}
	return record, nil
}

func (r *Repository) CreateApprovalWithEvent(ctx context.Context, runID string, toolCallID string, agentID string, toolName string, argsJSON string, argsHash string, expiresAt string) (ApprovalRecord, Event, error) {
	createdAt := now()
	record := ApprovalRecord{
		ApprovalID: ids.New("appr"),
		RunID:      runID,
		ToolCallID: toolCallID,
		AgentID:    agentID,
		ToolName:   toolName,
		ArgsJSON:   argsJSON,
		ArgsHash:   argsHash,
		Status:     "pending",
		ExpiresAt:  expiresAt,
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var nullableToolCallID any
	if toolCallID != "" {
		nullableToolCallID = toolCallID
	}
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO approvals (id, run_id, tool_call_id, agent_id, tool_name, args_json, args_hash, status, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`, record.ApprovalID, runID, nullableToolCallID, agentID, toolName, argsJSON, argsHash, expiresAt, createdAt)
	if err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	if inserted == 0 {
		var existingID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM approvals WHERE run_id = ? AND tool_call_id = ?`, runID, toolCallID).Scan(&existingID); err != nil {
			return ApprovalRecord{}, Event{}, err
		}
		existing, err := approvalByID(ctx, tx, existingID)
		if err != nil {
			return ApprovalRecord{}, Event{}, err
		}
		if existing.AgentID != agentID || existing.ToolName != toolName || existing.ArgsHash != argsHash {
			return ApprovalRecord{}, Event{}, errors.New("approval tool context conflicts with existing approval")
		}
		if err := tx.Commit(); err != nil {
			return ApprovalRecord{}, Event{}, err
		}
		return existing, Event{}, nil
	}
	if toolCallID != "" {
		result, err = tx.ExecContext(ctx, `UPDATE tool_calls SET approval_id = ?, status = 'approval_required' WHERE id = ? AND run_id = ?`, record.ApprovalID, toolCallID, runID)
		if err != nil {
			return ApprovalRecord{}, Event{}, err
		}
		if err := expectOneRow(result, "tool call not found"); err != nil {
			return ApprovalRecord{}, Event{}, err
		}
	}
	record, err = approvalByID(ctx, tx, record.ApprovalID)
	if err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	// approval.requested IS the lifecycle event for running -> waiting approval,
	// so it carries the resulting state instead of a second state-changed event
	// landing beside it.
	var requestTraceID string
	if err := tx.QueryRowContext(ctx, `SELECT trace_id FROM agent_runs WHERE id = ?`, runID).Scan(&requestTraceID); err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	transition, err := awaitApprovalTransitionTx(ctx, tx, runID, record.ApprovalID,
		approvalLifecyclePayload(record, requestTraceID, "approval.requested"))
	if err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	var event Event
	if len(transition.Events) > 0 {
		event = transition.Events[0]
	} else {
		// The run was already waiting on an earlier approval, so its lifecycle
		// did not change. The request still has to be announced, on the same
		// terms as the decision below: inside this transaction, carrying the
		// run state as it stands, so a client cannot receive one
		// approval.requested with a state projection and another without.
		event, err = appendApprovalRunStateEventTx(ctx, tx, record, "approval.requested", transition.State, createdAt)
		if err != nil {
			return ApprovalRecord{}, Event{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	return record, event, nil
}

// awaitApprovalTransitionTx commits running -> waiting approval.
//
// Recovering is deliberately not an allowed source. A run whose worker
// ownership is unproven cannot promise to make the call the user would be
// authorizing, and a second pending approval on a run nobody owns is a decision
// the user makes for nothing. Queued is not a source either: there is no worker
// yet to have asked.
func awaitApprovalTransitionTx(ctx context.Context, tx *sql.Tx, runID string, approvalID string, payload ...map[string]any) (RunTransitionResult, error) {
	transition := runTransition{
		runID:            runID,
		expectedVersion:  unresolvedStateVersion,
		transactionLocal: true,
		allowedFrom:      []string{lifecycleRunning},
		to:               lifecycleWaitingApproval,
		reason:           runoutcome.ReasonNone,
		identity:         runTransitionIdentity{approvalID: approvalID},
	}
	if len(payload) == 1 {
		transition.eventType = "approval.requested"
		transition.eventPayload = payload[0]
	}
	return applyRunTransitionTx(ctx, tx, transition, nil)
}

// ResumeApprovedRunInput is the trigger identity a worker must prove to move a
// run out of waiting-approval.
//
// Every field is part of that identity rather than a hint: the run the resume
// is about, the approval the decision belongs to, the worker the orchestrator
// authenticated, the assignment attempt that still owns the run, and the
// version the worker computed its readiness against. A resume missing any of
// them could be a fenced predecessor's, and a fenced predecessor resuming a run
// somebody else now owns is the exact failure this transition exists to refuse.
type ResumeApprovedRunInput struct {
	RunID                string
	ApprovalID           string
	WorkerID             string
	AssignmentAttemptID  string
	ExpectedStateVersion int64
}

// ResumeApprovedRun returns a waiting-approval run to running.
//
// Persisting a decision, minting its token, or delivering it to a worker does
// not do this: none of them is the worker saying it has restored the paused
// attempt and is ready to act. This is that statement, and it is guarded like
// every other lifecycle change — one version forward, from waiting-approval
// only, for the exact worker and attempt the row still records.
//
// The approval is guarded twice over, because it is the only part of the
// trigger the run row cannot vouch for. It has to still authorize something,
// which pending, denied and expired do not; and it is committed into the
// transition's own event so the duplicate rule can tell the resume that
// actually moved this run from a second one arriving behind it. Without that,
// a run with two outstanding authorizations answers the second Ready with the
// first one's acceptance.
//
// A repeat of the identical resume is the write-free duplicate the shared core
// already recognizes, which is what lets a worker that lost the acceptance ask
// again and be told the same thing rather than fenced.
func (r *Repository) ResumeApprovedRun(ctx context.Context, input ResumeApprovedRunInput) (RunTransitionResult, error) {
	return r.runInTransition(ctx, runTransition{
		runID:           input.RunID,
		expectedVersion: input.ExpectedStateVersion,
		allowedFrom:     []string{lifecycleWaitingApproval},
		to:              lifecycleRunning,
		reason:          runoutcome.ReasonNone,
		identity: runTransitionIdentity{
			workerID:            input.WorkerID,
			assignmentAttemptID: input.AssignmentAttemptID,
			approvalID:          input.ApprovalID,
		},
		requiresAuthorizedApproval: true,
		durableApprovalIdentity:    true,
		eventPayload:               map[string]any{approvalIdentityPayloadKey: input.ApprovalID},
	}, nil)
}

func (r *Repository) GetApproval(ctx context.Context, approvalID string) (ApprovalRecord, error) {
	return approvalByID(ctx, r.db, approvalID)
}

func (r *Repository) GetApprovalByToolCall(ctx context.Context, runID string, toolCallID string) (ApprovalRecord, error) {
	var approvalID string
	if err := r.db.QueryRowContext(ctx, `SELECT id FROM approvals WHERE run_id = ? AND tool_call_id = ?`, runID, toolCallID).Scan(&approvalID); err != nil {
		return ApprovalRecord{}, err
	}
	return approvalByID(ctx, r.db, approvalID)
}

func (r *Repository) GetPendingApprovalForRun(ctx context.Context, runID string) (ApprovalRecord, error) {
	var approvalID string
	query := `SELECT id FROM approvals WHERE run_id = ? AND status = 'pending' ORDER BY ` + sqliteTimestampNanos("created_at") + ` DESC, id DESC LIMIT 1`
	if err := r.db.QueryRowContext(ctx, query, runID).Scan(&approvalID); err != nil {
		return ApprovalRecord{}, err
	}
	return approvalByID(ctx, r.db, approvalID)
}

func (r *Repository) ApproveApproval(ctx context.Context, approvalID string, approvalToken string, approvalComment sql.NullString, decidedAt string) (ApprovalRecord, error) {
	transition, err := r.ApproveApprovalWithEvent(ctx, approvalID, approvalToken, approvalComment, decidedAt)
	return transition.Approval, err
}

func (r *Repository) ApproveApprovalWithEvent(ctx context.Context, approvalID string, approvalToken string, approvalComment sql.NullString, decidedAt string) (ApprovalTerminalization, error) {
	if decidedAt == "" {
		decidedAt = now()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if record.Status == "approved" {
		if err := tx.Commit(); err != nil {
			return ApprovalTerminalization{}, err
		}
		return ApprovalTerminalization{Approval: record}, nil
	}
	if record.Status != "pending" {
		return ApprovalTerminalization{}, errors.New("approval is not pending")
	}
	if approvalExpiredAtDecision(record.ExpiresAt, decidedAt) {
		return ApprovalTerminalization{Approval: record}, ErrApprovalExpired
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE approvals
		SET status = 'approved',
			approval_jti = ?,
			approval_token = ?,
			approval_comment = ?,
			decided_at = ?
		WHERE id = ? AND status = 'pending'
	`, approvalID, approvalToken, nullableString(approvalComment), decidedAt, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if err := expectOneRow(result, "approval is not pending"); err != nil {
		return ApprovalTerminalization{}, err
	}
	record, err = approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	// Approving records a decision; it does not resume the run.
	//
	// Persisting the approval and minting its token are both things the
	// orchestrator does to itself, and the worker they are for may not even be
	// reachable. The row leaves waiting-approval only when that worker proves
	// it restored the paused attempt and the guarded resume commits — see
	// ResumeApprovedRun. Moving it here used to mean a run could report itself
	// running while the only process that could act on the approval had
	// already gone.
	//
	// The decision still has to be announced inside this transaction, or every
	// client goes on asking the user to authorize something they already
	// authorized. It carries the run state as it stands, so the projection can
	// never disagree with the row it describes.
	row, err := readRunRow(ctx, tx, record.RunID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	// A decision may only be recorded for a run that could still act on it.
	// Waiting-approval is the ordinary case; running covers a run an earlier
	// decision already resumed while it still held this second authorization.
	// Anything else — recovering, queued, or terminal — has no attempt that
	// could make the call the user is being asked to authorize.
	if row.lifecycle != lifecycleWaitingApproval && row.lifecycle != lifecycleRunning {
		return ApprovalTerminalization{}, errors.New("run is not waiting for approval")
	}
	event, err := appendApprovalRunStateEventTx(ctx, tx, record, "approval.approved", row.state(), decidedAt)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApprovalTerminalization{}, err
	}
	return ApprovalTerminalization{Approval: record, ApprovalEvent: event, Changed: true}, nil
}

func (r *Repository) ExpireApproval(ctx context.Context, approvalID string, decidedAt string) (ApprovalRecord, error) {
	transition, err := r.ExpireApprovalWithEvent(ctx, approvalID, decidedAt)
	return transition.Approval, err
}

func (r *Repository) DenyApproval(ctx context.Context, approvalID string, denialReason sql.NullString, decidedAt string) (ApprovalRecord, error) {
	transition, err := r.DenyApprovalWithEvent(ctx, approvalID, denialReason, decidedAt)
	return transition.Approval, err
}

func (r *Repository) ExpireApprovalWithEvent(ctx context.Context, approvalID string, decidedAt string) (ApprovalTerminalization, error) {
	return r.terminalizeApproval(ctx, approvalID, sql.NullString{}, decidedAt, "expired",
		runoutcome.NormalizeFailure(runoutcome.OriginApprovalExpiry, "approval_expired", runoutcome.RetryClassNever),
		"failed", "approval.expired", false)
}

func (r *Repository) DenyApprovalWithEvent(ctx context.Context, approvalID string, denialReason sql.NullString, decidedAt string) (ApprovalTerminalization, error) {
	// approval_denied is not an allowlisted failure code, and it is not meant to
	// be: a user refusing a tool call is a policy outcome, not a system fault.
	return r.terminalizeApproval(ctx, approvalID, denialReason, decidedAt, "denied",
		runoutcome.NormalizeFailure(runoutcome.OriginToolPolicy, "tool_policy_decision_failed", runoutcome.RetryClassNever),
		"denied", "approval.denied", true)
}

func (r *Repository) FailApprovalDeliveryWithEvent(ctx context.Context, approvalID string, decidedAt string) (ApprovalTerminalization, error) {
	return r.terminalizeApproval(ctx, approvalID, sql.NullString{}, decidedAt, "denied",
		runoutcome.NormalizeFailure(runoutcome.OriginApprovalTransport, "approval_delivery_failed", runoutcome.RetryClassNever),
		"failed", "approval.denied", false)
}

func (r *Repository) terminalizeApproval(
	ctx context.Context,
	approvalID string,
	denialReason sql.NullString,
	decidedAt string,
	approvalStatus string,
	failure runoutcome.Failure,
	toolCallStatus string,
	approvalEventType string,
	expireAtDeadline bool,
) (ApprovalTerminalization, error) {
	if decidedAt == "" {
		decidedAt = now()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if record.Status == approvalStatus {
		if err := tx.Commit(); err != nil {
			return ApprovalTerminalization{}, err
		}
		return ApprovalTerminalization{Approval: record}, nil
	}
	approvedExpiration := approvalStatus == "expired" && record.Status == "approved"
	if record.Status != "pending" && !approvedExpiration {
		return ApprovalTerminalization{}, errors.New("approval is not pending")
	}
	if expireAtDeadline && approvalExpiredAtDecision(record.ExpiresAt, decidedAt) {
		approvalStatus = "expired"
		failure = runoutcome.NormalizeFailure(runoutcome.OriginApprovalExpiry, "approval_expired", runoutcome.RetryClassNever)
		toolCallStatus = "failed"
		approvalEventType = "approval.expired"
	}
	errorCode := failure.Code()
	category := failure.Reason()
	var sessionID, traceID, runStatus string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id, status FROM agent_runs WHERE id = ?`, record.RunID).Scan(&sessionID, &traceID, &runStatus); err != nil {
		return ApprovalTerminalization{}, err
	}
	lateRuntimeFailure := runStatus == lifecycleFailed
	// Recovering is accepted for the same reason the recovery scan sees it: a
	// run fenced out of waiting-approval still holds this pending approval, and
	// refusing to close it here would leave the user's decision stranded.
	awaitingDecision := runStatus == lifecycleWaitingApproval || runStatus == lifecycleRecovering
	if !awaitingDecision && !lateRuntimeFailure && (!approvedExpiration || runStatus != lifecycleRunning) {
		return ApprovalTerminalization{}, errors.New("run not found for approval")
	}
	if lateRuntimeFailure {
		if err := validateLateApprovalRuntimeFailure(ctx, tx, record); err != nil {
			return ApprovalTerminalization{}, err
		}
	}
	approvalStatusPredicate := "status = 'pending'"
	if approvedExpiration {
		approvalStatusPredicate = "status = 'approved'"
	}
	storedDenialReason := sql.NullString{}
	if approvalStatus == "denied" {
		storedDenialReason = denialReason
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE approvals
		SET status = ?,
			approval_jti = CASE WHEN ? = 'expired' THEN NULL ELSE approval_jti END,
			approval_token = CASE WHEN ? = 'expired' THEN NULL ELSE approval_token END,
			denial_reason = ?,
			decided_at = ?
		WHERE id = ? AND `+approvalStatusPredicate,
		approvalStatus,
		approvalStatus,
		approvalStatus,
		nullableString(storedDenialReason),
		decidedAt,
		approvalID,
	)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if err := expectOneRow(result, "approval is not pending"); err != nil {
		return ApprovalTerminalization{}, err
	}
	toolCallChanged := false
	toolCallServerName := ""
	if record.ToolCallID != "" {
		result, err = tx.ExecContext(ctx, `
			UPDATE tool_calls
			SET status = ?, error_code = ?, error_message = NULL, completed_at = ?
			WHERE id = ? AND run_id = ? AND status IN ('requested', 'allowed', 'approval_required')
		`, toolCallStatus, errorCode, decidedAt, record.ToolCallID, record.RunID)
		if err != nil {
			return ApprovalTerminalization{}, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return ApprovalTerminalization{}, err
		}
		toolCallChanged = changed == 1
		if changed != 1 {
			var currentStatus string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = ? AND run_id = ?`, record.ToolCallID, record.RunID).Scan(&currentStatus); err != nil {
				return ApprovalTerminalization{}, err
			}
			if !isTerminalToolCallStatus(currentStatus) {
				return ApprovalTerminalization{}, errors.New("tool call not found for approval")
			}
		}
		if toolCallChanged {
			if err := tx.QueryRowContext(ctx, `SELECT server_name FROM tool_calls WHERE id = ? AND run_id = ?`, record.ToolCallID, record.RunID).Scan(&toolCallServerName); err != nil {
				return ApprovalTerminalization{}, err
			}
		}
	}
	record, err = approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	var approvalEvent Event
	if approvalEventType != "" {
		approvalEvent, err = appendApprovalLifecycleEventTx(ctx, tx, record, approvalEventType, decidedAt)
		if err != nil {
			return ApprovalTerminalization{}, err
		}
	}
	var toolEvent Event
	if toolCallChanged {
		payloadJSON, err := marshalToolLifecyclePayload(record.ToolCallID, toolCallServerName, record.ToolName, category)
		if err != nil {
			return ApprovalTerminalization{}, err
		}
		toolEventType := "tool.call.failed"
		if toolCallStatus == "denied" {
			toolEventType = "tool.call.denied"
		}
		toolEvent, err = appendRunEventTx(ctx, tx, sessionID, record.RunID, traceID, toolEventType, payloadJSON, decidedAt)
		if err != nil {
			return ApprovalTerminalization{}, err
		}
	}
	var event Event
	if !lateRuntimeFailure {
		// Recovering is a legal source here: a run fenced out of waiting
		// approval still holds the pending approval, and leaving it
		// unterminalizable would strand the user's decision forever. An
		// already-approved authorization that expires may also be terminalized
		// from running.
		allowedFrom := []string{lifecycleWaitingApproval, lifecycleRecovering}
		if approvedExpiration {
			allowedFrom = append(allowedFrom, lifecycleRunning)
		}
		terminal, err := failRunTx(ctx, tx, FailRunInput{
			RunID:                   record.RunID,
			Failure:                 failure,
			allowedFrom:             allowedFrom,
			resolveVersionInTx:      true,
			leaveExecutionUntouched: true,
		})
		if err != nil {
			if errors.Is(err, ErrRunNotFailable) || errors.Is(err, ErrRunTransitionConflict) {
				return ApprovalTerminalization{}, errors.New("run not found for approval")
			}
			return ApprovalTerminalization{}, err
		}
		for _, appended := range terminal.Events {
			if appended.Type == "agent.run.failed" {
				event = appended
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return ApprovalTerminalization{}, err
	}
	return ApprovalTerminalization{Approval: record, ApprovalEvent: approvalEvent, ToolEvent: toolEvent, RunFailedEvent: event, Changed: true}, nil
}

func approvalExpiredAtDecision(expiresAt string, decidedAt string) bool {
	expiry, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return true
	}
	decision, err := time.Parse(time.RFC3339Nano, decidedAt)
	if err != nil {
		return true
	}
	return !expiry.After(decision)
}

func validateLateApprovalRuntimeFailure(ctx context.Context, tx *sql.Tx, record ApprovalRecord) error {
	if record.ToolCallID != "" {
		var toolCallStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = ? AND run_id = ?`, record.ToolCallID, record.RunID).Scan(&toolCallStatus); err != nil {
			return err
		}
		if toolCallStatus != "failed" {
			return errors.New("run failed before approval terminalization is inconsistent")
		}
	}
	var jobStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE run_id = ?`, record.RunID).Scan(&jobStatus); err != nil {
		return err
	}
	if jobStatus != "failed" {
		return errors.New("run failed before approval terminalization is inconsistent")
	}
	var failedEvents int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, record.RunID).Scan(&failedEvents); err != nil {
		return err
	}
	if failedEvents != 1 {
		return errors.New("run failed before approval terminalization is inconsistent")
	}
	return nil
}

func isTerminalToolCallStatus(status string) bool {
	switch status {
	case "completed", "failed", "denied":
		return true
	default:
		return false
	}
}

func (r *Repository) ConsumeApproval(ctx context.Context, approvalID string, consumedAt string) (ApprovalRecord, error) {
	transition, err := r.ConsumeApprovalWithEvent(ctx, approvalID, consumedAt)
	return transition.Approval, err
}

func (r *Repository) ConsumeApprovalWithEvent(ctx context.Context, approvalID string, consumedAt string) (ApprovalTerminalization, error) {
	if consumedAt == "" {
		consumedAt = now()
	}
	consumedTime, err := time.Parse(time.RFC3339Nano, consumedAt)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if current.Status == "consumed" {
		return ApprovalTerminalization{Approval: current}, ErrApprovalAlreadyConsumed
	}
	if current.Status != "approved" {
		return ApprovalTerminalization{}, errors.New("approval is not approved")
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE approvals
		SET status = 'consumed', consumed_at = ?
		WHERE id = ?
			AND status = 'approved'
			AND `+sqliteTimestampNanos("expires_at")+` > ?
	`, consumedAt, approvalID, consumedTime.UTC().UnixNano())
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if changed != 1 {
		if approvalExpiredAtDecision(current.ExpiresAt, consumedAt) {
			if err := tx.Rollback(); err != nil {
				return ApprovalTerminalization{}, err
			}
			transition, terminalizeErr := r.ExpireApprovalWithEvent(ctx, approvalID, consumedAt)
			if terminalizeErr != nil {
				return ApprovalTerminalization{}, terminalizeErr
			}
			return transition, ErrApprovalExpired
		}
		return ApprovalTerminalization{}, errors.New("approval is not approved")
	}
	record, err := approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	event, err := appendApprovalLifecycleEventTx(ctx, tx, record, "approval.consumed", consumedAt)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApprovalTerminalization{}, err
	}
	return ApprovalTerminalization{Approval: record, ApprovalEvent: event, Changed: true}, nil
}

type approvalQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func approvalByID(ctx context.Context, q approvalQuerier, approvalID string) (ApprovalRecord, error) {
	var record ApprovalRecord
	var toolCallID sql.NullString
	var approvalToken sql.NullString
	var approvalComment sql.NullString
	var denialReason sql.NullString
	var modelToolCallID sql.NullString
	err := q.QueryRowContext(ctx, `
		SELECT a.id, a.run_id, a.tool_call_id, a.agent_id, COALESCE(tc.server_name, ''), a.tool_name, a.args_json, a.args_hash,
			a.status, a.approval_token, a.approval_comment, a.denial_reason, a.expires_at, tc.model_tool_call_id
		FROM approvals a
		LEFT JOIN tool_calls tc ON tc.id = a.tool_call_id
		WHERE a.id = ?
	`, approvalID).Scan(
		&record.ApprovalID,
		&record.RunID,
		&toolCallID,
		&record.AgentID,
		&record.ServerName,
		&record.ToolName,
		&record.ArgsJSON,
		&record.ArgsHash,
		&record.Status,
		&approvalToken,
		&approvalComment,
		&denialReason,
		&record.ExpiresAt,
		&modelToolCallID,
	)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if toolCallID.Valid {
		record.ToolCallID = toolCallID.String
	}
	if approvalToken.Valid {
		record.ApprovalToken = approvalToken.String
	}
	record.ApprovalComment = approvalComment
	record.DenialReason = denialReason
	if modelToolCallID.Valid {
		record.ModelToolCallID = modelToolCallID.String
	}
	return record, nil
}

// approvalFailureCategory is the repository's alias for the shared rule that
// maps an approval event type to the one category it may carry. The rule lives
// in runoutcome so the live writers here and the migration that rewrites
// historical rows cannot answer the question differently.
func approvalFailureCategory(eventType string) (runoutcome.Reason, bool) {
	return runoutcome.ApprovalFailureCategory(eventType)
}

// approvalLifecyclePayload is the shared approval projection. It is built
// separately from the append so an approval event that IS a run lifecycle
// transition can be emitted by the guarded transition core, carrying the
// resulting run state, instead of being appended beside it.
func approvalLifecyclePayload(approval ApprovalRecord, traceID string, eventType string) map[string]any {
	payload := map[string]any{
		"approvalId": approval.ApprovalID,
		"toolName":   approval.ToolName,
		"runId":      approval.RunID,
		"traceId":    traceID,
	}
	// tool_call_id is nullable, and the migration drops identity keys that are
	// empty rather than publishing a blank one. Emitting "" here would make a
	// freshly written approval failure distinguishable from a rewritten one on
	// exactly the key a client uses to join the event to its tool call.
	if approval.ToolCallID != "" {
		payload["toolCallId"] = approval.ToolCallID
	}
	if eventType == "approval.requested" {
		payload["argsSummary"] = approvalArgsSummary(approval.ArgsJSON)
	}
	if approval.ModelToolCallID != "" {
		payload["modelToolCallId"] = approval.ModelToolCallID
	}
	if category, ok := approvalFailureCategory(eventType); ok {
		payload["category"] = string(category)
	}
	return payload
}

// terminalApproval is the identity of one approval that a run-level cleanup is
// ending. The two cleanups that revoke approvals in bulk — a run reaching a
// terminal state, and a recovery discarding an authorization it can no longer
// trust — read approvals directly rather than through ApprovalRecord, so this
// carries just the identity the projection is allowed to publish.
type terminalApproval struct {
	id, toolCallID, toolName, modelToolCallID string
}

// appendApprovalTerminalEventTx writes the terminal projection for one approval
// revoked by a run-level cleanup.
//
// Both cleanups used to hand-build this payload and label it with a category
// borrowed from whatever was failing around them — the run's own failure
// reason in one case, side_effect_uncertain in the other. Neither described the
// approval: an approval swept up by a terminal run or a lost lease expired,
// full stop. Routing both through one helper means the category comes from the
// event type, exactly as it does for the decided approvals, so a reader cannot
// tell whether an approval.expired was written by a decision path or a cleanup.
func appendApprovalTerminalEventTx(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
	runID string,
	traceID string,
	approval terminalApproval,
	eventType string,
	createdAt string,
) (Event, error) {
	category, ok := approvalFailureCategory(eventType)
	if !ok {
		return Event{}, fmt.Errorf("%q is not an approval failure event", eventType)
	}
	payload := map[string]any{
		"approvalId": approval.id,
		"toolName":   approval.toolName,
		"category":   string(category),
	}
	if approval.toolCallID != "" {
		payload["toolCallId"] = approval.toolCallID
	}
	if approval.modelToolCallID != "" {
		payload["modelToolCallId"] = approval.modelToolCallID
	}
	payloadJSON, err := marshalEventPayload(payload)
	if err != nil {
		return Event{}, err
	}
	return appendRunEventTx(ctx, tx, sessionID, runID, traceID, eventType, payloadJSON, createdAt)
}

func appendApprovalLifecycleEventTx(ctx context.Context, tx *sql.Tx, approval ApprovalRecord, eventType string, createdAt string) (Event, error) {
	var sessionID, traceID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, approval.RunID).Scan(&sessionID, &traceID); err != nil {
		return Event{}, err
	}
	payloadJSON, err := marshalEventPayload(approvalLifecyclePayload(approval, traceID, eventType))
	if err != nil {
		return Event{}, err
	}
	return appendRunEventTx(ctx, tx, sessionID, approval.RunID, traceID, eventType, payloadJSON, createdAt)
}

// appendApprovalRunStateEventTx announces an approval lifecycle event on a run
// whose lifecycle did not change, carrying the state that is actually current.
//
// It is the fallback for the two events that normally ARE run transitions:
// when a run already has a pending approval, a second request does not move it
// to waiting approval, and when an earlier decision already resumed it, a
// second decision does not move it to running. The approval row still changed,
// so the event is owed — and it carries the same snapshot the transition-borne
// event would have carried, so one event type never arrives sometimes with a
// run state and sometimes without.
//
// The state must be the one the guarded transition just reported, read under
// the same transaction, rather than a fresh read that could disagree with it.
func appendApprovalRunStateEventTx(ctx context.Context, tx *sql.Tx, approval ApprovalRecord, eventType string, state RunState, createdAt string) (Event, error) {
	var sessionID, traceID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, approval.RunID).Scan(&sessionID, &traceID); err != nil {
		return Event{}, err
	}
	payloadJSON, err := marshalRunStatePayload(approvalLifecyclePayload(approval, traceID, eventType), state)
	if err != nil {
		return Event{}, err
	}
	return appendRunEventTx(ctx, tx, sessionID, approval.RunID, traceID, eventType, payloadJSON, createdAt)
}

func approvalArgsSummary(argsJSON string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err == nil {
		if path, ok := args["path"].(string); ok && path != "" {
			return "Requested change to " + path
		}
	}
	return "Requested tool use"
}

func expectOneRow(result sql.Result, message string) error {
	return expectOneRowErr(result, errors.New(message))
}

func expectOneRowErr(result sql.Result, noRowsErr error) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return noRowsErr
	}
	return nil
}
