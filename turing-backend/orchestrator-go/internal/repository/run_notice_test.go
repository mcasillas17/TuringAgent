package repository

import (
	"context"
	"encoding/json"
	"testing"
)

// The whole client contract rests on `note`: RunNoticeCard reads that key and
// nothing else. appendRunNoticeTx therefore takes it as a real parameter and
// assigns it last so extras cannot shadow it — an operator field named "note"
// must not be able to replace the user-facing sentence.
func TestAppendRunNoticeKeepsNoteAndMergesExtras(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Notice payload")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "notice", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	event, err := appendRunNoticeTx(ctx, tx, session.SessionID, enqueued.RunID, enqueued.TraceID,
		"the real note",
		map[string]any{"note": "an impostor", "attempt": 2}, now())
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["note"] != "the real note" {
		t.Fatalf("note = %v, want the parameter to win over the extras key", payload["note"])
	}
	// Extras still ride along for operators reading the event log.
	if payload["attempt"] != float64(2) {
		t.Fatalf("attempt = %v (%T), want 2", payload["attempt"], payload["attempt"])
	}
	if event.Type != "agent.run.step" {
		t.Fatalf("event type = %q, want agent.run.step", event.Type)
	}
}
