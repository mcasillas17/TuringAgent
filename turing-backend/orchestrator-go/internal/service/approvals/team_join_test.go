package approvals

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

type countingDispatcher struct{ calls atomic.Int32 }

func (d *countingDispatcher) DispatchPending(context.Context) error {
	d.calls.Add(1)
	return nil
}

// A delegated task that ends because the user denied its approval can be the
// last of its parent's tasks to finish. The join that triggers lands on the
// parent's stream live, like every other event the denial commits, and its
// continuation is handed to a free worker straight away: no worker has ever
// held it, so nothing else would.
func TestDenyingADelegatedTasksApprovalPublishesTheJoin(t *testing.T) {
	h := newApprovalHarness(t)
	dispatcher := &countingDispatcher{}
	h.service.SetDispatcher(dispatcher)
	ctx := context.Background()
	if err := h.repo.UpsertTools(ctx, []repository.DiscoveredTool{
		{ServerName: "team", ToolName: "team.delegate", SchemaJSON: `{}`, Policy: "safe"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(ctx, `
		INSERT INTO agent_profile_settings (profile_id, enabled, granted_revision, granted_at, updated_at)
		VALUES ('research', 1, 'rev-1', '2026-10-05T00:00:00Z', '2026-10-05T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	session, err := h.repo.CreateSession(ctx, "Turing")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "prep me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
		TeamRoster: []repository.TeamRosterEntry{{
			ProfileID: "research", Revision: "rev-1", Name: "Research", Emoji: "🔬", Description: "Looks things up",
		}},
		TeamContinuation: repository.TeamContinuation{Tools: []string{}, ResultMaxBytes: 4096},
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := &repository.WorkerRoutingCapabilities{
		Models:            []repository.RoutingModelCapability{{Provider: "ollama", Model: "llama3.2", MaxContextTokens: 8192}},
		Tools:             []string{"files/files.update"},
		MaxConcurrentRuns: 2, TeamProtocolVersion: 1,
	}
	claim := func() repository.Job {
		claimed, err := h.repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "team-worker", 0, time.Hour,
			worker, func(repository.RoutingRequirements) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		return claimed
	}
	claimedParent := claim()
	created, err := h.repo.CreateDelegation(ctx, repository.CreateDelegationInput{
		ParentRunID: parent.RunID, AssignmentAttemptID: claimedParent.AssignmentAttemptID, ToolCallID: "call_1",
		ArgsHash: "sha256:call_1", Policy: "safe", MaxPerRun: 3,
		Profile: repository.AgentProfileSnapshot{
			ProfileID: "research", Revision: "rev-1", DisplayName: "Research", Emoji: "🔬", Instructions: "You are Research.",
		},
		ModelProvider: "ollama", Model: "llama3.2", SelectedTools: []string{"files/files.update"},
		Brief: "BEGIN TURING_RETRIEVED_DELEGATION_BRIEF_x\nTask:\nUpdate the note\nEND TURING_RETRIEVED_DELEGATION_BRIEF_x",
	})
	if err != nil {
		t.Fatal(err)
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
	child := created.Delegation
	if claimed := claim(); claimed.RunID != child.ChildRunID {
		t.Fatalf("claimed %s, want the child %s", claimed.RunID, child.ChildRunID)
	}
	if err := h.repo.RecordToolCallBefore(ctx, repository.ToolCallRecord{
		ToolCallID: "call_child", RunID: child.ChildRunID, ModelToolCallID: "model_call_child",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:child"); err != nil {
		t.Fatal(err)
	}
	approvalID, err := h.service.CreateApprovalForTool(ctx, child.ChildRunID, "call_child", "general_assistant",
		"files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	published, unsubscribe := h.bus.Subscribe(session.SessionID)
	defer unsubscribe()

	if _, err := newReviewedClient(t, h).DenyApproval(ctx, &turingv1.DenyApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	var seen []string
	for len(seen) < 2 {
		select {
		case event := <-published:
			if event.Type == "delegation.finished" || (event.Type == "agent.run.queued" && event.RunID != parent.RunID) {
				seen = append(seen, event.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("published join events = %v, want delegation.finished and the continuation queued", seen)
		}
	}
	if got := dispatcher.calls.Load(); got != 1 {
		t.Fatalf("dispatches = %d, want the continuation dispatched once", got)
	}
}
