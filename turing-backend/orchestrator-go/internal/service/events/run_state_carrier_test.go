package events

import "testing"

// runStateCarrierEventTypes is the exact set this task derived from the
// repository's actual writers: every canonical type whose only committer
// merges a caller's payload with a genuinely committed RunState through
// marshalRunStatePayload.
//
//   - agent.run.queued, agent.run.started, agent.run.state_changed,
//     agent.run.completed, agent.run.failed, agent.run.cancelled all go
//     through the single generic transition committer
//     (repository.runTransition -> marshalRunStatePayload).
//   - approval.requested carries a RunState through two writers: primarily
//     the running -> waiting_approval transition itself (through that same
//     generic transition committer), and as a fallback
//     (appendApprovalRunStateEventTx) when the run was already waiting on an
//     earlier approval so this request does not itself move the lifecycle.
//     approval.approved never moves a run's lifecycle at all — approving
//     only records a decision, a separate resume moves the run — so
//     appendApprovalRunStateEventTx is its only writer. approval.denied,
//     approval.expired, and approval.consumed all go through
//     appendApprovalLifecycleEventTx (or its terminal-projection sibling)
//     instead, which never merges a RunState in at all, and are verified as
//     non-carriers below alongside every other type.
//
// This list is the test's own oracle, independent of whatever isRunStateCarrier
// happens to contain, so a change to the implementation's list is what this
// file is here to catch.
var runStateCarrierEventTypes = []string{
	"agent.run.queued",
	"agent.run.started",
	"agent.run.state_changed",
	"agent.run.completed",
	"agent.run.failed",
	"agent.run.cancelled",
	"approval.requested",
	"approval.approved",
}

// runStateNonCarrierEventTypes is every other canonical type this build knows
// about, plus one this build has never heard of. None of these has a writer
// that ever merges a RunState into its payload — a value under this event's
// own "runState" key was put there by something else: a worker, a hand edit, a
// restored backup, or a newer build.
var runStateNonCarrierEventTypes = []string{
	"message.started",
	"message.delta",
	"message.completed",
	"agent.run.step",
	"tool.call.started",
	"tool.call.completed",
	"tool.call.failed",
	"tool.call.denied",
	"approval.denied",
	"approval.expired",
	"approval.consumed",
	"error",
	"system",
	"session.updated",
	"session.deleted",
	"an.unknown.type.this.build.never.wrote",
}

// TestRunStateCarrierOracleCoversEveryCanonicalType pins the two lists above
// against eventTypes itself — the one place a durable type name is written
// down — rather than trusting they happen to enumerate the same 23 names this
// file was written against.
//
// Without this, adding a brand new canonical event type to eventTypes (a
// future writer, a new lifecycle projection) would silently fall through
// every table above: neither oracle list mentions it, so
// TestDecodeProjectsTypedRunStateOnlyForCarrierTypes never exercises it at
// all, and whichever way isRunStateCarrier happens to classify it — right or
// wrong — passes unnoticed. Requiring every real canonical name to appear in
// exactly one of the two lists means a new type has to be deliberately
// classified here before this test suite can pass again, which is exactly the
// moment its writer should be asked whether it actually commits a RunState.
func TestRunStateCarrierOracleCoversEveryCanonicalType(t *testing.T) {
	classified := make(map[string]string, len(runStateCarrierEventTypes)+len(runStateNonCarrierEventTypes))
	for _, eventType := range runStateCarrierEventTypes {
		if existing, already := classified[eventType]; already {
			t.Fatalf("%s classified as both %s and carrier", eventType, existing)
		}
		classified[eventType] = "carrier"
	}
	for _, eventType := range runStateNonCarrierEventTypes {
		if existing, already := classified[eventType]; already {
			t.Fatalf("%s classified as both %s and non-carrier", eventType, existing)
		}
		classified[eventType] = "non-carrier"
	}
	for canonical := range eventTypes {
		if _, ok := classified[canonical]; !ok {
			t.Errorf("canonical type %q is unclassified: add it to runStateCarrierEventTypes or runStateNonCarrierEventTypes", canonical)
		}
	}
	if len(classified) != len(eventTypes)+1 {
		t.Fatalf("classified %d types (want every canonical type plus the one deliberately unknown one = %d)",
			len(classified), len(eventTypes)+1)
	}
}

// wellFormedSelfNamingRunState is a snapshot that names the row's own run and
// uses only words this build actually recognizes — "completed"/"none" rather
// than gibberish. It is deliberately NOT the "unknown word" shape the older
// hostile-snapshot tests exercise: a non-carrier must refuse this even though
// nothing about its content looks malformed, because the only thing that
// disqualifies it is which event type is carrying it.
func wellFormedSelfNamingRunState(runID string) string {
	return `{"jobId":"job_1","attempt":1,"runState":{` +
		`"runId":"` + runID + `",` +
		`"userMessageId":"msg_user",` +
		`"assistantMessageId":"msg_assistant",` +
		`"lifecycle":"completed",` +
		`"outcomeReason":"none",` +
		`"stateVersion":999,` +
		`"stateUpdatedAt":"2026-08-20T00:00:00.000000000Z",` +
		`"hasDisplayableContent":true}}`
}

// TestDecodeProjectsTypedRunStateOnlyForCarrierTypes is the RED test for this
// task's defect: every canonical type used to read a self-naming, valid-shaped
// runState snapshot straight into the typed field, regardless of whether that
// event's own writer ever commits one. A legacy row of any non-authoritative
// type — a tool call, a message delta, a system notice, an unrecognized type —
// could therefore carry a forged snapshot with an inflated stateVersion and
// have it published as canonical truth.
//
// Every carrier type must still project the snapshot (unaffected by this
// fix); every non-carrier type must now project nothing at all, even though
// the snapshot here is fully well-formed and would have projected a real,
// recognized lifecycle/outcome pair had it been trusted.
func TestDecodeProjectsTypedRunStateOnlyForCarrierTypes(t *testing.T) {
	const runID = "run_carrier_control"
	payload := wellFormedSelfNamingRunState(runID)

	t.Run("carriers", func(t *testing.T) {
		for _, eventType := range runStateCarrierEventTypes {
			t.Run(eventType, func(t *testing.T) {
				safe := Decode(eventType, runID, payload)
				if safe.RunState == nil {
					t.Fatalf("%s: carrier type produced no RunState from a well-formed self-naming snapshot", eventType)
				}
				if safe.RunState.GetRunId() != runID {
					t.Fatalf("%s: RunState.RunId = %q, want %q", eventType, safe.RunState.GetRunId(), runID)
				}
				if safe.RunState.GetStateVersion() != 999 {
					t.Fatalf("%s: RunState.StateVersion = %d, want 999", eventType, safe.RunState.GetStateVersion())
				}
			})
		}
	})

	t.Run("non-carriers", func(t *testing.T) {
		for _, eventType := range runStateNonCarrierEventTypes {
			t.Run(eventType, func(t *testing.T) {
				safe := Decode(eventType, runID, payload)
				if safe.RunState != nil {
					t.Fatalf("%s: non-carrier type projected a typed RunState %+v from a payload it never authored",
						eventType, safe.RunState)
				}
				if _, republished := safe.Payload[runStatePayloadKey]; republished {
					t.Fatalf("%s: the refused snapshot survived in the public payload: %#v", eventType, safe.Payload)
				}
			})
		}
	})
}

// TestDecodeCarrierGateSurvivesAliasCanonicalization pins the fold-then-decide
// order the task calls out explicitly: an uppercase, underscored, or
// generated-enum spelling of a carrier type still resolves to a canonical name
// that IS a carrier, and the equivalent spelling of a non-carrier type still
// resolves to one that is NOT — so a row that only survived under an alternate
// spelling (a restored backup, an older writer that persisted the generated
// constant) cannot dodge, or wrongly gain, this gate by how it is spelled.
func TestDecodeCarrierGateSurvivesAliasCanonicalization(t *testing.T) {
	const runID = "run_alias"
	payload := wellFormedSelfNamingRunState(runID)

	carrierAliases := map[string]string{
		"AGENT_RUN_STARTED":                     "agent.run.started",
		"  agent_run_started  ":                 "agent.run.started",
		"TURING_EVENT_TYPE_AGENT_RUN_STARTED":   "agent.run.started",
		"Approval.Requested":                    "approval.requested",
		"APPROVAL_APPROVED":                     "approval.approved",
		"TURING_EVENT_TYPE_AGENT_RUN_CANCELLED": "agent.run.cancelled",
	}
	for alias, canonical := range carrierAliases {
		t.Run(alias, func(t *testing.T) {
			if CanonicalType(alias) != canonical {
				t.Fatalf("CanonicalType(%q) = %q, want %q", alias, CanonicalType(alias), canonical)
			}
			safe := Decode(alias, runID, payload)
			if safe.RunState == nil {
				t.Fatalf("alias %q of carrier %q produced no RunState", alias, canonical)
			}
		})
	}

	nonCarrierAliases := map[string]string{
		"SYSTEM":                              "system",
		"TOOL_CALL_STARTED":                   "tool.call.started",
		"TURING_EVENT_TYPE_TOOL_CALL_STARTED": "tool.call.started",
		"Approval.Denied":                     "approval.denied",
		"MESSAGE_COMPLETED":                   "message.completed",
	}
	for alias, canonical := range nonCarrierAliases {
		t.Run(alias, func(t *testing.T) {
			if CanonicalType(alias) != canonical {
				t.Fatalf("CanonicalType(%q) = %q, want %q", alias, CanonicalType(alias), canonical)
			}
			safe := Decode(alias, runID, payload)
			if safe.RunState != nil {
				t.Fatalf("alias %q of non-carrier %q projected a typed RunState %+v", alias, canonical, safe.RunState)
			}
		})
	}
}

// TestDecodeNonCarrierRunStepPreservesLegitimateNarration proves the fix is
// narrow: an agent.run.step row that never named itself a repository notice —
// a worker's own narration — keeps its content exactly as its writer left it.
// Only the typed RunState projection is what this task removes for
// non-carriers; the notice/narration split public_payload.go already applies
// is untouched.
func TestDecodeNonCarrierRunStepPreservesLegitimateNarration(t *testing.T) {
	safe := Decode("agent.run.step", "run_narration",
		`{"note":"reading the repository layout","reason":"context_budget"}`)
	if safe.RunState != nil {
		t.Fatalf("a plain narration step produced a RunState: %+v", safe.RunState)
	}
	if safe.Payload["note"] != "reading the repository layout" || safe.Payload["reason"] != "context_budget" {
		t.Fatalf("narration payload changed: %#v", safe.Payload)
	}
}
