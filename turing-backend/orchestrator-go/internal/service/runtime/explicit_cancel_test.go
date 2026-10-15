package runtime

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

func explicitCancelledWorker(t *testing.T) (*harness, *worker, repository.EnqueueUserMessageResult, repository.Assignment) {
	t.Helper()
	return stoppedWorker(t, func(h *harness, run repository.EnqueueUserMessageResult) {
		if _, err := h.repo.CancelUserRun(context.Background(), run.SessionID, run.RunID, "cancel:"+run.RunID); err != nil {
			t.Fatal(err)
		}
	})
}

// stoppedWorker is a run delivered to a registered worker and then stopped by
// stop, before the worker has been told.
func stoppedWorker(t *testing.T, stop func(*harness, repository.EnqueueUserMessageResult)) (*harness, *worker, repository.EnqueueUserMessageResult, repository.Assignment) {
	t.Helper()
	h := newHarness(t)
	ctx := context.Background()
	run := h.enqueueRun(t, "durable explicit stop")
	job, err := h.repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-explicit", 0, 0, nil, nil)
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
	stop(h, run)
	return h, connected, run, assigned
}

// A task stopped because its turn ended is held like a user's cancellation:
// a heartbeat does not release it, and the recovery loop keeps telling its
// worker until the worker lets go.
func TestAStoppedTaskHeartbeatIsNotAnExitAcknowledgement(t *testing.T) {
	h, connected, run, assigned := stoppedWorker(t, func(h *harness, run repository.EnqueueUserMessageResult) {
		if _, err := h.repo.CancelRunCanonical(context.Background(), repository.CancelRunInput{
			RunID: run.RunID, ExpectedStateVersion: h.runState(t, run.RunID).StateVersion,
			Cancellation: runoutcome.ParentEndedCancellation(),
		}); err != nil {
			t.Fatal(err)
		}
	})
	if err := h.service.renewWorkerLeases(context.Background(), assigned.WorkerID, connected, &turingv1.RuntimeHeartbeat{WorkerId: assigned.WorkerID}); err != nil {
		t.Fatal(err)
	}
	current, err := h.repo.GetRun(context.Background(), run.RunID)
	if err != nil || !current.ExecutionActive || !connected.hasAssignment(run.RunID) {
		t.Fatalf("heartbeat released a stopped task: %+v, %v", current, err)
	}
	if err := h.service.RecoverOrphanedAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case cmd := <-connected.commands:
		if cancel := cmd.command.GetRunCancelled(); cancel.GetRunId() != run.RunID || cancel.GetReason() != runoutcome.CodeParentRunEnded {
			t.Fatalf("redelivery = %v", cmd.command)
		}
	default:
		t.Fatal("the stopped task was not redelivered after the heartbeat")
	}
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

// Telling a stopped task's worker never waits on that worker. A turn's failure
// is handled under its worker's update lock, the task often shares that
// worker, and only the command loop drains a full buffer, after taking the
// same lock. Shutdown still waits for the stop to go out before the database
// closes.
func TestTellingAStoppedTaskNeverWaitsOnItsWorker(t *testing.T) {
	h, connected, run, _ := stoppedWorker(t, func(h *harness, run repository.EnqueueUserMessageResult) {
		if _, err := h.repo.CancelRunCanonical(context.Background(), repository.CancelRunInput{
			RunID: run.RunID, ExpectedStateVersion: h.runState(t, run.RunID).StateVersion,
			Cancellation: runoutcome.ParentEndedCancellation(),
		}); err != nil {
			t.Fatal(err)
		}
	})
	for len(connected.commands) < cap(connected.commands) {
		connected.commands <- workerCommand{command: &turingv1.RuntimeCommand{}}
	}
	connected.updateMu.Lock()
	told := make(chan struct{})
	go func() {
		h.service.stopDelegatedTasks([]string{run.RunID})
		close(told)
	}()
	select {
	case <-told:
	case <-time.After(time.Second):
		t.Fatal("telling a stopped task waited on its worker's full command buffer")
	}
	connected.updateMu.Unlock()
	shutDown := make(chan struct{})
	go func() {
		h.service.WaitForWorkerStreams()
		close(shutDown)
	}()
	select {
	case <-shutDown:
		t.Fatal("shutdown did not wait for the stop to go out")
	case <-time.After(100 * time.Millisecond):
	}
	<-connected.commands
	for {
		cmd := <-connected.commands
		if cancel := cmd.command.GetRunCancelled(); cancel != nil {
			if cancel.GetRunId() != run.RunID || cancel.GetReason() != runoutcome.CodeParentRunEnded {
				t.Fatalf("stop = %v", cmd.command)
			}
			break
		}
	}
	select {
	case <-shutDown:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown still waiting after the stop went out")
	}
}
