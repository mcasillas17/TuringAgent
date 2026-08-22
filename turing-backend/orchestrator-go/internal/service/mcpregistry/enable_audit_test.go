package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
)

// toolsListVendor returns an httptest server that answers tools/list with a
// single, harmless tool — enough for discover() to succeed.
func toolsListVendor(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": map[string]any{"tools": []any{map[string]any{
				"name": "vendor.lookup", "inputSchema": map[string]any{"type": "object"},
			}}},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// failingVendor returns an httptest server whose tools/list reply always
// fails with the given message, so discover() reports an error.
func failingVendor(t *testing.T, message string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"error":   map[string]any{"code": -32000, "message": message},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func onlyExpectedAuditKeys(t *testing.T, payload map[string]any, allowed ...string) {
	t.Helper()
	for key, value := range payload {
		found := false
		for _, want := range allowed {
			if key == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("payload has unexpected key %q=%v", key, value)
		}
	}
}

// TestEnablingRemoteServerAuditsSuccessWithNameTierAndRemoteContact proves a
// successful remote-URL enable is audited exactly once, targeted at the
// server id, with a payload limited to name/tier/remoteContact — no token,
// no URL, no status/error text — and remoteContact true because enabling a
// remote-url server really did contact it.
func TestEnablingRemoteServerAuditsSuccessWithNameTierAndRemoteContact(t *testing.T) {
	service, repo := newRegistryTestService(t)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)
	vendor := toolsListVendor(t)
	service.httpClient = vendor.Client()
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "remote", URL: vendor.URL, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UP {
		t.Fatalf("liveness = %v, want up", descriptor.GetLiveness())
	}

	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want exactly one", recorder.records)
	}
	record := recorder.records[0]
	if record.action != "mcp.server.enabled" {
		t.Fatalf("action = %q, want mcp.server.enabled", record.action)
	}
	if record.target != server.ID {
		t.Fatalf("target = %q, want the server id %q", record.target, server.ID)
	}
	if record.payload["name"] != "remote" {
		t.Fatalf("payload name = %v, want remote", record.payload["name"])
	}
	if record.payload["tier"] != string(repository.MCPServerTierRemoteURL) {
		t.Fatalf("payload tier = %v, want remote_url", record.payload["tier"])
	}
	if record.payload["remoteContact"] != true {
		t.Fatalf("payload remoteContact = %v, want true", record.payload["remoteContact"])
	}
	onlyExpectedAuditKeys(t, record.payload, "name", "tier", "remoteContact")
}

// TestEnablingRemoteServerAuditsFailureWithoutStatusOrToken proves that even
// when the enable-time discovery call fails, the RPC still returns a
// descriptor (down, still enabled) and the state change is still audited —
// the audit/notify step happens before descriptor mapping so a real,
// committed state change is never left unaudited — with a payload that
// still carries no status/error text and no token.
func TestEnablingRemoteServerAuditsFailureWithoutStatusOrToken(t *testing.T) {
	const sentinel = "enable-failure-sentinel-4b8f2a91-do-not-leak"
	service, repo := newRegistryTestService(t)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)
	vendor := failingVendor(t, "unauthorized for Bearer "+sentinel)
	service.httpClient = vendor.Client()
	sealed, err := service.sealServerToken("remote-failing", sentinel)
	if err != nil {
		t.Fatal(err)
	}
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "remote-failing", URL: vendor.URL, SealedToken: sealed, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !descriptor.GetEnabled() {
		t.Fatal("a remote server that fails discovery must remain enabled")
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_DOWN {
		t.Fatalf("liveness = %v, want down", descriptor.GetLiveness())
	}

	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want exactly one audit record despite the discovery failure", recorder.records)
	}
	record := recorder.records[0]
	if record.action != "mcp.server.enabled" {
		t.Fatalf("action = %q, want mcp.server.enabled even though discovery failed", record.action)
	}
	if record.target != server.ID {
		t.Fatalf("target = %q, want the server id %q", record.target, server.ID)
	}
	if record.payload["remoteContact"] != true {
		t.Fatalf("payload remoteContact = %v, want true", record.payload["remoteContact"])
	}
	onlyExpectedAuditKeys(t, record.payload, "name", "tier", "remoteContact")
	assertStringSentinelFree(t, "audit payload", fmt.Sprintf("%+v", record.payload), sentinel)
}

// TestDisablingServerAuditsWithoutRemoteContact proves disabling any
// non-bundled server is audited as mcp.server.disabled with
// remoteContact=false — disabling never contacts anything.
func TestDisablingServerAuditsWithoutRemoteContact(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "remote", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(context.Background(), server.ID, true); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.GetEnabled() {
		t.Fatal("disabling must actually disable")
	}

	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want exactly one", recorder.records)
	}
	record := recorder.records[0]
	if record.action != "mcp.server.disabled" {
		t.Fatalf("action = %q, want mcp.server.disabled", record.action)
	}
	if record.target != server.ID {
		t.Fatalf("target = %q, want the server id %q", record.target, server.ID)
	}
	if record.payload["remoteContact"] != false {
		t.Fatalf("payload remoteContact = %v, want false", record.payload["remoteContact"])
	}
	onlyExpectedAuditKeys(t, record.payload, "name", "tier", "remoteContact")
}

// TestSetMcpServerEnabledNotifiesAndAuditsBeforeADescriptorFailure is the
// discriminating case for "move notify/audit before descriptor mapping": a
// broken tool schema makes serverDescriptor fail on the read-back, but the
// enable/disable mutation and its audit record must already have committed
// by that point. This uses disable (not enable) so discover() never runs
// and never touches the tools table itself — the only thing that can
// explain the broken schema still being there is that nothing rediscovered
// over it, isolating the ordering this test is about. Mirrors
// TestRegisterMcpServerNotifiesAndAuditsBeforeADescriptorFailure and
// TestRotateMcpServerTokenNotifiesAndAuditsBeforeADescriptorFailure.
func TestSetMcpServerEnabledNotifiesAndAuditsBeforeADescriptorFailure(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(context.Background(), server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(context.Background(), server.ID, []repository.MCPServerTool{
		{Name: "vendor.broken", Policy: "safe", SchemaJSON: "not valid json", Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}

	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	_, err = service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: false,
	})
	if err == nil {
		t.Fatal("want an error from the broken tool schema breaking descriptor construction")
	}

	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 despite the later descriptor failure", notifier.calls)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want one audit row despite the later descriptor failure", recorder.records)
	}
	if recorder.records[0].action != "mcp.server.disabled" {
		t.Fatalf("action = %q, want mcp.server.disabled", recorder.records[0].action)
	}
	updated, err := repo.GetMCPServer(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("the repository mutation must have committed despite the later descriptor failure")
	}
}

// TestEnablingRemoteServerSentinelBearerAbsentFromAuditLogEventAndResponse is
// the one test to keep if every other enable-audit test here were deleted:
// a bearer token given to a remote server must never leak into the audit
// row, the process log, the session events table, or the RPC response, even
// when the vendor's own failure message echoes it back — using the real
// audit service, not a test double, so the actual persisted payload is what
// gets inspected.
func TestEnablingRemoteServerSentinelBearerAbsentFromAuditLogEventAndResponse(t *testing.T) {
	const sentinel = "enable-sentinel-9d3c6f18-should-never-leak-anywhere"
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

	vendor := failingVendor(t, "unauthorized for Bearer "+sentinel)
	service.httpClient = vendor.Client()
	sealed, err := service.sealServerToken("remote-failing", sentinel)
	if err != nil {
		t.Fatal(err)
	}
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "remote-failing", URL: vendor.URL, SealedToken: sealed, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStringSentinelFree(t, "enable response", descriptor.String(), sentinel)

	var auditCount int
	if err := database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_logs`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount == 0 {
		t.Fatal("no audit rows were written, so this check proves nothing about them")
	}
	rows, err := database.QueryContext(context.Background(), `SELECT payload_json FROM audit_logs`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		assertStringSentinelFree(t, "audit payload", payload, sentinel)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	// Enabling a server (like registering or rotating one) is audited, not
	// emitted as a session event.
	var eventCount int
	if err := database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("event count = %d, want 0: enabling an MCP server must not emit session events", eventCount)
	}

	assertStringSentinelFree(t, "process log", logged.String(), sentinel)
}
