package mcpregistry

import (
	"context"
	"testing"
)

func TestExplicitEmptyRemoteSnapshotWithdrawsPreviousTools(t *testing.T) {
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
	if _, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": []
			}
		}
	}`)); err != nil {
		t.Fatal(err)
	}
	tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Present {
		t.Fatalf("tools after empty snapshot = %+v, want previous tool unavailable", tools)
	}
}
