package mcpregistry

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestWithdrawnToolCannotBeReenabledByServerOrPolicyToggle(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(context.Background(), server.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(context.Background(), server.Server.ID, []DiscoveredTool{
		{Name: "vendor.keep", SchemaJSON: `{"type":"object"}`},
		{Name: "vendor.removed", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(context.Background(), server.Server.ID, []DiscoveredTool{{
		Name: "vendor.keep", SchemaJSON: `{"type":"object"}`,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(context.Background(), server.Server.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(context.Background(), server.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPToolPolicy(context.Background(), server.Server.ID, "vendor.removed", "safe"); err != nil {
		t.Fatal(err)
	}

	tools, err := repo.ListMCPServerTools(context.Background(), server.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if tool.Name == "vendor.removed" && tool.Enabled {
			t.Fatal("withdrawn tool was re-enabled")
		}
	}
}

func TestReimportWithChangedEndpointIsSkippedAndLeavesServerUntouched(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if _, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://one.example/mcp",
				"tools": [{"name": "vendor.lookup", "inputSchema": {"type": "object"}}]
			}

		}
	}`)); err != nil {
		t.Fatal(err)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	server := findRepositoryServer(t, servers, "vendor")
	if err := repo.SetMCPServerEnabled(context.Background(), server.ID, true); err != nil {
		t.Fatal(err)
	}

	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://two.example/mcp"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skipped) != 1 || report.Skipped[0] != "vendor" {
		t.Fatalf("Skipped = %v, want [vendor]", report.Skipped)
	}
	updated, err := repo.GetMCPServer(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled {
		t.Fatal("reimport with a changed endpoint disabled an enabled server")
	}
	if updated.URL != "https://one.example/mcp" {
		t.Fatalf("URL = %q, want the original endpoint preserved", updated.URL)
	}
	tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || !tools[0].Present {
		t.Fatalf("old endpoint tools = %+v, want the original snapshot retained", tools)
	}
}
