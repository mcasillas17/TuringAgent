package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
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
	assertProtoField(t, turingv1.File_turing_v1_tools_proto.Messages().ByName("ToolPolicyDecision"), "run_state_version", 7, protoreflect.Int64Kind, false, "")
	assertProtoFieldMembers(t, turingv1.File_turing_v1_tools_proto.Messages().ByName("ToolPolicyDecision"), map[protoreflect.Name]protoreflect.FieldNumber{
		"decision": 1, "tool_call_id": 2, "approval_id": 3, "reason": 4, "terminal_run": 5, "phase": 6, "run_state_version": 7,
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

	assertProtoField(t, runtime.Messages().ByName("AgentJob"), "expected_state_version", 17, protoreflect.Int64Kind, false, "")
	assertProtoField(t, runtime.Messages().ByName("AgentJob"), "assignment_attempt_id", 18, protoreflect.StringKind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeRunCompleted"), "expected_state_version", 6, protoreflect.Int64Kind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeRunFailed"), "failure_origin", 5, protoreflect.EnumKind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeRunFailed"), "automatic_retry_class", 6, protoreflect.EnumKind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeRunFailed"), "expected_state_version", 7, protoreflect.Int64Kind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeCancelledAck"), "observed_state_version", 2, protoreflect.Int64Kind, false, "")
	assertProtoFieldMembers(t, runtime.Messages().ByName("AgentJob"), map[protoreflect.Name]protoreflect.FieldNumber{
		"job_id": 1, "run_id": 2, "session_id": 3, "user_message_id": 4, "assistant_message_id": 5, "agent_id": 6,
		"trace_id": 7, "model_provider": 8, "model": 9, "user_text": 10, "requested_tools": 11, "attempt": 12,
		"skills": 13, "external_agent": 14, "required_context_tokens": 15, "minimum_worker_max_concurrent_runs": 16,
		"expected_state_version": 17, "assignment_attempt_id": 18,
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
	assertProtoField(t, runtime.Messages().ByName("RuntimeCommand"), "approval_resume_accepted", 7, protoreflect.MessageKind, false, "turing.v1.RuntimeApprovalResumeAccepted")
	assertProtoField(t, runtime.Messages().ByName("RuntimeRunCancelled"), "state_version", 3, protoreflect.Int64Kind, false, "")
	assertProtoField(t, runtime.Messages().ByName("RuntimeApprovalUpdated"), "state_version", 4, protoreflect.Int64Kind, false, "")
	assertProtoFieldMembers(t, resumeAccepted, map[protoreflect.Name]protoreflect.FieldNumber{
		"run_id": 1, "approval_id": 2, "state_version": 3, "assignment_attempt_id": 4,
	})
	assertProtoFieldMembers(t, runtime.Messages().ByName("RuntimeCommand"), map[protoreflect.Name]protoreflect.FieldNumber{
		"worker_accepted": 1, "run_assigned": 2, "run_cancelled": 3, "approval_updated": 4,
		"shutdown_requested": 5, "tool_policy_decision": 6, "approval_resume_accepted": 7,
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

	value22 := enum.Values().ByNumber(22)
	switch {
	case value22 == nil:
		if !enum.ReservedRanges().Has(22) {
			t.Fatal("TuringEventType must either reserve 22 or assign it to TURING_EVENT_TYPE_SESSION_DELETED")
		}
		if enum.Values().Len() != len(required) {
			t.Fatalf("TuringEventType has %d values, want %d when 22 is reserved", enum.Values().Len(), len(required))
		}
	case value22.Name() == "TURING_EVENT_TYPE_SESSION_DELETED":
		if enum.ReservedRanges().Has(22) {
			t.Fatal("TuringEventType must not reserve 22 once TURING_EVENT_TYPE_SESSION_DELETED uses it")
		}
		if enum.Values().Len() != len(required)+1 {
			t.Fatalf("TuringEventType has %d values, want %d when TURING_EVENT_TYPE_SESSION_DELETED=22 is present", enum.Values().Len(), len(required)+1)
		}
	default:
		t.Fatalf("TuringEventType value 22 = %s, want TURING_EVENT_TYPE_SESSION_DELETED or reserved", value22.Name())
	}
}

func TestRunFailedRetryableRemainsDeprecatedAtFieldFour(t *testing.T) {
	runFailed := turingv1.File_turing_v1_chat_proto.Messages().ByName("RunFailed")
	field := runFailed.Fields().ByName("retryable")
	if field == nil || field.Number() != 4 || field.Kind() != protoreflect.BoolKind || !field.Options().(*descriptorpb.FieldOptions).GetDeprecated() {
		t.Fatalf("RunFailed.retryable must remain deprecated bool field 4: %v", field)
	}
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
