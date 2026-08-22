package repository

import (
	"context"
	"errors"
	"testing"
)

// RegisterMCPServer must refuse any ImportedMCPServer carrying a Tools
// snapshot — nil vs. non-nil, the same "present" semantics
// ImportedMCPServer.Tools documents for ImportMCPServer — with the named
// ErrMCPServerToolsNotAllowed error, and must leave no row behind. This is
// the guard against a caller bypassing the service layer's own tool
// validation (name/schema shape, bundled-namespace and inter-server
// collision checks) by handing tools straight to direct registration,
// which ImportMCPServer alone is meant to reconcile.
func TestRegisterMCPServerRefusesToolsSnapshot(t *testing.T) {
	for _, test := range []struct {
		name  string
		tools []MCPServerTool
	}{
		{name: "non-empty tools", tools: []MCPServerTool{{Name: "vendor.lookup", Policy: "safe", SchemaJSON: `{"type":"object"}`}}},
		{name: "explicit empty tools", tools: []MCPServerTool{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			_, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
				Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
				Tools: test.tools,
			})
			if !errors.Is(err, ErrMCPServerToolsNotAllowed) {
				t.Fatalf("err = %v, want ErrMCPServerToolsNotAllowed", err)
			}
			if _, ferr := repo.GetMCPServerByName(ctx, "vendor"); !errors.Is(ferr, ErrMCPServerNotFound) {
				t.Fatalf("err = %v, want ErrMCPServerNotFound: a refused registration must leave no row", ferr)
			}
		})
	}
}

// A normal direct registration — no Tools at all (nil, the "absent" case
// the guard must not reject) — must be entirely unaffected by the guard.
func TestRegisterMCPServerWithoutToolsStillSucceeds(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Name != "vendor" {
		t.Fatalf("Name = %q, want vendor", server.Name)
	}
}

// Adopting a migration-0016 placeholder resets its liveness to unknown
// with an empty status message in the same transaction as the row
// update, exactly like ReplaceMCPServerToken resets liveness alongside a
// rotated credential: a placeholder's endpoint was never verified, so
// whatever liveness it happened to carry (even a prior "down") says
// nothing about the real endpoint this adoption now populates.
func TestImportMCPServerPlaceholderAdoptionResetsLivenessStatusToUnknown(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	placeholder, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantPriorMessage = "down: last probe failed at 2024-01-01T00:00:00Z"
	if err := repo.SetMCPServerStatus(ctx, placeholder.ID, "down", wantPriorMessage); err != nil {
		t.Fatal(err)
	}
	before, err := repo.GetMCPServer(ctx, placeholder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != "down" || before.StatusError != wantPriorMessage {
		t.Fatalf("test setup failed: before = %+v, want down status with the prior message", before)
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
	if result.Server.Status != "unknown" {
		t.Fatalf("Status = %q, want unknown: adoption must reset a prior status", result.Server.Status)
	}
	if result.Server.StatusError != "" {
		t.Fatalf("StatusError = %q, want empty: adoption must reset a prior status message", result.Server.StatusError)
	}

	after, err := repo.GetMCPServer(ctx, placeholder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "unknown" || after.StatusError != "" {
		t.Fatalf("after = %+v, want unknown status with no message persisted", after)
	}
}
