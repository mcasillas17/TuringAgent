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
//
// Agreement is asserted per overlapping row rather than per run, because a page
// that dropped every state would agree with the other page vacuously — the
// deduplicated set would still hold one state per run, contributed entirely by
// the page that kept them. So the older page is also checked on its own, and
// both pages are checked for causal order, which is the other thing a client
// cannot repair after the fact.
func TestOverlappingMessagePagesKeepOneMessageAndOneRunVersion(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Overlapping pages")
	if err != nil {
		t.Fatal(err)
	}
	var anchor string
	// The committed state of each run, keyed by the assistant turn that owns
	// it. The internal content digest is never selected by the history reader,
	// so it is cleared here rather than tolerated in the comparison.
	committed := map[string]RunState{}
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
		want := completed.State
		want.ContentSHA256 = ""
		committed[enqueued.AssistantMessageID] = want
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
	assertCausalOrder(t, "newest page", newest)
	assertCausalOrder(t, "older page", older)

	// The older page is asserted on its own terms first. A client that scrolls
	// back reads it without the newest page in hand, so whatever it omits is
	// simply missing from the conversation as that client sees it.
	olderStates := 0
	for _, message := range older {
		want, owned := committed[message.MessageID]
		if !owned {
			if message.RunState != nil {
				t.Fatalf("older page attached run state %+v to %q, which owns none", message.RunState, message.MessageID)
			}
			continue
		}
		if message.RunState == nil {
			t.Fatalf("older page dropped the outcome of assistant turn %q", message.MessageID)
		}
		if *message.RunState != want {
			t.Fatalf("older page state for %q = %+v, want the committed %+v", message.MessageID, *message.RunState, want)
		}
		if !message.RunState.HasDisplayableContent {
			t.Fatalf("older page reports no displayable content for %q, which has an answer", message.MessageID)
		}
		olderStates++
	}
	if olderStates != 2 {
		t.Fatalf("older page carried %d run states, want one per completed turn it contains", olderStates)
	}

	messages := map[string]Message{}
	states := map[string]RunState{}
	overlapping := 0
	for _, page := range [][]Message{newest, older} {
		for _, message := range page {
			if previous, seen := messages[message.MessageID]; seen {
				overlapping++
				if previous.MessageID != message.MessageID || previous.RunID != message.RunID ||
					previous.Sequence != message.Sequence || previous.Role != message.Role ||
					previous.Content != message.Content || previous.CreatedAt != message.CreatedAt {
					t.Fatalf("overlapping message identity = %+v and %+v", previous, message)
				}
				// Presence is compared before value: one page saying an outcome
				// exists while the other says none was recorded is the exact
				// disagreement a client cannot reconcile, and comparing only
				// the states that happen to be present would never see it.
				if (previous.RunState == nil) != (message.RunState == nil) {
					t.Fatalf("overlapping message %q carries state %+v in one page and %+v in the other",
						message.MessageID, previous.RunState, message.RunState)
				}
				if previous.RunState != nil && *previous.RunState != *message.RunState {
					t.Fatalf("overlapping state for %q = %+v and %+v",
						message.MessageID, *previous.RunState, *message.RunState)
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
	if overlapping != len(older) {
		t.Fatalf("overlapping messages = %d, want the %d rows the older page repeats", overlapping, len(older))
	}
	if len(messages) != 6 {
		t.Fatalf("deduplicated messages = %d, want six", len(messages))
	}
	if len(states) != 3 {
		t.Fatalf("deduplicated run states = %d, want one per run", len(states))
	}
}

// assertCausalOrder proves a page reads oldest first. History is rendered in
// the order it is returned, so a reversed page is a conversation whose answers
// precede the questions that caused them.
func assertCausalOrder(t *testing.T, page string, messages []Message) {
	t.Helper()
	for index := 1; index < len(messages); index++ {
		if messages[index].Sequence <= messages[index-1].Sequence {
			t.Fatalf("%s is not in causal order at index %d: sequences %d then %d",
				page, index, messages[index-1].Sequence, messages[index].Sequence)
		}
	}
}
