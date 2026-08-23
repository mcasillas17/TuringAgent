package repository

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestImportMCPServerCreatesNewDisabledServer(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created {
		t.Fatal("Created = false, want true for a brand new server")
	}
	if result.Server.Enabled {
		t.Fatal("a freshly imported server must arrive disabled")
	}
	if result.Server.URL != "http://vendor:9000/mcp" {
		t.Fatalf("URL = %q", result.Server.URL)
	}
}

func TestImportMCPServerSkipsExistingServerAndPreservesState(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	created, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", SealedToken: []byte("sealed-original"), Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Created {
		t.Fatal("first import should create the server")
	}
	if err := repo.SetMCPServerEnabled(ctx, created.Server.ID, true); err != nil {
		t.Fatal(err)
	}

	// Reimport with a different URL, tier, and token: this must be a no-op
	// on the existing row other than reporting it as already-registered.
	result, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", SealedToken: []byte("sealed-rotated"), Tier: MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created {
		t.Fatal("Created = true, want false for an existing server")
	}
	if result.Server.ID != created.Server.ID {
		t.Fatalf("server id changed on reimport: %q vs %q", result.Server.ID, created.Server.ID)
	}
	if !result.Server.Enabled {
		t.Fatal("reimport disabled a server that was enabled")
	}
	if result.Server.URL != "http://vendor:9000/mcp" {
		t.Fatalf("reimport changed URL to %q", result.Server.URL)
	}
	if result.Server.Tier != MCPServerTierLocalContainer {
		t.Fatalf("reimport changed tier to %q", result.Server.Tier)
	}
	if !bytes.Equal(result.Server.SealedToken, []byte("sealed-original")) {
		t.Fatalf("reimport rotated the sealed token to %q", result.Server.SealedToken)
	}

	fetched, err := repo.GetMCPServer(ctx, created.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !fetched.Enabled || fetched.URL != "http://vendor:9000/mcp" {
		t.Fatalf("persisted server was mutated by reimport: %+v", fetched)
	}
}

func TestImportMCPServerRefusesBundled(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	_, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "system", URL: "http://turing-mcp-system:7100/mcp", Tier: MCPServerTierLocalContainer,
	})
	if !errors.Is(err, ErrMCPServerBundled) {
		t.Fatalf("err = %v, want ErrMCPServerBundled", err)
	}
}

func TestImportMCPServerRefusesTombstonedName(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	created, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DeleteMCPServer(ctx, created.Server.ID); err != nil {
		t.Fatal(err)
	}
	_, err = repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if !errors.Is(err, ErrMCPServerImportSuppressed) {
		t.Fatalf("err = %v, want ErrMCPServerImportSuppressed", err)
	}
}

func TestRegisterMCPServerClearsTombstoneAndCreatesDisabledServer(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	created, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DeleteMCPServer(ctx, created.Server.ID); err != nil {
		t.Fatal(err)
	}

	registered, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if registered.Server.Enabled {
		t.Fatal("explicit registration must arrive disabled")
	}
	if registered.Server.URL != "https://vendor.example/mcp" {
		t.Fatalf("URL = %q", registered.Server.URL)
	}

	// The tombstone must be gone: a plain import can now create it again
	// (proving it is a fresh row, not merely unlocked).
	again, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-second-probe", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = again
	var tombstoned int
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_import_tombstones WHERE name = ?`, "vendor").Scan(&tombstoned); err != nil {
		t.Fatal(err)
	}
	if tombstoned != 0 {
		t.Fatal("registration did not clear the matching tombstone")
	}
}

func TestRegisterMCPServerRefusesExistingName(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	if _, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if !errors.Is(err, ErrMCPServerNameTaken) {
		t.Fatalf("err = %v, want ErrMCPServerNameTaken", err)
	}
}

func TestRegisterMCPServerRefusesBundledName(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	_, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "system", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if !errors.Is(err, ErrMCPServerBundled) {
		t.Fatalf("err = %v, want ErrMCPServerBundled", err)
	}
}

func TestRegisterMCPServerFailureLeavesTombstoneIntact(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	created, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Contrive a tombstone that coexists with a live server of the same
	// name (this cannot happen through normal deletion, which always
	// removes the row it tombstones — it validates that a failed
	// registration never clears a tombstone as a side effect).
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO mcp_import_tombstones (name, deleted_at) VALUES (?, ?)
	`, "vendor", "2024-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	_, err = repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if !errors.Is(err, ErrMCPServerNameTaken) {
		t.Fatalf("err = %v, want ErrMCPServerNameTaken", err)
	}
	_ = created
	var tombstoned int
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_import_tombstones WHERE name = ?`, "vendor").Scan(&tombstoned); err != nil {
		t.Fatal(err)
	}
	if tombstoned != 1 {
		t.Fatal("failed registration cleared the tombstone")
	}
}

func TestReplaceMCPServerTokenStoresAndClears(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	created, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repo.ReplaceMCPServerToken(ctx, created.Server.ID, []byte("sealed-rotated"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(updated.SealedToken, []byte("sealed-rotated")) {
		t.Fatalf("SealedToken = %q, want rotated value", updated.SealedToken)
	}
	fetched, err := repo.GetMCPServer(ctx, created.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fetched.SealedToken, []byte("sealed-rotated")) {
		t.Fatalf("persisted SealedToken = %q, want rotated value", fetched.SealedToken)
	}

	cleared, err := repo.ReplaceMCPServerToken(ctx, created.Server.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.SealedToken != nil {
		t.Fatalf("SealedToken = %q, want nil after clearing", cleared.SealedToken)
	}
	var raw any
	if err := repo.db.QueryRowContext(ctx, `SELECT sealed_token FROM mcp_servers WHERE id = ?`, created.Server.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Fatalf("sealed_token column = %v, want SQL NULL after clearing", raw)
	}
}

func TestReplaceMCPServerTokenRefusesBundled(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var bundledID string
	for _, server := range servers {
		if server.Tier == MCPServerTierBundled {
			bundledID = server.ID
			break
		}
	}
	if bundledID == "" {
		t.Fatal("no bundled server seeded by migrations")
	}
	_, err = repo.ReplaceMCPServerToken(ctx, bundledID, []byte("sealed"))
	if !errors.Is(err, ErrMCPServerBundled) {
		t.Fatalf("err = %v, want ErrMCPServerBundled", err)
	}
}
