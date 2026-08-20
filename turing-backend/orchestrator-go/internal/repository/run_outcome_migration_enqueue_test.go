package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// TestPostMigrationEnqueueWritesCanonicalVersionOneFields pins the writer side
// of the canonical schema: once agent_runs requires the state fields, the one
// path that creates a run has to supply them, and it has to supply the version
// the rest of the design counts from.
func TestPostMigrationEnqueueWritesCanonicalVersionOneFields(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Canonical enqueue")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}

	var (
		status         string
		outcomeReason  string
		stateVersion   int64
		stateUpdatedAt string
		createdAt      string
		contentSHA     string
	)
	if err := database.QueryRowContext(ctx, `
		SELECT status, outcome_reason, state_version, state_updated_at, created_at,
			assistant_content_sha256
		FROM agent_runs WHERE id = ?
	`, enqueued.RunID).Scan(
		&status, &outcomeReason, &stateVersion, &stateUpdatedAt, &createdAt, &contentSHA,
	); err != nil {
		t.Fatalf("read enqueued run: %v", err)
	}
	if status != "queued" || outcomeReason != "none" {
		t.Fatalf("enqueued run = %s/%s, want queued/none", status, outcomeReason)
	}
	if stateVersion != 1 {
		t.Fatalf("state_version = %d, want 1", stateVersion)
	}
	if stateUpdatedAt != createdAt {
		t.Fatalf("state_updated_at = %q, want the run's creation timestamp %q", stateUpdatedAt, createdAt)
	}
	if want := runoutcome.ContentSHA256(""); contentSHA != want {
		t.Fatalf("assistant_content_sha256 = %q, want the empty-content digest %q", contentSHA, want)
	}

	// Deliberately still absent: the queued event carries no run-state snapshot
	// until the versioned transition work lands. Asserting its absence keeps
	// that later step honestly red instead of accidentally satisfied here.
	var queuedPayload string
	if err := database.QueryRowContext(ctx, `
		SELECT payload_json FROM events WHERE run_id = ? AND type = 'agent.run.queued'
	`, enqueued.RunID).Scan(&queuedPayload); err != nil {
		t.Fatalf("read queued event: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(queuedPayload), &payload); err != nil {
		t.Fatalf("decode queued payload: %v", err)
	}
	if _, present := payload["runState"]; present {
		t.Fatal("queued event already carries a run-state snapshot; that projection belongs to the transition task")
	}
}
