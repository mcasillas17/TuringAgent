package repository

import (
	"context"
	"encoding/json"
	"testing"
)

// claimRetryRun enqueues a message and claims its job, leaving the run running
// and its job in_progress under the given worker.
func claimRetryRun(t *testing.T, repo *Repository, worker string) (EnqueueUserMessageResult, Job) {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Retry requeue")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "retry me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", worker)
	if err != nil {
		t.Fatal(err)
	}
	return enqueued, claimed
}

func TestRequeueOrFailRetryableRunRequeuesWhileAttemptsRemain(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, claimed := claimRetryRun(t, repo, "worker-busy")

	decision, err := repo.RequeueOrFailRetryableRun(ctx, enqueued.RunID, "worker_busy", "worker cannot accept the run", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Requeued {
		t.Fatalf("decision = %+v, want requeued", decision)
	}
	if len(decision.Events) != 0 {
		t.Fatalf("requeue emitted %d events, want none", len(decision.Events))
	}

	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" || run.ExecutionActive || run.WorkerID != "" {
		t.Fatalf("requeued run = %+v, want queued and idle", run)
	}

	var jobStatus string
	var attempt int
	if err := database.QueryRowContext(ctx, `SELECT status, attempt FROM jobs WHERE id = ?`, claimed.JobID).Scan(&jobStatus, &attempt); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "pending" || attempt != 2 {
		t.Fatalf("requeued job = status:%q attempt:%d, want pending attempt 2", jobStatus, attempt)
	}
}

func TestRequeueOrFailRetryableRunFailsAfterAttemptCap(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, claimed := claimRetryRun(t, repo, "worker-busy")

	const maxAttempts = 3
	// Attempts 1 and 2 requeue; the run is re-claimed each time so it is running
	// again for the next rejection.
	for i := 0; i < maxAttempts-1; i++ {
		decision, err := repo.RequeueOrFailRetryableRun(ctx, enqueued.RunID, "worker_busy", "busy", maxAttempts)
		if err != nil {
			t.Fatal(err)
		}
		if !decision.Requeued {
			t.Fatalf("attempt %d: decision = %+v, want requeued", i+1, decision)
		}
		if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-busy"); err != nil {
			t.Fatal(err)
		}
	}

	decision, err := repo.RequeueOrFailRetryableRun(ctx, enqueued.RunID, "worker_busy", "busy", maxAttempts)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Requeued {
		t.Fatalf("attempt %d exceeded cap but was requeued", maxAttempts)
	}

	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || run.ExecutionActive {
		t.Fatalf("exhausted run = %+v, want failed and idle", run)
	}

	var runErrorCode, jobStatus, jobErrorCode string
	if err := database.QueryRowContext(ctx, `
		SELECT r.error_code, j.status, j.error_code
		FROM agent_runs r JOIN jobs j ON j.id = ?
		WHERE r.id = ?
	`, claimed.JobID, enqueued.RunID).Scan(&runErrorCode, &jobStatus, &jobErrorCode); err != nil {
		t.Fatal(err)
	}
	if runErrorCode != RetriesExhaustedCode || jobStatus != "failed" || jobErrorCode != RetriesExhaustedCode {
		t.Fatalf("exhausted terminal state = run code:%q job status:%q job code:%q, want %q",
			runErrorCode, jobStatus, jobErrorCode, RetriesExhaustedCode)
	}

	var terminal Event
	for _, event := range decision.Events {
		if event.Type == "agent.run.failed" {
			terminal = event
		}
	}
	if terminal.EventID == "" {
		t.Fatalf("exhaustion emitted no agent.run.failed event: %+v", decision.Events)
	}
	var payload struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}
	if err := json.Unmarshal([]byte(terminal.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != RetriesExhaustedCode || payload.Retryable {
		t.Fatalf("terminal payload = %+v, want code %q retryable false", payload, RetriesExhaustedCode)
	}
}
