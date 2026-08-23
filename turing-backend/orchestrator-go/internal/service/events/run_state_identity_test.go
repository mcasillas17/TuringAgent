package events

import (
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// snapshotPayload builds the payload shape a repository transition writes: the
// writer's own keys plus the canonical snapshot under runState.
func snapshotPayload(runID string, lifecycle string, outcome string) string {
	return `{"runState":{` +
		`"runId":"` + runID + `",` +
		`"userMessageId":"msg_user",` +
		`"assistantMessageId":"msg_assistant",` +
		`"lifecycle":"` + lifecycle + `",` +
		`"outcomeReason":"` + outcome + `",` +
		`"stateVersion":4,` +
		`"stateUpdatedAt":"2026-08-20T00:00:00.000000000Z",` +
		`"hasDisplayableContent":false}}`
}

// TestDecodeRejectsRunStateThatDoesNotNameItsOwnRow is the read half of the
// rule the runtime ingress enforces on the write side.
//
// A snapshot is only believable when it names the run whose event row carries
// it. A row is not proof of who wrote it — a payload can be restored from a
// backup, edited by hand, or written by a build that let a worker author the
// key — so the decoder checks identity itself rather than trusting that
// somebody upstream already did. The event type here is a legitimate
// repository lifecycle projection on purpose: the check may not depend on the
// type being suspicious.
func TestDecodeRejectsRunStateThatDoesNotNameItsOwnRow(t *testing.T) {
	tests := []struct {
		name          string
		eventRunID    string
		snapshotRunID string
		wantState     bool
	}{
		{name: "matching identity", eventRunID: "run_1", snapshotRunID: "run_1", wantState: true},
		{name: "empty snapshot run id", eventRunID: "run_1", snapshotRunID: "", wantState: false},
		{name: "foreign snapshot run id", eventRunID: "run_1", snapshotRunID: "run_2", wantState: false},
		{name: "row has no run id", eventRunID: "", snapshotRunID: "run_1", wantState: false},
		{name: "neither has a run id", eventRunID: "", snapshotRunID: "", wantState: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			safe := Decode("agent.run.state_changed", test.eventRunID,
				snapshotPayload(test.snapshotRunID, "running", "none"))
			if test.wantState {
				if safe.RunState == nil {
					t.Fatal("a snapshot that names its own row produced no state")
				}
				if safe.RunState.GetRunId() != test.eventRunID {
					t.Fatalf("state run id = %q, want %q", safe.RunState.GetRunId(), test.eventRunID)
				}
				return
			}
			if safe.RunState != nil {
				t.Fatalf("a snapshot naming %q was published on row %q as %+v",
					test.snapshotRunID, test.eventRunID, safe.RunState)
			}
			if _, carried := safe.Payload[runStatePayloadKey]; carried {
				t.Fatalf("rejected snapshot survived in the public payload: %#v", safe.Payload)
			}
		})
	}
}

// TestUnrecognizedRunStateWordsReachClientsAsDomainUnknown is the read
// boundary a client actually goes through, not a protobuf round trip.
//
// A row written by a newer server uses lifecycle and outcome words this build
// has no name for. The typed answer has to be the explicit domain unknown —
// "a phase this build cannot name" — and the stored words themselves must not
// reach the wire in any form: not as a payload key, not inside the rendered
// message.
func TestUnrecognizedRunStateWordsReachClientsAsDomainUnknown(t *testing.T) {
	h := newEventHarness(t)
	run := seedEventRun(t, h, "Future words")
	seeded := appendLegacyEvent(t, h, run, "agent.run.state_changed",
		snapshotPayload(run.runID, "hibernating", "sunspots"))

	safe := Decode("agent.run.state_changed", run.runID, seeded.PayloadJSON)
	if safe.RunState == nil {
		t.Fatal("a row this build cannot fully name produced no state at all")
	}
	if safe.RunState.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_UNKNOWN {
		t.Fatalf("decoded lifecycle = %v, want RUN_LIFECYCLE_UNKNOWN", safe.RunState.GetLifecycle())
	}
	if safe.RunState.GetOutcomeReason() != turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNKNOWN {
		t.Fatalf("decoded outcome = %v, want RUN_OUTCOME_REASON_UNKNOWN", safe.RunState.GetOutcomeReason())
	}

	public := listedEvent(t, h, run.sessionID, seeded.EventID)
	state := public.GetRunState()
	if state == nil {
		t.Fatal("EventService dropped the state of a row it could not fully name")
	}
	if state.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_UNKNOWN ||
		state.GetOutcomeReason() != turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNKNOWN {
		t.Fatalf("public state = %v/%v, want the explicit unknowns",
			state.GetLifecycle(), state.GetOutcomeReason())
	}
	if state.GetStateVersion() != 4 {
		t.Fatalf("public state version = %d, want the stored 4", state.GetStateVersion())
	}
	if _, carried := public.GetPayload().GetFields()[runStatePayloadKey]; carried {
		t.Fatalf("public payload republished the snapshot: %v", public.GetPayload())
	}
	rendered := public.String()
	for _, stored := range []string{"hibernating", "sunspots"} {
		if strings.Contains(rendered, stored) {
			t.Fatalf("public event rendered the stored word %q: %s", stored, rendered)
		}
	}
	// The enum's own numeric value is the other way a rejected word could
	// escape: a renderer that fell back to the integer would be publishing the
	// same unnamed thing in a shape a client cannot localize.
	if strings.Contains(rendered, "lifecycle:9999") || strings.Contains(rendered, "outcome_reason:9999") {
		t.Fatalf("public event rendered a raw enum number: %s", rendered)
	}
}
