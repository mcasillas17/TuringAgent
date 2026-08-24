package mcpregistry

import (
	"context"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
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

// TestUpdateToolPolicyByNameIsAuditedWithServerToolAndPolicy is finding #3's
// determining test: UpdateToolPolicyByName — the name-addressed
// compatibility RPC that also reaches the orchestrator-owned
// "skills"/"integrations" pseudo-servers, neither of which has an
// mcp_servers row UpdateMcpToolPolicy's own server-id lookup could ever
// resolve — commits a policy change the exact same way its id-addressed
// sibling does (TestUpdateMcpToolPolicyIsAuditedWithServerToolAndPolicy
// above), but previously audited nothing at all. It must audit immediately,
// using the identical mcp.server.tool_policy_changed action and
// name/toolName/toolPolicy payload shape, so both RPCs project through the
// one shared typed audit-read rule (service/audit/service.go) without it
// ever needing to know which RPC wrote a given row.
func TestUpdateToolPolicyByNameIsAuditedWithServerToolAndPolicy(t *testing.T) {
	service, repo := newRegistryTestService(t)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	if err := repo.UpsertTools(context.Background(), []repository.DiscoveredTool{
		{ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required"},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.UpdateToolPolicyByName(context.Background(), &turingv1.UpdateToolPolicyByNameRequest{
		ServerName: "integrations", ToolName: "github.list_issues",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateToolPolicyByName(context.Background(), &turingv1.UpdateToolPolicyByNameRequest{
		ServerName: "integrations", ToolName: "github.list_issues",
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
	if first.target != "integrations" {
		t.Fatalf("first target = %q, want the pseudo-server name %q: it has no mcp_servers row/id an audit target could otherwise use", first.target, "integrations")
	}
	if first.payload["name"] != "integrations" || first.payload["toolName"] != "github.list_issues" || first.payload["toolPolicy"] != "safe" {
		t.Fatalf("first payload = %+v, want name=integrations toolName=github.list_issues toolPolicy=safe", first.payload)
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
// auditMCPEvent call site and TestUpdateMcpToolPolicySucceedsDespiteAuditFailure
// above.
func TestUpdateToolPolicyByNameSucceedsDespiteAuditFailure(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if err := repo.UpsertTools(context.Background(), []repository.DiscoveredTool{
		{ServerName: "skills", ToolName: "skills_list", SchemaJSON: `{}`, Policy: "approval_required"},
	}); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingAuditRecorder{fail: status.Error(codes.Internal, "audit sink unavailable")}
	service.SetAuditRecorder(recorder)

	descriptor, err := service.UpdateToolPolicyByName(context.Background(), &turingv1.UpdateToolPolicyByNameRequest{
		ServerName: "skills", ToolName: "skills_list",
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

	policy, _, found, err := repo.GetToolPolicy(context.Background(), "skills", "skills_list")
	if err != nil {
		t.Fatal(err)
	}
	if !found || policy != "disabled" {
		t.Fatalf("policy = %q found=%v, want disabled/true: the mutation must have committed despite the audit failure", policy, found)
	}
}

// TestUpdateToolPolicyByNamePersistsRealAuditRowReadableViaAuditAPI proves
// finding #3's own explicit acceptance criterion: an integrations policy
// change lands a real row in audit_logs and is readable back through the
// actual public read RPC (audit.Server.ListAuditEntries), not merely a
// direct table query — the same typed mcp.server.tool_policy_changed
// projection TestUpdateMcpToolPolicyPersistsRealAuditRow already exercises
// for the id-addressed RPC, proving it disclosed ServerName/ToolName/
// ToolPolicy and nothing else — no schema, no args, no token — regardless
// of which of the two writer RPCs produced the row.
func TestUpdateToolPolicyByNamePersistsRealAuditRowReadableViaAuditAPI(t *testing.T) {
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
	if err := repo.UpsertTools(context.Background(), []repository.DiscoveredTool{
		{ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{"type":"object","properties":{"repo":{"type":"string"}}}`, Policy: "approval_required"},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.UpdateToolPolicyByName(context.Background(), &turingv1.UpdateToolPolicyByNameRequest{
		ServerName: "integrations", ToolName: "github.list_issues",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	}); err != nil {
		t.Fatal(err)
	}

	payloads := auditPayloadsForAction(t, database, "mcp.server.tool_policy_changed")
	if len(payloads) != 1 {
		t.Fatalf("mcp.server.tool_policy_changed audit rows = %d, want exactly 1", len(payloads))
	}
	if payloads[0]["name"] != "integrations" || payloads[0]["toolName"] != "github.list_issues" || payloads[0]["toolPolicy"] != "safe" {
		t.Fatalf("persisted payload = %+v, want name=integrations toolName=github.list_issues toolPolicy=safe", payloads[0])
	}
	for key := range payloads[0] {
		if key != "name" && key != "toolName" && key != "toolPolicy" {
			t.Fatalf("persisted payload has unexpected key %q (want only name/toolName/toolPolicy)", key)
		}
	}

	auditService := audit.New(repo)
	action := "mcp.server.tool_policy_changed"
	response, err := auditService.ListAuditEntries(context.Background(), &turingv1.ListAuditEntriesRequest{
		Action: &action,
		Order:  turingv1.AuditOrder_AUDIT_ORDER_DESCENDING,
		Page:   &turingv1.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetEntries()) != 1 {
		t.Fatalf("ListAuditEntries entries = %+v, want exactly 1", response.GetEntries())
	}
	entry := response.GetEntries()[0]
	if entry.GetAction() != "mcp.server.tool_policy_changed" {
		t.Fatalf("entry action = %q, want mcp.server.tool_policy_changed", entry.GetAction())
	}
	payload := entry.GetPayload()
	if payload.GetServerName() != "integrations" {
		t.Fatalf("payload server_name = %q, want integrations", payload.GetServerName())
	}
	if payload.GetToolName() != "github.list_issues" {
		t.Fatalf("payload tool_name = %q, want github.list_issues", payload.GetToolName())
	}
	if payload.GetToolPolicy() != "safe" {
		t.Fatalf("payload tool_policy = %q, want safe", payload.GetToolPolicy())
	}
	encoded, err := protojson.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "properties") || strings.Contains(string(encoded), "repo") {
		t.Fatalf("audit-read payload leaked the tool's own schema: %s", encoded)
	}
}

// A policy change must not become a lost audit record just because
// building the response descriptor afterward hits an unexpected failure —
// mirroring TestUpdateMcpToolPolicyAuditsBeforeADescriptorMappingFailure
// above, but for the name-addressed RPC.
func TestUpdateToolPolicyByNameAuditsBeforeADescriptorMappingFailure(t *testing.T) {
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(context.Background(), server.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.broken", Policy: "safe", SchemaJSON: "not valid json", Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}

	_, err = service.UpdateToolPolicyByName(context.Background(), &turingv1.UpdateToolPolicyByNameRequest{
		ServerName: "vendor", ToolName: "vendor.broken",
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
// approval FailedPrecondition fires before SetToolPolicyByName is ever
// called, so nothing committed and no mcp.server.tool_policy_changed row
// may exist. Mirrors TestUpdateMcpToolPolicyBundledRefusalProducesNoAudit.
func TestUpdateToolPolicyByNameBundledRefusalProducesNoAudit(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if err := repo.UpsertTools(context.Background(), []repository.DiscoveredTool{{
		ServerName: "files", ToolName: "files.create",
		SchemaJSON: `{"type":"object"}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	_, err := service.UpdateToolPolicyByName(context.Background(), &turingv1.UpdateToolPolicyByNameRequest{
		ServerName: "files", ToolName: "files.create",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition for a bundled mutating tool downgraded to safe", status.Code(err))
	}
	if len(recorder.records) != 0 {
		t.Fatalf("records = %+v, want none: a refused policy change must not be audited", recorder.records)
	}
}

// The name-addressed RPC's own, additional integrations-mutating-requires-
// approval precondition (UpdateMcpToolPolicy has no equivalent check: it
// never reaches the integrations pseudo-server at all) must likewise
// refuse before anything commits, producing no audit row either.
func TestUpdateToolPolicyByNameIntegrationsRefusalProducesNoAudit(t *testing.T) {
	service, repo := newRegistryTestService(t)
	if err := repo.UpsertTools(context.Background(), []repository.DiscoveredTool{{
		ServerName: "integrations", ToolName: "github.create_comment",
		SchemaJSON: `{}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	_, err := service.UpdateToolPolicyByName(context.Background(), &turingv1.UpdateToolPolicyByNameRequest{
		ServerName: "integrations", ToolName: "github.create_comment",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition for an integration mutating tool downgraded to safe", status.Code(err))
	}
	if len(recorder.records) != 0 {
		t.Fatalf("records = %+v, want none: a refused policy change must not be audited", recorder.records)
	}
}

// A policy change naming a tool that does not exist must never be audited
// either, mirroring TestUpdateMcpToolPolicyUnknownToolProducesNoAudit above.
func TestUpdateToolPolicyByNameUnknownToolProducesNoAudit(t *testing.T) {
	service, _ := newRegistryTestService(t)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	_, err := service.UpdateToolPolicyByName(context.Background(), &turingv1.UpdateToolPolicyByNameRequest{
		ServerName: "files", ToolName: "missing",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound for an unknown tool", status.Code(err))
	}
	if len(recorder.records) != 0 {
		t.Fatalf("records = %+v, want none: a failed policy change must not be audited", recorder.records)
	}
}
