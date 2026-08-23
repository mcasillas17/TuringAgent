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

// SetMcpServerEnabled's final step — building the response descriptor via
// serverDescriptor/toolDescriptor — can fail on a poisoned stored
// schema_json (e.g. corrupted by something other than this package, or
// left over from a bug elsewhere): that failure must never reach the
// caller as the raw json.Unmarshal/structpb error, the same way
// UpdateMcpToolPolicy already maps its own toolDescriptor failure to a
// fixed, generic status rather than returning it as-is (which would
// surface as the unhelpful default codes.Unknown and could, in principle,
// repeat some part of the stored value). This uses disable (not enable)
// so discover() never runs and never touches the tools table itself — the
// only thing that can explain the poisoned schema still being there is
// that nothing rediscovered over it.
func TestSetMcpServerEnabledMapsPoisonedSchemaDescriptorFailureToFixedInternalStatus(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(ctx, server.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	const poisonSentinel = "poison-schema-sentinel-6a2f19dd-must-not-leak"
	if err := repo.ReplaceMCPServerTools(ctx, server.Server.ID, []repository.MCPServerTool{
		{
			Name:       "vendor.broken",
			Policy:     "safe",
			SchemaJSON: `{"broken": ` + poisonSentinel, // deliberately invalid JSON
			Enabled:    true,
			Present:    true,
		},
	}); err != nil {
		t.Fatal(err)
	}

	_, err = service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.Server.ID, Enabled: false,
	})
	if err == nil {
		t.Fatal("want an error from the poisoned tool schema breaking descriptor construction")
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
	if err.Error() != status.Error(codes.Internal, "read MCP server failed").Error() {
		t.Fatalf("err = %q, want the fixed \"read MCP server failed\" Internal status", err.Error())
	}
	if strings.Contains(err.Error(), poisonSentinel) {
		t.Fatalf("err = %q, must not leak the poisoned schema content", err.Error())
	}
	if strings.Contains(err.Error(), "broken") || strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("err = %q, must not leak the raw json.Unmarshal error text or stored content", err.Error())
	}

	// The mutation itself (disable) must still have committed: descriptor
	// construction is the last step, strictly after the repository write.
	updated, err := repo.GetMCPServer(ctx, server.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("the disable mutation must have committed despite the later descriptor failure")
	}
}
