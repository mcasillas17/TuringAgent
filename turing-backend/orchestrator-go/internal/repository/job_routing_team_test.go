package repository

import (
	"context"
	"testing"
	"time"
)

// requireTeamProtocol stamps a queued job the way PR 2b's producers will:
// children, continuations, roster-carrying parents and later turns in a
// delegating session all carry this payload key.
func requireTeamProtocol(t *testing.T, ctx context.Context, repo *Repository, jobID string) {
	t.Helper()
	if _, err := repo.db.ExecContext(ctx, `
		UPDATE jobs SET payload_json = json_set(payload_json, '$.minimumTeamProtocolVersion', 1)
		WHERE id = ?`, jobID); err != nil {
		t.Fatal(err)
	}
}

func enqueueLocalTurn(t *testing.T, ctx context.Context, repo *Repository, title string) EnqueueUserMessageResult {
	t.Helper()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: title, AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	return enqueued
}

func localWorkerAt(teamProtocolVersion int) *WorkerRoutingCapabilities {
	return &WorkerRoutingCapabilities{
		Models:              []RoutingModelCapability{{Provider: "ollama", Model: "llama3.2", MaxContextTokens: 8192}},
		MaxConcurrentRuns:   1,
		TeamProtocolVersion: teamProtocolVersion,
	}
}

// An empty tool set matches every model-compatible worker, so only the
// version itself can keep an older worker off a team job. The older worker
// still claims unrelated work queued behind it.
func TestClaimKeepsTeamProtocolJobsFromOlderWorkers(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	gated := enqueueLocalTurn(t, ctx, repo, "Needs the team protocol")
	requireTeamProtocol(t, ctx, repo, gated.JobID)
	plain := enqueueLocalTurn(t, ctx, repo, "Plain turn")

	var seen []RoutingRequirements
	record := func(route RoutingRequirements) bool { seen = append(seen, route); return true }
	claimed, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-v0", 0, time.Hour, localWorkerAt(0), record)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != plain.JobID {
		t.Fatalf("version-0 worker claimed %q, want the plain job %q (gated %q)", claimed.JobID, plain.JobID, gated.JobID)
	}
	if len(seen) != 1 || seen[0].MinimumTeamProtocolVersion != 0 {
		t.Fatalf("version-0 worker was offered %+v, want only the plain job", seen)
	}

	seen = nil
	claimed, err = repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-v1", 0, time.Hour, localWorkerAt(1), record)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != gated.JobID || claimed.MinimumTeamProtocolVersion != 1 {
		t.Fatalf("version-1 worker claimed %q (minimum %d), want %q at 1", claimed.JobID, claimed.MinimumTeamProtocolVersion, gated.JobID)
	}
	if len(seen) != 1 || seen[0].MinimumTeamProtocolVersion != 1 {
		t.Fatalf("compatibility check saw %+v, want the gated job's minimum", seen)
	}
}

// A job queued before the key existed has none, and must stay claimable by
// every worker that could run it before.
func TestClaimTreatsAMissingTeamProtocolKeyAsZero(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	plain := enqueueLocalTurn(t, ctx, repo, "Legacy turn")
	var payload string
	if err := repo.db.QueryRowContext(ctx, `SELECT payload_json FROM jobs WHERE id = ?`, plain.JobID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var present int
	if err := repo.db.QueryRowContext(ctx, `SELECT json_type(?, '$.minimumTeamProtocolVersion') IS NOT NULL`, payload).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if present != 0 {
		t.Fatalf("an ordinary turn carries the team key: %s", payload)
	}
	claimed, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-v0", 0, time.Hour, localWorkerAt(0), nil)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != plain.JobID {
		t.Fatalf("version-0 worker claimed %q, want %q", claimed.JobID, plain.JobID)
	}
}

// TUR-010 reads queued work through this page, so a gated job is reported as
// waiting for a team-protocol worker rather than as routable.
func TestListPendingRoutingWorkPageCopiesTheTeamProtocolMinimum(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	gated := enqueueLocalTurn(t, ctx, repo, "Gated")
	requireTeamProtocol(t, ctx, repo, gated.JobID)
	plain := enqueueLocalTurn(t, ctx, repo, "Plain")

	work, _, err := repo.ListPendingRoutingWorkPage(ctx, PendingRoutingCursor{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, item := range work {
		got[item.RunID] = item.Requirements.MinimumTeamProtocolVersion
	}
	if got[gated.RunID] != 1 || got[plain.RunID] != 0 || len(got) != 2 {
		t.Fatalf("minimums = %v, want gated 1 and plain 0", got)
	}
}
