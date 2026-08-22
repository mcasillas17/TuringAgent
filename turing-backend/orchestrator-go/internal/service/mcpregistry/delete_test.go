package mcpregistry

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestDeleteMCPServerRemovesImportedServerAndItsTools(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(context.Background(), server.ID, []DiscoveredTool{{
		Name: "vendor.lookup", SchemaJSON: `{"type":"object"}`,
	}}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.DeleteMcpServer(context.Background(), &turingv1.DeleteMcpServerRequest{
		ServerId: server.ID,
	}); err != nil {
		t.Fatal(err)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range servers {
		if listed.ID == server.ID {
			t.Fatal("deleted MCP server remains registered")
		}
	}
}
