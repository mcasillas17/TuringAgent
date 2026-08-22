package events

import (
	"database/sql"
	"encoding/json"
	"math"
	"slices"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/safejson"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runstate"
)

// SafeEvent is one durable event row as a client may see it: an allowlisted
// payload and the canonical run state, or neither.
//
// Both services build it through the same function on purpose. Migration
// already rewrote the failure-like payloads this product wrote before TUR-009,
// and the live writers now normalize before they write — but a database can be
// restored from a backup taken mid-migration, edited by hand, or written by a
// build that is not this one. This is the boundary that decides what leaves the
// process, and there is exactly one of it so ChatService and EventService
// cannot come to different conclusions about the same row.
type SafeEvent struct {
	// Payload is the allowlisted public payload. It is never nil, so a caller
	// cannot accidentally distinguish "unreadable" from "empty" and publish the
	// difference.
	Payload map[string]any
	// RunState is the canonical projection carried by lifecycle events, or nil
	// when the row has none this build can vouch for. Absence is a real answer:
	// a client renders it as "no state was recorded" rather than as an outcome.
	RunState *turingv1.RunState
}

// runStatePayloadKey is where a lifecycle event carries its committed snapshot.
// It is read into the typed RunState above and then dropped from the public
// payload: the typed field is a closed vocabulary a client can localize, while
// the stored strings are this server's internal words for the same thing.
const runStatePayloadKey = "runState"

// runStateCarrierTypes is the exact, closed set of canonical event types whose
// writer is the repository's own guarded transition core — the only code that
// ever merges a genuinely committed RunState into an event's payload under
// runStatePayloadKey.
//
//   - agent.run.queued, agent.run.started, agent.run.state_changed,
//     agent.run.completed, agent.run.failed, and agent.run.cancelled are the
//     event types the single generic transition committer
//     (repository's runTransition, via marshalRunStatePayload) ever appends.
//   - approval.requested carries a RunState through two different writers,
//     never through neither: its primary path IS the running ->
//     waiting_approval transition itself (awaitApprovalTransitionTx, through
//     the same generic transition committer above), and
//     appendApprovalRunStateEventTx is only its fallback — used when the run
//     was already waiting on an earlier approval, so this request does not
//     itself move the lifecycle but still owes the same announcement.
//     approval.approved never moves a run's lifecycle at all (approving only
//     records a decision; ResumeApprovedRun is what resumes the run), so
//     appendApprovalRunStateEventTx is its only writer.
//   - approval.denied, approval.expired, and approval.consumed are
//     deliberately absent: every one of them is written by
//     appendApprovalLifecycleEventTx (or its terminal-projection sibling)
//     instead, which never merges a RunState in at all.
//
// Every other type — every message.*, every tool.call.*, agent.run.step,
// system, error, session.*, and anything this build does not recognize — is
// not a carrier. A row of one of those types may still contain a
// self-naming, valid-shaped value under runStatePayloadKey (a worker that
// tried to forge one, a hand edit, a restored backup, a future build's own
// convention), but nothing about its own writer ever committed it, so it is
// never projected into the typed field below — no matter how well-formed it
// looks.
var runStateCarrierTypes = map[string]bool{
	"agent.run.queued":        true,
	"agent.run.started":       true,
	"agent.run.state_changed": true,
	"agent.run.completed":     true,
	"agent.run.failed":        true,
	"agent.run.cancelled":     true,
	"approval.requested":      true,
	"approval.approved":       true,
}

// isRunStateCarrier reports whether canonicalType is one this build's own
// repository writers ever commit a RunState snapshot under. It takes the
// already-resolved canonical name — never the stored string — for the same
// reason publicPayload does: a row cannot dodge, or wrongly gain, this gate by
// which spelling of its type it happens to carry.
func isRunStateCarrier(canonicalType string) bool {
	return runStateCarrierTypes[canonicalType]
}

// approvalIdentityKeys and ToolCallIdentityKeys are the only payload keys a
// public failure event may carry besides its allowlisted category. They are the
// identities the contract already promises a client — the approval it was asked
// to decide, the tool call it watched start. Everything else is dropped rather
// than renamed, so an unmigrated row cannot smuggle its old diagnostic under a
// new name.
var approvalIdentityKeys = []string{"approvalId", "toolCallId", "toolName", "runId", "traceId", "modelToolCallId"}

// ToolCallIdentityKeys is exported so runtime ingress can build the same
// bounded identity into a worker-authored TOOL_CALL_STARTED/TOOL_CALL_FAILED
// payload before it is ever persisted, rather than keeping a second copy of
// this list that could drift from what the public boundary allows.
var ToolCallIdentityKeys = []string{"toolCallId", "toolName", "serverName", "modelToolCallId"}

// executionOnlyKeys are payload keys that identify who is executing a run
// rather than what happened to it. A client has no business knowing which
// worker or which assignment attempt holds a run, and the durable log keeps
// them for recovery, so they are dropped on the way out.
var executionOnlyKeys = []string{"assignmentAttemptId", "workerId", "leaseOwner", "executionState"}

// The bounded retry/recovery projection's keys — category, attempt, attempts,
// maxAttempts, stateVersion — form the vocabulary only the repository can
// anchor to a committed state version. A generic worker step may carry its
// own note and reason, but never those. That vocabulary lives once in
// runoutcome (HasReservedRetryNoticeKey, DeleteReservedRetryNoticeKeys) so
// this boundary and the migration's rewrite ask the identical question
// instead of keeping two copies of the same list.

// Decode reads one durable event row as the public contract allows it to be
// read.
//
// A payload this build cannot parse yields an empty payload and no state.
// Returning the parser's own sentence is what the old mapper did, and a parser
// message is built from the bytes it failed on — which is precisely the content
// this boundary exists to keep in the database.
//
// The type is resolved through CanonicalType exactly once, so the allowlist
// below, the carrier gate, and the public type a client is told are three
// consequences of one answer. Deciding them separately is how a row stored as
// AGENT_RUN_FAILED came to be published as a failure whose payload had never
// seen the failure allowlist.
//
// eventRunID is the run the row itself belongs to, and a snapshot is only read
// when it names that run. The identity is taken from the row rather than from
// the payload because the payload is the part a writer controls. But naming
// the right run is not enough on its own: only isRunStateCarrier's types ever
// have a writer that actually commits a RunState, so every other type's
// payload is never even offered to runStateFrom, regardless of what it
// contains under runStatePayloadKey. A non-carrier row that names the right
// run with a perfectly well-formed snapshot is still not proof anyone
// authoritative wrote it.
func Decode(eventType string, eventRunID string, payloadJSON string) SafeEvent {
	payload, err := decodeEventPayload(payloadJSON)
	if err != nil {
		return SafeEvent{Payload: map[string]any{}}
	}
	canonicalType := CanonicalType(eventType)
	var state *turingv1.RunState
	if isRunStateCarrier(canonicalType) {
		state = runStateFrom(eventRunID, payload)
	}
	return SafeEvent{Payload: publicPayload(canonicalType, payload), RunState: state}
}

// StripRepositoryAuthoredEventFields removes projections from an event whose
// author is not the repository — today, a worker's generic runtime event.
//
// Canonical run state and retry/recovery notices are records of transitions the
// repository committed. A worker that could attach either would be able to tell
// a client the run failed or exhausted retries at a version the database never
// committed. These fields are dropped at ingress rather than rejecting the
// event so legitimate worker narration still reaches the log.
func StripRepositoryAuthoredEventFields(event *turingv1.TuringEvent) {
	if event == nil {
		return
	}
	event.RunState = nil
	payload := event.GetPayload()
	if payload == nil {
		return
	}
	delete(payload.GetFields(), runStatePayloadKey)
	if event.GetType() != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
		return
	}
	runoutcome.DeleteReservedRetryNoticeKeys(payload.GetFields())
}

func decodeEventPayload(payloadJSON string) (map[string]any, error) {
	if strings.TrimSpace(payloadJSON) == "" {
		return map[string]any{}, nil
	}
	return safejson.DecodeObject(json.NewDecoder(strings.NewReader(payloadJSON)))
}

// publicPayload applies the allowlist for the event types whose payloads are
// failure-like or carry execution identity. Every other type keeps the payload
// its writer built, minus the canonical snapshot, because those are the
// governed projections this product deliberately shows: an assistant's own
// output, the egress warning, an approval request's argument summary.
//
// It takes the canonical type rather than the stored string, so a row cannot
// dodge its allowlist by being spelled differently from the name this build
// writes.
func publicPayload(canonicalType string, payload map[string]any) map[string]any {
	switch canonicalType {
	case "agent.run.failed", "agent.run.cancelled":
		// The canonical state says everything a client is allowed to learn
		// about a terminal outcome, and it travels in the typed field.
		return map[string]any{}
	case "agent.run.step":
		return publicRunStep(payload)
	case "approval.denied", "approval.expired":
		return identityPayload(payload, approvalIdentityKeys, approvalCategory(canonicalType))
	case "tool.call.failed", "tool.call.denied":
		return identityPayload(payload, ToolCallIdentityKeys, toolCallCategory(canonicalType))
	default:
		return withoutInternalKeys(payload)
	}
}

// withoutInternalKeys copies a payload that is otherwise published as written,
// dropping the snapshot the typed field carries and the execution identities no
// client may see.
func withoutInternalKeys(payload map[string]any) map[string]any {
	public := make(map[string]any, len(payload))
	for key, value := range payload {
		if key == runStatePayloadKey {
			continue
		}
		if slices.Contains(executionOnlyKeys, key) {
			continue
		}
		public[key] = value
	}
	return public
}

// identityPayload keeps the intended structural IDs and adds the one category
// the event type itself determines. The category is read off the type the
// server chose, never off a code a provider or a tool influenced, so a
// rewritten row and a freshly written one are indistinguishable here.
func identityPayload(payload map[string]any, identityKeys []string, category runoutcome.Reason) map[string]any {
	public := make(map[string]any, len(identityKeys)+1)
	for _, key := range identityKeys {
		text, ok := payload[key].(string)
		if !ok || text == "" {
			continue
		}
		public[key] = text
	}
	public["category"] = string(category)
	return public
}

func approvalCategory(eventType string) runoutcome.Reason {
	if category, ok := runoutcome.ApprovalFailureCategory(eventType); ok {
		return category
	}
	return runoutcome.ReasonPolicyDenied
}

func toolCallCategory(eventType string) runoutcome.Reason {
	if category, ok := runoutcome.ToolCallFailureCategory(eventType); ok {
		return category
	}
	return runoutcome.ReasonToolFailure
}

// publicRunStep separates the two things that share this event type. A retry,
// a recovery retry, and a give-up are failure projections and are reduced to a
// category and bounded counters. Everything else — the egress warning, the
// model limit notice — is the product telling the user something true about
// their own run, and is published as written.
func publicRunStep(payload map[string]any) map[string]any {
	category, attempt, maxAttempts, failureLike := runStepNotice(payload)
	if !failureLike {
		return withoutInternalKeys(payload)
	}
	public := map[string]any{"category": string(category)}
	// Published only when a writer actually supplied one. Zero is protobuf
	// absence, so publishing it would read as a version older than every
	// stored one rather than as "no version known".
	if version, ok := payloadInt64(payload, "stateVersion"); ok && version >= 1 {
		public["stateVersion"] = version
	}
	// Counters survive only if they pass the notice constructor's bounds. A row
	// with an impossible budget keeps its category and loses the numbers rather
	// than publishing an unbounded count.
	if notice, err := runoutcome.NewStepNotice(category, attempt, maxAttempts); err == nil {
		public["attempt"] = int64(notice.Attempt())
		public["maxAttempts"] = int64(notice.MaxAttempts())
	}
	return public
}

// runStepNotice recognizes a failure-like notice from either shape: the
// normalized one this build writes, and the legacy one a row written before the
// migration still has.
//
// Which of these two shapes a row is attempting is decided by which reserved
// retry/recovery notice key (runoutcome.HasReservedRetryNoticeKey) it carries
// at all, never by whether those keys happen to parse. Gating on a successful
// parse used to mean a malformed counter — a string, or a value outside this
// build's vocabulary — read as "this key was never here", and a row that
// named itself a retry fell through to the pass-through arm below with its
// raw note and reason intact. A row carrying any reserved key is claiming to
// be a repository-authored notice, and only the repository ever writes them,
// so that claim is honored by resolving to a bounded typed notice — never by
// republishing what came with it.
func runStepNotice(payload map[string]any) (runoutcome.NoticeCategory, int32, int32, bool) {
	maxAttempts, _ := payloadInt32(payload, "maxAttempts")
	if stored, ok := payload["category"].(string); ok {
		category := runoutcome.NoticeCategory(stored)
		switch category {
		case runoutcome.NoticeDispatchRetry, runoutcome.NoticeRecoveryRetry, runoutcome.NoticeRecoveryExhausted:
			attempt, _ := payloadInt32(payload, "attempt")
			return category, attempt, maxAttempts, true
		}
	}
	if !runoutcome.HasReservedRetryNoticeKey(payload) {
		return "", 0, 0, false
	}
	_, hasAttempts := payload["attempts"]
	attempt, _ := payloadInt32(payload, "attempt")
	attempts, _ := payloadInt32(payload, "attempts")
	switch {
	case hasAttempts:
		return runoutcome.NoticeRecoveryExhausted, attempts, maxAttempts, true
	case payloadText(payload, "reason") == "worker_unavailable":
		return runoutcome.NoticeRecoveryRetry, attempt, maxAttempts, true
	default:
		return runoutcome.NoticeDispatchRetry, attempt, maxAttempts, true
	}
}

// runStateFrom projects the committed snapshot a lifecycle event carries.
//
// It reads the stored strings into the canonical durable shape and hands them
// to the one projection every reader uses, so an event and a reopened message
// cannot disagree about the same run. A value this build does not recognize
// becomes the explicit unknown there; a snapshot with no identity or no version
// becomes no state at all.
//
// The snapshot must name the run whose row carries it. Only the repository's
// transition writers author these, and they write the run they just moved — so
// a snapshot that names nothing, or names some other run, was authored by
// something that had no business authoring one, and there is no reading of it
// that a client should be told.
func runStateFrom(eventRunID string, payload map[string]any) *turingv1.RunState {
	snapshot, ok := payload[runStatePayloadKey].(map[string]any)
	if !ok {
		return nil
	}
	snapshotRunID := payloadText(snapshot, "runId")
	if snapshotRunID == "" || snapshotRunID != eventRunID {
		return nil
	}
	version, _ := payloadInt64(snapshot, "stateVersion")
	state := repository.RunState{
		RunID:                 snapshotRunID,
		UserMessageID:         payloadText(snapshot, "userMessageId"),
		AssistantMessageID:    payloadText(snapshot, "assistantMessageId"),
		Lifecycle:             payloadText(snapshot, "lifecycle"),
		OutcomeReason:         payloadText(snapshot, "outcomeReason"),
		StateVersion:          version,
		StateUpdatedAt:        payloadText(snapshot, "stateUpdatedAt"),
		HasDisplayableContent: payloadBool(snapshot, "hasDisplayableContent"),
	}
	if finished := payloadText(snapshot, "finishedAt"); finished != "" {
		state.FinishedAt = sql.NullString{String: finished, Valid: true}
	}
	return runstate.Project(state)
}

func payloadText(payload map[string]any, key string) string {
	text, _ := payload[key].(string)
	return text
}

func payloadBool(payload map[string]any, key string) bool {
	value, ok := payload[key].(bool)
	return ok && value
}

// payloadInt64 accepts both shapes a decoded payload can hold: safejson decodes
// with UseNumber, while a value that arrived through a protobuf struct is a
// float64.
func payloadInt64(payload map[string]any, key string) (int64, bool) {
	switch value := payload[key].(type) {
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || value < math.MinInt64 || value > math.MaxInt64 {
			return 0, false
		}
		if value != math.Trunc(value) {
			// A fractional value is not a counter any writer this build
			// produces: truncating it (2.9 -> 2) would fabricate a
			// plausible-looking count instead of surfacing that the stored
			// value was never a real one.
			return 0, false
		}
		return int64(value), true
	case int64:
		return value, true
	default:
		return 0, false
	}
}

func payloadInt32(payload map[string]any, key string) (int32, bool) {
	value, ok := payloadInt64(payload, key)
	if !ok {
		return 0, false
	}
	if value < math.MinInt32 || value > math.MaxInt32 {
		// Present but unusable: the caller learns the key was there, which is
		// what distinguishes a failure-like notice from a governed one, and the
		// out-of-range number is dropped rather than truncated into a plausible
		// count.
		return 0, true
	}
	return int32(value), true
}
