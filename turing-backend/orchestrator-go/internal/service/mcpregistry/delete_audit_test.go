package mcpregistry

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A successful delete is a consequential change to standing registry state —
// the same reasoning that already audits register/rotate/reimport — so it
// must leave a record naming the server and its tier. The payload never
// carries a URL or token-related key: unlike RegisterMcpServer's payload
// (which legitimately includes the canonicalized URL), a deletion payload is
// deliberately narrower, matching the reviewed policy in audit/service.go.
func TestDeleteMcpServerIsAuditedWithNameAndTier(t *testing.T) {
	service, repo := newRegistryTestService(t)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.DeleteMcpServer(context.Background(), &turingv1.DeleteMcpServerRequest{
		ServerId: server.Server.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want exactly one", recorder.records)
	}
	record := recorder.records[0]
	if record.action != "mcp.server.deleted" {
		t.Fatalf("action = %q, want mcp.server.deleted", record.action)
	}
	if record.target != server.Server.ID {
		t.Fatalf("target = %q, want the deleted server id %q", record.target, server.Server.ID)
	}
	if record.payload["name"] != "vendor" {
		t.Fatalf("payload = %+v, want name=vendor", record.payload)
	}
	if record.payload["tier"] != string(repository.MCPServerTierRemoteURL) {
		t.Fatalf("payload = %+v, want tier=remote_url", record.payload)
	}
	for key := range record.payload {
		if key != "name" && key != "tier" {
			t.Fatalf("delete payload has unexpected key %q (want only name/tier — no url or token fields)", key)
		}
	}
}

// The audit write is best-effort, matching every other auditMCPEvent call
// site: a client must never be told a delete failed just because recording it
// did.
func TestDeleteMcpServerSucceedsDespiteAuditFailure(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingAuditRecorder{fail: status.Error(codes.Internal, "audit sink unavailable")}
	service.SetAuditRecorder(recorder)

	if _, err := service.DeleteMcpServer(context.Background(), &turingv1.DeleteMcpServerRequest{
		ServerId: server.Server.ID,
	}); err != nil {
		t.Fatalf("an audit failure must not fail the delete: %v", err)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want exactly one attempted audit call despite it failing", recorder.records)
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range servers {
		if listed.ID == server.Server.ID {
			t.Fatal("the delete must have committed despite the audit failure")
		}
	}
}

// TestDeleteMcpServerPersistsRealAuditRow proves an actual row lands in
// audit_logs — not just that the AuditRecorder interface was called — the
// same discriminating check enable_status_failure_test.go already applies to
// SetMcpServerEnabled.
func TestDeleteMcpServerPersistsRealAuditRow(t *testing.T) {
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.DeleteMcpServer(context.Background(), &turingv1.DeleteMcpServerRequest{
		ServerId: server.Server.ID,
	}); err != nil {
		t.Fatal(err)
	}

	payloads := auditPayloadsForAction(t, database, "mcp.server.deleted")
	if len(payloads) != 1 {
		t.Fatalf("mcp.server.deleted audit rows = %d, want exactly 1", len(payloads))
	}
	if payloads[0]["name"] != "vendor" || payloads[0]["tier"] != string(repository.MCPServerTierLocalContainer) {
		t.Fatalf("persisted payload = %+v, want name=vendor tier=local_container", payloads[0])
	}
}

// A server_id naming nothing at all must map to NotFound: DeleteMCPServer
// itself refuses inside its own transaction (no row to read, tombstone,
// or delete), returning the zero-value record alongside the named error,
// so no audit row may be produced for a delete that never happened.
func TestDeleteMcpServerMissingServerIsNotFoundWithoutAudit(t *testing.T) {
	service, _ := newRegistryTestService(t)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	_, err := service.DeleteMcpServer(context.Background(), &turingv1.DeleteMcpServerRequest{
		ServerId: "mcp_missing",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound for a missing server", status.Code(err))
	}
	if len(recorder.records) != 0 {
		t.Fatalf("records = %+v, want none: a delete that never happened must not be audited", recorder.records)
	}
}

// A bundled server still refuses deletion with the same FailedPrecondition
// it always has (repository.ErrMCPServerBundled) — decided by
// DeleteMCPServer's own tier check inside its single transaction, which
// also means the tombstone insert and delete never even run — so no audit
// row is produced for it either.
func TestDeleteMcpServerBundledRefusesWithoutAudit(t *testing.T) {
	service, repo := newRegistryTestService(t)
	bundled, err := repo.ImportMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "bundled-vendor", URL: "https://bundled.example/mcp", Tier: repository.MCPServerTierBundled,
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	_, err = service.DeleteMcpServer(context.Background(), &turingv1.DeleteMcpServerRequest{
		ServerId: bundled.Server.ID,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition for a bundled server", status.Code(err))
	}
	if len(recorder.records) != 0 {
		t.Fatalf("records = %+v, want none: a refused delete must not be audited", recorder.records)
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, listed := range servers {
		if listed.ID == bundled.Server.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the bundled server must still be registered; the refused delete must not have committed")
	}
}
