package events

import (
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// eventTypes is the one place a durable event type name is written down, and
// the one place it is paired with the public value a client is told.
//
// The keys are the strings writers actually persist. Everything that has to
// reason about "which event is this" — the payload allowlist, ChatService's
// union dispatch, both public type mappers — resolves through this table, so
// they cannot come to different conclusions about the same row. They did: the
// type mappers folded case, the generated enum's prefix and underscores before
// switching, while the payload allowlist switched on the raw string. A row
// stored as AGENT_RUN_FAILED was therefore published as a failure whose payload
// had never been through the failure allowlist.
var eventTypes = map[string]turingv1.TuringEventType{
	"message.started":     turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED,
	"message.delta":       turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
	"message.completed":   turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED,
	"agent.run.queued":    turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_QUEUED,
	"agent.run.started":   turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STARTED,
	"agent.run.step":      turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
	"agent.run.completed": turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_COMPLETED,
	"agent.run.failed":    turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED,
	"agent.run.cancelled": turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_CANCELLED,
	// The durable name keeps an underscore inside its last segment, which is
	// exactly why folding underscores to dots is not by itself enough to
	// recognize a row: the folded spelling has to be resolved back to the name
	// writers persist before anything switches on it.
	"agent.run.state_changed": turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED,
	"tool.call.started":       turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED,
	"tool.call.completed":     turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
	"tool.call.failed":        turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
	"tool.call.denied":        turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED,
	"approval.requested":      turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED,
	"approval.approved":       turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_APPROVED,
	"approval.denied":         turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED,
	"approval.expired":        turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_EXPIRED,
	"approval.consumed":       turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_CONSUMED,
	"error":                   turingv1.TuringEventType_TURING_EVENT_TYPE_ERROR,
	"system":                  turingv1.TuringEventType_TURING_EVENT_TYPE_SYSTEM,
	"session.updated":         turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_UPDATED,
	"session.deleted":         turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_DELETED,
}

// canonicalByFoldedType indexes the same table by the spelling-insensitive form
// below, so a row whose type survived a backup, a hand edit, or an older writer
// that persisted the generated constant still resolves to the durable name.
var canonicalByFoldedType = buildCanonicalIndex()

func buildCanonicalIndex() map[string]string {
	index := make(map[string]string, len(eventTypes))
	for canonical := range eventTypes {
		index[foldEventType(canonical)] = canonical
	}
	return index
}

// foldEventType erases the differences that are spelling rather than meaning:
// surrounding blanks, case, the generated enum's prefix, and the underscore a
// serializer writes where this product writes a dot.
func foldEventType(value string) string {
	folded := strings.ToLower(strings.TrimSpace(value))
	folded = strings.TrimPrefix(folded, "turing_event_type_")
	return strings.ReplaceAll(folded, "_", ".")
}

// CanonicalType resolves one stored type onto the durable name this build
// reasons about, or reports "" for a name it has never heard of.
//
// Absence is deliberate: an unrecognized type must not resolve to a plausible
// neighbour, because every allowlist decision downstream keys off this answer.
func CanonicalType(value string) string {
	return canonicalByFoldedType[foldEventType(value)]
}

// MapEventType is the public type both services publish. An unrecognized name
// yields the unspecified value, which is what a client renders as "an event
// this build cannot name" rather than as some other event.
func MapEventType(value string) turingv1.TuringEventType {
	return eventTypes[CanonicalType(value)]
}
