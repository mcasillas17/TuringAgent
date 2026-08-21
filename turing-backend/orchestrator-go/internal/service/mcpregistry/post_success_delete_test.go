package mcpregistry

import (
	"context"
	"testing"
)

func TestSuccessfulCallSurvivesConcurrentServerDeletion(t *testing.T) {
	h := newRegistryCallHarness(t)
	runID := h.runningToolCall(t, "call_delete_after_dispatch", map[string]any{"path": "x"})
	if err := h.repo.SetMCPToolPolicy(context.Background(), h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}
	h.deleteOnCall.Store(true)
	if _, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID, RunID: runID, ToolName: "vendor.write",
		Args: map[string]any{"path": "x"},
	}); err != nil {
		t.Fatalf("successful MCP result was replaced by liveness failure: %v", err)
	}
}
