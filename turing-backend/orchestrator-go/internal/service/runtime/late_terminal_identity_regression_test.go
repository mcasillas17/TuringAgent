package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func claimLateTerminalAssignment(t *testing.T, h *harness, content string) (repository.EnqueueUserMessageResult, repository.Assignment, *worker) {
	t.Helper()
	enqueued := h.enqueueRun(t, content)
	claimed, err := h.repo.ClaimNextJob(context.Background(), "general_assistant", "worker-terminal-fence")
	if err != nil {
		t.Fatal(err)
	}
	repoAssignment := repository.Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-terminal-fence", AttemptID: claimed.AssignmentAttemptID,
	}
	if err := h.repo.BeginAssignmentSend(context.Background(), repoAssignment); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkAssignmentDelivered(context.Background(), repoAssignment); err != nil {
		t.Fatal(err)
	}
	return enqueued, repoAssignment, &worker{
		commands:    make(chan *turingv1.RuntimeCommand, 1),
		assignments: map[string]assignment{enqueued.RunID: {jobID: repoAssignment.JobID, runID: repoAssignment.RunID, attemptID: repoAssignment.AttemptID}},
	}
}

func requireLateTerminalFence(t *testing.T, h *harness, runID string, connected *worker, update *turingv1.RuntimeUpdate) {
	t.Helper()
	handled, err := h.service.reconcileLateAssignedUpdate(context.Background(), connected, "worker-terminal-fence", update)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("late terminal update was not handled")
	}
	run, err := h.repo.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.ExecutionActive {
		t.Fatalf("late terminal update released execution: %+v", run)
	}
	if !connected.hasAssignment(runID) {
		t.Fatal("late terminal update released worker assignment")
	}
}

func TestLateAssignedTerminalUpdateDoesNotReleaseConflictingFailure(t *testing.T) {
	h := newHarness(t)
	enqueued, _, connected := claimLateTerminalAssignment(t, h, "failure identity")
	payload, err := encodePayload(map[string]any{
		"runId": enqueued.RunID, "code": "persisted", "message": "persisted failure", "retryable": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.FailRunWithEventPreservingExecution(context.Background(), enqueued.RunID, "persisted", "persisted failure", payload); err != nil {
		t.Fatal(err)
	}
	requireLateTerminalFence(t, h, enqueued.RunID, connected, &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
			RunId: enqueued.RunID, Code: "stale", Message: "stale failure", Retryable: true,
		}},
	})
}

func TestLateAssignedTerminalUpdateDoesNotReleaseConflictingCompletion(t *testing.T) {
	h := newHarness(t)
	enqueued, _, connected := claimLateTerminalAssignment(t, h, "completion identity")
	payload, err := encodePayload(map[string]any{
		"runId": enqueued.RunID, "assistantMessageId": enqueued.AssistantMessageID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.CompleteRunWithEvent(context.Background(), enqueued.RunID, enqueued.AssistantMessageID, "persisted completion", payload); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `
		UPDATE agent_runs
		SET execution_active = 1, execution_state = 'delivered'
		WHERE id = ?
	`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	requireLateTerminalFence(t, h, enqueued.RunID, connected, &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
			RunId: enqueued.RunID, AssistantMessageId: enqueued.AssistantMessageID, Content: "stale completion",
		}},
	})
}

func TestLateAssignedTerminalUpdateDoesNotReleaseStaleAttempt(t *testing.T) {
	h := newHarness(t)
	enqueued, repoAssignment, connected := claimLateTerminalAssignment(t, h, "attempt identity")
	payload, err := encodePayload(map[string]any{
		"runId": enqueued.RunID, "code": "persisted", "message": "persisted failure", "retryable": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.FailRunWithEventPreservingExecution(context.Background(), enqueued.RunID, "persisted", "persisted failure", payload); err != nil {
		t.Fatal(err)
	}
	connected.assignments[enqueued.RunID] = assignment{jobID: repoAssignment.JobID, runID: repoAssignment.RunID, attemptID: "stale-attempt"}
	requireLateTerminalFence(t, h, enqueued.RunID, connected, &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
			RunId: enqueued.RunID, Code: "persisted", Message: "persisted failure", Retryable: false,
		}},
	})
}
