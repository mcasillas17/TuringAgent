package runtime

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// seedDelegatedParent is a Turing turn that delegated one task asking for
// childTools and then completed, run by a worker this test drives through the
// repository.
func seedDelegatedParent(t *testing.T, h *harness, childTools []string) (string, repository.EnqueueUserMessageResult, repository.Delegation) {
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
	claimed, err := h.repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "parent-worker", 0, time.Hour,
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
			ProfileID: "research", Revision: "rev-1", DisplayName: "Research", Instructions: "You are Research.",
		},
		ModelProvider: "ollama", Model: "llama3.2", SelectedTools: childTools,
		Brief: "BEGIN TURING_RETRIEVED_DELEGATION_BRIEF_x\nTask:\nGather the notes\nEND TURING_RETRIEVED_DELEGATION_BRIEF_x",
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
	return session.SessionID, parent, created.Delegation
}

// A delegated task that no worker can run ends at the queue bound, and when
// it is its parent's last, ending it queues the parent's continuation. The
// sweep that ended it dispatches that continuation once the sweep is done,
// rather than leaving it for the next reaper tick.
func TestAQueueExpiryThatJoinsDispatchesTheContinuation(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, queueWaitDispatch(runoutcome.QueueTimeoutPolicyFail))
	h.service.queueNow = clock.read
	// No worker serves this tool, so the task can only wait.
	sessionID, parent, delegation := seedDelegatedParent(t, h, []string{"system/system.unserved"})
	stream := connectWorkerCapabilities(t, h, "team-worker", "registration-team", teamCapabilities(1, "system.time"))
	refreshQueue(t, h)
	clock.advance(11 * time.Minute)

	// The bound ends the task inside the sweep; nothing else dispatches.
	refreshQueue(t, h)

	if child := h.runState(t, delegation.ChildRunID); child.Lifecycle != "failed" {
		t.Fatalf("child = %s, want failed at the queue bound", child.Lifecycle)
	}
	assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetSessionId() != sessionID || assigned.GetRunId() == parent.RunID || !assigned.GetEnforceSelectedTools() {
		t.Fatalf("assigned = %+v, want the parent's continuation", assigned)
	}
}

// Denying a delegated task's approval can finish its parent's last task. The
// worker running the task is only told of the decision, and no worker has
// held the continuation that queued, so the decision itself dispatches it:
// the runtime registers as the approval service's dispatcher.
func TestDenyingTheLastTasksApprovalDispatchesTheContinuation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sessionID, parent, delegation := seedDelegatedParent(t, h, []string{"system/system.time"})
	capabilities := teamCapabilities(1, "system.time")
	capabilities.MaxConcurrentRuns = 2
	stream := connectWorkerCapabilities(t, h, "team-worker", "registration-team", capabilities)
	if err := h.service.DispatchPending(ctx); err != nil {
		t.Fatal(err)
	}
	child := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if child.GetRunId() != delegation.ChildRunID {
		t.Fatalf("assigned %s, want the task %s", child.GetRunId(), delegation.ChildRunID)
	}
	if err := h.repo.RecordToolCallBefore(ctx, repository.ToolCallRecord{
		ToolCallID: "call_child", RunID: delegation.ChildRunID, ModelToolCallID: "model_call_child",
	}, "general_assistant", "system", "system.time", `{}`, "sha256:child"); err != nil {
		t.Fatal(err)
	}
	approvalID, err := h.approvals.CreateApprovalForTool(ctx, delegation.ChildRunID, "call_child", "general_assistant", "system.time", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.approvals.DenyApproval(ctx, &turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: "no"}); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetSessionId() != sessionID || assigned.GetRunId() == parent.RunID || assigned.GetRunId() == delegation.ChildRunID {
		t.Fatalf("assigned = %+v, want the parent's continuation", assigned)
	}
}
