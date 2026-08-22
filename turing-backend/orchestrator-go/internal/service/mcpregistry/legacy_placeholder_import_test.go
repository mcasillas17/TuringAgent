package mcpregistry

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// Migration 0016 seeds a disabled, non-bundled placeholder row (url=”) for
// any server a pre-registry runtime had reported tools for, so the tool's
// policy and schema survive until an operator imports a real endpoint. A
// reimport of that same name with a valid endpoint must adopt the
// placeholder in place rather than skip it forever the way an ordinary
// already-registered name is skipped.
func TestImportJSONAdoptsLegacyPlaceholderInsteadOfSkippingForever(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	// Seed the exact migration-0016 shape: a disabled, non-bundled row with
	// an empty URL, plus the tool row and an operator-edited policy a
	// pre-registry runtime would have left behind.
	placeholder, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor",
		URL:  "",
		Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, placeholder.ID, []repository.MCPServerTool{
		{Name: "vendor.lookup", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPToolPolicy(ctx, placeholder.ID, "vendor.lookup", "safe"); err != nil {
		t.Fatal(err)
	}

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer legacy-placeholder-token"},
				"tools": [{"name": "vendor.lookup", "inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want the placeholder adopted rather than skipped", report.Skipped)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", report.Imported)
	}

	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if vendor.ID != placeholder.ID {
		t.Fatalf("ID = %q, want the placeholder row %q adopted in place", vendor.ID, placeholder.ID)
	}
	if vendor.URL != "https://vendor.example/mcp" {
		t.Fatalf("URL = %q, want the imported endpoint populated", vendor.URL)
	}
	if vendor.Tier != repository.MCPServerTierRemoteURL {
		t.Fatalf("Tier = %q, want the classified tier populated", vendor.Tier)
	}
	if len(vendor.SealedToken) == 0 {
		t.Fatal("SealedToken is empty, want the imported token sealed and stored")
	}
	if vendor.Enabled {
		t.Fatal("adopting a placeholder must not enable the server")
	}

	tools, err := repo.ListMCPServerTools(ctx, vendor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Policy != "safe" {
		t.Fatalf("tools = %+v, want the pre-existing edited policy preserved", tools)
	}

	tombstoned, err := repo.MCPServerTombstoned(ctx, "vendor")
	if err != nil {
		t.Fatal(err)
	}
	if tombstoned {
		t.Fatal("adopting a placeholder must not involve the tombstone table")
	}
}

// Adopting a placeholder with no bearer token (no Authorization header) must
// still populate url/tier and leave the server with no sealed token, rather
// than requiring one.
func TestImportJSONAdoptsLegacyPlaceholderWithoutToken(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	placeholder, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor",
		URL:  "",
		Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", report.Imported)
	}

	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if vendor.ID != placeholder.ID {
		t.Fatalf("ID = %q, want the placeholder row %q adopted in place", vendor.ID, placeholder.ID)
	}
	if vendor.URL != "https://vendor.example/mcp" {
		t.Fatalf("URL = %q, want the imported endpoint populated", vendor.URL)
	}
	if len(vendor.SealedToken) != 0 {
		t.Fatalf("SealedToken = %v, want empty when no token was imported", vendor.SealedToken)
	}
}

// A placeholder import still requires a sealer when the mcp.json entry
// carries a bearer token: adopting an empty-URL row must not bypass token
// sealing the way the ordinary already-registered skip path does (which
// never needs a sealer because it never touches the token).
func TestImportJSONPlaceholderWithTokenStillRequiresSealer(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	ctx := context.Background()

	if _, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor",
		URL:  "",
		Tier: repository.MCPServerTierLocalContainer,
	}); err != nil {
		t.Fatal(err)
	}

	unsealed := New(repo, nil, nil)
	report, err := unsealed.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer legacy-placeholder-token"}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("ImportJSON returned an error instead of reporting Unsupported: %v", err)
	}
	if len(report.Imported) != 0 || len(report.Skipped) != 0 {
		t.Fatalf("Imported = %v, Skipped = %v, want the placeholder refused into Unsupported without a sealer", report.Imported, report.Skipped)
	}
	if _, present := report.Unsupported["vendor"]; !present {
		t.Fatalf("Unsupported = %v, want vendor refused for missing the sealer", report.Unsupported)
	}
}
