package repository

import (
	"context"
	"strings"
	"testing"
)

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

func newTitleTestRepo(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	return New(openTestDB(t)), context.Background()
}
