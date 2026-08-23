package mcpregistry

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"google.golang.org/protobuf/encoding/protojson"
)

// A sentinel improbable enough that finding it anywhere means it leaked from
// this test's own registration/rotation calls.
const mcpTokenSentinel = "mcp-registry-sentinel-3f9a6c2e8b1d5074-do-not-leak"

// The one test to keep if every other one in this package were deleted: a
// bearer token given to RegisterMcpServer and then to RotateMcpServerToken
// must never reach a response, an audit row, an event, a returned error, or
// the log — only the sealed column may hold it.
func TestBearerTokenNeverLeaksAcrossRegisterAndRotate(t *testing.T) {
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

	var logged bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logged)
	// Restored via Cleanup, which runs after every assertion in this test
	// (including the process-log check near the end) rather than being
	// swapped back early, so nothing logged by Register/Rotate escapes
	// the captured buffer before it is inspected.
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })

	registered, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name:        "vendor",
		Url:         "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: mcpTokenSentinel,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	assertNoSentinel(t, "register response", registered)

	rotatedSentinel := mcpTokenSentinel + "-rotated"
	rotated, err := service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId:    registered.GetServerId(),
		BearerToken: rotatedSentinel,
	})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	assertNoSentinel(t, "rotate response", rotated)

	// A validation failure is the other realistic way a token-bearing
	// request could leak it: the returned error string must stay
	// sentinel-free too, not just the happy-path responses above.
	invalidTokenSentinel := mcpTokenSentinel + "\ncontains-a-line-break-so-validation-fails"
	if _, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name:        "vendor-invalid",
		Url:         "https://vendor-invalid.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: invalidTokenSentinel,
	}); err == nil {
		t.Fatal("register with a bearer token containing a line break must fail validation")
	} else {
		assertStringSentinelFree(t, "register validation error", err.Error(), mcpTokenSentinel, rotatedSentinel)
	}
	if _, err := service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId:    registered.GetServerId(),
		BearerToken: invalidTokenSentinel,
	}); err == nil {
		t.Fatal("rotate with a bearer token containing a line break must fail validation")
	} else {
		assertStringSentinelFree(t, "rotate validation error", err.Error(), mcpTokenSentinel, rotatedSentinel)
	}

	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount == 0 {
		t.Fatal("no audit rows were written, so this check proves nothing about them")
	}

	rows, err := database.QueryContext(ctx, `SELECT payload_json FROM audit_logs`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		assertStringSentinelFree(t, "audit payload", payload, mcpTokenSentinel, rotatedSentinel)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	// MCP registry management is audited (checked above), not emitted as a
	// session event — wiring management calls into session events is
	// explicitly Task 4's job, not this one. Asserting the count is zero
	// here (rather than conditionally inspecting rows only if any exist)
	// is itself the useful assertion: it fails the moment something
	// starts writing to `events` from this package without a matching
	// test deciding that on purpose.
	var eventCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("event count = %d, want 0: MCP management must not emit session events", eventCount)
	}

	// The token must never reach the process log either, across both the
	// successful register/rotate calls and the validation failures above.
	assertStringSentinelFree(t, "process log", logged.String(), mcpTokenSentinel, rotatedSentinel)
}

// mcpHeaderKeySentinel is a second, distinct sentinel (from mcpTokenSentinel)
// used as an mcp.json entry's *bearer token value* in
// TestImportUnsupportedHeaderKeyEqualToBearerTokenNeverLeaks, so that test
// can also use its own header-name-echo-shaped sentinel without the two
// tests' assertions ever being confused with one another.
const mcpHeaderKeySentinel = "mcp-registry-header-key-sentinel-9c4e71a0-do-not-leak"

// TestImportUnsupportedHeaderKeyEqualToBearerTokenNeverLeaks is the
// sentinel proof for the "unsupported header name" finding: an mcp.json
// entry's headers object can carry a second, unsupported header whose own
// *key* happens to equal the entry's configured bearer token (whether by
// an operator's templating mistake or an attacker deliberately shaping the
// document that way, hoping an error message will echo an unsupported
// header's name back verbatim). Before this fix, bearerFromHeaders' error
// text named exactly that unsupported header's key, so the secret would
// leak into every place an ordinary Unsupported reason already reaches:
// the in-memory ImportReport, mcp_import_issues, the ReimportMcpJson RPC
// response, and (via auditMCPEvent, which only ever records *counts* for a
// reimport — never names or reasons) would have been safe regardless, but
// this sweeps it anyway, alongside the process log and the session events
// table, the same way TestBearerTokenNeverLeaksAcrossRegisterAndRotate
// above sweeps the register/rotate paths.
func TestImportUnsupportedHeaderKeyEqualToBearerTokenNeverLeaks(t *testing.T) {
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
	document := []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {
					"Authorization": "Bearer ` + mcpHeaderKeySentinel + `",
					"` + mcpHeaderKeySentinel + `": "irrelevant-value"
				}
			}
		}
	}`)
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), document, 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	var logged bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })

	response, err := service.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatal(err)
	}

	// refusal / RPC response.
	if len(response.GetRefused()) != 1 {
		t.Fatalf("Refused = %+v, want exactly one refused entry", response.GetRefused())
	}
	refused := response.GetRefused()[0]
	if refused.GetName() != "vendor" {
		t.Fatalf("Refused[0].Name = %q, want vendor", refused.GetName())
	}
	if refused.GetReason() != errMCPUnsupportedHeader.Error() {
		t.Fatalf("Refused[0].Reason = %q, want the fixed errMCPUnsupportedHeader reason", refused.GetReason())
	}
	assertStringSentinelFree(t, "RPC response", refused.GetReason(), mcpHeaderKeySentinel)
	if len(response.GetImported()) != 0 {
		t.Fatalf("Imported = %v, want none: the refused entry must not register", response.GetImported())
	}

	// DB: mcp_import_issues, written by ReimportConfiguredJSON/ImportJSON.
	issues, err := repo.ListMCPImportIssues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want exactly one", issues)
	}
	assertStringSentinelFree(t, "mcp_import_issues row", issues[0].Name+" "+issues[0].Reason, mcpHeaderKeySentinel)

	// No server row: a refused entry must not register.
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err == nil {
		t.Fatal("a refused entry must not create a server row")
	}

	// audit: a reimport only ever records counts (see ReimportMcpJson),
	// never names or reasons, but this proves that invariant rather than
	// assuming it.
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
		assertStringSentinelFree(t, "audit payload", payload, mcpHeaderKeySentinel)
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
	assertStringSentinelFree(t, "process log", logged.String(), mcpHeaderKeySentinel)
}

func assertNoSentinel(t *testing.T, what string, descriptor *turingv1.McpServerDescriptor) {
	t.Helper()
	encoded, err := protojson.Marshal(descriptor)
	if err != nil {
		t.Fatalf("marshal %s: %v", what, err)
	}
	assertStringSentinelFree(t, what, string(encoded), mcpTokenSentinel, mcpTokenSentinel+"-rotated")
}

func assertStringSentinelFree(t *testing.T, what string, haystack string, sentinels ...string) {
	t.Helper()
	for _, sentinel := range sentinels {
		if strings.Contains(haystack, sentinel) {
			t.Fatalf("%s carries the sentinel token: %s", what, haystack)
		}
		// A long substantial substring is as bad as the whole thing.
		if len(sentinel) > 16 && strings.Contains(haystack, sentinel[:16]) {
			t.Fatalf("%s carries a substantial substring of the sentinel token: %s", what, haystack)
		}
	}
}
