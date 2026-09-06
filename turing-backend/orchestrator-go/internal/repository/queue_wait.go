package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// ErrQueueWaitNotApplicable reports that the run this command names is no
// longer accepted queued work: it was claimed, cancelled, deleted, or already
// terminal between the scan that selected it and the write that would have
// acted on it.
//
// It is a normal outcome, not a failure. The queue sweep reads a page outside
// any transaction and then writes one run at a time, so losing that race is the
// expected way a scan finds out that dispatch won.
var ErrQueueWaitNotApplicable = errors.New("run is no longer queued pending work")

// QueueWaitClock is the durable answer to "how long has this run been waiting,
// and since when has nothing been able to serve it".
//
// WaitedNanos is time already banked from previous queued intervals;
// QueuedSinceNanos starts the current one. UnroutableSinceNanos is null unless
// the orchestrator has actually observed that no live worker satisfies the
// route — it is never inferred from the absence of a dispatch.
type QueueWaitClock struct {
	Reason               string
	WaitedNanos          int64
	QueuedSinceNanos     sql.NullInt64
	UnroutableSinceNanos sql.NullInt64
}

// TotalWaitedNanos is the run's whole queue age at now: everything banked from
// earlier intervals plus the interval currently open.
//
// A run with no open interval contributes only what is banked. A clock that
// reads backwards contributes nothing for the open interval rather than a
// negative amount, so a corrected system clock cannot shorten a deadline that
// had already been reached.
func (c QueueWaitClock) TotalWaitedNanos(nowNanos int64) int64 {
	total := c.WaitedNanos
	if c.QueuedSinceNanos.Valid && nowNanos > c.QueuedSinceNanos.Int64 {
		total += nowNanos - c.QueuedSinceNanos.Int64
	}
	return total
}

// UnroutableForNanos is how long the current no-compatible-worker interval has
// run, or zero when there is no such interval open.
func (c QueueWaitClock) UnroutableForNanos(nowNanos int64) int64 {
	if !c.UnroutableSinceNanos.Valid || nowNanos <= c.UnroutableSinceNanos.Int64 {
		return 0
	}
	return nowNanos - c.UnroutableSinceNanos.Int64
}

// ObserveQueuedRunRouting records what the queue observer just saw about one
// accepted queued run, and returns the transition it committed.
//
// It is the deduplication boundary for TUR-010's public queue truth: a run
// whose stored reason already says what the observer saw is left completely
// alone — no version, no event, no write — so repeated reconciliation over an
// unchanged queue is free. Only a real change commits, and it commits as one
// guarded queued -> queued transition, which is what makes the new truth reach
// both a live stream and a reopened conversation through the same versioned
// snapshot every other lifecycle change uses.
//
// The pending-job guard is inside the transaction on purpose. A scan reads its
// page outside one, so by the time this runs the job may already be claimed;
// writing a "no compatible worker" notice onto a run a worker is executing
// would be worse than writing nothing.
func (r *Repository) ObserveQueuedRunRouting(
	ctx context.Context,
	runID string,
	reason runoutcome.QueueWaitReason,
	unroutableAtNanos int64,
) (RunTransitionResult, bool, error) {
	// Narrowed to the two an observation can legitimately assert rather than to
	// every reason this build knows. queue_timeout is terminal-only, and this
	// writer commits a queued -> queued self-edge, so accepting it here would
	// let a caller persist "the queue gave up on this" onto a run that is still
	// waiting — which the client renders as an ordinary queued card, with
	// nothing to say anything went wrong. Same fail-closed rule ExpireQueuedRun
	// applies to its own code.
	if reason != runoutcome.QueueWaitNone && reason != runoutcome.QueueWaitNoCompatibleWorker {
		return RunTransitionResult{}, false, runoutcome.ErrUnsupportedQueueOutcome
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RunTransitionResult{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	stored, err := queuedPendingWaitReasonTx(ctx, tx, runID)
	if err != nil {
		if errors.Is(err, ErrQueueWaitNotApplicable) {
			return RunTransitionResult{}, false, nil
		}
		return RunTransitionResult{}, false, err
	}
	if stored == string(reason) {
		return RunTransitionResult{}, false, nil
	}
	// The observation timestamp belongs to the observer, not to this write: the
	// no-worker deadline has to run from when the condition was first seen, and
	// a run whose reason is being cleared has no interval open at all.
	extraSet := `queue_unroutable_since_ns = NULL`
	var extraArgs []any
	if reason == runoutcome.QueueWaitNoCompatibleWorker {
		extraSet = `queue_unroutable_since_ns = COALESCE(queue_unroutable_since_ns, ?)`
		extraArgs = []any{unroutableAtNanos}
	}
	result, err := applyRunTransitionTx(ctx, tx, runTransition{
		runID:            runID,
		expectedVersion:  unresolvedStateVersion,
		transactionLocal: true,
		allowedFrom:      []string{lifecycleQueued},
		to:               lifecycleQueued,
		reason:           runoutcome.ReasonNone,
		queueWaitReason:  string(reason),
		rejection:        ErrQueueWaitNotApplicable,
		extraSet:         extraSet,
		extraArgs:        extraArgs,
		// runId only. The queue reason reaches every reader through the typed
		// RunState the transition merges in, which is a closed vocabulary a
		// client can localize; publishing this server's own stored word beside
		// it would be a second, free-form spelling of the same fact for a
		// client to disagree with. The other state_changed writers add no
		// public key either.
		eventPayload: map[string]any{"runId": runID},
	}, nil)
	if err != nil {
		return RunTransitionResult{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return RunTransitionResult{}, false, err
	}
	return result, true, nil
}

// ExpireQueuedRunInput is one bounded outcome for one over-waiting run.
type ExpireQueuedRunInput struct {
	RunID string
	// Policy decides the terminal lifecycle. Code says which bound ran out and
	// is one of runoutcome's two queue codes; anything else is refused rather
	// than persisted.
	Policy runoutcome.QueueTimeoutPolicy
	Code   string
}

// ExpireQueuedRun commits the bounded outcome a queued run reached, or reports
// that it is no longer eligible.
//
// Everything that makes this safe is inside one transaction: the run must still
// be queued, its job must still be pending, and the expectation is resolved
// from the row under the guard rather than from the scan that selected it. A
// claim that won the race therefore makes this a no-op instead of failing a run
// a worker is already executing, a cancelled or deleted run is not queued and
// is left alone, and a terminal run cannot be revived.
func (r *Repository) ExpireQueuedRun(ctx context.Context, input ExpireQueuedRunInput) (RunTransitionResult, error) {
	if !runoutcome.KnownQueueTimeoutPolicy(string(input.Policy)) {
		return RunTransitionResult{}, runoutcome.ErrUnsupportedQueueOutcome
	}
	if input.Code != runoutcome.CodeQueueWaitExpired && input.Code != runoutcome.CodeQueueNoCompatibleWorker {
		return RunTransitionResult{}, runoutcome.ErrUnsupportedQueueOutcome
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RunTransitionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := queuedPendingWaitReasonTx(ctx, tx, input.RunID); err != nil {
		return RunTransitionResult{}, err
	}
	// The queue reason a terminal run carries is asserted by this transition,
	// never inherited from the row. Leaving the queue clears it, so the only
	// way a terminal run says "no assistant became available" is if the bound
	// that ended it said so here. Without that, an EXPIRED run would be
	// indistinguishable from an expired approval — and a run that waited, ran,
	// and was then abandoned by its client would be reported as one that never
	// started.
	terminalReason := runoutcome.QueueWaitNoCompatibleWorker
	if input.Code == runoutcome.CodeQueueWaitExpired {
		terminalReason = runoutcome.QueueWaitQueueTimeout
	}
	var result RunTransitionResult
	switch input.Policy {
	case runoutcome.QueueTimeoutPolicyCancel:
		cancellation, err := runoutcome.QueueTimeoutCancellation(input.Code)
		if err != nil {
			return RunTransitionResult{}, err
		}
		result, err = cancelRunTx(ctx, tx, CancelRunInput{
			RunID:              input.RunID,
			Cancellation:       cancellation,
			resolveVersionInTx: true,
			queueWaitReason:    string(terminalReason),
		})
		if err != nil {
			return RunTransitionResult{}, err
		}
	default:
		failure, err := runoutcome.QueueTimeoutFailure(input.Code)
		if err != nil {
			return RunTransitionResult{}, err
		}
		result, err = failRunTx(ctx, tx, FailRunInput{
			RunID:              input.RunID,
			Failure:            failure,
			allowedFrom:        []string{lifecycleQueued},
			resolveVersionInTx: true,
			queueWaitReason:    string(terminalReason),
		})
		if err != nil {
			return RunTransitionResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return RunTransitionResult{}, err
	}
	return result, nil
}

// queuedPendingWaitReasonTx proves, inside the caller's transaction, that this
// run is still accepted queued work, and returns the queue reason it currently
// holds.
//
// Both halves matter. A run whose status left queued has been claimed,
// cancelled, or terminalized; a run whose job is no longer pending has been
// claimed even if its own row has not caught up within this snapshot. Neither
// is work the queue sweep may still decide anything about.
func queuedPendingWaitReasonTx(ctx context.Context, tx *sql.Tx, runID string) (string, error) {
	var reason string
	err := tx.QueryRowContext(ctx, `
		SELECT runs.queue_wait_reason
		FROM agent_runs AS runs
		WHERE runs.id = ?
		  AND runs.status = 'queued'
		  AND EXISTS (
			SELECT 1
			FROM jobs
			WHERE jobs.run_id = runs.id
			  AND jobs.status = 'pending'
		  )
	`, runID).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrQueueWaitNotApplicable
	}
	if err != nil {
		return "", err
	}
	return reason, nil
}
