package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	// ErrRunNotCompletable indicates a run is no longer in a status that can complete.
	ErrRunNotCompletable = errors.New("run is not completable")
	// ErrRunNotFailable indicates a run is no longer in a status that can fail.
	ErrRunNotFailable = errors.New("run is not failable")
	// ErrRunNotCancellable indicates a run is no longer in a status that can be cancelled.
	ErrRunNotCancellable = errors.New("run is not cancellable")
	// ErrRunNotActive indicates a runtime event arrived for a non-active run.
	ErrRunNotActive = errors.New("run is not active")
)

type Run struct {
	RunID                string
	SessionID            string
	Status               string
	TraceID              string
	AssistantMessageID   string
	AssistantContent     string
	TerminalEventType    string
	TerminalEventPayload string
	ExecutionActive      bool
	WorkerID             string
	ExecutionAttemptID   string
	ExecutionState       string
}

func (r *Repository) MarkRunRunning(ctx context.Context, runID string) error {
	startedAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'running', started_at = ? WHERE id = ? AND status = 'queued'`, startedAt, runID)
	if err != nil {
		return err
	}
	if err := expectOneRow(result, "run is not queued"); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `UPDATE jobs SET status = 'in_progress', picked_up_at = ? WHERE run_id = ? AND status = 'pending'`, startedAt, runID)
	if err != nil {
		return err
	}
	if err := expectOneRow(result, "pending job not found for run"); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) CompleteRun(ctx context.Context, runID string, assistantMessageID string, content string) error {
	finishedAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET status = 'completed',
			finished_at = ?,
			execution_active = 0,
			execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
			execution_state = 'exited',
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL
		WHERE id = ? AND status IN ('running','waiting_approval')
	`, finishedAt, finishedAt, runID)
	if err != nil {
		return err
	}
	if err := expectOneRowErr(result, ErrRunNotCompletable); err != nil {
		return err
	}
	if assistantMessageID != "" {
		result, err = tx.ExecContext(ctx, `UPDATE messages SET content = ? WHERE id = ? AND run_id = ? AND role = 'assistant'`, content, assistantMessageID, runID)
		if err != nil {
			return err
		}
		if err := expectOneRow(result, "assistant message not found"); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'completed', finished_at = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, finishedAt, runID); err != nil {
		return err
	}
	if _, err := failPendingApprovalLifecycleTx(ctx, tx, runID, "run_completed", "Run completed before tool call finished", finishedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) CompleteRunWithEvent(ctx context.Context, runID string, assistantMessageID string, content string, payloadJSON string) ([]Event, error) {
	finishedAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var sessionID, traceID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, runID).Scan(&sessionID, &traceID); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET status = 'completed',
			finished_at = ?,
			execution_active = 0,
			execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
			execution_state = 'exited',
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL
		WHERE id = ? AND status IN ('running','waiting_approval')
	`, finishedAt, finishedAt, runID)
	if err != nil {
		return nil, err
	}
	if err := expectOneRowErr(result, ErrRunNotCompletable); err != nil {
		return nil, err
	}
	if assistantMessageID != "" {
		result, err = tx.ExecContext(ctx, `UPDATE messages SET content = ? WHERE id = ? AND run_id = ? AND role = 'assistant'`, content, assistantMessageID, runID)
		if err != nil {
			return nil, err
		}
		if err := expectOneRow(result, "assistant message not found"); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'completed', finished_at = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, finishedAt, runID); err != nil {
		return nil, err
	}
	events, err := failPendingApprovalLifecycleTx(ctx, tx, runID, "run_completed", "Run completed before tool call finished", finishedAt)
	if err != nil {
		return nil, err
	}
	if assistantMessageID != "" && content != "" {
		messagePayload, err := json.Marshal(map[string]any{
			"messageId": assistantMessageID,
			"content":   content,
		})
		if err != nil {
			return nil, err
		}
		messageEvent, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "message.completed", string(messagePayload), finishedAt)
		if err != nil {
			return nil, err
		}
		events = append(events, messageEvent)
	}
	event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "agent.run.completed", payloadJSON, finishedAt)
	if err != nil {
		return nil, err
	}
	events = append(events, event)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *Repository) FailRun(ctx context.Context, runID string, code string, message string) error {
	finishedAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
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
		WHERE id = ? AND status IN ('queued','running','waiting_approval')
	`, code, message, finishedAt, finishedAt, runID)
	if err != nil {
		return err
	}
	if err := expectOneRowErr(result, ErrRunNotFailable); err != nil {
		return err
	}
	if _, err := failPendingApprovalLifecycleTx(ctx, tx, runID, code, message, finishedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'failed', finished_at = ?, error_code = ?, error_message = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, finishedAt, code, message, runID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) FailRunWithEvent(ctx context.Context, runID string, code string, message string, payloadJSON string) ([]Event, error) {
	return r.failRunWithEvent(ctx, runID, code, message, payloadJSON, false)
}

func (r *Repository) FailRunWithEventPreservingExecution(ctx context.Context, runID string, code string, message string, payloadJSON string) ([]Event, error) {
	return r.failRunWithEvent(ctx, runID, code, message, payloadJSON, true)
}

func (r *Repository) failRunWithEvent(ctx context.Context, runID string, code string, message string, payloadJSON string, preserveExecution bool) ([]Event, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	events, err := failRunWithEventTx(ctx, tx, runID, code, message, payloadJSON, preserveExecution)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

// failRunWithEventTx fails a run and its job inside an existing transaction,
// revoking any pending approvals/tool calls and appending the terminal
// agent.run.failed event. It is shared by the direct failure path and the
// retry-exhaustion path so both terminalize a run identically.
func failRunWithEventTx(ctx context.Context, tx *sql.Tx, runID string, code string, message string, payloadJSON string, preserveExecution bool) ([]Event, error) {
	finishedAt := now()
	var sessionID, traceID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, runID).Scan(&sessionID, &traceID); err != nil {
		return nil, err
	}
	var result sql.Result
	var err error
	if preserveExecution {
		result, err = tx.ExecContext(ctx, `
			UPDATE agent_runs
			SET status = 'failed',
				error_code = ?,
				error_message = ?,
				finished_at = ?,
				execution_exit_acknowledged_at = CASE
					WHEN execution_active = 1 THEN execution_exit_acknowledged_at
					ELSE COALESCE(execution_exit_acknowledged_at, ?)
				END,
				execution_state = CASE WHEN execution_active = 1 THEN 'uncertain' ELSE 'exited' END,
				execution_lease_expires_at = CASE WHEN execution_active = 1 THEN execution_lease_expires_at ELSE NULL END,
				execution_lease_expires_at_ns = CASE WHEN execution_active = 1 THEN execution_lease_expires_at_ns ELSE NULL END
			WHERE id = ? AND status IN ('queued','running','waiting_approval')
		`, code, message, finishedAt, finishedAt, runID)
	} else {
		result, err = tx.ExecContext(ctx, `
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
			WHERE id = ? AND status IN ('queued','running','waiting_approval')
		`, code, message, finishedAt, finishedAt, runID)
	}
	if err != nil {
		return nil, err
	}
	if err := expectOneRowErr(result, ErrRunNotFailable); err != nil {
		return nil, err
	}
	events, err := failPendingApprovalLifecycleTx(ctx, tx, runID, code, message, finishedAt)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'failed', finished_at = ?, error_code = ?, error_message = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, finishedAt, code, message, runID); err != nil {
		return nil, err
	}
	event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "agent.run.failed", payloadJSON, finishedAt)
	if err != nil {
		return nil, err
	}
	events = append(events, event)
	return events, nil
}

func (r *Repository) CancelRun(ctx context.Context, runID string, reason string) error {
	finishedAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'cancelled', cancellation_reason = ?, finished_at = ? WHERE id = ? AND status IN ('queued','running','waiting_approval')`, reason, finishedAt, runID)
	if err != nil {
		return err
	}
	if err := expectOneRowErr(result, ErrRunNotCancellable); err != nil {
		return err
	}
	if _, err := failPendingApprovalLifecycleTx(ctx, tx, runID, "run_cancelled", reason, finishedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'cancelled', finished_at = ?, error_code = 'cancelled', error_message = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, finishedAt, reason, runID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) AcknowledgeExecutionExit(ctx context.Context, runID string) error {
	acknowledgedAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_active = 0,
			execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
			execution_state = 'exited',
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL
		WHERE id = ? AND status IN ('completed', 'failed', 'cancelled') AND execution_active = 1
	`, acknowledgedAt, runID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM agent_runs WHERE id = ?`, runID).Scan(&status); err != nil {
			return err
		}
		if status != "completed" && status != "failed" && status != "cancelled" {
			return ErrRunNotActive
		}
	}
	return tx.Commit()
}

func (r *Repository) CancelRunWithEvent(ctx context.Context, runID string, reason string, payloadJSON string) ([]Event, error) {
	finishedAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var sessionID, traceID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, runID).Scan(&sessionID, &traceID); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'cancelled', cancellation_reason = ?, finished_at = ? WHERE id = ? AND status IN ('queued','running','waiting_approval')`, reason, finishedAt, runID)
	if err != nil {
		return nil, err
	}
	if err := expectOneRowErr(result, ErrRunNotCancellable); err != nil {
		return nil, err
	}
	events, err := failPendingApprovalLifecycleTx(ctx, tx, runID, "run_cancelled", reason, finishedAt)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'cancelled', finished_at = ?, error_code = 'cancelled', error_message = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, finishedAt, reason, runID); err != nil {
		return nil, err
	}
	event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "agent.run.cancelled", payloadJSON, finishedAt)
	if err != nil {
		return nil, err
	}
	events = append(events, event)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *Repository) GetRun(ctx context.Context, runID string) (Run, error) {
	var run Run
	err := r.db.QueryRowContext(ctx, `
		SELECT r.id, r.session_id, r.status, r.trace_id, COALESCE(r.assistant_message_id, ''),
			COALESCE(m.content, ''),
			COALESCE((
				SELECT e.type
				FROM events e
				WHERE e.run_id = r.id
					AND e.type IN ('agent.run.completed', 'agent.run.failed', 'agent.run.cancelled')
				ORDER BY e.sequence DESC
				LIMIT 1
			), ''),
			COALESCE((
				SELECT e.payload_json
				FROM events e
				WHERE e.run_id = r.id
					AND e.type IN ('agent.run.completed', 'agent.run.failed', 'agent.run.cancelled')
				ORDER BY e.sequence DESC
				LIMIT 1
			), ''),
			r.execution_active, COALESCE(r.worker_id, ''), COALESCE(r.execution_attempt_id, ''), r.execution_state
		FROM agent_runs r
		LEFT JOIN messages m ON m.id = r.assistant_message_id
		WHERE r.id = ?
	`, runID).Scan(
		&run.RunID,
		&run.SessionID,
		&run.Status,
		&run.TraceID,
		&run.AssistantMessageID,
		&run.AssistantContent,
		&run.TerminalEventType,
		&run.TerminalEventPayload,
		&run.ExecutionActive,
		&run.WorkerID,
		&run.ExecutionAttemptID,
		&run.ExecutionState,
	)
	return run, err
}

func failPendingApprovalLifecycleTx(ctx context.Context, tx *sql.Tx, runID string, code string, message string, finishedAt string) ([]Event, error) {
	var sessionID, traceID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, runID).Scan(&sessionID, &traceID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT a.id, COALESCE(a.tool_call_id, ''), a.tool_name, COALESCE(tc.model_tool_call_id, '')
		FROM approvals a
		LEFT JOIN tool_calls tc ON tc.id = a.tool_call_id AND tc.run_id = a.run_id
		WHERE a.run_id = ? AND a.status IN ('pending', 'approved')
		ORDER BY `+sqliteTimestampNanos("a.created_at")+`, a.id
	`, runID)
	if err != nil {
		return nil, err
	}
	type approvalToRevoke struct {
		id, toolCallID, toolName, modelToolCallID string
	}
	var approvals []approvalToRevoke
	for rows.Next() {
		var approval approvalToRevoke
		if err := rows.Scan(&approval.id, &approval.toolCallID, &approval.toolName, &approval.modelToolCallID); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		approvals = append(approvals, approval)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	toolRows, err := tx.QueryContext(ctx, `
		SELECT id, server_name, tool_name
		FROM tool_calls
		WHERE run_id = ? AND status IN ('requested', 'allowed', 'approval_required')
		ORDER BY `+sqliteTimestampNanos("created_at")+`, id
	`, runID)
	if err != nil {
		return nil, err
	}
	type toolCallToFail struct {
		id, serverName, toolName string
	}
	var toolCalls []toolCallToFail
	for toolRows.Next() {
		var toolCall toolCallToFail
		if err := toolRows.Scan(&toolCall.id, &toolCall.serverName, &toolCall.toolName); err != nil {
			return nil, errors.Join(err, toolRows.Close())
		}
		toolCalls = append(toolCalls, toolCall)
	}
	if err := toolRows.Err(); err != nil {
		return nil, errors.Join(err, toolRows.Close())
	}
	if err := toolRows.Close(); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE approvals
		SET status = 'expired', approval_jti = NULL, approval_token = NULL,
			decided_at = COALESCE(decided_at, ?)
		WHERE run_id = ? AND status IN ('pending', 'approved')
	`, finishedAt, runID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tool_calls
		SET status = 'failed',
			error_code = ?,
			error_message = ?,
			completed_at = COALESCE(completed_at, ?)
		WHERE run_id = ?
			AND status IN ('requested', 'allowed', 'approval_required')
	`, code, message, finishedAt, runID); err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(approvals)+len(toolCalls))
	for _, approval := range approvals {
		payload := map[string]any{
			"approvalId": approval.id,
			"toolName":   approval.toolName,
			"reason":     code,
		}
		if approval.toolCallID != "" {
			payload["toolCallId"] = approval.toolCallID
		}
		if approval.modelToolCallID != "" {
			payload["modelToolCallId"] = approval.modelToolCallID
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "approval.expired", string(payloadJSON), finishedAt)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	for _, toolCall := range toolCalls {
		payloadJSON, err := marshalToolLifecyclePayload(toolCall.id, toolCall.serverName, toolCall.toolName, message)
		if err != nil {
			return nil, err
		}
		event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "tool.call.failed", payloadJSON, finishedAt)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

// appendRunNoticeTx appends a user-facing agent.run.step notice. The client
// renders this event type generically, reading ONLY the note — so note is a
// required parameter rather than a map key that could be forgotten, and it is
// assigned last so no extras key can shadow it.
//
// extras exist purely for operators reading the event log. Note the deliberate
// key split: retry notices carry "attempt" (the one about to start), give-up
// notices carry "attempts" (the number spent).
//
// createdAt is passed in rather than taken here so a notice shares the clock
// reading of the terminal event it accompanies — otherwise the explanation is
// stamped after the failure it explains.
func appendRunNoticeTx(ctx context.Context, tx *sql.Tx, sessionID string, runID string, traceID string, note string, extras map[string]any, createdAt string) (Event, error) {
	payload := make(map[string]any, len(extras)+1)
	for key, value := range extras {
		payload[key] = value
	}
	payload["note"] = note
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	return appendRunEventTx(ctx, tx, sessionID, runID, traceID, "agent.run.step", string(payloadJSON), createdAt)
}

func appendRunEventTx(ctx context.Context, tx *sql.Tx, sessionID string, runID string, traceID string, eventType string, payloadJSON string, createdAt string) (Event, error) {
	if payloadJSON == "" {
		payloadJSON = "{}"
	}
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM events WHERE session_id = ?`, sessionID).Scan(&sequence); err != nil {
		return Event{}, err
	}
	event := Event{
		EventID:     ids.New("evt"),
		SessionID:   sessionID,
		RunID:       sql.NullString{String: runID, Valid: true},
		TraceID:     traceID,
		Sequence:    sequence,
		Type:        eventType,
		PayloadJSON: payloadJSON,
		CreatedAt:   createdAt,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events (id, session_id, run_id, trace_id, sequence, type, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, event.EventID, event.SessionID, runID, event.TraceID, event.Sequence, event.Type, event.PayloadJSON, event.CreatedAt); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (r *Repository) AppendRuntimeEvent(ctx context.Context, event *turingv1.TuringEvent) (Event, error) {
	if event == nil {
		return Event{}, errors.New("runtime event is required")
	}
	if event.RunId == "" {
		return Event{}, errors.New("runtime event run_id is required")
	}
	payloadJSON := "{}"
	if event.Payload != nil {
		payload, err := protojson.Marshal(event.Payload)
		if err != nil {
			return Event{}, err
		}
		payloadJSON = string(payload)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status = status WHERE id = ? AND status IN ('running','waiting_approval')`, event.RunId)
	if err != nil {
		return Event{}, err
	}
	if err := expectOneRowErr(result, ErrRunNotActive); err != nil {
		return Event{}, err
	}
	var sessionID, traceID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, event.RunId).Scan(&sessionID, &traceID); err != nil {
		return Event{}, err
	}
	appended, err := appendRunEventTx(ctx, tx, sessionID, event.RunId, traceID, runtimeEventType(event.Type), payloadJSON, now())
	if err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, err
	}
	return appended, nil
}

func runtimeEventType(value turingv1.TuringEventType) string {
	normalized := strings.ToLower(strings.TrimPrefix(value.String(), "TURING_EVENT_TYPE_"))
	return strings.ReplaceAll(normalized, "_", ".")
}

// AppendRunNotice publishes a user-facing note against a live run, outside any
// transaction of the caller's own.
//
// appendRunNoticeTx exists for notices that must land with the event they
// explain. This one is for a notice about something that has already happened
// and is separately durable — an approval the orchestrator granted on an
// automation's behalf, for instance.
func (r *Repository) AppendRunNotice(ctx context.Context, runID string, note string, extras map[string]any) (Event, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var sessionID, traceID string
	if err := tx.QueryRowContext(ctx,
		`SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, runID).Scan(&sessionID, &traceID); err != nil {
		return Event{}, err
	}
	event, err := appendRunNoticeTx(ctx, tx, sessionID, runID, traceID, note, extras, now())
	if err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, err
	}
	return event, nil
}
