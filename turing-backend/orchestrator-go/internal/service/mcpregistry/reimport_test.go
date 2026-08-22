package mcpregistry

import (
	"bytes"
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
)

// Reimporting an existing, non-bundled server is create-only: the file
// import must report it as skipped and leave every observable bit of its
// state exactly as it was, whatever the new mcp.json says.

func TestReimportOfDisabledServerLeavesItDisabledAndReportsSkipped(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
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

	report, err = service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skipped) != 1 || report.Skipped[0] != "vendor" {
		t.Fatalf("Skipped = %v, want [vendor]", report.Skipped)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none on reimport", report.Imported)
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if vendor.Enabled {
		t.Fatal("reimport must not enable a server")
	}
}

func TestReimportPreservesEditedToolPolicy(t *testing.T) {
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
	vendor := findRepositoryServer(t, servers, "vendor")
	if err := repo.SetMCPToolPolicy(context.Background(), vendor.ID, "vendor.lookup", "safe"); err != nil {
		t.Fatal(err)
	}

	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "vendor.lookup", "inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skipped) != 1 || report.Skipped[0] != "vendor" {
		t.Fatalf("Skipped = %v, want [vendor]", report.Skipped)
	}

	tools, err := repo.ListMCPServerTools(context.Background(), vendor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Policy != "safe" {
		t.Fatalf("tools = %+v, want the edited policy preserved", tools)
	}
}

func TestReimportPreservesRotatedTokenInsteadOfReplacingIt(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if _, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer original-secret"}
			}
		}
	}`)); err != nil {
		t.Fatal(err)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	original := findRepositoryServer(t, servers, "vendor")

	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer rotated-secret"}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skipped) != 1 || report.Skipped[0] != "vendor" {
		t.Fatalf("Skipped = %v, want [vendor]", report.Skipped)
	}

	servers, err = repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after := findRepositoryServer(t, servers, "vendor")
	if !bytes.Equal(after.SealedToken, original.SealedToken) {
		t.Fatal("reimport replaced the sealed token instead of preserving it")
	}
}

// A particularly important case: an existing server that already has a
// token must still be skipped on reimport even when the service has no
// sealer configured, rather than failing with ErrNoKey.
func TestReimportSkipsExistingTokenedServerEvenWithoutSealer(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)

	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	seeded := New(repo, sealer, nil)
	if _, err := seeded.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer original-secret"}
			}
		}
	}`)); err != nil {
		t.Fatal(err)
	}

	unsealed := New(repo, nil, nil)
	report, err := unsealed.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer rotated-secret"}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("ImportJSON returned an error instead of skipping: %v", err)
	}
	if len(report.Skipped) != 1 || report.Skipped[0] != "vendor" {
		t.Fatalf("Skipped = %v, want [vendor]", report.Skipped)
	}
	if len(report.Unsupported) != 0 {
		t.Fatalf("Unsupported = %v, want none", report.Unsupported)
	}
}

func TestTombstonedNameRemainsRefusedByFileReimport(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if _, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`)); err != nil {
		t.Fatal(err)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if err := repo.DeleteMCPServer(context.Background(), vendor.ID); err != nil {
		t.Fatal(err)
	}

	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 || len(report.Skipped) != 0 {
		t.Fatalf("Imported = %v, Skipped = %v, want a tombstoned name refused into Unsupported", report.Imported, report.Skipped)
	}
	if _, present := report.Unsupported["vendor"]; !present {
		t.Fatalf("Unsupported = %v, want vendor refused", report.Unsupported)
	}

	tombstoned, err := repo.MCPServerTombstoned(context.Background(), "vendor")
	if err != nil {
		t.Fatal(err)
	}
	if !tombstoned {
		t.Fatal("tombstone was cleared by a refused file reimport")
	}
}

func TestImportReportSortsImportedAndSkippedDeterministically(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if _, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"zebra": {"url": "https://zebra.example/mcp"},
			"alpha": {"url": "https://alpha.example/mcp"}
		}
	}`)); err != nil {
		t.Fatal(err)
	}
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"zebra": {"url": "https://zebra.example/mcp"},
			"alpha": {"url": "https://alpha.example/mcp"},
			"middle": {"url": "https://middle.example/mcp"},
			"beta": {"url": "https://beta.example/mcp"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Imported; len(got) != 2 || got[0] != "beta" || got[1] != "middle" {
		t.Fatalf("Imported = %v, want sorted [beta middle]", got)
	}
	if got := report.Skipped; len(got) != 2 || got[0] != "alpha" || got[1] != "zebra" {
		t.Fatalf("Skipped = %v, want sorted [alpha zebra]", got)
	}
	_ = repo
}
