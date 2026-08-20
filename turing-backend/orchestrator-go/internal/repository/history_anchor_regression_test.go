package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestListMessagesBeforeUsesSequenceAndRequiresSessionAnchor(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Causal ordering")
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.CreateSession(ctx, "Other")
	if err != nil {
		t.Fatal(err)
	}
	const createdAt = "2026-08-11T00:00:00.000000000Z"
	for _, message := range []struct {
		id       string
		sequence int
	}{
		{id: "message_z_first", sequence: 1},
		{id: "message_a_second", sequence: 2},
		{id: "message_q_third", sequence: 3},
		{id: "message_m_anchor", sequence: 4},
	} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
			VALUES (?, ?, 'user', ?, 'text', ?, ?)
		`, message.id, session.SessionID, message.id, message.sequence, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('message_other_anchor', ?, 'user', 'other', 'text', 1, ?)
	`, other.SessionID, createdAt); err != nil {
		t.Fatal(err)
	}

	messages, err := repo.ListMessagesBefore(ctx, session.SessionID, "message_m_anchor", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Sequence != 2 || messages[1].Sequence != 3 {
		t.Fatalf("predecessors = %+v, want sequences 2 then 3", messages)
	}
	for _, anchorID := range []string{"missing_anchor", "message_other_anchor"} {
		if _, err := repo.ListMessagesBefore(ctx, session.SessionID, anchorID, 10); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("anchor %q error = %v, want sql.ErrNoRows", anchorID, err)
		}
	}
}

// TestListMessagesBeforeAppliesBoundaryBeforeRunProjection pins the order the
// two operations happen in. The page boundary and limit belong to message rows;
// if the run projection were applied first, or the limit counted joined rows,
// an older page could gain or lose turns because of what their runs did.
func TestListMessagesBeforeAppliesBoundaryBeforeRunProjection(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Bounded page")
	if err != nil {
		t.Fatal(err)
	}
	type turn struct {
		enqueued EnqueueUserMessageResult
		state    RunState
	}
	turns := make([]turn, 0, 3)
	for index := 0; index < 3; index++ {
		enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: "ask", AgentID: "general_assistant",
			ModelProvider: "ollama", Model: "llama3.2",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
			t.Fatal(err)
		}
		running, err := repo.GetRunState(ctx, enqueued.RunID)
		if err != nil {
			t.Fatal(err)
		}
		completed, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
			RunID:                enqueued.RunID,
			AssistantMessageID:   enqueued.AssistantMessageID,
			Content:              "answer",
			ExpectedStateVersion: running.StateVersion,
		})
		if err != nil {
			t.Fatal(err)
		}
		turns = append(turns, turn{enqueued: enqueued, state: completed.State})
	}

	// Every turn writes two message rows, so a limit of 2 anchored at the third
	// turn's user message is exactly the second turn — both of its rows and
	// nothing from the run that follows.
	page, err := repo.ListMessagesBefore(ctx, session.SessionID, turns[2].enqueued.UserMessageID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("page size = %d, want the limit applied to message rows", len(page))
	}
	if page[0].MessageID != turns[1].enqueued.UserMessageID ||
		page[1].MessageID != turns[1].enqueued.AssistantMessageID {
		t.Fatalf("page = %+v, want the second turn in causal order", page)
	}
	if page[0].RunState != nil {
		t.Fatalf("user message in an older page carries run state %+v", page[0].RunState)
	}
	// The internal content digest is never selected by the history reader, so
	// the committed state is compared without it.
	want := turns[1].state
	want.ContentSHA256 = ""
	if page[1].RunState == nil || *page[1].RunState != want {
		t.Fatalf("older page state = %+v, want the second turn's committed state %+v", page[1].RunState, want)
	}
}

// TestOverlappingMessagePagesKeepOneMessageAndOneRunVersion covers what a
// client does with two pages that overlap: it deduplicates messages by ID and
// run states by run ID plus version. That only converges if both pages report
// the same identity and the same version for the same row.
func TestOverlappingMessagePagesKeepOneMessageAndOneRunVersion(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Overlapping pages")
	if err != nil {
		t.Fatal(err)
	}
	var anchor string
	for index := 0; index < 3; index++ {
		enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: "ask", AgentID: "general_assistant",
			ModelProvider: "ollama", Model: "llama3.2",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
			t.Fatal(err)
		}
		running, err := repo.GetRunState(ctx, enqueued.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
			RunID:                enqueued.RunID,
			AssistantMessageID:   enqueued.AssistantMessageID,
			Content:              "answer",
			ExpectedStateVersion: running.StateVersion,
		}); err != nil {
			t.Fatal(err)
		}
		if index == 2 {
			anchor = enqueued.AssistantMessageID
		}
	}

	newest, err := repo.ListMessages(ctx, session.SessionID, 50)
	if err != nil {
		t.Fatal(err)
	}
	older, err := repo.ListMessagesBefore(ctx, session.SessionID, anchor, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(older) == 0 {
		t.Fatal("older page is empty, so the pages do not overlap")
	}

	messages := map[string]Message{}
	states := map[string]RunState{}
	overlapping := 0
	for _, page := range [][]Message{newest, older} {
		for _, message := range page {
			if previous, seen := messages[message.MessageID]; seen {
				overlapping++
				if previous.MessageID != message.MessageID || previous.RunID != message.RunID ||
					previous.Sequence != message.Sequence {
					t.Fatalf("overlapping message identity = %+v and %+v", previous, message)
				}
			}
			messages[message.MessageID] = message
			if message.RunState == nil {
				continue
			}
			if previous, seen := states[message.RunState.RunID]; seen && previous != *message.RunState {
				t.Fatalf("overlapping run state = %+v and %+v", previous, *message.RunState)
			}
			states[message.RunState.RunID] = *message.RunState
		}
	}
	if overlapping == 0 {
		t.Fatal("no message appeared in both pages, so deduplication was never exercised")
	}
	if len(messages) != 6 {
		t.Fatalf("deduplicated messages = %d, want six", len(messages))
	}
	if len(states) != 3 {
		t.Fatalf("deduplicated run states = %d, want one per run", len(states))
	}
}
