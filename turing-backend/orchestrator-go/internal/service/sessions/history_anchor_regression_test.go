package sessions

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListMessagesBeforeMissingOrForeignAnchorReturnsNotFound(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Anchor owner")
	if err != nil {
		t.Fatal(err)
	}
	other, err := h.repo.CreateSession(ctx, "Anchor foreign")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: other.SessionID, Content: "foreign", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, anchorID := range []string{"message_missing", foreign.UserMessageID} {
		_, err := client.ListMessages(ctx, &turingv1.ListMessagesRequest{
			SessionId: session.SessionID, BeforeMessageId: anchorID, Limit: 10,
		})
		if status.Code(err) != codes.NotFound {
			t.Fatalf("ListMessages(%q) error = %v, want NotFound", anchorID, err)
		}
	}
}
