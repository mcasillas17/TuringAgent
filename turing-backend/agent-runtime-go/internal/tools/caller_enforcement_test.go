package tools

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestRunnerUsesCallerSideApprovalWithoutForwardingTheJWT(t *testing.T) {
	client := &callerEnforcedTestClient{}
	runner := &Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			decision := turingv1.ToolPolicyDecision_DECISION_ALLOW
			approvalID := ""
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
				decision = turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED
				approvalID = "appr_vendor"
			}
			return &turingv1.ToolPolicyDecision{
				Decision:   decision,
				ToolCallId: beacon.GetToolCallId(),
				ApprovalId: approvalID,
			}, nil
		},
		WaitApproval: func(context.Context, string) (string, error) {
			return "signed-approval-jwt-must-not-reach-vendor", nil
		},
		ResumeApproved: allowResume,
	}

	if _, err := runner.Run(context.Background(), RunInput{
		AgentID:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		RunID:      "run_vendor",
		ServerName: "vendor",
		ToolName:   "vendor.write",
		Args:       map[string]any{"path": "x"},
		MCPClient:  client,
	}); err != nil {
		t.Fatal(err)
	}

	if client.ordinaryCalled {
		t.Fatal("caller-enforced server used the ordinary token-forwarding path")
	}
	if client.runID != "run_vendor" || client.approvalID != "appr_vendor" {
		t.Fatalf("caller enforcement context = run:%q approval:%q", client.runID, client.approvalID)
	}
}

type callerEnforcedTestClient struct {
	ordinaryCalled bool
	runID          string
	approvalID     string
}

func (c *callerEnforcedTestClient) CallTool(
	context.Context,
	string,
	map[string]any,
	...string,
) (map[string]any, error) {
	c.ordinaryCalled = true
	return map[string]any{}, nil
}

func (c *callerEnforcedTestClient) CallToolWithCallerApproval(
	_ context.Context,
	runID string,
	approvalID string,
	_ string,
	_ map[string]any,
) (map[string]any, error) {
	c.runID = runID
	c.approvalID = approvalID
	return map[string]any{"ok": true}, nil
}
