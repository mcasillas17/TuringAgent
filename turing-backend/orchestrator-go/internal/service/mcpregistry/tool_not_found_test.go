package mcpregistry

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUpdateUnknownMCPToolPolicyReturnsNotFound(t *testing.T) {
	service, repo := newRegistryTestService(t)
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	files := findRepositoryServer(t, servers, "files")
	_, err = service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: files.ID, ToolName: "missing",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("error = %v, want NotFound", err)
	}
}
