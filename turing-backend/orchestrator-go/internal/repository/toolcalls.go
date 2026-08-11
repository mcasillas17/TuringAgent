package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

var (
	ErrToolCallConflict          = errors.New("tool call conflict")
	ErrToolCallNotFound          = errors.New("tool call not found")
	ErrToolCallInvalidTransition = errors.New("tool call transition is invalid")
)

type ToolCallRecord struct {
	ToolCallID      string
	RunID           string
	Status          string
	ApprovalID      string
	ModelToolCallID string
}

type ToolCallBeforeResult struct {
	Record   ToolCallRecord
	Inserted bool
}

type ToolCallBeforeEvent struct {
	SessionID   string
	TraceID     string
	Type        string
	PayloadJSON string
}

type ToolCallAfterRecord struct {
	ToolCallID      string
	RunID           string
	ServerName      string
	ToolName        string
	ModelToolCallID string
	ArgsHash        string
	WorkerID        string
	Status          string
	ResultSummary   string
	ErrorCode       string
	ErrorMessage    string
	DurationMS      int64
}

func (r *Repository) RecordToolCallBefore(ctx context.Context, record ToolCallRecord, agentID string, serverName string, toolName string, argsJSON string, argsHash string) error {
	_, err := r.RecordToolCallBeforeNew(ctx, record, agentID, serverName, toolName, argsJSON, argsHash)
	return err
}

func (r *Repository) RecordToolCallBeforeNew(ctx context.Context, record ToolCallRecord, agentID string, serverName string, toolName string, argsJSON string, argsHash string) (ToolCallBeforeResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ToolCallBeforeResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := recordToolCallBeforeTx(ctx, tx, record, agentID, serverName, toolName, argsJSON, argsHash)
	if err != nil {
		return ToolCallBeforeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ToolCallBeforeResult{}, err
	}
	return result, nil
}

func (r *Repository) RecordToolCallBeforeWithEvent(ctx context.Context, record ToolCallRecord, agentID string, serverName string, toolName string, argsJSON string, argsHash string, eventInput ToolCallBeforeEvent) (ToolCallBeforeResult, Event, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ToolCallBeforeResult{}, Event{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := recordToolCallBeforeTx(ctx, tx, record, agentID, serverName, toolName, argsJSON, argsHash)
	if err != nil {
		return ToolCallBeforeResult{}, Event{}, err
	}
	exists, err := toolCallEventExistsTx(ctx, tx, result.Record.RunID, eventInput.Type, result.Record.ToolCallID)
	if err != nil {
		return ToolCallBeforeResult{}, Event{}, err
	}
	var event Event
	if !exists {
		event, err = appendRunEventTx(ctx, tx, eventInput.SessionID, result.Record.RunID, eventInput.TraceID, eventInput.Type, eventInput.PayloadJSON, now())
		if err != nil {
			return ToolCallBeforeResult{}, Event{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ToolCallBeforeResult{}, Event{}, err
	}
	return result, event, nil
}

func recordToolCallBeforeTx(ctx context.Context, tx *sql.Tx, record ToolCallRecord, agentID string, serverName string, toolName string, argsJSON string, argsHash string) (ToolCallBeforeResult, error) {
	status := record.Status
	if status == "" {
		status = "requested"
	}
	var existingRunID, existingAgentID, existingServerName, existingToolName, existingArgsHash, existingStatus string
	var existingModelToolCallID, existingApprovalID sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT run_id, agent_id, server_name, tool_name, args_hash, model_tool_call_id, status, approval_id
		FROM tool_calls WHERE id = ?
	`, record.ToolCallID).Scan(
		&existingRunID, &existingAgentID, &existingServerName, &existingToolName,
		&existingArgsHash, &existingModelToolCallID, &existingStatus, &existingApprovalID,
	)
	if err == nil {
		if existingRunID == record.RunID && existingAgentID == agentID && existingServerName == serverName && existingToolName == toolName && existingArgsHash == argsHash && nullableStringValue(existingModelToolCallID) == record.ModelToolCallID {
			record.Status = existingStatus
			record.ApprovalID = nullableStringValue(existingApprovalID)
			return ToolCallBeforeResult{Record: record}, nil
		}
		return ToolCallBeforeResult{}, ErrToolCallConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ToolCallBeforeResult{}, err
	}
	var approvalID any
	if record.ApprovalID != "" {
		approvalID = record.ApprovalID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tool_calls (id, run_id, agent_id, server_name, tool_name, model_tool_call_id, args_json, args_hash, status, approval_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.ToolCallID, record.RunID, agentID, serverName, toolName, nullableText(record.ModelToolCallID), argsJSON, argsHash, status, approvalID, now()); err != nil {
		return ToolCallBeforeResult{}, err
	}
	record.Status = status
	return ToolCallBeforeResult{Record: record, Inserted: true}, nil
}

func toolCallEventExistsTx(ctx context.Context, tx *sql.Tx, runID string, eventType string, toolCallID string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT payload_json FROM events WHERE run_id = ? AND type = ?`, runID, eventType)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var payloadJSON string
		if err := rows.Scan(&payloadJSON); err != nil {
			return false, err
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return false, err
		}
		if recordedToolCallID, _ := payload["toolCallId"].(string); recordedToolCallID == toolCallID {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (r *Repository) RecordToolCallAfter(ctx context.Context, record ToolCallAfterRecord) (bool, error) {
	if record.Status == "" {
		record.Status = "completed"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var serverName, toolName, argsHash, currentStatus, resultSummary, errorCode, errorMessage string
	var modelToolCallID, approvalID sql.NullString
	var durationMS int64
	err = tx.QueryRowContext(ctx, `
		SELECT server_name, tool_name, args_hash, model_tool_call_id, status,
			approval_id, COALESCE(result_summary, ''), COALESCE(error_code, ''), COALESCE(error_message, ''), COALESCE(duration_ms, 0)
		FROM tool_calls
		WHERE id = ? AND run_id = ?
	`, record.ToolCallID, record.RunID).Scan(&serverName, &toolName, &argsHash, &modelToolCallID, &currentStatus, &approvalID, &resultSummary, &errorCode, &errorMessage, &durationMS)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrToolCallNotFound
	}
	if err != nil {
		return false, err
	}
	if serverName != record.ServerName || toolName != record.ToolName || nullableStringValue(modelToolCallID) != record.ModelToolCallID {
		return false, ErrToolCallInvalidTransition
	}
	if record.ArgsHash != "" && argsHash != record.ArgsHash {
		return false, ErrToolCallInvalidTransition
	}
	cleanup, err := terminalApprovalCleanupAllowed(ctx, tx, approvalID, currentStatus, record.Status)
	if err != nil {
		return false, err
	}
	if cleanup {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if !isOpenToolCallStatus(currentStatus) {
		if currentStatus == record.Status && resultSummary == record.ResultSummary && errorCode == record.ErrorCode && errorMessage == record.ErrorMessage && durationMS == record.DurationMS {
			if err := tx.Commit(); err != nil {
				return false, err
			}
			return false, nil
		}
		return false, ErrToolCallInvalidTransition
	}
	if record.Status == "completed" && !toolCallCanComplete(currentStatus) {
		return false, ErrToolCallInvalidTransition
	}
	if currentStatus == "approval_required" && record.Status == "completed" {
		if err := approvalAllowsCompletion(ctx, tx, approvalID); err != nil {
			return false, err
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE tool_calls
		SET status = ?, result_summary = ?, error_code = ?, error_message = ?, duration_ms = ?, completed_at = ?
		WHERE id = ? AND run_id = ? AND status IN ('requested', 'allowed', 'approval_required')
	`, record.Status, nullableText(record.ResultSummary), nullableText(record.ErrorCode), nullableText(record.ErrorMessage), record.DurationMS, now(), record.ToolCallID, record.RunID)
	if err != nil {
		return false, err
	}
	if err := expectOneRowErr(result, ErrToolCallInvalidTransition); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) RecordToolCallAfterWithEvent(ctx context.Context, record ToolCallAfterRecord, eventType string, payloadJSON string) (bool, Event, error) {
	if record.Status == "" {
		record.Status = "completed"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, Event{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var serverName, toolName, argsHash, currentStatus, resultSummary, errorCode, errorMessage, runStatus, sessionID, traceID, workerID string
	var toolCompletedAt, runFinishedAt string
	var modelToolCallID, approvalID sql.NullString
	var durationMS int64
	err = tx.QueryRowContext(ctx, `
		SELECT tc.server_name, tc.tool_name, tc.args_hash, tc.model_tool_call_id, tc.status,
			tc.approval_id, COALESCE(tc.result_summary, ''), COALESCE(tc.error_code, ''), COALESCE(tc.error_message, ''), COALESCE(tc.duration_ms, 0),
			ar.status, ar.session_id, ar.trace_id, COALESCE(ar.worker_id, ''),
			COALESCE(tc.completed_at, ''), COALESCE(ar.finished_at, '')
		FROM tool_calls tc
		JOIN agent_runs ar ON ar.id = tc.run_id
		WHERE tc.id = ? AND tc.run_id = ?
	`, record.ToolCallID, record.RunID).Scan(
		&serverName, &toolName, &argsHash, &modelToolCallID, &currentStatus, &approvalID,
		&resultSummary, &errorCode, &errorMessage, &durationMS,
		&runStatus, &sessionID, &traceID, &workerID, &toolCompletedAt, &runFinishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, Event{}, ErrToolCallNotFound
	}
	if err != nil {
		return false, Event{}, err
	}
	if serverName != record.ServerName || toolName != record.ToolName || nullableStringValue(modelToolCallID) != record.ModelToolCallID {
		return false, Event{}, ErrToolCallInvalidTransition
	}
	if record.ArgsHash != "" && argsHash != record.ArgsHash {
		return false, Event{}, ErrToolCallInvalidTransition
	}
	if record.WorkerID != "" && workerID != record.WorkerID {
		return false, Event{}, ErrToolCallInvalidTransition
	}
	cleanup, err := terminalApprovalCleanupAllowed(ctx, tx, approvalID, currentStatus, record.Status)
	if err != nil {
		return false, Event{}, err
	}
	if cleanup {
		if err := tx.Commit(); err != nil {
			return false, Event{}, err
		}
		return false, Event{}, nil
	}
	safeCleanup, err := terminalSafeToolCallCleanupAllowed(ctx, tx, approvalID, currentStatus, runStatus, workerID, resultSummary, toolCompletedAt, runFinishedAt, record)
	if err != nil {
		return false, Event{}, err
	}
	if safeCleanup {
		if err := tx.Commit(); err != nil {
			return false, Event{}, err
		}
		return false, Event{}, nil
	}
	if !isOpenToolCallStatus(currentStatus) {
		if currentStatus == record.Status && resultSummary == record.ResultSummary && errorCode == record.ErrorCode && errorMessage == record.ErrorMessage && durationMS == record.DurationMS {
			if err := tx.Commit(); err != nil {
				return false, Event{}, err
			}
			return false, Event{}, nil
		}
		return false, Event{}, ErrToolCallInvalidTransition
	}
	if record.Status == "completed" && !toolCallCanComplete(currentStatus) {
		return false, Event{}, ErrToolCallInvalidTransition
	}
	if runStatus != "running" && runStatus != "waiting_approval" {
		return false, Event{}, ErrToolCallInvalidTransition
	}
	if currentStatus == "approval_required" && record.Status == "completed" {
		if err := approvalAllowsCompletion(ctx, tx, approvalID); err != nil {
			return false, Event{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE tool_calls
		SET status = ?, result_summary = ?, error_code = ?, error_message = ?, duration_ms = ?, completed_at = ?
		WHERE id = ? AND run_id = ? AND status IN ('requested', 'allowed', 'approval_required')
	`, record.Status, nullableText(record.ResultSummary), nullableText(record.ErrorCode), nullableText(record.ErrorMessage), record.DurationMS, now(), record.ToolCallID, record.RunID)
	if err != nil {
		return false, Event{}, err
	}
	if err := expectOneRowErr(result, ErrToolCallInvalidTransition); err != nil {
		return false, Event{}, err
	}
	event, err := appendRunEventTx(ctx, tx, sessionID, record.RunID, traceID, eventType, payloadJSON, now())
	if err != nil {
		return false, Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return false, Event{}, err
	}
	return true, event, nil
}

func approvalAllowsCompletion(ctx context.Context, tx *sql.Tx, approvalID sql.NullString) error {
	if !approvalID.Valid {
		return ErrToolCallInvalidTransition
	}
	var approvalStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM approvals WHERE id = ?`, approvalID.String).Scan(&approvalStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrToolCallInvalidTransition
		}
		return err
	}
	if approvalStatus != "consumed" {
		return ErrToolCallInvalidTransition
	}
	return nil
}

func marshalToolLifecyclePayload(toolCallID string, serverName string, toolName string, errorMessage string) (string, error) {
	payload := map[string]any{
		"toolCallId": toolCallID,
		"toolName":   toolName,
	}
	if serverName != "" {
		payload["serverName"] = serverName
	}
	if errorMessage != "" {
		payload["error"] = errorMessage
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(payloadJSON), nil
}

func isOpenToolCallStatus(status string) bool {
	switch status {
	case "requested", "allowed", "approval_required":
		return true
	default:
		return false
	}
}

func terminalApprovalCleanupAllowed(ctx context.Context, tx *sql.Tx, approvalID sql.NullString, currentStatus string, recordStatus string) (bool, error) {
	if recordStatus != "failed" || !approvalID.Valid {
		return false, nil
	}
	var approvalStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM approvals WHERE id = ?`, approvalID.String).Scan(&approvalStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return (currentStatus == "denied" && approvalStatus == "denied") ||
		(currentStatus == "failed" && approvalStatus == "expired"), nil
}

func terminalSafeToolCallCleanupAllowed(ctx context.Context, tx *sql.Tx, approvalID sql.NullString, currentStatus string, runStatus string, workerID string, resultSummary string, toolCompletedAt string, runFinishedAt string, record ToolCallAfterRecord) (bool, error) {
	if approvalID.Valid ||
		currentStatus != "failed" ||
		record.Status != "failed" ||
		record.WorkerID == "" ||
		record.WorkerID != workerID ||
		record.ResultSummary != resultSummary ||
		toolCompletedAt == "" ||
		toolCompletedAt != runFinishedAt {
		return false, nil
	}
	switch runStatus {
	case "completed", "failed", "cancelled":
	default:
		return false, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload_json FROM events WHERE run_id = ? AND type = 'tool.call.failed'`, record.RunID)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var payloadJSON string
		if err := rows.Scan(&payloadJSON); err != nil {
			return false, err
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return false, err
		}
		if toolCallID, _ := payload["toolCallId"].(string); toolCallID == record.ToolCallID {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func toolCallCanComplete(status string) bool {
	return status == "allowed" || status == "approval_required"
}

func nullableStringValue(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
