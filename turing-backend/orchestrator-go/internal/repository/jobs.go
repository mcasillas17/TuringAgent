package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

type EnqueueUserMessageInput struct {
	SessionID                      string
	Content                        string
	AgentID                        string
	ModelProvider                  string
	Model                          string
	RequestedTools                 []string
	RequiredContextTokens          int
	MinimumWorkerMaxConcurrentRuns int
	ValidateRouting                func(context.Context, RoutingRequirements) error
}

type RoutingRequirements struct {
	AgentID                        string
	ModelProvider                  string
	Model                          string
	RequestedTools                 []string
	RequiredContextTokens          int
	MinimumWorkerMaxConcurrentRuns int
	ExternalAgent                  bool
}

type RoutingModelCapability struct {
	Provider         string
	Model            string
	MaxContextTokens int
}

type WorkerRoutingCapabilities struct {
	Models                 []RoutingModelCapability
	Tools                  []string
	MaxConcurrentRuns      int
	SupportsExternalAgents bool
}

type PendingRoutingWork struct {
	RunID        string
	Requirements RoutingRequirements
}

type EnqueueUserMessageResult struct {
	SessionID           string
	UserMessageID       string
	AssistantMessageID  string
	RunID               string
	JobID               string
	TraceID             string
	Status              string
	SessionUpdatedEvent Event
	QueuedEvent         Event
	// RoutingEvents carries the notices written in the same transaction as the
	// run — today, the one saying this message is leaving the machine. They are
	// returned rather than published here because the repository does not own
	// the event bus, and a caller that forgot to publish them would leave a
	// subscriber with no record of the egress.
	RoutingEvents []Event
}

type Job struct {
	JobID                          string
	RunID                          string
	SessionID                      string
	UserMessageID                  string
	AssistantMessageID             string
	AgentID                        string
	TraceID                        string
	ModelProvider                  string
	Model                          string
	UserText                       string
	RequestedTools                 []string
	RequiredContextTokens          int
	MinimumWorkerMaxConcurrentRuns int
	Attempt                        int
	AssignmentAttemptID            string
	Skills                         []AttachedSkill
	// ExternalAgent is nil for the local assistant, which is the default and
	// the common case.
	ExternalAgent *ExternalAgentTarget
	StartedEvent  Event
}

type queuedJobPayload struct {
	UserText                       string               `json:"userText"`
	RequestedTools                 []string             `json:"requestedTools"`
	RequiredContextTokens          int                  `json:"requiredContextTokens"`
	MinimumWorkerMaxConcurrentRuns int                  `json:"minimumWorkerMaxConcurrentRuns"`
	Skills                         []AttachedSkill      `json:"skills"`
	ExternalAgent                  *ExternalAgentTarget `json:"externalAgent"`
}

type Assignment struct {
	JobID     string
	RunID     string
	WorkerID  string
	AttemptID string
}

var ErrAssignmentFenced = errors.New("assignment attempt is fenced")

// RetriesExhaustedCode marks a run that was terminally failed only after its
// retry budget was spent. It lets an operator distinguish "gave up after
// retries" from a run that failed on its first attempt.
const RetriesExhaustedCode = "retries_exhausted"

// RetryDecision reports how a retryable run failure was resolved: either the
// job was requeued for another attempt, or (once attempts were exhausted) the
// run was terminally failed and the terminal lifecycle events were emitted.
type RetryDecision struct {
	Requeued bool
	Events   []Event
}

// RequeueOrFailRetryableRun handles a retryable run failure (for example a
// worker rejecting an assignment because it was busy). While the job still has
// attempts left it is requeued for another worker to claim; once the attempt
// budget is spent — or the run is no longer in a requeueable state — the run is
// terminally failed with a distinguishable code so the message does not bounce
// forever. maxAttempts caps the total number of attempts.
func (r *Repository) RequeueOrFailRetryableRun(ctx context.Context, runID string, code string, message string, maxAttempts int) (RetryDecision, error) {
	if maxAttempts <= 0 {
		maxAttempts = defaultAssignmentMaxAttempts
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RetryDecision{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var runStatus, sessionID, traceID string
	// session_id and trace_id are selected here rather than only in
	// failRunWithEventTx because both branches below now append a notice event,
	// and appendRunEventTx needs them.
	if err := tx.QueryRowContext(ctx, `SELECT status, session_id, trace_id FROM agent_runs WHERE id = ?`, runID).Scan(&runStatus, &sessionID, &traceID); err != nil {
		return RetryDecision{}, err
	}
	var attempt int
	jobErr := tx.QueryRowContext(ctx, `SELECT attempt FROM jobs WHERE run_id = ? AND status = 'in_progress'`, runID).Scan(&attempt)
	if jobErr != nil && !errors.Is(jobErr, sql.ErrNoRows) {
		return RetryDecision{}, jobErr
	}
	requeueable := runStatus == "running" && jobErr == nil

	if requeueable && attempt < maxAttempts {
		if err := requeueRunForRetryTx(ctx, tx, runID); err != nil {
			return RetryDecision{}, err
		}
		// attempt is the one that just failed, so the attempt the user is about to
		// wait through is attempt+1 — matching the counter requeueRunForRetryTx
		// has just incremented.
		notice, err := appendRunNoticeTx(ctx, tx, sessionID, runID, traceID,
			fmt.Sprintf("Retrying (attempt %d of %d)", attempt+1, maxAttempts),
			map[string]any{"attempt": attempt + 1, "maxAttempts": maxAttempts, "reason": code}, now())
		if err != nil {
			return RetryDecision{}, err
		}
		if err := tx.Commit(); err != nil {
			return RetryDecision{}, err
		}
		return RetryDecision{Requeued: true, Events: []Event{notice}}, nil
	}

	failCode, failMessage := code, message
	var events []Event
	if requeueable && attempt >= maxAttempts {
		failCode = RetriesExhaustedCode
		// The client renders a terminal failure card for agent.run.failed, but
		// that card explains the failure, not that retries were attempted and
		// exhausted; this notice carries the attempt count. Ordered before the
		// terminal events so the explanation precedes the failure it explains.
		notice, err := appendRunNoticeTx(ctx, tx, sessionID, runID, traceID,
			giveUpNote(attempt),
			map[string]any{"attempts": attempt, "maxAttempts": maxAttempts, "reason": code}, now())
		if err != nil {
			return RetryDecision{}, err
		}
		events = append(events, notice)
	}
	payloadJSON, err := json.Marshal(map[string]any{
		"runId": runID, "code": failCode, "message": failMessage, "retryable": false,
	})
	if err != nil {
		return RetryDecision{}, err
	}
	terminal, err := failRunWithEventTx(ctx, tx, runID, failCode, failMessage, string(payloadJSON), false)
	if err != nil {
		return RetryDecision{}, err
	}
	events = append(events, terminal...)
	if err := tx.Commit(); err != nil {
		return RetryDecision{}, err
	}
	return RetryDecision{Events: events}, nil
}

// giveUpNote words the "we stopped retrying" notice. Shared by both terminal
// paths — assignment rejection and lease/worker recovery — so a user sees the
// same sentence whichever way the run ran out of attempts.
func giveUpNote(attempts int) string {
	if attempts == 1 {
		return "Gave up after 1 attempt"
	}
	return fmt.Sprintf("Gave up after %d attempts", attempts)
}

// requeueRunForRetryTx returns a running run and its in-progress job to the
// queue for another attempt, incrementing the job's attempt counter and
// clearing all execution/lease state so a fresh worker can claim it.
func requeueRunForRetryTx(ctx context.Context, tx *sql.Tx, runID string) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'pending',
			lease_owner = NULL,
			lease_expires_at = NULL,
			lease_expires_at_ns = NULL,
			picked_up_at = NULL,
			assignment_attempt_id = NULL,
			attempt = attempt + 1
		WHERE run_id = ? AND status = 'in_progress'
	`, runID)
	if err != nil {
		return err
	}
	if err := expectOneRow(result, "in-progress job not found for retry requeue"); err != nil {
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
	`, runID)
	if err != nil {
		return err
	}
	return expectOneRow(result, "running run not found for retry requeue")
}

func (r *Repository) EnqueueUserMessage(ctx context.Context, input EnqueueUserMessageInput) (EnqueueUserMessageResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := enqueueUserMessageTx(ctx, tx, input)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	return result, nil
}

type resolvedEnqueueRoute struct {
	requirements      RoutingRequirements
	routedAgent       ExternalAgent
	externalTarget    *ExternalAgentTarget
	externalAgentName sql.NullString
	externalAgentHost sql.NullString
}

func resolveEnqueueRouteTx(ctx context.Context, tx *sql.Tx, input EnqueueUserMessageInput) (resolvedEnqueueRoute, error) {
	resolved := resolvedEnqueueRoute{requirements: RoutingRequirements{
		AgentID:                        input.AgentID,
		ModelProvider:                  input.ModelProvider,
		Model:                          input.Model,
		RequestedTools:                 append([]string(nil), input.RequestedTools...),
		RequiredContextTokens:          input.RequiredContextTokens,
		MinimumWorkerMaxConcurrentRuns: input.MinimumWorkerMaxConcurrentRuns,
	}}
	routedAgent, routed, err := sessionExternalAgentTx(ctx, tx, input.SessionID)
	if err != nil {
		return resolvedEnqueueRoute{}, err
	}
	if !routed {
		return resolved, nil
	}
	resolved.requirements.ModelProvider = "openai_compatible"
	resolved.requirements.Model = routedAgent.Model
	resolved.requirements.ExternalAgent = true
	resolved.routedAgent = routedAgent
	resolved.externalTarget = &ExternalAgentTarget{
		DisplayName:   routedAgent.DisplayName,
		BaseURL:       routedAgent.BaseURL,
		CredentialRef: routedAgent.CredentialRef,
	}
	resolved.externalAgentName = sql.NullString{String: routedAgent.DisplayName, Valid: true}
	if parsed, parseErr := url.Parse(routedAgent.BaseURL); parseErr == nil && parsed.Host != "" {
		resolved.externalAgentHost = sql.NullString{String: parsed.Host, Valid: true}
	}
	return resolved, nil
}

// enqueueUserMessageTx is the whole of "a message arrives and a run is
// queued", minus the transaction around it. It is separate so the scheduler
// can claim a due automation and queue its run in ONE transaction: a crash
// between advancing the schedule and creating the run would otherwise either
// lose a run or fire the same one twice.
func enqueueUserMessageTx(ctx context.Context, tx *sql.Tx, input EnqueueUserMessageInput) (EnqueueUserMessageResult, error) {
	// Resolve the effective destination before writing anything. A conversation
	// routed to an external agent overrides request provider/model fields, and
	// routing validation must evaluate that same frozen destination.
	resolvedRoute, err := resolveEnqueueRouteTx(ctx, tx, input)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if input.ValidateRouting != nil {
		err := input.ValidateRouting(ctx, resolvedRoute.requirements)
		if err != nil {
			return EnqueueUserMessageResult{}, err
		}
	}
	modelProvider, model := resolvedRoute.requirements.ModelProvider, resolvedRoute.requirements.Model

	created := time.Now().UTC()
	userMessageID := ids.New("msg")
	assistantMessageID := ids.New("msg")
	runID := ids.New("run")
	jobID := ids.New("job")
	traceID := ids.New("trace")
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE session_id = ?`, input.SessionID).Scan(&next); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	var latestCreatedAt string
	latestQuery := `SELECT created_at FROM messages WHERE session_id = ? ORDER BY ` + sqliteTimestampNanos("created_at") + ` DESC, id DESC LIMIT 1`
	err = tx.QueryRowContext(ctx, latestQuery, input.SessionID).Scan(&latestCreatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return EnqueueUserMessageResult{}, err
	}
	if latestCreatedAt != "" {
		latest, parseErr := time.Parse(time.RFC3339Nano, latestCreatedAt)
		if parseErr != nil {
			return EnqueueUserMessageResult{}, parseErr
		}
		if !created.After(latest) {
			created = latest.Add(time.Nanosecond)
		}
	}
	createdAt := FormatTimestamp(created)
	assistantCreatedAt := FormatTimestamp(created.Add(time.Nanosecond))
	derivedTitle := DeriveSessionTitle(input.Content)
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at) VALUES (?, ?, 'user', ?, 'text', ?, ?)`, userMessageID, input.SessionID, input.Content, next, createdAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at) VALUES (?, ?, ?, 'assistant', '', 'text', ?, ?)`, assistantMessageID, input.SessionID, runID, next+1, assistantCreatedAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	// Recorded on the run itself, not left to be derived later from
	// session_external_agent. That table says where the conversation points
	// NOW; re-pointing or deleting an agent afterwards would silently rewrite
	// the history of where earlier messages were actually sent, and "what left
	// this machine" is exactly the record that must not be rewritable.
	//
	// The host rather than the base URL: a URL can carry a path, a query, and
	// from a careless paste a credential. Only the recipient is worth keeping.
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_runs (id, session_id, user_message_id, assistant_message_id, agent_id, trace_id, status, model_provider, model_name, external_agent_name, external_agent_host, created_at) VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?, ?, ?)`, runID, input.SessionID, userMessageID, assistantMessageID, input.AgentID, traceID, modelProvider, model, resolvedRoute.externalAgentName, resolvedRoute.externalAgentHost, createdAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	// Name the conversation after the first thing said in it, and mark the
	// session as touched so the client's most-recent-first list actually
	// reflects activity rather than creation order.
	//
	// The untitled check lives in the statement rather than in a read followed
	// by a write. SetMaxOpenConns(1) serialises writers today, so a read-modify
	// -write would also be correct — but it would be correct only because of a
	// pool setting made elsewhere for another reason. This form does not care.
	if _, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET title = CASE
				WHEN title_origin = 'unset' AND ? <> ''
					THEN ?
				ELSE title
			END,
			title_origin = CASE
				WHEN title_origin = 'unset' AND ? <> ''
					THEN 'derived'
				ELSE title_origin
			END,
			updated_at = ?
		WHERE id = ?
	`, derivedTitle, derivedTitle, derivedTitle, createdAt, input.SessionID); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	var sessionTitle string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(title, '') FROM sessions WHERE id = ?`, input.SessionID).Scan(&sessionTitle); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	sessionUpdatedPayload, err := json.Marshal(map[string]string{
		"title":     sessionTitle,
		"updatedAt": createdAt,
	})
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	sessionUpdatedEvent, err := appendSessionEventTx(ctx, tx, input.SessionID, traceID, "session.updated", string(sessionUpdatedPayload), createdAt)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	// Frozen into the payload rather than read when the job is claimed: a
	// skill edited or detached while the job waits must not change what this
	// run was told to do. It is the same reason userText is stored here.
	attachedSkills, err := attachedSkillsTx(ctx, tx, input.SessionID)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	jobPayload, err := json.Marshal(map[string]any{
		"userText":                       input.Content,
		"sessionId":                      input.SessionID,
		"userMessageId":                  userMessageID,
		"assistantMessageId":             assistantMessageID,
		"traceId":                        traceID,
		"modelProvider":                  modelProvider,
		"model":                          model,
		"requestedTools":                 input.RequestedTools,
		"requiredContextTokens":          input.RequiredContextTokens,
		"minimumWorkerMaxConcurrentRuns": input.MinimumWorkerMaxConcurrentRuns,
		"skills":                         attachedSkills,
		// Frozen for the same reason the skills are: re-pointing or deleting
		// the agent while this job waits must not redirect a message the user
		// already sent, and must not send it to a company they did not pick.
		"externalAgent": resolvedRoute.externalTarget,
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
	// Written into the transcript, in the same transaction that accepts the
	// message, because a conversation that leaves this machine has to say so
	// where it happens rather than only in a settings screen the user is not
	// looking at. Ordered after the queued event so it lands inside the run
	// the sender is already streaming.
	//
	// Worded prospectively, and that is not a stylistic choice. Nothing has
	// left the machine at this point — the job is only queued — and several
	// designed paths mean it never will: a missing API key or an unwired
	// runtime fails the run with external_agent_unavailable, a debug tool
	// command is answered locally before a provider is ever chosen, and a
	// cancelled or unclaimed run never reaches a vendor at all. "Sent … left
	// your machine" would be a false statement in the transcript on every one
	// of those, which this project ranks as the worst kind of defect. "Sending
	// … leaves" is true in both outcomes.
	var routingEvents []Event
	if resolvedRoute.requirements.ExternalAgent {
		notice, err := appendRunNoticeTx(ctx, tx, input.SessionID, runID, traceID,
			"Sending to "+flattenNoticeText(resolvedRoute.routedAgent.DisplayName)+" — this message leaves your machine",
			map[string]any{
				"externalAgent": resolvedRoute.routedAgent.DisplayName,
				"endpoint":      ExternalAgentEndpointHost(resolvedRoute.routedAgent.BaseURL),
				"model":         resolvedRoute.routedAgent.Model,
			}, createdAt)
		if err != nil {
			return EnqueueUserMessageResult{}, err
		}
		routingEvents = append(routingEvents, notice)
	}
	return EnqueueUserMessageResult{SessionID: input.SessionID, UserMessageID: userMessageID, AssistantMessageID: assistantMessageID, RunID: runID, JobID: jobID, TraceID: traceID, Status: "queued", SessionUpdatedEvent: sessionUpdatedEvent, QueuedEvent: queuedEvent, RoutingEvents: routingEvents}, nil
}

// flattenNoticeText collapses a user-chosen name to a single line before it is
// pasted into a sentence. An agent named across two lines would otherwise
// break the notice in half, and the second half would read as its own claim.
func flattenNoticeText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func (r *Repository) ClaimNextJob(ctx context.Context, agentID string, leaseOwner string) (Job, error) {
	return r.ClaimNextJobWithLimit(ctx, agentID, leaseOwner, 0, 5*time.Minute)
}

func (r *Repository) ClaimNextJobWithLimit(ctx context.Context, agentID string, leaseOwner string, globalLimit int, leaseDuration time.Duration) (Job, error) {
	return r.ClaimNextCompatibleJobWithLimit(ctx, agentID, leaseOwner, globalLimit, leaseDuration, nil, nil)
}

func (r *Repository) ClaimNextCompatibleJobWithLimit(
	ctx context.Context,
	agentID string,
	leaseOwner string,
	globalLimit int,
	leaseDuration time.Duration,
	capabilities *WorkerRoutingCapabilities,
	compatible func(RoutingRequirements) bool,
) (Job, error) {
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
	defer func() { _ = tx.Rollback() }()
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
	routingFilter, routingArgs := claimRoutingFilterSQL(capabilities)
	claimQuery := `
		SELECT
			j.id,
			j.run_id,
			r.session_id,
			r.user_message_id,
			COALESCE(r.assistant_message_id, ''),
			j.agent_id,
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
			)` + routingFilter + `
		ORDER BY ` + sqliteTimestampNanos("j.created_at") + `, j.id
	`
	queryArgs := []any{agentID}
	queryArgs = append(queryArgs, routingArgs...)
	rows, err := tx.QueryContext(ctx, claimQuery, queryArgs...)
	if err != nil {
		return Job{}, err
	}
	defer func() { _ = rows.Close() }()

	var job Job
	for rows.Next() {
		var candidate Job
		var payloadJSON string
		if err := rows.Scan(
			&candidate.JobID,
			&candidate.RunID,
			&candidate.SessionID,
			&candidate.UserMessageID,
			&candidate.AssistantMessageID,
			&candidate.AgentID,
			&candidate.TraceID,
			&candidate.ModelProvider,
			&candidate.Model,
			&payloadJSON,
			&candidate.Attempt,
		); err != nil {
			return Job{}, err
		}
		var payload queuedJobPayload
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return Job{}, err
		}
		candidate.UserText = payload.UserText
		candidate.RequestedTools = payload.RequestedTools
		candidate.RequiredContextTokens = payload.RequiredContextTokens
		candidate.MinimumWorkerMaxConcurrentRuns = payload.MinimumWorkerMaxConcurrentRuns
		// Absent for jobs enqueued before skills existed, which decodes to nil —
		// the same as a conversation with none attached.
		candidate.Skills = payload.Skills
		// Absent for every job enqueued before routing existed, and for every
		// conversation that was never routed away. nil means the local assistant,
		// which is the only default that keeps the transcript on this machine.
		candidate.ExternalAgent = payload.ExternalAgent
		if compatible != nil && !compatible(RoutingRequirements{
			AgentID:                        candidate.AgentID,
			ModelProvider:                  candidate.ModelProvider,
			Model:                          candidate.Model,
			RequestedTools:                 candidate.RequestedTools,
			RequiredContextTokens:          candidate.RequiredContextTokens,
			MinimumWorkerMaxConcurrentRuns: candidate.MinimumWorkerMaxConcurrentRuns,
			ExternalAgent:                  candidate.ExternalAgent != nil,
		}) {
			continue
		}
		job = candidate
		break
	}
	if err := rows.Err(); err != nil {
		return Job{}, err
	}
	if err := rows.Close(); err != nil {
		return Job{}, err
	}
	if job.JobID == "" {
		return Job{}, nil
	}
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

func claimRoutingFilterSQL(capabilities *WorkerRoutingCapabilities) (string, []any) {
	if capabilities == nil {
		return "", nil
	}
	const externalAgentType = `json_type(j.payload_json, '$.externalAgent')`
	var destinations []string
	var args []any
	if capabilities.SupportsExternalAgents {
		destinations = append(destinations, externalAgentType+` = 'object'`)
	}
	for _, model := range capabilities.Models {
		destinations = append(destinations, `(
			COALESCE(`+externalAgentType+`, 'null') <> 'object'
			AND r.model_provider = ?
			AND r.model_name = ?
			AND COALESCE(CAST(json_extract(j.payload_json, '$.requiredContextTokens') AS INTEGER), 0) <= ?
		)`)
		args = append(args, model.Provider, model.Model, model.MaxContextTokens)
	}
	if len(destinations) == 0 {
		destinations = append(destinations, "0")
	}
	filter := `
		AND (` + strings.Join(destinations, " OR ") + `)
		AND MAX(
			COALESCE(CAST(json_extract(j.payload_json, '$.minimumWorkerMaxConcurrentRuns') AS INTEGER), 0),
			1
		) <= ?`
	args = append(args, capabilities.MaxConcurrentRuns)
	if len(capabilities.Tools) == 0 {
		filter += `
		AND NOT EXISTS (
			SELECT 1
			FROM json_each(COALESCE(json_extract(j.payload_json, '$.requestedTools'), json('[]')))
		)`
		return filter, args
	}
	placeholders := make([]string, len(capabilities.Tools))
	for index, tool := range capabilities.Tools {
		placeholders[index] = "?"
		args = append(args, tool)
	}
	filter += `
		AND NOT EXISTS (
			SELECT 1
			FROM json_each(COALESCE(json_extract(j.payload_json, '$.requestedTools'), json('[]'))) requested_tool
			WHERE requested_tool.value NOT IN (` + strings.Join(placeholders, ", ") + `)
		)`
	return filter, args
}

func (r *Repository) ListPendingRoutingWork(ctx context.Context) ([]PendingRoutingWork, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT j.run_id, j.agent_id, r.model_provider, r.model_name, j.payload_json
		FROM jobs j
		JOIN agent_runs r ON r.id = j.run_id
		WHERE j.status = 'pending' AND r.status = 'queued'
		ORDER BY `+sqliteTimestampNanos("j.created_at")+`, j.id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var work []PendingRoutingWork
	for rows.Next() {
		var item PendingRoutingWork
		var payloadJSON string
		if err := rows.Scan(
			&item.RunID,
			&item.Requirements.AgentID,
			&item.Requirements.ModelProvider,
			&item.Requirements.Model,
			&payloadJSON,
		); err != nil {
			return nil, err
		}
		var payload queuedJobPayload
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return nil, err
		}
		item.Requirements.RequestedTools = append([]string(nil), payload.RequestedTools...)
		item.Requirements.RequiredContextTokens = payload.RequiredContextTokens
		item.Requirements.MinimumWorkerMaxConcurrentRuns = payload.MinimumWorkerMaxConcurrentRuns
		item.Requirements.ExternalAgent = payload.ExternalAgent != nil
		work = append(work, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return work, nil
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
	defer func() { _ = tx.Rollback() }()
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
