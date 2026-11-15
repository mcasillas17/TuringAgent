package repository

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// claimAny claims the next job a team-protocol worker serving the fixture's
// children can take, or none.
func (f delegationFixture) claimAny(t *testing.T) Job {
	t.Helper()
	claimed, err := f.repo.ClaimNextCompatibleJobWithLimit(f.ctx, "general_assistant", "team-worker", 0, time.Hour,
		egressWorker(), func(RoutingRequirements) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	return claimed
}

// revoke changes Research's setting row, the claim fence, directly.
func (f delegationFixture) setResearch(t *testing.T, enabled bool, grantedRevision string) {
	t.Helper()
	f.exec(t, `UPDATE agent_profile_settings SET enabled = ?, granted_revision = ? WHERE profile_id = 'research'`,
		boolToInt(enabled), grantedRevision)
}

// wantRevoked asserts the task was withdrawn as the orchestrator's doing,
// never run: cancelled, abandoned, profile_grant_revoked, its job cancelled
// and its execution never active.
func (f delegationFixture) wantRevoked(t *testing.T, delegation Delegation) {
	t.Helper()
	var status, reason, code, jobStatus, executionState string
	var executionActive int
	if err := f.repo.db.QueryRowContext(f.ctx, `
		SELECT run.status, run.outcome_reason, COALESCE(run.cancellation_reason, ''), job.status,
			run.execution_active, run.execution_state
		FROM agent_runs run JOIN jobs job ON job.run_id = run.id WHERE run.id = ?`, delegation.ChildRunID).
		Scan(&status, &reason, &code, &jobStatus, &executionActive, &executionState); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || reason != string(runoutcome.ReasonAbandoned) || code != runoutcome.CodeProfileGrantRevoked ||
		jobStatus != "cancelled" || executionActive != 0 {
		t.Fatalf("task %s = %s/%s/%s, job %s, execution %d/%s; want cancelled, abandoned, profile_grant_revoked, never executing",
			delegation.ChildRunID, status, reason, code, jobStatus, executionActive, executionState)
	}
}

func (f delegationFixture) runStatus(t *testing.T, runID string) string {
	t.Helper()
	var status string
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT status FROM agent_runs WHERE id = ?`, runID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

// The setting row is the claim fence: a task is claimed only while its
// specialist is enabled and granted at the revision the task was created
// under. Anything else leaves it queued, and the work behind it is claimed.
func TestATaskIsClaimedOnlyWhileItsSpecialistHoldsItsGrant(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(t *testing.T, f delegationFixture)
		claimed bool
	}{
		{name: "unchanged", change: func(*testing.T, delegationFixture) {}, claimed: true},
		{name: "disabled", change: func(t *testing.T, f delegationFixture) { f.setResearch(t, false, "rev-1") }},
		{name: "re-granted at another revision", change: func(t *testing.T, f delegationFixture) { f.setResearch(t, true, "rev-2") }},
		{name: "setting gone", change: func(t *testing.T, f delegationFixture) {
			f.exec(t, `DELETE FROM agent_profile_settings WHERE profile_id = 'research'`)
		}},
		{name: "re-enabled at its own revision", change: func(t *testing.T, f delegationFixture) {
			f.setResearch(t, false, "rev-1")
			f.setResearch(t, true, "rev-1")
		}, claimed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDelegationFixture(t)
			delegation := f.delegate(t, "call_1")
			tc.change(t, f)

			claimed := f.claimAny(t)
			if !tc.claimed {
				if claimed.JobID != "" {
					t.Fatalf("claim = %+v; want the task skipped", claimed)
				}
				if status := f.runStatus(t, delegation.ChildRunID); status != "queued" {
					t.Fatalf("task = %s, want it left queued for a batch", status)
				}
				return
			}
			if claimed.RunID != delegation.ChildRunID ||
				claimed.DelegationProfileID != "research" || claimed.DelegationProfileRevision != "rev-1" {
				t.Fatalf("claim = %+v; want the task, naming its specialist at rev-1", claimed)
			}
		})
	}
}

// A revoked task does not hold up the queue: work queued behind it is
// claimed, and a run that is not a task carries no specialist.
func TestARevokedTaskDoesNotBlockTheWorkBehindIt(t *testing.T) {
	f := newDelegationFixture(t)
	f.delegate(t, "call_1")
	f.setResearch(t, false, "rev-1")
	session, err := f.repo.CreateSession(f.ctx, "Later")
	if err != nil {
		t.Fatal(err)
	}
	later, err := f.repo.EnqueueUserMessage(f.ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed := f.claimAny(t)
	if claimed.RunID != later.RunID || claimed.DelegationProfileID != "" || claimed.DelegationProfileRevision != "" {
		t.Fatalf("claim = %+v; want the later turn, with no specialist", claimed)
	}
}

// A batch cancels the queued tasks, consent-held ones included, whose
// specialist no longer holds their grant, and only those. A running task is
// left to finish; the join then reports each withdrawn task once.
func TestARevocationBatchWithdrawsQueuedAndConsentHeldTasks(t *testing.T) {
	f := newDelegationFixture(t)
	running := f.delegate(t, "call_1")
	f.claimChild(t, running)
	queued := f.delegate(t, "call_2")
	held := f.delegateNeedingConsent(t, "call_3", false)
	f.complete(t, f.parent.RunID, "Asked Research.")
	f.setResearch(t, false, "rev-1")

	batch, err := f.repo.CancelRevokedDelegations(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(batch.Cancelled, []string{queued.ChildRunID, held.ChildRunID}) {
		t.Fatalf("cancelled = %v, want the queued and the consent-held task in order", batch.Cancelled)
	}
	f.wantRevoked(t, queued)
	f.wantRevoked(t, held)
	if status := f.runStatus(t, running.ChildRunID); status != "running" {
		t.Fatalf("running task = %s, want it left running", status)
	}
	updates := decodeUpdates(t, batch.Events)
	if len(updates) != 2 || updates[0].State != "cancelled" || updates[1].State != "cancelled" ||
		!strings.Contains(updates[0].Summary, "was not run because its profile was disabled or changed") {
		t.Fatalf("updates = %+v; want one cancelled card per task, saying why", updates)
	}
	f.wantNoJoin(t)
	if again, err := f.repo.CancelRevokedDelegations(f.ctx); err != nil || len(again.Cancelled) != 0 {
		t.Fatalf("second batch = %+v, %v; want nothing left", again, err)
	}

	f.complete(t, running.ChildRunID, "Found the notes.")
	joins := f.joinMessages(t)
	if len(joins) != 1 || strings.Count(joins[0].Content, "was not run because its profile was disabled or changed") != 2 {
		t.Fatalf("joins = %+v; want one join reporting both withdrawn tasks", joins)
	}
}

// The batch that withdraws a parent's last outstanding task joins it, and
// says so.
func TestARevocationBatchThatFinishesATurnJoinsIt(t *testing.T) {
	f := newDelegationFixture(t)
	f.delegate(t, "call_1")
	f.complete(t, f.parent.RunID, "Asked Research.")
	f.setResearch(t, true, "rev-2")

	batch, err := f.repo.CancelRevokedDelegations(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	continuation := f.continuationRunID(t)
	if continuation == "" || !slices.Equal(batch.ContinuationRunIDs, []string{continuation}) {
		t.Fatalf("continuations = %v, want %q", batch.ContinuationRunIDs, continuation)
	}
	if !slices.Contains(eventTypes(batch.Events), "delegation.finished") {
		t.Fatalf("events = %v; want the join's", eventTypes(batch.Events))
	}
	if joins := f.joinMessages(t); len(joins) != 1 {
		t.Fatalf("joins = %+v, want one", joins)
	}
}

// One transaction never withdraws more than a batch, so revoking a profile
// with a long backlog never holds the one connection for long. The next call
// picks up where the last left off, with no marker: the fence predicate is
// the cursor.
func TestARevocationBatchWithdrawsAtMostOneHundredTasks(t *testing.T) {
	f := newDelegationFixture(t)
	for index := range 150 {
		input := f.input(fmt.Sprintf("call_%d", index))
		input.MaxPerRun = 200
		if _, err := f.repo.CreateDelegation(f.ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	f.setResearch(t, false, "rev-1")
	for _, want := range []int{MaxRevokedDelegationBatch, 50, 0} {
		batch, err := f.repo.CancelRevokedDelegations(f.ctx)
		if err != nil || len(batch.Cancelled) != want {
			t.Fatalf("batch = %d, %v; want %d", len(batch.Cancelled), err, want)
		}
	}
	if left := f.count(t, `SELECT COUNT(*) FROM agent_runs WHERE status = 'queued'`); left != 0 {
		t.Fatalf("queued = %d, want none left", left)
	}
}

// A task re-enabled before a batch reaches it qualifies again and is left
// runnable; a re-grant at another revision never requalifies it.
func TestOnlyAReEnableAtItsOwnRevisionKeepsATaskRunnable(t *testing.T) {
	t.Run("re-enabled", func(t *testing.T) {
		f := newDelegationFixture(t)
		delegation := f.delegate(t, "call_1")
		f.setResearch(t, false, "rev-1")
		f.setResearch(t, true, "rev-1")
		if batch, err := f.repo.CancelRevokedDelegations(f.ctx); err != nil || len(batch.Cancelled) != 0 {
			t.Fatalf("batch = %+v, %v; want nothing withdrawn", batch, err)
		}
		if claimed := f.claimAny(t); claimed.RunID != delegation.ChildRunID {
			t.Fatalf("claim = %+v; want the task", claimed)
		}
	})
	t.Run("re-granted at another revision", func(t *testing.T) {
		f := newDelegationFixture(t)
		delegation := f.delegate(t, "call_1")
		f.setResearch(t, false, "rev-1")
		f.setResearch(t, true, "rev-2")
		if claimed := f.claimAny(t); claimed.JobID != "" {
			t.Fatalf("claim = %+v; want the task skipped", claimed)
		}
		batch, err := f.repo.CancelRevokedDelegations(f.ctx)
		if err != nil || !slices.Equal(batch.Cancelled, []string{delegation.ChildRunID}) {
			t.Fatalf("batch = %+v, %v; want the task withdrawn", batch, err)
		}
		f.wantRevoked(t, delegation)
	})
}

// The batch starts from the queued runs, so it costs nothing per delegation
// a conversation ever made.
func TestTheRevocationBatchStartsFromQueuedRuns(t *testing.T) {
	f := newDelegationFixture(t)
	rows, err := f.repo.db.QueryContext(f.ctx, `EXPLAIN QUERY PLAN `+revokedDelegationsQuery, MaxRevokedDelegationBatch)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "idx_runs_status") || strings.Contains(joined, "SCAN d") {
		t.Fatalf("plan:\n%s\nwant a seek on idx_runs_status and no scan of delegations", joined)
	}
}

// A task the dispatcher claimed and then found its specialist no longer
// active at its revision is withdrawn before any worker is sent it: the
// claim's execution is released with it, since nothing ever ran.
func TestAnUnsentTaskIsWithdrawnWithItsClaim(t *testing.T) {
	f := newDelegationFixture(t)
	delegation := f.delegate(t, "call_1")
	f.complete(t, f.parent.RunID, "Asked Research.")
	claimed := f.claimChild(t, delegation)
	assignment := Assignment{JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "team-worker", AttemptID: claimed.AssignmentAttemptID}

	result, err := f.repo.CancelUnsentRevokedDelegation(f.ctx, assignment)
	if err != nil {
		t.Fatal(err)
	}
	f.wantRevoked(t, delegation)
	if result.ContinuationRunID == "" || result.ContinuationRunID != f.continuationRunID(t) {
		t.Fatalf("continuation = %q, want the join's", result.ContinuationRunID)
	}
	if _, err := f.repo.CancelUnsentRevokedDelegation(f.ctx, assignment); !errors.Is(err, ErrAssignmentFenced) {
		t.Fatalf("second withdrawal = %v, want the claim fenced", err)
	}
}

// Once the claim's send has begun, a worker may hold the task, so it is not
// withdrawn as unsent.
func TestASentTaskIsNotWithdrawnAsUnsent(t *testing.T) {
	f := newDelegationFixture(t)
	delegation := f.delegate(t, "call_1")
	claimed := f.claimChild(t, delegation)
	assignment := Assignment{JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "team-worker", AttemptID: claimed.AssignmentAttemptID}
	if err := f.repo.BeginAssignmentSend(f.ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.CancelUnsentRevokedDelegation(f.ctx, assignment); !errors.Is(err, ErrAssignmentFenced) {
		t.Fatalf("withdrawal = %v, want the sent claim fenced", err)
	}
	if status := f.runStatus(t, delegation.ChildRunID); status != "running" {
		t.Fatalf("task = %s, want it left running", status)
	}
}
