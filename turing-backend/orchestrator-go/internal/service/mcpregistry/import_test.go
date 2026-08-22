package mcpregistry

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
)

func TestImportingMcpJSONLeavesEverythingOff(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer vendor-secret"},
				"tools": [{
					"name": "vendor.lookup",
					"inputSchema": {"type": "object"}
				}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unsupported) != 0 {
		t.Fatalf("unsupported = %v, want none", report.Unsupported)
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if vendor.Enabled {
		t.Fatal("an imported server must arrive disabled")
	}
	if vendor.Tier != repository.MCPServerTierRemoteURL {
		t.Fatalf("tier = %q, want remote URL", vendor.Tier)
	}
	if bytes.Contains(vendor.SealedToken, []byte("vendor-secret")) {
		t.Fatal("the imported bearer was stored in plaintext")
	}

	tools, err := repo.ListMCPServerTools(context.Background(), vendor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Policy != "approval_required" || tools[0].Enabled {
		t.Fatalf("imported tools = %+v, want disabled and approval_required", tools)
	}
}

func TestStdioEntriesAreReportedAsUnsupported(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"local": {"command": "npx", "args": ["x"]}
		}

	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Unsupported["local"], "stdio") {
		t.Fatalf("report = %q, want it to say why stdio is unsupported", report.Unsupported["local"])
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Name == "local" {
			t.Fatal("a stdio entry must not be registered")
		}
	}
}

func TestIntegrationsNameAndGitHubToolNamespaceAreReserved(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{"mcpServers":{"integrations":{"url":"https://vendor.example/mcp"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Unsupported["integrations"], "reserved") {
		t.Fatalf("report = %+v", report.Unsupported)
	}
	server, err := repo.UpsertImportedMCPServer(context.Background(), repository.ImportedMCPServer{Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL})
	if err != nil {
		t.Fatal(err)
	}
	err = service.RecordDiscovery(context.Background(), server.ID, []DiscoveredTool{{Name: "github.list_issues", SchemaJSON: `{}`}})
	if !errors.Is(err, repository.ErrMCPToolNameCollision) {
		t.Fatalf("collision error = %v", err)
	}
}

func newRegistryTestService(t *testing.T) (*Server, *repository.Repository) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	return New(repo, sealer, nil), repo
}

func findRepositoryServer(t *testing.T, servers []repository.MCPServerRecord, name string) repository.MCPServerRecord {
	t.Helper()
	for _, server := range servers {
		if server.Name == name {
			return server
		}
	}
	t.Fatalf("server %q not found in %+v", name, servers)
	return repository.MCPServerRecord{}
}
