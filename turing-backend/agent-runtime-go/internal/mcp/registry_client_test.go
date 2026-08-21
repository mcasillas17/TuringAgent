package mcp

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestRegistryClientsExposeEnabledThirdPartyServersAndProxyCalls(t *testing.T) {
	schema, err := structpb.NewStruct(map[string]any{"type": "object"})
	if err != nil {
		t.Fatal(err)
	}
	rpc := &registryRPCTestDouble{
		response: &turingv1.ListMcpServersResponse{Servers: []*turingv1.McpServerDescriptor{
			{
				ServerId: "bundled", Name: "system",
				Tier: turingv1.McpServerTier_MCP_SERVER_TIER_BUNDLED, Enabled: true,
			},
			{
				ServerId: "disabled", Name: "off",
				Tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER,
			},
			{
				ServerId: "vendor-id", Name: "vendor",
				Tier: turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER, Enabled: true,
				Tools: []*turingv1.McpToolDescriptor{{
					ToolName: "vendor.lookup",
					Policy:   turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
					Schema:   schema,
					Enabled:  true,
				}, {
					ToolName: "vendor.disabled",
					Policy:   turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
					Schema:   schema,
					Enabled:  true,
				}},
			},
		}},
	}

	clients, err := NewRegistryClients(context.Background(), rpc)
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 || clients["vendor"] == nil {
		t.Fatalf("registry clients = %v, want only vendor", clients)
	}
	tools, err := clients["vendor"].ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0]["name"] != "vendor.lookup" {
		t.Fatalf("tools = %+v", tools)
	}
	if _, err := clients["vendor"].CallToolWithCallerApproval(
		context.Background(),
		"run_1",
		"appr_1",
		"vendor.lookup",
		map[string]any{"id": "42"},
	); err != nil {
		t.Fatal(err)
	}
	if rpc.call.GetServerId() != "vendor-id" ||
		rpc.call.GetRunId() != "run_1" ||
		rpc.call.GetApprovalId() != "appr_1" {
		t.Fatalf("proxied call = %+v", rpc.call)
	}
}

type registryRPCTestDouble struct {
	response *turingv1.ListMcpServersResponse
	call     *turingv1.CallRegisteredMcpToolRequest
}

func (r *registryRPCTestDouble) ListMCPServers(context.Context) (*turingv1.ListMcpServersResponse, error) {
	return r.response, nil
}

func (r *registryRPCTestDouble) CallRegisteredMCPTool(
	_ context.Context,
	request *turingv1.CallRegisteredMcpToolRequest,
) (*turingv1.CallRegisteredMcpToolResponse, error) {
	r.call = request
	result, _ := structpb.NewStruct(map[string]any{"ok": true})
	return &turingv1.CallRegisteredMcpToolResponse{Result: result}, nil
}
