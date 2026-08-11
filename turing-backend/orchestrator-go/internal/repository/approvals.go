package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

type ApprovalRecord struct {
	ApprovalID    string
	RunID         string
	ToolCallID    string
	AgentID       string
	ToolName      string
	ArgsJSON      string
	ArgsHash      string
	Status        string
	ApprovalToken string
	ExpiresAt     string
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
	if err := tx.Commit(); err != nil {
		return ApprovalRecord{}, err
	}
	return record, nil
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

func (r *Repository) ApproveApproval(ctx context.Context, approvalID string, approvalToken string, decidedAt string) (ApprovalRecord, error) {
	if decidedAt == "" {
		decidedAt = now()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE approvals SET status = 'approved', approval_jti = ?, approval_token = ?, decided_at = ? WHERE id = ? AND status = 'pending'`, approvalID, approvalToken, decidedAt, approvalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := expectOneRow(result, "approval is not pending"); err != nil {
		return ApprovalRecord{}, err
	}
	result, err = tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'running' WHERE id = (SELECT run_id FROM approvals WHERE id = ?) AND status = 'waiting_approval'`, approvalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := expectOneRow(result, "run is not waiting for approval"); err != nil {
		return ApprovalRecord{}, err
	}
	record, err := approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApprovalRecord{}, err
	}
	return record, nil
}

func (r *Repository) ExpireApproval(ctx context.Context, approvalID string, decidedAt string) (ApprovalRecord, error) {
	if decidedAt == "" {
		decidedAt = now()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE approvals SET status = 'expired', decided_at = ? WHERE id = ? AND status = 'pending'`, decidedAt, approvalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := expectOneRow(result, "approval is not pending"); err != nil {
		return ApprovalRecord{}, err
	}
	result, err = tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'failed', error_code = 'approval_expired', error_message = 'Approval expired', finished_at = ? WHERE id = (SELECT run_id FROM approvals WHERE id = ?) AND status = 'waiting_approval'`, decidedAt, approvalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := expectOneRow(result, "run not found for approval"); err != nil {
		return ApprovalRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'failed', finished_at = ?, error_code = 'approval_expired', error_message = 'Approval expired', lease_owner = NULL, lease_expires_at = NULL WHERE run_id = (SELECT run_id FROM approvals WHERE id = ?) AND status IN ('pending','in_progress')`, decidedAt, approvalID); err != nil {
		return ApprovalRecord{}, err
	}
	record, err := approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApprovalRecord{}, err
	}
	return record, nil
}

func (r *Repository) DenyApproval(ctx context.Context, approvalID string, decidedAt string) (ApprovalRecord, error) {
	if decidedAt == "" {
		decidedAt = now()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE approvals SET status = 'denied', decided_at = ? WHERE id = ? AND status = 'pending'`, decidedAt, approvalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := expectOneRow(result, "approval is not pending"); err != nil {
		return ApprovalRecord{}, err
	}
	result, err = tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'failed', error_code = 'approval_denied', error_message = 'User denied approval', finished_at = ? WHERE id = (SELECT run_id FROM approvals WHERE id = ?) AND status = 'waiting_approval'`, decidedAt, approvalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := expectOneRow(result, "run not found for approval"); err != nil {
		return ApprovalRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'failed', finished_at = ?, error_code = 'approval_denied', error_message = 'User denied approval', lease_owner = NULL, lease_expires_at = NULL WHERE run_id = (SELECT run_id FROM approvals WHERE id = ?) AND status IN ('pending','in_progress')`, decidedAt, approvalID); err != nil {
		return ApprovalRecord{}, err
	}
	record, err := approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApprovalRecord{}, err
	}
	return record, nil
}

func (r *Repository) ConsumeApproval(ctx context.Context, approvalID string, consumedAt string) (ApprovalRecord, error) {
	if consumedAt == "" {
		consumedAt = now()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE approvals SET status = 'consumed', consumed_at = ? WHERE id = ? AND status = 'approved'`, consumedAt, approvalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := expectOneRow(result, "approval is not approved"); err != nil {
		return ApprovalRecord{}, err
	}
	record, err := approvalByID(ctx, tx, approvalID)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApprovalRecord{}, err
	}
	return record, nil
}

type approvalQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func approvalByID(ctx context.Context, q approvalQuerier, approvalID string) (ApprovalRecord, error) {
	var record ApprovalRecord
	var toolCallID sql.NullString
	var approvalToken sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id, run_id, tool_call_id, agent_id, tool_name, args_json, args_hash, status, approval_token, expires_at FROM approvals WHERE id = ?`, approvalID).Scan(&record.ApprovalID, &record.RunID, &toolCallID, &record.AgentID, &record.ToolName, &record.ArgsJSON, &record.ArgsHash, &record.Status, &approvalToken, &record.ExpiresAt)
	if err != nil {
		return ApprovalRecord{}, err
	}
	if toolCallID.Valid {
		record.ToolCallID = toolCallID.String
	}
	if approvalToken.Valid {
		record.ApprovalToken = approvalToken.String
	}
	return record, nil
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
