package events

import (
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// typeSpellings are the shapes one durable event type can arrive in.
//
// A row is written by this build with the dotted durable name, but the same
// row can reach a reader spelled differently: restored from a backup taken
// while an older writer used the enum's own name, edited by hand, or written by
// a build that persisted the generated constant instead of the durable string.
// Every one of them names the same event, so every one of them has to be read
// as the same event — including by the allowlist that decides what leaves the
// process.
var typeSpellings = map[string]func(string) string{
	"durable": func(durable string) string { return durable },
	"underscored": func(durable string) string {
		return strings.ReplaceAll(durable, ".", "_")
	},
	"uppercased": func(durable string) string {
		return strings.ToUpper(strings.ReplaceAll(durable, ".", "_"))
	},
	"enum prefixed": func(durable string) string {
		return "TURING_EVENT_TYPE_" + strings.ToUpper(strings.ReplaceAll(durable, ".", "_"))
	},
	"mixed case dotted": func(durable string) string {
		return strings.ToUpper(durable[:1]) + durable[1:]
	},
}

// legacyFailurePayload is what an unmigrated failure-like row still holds: the
// provider's own sentence, a path off this machine, a tool call's arguments and
// the token that authorized one.
const legacyFailurePayload = `{"approvalId":"appr_1","toolCallId":"call_1","toolName":"system.shell","serverName":"system",` +
	`"code":"model_error","retryable":true,` +
	`"message":"connection refused by ollama at 127.0.0.1:11434",` +
	`"reason":"denied because this would email the whole company",` +
	`"detail":"/Users/someone/secrets/private.key",` +
	`"args":{"command":"rm -rf /Users/someone"},"approvalToken":"******"}`

// publicFailureInventory is every durable type whose payload the boundary
// reduces to an allowlist, paired with the public type it is published as and
// the single category that type is allowed to carry.
var publicFailureInventory = []struct {
	durable      string
	publicType   turingv1.TuringEventType
	wantCategory string
	wantEmpty    bool
}{
	{
		durable:    "agent.run.failed",
		publicType: turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED,
		wantEmpty:  true,
	},
	{
		durable:    "agent.run.cancelled",
		publicType: turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_CANCELLED,
		wantEmpty:  true,
	},
	{
		durable:      "approval.denied",
		publicType:   turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED,
		wantCategory: "policy_denied",
	},
	{
		durable:      "approval.expired",
		publicType:   turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_EXPIRED,
		wantCategory: "expired",
	},
	{
		durable:      "tool.call.failed",
		publicType:   turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
		wantCategory: "tool_failure",
	},
	{
		durable:      "tool.call.denied",
		publicType:   turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED,
		wantCategory: "policy_denied",
	},
}

// TestEveryTypeSpellingTakesTheSameFailureSanitizer is the rule the two halves
// of this boundary have to agree on: whatever spelling a row's type arrived in,
// the type a client is told and the allowlist the payload went through must be
// derived from the same answer.
//
// They were derived from two answers. The type mapper folded case, the enum
// prefix and underscores before switching, so an uppercased row was published
// as AGENT_RUN_FAILED — while the payload allowlist switched on the raw string,
// missed it, and published the provider's sentence with it.
func TestEveryTypeSpellingTakesTheSameFailureSanitizer(t *testing.T) {
	for _, entry := range publicFailureInventory {
		for spelling, rewrite := range typeSpellings {
			t.Run(entry.durable+" "+spelling, func(t *testing.T) {
				h := newEventHarness(t)
				run := seedEventRun(t, h, entry.durable+" "+spelling)
				stored := rewrite(entry.durable)
				seeded := appendLegacyEvent(t, h, run, stored, legacyFailurePayload)
				public := listedEvent(t, h, run.sessionID, seeded.EventID)

				if public.GetType() != entry.publicType {
					t.Fatalf("row stored as %q published as %v, want %v", stored, public.GetType(), entry.publicType)
				}
				assertNoRawDiagnostics(t, public.GetPayload())
				if entry.wantEmpty {
					if len(public.GetPayload().GetFields()) != 0 {
						t.Fatalf("row stored as %q published the payload %s", stored, public.GetPayload())
					}
					return
				}
				if got := public.GetPayload().GetFields()["category"].GetStringValue(); got != entry.wantCategory {
					t.Fatalf("row stored as %q published category %q, want %q",
						stored, got, entry.wantCategory)
				}
			})
		}
	}
}

// TestEveryTypeSpellingPublishesTheSameEventType walks the whole public type
// inventory rather than the failure half, because the mapper a client's
// rendering depends on has to answer identically for every spelling too.
func TestEveryTypeSpellingPublishesTheSameEventType(t *testing.T) {
	durableTypes := map[string]turingv1.TuringEventType{
		"message.started":         turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED,
		"message.delta":           turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		"message.completed":       turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED,
		"agent.run.queued":        turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_QUEUED,
		"agent.run.started":       turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STARTED,
		"agent.run.step":          turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
		"agent.run.completed":     turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_COMPLETED,
		"agent.run.failed":        turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED,
		"agent.run.cancelled":     turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_CANCELLED,
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
	}
	for durable, want := range durableTypes {
		for spelling, rewrite := range typeSpellings {
			stored := rewrite(durable)
			if got := MapEventType(stored); got != want {
				t.Fatalf("%s spelled %q mapped to %v, want %v", spelling, stored, got, want)
			}
			if got := CanonicalType(stored); got != durable {
				t.Fatalf("%s spelled %q canonicalized to %q, want %q", spelling, stored, got, durable)
			}
		}
	}
}

// publicFailureTypes is the set of public values a client renders as an outcome
// whose details it is deliberately not told. They are written as the enum
// values rather than the durable names because the rule below is about what a
// client is told, so the set it quantifies over has to be the one a client
// actually sees.
var publicFailureTypes = map[turingv1.TuringEventType]bool{
	turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED:    true,
	turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_CANCELLED: true,
	turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED:     true,
	turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_EXPIRED:    true,
	turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED:    true,
	turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED:    true,
}

// TestDecodeSanitizesWhateverTheTypeMapperCallsAFailure states the coupling
// itself rather than one more list of examples.
//
// The rule is that a row a client is told is a failure has had its payload
// through the failure allowlist — for every string, not for the handful an
// inventory happens to name. So the subject is quantified out of this build's
// own vocabulary and asked of the public mapper: whatever MapEventType calls a
// failure, Decode has to have sanitized. A future type added to the table is
// covered the day it is added, and a second normalization reintroduced beside
// the shared one fails here even if every inventory beside it was updated.
func TestDecodeSanitizesWhateverTheTypeMapperCallsAFailure(t *testing.T) {
	var checked int
	for durable := range eventTypes {
		for _, rewrite := range typeSpellings {
			stored := rewrite(durable)
			if !publicFailureTypes[MapEventType(stored)] {
				continue
			}
			checked++
			payload := Decode(stored, legacyFailurePayload).Payload
			for _, key := range forbiddenPayloadKeys {
				if _, exists := payload[key]; exists {
					t.Fatalf("%q is published as a failure but kept the diagnostic key %q: %v",
						stored, key, payload)
				}
			}
			assertNoRawDiagnosticValue(t, payload)
		}
	}
	// Every failure type in every spelling, so a fold that silently stopped
	// recognizing one would show up as a shortfall here rather than as a run
	// that quietly checked nothing.
	if want := len(publicFailureTypes) * len(typeSpellings); checked != want {
		t.Fatalf("checked %d failure spellings, want %d", checked, want)
	}
}

// TestUnrecognizedEventTypesAreUnspecifiedAndUncanonical pins the other side:
// a type this build has never heard of gets the unspecified value and no
// canonical name, so nothing downstream can mistake it for a governed one.
func TestUnrecognizedEventTypesAreUnspecifiedAndUncanonical(t *testing.T) {
	for _, unknown := range []string{
		"", "   ", "agent.run.hibernated", "TURING_EVENT_TYPE_AGENT_RUN_HIBERNATED",
		"agent.run", "turing_event_type_", "agent.run.failed.extra",
	} {
		if got := MapEventType(unknown); got != turingv1.TuringEventType_TURING_EVENT_TYPE_UNSPECIFIED {
			t.Fatalf("unknown type %q mapped to %v, want unspecified", unknown, got)
		}
		if got := CanonicalType(unknown); got != "" {
			t.Fatalf("unknown type %q canonicalized to %q, want no canonical name", unknown, got)
		}
	}
}
