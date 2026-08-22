package repository

import (
	"context"
	"errors"
	"testing"
)

// Migration 0016 seeds a disabled, non-bundled placeholder row (url="") for
// any server a pre-registry runtime had reported tools for. A mobile
// operator has no way to edit backend files, so an explicit
// RegisterMCPServer of that exact name with a valid endpoint is the only
// escape hatch available to them: it must adopt the placeholder in place
// (same ID) rather than refuse it as already-registered the way a real,
// already-configured row is refused. Explicitly naming the server and
// supplying a working endpoint is treated as the operator's consent to
// adopt it.
func TestRegisterMCPServerAdoptsLegacyPlaceholderInPlace(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	placeholder, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if placeholder.Adopted {
		t.Fatal("Adopted = true for a freshly inserted row (no prior name existed), want false")
	}
	if err := repo.ReplaceMCPServerTools(ctx, placeholder.Server.ID, []MCPServerTool{
		{Name: "vendor.lookup", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}
	// An operator may have edited the carried tool's policy while it sat
	// disabled; adoption must preserve that edit even though the tool
	// itself is withdrawn.
	if err := repo.SetMCPToolPolicy(ctx, placeholder.Server.ID, "vendor.lookup", "safe"); err != nil {
		t.Fatal(err)
	}
	// Prove adoption forces disabled rather than merely leaving an
	// already-disabled row alone: flip it enabled first.
	if err := repo.SetMCPServerEnabled(ctx, placeholder.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	// A placeholder's liveness reading (if any) was never actually
	// observed against a real endpoint; seed a non-unknown one so
	// adoption's reset is provable.
	if err := repo.SetMCPServerStatus(ctx, placeholder.Server.ID, "down", "stale placeholder reading"); err != nil {
		t.Fatal(err)
	}

	adopted, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", SealedToken: []byte("sealed-token"), Tier: MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatalf("adopting a legacy placeholder must not return an error: %v", err)
	}
	if !adopted.Adopted {
		t.Fatal("Adopted = false for a registration that reused a legacy placeholder's name, want true")
	}
	if adopted.Server.ID != placeholder.Server.ID {
		t.Fatalf("ID = %q, want the placeholder row %q adopted in place", adopted.Server.ID, placeholder.Server.ID)
	}
	if adopted.Server.URL != "https://vendor.example/mcp" {
		t.Fatalf("URL = %q, want the registered endpoint populated", adopted.Server.URL)
	}
	if adopted.Server.Tier != MCPServerTierRemoteURL {
		t.Fatalf("Tier = %q, want the registered tier populated", adopted.Server.Tier)
	}
	if len(adopted.Server.SealedToken) == 0 {
		t.Fatal("SealedToken is empty, want the registered token sealed and stored")
	}
	if adopted.Server.Enabled {
		t.Fatal("adopting a placeholder must force the server disabled")
	}
	if adopted.Server.Status != "unknown" || adopted.Server.StatusError != "" {
		t.Fatalf("Status = %q, StatusError = %q, want liveness reset to unknown/empty", adopted.Server.Status, adopted.Server.StatusError)
	}

	tools, err := repo.ListMCPServerTools(ctx, adopted.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %+v, want the one carried tool retained (withdrawn, not deleted)", tools)
	}
	if tools[0].Present || tools[0].Enabled {
		t.Fatalf("tools[0] = %+v, want present=0, enabled=0: its endpoint was never verified", tools[0])
	}
	if tools[0].Policy != "safe" {
		t.Fatalf("tools[0].Policy = %q, want the operator's prior edit preserved", tools[0].Policy)
	}

	tombstoned, err := repo.MCPServerTombstoned(ctx, "vendor")
	if err != nil {
		t.Fatal(err)
	}
	if tombstoned {
		t.Fatal("adopting a placeholder must not involve the tombstone table")
	}
}

// A real, already-registered server (non-empty URL) must still be refused
// as already taken — adoption applies only to a url-empty placeholder, not
// to any other pre-existing name.
func TestRegisterMCPServerRealExistingRowStillRefusedAsNameTaken(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	if _, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor-two.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if !errors.Is(err, ErrMCPServerNameTaken) {
		t.Fatalf("err = %v, want ErrMCPServerNameTaken: a real existing row must not be adopted", err)
	}
}

// A bundled row must be refused even when its URL happens to be empty (it
// never should be, but adoption must key off tier before url, not the
// other way around).
func TestRegisterMCPServerBundledPlaceholderStillRefused(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "system-like", URL: "", Tier: MCPServerTierBundled,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "system-like", URL: "https://impostor.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if !errors.Is(err, ErrMCPServerBundled) {
		t.Fatalf("err = %v, want ErrMCPServerBundled", err)
	}
}

// Registration remains zero-network: adopting a placeholder must not
// require or perform any liveness contact, exactly like a brand-new
// registration.
func TestRegisterMCPServerAdoptionRejectsToolsSnapshotLikeAnyOtherRegistration(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	if _, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "", Tier: MCPServerTierLocalContainer,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
		Tools: []MCPServerTool{{Name: "vendor.lookup", Policy: "safe", SchemaJSON: `{"type":"object"}`}},
	})
	if !errors.Is(err, ErrMCPServerToolsNotAllowed) {
		t.Fatalf("err = %v, want ErrMCPServerToolsNotAllowed even when adopting a placeholder", err)
	}
}
