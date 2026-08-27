package chat

import (
	"context"
	"slices"
	"strconv"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// A consent dialog is a promise that the run it describes can happen.
//
// The route a prepare validates was built before anything had looked at where
// the frozen tools go, so it said "a local model, no egress decision" — and a
// worker built before egress decisions existed satisfies that. The tool
// snapshot was then taken from that same pre-decision candidate set, a remote
// MCP server or an integration turned up in it, and the challenge went out. The
// send that follows freezes a decision onto the job, which none of the workers
// the route was validated against can execute: the job is queued behind a
// worker that will never claim it, after the user has consented to sending
// their words to a remote destination.
//
// The route is now rebuilt once the egress decision requirement is known and
// the selected tools are frozen, and validated again on exactly that. The tools
// that are validated are the same slice that goes into the challenge, so
// nothing can move between the two.
func remoteMCPWorkerCapabilities(t *testing.T, h *harness, decisionVersion int32) *turingv1.WorkerCapabilities {
	t.Helper()
	server, err := h.repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetMCPServerEnabled(context.Background(), server.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.ReplaceMCPServerTools(context.Background(), server.Server.ID, []repository.MCPServerTool{{
		Name: "vendor.lookup", Policy: "approval_required", SchemaJSON: `{"type":"object"}`,
	}}); err != nil {
		t.Fatal(err)
	}
	capabilities := defaultChatWorkerCapabilities(false)
	capabilities.Tools = append(capabilities.Tools, &turingv1.DiscoveredTool{
		ServerName: "vendor", ToolName: "vendor.lookup", Schema: &structpb.Struct{},
	})
	capabilities.RemoteEgressDecisionVersion = decisionVersion
	return capabilities
}

func localModelRemoteToolPrepare(sessionID, idempotencyKey string) *turingv1.PrepareRemoteEgressRequest {
	return &turingv1.PrepareRemoteEgressRequest{
		SessionId: sessionID, Content: "look it up", ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
		IdempotencyKey: idempotencyKey, RequestedTools: []string{"vendor/vendor.lookup"},
	}
}

func requireEgressAwareRoutingRefusal(t *testing.T, err error) {
	t.Helper()
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("error=%v, want a FailedPrecondition the client can read as 'no worker can run this'", err)
	}
	detail := chatRoutingUnavailableDetail(t, err)
	if detail == nil {
		t.Fatalf("refusal carried no routing detail: %v", err)
	}
	if detail.GetKind() != turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_PROVIDER {
		t.Fatalf("routing detail kind=%v, want the provider-shaped egress-decision refusal", detail.GetKind())
	}
	if detail.GetRequested() != "remote egress decision v"+strconv.Itoa(repository.RunEgressDecisionVersion) {
		t.Fatalf("routing detail requested=%q, want the decision version the run would carry", detail.GetRequested())
	}
}

// The literal pre-memory number, not the constant: a worker built before memory
// joined the decision cannot be handed one, and this has to keep refusing after
// the constant moves again.
const preMemoryWorkerDecisionVersion = 2

func TestPrepareRefusesALocalRunWhoseRemoteToolNoWorkerCanDecideFor(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, remoteMCPWorkerCapabilities(t, h, preMemoryWorkerDecisionVersion))
	defer func() { _ = worker.CloseSend() }()

	prepared, err := h.chatClient.PrepareRemoteEgress(
		h.clientContext(),
		localModelRemoteToolPrepare(h.createSession(t), "local_remote_tool_v2"),
	)
	if err == nil {
		t.Fatalf("a challenge was issued for a run no connected worker can execute: %+v", prepared.GetDisclosure())
	}
	requireEgressAwareRoutingRefusal(t, err)
}

func TestPrepareIssuesTheChallengeWhenAWorkerCanDecideForTheRemoteTool(t *testing.T) {
	h := newHarness(t)
	capabilities := remoteMCPWorkerCapabilities(t, h, int32(repository.RunEgressDecisionVersion))
	worker := connectChatTestWorker(t, h, capabilities)
	defer func() { _ = worker.CloseSend() }()

	prepared, err := h.chatClient.PrepareRemoteEgress(
		h.clientContext(),
		localModelRemoteToolPrepare(h.createSession(t), "local_remote_tool_v3"),
	)
	if err != nil {
		t.Fatal(err)
	}
	disclosure := prepared.GetDisclosure()
	if disclosure == nil || disclosure.GetChallenge() == "" {
		t.Fatal("an executable local run with a remote tool was not disclosed and signed")
	}
	if !slices.Contains(disclosure.GetSelectedTools(), "vendor/vendor.lookup") {
		t.Fatalf("selected tools=%v, want the remote tool the disclosure is about", disclosure.GetSelectedTools())
	}
	if len(disclosure.GetRemoteMcpServers()) != 1 {
		t.Fatalf("remote MCP destinations=%+v, want the one the tool reaches", disclosure.GetRemoteMcpServers())
	}
	// The route that was validated is the one the challenge froze: the tools
	// in the signed payload are the tools the worker was checked against.
	payload, _, err := h.service.parseEgressChallenge(disclosure.GetChallenge())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(payload.SelectedTools, disclosure.GetSelectedTools()) {
		t.Fatalf("signed tools=%v, disclosed tools=%v", payload.SelectedTools, disclosure.GetSelectedTools())
	}
}

// The same rebuild governs an integration destination, which is the other way a
// run that keeps its model on the machine sends the user's words off it.
func TestPrepareRefusesALocalRunWhoseIntegrationNoWorkerCanDecideFor(t *testing.T) {
	h := newHarness(t)
	capabilities := integrationEgressCapabilities()
	capabilities.RemoteEgressDecisionVersion = preMemoryWorkerDecisionVersion
	worker := connectChatTestWorker(t, h, capabilities)
	defer func() { _ = worker.CloseSend() }()
	createChatGitHubConnection(t, h.repo, 1)

	prepared, err := h.chatClient.PrepareRemoteEgress(
		h.clientContext(),
		integrationPrepareRequest(h.createSession(t), "local_integration_v2"),
	)
	if err == nil {
		t.Fatalf("a challenge was issued for an integration run no worker can execute: %+v", prepared.GetDisclosure())
	}
	requireEgressAwareRoutingRefusal(t, err)
}

// And the send that would have followed says the same thing. Telling the user
// "explicit consent is required" for a run that can never be executed sends
// them to consent to something that will then sit in a queue forever; the
// refusal they get instead names what is missing.
func TestSendRefusesTheSameRouteRatherThanAskingForConsent(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, remoteMCPWorkerCapabilities(t, h, preMemoryWorkerDecisionVersion))
	defer func() { _ = worker.CloseSend() }()

	err := sendMessageError(h, &turingv1.SendMessageRequest{
		SessionId: h.createSession(t), Content: "look it up", ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
		IdempotencyKey: "local_remote_tool_send_v2", RequestedTools: []string{"vendor/vendor.lookup"},
	})
	if err == nil {
		t.Fatal("a send was accepted for a run no connected worker can execute")
	}
	requireEgressAwareRoutingRefusal(t, err)
}

// A purely local run is untouched: no destination, no decision, and no reason
// to ask for a worker that can validate one.
func TestPrepareStillLeavesAPurelyLocalRunAlone(t *testing.T) {
	h := newHarness(t)
	capabilities := defaultChatWorkerCapabilities(false)
	capabilities.RemoteEgressDecisionVersion = preMemoryWorkerDecisionVersion
	worker := connectChatTestWorker(t, h, capabilities)
	defer func() { _ = worker.CloseSend() }()

	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: h.createSession(t), Content: "stay local", ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
		IdempotencyKey: "purely_local_v2", RequestedTools: []string{"system/system.time"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.GetDisclosure() != nil {
		t.Fatalf("a local run with local tools was disclosed: %+v", prepared.GetDisclosure())
	}
}
