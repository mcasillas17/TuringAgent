package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
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

// MarkRunRunning starts a run without going through job claiming. It is the
// direct queued-to-running transition, and like every other lifecycle change
// it commits a version and appends its projection: a start nobody can observe
// is the same defect as a recovery nobody can observe.
func (r *Repository) MarkRunRunning(ctx context.Context, runID string) error {
	_, err := r.runInTransition(ctx, runTransition{
		runID:            runID,
		expectedVersion:  unresolvedStateVersion,
		transactionLocal: true,
		allowedFrom:      []string{lifecycleQueued},
		to:               lifecycleRunning,
		reason:           runoutcome.ReasonNone,
		extraSet:         `started_at = COALESCE(started_at, ?)`,
		extraArgs:        []any{transitionTime},
	}, func(ctx context.Context, tx *sql.Tx, state RunState, at string) ([]Event, error) {
		result, err := tx.ExecContext(ctx,
			`UPDATE jobs SET status = 'in_progress', picked_up_at = ? WHERE run_id = ? AND status = 'pending'`, at, runID)
		if err != nil {
			return nil, err
		}
		return nil, expectOneRow(result, "pending job not found for run")
	})
	return err
}

// RunTokenUsage is what a provider REPORTED for a run. A nil field means it
// reported nothing, which is stored as NULL and read back as unknown. Nothing
// in this package computes, estimates or defaults these.
type RunTokenUsage struct {
	InputTokens  *int64
	OutputTokens *int64
}

// nonNegativeNullInt64 turns a count into what SQLite stores. Absent becomes
// NULL, and so does a negative — there is no count below zero worth keeping,
// and NULL is the value every reader already knows means "unknown".
func nonNegativeNullInt64(value *int64) sql.NullInt64 {
	if value == nil || *value < 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
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

// failPendingApprovalLifecycleTx closes everything a terminalizing run leaves
// open: its pending approvals and its unfinished tool calls.
//
// category is an allowlisted outcome class and code an allowlisted server code.
// Neither is a sentence, and no diagnostic message is written: the columns this
// used to fill with provider and tool prose are the ones the migration had to
// scrub, so refilling them here would undo that in one release.
func failPendingApprovalLifecycleTx(ctx context.Context, tx *sql.Tx, runID string, category runoutcome.Reason, code string, finishedAt string) ([]Event, error) {
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
			error_message = NULL,
			completed_at = COALESCE(completed_at, ?)
		WHERE run_id = ?
			AND status IN ('requested', 'allowed', 'approval_required')
	`, code, finishedAt, runID); err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(approvals)+len(toolCalls))
	for _, approval := range approvals {
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
			return nil, err
		}
		event, err := appendRunEventTx(ctx, tx, sessionID, runID, traceID, "approval.expired", payloadJSON, finishedAt)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	for _, toolCall := range toolCalls {
		payloadJSON, err := marshalToolLifecyclePayload(toolCall.id, toolCall.serverName, toolCall.toolName, category)
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

// appendStepNoticeTx appends a failure-like agent.run.step projection.
//
// It takes a StepNotice rather than a sentence because these three notices —
// retrying, retrying after losing a worker, and giving up — used to be English
// prose built by string formatting and stored in the durable log. A client
// cannot localize that, an operator cannot filter on it, and the format string
// was one careless edit away from carrying a provider's words. The category and
// the two numbers say everything the sentence did.
func appendStepNoticeTx(ctx context.Context, tx *sql.Tx, sessionID string, runID string, traceID string, notice runoutcome.StepNotice, createdAt string) (Event, error) {
	if !notice.Valid() {
		return Event{}, runoutcome.ErrUnsupportedNotice
	}
	payloadJSON, err := marshalEventPayload(map[string]any{
		"category":    string(notice.Category()),
		"attempt":     notice.Attempt(),
		"maxAttempts": notice.MaxAttempts(),
	})
	if err != nil {
		return Event{}, err
	}
	return appendRunEventTx(ctx, tx, sessionID, runID, traceID, "agent.run.step", payloadJSON, createdAt)
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
	payloadJSON, err := marshalRunNoticePayload(note, extras)
	if err != nil {
		return Event{}, err
	}
	return appendRunEventTx(ctx, tx, sessionID, runID, traceID, "agent.run.step", payloadJSON, createdAt)
}

func marshalRunNoticePayload(note string, extras map[string]any) (string, error) {
	payload := make(map[string]any, len(extras)+1)
	for key, value := range extras {
		payload[key] = value
	}
	payload["note"] = note
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(payloadJSON), nil
}

func appendSessionEventTx(ctx context.Context, tx *sql.Tx, sessionID string, traceID string, eventType string, payloadJSON string, createdAt string) (Event, error) {
	return appendEventTx(ctx, tx, sessionID, sql.NullString{}, traceID, eventType, payloadJSON, createdAt)
}

func appendRunEventTx(ctx context.Context, tx *sql.Tx, sessionID string, runID string, traceID string, eventType string, payloadJSON string, createdAt string) (Event, error) {
	return appendEventTx(ctx, tx, sessionID, sql.NullString{String: runID, Valid: true}, traceID, eventType, payloadJSON, createdAt)
}

func appendEventTx(ctx context.Context, tx *sql.Tx, sessionID string, runID sql.NullString, traceID string, eventType string, payloadJSON string, createdAt string) (Event, error) {
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
		RunID:       runID,
		TraceID:     traceID,
		Sequence:    sequence,
		Type:        eventType,
		PayloadJSON: payloadJSON,
		CreatedAt:   createdAt,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events (id, session_id, run_id, trace_id, sequence, type, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, event.EventID, event.SessionID, nullableString(runID), event.TraceID, event.Sequence, event.Type, event.PayloadJSON, event.CreatedAt); err != nil {
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
	// Recovering is deliberately absent: the generic ingest path is a worker
	// narrating a run it claims to own, and recovering is precisely the state
	// where that claim cannot be proven. Whatever a fenced run still has to
	// record travels through the guarded recovery, terminal, and approval
	// paths, which establish their own identity before they write.
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

// AppendPendingRunNotice appends a queue notice only if the run is still
// queued and its job is still pending at the write boundary.
func (r *Repository) AppendPendingRunNotice(ctx context.Context, runID string, note string, extras map[string]any) (Event, bool, error) {
	payloadJSON, err := marshalRunNoticePayload(note, extras)
	if err != nil {
		return Event{}, false, err
	}
	eventID := ids.New("evt")
	createdAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO events (id, session_id, run_id, trace_id, sequence, type, payload_json, created_at)
		SELECT
			?,
			runs.session_id,
			runs.id,
			runs.trace_id,
			(SELECT COALESCE(MAX(events.sequence), 0) + 1
			 FROM events
			 WHERE events.session_id = runs.session_id),
			'agent.run.step',
			?,
			?
		FROM agent_runs AS runs
		WHERE runs.id = ?
		  AND runs.status = 'queued'
		  AND EXISTS (
			SELECT 1
			FROM jobs
			WHERE jobs.run_id = runs.id
			  AND jobs.status = 'pending'
		  )
	`, eventID, payloadJSON, createdAt, runID)
	if err != nil {
		return Event{}, false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Event{}, false, err
	}
	if rowsAffected == 0 {
		return Event{}, false, nil
	}
	var event Event
	if err := tx.QueryRowContext(ctx, `
		SELECT id, session_id, run_id, trace_id, sequence, type, payload_json, created_at
		FROM events
		WHERE id = ?
	`, eventID).Scan(
		&event.EventID,
		&event.SessionID,
		&event.RunID,
		&event.TraceID,
		&event.Sequence,
		&event.Type,
		&event.PayloadJSON,
		&event.CreatedAt,
	); err != nil {
		return Event{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, false, err
	}
	return event, true, nil
}
