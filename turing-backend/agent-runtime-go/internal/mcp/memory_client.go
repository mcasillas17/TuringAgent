package mcp

import (
	"context"
	"errors"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// MemoryRPC is the internal facet of the memory service, and the whole of what
// the runtime is allowed to ask for. Reading the vault and deciding a proposal
// are the user's, and holding the internal token is not being the user.
type MemoryRPC interface {
	ListMemoryTools(context.Context) (*turingv1.ListMemoryToolsResponse, error)
	CallMemoryTool(context.Context, *turingv1.CallMemoryToolRequest) (*turingv1.CallMemoryToolResponse, error)
}

// MemoryClient wires the memory pseudo-server into tool discovery.
//
// It holds no vault path, no session id and no schema of its own: the server
// answers what exists right now, which is how the memory toggle reaches a
// worker that is already connected. An empty list is the toggle being off, and
// it is passed through as an empty list rather than softened into a default.
type MemoryClient struct{ rpc MemoryRPC }

func NewMemoryClient(rpc MemoryRPC) *MemoryClient { return &MemoryClient{rpc: rpc} }

func (c *MemoryClient) ListTools(ctx context.Context) ([]map[string]any, error) {
	response, err := c.rpc.ListMemoryTools(ctx)
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
		result = append(result, map[string]any{
			"name":        descriptor.GetToolName(),
			"description": descriptor.GetDescription(),
			"inputSchema": schema,
		})
	}
	return result, nil
}

// CallTool refuses. memory.remember is approval_required and the orchestrator
// is the component that executes it, so the approval has to be consumed on that
// side; a path that ran a memory tool without going through it would be an
// approval nobody spent.
func (*MemoryClient) CallTool(context.Context, string, map[string]any, ...string) (map[string]any, error) {
	return nil, errors.New("memory tools require orchestrator caller-side enforcement")
}

// CallToolWithCallerApproval dispatches one memory tool over the internal
// channel, naming only the run and the approval.
//
// The result comes back exactly as the server framed it. Search and read
// answers arrive inside a per-call delimiter that says they are the user's own
// notes and never instructions; unwrapping or re-framing them here would either
// strip that label or nest a second one, and both end with a model reading a
// note as something addressed to it.
func (c *MemoryClient) CallToolWithCallerApproval(
	ctx context.Context,
	runID, approvalID, name string,
	args map[string]any,
) (map[string]any, error) {
	value, err := structpb.NewStruct(args)
	if err != nil {
		return nil, err
	}
	response, err := c.rpc.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ApprovalId: approvalID, ToolName: name, Args: value,
	})
	if err != nil {
		return nil, err
	}
	if response.GetResult() == nil {
		return nil, errors.New("memory tool returned no result")
	}
	return response.GetResult().AsMap(), nil
}
