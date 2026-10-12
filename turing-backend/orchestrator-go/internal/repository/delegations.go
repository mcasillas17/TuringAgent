package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/teamfiles"
)

var (
	ErrDelegationArgumentsChanged = errors.New("this tool call already delegated a task with other arguments")
	ErrDelegationPolicyChanged    = errors.New("the team.delegate policy changed before the delegation was created")
	ErrDelegationProfileChanged   = errors.New("the specialist was disabled or regranted before the delegation was created")
	ErrDelegationCapReached       = errors.New("this run has delegated as many tasks as it may")
	ErrTeamNameCollision          = errors.New("the team name is taken")
)

// Delegation is one task a Turing run handed to a specialist.
type Delegation struct {
	ID              string
	ParentSessionID string
	ParentRunID     string
	ChildSessionID  string
	ChildRunID      string
	ProfileID       string
	// ArgsHash is the approvals' hash of the call's arguments; a retried call
	// must match it.
	ArgsHash string
	// State is the child run's status, with recovering shown as running. A
	// delegation keeps no state of its own.
	State string
}

// DelegationCount is how many tasks a run has delegated so far.
func (r *Repository) DelegationCount(ctx context.Context, runID string) (int, error) {
	return delegationCount(ctx, r.db, runID)
}

func delegationCount(ctx context.Context, q rowQuerier, runID string) (int, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM delegations WHERE parent_run_id = ?`, runID).Scan(&count)
	return count, err
}

// DelegationAssignmentCurrent reports whether attemptID still owns the run:
// running, executing, its job in progress under the same attempt, and its
// conversation not being deleted. A worker that was fenced, reassigned or is
// recovering cannot delegate.
func (r *Repository) DelegationAssignmentCurrent(ctx context.Context, runID, attemptID string) (bool, error) {
	return delegationAssignmentCurrent(ctx, r.db, runID, attemptID)
}

func delegationAssignmentCurrent(ctx context.Context, q rowQuerier, runID, attemptID string) (bool, error) {
	var current bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM agent_runs run
			JOIN sessions session ON session.id = run.session_id
			JOIN jobs job ON job.run_id = run.id
			WHERE run.id = ? AND ? <> '' AND run.execution_attempt_id = ?
				AND run.execution_active = 1 AND run.status = 'running'
				AND job.status = 'in_progress' AND job.assignment_attempt_id = ?
				AND session.deletion_state = 'active'
		)`, runID, attemptID, attemptID, attemptID).Scan(&current)
	return current, err
}

// DelegationForToolCall is the delegation one tool call already created.
func (r *Repository) DelegationForToolCall(ctx context.Context, parentRunID, toolCallID string) (Delegation, bool, error) {
	return delegationForToolCall(ctx, r.db, parentRunID, toolCallID)
}

func delegationForToolCall(ctx context.Context, q rowQuerier, parentRunID, toolCallID string) (Delegation, bool, error) {
	var delegation Delegation
	err := q.QueryRowContext(ctx, `
		SELECT d.id, d.parent_session_id, d.parent_run_id, d.child_session_id, d.child_run_id,
			d.profile_id, d.args_hash, run.status
		FROM delegations d JOIN agent_runs run ON run.id = d.child_run_id
		WHERE d.parent_run_id = ? AND d.parent_tool_call_id = ?`, parentRunID, toolCallID,
	).Scan(&delegation.ID, &delegation.ParentSessionID, &delegation.ParentRunID, &delegation.ChildSessionID,
		&delegation.ChildRunID, &delegation.ProfileID, &delegation.ArgsHash, &delegation.State)
	if errors.Is(err, sql.ErrNoRows) {
		return Delegation{}, false, nil
	}
	if err != nil {
		return Delegation{}, false, err
	}
	if delegation.State == lifecycleRecovering {
		delegation.State = lifecycleRunning
	}
	return delegation, true, nil
}

// CreateDelegationInput is one admitted team.delegate call, with the child
// the service resolved for it.
type CreateDelegationInput struct {
	ParentRunID         string
	AssignmentAttemptID string
	ToolCallID          string
	ArgsHash            string
	// Policy is the team.delegate policy the call was admitted under.
	Policy    string
	MaxPerRun int
	// Profile is the specialist as the child will run, at the roster's
	// revision.
	Profile       AgentProfileSnapshot
	SkillPatterns []string
	// ModelProvider and Model are the child's route, as the service
	// validated it.
	ModelProvider string
	Model         string
	SelectedTools []string
	// Brief is the framed task: the child's first message and its live turn.
	Brief string
}

type CreateDelegationResult struct {
	Delegation Delegation
	// Replayed means the call had already created Delegation; nothing new
	// was written.
	Replayed bool
	// Events are the child's queued event and the parent's
	// delegation.started, for publishing after the commit.
	Events []Event
}

// CreateDelegation writes one delegation in a single transaction. It first
// reads again everything the database holds that an approval wait could
// outlast: the caller's assignment, an earlier result for the same tool call,
// the admitted policy, the profile's enablement and grant, a `team` name
// collision and the per-run cap. Any change refuses the call and writes
// nothing.
func (r *Repository) CreateDelegation(ctx context.Context, input CreateDelegationInput) (CreateDelegationResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return CreateDelegationResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if current, err := delegationAssignmentCurrent(ctx, tx, input.ParentRunID, input.AssignmentAttemptID); err != nil {
		return CreateDelegationResult{}, err
	} else if !current {
		return CreateDelegationResult{}, ErrAssignmentFenced
	}
	if existing, found, err := delegationForToolCall(ctx, tx, input.ParentRunID, input.ToolCallID); err != nil {
		return CreateDelegationResult{}, err
	} else if found {
		if existing.ArgsHash != input.ArgsHash {
			return CreateDelegationResult{}, ErrDelegationArgumentsChanged
		}
		return CreateDelegationResult{Delegation: existing, Replayed: true}, nil
	}
	if active, err := pseudoServerDispatchActive(ctx, tx, "team", input.ParentRunID, "team.delegate", input.Policy); err != nil {
		return CreateDelegationResult{}, err
	} else if !active {
		return CreateDelegationResult{}, ErrDelegationPolicyChanged
	}
	var granted bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM agent_profile_settings
			WHERE profile_id = ? AND enabled = 1 AND granted_revision = ?)`,
		input.Profile.ProfileID, input.Profile.Revision).Scan(&granted); err != nil {
		return CreateDelegationResult{}, err
	}
	if !granted {
		return CreateDelegationResult{}, ErrDelegationProfileChanged
	}
	if collision, err := teamNameCollision(ctx, tx); err != nil {
		return CreateDelegationResult{}, err
	} else if collision != "" {
		return CreateDelegationResult{}, fmt.Errorf("%w: %s", ErrTeamNameCollision, collision)
	}
	if delegated, err := delegationCount(ctx, tx, input.ParentRunID); err != nil {
		return CreateDelegationResult{}, err
	} else if delegated >= input.MaxPerRun {
		return CreateDelegationResult{}, ErrDelegationCapReached
	}
	var parentSessionID, parentTraceID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id, trace_id FROM agent_runs WHERE id = ?`, input.ParentRunID).
		Scan(&parentSessionID, &parentTraceID); err != nil {
		return CreateDelegationResult{}, err
	}
	var childSkills []SkillSnapshot
	if len(input.SkillPatterns) > 0 {
		skills, err := r.enabledSkillSnapshotsReadOnlyTx(ctx, tx)
		if err != nil {
			return CreateDelegationResult{}, err
		}
		for _, skill := range skills {
			if !skill.Withheld && teamfiles.MatchAny(input.SkillPatterns, skill.SkillID) {
				childSkills = append(childSkills, skill)
			}
		}
	}

	delegation := Delegation{
		ID:              ids.New("dlg"),
		ParentSessionID: parentSessionID,
		ParentRunID:     input.ParentRunID,
		ChildSessionID:  ids.New("sess"),
		ChildRunID:      ids.New("run"),
		ProfileID:       input.Profile.ProfileID,
		ArgsHash:        input.ArgsHash,
		State:           lifecycleQueued,
	}
	sessionCreatedAt := now()
	title := strings.TrimSpace(input.Profile.Emoji + " " + input.Profile.DisplayName)
	// Hidden from every chat surface by its kind; titled from the profile,
	// never by a model, and announced by no session.updated.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sessions (id, kind, parent_session_id, title, title_origin, created_at, updated_at)
		VALUES (?, 'delegation', ?, ?, 'explicit', ?, ?)`,
		delegation.ChildSessionID, parentSessionID, title, sessionCreatedAt, sessionCreatedAt); err != nil {
		return CreateDelegationResult{}, err
	}
	anchor, err := insertSessionAnchorTx(ctx, tx, delegation.ChildSessionID, delegation.ChildRunID,
		"user", backendegress.DelegationBriefContentType, input.Brief)
	if err != nil {
		return CreateDelegationResult{}, err
	}
	// A specialist never receives the user's persona or profile: Turing owns
	// their identity and voice. Both are frozen as withheld.
	memoryPreimage := MemoryEgressSnapshot{}.Preimage(input.SelectedTools)
	memoryFingerprint, err := backendegress.MemorySnapshotFingerprint(memoryPreimage)
	if err != nil {
		return CreateDelegationResult{}, err
	}
	profile := input.Profile
	queuedEvent, err := insertQueuedRunTx(ctx, tx, queuedRun{
		runID: delegation.ChildRunID, sessionID: delegation.ChildSessionID, anchor: anchor,
		modelProvider: input.ModelProvider, model: input.Model,
		payload: map[string]any{
			"userText":                       input.Brief,
			"requestedTools":                 input.SelectedTools,
			"requiredContextTokens":          0,
			"minimumWorkerMaxConcurrentRuns": 0,
			"skills":                         childSkills,
			"externalAgent":                  nil,
			"egressDecision":                 nil,
			"selectedTools":                  input.SelectedTools,
			"pinnedPersona":                  pinnedPersonaSnapshot(memoryPreimage),
			"pinnedProfile":                  pinnedProfileSnapshot(memoryPreimage),
			"memorySnapshotFingerprint":      memoryFingerprint,
			"agentProfile":                   &profile,
			"enforceSelectedTools":           true,
			"skipAutomaticRecall":            true,
			// Only a worker that honors the specialist-job contract may run
			// it; an older one would run it as Turing with every tool.
			"minimumTeamProtocolVersion": 1,
		},
	})
	if err != nil {
		return CreateDelegationResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO delegations (
			id, parent_session_id, parent_run_id, parent_tool_call_id, child_session_id, child_run_id,
			profile_id, profile_revision, args_hash, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		delegation.ID, parentSessionID, input.ParentRunID, input.ToolCallID, delegation.ChildSessionID,
		delegation.ChildRunID, profile.ProfileID, profile.Revision, input.ArgsHash, anchor.CreatedAt); err != nil {
		return CreateDelegationResult{}, err
	}
	startedPayload, err := json.Marshal(map[string]any{
		"delegationId":   delegation.ID,
		"parentRunId":    input.ParentRunID,
		"childRunId":     delegation.ChildRunID,
		"childSessionId": delegation.ChildSessionID,
		"profileId":      profile.ProfileID,
		"displayName":    profile.DisplayName,
		"emoji":          profile.Emoji,
		"state":          delegation.State,
		"summary":        "Asked " + title,
	})
	if err != nil {
		return CreateDelegationResult{}, err
	}
	started, err := appendRunEventTx(ctx, tx, parentSessionID, input.ParentRunID, parentTraceID,
		"delegation.started", string(startedPayload), anchor.CreatedAt)
	if err != nil {
		return CreateDelegationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CreateDelegationResult{}, err
	}
	return CreateDelegationResult{Delegation: delegation, Events: []Event{queuedEvent, started}}, nil
}

// sessionAnchor is the message a run is anchored on and the empty assistant
// placeholder its reply fills.
type sessionAnchor struct {
	MessageID     string
	PlaceholderID string
	CreatedAt     string
	created       time.Time
}

// insertSessionAnchorTx writes an orchestrator-authored anchor message and
// its placeholder at the end of a session, and moves the session's activity
// time to the anchor. Unlike a user turn it derives no title, does no routing
// or egress work, and publishes nothing; the placeholder is always an empty
// assistant text message.
func insertSessionAnchorTx(ctx context.Context, tx *sql.Tx, sessionID, runID, role, contentType, content string) (sessionAnchor, error) {
	next, created, err := nextMessageSlotTx(ctx, tx, sessionID, time.Now().UTC())
	if err != nil {
		return sessionAnchor{}, err
	}
	anchor := sessionAnchor{MessageID: ids.New("msg"), PlaceholderID: ids.New("msg"), CreatedAt: FormatTimestamp(created), created: created}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		anchor.MessageID, sessionID, role, content, contentType, next, anchor.CreatedAt); err != nil {
		return sessionAnchor{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
		VALUES (?, ?, ?, 'assistant', '', 'text', ?, ?)`,
		anchor.PlaceholderID, sessionID, runID, next+1, FormatTimestamp(created.Add(time.Nanosecond))); err != nil {
		return sessionAnchor{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, anchor.CreatedAt, sessionID); err != nil {
		return sessionAnchor{}, err
	}
	return anchor, nil
}

// queuedRun is a run the orchestrator queues on its own anchor rather than a
// user turn's.
type queuedRun struct {
	runID, sessionID     string
	anchor               sessionAnchor
	modelProvider, model string
	// continuesRunID names the run a continuation answers for; empty for
	// every other run.
	continuesRunID string
	payload        map[string]any
}

// insertQueuedRunTx writes a queued general-assistant run anchored on its
// message, its job, and its queued event.
func insertQueuedRunTx(ctx context.Context, tx *sql.Tx, run queuedRun) (Event, error) {
	createdAtNanos, err := persisttime.ParseCanonical(run.anchor.CreatedAt)
	if err != nil {
		return Event{}, err
	}
	traceID := ids.New("trace")
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_runs (id, session_id, user_message_id, assistant_message_id, agent_id, trace_id, status,
			model_provider, model_name, created_at, state_version, state_updated_at, outcome_reason,
			assistant_content_sha256, queued_since_ns, continues_run_id)
		VALUES (?, ?, ?, ?, 'general_assistant', ?, 'queued', ?, ?, ?, 1, ?, 'none', ?, ?, ?)`,
		run.runID, run.sessionID, run.anchor.MessageID, run.anchor.PlaceholderID, traceID,
		run.modelProvider, run.model, run.anchor.CreatedAt, run.anchor.CreatedAt,
		emptyAssistantContentSHA256, createdAtNanos.UnixNano(), nullableText(run.continuesRunID)); err != nil {
		return Event{}, err
	}
	queuedRow, err := readRunRow(ctx, tx, run.runID)
	if err != nil {
		return Event{}, err
	}
	if err := validateRunCorrelationLink(queuedRow.link()); err != nil {
		return Event{}, err
	}
	jobID := ids.New("job")
	payload := map[string]any{
		"sessionId":          run.sessionID,
		"userMessageId":      run.anchor.MessageID,
		"assistantMessageId": run.anchor.PlaceholderID,
		"traceId":            traceID,
		"modelProvider":      run.modelProvider,
		"model":              run.model,
	}
	for key, value := range run.payload {
		payload[key] = value
	}
	jobPayload, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO jobs (id, run_id, agent_id, status, payload_json, created_at, created_at_ns)
		VALUES (?, ?, 'general_assistant', 'pending', ?, ?, ?)`,
		jobID, run.runID, string(jobPayload), run.anchor.CreatedAt, run.anchor.created.UnixNano()); err != nil {
		return Event{}, err
	}
	queuedPayload, err := marshalRunStatePayload(map[string]any{
		"runId": run.runID, "jobId": jobID, "status": "queued", "agentId": "general_assistant",
	}, queuedRow.state())
	if err != nil {
		return Event{}, err
	}
	return appendRunEventTx(ctx, tx, run.sessionID, run.runID, traceID, "agent.run.queued", queuedPayload, run.anchor.CreatedAt)
}
