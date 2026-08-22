package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// mcp.json's static "tools" snapshot must be bounded by the exact same
// maxMCPTools count limit a live tools/list response is bounded by
// (mcpClient.listTools): an entry naming more tools than that must be
// refused entirely, with a fixed, generic, bounded reason, and must leave
// no server row behind — never a partial import a corrected, smaller
// snapshot could only skip.
func TestImportJSONStaticSnapshotToolCountLimitRefusesWholeEntryWithNoPartialRow(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	tools := make([]map[string]any, maxMCPTools+1)
	for i := range tools {
		tools[i] = map[string]any{
			"name":        fmt.Sprintf("vendor.tool_%d", i),
			"inputSchema": map[string]any{"type": "object"},
		}
	}
	document, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"vendor": map[string]any{
				"url":   "https://vendor.example/mcp",
				"tools": tools,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.ImportJSON(ctx, document)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: exceeding maxMCPTools must refuse the whole entry", report.Imported)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if !strings.Contains(reason, fmt.Sprintf("%d", maxMCPTools)) {
		t.Fatalf("reason = %q, want it to name the maxMCPTools limit", reason)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: no row may remain after the refusal", err)
	}
}

// mcp.json's static "tools" snapshot must also be bounded by the same
// maxMCPToolBytes serialized-size limit a live tools/list response is
// bounded by, counted the same way live discovery counts it: a running
// total of each tool's encoded descriptor. A single tool whose serialized
// name+schema alone exceeds the limit must refuse the whole entry with a
// fixed, generic, bounded reason (never echoing the oversized schema back)
// and leave no server row behind.
func TestImportJSONStaticSnapshotByteLimitRefusesWholeEntryWithNoPartialRow(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	oversizedSchema := map[string]any{
		"type":    "object",
		"padding": strings.Repeat("a", maxMCPToolBytes+1),
	}
	document, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"vendor": map[string]any{
				"url": "https://vendor.example/mcp",
				"tools": []map[string]any{
					{"name": "vendor.oversized", "inputSchema": oversizedSchema},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.ImportJSON(ctx, document)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: exceeding maxMCPToolBytes must refuse the whole entry", report.Imported)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if strings.Contains(reason, "aaaa") {
		t.Fatalf("reason = %q, must not echo the oversized schema back", reason)
	}
	if !strings.Contains(reason, fmt.Sprintf("%d", maxMCPToolBytes)) {
		t.Fatalf("reason = %q, want it to name the maxMCPToolBytes limit", reason)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: no row may remain after the refusal", err)
	}
}

// A snapshot within both limits must still import normally: the limits
// must not be off-by-one against a legitimate, in-bounds snapshot.
func TestImportJSONStaticSnapshotWithinLimitsStillImports(t *testing.T) {
	service, _ := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
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
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", report.Imported)
	}
}
