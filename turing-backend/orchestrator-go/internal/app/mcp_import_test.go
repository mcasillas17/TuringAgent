package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
)

func TestMalformedMCPJSONIsReportedWithoutPreventingStartup(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{"mcpServers":`), 0o600); err != nil {
		t.Fatal(err)
	}
	application, err := New(config.Config{
		ClientAPIKey: "client", RuntimeToken: "runtime",
		ApprovalConsumerToken: "consumer", ApprovalJWTSecret: "approval",
		EgressSigningSecret: "egress", DatabasePath: filepath.Join(t.TempDir(), "turing.db"),
		MCPConfigRoot: root, OllamaModel: "local",
	})
	if err != nil {
		t.Fatalf("startup failed for malformed mcp.json: %v", err)
	}
	t.Cleanup(application.Stop)
	issues, err := application.Repository.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Name != "_document" {
		t.Fatalf("import issues = %+v, want document-level diagnostic", issues)
	}
}
