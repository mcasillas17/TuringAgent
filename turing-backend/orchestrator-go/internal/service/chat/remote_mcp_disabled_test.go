package chat

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestDisabledRemoteMCPToolDoesNotRequireEgressConsent(t *testing.T) {
	h := newHarness(t)
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
	worker := connectChatTestWorker(t, h, capabilities)
	defer func() { _ = worker.CloseSend() }()
	if err := h.repo.SetMCPToolPolicy(context.Background(), server.Server.ID, "vendor.lookup", "disabled"); err != nil {
		t.Fatal(err)
	}

	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: h.createSession(t), Content: "stay local", ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2", IdempotencyKey: "disabled_remote_mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.GetDisclosure() != nil {
		t.Fatalf("disabled remote tool produced disclosure: %+v", prepared.GetDisclosure())
	}
}
