package mcpregistry

import (
	"bytes"
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

// A tool's "description" is accepted (it must not, on its own, refuse an
// otherwise-valid entry) but is never stored or returned anywhere:
// repository.MCPServerTool has no field for it, toolDescriptor's proto
// mapping has nothing to read it from, and this sweeps the actual
// persisted schema_json column to confirm the description text was never
// folded into it either.
func TestImportJSONStaticSnapshotDescriptionIsAcceptedButNeverStored(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	const description = "looks up a vendor record by its identifier"

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{
					"name": "vendor.lookup",
					"description": "`+description+`",
					"inputSchema": {"type": "object"}
				}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]: a benign description must not refuse an otherwise-valid entry", report.Imported)
	}

	server, err := repo.GetMCPServerByName(ctx, "vendor")
	if err != nil {
		t.Fatal(err)
	}
	tools, err := repo.ListMCPServerTools(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %+v, want exactly one", tools)
	}
	if strings.Contains(tools[0].SchemaJSON, description) {
		t.Fatalf("schema_json = %q, want the description never folded into the stored schema", tools[0].SchemaJSON)
	}
}

// A tool's "description" is never stored (repository.MCPServerTool has no
// field for it), but it must still count toward maxMCPToolBytes: otherwise
// an oversized description would inflate this call's real in-memory and
// wire footprint arbitrarily while completely evading the one limit meant
// to bound a static snapshot's size, since the byte accounting previously
// only ever summed name+schema.
func TestImportJSONStaticSnapshotByteLimitCountsDescriptionBytes(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	oversizedDescription := strings.Repeat("a", maxMCPToolBytes+1)
	document, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"vendor": map[string]any{
				"url": "https://vendor.example/mcp",
				"tools": []map[string]any{
					{
						"name":        "vendor.lookup",
						"description": oversizedDescription,
						"inputSchema": map[string]any{"type": "object"},
					},
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
		t.Fatalf("Imported = %v, want none: an oversized description alone must exceed the per-snapshot byte limit", report.Imported)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if !strings.Contains(reason, fmt.Sprintf("%d", maxMCPToolBytes)) {
		t.Fatalf("reason = %q, want it to name the maxMCPToolBytes limit", reason)
	}
	if strings.Contains(reason, "aaaa") {
		t.Fatalf("reason = %q, must not echo the oversized description back", reason)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: no row may remain after the refusal", err)
	}
}

// The entire mcp.json document is bounded before it is ever handed to the
// JSON decoder, independent of any per-server/per-tool limit: a document
// whose raw byte size alone exceeds maxMCPImportDocumentBytes is refused
// generically and boundedly (never echoing any of its content), and
// leaves no partial writes — nothing between decode and the repository
// ever runs.
func TestImportJSONRefusesOversizedWholeDocumentBeforeDecode(t *testing.T) {
	service, _ := newRegistryTestService(t)
	ctx := context.Background()

	oversized := bytes.Repeat([]byte("a"), maxMCPImportDocumentBytes+1)
	_, err := service.ImportJSON(ctx, oversized)
	if err == nil {
		t.Fatal("want an error for a document exceeding maxMCPImportDocumentBytes")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", maxMCPImportDocumentBytes)) {
		t.Fatalf("err = %v, want it to name the maxMCPImportDocumentBytes limit", err)
	}
}

// A document at exactly the cap must still be handed to the decoder
// normally — the size cap uses a strict `>` comparison
// (len(data) > maxMCPImportDocumentBytes), so this constructs a document
// whose byte length is precisely maxMCPImportDocumentBytes (padded with
// insignificant leading whitespace, which JSON permits before the opening
// brace and which json.Decoder simply skips) to prove the boundary itself
// is not off-by-one against a legitimate, in-bounds document — not just
// "some small document far under the cap," which the oversized-document
// test's own "cap+1" case does not, by itself, rule out.
func TestImportJSONDocumentAtSizeCapStillDecodes(t *testing.T) {
	service, _ := newRegistryTestService(t)
	ctx := context.Background()

	base := []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`)
	padding := maxMCPImportDocumentBytes - len(base)
	if padding < 0 {
		t.Fatalf("test setup is broken: base document (%d bytes) already exceeds maxMCPImportDocumentBytes (%d)", len(base), maxMCPImportDocumentBytes)
	}
	document := append(bytes.Repeat([]byte(" "), padding), base...)
	if len(document) != maxMCPImportDocumentBytes {
		t.Fatalf("test setup is broken: document = %d bytes, want exactly maxMCPImportDocumentBytes (%d)", len(document), maxMCPImportDocumentBytes)
	}

	report, err := service.ImportJSON(ctx, document)
	if err != nil {
		t.Fatalf("a document of exactly maxMCPImportDocumentBytes must not be refused: %v", err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", report.Imported)
	}
}
