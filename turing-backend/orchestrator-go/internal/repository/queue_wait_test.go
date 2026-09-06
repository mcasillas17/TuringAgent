package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

func queueColumns(t *testing.T, repo *Repository, runID string) (reason string, waited int64, since *int64, unroutable *int64) {
	t.Helper()
	if err := repo.db.QueryRowContext(context.Background(), `
		SELECT queue_wait_reason, queue_waited_ns, queued_since_ns, queue_unroutable_since_ns
		FROM agent_runs WHERE id = ?
	`, runID).Scan(&reason, &waited, &since, &unroutable); err != nil {
		t.Fatalf("read queue columns: %v", err)
	}
	return reason, waited, since, unroutable
}

func enqueueQueuedRun(t *testing.T, repo *Repository, title string) EnqueueUserMessageResult {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: title, AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	return enqueued
}

// A run that is never dispatched has to have a queue age anyway, and it has to
// start when the run was accepted rather than when something first looked at
// it. Otherwise the bound measures how often the sweep runs.
func TestEnqueueOpensTheQueuedInterval(t *testing.T) {
	repo := New(openTestDB(t))
	enqueued := enqueueQueuedRun(t, repo, "opens the interval")

	reason, waited, since, unroutable := queueColumns(t, repo, enqueued.RunID)
	if reason != string(runoutcome.QueueWaitNone) {
		t.Fatalf("fresh queue reason = %q, want %q", reason, runoutcome.QueueWaitNone)
	}
	if waited != 0 {
		t.Fatalf("fresh banked queue age = %d, want 0", waited)
	}
	if since == nil {
		t.Fatal("enqueue opened no queued interval, so this run would never accrue queue age")
	}
	if unroutable != nil {
		t.Fatal("enqueue opened a no-worker interval nothing had observed")
	}
	if elapsed := time.Since(time.Unix(0, *since)); elapsed < 0 || elapsed > time.Minute {
		t.Fatalf("queued interval opened %v ago, want it at the enqueue instant", elapsed)
	}
}

// The dedup boundary: reconciliation runs on every reaper tick, and an
// unchanged queue must cost nothing — no version, no event, no row write.
func TestObserveQueuedRunRoutingIsWriteFreeWhenNothingChanged(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueQueuedRun(t, repo, "unchanged")
	observedAt := time.Now().UTC().UnixNano()

	result, changed, err := repo.ObserveQueuedRunRouting(ctx, enqueued.RunID, runoutcome.QueueWaitNoCompatibleWorker, observedAt)
	if err != nil {
		t.Fatalf("first observation: %v", err)
	}
	if !changed || len(result.Events) != 1 {
		t.Fatalf("first observation changed=%v with %d events, want one committed transition", changed, len(result.Events))
	}
	first, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}

	for range 3 {
		_, changed, err := repo.ObserveQueuedRunRouting(ctx, enqueued.RunID,
			runoutcome.QueueWaitNoCompatibleWorker, observedAt+int64(time.Hour))
		if err != nil {
			t.Fatalf("repeat observation: %v", err)
		}
		if changed {
			t.Fatal("a repeat observation committed a transition")
		}
	}

	after, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after != first {
		t.Fatalf("repeat observations moved the run: %+v, want %+v", after, first)
	}
	// The interval keeps the instant it opened at, not the latest one seen: a
	// clock that restarted on every scan would never reach any deadline.
	_, _, _, unroutable := queueColumns(t, repo, enqueued.RunID)
	if unroutable == nil || *unroutable != observedAt {
		t.Fatalf("no-worker interval = %v, want it pinned to the first observation", unroutable)
	}
}

func TestObserveQueuedRunRoutingClearsTheIntervalOnRestoration(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueQueuedRun(t, repo, "restored")
	observedAt := time.Now().UTC().UnixNano()

	if _, _, err := repo.ObserveQueuedRunRouting(ctx, enqueued.RunID, runoutcome.QueueWaitNoCompatibleWorker, observedAt); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := repo.ObserveQueuedRunRouting(ctx, enqueued.RunID, runoutcome.QueueWaitNone, observedAt); err != nil {
		t.Fatal(err)
	} else if !changed {
		t.Fatal("restoration committed nothing")
	}

	reason, _, since, unroutable := queueColumns(t, repo, enqueued.RunID)
	if reason != string(runoutcome.QueueWaitNone) {
		t.Fatalf("restored queue reason = %q, want %q", reason, runoutcome.QueueWaitNone)
	}
	if unroutable != nil {
		t.Fatal("restoration left the no-worker interval open")
	}
	// Restoring the route is not leaving the queue: the overall queue age must
	// keep running, or a flapping worker would reset the outer bound forever.
	if since == nil {
		t.Fatal("restoration closed the queued interval")
	}
}

// The queued -> queued observation is a real transition and must obey the same
// guards as every other one: only a queued run with pending work.
func TestObserveQueuedRunRoutingRefusesWorkThatIsNoLongerPending(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueQueuedRun(t, repo, "claimed away")
	if _, err := repo.db.ExecContext(ctx, `UPDATE jobs SET status = 'in_progress' WHERE run_id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	before, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}

	_, changed, err := repo.ObserveQueuedRunRouting(ctx, enqueued.RunID, runoutcome.QueueWaitNoCompatibleWorker, time.Now().UnixNano())
	if err != nil {
		t.Fatalf("ObserveQueuedRunRouting: %v", err)
	}
	if changed {
		t.Fatal("the observer wrote onto a run whose job is no longer pending")
	}
	after, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("the observer changed a claimed run: %+v, want %+v", after, before)
	}
}

func TestExpireQueuedRunRefusesWorkThatIsNoLongerPending(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueQueuedRun(t, repo, "already claimed")
	if _, err := repo.db.ExecContext(ctx, `UPDATE jobs SET status = 'in_progress' WHERE run_id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	_, err := repo.ExpireQueuedRun(ctx, ExpireQueuedRunInput{
		RunID:  enqueued.RunID,
		Policy: runoutcome.QueueTimeoutPolicyFail,
		Code:   runoutcome.CodeQueueWaitExpired,
	})
	if !errors.Is(err, ErrQueueWaitNotApplicable) {
		t.Fatalf("ExpireQueuedRun on claimed work = %v, want %v", err, ErrQueueWaitNotApplicable)
	}
	if state, stateErr := repo.GetRunState(ctx, enqueued.RunID); stateErr != nil {
		t.Fatal(stateErr)
	} else if state.Lifecycle != lifecycleQueued {
		t.Fatalf("claimed run = %q, want it untouched", state.Lifecycle)
	}
}

// queue_timeout describes a run that stopped waiting. An observation is about a
// run that is still waiting, so it must not be able to persist one — a queued
// run holding it would render as an ordinary queued card while claiming, to
// anything that reads the column, that the queue had already given up.
func TestObserveQueuedRunRoutingRefusesATerminalOnlyReason(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueQueuedRun(t, repo, "terminal only")
	before, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}

	_, changed, err := repo.ObserveQueuedRunRouting(ctx, enqueued.RunID,
		runoutcome.QueueWaitQueueTimeout, time.Now().UnixNano())
	if !errors.Is(err, runoutcome.ErrUnsupportedQueueOutcome) {
		t.Fatalf("observing a terminal-only reason = %v, want %v", err, runoutcome.ErrUnsupportedQueueOutcome)
	}
	if changed {
		t.Fatal("a refused observation reported a committed transition")
	}
	after, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("a refused observation changed the run: %+v, want %+v", after, before)
	}
}

func TestExpireQueuedRunRefusesAnUnapprovedCode(t *testing.T) {
	repo := New(openTestDB(t))
	enqueued := enqueueQueuedRun(t, repo, "bad code")

	_, err := repo.ExpireQueuedRun(context.Background(), ExpireQueuedRunInput{
		RunID:  enqueued.RunID,
		Policy: runoutcome.QueueTimeoutPolicyFail,
		Code:   "queue_wait_expired_probably",
	})
	if !errors.Is(err, runoutcome.ErrUnsupportedQueueOutcome) {
		t.Fatalf("ExpireQueuedRun with an unapproved code = %v, want %v", err, runoutcome.ErrUnsupportedQueueOutcome)
	}
}

// Reopen parity: the reason a run is waiting, and the bound that ended one, must
// come back on history and not only on the live stream.
func TestHistoryCarriesTheQueueWaitReason(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueQueuedRun(t, repo, "reopen me")
	if _, _, err := repo.ObserveQueuedRunRouting(ctx, enqueued.RunID,
		runoutcome.QueueWaitNoCompatibleWorker, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}

	waiting := queueReasonFromHistory(t, repo, enqueued.SessionID, enqueued.AssistantMessageID)
	if waiting != string(runoutcome.QueueWaitNoCompatibleWorker) {
		t.Fatalf("reopened waiting run queue reason = %q, want %q", waiting, runoutcome.QueueWaitNoCompatibleWorker)
	}

	if _, err := repo.ExpireQueuedRun(ctx, ExpireQueuedRunInput{
		RunID:  enqueued.RunID,
		Policy: runoutcome.QueueTimeoutPolicyFail,
		Code:   runoutcome.CodeQueueNoCompatibleWorker,
	}); err != nil {
		t.Fatalf("ExpireQueuedRun: %v", err)
	}

	ended := queueReasonFromHistory(t, repo, enqueued.SessionID, enqueued.AssistantMessageID)
	if ended != string(runoutcome.QueueWaitNoCompatibleWorker) {
		t.Fatalf("reopened terminal run queue reason = %q, want the bound that ended it", ended)
	}
	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Lifecycle != lifecycleFailed || state.OutcomeReason != string(runoutcome.ReasonExpired) {
		t.Fatalf("expired run = %s/%s, want failed/expired", state.Lifecycle, state.OutcomeReason)
	}
	// The job stops being work: leaving it pending would let a later scan pick
	// the run up again, and would block the session's next turn forever.
	var jobStatus string
	if err := repo.db.QueryRowContext(ctx, `SELECT status FROM jobs WHERE run_id = ?`, enqueued.RunID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus == "pending" {
		t.Fatal("an expired run left its job pending")
	}
}

// The overall bound is the one that ends a run nothing was wrong with, and it
// has to be distinguishable from an approval that expired — both report the
// same public outcome.
func TestQueueTimeoutIsDistinguishableFromAnApprovalExpiry(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueQueuedRun(t, repo, "timed out")

	if _, err := repo.ExpireQueuedRun(ctx, ExpireQueuedRunInput{
		RunID:  enqueued.RunID,
		Policy: runoutcome.QueueTimeoutPolicyFail,
		Code:   runoutcome.CodeQueueWaitExpired,
	}); err != nil {
		t.Fatalf("ExpireQueuedRun: %v", err)
	}
	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.OutcomeReason != string(runoutcome.ReasonExpired) {
		t.Fatalf("outcome = %q, want %q", state.OutcomeReason, runoutcome.ReasonExpired)
	}
	if state.QueueWaitReason != string(runoutcome.QueueWaitQueueTimeout) {
		t.Fatalf("queue reason = %q, want %q", state.QueueWaitReason, runoutcome.QueueWaitQueueTimeout)
	}
}

func TestQueueWaitClockAccountsForOpenAndBankedIntervals(t *testing.T) {
	base := time.Now().UTC().UnixNano()
	open := int64(5 * time.Minute)
	clock := QueueWaitClock{
		WaitedNanos:      int64(2 * time.Minute),
		QueuedSinceNanos: sql.NullInt64{Int64: base, Valid: true},
	}
	if got := clock.TotalWaitedNanos(base + open); got != int64(7*time.Minute) {
		t.Fatalf("total waited = %v, want 7m", time.Duration(got))
	}
	// A clock that went backwards contributes nothing rather than a negative
	// amount, so a corrected system clock cannot un-reach a deadline.
	if got := clock.TotalWaitedNanos(base - open); got != int64(2*time.Minute) {
		t.Fatalf("total waited with a backwards clock = %v, want the banked 2m", time.Duration(got))
	}
	// No open interval at all: a run that is not queued right now banks only
	// what it already spent.
	closed := QueueWaitClock{WaitedNanos: int64(2 * time.Minute)}
	if got := closed.TotalWaitedNanos(base + open); got != int64(2*time.Minute) {
		t.Fatalf("total waited with no open interval = %v, want 2m", time.Duration(got))
	}
	// The no-worker interval is separate and is zero unless one is open.
	if got := closed.UnroutableForNanos(base + open); got != 0 {
		t.Fatalf("unroutable duration with no interval = %v, want 0", time.Duration(got))
	}
	unroutable := QueueWaitClock{UnroutableSinceNanos: sql.NullInt64{Int64: base, Valid: true}}
	if got := unroutable.UnroutableForNanos(base + open); got != open {
		t.Fatalf("unroutable duration = %v, want 5m", time.Duration(got))
	}
}

// The reason describes waiting. A run that stopped waiting — because a worker
// picked it up — must not carry it into whatever ends it later: an abandoned or
// approval-expired run that really did start would otherwise be reported as one
// that never started, which is the same sentence and a different fact.
func TestLeavingTheQueueClearsTheWaitReason(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueQueuedRun(t, repo, "waited then ran")
	if _, _, err := repo.ObserveQueuedRunRouting(ctx, enqueued.RunID,
		runoutcome.QueueWaitNoCompatibleWorker, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}

	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-late")
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	if claimed.RunID != enqueued.RunID {
		t.Fatalf("claimed %q, want the run under test", claimed.RunID)
	}
	running, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if running.QueueWaitReason != string(runoutcome.QueueWaitNone) {
		t.Fatalf("running run queue reason = %q, want %q — it is no longer waiting",
			running.QueueWaitReason, runoutcome.QueueWaitNone)
	}

	// And the ending that follows carries no queue claim of its own.
	if _, err := repo.CancelRunCanonical(ctx, CancelRunInput{
		RunID:                enqueued.RunID,
		AssistantMessageID:   enqueued.AssistantMessageID,
		ExpectedStateVersion: running.StateVersion,
		Cancellation:         runoutcome.AbandonedCancellation(),
	}); err != nil {
		t.Fatalf("CancelRunCanonical: %v", err)
	}
	abandoned, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if abandoned.QueueWaitReason != string(runoutcome.QueueWaitNone) {
		t.Fatalf("abandoned run queue reason = %q, want %q — this run did start",
			abandoned.QueueWaitReason, runoutcome.QueueWaitNone)
	}
}

// A queued run that its client abandons is not a run the queue gave up on, and
// must not borrow the queue bound's explanation either.
func TestAbandoningAQueuedRunDoesNotClaimAQueueBound(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := enqueueQueuedRun(t, repo, "abandoned while waiting")
	if _, _, err := repo.ObserveQueuedRunRouting(ctx, enqueued.RunID,
		runoutcome.QueueWaitNoCompatibleWorker, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	waiting, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.CancelRunCanonical(ctx, CancelRunInput{
		RunID:                enqueued.RunID,
		AssistantMessageID:   enqueued.AssistantMessageID,
		ExpectedStateVersion: waiting.StateVersion,
		Cancellation:         runoutcome.AbandonedCancellation(),
	}); err != nil {
		t.Fatalf("CancelRunCanonical: %v", err)
	}
	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.QueueWaitReason != string(runoutcome.QueueWaitNone) {
		t.Fatalf("client-abandoned run queue reason = %q, want %q — no queue bound ended it",
			state.QueueWaitReason, runoutcome.QueueWaitNone)
	}
}

// queueReasonFromHistory reads the queue reason a reopened conversation would
// render, through the same ListMessages projection the client uses.
func queueReasonFromHistory(t *testing.T, repo *Repository, sessionID string, assistantMessageID string) string {
	t.Helper()
	messages, err := repo.ListMessages(context.Background(), sessionID, 50)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	for _, message := range messages {
		if message.MessageID != assistantMessageID {
			continue
		}
		if message.RunState == nil {
			t.Fatal("the reopened assistant row carried no run state")
		}
		return message.RunState.QueueWaitReason
	}
	t.Fatalf("assistant message %q was not in the reopened history", assistantMessageID)
	return ""
}
