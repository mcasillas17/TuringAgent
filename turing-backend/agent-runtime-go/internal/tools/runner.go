package tools

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/safejson"
	"github.com/oklog/ulid/v2"
	"golang.org/x/sync/errgroup"
)

// MCPClient is the transport for one tool call. The variadic tokens are the
// server-issued capabilities for that call, in order: the approval token, then
// the provenance capability. Both may be empty, and a server that is issued
// neither is called exactly as before.
type MCPClient interface {
	CallTool(ctx context.Context, name string, args map[string]any, tokens ...string) (map[string]any, error)
}

type CallerEnforcedMCPClient interface {
	MCPClient
	CallToolWithCallerApproval(
		ctx context.Context,
		runID string,
		approvalID string,
		name string,
		args map[string]any,
	) (map[string]any, error)
}

type Runner struct {
	PostBeacon   func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)
	WaitApproval func(context.Context, string) (string, error)
	// ResumeApproved completes the lifecycle handshake for an approved call.
	//
	// It is separate from WaitApproval because the two answer different
	// questions. WaitApproval answers "did someone authorize this?", which is
	// about the approval. ResumeApproved answers "is this run durably running
	// again, under this worker, right now?", which is about the run — and only
	// that second answer makes it safe to commit a side effect. A runner with
	// no way to ask it fails the call rather than proceeding on the first.
	ResumeApproved func(context.Context, ApprovalResume) error
	// ApprovalWaitTimeout is the whole budget an approval-gated call may spend
	// waiting: for the decision AND for the resume that follows it. One budget
	// rather than two, because a second one would silently double how long a
	// run can hold its worker slot after a user already waited out the first.
	ApprovalWaitTimeout time.Duration
	MetadataFetchers    []func(context.Context) error
}

// ApprovalResume identifies one resume handshake for the worker that has to
// carry it out.
//
// Deadline is the instant the approval wait already had, not a fresh one. Zero
// means no approval budget is configured and the run context is the only bound.
type ApprovalResume struct {
	RunID      string
	ApprovalID string
	Deadline   time.Time
}

type RunInput struct {
	AgentID         turingv1.AgentId
	RunID           string
	TraceID         string
	ModelToolCallID string
	ServerName      string
	ToolName        string
	Args            map[string]any
	MCPClient       MCPClient
	Timeout         time.Duration
	TotalTimeout    time.Duration
}

func (r *Runner) Run(ctx context.Context, input RunInput) (map[string]any, error) {
	outcome, err := r.RunWithOutcome(ctx, input)
	return outcome.Result, err
}

type RunOutcome struct {
	Result        map[string]any
	SideEffecting bool
}

const fallbackAfterReportTimeout = 5 * time.Second

func (r *Runner) RunWithOutcome(ctx context.Context, input RunInput) (RunOutcome, error) {
	ctx, cancel := stageContext(ctx, input.TotalTimeout)
	defer cancel()
	if input.Args == nil {
		input.Args = map[string]any{}
	}
	arguments, err := safejson.ToStruct(input.Args)
	if err != nil {
		return RunOutcome{}, fmt.Errorf("serialize tool arguments: %w", err)
	}
	input.Args = arguments.AsMap()
	if err := r.fetchMetadata(ctx, input.Timeout); err != nil {
		return RunOutcome{}, err
	}
	toolCallID := NewToolCallID()
	started := time.Now()
	beforeBeacon, err := beacon(input, toolCallID, turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, turingv1.ToolCallStatus_TOOL_CALL_STATUS_UNSPECIFIED, "", nil, 0)
	if err != nil {
		return RunOutcome{}, err
	}
	decision, err := r.postWithTimeout(ctx, input.Timeout, beforeBeacon)
	if err != nil {
		operationErr := beaconReportingError{err: err}
		if !beaconWasPosted(err) {
			return RunOutcome{}, operationErr
		}
		if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "tool_policy_decision_failed"}, started); reportErr != nil {
			return RunOutcome{}, ReportingFailureError{operationErr: operationErr, reportErr: reportErr}
		}
		return RunOutcome{}, operationErr
	}
	if err := validatePolicyDecision(decision, toolCallID); err != nil {
		operationErr := beaconReportingError{err: markBeaconPosted(err)}
		if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "tool_policy_decision_invalid"}, started); reportErr != nil {
			return RunOutcome{}, ReportingFailureError{operationErr: operationErr, reportErr: reportErr}
		}
		return RunOutcome{}, operationErr
	}
	if decision.GetTerminalRun() {
		reason := decision.GetReason()
		if reason == "" {
			reason = "run terminalized by tool policy"
		}
		return RunOutcome{}, terminalRunError{err: errors.New(reason)}
	}
	approvalToken := ""
	// Minted by the orchestrator for this exact call and forwarded untouched.
	// It is empty for servers that do not write sandbox artifacts, and an empty
	// one is simply not sent.
	provenanceToken := decision.GetProvenanceToken()
	sideEffecting := false
	switch decision.GetDecision() {
	case turingv1.ToolPolicyDecision_DECISION_ALLOW:
	case turingv1.ToolPolicyDecision_DECISION_DENY:
		reason := decision.GetReason()
		if reason == "" {
			reason = "tool_denied"
		}
		return RunOutcome{}, ToolRejectedError{Reason: reason}
	case turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED:
		sideEffecting = !decision.GetReadOnly()
		if r.WaitApproval == nil {
			err = ApprovalWaitError{err: errors.New("approval waiter is not configured")}
			if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "approval_wait_failed"}, started); reportErr != nil {
				return RunOutcome{}, ReportingFailureError{operationErr: err, reportErr: reportErr}
			}
			return RunOutcome{}, err
		}
		// The budget is fixed here, before the decision is waited for, so the
		// resume below inherits whatever is left of it rather than starting a
		// second one of its own.
		approvalCtx, cancelApproval := approvalWaitContext(ctx, r.ApprovalWaitTimeout)
		approvalToken, err = r.WaitApproval(approvalCtx, decision.GetApprovalId())
		if err != nil {
			cancelApproval()
			if cause := terminalApprovalCause(ctx); cause != nil {
				return RunOutcome{}, terminalRunError{err: cause}
			}
			if runWasTerminalized(err) {
				return RunOutcome{}, terminalRunError{err: err}
			}
			operationErr := ApprovalWaitError{err: err}
			if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "approval_wait_failed"}, started); reportErr != nil {
				return RunOutcome{}, ReportingFailureError{operationErr: operationErr, reportErr: reportErr}
			}
			return RunOutcome{}, operationErr
		}
		// Holding the token is not permission to act. The run itself has to be
		// durably running again under this worker first, and nothing below this
		// line may touch the tool until it is.
		resumeErr := r.resumeApproved(approvalCtx, input, decision.GetApprovalId())
		cancelApproval()
		if resumeErr != nil {
			if cause := terminalApprovalCause(ctx); cause != nil {
				return RunOutcome{}, terminalRunError{err: cause}
			}
			if runWasTerminalized(resumeErr) {
				return RunOutcome{}, terminalRunError{err: resumeErr}
			}
			operationErr := ApprovalWaitError{err: resumeErr}
			if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "approval_wait_failed"}, started); reportErr != nil {
				return RunOutcome{}, ReportingFailureError{operationErr: operationErr, reportErr: reportErr}
			}
			return RunOutcome{}, operationErr
		}
	default:
		err = errors.New("unsupported tool policy decision")
		if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "tool_policy_decision_invalid"}, started); reportErr != nil {
			return RunOutcome{}, ReportingFailureError{operationErr: err, reportErr: reportErr}
		}
		return RunOutcome{}, markBeaconPosted(err)
	}
	callCtx, cancel := stageContext(ctx, input.Timeout)
	var result map[string]any
	if callerEnforced, ok := input.MCPClient.(CallerEnforcedMCPClient); ok {
		result, err = callerEnforced.CallToolWithCallerApproval(
			callCtx,
			input.RunID,
			decision.GetApprovalId(),
			input.ToolName,
			input.Args,
		)
	} else {
		result, err = input.MCPClient.CallTool(callCtx, input.ToolName, input.Args, approvalToken, provenanceToken)
	}
	cancel()
	if err != nil {
		var operationErr error = RecoverableToolError{err: err}
		if sideEffecting {
			operationErr = SideEffectUnknownError{err: err}
		}
		if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "mcp_call_failed"}, started); reportErr != nil {
			return RunOutcome{}, ReportingFailureError{operationErr: operationErr, reportErr: reportErr}
		}
		return RunOutcome{}, operationErr
	}
	outcome := RunOutcome{Result: result, SideEffecting: sideEffecting}
	if err := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED, safejson.Summary(result, 500), nil, started); err != nil {
		if sideEffecting {
			return outcome, SideEffectCommittedError{err: err}
		}
		return outcome, ReportingFailureError{operationErr: errors.New("tool call completed under a non-side-effecting decision"), reportErr: err}
	}
	return outcome, nil
}

// resumeApproved completes the lifecycle handshake between an approved decision
// and the call it authorizes.
//
// It is required rather than optional. A runner that cannot ask whether the run
// was durably resumed has no way to tell "approved and running under me" from
// "approved, then handed to somebody else while I was waiting", and the second
// one is exactly when committing the side effect does damage.
func (r *Runner) resumeApproved(ctx context.Context, input RunInput, approvalID string) error {
	if r.ResumeApproved == nil {
		return errors.New("approval resume is not configured")
	}
	return r.ResumeApproved(ctx, ApprovalResume{
		RunID:      input.RunID,
		ApprovalID: approvalID,
		Deadline:   contextDeadline(ctx),
	})
}

// approvalWaitContext bounds the whole approval phase — decision and resume —
// by one budget.
func approvalWaitContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func contextDeadline(ctx context.Context) time.Time {
	deadline, ok := ctx.Deadline()
	if !ok {
		return time.Time{}
	}
	return deadline
}

func (r *Runner) fetchMetadata(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := stageContext(ctx, timeout)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)
	for _, fetch := range r.MetadataFetchers {
		fetch := fetch
		group.Go(func() error { return fetch(ctx) })
	}
	return group.Wait()
}

// postAfter reports how a tool call ended.
//
// The after-beacon carries a code from this package's own allowlisted
// vocabulary and no message. The wrapped error still travels back to the caller
// as a Go error — that is where a developer reads it — but it does not cross
// the beacon boundary, because everything on a beacon can become a durable
// public payload.
func (r *Runner) postAfter(ctx context.Context, input RunInput, toolCallID string, status turingv1.ToolCallStatus, summary string, callErr *turingv1.ToolCallError, started time.Time) error {
	reportCtx, cancel := afterReportContext(ctx, input.Timeout)
	defer cancel()
	afterBeacon, err := beacon(input, toolCallID, turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER, status, summary, callErr, time.Since(started).Milliseconds())
	if err != nil {
		return err
	}
	decision, err := r.postWithTimeout(reportCtx, input.Timeout, afterBeacon)
	if err == nil {
		err = validatePolicyDecision(decision, toolCallID)
		if err == nil && decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
			err = fmt.Errorf("after tool policy decision must be allow, got %s", decision.GetDecision().String())
		}

		if err != nil {
			err = markBeaconPosted(err)
		}
	}
	return err
}

func afterReportContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = fallbackAfterReportTimeout
	}
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}

func validatePolicyDecision(decision *turingv1.ToolPolicyDecision, toolCallID string) error {
	if decision == nil {
		return errors.New("tool policy decision is required")
	}
	if decision.GetToolCallId() == "" {
		return errors.New("tool policy decision tool_call_id is required")
	}
	if decision.GetToolCallId() != toolCallID {
		return fmt.Errorf("tool policy decision tool_call_id %q does not match beacon %q", decision.GetToolCallId(), toolCallID)
	}
	return nil
}

func (r *Runner) postWithTimeout(ctx context.Context, timeout time.Duration, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
	if r.PostBeacon == nil {
		return nil, errors.New("tool beacon poster is not configured")
	}
	stageCtx, cancel := stageContext(ctx, timeout)
	defer cancel()
	return r.PostBeacon(stageCtx, beacon)
}

func stageContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}

type postedBeaconError interface {
	BeaconPosted() bool
}

func beaconWasPosted(err error) bool {
	var posted postedBeaconError
	return errors.As(err, &posted) && posted.BeaconPosted()
}

func BeaconWasPosted(err error) bool {
	return beaconWasPosted(err)
}

type terminalRunState interface {
	RunTerminal() bool
}

type terminalApprovalState interface {
	TerminalApproval() bool
}

func runWasTerminalized(err error) bool {
	var terminal terminalRunState
	return errors.As(err, &terminal) && terminal.RunTerminal()
}

func terminalApprovalCause(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	cause := context.Cause(ctx)
	var terminal terminalApprovalState
	if errors.As(cause, &terminal) && terminal.TerminalApproval() {
		return cause
	}
	return nil
}

func RunWasTerminalized(err error) bool {
	return runWasTerminalized(err)
}

type committedSideEffect interface {
	SideEffectCommitted() bool
}

func SideEffectWasCommitted(err error) bool {
	var committed committedSideEffect
	return errors.As(err, &committed) && committed.SideEffectCommitted()
}

type reportingFailure interface {
	ReportingFailed() bool
}

func ReportingFailed(err error) bool {
	var reporting reportingFailure
	return errors.As(err, &reporting) && reporting.ReportingFailed()
}

type ReportingFailureError struct {
	operationErr error
	reportErr    error
}

func (e ReportingFailureError) Error() string {
	return fmt.Sprintf("%v; report tool outcome: %v", e.operationErr, e.reportErr)
}
func (e ReportingFailureError) Unwrap() []error       { return []error{e.operationErr, e.reportErr} }
func (e ReportingFailureError) BeaconPosted() bool    { return true }
func (e ReportingFailureError) ReportingFailed() bool { return true }

type beaconReportingError struct {
	err error
}

func (e beaconReportingError) Error() string         { return e.err.Error() }
func (e beaconReportingError) Unwrap() error         { return e.err }
func (e beaconReportingError) BeaconPosted() bool    { return beaconWasPosted(e.err) }
func (e beaconReportingError) ReportingFailed() bool { return true }

type uncertainSideEffect interface {
	SideEffectUncertain() bool
}

func SideEffectWasUncertain(err error) bool {
	var uncertain uncertainSideEffect
	return errors.As(err, &uncertain) && uncertain.SideEffectUncertain()
}

type SideEffectUnknownError struct {
	err error
}

func (e SideEffectUnknownError) Error() string             { return e.err.Error() }
func (e SideEffectUnknownError) Unwrap() error             { return e.err }
func (e SideEffectUnknownError) BeaconPosted() bool        { return true }
func (e SideEffectUnknownError) SideEffectUncertain() bool { return true }

type RecoverableToolError struct {
	err error
}

func (e RecoverableToolError) Error() string      { return e.err.Error() }
func (e RecoverableToolError) Unwrap() error      { return e.err }
func (e RecoverableToolError) BeaconPosted() bool { return true }
func (e RecoverableToolError) Recoverable() bool  { return true }

type ToolRejectedError struct {
	Reason string
}

func (e ToolRejectedError) Error() string {
	return fmt.Sprintf("tool denied: %s", e.Reason)
}
func (e ToolRejectedError) BeaconPosted() bool { return true }
func (e ToolRejectedError) Recoverable() bool  { return true }

type approvalWaitFailure interface {
	ApprovalWaitFailed() bool
}

func ApprovalWaitFailed(err error) bool {
	var waitFailure approvalWaitFailure
	return errors.As(err, &waitFailure) && waitFailure.ApprovalWaitFailed()
}

type ApprovalWaitError struct {
	err error
}

func (e ApprovalWaitError) Error() string            { return fmt.Sprintf("wait for tool approval: %v", e.err) }
func (e ApprovalWaitError) Unwrap() error            { return e.err }
func (e ApprovalWaitError) BeaconPosted() bool       { return true }
func (e ApprovalWaitError) ApprovalWaitFailed() bool { return true }

type TerminalApprovalError struct {
	Status string
}

func (e TerminalApprovalError) Error() string {
	return "approval " + e.Status
}

func (e TerminalApprovalError) TerminalApproval() bool {
	return e.Status == "denied" || e.Status == "expired"
}

func (e TerminalApprovalError) RunTerminal() bool {
	return e.TerminalApproval()
}

type SideEffectCommittedError struct {
	err error
}

func (e SideEffectCommittedError) Error() string             { return e.err.Error() }
func (e SideEffectCommittedError) Unwrap() error             { return e.err }
func (e SideEffectCommittedError) BeaconPosted() bool        { return true }
func (e SideEffectCommittedError) SideEffectCommitted() bool { return true }

type terminalRunError struct {
	err error
}

func (e terminalRunError) Error() string      { return e.err.Error() }
func (e terminalRunError) Unwrap() error      { return e.err }
func (e terminalRunError) BeaconPosted() bool { return true }
func (e terminalRunError) RunTerminal() bool  { return true }

type beaconPostedError struct {
	err error
}

func (e beaconPostedError) Error() string      { return e.err.Error() }
func (e beaconPostedError) Unwrap() error      { return e.err }
func (e beaconPostedError) BeaconPosted() bool { return true }

func markBeaconPosted(err error) error {
	if err == nil || beaconWasPosted(err) {
		return err
	}
	return beaconPostedError{err: err}
}

func beacon(input RunInput, toolCallID string, phase turingv1.ToolCallPhase, status turingv1.ToolCallStatus, summary string, callErr *turingv1.ToolCallError, durationMS int64) (*turingv1.ToolCallBeacon, error) {
	args, err := safejson.ToStruct(input.Args)
	if err != nil {
		return nil, fmt.Errorf("serialize tool arguments: %w", err)
	}
	return &turingv1.ToolCallBeacon{
		Phase:           phase,
		ToolCallId:      toolCallID,
		AgentId:         input.AgentID,
		ServerName:      input.ServerName,
		ToolName:        input.ToolName,
		Args:            args,
		Status:          status,
		ResultSummary:   summary,
		DurationMs:      durationMS,
		Error:           callErr,
		RunId:           input.RunID,
		TraceId:         input.TraceID,
		ModelToolCallId: input.ModelToolCallID,
	}, nil
}

func NewToolCallID() string {
	return "call_" + ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}
