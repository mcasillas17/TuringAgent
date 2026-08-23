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
	if server.Fields().ByName("token") != nil || server.Fields().ByName("sealed_token") != nil {
		t.Fatal("McpServerDescriptor must not expose server credentials")
	}
	tool := file.Messages().ByName("McpToolDescriptor")
	assertProtoField(t, tool, "present", 5, protoreflect.BoolKind, false, "")

	service := file.Services().ByName("McpRegistryService")
	for _, name := range []protoreflect.Name{
		"ListMcpServers",
		"SetMcpServerEnabled",
		"UpdateMcpToolPolicy",
		"DeleteMcpServer",
		"CallRegisteredMcpTool",
	} {
		if service == nil || service.Methods().ByName(name) == nil {
			t.Fatalf("McpRegistryService.%s is missing", name)
		}
	}
	command := turingv1.File_turing_v1_runtime_proto.Messages().ByName("RuntimeCommand")
	assertProtoField(t, command, "mcp_registry_changed", 7, protoreflect.MessageKind, false, "turing.v1.RuntimeMcpRegistryChanged")
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
