package approvals

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestThirdPartyConsumeRejectsEveryApprovalBindingMismatch(t *testing.T) {
	// "mcp_bundled_files" is the real, migration-seeded id of the
	// bundled "files" server (see schema/0016_mcp_registry.sql) that
	// createRunningToolCall's own tool_calls row (server_name "files")
	// resolves to via repository.lookupMCPServerIDByNameTx — the correct
	// serverID every case below keeps fixed except the one case that
	// mismatches it on purpose.
	const filesServerID = "mcp_bundled_files"
	tests := []struct {
		name       string
		runID      string
		serverName string
		serverID   string
		toolName   string
		args       map[string]any
	}{
		{name: "run", runID: "other-run", serverName: "files", serverID: filesServerID, toolName: "files.update", args: map[string]any{"path": "note.txt"}},
		{name: "server", serverName: "vendor", serverID: filesServerID, toolName: "files.update", args: map[string]any{"path": "note.txt"}},
		{name: "serverID", serverName: "files", serverID: "mcp_bundled_system", toolName: "files.update", args: map[string]any{"path": "note.txt"}},
		{name: "tool", serverName: "files", serverID: filesServerID, toolName: "files.create", args: map[string]any{"path": "note.txt"}},
		{name: "arguments", serverName: "files", serverID: filesServerID, toolName: "files.update", args: map[string]any{"path": "other.txt"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newApprovalHarness(t)
			enqueued := h.createRunningToolCall(t)
			approvedArgs := map[string]any{"path": "note.txt"}
			approvalID, err := h.service.CreateApprovalForTool(
				context.Background(),
				enqueued.RunID,
				"call_1",
				"general_assistant",
				"files.update",
				approvedArgs,
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{
				ApprovalId: approvalID,
			}); err != nil {
				t.Fatal(err)
			}
			runID := test.runID
			if runID == "" {
				runID = enqueued.RunID
			}

			err = h.service.ConsumeApprovalForThirdParty(
				context.Background(),
				approvalID,
				runID,
				test.serverName,
				test.serverID,
				test.toolName,
				test.args,
			)
			if err == nil {
				t.Fatal("mismatched approval binding was consumed")
			}
			approval, err := h.repo.GetApproval(context.Background(), approvalID)
			if err != nil {
				t.Fatal(err)
			}
			if approval.Status != "approved" {
				t.Fatalf("approval status = %q, want approved", approval.Status)
			}
		})
	}
}
