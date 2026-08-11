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
	RunID              string
	SessionID          string
	Status             string
	TraceID            string
	AssistantMessageID string
	ExecutionActive    bool
	WorkerID           string
	ExecutionAttemptID string
	ExecutionState     string
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
	defer tx.Rollback()
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
	events := make([]Event, 0, 2)
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
	defer tx.Rollback()
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
	if err := failPendingApprovalLifecycleTx(ctx, tx, runID, code, message, finishedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'failed', finished_at = ?, error_code = ?, error_message = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, finishedAt, code, message, runID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) FailRunWithEvent(ctx context.Context, runID string, code string, message string, payloadJSON string) (Event, error) {
	finishedAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var sessionID, traceID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, runID).Scan(&sessionID, &traceID); err != nil {
		return Event{}, err
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
		WHERE id = ? AND status IN ('queued','running','waiting_approval')
	`, code, message, finishedAt, finishedAt, runID)
	if err != nil {
		return Event{}, err
	}
	if err := expectOneRowErr(result, ErrRunNotFailable); err != nil {
		return Event{}, err
	}
	if err := failPendingApprovalLifecycleTx(ctx, tx, runID, code, message, finishedAt); err != nil {
		return Event{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'failed', finished_at = ?, error_code = ?, error_message = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, finishedAt, code, message, runID); err != nil {
		return Event{}, err
	}
	event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "agent.run.failed", payloadJSON, finishedAt)
	if err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, err
	}
	return event, nil
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
	if err := failPendingApprovalLifecycleTx(ctx, tx, runID, "cancelled", reason, finishedAt); err != nil {
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
	defer tx.Rollback()
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

func (r *Repository) CancelRunWithEvent(ctx context.Context, runID string, reason string, payloadJSON string) (Event, error) {
	finishedAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var sessionID, traceID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, runID).Scan(&sessionID, &traceID); err != nil {
		return Event{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'cancelled', cancellation_reason = ?, finished_at = ? WHERE id = ? AND status IN ('queued','running','waiting_approval')`, reason, finishedAt, runID)
	if err != nil {
		return Event{}, err
	}
	if err := expectOneRowErr(result, ErrRunNotCancellable); err != nil {
		return Event{}, err
	}
	if err := failPendingApprovalLifecycleTx(ctx, tx, runID, "cancelled", reason, finishedAt); err != nil {
		return Event{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'cancelled', finished_at = ?, error_code = 'cancelled', error_message = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, finishedAt, reason, runID); err != nil {
		return Event{}, err
	}
	event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "agent.run.cancelled", payloadJSON, finishedAt)
	if err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (r *Repository) GetRun(ctx context.Context, runID string) (Run, error) {
	var run Run
	err := r.db.QueryRowContext(ctx, `
		SELECT id, session_id, status, trace_id, COALESCE(assistant_message_id, ''), execution_active,
			COALESCE(worker_id, ''), COALESCE(execution_attempt_id, ''), execution_state
		FROM agent_runs
		WHERE id = ?
	`, runID).Scan(
		&run.RunID,
		&run.SessionID,
		&run.Status,
		&run.TraceID,
		&run.AssistantMessageID,
		&run.ExecutionActive,
		&run.WorkerID,
		&run.ExecutionAttemptID,
		&run.ExecutionState,
	)
	return run, err
}

func failPendingApprovalLifecycleTx(ctx context.Context, tx *sql.Tx, runID string, code string, message string, finishedAt string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE approvals
		SET status = 'expired', decided_at = COALESCE(decided_at, ?)
		WHERE run_id = ? AND status = 'pending'
	`, finishedAt, runID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE tool_calls
		SET status = 'failed',
			error_code = ?,
			error_message = ?,
			completed_at = COALESCE(completed_at, ?)
		WHERE run_id = ?
			AND status IN ('requested', 'allowed', 'approval_required')
	`, code, message, finishedAt, runID)
	return err
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
