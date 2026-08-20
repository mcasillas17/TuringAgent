package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
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

// assertStepNotice checks a failure-like notice by its allowlisted category and
// its two bounded numbers.
//
// The sentence these notices used to carry is gone deliberately. It could not
// be localized, it could not be filtered on, and it was assembled by string
// formatting a few lines away from provider and worker text. A category plus
// "attempt N of M" says everything the sentence did and nothing it should not.
func assertStepNotice(t *testing.T, event Event, category runoutcome.NoticeCategory, attempt int, maxAttempts int) {
	t.Helper()
	if event.Type != "agent.run.step" {
		t.Fatalf("event type = %q, want agent.run.step", event.Type)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatalf("run step payload %q: %v", event.PayloadJSON, err)
	}
	want := map[string]any{
		"category":    string(category),
		"attempt":     float64(attempt),
		"maxAttempts": float64(maxAttempts),
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("run step payload = %#v, want %#v", payload, want)
	}
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
	assertStepNotice(t, notice, runoutcome.NoticeDispatchRetry, 2, 3)
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
		assertStepNotice(t, onlyRunStepEvent(t, decision.Events), runoutcome.NoticeDispatchRetry, i+2, maxAttempts)
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

	// The client's agent.run.failed card communicates the terminal failure and
	// its reason, not the attempt count that led up to it, so this notice is
	// still needed to tell the user retries stopped and how many were tried.
	giveUp := onlyRunStepEvent(t, decision.Events)
	assertStepNotice(t, giveUp, runoutcome.NoticeRecoveryExhausted, 3, 3)

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
// fenced by a concurrent reconciliation — must not be described as retrying.
// This pins the retry notice's `requeueable` gate; the give-up gate needs a
// job that has actually spent its attempts, which is
// TestRequeueOrFailRetryableRunEmitsNoGiveUpForUnclaimableExhaustedJob below.
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

	// It must not be reported as retries_exhausted: it never spent a retry
	// budget. worker_busy cannot be the terminal code either — it normalizes to
	// no outcome at all, because "a worker was busy" describes a dispatch
	// condition, not a reason a user's request ended.
	var errorCode, outcomeReason string
	if err := database.QueryRowContext(ctx,
		`SELECT error_code, outcome_reason FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&errorCode, &outcomeReason); err != nil {
		t.Fatal(err)
	}
	if errorCode == RetriesExhaustedCode {
		t.Fatalf("run error code = %q, want anything but the exhausted code", errorCode)
	}
	if outcomeReason != "recovery_interrupted" {
		t.Fatalf("run outcome reason = %q, want recovery_interrupted", outcomeReason)
	}
}

// The give-up notice is gated on `requeueable` as well as on the attempt count.
// A run that is no longer running, whose job row still shows a spent attempt
// budget, must not be told "Gave up after 3 attempts" — it never gave up, it
// was terminalized by something else. Without this, dropping `requeueable &&`
// from the exhaustion guard would go unnoticed.
func TestRequeueOrFailRetryableRunEmitsNoGiveUpForUnclaimableExhaustedJob(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, claimed := claimRetryRun(t, repo, "worker-gone")

	// Spend the attempt budget, then take the run out of `running` so the
	// requeueable gate is false while attempt >= maxAttempts stays true.
	if _, err := database.ExecContext(ctx, `UPDATE jobs SET attempt = 3 WHERE id = ?`, claimed.JobID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE agent_runs SET status = 'queued' WHERE id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	decision, err := repo.RequeueOrFailRetryableRun(ctx, enqueued.RunID, "worker_busy", "busy", 3)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Requeued {
		t.Fatalf("decision = %+v, want terminal failure", decision)
	}
	for _, event := range decision.Events {
		if event.Type == "agent.run.step" {
			t.Fatalf("a run that never gave up was announced as giving up: %s", event.PayloadJSON)
		}
	}
}

// TestRetryRequeueCommitsBothTransitionsWithoutARunningToQueuedShortcut pins
// the shape of a retry.
//
// The old code rewrote running straight back to queued, which erased the fact
// that the run had lost its worker: a client watching versions would have seen
// the run go from running to queued with nothing in between, and a client
// reopening mid-retry would have been told the run was simply waiting. Both
// transitions are real, so both are committed — in one transaction, each with
// its own version and its own projection.
func TestRetryRequeueCommitsBothTransitionsWithoutARunningToQueuedShortcut(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _ := claimRetryRun(t, repo, "worker-two-versions")
	running, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}

	decision, err := repo.RequeueOrFailRetryableRun(ctx, enqueued.RunID, "worker_busy", "worker cannot accept the run", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Requeued {
		t.Fatalf("decision = %+v, want requeued", decision)
	}

	var projected []eventSnapshot
	for _, event := range decision.Events {
		if event.Type == runStateChangedEventType {
			projected = append(projected, decodeRunStateSnapshot(t, event))
		}
	}
	if len(projected) != 2 {
		t.Fatalf("requeue projected %d state changes, want exactly 2", len(projected))
	}
	if projected[0].Lifecycle != lifecycleRecovering || projected[0].StateVersion != running.StateVersion+1 {
		t.Fatalf("first projection = %s at version %d, want recovering at %d",
			projected[0].Lifecycle, projected[0].StateVersion, running.StateVersion+1)
	}
	if projected[1].Lifecycle != lifecycleQueued || projected[1].StateVersion != running.StateVersion+2 {
		t.Fatalf("second projection = %s at version %d, want queued at %d",
			projected[1].Lifecycle, projected[1].StateVersion, running.StateVersion+2)
	}
	// A nonterminal dispatch condition leaves no outcome behind. "A worker was
	// busy" is not a reason a user's request ended, because it did not end.
	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Lifecycle != lifecycleQueued || state.OutcomeReason != "none" {
		t.Fatalf("requeued state = %s/%s, want queued/none", state.Lifecycle, state.OutcomeReason)
	}
	if state.StateVersion != running.StateVersion+2 {
		t.Fatalf("version = %d, want two increments past %d", state.StateVersion, running.StateVersion)
	}
	// A nonterminal requeue leaves no finish time behind, in either half of the
	// column: NULL and the empty string both have to be absent, because a
	// client that reads either one would show the run as over.
	if state.FinishedAt.Valid || state.FinishedAt.String != "" {
		t.Fatalf("a requeued run carries a finish time: %+v", state.FinishedAt)
	}
	// The projections must be ordered in the durable log, not just in the slice.
	for index := 1; index < len(decision.Events); index++ {
		if decision.Events[index].Sequence <= decision.Events[index-1].Sequence {
			t.Fatalf("event %d has sequence %d, not after %d",
				index, decision.Events[index].Sequence, decision.Events[index-1].Sequence)
		}
	}
}
