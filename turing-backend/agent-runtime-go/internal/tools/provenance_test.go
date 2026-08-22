package tools

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestRunnerForwardsProvenanceCapabilityForSafeTool(t *testing.T) {
	var tokens []string
	runner := &Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			return &turingv1.ToolPolicyDecision{
				Decision:        turingv1.ToolPolicyDecision_DECISION_ALLOW,
				ToolCallId:      beacon.GetToolCallId(),
				ProvenanceToken: "provenance-capability",
			}, nil
		},
	}

	if _, err := runner.Run(context.Background(), RunInput{
		RunID: "run_1", ToolName: "files.read", ServerName: "files",
		Args: map[string]any{"path": "note.txt"},
		MCPClient: mcpClientFunc(func(_ context.Context, _ string, _ map[string]any, callTokens ...string) (map[string]any, error) {
			tokens = append([]string{}, callTokens...)
			return map[string]any{"content": "hello"}, nil
		}),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(tokens) != 2 || tokens[0] != "" || tokens[1] != "provenance-capability" {
		t.Fatalf("call tokens = %#v, want an empty approval token and the issued capability", tokens)
	}
}

func TestRunnerForwardsBothApprovalAndProvenanceForMutatingTool(t *testing.T) {
	var tokens []string
	runner := &Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
				return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: beacon.GetToolCallId()}, nil
			}
			return &turingv1.ToolPolicyDecision{
				Decision:        turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
				ToolCallId:      beacon.GetToolCallId(),
				ApprovalId:      "appr_1",
				ProvenanceToken: "provenance-capability",
			}, nil
		},
		WaitApproval:   func(context.Context, string) (string, error) { return "approval-token", nil },
		ResumeApproved: allowResume,
	}

	if _, err := runner.Run(context.Background(), RunInput{
		RunID: "run_1", ToolName: "files.update", ServerName: "files",
		Args: map[string]any{"path": "note.txt", "content": "hello"},
		MCPClient: mcpClientFunc(func(_ context.Context, _ string, _ map[string]any, callTokens ...string) (map[string]any, error) {
			tokens = append([]string{}, callTokens...)
			return map[string]any{"path": "note.txt"}, nil
		}),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(tokens) != 2 || tokens[0] != "approval-token" || tokens[1] != "provenance-capability" {
		t.Fatalf("call tokens = %#v, want the approval token then the provenance capability", tokens)
	}
}
