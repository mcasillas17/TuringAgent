package chat

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A child session's ID is visible to the client, but sending into it or
// preparing consent for it is refused, and nothing is enqueued.
func TestSendingIntoADelegationSessionIsRefused(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	if _, err := h.database.ExecContext(context.Background(), `UPDATE sessions SET kind = 'delegation' WHERE id = ?`, sessionID); err != nil {
		t.Fatal(err)
	}

	err := sendMessageError(h, &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: "write into the child",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
	})
	if status.Code(err) != codes.FailedPrecondition || status.Convert(err).Message() != "delegation sessions are read-only" {
		t.Fatalf("SendMessage = %v, want FailedPrecondition read-only", err)
	}
	_, err = h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: sessionID, Content: "send remotely", ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "gpt-4o-mini", IdempotencyKey: "child_disclosure",
	})
	if status.Code(err) != codes.FailedPrecondition || status.Convert(err).Message() != "delegation sessions are read-only" {
		t.Fatalf("PrepareRemoteEgress = %v, want FailedPrecondition read-only", err)
	}
	var runs int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM agent_runs WHERE session_id = ?`, sessionID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 0 {
		t.Fatalf("runs in the child = %d, want none", runs)
	}
}

// Cancelling is how a user stops a specialist, so the public CancelRun keeps
// working on a delegation session's run.
func TestCancellingTheRunOfADelegationSessionStillWorks(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sessionID := h.createSession(t)
	run, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: sessionID, Content: "the brief", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(ctx, `UPDATE sessions SET kind = 'delegation' WHERE id = ?`, sessionID); err != nil {
		t.Fatal(err)
	}
	got, err := h.chatClient.CancelRun(ctx, &turingv1.CancelRunRequest{SessionId: sessionID, RunId: run.RunID, IdempotencyKey: "cancel-child"})
	if err != nil || got.GetResult() != turingv1.CancelRunResult_CANCEL_RUN_RESULT_ACCEPTED {
		t.Fatalf("CancelRun on the child's run = %v, %v; want accepted", got, err)
	}
}

// A keyed replay is answered from the idempotency record without reaching the
// enqueue guard, so the service refuses a delegation session before looking
// it up: the replay is refused and adds nothing.
func TestAKeyedReplayIntoADelegationSessionIsRefused(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	request := &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: "the original message",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
		IdempotencyKey: "keyed-before-delegation",
	}
	if err := sendMessageError(h, request); err != nil {
		t.Fatalf("original SendMessage: %v", err)
	}
	if _, err := h.database.ExecContext(ctx, `UPDATE sessions SET kind = 'delegation' WHERE id = ?`, sessionID); err != nil {
		t.Fatal(err)
	}

	err := sendMessageError(h, request)
	if status.Code(err) != codes.FailedPrecondition || status.Convert(err).Message() != "delegation sessions are read-only" {
		t.Fatalf("replayed SendMessage = %v, want FailedPrecondition read-only", err)
	}
	var runs int
	if err := h.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs WHERE session_id = ?`, sessionID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("runs after the refused replay = %d, want the original 1", runs)
	}
}
