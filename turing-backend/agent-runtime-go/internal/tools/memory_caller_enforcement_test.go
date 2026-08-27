package tools

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/mcp"
	"google.golang.org/protobuf/types/known/structpb"
)

type memoryToolRPC struct {
	runID      string
	approvalID string
	toolName   string
}

func (r *memoryToolRPC) ListMemoryTools(context.Context) (*turingv1.ListMemoryToolsResponse, error) {
	return &turingv1.ListMemoryToolsResponse{}, nil
}

func (r *memoryToolRPC) CallMemoryTool(
	_ context.Context,
	request *turingv1.CallMemoryToolRequest,
) (*turingv1.CallMemoryToolResponse, error) {
	r.runID = request.GetRunId()
	r.approvalID = request.GetApprovalId()
	r.toolName = request.GetToolName()
	result, err := structpb.NewStruct(map[string]any{"content": "framed"})
	if err != nil {
		return nil, err
	}
	return &turingv1.CallMemoryToolResponse{Result: result}, nil
}

// memory.search is approval-gated but read-only. The orchestrator says so in
// the policy decision, and the runner has to carry that through: reporting a
// read as side-effecting would make a failure after it non-retryable, so a
// question the user asked would be answered by giving up.
func TestMemoryToolRunsCallerEnforcedAndStaysReadOnly(t *testing.T) {
	rpc := &memoryToolRPC{}
	runner := &Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			decision := turingv1.ToolPolicyDecision_DECISION_ALLOW
			approvalID := ""
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
				decision = turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED
				approvalID = "appr_memory"
			}
			return &turingv1.ToolPolicyDecision{
				Decision: decision, ToolCallId: beacon.GetToolCallId(),
				ApprovalId: approvalID, ReadOnly: true,
			}, nil
		},
		WaitApproval:   func(context.Context, string) (string, error) { return "approval-jwt", nil },
		ResumeApproved: allowResume,
	}

	outcome, err := runner.RunWithOutcome(context.Background(), RunInput{
		AgentID:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		RunID:      "run_memory",
		ServerName: "memory",
		ToolName:   "memory.search",
		Args:       map[string]any{"query": "chickens"},
		MCPClient:  mcp.NewMemoryClient(rpc),
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.SideEffecting {
		t.Fatal("a read-only memory tool was reported as side-effecting")
	}
	if rpc.runID != "run_memory" || rpc.approvalID != "appr_memory" || rpc.toolName != "memory.search" {
		t.Fatalf("dispatch context = run:%q approval:%q tool:%q", rpc.runID, rpc.approvalID, rpc.toolName)
	}
	if outcome.Result["content"] != "framed" {
		t.Fatalf("result = %+v, want the server's framed content untouched", outcome.Result)
	}
}

// A write proposal is not read-only, and the decision saying so has to survive
// the same path.
func TestMemoryRememberStaysSideEffectingWhenThePolicySaysSo(t *testing.T) {
	runner := &Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			decision := turingv1.ToolPolicyDecision_DECISION_ALLOW
			approvalID := ""
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
				decision = turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED
				approvalID = "appr_remember"
			}
			return &turingv1.ToolPolicyDecision{
				Decision: decision, ToolCallId: beacon.GetToolCallId(), ApprovalId: approvalID,
			}, nil
		},
		WaitApproval:   func(context.Context, string) (string, error) { return "approval-jwt", nil },
		ResumeApproved: allowResume,
	}

	outcome, err := runner.RunWithOutcome(context.Background(), RunInput{
		AgentID:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		RunID:      "run_memory",
		ServerName: "memory",
		ToolName:   "memory.remember",
		Args:       map[string]any{"title": "t", "body": "b"},
		MCPClient:  mcp.NewMemoryClient(&memoryToolRPC{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.SideEffecting {
		t.Fatal("a memory proposal was reported as read-only")
	}
}
