package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/attention"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// quietRequeue is one path that would send a run back to the queue. It runs
// the path against assignment and returns the events its caller publishes
// live: nil for the aborts, whose callers publish nothing.
type quietRequeue struct {
	name string
	// send is how far the assignment got: "pending_send" (claimed), "sending"
	// (a send begun) or "delivered".
	send    string
	requeue func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment) []Event
}

func reconciliationEvents(t *testing.T, what string, result AssignmentReconciliation, err error) []Event {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if result.Requeued || !result.Cleared {
		t.Fatalf("%s = %+v, want cleared and not requeued", what, result)
	}
	return result.Events
}

func retryEvents(t *testing.T, decision RetryDecision, err error) []Event {
	t.Helper()
	if err != nil {
		t.Fatalf("retryable failure: %v", err)
	}
	if decision.Requeued {
		t.Fatalf("retryable failure = %+v, want not requeued", decision)
	}
	return decision.Events
}

// quietRequeues are every path TUR-028a turns from a requeue into a quiet
// cancellation: the lost-lease requeue that stale recovery reaches three ways,
// a retryable failure with and without a proven owner, both aborts, and the
// release of a claim a disconnected worker never received.
func quietRequeues() []quietRequeue {
	return []quietRequeue{
		{name: "a lease lost, found by a heartbeat", send: "delivered",
			requeue: func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment) []Event {
				result, err := repo.ReconcileHeartbeatAssignment(ctx, assignment, 3)
				return reconciliationEvents(t, "heartbeat reconcile", result, err)
			}},
		{name: "a lease lost, found by startup recovery", send: "delivered",
			requeue: func(t *testing.T, ctx context.Context, repo *Repository, _ Assignment) []Event {
				events, err := repo.RecoverAllActiveAssignmentsWithLimit(ctx, 3)
				if err != nil {
					t.Fatalf("startup recovery: %v", err)
				}
				return events
			}},
		{name: "a lease lost, found at the reaper's cutoff", send: "delivered",
			requeue: func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment) []Event {
				result, err := repo.RecoverAssignmentAtCutoffWithLimit(ctx, assignment, time.Now().Add(2*time.Hour), 3)
				return reconciliationEvents(t, "cutoff recovery", result, err)
			}},
		{name: "a retryable failure its owner reported", send: "delivered",
			requeue: func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment) []Event {
				decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
					RunID: assignment.RunID, ExpectedStateVersion: currentVersion(t, repo, assignment.RunID),
					WorkerID: assignment.WorkerID, AssignmentAttemptID: assignment.AttemptID,
					Failure: dispatchCondition("worker_busy"), MaxAttempts: 3,
				})
				return retryEvents(t, decision, err)
			}},
		{name: "a retryable failure nobody can attribute", send: "delivered",
			requeue: func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment) []Event {
				decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
					RunID: assignment.RunID, Failure: dispatchCondition("worker_busy"), MaxAttempts: 3,
				})
				return retryEvents(t, decision, err)
			}},
		{name: "a claim released before delivery", send: "pending_send",
			requeue: func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment) []Event {
				if err := repo.AbortPendingAssignment(ctx, assignment); err != nil {
					t.Fatalf("AbortPendingAssignment: %v", err)
				}
				return nil
			}},
		{name: "a send proven never started", send: "sending",
			requeue: func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment) []Event {
				if err := repo.AbortUnsentAssignment(ctx, assignment); err != nil {
					t.Fatalf("AbortUnsentAssignment: %v", err)
				}
				return nil
			}},
		{name: "a worker disconnected before the send", send: "pending_send",
			requeue: func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment) []Event {
				result, err := repo.ReconcileAssignmentWithLimit(ctx, assignment, 3)
				return reconciliationEvents(t, "disconnect reconcile", result, err)
			}},
	}
}

// claimInto claims the next job for worker-1 and takes its assignment as far
// as send says.
func claimInto(t *testing.T, ctx context.Context, repo *Repository, send string) (Job, Assignment) {
	t.Helper()
	claimed := claimNext(t, ctx, repo, 1)
	if claimed.RunID == "" {
		t.Fatal("nothing was claimed")
	}
	assignment := Assignment{JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-1", AttemptID: claimed.AssignmentAttemptID}
	if send == "sending" || send == "delivered" {
		if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
			t.Fatal(err)
		}
	}
	if send == "delivered" {
		if err := repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
			t.Fatal(err)
		}
	}
	return claimed, assignment
}

// wantQuietReleased asserts the quiet branch's whole write: the run cancelled
// abandoned with owner_quiet, its job cancelled, and the execution released
// as a failure releases it, so its slot is free at once.
func wantQuietReleased(t *testing.T, ctx context.Context, repo *Repository, runID string) {
	t.Helper()
	var status, reason, code, jobStatus, executionState string
	var active, acknowledged, leased bool
	if err := repo.db.QueryRowContext(ctx, `
		SELECT r.status, r.outcome_reason, COALESCE(r.cancellation_reason, ''), j.status, r.execution_active,
			r.execution_state, r.execution_exit_acknowledged_at IS NOT NULL,
			r.execution_lease_expires_at IS NOT NULL OR r.execution_lease_expires_at_ns IS NOT NULL
		FROM agent_runs r JOIN jobs j ON j.run_id = r.id WHERE r.id = ?`, runID).
		Scan(&status, &reason, &code, &jobStatus, &active, &executionState, &acknowledged, &leased); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || reason != string(runoutcome.ReasonAbandoned) || code != runoutcome.CodeOwnerQuiet || jobStatus != "cancelled" {
		t.Fatalf("run %s = %s/%s/%s with job %s, want cancelled, abandoned, owner_quiet, job cancelled", runID, status, reason, code, jobStatus)
	}
	if active || executionState != "exited" || !acknowledged || leased {
		t.Fatalf("run %s execution = active %v, %s, acknowledged %v, leased %v; want released: inactive, exited, acknowledged, no lease",
			runID, active, executionState, acknowledged, leased)
	}
}

func countRunEventsOfType(t *testing.T, ctx context.Context, repo *Repository, runID, eventType string) int {
	t.Helper()
	var count int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE run_id = ? AND type = ?`, runID, eventType).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func wantNoAutomationAttention(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	page, err := repo.ListAttention(ctx, AttentionQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range page.Rows {
		if row.Kind == attention.KindAutomation {
			t.Fatalf("attention row %+v, want none: a quiet cancellation needs nobody", row)
		}
	}
}

// TUR-028a: while quiet is on, and after its drain has finished, no path
// sends an unattended run back to the queue. Each one cancels it owner_quiet
// instead, in the transaction that would have requeued it, with the
// execution release that requeue would have written: an attended turn is
// claimed into its slot at once, with the reaper nowhere involved. The live
// paths return the cancellation for their caller to publish, with no retry
// notice; nothing raises an attention row; and turning quiet off starts none
// of them.
func TestEveryRequeueOfAnUnattendedRunIsAQuietCancellation(t *testing.T) {
	for _, path := range quietRequeues() {
		t.Run(path.name, func(t *testing.T) {
			repo, ctx := newTitleTestRepo(t)
			fireAutomation(t, ctx, repo, "Digest")
			claimed, assignment := claimInto(t, ctx, repo, path.send)
			mustSetQuiet(t, ctx, repo, true, 0)
			finishQuietDrain(t, ctx, repo)
			wantFenced(t, ctx, repo, true)

			events := path.requeue(t, ctx, repo, assignment)
			wantQuietReleased(t, ctx, repo, claimed.RunID)
			if path.send == "delivered" || path.name == "a worker disconnected before the send" {
				cancelled := 0
				for _, event := range events {
					if event.Type == "agent.run.cancelled" && event.RunID.String == claimed.RunID {
						cancelled++
					}
				}
				if cancelled != 1 {
					t.Fatalf("events for the caller to publish = %+v, want the one cancellation", events)
				}
			}
			if got := countRunEventsOfType(t, ctx, repo, claimed.RunID, "agent.run.cancelled"); got != 1 {
				t.Fatalf("durable cancellations = %d, want 1, seen on replay", got)
			}
			if got := countRunEventsOfType(t, ctx, repo, claimed.RunID, "agent.run.step"); got != 0 {
				t.Fatalf("retry notices = %d, want none: the run was not requeued", got)
			}
			wantNoAutomationAttention(t, ctx, repo)

			turn := enqueueLocalTurn(t, ctx, repo, "Owner")
			attended := claimNext(t, ctx, repo, 1)
			if attended.RunID != turn.RunID {
				t.Fatalf("claim at a limit of one = %q, want the owner's turn %q at once: the cancelled run's slot is free", attended.RunID, turn.RunID)
			}
			completeClaimed(t, repo, attended)
			mustSetQuiet(t, ctx, repo, false, 1)
			if got := claimNext(t, ctx, repo, 0); got.RunID != "" {
				t.Fatalf("claim after quiet turned off = %q, want nothing: the cancelled run never starts", got.RunID)
			}
		})
	}
}

// A disconnect from a delivered assignment proves nothing about its executor,
// so during quiet it is fenced as ever and keeps its containment, holding its
// slot; only its later stale recovery, which would have requeued it, cancels
// it, with the execution release.
func TestADeliveredDisconnectDuringQuietStaysContainedUntilItsStaleRecovery(t *testing.T) {
	for _, path := range quietRequeues()[:3] {
		t.Run(path.name, func(t *testing.T) {
			repo, ctx := newTitleTestRepo(t)
			fireAutomation(t, ctx, repo, "Digest")
			claimed, assignment := claimInto(t, ctx, repo, "delivered")
			mustSetQuiet(t, ctx, repo, true, 0)
			finishQuietDrain(t, ctx, repo)

			disconnect, err := repo.ReconcileAssignmentWithLimit(ctx, assignment, 3)
			if err != nil || !disconnect.Fenced || disconnect.Requeued || disconnect.Cleared {
				t.Fatalf("disconnect = %+v, %v; want fenced only", disconnect, err)
			}
			run, err := repo.GetRun(ctx, claimed.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != lifecycleRecovering || run.ExecutionState != "uncertain" || !run.ExecutionActive {
				t.Fatalf("run after the disconnect = %s/%s/active %v, want recovering, uncertain, active", run.Status, run.ExecutionState, run.ExecutionActive)
			}
			turn := enqueueLocalTurn(t, ctx, repo, "Owner")
			if got := claimNext(t, ctx, repo, 1); got.RunID != "" {
				t.Fatalf("claim at a limit of one = %q, want nothing in the contained run's slot", got.RunID)
			}

			path.requeue(t, ctx, repo, assignment)
			wantQuietReleased(t, ctx, repo, claimed.RunID)
			if got := claimNext(t, ctx, repo, 1); got.RunID != turn.RunID {
				t.Fatalf("claim after the stale recovery = %q, want the owner's turn %q", got.RunID, turn.RunID)
			}
		})
	}
}

// Quiet changes only what happens to unattended work. An owner's turn whose
// lease is lost during quiet is requeued, with its retry notice and its next
// attempt, and one released before delivery goes straight back to the queue
// with its attempt unchanged and no recovering phase, as before.
func TestAnAttendedRunIsRequeuedAsEverWhileQuiet(t *testing.T) {
	for _, path := range []struct {
		name    string
		send    string
		requeue func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment)
		attempt int
		unsent  bool
	}{
		{name: "a lease lost", send: "delivered", attempt: 2,
			requeue: func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment) {
				result, err := repo.ReconcileHeartbeatAssignment(ctx, assignment, 3)
				if err != nil || !result.Requeued {
					t.Fatalf("heartbeat reconcile = %+v, %v; want requeued", result, err)
				}
			}},
		{name: "a claim released before delivery", send: "pending_send", attempt: 1, unsent: true,
			requeue: func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment) {
				if err := repo.AbortPendingAssignment(ctx, assignment); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "a send proven never started", send: "sending", attempt: 1, unsent: true,
			requeue: func(t *testing.T, ctx context.Context, repo *Repository, assignment Assignment) {
				if err := repo.AbortUnsentAssignment(ctx, assignment); err != nil {
					t.Fatal(err)
				}
			}},
	} {
		t.Run(path.name, func(t *testing.T) {
			repo, ctx := newTitleTestRepo(t)
			turn := enqueueLocalTurn(t, ctx, repo, "Owner")
			claimed, assignment := claimInto(t, ctx, repo, path.send)
			mustSetQuiet(t, ctx, repo, true, 0)
			before := runStateProjections(t, repo, turn.RunID)

			path.requeue(t, ctx, repo, assignment)
			if got := readJobProgress(t, repo, claimed.JobID); got != (jobProgress{status: "pending", attempt: path.attempt}) {
				t.Fatalf("job after the requeue = %+v, want pending at attempt %d", got, path.attempt)
			}
			if path.unsent {
				onlyQueuedProjection(t, repo, turn.RunID, before, claimed.ExpectedStateVersion+1)
				assertReleasedToQueue(t, repo, turn.RunID, claimed.ExpectedStateVersion+1)
			} else if got := countRunEventsOfType(t, ctx, repo, turn.RunID, "agent.run.step"); got != 1 {
				t.Fatalf("retry notices = %d, want the requeue's one", got)
			}
			if again := claimNext(t, ctx, repo, 1); again.RunID != turn.RunID {
				t.Fatalf("claim while quiet = %q, want the requeued owner's turn %q", again.RunID, turn.RunID)
			}
		})
	}
}

// The quiet branch keeps the helper's job guard: a release naming a job the
// run no longer holds in progress is refused as fenced and cancels nothing.
func TestAQuietReleaseOfAJobNoLongerHeldIsFencedAndCancelsNothing(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	fireAutomation(t, ctx, repo, "Digest")
	claimed, assignment := claimInto(t, ctx, repo, "pending_send")
	mustSetQuiet(t, ctx, repo, true, 0)
	stale := assignment
	stale.JobID = "job_stale"
	if err := repo.AbortPendingAssignment(ctx, stale); !errors.Is(err, ErrAssignmentFenced) {
		t.Fatalf("abort naming a job the run does not hold = %v, want fenced", err)
	}
	if run, err := repo.GetRun(ctx, claimed.RunID); err != nil || run.Status != lifecycleRunning || !run.ExecutionActive {
		t.Fatalf("run after a fenced abort = %+v, %v; want it untouched", run, err)
	}
}

// requeueAssignment names the job it releases. An abort naming none is
// refused as fenced, as the statement it replaced refused it, rather than
// releasing whatever job the run holds.
func TestAnAbortNamingNoJobIsFenced(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	enqueueLocalTurn(t, ctx, repo, "Owner")
	claimed, assignment := claimInto(t, ctx, repo, "pending_send")
	assignment.JobID = ""
	if err := repo.AbortPendingAssignment(ctx, assignment); !errors.Is(err, ErrAssignmentFenced) {
		t.Fatalf("abort naming no job = %v, want fenced", err)
	}
	if got := readJobProgress(t, repo, claimed.JobID); got != (jobProgress{status: "in_progress", attempt: 1}) {
		t.Fatalf("job after a refused abort = %+v, want it untouched", got)
	}
}
