package mcp

import (
	"context"
	"errors"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

type fakeMemoryRPC struct {
	tools    []*turingv1.MemoryToolDescriptor
	listErr  error
	calls    []*turingv1.CallMemoryToolRequest
	result   *structpb.Struct
	callErr  error
	listCall int
}

func (f *fakeMemoryRPC) ListMemoryTools(context.Context) (*turingv1.ListMemoryToolsResponse, error) {
	f.listCall++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &turingv1.ListMemoryToolsResponse{Tools: f.tools}, nil
}

func (f *fakeMemoryRPC) CallMemoryTool(
	_ context.Context,
	request *turingv1.CallMemoryToolRequest,
) (*turingv1.CallMemoryToolResponse, error) {
	f.calls = append(f.calls, request)
	if f.callErr != nil {
		return nil, f.callErr
	}
	return &turingv1.CallMemoryToolResponse{Result: f.result}, nil
}

func TestMemoryClientListsEnabledToolsAndDropsDisabledOnes(t *testing.T) {
	schema, err := structpb.NewStruct(map[string]any{"type": "object"})
	if err != nil {
		t.Fatal(err)
	}
	rpc := &fakeMemoryRPC{tools: []*turingv1.MemoryToolDescriptor{
		{ToolName: "memory.search", Description: "Search", Schema: schema, Enabled: true,
			Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE},
		{ToolName: "memory.remember", Description: "Propose", Schema: schema, Enabled: true,
			Policy: turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED},
		{ToolName: "memory.read", Description: "Read", Schema: schema,
			Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED},
	}}

	listed, err := NewMemoryClient(rpc).ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0]["name"] != "memory.search" || listed[1]["name"] != "memory.remember" {
		t.Fatalf("listed = %+v", listed)
	}
	if listed[0]["description"] != "Search" {
		t.Fatalf("descriptions were dropped: %+v", listed)
	}
}

// The toggle is expressed as an empty list, and that has to survive: an empty
// answer takes memory away from a worker that is already connected, with no
// restart, and a client that substituted a default set would put it back.
func TestMemoryClientPassesAnEmptyListThrough(t *testing.T) {
	listed, err := NewMemoryClient(&fakeMemoryRPC{}).ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("listed = %+v, want nothing while memory is off", listed)
	}
}

// Memory tools are caller-enforced: the orchestrator consumes the approval and
// runs the tool itself, so the plain path must refuse rather than find some
// other way to call it.
func TestMemoryClientRefusesTheUnenforcedCallPath(t *testing.T) {
	if _, err := NewMemoryClient(&fakeMemoryRPC{}).CallTool(
		context.Background(), "memory.search", map[string]any{"query": "x"},
	); err == nil {
		t.Fatal("the unenforced call path succeeded")
	}
}

func TestMemoryClientDispatchesWithRunAndApprovalIdentity(t *testing.T) {
	result, err := structpb.NewStruct(map[string]any{"content": "BEGIN TURING_RETRIEVED_MEMORY_SEARCH_x\n..."})
	if err != nil {
		t.Fatal(err)
	}
	rpc := &fakeMemoryRPC{result: result}

	got, err := NewMemoryClient(rpc).CallToolWithCallerApproval(
		context.Background(), "run_1", "approval_1", "memory.search", map[string]any{"query": "chickens"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rpc.calls) != 1 || rpc.calls[0].GetRunId() != "run_1" ||
		rpc.calls[0].GetApprovalId() != "approval_1" || rpc.calls[0].GetToolName() != "memory.search" {
		t.Fatalf("dispatched = %+v", rpc.calls)
	}
	if rpc.calls[0].GetArgs().AsMap()["query"] != "chickens" {
		t.Fatalf("arguments = %+v", rpc.calls[0].GetArgs().AsMap())
	}
	// The frame is drawn on the orchestrator side. The client hands the result
	// through untouched: unwrapping it here would strip the one signal that
	// says these bytes are the user's notes rather than an instruction.
	if got["content"] != "BEGIN TURING_RETRIEVED_MEMORY_SEARCH_x\n..." {
		t.Fatalf("result = %+v, want the framed content verbatim", got)
	}
}

func TestMemoryClientRefusesAResultlessDispatch(t *testing.T) {
	if _, err := NewMemoryClient(&fakeMemoryRPC{}).CallToolWithCallerApproval(
		context.Background(), "run_1", "", "memory.search", map[string]any{"query": "x"},
	); err == nil {
		t.Fatal("a memory tool call with no result was accepted")
	}
	if _, err := NewMemoryClient(&fakeMemoryRPC{callErr: errors.New("denied")}).CallToolWithCallerApproval(
		context.Background(), "run_1", "", "memory.search", map[string]any{"query": "x"},
	); err == nil {
		t.Fatal("a refused memory tool call was reported as a success")
	}
}
