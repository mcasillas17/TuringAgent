package mcp

import (
	"context"
	"errors"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

type fakeTeamRPC struct {
	calls   []*turingv1.CallTeamToolRequest
	result  *structpb.Struct
	callErr error
}

func (*fakeTeamRPC) ListTeamTools(context.Context, string) (*turingv1.ListTeamToolsResponse, error) {
	return &turingv1.ListTeamToolsResponse{}, nil
}

func (f *fakeTeamRPC) CallTeamTool(_ context.Context, request *turingv1.CallTeamToolRequest) (*turingv1.CallTeamToolResponse, error) {
	f.calls = append(f.calls, request)
	if f.callErr != nil {
		return nil, f.callErr
	}
	return &turingv1.CallTeamToolResponse{Result: f.result}, nil
}

// A delegation names the run, the assignment it holds, the decided approval
// and the model's tool call, so the orchestrator can fence a stale worker and
// replay a retried call.
func TestTeamCallClientForwardsTheCallWithItsAssignmentAndToolCall(t *testing.T) {
	result, err := structpb.NewStruct(map[string]any{"delegation_id": "dlg_1", "agent": "research", "state": "queued"})
	if err != nil {
		t.Fatal(err)
	}
	rpc := &fakeTeamRPC{result: result}
	got, err := NewTeamCallClient(rpc, "attempt_1", "call_7").CallToolWithCallerApproval(
		context.Background(), "run_1", "appr_1", "team.delegate", map[string]any{"agent": "research", "task": "Gather the notes"})
	if err != nil {
		t.Fatal(err)
	}
	if got["delegation_id"] != "dlg_1" || got["state"] != "queued" {
		t.Fatalf("result = %+v", got)
	}
	if len(rpc.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(rpc.calls))
	}
	call := rpc.calls[0]
	if call.GetRunId() != "run_1" || call.GetAssignmentAttemptId() != "attempt_1" || call.GetApprovalId() != "appr_1" ||
		call.GetToolCallId() != "call_7" || call.GetToolName() != "team.delegate" || call.GetArgs().AsMap()["task"] != "Gather the notes" {
		t.Fatalf("request = %+v", call)
	}
}

// The orchestrator creates the delegation and consumes its approval, so the
// plain call path refuses, and an empty answer is an error, not a delegation.
func TestTeamCallClientRefusesThePlainPathAndAnEmptyResult(t *testing.T) {
	rpc := &fakeTeamRPC{}
	client := NewTeamCallClient(rpc, "attempt_1", "call_7")
	if _, err := client.CallTool(context.Background(), "team.delegate", map[string]any{}); err == nil {
		t.Fatal("the plain call path delegated")
	}
	if len(rpc.calls) != 0 {
		t.Fatalf("calls = %d, want none from the plain path", len(rpc.calls))
	}
	if _, err := client.CallToolWithCallerApproval(context.Background(), "run_1", "", "team.delegate", map[string]any{}); err == nil {
		t.Fatal("an empty result was accepted")
	}
	rpc.callErr = errors.New("that specialist is not on this run's team")
	if _, err := client.CallToolWithCallerApproval(context.Background(), "run_1", "", "team.delegate", map[string]any{}); err == nil {
		t.Fatal("a refused delegation was reported as a result")
	}
}
