package repository

import (
	"context"
	"testing"
	"time"
)

func TestRenewAssignmentsExtendsOnlyMatchingAttemptLease(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Heartbeat")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "keep alive", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-heartbeat", 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	renewedUntil := time.Now().UTC().Add(time.Hour)
	renewed, err := repo.RenewAssignments(ctx, []Assignment{{
		JobID: job.JobID, RunID: job.RunID, WorkerID: "worker-heartbeat", AttemptID: job.AssignmentAttemptID,
	}}, renewedUntil)
	if err != nil {
		t.Fatal(err)
	}
	if len(renewed) != 1 || renewed[0].AttemptID != job.AssignmentAttemptID {
		t.Fatalf("renewed assignments = %+v, want matching attempt", renewed)
	}

	renewed, err = repo.RenewAssignments(ctx, []Assignment{{
		JobID: enqueued.JobID, RunID: enqueued.RunID, WorkerID: "worker-heartbeat", AttemptID: "stale-attempt",
	}}, renewedUntil.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(renewed) != 0 {
		t.Fatalf("stale attempt renewed assignments = %+v, want none", renewed)
	}
}
