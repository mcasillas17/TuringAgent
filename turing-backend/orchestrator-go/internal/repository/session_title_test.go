package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

type sessionUpdatedPayload struct {
	Title     string `json:"title"`
	UpdatedAt string `json:"updatedAt"`
}

func decodeSessionUpdatedPayload(t *testing.T, event Event) sessionUpdatedPayload {
	t.Helper()
	var payload sessionUpdatedPayload
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode session.updated payload: %v", err)
	}
	return payload
}

func TestDeriveSessionTitle(t *testing.T) {
	longWord := strings.Repeat("a", 90)

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "short message is used verbatim",
			content: "Summarise the release notes",
			want:    "Summarise the release notes",
		},
		{
			name:    "surrounding whitespace is trimmed",
			content: "   hello there\n\n",
			want:    "hello there",
		},
		{
			name:    "newlines and tabs collapse to single spaces",
			content: "first line\n\tsecond   line",
			want:    "first line second line",
		},
		{
			name:    "empty content yields no title",
			content: "",
			want:    "",
		},
		{
			name:    "whitespace-only content yields no title",
			content: " \n\t  ",
			want:    "",
		},
		{
			name:    "exactly the budget is not truncated",
			content: strings.Repeat("b", maxTitleRunes),
			want:    strings.Repeat("b", maxTitleRunes),
		},
		{
			name:    "one rune over the budget truncates",
			content: strings.Repeat("b", maxTitleRunes+1),
			want:    strings.Repeat("b", maxTitleRunes) + "…",
		},
		{
			name:    "long message is cut on a word boundary",
			content: "Please read every file under the sandbox directory and tell me which ones changed today",
			want:    "Please read every file under the sandbox directory and tell…",
		},
		{
			name:    "a first word longer than the budget is hard cut",
			content: longWord + " tail",
			want:    strings.Repeat("a", maxTitleRunes) + "…",
		},
		{
			name: "a word boundary that would leave a stub is ignored",
			// "hi" then a single very long token: cutting at the space would
			// leave a two-character title, so the hard cut is kept instead.
			content: "hi " + longWord,
			want:    ("hi " + longWord)[:maxTitleRunes] + "…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveSessionTitle(tt.content); got != tt.want {
				t.Fatalf("DeriveSessionTitle(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

// Multi-byte input is the case a byte-based truncation gets wrong: it would
// slice through a rune and produce mojibake, or keep only a third of the
// visible characters.
func TestDeriveSessionTitleCountsRunesNotBytes(t *testing.T) {
	content := strings.Repeat("日", maxTitleRunes+10)

	got := DeriveSessionTitle(content)

	want := strings.Repeat("日", maxTitleRunes) + "…"
	if got != want {
		t.Fatalf("multi-byte title = %q, want %q", got, want)
	}
	if runes := []rune(got); len(runes) != maxTitleRunes+1 {
		t.Fatalf("title has %d runes, want %d", len(runes), maxTitleRunes+1)
	}
}

func TestEnqueueUserMessagePersistsSessionUpdatedEvent(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "What is in the sandbox?",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	event := enqueued.SessionUpdatedEvent
	if event.Type != "session.updated" {
		t.Fatalf("event type = %q, want session.updated", event.Type)
	}
	if event.RunID.Valid {
		t.Fatalf("session event has run_id %q", event.RunID.String)
	}
	if event.Sequence >= enqueued.QueuedEvent.Sequence {
		t.Fatalf("session event sequence %d, want before queued event %d", event.Sequence, enqueued.QueuedEvent.Sequence)
	}
	payload := decodeSessionUpdatedPayload(t, event)
	if payload.Title != "What is in the sandbox?" {
		t.Fatalf("event title = %q, want derived title", payload.Title)
	}
	if payload.UpdatedAt == "" {
		t.Fatal("event updatedAt is empty")
	}

	replayed, _, err := repo.ReplayEvents(ctx, session.SessionID, 0, 10)
	if err != nil {
		t.Fatalf("replay events: %v", err)
	}
	if len(replayed) < 2 || replayed[0].EventID != event.EventID {
		t.Fatalf("replayed events = %+v, want session update first", replayed)
	}
}

func TestListLatestSessionUpdatedEventsReturnsOneSnapshotPerSession(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	first, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	input := EnqueueUserMessageInput{
		SessionID:     first.SessionID,
		Content:       "First session",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}
	firstEnqueue, err := repo.EnqueueUserMessage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Content = "Later activity"
	latestFirst, err := repo.EnqueueUserMessage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.SessionID = second.SessionID
	input.Content = "Second session"
	latestSecond, err := repo.EnqueueUserMessage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}

	events, err := repo.ListLatestSessionUpdatedEvents(ctx, 50)
	if err != nil {
		t.Fatalf("list latest session updates: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want one per session", events)
	}
	got := map[string]string{}
	for _, event := range events {
		got[event.SessionID] = event.EventID
	}
	if got[first.SessionID] != latestFirst.SessionUpdatedEvent.EventID {
		t.Fatalf("first latest = %q, want %q (not %q)",
			got[first.SessionID], latestFirst.SessionUpdatedEvent.EventID, firstEnqueue.SessionUpdatedEvent.EventID)
	}
	if got[second.SessionID] != latestSecond.SessionUpdatedEvent.EventID {
		t.Fatalf("second latest = %q, want %q",
			got[second.SessionID], latestSecond.SessionUpdatedEvent.EventID)
	}
}

func TestListLatestSessionUpdatedEventsUsesSessionListBoundAndOrder(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	var oldestSessionID, newestSessionID string
	for i := range 51 {
		session, err := repo.CreateSession(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			oldestSessionID = session.SessionID
		}
		newestSessionID = session.SessionID
		if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
			SessionID:     session.SessionID,
			Content:       "Conversation",
			AgentID:       "general_assistant",
			ModelProvider: "ollama",
			Model:         "qwen2.5:7b",
		}); err != nil {
			t.Fatal(err)
		}
	}

	events, err := repo.ListLatestSessionUpdatedEvents(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 50 {
		t.Fatalf("events = %d, want the 50-row session page", len(events))
	}
	found := map[string]bool{}
	for _, event := range events {
		found[event.SessionID] = true
	}
	if found[oldestSessionID] {
		t.Fatalf("oldest session %s was not bounded out", oldestSessionID)
	}
	if !found[newestSessionID] {
		t.Fatalf("newest session %s is missing", newestSessionID)
	}
}

func TestEnqueueUserMessageTitlesAnUntitledSession(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "What is in the sandbox?",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stored, err := repo.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if stored.Title.String != "What is in the sandbox?" {
		t.Fatalf("title = %q, want the first user message", stored.Title.String)
	}
}

// The title is a first impression, not a running summary: once a conversation
// has a name, later messages must not rewrite it.
func TestEnqueueUserMessageKeepsAnExistingTitle(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	input := EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "first message",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}
	if _, err := repo.EnqueueUserMessage(ctx, input); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}

	input.Content = "second message"
	if _, err := repo.EnqueueUserMessage(ctx, input); err != nil {
		t.Fatalf("second enqueue: %v", err)
	}

	stored, err := repo.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if stored.Title.String != "first message" {
		t.Fatalf("title = %q, want it unchanged after the second message", stored.Title.String)
	}
}

// A session the user named explicitly is theirs, not ours to overwrite.
func TestEnqueueUserMessageDoesNotOverwriteAUserSuppliedTitle(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	session, err := repo.CreateSession(ctx, "Budget planning")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "something entirely unrelated",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stored, err := repo.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if stored.Title.String != "Budget planning" {
		t.Fatalf("title = %q, want the caller-supplied title preserved", stored.Title.String)
	}
}

func TestEnqueueUserMessageDoesNotOverwriteExplicitNewChatTitle(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "New chat")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "This must not replace the explicit title",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stored, err := repo.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if stored.Title.String != "New chat" {
		t.Fatalf("title = %q, want explicit New chat preserved", stored.Title.String)
	}
}

func TestEnqueueUserMessageDoesNotOverwriteDerivedNewChatTitle(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	input := EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "New chat",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}
	if _, err := repo.EnqueueUserMessage(ctx, input); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	input.Content = "This must not replace the derived title"
	if _, err := repo.EnqueueUserMessage(ctx, input); err != nil {
		t.Fatalf("second enqueue: %v", err)
	}

	stored, err := repo.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if stored.Title.String != "New chat" {
		t.Fatalf("title = %q, want derived New chat preserved", stored.Title.String)
	}
}

// Sending a message must move a conversation to the top of the client's list.
// Before updated_at was bumped here, the list was ordered by creation time and
// an old conversation you had just used stayed buried.
func TestEnqueueUserMessageBumpsSessionToTheTopOfTheList(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	older, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create older session: %v", err)
	}
	newer, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create newer session: %v", err)
	}

	sessions, err := repo.ListSessions(ctx, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 2 || sessions[0].SessionID != newer.SessionID {
		t.Fatalf("precondition: expected the newest session first, got %+v", sessions)
	}

	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     older.SessionID,
		Content:       "reviving the old conversation",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	sessions, err = repo.ListSessions(ctx, 10)
	if err != nil {
		t.Fatalf("list sessions after enqueue: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].SessionID != older.SessionID {
		t.Fatalf("expected the messaged session first, got %q", sessions[0].SessionID)
	}
}

// A message with nothing but whitespace leaves the session untitled so the
// client keeps showing its "New chat" placeholder rather than an empty row.
func TestEnqueueUserMessageLeavesWhitespaceOnlyMessagesUntitled(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "   \n\t ",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stored, err := repo.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if stored.Title.Valid && stored.Title.String != "" {
		t.Fatalf("title = %q, want the session left untitled", stored.Title.String)
	}
}

// CreateSession stores NULL for an empty title, so the `title = ”` half of
// the guard is unreachable through the application. It is still the state a
// direct write or a future migration could leave behind, and treating it as
// "already named" would strand the conversation with a blank label forever.
func TestEnqueueUserMessageTitlesASessionWithAnEmptyStringTitle(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE sessions SET title = '' WHERE id = ?`, session.SessionID); err != nil {
		t.Fatalf("force empty title: %v", err)
	}

	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "name me",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stored, err := repo.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if stored.Title.String != "name me" {
		t.Fatalf("title = %q, want the empty title to have been filled", stored.Title.String)
	}
}

// Covers the join between DeriveSessionTitle and the SQL write for a message
// long enough to be truncated — every other enqueue test uses a short one, so
// the stored value and the derived value would agree even if the wrong string
// were bound.
func TestEnqueueUserMessageStoresTheTruncatedTitle(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	content := "Please read every file under the sandbox directory and tell me which ones changed today"

	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       content,
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stored, err := repo.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	want := DeriveSessionTitle(content)
	if stored.Title.String != want {
		t.Fatalf("title = %q, want %q", stored.Title.String, want)
	}
	if stored.Title.String == content {
		t.Fatalf("title was stored untruncated")
	}
}

func TestBackfillSessionTitlesNamesLegacyConversations(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	// What every conversation created before this change looks like: the
	// client's placeholder stored as if it were a real title.
	legacy := seedSession(t, ctx, repo, "New chat", "How do I roast coffee at home?")
	blank := seedSession(t, ctx, repo, "", "Second conversation")
	named := seedSession(t, ctx, repo, "Budget planning", "unrelated content")
	empty := seedSession(t, ctx, repo, "New chat", "")

	renamed, err := repo.BackfillSessionTitles(ctx)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if renamed != 2 {
		t.Fatalf("renamed %d sessions, want 2", renamed)
	}

	assertTitle(t, ctx, repo, legacy, "How do I roast coffee at home?")
	assertTitle(t, ctx, repo, blank, "Second conversation")
	// A title the user chose is theirs, backfill or not.
	assertTitle(t, ctx, repo, named, "Budget planning")
	// Nothing was ever said in it, so there is nothing to name it after.
	assertTitle(t, ctx, repo, empty, "New chat")
}

func TestBackfillSessionTitlesDoesNotOverwriteExplicitNewChatTitle(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "New chat")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := repo.db.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at) VALUES (?, ?, 'user', ?, 'text', 1, ?)`,
		ids.New("msg"), session.SessionID, "This is not the title", now()); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	if _, err := repo.BackfillSessionTitles(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	assertTitle(t, ctx, repo, session.SessionID, "New chat")
}

func TestBackfillSessionTitlesSkipsWhitespaceOnlyUserTurns(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session := seedSession(t, ctx, repo, "New chat", " \n\t ")
	if _, err := repo.db.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at) VALUES (?, ?, 'user', ?, 'text', 2, ?)`,
		ids.New("msg"), session, "First usable turn", now()); err != nil {
		t.Fatalf("insert second message: %v", err)
	}

	if _, err := repo.BackfillSessionTitles(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	assertTitle(t, ctx, repo, session, "First usable turn")
}

// Startup runs this every time, so a second pass must be a no-op rather than
// re-deriving titles over ones already assigned.
func TestBackfillSessionTitlesIsIdempotent(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session := seedSession(t, ctx, repo, "New chat", "first thing said")

	if _, err := repo.BackfillSessionTitles(ctx); err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	renamed, err := repo.BackfillSessionTitles(ctx)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if renamed != 0 {
		t.Fatalf("second pass renamed %d sessions, want 0", renamed)
	}
	assertTitle(t, ctx, repo, session, "first thing said")
}

// The backfill and the live path must not disagree about what a message is
// called, or the same conversation would be named differently depending on
// whether it was open when the feature shipped.
func TestBackfillMatchesTheLiveTitleForTheSameMessage(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	content := "Please read every file under the sandbox directory and tell me which ones changed today"

	backfilled := seedSession(t, ctx, repo, "New chat", content)
	if _, err := repo.BackfillSessionTitles(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	live, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     live.SessionID,
		Content:       content,
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	backfilledSession, err := repo.GetSession(ctx, backfilled)
	if err != nil {
		t.Fatalf("get backfilled: %v", err)
	}
	liveSession, err := repo.GetSession(ctx, live.SessionID)
	if err != nil {
		t.Fatalf("get live: %v", err)
	}
	if backfilledSession.Title.String != liveSession.Title.String {
		t.Fatalf("backfilled title %q != live title %q",
			backfilledSession.Title.String, liveSession.Title.String)
	}
}

// A conversation still carrying the placeholder gets named the moment it is
// used again, even if the startup pass never reached it.
func TestEnqueueUserMessageReplacesTheLegacyPlaceholder(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session := seedSession(t, ctx, repo, "New chat", "an older message")

	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session,
		Content:       "a newer message",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	assertTitle(t, ctx, repo, session, "a newer message")
}

// seedSession creates a session with the given stored title and, unless
// content is empty, one user message — the shape a conversation had before
// backend naming existed.
func seedSession(t *testing.T, ctx context.Context, repo *Repository, title, content string) string {
	t.Helper()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	// CreateSession stores NULL for "", so force the exact stored value under
	// test rather than trusting it to round-trip.
	titleOrigin := "explicit"
	if title == "" || title == "New chat" {
		titleOrigin = "unset"
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE sessions SET title = ?, title_origin = ? WHERE id = ?`,
		nullableString(sql.NullString{String: title, Valid: title != ""}), titleOrigin, session.SessionID); err != nil {
		t.Fatalf("set title: %v", err)
	}
	if content != "" {
		if _, err := repo.db.ExecContext(ctx,
			`INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at) VALUES (?, ?, 'user', ?, 'text', 1, ?)`,
			ids.New("msg"), session.SessionID, content, now()); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}
	return session.SessionID
}

func assertTitle(t *testing.T, ctx context.Context, repo *Repository, sessionID, want string) {
	t.Helper()
	session, err := repo.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("get session %s: %v", sessionID, err)
	}
	if session.Title.String != want {
		t.Fatalf("session %s title = %q, want %q", sessionID, session.Title.String, want)
	}
}

func newTitleTestRepo(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	return New(openTestDB(t)), context.Background()
}
