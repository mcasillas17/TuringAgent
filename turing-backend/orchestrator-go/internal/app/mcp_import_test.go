package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
)

// A directory in place of mcp.json (or any other non-ENOENT read failure) is
// not the clean-slate "absent file" case: it must abort startup with a
// fixed, safe error rather than silently starting with a registry no one
// can inspect, and that error must never repeat the config root's
// filesystem path.
func TestUnreadableMCPJSONAbortsStartup(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "mcp.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := New(config.Config{
		ClientAPIKey: "client", RuntimeToken: "runtime",
		ApprovalConsumerToken: "consumer", ApprovalJWTSecret: "approval",
		EgressSigningSecret: "egress", DatabasePath: filepath.Join(t.TempDir(), "turing.db"),
		MCPConfigRoot: root, OllamaModel: "local",
	})
	if err == nil {
		t.Fatal("startup succeeded despite an unreadable mcp.json")
	}
	if strings.Contains(err.Error(), root) {
		t.Fatalf("startup error = %q, must not expose the config root path", err.Error())
	}
}

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
