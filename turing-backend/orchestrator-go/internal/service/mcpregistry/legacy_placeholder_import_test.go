package mcpregistry

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
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

// A reimport whose mcp.json entry carries no "tools" key at all (as
// opposed to the previous test's matching snapshot) must still withdraw
// every tool the placeholder carried: that endpoint was never verified, so
// its tools must not simply carry over as present just because this
// reimport happened to say nothing about tools.
func TestImportJSONAdoptsLegacyPlaceholderWithNoToolsKeyWithdrawsCarriedTools(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	placeholder, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, placeholder.ID, []repository.MCPServerTool{
		{Name: "vendor.lookup", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
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
	vendorRecord := findRepositoryServer(t, servers, "vendor")
	if vendorRecord.ID != placeholder.ID {
		t.Fatalf("ID = %q, want the placeholder %q adopted in place", vendorRecord.ID, placeholder.ID)
	}

	tools, err := repo.ListMCPServerTools(ctx, vendorRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Present || tools[0].Enabled {
		t.Fatalf("tools = %+v, want the carried tool withdrawn (present=0, enabled=0): the reimport carried no tools snapshot", tools)
	}
}

// TestImportedPlaceholderEnableWithFailedDiscoveryDoesNotActivateWithdrawnTools
// covers the discriminating case a plain adoption test cannot: once a
// placeholder is adopted and its carried tool withdrawn (present=0,
// enabled=0), enabling the server and having live discovery then fail must
// not resurrect it. discover() never calls
// RecordDiscovery/ReplaceMCPServerTools when the vendor round trip itself
// fails, so the withdrawn tool must still be exactly as the adoption left
// it. This uses repo.ImportMCPServer directly (the same call ImportJSON
// itself makes for a placeholder adoption) rather than service.ImportJSON,
// because the httptest server's loopback URL would fail ImportJSON's own
// URL classification — unrelated to what this test is proving — the same
// workaround TestImportedRemoteServerDiscoveryFailurePreservesSnapshotAndPolicy
// uses for the same reason.
func TestImportedPlaceholderEnableWithFailedDiscoveryDoesNotActivateWithdrawnTools(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	placeholder, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, placeholder.ID, []repository.MCPServerTool{
		{Name: "vendor.lookup", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}

	vendor := failingVendor(t, "tools/list unavailable")
	service.httpClient = vendor.Client()

	result, err := repo.ImportMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: vendor.URL, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Server.ID != placeholder.ID {
		t.Fatal("test setup failed: adoption must reuse the placeholder's id")
	}
	adoptedTools, err := repo.ListMCPServerTools(ctx, placeholder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(adoptedTools) != 1 || adoptedTools[0].Present {
		t.Fatalf("test setup failed: tools after adoption = %+v, want withdrawn before the enable attempt", adoptedTools)
	}

	descriptor, err := service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
		ServerId: placeholder.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_DOWN {
		t.Fatalf("liveness = %v, want down: the vendor's tools/list call fails", descriptor.GetLiveness())
	}

	stillWithdrawn, err := repo.ListMCPServerTools(ctx, placeholder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stillWithdrawn) != 1 || stillWithdrawn[0].Present || stillWithdrawn[0].Enabled {
		t.Fatalf("tools after enabling with failed discovery = %+v, want still withdrawn", stillWithdrawn)
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
