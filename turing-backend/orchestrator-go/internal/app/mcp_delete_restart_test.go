package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
)

func TestDeletedImportedMCPServerStaysDeletedAfterRestart(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {"vendor": {"url": "https://vendor.example/mcp"}}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(t.TempDir(), "turing.db")
	cfg := config.Config{
		ClientAPIKey: "client", RuntimeToken: "runtime",
		ApprovalConsumerToken: "consumer", ApprovalJWTSecret: "approval",
		EgressSigningSecret: "egress", DatabasePath: databasePath,
		MCPConfigRoot: root, OllamaModel: "local",
	}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	servers, err := first.Repository.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var vendorID string
	for _, server := range servers {
		if server.Name == "vendor" {
			vendorID = server.ID
		}
	}
	if vendorID == "" {
		t.Fatal("vendor was not imported")
	}
	if _, err := first.MCPRegistryService.DeleteMcpServer(
		context.Background(),
		&turingv1.DeleteMcpServerRequest{ServerId: vendorID},
	); err != nil {
		t.Fatal(err)
	}
	first.Stop()

	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Stop)
	servers, err = second.Repository.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Name == "vendor" {
			t.Fatal("deleted imported server reappeared after restart")
		}
	}
}
