package mcpregistry

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
)

// A static tools snapshot is fully validated — name, schema, bundled
// namespace collision, and the entry's own configured bearer token never
// appearing verbatim in a tool's name or schema — before ImportJSON
// touches the repository at all. This is the one test to keep if every
// other fail-closed-import test here were deleted: a long, unique sentinel
// embedded in a tool's name must refuse the whole entry with a generic
// reason, and must never appear anywhere: the response, the persisted
// import issue, the audit trail, the process log, session events, or any
// plaintext database column (only the sealed_token ciphertext column is
// exempt, and no row exists here to hold one anyway).
func TestImportJSONTokenSentinelInToolNameRefusesEntryFailClosedAndSentinelFree(t *testing.T) {
	const sentinel = "mcp-import-atomic-sentinel-7f2b9e4c1a6d8035-do-not-leak"
	database, service, repo := newSentinelSweepableRegistryService(t)

	var logged strings.Builder
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	report, err := service.ImportJSON(context.Background(), []byte(fmt.Sprintf(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer %s"},
				"tools": [{"name": "vendor.%s", "inputSchema": {"type": "object"}}]
			}
		}
	}`, sentinel, sentinel)))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: a tool name carrying the configured token must refuse the whole entry", report.Imported)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if strings.Contains(strings.ToLower(reason), "token") || strings.Contains(strings.ToLower(reason), "metadata") {
		t.Fatalf("reason = %q, must be generic: it must not name why (token/metadata) the entry was refused", reason)
	}
	assertStringSentinelFree(t, "unsupported reason", reason, sentinel)

	if _, err := repo.GetMCPServerByName(context.Background(), "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: no server row may remain after a fail-closed refusal", err)
	}

	issues, err := repo.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range issues {
		assertStringSentinelFree(t, "persisted import issue "+issue.Name, issue.Reason, sentinel)
	}

	assertStringSentinelFree(t, "process log", logged.String(), sentinel)
	assertDatabaseSentinelFreeExceptSealedToken(t, database, sentinel)
}

// The same fail-closed refusal, but the sentinel appears in the tool's
// serialized inputSchema instead of its name.
func TestImportJSONTokenSentinelInToolSchemaRefusesEntryFailClosedAndSentinelFree(t *testing.T) {
	const sentinel = "mcp-import-atomic-sentinel-3c8a1f6e9b2d7714-do-not-leak"
	database, service, repo := newSentinelSweepableRegistryService(t)

	report, err := service.ImportJSON(context.Background(), []byte(fmt.Sprintf(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer %s"},
				"tools": [{
					"name": "vendor.lookup",
					"inputSchema": {"type": "object", "description": "%s"}
				}]
			}
		}
	}`, sentinel, sentinel)))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: a schema carrying the configured token must refuse the whole entry", report.Imported)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if strings.Contains(strings.ToLower(reason), "token") || strings.Contains(strings.ToLower(reason), "metadata") {
		t.Fatalf("reason = %q, must be generic: it must not name why (token/metadata) the entry was refused", reason)
	}
	assertStringSentinelFree(t, "unsupported reason", reason, sentinel)

	if _, err := repo.GetMCPServerByName(context.Background(), "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: no server row may remain after a fail-closed refusal", err)
	}
	assertDatabaseSentinelFreeExceptSealedToken(t, database, sentinel)
}

// The token-sentinel check must win regardless of what ELSE is wrong with
// the tool: a tool whose name carries the sentinel AND whose inputSchema
// is independently invalid (a non-object root type) must still refuse
// with the one generic, sentinel-free reason — never the schema-validation
// message, which embeds the tool's (sentinel-bearing) name verbatim via
// %q. This is the discriminating case a valid-schema-only sentinel test
// cannot catch: it proves the token check is not shadowed by an earlier
// validation failure that would otherwise print the name into the reason.
func TestImportJSONTokenSentinelInNameWithInvalidSchemaStillRefusesGenericallyAndSentinelFree(t *testing.T) {
	const sentinel = "mcp-import-atomic-sentinel-9e4b7d2a6f1c8035-do-not-leak"
	database, service, repo := newSentinelSweepableRegistryService(t)

	report, err := service.ImportJSON(context.Background(), []byte(fmt.Sprintf(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer %s"},
				"tools": [{"name": "vendor.%s", "inputSchema": {"type": "array"}}]
			}
		}
	}`, sentinel, sentinel)))
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
		t.Fatalf("reason = %q, want the fixed generic %q: an independently invalid schema must not let the "+
			"schema-validation message (which embeds the sentinel-bearing tool name) win over the token check", reason, mcpToolDefinitionRefusedMessage)
	}
	assertStringSentinelFree(t, "unsupported reason", reason, sentinel)
	if _, err := repo.GetMCPServerByName(context.Background(), "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: no server row may remain after a fail-closed refusal", err)
	}
	assertDatabaseSentinelFreeExceptSealedToken(t, database, sentinel)
}

// A static snapshot naming a tool in a bundled server's namespace (e.g.
// "files.") must refuse the whole entry before any mutation, leaving no
// row behind, and a corrected reimport (dropping the offending tool) must
// then succeed rather than being stuck skipping a poisoned partial row.
func TestImportJSONStaticSnapshotBundledNamespaceCollisionCreatesNoRowAndCorrectedReimportWorks(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "files.delete", "inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: a bundled-namespace tool collision must refuse the whole entry", report.Imported)
	}
	if _, refused := report.Unsupported["vendor"]; !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: no row may remain after the refusal", err)
	}

	corrected, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "vendor.safe_tool", "inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(corrected.Imported) != 1 || corrected.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor] once the offending tool is removed", corrected.Imported)
	}
}

// A static snapshot naming a tool another already-registered server
// presently owns must also refuse the whole entry before any mutation,
// leaving no row behind, and a corrected reimport must then succeed.
func TestImportJSONStaticSnapshotInterServerCollisionCreatesNoRowAndCorrectedReimportWorks(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	owner, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "owner", URL: "https://owner.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, owner.Server.ID, []repository.MCPServerTool{
		{Name: "shared.tool", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "shared.tool", "inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: an inter-server tool collision must refuse the whole entry", report.Imported)
	}
	if reason, refused := report.Unsupported["vendor"]; !refused || reason == "" {
		t.Fatalf("Unsupported = %+v, want vendor refused with a non-empty reason", report.Unsupported)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: no row may remain after the refusal", err)
	}

	corrected, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "vendor.safe_tool", "inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(corrected.Imported) != 1 || corrected.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor] once the colliding tool is removed", corrected.Imported)
	}
}

// A brand-new entry whose static tool has an invalid name must be Refused
// only — never both Imported and Refused, which is what happens if the
// server row is marked imported before its tools snapshot is validated.
func TestImportJSONNewEntryWithInvalidToolIsRefusedOnlyNotAlsoImported(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "", "inputSchema": {"type": "object"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: an invalid tool must refuse the whole entry, not also mark it imported", report.Imported)
	}
	for _, name := range report.Imported {
		if _, alsoRefused := report.Unsupported[name]; alsoRefused {
			t.Fatalf("%q appears in both Imported and Unsupported", name)
		}
	}
	if _, refused := report.Unsupported["vendor"]; !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: an invalid tool must leave no row behind", err)
	}
}

// The same double-status bug, but for an invalid inputSchema root type
// rather than an empty name, and asserting a corrected reimport succeeds
// afterward.
func TestImportJSONNewEntryWithInvalidSchemaIsRefusedOnlyAndCorrectedReimportWorks(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "vendor.lookup", "inputSchema": {"type": "array"}}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none: an invalid schema must refuse the whole entry, not also mark it imported", report.Imported)
	}
	if _, refused := report.Unsupported["vendor"]; !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: an invalid schema must leave no row behind", err)
	}

	corrected, err := service.ImportJSON(ctx, []byte(`{
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
	if len(corrected.Imported) != 1 || corrected.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor] once the schema is corrected", corrected.Imported)
	}
}

// newSentinelSweepableRegistryService is newRegistryTestService's
// counterpart for tests that need to sweep the database and the audit
// trail for a sentinel: it wires a real audit.Server (not a test double)
// so the sweep can inspect what actually got persisted.
func newSentinelSweepableRegistryService(t *testing.T) (*db.DB, *Server, *repository.Repository) {
	t.Helper()
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
	service := New(repo, sealer, nil)
	service.SetAuditRecorder(audit.New(repo))
	return database, service, repo
}

// assertDatabaseSentinelFreeExceptSealedToken sweeps every text-typed
// column of every user table for sentinel, skipping only BLOB-typed
// columns (sealed_token, credential_ciphertext): a sealed ciphertext is
// expected to differ from its plaintext input, so it is deliberately
// excluded from a plaintext sweep rather than asserted against, not
// silently ignored for any other reason.
func assertDatabaseSentinelFreeExceptSealedToken(t *testing.T, database *db.DB, sentinel string) {
	t.Helper()
	ctx := context.Background()
	tableRows, err := database.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for tableRows.Next() {
		var name string
		if err := tableRows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := tableRows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = tableRows.Close()

	for _, table := range tables {
		colRows, err := database.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
		if err != nil {
			t.Fatal(err)
		}
		var textColumns []string
		for colRows.Next() {
			var cid, notnull, pk int
			var name, ctype string
			var dflt any
			if err := colRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				t.Fatal(err)
			}
			if !strings.EqualFold(ctype, "BLOB") {
				textColumns = append(textColumns, name)
			}
		}
		if err := colRows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = colRows.Close()
		if len(textColumns) == 0 {
			continue
		}
		quoted := make([]string, len(textColumns))
		for i, name := range textColumns {
			quoted[i] = fmt.Sprintf("%q", name)
		}
		rows, err := database.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %q", strings.Join(quoted, ", "), table))
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(textColumns))
			dest := make([]any, len(textColumns))
			for i := range dest {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			for i, raw := range values {
				var value string
				switch typed := raw.(type) {
				case nil:
					continue
				case string:
					value = typed
				case []byte:
					value = string(typed)
				default:
					value = fmt.Sprint(typed)
				}
				if strings.Contains(value, sentinel) {
					t.Fatalf("table %s column %s contains the sentinel: %q", table, textColumns[i], value)
				}
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
	}
}
