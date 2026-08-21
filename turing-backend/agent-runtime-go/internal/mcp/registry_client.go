package mcp

import (
	"context"
	"errors"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

type RegistryRPC interface {
	ListMCPServers(context.Context) (*turingv1.ListMcpServersResponse, error)
	CallRegisteredMCPTool(
		context.Context,
		*turingv1.CallRegisteredMcpToolRequest,
	) (*turingv1.CallRegisteredMcpToolResponse, error)
}

type RegistryClient struct {
	rpc      RegistryRPC
	serverID string
	tools    []*turingv1.McpToolDescriptor
}

func NewRegistryClients(ctx context.Context, rpc RegistryRPC) (map[string]*RegistryClient, error) {
	response, err := rpc.ListMCPServers(ctx)
	if err != nil {
		return nil, err
	}
	clients := make(map[string]*RegistryClient)
	for _, server := range response.GetServers() {
		if !server.GetEnabled() ||
			server.GetTier() == turingv1.McpServerTier_MCP_SERVER_TIER_BUNDLED ||
			server.GetTier() == turingv1.McpServerTier_MCP_SERVER_TIER_UNSPECIFIED {
			continue
		}
		clients[server.GetName()] = &RegistryClient{
			rpc:      rpc,
			serverID: server.GetServerId(),
			tools:    append([]*turingv1.McpToolDescriptor(nil), server.GetTools()...),
		}
	}
	return clients, nil
}

func (c *RegistryClient) ListTools(context.Context) ([]map[string]any, error) {
	tools := make([]map[string]any, 0, len(c.tools))
	for _, descriptor := range c.tools {
		if !descriptor.GetEnabled() ||
			descriptor.GetPolicy() == turingv1.ToolPolicy_TOOL_POLICY_DISABLED {
			continue
		}
		schema := map[string]any{"type": "object"}
		if descriptor.GetSchema() != nil {
			schema = descriptor.GetSchema().AsMap()
		}
		tools = append(tools, map[string]any{
			"name":        descriptor.GetToolName(),
			"inputSchema": schema,
		})
	}
	return tools, nil
}

func (*RegistryClient) CallTool(context.Context, string, map[string]any, ...string) (map[string]any, error) {
	return nil, errors.New("registered MCP server requires orchestrator caller-side enforcement")
}

func (c *RegistryClient) CallToolWithCallerApproval(
	ctx context.Context,
	runID string,
	approvalID string,
	name string,
	args map[string]any,
) (map[string]any, error) {
	value, err := structpb.NewStruct(args)
	if err != nil {
		return nil, err
	}
	response, err := c.rpc.CallRegisteredMCPTool(ctx, &turingv1.CallRegisteredMcpToolRequest{
		ServerId:   c.serverID,
		RunId:      runID,
		ApprovalId: approvalID,
		ToolName:   name,
		Args:       value,
	})
	if err != nil {
		return nil, err
	}
	if response.GetResult() == nil {
		return nil, errors.New("registered MCP server returned no result")
	}
	return response.GetResult().AsMap(), nil
}
