package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// buildMCPServersDocument builds a well-formed mcp.json document naming
// exactly count distinct, otherwise-valid server entries, so tests can
// prove behavior at and beyond the entry-count cap without each entry
// itself being the thing under test.
func buildMCPServersDocument(t *testing.T, count int) []byte {
	t.Helper()
	servers := make(map[string]any, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("vendor-%d", i)
		servers[name] = map[string]any{"url": fmt.Sprintf("http://%s:9000/mcp", name)}
	}
	document, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// mcp.json's entry count is capped before any entry is processed at all —
// the same repository.MaxNonBundledMCPServers limit the registry itself
// enforces, since a document naming more third-party servers than could
// ever actually register could otherwise force this call to open one
// repository transaction per entry for no possible benefit. Exceeding it
// must refuse the whole document as one hard error (mirroring the
// existing whole-document byte-size cap), not a per-entry Unsupported row
// for every single name.
func TestImportJSONRefusesTooManyEntriesBeforeProcessing(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	document := buildMCPServersDocument(t, repository.MaxNonBundledMCPServers+1)
	_, err := service.ImportJSON(ctx, document)
	if err == nil {
		t.Fatal("want an error for a document naming more entries than repository.MaxNonBundledMCPServers")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", repository.MaxNonBundledMCPServers)) {
		t.Fatalf("err = %v, want it to name the entry-count limit", err)
	}
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledServiceCount(servers) != 0 {
		t.Fatalf("non-bundled count = %d, want zero: an over-limit document must process no entry at all", nonBundledServiceCount(servers))
	}
}

// A document naming exactly repository.MaxNonBundledMCPServers entries
// must still be processed normally: the entry-count cap must not be
// off-by-one against a document that exactly fills the registry.
func TestImportJSONAtExactEntryCountCapStillProcessesNormally(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	document := buildMCPServersDocument(t, repository.MaxNonBundledMCPServers)
	report, err := service.ImportJSON(ctx, document)
	if err != nil {
		t.Fatalf("a document naming exactly the cap must not be refused outright: %v", err)
	}
	if len(report.Imported) != repository.MaxNonBundledMCPServers {
		t.Fatalf("Imported = %d entries, want exactly %d", len(report.Imported), repository.MaxNonBundledMCPServers)
	}
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledServiceCount(servers) != repository.MaxNonBundledMCPServers {
		t.Fatalf("non-bundled count = %d, want exactly %d", nonBundledServiceCount(servers), repository.MaxNonBundledMCPServers)
	}
}

// ReimportConfiguredJSON must collapse an over-limit document into exactly
// one bounded "_document" refusal — not one row per named entry — so
// mcp_import_issues and the ReimportMcpJson RPC's own Refused list stay
// bounded regardless of how many names an oversized document claims.
func TestReimportConfiguredJSONCollapsesTooManyEntriesToOneDocumentRefusal(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	root := t.TempDir()
	document := buildMCPServersDocument(t, repository.MaxNonBundledMCPServers+1)
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), document, 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	report, err := service.ReimportConfiguredJSON(ctx)
	if err != nil {
		t.Fatalf("an over-limit document must be reported, not returned as an error: %v", err)
	}
	if len(report.Imported) != 0 || len(report.Skipped) != 0 {
		t.Fatalf("report = %+v, want no imports or skips for an over-limit document", report)
	}
	if len(report.Unsupported) != 1 {
		t.Fatalf("Unsupported = %+v, want exactly one collapsed _document entry, not one per named server", report.Unsupported)
	}
	if _, refused := report.Unsupported["_document"]; !refused {
		t.Fatalf("Unsupported = %+v, want the _document key", report.Unsupported)
	}
	issues, err := repo.ListMCPImportIssues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Name != "_document" {
		t.Fatalf("issues = %+v, want exactly one bounded _document issue", issues)
	}
}

func nonBundledServiceCount(servers []repository.MCPServerRecord) int {
	count := 0
	for _, server := range servers {
		if server.Tier != repository.MCPServerTierBundled {
			count++
		}
	}
	return count
}
