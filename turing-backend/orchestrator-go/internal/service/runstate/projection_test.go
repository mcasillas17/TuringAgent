package runstate

import (
	"database/sql"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// canonicalState is a durable state the repository could really hold, so a test
// that changes one field is changing only that field.
func canonicalState(lifecycle string, reason string) repository.RunState {
	return repository.RunState{
		RunID:              "run_projection",
		UserMessageID:      "msg_user",
		AssistantMessageID: "msg_assistant",
		Lifecycle:          lifecycle,
		OutcomeReason:      reason,
		StateVersion:       7,
		StateUpdatedAt:     "2026-08-20T10:11:12.000000013Z",
	}
}

// TestRunStateProjectionMapsEveryKnownLifecycleAndReason walks the whole
// normative matrix. Every durable pair a writer can commit has to arrive at a
// client as a named phase and a named reason — a gap here is a run whose
// outcome silently reads as absent.
func TestRunStateProjectionMapsEveryKnownLifecycleAndReason(t *testing.T) {
	wantLifecycles := map[string]turingv1.RunLifecycle{
		"queued":           turingv1.RunLifecycle_RUN_LIFECYCLE_QUEUED,
		"running":          turingv1.RunLifecycle_RUN_LIFECYCLE_RUNNING,
		"waiting_approval": turingv1.RunLifecycle_RUN_LIFECYCLE_WAITING_APPROVAL,
		"recovering":       turingv1.RunLifecycle_RUN_LIFECYCLE_RECOVERING,
		"completed":        turingv1.RunLifecycle_RUN_LIFECYCLE_COMPLETED,
		"failed":           turingv1.RunLifecycle_RUN_LIFECYCLE_FAILED,
		"cancelled":        turingv1.RunLifecycle_RUN_LIFECYCLE_CANCELLED,
		"unknown":          turingv1.RunLifecycle_RUN_LIFECYCLE_UNKNOWN,
	}
	wantReasons := map[runoutcome.Reason]turingv1.RunOutcomeReason{
		"none":                     turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE,
		"completed_no_content":     turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT,
		"user_cancelled":           turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_USER_CANCELLED,
		"abandoned":                turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_ABANDONED,
		"expired":                  turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_EXPIRED,
		"context_limit":            turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_CONTEXT_LIMIT,
		"provider_failure":         turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_PROVIDER_FAILURE,
		"tool_failure":             turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_TOOL_FAILURE,
		"policy_denied":            turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_POLICY_DENIED,
		"retries_exhausted":        turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_RETRIES_EXHAUSTED,
		"recovery_interrupted":     turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_RECOVERY_INTERRUPTED,
		"side_effect_uncertain":    turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_SIDE_EFFECT_UNCERTAIN,
		"approval_delivery_failed": turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_APPROVAL_DELIVERY_FAILED,
		"internal_failure":         turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_INTERNAL_FAILURE,
		"unknown":                  turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNKNOWN,
		"legacy_unknown":           turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_LEGACY_UNKNOWN,
	}

	seenLifecycles := map[turingv1.RunLifecycle]bool{}
	seenReasons := map[turingv1.RunOutcomeReason]bool{}
	for lifecycle, allowed := range allowedOutcomeReasons {
		wantLifecycle, named := wantLifecycles[lifecycle]
		if !named {
			t.Fatalf("matrix lifecycle %q has no expected enum in this test", lifecycle)
		}
		for _, reason := range allowed {
			wantReason, named := wantReasons[reason]
			if !named {
				t.Fatalf("matrix reason %q has no expected enum in this test", reason)
			}
			projected := Project(canonicalState(lifecycle, string(reason)))
			if projected == nil {
				t.Fatalf("canonical pair %q/%q projected no state", lifecycle, reason)
			}
			if projected.GetLifecycle() != wantLifecycle {
				t.Fatalf("lifecycle %q = %v, want %v", lifecycle, projected.GetLifecycle(), wantLifecycle)
			}
			if projected.GetOutcomeReason() != wantReason {
				t.Fatalf("reason %q = %v, want %v", reason, projected.GetOutcomeReason(), wantReason)
			}
			seenLifecycles[wantLifecycle] = true
			seenReasons[wantReason] = true
		}
	}

	// Exhaustive against the generated enums, not against this test's own
	// table: a value added to the contract and never mapped would otherwise
	// reach clients as UNSPECIFIED, which reads as "no state at all".
	for value, name := range turingv1.RunLifecycle_name {
		phase := turingv1.RunLifecycle(value)
		if phase == turingv1.RunLifecycle_RUN_LIFECYCLE_UNSPECIFIED {
			continue
		}
		if !seenLifecycles[phase] {
			t.Fatalf("lifecycle %s is never produced by any stored string", name)
		}
	}
	for value, name := range turingv1.RunOutcomeReason_name {
		reason := turingv1.RunOutcomeReason(value)
		if reason == turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNSPECIFIED {
			continue
		}
		if !seenReasons[reason] {
			t.Fatalf("outcome reason %s is never produced by any stored string", name)
		}
	}
}

// TestRunStateProjectionMapsUnknownStoredStringsToSemanticUnknown covers a row
// this build cannot name — written by a newer server, or by a downgrade. The
// honest answer is the explicit unknown value, never UNSPECIFIED (which means
// "absent") and never a plausible neighbouring phase.
func TestRunStateProjectionMapsUnknownStoredStringsToSemanticUnknown(t *testing.T) {
	tests := []struct {
		name      string
		lifecycle string
		reason    string
	}{
		{name: "future_strings", lifecycle: "quiescing", reason: "quota_reclaimed"},
		{name: "empty_strings", lifecycle: "", reason: ""},
		{name: "case_variant_is_not_the_stored_value", lifecycle: "Completed", reason: "None"},
		{name: "numeric_text_is_not_an_enum_number", lifecycle: "6", reason: "2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projected := Project(canonicalState(test.lifecycle, test.reason))
			if projected == nil {
				t.Fatal("unrecognized strings projected no state, want the explicit unknown values")
			}
			if projected.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_UNKNOWN {
				t.Fatalf("lifecycle = %v, want RUN_LIFECYCLE_UNKNOWN", projected.GetLifecycle())
			}
			if projected.GetOutcomeReason() != turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNKNOWN {
				t.Fatalf("reason = %v, want RUN_OUTCOME_REASON_UNKNOWN", projected.GetOutcomeReason())
			}
		})
	}
}

// TestRunStateProjectionOmitsInvalidCanonicalPairs covers a row whose two
// halves are each nameable but cannot both be true. A cancelled run that claims
// it ran out of context, or a queued run that claims it was denied, is a
// corrupt row rather than an outcome — so history falls back to the neutral
// legacy path instead of publishing a story nobody can justify.
func TestRunStateProjectionOmitsInvalidCanonicalPairs(t *testing.T) {
	tests := []struct {
		name  string
		state repository.RunState
	}{
		{name: "queued_with_terminal_reason", state: canonicalState("queued", "policy_denied")},
		{name: "cancelled_with_failure_reason", state: canonicalState("cancelled", "context_limit")},
		{name: "failed_with_no_reason", state: canonicalState("failed", "none")},
		{name: "completed_with_cancellation_reason", state: canonicalState("completed", "user_cancelled")},
		{name: "running_with_legacy_unknown", state: canonicalState("running", "legacy_unknown")},
		{name: "unknown_lifecycle_with_real_reason", state: canonicalState("unknown", "none")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if projected := Project(test.state); projected != nil {
				t.Fatalf("invalid pair projected %+v, want the neutral legacy path", projected)
			}
		})
	}

	// A state no client could reconcile is the same problem in a different
	// place: run ID plus version is the deduplication key, and version zero is
	// protobuf absence rather than a version older than every other.
	unreconcilable := canonicalState("completed", "none")
	unreconcilable.RunID = ""
	if projected := Project(unreconcilable); projected != nil {
		t.Fatalf("state with no run identity projected %+v, want no state", projected)
	}
	for _, version := range []int64{0, -1} {
		versionless := canonicalState("completed", "none")
		versionless.StateVersion = version
		if projected := Project(versionless); projected != nil {
			t.Fatalf("state at version %d projected %+v, want no state", version, projected)
		}
	}
}

// TestRunStateProjectionOmitsContentHashAndInternalExecution is the leak test.
// The durable row carries internal identity and execution detail beside the
// public outcome, and this mapper is the boundary that decides which of those a
// client ever sees.
func TestRunStateProjectionOmitsContentHashAndInternalExecution(t *testing.T) {
	state := canonicalState("completed", "none")
	state.ContentSHA256 = "0db52f4076c082518412afd3dd3576e2cb0c63703fd7fed5e23ade60efef31d9"
	state.FinishedAt = sql.NullString{String: "2026-08-20T10:11:12.000000014Z", Valid: true}
	state.HasDisplayableContent = true

	projected := Project(state)
	if projected == nil {
		t.Fatal("a canonical completed state projected nothing")
	}

	// The field set is pinned rather than sampled: a field added to RunState
	// later is a decision to publish something, and it should have to be made
	// here rather than inherited silently.
	want := map[string]bool{
		"run_id": true, "user_message_id": true, "assistant_message_id": true,
		"lifecycle": true, "outcome_reason": true, "state_version": true,
		"state_updated_at": true, "finished_at": true, "has_displayable_content": true,
	}
	fields := projected.ProtoReflect().Descriptor().Fields()
	for index := 0; index < fields.Len(); index++ {
		name := string(fields.Get(index).Name())
		if !want[name] {
			t.Fatalf("RunState carries unreviewed field %q", name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("RunState lost expected fields %v", want)
	}

	// The digest could only reach a client through some field's bytes, so the
	// encoded message is searched rather than the struct inspected.
	encoded, err := proto.Marshal(projected)
	if err != nil {
		t.Fatalf("marshal projected state: %v", err)
	}
	if strings.Contains(string(encoded), state.ContentSHA256) {
		t.Fatal("the internal content digest reached the encoded public state")
	}
	projected.ProtoReflect().Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Kind() == protoreflect.StringKind && value.String() == state.ContentSHA256 {
			t.Fatalf("field %q carries the internal content digest", field.Name())
		}
		return true
	})

	if projected.GetRunId() != state.RunID || projected.GetUserMessageId() != state.UserMessageID ||
		projected.GetAssistantMessageId() != state.AssistantMessageID ||
		projected.GetStateVersion() != state.StateVersion || !projected.GetHasDisplayableContent() {
		t.Fatalf("projected identity = %+v, want the durable state %+v", projected, state)
	}
	if got := projected.GetStateUpdatedAt().AsTime().Format("2006-01-02T15:04:05.000000000Z"); got != state.StateUpdatedAt {
		t.Fatalf("state updated at = %q, want %q", got, state.StateUpdatedAt)
	}
	if got := projected.GetFinishedAt().AsTime().Format("2006-01-02T15:04:05.000000000Z"); got != state.FinishedAt.String {
		t.Fatalf("finished at = %q, want %q", got, state.FinishedAt.String)
	}

	// A nonterminal run has not finished, and an unreadable timestamp is not a
	// time. Neither may become an instant a client would render.
	nonterminal := canonicalState("running", "none")
	if finished := Project(nonterminal).GetFinishedAt(); finished != nil {
		t.Fatalf("nonterminal state carries finished at %v", finished)
	}
	corrupt := canonicalState("completed", "none")
	corrupt.StateUpdatedAt = "not a timestamp"
	corrupt.FinishedAt = sql.NullString{String: "also not a timestamp", Valid: true}
	projectedCorrupt := Project(corrupt)
	if projectedCorrupt == nil {
		t.Fatal("an unreadable timestamp dropped the whole state, want the outcome without it")
	}
	if projectedCorrupt.GetStateUpdatedAt() != nil || projectedCorrupt.GetFinishedAt() != nil {
		t.Fatalf("unreadable timestamps became instants: %+v", projectedCorrupt)
	}
}
