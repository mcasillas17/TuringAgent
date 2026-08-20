package runoutcome

import (
	"fmt"
	"sort"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// runtimeOriginCase pins one generated FailureOrigin arm to the domain origin it
// must produce and to the public outcome that origin then selects for a code.
type runtimeOriginCase struct {
	protoOrigin turingv1.FailureOrigin
	code        string
	wantOrigin  Origin
	wantCode    string
	wantReason  Reason
}

// runtimeOriginCases carries every concrete generated FailureOrigin: each arm is
// listed once, with a code chosen so the row proves something. Most rows use a
// code the allowlist pairs with that origin, so the row fails if the arm returns
// the wrong domain origin. The orchestrator-internal row is a deliberate
// family-fallback: no code is allowlisted under it today, so it must drop to the
// unknown code and that family's generic outcome rather than keep a worker's
// spelling.
//
// Asserting the code alongside the reason is load-bearing. Collapsing tool_guard
// into tool_policy keeps the reason at tool_failure, because the unrecognized
// pair falls back to the tool family; only the dropped code and the changed
// origin expose it.
var runtimeOriginCases = map[string]runtimeOriginCase{
	"context_assembly": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_CONTEXT_ASSEMBLY,
		code:        "message_fetch_failed",
		wantOrigin:  OriginContextAssembly,
		wantCode:    "message_fetch_failed",
		wantReason:  ReasonInternalFailure,
	},
	"external_provider": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_EXTERNAL_PROVIDER,
		code:        "external_agent_unavailable",
		wantOrigin:  OriginExternalProvider,
		wantCode:    "external_agent_unavailable",
		wantReason:  ReasonProviderFailure,
	},
	"provider_configuration": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_CONFIGURATION,
		code:        "model_provider_unavailable",
		wantOrigin:  OriginProviderConfiguration,
		wantCode:    "model_provider_unavailable",
		wantReason:  ReasonProviderFailure,
	},
	"provider_protocol": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
		code:        "model_auth_failed",
		wantOrigin:  OriginProviderProtocol,
		wantCode:    "model_auth_failed",
		wantReason:  ReasonProviderFailure,
	},
	"provider_transport": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
		code:        "model_timeout",
		wantOrigin:  OriginProviderTransport,
		wantCode:    "model_timeout",
		wantReason:  ReasonProviderFailure,
	},
	"provider_output_guard": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_OUTPUT_GUARD,
		code:        "model_output_limit_exceeded",
		wantOrigin:  OriginProviderOutputGuard,
		wantCode:    "model_output_limit_exceeded",
		wantReason:  ReasonProviderFailure,
	},
	"tool_infrastructure": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_INFRASTRUCTURE,
		code:        "tool_discovery_failed",
		wantOrigin:  OriginToolInfrastructure,
		wantCode:    "tool_discovery_failed",
		wantReason:  ReasonToolFailure,
	},
	"tool_execution": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_EXECUTION,
		code:        "tool_call_failed",
		wantOrigin:  OriginToolExecution,
		wantCode:    "tool_call_failed",
		wantReason:  ReasonToolFailure,
	},
	"tool_guard": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_GUARD,
		code:        "tool_call_limit_exceeded",
		wantOrigin:  OriginToolGuard,
		wantCode:    "tool_call_limit_exceeded",
		wantReason:  ReasonToolFailure,
	},
	"tool_policy": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_POLICY,
		code:        "tool_policy_decision_failed",
		wantOrigin:  OriginToolPolicy,
		wantCode:    "tool_policy_decision_failed",
		wantReason:  ReasonPolicyDenied,
	},
	"approval_transport": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_APPROVAL_TRANSPORT,
		code:        "approval_delivery_failed",
		wantOrigin:  OriginApprovalTransport,
		wantCode:    "approval_delivery_failed",
		wantReason:  ReasonApprovalDeliveryFailed,
	},
	"approval_expiry": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_APPROVAL_EXPIRY,
		code:        "approval_expired",
		wantOrigin:  OriginApprovalExpiry,
		wantCode:    "approval_expired",
		wantReason:  ReasonExpired,
	},
	"automation_policy": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_AUTOMATION_POLICY,
		code:        "automation_tool_not_allowlisted",
		wantOrigin:  OriginAutomationPolicy,
		wantCode:    "automation_tool_not_allowlisted",
		wantReason:  ReasonPolicyDenied,
	},
	"worker_runtime": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_WORKER_RUNTIME,
		code:        "runtime_error",
		wantOrigin:  OriginWorkerRuntime,
		wantCode:    "runtime_error",
		wantReason:  ReasonInternalFailure,
	},
	"dispatch": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH,
		code:        "retries_exhausted",
		wantOrigin:  OriginDispatch,
		wantCode:    "retries_exhausted",
		wantReason:  ReasonRetriesExhausted,
	},
	"recovery": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_RECOVERY,
		code:        "side_effect_uncertain",
		wantOrigin:  OriginRecovery,
		wantCode:    "side_effect_uncertain",
		wantReason:  ReasonSideEffectUncertain,
	},
	// No code is allowlisted under the orchestrator-internal origin today, so
	// this arm is exercised through the family fallback on purpose: the typed
	// origin survives, the reported code does not.
	"orchestrator_internal": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_ORCHESTRATOR_INTERNAL,
		code:        "runtime_error",
		wantOrigin:  OriginOrchestratorInternal,
		wantCode:    CodeUnknown,
		wantReason:  ReasonInternalFailure,
	},
	"client_lifecycle": {
		protoOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_CLIENT_LIFECYCLE,
		code:        "client_cancelled",
		wantOrigin:  OriginClientLifecycle,
		wantCode:    "client_cancelled",
		wantReason:  ReasonAbandoned,
	},
}

// The worker-facing adapter is the only thing standing between a wire enum and a
// durable public outcome, and the two absence arms the other tests cover leave
// every concrete arm free to be rewired without a failure. So each one is pinned
// here to the domain origin it must produce and to the outcome that origin then
// selects.
func TestNormalizeRuntimeFailureMapsEveryConcreteProtoOrigin(t *testing.T) {
	for name, test := range runtimeOriginCases {
		t.Run(name, func(t *testing.T) {
			got := NormalizeRuntimeFailure(test.protoOrigin, test.code, turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_NEVER)
			if got.Origin() != test.wantOrigin {
				t.Fatalf("origin = %d, want %d", got.Origin(), test.wantOrigin)
			}
			if got.Code() != test.wantCode {
				t.Fatalf("code = %q, want %q", got.Code(), test.wantCode)
			}
			if got.Reason() != test.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason(), test.wantReason)
			}
		})
	}
}

// The table above only covers arms a human remembered to list. This is what
// makes a new proto origin fail until it is mapped and pinned: a generated value
// with no row here has no proof that it reaches a domain origin at all, and an
// unmapped arm silently becomes the internal-failure default.
func TestRuntimeOriginCasesCoverEveryGeneratedFailureOrigin(t *testing.T) {
	covered := make(map[int32]string, len(runtimeOriginCases))
	for name, test := range runtimeOriginCases {
		number := int32(test.protoOrigin)
		if previous, clash := covered[number]; clash {
			t.Fatalf("proto origin %d is listed twice, as %q and %q", number, previous, name)
		}
		covered[number] = name
	}

	var missing []string
	for number, enumName := range turingv1.FailureOrigin_name {
		switch turingv1.FailureOrigin(number) {
		case turingv1.FailureOrigin_FAILURE_ORIGIN_UNSPECIFIED, turingv1.FailureOrigin_FAILURE_ORIGIN_UNKNOWN:
			// Absence and forward-compatibility values are covered by the
			// fail-closed tests instead; neither is a real reporting site.
			continue
		}
		if _, ok := covered[number]; !ok {
			missing = append(missing, enumName)
		}
		delete(covered, number)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("generated FailureOrigin values %v have no pinned case; map them in originFromProto and add a row", missing)
	}

	// Anything still here pins a numeric the loop above never visited: either a
	// concrete arm the enum dropped or renumbered, or one of the two absence
	// values, which must not be pinned as a real reporting site.
	var stale []string
	for number, name := range covered {
		stale = append(stale, fmt.Sprintf("%s pins %d", name, number))
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("cases %v pin numerics that are not concrete generated FailureOrigin values", stale)
	}
}

// Two reporting sites that collapse onto one domain origin are indistinguishable
// downstream, which is exactly how a guard failure would start reading as a
// policy denial. Distinctness is stated here so it fails on its own terms rather
// than only as a surprising code mismatch in some other row.
func TestConcreteProtoOriginsMapToDistinctDomainOrigins(t *testing.T) {
	seen := make(map[Origin]string, len(runtimeOriginCases))
	for name, test := range runtimeOriginCases {
		got := NormalizeRuntimeFailure(test.protoOrigin, test.code, turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_NEVER).Origin()
		if got == OriginUnknown {
			t.Errorf("case %q reached the unknown origin, so its arm is unmapped", name)
			continue
		}
		if previous, clash := seen[got]; clash {
			t.Errorf("cases %q and %q both map to domain origin %d", previous, name, got)
			continue
		}
		seen[got] = name
	}
}

// Retry class is internal dispatch policy, and the adapter is the only place a
// worker's request survives into it. Every other runtime test lands on a
// fail-closed origin, so nothing yet proves the transient class is preserved at
// all: replacing the whole adapter body with the never class would pass. This
// pins the one pair that must keep it, using an allowlisted origin and code so
// the retry enum is the only variable.
func TestNormalizeRuntimeFailurePreservesRetryClassOnlyForTheTransientArm(t *testing.T) {
	// A worker-busy dispatch report is nonterminal: it explains a requeue, so
	// the run keeps the none outcome while the retry class decides the requeue.
	const nonterminalCode = "worker_busy"

	tests := []struct {
		name  string
		retry turingv1.AutomaticRetryClass
		want  RetryClass
	}{
		{
			name:  "same_run_transient_is_preserved",
			retry: turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
			want:  RetryClassSameRunTransient,
		},
		{
			name:  "never_stays_never",
			retry: turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_NEVER,
			want:  RetryClassNever,
		},
		{
			name:  "unspecified_fails_closed",
			retry: turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_UNSPECIFIED,
			want:  RetryClassNever,
		},
		{
			name:  "unknown_fails_closed",
			retry: turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_UNKNOWN,
			want:  RetryClassNever,
		},
		{
			// A newer worker can send a numeric this build has never seen.
			name:  "unrecognized_numeric_fails_closed",
			retry: turingv1.AutomaticRetryClass(777),
			want:  RetryClassNever,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizeRuntimeFailure(turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH, nonterminalCode, test.retry)
			if got.RetryClass() != test.want {
				t.Fatalf("retry class = %d, want %d", got.RetryClass(), test.want)
			}
			// The retry class must not move the public outcome: the same
			// nonterminal pair reports the same origin, code, and reason
			// whatever the worker asked for.
			if got.Origin() != OriginDispatch || got.Code() != nonterminalCode || got.Reason() != ReasonNone {
				t.Fatalf("normalized = origin %d code %q reason %q, want dispatch/%s/%s",
					got.Origin(), got.Code(), got.Reason(), nonterminalCode, ReasonNone)
			}
		})
	}
}
