package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// joinedDelegation is one finished task as its join reports it.
type joinedDelegation struct {
	id, childSessionID, childRunID, profileID string
	displayName, emoji                        string
	state, outcomeReason, errorCode           string
	cancellationCode, finishedAt, result      string
}

// maybeJoinTx is the join, run inside the transaction that terminalizes
// runID, whether runID is a child or the parent that delegated. It acts only
// once the parent's conversation is active, the parent has completed, it has
// a delegation not yet joined, and every one of its delegations has finished;
// otherwise it writes nothing and returns no error, so the terminalization
// that called it still commits.
//
// When it acts it writes, in this transaction: the results in the parent's
// conversation as a role-system delegation_results message, unframed; one
// delegation.finished per delegation; the continuation run that answers for
// Turing from the results; and every delegation marked joined, so it cannot
// run twice. It returns the continuation's run ID and the events to publish.
//
// Everything it reads is in the database. What the continuation may use was
// frozen onto the parent beside its roster, and it pins what the parent
// pinned, because it finishes the parent's turn.
func maybeJoinTx(ctx context.Context, tx *sql.Tx, runID string) (string, []Event, error) {
	parentRunID := runID
	var parent sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT parent_run_id FROM delegations WHERE child_run_id = ?`, runID).Scan(&parent)
	switch {
	case err == nil:
		parentRunID = parent.String
	case !errors.Is(err, sql.ErrNoRows):
		return "", nil, err
	}
	// A run that never delegated, the common case, stops at one indexed read.
	var unjoined bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM delegations WHERE parent_run_id = ? AND joined = 0)`, parentRunID).Scan(&unjoined); err != nil {
		return "", nil, err
	}
	if !unjoined {
		return "", nil, nil
	}
	var status, sessionID, traceID, provider, model, deletionState string
	var outstanding bool
	err = tx.QueryRowContext(ctx, `
		SELECT run.status, run.session_id, run.trace_id, run.model_provider, run.model_name, session.deletion_state,
			EXISTS(SELECT 1 FROM delegations d JOIN agent_runs child ON child.id = d.child_run_id
				WHERE d.parent_run_id = run.id AND child.status NOT IN ('completed', 'failed', 'cancelled'))
		FROM agent_runs run JOIN sessions session ON session.id = run.session_id
		WHERE run.id = ?`, parentRunID).Scan(&status, &sessionID, &traceID, &provider, &model, &deletionState, &outstanding)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if deletionState != "active" || status != lifecycleCompleted || outstanding {
		return "", nil, nil
	}

	delegations, err := joinedDelegationsTx(ctx, tx, parentRunID)
	if err != nil {
		return "", nil, err
	}
	continuation, err := runTeamContinuation(ctx, tx, parentRunID)
	if err != nil {
		return "", nil, err
	}
	resultMaxBytes := continuation.ResultMaxBytes
	if resultMaxBytes <= 0 {
		resultMaxBytes = backendegress.DefaultDelegationResultMaxBytes
	}
	// The frozen cap was validated against the delegation limit configured
	// when the parent was enqueued, and a restart can raise that limit. Cutting
	// each result to what this many tasks leave room for keeps the per-result
	// cut the only one; the frame's own bound is the backstop.
	resultMaxBytes = max(min(resultMaxBytes, backendegress.DelegationResultBudget(len(delegations))), 1)
	tools := continuation.Tools
	if tools == nil {
		tools = []string{}
	}
	content := delegationResultsContent(delegations, resultMaxBytes)
	userText, err := backendegress.FrameRetrievedContent(backendegress.DelegationResultsFraming(), []byte(content))
	if err != nil {
		return "", nil, err
	}

	continuationRunID := ids.New("run")
	anchor, err := insertSessionAnchorTx(ctx, tx, sessionID, continuationRunID,
		"system", backendegress.DelegationResultsContentType, content)
	if err != nil {
		return "", nil, err
	}
	var events []Event
	for _, delegation := range delegations {
		if _, err := tx.ExecContext(ctx, `
			UPDATE delegations SET joined = 1, finished_at = ?, result_bytes = ?, error_code = ? WHERE id = ?`,
			nullableText(delegation.finishedAt), len(delegation.result), nullableText(delegation.code()), delegation.id); err != nil {
			return "", nil, err
		}
		payload, err := json.Marshal(map[string]any{
			"delegationId":      delegation.id,
			"parentRunId":       parentRunID,
			"childRunId":        delegation.childRunID,
			"childSessionId":    delegation.childSessionID,
			"profileId":         delegation.profileID,
			"displayName":       delegation.displayName,
			"emoji":             delegation.emoji,
			"state":             delegation.state,
			"summary":           delegation.displayName + " " + delegation.outcome(),
			"continuationRunId": continuationRunID,
		})
		if err != nil {
			return "", nil, err
		}
		finished, err := appendRunEventTx(ctx, tx, sessionID, parentRunID, traceID,
			"delegation.finished", string(payload), anchor.CreatedAt)
		if err != nil {
			return "", nil, err
		}
		events = append(events, finished)
	}

	inherited, err := continuationInheritanceTx(ctx, tx, parentRunID, tools)
	if err != nil {
		return "", nil, err
	}
	queued, err := insertQueuedRunTx(ctx, tx, queuedRun{
		runID: continuationRunID, sessionID: sessionID, anchor: anchor, continuesRunID: parentRunID,
		modelProvider: provider, model: model,
		payload: map[string]any{
			"userText":                       userText,
			"requestedTools":                 tools,
			"requiredContextTokens":          inherited.requiredContextTokens,
			"minimumWorkerMaxConcurrentRuns": 0,
			"skills":                         inherited.skills,
			"externalAgent":                  nil,
			// No egress decision: a consent the parent carried never covers a
			// turn that holds a specialist's output.
			"egressDecision":            nil,
			"selectedTools":             tools,
			"pinnedPersona":             inherited.persona,
			"pinnedProfile":             inherited.profile,
			"memorySnapshotFingerprint": inherited.fingerprint,
			// Specialist text must not choose what the continuation may call
			// or steer a recall query.
			"enforceSelectedTools": true,
			"skipAutomaticRecall":  true,
			// Only a worker that frames a join as data may replay one.
			"minimumTeamProtocolVersion": 1,
		},
	})
	if err != nil {
		return "", nil, err
	}
	return continuationRunID, append(events, queued), nil
}

func joinedDelegationsTx(ctx context.Context, tx *sql.Tx, parentRunID string) ([]joinedDelegation, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT d.id, d.child_session_id, d.child_run_id, d.profile_id,
			COALESCE(json_extract(job.payload_json, '$.agentProfile.displayName'), d.profile_id),
			COALESCE(json_extract(job.payload_json, '$.agentProfile.emoji'), ''),
			child.status, child.outcome_reason, COALESCE(child.error_code, ''),
			COALESCE(child.cancellation_reason, ''), COALESCE(child.finished_at, ''), COALESCE(reply.content, '')
		FROM delegations d
		JOIN agent_runs child ON child.id = d.child_run_id
		LEFT JOIN jobs job ON job.run_id = child.id
		LEFT JOIN messages reply ON reply.id = child.assistant_message_id
		WHERE d.parent_run_id = ? AND d.joined = 0
		ORDER BY d.created_at, d.id`, parentRunID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var delegations []joinedDelegation
	for rows.Next() {
		var d joinedDelegation
		if err := rows.Scan(&d.id, &d.childSessionID, &d.childRunID, &d.profileID, &d.displayName, &d.emoji,
			&d.state, &d.outcomeReason, &d.errorCode, &d.cancellationCode, &d.finishedAt, &d.result); err != nil {
			return nil, err
		}
		delegations = append(delegations, d)
	}
	return delegations, rows.Err()
}

// code is the error code a finished delegation records: the failure's, or
// the cancellation's, and none for a completed task.
func (d joinedDelegation) code() string {
	switch d.state {
	case lifecycleFailed:
		return d.errorCode
	case lifecycleCancelled:
		return d.cancellationCode
	}
	return ""
}

// outcome is how a task ended, in the orchestrator's words.
func (d joinedDelegation) outcome() string {
	switch {
	case d.state == lifecycleCompleted:
		return "completed"
	case d.state == lifecycleCancelled && d.outcomeReason == string(runoutcome.ReasonUserCancelled):
		return "was cancelled by you"
	case d.state == lifecycleCancelled:
		return "was cancelled (" + d.code() + ")"
	default:
		return "failed (" + d.code() + ")"
	}
}

// delegationResultsContent is a join's stored, unframed content: each task in
// the order it was asked for, who did it, how it ended, and its result, cut
// to resultMaxBytes on a character boundary with the cut said out loud. The
// header and the cut notice fit DelegationResultHeaderBytes, so the whole
// fits the one frame configuration validated it against.
func delegationResultsContent(delegations []joinedDelegation, resultMaxBytes int) string {
	var content strings.Builder
	fmt.Fprintf(&content, "Results of the %d task(s) Turing delegated, in the order they were asked for.", len(delegations))
	for _, d := range delegations {
		who := strings.TrimSpace(utf8Prefix(d.emoji, 32) + " " + utf8Prefix(d.displayName, 256))
		fmt.Fprintf(&content, "\n\n## %s (%s): %s\n", who, utf8Prefix(d.profileID, 128), utf8Prefix(d.outcome(), 256))
		switch result := utf8Prefix(d.result, resultMaxBytes); {
		case strings.TrimSpace(d.result) == "":
			content.WriteString("(no result)")
		case len(result) < len(d.result):
			content.WriteString(result)
			fmt.Fprintf(&content, "\n[Result cut: %d of %d bytes kept.]", len(result), len(d.result))
		default:
			content.WriteString(result)
		}
	}
	return content.String()
}

// utf8Prefix is the longest prefix of value within maxBytes that ends on a
// character boundary.
func utf8Prefix(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

// continuationInheritance is what a continuation takes from its parent's job.
type continuationInheritance struct {
	skills                json.RawMessage
	persona               PinnedPersonaSnapshot
	profile               PinnedProfileSnapshot
	fingerprint           string
	requiredContextTokens int
}

// continuationInheritanceTx reads the parent's frozen skills, pinned persona
// and profile, and context requirement, and fingerprints the pins against the
// continuation's own tools.
func continuationInheritanceTx(ctx context.Context, tx *sql.Tx, parentRunID string, tools []string) (continuationInheritance, error) {
	var skills, persona, profile sql.NullString
	var inherited continuationInheritance
	if err := tx.QueryRowContext(ctx, `
		SELECT json_extract(payload_json, '$.skills'), json_extract(payload_json, '$.pinnedPersona'),
			json_extract(payload_json, '$.pinnedProfile'), COALESCE(json_extract(payload_json, '$.requiredContextTokens'), 0)
		FROM jobs WHERE run_id = ?`, parentRunID).Scan(&skills, &persona, &profile, &inherited.requiredContextTokens); err != nil {
		return continuationInheritance{}, err
	}
	// The parent's skills exactly as frozen; a parent with none carries null,
	// and so does its continuation.
	if skills.Valid {
		inherited.skills = json.RawMessage(skills.String)
	}
	// A pin the parent did not carry is withheld, never invented.
	inherited.persona = PinnedPersonaSnapshot{Withheld: true}
	if persona.Valid {
		if err := json.Unmarshal([]byte(persona.String), &inherited.persona); err != nil {
			return continuationInheritance{}, err
		}
	}
	inherited.profile = PinnedProfileSnapshot{Withheld: true}
	if profile.Valid {
		if err := json.Unmarshal([]byte(profile.String), &inherited.profile); err != nil {
			return continuationInheritance{}, err
		}
	}
	fingerprint, err := backendegress.MemorySnapshotFingerprint(
		continuationMemoryPreimage(inherited.persona, inherited.profile, tools))
	if err != nil {
		return continuationInheritance{}, err
	}
	inherited.fingerprint = fingerprint
	return inherited, nil
}

// continuationMemoryPreimage rebuilds the preimage the parent's pins came
// from, bound to the continuation's tools.
func continuationMemoryPreimage(persona PinnedPersonaSnapshot, profile PinnedProfileSnapshot, tools []string) backendegress.MemorySnapshot {
	return backendegress.MemorySnapshot{
		PersonaID:           persona.PersonaID,
		PersonaDisplayName:  persona.DisplayName,
		PersonaBody:         persona.Body,
		PersonaContentHash:  persona.ContentHash,
		PersonaWithheld:     persona.Withheld,
		ProfileID:           profile.ProfileID,
		ProfileBody:         profile.Body,
		ProfileContentHash:  profile.ContentHash,
		ProfileWithheld:     profile.Withheld,
		MemoryToolsSelected: backendegress.SelectedToolsIncludeMemory(tools),
	}.Canonical()
}
