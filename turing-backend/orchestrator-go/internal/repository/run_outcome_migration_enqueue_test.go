package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// pinnedEmptyAssistantContentSHA256 fails to compile if the empty-content
// digest ever becomes a variable again. The value is written into every
// enqueued run and later compared against a terminal report's content
// identity, so a package-level var would let any code in this package — or, if
// exported, in any other — reassign the thing duplicate detection is judged
// against, at runtime, with no test able to see it.
const pinnedEmptyAssistantContentSHA256 = emptyAssistantContentSHA256

// TestEmptyAssistantContentDigestMatchesTheSharedHash keeps the literal above
// from drifting away from the function that computes it everywhere else.
func TestEmptyAssistantContentDigestMatchesTheSharedHash(t *testing.T) {
	want := runoutcome.ContentSHA256("")
	if pinnedEmptyAssistantContentSHA256 != want {
		t.Fatalf("emptyAssistantContentSHA256 = %q, want the shared empty-content digest %q",
			pinnedEmptyAssistantContentSHA256, want)
	}
	// The schema's length check and the lowercase-hex convention are what make
	// the stored digest comparable at all, so both are asserted on the literal
	// rather than inferred from the equality above.
	if got := len(pinnedEmptyAssistantContentSHA256); got != 64 {
		t.Fatalf("digest length = %d, want 64", got)
	}
	if pinnedEmptyAssistantContentSHA256 != strings.ToLower(pinnedEmptyAssistantContentSHA256) {
		t.Fatalf("digest %q is not lowercase hex", pinnedEmptyAssistantContentSHA256)
	}
}

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

	// The queued event now projects the version-1 state the row already holds,
	// so a client that only ever sees the event agrees with a client that
	// reopens and reads the row.
	var queuedPayload string
	if err := database.QueryRowContext(ctx, `
		SELECT payload_json FROM events WHERE run_id = ? AND type = 'agent.run.queued'
	`, enqueued.RunID).Scan(&queuedPayload); err != nil {
		t.Fatalf("read queued event: %v", err)
	}
	var payload struct {
		RunState struct {
			Lifecycle             string `json:"lifecycle"`
			OutcomeReason         string `json:"outcomeReason"`
			StateVersion          int64  `json:"stateVersion"`
			StateUpdatedAt        string `json:"stateUpdatedAt"`
			HasDisplayableContent bool   `json:"hasDisplayableContent"`
		} `json:"runState"`
	}
	if err := json.Unmarshal([]byte(queuedPayload), &payload); err != nil {
		t.Fatalf("decode queued payload: %v", err)
	}
	if payload.RunState.Lifecycle != status || payload.RunState.OutcomeReason != outcomeReason ||
		payload.RunState.StateVersion != stateVersion || payload.RunState.StateUpdatedAt != stateUpdatedAt {
		t.Fatalf("queued snapshot = %+v, want the row's %s/%s at version %d and %q",
			payload.RunState, status, outcomeReason, stateVersion, stateUpdatedAt)
	}
	if payload.RunState.HasDisplayableContent {
		t.Fatal("queued snapshot claims displayable assistant content before the run produced any")
	}
}
