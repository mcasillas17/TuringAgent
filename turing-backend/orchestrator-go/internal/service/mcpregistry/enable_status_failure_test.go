package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newRegistryTestServiceWithRealAudit is like newRegistryTestService but
// also returns the underlying *db.DB (so a test can install a failure
// trigger directly) and wires the real audit service — not a test double —
// so a test can inspect what actually persisted to audit_logs, the same way
// TestEnablingRemoteServerSentinelBearerAbsentFromAuditLogEventAndResponse
// does.
func newRegistryTestServiceWithRealAudit(t *testing.T) (*Server, *repository.Repository, *db.DB) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	service := New(repo, sealer, nil)
	service.SetAuditRecorder(audit.New(repo))
	return service, repo, database
}

// forceMCPServerStatusUpdateFailure installs a trigger that aborts every
// UPDATE against mcp_server_status. Registration already inserts an initial
// status row for every server, so both the disable path
// (status -> "unknown") and the enable-time discovery-failure fallback
// (status -> "down") hit this trigger via SQLite's
// INSERT ... ON CONFLICT DO UPDATE upsert, simulating a liveness-status
// persistence failure that happens strictly after the enable/disable
// mutation has already committed.
func forceMCPServerStatusUpdateFailure(t *testing.T, database *db.DB) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_mcp_server_status_update
		BEFORE UPDATE ON mcp_server_status
		BEGIN
			SELECT RAISE(ABORT, 'mcp_server_status update unavailable');
		END;
	`); err != nil {
		t.Fatal(err)
	}
}

func auditPayloadsForAction(t *testing.T, database *db.DB, action string) []map[string]any {
	t.Helper()
	rows, err := database.QueryContext(context.Background(),
		`SELECT payload_json FROM audit_logs WHERE action = ?`, action)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var payloads []map[string]any
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("audit payload is not valid JSON: %v", err)
		}
		payloads = append(payloads, payload)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return payloads
}

// TestSetMcpServerEnabledDisableStatusFailureStillNotifiesAuditsAndReturnsInternal
// is the discriminating test for the core reported bug: SetMCPServerEnabled
// commits (enabled -> disabled) before the liveness-status write is even
// attempted, so a trigger that makes the subsequent mcp_server_status
// UPDATE fail must not swallow the notify/audit that describe the change
// that already happened. The RPC does still surface the status failure —
// as a fixed Internal error carrying no detail from the trigger — but only
// after notify/audit have already run against the committed state.
func TestSetMcpServerEnabledDisableStatusFailureStillNotifiesAuditsAndReturnsInternal(t *testing.T) {
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "remote", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(context.Background(), server.ID, true); err != nil {
		t.Fatal(err)
	}
	forceMCPServerStatusUpdateFailure(t, database)

	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	_, err = service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: false,
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal when the post-commit status write fails", status.Code(err))
	}

	updated, err := repo.GetMCPServer(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("the disable must have committed despite the later status-write failure")
	}

	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 despite the status-write failure", notifier.calls)
	}

	payloads := auditPayloadsForAction(t, database, "mcp.server.disabled")
	if len(payloads) != 1 {
		t.Fatalf("mcp.server.disabled audit rows = %d, want exactly 1 despite the status-write failure", len(payloads))
	}
	onlyExpectedAuditKeys(t, payloads[0], "name", "tier", "remoteDiscoveryAttempted", "discoverySucceeded")
	if payloads[0]["remoteDiscoveryAttempted"] != false {
		t.Fatalf("payload remoteDiscoveryAttempted = %v, want false", payloads[0]["remoteDiscoveryAttempted"])
	}
	if payloads[0]["discoverySucceeded"] != false {
		t.Fatalf("payload discoverySucceeded = %v, want false", payloads[0]["discoverySucceeded"])
	}
}

// TestSetMcpServerEnabledDiscoveryFailureStatusFailureStillNotifiesAuditsAndReturnsInternal
// is the enable-time counterpart: discovery itself fails organically (a
// real vendor error carrying a token sentinel), and the fallback status
// write ("down") that would normally record that also fails via the
// trigger. The enable must still have committed, notify/audit must still
// have run — with a token-free payload — and only then does the RPC return
// a fixed Internal error.
func TestSetMcpServerEnabledDiscoveryFailureStatusFailureStillNotifiesAuditsAndReturnsInternal(t *testing.T) {
	const sentinel = "enable-status-failure-sentinel-7a1e9c53-do-not-leak"
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
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
	forceMCPServerStatusUpdateFailure(t, database)

	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	_, err = service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal when the post-commit status write fails", status.Code(err))
	}

	updated, err := repo.GetMCPServer(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled {
		t.Fatal("the enable must have committed despite the later status-write failure")
	}

	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 despite the status-write failure", notifier.calls)
	}

	payloads := auditPayloadsForAction(t, database, "mcp.server.enabled")
	if len(payloads) != 1 {
		t.Fatalf("mcp.server.enabled audit rows = %d, want exactly 1 despite the status-write failure", len(payloads))
	}
	onlyExpectedAuditKeys(t, payloads[0], "name", "tier", "remoteDiscoveryAttempted", "discoverySucceeded")
	if payloads[0]["remoteDiscoveryAttempted"] != true {
		t.Fatalf("payload remoteDiscoveryAttempted = %v, want true", payloads[0]["remoteDiscoveryAttempted"])
	}
	if payloads[0]["discoverySucceeded"] != false {
		t.Fatalf("payload discoverySucceeded = %v, want false: discovery itself failed", payloads[0]["discoverySucceeded"])
	}
	encoded, err := json.Marshal(payloads[0])
	if err != nil {
		t.Fatal(err)
	}
	assertStringSentinelFree(t, "audit payload", string(encoded), sentinel)
}
