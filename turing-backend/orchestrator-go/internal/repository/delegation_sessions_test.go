package repository

import (
	"context"
	"errors"
	"testing"
)

// markDelegation turns a session into a hidden child session. PR 2b creates
// them through delegation; until then a test builds one directly.
func markDelegation(t *testing.T, ctx context.Context, repo *Repository, sessionID string) {
	t.Helper()
	if _, err := repo.db.ExecContext(ctx, `UPDATE sessions SET kind = 'delegation' WHERE id = ?`, sessionID); err != nil {
		t.Fatal(err)
	}
}

func localTurn(sessionID, content string) EnqueueUserMessageInput {
	return EnqueueUserMessageInput{
		SessionID: sessionID, Content: content, AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	}
}

func TestDelegationSessionsAreLeftOutOfEverySessionList(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	chat := seedSession(t, ctx, repo, "Chat", "")
	child := seedSession(t, ctx, repo, "Child", "")
	markDelegation(t, ctx, repo, child)
	for _, filter := range []SessionListFilter{SessionListActive, SessionListAll} {
		sessions, err := repo.ListSessionsPage(ctx, ListSessionsInput{Filter: filter, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(sessions) != 1 || sessions[0].SessionID != chat {
			t.Fatalf("%s list = %+v, want only the chat", filter, sessions)
		}
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE sessions SET status = 'archived'`); err != nil {
		t.Fatal(err)
	}
	archived, err := repo.ListSessionsPage(ctx, ListSessionsInput{Filter: SessionListArchived, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 || archived[0].SessionID != chat {
		t.Fatalf("archived list = %+v, want only the chat", archived)
	}
}

// Search and automatic recall share one predicate, so a specialist's work
// never comes back as a recalled snippet in another conversation.
func TestDelegationSessionsAreLeftOutOfSearchAndRecall(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	chat := seedSession(t, ctx, repo, "Chat", "the quokka migration")
	child := seedSession(t, ctx, repo, "Child", "the quokka brief")
	markDelegation(t, ctx, repo, child)

	for _, scope := range []string{"", child} {
		messages, err := repo.SearchMessages(ctx, scope, "", "quokka", 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range messages {
			if message.SessionID == child {
				t.Fatalf("search scoped %q found the child's message %+v", scope, message)
			}
		}
		hits, err := repo.SearchMessageHits(ctx, scope, "", "quokka", 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range hits {
			if hit.Message.SessionID == child {
				t.Fatalf("hits scoped %q found the child's message %+v", scope, hit)
			}
		}
	}
	messages, err := repo.SearchMessages(ctx, "", "", "quokka", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].SessionID != chat {
		t.Fatalf("unscoped search = %+v, want only the chat's message", messages)
	}
}

func TestDelegationSessionsAreNeverTitledByTheBackfill(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	chat := seedSession(t, ctx, repo, "", "How do I roast coffee?")
	child := seedSession(t, ctx, repo, "", "Brief: list the sandbox files")
	markDelegation(t, ctx, repo, child)
	renamed, err := repo.BackfillSessionTitles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if renamed != 1 {
		t.Fatalf("renamed %d sessions, want only the chat", renamed)
	}
	assertTitle(t, ctx, repo, chat, "How do I roast coffee?")
	assertTitle(t, ctx, repo, child, "")
}

func TestDelegationSessionsAreLeftOutOfTheSessionUpdateSnapshot(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	chat := seedSession(t, ctx, repo, "", "")
	child := seedSession(t, ctx, repo, "", "")
	for _, id := range []string{chat, child} {
		if _, err := repo.EnqueueUserMessage(ctx, localTurn(id, "hello")); err != nil {
			t.Fatal(err)
		}
	}
	markDelegation(t, ctx, repo, child)
	events, err := repo.ListLatestSessionUpdatedEvents(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].SessionID != chat {
		t.Fatalf("snapshot = %+v, want only the chat's update", events)
	}
}

// Every public mutation is refused on a child session, including an
// idempotent replay of a send that was accepted before it became one: the
// guard runs before the replay lookup.
func TestPublicSessionMutationsRefuseADelegationSession(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	child := seedSession(t, ctx, repo, "Child", "")
	keyed := localTurn(child, "first")
	keyed.IdempotencyKey = "send-once"
	if _, err := repo.EnqueueUserMessage(ctx, keyed); err != nil {
		t.Fatal(err)
	}
	markDelegation(t, ctx, repo, child)
	var runsBefore int
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs WHERE session_id = ?`, child).Scan(&runsBefore); err != nil {
		t.Fatal(err)
	}

	refusals := map[string]func() error{
		"enqueue": func() error { _, err := repo.EnqueueUserMessage(ctx, localTurn(child, "more")); return err },
		"replay":  func() error { _, err := repo.EnqueueUserMessage(ctx, keyed); return err },
		"rename":  func() error { _, err := repo.RenameSession(ctx, child, "Renamed"); return err },
		"archive": func() error { _, err := repo.ArchiveSession(ctx, child); return err },
		"restore": func() error { _, err := repo.RestoreSession(ctx, child); return err },
		"route":   func() error { _, err := repo.SetSessionAgent(ctx, child, "agent_any"); return err },
		"unroute": func() error { return repo.ClearSessionAgent(ctx, child) },
		"delete":  func() error { _, err := repo.BeginSessionDeletion(ctx, child); return err },
	}
	for name, mutate := range refusals {
		if err := mutate(); !errors.Is(err, ErrDelegationSessionReadOnly) {
			t.Fatalf("%s on a delegation session: %v, want ErrDelegationSessionReadOnly", name, err)
		}
	}

	var runsAfter, receipts int
	if err := repo.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM agent_runs WHERE session_id = ?),
			(SELECT COUNT(*) FROM session_deletions WHERE session_id = ?)`, child, child).Scan(&runsAfter, &receipts); err != nil {
		t.Fatal(err)
	}
	if runsAfter != runsBefore || receipts != 0 {
		t.Fatalf("runs %d -> %d, receipts %d: a refused mutation wrote something", runsBefore, runsAfter, receipts)
	}
	session, err := repo.GetSession(ctx, child)
	if err != nil || session.Title.String != "Child" || session.Status != "active" {
		t.Fatalf("GetSession = %+v, %v; want the unchanged, readable child", session, err)
	}
	if _, err := repo.ListMessages(ctx, child, 10); err != nil {
		t.Fatalf("ListMessages on a delegation session: %v", err)
	}
	state, err := repo.SessionWithdrawalState(ctx, child)
	if err != nil || !state.Active || state.Kind != "delegation" {
		t.Fatalf("withdrawal state = %+v, %v; want active delegation", state, err)
	}
}

// The guard is about the kind, not about the session: a chat passes every
// one of the same mutations.
func TestPublicSessionMutationsStillWorkOnAChat(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	chat := seedSession(t, ctx, repo, "Chat", "")
	if _, err := repo.EnqueueUserMessage(ctx, localTurn(chat, "hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RenameSession(ctx, chat, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ArchiveSession(ctx, chat); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RestoreSession(ctx, chat); err != nil {
		t.Fatal(err)
	}
	state, err := repo.SessionWithdrawalState(ctx, chat)
	if err != nil || state.Kind != "chat" {
		t.Fatalf("withdrawal state = %+v, %v; want chat", state, err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, chat); err != nil {
		t.Fatal(err)
	}
}

// The client may still cancel a child's run: cancellation is how a user stops
// a specialist, and it is not a mutation of the session itself.
func TestTheRunInADelegationSessionCanStillBeCancelled(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	child := seedSession(t, ctx, repo, "Child", "")
	enqueued, err := repo.EnqueueUserMessage(ctx, localTurn(child, "brief"))
	if err != nil {
		t.Fatal(err)
	}
	markDelegation(t, ctx, repo, child)
	result, err := repo.CancelUserRun(ctx, child, enqueued.RunID, "stop-the-specialist")
	if err != nil || !result.Accepted {
		t.Fatalf("CancelUserRun on the child's run = %+v, %v; want accepted", result, err)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil || run.Status != "cancelled" {
		t.Fatalf("child run = %+v, %v; want cancelled", run, err)
	}
}
