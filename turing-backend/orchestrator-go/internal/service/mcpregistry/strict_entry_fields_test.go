package mcpregistry

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// Go's encoding/json resolves a struct tag case-insensitively whenever no
// exact-case match exists: an entry spelling "tools" as "TOOLS" (or
// "Tools") still decodes straight into mcpJSONServer.Tools. Before this
// fix, ImportJSON's own toolsPresent flag was computed from a *second*,
// independent exact-lowercase-key map lookup, so it disagreed with what
// the struct silently decoded: the entry was treated as if it had no
// "tools" key at all — buildImportTools was never called, and whatever
// the entry's author believed they were importing was silently dropped
// with no refusal, no reason, and no imported/skipped disposition either.
// That silent divergence is exactly what this test rules out: an entry
// spelling "tools" wrong must be refused outright, deterministically, with
// a bounded reason — never partially imported with its tools quietly
// discarded.
func TestImportJSONRejectsToolsCasingVariant(t *testing.T) {
	for _, key := range []string{"TOOLS", "Tools", "tOoLs"} {
		t.Run(key, func(t *testing.T) {
			service, repo := newRegistryTestService(t)
			ctx := context.Background()

			report, err := service.ImportJSON(ctx, []byte(`{
				"mcpServers": {
					"vendor": {
						"url": "https://vendor.example/mcp",
						"`+key+`": [{"name": "vendor.lookup", "inputSchema": {"type": "object"}}]
					}
				}
			}`))
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Imported) != 0 {
				t.Fatalf("Imported = %v, want none: a %q key must never be silently accepted as \"tools\"", report.Imported, key)
			}
			if len(report.Skipped) != 0 {
				t.Fatalf("Skipped = %v, want none", report.Skipped)
			}
			if _, refused := report.Unsupported["vendor"]; !refused {
				t.Fatalf("Unsupported = %+v, want vendor refused for a %q key", report.Unsupported, key)
			}
			if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
				t.Fatalf("err = %v, want ErrMCPServerNotFound: a rejected entry must create no row", err)
			}
		})
	}
}

// Every one of an entry's six recognized field names — url, headers,
// command, args, env, tools — must be refused the same way when spelled
// with any other case, not just "tools". This proves the fix is a general
// exact-canonical-key requirement, not a point patch for one field name.
func TestImportJSONRejectsCaseVariantForEveryCanonicalField(t *testing.T) {
	for _, test := range []struct {
		name  string
		entry string
	}{
		{name: "URL", entry: `{"URL": "https://vendor.example/mcp"}`},
		{name: "Url", entry: `{"Url": "https://vendor.example/mcp"}`},
		{name: "HEADERS", entry: `{"url": "https://vendor.example/mcp", "HEADERS": {}}`},
		{name: "Command", entry: `{"Command": "npx"}`},
		{name: "ARGS", entry: `{"url": "https://vendor.example/mcp", "ARGS": ["x"]}`},
		{name: "Env", entry: `{"url": "https://vendor.example/mcp", "Env": {}}`},
		{name: "TOOLS", entry: `{"url": "https://vendor.example/mcp", "TOOLS": []}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo := newRegistryTestService(t)
			ctx := context.Background()

			report, err := service.ImportJSON(ctx, []byte(`{"mcpServers": {"vendor": `+test.entry+`}}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, refused := report.Unsupported["vendor"]; !refused {
				t.Fatalf("Unsupported = %+v, want vendor refused for field name %q", report.Unsupported, test.name)
			}
			if len(report.Imported) != 0 || len(report.Skipped) != 0 {
				t.Fatalf("Imported = %v, Skipped = %v, want neither for an entry with a %q field name", report.Imported, report.Skipped, test.name)
			}
			if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
				t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
			}
		})
	}
}

// A duplicate "tools" key — whether an exact-spelling repeat (which JSON
// itself permits inside a single object) or a case-insensitive one (e.g.
// "tools" and "Tools" both present) — must be refused rather than
// silently resolved by taking whichever value plain unmarshaling would
// keep.
func TestImportJSONRejectsDuplicateToolsKey(t *testing.T) {
	for _, test := range []struct {
		name  string
		entry string
	}{
		{
			name: "exact duplicate spelling",
			entry: `{
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "vendor.first", "inputSchema": {"type": "object"}}],
				"tools": [{"name": "vendor.second", "inputSchema": {"type": "object"}}]
			}`,
		},
		{
			name: "case-insensitive duplicate",
			entry: `{
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "vendor.first", "inputSchema": {"type": "object"}}],
				"Tools": [{"name": "vendor.second", "inputSchema": {"type": "object"}}]
			}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo := newRegistryTestService(t)
			ctx := context.Background()

			report, err := service.ImportJSON(ctx, []byte(`{"mcpServers": {"vendor": `+test.entry+`}}`))
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Imported) != 0 {
				t.Fatalf("Imported = %v, want none: a duplicate tools key must refuse the whole entry", report.Imported)
			}
			if _, refused := report.Unsupported["vendor"]; !refused {
				t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
			}
			if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
				t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
			}
		})
	}
}

// A duplicate "url" key must be refused the same way a duplicate "tools"
// key is — not resolved by silently keeping whichever value plain
// unmarshaling would keep (typically the last one). Without this, an
// attacker-controlled second "url" member could redirect an import to a
// different endpoint than the one a reviewer of the mcp.json file saw
// first.
func TestImportJSONRejectsDuplicateURLKey(t *testing.T) {
	for _, test := range []struct {
		name  string
		entry string
	}{
		{
			name:  "exact duplicate spelling",
			entry: `{"url": "https://first.example/mcp", "url": "https://second.example/mcp"}`,
		},
		{
			name:  "case-insensitive duplicate",
			entry: `{"url": "https://first.example/mcp", "URL": "https://second.example/mcp"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo := newRegistryTestService(t)
			ctx := context.Background()

			report, err := service.ImportJSON(ctx, []byte(`{"mcpServers": {"vendor": `+test.entry+`}}`))
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Imported) != 0 {
				t.Fatalf("Imported = %v, want none: a duplicate url key must refuse the whole entry", report.Imported)
			}
			if _, refused := report.Unsupported["vendor"]; !refused {
				t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
			}
			if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
				t.Fatalf("err = %v, want ErrMCPServerNotFound: no row, not even one keyed to either URL", err)
			}
			servers, err := repo.ListMCPServers(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, server := range servers {
				if server.Name == "vendor" {
					t.Fatalf("a row was created despite the duplicate url refusal: %+v", server)
				}
			}
		})
	}
}

// A field-name conflict (case variant or duplicate) must never adopt a
// pre-existing legacy migration-0016 placeholder row: the refusal has to
// happen before ImportJSON ever looks up whether "vendor" already exists,
// so the placeholder is left completely untouched — no partial adoption,
// no url/tier/token change, no liveness reset, no tool withdrawal.
func TestImportJSONEntryFieldConflictNeverAdoptsPlaceholder(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	placeholder, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, placeholder.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.lookup", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"URL": "https://attacker.example/mcp"
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 || len(report.Skipped) != 0 {
		t.Fatalf("Imported = %v, Skipped = %v, want neither: the placeholder must not be touched", report.Imported, report.Skipped)
	}
	if _, refused := report.Unsupported["vendor"]; !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}

	after, err := repo.GetMCPServer(ctx, placeholder.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.URL != "" {
		t.Fatalf("URL = %q, want the placeholder's empty url left untouched", after.URL)
	}
	if after.Tier != repository.MCPServerTierLocalContainer {
		t.Fatalf("Tier = %q, want the placeholder's original tier untouched", after.Tier)
	}
	tools, err := repo.ListMCPServerTools(ctx, placeholder.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || !tools[0].Present {
		t.Fatalf("tools = %+v, want the placeholder's carried tool left present, not withdrawn", tools)
	}
}

// An entry using every canonical field name, spelled exactly, must still
// import normally: the new exact-key requirement must not be off-by-one
// against a legitimate, well-formed entry.
func TestImportJSONExactCanonicalFieldNamesStillImport(t *testing.T) {
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
