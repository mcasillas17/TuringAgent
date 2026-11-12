package team

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"google.golang.org/grpc/codes"
)

// A task's specialist counts as active only as the task was granted: enabled,
// granted at the task's revision, and resolving Active from its file now.
// Editing its instructions changes no authority field, so it does not revoke.
func TestATasksSpecialistIsActiveOnlyAsItWasGranted(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")
	revision := h.profile(t, "research").GetRevision()
	active := func(t *testing.T, profileID, revision string, want bool) {
		t.Helper()
		got, err := h.server.DelegatedProfileActive(ctx, profileID, revision)
		if err != nil || got != want {
			t.Fatalf("active(%s, %s) = %v, %v; want %v", profileID, revision, got, err, want)
		}
	}

	active(t, "research", revision, true)
	active(t, "research", "rev-other", false)
	active(t, "nobody", revision, false)

	h.writeFile(t, "research", "---\n"+researchFrontmatter+"---\nYou are a careful specialist.\n")
	active(t, "research", revision, true)

	h.write(t, "research", strings.Replace(researchFrontmatter, "max_tool_calls: 12", "max_tool_calls: 8", 1))
	active(t, "research", revision, false)
	h.write(t, "research", researchFrontmatter)
	active(t, "research", revision, true)

	if _, err := h.server.SetAgentProfileEnabled(ctx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "research"}); err != nil {
		t.Fatal(err)
	}
	active(t, "research", revision, false)
	h.activate(t, "research")
	active(t, "research", revision, true)

	if err := os.RemoveAll(filepath.Join(h.root, "research")); err != nil {
		t.Fatal(err)
	}
	active(t, "research", revision, false)
}

// A caller whose context ends while the profile is being resolved learns
// that, not that the profile is inactive: the routing reads a resolver makes
// fail on an ended context, which would otherwise read as no model served.
func TestAnEndedResolutionIsNotAnInactiveProfile(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")
	revision := h.profile(t, "research").GetRevision()
	ctx, cancel := context.WithCancel(context.Background())
	h.routes.onCapabilities = cancel

	if active, err := h.server.DelegatedProfileActive(ctx, "research", revision); !errors.Is(err, context.Canceled) || active {
		t.Fatalf("active = %v, %v; want the ended context reported", active, err)
	}
}

// Consent is never signed or recorded for a task whose specialist no longer
// resolves as it was granted, and a consent disclosed before the change is
// refused without touching the task.
func TestConsentIsRefusedForATaskWhoseSpecialistChanged(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, h *harness)
	}{
		{name: "authority field edited", change: func(t *testing.T, h *harness) {
			frontmatter := strings.Replace(researchFrontmatter, "system.time]", "system.time, github.get_issue]", 1)
			h.write(t, "research", strings.Replace(frontmatter, "max_tool_calls: 12", "max_tool_calls: 8", 1))
		}},
		{name: "file deleted", change: func(t *testing.T, h *harness) {
			if err := os.RemoveAll(filepath.Join(h.root, "research")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "disabled", change: func(t *testing.T, h *harness) {
			if _, err := h.server.SetAgentProfileEnabled(context.Background(),
				&turingv1.SetAgentProfileEnabledRequest{ProfileId: "research"}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			_, id := h.consentTask(t, "")
			disclosure := h.prepare(t, id)
			tc.change(t, h)

			_, err := h.server.PrepareDelegationEgress(context.Background(), &turingv1.PrepareDelegationEgressRequest{DelegationId: id})
			wantCode(t, err, codes.FailedPrecondition)
			_, err = h.decide(id, allow(disclosure))
			wantCode(t, err, codes.FailedPrecondition)
			record, err := h.repo.GetDelegation(context.Background(), id)
			if err != nil || record.State != repository.DelegationWaitingConsent || record.EgressConsent != "pending" {
				t.Fatalf("delegation = %+v, %v; want it still waiting, with nothing recorded", record, err)
			}
		})
	}
}

// backlog is a profile's queued work: parents Turing turns, each completed
// after delegating perParent tasks to Research, every other one held for the
// user's consent to a GitHub tool.
type backlog struct {
	parents  []repository.EnqueueUserMessageResult
	children int
}

func (h *harness) seedBacklog(t *testing.T, parents, perParent int) backlog {
	t.Helper()
	ctx := context.Background()
	h.routes.tools[defaultModel] = append(h.routes.tools[defaultModel], "integrations/github.get_issue")
	frontmatter := strings.Replace(researchFrontmatter, "system.time]", "system.time, github.get_issue]", 1)
	h.write(t, "research", frontmatter)
	h.activate(t, "research")
	h.addTool(t, repository.DiscoveredTool{ServerName: "team", ToolName: "team.delegate", SchemaJSON: `{}`, Policy: "safe"})
	revision := h.profile(t, "research").GetRevision()
	roster := []repository.TeamRosterEntry{{ProfileID: "research", Revision: revision, Name: "Research"}}
	worker := &repository.WorkerRoutingCapabilities{
		Models:            []repository.RoutingModelCapability{{Provider: "ollama", Model: defaultModel, MaxContextTokens: 8192}},
		MaxConcurrentRuns: 1, TeamProtocolVersion: 1,
	}
	var seeded backlog
	attempts := map[string]string{}
	for range parents {
		session, err := h.repo.CreateSession(ctx, "Turing")
		if err != nil {
			t.Fatal(err)
		}
		parent, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: "prep me", AgentID: "general_assistant",
			ModelProvider: "ollama", Model: defaultModel, TeamRoster: roster,
			TeamContinuation: repository.TeamContinuation{Tools: []string{}, ResultMaxBytes: 256},
		})
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := h.repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "team-worker", 0, time.Hour,
			worker, func(repository.RoutingRequirements) bool { return true })
		if err != nil || claimed.RunID != parent.RunID {
			t.Fatalf("claim = %+v, %v; want parent %s", claimed, err, parent.RunID)
		}
		attempts[parent.RunID] = claimed.AssignmentAttemptID
		seeded.parents = append(seeded.parents, parent)
	}
	for _, parent := range seeded.parents {
		for index := range perParent {
			input := repository.CreateDelegationInput{
				ParentRunID: parent.RunID, AssignmentAttemptID: attempts[parent.RunID],
				ToolCallID: fmt.Sprintf("call_%d", index), ArgsHash: fmt.Sprintf("sha256:%d", index),
				Policy: "safe", MaxPerRun: perParent,
				Profile: repository.AgentProfileSnapshot{
					ProfileID: "research", Revision: revision, DisplayName: "Research", Emoji: "🔬", Instructions: "You research.",
				},
				ModelProvider: "ollama", Model: defaultModel, SelectedTools: []string{"system/system.time"},
				Brief: "BEGIN TURING_RETRIEVED_DELEGATION_BRIEF_x\nTask:\nGather\nEND TURING_RETRIEVED_DELEGATION_BRIEF_x",
				Task:  "Gather",
			}
			if index%2 == 1 {
				input.EgressTools = []string{"integrations/github.get_issue"}
			}
			if _, err := h.repo.CreateDelegation(ctx, input); err != nil {
				t.Fatal(err)
			}
			seeded.children++
		}
		state, err := h.repo.GetRunState(ctx, parent.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.repo.CompleteRunCanonical(ctx, repository.CompleteRunInput{
			RunID: parent.RunID, AssistantMessageID: parent.AssistantMessageID, Content: "Asked Research.",
			ExpectedStateVersion: state.StateVersion,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return seeded
}

func (h *harness) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	if err := h.database.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// wantEveryTaskWithdrawnAndJoinedOnce asserts every task of the backlog ended
// profile_grant_revoked, never run, and that each parent was joined exactly
// once, with one continuation.
func (h *harness) wantEveryTaskWithdrawnAndJoinedOnce(t *testing.T, seeded backlog) {
	t.Helper()
	if got := h.count(t, `
		SELECT COUNT(*) FROM delegations d JOIN agent_runs child ON child.id = d.child_run_id
		WHERE child.status = 'cancelled' AND child.outcome_reason = ? AND child.cancellation_reason = ?
			AND child.started_at IS NULL`,
		string(runoutcome.ReasonAbandoned), runoutcome.CodeProfileGrantRevoked); got != seeded.children {
		t.Fatalf("withdrawn tasks = %d, want all %d", got, seeded.children)
	}
	for _, parent := range seeded.parents {
		joins := h.count(t, `SELECT COUNT(*) FROM messages WHERE session_id = ? AND content_type = 'delegation_results'`, parent.SessionID)
		continuations := h.count(t, `SELECT COUNT(*) FROM agent_runs WHERE continues_run_id = ?`, parent.RunID)
		unjoined := h.count(t, `SELECT COUNT(*) FROM delegations WHERE parent_run_id = ? AND joined = 0`, parent.RunID)
		if joins != 1 || continuations != 1 || unjoined != 0 {
			t.Fatalf("parent %s: joins %d, continuations %d, unjoined %d; want it joined exactly once",
				parent.RunID, joins, continuations, unjoined)
		}
	}
}

// Disabling a specialist with 10,000 queued and consent-held tasks writes one
// setting row; the fence stops every claim from that commit on, and the
// maintenance duty then withdraws them in transactions of at most 100, while
// other work keeps being claimed between them. Every task ends
// profile_grant_revoked and every turn is joined exactly once.
func TestDisablingASpecialistWithALongBacklogWithdrawsItInBatches(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	seeded := h.seedBacklog(t, 200, 50)
	if seeded.children != 10000 {
		t.Fatalf("seeded %d tasks, want 10000", seeded.children)
	}

	before := h.count(t, `SELECT total_changes()`)
	if _, err := h.server.SetAgentProfileEnabled(ctx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "research"}); err != nil {
		t.Fatal(err)
	}
	if touched := h.count(t, `SELECT total_changes()`) - before; touched != 1 {
		t.Fatalf("disabling touched %d rows, want the one setting row", touched)
	}

	var batches []int
	var attended repository.EnqueueUserMessageResult
	var claimedDuring repository.Job
	h.server.revocationBatchDone = func(cancelled int) {
		batches = append(batches, cancelled)
		if len(batches) != 1 {
			return
		}
		session, err := h.repo.CreateSession(ctx, "Attended")
		if err != nil {
			t.Error(err)
			return
		}
		if attended, err = h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant", ModelProvider: "ollama", Model: defaultModel,
		}); err != nil {
			t.Error(err)
			return
		}
		// A worker older than the team protocol can claim only the attended
		// turn, never a task or a continuation.
		claimedDuring, err = h.repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "attended-worker", 0, time.Hour,
			&repository.WorkerRoutingCapabilities{
				Models:            []repository.RoutingModelCapability{{Provider: "ollama", Model: defaultModel, MaxContextTokens: 8192}},
				MaxConcurrentRuns: 1,
			}, func(repository.RoutingRequirements) bool { return true })
		if err != nil {
			t.Error(err)
		}
	}
	if err := h.server.CancelRevokedDelegations(ctx); err != nil {
		t.Fatal(err)
	}

	total := 0
	for index, cancelled := range batches {
		if cancelled > repository.MaxRevokedDelegationBatch {
			t.Fatalf("batch %d withdrew %d tasks, want at most %d", index, cancelled, repository.MaxRevokedDelegationBatch)
		}
		total += cancelled
	}
	if total != seeded.children || len(batches) < seeded.children/repository.MaxRevokedDelegationBatch {
		t.Fatalf("batches = %v (total %d), want all %d tasks in batches of at most 100", batches, total, seeded.children)
	}
	if attended.RunID == "" || claimedDuring.RunID != attended.RunID {
		t.Fatalf("claimed during the batches = %+v, want the attended turn %s", claimedDuring, attended.RunID)
	}
	h.wantEveryTaskWithdrawnAndJoinedOnce(t, seeded)
	if h.routes.dispatched == 0 {
		t.Fatal("the continuations the batches queued were never dispatched")
	}
}

// A crash between batches loses nothing: there is no marker to lose, and the
// next tick withdraws the rest.
func TestARevocationInterruptedBetweenBatchesResumesOnTheNextTick(t *testing.T) {
	h := newHarness(t)
	seeded := h.seedBacklog(t, 5, 50)
	// Re-granted at a new revision: the tasks' own revision no longer holds.
	frontmatter := strings.Replace(researchFrontmatter, "system.time]", "system.time, github.get_issue]", 1)
	h.write(t, "research", strings.Replace(frontmatter, "max_tool_calls: 12", "max_tool_calls: 8", 1))
	h.activate(t, "research")

	crashed, crash := context.WithCancel(context.Background())
	h.server.revocationBatchDone = func(int) { crash() }
	if err := h.server.CancelRevokedDelegations(crashed); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted duty = %v, want it cut short", err)
	}
	if left := h.count(t, `SELECT COUNT(*) FROM agent_runs WHERE status = 'queued' AND id IN (SELECT child_run_id FROM delegations)`); left != seeded.children-repository.MaxRevokedDelegationBatch {
		t.Fatalf("queued tasks after the crash = %d, want %d", left, seeded.children-repository.MaxRevokedDelegationBatch)
	}

	restarted := New(h.repo, h.routes, defaultModel, true)
	if err := restarted.CancelRevokedDelegations(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.wantEveryTaskWithdrawnAndJoinedOnce(t, seeded)
}
