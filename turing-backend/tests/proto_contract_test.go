package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtoContractsDefineRequiredServices(t *testing.T) {
	root := filepath.Join("..", "..", "proto", "turing", "v1")
	required := map[string][]string{
		"chat.proto":      {"service ChatService", "rpc SendMessage", "returns (stream ChatStreamEvent)", "message TokenDelta"},
		"events.proto":    {"service EventService", "rpc ListEvents", "rpc SubscribeSessionEvents", "message TuringEvent"},
		"runtime.proto":   {"service RuntimeService", "rpc ConnectWorker", "returns (stream RuntimeCommand)", "stream RuntimeUpdate", "tool_policy_decision", "approval_token"},
		"sessions.proto":  {"service SessionService", "rpc CreateSession", "rpc ListMessages", "rpc SearchMessages", "message SearchMessagesRequest", "message SearchMessagesResponse", "rpc ListTools"},
		"approvals.proto": {"service ApprovalService", "rpc ApproveApproval", "rpc DenyApproval", "rpc GetApprovalForRuntime", "rpc ConsumeApproval"},
		"tools.proto":     {"message ToolCallBeacon", "message ToolPolicyDecision"},
		"mcp.proto":       {"message McpRequest", "message McpResult"},
		"health.proto":    {"service HealthService", "rpc Check", "rpc Version"},
	}
	for file, snippets := range required {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := string(data)
		for _, snippet := range snippets {
			if !strings.Contains(text, snippet) {
				t.Fatalf("%s missing %q", file, snippet)
			}
		}
	}
}

func TestDynamicFieldsUseStructNotRawJsonStrings(t *testing.T) {
	root := filepath.Join("..", "..", "proto", "turing", "v1")
	files, err := filepath.Glob(filepath.Join(root, "*.proto"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "bytes raw_json") || strings.Contains(text, "string raw_json") {
			t.Fatalf("%s uses raw_json instead of google.protobuf.Struct", filepath.Base(file))
		}
	}
}
