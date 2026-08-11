package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

type EnqueueUserMessageInput struct {
	SessionID     string
	Content       string
	AgentID       string
	ModelProvider string
	Model         string
}

type EnqueueUserMessageResult struct {
	SessionID          string
	UserMessageID      string
	AssistantMessageID string
	RunID              string
	JobID              string
	TraceID            string
	Status             string
	QueuedEvent        Event
}

type Job struct {
	JobID               string
	RunID               string
	SessionID           string
	UserMessageID       string
	AssistantMessageID  string
	TraceID             string
	ModelProvider       string
	Model               string
	UserText            string
	Attempt             int
	AssignmentAttemptID string
	StartedEvent        Event
}

type Assignment struct {
	JobID     string
	RunID     string
	WorkerID  string
	AttemptID string
}

var ErrAssignmentFenced = errors.New("assignment attempt is fenced")

func (r *Repository) EnqueueUserMessage(ctx context.Context, input EnqueueUserMessageInput) (EnqueueUserMessageResult, error) {
	createdAt := now()
	userMessageID := ids.New("msg")
	assistantMessageID := ids.New("msg")
	runID := ids.New("run")
	jobID := ids.New("job")
	traceID := ids.New("trace")
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE session_id = ?`, input.SessionID).Scan(&next); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at) VALUES (?, ?, 'user', ?, 'text', ?, ?)`, userMessageID, input.SessionID, input.Content, next, createdAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at) VALUES (?, ?, ?, 'assistant', '', 'text', ?, ?)`, assistantMessageID, input.SessionID, runID, next+1, createdAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_runs (id, session_id, user_message_id, assistant_message_id, agent_id, trace_id, status, model_provider, model_name, created_at) VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?)`, runID, input.SessionID, userMessageID, assistantMessageID, input.AgentID, traceID, input.ModelProvider, input.Model, createdAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	jobPayload, err := json.Marshal(map[string]any{
		"userText":           input.Content,
		"sessionId":          input.SessionID,
		"userMessageId":      userMessageID,
		"assistantMessageId": assistantMessageID,
		"traceId":            traceID,
		"modelProvider":      input.ModelProvider,
		"model":              input.Model,
	})
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs (id, run_id, agent_id, status, payload_json, created_at) VALUES (?, ?, ?, 'pending', ?, ?)`, jobID, runID, input.AgentID, string(jobPayload), createdAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	queuedPayload, err := json.Marshal(map[string]any{
		"runId":   runID,
		"jobId":   jobID,
		"status":  "queued",
		"agentId": input.AgentID,
	})
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	queuedEvent, err := appendRunEventTx(ctx, tx, input.SessionID, runID, traceID, "agent.run.queued", string(queuedPayload), createdAt)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	return EnqueueUserMessageResult{SessionID: input.SessionID, UserMessageID: userMessageID, AssistantMessageID: assistantMessageID, RunID: runID, JobID: jobID, TraceID: traceID, Status: "queued", QueuedEvent: queuedEvent}, nil
}

func (r *Repository) ClaimNextJob(ctx context.Context, agentID string, leaseOwner string) (Job, error) {
	return r.ClaimNextJobWithLimit(ctx, agentID, leaseOwner, 0, 5*time.Minute)
}

func (r *Repository) ClaimNextJobWithLimit(ctx context.Context, agentID string, leaseOwner string, globalLimit int, leaseDuration time.Duration) (Job, error) {
	pickedUpAt := now()
	if leaseDuration <= 0 {
		leaseDuration = 5 * time.Minute
	}
	leaseExpires := time.Now().UTC().Add(leaseDuration)
	leaseExpiresAt := FormatTimestamp(leaseExpires)
	leaseExpiresAtNanos := leaseExpires.UnixNano()
	assignmentAttemptID := ids.New("attempt")
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	if globalLimit > 0 {
		var active int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM agent_runs
			WHERE agent_id = ?
				AND execution_active = 1
		`, agentID).Scan(&active); err != nil {
			return Job{}, err
		}
		if active >= globalLimit {
			return Job{}, nil
		}
	}
	var job Job
	var payloadJSON string
	claimQuery := `
		SELECT
			j.id,
			j.run_id,
			r.session_id,
			r.user_message_id,
			COALESCE(r.assistant_message_id, ''),
			r.trace_id,
			r.model_provider,
			r.model_name,
			j.payload_json,
			j.attempt
		FROM jobs j
		JOIN agent_runs r ON r.id = j.run_id
		WHERE j.agent_id = ? AND j.status = 'pending' AND r.status = 'queued'
			AND NOT EXISTS (
				SELECT 1
				FROM agent_runs earlier
				JOIN messages earlier_assistant ON earlier_assistant.id = earlier.assistant_message_id
				JOIN messages candidate_assistant ON candidate_assistant.id = r.assistant_message_id
				WHERE earlier.session_id = r.session_id
					AND earlier_assistant.sequence < candidate_assistant.sequence
					AND (
						earlier.execution_active = 1
						OR
						earlier.status NOT IN ('completed', 'failed', 'cancelled')
						OR EXISTS (
							SELECT 1
							FROM jobs earlier_job
							WHERE earlier_job.run_id = earlier.id
								AND earlier_job.status NOT IN ('completed', 'failed', 'cancelled')
						)
					)
			)
		ORDER BY ` + sqliteTimestampNanos("j.created_at") + `, j.id
		LIMIT 1
	`
	err = tx.QueryRowContext(ctx, claimQuery, agentID).Scan(
		&job.JobID,
		&job.RunID,
		&job.SessionID,
		&job.UserMessageID,
		&job.AssistantMessageID,
		&job.TraceID,
		&job.ModelProvider,
		&job.Model,
		&payloadJSON,
		&job.Attempt,
	)
	if err == sql.ErrNoRows {
		return Job{}, nil
	}
	if err != nil {
		return Job{}, err
	}
	var payload struct {
		UserText string `json:"userText"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return Job{}, err
	}
	job.UserText = payload.UserText
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'in_progress', lease_owner = ?, lease_expires_at = ?, lease_expires_at_ns = ?, picked_up_at = ?, assignment_attempt_id = ?
		WHERE id = ? AND status = 'pending'
	`, leaseOwner, leaseExpiresAt, leaseExpiresAtNanos, pickedUpAt, assignmentAttemptID, job.JobID)
	if err != nil {
		return Job{}, err
	}
	if err := expectOneRow(result, "pending job not found for claim"); err != nil {
		return Job{}, err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET status = 'running',
			started_at = COALESCE(started_at, ?),
			worker_id = ?,
			execution_active = 1,
			execution_exit_acknowledged_at = NULL,
			execution_attempt_id = ?,
			execution_state = 'pending_send',
			execution_lease_expires_at = ?,
			execution_lease_expires_at_ns = ?
		WHERE id = ? AND status = 'queued'
	`, pickedUpAt, leaseOwner, assignmentAttemptID, leaseExpiresAt, leaseExpiresAtNanos, job.RunID)
	if err != nil {
		return Job{}, err
	}
	if err := expectOneRow(result, "run is not queued"); err != nil {
		return Job{}, err
	}
	startedPayload, err := json.Marshal(map[string]any{
		"runId":               job.RunID,
		"jobId":               job.JobID,
		"status":              "running",
		"agentId":             agentID,
		"attempt":             job.Attempt,
		"assignmentAttemptId": assignmentAttemptID,
	})
	if err != nil {
		return Job{}, err
	}
	startedEvent, err := appendRunEventTx(ctx, tx, job.SessionID, job.RunID, job.TraceID, "agent.run.started", string(startedPayload), pickedUpAt)
	if err != nil {
		return Job{}, err
	}
	job.AssignmentAttemptID = assignmentAttemptID
	job.StartedEvent = startedEvent
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (r *Repository) RequeueClaimedJob(ctx context.Context, jobID string, runID string) error {
	return r.requeueAssignment(ctx, Assignment{JobID: jobID, RunID: runID}, false)
}

func (r *Repository) AbortPendingAssignment(ctx context.Context, assignment Assignment) error {
	return r.requeueAssignment(ctx, assignment, true)
}

func (r *Repository) requeueAssignment(ctx context.Context, assignment Assignment, onlyPendingSend bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var attemptID, executionState string
	var workerID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(execution_attempt_id, ''), execution_state, worker_id
		FROM agent_runs
		WHERE id = ?
	`, assignment.RunID).Scan(&attemptID, &executionState, &workerID)
	if err != nil {
		return err
	}
	if assignment.AttemptID != "" && attemptID != assignment.AttemptID {
		return ErrAssignmentFenced
	}
	if assignment.WorkerID != "" && (!workerID.Valid || workerID.String != assignment.WorkerID) {
		return ErrAssignmentFenced
	}
	if onlyPendingSend && executionState != "pending_send" {
		return ErrAssignmentFenced
	}
	if executionState == "sending" || executionState == "uncertain" {
		return ErrAssignmentFenced
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'pending',
			lease_owner = NULL,
			lease_expires_at = NULL,
			lease_expires_at_ns = NULL,
			picked_up_at = NULL,
			assignment_attempt_id = NULL,
			attempt = attempt + 1
		WHERE id = ? AND run_id = ? AND status = 'in_progress'
	`, assignment.JobID, assignment.RunID)
	if err != nil {
		return err
	}
	if err := expectOneRow(result, "claimed job not found for requeue"); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET status = 'queued',
			started_at = NULL,
			worker_id = NULL,
			execution_active = 0,
			execution_exit_acknowledged_at = NULL,
			execution_attempt_id = NULL,
			execution_state = 'none',
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL
		WHERE id = ? AND status = 'running'
	`, assignment.RunID)
	if err != nil {
		return err
	}
	if err := expectOneRow(result, "running run not found for requeue"); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) BeginAssignmentSend(ctx context.Context, assignment Assignment) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_state = 'sending'
		WHERE id = ?
			AND worker_id = ?
			AND execution_attempt_id = ?
			AND execution_active = 1
			AND execution_state = 'pending_send'
	`, assignment.RunID, assignment.WorkerID, assignment.AttemptID)
	if err != nil {
		return err
	}
	return expectOneRowErr(result, ErrAssignmentFenced)
}

func (r *Repository) MarkAssignmentDelivered(ctx context.Context, assignment Assignment) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_state = 'delivered'
		WHERE id = ?
			AND worker_id = ?
			AND execution_attempt_id = ?
			AND execution_active = 1
			AND execution_state = 'sending'
	`, assignment.RunID, assignment.WorkerID, assignment.AttemptID)
	if err != nil {
		return err
	}
	return expectOneRowErr(result, ErrAssignmentFenced)
}

func (r *Repository) MarkAssignmentDeliveryUncertain(ctx context.Context, assignment Assignment) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_state = 'uncertain'
		WHERE id = ?
			AND worker_id = ?
			AND execution_attempt_id = ?
			AND execution_active = 1
			AND execution_state IN ('sending', 'delivered')
	`, assignment.RunID, assignment.WorkerID, assignment.AttemptID)
	if err != nil {
		return err
	}
	return expectOneRowErr(result, ErrAssignmentFenced)
}
