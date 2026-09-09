package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
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
