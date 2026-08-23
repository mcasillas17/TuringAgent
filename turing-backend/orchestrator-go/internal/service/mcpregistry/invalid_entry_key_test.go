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

// mcpInvalidEntryKeyBearerSentinel is chosen to be pattern-invalid (the
// embedded space fails mcpServerNamePattern, which allows only
// "[A-Za-z0-9._-]" after a leading alphanumeric) while still looking
// exactly like a secret value an operator could have pasted into the
// wrong mcp.json slot — used here as both the entry's own raw key *and*
// the value of that same entry's Authorization bearer header, so this one
// sentinel stands in for whatever a real leak would actually expose.
const mcpInvalidEntryKeyBearerSentinel = "mcp-registry-invalid-key-bearer-sentinel-1a9c47e0 do-not-leak"

// TestImportInvalidEntryKeyEqualToBearerSentinelNeverLeaks is the sentinel
// proof for the invalid-entry-key finding: an mcp.json entry whose own
// key/name fails validateMCPServerName (here, because it contains a
// space) is refused *before* this package ever gets to parse that same
// entry's "headers"/bearer token (see ImportJSON's own ordering) — but,
// before this fix, the refusal was still recorded under that raw,
// unvalidated key, so a key that happened to equal a real secret (as this
// one does, mirroring its own Authorization header) leaked into the
// in-memory report, mcp_import_issues, the ReimportMcpJson RPC response,
// ListMcpServers' own Unsupported list, and (via those two RPC responses)
// the Flutter UI — exactly the class of leak
// TestImportUnsupportedHeaderKeyEqualToBearerTokenNeverLeaks and
// TestImportNestedUnknownToolKeyNeverLeaksAcrossAllSurfaces already close
// for a header key and a nested tool key respectively. This sweeps every
// one of those same surfaces, plus the audit log, the events table, and
// the process log, the same way those two tests do.
func TestImportInvalidEntryKeyEqualToBearerSentinelNeverLeaks(t *testing.T) {
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
			"` + mcpInvalidEntryKeyBearerSentinel + `": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer ` + mcpInvalidEntryKeyBearerSentinel + `"}
			},
			"vendor-good": {
				"url": "https://vendor-good.example/mcp"
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

	// RPC response: the well-formed sibling entry still imports...
	if len(response.GetImported()) != 1 || response.GetImported()[0] != "vendor-good" {
		t.Fatalf("Imported = %v, want [vendor-good]", response.GetImported())
	}
	// ...and the invalid-keyed one is refused under a bounded, synthetic
	// label — never its own raw key.
	if len(response.GetUnsupported()) != 1 {
		t.Fatalf("Refused = %+v, want exactly one refused entry", response.GetUnsupported())
	}
	refused := response.GetUnsupported()[0]
	if refused.GetName() != "_invalid_server_1" {
		t.Fatalf("Refused[0].Name = %q, want the synthetic label _invalid_server_1", refused.GetName())
	}
	if refused.GetReason() != invalidMCPEntryNameMessage {
		t.Fatalf("Refused[0].Reason = %q, want the fixed reason %q", refused.GetReason(), invalidMCPEntryNameMessage)
	}
	assertStringSentinelFree(t, "RPC response", refused.GetName()+" "+refused.GetReason(), mcpInvalidEntryKeyBearerSentinel)

	// DB: mcp_import_issues, written by ReimportConfiguredJSON/ImportJSON.
	issues, err := repo.ListMCPImportIssues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want exactly one", issues)
	}
	if issues[0].Name != "_invalid_server_1" {
		t.Fatalf("persisted issue name = %q, want the synthetic label _invalid_server_1", issues[0].Name)
	}
	assertStringSentinelFree(t, "mcp_import_issues row", issues[0].Name+" "+issues[0].Reason, mcpInvalidEntryKeyBearerSentinel)

	// ListMcpServers surfaces the same persisted issues table.
	listed, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, unsupported := range listed.GetUnsupported() {
		if unsupported.GetName() != "_invalid_server_1" {
			continue
		}
		found = true
		if unsupported.GetReason() != invalidMCPEntryNameMessage {
			t.Fatalf("ListMcpServers Unsupported reason = %q, want the fixed reason", unsupported.GetReason())
		}
	}
	if !found {
		t.Fatal("ListMcpServers did not report _invalid_server_1 as unsupported")
	}
	assertStringSentinelFree(t, "ListMcpServers response", listed.String(), mcpInvalidEntryKeyBearerSentinel)

	// No server row: a refused entry must not register, under any name.
	if _, err := repo.GetMCPServerByName(ctx, mcpInvalidEntryKeyBearerSentinel); err == nil {
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
		assertStringSentinelFree(t, "audit payload", payload, mcpInvalidEntryKeyBearerSentinel)
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
	assertStringSentinelFree(t, "process log", logged.String(), mcpInvalidEntryKeyBearerSentinel)
}

// TestImportJSONDirectInvalidEntryKeySentinelSweep confirms the same
// sentinel-sweep guarantee TestImportInvalidEntryKeyEqualToBearerSentinelNeverLeaks
// proves through the reimport-from-file path also holds for ImportJSON
// called directly (never through ReimportMcpJson/mcp.json at all), and
// across the in-memory report specifically.
func TestImportJSONDirectInvalidEntryKeySentinelSweep(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	document := []byte(`{
		"mcpServers": {
			"` + mcpInvalidEntryKeyBearerSentinel + `": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer ` + mcpInvalidEntryKeyBearerSentinel + `"}
			}
		}
	}`)
	report, err := service.ImportJSON(ctx, document)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unsupported) != 1 {
		t.Fatalf("Unsupported = %+v, want exactly one entry", report.Unsupported)
	}
	reason, refused := report.Unsupported["_invalid_server_1"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want _invalid_server_1 present", report.Unsupported)
	}
	if reason != invalidMCPEntryNameMessage {
		t.Fatalf("reason = %q, want the fixed reason %q", reason, invalidMCPEntryNameMessage)
	}
	for key, value := range report.Unsupported {
		assertStringSentinelFree(t, "ImportJSON report", key+" "+value, mcpInvalidEntryKeyBearerSentinel)
	}
	if _, err := repo.GetMCPServerByName(ctx, mcpInvalidEntryKeyBearerSentinel); err == nil {
		t.Fatal("a refused entry must not create a server row")
	}
}
