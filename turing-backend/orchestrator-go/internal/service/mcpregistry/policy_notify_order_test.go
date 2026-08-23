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

// A successful policy update must notify the runtime so its tool cache
// picks up the change — the ordinary, uncomplicated case the failure test
// below extends.
func TestUpdateMcpToolPolicyNotifiesRegistryChangeOnSuccess(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, server.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.lookup", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	if _, err := service.UpdateMcpToolPolicy(ctx, &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.Server.ID, ToolName: "vendor.lookup",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
	}); err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 after a successful policy update", notifier.calls)
	}
}

// A policy change must not become a lost notification just because
// building the response descriptor afterward hits an unexpected failure: a
// tool with a deliberately invalid stored schema makes toolDescriptor fail
// to unmarshal it, and the RPC does surface that as Internal, but
// SetMCPToolPolicy's own repository mutation and the registry-change
// notification must both already have happened by then — a client seeing
// an Internal error must not be left thinking the policy change never took
// effect, nor go unnotified so the runtime's tool cache goes stale. This is
// the same reasoning already applied to
// TestRegisterMcpServerNotifiesAndAuditsBeforeADescriptorFailure and
// TestRotateMcpServerTokenNotifiesAndAuditsBeforeADescriptorFailure, for
// UpdateMcpToolPolicy's own post-commit notify.
func TestUpdateMcpToolPolicyNotifiesBeforeADescriptorMappingFailure(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Poison the stored schema directly through the repository, bypassing
	// the service's own JSON validation (which would normally refuse
	// this) — simulating some other corruption of a previously-valid
	// row, the same setup TestRotateMcpServerTokenNotifiesAndAuditsBeforeADescriptorFailure
	// uses for the equivalent rotate-path test.
	if err := repo.ReplaceMCPServerTools(ctx, server.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.broken", Policy: "safe", SchemaJSON: "not valid json", Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}

	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	_, err = service.UpdateMcpToolPolicy(ctx, &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.Server.ID, ToolName: "vendor.broken",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal from the broken tool schema breaking descriptor construction", status.Code(err))
	}
	// The raw JSON-unmarshal error must never leak through as the gRPC
	// status message or (via defaulting to codes.Unknown) as the status
	// code: both must be the one fixed, generic mapping.
	if strings.Contains(strings.ToLower(err.Error()), "invalid character") || strings.Contains(strings.ToLower(err.Error()), "unmarshal") {
		t.Fatalf("err = %q, must not leak the raw JSON decode error", err.Error())
	}

	policy, _, found, perr := repo.GetToolPolicy(ctx, "vendor", "vendor.broken")
	if perr != nil {
		t.Fatal(perr)
	}
	if !found {
		t.Fatal("tool policy must still be found despite the later descriptor failure")
	}
	if policy != "disabled" {
		t.Fatalf("policy = %q, want disabled: the mutation must have committed despite the later descriptor failure", policy)
	}

	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 despite the later descriptor failure", notifier.calls)
	}
}
