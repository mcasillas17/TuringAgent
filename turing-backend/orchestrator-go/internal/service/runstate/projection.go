// Package runstate projects the durable run state onto the public contract.
//
// It exists as one package, and one function, because a run's outcome now
// reaches clients from several places — reopened history, live lifecycle
// events, and whatever comes next — and a second projection would be a second
// opinion about what a stored row means. Two opinions is how a reopened session
// and a live stream come to disagree about the same run, which is the failure
// this whole design exists to remove.
//
// It consumes the repository's stored strings rather than a decoded protobuf
// message. There is no protobuf-to-protobuf boundary here to test: the numeric
// wire values are exercised where generated enums actually arrive from
// somewhere else.
package runstate

import (
	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The stored lifecycle vocabulary. The repository writes these strings into
// agent_runs.status, and the schema's CHECK constraint is what keeps the set
// closed; this map is the reader's half of that agreement.
//
// "unknown" is listed even though TUR-009 never writes it, because a row could
// arrive from a newer server that does. Mapping it to the explicit UNKNOWN
// value is different from failing to recognize it: one says "a phase this build
// cannot name", the other would say UNSPECIFIED, which means "no phase at all".
const (
	lifecycleQueued          = "queued"
	lifecycleRunning         = "running"
	lifecycleWaitingApproval = "waiting_approval"
	lifecycleRecovering      = "recovering"
	lifecycleCompleted       = "completed"
	lifecycleFailed          = "failed"
	lifecycleCancelled       = "cancelled"
	lifecycleUnknown         = "unknown"
)

var lifecycles = map[string]turingv1.RunLifecycle{
	lifecycleQueued:          turingv1.RunLifecycle_RUN_LIFECYCLE_QUEUED,
	lifecycleRunning:         turingv1.RunLifecycle_RUN_LIFECYCLE_RUNNING,
	lifecycleWaitingApproval: turingv1.RunLifecycle_RUN_LIFECYCLE_WAITING_APPROVAL,
	lifecycleRecovering:      turingv1.RunLifecycle_RUN_LIFECYCLE_RECOVERING,
	lifecycleCompleted:       turingv1.RunLifecycle_RUN_LIFECYCLE_COMPLETED,
	lifecycleFailed:          turingv1.RunLifecycle_RUN_LIFECYCLE_FAILED,
	lifecycleCancelled:       turingv1.RunLifecycle_RUN_LIFECYCLE_CANCELLED,
	lifecycleUnknown:         turingv1.RunLifecycle_RUN_LIFECYCLE_UNKNOWN,
}

// reasons maps the closed outcome vocabulary. The keys are runoutcome's own
// constants rather than string literals, so the durable vocabulary is spelled
// once and this mapper cannot drift from the values writers persist.
var reasons = map[runoutcome.Reason]turingv1.RunOutcomeReason{
	runoutcome.ReasonNone:                   turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE,
	runoutcome.ReasonCompletedNoContent:     turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT,
	runoutcome.ReasonUserCancelled:          turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_USER_CANCELLED,
	runoutcome.ReasonAbandoned:              turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_ABANDONED,
	runoutcome.ReasonExpired:                turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_EXPIRED,
	runoutcome.ReasonContextLimit:           turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_CONTEXT_LIMIT,
	runoutcome.ReasonProviderFailure:        turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_PROVIDER_FAILURE,
	runoutcome.ReasonToolFailure:            turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_TOOL_FAILURE,
	runoutcome.ReasonPolicyDenied:           turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_POLICY_DENIED,
	runoutcome.ReasonRetriesExhausted:       turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_RETRIES_EXHAUSTED,
	runoutcome.ReasonRecoveryInterrupted:    turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_RECOVERY_INTERRUPTED,
	runoutcome.ReasonSideEffectUncertain:    turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_SIDE_EFFECT_UNCERTAIN,
	runoutcome.ReasonApprovalDeliveryFailed: turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_APPROVAL_DELIVERY_FAILED,
	runoutcome.ReasonInternalFailure:        turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_INTERNAL_FAILURE,
	runoutcome.ReasonUnknown:                turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNKNOWN,
	runoutcome.ReasonLegacyUnknown:          turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_LEGACY_UNKNOWN,
}

// allowedOutcomeReasons is the normative lifecycle/reason matrix, restated for
// the read boundary. The repository enforces it on write; this is what a reader
// checks a row it did not write against, because a database can be restored,
// downgraded, or edited by hand between the two.
var allowedOutcomeReasons = map[string][]runoutcome.Reason{
	lifecycleQueued:          {runoutcome.ReasonNone},
	lifecycleRunning:         {runoutcome.ReasonNone},
	lifecycleWaitingApproval: {runoutcome.ReasonNone},
	lifecycleRecovering:      {runoutcome.ReasonNone},
	lifecycleCompleted:       {runoutcome.ReasonNone, runoutcome.ReasonCompletedNoContent},
	lifecycleFailed: {
		runoutcome.ReasonExpired,
		runoutcome.ReasonContextLimit,
		runoutcome.ReasonProviderFailure,
		runoutcome.ReasonToolFailure,
		runoutcome.ReasonPolicyDenied,
		runoutcome.ReasonRetriesExhausted,
		runoutcome.ReasonRecoveryInterrupted,
		runoutcome.ReasonSideEffectUncertain,
		runoutcome.ReasonApprovalDeliveryFailed,
		runoutcome.ReasonInternalFailure,
	},
	lifecycleCancelled: {runoutcome.ReasonUserCancelled, runoutcome.ReasonAbandoned},
	lifecycleUnknown:   {runoutcome.ReasonUnknown, runoutcome.ReasonLegacyUnknown},
}

// Project renders one durable run state as the public one, or nothing.
//
// Nothing is a real answer here. A client that receives no state renders the
// neutral "no assistant response was recorded" card; a client that receives a
// state renders that state as fact. So anything this function cannot vouch for
// — a pair the matrix forbids, a run with no identity, a version no client
// could reconcile against — is returned as absence rather than as a plausible
// outcome nobody can justify.
//
// An unrecognized lifecycle or reason is the opposite case and is projected as
// the explicit UNKNOWN value: the row is internally consistent as far as this
// build can tell, it simply uses a word this build does not have. Saying so is
// honest; guessing at the nearest phase would not be.
func Project(state repository.RunState) *turingv1.RunState {
	// Run ID plus state version is the deduplication and reconciliation key for
	// every consumer of this state. Without both, a client cannot tell this
	// state from a different one, or from an older one — and version zero is
	// protobuf absence rather than a version below every other.
	if state.RunID == "" || state.StateVersion < 1 {
		return nil
	}
	lifecycle, namedLifecycle := lifecycles[state.Lifecycle]
	reason, namedReason := reasons[runoutcome.Reason(state.OutcomeReason)]
	if namedLifecycle && namedReason && !allowedPair(state.Lifecycle, runoutcome.Reason(state.OutcomeReason)) {
		return nil
	}
	if !namedLifecycle {
		lifecycle = turingv1.RunLifecycle_RUN_LIFECYCLE_UNKNOWN
	}
	if !namedReason {
		reason = turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNKNOWN
	}
	projected := &turingv1.RunState{
		RunId:              state.RunID,
		UserMessageId:      state.UserMessageID,
		AssistantMessageId: state.AssistantMessageID,
		Lifecycle:          lifecycle,
		OutcomeReason:      reason,
		StateVersion:       state.StateVersion,
		StateUpdatedAt:     instant(state.StateUpdatedAt),
		// Nothing below this line is public: the content digest is internal
		// duplicate-report identity, and the worker, assignment attempt,
		// execution columns, token counts, tool arguments, and every diagnostic
		// string are execution detail a client has no business seeing. They are
		// absent by never being read here, not by being read and dropped.
		HasDisplayableContent: state.HasDisplayableContent,
	}
	// A run that has not reached a terminal phase has not finished, whatever a
	// stray column says — and an unrecognized phase is not one this build can
	// call terminal.
	if state.FinishedAt.Valid && terminal(state.Lifecycle) {
		projected.FinishedAt = instant(state.FinishedAt.String)
	}
	return projected
}

func allowedPair(lifecycle string, reason runoutcome.Reason) bool {
	for _, allowed := range allowedOutcomeReasons[lifecycle] {
		if allowed == reason {
			return true
		}
	}
	return false
}

func terminal(lifecycle string) bool {
	switch lifecycle {
	case lifecycleCompleted, lifecycleFailed, lifecycleCancelled:
		return true
	default:
		return false
	}
}

// instant reads a persisted timestamp through the one parser that knows every
// shape this database has ever written. A value it cannot read is returned as
// no timestamp: the state's authority is its version, and an unreadable column
// must not become an instant a client would render as fact.
func instant(value string) *timestamppb.Timestamp {
	parsed, err := persisttime.ParseLegacy(value)
	if err != nil {
		return nil
	}
	return timestamppb.New(parsed)
}
