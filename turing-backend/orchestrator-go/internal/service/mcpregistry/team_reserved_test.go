package mcpregistry

import (
	"context"
	"errors"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// "team" is the orchestrator's own namespace for delegation. A third-party
// server under it, in any letter case, would own team.delegate.
func TestTeamServerNameIsReservedEverywhereARegistrationCanArrive(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	for _, name := range []string{"team", "Team", "TEAM"} {
		if _, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
			Name: name, Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		}); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("registering %q error = %v, want FailedPrecondition", name, err)
		}
		if _, err := repo.GetMCPServerByName(ctx, name); err == nil {
			t.Fatalf("a server registered under the reserved name %q", name)
		}
	}
	if _, err := service.ImportJSON(ctx, []byte(`{"mcpServers":{"TEAM":{"url":"https://vendor.example/mcp"}}}`)); err != nil {
		t.Fatalf("ImportJSON: %v", err)
	}
	if _, err := repo.GetMCPServerByName(ctx, "TEAM"); err == nil {
		t.Fatal("an mcp.json entry imported over the reserved team namespace")
	}
}

func TestThirdPartyTeamToolsAreACollision(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"team.delegate", "team.other", "Team.Delegate", "TEAM.other"} {
		err := service.RecordDiscovery(context.Background(), server.Server.ID, []DiscoveredTool{{Name: tool, SchemaJSON: `{}`}})
		if !errors.Is(err, repository.ErrMCPToolNameCollision) {
			t.Fatalf("discovering %s: %v, want ErrMCPToolNameCollision", tool, err)
		}
	}
}

// The user changes team.delegate's policy in the Tools settings like any
// other pseudo-server tool.
func TestListPseudoServerToolsDescribesTheTeamServer(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{
		{ServerName: "team", ToolName: "team.delegate", SchemaJSON: `{"type":"object"}`, Policy: "safe"},
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := service.ListPseudoServerTools(ctx, &turingv1.ListPseudoServerToolsRequest{ServerName: "team"})
	if err != nil {
		t.Fatalf("ListPseudoServerTools: %v", err)
	}
	if len(listed.GetTools()) != 1 || listed.GetTools()[0].GetToolName() != "team.delegate" ||
		listed.GetTools()[0].GetPolicy() != turingv1.ToolPolicy_TOOL_POLICY_SAFE {
		t.Fatalf("team tools = %v, want team.delegate SAFE", listed.GetTools())
	}
	for _, policy := range []turingv1.ToolPolicy{
		turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
		turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
		turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	} {
		descriptor, err := service.UpdateToolPolicyByName(ctx, &turingv1.UpdateToolPolicyByNameRequest{
			ServerName: "team", ToolName: "team.delegate", Policy: policy,
		})
		if err != nil || descriptor.GetPolicy() != policy {
			t.Fatalf("UpdateToolPolicyByName(%v) = %v, %v", policy, descriptor.GetPolicy(), err)
		}
	}
}
