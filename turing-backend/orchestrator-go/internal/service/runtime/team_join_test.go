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

// runningParentWithTask is a Turing turn and the task it delegated, both
// assigned to one connected team-protocol worker.
func runningParentWithTask(t *testing.T, h *harness) (turingv1.RuntimeService_ConnectWorkerClient, *turingv1.AgentJob, repository.Delegation) {
	t.Helper()
	stream, parent, delegation := delegatingParentOnWorker(t, h)
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned().GetRunId() == delegation.ChildRunID
	})
	return stream, parent, delegation
}

// delegatingParentOnWorker is a Turing turn assigned to one connected
// team-protocol worker that has delegated a task, not yet dispatched.
func delegatingParentOnWorker(t *testing.T, h *harness) (turingv1.RuntimeService_ConnectWorkerClient, *turingv1.AgentJob, repository.Delegation) {
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
	capabilities := teamCapabilities(1, "system.time")
	capabilities.MaxConcurrentRuns = 2
	stream := connectWorkerCapabilities(t, h, "team-worker", "registration-team", capabilities)
	assignedParent := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned().GetRunId() == parent.RunID
	}).GetRunAssigned()
	created, err := h.repo.CreateDelegation(ctx, repository.CreateDelegationInput{
		ParentRunID: parent.RunID, AssignmentAttemptID: assignedParent.GetAssignmentAttemptId(), ToolCallID: "call_1",
		ArgsHash: "sha256:call_1", Policy: "safe", MaxPerRun: 3,
		Profile: repository.AgentProfileSnapshot{
			ProfileID: "research", Revision: "rev-1", DisplayName: "Research", Instructions: "You are Research.",
		},
		ModelProvider: "ollama", Model: "llama3.2", SelectedTools: []string{"system/system.time"},
		Brief: "BEGIN TURING_RETRIEVED_DELEGATION_BRIEF_x\nTask:\nGather the notes\nEND TURING_RETRIEVED_DELEGATION_BRIEF_x",
	})
	if err != nil {
		t.Fatal(err)
	}
	return stream, assignedParent, created.Delegation
}

// Handing a task to a worker is reported on its turn's stream as it happens.
func TestAssigningATaskReportsItOnItsTurnsStream(t *testing.T) {
	h := newHarness(t)
	stream, parent, delegation := delegatingParentOnWorker(t, h)
	published, unsubscribe := h.bus.Subscribe(parent.GetSessionId())
	defer unsubscribe()
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned().GetRunId() == delegation.ChildRunID
	})
	for {
		select {
		case event := <-published:
			if event.Type == "delegation.updated" {
				return
			}
		case <-time.After(time.Second):
			t.Fatal("the task's delegation.updated was not published on its turn's stream")
		}
	}
}

// A worker reporting that a turn failed stops the turn's tasks, and the worker
// running one of them is told to let it go at once.
func TestATurnsReportedFailureTellsItsTasksWorker(t *testing.T) {
	h := newHarness(t)
	stream, parent, delegation := runningParentWithTask(t, h)

	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId: parent.GetRunId(), Code: "model_stream_failed",
		FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
		ExpectedStateVersion: parent.GetExpectedStateVersion(),
	}}}); err != nil {
		t.Fatal(err)
	}
	cancelled := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunCancelled() != nil
	}).GetRunCancelled()
	if cancelled.GetRunId() != delegation.ChildRunID || cancelled.GetReason() != runoutcome.CodeParentRunEnded {
		t.Fatalf("cancelled = %+v, want the task stopped because its turn ended", cancelled)
	}
}

// A worker that missed being told its task was stopped is told again by the
// recovery loop, as one that missed a user's cancellation is.
func TestTheRecoveryLoopRetellsAStoppedTasksWorker(t *testing.T) {
	h := newHarness(t)
	stream, parent, delegation := runningParentWithTask(t, h)
	// Fail the turn behind the runtime's back: nothing tells the worker.
	if _, err := h.repo.FailRunCanonical(context.Background(), repository.FailRunInput{
		RunID: parent.GetRunId(), ExpectedStateVersion: parent.GetExpectedStateVersion(),
		Failure: runoutcome.NormalizeFailure(runoutcome.OriginExternalProvider, "model_error", runoutcome.RetryClassNever),
	}); err != nil {
		t.Fatal(err)
	}

	if err := h.service.RecoverOrphanedAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancelled := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunCancelled().GetRunId() == delegation.ChildRunID
	}).GetRunCancelled()
	if cancelled.GetReason() != runoutcome.CodeParentRunEnded {
		t.Fatalf("cancelled = %+v, want the task stopped because its turn ended", cancelled)
	}
}

// A turn sent back to the queue can run out of queue time while a task it
// started is still running. The sweep that ends it stops the task, and the
// task's worker is told once the sweep is done.
func TestAQueueExpiryOfATurnTellsItsTasksWorker(t *testing.T) {
	clock := newQueueClock()
	h := newHarnessWithDispatch(t, queueWaitDispatch(runoutcome.QueueTimeoutPolicyFail))
	h.service.queueNow = clock.read
	ctx := context.Background()
	stream, parent, delegation := runningParentWithTask(t, h)
	// The turn's worker is lost and the turn is requeued, the way recovery
	// does it; the task keeps running.
	if _, err := h.repo.FenceRunOwnership(ctx, repository.FenceRunOwnershipInput{
		RunID: parent.GetRunId(), ExpectedStateVersion: h.runState(t, parent.GetRunId()).StateVersion,
		WorkerID: "team-worker", AssignmentAttemptID: parent.GetAssignmentAttemptId(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.RequeueRecoveringRun(ctx, repository.RequeueRecoveringRunInput{
		RunID: parent.GetRunId(), ExpectedStateVersion: h.runState(t, parent.GetRunId()).StateVersion,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(ctx, `UPDATE jobs SET status = 'pending', lease_owner = NULL,
		lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ?`, parent.GetRunId()); err != nil {
		t.Fatal(err)
	}
	refreshQueue(t, h)
	clock.advance(2 * time.Hour)

	refreshQueue(t, h)

	if state := h.runState(t, parent.GetRunId()); state.Lifecycle != "failed" {
		t.Fatalf("turn = %s, want failed at the queue bound", state.Lifecycle)
	}
	cancelled := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunCancelled().GetRunId() == delegation.ChildRunID
	}).GetRunCancelled()
	if cancelled.GetReason() != runoutcome.CodeParentRunEnded {
		t.Fatalf("cancelled = %+v, want the task stopped because its turn ended", cancelled)
	}
}

// A turn whose retryable failure has used up its attempts fails through the
// retry path, which stops its tasks and tells their worker the same way.
func TestATurnOutOfRetriesTellsItsTasksWorker(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{MaxAttempts: 1})
	stream, parent, delegation := runningParentWithTask(t, h)

	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId: parent.GetRunId(), Code: "tool_discovery_failed",
		FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_INFRASTRUCTURE,
		AutomaticRetryClass:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
		ExpectedStateVersion: parent.GetExpectedStateVersion(),
	}}}); err != nil {
		t.Fatal(err)
	}
	cancelled := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunCancelled() != nil
	}).GetRunCancelled()
	if cancelled.GetRunId() != delegation.ChildRunID || cancelled.GetReason() != runoutcome.CodeParentRunEnded {
		t.Fatalf("cancelled = %+v, want the task stopped because its turn ended", cancelled)
	}
	if state := h.runState(t, parent.GetRunId()); state.Lifecycle != "failed" {
		t.Fatalf("turn = %s, want failed with its attempts used up", state.Lifecycle)
	}
}

// A turn that fails because its approval could not even be created stops its
// tasks, and their worker is told at once.
func TestAnUncreatableApprovalOfATurnTellsItsTasksWorker(t *testing.T) {
	h := newHarness(t)
	stream, parent, delegation := runningParentWithTask(t, h)
	if err := h.service.terminalizePostCommitApprovalFailure(context.Background(), parent.GetRunId(), "call_missing_approval"); err != nil {
		t.Fatal(err)
	}
	cancelled := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunCancelled().GetRunId() == delegation.ChildRunID
	}).GetRunCancelled()
	if cancelled.GetReason() != runoutcome.CodeParentRunEnded {
		t.Fatalf("cancelled = %+v, want the task stopped because its turn ended", cancelled)
	}
}
