package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// MaxNonBundledMCPServers bounds how many non-bundled MCP server rows the
// registry may ever hold at once, enforced at the repository transaction
// boundary so neither an mcp.json import nor repeated direct
// RegisterMcpServer calls can grow ListMcpServers without limit. This
// fills the registry to exactly the cap via RegisterMCPServer, in a loop,
// and requires every one of those to succeed: the cap must not be
// off-by-one against legitimately filling the registry up to its limit.
func TestRegisterMCPServerAllowsExactlyTheCap(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	for i := 0; i < MaxNonBundledMCPServers; i++ {
		if _, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
			Name: fmt.Sprintf("vendor-%d", i),
			URL:  fmt.Sprintf("http://vendor-%d:9000/mcp", i),
			Tier: MCPServerTierLocalContainer,
		}); err != nil {
			t.Fatalf("register #%d (within the cap) failed: %v", i, err)
		}
	}
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledCount(servers) != MaxNonBundledMCPServers {
		t.Fatalf("non-bundled count = %d, want exactly %d", nonBundledCount(servers), MaxNonBundledMCPServers)
	}
}

// The (cap+1)th direct registration must be refused with a named error a
// caller can map consistently, and must leave the registry exactly at the
// cap — no partial row, no off-by-one growth.
func TestRegisterMCPServerRefusesOneBeyondTheCap(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	for i := 0; i < MaxNonBundledMCPServers; i++ {
		if _, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
			Name: fmt.Sprintf("vendor-%d", i),
			URL:  fmt.Sprintf("http://vendor-%d:9000/mcp", i),
			Tier: MCPServerTierLocalContainer,
		}); err != nil {
			t.Fatalf("register #%d (within the cap) failed: %v", i, err)
		}
	}

	_, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "one-too-many", URL: "http://one-too-many:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if !errors.Is(err, ErrMCPServerRegistryFull) {
		t.Fatalf("err = %v, want ErrMCPServerRegistryFull for the (cap+1)th registration", err)
	}

	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledCount(servers) != MaxNonBundledMCPServers {
		t.Fatalf("non-bundled count = %d, want it to remain exactly %d after the refusal", nonBundledCount(servers), MaxNonBundledMCPServers)
	}
	if _, err := repo.GetMCPServerByName(ctx, "one-too-many"); err != ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: the refused registration must create no row", err)
	}
}

// ImportMCPServer (the file-import repository path) must enforce the
// identical cap: a genuinely new entry beyond the cap is refused, leaving
// the registry unchanged, exactly the same way RegisterMCPServer's own
// refusal does.
func TestImportMCPServerRefusesOneBeyondTheCap(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	for i := 0; i < MaxNonBundledMCPServers; i++ {
		if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
			Name: fmt.Sprintf("vendor-%d", i),
			URL:  fmt.Sprintf("http://vendor-%d:9000/mcp", i),
			Tier: MCPServerTierLocalContainer,
		}); err != nil {
			t.Fatalf("import #%d (within the cap) failed: %v", i, err)
		}
	}

	_, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "one-too-many", URL: "http://one-too-many:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if !errors.Is(err, ErrMCPServerRegistryFull) {
		t.Fatalf("err = %v, want ErrMCPServerRegistryFull for the (cap+1)th import", err)
	}
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledCount(servers) != MaxNonBundledMCPServers {
		t.Fatalf("non-bundled count = %d, want it to remain exactly %d after the refusal", nonBundledCount(servers), MaxNonBundledMCPServers)
	}
}

// A bundled server never counts toward the non-bundled cap: seeding one
// (as migrations already do for "system"/"files"/"skills") must not
// shrink how many non-bundled servers can still be registered.
func TestMCPServerCapCountsOnlyNonBundled(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "already-bundled", URL: "https://bundled.example/mcp", Tier: MCPServerTierBundled,
	}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < MaxNonBundledMCPServers; i++ {
		if _, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
			Name: fmt.Sprintf("vendor-%d", i),
			URL:  fmt.Sprintf("http://vendor-%d:9000/mcp", i),
			Tier: MCPServerTierLocalContainer,
		}); err != nil {
			t.Fatalf("register #%d must still fit under the cap despite the bundled row: %v", i, err)
		}
	}
}

// Adopting a legacy migration-0016 placeholder in place (RegisterMCPServer's
// own adoption branch) is an UPDATE, not an INSERT: it must still succeed
// even when the registry is already exactly at the cap, because it does
// not grow the non-bundled row count. A genuinely new registration
// attempted immediately afterward, at that same (unchanged) count, must
// still be refused.
func TestRegisterMCPServerPlaceholderAdoptionAtCapDoesNotConsumeANewSlot(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	for i := 0; i < MaxNonBundledMCPServers-1; i++ {
		if _, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
			Name: fmt.Sprintf("vendor-%d", i),
			URL:  fmt.Sprintf("http://vendor-%d:9000/mcp", i),
			Tier: MCPServerTierLocalContainer,
		}); err != nil {
			t.Fatalf("register #%d: %v", i, err)
		}
	}
	// The last available slot is a legacy-placeholder-shaped row (a
	// disabled, non-bundled row with url==""), matching migration 0016's
	// own shape, bringing the registry to exactly the cap.
	placeholder, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "legacy-placeholder", URL: "", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatalf("seed the placeholder at the cap boundary: %v", err)
	}

	adopted, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "legacy-placeholder", URL: "https://legacy.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatalf("adopting a placeholder at the cap must not be refused: %v", err)
	}
	if !adopted.Adopted {
		t.Fatal("Adopted = false, want true")
	}
	if adopted.Server.ID != placeholder.Server.ID {
		t.Fatalf("adopted row id = %q, want the same placeholder row %q", adopted.Server.ID, placeholder.Server.ID)
	}

	if _, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "one-more", URL: "http://one-more:9000/mcp", Tier: MCPServerTierLocalContainer,
	}); !errors.Is(err, ErrMCPServerRegistryFull) {
		t.Fatalf("err = %v, want ErrMCPServerRegistryFull: adoption must not have freed or consumed a slot", err)
	}
}

// The same placeholder-adoption-does-not-consume-a-slot guarantee must
// hold for ImportMCPServer's own inline adoption branch (a file reimport
// naming an existing url-empty placeholder), not just RegisterMCPServer's.
func TestImportMCPServerPlaceholderAdoptionAtCapDoesNotConsumeANewSlot(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	for i := 0; i < MaxNonBundledMCPServers-1; i++ {
		if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
			Name: fmt.Sprintf("vendor-%d", i),
			URL:  fmt.Sprintf("http://vendor-%d:9000/mcp", i),
			Tier: MCPServerTierLocalContainer,
		}); err != nil {
			t.Fatalf("import #%d: %v", i, err)
		}
	}
	placeholder, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "legacy-placeholder", URL: "", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatalf("seed the placeholder at the cap boundary: %v", err)
	}

	adopted, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "legacy-placeholder", URL: "https://legacy.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatalf("adopting a placeholder at the cap via import must not be refused: %v", err)
	}
	if !adopted.Created {
		t.Fatal("Created = false, want true (adoption reports Created for the import path)")
	}
	if adopted.Server.ID != placeholder.Server.ID {
		t.Fatalf("adopted row id = %q, want the same placeholder row %q", adopted.Server.ID, placeholder.Server.ID)
	}

	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "one-more", URL: "http://one-more:9000/mcp", Tier: MCPServerTierLocalContainer,
	}); !errors.Is(err, ErrMCPServerRegistryFull) {
		t.Fatalf("err = %v, want ErrMCPServerRegistryFull: adoption must not have freed or consumed a slot", err)
	}
}

func nonBundledCount(servers []MCPServerRecord) int {
	count := 0
	for _, server := range servers {
		if server.Tier != MCPServerTierBundled {
			count++
		}
	}
	return count
}
