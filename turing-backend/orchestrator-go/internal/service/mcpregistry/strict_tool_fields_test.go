package mcpregistry

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// Go's encoding/json resolves a struct tag case-insensitively whenever no
// exact-case match exists: a tool object spelling "name" as "Name" (or
// "NAME") used to decode straight into mcpJSONTool.Name exactly as if
// spelled correctly. This is the tool-object counterpart of
// TestImportJSONRejectsToolsCasingVariant (strict_entry_fields_test.go) —
// the same exact-canonical-key requirement, applied one level deeper, to
// each element of a "tools" array rather than an entry's own top-level
// fields.
func TestImportJSONRejectsToolNameCasingVariant(t *testing.T) {
	for _, key := range []string{"Name", "NAME", "nAmE"} {
		t.Run(key, func(t *testing.T) {
			service, repo := newRegistryTestService(t)
			ctx := context.Background()

			report, err := service.ImportJSON(ctx, []byte(`{
				"mcpServers": {
					"vendor": {
						"url": "https://vendor.example/mcp",
						"tools": [{"`+key+`": "vendor.lookup", "inputSchema": {"type": "object"}}]
					}
				}
			}`))
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Imported) != 0 {
				t.Fatalf("Imported = %v, want none: a %q key must never be silently accepted as \"name\"", report.Imported, key)
			}
			reason, refused := report.Unsupported["vendor"]
			if !refused {
				t.Fatalf("Unsupported = %+v, want vendor refused for a %q key", report.Unsupported, key)
			}
			if reason != mcpToolDefinitionRefusedMessage {
				t.Fatalf("reason = %q, want the fixed generic reason %q", reason, mcpToolDefinitionRefusedMessage)
			}
			if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
				t.Fatalf("err = %v, want ErrMCPServerNotFound: a rejected entry must create no row", err)
			}
		})
	}
}

// A duplicate "name" key inside one tool object — an exact-spelling
// repeat, which JSON itself permits inside a single object — must be
// refused rather than silently resolved to whichever value plain
// unmarshaling would keep.
func TestImportJSONRejectsDuplicateToolNameKey(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "vendor.first", "name": "vendor.second", "inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: a duplicate name key must refuse the whole entry", report.Imported)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if reason != mcpToolDefinitionRefusedMessage {
		t.Fatalf("reason = %q, want the fixed generic reason %q", reason, mcpToolDefinitionRefusedMessage)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
	}
}

// A duplicate "inputSchema" key — whether an exact-spelling repeat or a
// case-insensitive one (e.g. "inputSchema" and "InputSchema" both
// present) — must be refused the same way.
func TestImportJSONRejectsDuplicateOrCaseVariantInputSchemaKey(t *testing.T) {
	for _, test := range []struct {
		name string
		tool string
	}{
		{
			name: "exact duplicate spelling",
			tool: `{"name": "vendor.lookup", "inputSchema": {"type": "object"}, "inputSchema": {"type": "object", "extra": true}}`,
		},
		{
			name: "case-insensitive duplicate",
			tool: `{"name": "vendor.lookup", "inputSchema": {"type": "object"}, "InputSchema": {"type": "object", "extra": true}}`,
		},
		{
			name: "case variant alone",
			tool: `{"name": "vendor.lookup", "InputSchema": {"type": "object"}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo := newRegistryTestService(t)
			ctx := context.Background()

			report, err := service.ImportJSON(ctx, []byte(`{
				"mcpServers": {
					"vendor": {
						"url": "https://vendor.example/mcp",
						"tools": [`+test.tool+`]
					}
				}
			}`))
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Imported) != 0 {
				t.Fatalf("Imported = %v, want none", report.Imported)
			}
			reason, refused := report.Unsupported["vendor"]
			if !refused {
				t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
			}
			if reason != mcpToolDefinitionRefusedMessage {
				t.Fatalf("reason = %q, want the fixed generic reason %q", reason, mcpToolDefinitionRefusedMessage)
			}
			if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
				t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
			}
		})
	}
}

// An unknown key inside a tool object (neither a canonical name nor a
// case variant of one) must be refused the same generic way, and must
// never adopt a preexisting legacy placeholder row: no partial
// insert/adoption from a malformed tool definition.
func TestImportJSONUnknownToolKeyNeverAdoptsPlaceholder(t *testing.T) {
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
				"tools": [{"name": "vendor.lookup", "inputSchema": {"type": "object"}, "unexpected_key": true}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 || len(report.Skipped) != 0 {
		t.Fatalf("Imported = %v, Skipped = %v, want neither: the placeholder must not be touched", report.Imported, report.Skipped)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if reason != mcpToolDefinitionRefusedMessage {
		t.Fatalf("reason = %q, want the fixed generic reason %q", reason, mcpToolDefinitionRefusedMessage)
	}

	after, err := repo.GetMCPServer(ctx, placeholder.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.URL != "" {
		t.Fatalf("URL = %q, want the placeholder's empty url left untouched", after.URL)
	}
	tools, err := repo.ListMCPServerTools(ctx, placeholder.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || !tools[0].Present {
		t.Fatalf("tools = %+v, want the placeholder's carried tool left present, not withdrawn", tools)
	}
}

// A tool's own configured bearer token must never leak through a
// case-variant or duplicate-key refusal's reason: the fixed, generic
// mcpToolDefinitionRefusedMessage never repeats any part of the input,
// so this is really a regression guard that the *new* strict-field check
// keeps using that same fixed string, never something built from the
// entry's own content.
func TestImportJSONToolFieldConflictRefusalNeverLeaksToken(t *testing.T) {
	const token = "vendor-secret-tool-field-conflict"
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer `+token+`"},
				"tools": [{"Name": "vendor.lookup", "inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	assertStringSentinelFree(t, "ImportReport reason", reason, token)
	if reason != mcpToolDefinitionRefusedMessage {
		t.Fatalf("reason = %q, want the fixed generic reason %q", reason, mcpToolDefinitionRefusedMessage)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
	}
}

// Every canonical tool field, spelled exactly (including "inputSchema"'s
// camelCase spelling), plus the optional "description" field, must still
// import normally: the new exact-key requirement must not be off-by-one
// against a legitimate, well-formed tool definition.
func TestImportJSONExactCanonicalToolFieldsStillImport(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "vendor.lookup", "description": "looks things up", "inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", report.Imported)
	}
	server, err := repo.GetMCPServerByName(ctx, "vendor")
	if err != nil {
		t.Fatal(err)
	}
	tools, err := repo.ListMCPServerTools(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "vendor.lookup" || !tools[0].Present {
		t.Fatalf("tools = %+v, want exactly one present tool named vendor.lookup", tools)
	}
}

// A tool object with no "name" key at all is refused by buildImportTools'
// own existing empty-name check — unaffected by the new strict-field
// decode, which only governs which *keys* are permitted, not whether
// "name" itself is required to be non-empty (that remains
// buildImportTools' own job).
func TestImportJSONToolWithNoNameKeyStillRefused(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{"inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none", report.Imported)
	}
	if _, refused := report.Unsupported["vendor"]; !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused for a missing tool name", report.Unsupported)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
	}
}

// A "tools" value that is not a JSON array at all (an object, in this
// case) must be refused with the fixed generic reason, not a raw
// encoding/json type-mismatch message.
func TestImportJSONNonArrayToolsValueRefused(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": {"name": "vendor.lookup"}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none", report.Imported)
	}
	if _, refused := report.Unsupported["vendor"]; !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused for a non-array tools value", report.Unsupported)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
	}
}

// An explicit "tools": null must be treated exactly like an absent
// "tools" key — no tools, no refusal — matching what an absent or
// JSON-null []mcpJSONTool field decoded to previously.
func TestImportJSONNullToolsValueImportsWithNoTools(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": null
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", report.Imported)
	}
	server, err := repo.GetMCPServerByName(ctx, "vendor")
	if err != nil {
		t.Fatal(err)
	}
	tools, err := repo.ListMCPServerTools(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools = %+v, want none for an explicit null tools value", tools)
	}
}

// An explicit "tools": [] (an empty array, not absent and not null) must
// import successfully with zero tools — the boundary case between "no
// tools key at all"/"null" (also no tools) and a populated array.
func TestImportJSONEmptyToolsArrayImportsWithNoTools(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": []
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", report.Imported)
	}
	server, err := repo.GetMCPServerByName(ctx, "vendor")
	if err != nil {
		t.Fatal(err)
	}
	tools, err := repo.ListMCPServerTools(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools = %+v, want none for an explicit empty tools array", tools)
	}
}

// A "tools" array element that is not a JSON object at all — a bare
// string, in this case — must be refused with the fixed generic reason:
// decodeMCPToolFields requires an opening '{' token and refuses anything
// else, rather than panicking or silently skipping the malformed element.
func TestImportJSONNonObjectToolArrayElementRefused(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": ["not-an-object"]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none", report.Imported)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused for a non-object tools element", report.Unsupported)
	}
	if reason != mcpToolDefinitionRefusedMessage {
		t.Fatalf("reason = %q, want the fixed generic reason %q", reason, mcpToolDefinitionRefusedMessage)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
	}
}
