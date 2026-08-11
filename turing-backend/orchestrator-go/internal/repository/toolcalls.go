package repository

import (
	"context"
	"database/sql"
	"errors"
)

var (
	ErrToolCallConflict = errors.New("tool call conflict")
	ErrToolCallNotFound = errors.New("tool call not found")
)

type ToolCallRecord struct {
	ToolCallID string
	RunID      string
	Status     string
	ApprovalID string
}

func (r *Repository) RecordToolCallBefore(ctx context.Context, record ToolCallRecord, agentID string, serverName string, toolName string, argsJSON string, argsHash string) error {
	status := record.Status
	if status == "" {
		status = "requested"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var existingRunID, existingAgentID, existingServerName, existingToolName, existingArgsHash string
	err = tx.QueryRowContext(ctx, `SELECT run_id, agent_id, server_name, tool_name, args_hash FROM tool_calls WHERE id = ?`, record.ToolCallID).Scan(&existingRunID, &existingAgentID, &existingServerName, &existingToolName, &existingArgsHash)
	if err == nil {
		if existingRunID == record.RunID && existingAgentID == agentID && existingServerName == serverName && existingToolName == toolName && existingArgsHash == argsHash {
			return tx.Commit()
		}
		return ErrToolCallConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var approvalID any
	if record.ApprovalID != "" {
		approvalID = record.ApprovalID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tool_calls (id, run_id, agent_id, server_name, tool_name, args_json, args_hash, status, approval_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.ToolCallID, record.RunID, agentID, serverName, toolName, argsJSON, argsHash, status, approvalID, now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) RecordToolCallAfter(ctx context.Context, toolCallID string, runID string, status string, resultSummary string, errorCode string, errorMessage string, durationMS int64) error {
	if status == "" {
		status = "completed"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE tool_calls SET status = ?, result_summary = ?, error_code = ?, error_message = ?, duration_ms = ?, completed_at = ? WHERE id = ? AND run_id = ?`, status, nullableText(resultSummary), nullableText(errorCode), nullableText(errorMessage), durationMS, now(), toolCallID, runID)
	if err != nil {
		return err
	}
	if err := expectOneRowErr(result, ErrToolCallNotFound); err != nil {
		return err
	}
	return tx.Commit()
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
