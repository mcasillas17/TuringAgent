package runtime

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func explicitCancelledWorker(t *testing.T) (*harness, *worker, repository.EnqueueUserMessageResult, repository.Assignment) {
	t.Helper()
	h := newHarness(t)
	ctx := context.Background()
	run := h.enqueueRun(t, "durable explicit stop")
	job, err := h.repo.ClaimNextJob(ctx, "general_assistant", "worker-explicit")
	if err != nil {
		t.Fatal(err)
	}
	assigned := repository.Assignment{RunID: run.RunID, JobID: run.JobID, WorkerID: "worker-explicit", AttemptID: job.AssignmentAttemptID}
	if err := h.repo.BeginAssignmentSend(ctx, assigned); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkAssignmentDelivered(ctx, assigned); err != nil {
		t.Fatal(err)
	}
	connected := &worker{
		commands: make(chan workerCommand, 8), done: make(chan struct{}),
		maxConcurrent: 1, lastHeartbeat: time.Now().UTC(),
		assignments: map[string]assignment{run.RunID: {runID: run.RunID, jobID: run.JobID, attemptID: assigned.AttemptID}},
	}
	h.service.mu.Lock()
	h.service.workers[assigned.WorkerID] = connected
	h.service.mu.Unlock()
	if _, err := h.repo.CancelUserRun(ctx, run.SessionID, run.RunID, "cancel:"+run.RunID); err != nil {
		t.Fatal(err)
	}
	return h, connected, run, assigned
}

func TestExplicitCancelSweepRedeliversLostNotificationAndAck(t *testing.T) {
	h, connected, run, _ := explicitCancelledWorker(t)
	for range 2 {
		if err := h.service.RecoverOrphanedAssignments(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case cmd := <-connected.commands:
			cancel := cmd.command.GetRunCancelled()
			if cancel == nil || cancel.RunId != run.RunID || cancel.StateVersion != h.runState(t, run.RunID).StateVersion {
				t.Fatalf("redelivery = %v", cmd.command)
			}
		default:
			t.Fatal("committed stop did not drive cancellation redelivery")
		}
		current, err := h.repo.GetRun(context.Background(), run.RunID)
		if err != nil || !current.ExecutionActive {
			t.Fatalf("sending a stop released execution: %+v, %v", current, err)
		}
	}
}

func TestExplicitCancelHeartbeatIsNotAnExitAcknowledgement(t *testing.T) {
	h, connected, run, assigned := explicitCancelledWorker(t)
	if err := h.service.renewWorkerLeases(context.Background(), assigned.WorkerID, connected, &turingv1.RuntimeHeartbeat{WorkerId: assigned.WorkerID}); err != nil {
		t.Fatal(err)
	}
	current, err := h.repo.GetRun(context.Background(), run.RunID)
	if err != nil || !current.ExecutionActive || !connected.hasAssignment(run.RunID) {
		t.Fatalf("heartbeat released an executing cancellation: %+v, %v", current, err)
	}
}

func TestExplicitCancelNotificationDoesNotReachDifferentAttempt(t *testing.T) {
	h, connected, run, _ := explicitCancelledWorker(t)
	connected.mu.Lock()
	held := connected.assignments[run.RunID]
	held.attemptID = "different-attempt"
	connected.assignments[run.RunID] = held
	connected.mu.Unlock()
	h.service.CancelRun(context.Background(), run.RunID, "user_cancelled")
	select {
	case cmd := <-connected.commands:
		t.Fatalf("wrong attempt received cancellation: %v", cmd.command)
	default:
	}
	current, err := h.repo.GetRun(context.Background(), run.RunID)
	if err != nil || !current.ExecutionActive {
		t.Fatalf("wrong attempt released fence: %+v, %v", current, err)
	}
}
