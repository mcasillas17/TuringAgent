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
			_ = r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "tool_policy_decision_failed", Message: err.Error()}, started)
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
		_ = r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_DENIED, "", &turingv1.ToolCallError{Code: "tool_denied", Message: reason}, started)
		return nil, markBeaconPosted(fmt.Errorf("tool denied: %s", reason))
	case turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED:
		if r.WaitApproval == nil {
			_ = r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_DENIED, "", &turingv1.ToolCallError{Code: "approval_unavailable", Message: "approval waiter is not configured"}, started)
			return nil, markBeaconPosted(errors.New("approval waiter is not configured"))
		}
		approvalToken, err = r.WaitApproval(ctx, decision.GetApprovalId())
		if err != nil {
			if runWasTerminalized(err) {
				return nil, terminalRunError{err: err}
			}
			_ = r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_DENIED, "", &turingv1.ToolCallError{Code: "approval_denied", Message: err.Error()}, started)
			return nil, markBeaconPosted(err)
		}
	default:
		_ = r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_DENIED, "", &turingv1.ToolCallError{Code: "tool_denied", Message: "unsupported policy decision"}, started)
		return nil, markBeaconPosted(errors.New("unsupported tool policy decision"))
	}
	result, err := input.MCPClient.CallTool(ctx, input.ToolName, input.Args, approvalToken)
	if err != nil {
		_ = r.postAfter(ctx, input, toolCallID, turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED, "", &turingv1.ToolCallError{Code: "mcp_call_failed", Message: err.Error()}, started)
		return nil, markBeaconPosted(err)
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
