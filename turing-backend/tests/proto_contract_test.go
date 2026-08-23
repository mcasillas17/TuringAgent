package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
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
	assertProtoField(t, request, "exclude_session_id", 4, protoreflect.StringKind, false, "")
	assertProtoField(t, request, "response_format", 5, protoreflect.EnumKind, false, "")

	format := file.Enums().ByName("SearchMessagesResponseFormat")
	if format == nil {
		t.Fatal("SearchMessagesResponseFormat is missing")
	}
	for name, number := range map[protoreflect.Name]protoreflect.EnumNumber{
		"SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED":     0,
		"SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES": 1,
		"SEARCH_MESSAGES_RESPONSE_FORMAT_HITS":            2,
	} {
		value := format.Values().ByName(name)
		if value == nil || value.Number() != number {
			t.Fatalf("%s = %v, want number %d", name, value, number)
		}
	}

	response := file.Messages().ByName("SearchMessagesResponse")
	assertProtoField(t, response, "messages", 1, protoreflect.MessageKind, true, "turing.v1.Message")

	hit := file.Messages().ByName("SearchHit")
	assertProtoField(t, hit, "message", 1, protoreflect.MessageKind, false, "turing.v1.Message")
	assertProtoField(t, hit, "score", 2, protoreflect.DoubleKind, false, "")
	assertProtoField(t, hit, "snippet", 3, protoreflect.StringKind, false, "")
	assertProtoField(t, response, "hits", 2, protoreflect.MessageKind, true, "turing.v1.SearchHit")

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

// legacyFileDescriptor clones the real turing/v1/sessions.proto descriptor
// and removes one field from one message, simulating a caller that only
// knows about the file as it looked before this change landed.
func legacyFileDescriptor(t *testing.T, messageName protoreflect.Name, fieldNumber protoreflect.FieldNumber) protoreflect.FileDescriptor {
	t.Helper()
	cloned := protodesc.ToFileDescriptorProto(turingv1.File_turing_v1_sessions_proto)
	found := false
	for _, message := range cloned.GetMessageType() {
		if message.GetName() != string(messageName) {
			continue
		}
		kept := make([]*descriptorpb.FieldDescriptorProto, 0, len(message.GetField()))
		for _, field := range message.GetField() {
			if field.GetNumber() == int32(fieldNumber) {
				found = true
				continue
			}
			kept = append(kept, field)
		}
		message.Field = kept
	}
	if !found {
		t.Fatalf("field %d not present on %s in the real descriptor", fieldNumber, messageName)
	}
	legacyFile, err := protodesc.NewFile(cloned, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("build legacy file descriptor: %v", err)
	}
	return legacyFile
}

func TestSearchMessagesNewRequestIsReadableByLegacyDescriptor(t *testing.T) {
	legacyFile := legacyFileDescriptor(t, "SearchMessagesRequest", 5)
	legacyDescriptor := legacyFile.Messages().ByName("SearchMessagesRequest")
	if legacyDescriptor.Fields().ByNumber(5) != nil {
		t.Fatal("legacy SearchMessagesRequest descriptor unexpectedly retains field 5")
	}

	newRequest := &turingv1.SearchMessagesRequest{
		Query:            "budget",
		SessionId:        "session-1",
		Limit:            10,
		ExcludeSessionId: "session-2",
		ResponseFormat:   turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
	}
	wire, err := proto.Marshal(newRequest)
	if err != nil {
		t.Fatalf("marshal new request: %v", err)
	}

	legacyMessage := dynamicpb.NewMessage(legacyDescriptor)
	if err := proto.Unmarshal(wire, legacyMessage); err != nil {
		t.Fatalf("unmarshal into legacy request: %v", err)
	}

	if got := legacyMessage.Get(legacyDescriptor.Fields().ByNumber(1)).String(); got != newRequest.GetQuery() {
		t.Fatalf("query = %q, want %q", got, newRequest.GetQuery())
	}
	if got := legacyMessage.Get(legacyDescriptor.Fields().ByNumber(2)).String(); got != newRequest.GetSessionId() {
		t.Fatalf("session_id = %q, want %q", got, newRequest.GetSessionId())
	}
	if got := legacyMessage.Get(legacyDescriptor.Fields().ByNumber(3)).Int(); got != int64(newRequest.GetLimit()) {
		t.Fatalf("limit = %d, want %d", got, newRequest.GetLimit())
	}
	if got := legacyMessage.Get(legacyDescriptor.Fields().ByNumber(4)).String(); got != newRequest.GetExcludeSessionId() {
		t.Fatalf("exclude_session_id = %q, want %q", got, newRequest.GetExcludeSessionId())
	}
}

func TestSearchMessagesNewResponseIsReadableByLegacyDescriptor(t *testing.T) {
	legacyFile := legacyFileDescriptor(t, "SearchMessagesResponse", 2)
	legacyDescriptor := legacyFile.Messages().ByName("SearchMessagesResponse")
	if legacyDescriptor.Fields().ByNumber(2) != nil {
		t.Fatal("legacy SearchMessagesResponse descriptor unexpectedly retains field 2")
	}

	newResponse := &turingv1.SearchMessagesResponse{
		Hits: []*turingv1.SearchHit{{
			Message: &turingv1.Message{MessageId: "hit-1"},
			Score:   0.75,
			Snippet: "the budget was approved",
		}},
	}
	wire, err := proto.Marshal(newResponse)
	if err != nil {
		t.Fatalf("marshal new response: %v", err)
	}

	legacyMessage := dynamicpb.NewMessage(legacyDescriptor)
	if err := proto.Unmarshal(wire, legacyMessage); err != nil {
		t.Fatalf("unmarshal into legacy response: %v", err)
	}
	if len(legacyMessage.GetUnknown()) == 0 {
		t.Fatal("legacy message has no unknown fields, want field 2 (hits) preserved as unknown data")
	}
	if got := legacyMessage.Get(legacyDescriptor.Fields().ByNumber(1)).List().Len(); got != 0 {
		t.Fatalf("legacy messages length = %d, want 0", got)
	}
}

func TestSearchMessagesLegacyResponseIsReadableByNewBindings(t *testing.T) {
	legacyShaped := &turingv1.SearchMessagesResponse{
		Messages: []*turingv1.Message{{MessageId: "legacy"}},
	}
	wire, err := proto.Marshal(legacyShaped)
	if err != nil {
		t.Fatalf("marshal legacy-shaped response: %v", err)
	}

	var newResponse turingv1.SearchMessagesResponse
	if err := proto.Unmarshal(wire, &newResponse); err != nil {
		t.Fatalf("unmarshal into new response bindings: %v", err)
	}
	if len(newResponse.GetMessages()) != 1 || newResponse.GetMessages()[0].GetMessageId() != "legacy" {
		t.Fatalf("messages = %+v, want one message with id 'legacy'", newResponse.GetMessages())
	}
	if len(newResponse.GetHits()) != 0 {
		t.Fatalf("hits = %+v, want empty", newResponse.GetHits())
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
	assertProtoField(t, capabilities, "remote_egress_decision_version", 7, protoreflect.Int32Kind, false, "")

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

func TestRunOutcomeProtoContractUsesApprovedAllocations(t *testing.T) {
	common := turingv1.File_turing_v1_common_proto
	assertProtoEnumValues(t, common.Enums().ByName("RunLifecycle"), map[protoreflect.Name]protoreflect.EnumNumber{
		"RUN_LIFECYCLE_UNSPECIFIED":      0,
		"RUN_LIFECYCLE_UNKNOWN":          1,
		"RUN_LIFECYCLE_QUEUED":           2,
		"RUN_LIFECYCLE_RUNNING":          3,
		"RUN_LIFECYCLE_WAITING_APPROVAL": 4,
		"RUN_LIFECYCLE_RECOVERING":       5,
		"RUN_LIFECYCLE_COMPLETED":        6,
		"RUN_LIFECYCLE_FAILED":           7,
		"RUN_LIFECYCLE_CANCELLED":        8,
	})
	assertProtoEnumValues(t, common.Enums().ByName("RunOutcomeReason"), map[protoreflect.Name]protoreflect.EnumNumber{
		"RUN_OUTCOME_REASON_UNSPECIFIED":              0,
		"RUN_OUTCOME_REASON_UNKNOWN":                  1,
		"RUN_OUTCOME_REASON_NONE":                     2,
		"RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT":     3,
		"RUN_OUTCOME_REASON_USER_CANCELLED":           4,
		"RUN_OUTCOME_REASON_ABANDONED":                5,
		"RUN_OUTCOME_REASON_EXPIRED":                  6,
		"RUN_OUTCOME_REASON_CONTEXT_LIMIT":            7,
		"RUN_OUTCOME_REASON_PROVIDER_FAILURE":         8,
		"RUN_OUTCOME_REASON_TOOL_FAILURE":             9,
		"RUN_OUTCOME_REASON_POLICY_DENIED":            10,
		"RUN_OUTCOME_REASON_RETRIES_EXHAUSTED":        11,
		"RUN_OUTCOME_REASON_RECOVERY_INTERRUPTED":     12,
		"RUN_OUTCOME_REASON_SIDE_EFFECT_UNCERTAIN":    13,
		"RUN_OUTCOME_REASON_APPROVAL_DELIVERY_FAILED": 14,
		"RUN_OUTCOME_REASON_INTERNAL_FAILURE":         15,
		"RUN_OUTCOME_REASON_LEGACY_UNKNOWN":           16,
	})

	runState := common.Messages().ByName("RunState")
	assertProtoField(t, runState, "run_id", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, runState, "user_message_id", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, runState, "assistant_message_id", 3, protoreflect.StringKind, false, "")
	assertProtoField(t, runState, "lifecycle", 4, protoreflect.EnumKind, false, "")
	assertProtoField(t, runState, "outcome_reason", 5, protoreflect.EnumKind, false, "")
	assertProtoField(t, runState, "state_version", 6, protoreflect.Int64Kind, false, "")
	assertProtoField(t, runState, "state_updated_at", 7, protoreflect.MessageKind, false, "google.protobuf.Timestamp")
	assertProtoField(t, runState, "finished_at", 8, protoreflect.MessageKind, false, "google.protobuf.Timestamp")
	assertProtoField(t, runState, "has_displayable_content", 9, protoreflect.BoolKind, false, "")
	assertProtoFieldMembers(t, runState, map[protoreflect.Name]protoreflect.FieldNumber{
		"run_id": 1, "user_message_id": 2, "assistant_message_id": 3, "lifecycle": 4, "outcome_reason": 5,
		"state_version": 6, "state_updated_at": 7, "finished_at": 8, "has_displayable_content": 9,
	})
	assertProtoField(t, common.Messages().ByName("Message"), "run_state", 9, protoreflect.MessageKind, false, "turing.v1.RunState")
	assertProtoFieldMembers(t, common.Messages().ByName("Message"), map[protoreflect.Name]protoreflect.FieldNumber{
		"message_id": 1, "session_id": 2, "run_id": 3, "role": 4, "content": 5, "content_type": 6,
		"sequence": 7, "created_at": 8, "run_state": 9,
	})

	chat := turingv1.File_turing_v1_chat_proto
	for _, messageName := range []protoreflect.Name{"RunQueued", "RunStarted", "ApprovalEvent"} {
		assertProtoField(t, chat.Messages().ByName(messageName), "run_state", 4, protoreflect.MessageKind, false, "turing.v1.RunState")
	}
	assertProtoField(t, chat.Messages().ByName("RunCompleted"), "run_state", 3, protoreflect.MessageKind, false, "turing.v1.RunState")
	assertProtoField(t, chat.Messages().ByName("RunFailed"), "run_state", 5, protoreflect.MessageKind, false, "turing.v1.RunState")
	assertProtoField(t, chat.Messages().ByName("RunCancelled"), "run_state", 3, protoreflect.MessageKind, false, "turing.v1.RunState")
	assertProtoField(t, chat.Messages().ByName("RunStateChanged"), "run_state", 1, protoreflect.MessageKind, false, "turing.v1.RunState")
	assertProtoField(t, chat.Messages().ByName("ChatStreamEvent"), "run_state_changed", 27, protoreflect.MessageKind, false, "turing.v1.RunStateChanged")
	assertProtoOneofMember(t, chat.Messages().ByName("ChatStreamEvent"), "run_state_changed", "event")
	for messageName, fields := range map[protoreflect.Name]map[protoreflect.Name]protoreflect.FieldNumber{
		"RunQueued":       {"run_id": 1, "job_id": 2, "trace_id": 3, "run_state": 4},
		"RunStarted":      {"run_id": 1, "job_id": 2, "attempt": 3, "run_state": 4},
		"ApprovalEvent":   {"approval_id": 1, "tool_name": 2, "args_summary": 3, "run_state": 4},
		"RunCompleted":    {"run_id": 1, "assistant_message_id": 2, "run_state": 3},
		"RunFailed":       {"run_id": 1, "code": 2, "message": 3, "retryable": 4, "run_state": 5},
		"RunCancelled":    {"run_id": 1, "reason": 2, "run_state": 3},
		"RunStateChanged": {"run_state": 1},
		"ChatStreamEvent": {
			"session_id": 1, "run_id": 2, "trace_id": 3, "sequence": 4, "run_queued": 10, "run_started": 11,
			"message_started": 12, "token_delta": 13, "tool_call_started": 14, "tool_call_completed": 15,
			"tool_call_failed": 16, "approval_requested": 17, "approval_approved": 18, "approval_denied": 19,
			"approval_expired": 20, "approval_consumed": 21, "message_completed": 22, "run_completed": 23,
			"run_failed": 24, "run_cancelled": 25, "persisted_event": 26, "run_state_changed": 27,
		},
	} {
		assertProtoFieldMembers(t, chat.Messages().ByName(messageName), fields)
	}

	events := turingv1.File_turing_v1_events_proto
	assertProtoField(t, events.Messages().ByName("TuringEvent"), "run_state", 9, protoreflect.MessageKind, false, "turing.v1.RunState")
	assertProtoFieldMembers(t, events.Messages().ByName("TuringEvent"), map[protoreflect.Name]protoreflect.FieldNumber{
		"event_id": 1, "session_id": 2, "run_id": 3, "trace_id": 4, "sequence": 5, "type": 6,
		"created_at": 7, "payload": 8, "run_state": 9,
	})
	assertProtoField(t, turingv1.File_turing_v1_tools_proto.Messages().ByName("ToolPolicyDecision"), "read_only", 8, protoreflect.BoolKind, false, "")
	assertProtoField(t, turingv1.File_turing_v1_tools_proto.Messages().ByName("ToolPolicyDecision"), "run_state_version", 9, protoreflect.Int64Kind, false, "")
	assertProtoFieldMembers(t, turingv1.File_turing_v1_tools_proto.Messages().ByName("ToolPolicyDecision"), map[protoreflect.Name]protoreflect.FieldNumber{
		"decision": 1, "tool_call_id": 2, "approval_id": 3, "reason": 4, "terminal_run": 5, "phase": 6,
		"provenance_token": 7, "read_only": 8, "run_state_version": 9,
	})
}

func TestRunOutcomeEnumsHaveUnspecifiedAndUnknownValues(t *testing.T) {
	common := turingv1.File_turing_v1_common_proto
	for enumName, values := range map[protoreflect.Name]map[protoreflect.Name]protoreflect.EnumNumber{
		"RunLifecycle": {
			"RUN_LIFECYCLE_UNSPECIFIED": 0,
			"RUN_LIFECYCLE_UNKNOWN":     1,
		},
		"RunOutcomeReason": {
			"RUN_OUTCOME_REASON_UNSPECIFIED": 0,
			"RUN_OUTCOME_REASON_UNKNOWN":     1,
		},
	} {
		enum := common.Enums().ByName(enumName)
		if enum == nil {
			t.Fatalf("%s descriptor is missing", enumName)
		}
		for name, number := range values {
			value := enum.Values().ByName(name)
			if value == nil || value.Number() != number {
				t.Fatalf("%s.%s = %v, want %d", enumName, name, value, number)
			}
		}
	}

	runtime := turingv1.File_turing_v1_runtime_proto
	for enumName, values := range map[protoreflect.Name]map[protoreflect.Name]protoreflect.EnumNumber{
		"FailureOrigin": {
			"FAILURE_ORIGIN_UNSPECIFIED": 0,
			"FAILURE_ORIGIN_UNKNOWN":     1,
		},
		"AutomaticRetryClass": {
			"AUTOMATIC_RETRY_CLASS_UNSPECIFIED": 0,
			"AUTOMATIC_RETRY_CLASS_UNKNOWN":     1,
		},
	} {
		enum := runtime.Enums().ByName(enumName)
		if enum == nil {
			t.Fatalf("%s descriptor is missing", enumName)
		}
		for name, number := range values {
			value := enum.Values().ByName(name)
			if value == nil || value.Number() != number {
				t.Fatalf("%s.%s = %v, want %d", enumName, name, value, number)
			}
		}
	}
}

func TestRuntimeApprovalResumeProtoContractUsesApprovedAllocations(t *testing.T) {
	runtime := turingv1.File_turing_v1_runtime_proto
	assertProtoEnumValues(t, runtime.Enums().ByName("FailureOrigin"), map[protoreflect.Name]protoreflect.EnumNumber{
		"FAILURE_ORIGIN_UNSPECIFIED":            0,
		"FAILURE_ORIGIN_UNKNOWN":                1,
		"FAILURE_ORIGIN_CONTEXT_ASSEMBLY":       2,
		"FAILURE_ORIGIN_EXTERNAL_PROVIDER":      3,
		"FAILURE_ORIGIN_PROVIDER_CONFIGURATION": 4,
		"FAILURE_ORIGIN_PROVIDER_PROTOCOL":      5,
		"FAILURE_ORIGIN_PROVIDER_TRANSPORT":     6,
		"FAILURE_ORIGIN_PROVIDER_OUTPUT_GUARD":  7,
		"FAILURE_ORIGIN_TOOL_INFRASTRUCTURE":    8,
		"FAILURE_ORIGIN_TOOL_EXECUTION":         9,
		"FAILURE_ORIGIN_TOOL_GUARD":             10,
		"FAILURE_ORIGIN_TOOL_POLICY":            11,
		"FAILURE_ORIGIN_APPROVAL_TRANSPORT":     12,
		"FAILURE_ORIGIN_APPROVAL_EXPIRY":        13,
		"FAILURE_ORIGIN_AUTOMATION_POLICY":      14,
		"FAILURE_ORIGIN_WORKER_RUNTIME":         15,
		"FAILURE_ORIGIN_DISPATCH":               16,
		"FAILURE_ORIGIN_RECOVERY":               17,
		"FAILURE_ORIGIN_ORCHESTRATOR_INTERNAL":  18,
		"FAILURE_ORIGIN_CLIENT_LIFECYCLE":       19,
	})
	assertProtoEnumValues(t, runtime.Enums().ByName("AutomaticRetryClass"), map[protoreflect.Name]protoreflect.EnumNumber{
		"AUTOMATIC_RETRY_CLASS_UNSPECIFIED":        0,
		"AUTOMATIC_RETRY_CLASS_UNKNOWN":            1,
		"AUTOMATIC_RETRY_CLASS_NEVER":              2,
		"AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT": 3,
	})

	assertProtoField(t, runtime.Messages().ByName("AgentJob"), "expected_state_version", 19, protoreflect.Int64Kind, false, "")
	assertProtoField(t, runtime.Messages().ByName("AgentJob"), "assignment_attempt_id", 20, protoreflect.StringKind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeRunCompleted"), "expected_state_version", 6, protoreflect.Int64Kind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeRunFailed"), "failure_origin", 5, protoreflect.EnumKind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeRunFailed"), "automatic_retry_class", 6, protoreflect.EnumKind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeRunFailed"), "expected_state_version", 7, protoreflect.Int64Kind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeCancelledAck"), "observed_state_version", 2, protoreflect.Int64Kind, false, "")
	assertProtoFieldMembers(t, runtime.Messages().ByName("AgentJob"), map[protoreflect.Name]protoreflect.FieldNumber{
		"job_id": 1, "run_id": 2, "session_id": 3, "user_message_id": 4, "assistant_message_id": 5, "agent_id": 6,
		"trace_id": 7, "model_provider": 8, "model": 9, "user_text": 10, "requested_tools": 11, "attempt": 12,
		"skills": 13, "external_agent": 14, "required_context_tokens": 15, "minimum_worker_max_concurrent_runs": 16,
		"egress_decision": 17, "selected_tools": 18, "expected_state_version": 19, "assignment_attempt_id": 20,
	})
	assertProtoFieldMembers(t, runtime.Messages().ByName("RuntimeRunCompleted"), map[protoreflect.Name]protoreflect.FieldNumber{
		"run_id": 1, "assistant_message_id": 2, "content": 3, "usage": 4, "token_usage": 5, "expected_state_version": 6,
	})
	assertProtoFieldMembers(t, runtime.Messages().ByName("RuntimeRunFailed"), map[protoreflect.Name]protoreflect.FieldNumber{
		"run_id": 1, "code": 2, "message": 3, "retryable": 4, "failure_origin": 5, "automatic_retry_class": 6, "expected_state_version": 7,
	})
	assertProtoFieldMembers(t, runtime.Messages().ByName("RuntimeCancelledAck"), map[protoreflect.Name]protoreflect.FieldNumber{
		"run_id": 1, "observed_state_version": 2,
	})

	resumeReady := runtime.Messages().ByName("RuntimeApprovalResumeReady")
	assertProtoField(t, resumeReady, "run_id", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, resumeReady, "approval_id", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, resumeReady, "expected_state_version", 3, protoreflect.Int64Kind, false, "")
	assertProtoField(t, resumeReady, "assignment_attempt_id", 4, protoreflect.StringKind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeUpdate"), "approval_resume_ready", 9, protoreflect.MessageKind, false, "turing.v1.RuntimeApprovalResumeReady")
	assertProtoOneofMember(t, runtime.Messages().ByName("RuntimeUpdate"), "approval_resume_ready", "update")
	assertProtoFieldMembers(t, resumeReady, map[protoreflect.Name]protoreflect.FieldNumber{
		"run_id": 1, "approval_id": 2, "expected_state_version": 3, "assignment_attempt_id": 4,
	})
	assertProtoFieldMembers(t, runtime.Messages().ByName("RuntimeUpdate"), map[protoreflect.Name]protoreflect.FieldNumber{
		"worker_ready": 1, "heartbeat": 2, "event": 3, "tool_beacon": 4, "run_completed": 5, "run_failed": 6,
		"run_cancelled_ack": 7, "worker_capabilities_updated": 8, "approval_resume_ready": 9,
	})

	resumeAccepted := runtime.Messages().ByName("RuntimeApprovalResumeAccepted")
	assertProtoField(t, resumeAccepted, "run_id", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, resumeAccepted, "approval_id", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, resumeAccepted, "state_version", 3, protoreflect.Int64Kind, false, "")
	assertProtoField(t, resumeAccepted, "assignment_attempt_id", 4, protoreflect.StringKind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeCommand"), "approval_resume_accepted", 8, protoreflect.MessageKind, false, "turing.v1.RuntimeApprovalResumeAccepted")
	assertProtoOneofMember(t, runtime.Messages().ByName("RuntimeCommand"), "approval_resume_accepted", "command")
	assertProtoField(t, runtime.Messages().ByName("RuntimeRunCancelled"), "state_version", 3, protoreflect.Int64Kind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeApprovalUpdated"), "state_version", 4, protoreflect.Int64Kind, false, "")
	assertProtoFieldMembers(t, resumeAccepted, map[protoreflect.Name]protoreflect.FieldNumber{
		"run_id": 1, "approval_id": 2, "state_version": 3, "assignment_attempt_id": 4,
	})
	assertProtoFieldMembers(t, runtime.Messages().ByName("RuntimeCommand"), map[protoreflect.Name]protoreflect.FieldNumber{
		"worker_accepted": 1, "run_assigned": 2, "run_cancelled": 3, "approval_updated": 4,
		"shutdown_requested": 5, "tool_policy_decision": 6, "mcp_registry_changed": 7, "approval_resume_accepted": 8,
	})
	assertProtoFieldMembers(t, runtime.Messages().ByName("RuntimeRunCancelled"), map[protoreflect.Name]protoreflect.FieldNumber{
		"run_id": 1, "reason": 2, "state_version": 3,
	})
	assertProtoFieldMembers(t, runtime.Messages().ByName("RuntimeApprovalUpdated"), map[protoreflect.Name]protoreflect.FieldNumber{
		"approval_id": 1, "approval_token": 2, "status": 3, "state_version": 4,
	})
}

func TestRunStateChangedReservesEventTypeTwentyThree(t *testing.T) {
	events := turingv1.File_turing_v1_events_proto
	enum := events.Enums().ByName("TuringEventType")
	if enum == nil {
		t.Fatal("enum descriptor is missing")
	}
	required := map[protoreflect.Name]protoreflect.EnumNumber{
		"TURING_EVENT_TYPE_UNSPECIFIED":             0,
		"TURING_EVENT_TYPE_MESSAGE_STARTED":         1,
		"TURING_EVENT_TYPE_MESSAGE_DELTA":           2,
		"TURING_EVENT_TYPE_MESSAGE_COMPLETED":       3,
		"TURING_EVENT_TYPE_AGENT_RUN_QUEUED":        4,
		"TURING_EVENT_TYPE_AGENT_RUN_STARTED":       5,
		"TURING_EVENT_TYPE_AGENT_RUN_STEP":          6,
		"TURING_EVENT_TYPE_AGENT_RUN_COMPLETED":     7,
		"TURING_EVENT_TYPE_AGENT_RUN_FAILED":        8,
		"TURING_EVENT_TYPE_AGENT_RUN_CANCELLED":     9,
		"TURING_EVENT_TYPE_TOOL_CALL_STARTED":       10,
		"TURING_EVENT_TYPE_TOOL_CALL_COMPLETED":     11,
		"TURING_EVENT_TYPE_TOOL_CALL_FAILED":        12,
		"TURING_EVENT_TYPE_TOOL_CALL_DENIED":        13,
		"TURING_EVENT_TYPE_APPROVAL_REQUESTED":      14,
		"TURING_EVENT_TYPE_APPROVAL_APPROVED":       15,
		"TURING_EVENT_TYPE_APPROVAL_DENIED":         16,
		"TURING_EVENT_TYPE_APPROVAL_EXPIRED":        17,
		"TURING_EVENT_TYPE_APPROVAL_CONSUMED":       18,
		"TURING_EVENT_TYPE_ERROR":                   19,
		"TURING_EVENT_TYPE_SYSTEM":                  20,
		"TURING_EVENT_TYPE_SESSION_UPDATED":         21,
		"TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED": 23,
	}
	for name, number := range required {
		value := enum.Values().ByName(name)
		if value == nil || value.Number() != number {
			t.Fatalf("TuringEventType.%s = %v, want %d", name, value, number)
		}
	}

	// TUR-009 allocates 23 only. Value 22 belongs to TUR-004 and must stay
	// reserved until that work lands, whichever order the two features merge in.
	// TUR-004 is expected to name it TURING_EVENT_TYPE_SESSION_DELETED; if that
	// allocation changes, update this expected name rather than freeing 22.
	const tur004Value22 = protoreflect.Name("TURING_EVENT_TYPE_SESSION_DELETED")

	value22 := enum.Values().ByNumber(22)
	switch {
	case value22 == nil:
		if !enum.ReservedRanges().Has(22) {
			t.Fatalf("TuringEventType value 22 is neither reserved nor allocated; TUR-009 does not allocate 22, so it must stay reserved until TUR-004 (expected %s) uses it", tur004Value22)
		}
		if enum.Values().Len() != len(required) {
			t.Fatalf("TuringEventType has %d values, want %d while 22 is reserved", enum.Values().Len(), len(required))
		}
	case value22.Name() == tur004Value22:
		if enum.ReservedRanges().Has(22) {
			t.Fatalf("TuringEventType reserves 22 while %s also allocates it; drop the reservation now that TUR-004 has landed", value22.Name())
		}
		if enum.Values().Len() != len(required)+1 {
			t.Fatalf("TuringEventType has %d values, want %d once TUR-004 allocates 22 as %s", enum.Values().Len(), len(required)+1, value22.Name())
		}
	default:
		t.Fatalf("TuringEventType value 22 = %s, which TUR-009 does not allocate; if TUR-004 changed its value 22, update this guard's expected name (currently %s)", value22.Name(), tur004Value22)
	}
}

func TestRunFailedRetryableRemainsDeprecatedAtFieldFour(t *testing.T) {
	runFailed := turingv1.File_turing_v1_chat_proto.Messages().ByName("RunFailed")
	field := runFailed.Fields().ByName("retryable")
	if field == nil || field.Number() != 4 || field.Kind() != protoreflect.BoolKind || !field.Options().(*descriptorpb.FieldOptions).GetDeprecated() {
		t.Fatalf("RunFailed.retryable must remain deprecated bool field 4: %v", field)
	}
}

func TestRemoteEgressProtoContract(t *testing.T) {
	common := turingv1.File_turing_v1_common_proto
	categories := common.Enums().ByName("EgressDataCategory")
	for _, name := range []protoreflect.Name{
		"EGRESS_DATA_CATEGORY_CURRENT_MESSAGE",
		"EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY",
		"EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL",
		"EGRESS_DATA_CATEGORY_MEMORY_PROFILE",
		"EGRESS_DATA_CATEGORY_SKILL_CONTENT",
		"EGRESS_DATA_CATEGORY_TOOL_SCHEMAS",
		"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS",
		"EGRESS_DATA_CATEGORY_TOOL_RESULTS",
		"EGRESS_DATA_CATEGORY_ATTACHMENTS",
	} {
		if categories == nil || categories.Values().ByName(name) == nil {
			t.Fatalf("EgressDataCategory is missing %s", name)
		}

	}

	disclosure := common.Messages().ByName("RemoteEgressDisclosure")
	assertProtoField(t, disclosure, "challenge", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, disclosure, "provider", 2, protoreflect.EnumKind, false, "")
	assertProtoField(t, disclosure, "model", 3, protoreflect.StringKind, false, "")
	assertProtoField(t, disclosure, "endpoint", 4, protoreflect.StringKind, false, "")
	assertProtoField(t, disclosure, "endpoint_host", 5, protoreflect.StringKind, false, "")
	assertProtoField(t, disclosure, "external_agent_id", 6, protoreflect.StringKind, false, "")
	assertProtoField(t, disclosure, "data_categories", 7, protoreflect.EnumKind, true, "")
	assertProtoField(t, disclosure, "expires_at", 8, protoreflect.MessageKind, false, "google.protobuf.Timestamp")
	assertProtoField(t, disclosure, "remote_mcp_servers", 9, protoreflect.MessageKind, true, "turing.v1.RemoteMcpEgressDestination")
	assertProtoField(t, disclosure, "selected_tools", 10, protoreflect.StringKind, true, "")
	assertProtoField(t, disclosure, "integration_endpoints", 11, protoreflect.MessageKind, true, "turing.v1.IntegrationEgressDestination")
	assertProtoField(t, disclosure, "skills", 12, protoreflect.MessageKind, true, "turing.v1.SkillEgressDisclosure")

	skill := common.Messages().ByName("SkillEgressDisclosure")
	assertProtoField(t, skill, "skill_id", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, skill, "display_name", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, skill, "body_may_be_sent", 3, protoreflect.BoolKind, false, "")

	remoteMCP := common.Messages().ByName("RemoteMcpEgressDestination")
	assertProtoField(t, remoteMCP, "server_name", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, remoteMCP, "endpoint", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, remoteMCP, "endpoint_host", 3, protoreflect.StringKind, false, "")

	consent := common.Messages().ByName("RemoteEgressConsent")
	assertProtoField(t, consent, "challenge", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, consent, "acknowledged_data_categories", 2, protoreflect.EnumKind, true, "")
	assertProtoField(t, consent, "acknowledged", 3, protoreflect.BoolKind, false, "")

	decision := common.Messages().ByName("RunEgressDecision")
	assertProtoField(t, decision, "decision_id", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, decision, "version", 2, protoreflect.Int32Kind, false, "")
	assertProtoField(t, decision, "provider", 3, protoreflect.EnumKind, false, "")
	assertProtoField(t, decision, "model", 4, protoreflect.StringKind, false, "")
	assertProtoField(t, decision, "endpoint", 5, protoreflect.StringKind, false, "")
	assertProtoField(t, decision, "endpoint_host", 6, protoreflect.StringKind, false, "")
	assertProtoField(t, decision, "external_agent_id", 7, protoreflect.StringKind, false, "")
	assertProtoField(t, decision, "data_categories", 8, protoreflect.EnumKind, true, "")
	assertProtoField(t, decision, "consent_granted_at", 9, protoreflect.MessageKind, false, "google.protobuf.Timestamp")
	assertProtoField(t, decision, "challenge_fingerprint", 10, protoreflect.StringKind, false, "")
	assertProtoField(t, decision, "selected_tools", 11, protoreflect.StringKind, true, "")
	assertProtoField(t, decision, "skill_snapshot_fingerprint", 12, protoreflect.StringKind, false, "")
	assertProtoField(t, decision, "recall_applicable", 13, protoreflect.BoolKind, false, "")
	assertProtoField(t, decision, "memory_profile_applicable", 14, protoreflect.BoolKind, false, "")
	assertProtoField(t, decision, "external_credential_ref_hash", 15, protoreflect.StringKind, false, "")
	assertProtoField(t, decision, "request_digest", 16, protoreflect.StringKind, false, "")
	assertProtoField(t, decision, "remote_mcp_servers", 17, protoreflect.MessageKind, true, "turing.v1.RemoteMcpEgressDestination")

	provider := common.Messages().ByName("ProviderConfig")
	assertProtoField(t, provider, "remote_endpoint", 5, protoreflect.StringKind, false, "")
	assertProtoField(t, provider, "requires_per_run_consent", 6, protoreflect.BoolKind, false, "")

	chatFile := turingv1.File_turing_v1_chat_proto
	send := chatFile.Messages().ByName("SendMessageRequest")
	assertProtoField(t, send, "remote_egress_consent", 11, protoreflect.MessageKind, false, "turing.v1.RemoteEgressConsent")
	prepare := chatFile.Messages().ByName("PrepareRemoteEgressRequest")
	assertProtoField(t, prepare, "session_id", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, prepare, "content", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, prepare, "idempotency_key", 7, protoreflect.StringKind, false, "")
	response := chatFile.Messages().ByName("PrepareRemoteEgressResponse")
	assertProtoField(t, response, "disclosure", 1, protoreflect.MessageKind, false, "turing.v1.RemoteEgressDisclosure")
	service := chatFile.Services().ByName("ChatService")
	method := service.Methods().ByName("PrepareRemoteEgress")
	if method == nil {
		t.Fatal("ChatService.PrepareRemoteEgress is missing")
	}
	if got := string(method.Input().FullName()); got != "turing.v1.PrepareRemoteEgressRequest" {
		t.Fatalf("PrepareRemoteEgress input = %q", got)
	}
	if got := string(method.Output().FullName()); got != "turing.v1.PrepareRemoteEgressResponse" {
		t.Fatalf("PrepareRemoteEgress output = %q", got)
	}

	job := turingv1.File_turing_v1_runtime_proto.Messages().ByName("AgentJob")
	assertProtoField(t, job, "egress_decision", 17, protoreflect.MessageKind, false, "turing.v1.RunEgressDecision")
	assertProtoField(t, job, "selected_tools", 18, protoreflect.StringKind, true, "")
	externalTarget := turingv1.File_turing_v1_runtime_proto.Messages().ByName("ExternalAgentTarget")
	assertProtoField(t, externalTarget, "agent_id", 4, protoreflect.StringKind, false, "")

	auditPayload := turingv1.File_turing_v1_audit_proto.Messages().ByName("AuditPayload")
	assertProtoField(t, auditPayload, "endpoint_host", 22, protoreflect.StringKind, false, "")
	assertProtoField(t, auditPayload, "egress_data_categories", 23, protoreflect.EnumKind, true, "")
	assertProtoField(t, auditPayload, "egress_decision_version", 24, protoreflect.Int32Kind, false, "")
	assertProtoField(t, auditPayload, "egress_consent_granted_at", 25, protoreflect.MessageKind, false, "google.protobuf.Timestamp")
}

func TestMCPRegistryProtoContract(t *testing.T) {
	file := turingv1.File_turing_v1_mcp_proto
	server := file.Messages().ByName("McpServerDescriptor")
	assertProtoField(t, server, "server_id", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, server, "name", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, server, "transport", 3, protoreflect.StringKind, false, "")
	assertProtoField(t, server, "url", 4, protoreflect.StringKind, false, "")
	assertProtoField(t, server, "tier", 5, protoreflect.EnumKind, false, "")
	assertProtoField(t, server, "enabled", 6, protoreflect.BoolKind, false, "")
	assertProtoField(t, server, "liveness", 7, protoreflect.EnumKind, false, "")
	assertProtoField(t, server, "status_message", 8, protoreflect.StringKind, false, "")
	assertProtoField(t, server, "sandbox_confined", 9, protoreflect.BoolKind, false, "")
	assertProtoField(t, server, "tools", 10, protoreflect.MessageKind, true, "turing.v1.McpToolDescriptor")
	tool := file.Messages().ByName("McpToolDescriptor")
	assertProtoField(t, tool, "present", 5, protoreflect.BoolKind, false, "")

	listResp := file.Messages().ByName("ListMcpServersResponse")
	assertProtoField(t, listResp, "servers", 1, protoreflect.MessageKind, true, "turing.v1.McpServerDescriptor")
	assertProtoField(t, listResp, "unsupported", 2, protoreflect.MessageKind, true, "turing.v1.UnsupportedMcpServer")
	// registry_degraded/registry_degradation_reason are additive fields 3
	// and 4: an explicit, structured signal for a bounded/degraded
	// registry read (see repository.MCPRegistrySnapshot), replacing a
	// synthetic "_registry"-named Unsupported entry rather than
	// overloading that list with a non-per-entry systemic status.
	assertProtoField(t, listResp, "registry_degraded", 3, protoreflect.BoolKind, false, "")
	assertProtoField(t, listResp, "registry_degradation_reason", 4, protoreflect.StringKind, false, "")

	service := file.Services().ByName("McpRegistryService")
	for _, name := range []protoreflect.Name{
		"ListMcpServers",
		"SetMcpServerEnabled",
		"UpdateMcpToolPolicy",
		"UpdateToolPolicyByName",
		"ListPseudoServerTools",
		"DeleteMcpServer",
		"CallRegisteredMcpTool",
		"RegisterMcpServer",
		"ReimportMcpJson",
		"RotateMcpServerToken",
	} {
		if service == nil || service.Methods().ByName(name) == nil {
			t.Fatalf("McpRegistryService.%s is missing", name)
		}
	}
	if service.Methods().ByName("RegisterMcpServer").Input().FullName() != "turing.v1.RegisterMcpServerRequest" ||
		service.Methods().ByName("RegisterMcpServer").Output().FullName() != "turing.v1.McpServerDescriptor" {
		t.Fatal("McpRegistryService.RegisterMcpServer must accept RegisterMcpServerRequest and return McpServerDescriptor")
	}
	if service.Methods().ByName("ReimportMcpJson").Input().FullName() != "turing.v1.ReimportMcpJsonRequest" ||
		service.Methods().ByName("ReimportMcpJson").Output().FullName() != "turing.v1.ReimportMcpJsonResponse" {
		t.Fatal("McpRegistryService.ReimportMcpJson must accept ReimportMcpJsonRequest and return ReimportMcpJsonResponse")
	}
	if service.Methods().ByName("RotateMcpServerToken").Input().FullName() != "turing.v1.RotateMcpServerTokenRequest" ||
		service.Methods().ByName("RotateMcpServerToken").Output().FullName() != "turing.v1.McpServerDescriptor" {
		t.Fatal("McpRegistryService.RotateMcpServerToken must accept RotateMcpServerTokenRequest and return McpServerDescriptor")
	}
	command := turingv1.File_turing_v1_runtime_proto.Messages().ByName("RuntimeCommand")
	assertProtoField(t, command, "mcp_registry_changed", 7, protoreflect.MessageKind, false, "turing.v1.RuntimeMcpRegistryChanged")

	registerReq := file.Messages().ByName("RegisterMcpServerRequest")
	assertProtoField(t, registerReq, "name", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, registerReq, "url", 2, protoreflect.StringKind, false, "")
	// bearer_token stays at 3 and tier is appended at 4: the tier-less form
	// of this request shipped on main first, so moving bearer_token would
	// break every client already built against it on the wire.
	assertProtoField(t, registerReq, "bearer_token", 3, protoreflect.StringKind, false, "")
	assertProtoField(t, registerReq, "tier", 4, protoreflect.EnumKind, false, "")

	reimportReq := file.Messages().ByName("ReimportMcpJsonRequest")
	if reimportReq == nil {
		t.Fatal("ReimportMcpJsonRequest is missing")
	}
	if reimportReq.Fields().Len() != 0 {
		t.Fatal("ReimportMcpJsonRequest must remain empty")
	}

	reimportResp := file.Messages().ByName("ReimportMcpJsonResponse")
	assertProtoField(t, reimportResp, "imported", 1, protoreflect.StringKind, true, "")
	// unsupported keeps main's number 2 and skipped is appended at 3, for
	// the same wire-compatibility reason as RegisterMcpServerRequest above.
	assertProtoField(t, reimportResp, "unsupported", 2, protoreflect.MessageKind, true, "turing.v1.UnsupportedMcpServer")
	assertProtoField(t, reimportResp, "skipped", 3, protoreflect.StringKind, true, "")

	rotateReq := file.Messages().ByName("RotateMcpServerTokenRequest")
	assertProtoField(t, rotateReq, "server_id", 1, protoreflect.StringKind, false, "")
	assertProtoField(t, rotateReq, "bearer_token", 2, protoreflect.StringKind, false, "")

	// No MCP management response message may leak the bearer token or any
	// sealed/ciphertext form of it back to the client.
	forbiddenCredentialFields := []protoreflect.Name{"bearer_token", "sealed_token", "ciphertext", "token"}
	for _, respName := range []string{"McpServerDescriptor", "ReimportMcpJsonResponse"} {
		resp := file.Messages().ByName(protoreflect.Name(respName))
		if resp == nil {
			t.Fatalf("%s is missing", respName)
		}
		for _, forbidden := range forbiddenCredentialFields {
			if resp.Fields().ByName(forbidden) != nil {
				t.Fatalf("%s must not expose a %s field", respName, forbidden)
			}
		}
	}
}

// TestMCPRegistryAuditPayloadProtoContract pins the exact field numbers and
// kinds of the AuditPayload fields the MCP registry audit projection reads.
// These are additive fields 26-35 appended after the existing 25 (see
// TestRemoteEgressProtoContract for 22-25): server_name (3) and tool_name (2)
// are deliberately reused rather than duplicated, so only the ten new,
// MCP-specific fields are asserted here. A wrong number/kind here would let
// the wire contract silently drift out from under audit/service.go's
// applyAuditActionPolicy, which reads these fields by exact name.
func TestMCPRegistryAuditPayloadProtoContract(t *testing.T) {
	auditPayload := turingv1.File_turing_v1_audit_proto.Messages().ByName("AuditPayload")
	assertProtoField(t, auditPayload, "mcp_server_tier", 26, protoreflect.StringKind, false, "")
	assertProtoField(t, auditPayload, "mcp_server_url", 27, protoreflect.StringKind, false, "")
	assertProtoField(t, auditPayload, "adopted", 28, protoreflect.BoolKind, false, "")
	assertProtoField(t, auditPayload, "token_configured", 29, protoreflect.BoolKind, false, "")
	assertProtoField(t, auditPayload, "remote_discovery_attempted", 30, protoreflect.BoolKind, false, "")
	assertProtoField(t, auditPayload, "discovery_succeeded", 31, protoreflect.BoolKind, false, "")
	assertProtoField(t, auditPayload, "imported_servers", 32, protoreflect.Int64Kind, false, "")
	assertProtoField(t, auditPayload, "skipped_servers", 33, protoreflect.Int64Kind, false, "")
	assertProtoField(t, auditPayload, "refused_servers", 34, protoreflect.Int64Kind, false, "")
	assertProtoField(t, auditPayload, "tool_policy", 35, protoreflect.StringKind, false, "")

	// None of the ten new fields may collide with (or replace) the reused
	// server_name/tool_name fields at their existing numbers.
	assertProtoField(t, auditPayload, "tool_name", 2, protoreflect.StringKind, false, "")
	assertProtoField(t, auditPayload, "server_name", 3, protoreflect.StringKind, false, "")
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

func assertProtoOneofMember(t *testing.T, message protoreflect.MessageDescriptor, name protoreflect.Name, oneof protoreflect.Name) {
	t.Helper()
	if message == nil {
		t.Fatal("message descriptor is missing")
	}

	field := message.Fields().ByName(name)
	if field == nil {
		t.Fatalf("%s.%s is missing", message.Name(), name)
	}
	containing := field.ContainingOneof()
	if containing == nil {
		t.Fatalf("%s.%s is not in a oneof, want oneof %s", message.Name(), name, oneof)
	}
	if containing.IsSynthetic() {
		t.Fatalf("%s.%s is in synthetic oneof %s, want declared oneof %s", message.Name(), name, containing.Name(), oneof)
	}
	if containing.Name() != oneof {
		t.Fatalf("%s.%s is in oneof %s, want %s", message.Name(), name, containing.Name(), oneof)
	}
}

func assertProtoEnumValues(t *testing.T, enum protoreflect.EnumDescriptor, want map[protoreflect.Name]protoreflect.EnumNumber) {
	t.Helper()
	if enum == nil {
		t.Fatal("enum descriptor is missing")
	}
	if enum.Values().Len() != len(want) {
		t.Fatalf("%s has %d values, want %d", enum.Name(), enum.Values().Len(), len(want))
	}
	for name, number := range want {
		value := enum.Values().ByName(name)
		if value == nil || value.Number() != number {
			t.Fatalf("%s.%s = %v, want %d", enum.Name(), name, value, number)
		}
	}
}

func assertProtoFieldMembers(t *testing.T, message protoreflect.MessageDescriptor, want map[protoreflect.Name]protoreflect.FieldNumber) {
	t.Helper()
	if message == nil {
		t.Fatal("message descriptor is missing")
	}
	if message.Fields().Len() != len(want) {
		t.Fatalf("%s has %d fields, want %d", message.Name(), message.Fields().Len(), len(want))
	}
	for name, number := range want {
		field := message.Fields().ByName(name)
		if field == nil || field.Number() != number {
			t.Fatalf("%s.%s = %v, want field %d", message.Name(), name, field, number)
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
