package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// A second window on the same conversation learns about egress from the bus,
// not from a poll. Without publishing here, the only other subscriber is told
// nothing until replay catches up — and "told late" is barely better than "not
// told" for a message that already left.
func TestSendMessagePublishesTheEgressNoticeToOtherSubscribers(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	agent, err := h.repo.CreateExternalAgent(context.Background(), repository.ExternalAgentInput{
		DisplayName:   "Claude",
		Provider:      "anthropic",
		BaseURL:       "https://api.anthropic.com/v1",
		Model:         "claude-sonnet-4-5",
		CredentialRef: "claude",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if _, err := h.repo.SetSessionAgent(context.Background(), sessionID, agent.AgentID); err != nil {
		t.Fatalf("route: %v", err)
	}

	subscription, unsubscribe := h.bus.Subscribe(sessionID)
	defer unsubscribe()

	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "hello",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, ok := <-subscription:
			if !ok {
				t.Fatal("bus closed before the egress notice arrived")
			}
			if event.Type == "agent.run.step" && strings.Contains(event.PayloadJSON, "left your machine") {
				return
			}
		case <-deadline:
			t.Fatal("no egress notice published to the bus")
		}
	}
}

// The same send on an unrouted conversation must publish nothing of the kind,
// or the notice would become background noise nobody reads.
func TestSendMessagePublishesNoEgressNoticeForALocalConversation(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)

	subscription, unsubscribe := h.bus.Subscribe(sessionID)
	defer unsubscribe()

	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "hello",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case event, ok := <-subscription:
			if !ok {
				return
			}
			if strings.Contains(event.PayloadJSON, "left your machine") {
				t.Fatalf("a local conversation reported egress: %s", event.PayloadJSON)
			}
		case <-deadline:
			return
		}
	}
}
