package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// drainTransactionBudget bounds one drain batch's p95 against a large
// backlog: it cancels at most MaxQuietDrainBatch jobs, however many wait, and
// holds the database's one connection for that long while attended work
// waits. TestTheDrainReadDoesNotSortTheQueue pins that its read stops at the
// batch.
const drainTransactionBudget = time.Second

// finishQuietDrain runs drain batches until one reports the drain done, and
// returns every batch's cancellations in order.
func finishQuietDrain(t *testing.T, ctx context.Context, repo *Repository) []QuietDrainBatch {
	t.Helper()
	var batches []QuietDrainBatch
	for range 1_000 {
		batch, err := repo.DrainUnattendedQuiet(ctx)
		if err != nil {
			t.Fatal(err)
		}
		batches = append(batches, batch)
		if batch.Done {
			return batches
		}
	}
	t.Fatal("the drain never finished")
	return nil
}

// wantOwnerQuiet asserts runID was cancelled by the owner's quiet switch:
// abandoned, owner_quiet, its job cancelled and its execution free.
func wantOwnerQuiet(t *testing.T, ctx context.Context, repo *Repository, runID string) {
	t.Helper()
	var status, reason, code, jobStatus, executionState string
	var active bool
	if err := repo.db.QueryRowContext(ctx, `
		SELECT r.status, r.outcome_reason, COALESCE(r.cancellation_reason, ''), j.status, r.execution_active, r.execution_state
		FROM agent_runs r JOIN jobs j ON j.run_id = r.id WHERE r.id = ?`, runID).
		Scan(&status, &reason, &code, &jobStatus, &active, &executionState); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || reason != string(runoutcome.ReasonAbandoned) || code != runoutcome.CodeOwnerQuiet ||
		jobStatus != "cancelled" || active || (executionState != "exited" && executionState != "none") {
		t.Fatalf("run %s = %s/%s/%s, job %s, execution active %v %s; want cancelled abandoned owner_quiet with its job cancelled and nothing executing",
			runID, status, reason, code, jobStatus, active, executionState)
	}
}

// TUR-028a: quiet fences unattended work only. While it is on, and while a
// drain is pending after it is turned off, an owner's turn, a delegated task
// and its continuation are claimed as ever; the queued automation run is not.
func TestAttendedWorkIsClaimedWhileQuietAndUnattendedWorkIsNot(t *testing.T) {
	f := newDelegationFixture(t)
	fire := fireAutomation(t, f.ctx, f.repo, "Digest")
	mustSetQuiet(t, f.ctx, f.repo, true, 0)

	child := f.delegate(t, "call_1")
	f.startChild(t, child)
	f.complete(t, f.parent.RunID, "Asked Research.")
	f.complete(t, child.ChildRunID, "Notes.")
	continuation := f.continuationRunID(t)
	if continuation == "" {
		t.Fatal("the join queued no continuation")
	}
	claimed, err := f.repo.ClaimNextCompatibleJobWithLimit(f.ctx, "general_assistant", "team-worker", 0, time.Hour,
		joinWorker(1), func(RoutingRequirements) bool { return true })
	if err != nil || claimed.RunID != continuation {
		t.Fatalf("claim while quiet = %+v, %v; want the continuation %s", claimed, err, continuation)
	}
	f.complete(t, continuation, "Here is what Research found.")
	turn := enqueueLocalTurn(t, f.ctx, f.repo, "Owner")
	if got := claimNext(t, f.ctx, f.repo, 0); got.RunID != turn.RunID {
		t.Fatalf("claim while quiet = %q, want the owner's turn %q", got.RunID, turn.RunID)
	}
	if got := claimNext(t, f.ctx, f.repo, 0); got.RunID != "" {
		t.Fatalf("claim while quiet = %q, want nothing: the automation run %q is fenced", got.RunID, fire.RunID)
	}

	mustSetQuiet(t, f.ctx, f.repo, false, 1)
	later := enqueueLocalTurn(t, f.ctx, f.repo, "Owner again")
	if got := claimNext(t, f.ctx, f.repo, 0); got.RunID != later.RunID {
		t.Fatalf("claim while draining = %q, want the owner's turn %q", got.RunID, later.RunID)
	}
	if got := claimNext(t, f.ctx, f.repo, 0); got.RunID != "" {
		t.Fatalf("claim while draining = %q, want nothing: quiet turned off starts no queued unattended run", got.RunID)
	}
	finishQuietDrain(t, f.ctx, f.repo)
	wantOwnerQuiet(t, f.ctx, f.repo, fire.RunID)
}

// With 10,000 unattended jobs queued, turning quiet on is one bounded write
// that touches no job; each drain transaction then cancels at most 100,
// within its budget; an owner's turn sent mid-drain is claimed before the
// drain ends; and every queued job ends owner_quiet. Turned off at once after
// on, quiet starts none of them.
func TestTheDrainCancelsALargeUnattendedBacklogInBoundedBatches(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	const backlog = 10_000
	seedUnattendedBacklog(t, ctx, repo, backlog)
	countQueued := func() int {
		t.Helper()
		var queued int
		if err := repo.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM jobs WHERE status = 'pending' AND unattended = 1`).Scan(&queued); err != nil {
			t.Fatal(err)
		}
		return queued
	}

	started := time.Now()
	mustSetQuiet(t, ctx, repo, true, 0)
	t.Logf("turning quiet on with %d queued unattended jobs took %s", backlog, time.Since(started))
	if got := countQueued(); got != backlog {
		t.Fatalf("queued unattended jobs after turning quiet on = %d, want all %d: the switch's own write touches none", got, backlog)
	}
	mustSetQuiet(t, ctx, repo, false, 1)
	if got := claimNext(t, ctx, repo, 0); got.RunID != "" {
		t.Fatalf("claim after quiet turned off at once = %q, want nothing before the drain ends", got.RunID)
	}

	var samples []time.Duration
	var turn EnqueueUserMessageResult
	cancelled := 0
	for batchIndex := 0; ; batchIndex++ {
		started := time.Now()
		batch, err := repo.DrainUnattendedQuiet(ctx)
		elapsed := time.Since(started)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Cancelled) > MaxQuietDrainBatch {
			t.Fatalf("batch %d cancelled %d, want at most %d", batchIndex, len(batch.Cancelled), MaxQuietDrainBatch)
		}
		if len(batch.Cancelled) > 0 {
			samples = append(samples, elapsed)
		}
		cancelled += len(batch.Cancelled)
		if batchIndex == 10 {
			turn = enqueueLocalTurn(t, ctx, repo, "Owner")
			if got := claimNext(t, ctx, repo, 1); got.RunID != turn.RunID {
				t.Fatalf("claim mid-drain = %q, want the owner's turn %q", got.RunID, turn.RunID)
			}
			if remaining := countQueued(); remaining == 0 {
				t.Fatal("the drain finished before the owner's turn was claimed")
			}
		}
		if batch.Done {
			break
		}
	}
	if cancelled != backlog || countQueued() != 0 {
		t.Fatalf("drain cancelled %d, %d still queued; want all %d cancelled", cancelled, countQueued(), backlog)
	}
	var quietCancelled int
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agent_runs
		WHERE id LIKE 'run_backlog_%' AND status = 'cancelled' AND outcome_reason = ? AND cancellation_reason = ?`,
		string(runoutcome.ReasonAbandoned), runoutcome.CodeOwnerQuiet).Scan(&quietCancelled); err != nil {
		t.Fatal(err)
	}
	if quietCancelled != backlog {
		t.Fatalf("runs cancelled owner_quiet = %d, want %d", quietCancelled, backlog)
	}
	p50, p95 := percentile(samples, 50), percentile(samples, 95)
	t.Logf("drain transaction of %d jobs against a %d-job backlog: p50 %s, p95 %s", MaxQuietDrainBatch, backlog, p50, p95)
	if p95 > drainTransactionBudget {
		t.Fatalf("drain batch p95 = %s, want under %s", p95, drainTransactionBudget)
	}
	wantFenced(t, ctx, repo, false)
	if got := claimNext(t, ctx, repo, 0); got.RunID != "" {
		t.Fatalf("claim after the drain = %q, want nothing: no unattended run from before quiet starts", got.RunID)
	}
}

// The drain's read walks pending jobs and stops at its batch: it never sorts
// the queue, so its cost follows the batch, not the backlog.
func TestTheDrainReadDoesNotSortTheQueue(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	plan := explainQueryPlan(t, ctx, repo, quietDrainQuery, MaxQuietDrainBatch)
	if strings.Contains(plan, "TEMP B-TREE") || !strings.Contains(plan, "SEARCH j USING INDEX") {
		t.Fatalf("drain plan\n%s\nwant an index search of pending jobs with no sort", plan)
	}
}

// A drain key holding anything fences unattended work until a drain batch
// finds nothing left and clears it, so a value written by hand, or one this
// build cannot read, is finished like any other drain.
func TestAnUnreadableDrainIsFinishedLikeAnyOther(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	fire := fireAutomation(t, ctx, repo, "Digest")
	if _, err := repo.db.ExecContext(ctx,
		`INSERT INTO settings (key, value_json, updated_at) VALUES ('unattended_quiet_drain', '{', '2026-11-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	wantFenced(t, ctx, repo, true)
	batches := finishQuietDrain(t, ctx, repo)
	if len(batches) != 2 || len(batches[0].Cancelled) != 1 || len(batches[1].Cancelled) != 0 || !batches[1].Done {
		t.Fatalf("drain batches = %+v, want the queued run cancelled and then the drain cleared", batches)
	}
	wantOwnerQuiet(t, ctx, repo, fire.RunID)
	wantFenced(t, ctx, repo, false)
}

// Nothing is held, so no clock moves: an attended run queued across quiet
// and its drain keeps the queue clock its wait bound is computed from.
func TestQuietLeavesAnAttendedRunsQueueClockAlone(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	turn := enqueueLocalTurn(t, ctx, repo, "Owner")
	fireAutomation(t, ctx, repo, "Digest")
	clock := func() QueueWaitClock {
		t.Helper()
		page, _, err := repo.ListPendingRoutingWorkPage(ctx, PendingRoutingCursor{}, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page {
			if item.RunID == turn.RunID {
				return item.Clock
			}
		}
		t.Fatalf("the owner's queued turn %s is not pending", turn.RunID)
		return QueueWaitClock{}
	}
	before := clock()
	mustSetQuiet(t, ctx, repo, true, 0)
	finishQuietDrain(t, ctx, repo)
	mustSetQuiet(t, ctx, repo, false, 1)
	if after := clock(); after != before {
		t.Fatalf("queue clock of the attended run = %+v after quiet, want %+v unchanged", after, before)
	}
}

// SQLite serializes a due claim with the switch: either the switch commits
// first and the claim fires nothing, or the claim commits first and the drain
// cancels its run. Either way no unattended run is left to start.
func TestADueClaimRacingTheQuietSwitchFiresNothingOrIsDrained(t *testing.T) {
	fired, refused := 0, 0
	for attempt := range 20 {
		repo, ctx := newTitleTestRepo(t)
		automation := mustCreateDueAutomation(t, repo, ctx, fmt.Sprintf("Digest %d", attempt), everyFiveMinutes())
		due := mustParse(t, automation.NextDueAt)
		var wait sync.WaitGroup
		var fire AutomationFire
		var found bool
		var claimErr, quietErr error
		wait.Add(2)
		go func() {
			defer wait.Done()
			fire, found, claimErr = repo.ClaimDueAutomation(ctx, due, attendedTestDefaults)
		}()
		go func() {
			defer wait.Done()
			_, _, quietErr = repo.SetUnattendedQuiet(ctx, true, 0)
		}()
		wait.Wait()
		if claimErr != nil || quietErr != nil {
			t.Fatalf("claim %v, quiet %v", claimErr, quietErr)
		}
		finishQuietDrain(t, ctx, repo)
		if found {
			fired++
			wantOwnerQuiet(t, ctx, repo, fire.RunID)
		} else {
			refused++
			reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
			if err != nil || reloaded.NextDueAt != automation.NextDueAt || reloaded.LastRunID != "" {
				t.Fatalf("automation after a refused claim = %+v, %v; want it still due, never fired", reloaded, err)
			}
		}
		if got := claimNext(t, ctx, repo, 0); got.RunID != "" {
			t.Fatalf("claim after the race = %q, want nothing", got.RunID)
		}
	}
	t.Logf("of 20 races, the claim won %d and the switch %d", fired, refused)
}
