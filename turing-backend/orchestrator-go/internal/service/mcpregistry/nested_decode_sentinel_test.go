package mcpregistry

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
)

// mcpNestedToolKeySentinel is a JSON *key* name (not a value) nested
// inside an mcp.json entry's "tools" array — the one place
// decodeMCPToolFields/validateMCPToolFields (import.go) can refuse a key
// that is nested, rather than top-level: a tool object only recognizes
// "name", "description", and "inputSchema" (see
// canonicalMCPToolFieldNames), so any other key inside one of its array
// elements is refused even though the entry's own top-level fields (only
// "url" and "tools" here) are exactly canonical and pass
// validateMCPEntryFields cleanly. An attacker who controls an mcp.json
// entry controls every key in it exactly as much as every value, so this
// key is chosen to look exactly like a secret an error message must never
// echo back.
const mcpNestedToolKeySentinel = "mcp-registry-nested-tool-key-sentinel-6f1e8a3d-do-not-leak"

// TestImportNestedUnknownToolKeyNeverLeaksAcrossAllSurfaces is the
// sentinel proof for the generic-entry-decode-error finding: an mcp.json
// entry whose top-level shape is perfectly canonical can still be refused
// on a *nested* unknown key inside a "tools" array element, now decoded
// via decodeMCPToolEntries — captured as raw json.RawMessage rather than
// structurally decoded by entryDecoder.Decode's own DisallowUnknownFields,
// specifically so a case-variant or duplicate key inside a tool object is
// caught too, not just a wholly unrecognized one (see
// strict_tool_fields_test.go). Before any of this, that failure was
// reported as "entry is invalid: " + err.Error(), and encoding/json's own
// "unknown field %q" wording names the offending key verbatim — so an
// attacker-chosen (or accidentally sensitive) nested key would flow into
// every place an ordinary Unsupported reason already reaches: the
// in-memory ImportReport, mcp_import_issues, the ReimportMcpJson RPC
// response, ListMcpServers' own Unsupported list, the audit log, the
// events table, and the process log. This sweeps every one of those,
// mirroring TestImportUnsupportedHeaderKeyEqualToBearerTokenNeverLeaks and
// TestBearerTokenNeverLeaksAcrossRegisterAndRotate in
// security_sentinel_test.go. A second, valid entry in the same document
// proves the fix does not collapse unrelated entries into the same
// refusal or otherwise disturb them.
func TestImportNestedUnknownToolKeyNeverLeaksAcrossAllSurfaces(t *testing.T) {
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
	auditService := audit.New(repo)
	service.SetAuditRecorder(auditService)
	ctx := context.Background()

	root := t.TempDir()
	document := `{
		"mcpServers": {
			"vendor-good": {
				"url": "https://vendor-good.example/mcp"
			},
			"vendor-bad": {
				"url": "https://vendor-bad.example/mcp",
				"tools": [
					{
						"name": "vendor-bad.lookup",
						"inputSchema": {"type": "object"},
						"` + mcpNestedToolKeySentinel + `": "irrelevant-value"
					}
				]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	var logged bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })

	response, err := service.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatal(err)
	}

	// RPC response: the well-formed sibling entry must still import...
	if len(response.GetImported()) != 1 || response.GetImported()[0] != "vendor-good" {
		t.Fatalf("Imported = %v, want [vendor-good]", response.GetImported())
	}
	// ...and the malformed one must be refused with a fixed, generic,
	// sentinel-free reason rather than collapsing the whole document.
	if len(response.GetUnsupported()) != 1 {
		t.Fatalf("Refused = %+v, want exactly one refused entry", response.GetUnsupported())
	}
	refused := response.GetUnsupported()[0]
	if refused.GetName() != "vendor-bad" {
		t.Fatalf("Refused[0].Name = %q, want vendor-bad", refused.GetName())
	}
	if refused.GetReason() != mcpToolDefinitionRefusedMessage {
		t.Fatalf("Refused[0].Reason = %q, want the fixed generic tool-definition-refused reason %q",
			refused.GetReason(), mcpToolDefinitionRefusedMessage)
	}
	assertStringSentinelFree(t, "RPC response", refused.GetReason(), mcpNestedToolKeySentinel)

	// DB: mcp_import_issues.
	issues, err := repo.ListMCPImportIssues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want exactly one", issues)
	}
	assertStringSentinelFree(t, "mcp_import_issues row", issues[0].Name+" "+issues[0].Reason, mcpNestedToolKeySentinel)

	// ListMcpServers surfaces the same persisted issues table.
	listed, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, unsupported := range listed.GetUnsupported() {
		if unsupported.GetName() != "vendor-bad" {
			continue
		}
		found = true
		if unsupported.GetReason() != mcpToolDefinitionRefusedMessage {
			t.Fatalf("ListMcpServers Unsupported reason = %q, want the fixed reason", unsupported.GetReason())
		}
	}
	if !found {
		t.Fatal("ListMcpServers did not report vendor-bad as unsupported")
	}
	assertStringSentinelFree(t, "ListMcpServers response", listed.String(), mcpNestedToolKeySentinel)

	// No server row: a refused entry must not register.
	if _, err := repo.GetMCPServerByName(ctx, "vendor-bad"); err == nil {
		t.Fatal("a refused entry must not create a server row")
	}

	// audit.
	rows, err := database.QueryContext(ctx, `SELECT payload_json FROM audit_logs`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	auditRowCount := 0
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		auditRowCount++
		assertStringSentinelFree(t, "audit payload", payload, mcpNestedToolKeySentinel)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if auditRowCount == 0 {
		t.Fatal("no audit rows were written, so this check proves nothing about them")
	}

	// events: MCP registry management must not emit session events.
	var eventCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("event count = %d, want 0: MCP management must not emit session events", eventCount)
	}

	// process log.
	assertStringSentinelFree(t, "process log", logged.String(), mcpNestedToolKeySentinel)
}

// TestImportTopLevelTypeMismatchUsesFixedGenericReason proves the same
// fixed, generic reason is used for every other shape of decode failure
// entryDecoder.Decode can hit — not just an unknown nested key — such as a
// field whose JSON type does not match its Go struct field (here, "args"
// given as a string instead of an array of strings). This must never
// differ from the unknown-nested-key case: both are decode failures at
// the exact same call site, and neither should be distinguishable to an
// entry's own author from the response alone.
func TestImportTopLevelTypeMismatchUsesFixedGenericReason(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"args": "not-an-array"
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused for a type-mismatched field", report.Unsupported)
	}
	if reason != errMCPEntryFieldInvalid.Error() {
		t.Fatalf("reason = %q, want the fixed generic decode-failure reason %q", reason, errMCPEntryFieldInvalid.Error())
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
	}
}
