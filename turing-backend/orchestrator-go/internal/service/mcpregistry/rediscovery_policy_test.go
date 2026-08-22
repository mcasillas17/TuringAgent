package mcpregistry

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestRediscoveryPreservesDisabledToolPolicy(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(context.Background(), server.ID, true); err != nil {
		t.Fatal(err)
	}
	discovered := []DiscoveredTool{{
		Name: "vendor.lookup", SchemaJSON: `{"type":"object"}`,
	}}
	if err := service.RecordDiscovery(context.Background(), server.ID, discovered); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.ID, ToolName: "vendor.lookup",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(context.Background(), server.ID, discovered); err != nil {
		t.Fatal(err)
	}
	tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Policy != "disabled" || tools[0].Enabled {
		t.Fatalf("rediscovered disabled tool = %+v", tools)
	}
}
