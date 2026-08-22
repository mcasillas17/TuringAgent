package runoutcome

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// poison stands in for provider-, tool-, or worker-authored text. It must never
// survive normalization in any form, because everything a normalized value
// holds is eligible to be persisted and returned to clients.
const poison = "provider said: sk-live-SECRET at /Users/someone/private"

// failureMappingCase is one row of the approved mapping table: the typed report
// a call site makes, and the normalized value clients may see. wantOrigin and
// wantCode are only spelled out when normalization is expected to replace them.
type failureMappingCase struct {
	name       string
	origin     Origin
	code       string
	wantOrigin Origin
	wantCode   string
	wantReason Reason
}

// approvedFailureMappings is the normative statement of every (origin, code)
// pair this codebase is allowed to map onto a public reason, one row per
// failureReasons entry. It is written out by hand rather than derived from
// failureReasons, because deriving it would make the production map its own
// approval: a pair added there would arrive already "expected" here. The
// forward test below reads it as behavior to assert, and the reverse pin reads
// it as the allowlist failureReasons may not exceed.
func approvedFailureMappings() []failureMappingCase {
	return []failureMappingCase{
		// Existing run-terminal code mapping.
		{name: "message_fetch_failed", origin: OriginContextAssembly, code: "message_fetch_failed", wantReason: ReasonInternalFailure},
		{name: "external_agent_unavailable", origin: OriginExternalProvider, code: "external_agent_unavailable", wantReason: ReasonProviderFailure},
		{name: "model_provider_unavailable", origin: OriginProviderConfiguration, code: "model_provider_unavailable", wantReason: ReasonProviderFailure},
		{name: "tool_discovery_failed", origin: OriginToolInfrastructure, code: "tool_discovery_failed", wantReason: ReasonToolFailure},
		{name: "context_budget_exceeded", origin: OriginContextAssembly, code: "context_budget_exceeded", wantReason: ReasonContextLimit},
		{name: "model_timeout", origin: OriginProviderTransport, code: "model_timeout", wantReason: ReasonProviderFailure},
		{name: "model_stream_failed", origin: OriginProviderTransport, code: "model_stream_failed", wantReason: ReasonProviderFailure},
		{name: "model_output_limit_exceeded", origin: OriginProviderOutputGuard, code: "model_output_limit_exceeded", wantReason: ReasonProviderFailure},
		{name: "model_unavailable", origin: OriginProviderProtocol, code: "model_unavailable", wantReason: ReasonProviderFailure},
		{name: "model_auth_failed", origin: OriginProviderProtocol, code: "model_auth_failed", wantReason: ReasonProviderFailure},
		{name: "model_request_failed", origin: OriginProviderProtocol, code: "model_request_failed", wantReason: ReasonProviderFailure},
		{name: "model_error", origin: OriginExternalProvider, code: "model_error", wantReason: ReasonProviderFailure},
		{name: "model_quota_exceeded", origin: OriginProviderProtocol, code: "model_quota_exceeded", wantReason: ReasonProviderFailure},
		{name: "model_bad_chunk", origin: OriginProviderProtocol, code: "model_bad_chunk", wantReason: ReasonProviderFailure},
		{name: "model_stream_error", origin: OriginProviderTransport, code: "model_stream_error", wantReason: ReasonProviderFailure},
		{name: "tool_call_failed", origin: OriginToolExecution, code: "tool_call_failed", wantReason: ReasonToolFailure},
		{name: "tool_call_limit_exceeded", origin: OriginToolGuard, code: "tool_call_limit_exceeded", wantReason: ReasonToolFailure},
		{name: "tool_result_limit_exceeded", origin: OriginToolGuard, code: "tool_result_limit_exceeded", wantReason: ReasonToolFailure},
		{name: "runtime_error", origin: OriginWorkerRuntime, code: "runtime_error", wantReason: ReasonInternalFailure},
		{name: "retries_exhausted", origin: OriginDispatch, code: "retries_exhausted", wantReason: ReasonRetriesExhausted},
		{name: "job_timeout", origin: OriginRecovery, code: "job_timeout", wantReason: ReasonRecoveryInterrupted},
		{name: "side_effect_uncertain", origin: OriginRecovery, code: "side_effect_uncertain", wantReason: ReasonSideEffectUncertain},
		{name: "approval_delivery_failed", origin: OriginApprovalTransport, code: "approval_delivery_failed", wantReason: ReasonApprovalDeliveryFailed},
		{name: "approval_expired", origin: OriginApprovalExpiry, code: "approval_expired", wantReason: ReasonExpired},
		{name: "automation_approval_failed", origin: OriginAutomationPolicy, code: "automation_approval_failed", wantReason: ReasonPolicyDenied},
		{name: "automation_tool_not_allowlisted", origin: OriginAutomationPolicy, code: "automation_tool_not_allowlisted", wantReason: ReasonPolicyDenied},
		{name: "egress_decision_required", origin: OriginToolPolicy, code: "egress_decision_required", wantReason: ReasonPolicyDenied},
		{name: "egress_decision_invalid", origin: OriginToolPolicy, code: "egress_decision_invalid", wantReason: ReasonPolicyDenied},
		// The current transport path cannot tell a deliberate stop from a lost
		// socket, so it may only report abandonment.
		{name: "client_cancelled", origin: OriginClientLifecycle, code: "client_cancelled", wantReason: ReasonAbandoned},

		// Nonterminal dispatch conditions keep the run's outcome at none while
		// the lifecycle moves through recovering or queued.
		{name: "worker_busy_is_nonterminal", origin: OriginDispatch, code: "worker_busy", wantReason: ReasonNone},
		{name: "worker_unavailable_is_nonterminal", origin: OriginDispatch, code: "worker_unavailable", wantReason: ReasonNone},

		// Subsidiary failure code mapping.
		{name: "tool_policy_decision_failed", origin: OriginToolPolicy, code: "tool_policy_decision_failed", wantReason: ReasonPolicyDenied},
		{name: "tool_policy_decision_invalid", origin: OriginToolPolicy, code: "tool_policy_decision_invalid", wantReason: ReasonPolicyDenied},
		{name: "approval_wait_failed", origin: OriginApprovalTransport, code: "approval_wait_failed", wantReason: ReasonApprovalDeliveryFailed},
		{name: "mcp_call_failed", origin: OriginToolExecution, code: "mcp_call_failed", wantReason: ReasonToolFailure},
		{name: "unknown_tool_from_infrastructure", origin: OriginToolInfrastructure, code: "unknown_tool", wantReason: ReasonToolFailure},
		{name: "unknown_tool_from_policy", origin: OriginToolPolicy, code: "unknown_tool", wantReason: ReasonPolicyDenied},
		{name: "tool_runner_unavailable", origin: OriginToolInfrastructure, code: "tool_runner_unavailable", wantReason: ReasonToolFailure},
		{name: "worker_unavailable_recovery_notice", origin: OriginRecovery, code: "worker_unavailable", wantReason: ReasonRecoveryInterrupted},
		{name: "tool_cleanup_cancelled", origin: OriginClientLifecycle, code: "cancelled", wantReason: ReasonAbandoned},
	}
}

// The two approved mapping tables are normative: the call site supplies a typed
// origin and the normalizer decides the public reason. Keying on (origin, code)
// rather than the code alone is what lets unknown_tool mean "tool failure" from
// the tool-infrastructure path and "policy denied" from the policy path.
func TestNormalizeFailureMapsEveryExistingCode(t *testing.T) {
	tests := append(approvedFailureMappings(), []failureMappingCase{
		// Typed provider unknown-code fallbacks.
		{name: "unknown_code_external_provider", origin: OriginExternalProvider, code: poison, wantCode: CodeUnknown, wantReason: ReasonProviderFailure},
		{name: "unknown_code_provider_configuration", origin: OriginProviderConfiguration, code: poison, wantCode: CodeUnknown, wantReason: ReasonProviderFailure},
		{name: "unknown_code_provider_protocol", origin: OriginProviderProtocol, code: poison, wantCode: CodeUnknown, wantReason: ReasonProviderFailure},
		{name: "unknown_code_provider_transport", origin: OriginProviderTransport, code: poison, wantCode: CodeUnknown, wantReason: ReasonProviderFailure},
		{name: "unknown_code_provider_output_guard", origin: OriginProviderOutputGuard, code: poison, wantCode: CodeUnknown, wantReason: ReasonProviderFailure},

		// Typed tool unknown-code fallbacks.
		{name: "unknown_code_tool_infrastructure", origin: OriginToolInfrastructure, code: poison, wantCode: CodeUnknown, wantReason: ReasonToolFailure},
		{name: "unknown_code_tool_execution", origin: OriginToolExecution, code: poison, wantCode: CodeUnknown, wantReason: ReasonToolFailure},
		{name: "unknown_code_tool_guard", origin: OriginToolGuard, code: poison, wantCode: CodeUnknown, wantReason: ReasonToolFailure},
		{name: "unknown_code_tool_policy", origin: OriginToolPolicy, code: poison, wantCode: CodeUnknown, wantReason: ReasonToolFailure},

		// Every other unknown pair fails closed to an internal failure.
		{name: "unknown_origin", origin: OriginUnknown, code: "retries_exhausted", wantOrigin: OriginUnknown, wantCode: CodeUnknown, wantReason: ReasonInternalFailure},
		{name: "unspecified_origin", origin: OriginUnspecified, code: "retries_exhausted", wantOrigin: OriginUnknown, wantCode: CodeUnknown, wantReason: ReasonInternalFailure},
		{name: "out_of_range_origin", origin: Origin(200), code: "retries_exhausted", wantOrigin: OriginUnknown, wantCode: CodeUnknown, wantReason: ReasonInternalFailure},
		{name: "unknown_code_recovery", origin: OriginRecovery, code: poison, wantCode: CodeUnknown, wantReason: ReasonInternalFailure},
		{name: "unknown_code_client_lifecycle", origin: OriginClientLifecycle, code: poison, wantCode: CodeUnknown, wantReason: ReasonInternalFailure},
		{name: "empty_code_context_assembly", origin: OriginContextAssembly, code: "", wantCode: CodeUnknown, wantReason: ReasonInternalFailure},
	}...)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wantOrigin := test.wantOrigin
			if wantOrigin == OriginUnspecified {
				wantOrigin = test.origin
			}
			wantCode := test.wantCode
			if wantCode == "" {
				wantCode = test.code
			}

			got := NormalizeFailure(test.origin, test.code, RetryClassNever)
			if got.Origin() != wantOrigin {
				t.Fatalf("origin = %v, want %v", got.Origin(), wantOrigin)
			}
			if got.Code() != wantCode {
				t.Fatalf("code = %q, want %q", got.Code(), wantCode)
			}
			if got.Reason() != test.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason(), test.wantReason)
			}
			if wantCode == CodeUnknown {
				assertNoRawText(t, got, test.code)
			}
		})
	}

	retryTests := []struct {
		name   string
		origin Origin
		code   string
		retry  RetryClass
		want   RetryClass
	}{
		{name: "retry_class_unspecified_is_never", origin: OriginDispatch, code: "worker_busy", retry: RetryClassUnspecified, want: RetryClassNever},
		{name: "retry_class_unknown_is_never", origin: OriginDispatch, code: "worker_busy", retry: RetryClassUnknown, want: RetryClassNever},
		{name: "retry_class_out_of_range_is_never", origin: OriginDispatch, code: "worker_busy", retry: RetryClass(9), want: RetryClassNever},
		{name: "retry_class_never_is_never", origin: OriginDispatch, code: "worker_busy", retry: RetryClassNever, want: RetryClassNever},
		{name: "retry_class_same_run_transient_survives_a_known_pair", origin: OriginDispatch, code: "worker_busy", retry: RetryClassSameRunTransient, want: RetryClassSameRunTransient},
		{name: "retry_class_ignored_for_an_unknown_code", origin: OriginProviderTransport, code: poison, retry: RetryClassSameRunTransient, want: RetryClassNever},
		{name: "retry_class_ignored_for_an_unknown_origin", origin: OriginUnknown, code: "worker_busy", retry: RetryClassSameRunTransient, want: RetryClassNever},
	}
	for _, test := range retryTests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeFailure(test.origin, test.code, test.retry).RetryClass(); got != test.want {
				t.Fatalf("retry class = %v, want %v", got, test.want)
			}
		})
	}
}

// The forward table only asserts that the pairs someone listed still normalize
// the way they were approved. It is blind in the other direction: an entry added
// to failureReasons and never listed is simply never exercised, so a new pair —
// or a quiet reason change on an existing one — could start writing a public
// outcome with every test still green. This pin requires the production map and
// the approved table to be the same set of pairs carrying the same reasons, so
// widening the mapping is an edit a reviewer has to see.
func TestFailureReasonsHoldsExactlyTheApprovedMappings(t *testing.T) {
	cases := approvedFailureMappings()
	approved := make(map[failureKey]Reason, len(cases))
	for _, test := range cases {
		if test.wantOrigin != OriginUnspecified || test.wantCode != "" {
			t.Fatalf("approved mapping %q expects a rewritten origin or code; an allowlisted pair is reported as-is, so it belongs with the fallback cases", test.name)
		}
		key := failureKey{origin: test.origin, code: test.code}
		if previous, duplicate := approved[key]; duplicate {
			t.Fatalf("approved table lists (%v, %q) twice, as %q and %q", key.origin, key.code, previous, test.wantReason)
		}
		approved[key] = test.wantReason
	}

	for key, reason := range failureReasons {
		want, listed := approved[key]
		if !listed {
			t.Errorf("failureReasons maps (%v, %q) to %q, which no approved mapping case covers; list the pair in approvedFailureMappings or drop the entry",
				key.origin, key.code, reason)
			continue
		}
		if want != reason {
			t.Errorf("failureReasons maps (%v, %q) to %q, want the approved %q", key.origin, key.code, reason, want)
		}
	}
	for key, want := range approved {
		if _, present := failureReasons[key]; !present {
			t.Errorf("approved mapping case (%v, %q) -> %q has no failureReasons entry", key.origin, key.code, want)
		}
	}
}

// ReasonNone is the one reason that does not terminalize a run: a failure
// carrying it explains a requeue and leaves the run's lifecycle moving. A third
// pair acquiring it — by a new map entry, an edited reason, or a fallback that
// returned it — would strand a genuinely failed run in a nonterminal state with
// no public outcome, so the producing pairs are pinned to exactly the two
// dispatch conditions that were approved.
func TestReasonNoneComesFromExactlyTheApprovedDispatchPairs(t *testing.T) {
	nonterminal := map[failureKey]struct{}{
		{origin: OriginDispatch, code: "worker_busy"}:        {},
		{origin: OriginDispatch, code: "worker_unavailable"}: {},
	}

	mapped := map[failureKey]struct{}{}
	for key, reason := range failureReasons {
		if reason == ReasonNone {
			mapped[key] = struct{}{}
		}
	}
	if !reflect.DeepEqual(mapped, nonterminal) {
		t.Fatalf("failureReasons entries producing %q = %v, want exactly %v", ReasonNone, sortedKeys(mapped), sortedKeys(nonterminal))
	}

	// The map check alone would miss a fallback arm that answered ReasonNone,
	// so every origin this package accepts — plus the ones it rejects — is run
	// against every code it knows and some it does not.
	codes := map[string]struct{}{CodeUnknown: {}, CodeClientCancelled: {}, poison: {}, "": {}}
	for key := range failureReasons {
		codes[key.code] = struct{}{}
	}
	origins := []Origin{Origin(200)}
	for origin := OriginUnspecified; origin <= OriginClientLifecycle; origin++ {
		origins = append(origins, origin)
	}

	for _, origin := range origins {
		for code := range codes {
			reason := NormalizeFailure(origin, code, RetryClassSameRunTransient).Reason()
			_, approved := nonterminal[failureKey{origin: origin, code: code}]
			if reason == ReasonNone && !approved {
				t.Errorf("NormalizeFailure(%v, %q) produced %q; only the approved dispatch pairs may leave a run unterminalized",
					origin, code, ReasonNone)
			}
			if approved && reason != ReasonNone {
				t.Errorf("NormalizeFailure(%v, %q) produced %q, want the nonterminal %q", origin, code, reason, ReasonNone)
			}
		}
	}
}

// sortedKeys renders a pair set deterministically, so a failure names the pairs
// that differ instead of printing a map in random order.
func sortedKeys(keys map[failureKey]struct{}) []string {
	rendered := make([]string, 0, len(keys))
	for key := range keys {
		rendered = append(rendered, fmt.Sprintf("(%v, %q)", key.origin, key.code))
	}
	sort.Strings(rendered)
	return rendered
}

// The product has no explicit cancel affordance, so nothing in this package may
// mint a user-cancelled claim. The source assertion is the part that survives a
// well-meaning future edit: adding UserCancelledCancellation would fail here
// even if every value assertion above still passed.
func TestAbandonedCancellationNeverClaimsUserIntent(t *testing.T) {
	cancellation := AbandonedCancellation()
	if cancellation.Reason() != ReasonAbandoned {
		t.Fatalf("reason = %q, want %q", cancellation.Reason(), ReasonAbandoned)
	}
	if cancellation.Origin() != OriginClientLifecycle {
		t.Fatalf("origin = %v, want %v", cancellation.Origin(), OriginClientLifecycle)
	}
	if cancellation.Code() != CodeClientCancelled {
		t.Fatalf("code = %q, want %q", cancellation.Code(), CodeClientCancelled)
	}

	for _, code := range []string{"client_cancelled", "cancelled"} {
		if got := NormalizeFailure(OriginClientLifecycle, code, RetryClassNever).Reason(); got == ReasonUserCancelled {
			t.Fatalf("code %q claimed user intent", code)
		}
	}

	constructors := exportedFunctionsReturning(t, "Cancellation")
	want := []string{"AbandonedCancellation"}
	if !reflect.DeepEqual(constructors, want) {
		t.Fatalf("exported cancellation constructors = %v, want %v", constructors, want)
	}
}

// Normalized values are the ingestion boundary: if a raw message can reach one,
// it can reach the database and then the client.
func TestNormalizedFailuresExposeNoRawMessage(t *testing.T) {
	values := map[string]any{
		"unknown_code": NormalizeFailure(OriginProviderTransport, poison, RetryClassSameRunTransient),
		"known_code":   NormalizeFailure(OriginProviderTransport, "model_timeout", RetryClassNever),
		"cancellation": AbandonedCancellation(),
	}
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			valueType := reflect.TypeOf(value)
			for index := 0; index < valueType.NumField(); index++ {
				if field := valueType.Field(index); field.IsExported() {
					t.Fatalf("field %s is exported; normalized values must be constructor-only", field.Name)
				}
			}
			for _, forbidden := range []string{"Message", "Detail", "Details", "Note", "Text", "Error", "String"} {
				if _, found := valueType.MethodByName(forbidden); found {
					t.Fatalf("method %s exposes free-form text", forbidden)
				}
			}
			assertNoRawText(t, value, poison)
		})
	}
}

func TestRuntimeUnknownFailureOriginFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		origin turingv1.FailureOrigin
		code   string
		retry  turingv1.AutomaticRetryClass
	}{
		{
			name:   "unspecified_origin",
			origin: turingv1.FailureOrigin_FAILURE_ORIGIN_UNSPECIFIED,
			code:   "retries_exhausted",
			retry:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
		},
		{
			name:   "unknown_origin",
			origin: turingv1.FailureOrigin_FAILURE_ORIGIN_UNKNOWN,
			code:   "retries_exhausted",
			retry:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
		},
		{
			name:   "unknown_code_with_unknown_origin",
			origin: turingv1.FailureOrigin_FAILURE_ORIGIN_UNKNOWN,
			code:   poison,
			retry:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_NEVER,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizeRuntimeFailure(test.origin, test.code, test.retry)
			if got.Origin() != OriginUnknown || got.Code() != CodeUnknown || got.Reason() != ReasonInternalFailure {
				t.Fatalf("normalized = origin %v code %q reason %q, want unknown/%s/%s",
					got.Origin(), got.Code(), got.Reason(), CodeUnknown, ReasonInternalFailure)
			}
			if got.RetryClass() != RetryClassNever {
				t.Fatalf("retry class = %v, want never", got.RetryClass())
			}
			assertNoRawText(t, got, test.code)
		})
	}
}

// A newer worker can send enum numerics this build has never seen. The real Go
// protobuf consumer keeps them, so the normalizer is exercised through an
// actual unmarshal rather than a hand-built enum value.
func TestNormalizeRuntimeFailureRawWireUnknownEnumsFailClosed(t *testing.T) {
	const (
		unknownOriginNumber = 4242
		unknownRetryNumber  = 777
	)
	var raw []byte
	raw = protowire.AppendTag(raw, 1, protowire.BytesType)
	raw = protowire.AppendString(raw, "run-raw-wire")
	raw = protowire.AppendTag(raw, 2, protowire.BytesType)
	raw = protowire.AppendString(raw, "a_code_from_the_future")
	raw = protowire.AppendTag(raw, 3, protowire.BytesType)
	raw = protowire.AppendString(raw, poison)
	raw = protowire.AppendTag(raw, 5, protowire.VarintType)
	raw = protowire.AppendVarint(raw, unknownOriginNumber)
	raw = protowire.AppendTag(raw, 6, protowire.VarintType)
	raw = protowire.AppendVarint(raw, unknownRetryNumber)

	var report turingv1.RuntimeRunFailed
	if err := proto.Unmarshal(raw, &report); err != nil {
		t.Fatalf("unmarshal raw wire report: %v", err)
	}
	if int32(report.GetFailureOrigin()) != unknownOriginNumber {
		t.Fatalf("decoded origin = %d, want the unknown numeric %d", int32(report.GetFailureOrigin()), unknownOriginNumber)
	}
	if int32(report.GetAutomaticRetryClass()) != unknownRetryNumber {
		t.Fatalf("decoded retry class = %d, want the unknown numeric %d", int32(report.GetAutomaticRetryClass()), unknownRetryNumber)
	}

	got := NormalizeRuntimeFailure(report.GetFailureOrigin(), report.GetCode(), report.GetAutomaticRetryClass())
	if got.Origin() != OriginUnknown || got.Code() != CodeUnknown || got.Reason() != ReasonInternalFailure {
		t.Fatalf("normalized = origin %v code %q reason %q, want unknown/%s/%s",
			got.Origin(), got.Code(), got.Reason(), CodeUnknown, ReasonInternalFailure)
	}
	if got.RetryClass() != RetryClassNever {
		t.Fatalf("retry class = %v, want never", got.RetryClass())
	}
	assertNoRawText(t, got, report.GetCode())
	assertNoRawText(t, got, report.GetMessage())
	assertNoRawText(t, got, fmt.Sprint(unknownOriginNumber))
	assertNoRawText(t, got, fmt.Sprint(unknownRetryNumber))
}

// Rewritten failure-like agent.run.step notices carry a category and counters,
// never the sentence the old payload persisted.
func TestNormalizeFailureRunStepNoticeUsesOnlyAllowlistedCategoryAndAttempts(t *testing.T) {
	for _, category := range []NoticeCategory{NoticeDispatchRetry, NoticeRecoveryRetry, NoticeRecoveryExhausted} {
		t.Run(string(category), func(t *testing.T) {
			notice, err := NewStepNotice(category, 2, 3)
			if err != nil {
				t.Fatalf("NewStepNotice(%q) error: %v", category, err)
			}
			if notice.Category() != category {
				t.Fatalf("category = %q, want %q", notice.Category(), category)
			}
			if notice.Attempt() != 2 || notice.MaxAttempts() != 3 {
				t.Fatalf("attempts = %d of %d, want 2 of 3", notice.Attempt(), notice.MaxAttempts())
			}
			assertNoRawText(t, notice, poison)
		})
	}

	rejected := []struct {
		name        string
		category    NoticeCategory
		attempt     int32
		maxAttempts int32
	}{
		{name: "unlisted_category", category: NoticeCategory(poison), attempt: 1, maxAttempts: 1},
		{name: "empty_category", category: NoticeCategory(""), attempt: 1, maxAttempts: 1},
		{name: "zero_attempt", category: NoticeDispatchRetry, attempt: 0, maxAttempts: 3},
		{name: "negative_attempt", category: NoticeDispatchRetry, attempt: -1, maxAttempts: 3},
		{name: "attempt_beyond_max", category: NoticeDispatchRetry, attempt: 4, maxAttempts: 3},
		{name: "zero_max_attempts", category: NoticeDispatchRetry, attempt: 1, maxAttempts: 0},
		{name: "max_attempts_out_of_bounds", category: NoticeDispatchRetry, attempt: 1, maxAttempts: MaxNoticeAttempts + 1},
	}
	for _, test := range rejected {
		t.Run(test.name, func(t *testing.T) {
			notice, err := NewStepNotice(test.category, test.attempt, test.maxAttempts)
			if err == nil {
				t.Fatalf("NewStepNotice(%q, %d, %d) = %+v, want an error", test.category, test.attempt, test.maxAttempts, notice)
			}
			if err.Error() != ErrUnsupportedNotice.Error() {
				t.Fatalf("error = %q, want the value-free sentinel %q", err, ErrUnsupportedNotice)
			}
			if notice != (StepNotice{}) {
				t.Fatalf("rejected notice = %+v, want the zero value", notice)
			}
		})
	}
}

// Go hands back a zero value for free: a var declaration, a map miss, a struct
// field, or a constructor that returned an error alongside it. None of those may
// read as a benign outcome. A zero Failure that reported an unspecified origin
// and an empty reason would look like a nonterminal dispatch condition and quietly
// leave a run unterminalized, and a zero Cancellation would claim that nothing
// cancelled anything. The accessors are the boundary every reader goes through,
// so they are where the closed defaults belong.
func TestZeroValuesReadAsFailClosedDefaults(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		var failure Failure
		if got := failure.Origin(); got != OriginUnknown {
			t.Fatalf("zero Failure origin = %v, want %v", got, OriginUnknown)
		}
		if got := failure.Code(); got != CodeUnknown {
			t.Fatalf("zero Failure code = %q, want %q", got, CodeUnknown)
		}
		if got := failure.Reason(); got != ReasonInternalFailure {
			t.Fatalf("zero Failure reason = %q, want %q", got, ReasonInternalFailure)
		}
		if got := failure.Reason(); got == ReasonNone {
			t.Fatalf("zero Failure reason = %q, which reads as a nonterminal dispatch condition", got)
		}
		if got := failure.RetryClass(); got != RetryClassNever {
			t.Fatalf("zero Failure retry class = %v, want %v", got, RetryClassNever)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		var cancellation Cancellation
		if got := cancellation.Origin(); got != OriginClientLifecycle {
			t.Fatalf("zero Cancellation origin = %v, want %v", got, OriginClientLifecycle)
		}
		if got := cancellation.Code(); got != CodeClientCancelled {
			t.Fatalf("zero Cancellation code = %q, want %q", got, CodeClientCancelled)
		}
		if got := cancellation.Reason(); got != ReasonAbandoned {
			t.Fatalf("zero Cancellation reason = %q, want %q", got, ReasonAbandoned)
		}
		if got := cancellation.Reason(); got == ReasonUserCancelled {
			t.Fatalf("zero Cancellation reason = %q, which claims user intent this product cannot prove", got)
		}
	})

	// A notice has no honest zero: category, attempt, and budget are all
	// meaningful, and inventing "dispatch retry, attempt 1 of 1" for an
	// uninitialized value would persist a retry that never happened. So the zero
	// value stays zero and reports itself invalid instead.
	t.Run("step_notice", func(t *testing.T) {
		var notice StepNotice
		if notice.Valid() {
			t.Fatalf("zero StepNotice %+v reported itself valid", notice)
		}
		rejected, err := NewStepNotice(NoticeCategory(poison), 1, 1)
		if err == nil {
			t.Fatalf("NewStepNotice accepted an unlisted category")
		}
		if rejected.Valid() {
			t.Fatalf("rejected StepNotice %+v reported itself valid", rejected)
		}
		for _, category := range []NoticeCategory{NoticeDispatchRetry, NoticeRecoveryRetry, NoticeRecoveryExhausted} {
			notice, err := NewStepNotice(category, 1, 3)
			if err != nil {
				t.Fatalf("NewStepNotice(%q) error: %v", category, err)
			}
			if !notice.Valid() {
				t.Fatalf("constructed StepNotice %+v reported itself invalid", notice)
			}
		}
	})
}

// The closed defaults must not overwrite a real normalized value, and in
// particular must not turn the two nonterminal dispatch conditions into
// terminal internal failures.
func TestFailClosedDefaultsPreserveConstructedValues(t *testing.T) {
	nonterminal := NormalizeFailure(OriginDispatch, "worker_busy", RetryClassSameRunTransient)
	if nonterminal.Reason() != ReasonNone {
		t.Fatalf("worker_busy reason = %q, want %q", nonterminal.Reason(), ReasonNone)
	}
	if nonterminal.Origin() != OriginDispatch || nonterminal.Code() != "worker_busy" {
		t.Fatalf("worker_busy normalized to origin %v code %q, want dispatch/worker_busy",
			nonterminal.Origin(), nonterminal.Code())
	}
	if nonterminal.RetryClass() != RetryClassSameRunTransient {
		t.Fatalf("worker_busy retry class = %v, want %v", nonterminal.RetryClass(), RetryClassSameRunTransient)
	}

	terminal := NormalizeFailure(OriginProviderTransport, "model_timeout", RetryClassNever)
	if terminal.Origin() != OriginProviderTransport || terminal.Code() != "model_timeout" ||
		terminal.Reason() != ReasonProviderFailure || terminal.RetryClass() != RetryClassNever {
		t.Fatalf("model_timeout normalized to %+v, want the provider-transport mapping", terminal)
	}

	cancellation := AbandonedCancellation()
	if cancellation.Origin() != OriginClientLifecycle || cancellation.Code() != CodeClientCancelled ||
		cancellation.Reason() != ReasonAbandoned {
		t.Fatalf("AbandonedCancellation() = %+v, want the client-lifecycle abandonment", cancellation)
	}
}

// The scan is the assertion that survives a future edit, so it has to see a
// constructor the way an author would actually write one. Returning
// *Cancellation is at least as likely as returning Cancellation, and an
// identifier-only scan would quietly report "no user-cancel constructor exists"
// while one sat in the package. The fixture under testdata is never compiled;
// it is scanner input pinning exactly the shapes that must be caught and the
// ones that must not.
func TestExportedFunctionsReturningSeesPointerAndWrappedConstructors(t *testing.T) {
	const fixture = "testdata/constructorscan"

	got := exportedFunctionsReturningIn(t, fixture, "Cancellation")
	want := []string{"PairedCancellations", "UserCancelledCancellation", "ValueCancellation", "WrappedCancellation"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cancellation constructors in %s = %v, want %v", fixture, got, want)
	}

	gotNotices := exportedFunctionsReturningIn(t, fixture, "StepNotice")
	wantNotices := []string{"NoticeConstructor"}
	if !reflect.DeepEqual(gotNotices, wantNotices) {
		t.Fatalf("notice constructors in %s = %v, want %v", fixture, gotNotices, wantNotices)
	}

	if absent := exportedFunctionsReturningIn(t, fixture, "Failure"); len(absent) != 0 {
		t.Fatalf("failure constructors in %s = %v, want none", fixture, absent)
	}
}

// assertNoRawText fails when any field of a normalized value carries text that
// came from outside the allowlist, including through fmt verbs that read
// unexported fields.
func assertNoRawText(t *testing.T, value any, raw string) {
	t.Helper()
	if raw == "" {
		return
	}
	reflected := reflect.ValueOf(value)
	for index := 0; index < reflected.NumField(); index++ {
		field := reflected.Field(index)
		if field.Kind() == reflect.String && strings.Contains(field.String(), raw) {
			t.Fatalf("field %s retained raw text %q", reflected.Type().Field(index).Name, raw)
		}
	}
	for _, rendered := range []string{fmt.Sprintf("%v", value), fmt.Sprintf("%+v", value)} {
		if strings.Contains(rendered, raw) {
			t.Fatalf("rendered value %q retained raw text %q", rendered, raw)
		}
	}
}

// exportedFunctionsReturning reads this package's own non-test sources so the
// assertion covers constructors no test calls.
func exportedFunctionsReturning(t *testing.T, typeName string) []string {
	t.Helper()
	return exportedFunctionsReturningIn(t, ".", typeName)
}

// exportedFunctionsReturningIn names every exported package-level function in
// directory that returns typeName, by value or by pointer, in any result
// position. Taking the directory as a parameter is what lets the scan itself be
// tested against a fixture instead of only against this package.
func exportedFunctionsReturningIn(t *testing.T, directory string, typeName string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	fileSet := token.NewFileSet()
	var found []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(directory, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || !function.Name.IsExported() || function.Type.Results == nil {
				continue
			}
			for _, result := range function.Type.Results.List {
				if resultNames(result.Type) == typeName {
					found = append(found, function.Name.Name)
					break
				}
			}
		}
	}
	sort.Strings(found)
	return found
}

// resultNames reports the type name a result expression denotes, unwrapping a
// pointer. A constructor returning *Cancellation is the same escape hatch as one
// returning Cancellation, so both must resolve to the same name.
func resultNames(expression ast.Expr) string {
	if pointer, ok := expression.(*ast.StarExpr); ok {
		expression = pointer.X
	}
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name
	}
	return ""
}

// TestApprovalFailureCategoryIsDeterminedByTheEventType pins the rule both the
// live approval writers and the 0011 rewrite depend on. If these two ever
// disagree, a client could tell a migrated approval failure from a freshly
// written one — which is exactly the observable difference the shared rule
// exists to prevent.
func TestApprovalFailureCategoryIsDeterminedByTheEventType(t *testing.T) {
	for _, tc := range []struct {
		eventType string
		want      Reason
		wantOK    bool
	}{
		{"approval.denied", ReasonPolicyDenied, true},
		{"approval.expired", ReasonExpired, true},
		// The non-failure lifecycle events must not acquire a category: a
		// granted approval has no failure to categorize, and a reader that
		// filters on the key's presence would stop being able to tell them
		// apart from refused ones.
		{"approval.requested", "", false},
		{"approval.approved", "", false},
		{"approval.consumed", "", false},
		{"agent.run.failed", "", false},
		{"", "", false},
	} {
		got, ok := ApprovalFailureCategory(tc.eventType)
		if got != tc.want || ok != tc.wantOK {
			t.Fatalf("ApprovalFailureCategory(%q) = %q, %t, want %q, %t", tc.eventType, got, ok, tc.want, tc.wantOK)
		}
	}
}
