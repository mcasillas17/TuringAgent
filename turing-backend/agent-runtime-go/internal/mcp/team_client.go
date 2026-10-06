package mcp

import (
	"context"
	"errors"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// TeamRPC is the internal facet of the team service. A run asks for the team
// tool it was offered when it was enqueued, and delegates through it; nothing
// here can read or change a profile.
type TeamRPC interface {
	ListTeamTools(ctx context.Context, runID string) (*turingv1.ListTeamToolsResponse, error)
	CallTeamTool(context.Context, *turingv1.CallTeamToolRequest) (*turingv1.CallTeamToolResponse, error)
}

// TeamCallClient dispatches one team.delegate call. It is built per call,
// because the orchestrator needs what the runner does not pass: the assignment
// the run holds, so a fenced worker cannot spawn a child, and the model's tool
// call, so a retried call returns the delegation it already created.
type TeamCallClient struct {
	rpc        TeamRPC
	attemptID  string
	toolCallID string
}

func NewTeamCallClient(rpc TeamRPC, attemptID, toolCallID string) *TeamCallClient {
	return &TeamCallClient{rpc: rpc, attemptID: attemptID, toolCallID: toolCallID}
}

// CallTool refuses: the orchestrator creates the delegation and consumes its
// approval, so the only path is the caller-enforced one.
func (*TeamCallClient) CallTool(context.Context, string, map[string]any, ...string) (map[string]any, error) {
	return nil, errors.New("team tools require orchestrator caller-side enforcement")
}

func (c *TeamCallClient) CallToolWithCallerApproval(
	ctx context.Context,
	runID, approvalID, name string,
	args map[string]any,
) (map[string]any, error) {
	value, err := structpb.NewStruct(args)
	if err != nil {
		return nil, err
	}
	response, err := c.rpc.CallTeamTool(ctx, &turingv1.CallTeamToolRequest{
		RunId: runID, AssignmentAttemptId: c.attemptID, ApprovalId: approvalID,
		ToolCallId: c.toolCallID, ToolName: name, Args: value,
	})
	if err != nil {
		return nil, err
	}
	if response.GetResult() == nil {
		return nil, errors.New("team tool returned no result")
	}
	return response.GetResult().AsMap(), nil
}
