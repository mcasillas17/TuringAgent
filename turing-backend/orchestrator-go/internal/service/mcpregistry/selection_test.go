package mcpregistry

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/testsupport/approvalfixture"
)

// A job that enforces its frozen set reaches no registered MCP tool outside
// it: the call is refused before the approval is spent or the server reached,
// and the same call inside the set proceeds.
func TestAnMCPToolOutsideAnEnforcedSetIsRefusedBeforeItsApproval(t *testing.T) {
	h := newRegistryCallHarness(t)
	ctx := context.Background()
	args := map[string]any{"path": "x"}
	runID := h.runningToolCall(t, "call_selected", args)
	approvalID, err := h.approvals.CreateApprovalForTool(ctx, runID, "call_selected", "general_assistant", "vendor.write", args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := approvalfixture.Approve(t, h.approvals, ctx, &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	h.resumeApprovedRun(t, runID, approvalID)
	enforce := func(selected string) {
		t.Helper()
		if _, err := h.database.ExecContext(ctx, `
			UPDATE jobs SET payload_json = json_set(payload_json,
				'$.enforceSelectedTools', json('true'), '$.selectedTools', json(?))
			WHERE run_id = ?`, selected, runID); err != nil {
			t.Fatal(err)
		}
	}
	input := CallInput{ServerID: h.serverID, RunID: runID, ApprovalID: approvalID, ToolName: "vendor.write", Args: args}

	enforce(`["vendor/vendor.read"]`)
	if _, err := h.registry.CallTool(ctx, input); err == nil || err.Error() != "MCP tool is not selected for this run" {
		t.Fatalf("out-of-set CallTool = %v, want not selected", err)
	}
	if got := h.reached.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want none", got)
	}
	if approval, err := h.repo.GetApproval(ctx, approvalID); err != nil || approval.Status != "approved" {
		t.Fatalf("approval = %+v, %v; want it still approved, not spent", approval, err)
	}

	enforce(`["vendor/vendor.write"]`)
	if _, err := h.registry.CallTool(ctx, input); err != nil {
		t.Fatalf("in-set CallTool: %v", err)
	}
	if got := h.reached.Load(); got != 1 {
		t.Fatalf("vendor requests = %d, want the in-set call to reach it once", got)
	}
}
