package app

import (
	"context"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestAppRestartRecoversUnexpiredDeliveredAssignment(t *testing.T) {
	dbPath := t.TempDir() + "/turing.db"
	cfg := config.Config{
		ClientAPIKey: "client", RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer", ApprovalJWTSecret: "approval-secret", DatabasePath: dbPath,
	}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session, err := first.Repository.CreateSession(context.Background(), "Restart delivered recovery")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := first.Repository.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "recover delivered attempt", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := first.Repository.ClaimNextJobWithLimit(context.Background(), "general_assistant", "worker-before-restart", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	assignment := repository.Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-before-restart", AttemptID: claimed.AssignmentAttemptID,
	}
	if err := first.Repository.BeginAssignmentSend(context.Background(), assignment); err != nil {
		t.Fatal(err)
	}
	if err := first.Repository.MarkAssignmentDelivered(context.Background(), assignment); err != nil {
		t.Fatal(err)
	}
	first.Stop()

	restarted, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Stop)
	run, err := restarted.Repository.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" || run.ExecutionActive {
		t.Fatalf("restart left delivered assignment %+v, want queued inactive run", run)
	}
	reclaimed, err := restarted.Repository.ClaimNextJobWithLimit(context.Background(), "general_assistant", "worker-after-restart", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.RunID != enqueued.RunID || reclaimed.Attempt != 2 {
		t.Fatalf("reclaimed assignment = %+v, want recovered second attempt for %q", reclaimed, enqueued.RunID)
	}
}

func TestAppReaperRecoversUnownedExpiredAssignmentAndStops(t *testing.T) {
	cfg := config.Config{
		ClientAPIKey: "client", RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer", ApprovalJWTSecret: "approval-secret",
		DatabasePath: t.TempDir() + "/turing.db", JobReaperIntervalMS: 5,
	}
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			application.Stop()
		}
	}()
	session, err := application.Repository.CreateSession(context.Background(), "Periodic orphan recovery")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := application.Repository.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "recover orphan", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Repository.ClaimNextJobWithLimit(context.Background(), "general_assistant", "worker-gone", 1, time.Millisecond); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		run, err := application.Repository.GetRun(context.Background(), enqueued.RunID)
		if err == nil && run.Status == "queued" && !run.ExecutionActive {
			application.Stop()
			stopped = true
			select {
			case <-application.reaperDone:
				return
			case <-time.After(time.Second):
				t.Fatal("reaper did not stop with app context")
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("periodic reaper did not recover unowned expired assignment")
}
