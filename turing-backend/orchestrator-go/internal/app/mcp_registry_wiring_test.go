package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// mcpWiringIntegrationKey is a fixed, valid hex key (secretbox.KeySize bytes)
// so tests exercising RegisterMcpServer/RotateMcpServerToken with a bearer
// token have a sealer configured, exactly like an operator install with
// TURING_INTEGRATION_KEY set.
var mcpWiringIntegrationKey = strings.Repeat("cd", 32)

// newMCPWiringTestApp is newTestApp's counterpart for this file's tests: it
// additionally configures MCPConfigRoot (so ReimportConfiguredJSON has
// somewhere to read from) and an integration key (so a bearer token given to
// RegisterMcpServer/RotateMcpServerToken has a sealer to seal it with), the
// same two pieces of configuration app.New is responsible for wiring into
// the real mcpregistrysvc.Server this package tests against.
func newMCPWiringTestApp(t *testing.T, root string) *App {
	t.Helper()
	application, err := New(config.Config{
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret: "approval-secret",
		DatabasePath:      t.TempDir() + "/turing.db",
		OllamaModel:       "llama3.2",
		MCPConfigRoot:     root,
		IntegrationKey:    mcpWiringIntegrationKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(application.Stop)
	return application
}

func publicMCPRegistryClient(t *testing.T, app *App) turingv1.McpRegistryServiceClient {
	t.Helper()
	return turingv1.NewMcpRegistryServiceClient(newBufconnClient(t, app.PublicServer))
}

func publicMCPRegistryContext() context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+"client"))
}

func findMCPServerRecord(servers []repository.MCPServerRecord, name string) (repository.MCPServerRecord, bool) {
	for _, server := range servers {
		if server.Name == name {
			return server, true
		}
	}
	return repository.MCPServerRecord{}, false
}

// A malformed or refused entry in mcp.json at startup must still leave an
// operator-visible diagnostic in the process log — but only a bare count,
// never the entry's name, its refusal reason, a header name, or a token,
// any of which a refusal's details can carry. This test fails before
// app.New captured ReimportConfiguredJSON's ImportReport and logged its
// count: with no diagnostic at all, an operator staring at a silent
// startup log has no way to know mcp.json contained anything wrong.
func TestStartupImportRefusalLogsCountOnlyNeverNamesOrReasons(t *testing.T) {
	const headerSentinel = "X-Sentinel-Header-Do-Not-Log-9f21ac"
	const commandSentinel = "sentinel-command-do-not-log-4b7e"
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {
			"sentinel-header-vendor": {"url": "https://vendor.example/mcp", "headers": {"`+headerSentinel+`": "value"}},
			"sentinel-command-vendor": {"command": "`+commandSentinel+`", "args": ["x"]}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(previousLogOutput)

	application, err := New(config.Config{
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret: "approval-secret",
		DatabasePath:      t.TempDir() + "/turing.db",
		OllamaModel:       "llama3.2",
		MCPConfigRoot:     root,
		IntegrationKey:    mcpWiringIntegrationKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(application.Stop)

	if !strings.Contains(logged.String(), "mcp.json import refused 2 entries") {
		t.Fatalf("startup log = %q, want a count-only refusal diagnostic naming exactly 2 entries", logged.String())
	}
	for _, sentinel := range []string{
		headerSentinel, commandSentinel,
		"sentinel-header-vendor", "sentinel-command-vendor",
		"unsupported; only Authorization",
	} {
		if strings.Contains(logged.String(), sentinel) {
			t.Fatalf("startup log leaked %q: %s", sentinel, logged.String())
		}
	}
}

// Writing mcp.json after the app has already started and then reimporting it
// through the real public gRPC server (not by calling into the service
// directly, and not by restarting the app) is exactly the on-demand path
// app.New's ReimportConfiguredJSON wiring exists for.
func TestReimportMcpJsonThroughPublicRPCImportsNewFileWithoutRestart(t *testing.T) {
	root := t.TempDir()
	app := newMCPWiringTestApp(t, root)

	// The config root existed but mcp.json did not: startup must have
	// cleared any stale issues rather than leaving anything behind.
	issues, err := app.Repository.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues at startup = %+v, want none for an absent mcp.json", issues)
	}

	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {"vendor": {"url": "https://vendor.example/mcp"}}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	client := publicMCPRegistryClient(t, app)
	response, err := client.ReimportMcpJson(publicMCPRegistryContext(), &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatalf("ReimportMcpJson: %v", err)
	}
	if got := response.GetImported(); len(got) != 1 || got[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", got)
	}

	servers, err := app.Repository.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor, found := findMCPServerRecord(servers, "vendor")
	if !found {
		t.Fatal("vendor was not persisted by the on-demand reimport")
	}
	if vendor.Enabled {
		t.Fatal("a newly imported server must start disabled")
	}
}

// The report a client sees over the public RPC must be exact and
// deterministic: each of Imported/Skipped/Refused independently sorted, and
// every refusal carrying a non-empty reason.
func TestReimportMcpJsonThroughPublicRPCReportsDeterministicOrderAndReasons(t *testing.T) {
	root := t.TempDir()
	app := newMCPWiringTestApp(t, root)
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {
			"zz-new": {"url": "https://zz-new.example/mcp"},
			"aa-new": {"url": "https://aa-new.example/mcp"},
			"zz-bad": {"command": "npx", "args": ["x"]},
			"aa-bad": {"command": "npx", "args": ["x"]}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	client := publicMCPRegistryClient(t, app)
	response, err := client.ReimportMcpJson(publicMCPRegistryContext(), &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatalf("ReimportMcpJson: %v", err)
	}
	if got := response.GetImported(); len(got) != 2 || got[0] != "aa-new" || got[1] != "zz-new" {
		t.Fatalf("Imported = %v, want sorted [aa-new zz-new]", got)
	}
	refused := response.GetUnsupported()
	if len(refused) != 2 || refused[0].GetName() != "aa-bad" || refused[1].GetName() != "zz-bad" {
		t.Fatalf("Refused names = %v, want sorted [aa-bad zz-bad]", refused)
	}
	for _, entry := range refused {
		if entry.GetReason() == "" {
			t.Fatalf("refused entry %q has no reason", entry.GetName())
		}
	}
	if len(response.GetSkipped()) != 0 {
		t.Fatalf("Skipped = %v, want none on a first import", response.GetSkipped())
	}
}

// A reimport must never clobber operator state: a server the operator
// enabled, a tool policy the operator edited, and a bearer token the
// operator rotated out-of-band (through RotateMcpServerToken, never through
// mcp.json) must all survive an on-demand reimport unchanged, exercised
// through the real public gRPC server end-to-end.
func TestReimportMcpJsonThroughPublicRPCPreservesOperatorState(t *testing.T) {
	root := t.TempDir()
	app := newMCPWiringTestApp(t, root)
	mcpJSON := []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"tools": [{"name": "vendor.lookup", "inputSchema": {"type": "object"}}]
			}
		}
	}`)
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), mcpJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	client := publicMCPRegistryClient(t, app)
	ctx := publicMCPRegistryContext()
	if _, err := client.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{}); err != nil {
		t.Fatalf("initial ReimportMcpJson: %v", err)
	}
	servers, err := app.Repository.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor, found := findMCPServerRecord(servers, "vendor")
	if !found {
		t.Fatal("vendor was not imported")
	}

	// The operator explicitly enables the server (the default from import
	// is disabled, so this is a real, observable operator choice). Set
	// directly through the repository rather than the public
	// SetMcpServerEnabled RPC: enabling a remote-url server through that
	// RPC triggers live discovery — a real HTTP request (DNS lookup,
	// dial, TLS) against the server's own URL (see Server.discoverLocked)
	// — and this test's vendor.example URL is not a server this test
	// hermetically controls. What this test actually exercises is
	// reimport preservation of the operator's enabled choice, not
	// SetMcpServerEnabled's own discovery behavior (already covered
	// elsewhere), so flipping the repository column directly gets the
	// same observable precondition with no network dependency at all.
	if err := app.Repository.SetMCPServerEnabled(context.Background(), vendor.ID, true); err != nil {
		t.Fatalf("SetMCPServerEnabled: %v", err)
	}
	// The operator edits the imported tool's policy.
	if _, err := client.UpdateMcpToolPolicy(ctx, &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: vendor.ID, ToolName: "vendor.lookup", Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	}); err != nil {
		t.Fatalf("UpdateMcpToolPolicy: %v", err)
	}
	// The operator rotates the server's bearer token out-of-band — not
	// through mcp.json, which carries no token for this server at all.
	const rotatedToken = "vendor-rotated-out-of-band-token"
	if _, err := client.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId: vendor.ID, BearerToken: rotatedToken,
	}); err != nil {
		t.Fatalf("RotateMcpServerToken: %v", err)
	}

	response, err := client.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatalf("second ReimportMcpJson: %v", err)
	}
	if got := response.GetSkipped(); len(got) != 1 || got[0] != "vendor" {
		t.Fatalf("Skipped = %v, want [vendor] on reimport of an existing server", got)
	}

	servers, err = app.Repository.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	afterReimport, found := findMCPServerRecord(servers, "vendor")
	if !found {
		t.Fatal("vendor disappeared after reimport")
	}
	if !afterReimport.Enabled {
		t.Fatal("reimport must not reset the operator's enabled choice")
	}
	tools, err := app.Repository.ListMCPServerTools(context.Background(), vendor.ID)
	if err != nil {
		t.Fatal(err)
	}
	var lookupPolicy string
	for _, tool := range tools {
		if tool.Name == "vendor.lookup" {
			lookupPolicy = tool.Policy
		}
	}
	if lookupPolicy != "safe" {
		t.Fatalf("vendor.lookup policy = %q, want the operator's edited %q to survive reimport", lookupPolicy, "safe")
	}

	sealer, err := secretbox.FromHexKey(mcpWiringIntegrationKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterReimport.SealedToken) == 0 {
		t.Fatal("the rotated token must still be sealed on the row after reimport")
	}
	opened, err := sealer.Open(afterReimport.SealedToken, []byte("vendor"))
	if err != nil {
		t.Fatalf("open sealed token after reimport: %v", err)
	}
	if string(opened) != rotatedToken {
		t.Fatalf("sealed token after reimport = %q, want the operator's rotated token preserved", opened)
	}
}

// A server deleted through the public RPC stays deleted across an on-demand
// reimport: the reimport must refuse it (rather than silently re-creating
// it) and the tombstone recording that refusal must itself survive the
// reimport, so a later reimport keeps refusing it too.
func TestDeletedImportedMCPServerStaysRefusedAndTombstonedAcrossReimport(t *testing.T) {
	root := t.TempDir()
	app := newMCPWiringTestApp(t, root)
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {"vendor": {"url": "https://vendor.example/mcp"}}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	client := publicMCPRegistryClient(t, app)
	ctx := publicMCPRegistryContext()
	if _, err := client.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{}); err != nil {
		t.Fatalf("initial ReimportMcpJson: %v", err)
	}
	servers, err := app.Repository.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor, found := findMCPServerRecord(servers, "vendor")
	if !found {
		t.Fatal("vendor was not imported")
	}

	if _, err := client.DeleteMcpServer(ctx, &turingv1.DeleteMcpServerRequest{ServerId: vendor.ID}); err != nil {
		t.Fatalf("DeleteMcpServer: %v", err)
	}
	tombstoned, err := app.Repository.MCPServerTombstoned(context.Background(), "vendor")
	if err != nil {
		t.Fatal(err)
	}
	if !tombstoned {
		t.Fatal("deleting an imported server must tombstone its name")
	}

	response, err := client.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatalf("ReimportMcpJson after delete: %v", err)
	}
	if len(response.GetImported()) != 0 {
		t.Fatalf("Imported = %v, want none: a deleted server must not be re-created", response.GetImported())
	}
	refusedNames := make([]string, 0, len(response.GetUnsupported()))
	for _, entry := range response.GetUnsupported() {
		refusedNames = append(refusedNames, entry.GetName())
	}
	if len(refusedNames) != 1 || refusedNames[0] != "vendor" {
		t.Fatalf("Refused = %v, want [vendor]", refusedNames)
	}

	servers, err = app.Repository.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, found := findMCPServerRecord(servers, "vendor"); found {
		t.Fatal("deleted imported server reappeared after on-demand reimport")
	}
	tombstoned, err = app.Repository.MCPServerTombstoned(context.Background(), "vendor")
	if err != nil {
		t.Fatal(err)
	}
	if !tombstoned {
		t.Fatal("the tombstone must survive an on-demand reimport, not just a restart")
	}
}

// An explicit public RegisterMcpServer of a tombstoned name is the escape
// hatch that clears the tombstone: it creates a fresh, disabled server under
// that name, and a subsequent reimport of that name must no longer be
// refused as suppressed. Registering a bundled name, in contrast, is always
// refused regardless of tombstone state.
func TestRegisterMcpServerClearsTombstoneAndRefusesBundledCollision(t *testing.T) {
	root := t.TempDir()
	app := newMCPWiringTestApp(t, root)
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{
		"mcpServers": {"vendor": {"url": "https://vendor.example/mcp"}}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	client := publicMCPRegistryClient(t, app)
	ctx := publicMCPRegistryContext()
	if _, err := client.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{}); err != nil {
		t.Fatalf("initial ReimportMcpJson: %v", err)
	}
	servers, err := app.Repository.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor, found := findMCPServerRecord(servers, "vendor")
	if !found {
		t.Fatal("vendor was not imported")
	}
	if _, err := client.DeleteMcpServer(ctx, &turingv1.DeleteMcpServerRequest{ServerId: vendor.ID}); err != nil {
		t.Fatalf("DeleteMcpServer: %v", err)
	}

	descriptor, err := client.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})
	if err != nil {
		t.Fatalf("RegisterMcpServer of a tombstoned name: %v", err)
	}
	if descriptor.GetEnabled() {
		t.Fatal("an explicit registration must still create a disabled server")
	}
	tombstoned, err := app.Repository.MCPServerTombstoned(context.Background(), "vendor")
	if err != nil {
		t.Fatal(err)
	}
	if tombstoned {
		t.Fatal("explicit registration must clear the matching tombstone")
	}

	if _, err := client.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "system", Url: "https://impostor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("RegisterMcpServer over a bundled name code = %v, want FailedPrecondition", status.Code(err))
	}
}

// A mobile operator cannot edit backend files, so a migration-0016
// placeholder (a disabled, non-bundled row with url="") left behind for a
// pre-registry runtime's tools is otherwise reachable only through file
// reimport. This exercises the real public gRPC RegisterMcpServer RPC
// (the actual production wiring, not the service directly) adopting that
// exact placeholder in place instead of returning AlreadyExists.
func TestRegisterMcpServerThroughPublicRPCAdoptsLegacyPlaceholder(t *testing.T) {
	app := newMCPWiringTestApp(t, t.TempDir())
	client := publicMCPRegistryClient(t, app)
	ctx := publicMCPRegistryContext()

	placeholder, err := app.Repository.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Repository.ReplaceMCPServerTools(context.Background(), placeholder.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.lookup", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}

	descriptor, err := client.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})
	if err != nil {
		t.Fatalf("RegisterMcpServer must adopt the placeholder rather than error: %v", err)
	}
	if descriptor.GetServerId() != placeholder.Server.ID {
		t.Fatalf("ServerId = %q, want the placeholder %q adopted in place", descriptor.GetServerId(), placeholder.Server.ID)
	}
	if descriptor.GetUrl() != "https://vendor.example/mcp" {
		t.Fatalf("Url = %q, want the registered endpoint populated", descriptor.GetUrl())
	}
	if descriptor.GetEnabled() {
		t.Fatal("adopting a placeholder through the public RPC must still force the server disabled")
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UNKNOWN {
		t.Fatalf("liveness = %v, want unknown after adoption", descriptor.GetLiveness())
	}

	tools, err := app.Repository.ListMCPServerTools(context.Background(), placeholder.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Present || tools[0].Enabled {
		t.Fatalf("tools = %+v, want the carried tool withdrawn (present=0, enabled=0)", tools)
	}

	// A real, already-registered name (non-empty URL) must still be
	// refused as AlreadyExists through this same real wiring.
	if _, err := client.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor-two.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("RegisterMcpServer of a real existing row code = %v, want AlreadyExists", status.Code(err))
	}
}

// This is the one test in this file to keep if every other one were
// deleted: it exercises the real production wiring app.New assembles — the
// actual public gRPC server, the actual mcpregistrysvc.Server, and the
// actual audit service reached through SetAuditRecorder — and proves a
// bearer token given to RegisterMcpServer and then RotateMcpServerToken
// never reaches the RPC response, the audit_logs table, or the process log.
// Before this wiring existed, app.New never called SetAuditRecorder at all,
// so production writes were silently unaudited; this test fails against
// that gap.
func TestPublicRegisterAndRotateAuditThroughRealAppWithoutLeakingTheToken(t *testing.T) {
	const sentinel = "mcp-app-wiring-audit-sentinel-b7e4f21a9c60-do-not-leak"
	app := newMCPWiringTestApp(t, t.TempDir())
	client := publicMCPRegistryClient(t, app)
	ctx := publicMCPRegistryContext()

	var logged bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })

	registered, err := client.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL, BearerToken: sentinel,
	})
	if err != nil {
		t.Fatalf("RegisterMcpServer: %v", err)
	}
	registerJSON, err := protojson.Marshal(registered)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(registerJSON), sentinel) {
		t.Fatalf("RegisterMcpServer response leaked the bearer token: %s", registerJSON)
	}

	rotated, err := client.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId: registered.GetServerId(), BearerToken: sentinel + "-rotated",
	})
	if err != nil {
		t.Fatalf("RotateMcpServerToken: %v", err)
	}
	rotateJSON, err := protojson.Marshal(rotated)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rotateJSON), sentinel) {
		t.Fatalf("RotateMcpServerToken response leaked the bearer token: %s", rotateJSON)
	}

	// A map keyed by action would silently collapse a duplicate row for the
	// same action into a single entry, so the exact row count is checked
	// directly against the database before any of the map-based checks
	// below — which only ever see one payload per action — run.
	var auditedRowCount int
	if err := app.database.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM audit_logs
		WHERE action IN ('mcp.server.registered', 'mcp.server.token_rotated')
	`).Scan(&auditedRowCount); err != nil {
		t.Fatal(err)
	}
	if auditedRowCount != 2 {
		t.Fatalf("audit_logs rows for register/rotate = %d, want exactly 2 (one register, one rotate, with no duplicate hidden behind a repeated action)", auditedRowCount)
	}

	rows, err := app.database.QueryContext(context.Background(), `
		SELECT action, payload_json FROM audit_logs
		WHERE action IN ('mcp.server.registered', 'mcp.server.token_rotated')
		ORDER BY action
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	actions := make(map[string]string)
	for rows.Next() {
		var action, payloadJSON string
		if err := rows.Scan(&action, &payloadJSON); err != nil {
			t.Fatal(err)
		}
		actions[action] = payloadJSON
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 {
		t.Fatalf("audited actions = %v, want exactly the register and rotate actions recorded through the real audit service", actions)
	}
	for action, payloadJSON := range actions {
		if strings.Contains(payloadJSON, sentinel) {
			t.Fatalf("audit payload for %q leaked the bearer token: %s", action, payloadJSON)
		}
	}
	if strings.Contains(logged.String(), sentinel) {
		t.Fatalf("process log leaked the bearer token: %s", logged.String())
	}
}

// A placeholder adoption is exactly the "backend commits the row but the
// caller only cares about the descriptor" case findings #2/#3 are about:
// this proves it through the real public RPC and the real audit service
// together, not the repository return value alone (mcp_register_placeholder_adoption_test.go
// already covers that in isolation). A bearer token is supplied on the
// adopting call so this simultaneously proves the sealed token this
// produces is actually persisted on the adopted row, while its sentinel —
// and its sealed ciphertext — never reach the RPC response, the audit
// payload, or the process log, exactly like
// TestPublicRegisterAndRotateAuditThroughRealAppWithoutLeakingTheToken
// proves for a fresh registration.
func TestRegisterMcpServerPlaceholderAdoptionWithBearerTokenAuditsAdoptedWithoutLeakingSentinel(t *testing.T) {
	const sentinel = "mcp-placeholder-adoption-audit-sentinel-3f8a91d0-do-not-leak"
	app := newMCPWiringTestApp(t, t.TempDir())
	client := publicMCPRegistryClient(t, app)
	ctx := publicMCPRegistryContext()

	placeholder, err := app.Repository.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })

	descriptor, err := client.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp",
		Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL, BearerToken: sentinel,
	})
	if err != nil {
		t.Fatalf("RegisterMcpServer must adopt the placeholder rather than error: %v", err)
	}
	if descriptor.GetServerId() != placeholder.Server.ID {
		t.Fatalf("ServerId = %q, want the placeholder %q adopted in place", descriptor.GetServerId(), placeholder.Server.ID)
	}

	var sealedToken []byte
	if err := app.database.QueryRowContext(context.Background(),
		`SELECT sealed_token FROM mcp_servers WHERE id = ?`, placeholder.Server.ID,
	).Scan(&sealedToken); err != nil {
		t.Fatal(err)
	}
	if len(sealedToken) == 0 {
		t.Fatal("sealed_token was not persisted on the adopted row")
	}
	ciphertext := base64.StdEncoding.EncodeToString(sealedToken)

	registerJSON, err := protojson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(registerJSON), sentinel) {
		t.Fatalf("RegisterMcpServer response leaked the bearer token: %s", registerJSON)
	}
	if strings.Contains(string(registerJSON), ciphertext) {
		t.Fatalf("RegisterMcpServer response leaked the sealed ciphertext: %s", registerJSON)
	}

	var payloadJSON string
	if err := app.database.QueryRowContext(context.Background(),
		`SELECT payload_json FROM audit_logs WHERE action = 'mcp.server.registered' AND target = ?`, placeholder.Server.ID,
	).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payloadJSON, sentinel) {
		t.Fatalf("audit payload leaked the bearer token: %s", payloadJSON)
	}
	if strings.Contains(payloadJSON, ciphertext) {
		t.Fatalf("audit payload leaked the sealed ciphertext: %s", payloadJSON)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["adopted"] != true {
		t.Fatalf("audit payload = %+v, want adopted=true: this call adopted an existing placeholder, not created a fresh row", payload)
	}

	if strings.Contains(logged.String(), sentinel) {
		t.Fatalf("process log leaked the bearer token: %s", logged.String())
	}
	if strings.Contains(logged.String(), ciphertext) {
		t.Fatalf("process log leaked the sealed ciphertext: %s", logged.String())
	}
}

// An install that never configured MCPConfigRoot (cfg.MCPConfigRoot == "")
// must keep refusing ReimportMcpJson through the real public gRPC server,
// not just inside a unit test of the service in isolation: this is the
// production wiring app.New assembles, so it is the proof that fail-closed
// behavior for an unconfigured config root actually reaches an operator's
// deployment rather than only the mcpregistry package's own tests.
func TestReimportMcpJsonThroughPublicRPCFailsPreconditionWithoutConfigRoot(t *testing.T) {
	app := newTestApp(t)
	client := publicMCPRegistryClient(t, app)

	_, err := client.ReimportMcpJson(publicMCPRegistryContext(), &turingv1.ReimportMcpJsonRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ReimportMcpJson code = %v, want FailedPrecondition for an unconfigured MCPConfigRoot", status.Code(err))
	}
}

// The internal identity allowlist app.New wires up grants the runtime only
// ListMcpServers and CallRegisteredMcpTool; the three management RPCs must
// be unavailable to every internal identity — both the runtime and the
// approval-consumer — through the real internal gRPC server and its actual
// identity interceptor, never merely through the InternalServer wrapper's
// own hardcoded denial.
func TestMCPManagementRPCsAreUnavailableToEveryInternalIdentityThroughRealServer(t *testing.T) {
	app := newMCPWiringTestApp(t, t.TempDir())
	conn := newBufconnClient(t, app.InternalServer)
	client := turingv1.NewMcpRegistryServiceClient(conn)

	for _, identity := range []struct {
		name  string
		token string
	}{
		{"runtime", "internal"},
		{"approval-consumer", "internal-approval-consumer"},
	} {
		t.Run(identity.name, func(t *testing.T) {
			ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+identity.token))
			if _, err := client.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
				Name: "vendor", Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
			}); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("RegisterMcpServer code = %v, want PermissionDenied", status.Code(err))
			}
			if _, err := client.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
				ServerId: "mcp_any",
			}); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("RotateMcpServerToken code = %v, want PermissionDenied", status.Code(err))
			}
			if _, err := client.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{}); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("ReimportMcpJson code = %v, want PermissionDenied", status.Code(err))
			}
		})
	}

	// The runtime's own allowed methods must still work exactly as before.
	runtimeCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+"internal"))
	if _, err := client.ListMcpServers(runtimeCtx, &turingv1.ListMcpServersRequest{}); err != nil {
		t.Fatalf("runtime ListMcpServers: %v", err)
	}

	// The approval-consumer identity is not in the allowlist for either
	// ListMcpServers or CallRegisteredMcpTool, and must be denied both.
	approvalConsumerCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+"internal-approval-consumer"))
	if _, err := client.ListMcpServers(approvalConsumerCtx, &turingv1.ListMcpServersRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("approval-consumer ListMcpServers code = %v, want PermissionDenied", status.Code(err))
	}
	if _, err := client.CallRegisteredMcpTool(approvalConsumerCtx, &turingv1.CallRegisteredMcpToolRequest{
		ServerId: "missing", RunId: "run", ToolName: "tool",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("approval-consumer CallRegisteredMcpTool code = %v, want PermissionDenied", status.Code(err))
	}
}
