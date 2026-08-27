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

// "memory" is the orchestrator's own namespace. A third-party server allowed to
// register under it — or under a differently-cased spelling of it — would own
// the `memory.` tool names, which is how a remote process ends up answering
// memory.read for the user's own vault.
func TestMemoryServerNameIsReservedEverywhereARegistrationCanArrive(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	for _, name := range []string{"memory", "Memory", "MEMORY", "mEmOrY"} {
		if _, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
			Name: name, Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		}); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("registering %q error = %v, want FailedPrecondition", name, err)
		}
		if _, err := repo.GetMCPServerByName(ctx, name); err == nil {
			t.Fatalf("a server registered under the reserved name %q", name)
		}
	}

	// The same refusal has to hold for the other door into the registry: an
	// mcp.json the user dropped in.
	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"memory": {"url": "https://vendor.example/mcp"},
			"vendor": {"url": "https://vendor.example/other"}
		}
	}`))
	if err != nil {
		t.Fatalf("ImportJSON: %v", err)
	}
	if _, err := repo.GetMCPServerByName(ctx, "memory"); err == nil {
		t.Fatal("an mcp.json entry imported over the reserved memory namespace")
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != nil {
		t.Fatalf("the well-formed entry beside it was dropped too: %v", err)
	}
	refused := false
	for _, reason := range report.Unsupported {
		if strings.Contains(reason, "invalid or reserved") {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("import report = %+v, want the reserved entry recorded as unsupported", report.Unsupported)
	}
}

// The management surface has to describe memory the same way it describes the
// other two pseudo-servers, or the user can see a policy they cannot change.
func TestListPseudoServerToolsDescribesTheMemoryServer(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{
		{ServerName: "memory", ToolName: "memory.search", SchemaJSON: `{"type":"object"}`, Policy: "safe"},
		{ServerName: "memory", ToolName: "memory.remember", SchemaJSON: `{"type":"object"}`, Policy: "approval_required"},
	}); err != nil {
		t.Fatal(err)
	}

	listed, err := service.ListPseudoServerTools(ctx, &turingv1.ListPseudoServerToolsRequest{ServerName: "memory"})
	if err != nil {
		t.Fatalf("ListPseudoServerTools: %v", err)
	}
	policies := map[string]turingv1.ToolPolicy{}
	for _, tool := range listed.GetTools() {
		policies[tool.GetToolName()] = tool.GetPolicy()
	}
	if policies["memory.search"] != turingv1.ToolPolicy_TOOL_POLICY_SAFE {
		t.Fatalf("memory.search policy = %v, want SAFE", policies["memory.search"])
	}
	if policies["memory.remember"] != turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED {
		t.Fatalf("memory.remember policy = %v, want APPROVAL_REQUIRED", policies["memory.remember"])
	}

	// The user's own decision about when Turing may write a proposal is theirs
	// to make, in both directions, and it round-trips through this surface.
	for _, policy := range []turingv1.ToolPolicy{
		turingv1.ToolPolicy_TOOL_POLICY_SAFE,
		turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
		turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
	} {
		descriptor, err := service.UpdateToolPolicyByName(ctx, &turingv1.UpdateToolPolicyByNameRequest{
			ServerName: "memory", ToolName: "memory.remember", Policy: policy,
		})
		if err != nil {
			t.Fatalf("UpdateToolPolicyByName(%v): %v", policy, err)
		}
		if descriptor.GetPolicy() != policy {
			t.Fatalf("policy = %v, want %v", descriptor.GetPolicy(), policy)
		}
	}

	if _, err := service.ListPseudoServerTools(ctx, &turingv1.ListPseudoServerToolsRequest{
		ServerName: "vendor",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("third-party pseudo listing error = %v, want InvalidArgument", err)
	}
	if _, err := NewInternalServer(service).ListPseudoServerTools(ctx, &turingv1.ListPseudoServerToolsRequest{
		ServerName: "memory",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("internal pseudo listing error = %v, want PermissionDenied", err)
	}
}
