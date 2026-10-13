package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReadyQueuedWhileActiveLosesToExplicitCancelWithoutDisconnect(t *testing.T) {
	h := newHarness(t)
	f := newApprovalResumeFixture(t, h, "worker-ready-before-stop", "queued Ready cancellation race")
	owner := f.connectedWorker(t)
	owner.updateMu.Lock()
	locked := true
	defer func() {
		if locked {
			owner.updateMu.Unlock()
		}
	}()
	f.sendReady(t, f.ready())
	cancelled, err := h.repo.CancelUserRun(context.Background(), f.sessionID, f.runID, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	owner.updateMu.Unlock()
	locked = false
	command := approvalResumeCommand(t, f.stream, func(cmd *turingv1.RuntimeCommand) bool {
		if cmd.GetApprovalResumeAccepted() != nil {
			t.Fatal("Ready resumed after Stop committed")
		}
		return cmd.GetRunCancelled() != nil
	})
	if command.GetRunCancelled().StateVersion != cancelled.State.StateVersion {
		t.Fatalf("redelivery = %v", command)
	}
	run, err := h.repo.GetRun(context.Background(), f.runID)
	if err != nil || !run.ExecutionActive || h.service.registeredWorker(f.workerID) != owner {
		t.Fatalf("losing Ready dropped containment or registration: %+v, %v", run, err)
	}
}

func TestExplicitCancellationDoesNotExcuseInvalidApprovalReady(t *testing.T) {
	for _, change := range []string{"approval", "run", "attempt", "version", "missing-version", "pending"} {
		t.Run(change, func(t *testing.T) {
			h := newHarness(t)
			f := newApprovalResumeFixture(t, h, "worker-invalid-ready", "invalid cancelled Ready")
			ready := f.ready()
			if change == "pending" {
				approval, _, err := h.repo.CreateApprovalWithEvent(context.Background(), f.runID, "", "general_assistant", "files.create", `{"path":"pending.txt"}`, "sha256:pending", "2099-01-01T00:00:00Z")
				if err != nil {
					t.Fatal(err)
				}
				ready.ApprovalId = approval.ApprovalID
				ready.ExpectedStateVersion = h.runState(t, f.runID).StateVersion
			}
			if _, err := h.repo.CancelUserRun(context.Background(), f.sessionID, f.runID, "cancel"); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "approval":
				ready.ApprovalId = "other-approval"
			case "run":
				ready.RunId = "other-run"
			case "attempt":
				ready.AssignmentAttemptId = "old-attempt"
			case "version":
				ready.ExpectedStateVersion++
			case "missing-version":
				ready.ExpectedStateVersion = 0
			}
			f.sendReady(t, ready)
			if err := f.awaitExit(t); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("invalid Ready no longer rejected: %v", err)
			}
		})
	}
}

// A task stopped because its turn ended can lose a Ready the same way a
// user's cancellation can: the worker is told the task was stopped and stays
// connected, with its execution still contained.
func TestReadyQueuedWhileActiveLosesToAStoppedTaskWithoutDisconnect(t *testing.T) {
	h := newHarness(t)
	f := newApprovalResumeFixture(t, h, "worker-ready-before-task-stop", "queued Ready stopped-task race")
	owner := f.connectedWorker(t)
	owner.updateMu.Lock()
	locked := true
	defer func() {
		if locked {
			owner.updateMu.Unlock()
		}
	}()
	f.sendReady(t, f.ready())
	stopped, err := h.repo.CancelRunCanonical(context.Background(), repository.CancelRunInput{
		RunID: f.runID, ExpectedStateVersion: h.runState(t, f.runID).StateVersion,
		Cancellation: runoutcome.ParentEndedCancellation(),
	})
	if err != nil {
		t.Fatal(err)
	}
	owner.updateMu.Unlock()
	locked = false
	command := approvalResumeCommand(t, f.stream, func(cmd *turingv1.RuntimeCommand) bool {
		if cmd.GetApprovalResumeAccepted() != nil {
			t.Fatal("Ready resumed after the task was stopped")
		}
		return cmd.GetRunCancelled() != nil
	})
	if cancelled := command.GetRunCancelled(); cancelled.GetStateVersion() != stopped.State.StateVersion ||
		cancelled.GetReason() != runoutcome.CodeParentRunEnded {
		t.Fatalf("redelivery = %v", command)
	}
	run, err := h.repo.GetRun(context.Background(), f.runID)
	if err != nil || !run.ExecutionActive || h.service.registeredWorker(f.workerID) != owner {
		t.Fatalf("losing Ready dropped containment or registration: %+v, %v", run, err)
	}
}
