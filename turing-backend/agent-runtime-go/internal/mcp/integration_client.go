package mcp

import (
	"context"
	"errors"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

type IntegrationRPC interface {
	ListIntegrationTools(context.Context) (*turingv1.ListIntegrationToolsResponse, error)
	CallIntegrationTool(context.Context, *turingv1.CallIntegrationToolRequest) (*turingv1.CallIntegrationToolResponse, error)
}

type IntegrationClient struct{ rpc IntegrationRPC }

func NewIntegrationClient(rpc IntegrationRPC) *IntegrationClient { return &IntegrationClient{rpc: rpc} }

func (c *IntegrationClient) ListTools(ctx context.Context) ([]map[string]any, error) {
	response, err := c.rpc.ListIntegrationTools(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(response.GetTools()))
	for _, descriptor := range response.GetTools() {
		if descriptor.GetPolicy() == turingv1.ToolPolicy_TOOL_POLICY_DISABLED {
			continue
		}
		schema := map[string]any{"type": "object"}
		if descriptor.GetSchema() != nil {
			schema = descriptor.GetSchema().AsMap()
		}
		result = append(result, map[string]any{"name": descriptor.GetToolName(), "description": descriptor.GetDescription(), "inputSchema": schema})
	}
	return result, nil
}

func (*IntegrationClient) CallTool(context.Context, string, map[string]any, ...string) (map[string]any, error) {
	return nil, errors.New("integration tools require orchestrator caller-side enforcement")
}

func (c *IntegrationClient) CallToolWithCallerApproval(ctx context.Context, runID, approvalID, name string, args map[string]any) (map[string]any, error) {
	value, err := structpb.NewStruct(args)
	if err != nil {
		return nil, err
	}
	response, err := c.rpc.CallIntegrationTool(ctx, &turingv1.CallIntegrationToolRequest{RunId: runID, ApprovalId: approvalID, ToolName: name, Args: value})
	if err != nil {
		return nil, err
	}
	if response.GetResult() == nil {
		return nil, errors.New("integration tool returned no result")
	}
	return response.GetResult().AsMap(), nil
}
