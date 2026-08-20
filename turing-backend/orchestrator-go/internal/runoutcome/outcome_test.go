package runoutcome

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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

// The two approved mapping tables are normative: the call site supplies a typed
// origin and the normalizer decides the public reason. Keying on (origin, code)
// rather than the code alone is what lets unknown_tool mean "tool failure" from
// the tool-infrastructure path and "policy denied" from the policy path.
func TestNormalizeFailureMapsEveryExistingCode(t *testing.T) {
	tests := []struct {
		name       string
		origin     Origin
		code       string
		wantOrigin Origin
		wantCode   string
		wantReason Reason
	}{
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
	}

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
	entries, err := os.ReadDir(".")
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
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || !function.Name.IsExported() || function.Type.Results == nil {
				continue
			}
			for _, result := range function.Type.Results.List {
				if identifier, ok := result.Type.(*ast.Ident); ok && identifier.Name == typeName {
					found = append(found, function.Name.Name)
				}
			}
		}
	}
	sort.Strings(found)
	return found
}
