package mcpregistry

import (
	"context"
	"testing"
)

func TestCancelledRunCannotDispatchRegisteredTool(t *testing.T) {
	h := newRegistryCallHarness(t)
	runID := h.runningToolCall(t, "call_cancelled", map[string]any{"path": "x"})
	if err := h.repo.SetMCPToolPolicy(context.Background(), h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(
		context.Background(),
		`UPDATE agent_runs SET status = 'cancelled' WHERE id = ?`,
		runID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID, RunID: runID, ToolName: "vendor.write",
		Args: map[string]any{"path": "x"},
	}); err == nil {
		t.Fatal("cancelled run dispatched a registered tool")
	}
	if got := h.reached.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want zero", got)
	}
}
