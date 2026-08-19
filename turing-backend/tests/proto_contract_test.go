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
		// The credential_ref/credential_available pair is the whole storage
		// decision in the contract: a client learns the NAME of a key and
		// whether the backend has it, never the key.
		"agents.proto": {
			"service ExternalAgentService", "rpc ListExternalAgents", "rpc SetSessionAgent",
			"rpc ClearSessionAgent", "message ExternalAgent", "string credential_ref",
			"bool credential_available",
		},
		"mcp.proto":    {"message McpRequest", "message McpResult"},
		"health.proto": {"service HealthService", "rpc Check", "rpc Version"},
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
	assertProtoField(t, workerReady, "tool_discovery_status", 5, protoreflect.EnumKind, false, "")
	status := file.Enums().ByName("ToolDiscoveryStatus")
	if status == nil || status.Values().ByName("TOOL_DISCOVERY_STATUS_COMPLETE") == nil || status.Values().ByName("TOOL_DISCOVERY_STATUS_FAILED") == nil {
		t.Fatalf("ToolDiscoveryStatus must define COMPLETE and FAILED: %v", status)
	}
}

func TestWorkerCapabilityRoutingProtoContract(t *testing.T) {
	common := turingv1.File_turing_v1_common_proto
	model := common.Messages().ByName("ModelCapability")
	assertProtoField(t, model, "provider", 1, protoreflect.EnumKind, false, "")
	assertProtoField(t, model, "model", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, model, "max_context_tokens", 3, protoreflect.Int32Kind, false, "")

	unavailable := common.Messages().ByName("RoutingUnavailableDetail")
	assertProtoField(t, unavailable, "kind", 1, protoreflect.EnumKind, false, "")
	assertProtoField(t, unavailable, "requested", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, unavailable, "available", 3, protoreflect.StringKind, true, "")

	kinds := common.Enums().ByName("RoutingRequirementKind")
	for _, name := range []protoreflect.Name{
		"ROUTING_REQUIREMENT_KIND_PROVIDER",
		"ROUTING_REQUIREMENT_KIND_MODEL",
		"ROUTING_REQUIREMENT_KIND_CONTEXT",
		"ROUTING_REQUIREMENT_KIND_TOOL",
		"ROUTING_REQUIREMENT_KIND_AGENT",
		"ROUTING_REQUIREMENT_KIND_CAPACITY",
		"ROUTING_REQUIREMENT_KIND_EXTERNAL_AGENT_CREDENTIAL",
	} {
		if kinds == nil || kinds.Values().ByName(name) == nil {
			t.Fatalf("RoutingRequirementKind is missing %s", name)
		}
	}

	runtimeFile := turingv1.File_turing_v1_runtime_proto
	capabilities := runtimeFile.Messages().ByName("WorkerCapabilities")
	assertProtoField(t, capabilities, "models", 1, protoreflect.MessageKind, true, "turing.v1.ModelCapability")
	assertProtoField(t, capabilities, "agent_ids", 2, protoreflect.EnumKind, true, "")
	assertProtoField(t, capabilities, "tools", 3, protoreflect.MessageKind, true, "turing.v1.DiscoveredTool")
	assertProtoField(t, capabilities, "max_concurrent_runs", 4, protoreflect.Int32Kind, false, "")
	assertProtoField(t, capabilities, "supports_external_agents", 5, protoreflect.BoolKind, false, "")
	assertProtoField(t, capabilities, "external_agent_credential_refs", 6, protoreflect.StringKind, true, "")

	workerReady := runtimeFile.Messages().ByName("RuntimeWorkerReady")
	assertProtoField(t, workerReady, "registration_id", 6, protoreflect.StringKind, false, "")
	assertProtoField(t, workerReady, "capabilities", 7, protoreflect.MessageKind, false, "turing.v1.WorkerCapabilities")

	updated := runtimeFile.Messages().ByName("RuntimeWorkerCapabilitiesUpdated")
	assertProtoField(t, updated, "worker_id", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, updated, "registration_id", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, updated, "capabilities", 3, protoreflect.MessageKind, false, "turing.v1.WorkerCapabilities")

	update := runtimeFile.Messages().ByName("RuntimeUpdate")
	assertProtoField(t, update, "worker_capabilities_updated", 8, protoreflect.MessageKind, false, "turing.v1.RuntimeWorkerCapabilitiesUpdated")

	accepted := runtimeFile.Messages().ByName("RuntimeWorkerAccepted")
	assertProtoField(t, accepted, "registration_id", 2, protoreflect.StringKind, false, "")

	job := runtimeFile.Messages().ByName("AgentJob")
	assertProtoField(t, job, "required_context_tokens", 15, protoreflect.Int32Kind, false, "")
	assertProtoField(t, job, "minimum_worker_max_concurrent_runs", 16, protoreflect.Int32Kind, false, "")

	chat := turingv1.File_turing_v1_chat_proto.Messages().ByName("SendMessageRequest")
	assertProtoField(t, chat, "requested_tools", 8, protoreflect.StringKind, true, "")
	assertProtoField(t, chat, "required_context_tokens", 9, protoreflect.Int32Kind, false, "")
	assertProtoField(t, chat, "minimum_worker_max_concurrent_runs", 10, protoreflect.Int32Kind, false, "")

	provider := common.Messages().ByName("ProviderConfig")
	assertProtoField(t, provider, "models", 4, protoreflect.MessageKind, true, "turing.v1.ModelCapability")
	agent := common.Messages().ByName("AgentDescriptor")
	assertProtoField(t, agent, "available", 3, protoreflect.BoolKind, false, "")
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
