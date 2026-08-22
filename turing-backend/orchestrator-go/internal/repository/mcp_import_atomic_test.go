package repository

import (
	"context"
	"errors"
	"testing"
)

// ImportMCPServer's Tools snapshot is reconciled in the same transaction as
// the server row it belongs to: a brand-new row inserts exactly the
// supplied tools, disabled like the server itself, atomically with the
// insert.
func TestImportMCPServerNewRowInsertsSuppliedToolsAtomically(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{
			{Name: "vendor.lookup", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created {
		t.Fatal("Created = false, want true for a brand new server")
	}
	tools, err := repo.ListMCPServerTools(ctx, result.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "vendor.lookup" || !tools[0].Present || tools[0].Enabled {
		t.Fatalf("tools = %+v, want vendor.lookup present and disabled (a freshly imported server starts disabled)", tools)
	}
}

// A real, already-registered server (non-empty URL) is create-only: even
// when this call's Tools snapshot names a brand-new tool, nothing about
// the existing row's tools may be touched.
func TestImportMCPServerExistingRealRowIgnoresSuppliedToolsEntirely(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	created, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
		Tools: []MCPServerTool{{Name: "vendor.lookup", Policy: "approval_required", SchemaJSON: `{"type":"object"}`}},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
		Tools: []MCPServerTool{{Name: "vendor.new_tool", Policy: "safe", SchemaJSON: `{"type":"object"}`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created {
		t.Fatal("Created = true, want false for an existing real server")
	}
	tools, err := repo.ListMCPServerTools(ctx, created.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "vendor.lookup" {
		t.Fatalf("tools = %+v, want the original snapshot untouched: a create-only skip must ignore the supplied Tools entirely", tools)
	}
}

// Adopting a migration-0016 placeholder always withdraws every tool it
// carried (present=0, enabled=0) — its endpoint was never verified — even
// when this reimport's own entry carried no "tools" key at all (Tools is
// nil here, standing in for that absent key).
func TestImportMCPServerPlaceholderAdoptionWithdrawsCarriedToolsWhenNoSnapshotSupplied(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	placeholder, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, placeholder.ID, []MCPServerTool{
		{Name: "vendor.legacy", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.Server.ID != placeholder.ID {
		t.Fatalf("result = %+v, want the placeholder adopted in place", result)
	}
	tools, err := repo.ListMCPServerTools(ctx, placeholder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Present || tools[0].Enabled {
		t.Fatalf("tools = %+v, want the carried tool withdrawn (present=0, enabled=0): its endpoint was never verified", tools)
	}
}

// A placeholder adoption whose reimport does carry a "tools" snapshot must
// withdraw every carried tool first and then reconfirm only the tools that
// snapshot supplies, preserving an operator's previously edited policy for
// any tool that survives.
func TestImportMCPServerPlaceholderAdoptionReconfirmsSuppliedToolsPreservingEditedPolicy(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	placeholder, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, placeholder.ID, []MCPServerTool{
		{Name: "vendor.kept", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
		{Name: "vendor.dropped", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPToolPolicy(ctx, placeholder.ID, "vendor.kept", "safe"); err != nil {
		t.Fatal(err)
	}

	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
		Tools: []MCPServerTool{
			{Name: "vendor.kept", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.Server.ID != placeholder.ID {
		t.Fatalf("result = %+v, want the placeholder adopted in place", result)
	}
	tools, err := repo.ListMCPServerTools(ctx, placeholder.ID)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]MCPServerTool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	kept, ok := byName["vendor.kept"]
	if !ok || !kept.Present || kept.Policy != "safe" {
		t.Fatalf("vendor.kept = %+v (ok=%v), want present and the previously edited safe policy preserved", kept, ok)
	}
	dropped, ok := byName["vendor.dropped"]
	if !ok || dropped.Present {
		t.Fatalf("vendor.dropped = %+v (ok=%v), want withdrawn (present=0): it was not in the reimported snapshot", dropped, ok)
	}
}

// `"tools": []` — an explicit, present-but-empty snapshot — must withdraw
// a placeholder's carried tools exactly like an absent "tools" key does:
// there is nothing left to reconfirm.
func TestImportMCPServerPlaceholderAdoptionEmptyToolsSnapshotWithdrawsEverything(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	placeholder, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, placeholder.ID, []MCPServerTool{
		{Name: "vendor.legacy", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
		Tools: []MCPServerTool{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created {
		t.Fatal("Created = false, want true for the placeholder adoption")
	}
	tools, err := repo.ListMCPServerTools(ctx, placeholder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Present {
		t.Fatalf("tools = %+v, want the carried tool withdrawn by an explicit empty snapshot", tools)
	}
}

// A tool-name collision with another server's present tool must roll back
// the entire placeholder adoption — not just skip reconfirming that one
// tool — so a corrected reimport sees the placeholder exactly as it was,
// not a half-adopted row it can only skip.
func TestImportMCPServerPlaceholderAdoptionToolCollisionRollsBackAdoptionEntirely(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	owner, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "owner", URL: "https://owner.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, owner.ID, []MCPServerTool{
		{Name: "shared.tool", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}

	placeholder, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
		Tools: []MCPServerTool{
			{Name: "shared.tool", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
		},
	})
	if !errors.Is(err, ErrMCPToolNameCollision) {
		t.Fatalf("err = %v, want ErrMCPToolNameCollision", err)
	}

	stillPlaceholder, err := repo.GetMCPServer(ctx, placeholder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillPlaceholder.URL != "" {
		t.Fatalf("URL = %q, want the placeholder's adoption rolled back entirely on tool collision", stillPlaceholder.URL)
	}

	corrected, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
		Tools: []MCPServerTool{
			{Name: "vendor.safe_tool", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !corrected.Created || corrected.Server.ID != placeholder.ID {
		t.Fatalf("corrected = %+v, want the placeholder adopted on the corrected reimport", corrected)
	}
	if corrected.Server.URL != "https://vendor.example/mcp" {
		t.Fatalf("URL = %q, want the corrected endpoint populated", corrected.Server.URL)
	}
}

// The same collision, but for a brand-new server name rather than a
// placeholder: the collision must leave no row behind at all, and a
// corrected reimport must then create it cleanly.
func TestImportMCPServerNewRowToolCollisionCreatesNoRowAndCorrectedReimportWorks(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	owner, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "owner", URL: "https://owner.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, owner.ID, []MCPServerTool{
		{Name: "shared.tool", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}

	_, err = repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
		Tools: []MCPServerTool{
			{Name: "shared.tool", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
		},
	})
	if !errors.Is(err, ErrMCPToolNameCollision) {
		t.Fatalf("err = %v, want ErrMCPToolNameCollision", err)
	}

	if _, err := repo.GetMCPServerByName(ctx, "vendor"); !errors.Is(err, ErrMCPServerNotFound) {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: a tool collision must leave no row behind", err)
	}

	corrected, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
		Tools: []MCPServerTool{
			{Name: "vendor.safe_tool", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !corrected.Created {
		t.Fatalf("corrected = %+v, want the corrected reimport to create the server", corrected)
	}
}
