package mcpregistry

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestRemoteCallCoveredByTheRunDecisionDispatchesExactlyOnce(t *testing.T) {
	h := newRegistryCallHarness(t)
	if _, err := h.database.ExecContext(context.Background(), `
		UPDATE mcp_servers SET tier = 'remote_url' WHERE id = ?
	`, h.serverID); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"path": "x"}
	skillFingerprint, err := backendegress.SkillSnapshotFingerprint(nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := h.repo.CreateSession(context.Background(), "remote registry approval")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "write", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "test-model",
		SelectedTools: []string{"vendor/vendor.write"},
		EgressDecision: &repository.PendingEgressDecision{
			Version:                  backendegress.DecisionVersion,
			ChallengeNonce:           "nonce_remote_registry",
			ChallengeFingerprint:     "fingerprint_remote_registry",
			RequestDigest:            "digest_remote_registry",
			Provider:                 "ollama",
			Model:                    "test-model",
			DataCategories:           []string{"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS", "EGRESS_DATA_CATEGORY_TOOL_RESULTS"},
			SelectedTools:            []string{"vendor/vendor.write"},
			SkillSnapshotFingerprint: skillFingerprint,
			ConsentGrantedAt:         repository.FormatTimestamp(time.Now().UTC()),
			RemoteMCPServers: []repository.RemoteMCPServerEgress{{
				ServerName: "vendor", Endpoint: h.vendor.URL, EndpointHost: h.vendor.Listener.Addr().String(),
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.claimAndDeliverRun(t, enqueued.RunID)
	if err := h.repo.RecordToolCallBefore(
		context.Background(),
		repository.ToolCallRecord{ToolCallID: "call_remote_approved", RunID: enqueued.RunID},
		"general_assistant",
		"vendor",
		"vendor.write",
		`{"path":"x"}`,
		"sha256:test-placeholder",
	); err != nil {
		t.Fatal(err)
	}
	approvalID, err := h.approvals.CreateApprovalForTool(
		context.Background(), enqueued.RunID, "call_remote_approved",
		"general_assistant", "vendor.write", args,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.approvals.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{
		ApprovalId: approvalID,
	}); err != nil {
		t.Fatal(err)
	}
	h.resumeApprovedRun(t, enqueued.RunID, approvalID)

	if _, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID, RunID: enqueued.RunID, ApprovalID: approvalID,
		ToolName: "vendor.write", Args: args,
	}); err != nil {
		t.Fatal(err)
	}
	if got := h.reached.Load(); got != 1 {
		t.Fatalf("vendor requests = %d, want one", got)
	}
}
