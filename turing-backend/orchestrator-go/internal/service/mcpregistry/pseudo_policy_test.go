package mcpregistry

import (
	"context"
	"sync/atomic"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type countingRegistryNotifier struct{ calls atomic.Int32 }

func (n *countingRegistryNotifier) NotifyMCPRegistryChanged(context.Context) error {
	n.calls.Add(1)
	return nil
}

func TestUpdateToolPolicyByNameGuardsRoundTripsAndNotifies(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{
		{ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required"},
		{ServerName: "integrations", ToolName: "github.create_comment", SchemaJSON: `{}`, Policy: "approval_required"},
		{ServerName: "skills", ToolName: "skills_list", SchemaJSON: `{}`, Policy: "approval_required"},
		{ServerName: "files", ToolName: "files.create", SchemaJSON: `{}`, Policy: "approval_required"},
	}); err != nil {
		t.Fatal(err)
	}
	notifier := &countingRegistryNotifier{}
	service.SetRegistryChangeNotifier(notifier)
	if _, err := service.UpdateToolPolicyByName(ctx, &turingv1.UpdateToolPolicyByNameRequest{ServerName: "integrations", ToolName: "github.create_comment", Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("integration write safe error=%v", err)
	}
	if _, err := service.UpdateToolPolicyByName(ctx, &turingv1.UpdateToolPolicyByNameRequest{ServerName: "files", ToolName: "files.create", Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("bundled write safe error=%v", err)
	}
	for _, target := range []struct{ server, tool string }{{"integrations", "github.list_issues"}, {"skills", "skills_list"}} {
		if _, err := service.UpdateToolPolicyByName(ctx, &turingv1.UpdateToolPolicyByNameRequest{ServerName: target.server, ToolName: target.tool, Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE}); err != nil {
			t.Fatal(err)
		}
		for _, state := range []struct {
			policy  turingv1.ToolPolicy
			enabled bool
		}{
			{policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE, enabled: true},
			{policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED},
			{policy: turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED, enabled: true},
		} {
			if state.policy != turingv1.ToolPolicy_TOOL_POLICY_SAFE {
				if _, err := service.UpdateToolPolicyByName(ctx, &turingv1.UpdateToolPolicyByNameRequest{ServerName: target.server, ToolName: target.tool, Policy: state.policy}); err != nil {
					t.Fatal(err)
				}
			}
			listed, err := service.ListPseudoServerTools(ctx, &turingv1.ListPseudoServerToolsRequest{ServerName: target.server})
			if err != nil {
				t.Fatal(err)
			}
			var matched *turingv1.McpToolDescriptor
			for _, descriptor := range listed.GetTools() {
				if descriptor.GetToolName() == target.tool {
					matched = descriptor
				}
			}
			if matched == nil || matched.GetPolicy() != state.policy || matched.GetEnabled() != state.enabled {
				t.Fatalf("%s %s round trip=%+v", target.server, state.policy, listed)
			}
		}
	}
	if notifier.calls.Load() != 6 {
		t.Fatalf("notifications=%d, want 6", notifier.calls.Load())
	}
}
