package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

type ApprovalRecord struct {
	ApprovalID      string
	RunID           string
	ToolCallID      string
	AgentID         string
	ToolName        string
	ArgsJSON        string
	ArgsHash        string
	Status          string
	ApprovalToken   string
	ExpiresAt       string
	ModelToolCallID string
}

type ApprovalTerminalization struct {
	Approval       ApprovalRecord
	ApprovalEvent  Event
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
	result, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'waiting_approval' WHERE id = ? AND status IN ('queued','running','waiting_approval')`, runID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := expectOneRow(result, "run cannot wait for approval"); err != nil {
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
	defer tx.Rollback()
	var nullableToolCallID any
	if toolCallID != "" {
		nullableToolCallID = toolCallID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO approvals (id, run_id, tool_call_id, agent_id, tool_name, args_json, args_hash, status, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`, record.ApprovalID, runID, nullableToolCallID, agentID, toolName, argsJSON, argsHash, expiresAt, createdAt); err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	if toolCallID != "" {
		result, err := tx.ExecContext(ctx, `UPDATE tool_calls SET approval_id = ?, status = 'approval_required' WHERE id = ? AND run_id = ?`, record.ApprovalID, toolCallID, runID)
		if err != nil {
			return ApprovalRecord{}, Event{}, err
		}
		if err := expectOneRow(result, "tool call not found"); err != nil {
			return ApprovalRecord{}, Event{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'waiting_approval' WHERE id = ? AND status IN ('queued','running','waiting_approval')`, runID)
	if err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	if err := expectOneRow(result, "run cannot wait for approval"); err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	record, err = approvalByID(ctx, tx, record.ApprovalID)
	if err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	event, err := appendApprovalLifecycleEventTx(ctx, tx, record, "approval.requested", createdAt)
	if err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApprovalRecord{}, Event{}, err
	}
	return record, event, nil
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

func (r *Repository) ApproveApproval(ctx context.Context, approvalID string, approvalToken string, decidedAt string) (ApprovalRecord, error) {
	transition, err := r.ApproveApprovalWithEvent(ctx, approvalID, approvalToken, decidedAt)
	return transition.Approval, err
}

func (r *Repository) ApproveApprovalWithEvent(ctx context.Context, approvalID string, approvalToken string, decidedAt string) (ApprovalTerminalization, error) {
	if decidedAt == "" {
		decidedAt = now()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	defer tx.Rollback()
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
	result, err := tx.ExecContext(ctx, `UPDATE approvals SET status = 'approved', approval_jti = ?, approval_token = ?, decided_at = ? WHERE id = ? AND status = 'pending'`, approvalID, approvalToken, decidedAt, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if err := expectOneRow(result, "approval is not pending"); err != nil {
		return ApprovalTerminalization{}, err
	}
	result, err = tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'running' WHERE id = (SELECT run_id FROM approvals WHERE id = ?) AND status = 'waiting_approval'`, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if err := expectOneRow(result, "run is not waiting for approval"); err != nil {
		return ApprovalTerminalization{}, err
	}
	record, err = approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	event, err := appendApprovalLifecycleEventTx(ctx, tx, record, "approval.approved", decidedAt)
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

func (r *Repository) DenyApproval(ctx context.Context, approvalID string, decidedAt string) (ApprovalRecord, error) {
	transition, err := r.DenyApprovalWithEvent(ctx, approvalID, decidedAt)
	return transition.Approval, err
}

func (r *Repository) ExpireApprovalWithEvent(ctx context.Context, approvalID string, decidedAt string) (ApprovalTerminalization, error) {
	return r.terminalizeApproval(ctx, approvalID, decidedAt, "expired", "approval_expired", "Approval expired", "failed", "approval.expired", false)
}

func (r *Repository) DenyApprovalWithEvent(ctx context.Context, approvalID string, decidedAt string) (ApprovalTerminalization, error) {
	return r.terminalizeApproval(ctx, approvalID, decidedAt, "denied", "approval_denied", "User denied approval", "denied", "approval.denied", true)
}

func (r *Repository) FailApprovalDeliveryWithEvent(ctx context.Context, approvalID string, decidedAt string) (ApprovalTerminalization, error) {
	return r.terminalizeApproval(ctx, approvalID, decidedAt, "denied", "approval_delivery_failed", "Tool policy decision delivery failed", "failed", "", false)
}

func (r *Repository) terminalizeApproval(
	ctx context.Context,
	approvalID string,
	decidedAt string,
	approvalStatus string,
	errorCode string,
	errorMessage string,
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
	defer tx.Rollback()
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
	if record.Status != "pending" {
		return ApprovalTerminalization{}, errors.New("approval is not pending")
	}
	if expireAtDeadline && approvalExpiredAtDecision(record.ExpiresAt, decidedAt) {
		approvalStatus = "expired"
		errorCode = "approval_expired"
		errorMessage = "Approval expired"
		toolCallStatus = "failed"
		approvalEventType = "approval.expired"
	}
	var sessionID, traceID, runStatus string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id, status FROM agent_runs WHERE id = ?`, record.RunID).Scan(&sessionID, &traceID, &runStatus); err != nil {
		return ApprovalTerminalization{}, err
	}
	lateRuntimeFailure := runStatus == "failed"
	if runStatus != "waiting_approval" && !lateRuntimeFailure {
		return ApprovalTerminalization{}, errors.New("run not found for approval")
	}
	if lateRuntimeFailure {
		if err := validateLateApprovalRuntimeFailure(ctx, tx, record); err != nil {
			return ApprovalTerminalization{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE approvals SET status = ?, decided_at = ? WHERE id = ? AND status = 'pending'`, approvalStatus, decidedAt, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if err := expectOneRow(result, "approval is not pending"); err != nil {
		return ApprovalTerminalization{}, err
	}
	if record.ToolCallID != "" {
		result, err = tx.ExecContext(ctx, `
			UPDATE tool_calls
			SET status = ?, error_code = ?, error_message = ?, completed_at = ?
			WHERE id = ? AND run_id = ? AND status IN ('requested', 'allowed', 'approval_required')
		`, toolCallStatus, errorCode, errorMessage, decidedAt, record.ToolCallID, record.RunID)
		if err != nil {
			return ApprovalTerminalization{}, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return ApprovalTerminalization{}, err
		}
		if changed != 1 {
			var currentStatus string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM tool_calls WHERE id = ? AND run_id = ?`, record.ToolCallID, record.RunID).Scan(&currentStatus); err != nil {
				return ApprovalTerminalization{}, err
			}
			if !isTerminalToolCallStatus(currentStatus) {
				return ApprovalTerminalization{}, errors.New("tool call not found for approval")
			}
		}
	}
	var event Event
	if !lateRuntimeFailure {
		result, err = tx.ExecContext(ctx, `
			UPDATE agent_runs
			SET status = 'failed', error_code = ?, error_message = ?, finished_at = ?
			WHERE id = ? AND status = 'waiting_approval'
		`, errorCode, errorMessage, decidedAt, record.RunID)
		if err != nil {
			return ApprovalTerminalization{}, err
		}
		if err := expectOneRow(result, "run not found for approval"); err != nil {
			return ApprovalTerminalization{}, err
		}
		result, err = tx.ExecContext(ctx, `
			UPDATE jobs
			SET status = 'failed', finished_at = ?, error_code = ?, error_message = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL
			WHERE run_id = ? AND status IN ('pending', 'in_progress')
		`, decidedAt, errorCode, errorMessage, record.RunID)
		if err != nil {
			return ApprovalTerminalization{}, err
		}
		if err := expectOneRow(result, "job not found for approval"); err != nil {
			return ApprovalTerminalization{}, err
		}
		payloadJSON, err := json.Marshal(map[string]any{
			"runId":     record.RunID,
			"code":      errorCode,
			"message":   errorMessage,
			"retryable": false,
		})
		if err != nil {
			return ApprovalTerminalization{}, err
		}
		event, err = appendRunEventTx(ctx, tx, sessionID, record.RunID, traceID, "agent.run.failed", string(payloadJSON), decidedAt)
		if err != nil {
			return ApprovalTerminalization{}, err
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
	if err := tx.Commit(); err != nil {
		return ApprovalTerminalization{}, err
	}
	return ApprovalTerminalization{Approval: record, ApprovalEvent: approvalEvent, RunFailedEvent: event, Changed: true}, nil
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
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	defer tx.Rollback()
	current, err := approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if current.Status == "consumed" {
		if err := tx.Commit(); err != nil {
			return ApprovalTerminalization{}, err
		}
		return ApprovalTerminalization{Approval: current}, nil
	}
	if current.Status != "approved" {
		return ApprovalTerminalization{}, errors.New("approval is not approved")
	}
	result, err := tx.ExecContext(ctx, `UPDATE approvals SET status = 'consumed', consumed_at = ? WHERE id = ? AND status = 'approved'`, consumedAt, approvalID)
	if err != nil {
		return ApprovalTerminalization{}, err
	}
	if err := expectOneRow(result, "approval is not approved"); err != nil {
		return ApprovalTerminalization{}, err
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
	var modelToolCallID sql.NullString
	err := q.QueryRowContext(ctx, `
		SELECT a.id, a.run_id, a.tool_call_id, a.agent_id, a.tool_name, a.args_json, a.args_hash,
			a.status, a.approval_token, a.expires_at, tc.model_tool_call_id
		FROM approvals a
		LEFT JOIN tool_calls tc ON tc.id = a.tool_call_id
		WHERE a.id = ?
	`, approvalID).Scan(&record.ApprovalID, &record.RunID, &toolCallID, &record.AgentID, &record.ToolName, &record.ArgsJSON, &record.ArgsHash, &record.Status, &approvalToken, &record.ExpiresAt, &modelToolCallID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if toolCallID.Valid {
		record.ToolCallID = toolCallID.String
	}
	if approvalToken.Valid {
		record.ApprovalToken = approvalToken.String
	}
	if modelToolCallID.Valid {
		record.ModelToolCallID = modelToolCallID.String
	}
	return record, nil
}

func appendApprovalLifecycleEventTx(ctx context.Context, tx *sql.Tx, approval ApprovalRecord, eventType string, createdAt string) (Event, error) {
	var sessionID, traceID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, approval.RunID).Scan(&sessionID, &traceID); err != nil {
		return Event{}, err
	}
	payload := map[string]any{
		"approvalId": approval.ApprovalID,
		"toolCallId": approval.ToolCallID,
		"toolName":   approval.ToolName,
		"runId":      approval.RunID,
		"traceId":    traceID,
	}
	if eventType == "approval.requested" {
		payload["argsSummary"] = "Requested tool use"
	}
	if approval.ModelToolCallID != "" {
		payload["modelToolCallId"] = approval.ModelToolCallID
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	return appendRunEventTx(ctx, tx, sessionID, approval.RunID, traceID, eventType, string(payloadJSON), createdAt)
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
