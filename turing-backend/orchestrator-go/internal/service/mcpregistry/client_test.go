package mcpregistry

import (
	"context"
	"testing"
)

func TestThirdPartyServerNeverReceivesTheApprovalConsumerIdentity(t *testing.T) {
	const (
		vendorToken           = "vendor-token"
		approvalConsumerToken = "approval-consumer-must-stay-in-orchestrator"
	)
	h := newRegistryCallHarness(t)
	runID := h.runningToolCall(t, "call_identity", map[string]any{"path": "x"})
	if err := h.repo.SetMCPToolPolicy(context.Background(), h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID,
		RunID:    runID,
		ToolName: "vendor.write",
		Args:     map[string]any{"path": "x"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	authorization, _ := h.authorization.Load().(string)
	if authorization == "Bearer "+approvalConsumerToken {
		t.Fatal("the approval-consumer identity must never leave the orchestrator")
	}
	if authorization != "Bearer "+vendorToken {
		t.Fatalf("authorization = %q, want only the registered server bearer", authorization)
	}
}
