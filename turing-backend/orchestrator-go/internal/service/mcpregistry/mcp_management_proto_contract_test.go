package mcpregistry

import (
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// TestMcpManagementProtoContract is a compile-time contract test: it fails to
// compile until the RegisterMcpServer, ReimportMcpJson, and
// RotateMcpServerToken RPCs (and their request/response messages) exist in
// proto/turing/v1/mcp.proto with the exact fields described below. No bearer
// token or ciphertext may ever appear on a response message.
func TestMcpManagementProtoContract(t *testing.T) {
	registerReq := &turingv1.RegisterMcpServerRequest{
		Name:        "vendor",
		Url:         "https://vendor.example.com/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "secret-token",
	}
	if registerReq.GetName() != "vendor" ||
		registerReq.GetUrl() != "https://vendor.example.com/mcp" ||
		registerReq.GetTier() != turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL ||
		registerReq.GetBearerToken() != "secret-token" {
		t.Fatalf("RegisterMcpServerRequest field round-trip mismatch: %+v", registerReq)
	}
	var registerResp *turingv1.McpServerDescriptor = &turingv1.McpServerDescriptor{
		ServerId: "server-1",
		Name:     "vendor",
	}
	if registerResp.GetServerId() != "server-1" {
		t.Fatalf("RegisterMcpServer response mismatch: %+v", registerResp)
	}

	reimportReq := &turingv1.ReimportMcpJsonRequest{}
	_ = reimportReq
	reimportResp := &turingv1.ReimportMcpJsonResponse{
		Imported: []string{"vendor-a"},
		Skipped:  []string{"vendor-b"},
		Refused: []*turingv1.UnsupportedMcpServer{
			{Name: "vendor-c", Reason: "unsupported transport"},
		},
	}
	if len(reimportResp.GetImported()) != 1 || reimportResp.GetImported()[0] != "vendor-a" ||
		len(reimportResp.GetSkipped()) != 1 || reimportResp.GetSkipped()[0] != "vendor-b" ||
		len(reimportResp.GetRefused()) != 1 || reimportResp.GetRefused()[0].GetName() != "vendor-c" ||
		reimportResp.GetRefused()[0].GetReason() != "unsupported transport" {
		t.Fatalf("ReimportMcpJsonResponse field round-trip mismatch: %+v", reimportResp)
	}

	rotateReq := &turingv1.RotateMcpServerTokenRequest{
		ServerId:    "server-1",
		BearerToken: "new-secret-token",
	}
	if rotateReq.GetServerId() != "server-1" || rotateReq.GetBearerToken() != "new-secret-token" {
		t.Fatalf("RotateMcpServerTokenRequest field round-trip mismatch: %+v", rotateReq)
	}
	var rotateResp *turingv1.McpServerDescriptor = &turingv1.McpServerDescriptor{
		ServerId: "server-1",
		Name:     "vendor",
	}
	if rotateResp.GetServerId() != "server-1" {
		t.Fatalf("RotateMcpServerToken response mismatch: %+v", rotateResp)
	}
}
