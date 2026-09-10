package mcp

import (
	"context"
	"errors"
	"reflect"
	"strings"
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

// stubRegistryRPC answers CallRegisteredMCPTool with one canned result.
type stubRegistryRPC struct {
	RegistryRPC
	result map[string]any
}

func (s stubRegistryRPC) CallRegisteredMCPTool(
	context.Context, *turingv1.CallRegisteredMcpToolRequest,
) (*turingv1.CallRegisteredMcpToolResponse, error) {
	value, err := structpb.NewStruct(s.result)
	if err != nil {
		return nil, err
	}
	return &turingv1.CallRegisteredMcpToolResponse{Result: value}, nil
}

func TestAThirdPartyToolExecutionErrorIsNotReportedAsSuccess(t *testing.T) {
	// A registered server reports a tool failure with `isError: true` on an
	// otherwise successful result. The bundled path already turns that into a
	// failed call; the third-party path must too, or the model is handed a
	// result that only looks successful and the run records a completed tool
	// call that did nothing.
	client := &RegistryClient{rpc: stubRegistryRPC{result: map[string]any{
		"content": []any{map[string]any{"type": "text", "text": "the vendor refused"}},
		"isError": true,
	}}}

	result, err := client.CallToolWithCallerApproval(context.Background(), "run_1", "appr_1", "vendor.write", nil)
	if err == nil {
		t.Fatalf("result = %#v, want the tool execution failure surfaced", result)
	}
	var toolErr ToolCallError
	if !errors.As(err, &toolErr) {
		t.Fatalf("error = %T %v, want a ToolCallError", err, err)
	}
	if !strings.Contains(err.Error(), "the vendor refused") {
		t.Fatalf("error = %v, want the peer's own message preserved", err)
	}
}

func TestAThirdPartySuccessIsStillReturned(t *testing.T) {
	client := &RegistryClient{rpc: stubRegistryRPC{result: map[string]any{
		"content": []any{map[string]any{"type": "text", "text": "done"}},
		"isError": false,
	}}}

	result, err := client.CallToolWithCallerApproval(context.Background(), "run_1", "appr_1", "vendor.read", nil)
	if err != nil {
		t.Fatalf("CallToolWithCallerApproval: %v", err)
	}
	if _, present := result["content"]; !present {
		t.Fatalf("result = %#v, want the peer's result returned", result)
	}
}

func TestAThirdPartyStructuredResultIsUnwrappedLikeABundledOne(t *testing.T) {
	// CON-001's acceptance case is a stock server, and a stock server following
	// the specification's own advice answers with BOTH representations: the
	// payload in structuredContent and the same payload serialized into a
	// content text block. The bundled path unwraps that to the tool's own map.
	// This path must agree, or the model sees the tool's data twice — once
	// re-escaped — for a third-party tool and once for a bundled one, for what
	// is the same kind of call, and the 500-character run summary is spent on
	// the duplicate.
	payload := map[string]any{"stars": float64(3), "name": "vendor"}
	client := &RegistryClient{rpc: stubRegistryRPC{result: map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": `{"stars":3,"name":"vendor"}`}},
		"structuredContent": payload,
		"isError":           false,
	}}}

	result, err := client.CallToolWithCallerApproval(context.Background(), "run_1", "appr_1", "vendor.rate", nil)
	if err != nil {
		t.Fatalf("CallToolWithCallerApproval: %v", err)
	}
	if !reflect.DeepEqual(result, payload) {
		t.Fatalf("result = %#v, want the unwrapped tool payload %#v", result, payload)
	}
	if _, present := result["content"]; present {
		t.Error("the CallToolResult envelope reached the caller; the bundled path would have unwrapped it")
	}
}
