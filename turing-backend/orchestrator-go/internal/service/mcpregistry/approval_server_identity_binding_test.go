package mcpregistry

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// TestConsumeApprovalForThirdPartyRefusesAfterServerIsDeletedAndNameReregistered
// is the determinating test for finding #2 (immutable approval server-
// identity binding): create and approve a tool call/approval against
// server A, delete A, explicitly register a brand-new server B under A's
// exact former name, and reuse the same run for a second tool call/
// approval against B. The old (still-"approved", never-consumed)
// approval — bound at insert time to A's own id, never merely to the
// name "vendor" both A and B share — must not be usable to authorise a
// dispatch to B: ConsumeApprovalForThirdParty compares
// ApprovalRecord.MCPServerID (NULL now, since ON DELETE SET NULL cleared
// it when A was deleted) against the live server.ID CallTool passes, and
// a live, non-empty id can never equal that NULL binding. The old
// approval survives this refused attempt untouched (still "approved",
// not "consumed" or any other terminal state) — proving the refusal
// happens before any state change, not merely that the tool call itself
// fails afterward. A brand-new approval, created after B exists (so its
// own tool_calls row's mcp_server_id is freshly resolved to B, per
// repository.recordToolCallBeforeTx/lookupMCPServerIDByNameTx), can
// consume normally against B.
func TestConsumeApprovalForThirdPartyRefusesAfterServerIsDeletedAndNameReregistered(t *testing.T) {
	h := newRegistryCallHarness(t)
	ctx := context.Background()
	args := map[string]any{"path": "x"}

	// Server A: the harness's own vendor server, already registered,
	// enabled, and discovered by newRegistryCallHarness.
	serverA := h.serverID
	runID := h.runningToolCall(t, "call_a", args)
	approvalA, err := h.approvals.CreateApprovalForTool(ctx, runID, "call_a", "general_assistant", "vendor.write", args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.approvals.ApproveApproval(ctx, &turingv1.ApproveApprovalRequest{ApprovalId: approvalA}); err != nil {
		t.Fatal(err)
	}
	h.resumeApprovedRun(t, runID, approvalA)

	// Delete A. Its own tool_calls row (call_a) survives — only its
	// mcp_server_id binding is cleared (ON DELETE SET NULL) — and
	// approvalA remains "approved", never touched by the deletion.
	if _, err := h.repo.DeleteMCPServer(ctx, serverA); err != nil {
		t.Fatal(err)
	}
	approvalABeforeAttempt, err := h.repo.GetApproval(ctx, approvalA)
	if err != nil {
		t.Fatal(err)
	}
	if approvalABeforeAttempt.Status != "approved" {
		t.Fatalf("approvalA status after deleting its server = %q, want still approved", approvalABeforeAttempt.Status)
	}
	if approvalABeforeAttempt.MCPServerID != "" {
		t.Fatalf("approvalA MCPServerID after deleting its server = %q, want empty (ON DELETE SET NULL)", approvalABeforeAttempt.MCPServerID)
	}

	// Explicitly register a brand-new server B under A's exact former
	// name, enable it, and record its own (independent) discovery of the
	// same tool name — a materially different row (a new, distinct id)
	// that merely happens to share a name with the deleted A.
	serverB, err := h.repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: h.vendor.URL, Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if serverB.Server.ID == serverA {
		t.Fatal("test setup is broken: server B must have a different id from deleted server A")
	}
	if err := h.repo.SetMCPServerEnabled(ctx, serverB.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.registry.RecordDiscovery(ctx, serverB.Server.ID, []DiscoveredTool{{
		Name: "vendor.write", SchemaJSON: `{"type":"object"}`,
	}}); err != nil {
		t.Fatal(err)
	}

	// The old approval must not authorise a dispatch to B: CallTool
	// re-resolves ServerID to the live server.ID (B's), which cannot
	// equal approvalA's now-NULL MCPServerID.
	if _, err := h.registry.CallTool(ctx, CallInput{
		ServerID: serverB.Server.ID, RunID: runID, ApprovalID: approvalA, ToolName: "vendor.write", Args: args,
	}); err == nil {
		t.Fatal("the old approval (bound to deleted server A) must not authorise a call to newly-registered server B")
	}
	if got := h.reached.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want 0: the refused call must never reach the network", got)
	}
	approvalAAfterAttempt, err := h.repo.GetApproval(ctx, approvalA)
	if err != nil {
		t.Fatal(err)
	}
	if approvalAAfterAttempt.Status != "approved" {
		t.Fatalf("approvalA status after the refused attempt = %q, want still approved (never consumed)", approvalAAfterAttempt.Status)
	}

	// A second tool call on the same run, made after B exists, resolves
	// its own mcp_server_id to B — and a fresh approval for it can
	// consume normally against B.
	if err := h.repo.RecordToolCallBefore(
		ctx,
		repository.ToolCallRecord{ToolCallID: "call_b", RunID: runID},
		"general_assistant", "vendor", "vendor.write", `{"path":"x"}`, "sha256:test-placeholder-b",
	); err != nil {
		t.Fatal(err)
	}
	approvalB, err := h.approvals.CreateApprovalForTool(ctx, runID, "call_b", "general_assistant", "vendor.write", args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.approvals.ApproveApproval(ctx, &turingv1.ApproveApprovalRequest{ApprovalId: approvalB}); err != nil {
		t.Fatal(err)
	}
	h.resumeApprovedRun(t, runID, approvalB)
	if _, err := h.registry.CallTool(ctx, CallInput{
		ServerID: serverB.Server.ID, RunID: runID, ApprovalID: approvalB, ToolName: "vendor.write", Args: args,
	}); err != nil {
		t.Fatalf("the new approval (bound to server B) must authorise a call to B: %v", err)
	}
	if got := h.reached.Load(); got != 1 {
		t.Fatalf("vendor requests = %d, want exactly 1 (the successful call to B)", got)
	}
	approvalBAfter, err := h.repo.GetApproval(ctx, approvalB)
	if err != nil {
		t.Fatal(err)
	}
	if approvalBAfter.Status != "consumed" {
		t.Fatalf("approvalB status = %q, want consumed", approvalBAfter.Status)
	}
}
