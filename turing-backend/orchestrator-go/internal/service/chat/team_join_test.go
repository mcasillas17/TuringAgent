package chat

import (
	"context"
	"slices"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/types/known/structpb"
)

// seedDelegatedParent is a Turing turn that has delegated one task and
// completed, run by a worker this test drives through the repository. The
// task asks for a tool no connected worker serves, so it stays queued.
func seedDelegatedParent(t *testing.T, h *harness) (string, repository.EnqueueUserMessageResult, repository.Delegation) {
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
	sessionID := h.createSession(t)
	parent, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: sessionID, Content: "prep me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", TeamRoster: chatTestRoster,
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
			ProfileID: "research", Revision: "rev-1", DisplayName: "Research", Emoji: "🔬", Instructions: "You are Research.",
		},
		ModelProvider: "ollama", Model: "llama3.2", SelectedTools: []string{"system/system.unserved"},
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
	return sessionID, parent, created.Delegation
}

// Cancelling a task nobody had started involves no worker. Releasing the
// cancelled run is what hands the parent's continuation to a free worker;
// nothing else would until the next periodic dispatch.
func TestCancellingTheLastQueuedTaskDispatchesTheContinuation(t *testing.T) {
	h := newHarness(t)
	sessionID, parent, delegation := seedDelegatedParent(t, h)
	worker := connectTeamWorker(t, h, "team-worker", 1)

	response, err := h.chatClient.CancelRun(h.clientContext(), &turingv1.CancelRunRequest{
		SessionId: delegation.ChildSessionID, RunId: delegation.ChildRunID, IdempotencyKey: "cancel-child",
	})
	if err != nil || response.GetResult() != turingv1.CancelRunResult_CANCEL_RUN_RESULT_ACCEPTED {
		t.Fatalf("cancel = %+v, %v; want accepted", response, err)
	}
	assigned := recvRuntimeCommand(t, worker, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetSessionId() != sessionID || assigned.GetRunId() == parent.RunID ||
		!assigned.GetEnforceSelectedTools() || len(assigned.GetSelectedTools()) != 0 {
		t.Fatalf("assigned = %+v, want the parent's continuation with no tools", assigned)
	}
}

// Every later turn in a conversation that delegated needs a team-protocol
// worker, so a consented turn's tools are frozen from those workers only: an
// older worker can never run it, and must not narrow its set.
func TestALaterConsentedTurnFreezesTheTeamProtocolWorkersTools(t *testing.T) {
	h := newHarness(t)
	sessionID, _, _ := seedDelegatedParent(t, h)
	github := &turingv1.DiscoveredTool{ServerName: "integrations", ToolName: "github.list_issues", Schema: &structpb.Struct{}}
	files := &turingv1.DiscoveredTool{ServerName: "files", ToolName: "files.read", Schema: &structpb.Struct{}}
	connectTeamWorker(t, h, "old-worker", 0, github)
	connectTeamWorker(t, h, "team-worker", 1, github, files)
	createChatGitHubConnection(t, h.repo, 1)

	request := localTuringRequest(sessionID)
	request.RequestedTools = []string{"integrations/github.list_issues"}
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: request.GetSessionId(), Content: request.GetContent(), ContentType: request.GetContentType(),
		AgentId: request.GetAgentId(), ModelProvider: request.GetModelProvider(), Model: request.GetModel(),
		IdempotencyKey: "later_consent", RequestedTools: request.GetRequestedTools(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(prepared.GetDisclosure().GetSelectedTools(), "files/files.read") {
		t.Fatalf("disclosed tools = %v, want the team-protocol worker's files.read", prepared.GetDisclosure().GetSelectedTools())
	}
}
