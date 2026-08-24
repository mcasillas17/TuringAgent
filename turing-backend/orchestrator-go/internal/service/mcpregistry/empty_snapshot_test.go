package mcpregistry

import (
	"context"
	"testing"
)

func TestReimportWithEmptyToolsListDoesNotWithdrawPreviousTools(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if _, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
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
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": []
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skipped) != 1 || report.Skipped[0] != "vendor" {
		t.Fatalf("Skipped = %v, want [vendor]", report.Skipped)
	}
	tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || !tools[0].Present {
		t.Fatalf("tools after reimport with an empty snapshot = %+v, want the previous tool retained", tools)
	}
}
