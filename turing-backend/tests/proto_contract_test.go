package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
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

func TestSearchMessagesProtoContract(t *testing.T) {
	file := turingv1.File_turing_v1_sessions_proto
	request := file.Messages().ByName("SearchMessagesRequest")
	assertProtoField(t, request, "query", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, request, "session_id", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, request, "limit", 3, protoreflect.Int32Kind, false, "")

	response := file.Messages().ByName("SearchMessagesResponse")
	assertProtoField(t, response, "messages", 1, protoreflect.MessageKind, true, "turing.v1.Message")

	service := file.Services().ByName("SessionService")
	if service == nil {
		t.Fatal("SessionService descriptor is missing")
	}
	method := service.Methods().ByName("SearchMessages")
	if method == nil {
		t.Fatal("SearchMessages method descriptor is missing")
	}
	if got := string(method.Input().FullName()); got != "turing.v1.SearchMessagesRequest" {
		t.Fatalf("SearchMessages input = %q, want turing.v1.SearchMessagesRequest", got)
	}
	if got := string(method.Output().FullName()); got != "turing.v1.SearchMessagesResponse" {
		t.Fatalf("SearchMessages output = %q, want turing.v1.SearchMessagesResponse", got)
	}
}

func TestRuntimeWorkerReadyReportsDiscoveredTools(t *testing.T) {
	file := turingv1.File_turing_v1_runtime_proto
	discoveredTool := file.Messages().ByName("DiscoveredTool")
	assertProtoField(t, discoveredTool, "server_name", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, discoveredTool, "tool_name", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, discoveredTool, "schema", 3, protoreflect.MessageKind, false, "google.protobuf.Struct")

	workerReady := file.Messages().ByName("RuntimeWorkerReady")
	assertProtoField(t, workerReady, "tools", 4, protoreflect.MessageKind, true, "turing.v1.DiscoveredTool")
	assertProtoField(t, workerReady, "tool_discovery_complete", 5, protoreflect.BoolKind, false, "")
}

func assertProtoField(t *testing.T, message protoreflect.MessageDescriptor, name protoreflect.Name, number protoreflect.FieldNumber, kind protoreflect.Kind, repeated bool, messageType protoreflect.FullName) {
	t.Helper()
	if message == nil {
		t.Fatal("message descriptor is missing")
	}
	field := message.Fields().ByName(name)
	if field == nil {
		t.Fatalf("%s.%s is missing", message.Name(), name)
	}
	if field.Number() != number || field.Kind() != kind || field.Cardinality() == protoreflect.Repeated != repeated {
		t.Fatalf("%s.%s descriptor = number %d kind %s cardinality %s", message.Name(), name, field.Number(), field.Kind(), field.Cardinality())
	}
	if messageType != "" && field.Message().FullName() != messageType {
		t.Fatalf("%s.%s message type = %q, want %q", message.Name(), name, field.Message().FullName(), messageType)
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
