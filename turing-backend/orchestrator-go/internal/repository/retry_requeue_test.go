package repository

import (
	"context"
	"encoding/json"
	"fmt"
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

// onlyRunStepEvent asserts exactly one agent.run.step notice is present and
// returns it. Notices are the whole point of these events, so "one and only
// one" is the assertion that catches both silence and accidental duplicates.
func onlyRunStepEvent(t *testing.T, events []Event) Event {
	t.Helper()
	var found []Event
	for _, event := range events {
		if event.Type == "agent.run.step" {
			found = append(found, event)
		}
	}
	if len(found) != 1 {
		t.Fatalf("got %d agent.run.step events, want exactly 1: %+v", len(found), events)
	}
	return found[0]
}

func runStepNote(t *testing.T, event Event) string {
	t.Helper()
	var payload struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatalf("run step payload %q: %v", event.PayloadJSON, err)
	}
	return payload.Note
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
	// A requeue used to be silent, which is indistinguishable from a hang to a
	// watching client. It must now carry exactly one user-visible notice.
	notice := onlyRunStepEvent(t, decision.Events)
	if got := runStepNote(t, notice); got != "Retrying (attempt 2 of 3)" {
		t.Fatalf("requeue note = %q, want %q", got, "Retrying (attempt 2 of 3)")
	}
	if !notice.RunID.Valid || notice.RunID.String != enqueued.RunID {
		t.Fatalf("requeue notice run_id = %+v, want %q", notice.RunID, enqueued.RunID)
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
		// The notice must name the attempt that is about to START, not the one
		// that just failed: after the first rejection the user is on attempt 2.
		want := fmt.Sprintf("Retrying (attempt %d of %d)", i+2, maxAttempts)
		if got := runStepNote(t, onlyRunStepEvent(t, decision.Events)); got != want {
			t.Fatalf("attempt %d: note = %q, want %q", i+1, got, want)
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

	// Without this the user reads "Retrying (attempt 3 of 3)" and then hears
	// nothing ever again — worse than silence throughout, because the client has
	// no agent.run.failed case at all.
	giveUp := onlyRunStepEvent(t, decision.Events)
	if got := runStepNote(t, giveUp); got != "Gave up after 3 attempts" {
		t.Fatalf("exhaustion note = %q, want %q", got, "Gave up after 3 attempts")
	}

	var terminal Event
	terminalIndex, giveUpIndex := -1, -1
	for index, event := range decision.Events {
		if event.Type == "agent.run.failed" {
			terminal = event
			terminalIndex = index
		}
		if event.EventID == giveUp.EventID {
			giveUpIndex = index
		}
	}
	// The notice explains the failure, so it must not arrive after it.
	if giveUpIndex > terminalIndex {
		t.Fatalf("give-up notice at %d lands after agent.run.failed at %d", giveUpIndex, terminalIndex)
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

// A run that was never retryable in the first place — already terminalized or
// fenced by a concurrent reconciliation — must not be described as one that
// gave up after exhausting attempts. Both notices are gated on `requeueable`;
// this pins that gate, because moving either notice outside it would announce
// a retry that never happened.
func TestRequeueOrFailRetryableRunEmitsNoNoticeWhenNotRequeueable(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Not requeueable")
	if err != nil {
		t.Fatal(err)
	}
	// Enqueued but never claimed: the run is queued and its job pending, so no
	// in-progress attempt exists to requeue.
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "never claimed", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}

	decision, err := repo.RequeueOrFailRetryableRun(ctx, enqueued.RunID, "worker_busy", "busy", 3)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Requeued {
		t.Fatalf("decision = %+v, want terminal failure for an unclaimed run", decision)
	}
	for _, event := range decision.Events {
		if event.Type == "agent.run.step" {
			t.Fatalf("non-requeueable failure emitted a notice: %s", event.PayloadJSON)
		}
	}

	// It must also fail under the original code, not RetriesExhaustedCode: it
	// never spent a retry budget.
	var errorCode string
	if err := database.QueryRowContext(ctx, `SELECT error_code FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&errorCode); err != nil {
		t.Fatal(err)
	}
	if errorCode != "worker_busy" {
		t.Fatalf("run error code = %q, want %q", errorCode, "worker_busy")
	}
}
