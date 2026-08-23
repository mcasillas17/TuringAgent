package repository

import (
	"context"
	"testing"
)

func TestGlobalCapacityRetainsTerminalExecutionUntilExitAcknowledged(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	firstSession, err := repo.CreateSession(ctx, "First")
	if err != nil {
		t.Fatal(err)
	}
	secondSession, err := repo.CreateSession(ctx, "Second")
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: firstSession.SessionID, Content: "first", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: secondSession.SessionID, Content: "second", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-one", 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := cancelRunEvents(t, repo, first.RunID); err != nil {
		t.Fatal(err)
	}
	blocked, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-two", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.JobID != "" {
		t.Fatalf("global capacity released terminal executor before exit acknowledgement: %+v", blocked)
	}
	if err := repo.AcknowledgeExecutionExit(ctx, first.RunID); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-two", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.RunID != second.RunID {
		t.Fatalf("post-ack claim = %+v, want %q", claimed, second.RunID)
	}
}
