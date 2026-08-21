package repository

import (
	"context"
	"encoding/json"
	"errors"
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

// assertStepNotice checks a failure-like notice by its allowlisted category,
// its two bounded numbers, and the state version it is anchored to.
//
// The sentence these notices used to carry is gone deliberately. It could not
// be localized, it could not be filtered on, and it was assembled by string
// formatting a few lines away from provider and worker text. A category plus
// "attempt N of M" says everything the sentence did and nothing it should not.
//
// The version is required rather than optional because a notice a client
// cannot place against a run state is not reconcilable: it either duplicates
// on replay or is discarded as stale.
func assertStepNotice(t *testing.T, event Event, category runoutcome.NoticeCategory, attempt int, maxAttempts int, stateVersion int64) {
	t.Helper()
	if event.Type != "agent.run.step" {
		t.Fatalf("event type = %q, want agent.run.step", event.Type)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatalf("run step payload %q: %v", event.PayloadJSON, err)
	}
	want := map[string]any{
		"category":     string(category),
		"attempt":      float64(attempt),
		"maxAttempts":  float64(maxAttempts),
		"stateVersion": float64(stateVersion),
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

	decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
		RunID: enqueued.RunID, Failure: dispatchCondition("worker_busy"), MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Requeued {
		t.Fatalf("decision = %+v, want requeued", decision)
	}
	// A requeue used to be silent, which is indistinguishable from a hang to a
	// watching client. It must now carry exactly one user-visible notice.
	notice := onlyRunStepEvent(t, decision.Events)
	requeued, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	assertStepNotice(t, notice, runoutcome.NoticeDispatchRetry, 2, 3, requeued.StateVersion)
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
		decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
			RunID: enqueued.RunID, Failure: dispatchCondition("worker_busy"), MaxAttempts: maxAttempts,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !decision.Requeued {
			t.Fatalf("attempt %d: decision = %+v, want requeued", i+1, decision)
		}
		// The notice must name the attempt that is about to START, not the one
		// that just failed: after the first rejection the user is on attempt 2.
		requeued, err := repo.GetRunState(ctx, enqueued.RunID)
		if err != nil {
			t.Fatal(err)
		}
		assertStepNotice(t, onlyRunStepEvent(t, decision.Events), runoutcome.NoticeDispatchRetry, i+2, maxAttempts, requeued.StateVersion)
		if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-busy"); err != nil {
			t.Fatal(err)
		}
	}

	beforeGiveUp, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
		RunID: enqueued.RunID, Failure: dispatchCondition("worker_busy"), MaxAttempts: maxAttempts,
	})
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
	assertStepNotice(t, giveUp, runoutcome.NoticeRecoveryExhausted, 3, 3, beforeGiveUp.StateVersion)

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

	decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
		RunID: enqueued.RunID, Failure: dispatchCondition("worker_busy"), MaxAttempts: 3,
	})
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

	decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
		RunID: enqueued.RunID, Failure: dispatchCondition("worker_busy"), MaxAttempts: 3,
	})
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

	decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
		RunID: enqueued.RunID, Failure: dispatchCondition("worker_busy"), MaxAttempts: 3,
	})
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

// ---------------------------------------------------------------------------
// Confirmed release versus uncertain ownership.
//
// Both universal rules were wrong. Rewriting every retry as running-to-queued
// erased the interval where nobody owned a lost run; routing every retry
// through recovering invented an uncertain phase for a run whose owner had
// just handed it back in person. The difference is not the failure code, it is
// whether this transaction can prove who was holding the run when it was
// released.
//
// A proof is an authenticated current attempt returning a same-run-transient
// failure, or an assignment command that provably never reached a worker.
// Anything else — a lost stream, an unresolved send, a stale attempt, an
// expired lease — is uncertainty and keeps recovering.
// ---------------------------------------------------------------------------

// runStateProjections returns the durable public projections a run has
// accumulated, in log order. The decision slice is not enough on its own: the
// three pre-delivery writers return only an error, so what they appended can
// only be read back out of the log.
func runStateProjections(t *testing.T, repo *Repository, runID string) []eventSnapshot {
	t.Helper()
	rows, err := repo.db.QueryContext(context.Background(), `
		SELECT id, type, payload_json
		FROM events
		WHERE run_id = ? AND type = ?
		ORDER BY sequence
	`, runID, runStateChangedEventType)
	if err != nil {
		t.Fatalf("read run state projections: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var projections []eventSnapshot
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.EventID, &event.Type, &event.PayloadJSON); err != nil {
			t.Fatalf("scan run state projection: %v", err)
		}
		projections = append(projections, decodeRunStateSnapshot(t, event))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read run state projections: %v", err)
	}
	return projections
}

// jobProgress is the pair a fencing test has to watch: a write-free refusal
// must leave the job exactly where it found it, including the attempt counter
// a requeue would have spent.
type jobProgress struct {
	status  string
	attempt int
}

func readJobProgress(t *testing.T, repo *Repository, jobID string) jobProgress {
	t.Helper()
	var progress jobProgress
	if err := repo.db.QueryRowContext(context.Background(),
		`SELECT status, attempt FROM jobs WHERE id = ?`, jobID).Scan(&progress.status, &progress.attempt); err != nil {
		t.Fatalf("read job progress: %v", err)
	}
	return progress
}

// onlyQueuedProjection asserts the run gained exactly one projection since
// before, that it is queued at the expected version, and that no recovering
// projection was invented on the way there.
func onlyQueuedProjection(t *testing.T, repo *Repository, runID string, before []eventSnapshot, wantVersion int64) {
	t.Helper()
	after := runStateProjections(t, repo, runID)
	if len(after) != len(before)+1 {
		t.Fatalf("confirmed release appended %d projections, want exactly 1: %+v", len(after)-len(before), after[len(before):])
	}
	committed := after[len(after)-1]
	if committed.Lifecycle != lifecycleQueued {
		t.Fatalf("confirmed release projected %q, want queued (no uncertain phase to report)", committed.Lifecycle)
	}
	if committed.StateVersion != wantVersion {
		t.Fatalf("confirmed release projected version %d, want exactly one increment to %d",
			committed.StateVersion, wantVersion)
	}
	if committed.OutcomeReason != "none" {
		t.Fatalf("confirmed release projected outcome %q, want none: the run has not ended", committed.OutcomeReason)
	}
	for _, projection := range after[len(before):] {
		if projection.Lifecycle == lifecycleRecovering {
			t.Fatalf("confirmed release passed through recovering: %+v", projection)
		}
	}
}

// assertReleasedToQueue checks the row half of a confirmed release: the run is
// queued at one increment, carries no outcome, and no longer claims an owner.
func assertReleasedToQueue(t *testing.T, repo *Repository, runID string, wantVersion int64) {
	t.Helper()
	state, err := repo.GetRunState(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Lifecycle != lifecycleQueued || state.OutcomeReason != "none" {
		t.Fatalf("released run = %s/%s, want queued/none", state.Lifecycle, state.OutcomeReason)
	}
	if state.StateVersion != wantVersion {
		t.Fatalf("released run version = %d, want exactly one increment to %d", state.StateVersion, wantVersion)
	}
	if state.FinishedAt.Valid || state.FinishedAt.String != "" {
		t.Fatalf("released run carries a finish time: %+v", state.FinishedAt)
	}
	run, err := repo.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.WorkerID != "" || run.ExecutionActive {
		t.Fatalf("released run still claims an owner: %+v", run)
	}
}

// TestConfirmedReleaseRetryRequeuesRunningDirectlyToQueued is the honest shape
// of a worker handing a run back.
//
// The worker is authenticated, the attempt it names is the attempt that owns
// the run, the version it computed against is the version the row holds, and
// the failure it reported is same-run transient. Nothing about that is
// uncertain, so there is no uncertain phase to publish: one transaction, one
// increment, one queued projection.
func TestConfirmedReleaseRetryRequeuesRunningDirectlyToQueued(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed := claimRetryRun(t, repo, "worker-confirmed-release")
	before := runStateProjections(t, repo, enqueued.RunID)

	decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: claimed.ExpectedStateVersion,
		WorkerID:             "worker-confirmed-release",
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
		Failure:              dispatchCondition("worker_busy"),
		MaxAttempts:          3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Requeued {
		t.Fatalf("decision = %+v, want requeued", decision)
	}

	wantVersion := claimed.ExpectedStateVersion + 1
	onlyQueuedProjection(t, repo, enqueued.RunID, before, wantVersion)
	assertReleasedToQueue(t, repo, enqueued.RunID, wantVersion)

	// The decision the caller publishes has to carry the same single
	// projection: a client watching live must not be shown a phase the log
	// does not contain.
	var published []eventSnapshot
	for _, event := range decision.Events {
		if event.Type == runStateChangedEventType {
			published = append(published, decodeRunStateSnapshot(t, event))
		}
	}
	if len(published) != 1 || published[0].Lifecycle != lifecycleQueued || published[0].StateVersion != wantVersion {
		t.Fatalf("published projections = %+v, want one queued at version %d", published, wantVersion)
	}

	// The notice is anchored to the version the requeue actually committed,
	// and names the attempt the user is about to wait through.
	assertStepNotice(t, onlyRunStepEvent(t, decision.Events), runoutcome.NoticeDispatchRetry, 2, 3, wantVersion)
	if got := readJobProgress(t, repo, claimed.JobID); got != (jobProgress{status: "pending", attempt: 2}) {
		t.Fatalf("requeued job = %+v, want pending attempt 2", got)
	}
}

// TestConfirmedUnsentAssignmentsRequeueRunningDirectlyToQueued covers the other
// proof: the orchestrator knows the assignment command never reached anyone.
//
// A job that was claimed but never handed out, a pending send that was
// abandoned before it started, and a send that provably failed all leave the
// same fact behind — no worker ever received the work. Publishing recovering
// for those would describe an executor that does not exist.
func TestConfirmedUnsentAssignmentsRequeueRunningDirectlyToQueued(t *testing.T) {
	for name, release := range map[string]func(*testing.T, *Repository, Assignment) error{
		"claimed job never dispatched": func(t *testing.T, repo *Repository, assignment Assignment) error {
			return repo.RequeueClaimedJob(context.Background(), assignment.JobID, assignment.RunID)
		},
		"pending send abandoned before it started": func(t *testing.T, repo *Repository, assignment Assignment) error {
			return repo.AbortPendingAssignment(context.Background(), assignment)
		},
		"send proven not to have left the orchestrator": func(t *testing.T, repo *Repository, assignment Assignment) error {
			if err := repo.BeginAssignmentSend(context.Background(), assignment); err != nil {
				t.Fatalf("BeginAssignmentSend: %v", err)
			}
			return repo.AbortUnsentAssignment(context.Background(), assignment)
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			enqueued, claimed := claimRetryRun(t, repo, "worker-unsent")
			assignment := Assignment{
				JobID: claimed.JobID, RunID: enqueued.RunID,
				WorkerID: "worker-unsent", AttemptID: claimed.AssignmentAttemptID,
			}
			before := runStateProjections(t, repo, enqueued.RunID)

			if err := release(t, repo, assignment); err != nil {
				t.Fatalf("release unsent assignment: %v", err)
			}

			wantVersion := claimed.ExpectedStateVersion + 1
			onlyQueuedProjection(t, repo, enqueued.RunID, before, wantVersion)
			assertReleasedToQueue(t, repo, enqueued.RunID, wantVersion)
		})
	}
}

// TestUncertainOwnershipRequeueStillCommitsRecoveringThenQueued is the half the
// direct edge must not swallow.
//
// A lease that expired over a delivered assignment, and a retryable failure
// nobody can attribute to the attempt that owns the run, both mean the same
// thing: there may still be an executor out there. That interval is real, so
// it is published — recovering at one increment, queued at the next.
func TestUncertainOwnershipRequeueStillCommitsRecoveringThenQueued(t *testing.T) {
	for name, lose := range map[string]func(*testing.T, *Repository, EnqueueUserMessageResult, Job){
		"lost worker over a delivered assignment": func(t *testing.T, repo *Repository, enqueued EnqueueUserMessageResult, claimed Job) {
			ctx := context.Background()
			assignment := Assignment{
				JobID: claimed.JobID, RunID: enqueued.RunID,
				WorkerID: "worker-uncertain", AttemptID: claimed.AssignmentAttemptID,
			}
			if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
				t.Fatalf("BeginAssignmentSend: %v", err)
			}
			if err := repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
				t.Fatalf("MarkAssignmentDelivered: %v", err)
			}
			// The command reached a worker that is now gone. Nobody can say
			// whether it is still executing, which is the definition of the
			// state recovering exists to publish.
			reconciliation, err := repo.RecoverAssignmentWithLimit(ctx, assignment, 3)
			if err != nil {
				t.Fatalf("RecoverAssignmentWithLimit: %v", err)
			}
			if !reconciliation.Requeued {
				t.Fatalf("reconciliation = %+v, want requeued", reconciliation)
			}
		},
		"retryable failure with no proven owner": func(t *testing.T, repo *Repository, enqueued EnqueueUserMessageResult, claimed Job) {
			// No worker and no attempt: the caller cannot prove who released
			// the run, so the direct edge is not available to it even though
			// it knows the version.
			decision, err := repo.RequeueOrFailRetryableRun(context.Background(), RetryableRunFailureInput{
				RunID:                enqueued.RunID,
				ExpectedStateVersion: claimed.ExpectedStateVersion,
				Failure:              dispatchCondition("worker_unavailable"),
				MaxAttempts:          3,
			})
			if err != nil {
				t.Fatalf("RequeueOrFailRetryableRun: %v", err)
			}
			if !decision.Requeued {
				t.Fatalf("decision = %+v, want requeued", decision)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			enqueued, claimed := claimRetryRun(t, repo, "worker-uncertain")
			before := runStateProjections(t, repo, enqueued.RunID)

			lose(t, repo, enqueued, claimed)

			after := runStateProjections(t, repo, enqueued.RunID)
			committed := after[len(before):]
			if len(committed) != 2 {
				t.Fatalf("uncertain requeue projected %d states, want recovering then queued: %+v", len(committed), committed)
			}
			if committed[0].Lifecycle != lifecycleRecovering || committed[0].StateVersion != claimed.ExpectedStateVersion+1 {
				t.Fatalf("first projection = %s at version %d, want recovering at %d",
					committed[0].Lifecycle, committed[0].StateVersion, claimed.ExpectedStateVersion+1)
			}
			if committed[1].Lifecycle != lifecycleQueued || committed[1].StateVersion != claimed.ExpectedStateVersion+2 {
				t.Fatalf("second projection = %s at version %d, want queued at %d",
					committed[1].Lifecycle, committed[1].StateVersion, claimed.ExpectedStateVersion+2)
			}
			assertReleasedToQueue(t, repo, enqueued.RunID, claimed.ExpectedStateVersion+2)
		})
	}
}

// TestConfirmedReleaseRejectsStaleVersionWorkerAndAttempt pins the guard that
// makes the direct edge narrow enough to be safe.
//
// Every one of these reports is a claim about a run somebody else is holding,
// or about a state the run has already left. Applying any of them would requeue
// work that is still executing, so each is refused without touching the row,
// the job, or the log.
func TestConfirmedReleaseRejectsStaleVersionWorkerAndAttempt(t *testing.T) {
	for name, corrupt := range map[string]func(Job) RetryableRunFailureInput{
		"version the run has already left": func(claimed Job) RetryableRunFailureInput {
			return RetryableRunFailureInput{
				ExpectedStateVersion: claimed.ExpectedStateVersion - 1,
				WorkerID:             "worker-stale-report",
				AssignmentAttemptID:  claimed.AssignmentAttemptID,
			}
		},
		"version the run has never reached": func(claimed Job) RetryableRunFailureInput {
			return RetryableRunFailureInput{
				ExpectedStateVersion: claimed.ExpectedStateVersion + 7,
				WorkerID:             "worker-stale-report",
				AssignmentAttemptID:  claimed.AssignmentAttemptID,
			}
		},
		"worker that does not own the run": func(claimed Job) RetryableRunFailureInput {
			return RetryableRunFailureInput{
				ExpectedStateVersion: claimed.ExpectedStateVersion,
				WorkerID:             "worker-impostor",
				AssignmentAttemptID:  claimed.AssignmentAttemptID,
			}
		},
		"attempt that was already superseded": func(claimed Job) RetryableRunFailureInput {
			return RetryableRunFailureInput{
				ExpectedStateVersion: claimed.ExpectedStateVersion,
				WorkerID:             "worker-stale-report",
				AssignmentAttemptID:  claimed.AssignmentAttemptID + "-previous",
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, claimed := claimRetryRun(t, repo, "worker-stale-report")
			before, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			beforeProjections := len(runStateProjections(t, repo, enqueued.RunID))
			beforeEvents := countRunEvents(t, repo, enqueued.RunID)
			beforeJob := readJobProgress(t, repo, claimed.JobID)

			input := corrupt(claimed)
			input.RunID = enqueued.RunID
			input.Failure = dispatchCondition("worker_busy")
			input.MaxAttempts = 3
			decision, err := repo.RequeueOrFailRetryableRun(ctx, input)
			if !errors.Is(err, ErrAssignmentFenced) {
				t.Fatalf("stale confirmed release = (%+v, %v), want %v", decision, err, ErrAssignmentFenced)
			}

			after, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("fenced report changed the run: %+v, want %+v", after, before)
			}
			if got := len(runStateProjections(t, repo, enqueued.RunID)); got != beforeProjections {
				t.Fatalf("fenced report appended %d projections", got-beforeProjections)
			}
			if got := countRunEvents(t, repo, enqueued.RunID); got != beforeEvents {
				t.Fatalf("fenced report appended %d events", got-beforeEvents)
			}
			if got := readJobProgress(t, repo, claimed.JobID); got != beforeJob {
				t.Fatalf("fenced report changed the job: %+v, want %+v", got, beforeJob)
			}
		})
	}
}

// TestWaitingApprovalCannotUseConfirmedReleaseRequeue closes the one lifecycle
// where a released worker is not the whole story.
//
// A run waiting for a human answer has an approval under it, and possibly a
// tool call the answer authorizes. Dropping it straight back on the queue would
// leave that decision hanging over a run nobody is running. The direct edge is
// defined from running only, so this is refused outright rather than quietly
// terminalized.
func TestWaitingApprovalCannotUseConfirmedReleaseRequeue(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, _ := waitingApprovalRun(t, repo, "worker-waiting-approval")
	before, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents := countRunEvents(t, repo, enqueued.RunID)
	beforeJob := readJobProgress(t, repo, claimed.JobID)

	decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: before.StateVersion,
		WorkerID:             "worker-waiting-approval",
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
		Failure:              dispatchCondition("worker_busy"),
		MaxAttempts:          3,
	})
	if !errors.Is(err, ErrAssignmentFenced) {
		t.Fatalf("waiting-approval confirmed release = (%+v, %v), want %v", decision, err, ErrAssignmentFenced)
	}

	after, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("refused release changed the run: %+v, want %+v", after, before)
	}
	if got := countRunEvents(t, repo, enqueued.RunID); got != beforeEvents {
		t.Fatalf("refused release appended %d events", got-beforeEvents)
	}
	if got := readJobProgress(t, repo, claimed.JobID); got != beforeJob {
		t.Fatalf("refused release changed the job: %+v, want %+v", got, beforeJob)
	}
}

// TestRetryableRunFailureInputRejectsIncompleteOrInvalidConfirmedIdentityWithoutWrites
// pins the input gate that runs before the transaction opens.
//
// Half an identity — a worker with no attempt, or an attempt with no worker —
// proves nothing about who was holding the run, and without this gate it falls
// through confirmedRelease() as if the caller had honestly named nobody. The
// run is then published as recovering and requeued, which invents an uncertain
// interval for a caller that meant to prove something and got its own input
// wrong. A full identity with expected version 0 is the other half: the version
// is what the direct edge fences on, and 0 is never a version a run has held,
// so it would be refused as ErrAssignmentFenced — a stale-owner answer to what
// is really a malformed request.
//
// Each case must be refused by its own error and must leave the run, the job,
// the log, and the projections exactly where it found them.
func TestRetryableRunFailureInputRejectsIncompleteOrInvalidConfirmedIdentityWithoutWrites(t *testing.T) {
	for name, testCase := range map[string]struct {
		corrupt func(Job) RetryableRunFailureInput
		wantErr error
	}{
		"worker without its attempt": {
			corrupt: func(claimed Job) RetryableRunFailureInput {
				return RetryableRunFailureInput{
					ExpectedStateVersion: claimed.ExpectedStateVersion,
					WorkerID:             "worker-half-identity",
				}
			},
			wantErr: ErrRetryIdentityIncomplete,
		},
		"attempt without its worker": {
			corrupt: func(claimed Job) RetryableRunFailureInput {
				return RetryableRunFailureInput{
					ExpectedStateVersion: claimed.ExpectedStateVersion,
					AssignmentAttemptID:  claimed.AssignmentAttemptID,
				}
			},
			wantErr: ErrRetryIdentityIncomplete,
		},
		"full identity with no expected version": {
			corrupt: func(claimed Job) RetryableRunFailureInput {
				return RetryableRunFailureInput{
					ExpectedStateVersion: 0,
					WorkerID:             "worker-half-identity",
					AssignmentAttemptID:  claimed.AssignmentAttemptID,
				}
			},
			wantErr: ErrRunStateVersionInvalid,
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			enqueued, claimed := claimRetryRun(t, repo, "worker-half-identity")
			before, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			beforeProjections := runStateProjections(t, repo, enqueued.RunID)
			beforeEvents := countRunEvents(t, repo, enqueued.RunID)
			beforeJob := readJobProgress(t, repo, claimed.JobID)

			input := testCase.corrupt(claimed)
			input.RunID = enqueued.RunID
			input.Failure = dispatchCondition("worker_busy")
			input.MaxAttempts = 3
			decision, err := repo.RequeueOrFailRetryableRun(ctx, input)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("malformed retry report = (%+v, %v), want %v", decision, err, testCase.wantErr)
			}

			after, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("rejected report changed the run: %+v, want %+v", after, before)
			}
			afterProjections := runStateProjections(t, repo, enqueued.RunID)
			if len(afterProjections) != len(beforeProjections) {
				t.Fatalf("rejected report appended projections %+v", afterProjections[len(beforeProjections):])
			}
			if got := countRunEvents(t, repo, enqueued.RunID); got != beforeEvents {
				t.Fatalf("rejected report appended %d events", got-beforeEvents)
			}
			if got := readJobProgress(t, repo, claimed.JobID); got != beforeJob {
				t.Fatalf("rejected report changed the job: %+v, want %+v", got, beforeJob)
			}
		})
	}
}

// waitingApprovalRun leaves a run in waiting_approval with its owning attempt
// intact — the state a worker is in when it has asked a human a question and is
// still holding the run while it waits.
func waitingApprovalRun(t *testing.T, repo *Repository, worker string) (EnqueueUserMessageResult, Job, ApprovalRecord) {
	t.Helper()
	ctx := context.Background()
	enqueued := enqueueRun(t, repo, "Waiting approval release")
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", worker)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_waiting_release", RunID: enqueued.RunID, ModelToolCallID: "model_waiting_release",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:waiting-release"); err != nil {
		t.Fatalf("RecordToolCallBefore: %v", err)
	}
	approval, _, err := repo.CreateApprovalWithEvent(ctx, enqueued.RunID, "call_waiting_release", "general_assistant",
		"files.update", `{"path":"note.txt"}`, "sha256:waiting-release", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("CreateApprovalWithEvent: %v", err)
	}
	waiting, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Lifecycle != lifecycleWaitingApproval {
		t.Fatalf("run lifecycle = %q, want waiting_approval", waiting.Lifecycle)
	}
	return enqueued, claimed, approval
}

// TestRequeueClaimedJobAfterDeliveryUsesUncertainRecovery closes the hole the
// direct edge left open on the one requeue caller that names no owner.
//
// RequeueClaimedJob identifies a job and a run and nothing else — no worker, no
// attempt. That is enough proof for a claim that was never dispatched, and it
// used to be treated as enough for every state that is not an in-flight send.
// Once the assignment has been delivered there is a worker that may still be
// executing it, so releasing the run straight back to queued would publish a
// certainty nobody holds. Delivered ownership takes the uncertain edge:
// recovering first, queued after.
func TestRequeueClaimedJobAfterDeliveryUsesUncertainRecovery(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed := claimRetryRun(t, repo, "worker-delivered-requeue")
	assignment := Assignment{
		JobID: claimed.JobID, RunID: enqueued.RunID,
		WorkerID: "worker-delivered-requeue", AttemptID: claimed.AssignmentAttemptID,
	}
	if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatalf("BeginAssignmentSend: %v", err)
	}
	if err := repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
		t.Fatalf("MarkAssignmentDelivered: %v", err)
	}
	before := runStateProjections(t, repo, enqueued.RunID)

	if err := repo.RequeueClaimedJob(ctx, claimed.JobID, enqueued.RunID); err != nil {
		t.Fatalf("RequeueClaimedJob: %v", err)
	}

	committed := runStateProjections(t, repo, enqueued.RunID)[len(before):]
	if len(committed) != 2 {
		t.Fatalf("requeue after delivery projected %d states, want recovering then queued: %+v", len(committed), committed)
	}
	if committed[0].Lifecycle != lifecycleRecovering || committed[0].StateVersion != claimed.ExpectedStateVersion+1 {
		t.Fatalf("first projection = %s at version %d, want recovering at %d",
			committed[0].Lifecycle, committed[0].StateVersion, claimed.ExpectedStateVersion+1)
	}
	if committed[1].Lifecycle != lifecycleQueued || committed[1].StateVersion != claimed.ExpectedStateVersion+2 {
		t.Fatalf("second projection = %s at version %d, want queued at %d",
			committed[1].Lifecycle, committed[1].StateVersion, claimed.ExpectedStateVersion+2)
	}
	// Order in the slice is order in the durable log only because the sequences
	// say so: a client replaying the log must not be able to see queued before
	// the recovering phase it is recovering from. Reading the rows back in
	// insertion order proves both halves — that the sequences climb with the
	// writes, and that recovering was the write that landed first.
	inserted := runStateProjectionsInInsertionOrder(t, repo, enqueued.RunID)
	if len(inserted) < 2 {
		t.Fatalf("run has %d durable projections, want at least recovering then queued: %+v", len(inserted), inserted)
	}
	tail := inserted[len(inserted)-2:]
	if tail[0].Lifecycle != lifecycleRecovering || tail[1].Lifecycle != lifecycleQueued {
		t.Fatalf("durable insertion order ends %s then %s, want recovering then queued",
			tail[0].Lifecycle, tail[1].Lifecycle)
	}
	if tail[0].StateVersion != claimed.ExpectedStateVersion+1 || tail[1].StateVersion != claimed.ExpectedStateVersion+2 {
		t.Fatalf("durable insertion order carries versions %d then %d, want %d then %d",
			tail[0].StateVersion, tail[1].StateVersion,
			claimed.ExpectedStateVersion+1, claimed.ExpectedStateVersion+2)
	}
	// The row half: queued at the second increment, no owner left behind.
	assertReleasedToQueue(t, repo, enqueued.RunID, claimed.ExpectedStateVersion+2)
	if got := readJobProgress(t, repo, claimed.JobID); got != (jobProgress{status: "pending", attempt: 2}) {
		t.Fatalf("requeued job = %+v, want pending attempt 2", got)
	}
}

// runStateProjectionsInInsertionOrder returns a run's lifecycle projections in
// the order the rows were durably inserted and asserts that their sequences
// increase in that same order.
//
// The insertion order has to come from a key the writer does not choose:
// ordering by sequence and then checking that sequence increases proves
// nothing, because UNIQUE(session_id, sequence) already makes that sort
// strictly increasing whatever order the rows were written in. SQLite's rowid
// is assigned at insert time and is independent of the sequence column, so
// sorting by it and finding sequences out of order would mean a row committed
// earlier carries a later replay position than one committed after it.
func runStateProjectionsInInsertionOrder(t *testing.T, repo *Repository, runID string) []eventSnapshot {
	t.Helper()
	rows, err := repo.db.QueryContext(context.Background(), `
		SELECT sequence, id, type, payload_json
		FROM events
		WHERE run_id = ? AND type = ?
		ORDER BY rowid
	`, runID, runStateChangedEventType)
	if err != nil {
		t.Fatalf("read run state sequences: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var projections []eventSnapshot
	previous := int64(-1)
	for rows.Next() {
		var sequence int64
		var event Event
		if err := rows.Scan(&sequence, &event.EventID, &event.Type, &event.PayloadJSON); err != nil {
			t.Fatalf("scan run state sequence: %v", err)
		}
		if sequence <= previous {
			t.Fatalf("run state projection inserted after sequence %d carries sequence %d: "+
				"a later write took an earlier replay position", previous, sequence)
		}
		previous = sequence
		projections = append(projections, decodeRunStateSnapshot(t, event))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read run state sequences: %v", err)
	}
	return projections
}
