// Package runoutcome closes the durable ingestion boundary for run failures.
//
// Provider, tool, and worker text is untrusted: anything a normalized value
// holds may be persisted on a run, a job, a tool call, or a public event, and
// then returned to clients. So a normalized value carries only a typed origin,
// an allowlisted internal code, a closed public reason, and internal retry
// policy. It never carries a diagnostic message, and it can only be built by
// the constructors below — its fields are private and it has no raw-string
// setter.
//
// Classification never inspects message text. The reporting call site supplies
// a typed origin, and the (origin, code) pair selects the public reason. That
// is what lets the same unknown_tool code mean "tool failure" from the
// tool-infrastructure path and "policy denied" from the policy path, and it is
// why an unrecognized pair fails closed instead of guessing.
package runoutcome

import (
	"errors"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// Origin is where a failure actually came from, as reported by the call site.
type Origin uint8

const (
	OriginUnspecified Origin = iota
	OriginUnknown
	OriginContextAssembly
	OriginExternalProvider
	OriginProviderConfiguration
	OriginProviderProtocol
	OriginProviderTransport
	OriginProviderOutputGuard
	OriginToolInfrastructure
	OriginToolExecution
	OriginToolGuard
	OriginToolPolicy
	OriginApprovalTransport
	OriginApprovalExpiry
	OriginAutomationPolicy
	OriginWorkerRuntime
	OriginDispatch
	OriginRecovery
	OriginOrchestratorInternal
	OriginClientLifecycle
)

// RetryClass is internal dispatch policy. It is never a user-facing promise
// that repeating the request is safe, and it never becomes public outcome text.
type RetryClass uint8

const (
	RetryClassUnspecified RetryClass = iota
	RetryClassUnknown
	RetryClassNever
	RetryClassSameRunTransient
)

// Reason is the closed public outcome vocabulary persisted in
// agent_runs.outcome_reason and projected into RunState.
type Reason string

const (
	ReasonNone                   Reason = "none"
	ReasonUnknown                Reason = "unknown"
	ReasonLegacyUnknown          Reason = "legacy_unknown"
	ReasonCompletedNoContent     Reason = "completed_no_content"
	ReasonUserCancelled          Reason = "user_cancelled"
	ReasonAbandoned              Reason = "abandoned"
	ReasonExpired                Reason = "expired"
	ReasonContextLimit           Reason = "context_limit"
	ReasonProviderFailure        Reason = "provider_failure"
	ReasonToolFailure            Reason = "tool_failure"
	ReasonPolicyDenied           Reason = "policy_denied"
	ReasonRetriesExhausted       Reason = "retries_exhausted"
	ReasonRecoveryInterrupted    Reason = "recovery_interrupted"
	ReasonSideEffectUncertain    Reason = "side_effect_uncertain"
	ReasonApprovalDeliveryFailed Reason = "approval_delivery_failed"
	ReasonInternalFailure        Reason = "internal_failure"
)

const (
	// CodeUnknown replaces any code that is not in the allowlist. Keeping the
	// reported code would reopen the text channel this package exists to close.
	CodeUnknown = "unknown"
	// CodeClientCancelled is the only cancellation code the current transport
	// path can justify.
	CodeClientCancelled = "client_cancelled"
)

// ErrUnsupportedNotice rejects a run-step notice that is not fully allowlisted.
// It names the class of problem and never the rejected values.
var ErrUnsupportedNotice = errors.New("unsupported run step notice")

// MaxNoticeAttempts bounds notice counters so a caller cannot smuggle an
// arbitrary number into a public payload.
const MaxNoticeAttempts = 1000

// Failure is a normalized run or subsidiary failure.
//
// A Failure whose Reason is ReasonNone describes a nonterminal dispatch
// condition: it explains a requeue and must not terminalize a run. A Failure
// whose Reason is ReasonAbandoned describes the ambiguous transport-loss path
// and belongs on a cancellation transition, not a failed one.
//
// The accessors normalize on read so the Go zero value cannot be mistaken for a
// benign outcome. A zero Failure reaches a reader whenever a map lookup misses,
// a struct field is never assigned, or a caller keeps the value a rejecting
// constructor returned; reporting an unspecified origin and an empty reason
// there would read as "nothing failed" and, worse, as the nonterminal dispatch
// condition that leaves a run unterminalized.
type Failure struct {
	origin Origin
	code   string
	reason Reason
	retry  RetryClass
}

func (f Failure) Origin() Origin {
	if f.origin == OriginUnspecified {
		return OriginUnknown
	}
	return f.origin
}

func (f Failure) Code() string {
	if f.code == "" {
		return CodeUnknown
	}
	return f.code
}

func (f Failure) Reason() Reason {
	if f.reason == "" {
		return ReasonInternalFailure
	}
	return f.reason
}

func (f Failure) RetryClass() RetryClass { return normalizeRetryClass(f.retry) }

// Cancellation is a normalized run cancellation.
//
// Its accessors normalize on read for the same reason Failure's do, but its
// closed default is the abandonment this transport path can actually justify:
// the zero value describes a run whose client went away, so it reports the
// client-lifecycle origin and the abandoned reason rather than an unspecified
// origin or any claim of user intent.
type Cancellation struct {
	origin Origin
	code   string
	reason Reason
}

func (c Cancellation) Origin() Origin {
	if c.origin == OriginUnspecified {
		return OriginClientLifecycle
	}
	return c.origin
}

func (c Cancellation) Code() string {
	if c.code == "" {
		return CodeClientCancelled
	}
	return c.code
}

func (c Cancellation) Reason() Reason {
	if c.reason == "" {
		return ReasonAbandoned
	}
	return c.reason
}

// AbandonedCancellation is the only cancellation this product can honestly
// report. ChatService uses one stream-cancellation signal for a deliberate stop
// and for an unkeyed transport loss, and the client exposes no cancel
// affordance, so nothing here may claim user intent. A user-cancelled
// constructor may only be added alongside an explicit typed cancel-intent RPC.
func AbandonedCancellation() Cancellation {
	return Cancellation{origin: OriginClientLifecycle, code: CodeClientCancelled, reason: ReasonAbandoned}
}

// NoticeCategory is the allowlisted vocabulary for rewritten failure-like
// agent.run.step notices. Nonfailure notices keep their existing governed path.
type NoticeCategory string

const (
	NoticeDispatchRetry     NoticeCategory = "dispatch_retry"
	NoticeRecoveryRetry     NoticeCategory = "recovery_retry"
	NoticeRecoveryExhausted NoticeCategory = "recovery_exhausted"
)

// ApprovalFailureCategory maps an approval event type to the single category
// that event is allowed to carry, and reports whether the type is a failure
// event at all.
//
// The category is read off the event type the server chose, never off a failure
// code that arrived from somewhere else. An approval that ends in denial ended
// because policy said no; an approval that ends in expiry ended because it was
// no longer usable. Both facts are fully determined by which event is being
// written, so deriving the category from anything else can only introduce
// disagreement — and did: two hand-built expiry payloads once labelled
// themselves with the run's own failure reason and with side_effect_uncertain,
// neither of which describes an approval.
//
// It lives here, beside the vocabulary itself, because both the live writers
// and the migration that rewrites historical rows must answer this question
// identically. If they answered it separately they could drift, and a client
// would be able to tell a migrated approval.expired from a freshly written one.
//
// The non-failure approval events (requested, approved, consumed) get no
// category: there is no failure to categorize, and inventing one would make a
// granted approval indistinguishable from a refused one to a reader that
// filters on the key's presence.
func ApprovalFailureCategory(eventType string) (Reason, bool) {
	switch eventType {
	case "approval.denied":
		return ReasonPolicyDenied, true
	case "approval.expired":
		return ReasonExpired, true
	default:
		return "", false
	}
}

// ToolCallFailureCategory is the same rule for a tool call's terminal event,
// and exists for the same reason: the live writer and the 0011 rewrite must
// answer it identically or a client could tell a migrated tool.call.failed from
// a freshly written one.
//
// A tool call that ends denied was refused by policy; one that ends failed
// broke while running. Which event is being written settles that, so no code
// the beacon carried — and no message it carried — participates. tool.call.
// started and tool.call.completed describe no failure and get no category.
func ToolCallFailureCategory(eventType string) (Reason, bool) {
	switch eventType {
	case "tool.call.failed":
		return ReasonToolFailure, true
	case "tool.call.denied":
		return ReasonPolicyDenied, true
	default:
		return "", false
	}
}

// StepNotice is a failure-like run-step projection: a category plus bounded
// counters. It accepts no display string, so the sentence a reader shows has to
// be derived from the category rather than persisted by the backend. Deriving
// it is the planned Task 9/10 client work; what is committed here is the
// category the backend is allowed to publish.
type StepNotice struct {
	category    NoticeCategory
	attempt     int32
	maxAttempts int32
}

func (n StepNotice) Category() NoticeCategory { return n.category }
func (n StepNotice) Attempt() int32           { return n.attempt }
func (n StepNotice) MaxAttempts() int32       { return n.maxAttempts }

// Valid reports whether this notice came from NewStepNotice. Unlike a failure or
// a cancellation, a notice has no honest closed default: its category, attempt,
// and budget are all load-bearing, and defaulting an uninitialized value to
// "dispatch retry, attempt 1 of 1" would persist a retry that never happened.
// So the zero value stays zero and answers false here, and every projection or
// writer must reject an invalid notice instead of emitting one.
func (n StepNotice) Valid() bool {
	_, err := NewStepNotice(n.category, n.attempt, n.maxAttempts)
	return err == nil
}

// NewStepNotice builds a notice or fails closed. attempt is the attempt this
// notice is about and maxAttempts is the configured budget, so a valid notice
// always satisfies 1 <= attempt <= maxAttempts <= MaxNoticeAttempts.
func NewStepNotice(category NoticeCategory, attempt int32, maxAttempts int32) (StepNotice, error) {
	switch category {
	case NoticeDispatchRetry, NoticeRecoveryRetry, NoticeRecoveryExhausted:
	default:
		return StepNotice{}, ErrUnsupportedNotice
	}
	if attempt < 1 || maxAttempts < 1 || maxAttempts > MaxNoticeAttempts || attempt > maxAttempts {
		return StepNotice{}, ErrUnsupportedNotice
	}
	return StepNotice{category: category, attempt: attempt, maxAttempts: maxAttempts}, nil
}

type failureKey struct {
	origin Origin
	code   string
}

// failureReasons is the approved mapping. Every entry is a code this codebase
// actually writes today, paired with the origin its reporting call site knows.
var failureReasons = map[failureKey]Reason{
	// Run-terminal codes.
	{OriginContextAssembly, "message_fetch_failed"}:             ReasonInternalFailure,
	{OriginExternalProvider, "external_agent_unavailable"}:      ReasonProviderFailure,
	{OriginProviderConfiguration, "model_provider_unavailable"}: ReasonProviderFailure,
	{OriginToolInfrastructure, "tool_discovery_failed"}:         ReasonToolFailure,
	{OriginContextAssembly, "context_budget_exceeded"}:          ReasonContextLimit,
	{OriginProviderTransport, "model_timeout"}:                  ReasonProviderFailure,
	{OriginProviderTransport, "model_stream_failed"}:            ReasonProviderFailure,
	{OriginProviderOutputGuard, "model_output_limit_exceeded"}:  ReasonProviderFailure,
	{OriginProviderProtocol, "model_unavailable"}:               ReasonProviderFailure,
	{OriginProviderProtocol, "model_auth_failed"}:               ReasonProviderFailure,
	{OriginProviderProtocol, "model_request_failed"}:            ReasonProviderFailure,
	{OriginExternalProvider, "model_error"}:                     ReasonProviderFailure,
	{OriginProviderProtocol, "model_quota_exceeded"}:            ReasonProviderFailure,
	{OriginProviderProtocol, "model_bad_chunk"}:                 ReasonProviderFailure,
	{OriginProviderTransport, "model_stream_error"}:             ReasonProviderFailure,
	{OriginToolExecution, "tool_call_failed"}:                   ReasonToolFailure,
	{OriginToolGuard, "tool_call_limit_exceeded"}:               ReasonToolFailure,
	{OriginToolGuard, "tool_result_limit_exceeded"}:             ReasonToolFailure,
	{OriginWorkerRuntime, "runtime_error"}:                      ReasonInternalFailure,
	{OriginDispatch, "retries_exhausted"}:                       ReasonRetriesExhausted,
	{OriginRecovery, "job_timeout"}:                             ReasonRecoveryInterrupted,
	{OriginRecovery, "side_effect_uncertain"}:                   ReasonSideEffectUncertain,
	{OriginApprovalTransport, "approval_delivery_failed"}:       ReasonApprovalDeliveryFailed,
	{OriginApprovalExpiry, "approval_expired"}:                  ReasonExpired,
	{OriginAutomationPolicy, "automation_approval_failed"}:      ReasonPolicyDenied,
	{OriginAutomationPolicy, "automation_tool_not_allowlisted"}: ReasonPolicyDenied,
	{OriginClientLifecycle, "client_cancelled"}:                 ReasonAbandoned,

	// Nonterminal dispatch conditions: the run keeps outcome none while its
	// lifecycle moves through recovering or queued. Terminalizing an exhausted
	// budget is the recovery path's decision, reported under its own code.
	{OriginDispatch, "worker_busy"}:        ReasonNone,
	{OriginDispatch, "worker_unavailable"}: ReasonNone,

	// Subsidiary codes. They do not decide the run outcome, but their durable
	// public payloads are normalized before write.
	{OriginToolPolicy, "tool_policy_decision_failed"}:     ReasonPolicyDenied,
	{OriginToolPolicy, "tool_policy_decision_invalid"}:    ReasonPolicyDenied,
	{OriginApprovalTransport, "approval_wait_failed"}:     ReasonApprovalDeliveryFailed,
	{OriginToolExecution, "mcp_call_failed"}:              ReasonToolFailure,
	{OriginToolInfrastructure, "unknown_tool"}:            ReasonToolFailure,
	{OriginToolPolicy, "unknown_tool"}:                    ReasonPolicyDenied,
	{OriginToolInfrastructure, "tool_runner_unavailable"}: ReasonToolFailure,
	{OriginRecovery, "worker_unavailable"}:                ReasonRecoveryInterrupted,
	{OriginClientLifecycle, "cancelled"}:                  ReasonAbandoned,
}

// NormalizeFailure maps a typed report onto the approved outcome vocabulary.
// An unrecognized origin fails closed to an internal failure; an unrecognized
// code keeps its typed origin family but drops to that family's generic
// outcome, and its retry request is ignored because nothing about the pair is
// known well enough to justify retrying.
func NormalizeFailure(origin Origin, code string, retry RetryClass) Failure {
	typedOrigin, recognized := recognizedOrigin(origin)
	if !recognized {
		return Failure{origin: OriginUnknown, code: CodeUnknown, reason: ReasonInternalFailure, retry: RetryClassNever}
	}
	reason, allowlisted := failureReasons[failureKey{origin: typedOrigin, code: code}]
	if !allowlisted {
		return Failure{
			origin: typedOrigin,
			code:   CodeUnknown,
			reason: unknownCodeReason(typedOrigin),
			retry:  RetryClassNever,
		}
	}
	return Failure{origin: typedOrigin, code: code, reason: reason, retry: normalizeRetryClass(retry)}
}

// NormalizeRuntimeFailure adapts a worker report. It switches on generated enum
// values and sends every unrecognized numeric to the domain unknown through the
// default arm, so a newer worker's enum value can never panic here, be rendered
// as a number, or widen retrying. It never calls a generated name accessor.
func NormalizeRuntimeFailure(origin turingv1.FailureOrigin, code string, retry turingv1.AutomaticRetryClass) Failure {
	return NormalizeFailure(originFromProto(origin), code, retryClassFromProto(retry))
}

func originFromProto(origin turingv1.FailureOrigin) Origin {
	switch origin {
	case turingv1.FailureOrigin_FAILURE_ORIGIN_CONTEXT_ASSEMBLY:
		return OriginContextAssembly
	case turingv1.FailureOrigin_FAILURE_ORIGIN_EXTERNAL_PROVIDER:
		return OriginExternalProvider
	case turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_CONFIGURATION:
		return OriginProviderConfiguration
	case turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL:
		return OriginProviderProtocol
	case turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT:
		return OriginProviderTransport
	case turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_OUTPUT_GUARD:
		return OriginProviderOutputGuard
	case turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_INFRASTRUCTURE:
		return OriginToolInfrastructure
	case turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_EXECUTION:
		return OriginToolExecution
	case turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_GUARD:
		return OriginToolGuard
	case turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_POLICY:
		return OriginToolPolicy
	case turingv1.FailureOrigin_FAILURE_ORIGIN_APPROVAL_TRANSPORT:
		return OriginApprovalTransport
	case turingv1.FailureOrigin_FAILURE_ORIGIN_APPROVAL_EXPIRY:
		return OriginApprovalExpiry
	case turingv1.FailureOrigin_FAILURE_ORIGIN_AUTOMATION_POLICY:
		return OriginAutomationPolicy
	case turingv1.FailureOrigin_FAILURE_ORIGIN_WORKER_RUNTIME:
		return OriginWorkerRuntime
	case turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH:
		return OriginDispatch
	case turingv1.FailureOrigin_FAILURE_ORIGIN_RECOVERY:
		return OriginRecovery
	case turingv1.FailureOrigin_FAILURE_ORIGIN_ORCHESTRATOR_INTERNAL:
		return OriginOrchestratorInternal
	case turingv1.FailureOrigin_FAILURE_ORIGIN_CLIENT_LIFECYCLE:
		return OriginClientLifecycle
	default:
		return OriginUnknown
	}
}

func retryClassFromProto(retry turingv1.AutomaticRetryClass) RetryClass {
	switch retry {
	case turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT:
		return RetryClassSameRunTransient
	default:
		return RetryClassNever
	}
}

// recognizedOrigin reports whether an origin is a real reporting site. The
// unspecified value means the field was absent, the unknown value means a
// newer reporter chose something this build cannot interpret, and an
// out-of-range value means the same thing from Go callers.
func recognizedOrigin(origin Origin) (Origin, bool) {
	if origin <= OriginUnknown || origin > OriginClientLifecycle {
		return OriginUnknown, false
	}
	return origin, true
}

func unknownCodeReason(origin Origin) Reason {
	switch origin {
	case OriginExternalProvider, OriginProviderConfiguration, OriginProviderProtocol,
		OriginProviderTransport, OriginProviderOutputGuard:
		return ReasonProviderFailure
	case OriginToolInfrastructure, OriginToolExecution, OriginToolGuard, OriginToolPolicy:
		return ReasonToolFailure
	default:
		return ReasonInternalFailure
	}
}

func normalizeRetryClass(retry RetryClass) RetryClass {
	if retry == RetryClassSameRunTransient {
		return RetryClassSameRunTransient
	}
	return RetryClassNever
}
