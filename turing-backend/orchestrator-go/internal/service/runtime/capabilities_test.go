package runtime

import (
	"context"
	"slices"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestEgressToolNamesUsesIntersectionAcrossEligibleWorkers(t *testing.T) {
	h := newHarness(t)
	firstCapabilities := modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		"gpt-4o-mini",
		8192,
		1,
	)
	firstCapabilities.Tools = []*turingv1.DiscoveredTool{
		{ServerName: "system", ToolName: "common", Schema: &structpb.Struct{}},
		{ServerName: "system", ToolName: "first_only", Schema: &structpb.Struct{}},
	}

	first := connectWorkerCapabilities(t, h, "worker-egress-first", "registration-egress-first", firstCapabilities)
	defer func() { _ = first.CloseSend() }()
	secondCapabilities := modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		"gpt-4o-mini",
		8192,
		1,
	)
	secondCapabilities.Tools = []*turingv1.DiscoveredTool{
		{ServerName: "system", ToolName: "common", Schema: &structpb.Struct{}},
		{ServerName: "system", ToolName: "second_only", Schema: &structpb.Struct{}},
	}
	second := connectWorkerCapabilities(t, h, "worker-egress-second", "registration-egress-second", secondCapabilities)
	defer func() { _ = second.CloseSend() }()

	got := h.service.EgressToolNames(repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "openai_compatible",
		Model: "gpt-4o-mini",
	})
	if !slices.Equal(got, []string{"system/common"}) {
		t.Fatalf("egress tools = %v, want common intersection", got)
	}
}

func TestSelectedToolsAloneGateWorkerCompatibility(t *testing.T) {
	decoded, _, err := decodeWorkerCapabilities(modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		"gpt-4o-mini",
		8192,
		1,
	))
	if err != nil {
		t.Fatal(err)
	}
	if workerCapabilitiesSupportRoute(decoded, repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "openai_compatible",
		Model: "gpt-4o-mini", SelectedTools: []string{"files/files.read"},
	}) {
		t.Fatal("worker without frozen selected tool was considered compatible")
	}
}

func TestValidateRoutingRejectsEachUnavailableCapability(t *testing.T) {
	h := newHarness(t)
	stream := connectWorkerCapabilities(t, h, "worker-routing", "registration-routing", &turingv1.WorkerCapabilities{
		Models: []*turingv1.ModelCapability{{
			Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Model:            "llama3.2",
			MaxContextTokens: 8192,
		}},
		AgentIds:          []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
		Tools:             []*turingv1.DiscoveredTool{{ServerName: "system", ToolName: "system.time", Schema: &structpb.Struct{}}},
		MaxConcurrentRuns: 2,
	})
	defer func() { _ = stream.CloseSend() }()

	base := repository.RoutingRequirements{
		AgentID:                        "general_assistant",
		ModelProvider:                  "ollama",
		Model:                          "llama3.2",
		RequestedTools:                 []string{"system/system.time"},
		RequiredContextTokens:          8192,
		MinimumWorkerMaxConcurrentRuns: 2,
	}
	if err := h.service.ValidateRouting(context.Background(), base); err != nil {
		t.Fatalf("supported route rejected: %v", err)
	}

	tests := []struct {
		name string
		edit func(*repository.RoutingRequirements)
		kind turingv1.RoutingRequirementKind
	}{
		{
			name: "provider",
			edit: func(route *repository.RoutingRequirements) {
				route.ModelProvider = "openai_compatible"
				route.Model = "gpt-4o-mini"
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_PROVIDER,
		},
		{
			name: "model",
			edit: func(route *repository.RoutingRequirements) {
				route.Model = "other-model"
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_MODEL,
		},
		{
			name: "tool",
			edit: func(route *repository.RoutingRequirements) {
				route.RequestedTools = []string{"files/files.read"}
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_TOOL,
		},
		{
			name: "agent",
			edit: func(route *repository.RoutingRequirements) {
				route.AgentID = "specialist"
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_AGENT,
		},
		{
			name: "context",
			edit: func(route *repository.RoutingRequirements) {
				route.RequiredContextTokens = 8193
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_CONTEXT,
		},
		{
			name: "capacity",
			edit: func(route *repository.RoutingRequirements) {
				route.MinimumWorkerMaxConcurrentRuns = 3
			},
			kind: turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_CAPACITY,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route := base
			route.RequestedTools = append([]string(nil), base.RequestedTools...)
			test.edit(&route)
			err := h.service.ValidateRouting(context.Background(), route)
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("ValidateRouting error = %v, want FailedPrecondition", err)
			}
			detail := routingUnavailableDetail(t, err)
			if detail.GetKind() != test.kind || detail.GetRequested() == "" {
				t.Fatalf("routing detail = %+v, want kind %v with requested value", detail, test.kind)
			}
		})
	}
}

func TestProviderCapabilitiesIncludesModelsWithoutPositiveContextGuarantee(t *testing.T) {
	h := newHarness(t)
	stream := connectWorkerCapabilities(t, h, "worker-zero-context", "registration-zero-context", &turingv1.WorkerCapabilities{
		Models: []*turingv1.ModelCapability{{
			Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Model:    "operator-unspecified-context",
		}},
		AgentIds:          []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
		MaxConcurrentRuns: 1,
	})
	defer func() { _ = stream.CloseSend() }()

	models := h.service.ProviderCapabilities()[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA]
	if len(models) != 1 || models[0].GetModel() != "operator-unspecified-context" || models[0].GetMaxContextTokens() != 0 {
		t.Fatalf("provider capabilities = %+v, want the zero-context model", models)
	}
}

func TestWorkerCapabilitiesRejectInvalidExternalCredentialRefs(t *testing.T) {
	base := modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1)
	for name, refs := range map[string][]string{
		"blank":      {""},
		"whitespace": {" claude"},
		"duplicate":  {"claude", "claude"},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := proto.Clone(base).(*turingv1.WorkerCapabilities)
			snapshot.ExternalAgentCredentialRefs = refs
			if _, _, err := decodeWorkerCapabilities(snapshot); err == nil {
				t.Fatalf("credential refs %q were accepted", refs)
			}
		})
	}
}

func TestLegacyWorkerCapabilitiesRequireExplicitProfile(t *testing.T) {
	ready := &turingv1.RuntimeWorkerReady{
		WorkerId: "legacy-worker", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 2,
		Tools:               []*turingv1.DiscoveredTool{{ServerName: "system", ToolName: "system.time", Schema: &structpb.Struct{}}},
		ToolDiscoveryStatus: turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE,
	}
	if _, _, err := decodeLegacyWorkerCapabilities(nil, ready); err == nil {
		t.Fatal("legacy worker without an explicit profile was accepted")
	}
	profile := &LegacyCapabilityProfile{
		Models: []*turingv1.ModelCapability{{
			Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Model:            "llama3.2",
			MaxContextTokens: 8192,
		}},
		AgentIds: []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
	}
	capabilities, tools, err := decodeLegacyWorkerCapabilities(profile, ready)
	if err != nil {
		t.Fatalf("decode legacy capabilities: %v", err)
	}
	if capabilities.maxConcurrentRuns != 2 || len(capabilities.models) != 1 {
		t.Fatalf("legacy capabilities = %+v", capabilities)
	}
	if len(tools) != 1 || tools[0].ToolName != "system.time" {
		t.Fatalf("legacy tools = %+v", tools)
	}
}

func TestLegacyWorkerDoesNotSynthesizeUnadvertisedToolCapabilities(t *testing.T) {
	ready := &turingv1.RuntimeWorkerReady{
		WorkerId: "legacy-no-tools", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}
	profile := &LegacyCapabilityProfile{
		Models: []*turingv1.ModelCapability{{
			Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2", MaxContextTokens: 8192,
		}},
		AgentIds: []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
	}

	capabilities, discovered, err := decodeLegacyWorkerCapabilities(profile, ready)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 0 || len(capabilities.tools) != 0 {
		t.Fatalf("legacy no-tool snapshot produced discovered=%+v capabilities=%+v", discovered, capabilities.tools)
	}
}

func TestLegacyWorkerCannotAuthorizeRemoteEgress(t *testing.T) {
	ready := &turingv1.RuntimeWorkerReady{
		WorkerId: "legacy-external", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}
	profile := &LegacyCapabilityProfile{
		Models: []*turingv1.ModelCapability{{
			Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2", MaxContextTokens: 8192,
		}},
		AgentIds:               []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
		SupportsExternalAgents: true,
	}
	capabilities, _, err := decodeLegacyWorkerCapabilities(profile, ready)
	if err != nil {
		t.Fatal(err)
	}
	route := repository.RoutingRequirements{
		AgentID: "general_assistant", ExternalAgent: true, ExternalAgentCredentialRef: "claude",
	}
	if workerCapabilitiesSupportRoute(capabilities, route) {
		t.Fatal("legacy boolean-only profile authorized an external credential")
	}

	profile.ExternalAgentCredentialRefs = []string{"claude"}
	capabilities, _, err = decodeLegacyWorkerCapabilities(profile, ready)
	if err != nil {
		t.Fatal(err)
	}
	if workerCapabilitiesSupportRoute(capabilities, route) {
		t.Fatal("legacy credential ref authorized remote egress without decision enforcement")
	}
	route.ExternalAgentCredentialRef = "openai"
	if workerCapabilitiesSupportRoute(capabilities, route) {
		t.Fatal("explicit legacy credential ref authorized a different route")
	}
}

func TestLegacyWorkerRejectsUnknownReadyAgentID(t *testing.T) {
	ready := &turingv1.RuntimeWorkerReady{
		WorkerId: "legacy-unknown-agent", AgentId: turingv1.AgentId(99), MaxConcurrentRuns: 1,
	}
	profile := &LegacyCapabilityProfile{
		Models: []*turingv1.ModelCapability{{
			Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2", MaxContextTokens: 8192,
		}},
		AgentIds: []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
	}
	if _, _, err := decodeLegacyWorkerCapabilities(profile, ready); err == nil {
		t.Fatal("legacy worker with unknown ready agent_id was accepted")
	}
}

func TestModernWorkerRejectsUnknownToolDiscoveryStatus(t *testing.T) {
	h := newHarness(t)
	stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{
		WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId:            "worker-unknown-discovery",
			RegistrationId:      "registration-unknown-discovery",
			ToolDiscoveryStatus: turingv1.ToolDiscoveryStatus(99),
			Capabilities:        modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1),
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ConnectWorker error = %v, want InvalidArgument", err)
	}
}

func TestProviderAndAgentAvailabilityReflectLiveWorkerUnion(t *testing.T) {
	h := newHarness(t)
	first := connectWorkerCapabilities(t, h, "worker-view-a", "registration-view-a", &turingv1.WorkerCapabilities{
		Models: []*turingv1.ModelCapability{{
			Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2", MaxContextTokens: 4096,
		}},
		AgentIds: []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT}, MaxConcurrentRuns: 1,
	})
	defer func() { _ = first.CloseSend() }()
	second := connectWorkerCapabilities(t, h, "worker-view-b", "registration-view-b", &turingv1.WorkerCapabilities{
		Models: []*turingv1.ModelCapability{
			{Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2", MaxContextTokens: 8192},
			{Provider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, Model: "gpt-4o-mini", MaxContextTokens: 16384},
		},
		AgentIds: []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT}, MaxConcurrentRuns: 1,
		RemoteEgressDecisionVersion: int32(repository.RunEgressDecisionVersion),
	})
	defer func() { _ = second.CloseSend() }()

	providers := h.service.ProviderCapabilities()
	if got := providers[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA]; len(got) != 1 ||
		got[0].GetModel() != "llama3.2" || got[0].GetMaxContextTokens() != 8192 {
		t.Fatalf("Ollama provider view = %+v", got)
	}
	if got := providers[turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE]; len(got) != 1 ||
		got[0].GetModel() != "gpt-4o-mini" || got[0].GetMaxContextTokens() != 16384 {
		t.Fatalf("OpenAI provider view = %+v", got)
	}
	if !h.service.AgentAvailable(turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT) {
		t.Fatal("connected general assistant is unavailable")
	}

	if err := first.CloseSend(); err != nil {
		t.Fatal(err)
	}
	if err := second.CloseSend(); err != nil {
		t.Fatal(err)
	}
	eventually(t, eventuallyTimeout, func() bool {
		return len(h.service.ProviderCapabilities()) == 0 &&
			!h.service.AgentAvailable(turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT)
	})
}

func TestExternalAgentRouteRejectsPositiveContextRequirement(t *testing.T) {
	h := newHarness(t)
	stream := connectWorkerCapabilities(t, h, "worker-external-context", "registration-external-context", &turingv1.WorkerCapabilities{
		Models: []*turingv1.ModelCapability{{
			Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2", MaxContextTokens: 8192,
		}},
		AgentIds:                    []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
		MaxConcurrentRuns:           1,
		SupportsExternalAgents:      true,
		ExternalAgentCredentialRefs: []string{"claude"},
		RemoteEgressDecisionVersion: int32(repository.RunEgressDecisionVersion),
	})
	defer func() { _ = stream.CloseSend() }()
	route := repository.RoutingRequirements{
		AgentID: "general_assistant", ExternalAgent: true, ExternalAgentCredentialRef: "claude", RequiredContextTokens: 1,
	}
	err := h.service.ValidateRouting(context.Background(), route)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ValidateRouting error = %v, want FailedPrecondition", err)
	}
	if detail := routingUnavailableDetail(t, err); detail.GetKind() != turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_CONTEXT {
		t.Fatalf("routing detail = %+v, want context", detail)
	}
	connected := h.service.registeredWorker("worker-external-context")
	if connected == nil {
		t.Fatal("worker is not registered")
	}
	if workerCapabilitiesSupportRoute(connected.capabilities, route) {
		t.Fatal("dispatch matcher accepted an external route with an unguaranteed context requirement")
	}
}

func TestExternalAgentRouteRequiresExactCredentialRef(t *testing.T) {
	h := newHarness(t)
	stream := connectWorkerCapabilities(t, h, "worker-external-credential", "registration-external-credential", &turingv1.WorkerCapabilities{
		Models: []*turingv1.ModelCapability{{
			Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2", MaxContextTokens: 8192,
		}},
		AgentIds:                    []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
		MaxConcurrentRuns:           1,
		SupportsExternalAgents:      true,
		ExternalAgentCredentialRefs: []string{"claude"},
		RemoteEgressDecisionVersion: int32(repository.RunEgressDecisionVersion),
	})
	defer func() { _ = stream.CloseSend() }()

	supported := repository.RoutingRequirements{
		AgentID: "general_assistant", ExternalAgent: true, ExternalAgentCredentialRef: "claude",
	}
	if err := h.service.ValidateRouting(context.Background(), supported); err != nil {
		t.Fatalf("matching credential ref rejected: %v", err)
	}
	unsupported := supported
	unsupported.ExternalAgentCredentialRef = "openai"
	err := h.service.ValidateRouting(context.Background(), unsupported)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ValidateRouting error = %v, want FailedPrecondition", err)
	}
	detail := routingUnavailableDetail(t, err)
	if detail.GetKind() != turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_EXTERNAL_AGENT_CREDENTIAL ||
		detail.GetRequested() != "openai" ||
		len(detail.GetAvailable()) != 0 {
		t.Fatalf("routing detail = %+v, want credential kind/request without exposing available refs", detail)
	}
}

func connectWorkerCapabilities(
	t *testing.T,
	h *harness,
	workerID string,
	registrationID string,
	capabilities *turingv1.WorkerCapabilities,
) turingv1.RuntimeService_ConnectWorkerClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(h.internalContext(), 30*time.Second)
	t.Cleanup(cancel)
	stream, err := h.runtimeClient(t).ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{
		WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId:       workerID,
			RegistrationId: registrationID,
			Capabilities:   capabilities,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	accepted := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetWorkerAccepted() != nil
	}).GetWorkerAccepted()
	if accepted.GetWorkerId() != workerID || accepted.GetRegistrationId() != registrationID {
		t.Fatalf("worker accepted identity = %q/%q, want %q/%q", accepted.GetWorkerId(), accepted.GetRegistrationId(), workerID, registrationID)
	}
	return stream
}

func routingUnavailableDetail(t *testing.T, err error) *turingv1.RoutingUnavailableDetail {
	t.Helper()
	for _, detail := range status.Convert(err).Details() {
		if unavailable, ok := detail.(*turingv1.RoutingUnavailableDetail); ok {
			return unavailable
		}
	}
	t.Fatalf("error %v has no RoutingUnavailableDetail", err)
	return nil
}
