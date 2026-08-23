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
		commands:    make(chan workerCommand, 1),
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
	failRunFixture(t, h, enqueued.RunID, toolExecutionFailure())
	requireLateTerminalFence(t, h, enqueued.RunID, connected, &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
			RunId: enqueued.RunID, Code: "model_stream_failed",
			FailureOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
		}},
	})
}

func TestLateAssignedTerminalUpdateDoesNotReleaseConflictingCompletion(t *testing.T) {
	h := newHarness(t)
	enqueued, _, connected := claimLateTerminalAssignment(t, h, "completion identity")
	completeRunFixture(t, h, enqueued.RunID, enqueued.AssistantMessageID, "persisted completion")
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
	failRunFixture(t, h, enqueued.RunID, toolExecutionFailure())
	connected.assignments[enqueued.RunID] = assignment{jobID: repoAssignment.JobID, runID: repoAssignment.RunID, attemptID: "stale-attempt"}
	requireLateTerminalFence(t, h, enqueued.RunID, connected, &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
			RunId: enqueued.RunID, Code: "tool_call_failed",
			FailureOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_EXECUTION,
		}},
	})
}
