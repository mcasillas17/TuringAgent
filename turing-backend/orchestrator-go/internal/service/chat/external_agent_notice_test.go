package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A second window on the same conversation learns about egress from the bus,
// not from a poll. Without publishing here, the only other subscriber is told
// nothing until replay catches up — and "told late" is barely better than "not
// told" for a message that already left.
func TestSendMessagePublishesTheEgressNoticeToOtherSubscribers(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(true))
	defer func() { _ = worker.CloseSend() }()
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
			if event.Type == "agent.run.step" && strings.Contains(event.PayloadJSON, "leaves your machine") {
				return
			}
		case <-deadline:
			t.Fatal("no egress notice published to the bus")
		}
	}
}

func TestUnsupportedExternalAgentRouteFailsBeforePersistence(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	agent, err := h.repo.CreateExternalAgent(context.Background(), repository.ExternalAgentInput{
		DisplayName: "External", Provider: "anthropic", BaseURL: "https://example.com",
		Model: "external-model", CredentialRef: "external",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.SetSessionAgent(context.Background(), sessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}

	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: "hello", ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("SendMessage error = %v, want FailedPrecondition", err)
	}
	for _, table := range []string{"messages", "agent_runs", "jobs", "events"} {
		var count int
		query := `SELECT COUNT(*) FROM ` + table
		if table == "messages" || table == "agent_runs" || table == "events" {
			query += ` WHERE session_id = ?`
		} else {
			query += ` WHERE run_id IN (SELECT id FROM agent_runs WHERE session_id = ?)`
		}
		if err := h.database.QueryRowContext(context.Background(), query, sessionID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want no persisted enqueue work", table, count)
		}
	}
}

func TestExternalAgentRouteWithUnavailableCredentialFailsBeforePersistence(t *testing.T) {
	h := newHarness(t)
	capabilities := defaultChatWorkerCapabilities(true)
	capabilities.ExternalAgentCredentialRefs = []string{"other"}
	worker := connectChatTestWorker(t, h, capabilities)
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	agent, err := h.repo.CreateExternalAgent(context.Background(), repository.ExternalAgentInput{
		DisplayName: "Claude", Provider: "anthropic", BaseURL: "https://example.com",
		Model: "external-model", CredentialRef: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.SetSessionAgent(context.Background(), sessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}

	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: "hello", ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("SendMessage error = %v, want FailedPrecondition", err)
	}
	for _, table := range []string{"messages", "agent_runs", "jobs", "events"} {
		var count int
		query := `SELECT COUNT(*) FROM ` + table
		if table == "messages" || table == "agent_runs" || table == "events" {
			query += ` WHERE session_id = ?`
		} else {
			query += ` WHERE run_id IN (SELECT id FROM agent_runs WHERE session_id = ?)`
		}
		if err := h.database.QueryRowContext(context.Background(), query, sessionID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want no persisted enqueue work", table, count)
		}
	}
}

// The same send on an unrouted conversation must publish nothing of the kind,
// or the notice would become background noise nobody reads.
func TestSendMessagePublishesNoEgressNoticeForALocalConversation(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
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
			if strings.Contains(event.PayloadJSON, "leaves your machine") {
				t.Fatalf("a local conversation reported egress: %s", event.PayloadJSON)
			}
		case <-deadline:
			return
		}
	}
}
