package events

import (
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// TestStoredCategoryNeverOverridesTheTypeDerivedOne walks the whole category
// inventory with a row that labels itself something else.
//
// The category a public failure carries is read off the event type this server
// chose, never off a word in the payload. A payload word is influenced by
// whatever produced the row — an old build, a restored backup, a hand edit —
// and a reader that trusted it could be told a refused approval was an
// uncertain side effect, which is a different fact about the user's data.
func TestStoredCategoryNeverOverridesTheTypeDerivedOne(t *testing.T) {
	// Every value here is a real member of one of the two closed vocabularies,
	// so the assertion is about where the answer comes from rather than about
	// rejecting nonsense.
	hostileCategories := []string{
		"side_effect_uncertain", "tool_failure", "policy_denied", "expired",
		"internal_failure", "completed_no_content", "user_cancelled",
		"dispatch_retry", "recovery_exhausted", "", "not_a_category",
	}
	typeDerived := map[string]string{
		"approval.denied":  "policy_denied",
		"approval.expired": "expired",
		"tool.call.failed": "tool_failure",
		"tool.call.denied": "policy_denied",
	}
	for eventType, want := range typeDerived {
		for _, hostile := range hostileCategories {
			t.Run(eventType+" says "+hostile, func(t *testing.T) {
				h := newEventHarness(t)
				run := seedEventRun(t, h, eventType)
				seeded := appendLegacyEvent(t, h, run, eventType,
					`{"approvalId":"appr_1","toolCallId":"call_1","toolName":"system.shell",`+
						`"serverName":"system","category":"`+hostile+`",`+
						`"message":"connection refused by ollama at 127.0.0.1:11434"}`)
				public := listedEvent(t, h, run.sessionID, seeded.EventID)
				assertNoRawDiagnostics(t, public.GetPayload())
				if got := public.GetPayload().GetFields()["category"].GetStringValue(); got != want {
					t.Fatalf("%s storing category %q published %q, want the type-derived %q",
						eventType, hostile, got, want)
				}
			})
		}
	}
}

// TestRunStepCategoryComesFromTheNoticeShapeNotAStoredWord is the run-step half
// of the same rule. A step event has no type-derived category, so the shape of
// the notice decides — and a stored word outside the notice vocabulary is
// replaced rather than republished.
func TestRunStepCategoryComesFromTheNoticeShapeNotAStoredWord(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "retry labelled as a terminal outcome",
			payload: `{"category":"side_effect_uncertain","attempt":2,"maxAttempts":3}`,
			want:    "dispatch_retry",
		},
		{
			name:    "recovery retry labelled as a denial",
			payload: `{"category":"policy_denied","reason":"worker_unavailable","attempt":1,"maxAttempts":3}`,
			want:    "recovery_retry",
		},
		{
			name:    "give up mislabelled with a word this build never writes",
			payload: `{"category":"not_a_category","attempts":3,"maxAttempts":3}`,
			want:    "recovery_exhausted",
		},
		{
			name:    "unknown word with a retry shape",
			payload: `{"category":"not_a_category","attempt":2,"maxAttempts":3}`,
			want:    "dispatch_retry",
		},
	}
	// Whatever the row said, the published word is one of the three this
	// product's notice vocabulary contains.
	allowed := map[string]bool{"dispatch_retry": true, "recovery_retry": true, "recovery_exhausted": true}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newEventHarness(t)
			run := seedEventRun(t, h, test.name)
			seeded := appendLegacyEvent(t, h, run, "agent.run.step", test.payload)
			public := listedEvent(t, h, run.sessionID, seeded.EventID)
			got := public.GetPayload().GetFields()["category"].GetStringValue()
			if !allowed[got] {
				t.Fatalf("published category %q, which is outside the notice vocabulary", got)
			}
			if got != test.want {
				t.Fatalf("published category %q, want %q (%s)", got, test.want, public.GetPayload())
			}
		})
	}
}

// TestPublicReadsDropEveryExecutionOnlyKey covers the keys that name who is
// executing a run rather than what happened to it.
//
// One case per key, on the arms that publish a writer's payload as written, so
// dropping any single key from the internal list fails here rather than
// quietly widening what a client can see. The live claim really does write an
// assignment attempt into its own start event, so this is not hypothetical.
func TestPublicReadsDropEveryExecutionOnlyKey(t *testing.T) {
	passThroughTypes := []string{
		"agent.run.started", "agent.run.queued", "agent.run.state_changed",
		"agent.run.completed", "agent.run.step", "message.started",
		"approval.requested", "tool.call.started", "system",
	}
	secrets := map[string]string{
		"assignmentAttemptId": "attempt_7",
		"workerId":            "worker-1",
		"leaseOwner":          "worker-1-lease",
		"executionState":      "pending_send",
	}
	for _, eventType := range passThroughTypes {
		for key, value := range secrets {
			t.Run(eventType+" carrying "+key, func(t *testing.T) {
				h := newEventHarness(t)
				run := seedEventRun(t, h, eventType+key)
				seeded := appendLegacyEvent(t, h, run, eventType,
					`{"status":"running","`+key+`":"`+value+`"}`)
				public := listedEvent(t, h, run.sessionID, seeded.EventID)
				if _, exists := public.GetPayload().GetFields()[key]; exists {
					t.Fatalf("%s published the execution key %q: %s", eventType, key, public.GetPayload())
				}
				if strings.Contains(public.String(), value) {
					t.Fatalf("%s republished the execution identity %q: %s", eventType, value, public.String())
				}
				// The rest of the payload is the product's own governed
				// projection and has to survive the drop.
				if got := public.GetPayload().GetFields()["status"].GetStringValue(); got != "running" {
					t.Fatalf("%s lost its own payload alongside the execution key: %s", eventType, public.GetPayload())
				}
			})
		}
	}
}

// TestPassThroughLifecycleRowsNeverRepublishTheStoredSnapshot covers the arms
// that keep their writer's payload while still carrying a canonical snapshot.
//
// The snapshot is read into the typed field and dropped from the payload: the
// typed field is a closed vocabulary a client can localize, while the stored
// strings are this server's internal words for the same thing. A row a newer
// server wrote is the case that proves it — its words are ones this build has
// never heard of, and echoing them would hand a client a phase it cannot name
// dressed up as one it can.
func TestPassThroughLifecycleRowsNeverRepublishTheStoredSnapshot(t *testing.T) {
	for _, eventType := range []string{"agent.run.state_changed", "agent.run.started"} {
		t.Run(eventType, func(t *testing.T) {
			h := newEventHarness(t)
			run := seedEventRun(t, h, eventType)
			seeded := appendLegacyEvent(t, h, run, eventType, `{"jobId":"job_1","runState":{`+
				`"runId":"`+run.runID+`",`+
				`"userMessageId":"msg_user",`+
				`"assistantMessageId":"`+run.assistantMessageID+`",`+
				`"lifecycle":"hibernating",`+
				`"outcomeReason":"sunspots",`+
				`"stateVersion":9,`+
				`"stateUpdatedAt":"2026-08-20T00:00:00.000000000Z",`+
				`"hasDisplayableContent":true}}`)
			public := listedEvent(t, h, run.sessionID, seeded.EventID)

			state := public.GetRunState()
			if state == nil {
				t.Fatal("a row this build cannot fully name produced no state at all")
			}
			if state.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_UNKNOWN ||
				state.GetOutcomeReason() != turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNKNOWN {
				t.Fatalf("state = %v/%v, want the honest unknown on both",
					state.GetLifecycle(), state.GetOutcomeReason())
			}
			if state.GetStateVersion() != 9 {
				t.Fatalf("state version = %d, want the stored 9", state.GetStateVersion())
			}
			if _, republished := public.GetPayload().GetFields()["runState"]; republished {
				t.Fatalf("the payload republished the stored snapshot: %s", public.GetPayload())
			}
			// The writer's own keys survive; only the snapshot is taken out.
			if got := public.GetPayload().GetFields()["jobId"].GetStringValue(); got != "job_1" {
				t.Fatalf("the drop took the writer's payload with it: %s", public.GetPayload())
			}
			rendered := public.String()
			for _, stored := range []string{"hibernating", "sunspots", "runState"} {
				if strings.Contains(rendered, stored) {
					t.Fatalf("public event rendered the stored word %q: %s", stored, rendered)
				}
			}
		})
	}
}

// TestZeroStateVersionSnapshotsNeverBecomeTypedState pins the version rule at
// the read boundary.
//
// Run ID plus state version is how every consumer deduplicates and reconciles.
// Version zero is protobuf absence, so a snapshot carrying it names no state a
// client could reconcile against — publishing it would read as a version below
// every stored one rather than as "no version known". Absence is the honest
// answer, and it is what a reader renders as "no state was recorded".
func TestZeroStateVersionSnapshotsNeverBecomeTypedState(t *testing.T) {
	snapshots := map[string]string{
		"zero version":    `"stateVersion":0,`,
		"negative":        `"stateVersion":-3,`,
		"absent":          ``,
		"zero as a float": `"stateVersion":0.0,`,
		"unparseable":     `"stateVersion":"seven",`,
	}
	for _, eventType := range []string{
		"agent.run.state_changed", "agent.run.started", "agent.run.completed",
		"agent.run.failed", "agent.run.cancelled",
	} {
		for name, version := range snapshots {
			t.Run(eventType+" "+name, func(t *testing.T) {
				h := newEventHarness(t)
				run := seedEventRun(t, h, eventType+name)
				seeded := appendLegacyEvent(t, h, run, eventType, `{"runState":{`+
					`"runId":"`+run.runID+`",`+
					`"userMessageId":"msg_user",`+
					`"assistantMessageId":"`+run.assistantMessageID+`",`+
					`"lifecycle":"completed",`+
					`"outcomeReason":"none",`+
					version+
					`"stateUpdatedAt":"2026-08-20T00:00:00.000000000Z",`+
					`"hasDisplayableContent":true}}`)
				public := listedEvent(t, h, run.sessionID, seeded.EventID)
				if public.GetRunState() != nil {
					t.Fatalf("a snapshot with no usable version became the typed state %+v",
						public.GetRunState())
				}
				if _, republished := public.GetPayload().GetFields()["runState"]; republished {
					t.Fatalf("the unusable snapshot was republished as payload instead: %s",
						public.GetPayload())
				}
			})
		}
	}
}

// TestRunStepStateVersionIsPublishedOnlyWhenReconcilable is the same rule on
// the one payload key that carries a version rather than a whole snapshot.
func TestRunStepStateVersionIsPublishedOnlyWhenReconcilable(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		published bool
		want      float64
	}{
		{
			name:      "usable version",
			payload:   `{"category":"dispatch_retry","attempt":2,"maxAttempts":3,"stateVersion":4}`,
			published: true,
			want:      4,
		},
		{
			name:    "zero version",
			payload: `{"category":"dispatch_retry","attempt":2,"maxAttempts":3,"stateVersion":0}`,
		},
		{
			name:    "negative version",
			payload: `{"category":"dispatch_retry","attempt":2,"maxAttempts":3,"stateVersion":-1}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newEventHarness(t)
			run := seedEventRun(t, h, test.name)
			seeded := appendLegacyEvent(t, h, run, "agent.run.step", test.payload)
			public := listedEvent(t, h, run.sessionID, seeded.EventID)
			field, exists := public.GetPayload().GetFields()["stateVersion"]
			if exists != test.published {
				t.Fatalf("stateVersion present = %v, want %v (%s)", exists, test.published, public.GetPayload())
			}
			if test.published && field.GetNumberValue() != test.want {
				t.Fatalf("stateVersion = %v, want %v", field.GetNumberValue(), test.want)
			}
		})
	}
}
