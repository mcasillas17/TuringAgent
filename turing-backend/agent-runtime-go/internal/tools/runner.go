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

type MCPClient interface {
	CallTool(ctx context.Context, name string, args map[string]any, approvalToken ...string) (map[string]any, error)
}

type Runner struct {
	PostBeacon       func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)
	WaitApproval     func(context.Context, string) (string, error)
	MetadataFetchers []func(context.Context) error
}

type RunInput struct {
	AgentID    turingv1.AgentId
	RunID      string
	TraceID    string
	ToolCallID string
	ServerName string
	ToolName   string
	Args       map[string]any
	MCPClient  MCPClient
}

func (r *Runner) Run(ctx context.Context, input RunInput) (map[string]any, error) {
	if input.Args == nil {
		input.Args = map[string]any{}
	}
	if err := r.fetchMetadata(ctx); err != nil {
		return nil, err
	}
	toolCallID := input.ToolCallID
	if toolCallID == "" {
		toolCallID = NewToolCallID()
	}
	started := time.Now()
	decision, err := r.post(ctx, beacon(input, toolCallID, turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, turingv1.ToolCallStatus_TOOL_CALL_STATUS_UNSPECIFIED, "", nil, 0))
	if err != nil {
		if beaconWasPosted(err) {
			if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "tool_policy_decision_failed", Message: err.Error()}, started); reportErr != nil {
				return nil, ReportingFailureError{operationErr: err, reportErr: reportErr}
			}
			return nil, markBeaconPosted(err)
		}
		return nil, err
	}
	approvalToken := ""
	switch decision.GetDecision() {
	case turingv1.ToolPolicyDecision_DECISION_ALLOW:
	case turingv1.ToolPolicyDecision_DECISION_DENY:
		reason := decision.GetReason()
		if reason == "" {
			reason = "tool_denied"
		}
		return nil, ToolRejectedError{Reason: reason}
	case turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED:
		if r.WaitApproval == nil {
			err = ApprovalWaitError{err: errors.New("approval waiter is not configured")}
			if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "approval_wait_failed", Message: err.Error()}, started); reportErr != nil {
				return nil, ReportingFailureError{operationErr: err, reportErr: reportErr}
			}
			return nil, err
		}
		approvalToken, err = r.WaitApproval(ctx, decision.GetApprovalId())
		if err != nil {
			if runWasTerminalized(err) {
				return nil, terminalRunError{err: err}
			}
			operationErr := ApprovalWaitError{err: err}
			if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "approval_wait_failed", Message: err.Error()}, started); reportErr != nil {
				return nil, ReportingFailureError{operationErr: operationErr, reportErr: reportErr}
			}
			return nil, operationErr
		}
	default:
		err = errors.New("unsupported tool policy decision")
		if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_DENIED, "", &turingv1.ToolCallError{Code: "tool_denied", Message: err.Error()}, started); reportErr != nil {
			return nil, ReportingFailureError{operationErr: err, reportErr: reportErr}
		}
		return nil, markBeaconPosted(err)
	}
	result, err := input.MCPClient.CallTool(ctx, input.ToolName, input.Args, approvalToken)
	if err != nil {
		operationErr := SideEffectUnknownError{err: err}
		if reportErr := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "mcp_call_failed", Message: err.Error()}, started); reportErr != nil {
			return nil, ReportingFailureError{operationErr: operationErr, reportErr: reportErr}
		}
		return nil, operationErr
	}
	if err := r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED, safejson.Summary(result, 500), nil, started); err != nil {
		return nil, SideEffectCommittedError{err: err}
	}
	return result, nil
}

func (r *Runner) fetchMetadata(ctx context.Context) error {
	group, ctx := errgroup.WithContext(ctx)
	for _, fetch := range r.MetadataFetchers {
		fetch := fetch
		group.Go(func() error { return fetch(ctx) })
	}
	return group.Wait()
}

func (r *Runner) postAfter(ctx context.Context, input RunInput, toolCallID string, status turingv1.ToolCallStatus, summary string, callErr *turingv1.ToolCallError, started time.Time) error {
	_, err := r.post(ctx, beacon(input, toolCallID, turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER, status, summary, callErr, time.Since(started).Milliseconds()))
	return err
}

func (r *Runner) post(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
	if r.PostBeacon == nil {
		return nil, errors.New("tool beacon poster is not configured")
	}
	return r.PostBeacon(ctx, beacon)
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

func runWasTerminalized(err error) bool {
	var terminal terminalRunState
	return errors.As(err, &terminal) && terminal.RunTerminal()
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

func beacon(input RunInput, toolCallID string, phase turingv1.ToolCallPhase, status turingv1.ToolCallStatus, summary string, callErr *turingv1.ToolCallError, durationMS int64) *turingv1.ToolCallBeacon {
	args, _ := safejson.ToStruct(input.Args)
	return &turingv1.ToolCallBeacon{
		Phase:         phase,
		ToolCallId:    toolCallID,
		AgentId:       input.AgentID,
		ServerName:    input.ServerName,
		ToolName:      input.ToolName,
		Args:          args,
		Status:        status,
		ResultSummary: summary,
		DurationMs:    durationMS,
		Error:         callErr,
		RunId:         input.RunID,
		TraceId:       input.TraceID,
	}
}

func NewToolCallID() string {
	return "call_" + ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}
