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

func TestAppendPendingRunNoticeSkipsClaimedWork(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Conditional notice")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "claim before notice", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-conditional-notice"); err != nil {
		t.Fatal(err)
	}

	_, appended, err := repo.AppendPendingRunNotice(
		ctx,
		enqueued.RunID,
		"stale queue notice",
		map[string]any{"reason": "routing_capability_unavailable"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if appended {
		t.Fatal("notice appended after the pending job was claimed")
	}
	var count int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM events
		WHERE run_id = ? AND type = 'agent.run.step'
		  AND json_extract(payload_json, '$.reason') = 'routing_capability_unavailable'
	`, enqueued.RunID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("conditional notices = %d, want 0", count)
	}
}

func TestAppendPendingRunNoticeAppendsWhileQueued(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Pending notice")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "still waiting", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}

	event, appended, err := repo.AppendPendingRunNotice(
		ctx,
		enqueued.RunID,
		"queue notice",
		map[string]any{"reason": "routing_capability_unavailable"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !appended {
		t.Fatal("notice was not appended for queued pending work")
	}
	if event.RunID.String != enqueued.RunID || event.Type != "agent.run.step" {
		t.Fatalf("event = %+v, want agent.run.step for %s", event, enqueued.RunID)
	}
}
