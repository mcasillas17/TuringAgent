package mcpregistry

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The public server is where an operator manages their own registry; the
// internal server is where the runtime dispatches tool calls. Registration,
// reimport, and rotation are operator actions, so the public server must
// delegate them and the internal server must refuse them the same way it
// already refuses SetMcpServerEnabled/UpdateMcpToolPolicy/DeleteMcpServer.
func TestPublicServerDelegatesManagementRPCs(t *testing.T) {
	service, _ := newRegistryTestService(t)
	public := NewPublicServer(service)

	descriptor, err := public.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})
	if err != nil {
		t.Fatalf("RegisterMcpServer: %v", err)
	}
	if descriptor.GetName() != "vendor" {
		t.Fatalf("descriptor = %+v, want the registered server", descriptor)
	}

	if _, err := public.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ReimportMcpJson delegated with code %v, want FailedPrecondition (config root unset) rather than PermissionDenied", status.Code(err))
	}

	rotated, err := public.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: descriptor.GetServerId(), BearerToken: "vendor-secret",
	})
	if err != nil {
		t.Fatalf("RotateMcpServerToken: %v", err)
	}
	if rotated.GetServerId() != descriptor.GetServerId() {
		t.Fatalf("rotated = %+v, want the same server", rotated)
	}
}

func TestInternalServerDeniesManagementRPCs(t *testing.T) {
	service, _ := newRegistryTestService(t)
	internal := NewInternalServer(service)

	if _, err := internal.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("RegisterMcpServer code = %v, want PermissionDenied", status.Code(err))
	}
	if _, err := internal.ReimportMcpJson(context.Background(), &turingv1.ReimportMcpJsonRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ReimportMcpJson code = %v, want PermissionDenied", status.Code(err))
	}
	if _, err := internal.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: "mcp_any",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("RotateMcpServerToken code = %v, want PermissionDenied", status.Code(err))
	}
}
