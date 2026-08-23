package mcpregistry

import (
	"context"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A tool policy change is a consequential access-control change — narrowing
// or widening whether a tool can run without approval — so it must leave a
// record the same way register/rotate/enable/disable/delete already do. The
// payload carries only the server name, the tool name, and the new policy as
// its canonical string ("safe" / "approval_required" / "disabled"); it never
// carries the tool's schema, call arguments, or any token.
func TestUpdateMcpToolPolicyIsAuditedWithServerToolAndPolicy(t *testing.T) {
	service, repo := newRegistryTestService(t)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(context.Background(), server.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.write", Policy: "safe", SchemaJSON: `{"type":"object","properties":{"path":{"type":"string"}}}`, Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.Server.ID, ToolName: "vendor.write",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.Server.ID, ToolName: "vendor.write",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
	}); err != nil {
		t.Fatal(err)
	}

	if len(recorder.records) != 2 {
		t.Fatalf("records = %+v, want exactly two", recorder.records)
	}
	first := recorder.records[0]
	if first.action != "mcp.server.tool_policy_changed" {
		t.Fatalf("first action = %q, want mcp.server.tool_policy_changed", first.action)
	}
	if first.target != server.Server.ID {
		t.Fatalf("first target = %q, want the server id %q", first.target, server.Server.ID)
	}
	if first.payload["name"] != "vendor" || first.payload["toolName"] != "vendor.write" || first.payload["toolPolicy"] != "approval_required" {
		t.Fatalf("first payload = %+v, want name=vendor toolName=vendor.write toolPolicy=approval_required", first.payload)
	}
	second := recorder.records[1]
	if second.payload["toolPolicy"] != "disabled" {
		t.Fatalf("second payload = %+v, want toolPolicy=disabled", second.payload)
	}
	for _, record := range recorder.records {
		for key, value := range record.payload {
			if key != "name" && key != "toolName" && key != "toolPolicy" {
				t.Fatalf("tool policy payload has unexpected key %q=%v (want only name/toolName/toolPolicy — no schema, args, or token)", key, value)
			}
		}
	}
}

// The audit write is best-effort: a client must never be told a policy
// change failed just because recording it did, matching every other
// auditMCPEvent call site (register, rotate, delete).
func TestUpdateMcpToolPolicySucceedsDespiteAuditFailure(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(context.Background(), server.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.write", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingAuditRecorder{fail: status.Error(codes.Internal, "audit sink unavailable")}
	service.SetAuditRecorder(recorder)

	descriptor, err := service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.Server.ID, ToolName: "vendor.write",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
	})
	if err != nil {
		t.Fatalf("an audit failure must not fail the policy update: %v", err)
	}
	if descriptor.GetPolicy() != turingv1.ToolPolicy_TOOL_POLICY_DISABLED {
		t.Fatalf("descriptor policy = %v, want DISABLED despite the audit failure", descriptor.GetPolicy())
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want exactly one attempted audit call despite it failing", recorder.records)
	}

	policy, _, found, err := repo.GetToolPolicy(context.Background(), "vendor", "vendor.write")
	if err != nil {
		t.Fatal(err)
	}
	if !found || policy != "disabled" {
		t.Fatalf("policy = %q found=%v, want disabled/true: the mutation must have committed despite the audit failure", policy, found)
	}
}

// TestUpdateMcpToolPolicyPersistsRealAuditRow proves an actual row lands in
// audit_logs, the same discriminating check delete_audit_test.go and
// enable_status_failure_test.go already apply to their own mutations.
func TestUpdateMcpToolPolicyPersistsRealAuditRow(t *testing.T) {
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(context.Background(), server.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.write", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.Server.ID, ToolName: "vendor.write",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
	}); err != nil {
		t.Fatal(err)
	}

	payloads := auditPayloadsForAction(t, database, "mcp.server.tool_policy_changed")
	if len(payloads) != 1 {
		t.Fatalf("mcp.server.tool_policy_changed audit rows = %d, want exactly 1", len(payloads))
	}
	if payloads[0]["name"] != "vendor" || payloads[0]["toolName"] != "vendor.write" || payloads[0]["toolPolicy"] != "approval_required" {
		t.Fatalf("persisted payload = %+v, want name=vendor toolName=vendor.write toolPolicy=approval_required", payloads[0])
	}
}

// A policy change must not become a lost audit record just because building
// the response descriptor afterward hits an unexpected failure. This mirrors
// TestUpdateMcpToolPolicyNotifiesBeforeADescriptorMappingFailure
// (policy_notify_order_test.go) for notify, but for audit: SetMCPToolPolicy's
// own repository mutation must commit and the audit record must exist before
// the fallible tool-list/descriptor mapping ever runs, so an operator who
// receives the fixed Internal status from the poisoned schema is not left
// wondering whether the policy change — or its audit trail — ever happened.
func TestUpdateMcpToolPolicyAuditsBeforeADescriptorMappingFailure(t *testing.T) {
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Poison the stored schema directly through the repository, bypassing the
	// service's own JSON validation — the same setup
	// TestUpdateMcpToolPolicyNotifiesBeforeADescriptorMappingFailure uses.
	if err := repo.ReplaceMCPServerTools(context.Background(), server.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.broken", Policy: "safe", SchemaJSON: "not valid json", Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}

	_, err = service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.Server.ID, ToolName: "vendor.broken",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal from the broken tool schema breaking descriptor construction", status.Code(err))
	}

	payloads := auditPayloadsForAction(t, database, "mcp.server.tool_policy_changed")
	if len(payloads) != 1 {
		t.Fatalf("mcp.server.tool_policy_changed audit rows = %d, want exactly 1 despite the later descriptor failure", len(payloads))
	}
	if payloads[0]["name"] != "vendor" || payloads[0]["toolName"] != "vendor.broken" || payloads[0]["toolPolicy"] != "disabled" {
		t.Fatalf("persisted payload = %+v, want name=vendor toolName=vendor.broken toolPolicy=disabled", payloads[0])
	}
	if strings.Contains(err.Error(), "invalid character") || strings.Contains(err.Error(), "not valid json") {
		t.Fatalf("err = %q, must not leak the raw JSON decode error or stored schema content", err.Error())
	}
}

// A refused policy change must never be audited: the bundled-tool-requires-
// approval FailedPrecondition (TestBundledMutatingToolCannotBeDowngradedToSafe
// in bundled_policy_test.go covers the error code itself) fires before
// SetMCPToolPolicy is ever called, so nothing committed and no
// mcp.server.tool_policy_changed row may exist. Mirrors
// TestDeleteMcpServerBundledRefusesWithoutAudit.
func TestUpdateMcpToolPolicyBundledRefusalProducesNoAudit(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if err := repo.UpsertTools(context.Background(), []repository.DiscoveredTool{{
		ServerName: "files", ToolName: "files.create",
		SchemaJSON: `{"type":"object"}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	files := findRepositoryServer(t, servers, "files")
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	_, err = service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: files.ID, ToolName: "files.create",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition for a bundled mutating tool downgraded to safe", status.Code(err))
	}
	if len(recorder.records) != 0 {
		t.Fatalf("records = %+v, want none: a refused policy change must not be audited", recorder.records)
	}
}

// A policy change naming a tool that does not exist must never be audited
// either: SetMCPToolPolicy itself refuses with ErrMCPToolNotFound before
// anything commits. Mirrors TestDeleteMcpServerMissingServerIsNotFoundWithoutAudit.
func TestUpdateMcpToolPolicyUnknownToolProducesNoAudit(t *testing.T) {
	service, repo := newRegistryTestService(t)
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	files := findRepositoryServer(t, servers, "files")
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	_, err = service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: files.ID, ToolName: "missing",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound for an unknown tool", status.Code(err))
	}
	if len(recorder.records) != 0 {
		t.Fatalf("records = %+v, want none: a failed policy change must not be audited", recorder.records)
	}
}
