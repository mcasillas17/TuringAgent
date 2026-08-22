package runtime

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestRecoveryDispatchesQueuedJobsWithoutOrphanedAssignment(t *testing.T) {
	h := newHarness(t)
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(workerReady("recovery-dispatch-worker")); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetWorkerAccepted() != nil
	})

	enqueued := h.enqueueRun(t, "dispatch after transient failure")
	if err := h.service.RecoverOrphanedAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		run := command.GetRunAssigned()
		return run != nil && run.RunId == enqueued.RunID
	}).GetRunAssigned()
	if assigned.JobId != enqueued.JobID {
		t.Fatalf("recovery assignment = %+v, want job %q", assigned, enqueued.JobID)
	}
}

func TestCancelRunFencesUnownedUncertainExecutionUntilRecovery(t *testing.T) {
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
	cancelRunFixture(t, h, first.RunID)

	h.service.CancelRun(context.Background(), first.RunID, "client_cancelled")

	run, err := h.repo.GetRun(context.Background(), first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "cancelled" || !run.ExecutionActive || run.ExecutionState != "uncertain" {
		t.Fatalf("cancelled orphan run = %+v, want terminal active uncertain fence", run)
	}
	next, err := h.repo.ClaimNextJobWithLimit(context.Background(), "general_assistant", "worker-after-cancel", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if next.JobID != "" {
		t.Fatalf("claim after uncertain cancellation = %+v, want fenced capacity", next)
	}
	expired := time.Now().Add(-time.Second)
	if _, err := h.database.ExecContext(context.Background(), `
		UPDATE agent_runs
		SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
		WHERE id = ?
	`, expired.Format("2006-01-02T15:04:05.000000000Z"), expired.UnixNano(), first.RunID); err != nil {
		t.Fatal(err)
	}
	if err := h.service.RecoverOrphanedAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	next, err = h.repo.ClaimNextJobWithLimit(context.Background(), "general_assistant", "worker-after-cancel", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if next.RunID != second.RunID {
		t.Fatalf("claim after orphan recovery = %+v, want %q", next, second.RunID)
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

func TestRecoveryDispatchesRequeuedWorkAfterRoutingNoticeFailure(t *testing.T) {
	h := newHarness(t)
	recoverable := h.enqueueRun(t, "recover despite notice failure")
	claimed, err := h.repo.ClaimNextJobWithLimit(
		context.Background(), "general_assistant", "worker-gone", 1, time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.RunID != recoverable.RunID {
		t.Fatalf("claimed run = %q, want %q", claimed.RunID, recoverable.RunID)
	}

	stream := connectWorkerCapabilities(t, h, "worker-recovery-notice", "registration-recovery-notice", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	defer func() { _ = stream.CloseSend() }()
	session, err := h.repo.CreateSession(context.Background(), "Unavailable during recovery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs OpenAI", AgentID: "general_assistant",
		ModelProvider: "openai_compatible", Model: "gpt-4o-mini",
		EgressDecision: runtimeRemoteDecision("gpt-4o-mini"),
	}); err != nil {
		t.Fatal(err)
	}
	failRoutingNoticeInserts(t, h)
	time.Sleep(5 * time.Millisecond)

	if err := h.service.RecoverOrphanedAssignments(context.Background()); err != nil {
		t.Fatalf("recovery reported advisory notice failure: %v", err)
	}
	assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetRunId() != recoverable.RunID {
		t.Fatalf("recovery assignment = %+v, want run %q", assigned, recoverable.RunID)
	}
}
