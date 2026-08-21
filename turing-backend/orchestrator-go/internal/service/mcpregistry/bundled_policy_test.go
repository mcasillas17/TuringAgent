package mcpregistry

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestBundledMutatingToolCannotBeDowngradedToSafe(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if err := repo.UpsertTools(context.Background(), []repository.DiscoveredTool{{
		ServerName: "files", ToolName: "files.create",
		SchemaJSON: `{"type":"object"}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	files := findRepositoryServer(t, servers, "files")

	if _, err := service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: files.ID,
		ToolName: "files.create",
		Policy:   turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	}); err == nil {
		t.Fatal("bundled mutating tool was downgraded to safe")
	}
}
