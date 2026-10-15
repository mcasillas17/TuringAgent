package approvals

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
)

// recordingRunControl counts dispatches and records the runs it is asked to
// cancel, with their reasons.
type recordingRunControl struct {
	calls     atomic.Int32
	mu        sync.Mutex
	cancelled []string
}

func (d *recordingRunControl) DispatchPending(context.Context) error {
	d.calls.Add(1)
	return nil
}

func (d *recordingRunControl) CancelRun(_ context.Context, runID string, reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cancelled = append(d.cancelled, runID+":"+reason)
}

// A delegated task that ends because the user denied its approval can be the
// last of its parent's tasks to finish. The join that triggers lands on the
// parent's stream live, like every other event the denial commits, and its
// continuation is handed to a free worker straight away: no worker has ever
// held it, so nothing else would.
func TestDenyingADelegatedTasksApprovalPublishesTheJoin(t *testing.T) {
	h := newApprovalHarness(t)
	dispatcher := &recordingRunControl{}
	h.service.SetRunControl(dispatcher)
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

// Denying a turn's own approval fails the turn, which stops the tasks it
// started: the stopping is published on the turn's stream, and the runtime is
// asked to let each task's worker go.
func TestDenyingATurnsApprovalStopsItsTasks(t *testing.T) {
	h := newApprovalHarness(t)
	ctx := context.Background()
	runControl := &recordingRunControl{}
	h.service.SetRunControl(runControl)
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
		TeamRoster:       []repository.TeamRosterEntry{{ProfileID: "research", Revision: "rev-1", Name: "Research", Emoji: "🔬"}},
		TeamContinuation: repository.TeamContinuation{Tools: []string{}, ResultMaxBytes: 4096},
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := h.repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "team-worker", 0, time.Hour,
		&repository.WorkerRoutingCapabilities{
			Models:            []repository.RoutingModelCapability{{Provider: "ollama", Model: "llama3.2", MaxContextTokens: 8192}},
			MaxConcurrentRuns: 1, TeamProtocolVersion: 1,
		}, func(repository.RoutingRequirements) bool { return true })
	if err != nil || claimed.RunID != parent.RunID {
		t.Fatalf("parent claim = %+v, %v", claimed, err)
	}
	created, err := h.repo.CreateDelegation(ctx, repository.CreateDelegationInput{
		ParentRunID: parent.RunID, AssignmentAttemptID: claimed.AssignmentAttemptID, ToolCallID: "call_1",
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
	if err := h.repo.RecordToolCallBefore(ctx, repository.ToolCallRecord{
		ToolCallID: "call_parent", RunID: parent.RunID, ModelToolCallID: "model_call_parent",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:parent"); err != nil {
		t.Fatal(err)
	}
	approvalID, err := h.service.CreateApprovalForTool(ctx, parent.RunID, "call_parent", "general_assistant",
		"files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	published, unsubscribe := h.bus.Subscribe(session.SessionID)
	defer unsubscribe()

	if _, err := newReviewedClient(t, h).DenyApproval(ctx, &turingv1.DenyApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	for seen := false; !seen; {
		select {
		case event := <-published:
			seen = event.Type == "delegation.updated"
		case <-time.After(time.Second):
			t.Fatal("the stopped task's delegation.updated was not published on the turn's stream")
		}
	}
	runControl.mu.Lock()
	defer runControl.mu.Unlock()
	if want := []string{created.Delegation.ChildRunID + ":" + runoutcome.CodeParentRunEnded}; !slices.Equal(runControl.cancelled, want) {
		t.Fatalf("cancelled = %v, want %v", runControl.cancelled, want)
	}
}

// runningTaskAboutToAsk is a running task whose worker has announced a
// files.update call, ready to ask for its approval; it returns the turn's
// conversation and the task's run.
func runningTaskAboutToAsk(t *testing.T, h *approvalHarness) (string, string) {
	t.Helper()
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
		TeamRoster:       []repository.TeamRosterEntry{{ProfileID: "research", Revision: "rev-1", Name: "Research"}},
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
			ProfileID: "research", Revision: "rev-1", DisplayName: "Research", Instructions: "You are Research.",
		},
		ModelProvider: "ollama", Model: "llama3.2", SelectedTools: []string{"files/files.update"},
		Brief: "BEGIN TURING_RETRIEVED_DELEGATION_BRIEF_x\nTask:\nUpdate the note\nEND TURING_RETRIEVED_DELEGATION_BRIEF_x",
	})
	if err != nil {
		t.Fatal(err)
	}
	child := created.Delegation
	if claimed := claim(); claimed.RunID != child.ChildRunID {
		t.Fatalf("claimed %s, want the task", claimed.RunID)
	}
	if err := h.repo.RecordToolCallBefore(ctx, repository.ToolCallRecord{
		ToolCallID: "call_child", RunID: child.ChildRunID, ModelToolCallID: "model_call_child",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:child"); err != nil {
		t.Fatal(err)
	}
	return session.SessionID, child.ChildRunID
}

// nextDelegationUpdate is the next delegation.updated published on a stream.
func nextDelegationUpdate(t *testing.T, published <-chan events.Event) string {
	t.Helper()
	for {
		select {
		case event := <-published:
			if event.Type == "delegation.updated" {
				return event.PayloadJSON
			}
		case <-time.After(time.Second):
			t.Fatal("no delegation.updated was published on the turn's stream")
		}
	}
}

// A task asking for an approval reports, on its turn's stream and as it
// happens, that it is waiting and on which approval.
func TestATasksApprovalRequestIsReportedOnItsTurnsStream(t *testing.T) {
	h := newApprovalHarness(t)
	sessionID, childRunID := runningTaskAboutToAsk(t, h)
	published, unsubscribe := h.bus.Subscribe(sessionID)
	defer unsubscribe()

	approvalID, err := h.service.CreateApprovalForTool(context.Background(), childRunID, "call_child", "general_assistant",
		"files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if update := nextDelegationUpdate(t, published); !strings.Contains(update, approvalID) {
		t.Fatalf("delegation.updated = %s, want it to name the approval", update)
	}
}

// Approving a task's approval leaves it waiting for its worker to resume, but
// with nothing more for the user to decide, so its turn's stream is told the
// card no longer names the approval.
func TestApprovingATasksApprovalIsReportedOnItsTurnsStream(t *testing.T) {
	h := newApprovalHarness(t)
	ctx := context.Background()
	sessionID, childRunID := runningTaskAboutToAsk(t, h)
	approvalID, err := h.service.CreateApprovalForTool(ctx, childRunID, "call_child", "general_assistant",
		"files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	published, unsubscribe := h.bus.Subscribe(sessionID)
	defer unsubscribe()

	if _, err := reviewedApprove(t, h.service, h.database, ctx, &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	if update := nextDelegationUpdate(t, published); !strings.Contains(update, `"state":"waiting_approval"`) ||
		strings.Contains(update, "approvalId") {
		t.Fatalf("delegation.updated = %s, want the task still waiting, on no approval", update)
	}
}
