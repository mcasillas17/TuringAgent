package runtime

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestCancelRunClearsUnownedUncertainExecutionGate(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "cancel uncertain attempt")
	second := h.enqueueRun(t, "claim after uncertain cancellation")
	claimed, err := h.repo.ClaimNextJobWithLimit(context.Background(), "general_assistant", "worker-gone", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	assignment := repository.Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-gone", AttemptID: claimed.AssignmentAttemptID,
	}
	if err := h.repo.BeginAssignmentSend(context.Background(), assignment); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkAssignmentDeliveryUncertain(context.Background(), assignment); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.CancelRunWithEvent(context.Background(), first.RunID, "client_cancelled", `{"reason":"client_cancelled"}`); err != nil {
		t.Fatal(err)
	}

	h.service.CancelRun(context.Background(), first.RunID, "client_cancelled")

	run, err := h.repo.GetRun(context.Background(), first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "cancelled" || run.ExecutionActive {
		t.Fatalf("cancelled orphan run = %+v, want terminal inactive run", run)
	}
	next, err := h.repo.ClaimNextJobWithLimit(context.Background(), "general_assistant", "worker-after-cancel", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if next.RunID != second.RunID {
		t.Fatalf("claim after orphan cancellation = %+v, want %q", next, second.RunID)
	}
}

func TestRecoveryDoesNotRequeueExpiredAttemptOwnedByConnectedWorker(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "live worker owns delivered attempt")
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(workerReady("worker-live-expired-lease")); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == enqueued.RunID
	})
	expired := time.Now().Add(-time.Second)
	if _, err := h.database.ExecContext(context.Background(), `
		UPDATE agent_runs
		SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
		WHERE id = ?
	`, expired.Format("2006-01-02T15:04:05.000000000Z"), expired.UnixNano(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	if err := h.service.RecoverOrphanedAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" || !run.ExecutionActive || run.ExecutionState != "delivered" {
		t.Fatalf("live delivered assignment was recovered: %+v", run)
	}
}
