package repository

import (
	"context"
	"testing"
	"time"
)

func TestRecoverStaleUncertainAssignmentFencesAndRequeuesAtInjectedCutoff(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Stale assignment")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "recover stale attempt", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-lost")
	if err != nil {
		t.Fatal(err)
	}
	assignment := Assignment{JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-lost", AttemptID: claimed.AssignmentAttemptID}
	if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkAssignmentDeliveryUncertain(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReconcileAssignment(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" || !run.ExecutionActive {
		t.Fatalf("ambiguous attempt was released before recovery cutoff: %+v", run)
	}

	cutoff := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	expiredLease := cutoff.Add(-time.Second)
	if _, err := database.ExecContext(ctx, `UPDATE agent_runs SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ? WHERE id = ?`, FormatTimestamp(expiredLease), expiredLease.UnixNano(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecoverStaleAssignments(ctx, cutoff); err != nil {
		t.Fatal(err)
	}
	run, err = repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" || run.ExecutionActive {
		t.Fatalf("stale recovery run = %+v, want queued inactive", run)
	}
	recovered, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-fresh")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.RunID != enqueued.RunID || recovered.Attempt != 2 {
		t.Fatalf("recovered assignment = %+v, want second attempt for %q", recovered, enqueued.RunID)
	}
}
