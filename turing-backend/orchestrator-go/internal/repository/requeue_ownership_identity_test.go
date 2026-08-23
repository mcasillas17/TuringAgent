package repository

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
)

// ---------------------------------------------------------------------------
// Which assignment attempt cleared a run's ownership.
//
// A clearing transition (requeueTransition, via clearsOwnership) writes the
// same absence no matter which attempt triggered it: worker_id and
// execution_attempt_id both go to NULL. Duplicate detection recognized only
// that absence, so a replay carrying a DIFFERENT assignment attempt's identity
// read the same blank row and was told "yes, that was you" — the same
// unearned acceptance TestApprovalOwnershipIsCheckedBeforeAWriteFreeReplay
// documents for approvals, but for the identity that proves who released a
// run rather than who authorized it to resume.
//
// So the trigger is committed into the transition's own event, on the same
// terms the approval identity is, and the duplicate rule reads it back from
// there. The reopened-database test at the bottom is the whole point: the
// identity has to survive because it is durable, not because a process
// happens to remember it.
// ---------------------------------------------------------------------------

// requeueEventPayload reads back the durable payload of the state-changed
// event a clearing transition committed, from the log rather than from the
// writer's return value.
func requeueEventPayload(t *testing.T, repo *Repository, runID string, version int64) map[string]any {
	t.Helper()
	var payloadJSON string
	err := repo.db.QueryRowContext(context.Background(), `
		SELECT payload_json FROM events
		WHERE run_id = ? AND type = ?
			AND json_extract(payload_json, '$.runState.stateVersion') = ?
	`, runID, runStateChangedEventType, version).Scan(&payloadJSON)
	if err != nil {
		t.Fatalf("read the %s event at version %d: %v", runStateChangedEventType, version, err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode requeue payload %q: %v", payloadJSON, err)
	}
	return payload
}

// TestRequeueCommitsWhichAssignmentAttemptReleasedTheRun pins the durable
// record the duplicate rule is going to read: the attempt that actually
// cleared ownership, in the one place the row itself cannot keep it once it is
// cleared.
func TestRequeueCommitsWhichAssignmentAttemptReleasedTheRun(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, recovering := recoveringRun(t, repo, "worker-requeue-commits")

	committed, err := repo.RequeueRecoveringRun(ctx, RequeueRecoveringRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: recovering.StateVersion,
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
	})
	if err != nil {
		t.Fatalf("RequeueRecoveringRun: %v", err)
	}
	if committed.Duplicate {
		t.Fatal("the first requeue reported itself a duplicate")
	}

	payload := requeueEventPayload(t, repo, enqueued.RunID, committed.State.StateVersion)
	if payload[ownershipAssignmentAttemptIdentityPayloadKey] != claimed.AssignmentAttemptID {
		t.Fatalf("requeue payload %s = %v, want %q: %#v",
			ownershipAssignmentAttemptIdentityPayloadKey, payload[ownershipAssignmentAttemptIdentityPayloadKey],
			claimed.AssignmentAttemptID, payload)
	}
	// worker_id was never asserted on this input (RequeueRecoveringRunInput
	// carries no field for it), so nothing under that key should exist either.
	if _, present := payload[ownershipWorkerIdentityPayloadKey]; present {
		t.Fatalf("requeue payload carries an unasserted %s: %#v", ownershipWorkerIdentityPayloadKey, payload)
	}
}

// TestRequeueTriggerIdentityFencesADifferentAssignmentAttempt is the
// regression test for the bug this change fixes.
//
// Attempt A clears ownership. An exact replay by A must still cost nothing —
// hybrid requeue semantics depend on that. A replay by a DIFFERENT attempt B,
// naming the exact same run and the exact same version A already left, must
// conflict rather than be told it already succeeded: B never released
// anything, and answering it with a duplicate would let a stale or
// impersonating assignment attempt believe it owns a run's requeue that
// belongs to A.
func TestRequeueTriggerIdentityFencesADifferentAssignmentAttempt(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, recovering := recoveringRun(t, repo, "worker-requeue-fence")

	requeue := RequeueRecoveringRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: recovering.StateVersion,
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
	}
	first, err := repo.RequeueRecoveringRun(ctx, requeue)
	if err != nil {
		t.Fatalf("RequeueRecoveringRun: %v", err)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	// The exact replay: same run, same version, same attempt. Still free.
	replayed, err := repo.RequeueRecoveringRun(ctx, requeue)
	if err != nil {
		t.Fatalf("replayed RequeueRecoveringRun: %v", err)
	}
	if !replayed.Duplicate || replayed.State != first.State {
		t.Fatalf("replayed requeue = %+v, want the committed %+v as a duplicate", replayed, first.State)
	}
	if got := countRunEvents(t, repo, enqueued.RunID); got != events {
		t.Fatalf("the exact replay appended %d events", got-events)
	}

	// A different attempt, naming the same run and the same version A left.
	impostor := requeue
	impostor.AssignmentAttemptID = claimed.AssignmentAttemptID + "-impostor"
	before, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}

	result, err := repo.RequeueRecoveringRun(ctx, impostor)

	if !errors.Is(err, ErrRunTransitionConflict) {
		t.Fatalf("a different attempt's replay = (%+v, %v), want %v", result, err, ErrRunTransitionConflict)
	}
	if result.State != (RunState{}) || result.Duplicate || len(result.Events) != 0 {
		t.Fatalf("the fenced replay returned %+v, want nothing", result)
	}
	after, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("the fenced replay changed the run: %+v, want %+v", after, before)
	}
	if got := countRunEvents(t, repo, enqueued.RunID); got != events {
		t.Fatalf("the fenced replay appended %d events", got-events)
	}
}

// TestUncertainRequeueReplayWithNoIdentityStaysADuplicate preserves the other
// half of hybrid requeue semantics: a caller with no owner to prove — lease
// recovery, which requeues a run precisely because nobody can vouch for it —
// must not be newly fenced by this change. An absent identity is not a claim,
// so there is nothing for the durable trigger check to contradict.
func TestUncertainRequeueReplayWithNoIdentityStaysADuplicate(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, _, recovering := recoveringRun(t, repo, "worker-requeue-uncertain")

	requeue := RequeueRecoveringRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: recovering.StateVersion,
	}
	first, err := repo.RequeueRecoveringRun(ctx, requeue)
	if err != nil {
		t.Fatalf("RequeueRecoveringRun: %v", err)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	replayed, err := repo.RequeueRecoveringRun(ctx, requeue)
	if err != nil {
		t.Fatalf("replayed identity-less RequeueRecoveringRun: %v", err)
	}
	if !replayed.Duplicate || replayed.State != first.State {
		t.Fatalf("replayed identity-less requeue = %+v, want the committed %+v as a duplicate", replayed, first.State)
	}
	if got := countRunEvents(t, repo, enqueued.RunID); got != events {
		t.Fatalf("the identity-less replay appended %d events", got-events)
	}
}

// TestSpecificAttemptReplayAfterAnUnprovenClearIsFenced is the fail-closed
// mirror of the test above: an unproven clear commits no trigger at all, so a
// LATER command that does assert a specific attempt must not be told it was
// the one that cleared the row. Nothing durable says that, and treating an
// unexplained clear as this command's own would be exactly the acceptance this
// change exists to refuse.
func TestSpecificAttemptReplayAfterAnUnprovenClearIsFenced(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, recovering := recoveringRun(t, repo, "worker-requeue-unproven")

	if _, err := repo.RequeueRecoveringRun(ctx, RequeueRecoveringRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: recovering.StateVersion,
	}); err != nil {
		t.Fatalf("identity-less RequeueRecoveringRun: %v", err)
	}
	before, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	result, err := repo.RequeueRecoveringRun(ctx, RequeueRecoveringRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: recovering.StateVersion,
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
	})

	if !errors.Is(err, ErrRunTransitionConflict) {
		t.Fatalf("specific-attempt replay of an unproven clear = (%+v, %v), want %v", result, err, ErrRunTransitionConflict)
	}
	after, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("the fenced replay changed the run: %+v, want %+v", after, before)
	}
	if got := countRunEvents(t, repo, enqueued.RunID); got != events {
		t.Fatalf("the fenced replay appended %d events", got-events)
	}
}

// TestRequeueTriggerIdentityWithoutADurableRecordIsFenced pins the fail-closed
// edge: an event committed before this rule existed carries no assignment
// attempt, so nothing durable says which attempt cleared the row. A replay
// asserting a specific attempt across that upgrade is refused rather than
// answered, on the same terms TestApprovalResumeWithoutADurableTriggerIsFenced
// pins for approvals.
func TestRequeueTriggerIdentityWithoutADurableRecordIsFenced(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed, recovering := recoveringRun(t, repo, "worker-requeue-legacy")

	committed, err := repo.RequeueRecoveringRun(ctx, RequeueRecoveringRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: recovering.StateVersion,
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
	})
	if err != nil {
		t.Fatalf("RequeueRecoveringRun: %v", err)
	}
	// Exactly the shape the previous build wrote: the state projection, with no
	// record of the trigger beside it.
	if _, err := repo.db.ExecContext(ctx, `
		UPDATE events
		SET payload_json = json_remove(payload_json, ?)
		WHERE run_id = ? AND type = ?
			AND json_extract(payload_json, ?) = ?
	`, ownershipAssignmentAttemptIdentityPayloadPath, enqueued.RunID, runStateChangedEventType,
		runStateVersionPayloadPath, committed.State.StateVersion); err != nil {
		t.Fatalf("rewrite the requeue event as a legacy one: %v", err)
	}
	if payload := requeueEventPayload(t, repo, enqueued.RunID, committed.State.StateVersion); payload[ownershipAssignmentAttemptIdentityPayloadKey] != nil {
		t.Fatalf("the rewritten event still carries an assignment attempt: %#v", payload)
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	result, err := repo.RequeueRecoveringRun(ctx, RequeueRecoveringRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: recovering.StateVersion,
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
	})

	if !errors.Is(err, ErrRunTransitionConflict) {
		t.Fatalf("replay of a legacy event = (%+v, %v), want %v", result, err, ErrRunTransitionConflict)
	}
	after, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after != committed.State {
		t.Fatalf("the fenced replay changed the run: %+v, want %+v", after, committed.State)
	}
	if got := countRunEvents(t, repo, enqueued.RunID); got != events {
		t.Fatalf("the fenced replay appended %d events", got-events)
	}
}

// TestReleaseRunningTriggerIdentityFencesADifferentWorkerOrAttempt proves the
// shared primitive for the two-field identity shape a confirmed release
// asserts (worker AND assignment attempt), which RequeueRecoveringRun's
// single-field identity above cannot exercise.
//
// It calls releaseRunningTransition through r.runInTransition directly rather
// than through RequeueOrFailRetryableRun's confirmedRelease path. That public
// entry point re-reads the run row before it ever calls into the guarded
// transition and requires runStatus == "running" there — which a replay never
// satisfies, because the first call already moved the row to "queued" — so
// every replay is fenced at that outer gate regardless of identity, and the
// public entry point can never reach isRunTransitionDuplicate to exercise this
// fix. The guarded primitive both that entry point and RequeueRecoveringRun
// share is exactly what this change fixes, so it is exercised directly here,
// with the exact production constructor and production types
// requeueRetryableRunTx itself uses.
func TestReleaseRunningTriggerIdentityFencesADifferentWorkerOrAttempt(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, claimed := claimRetryRun(t, repo, "worker-release-fence")

	identity := runTransitionIdentity{workerID: "worker-release-fence", assignmentAttemptID: claimed.AssignmentAttemptID}
	release := func(id runTransitionIdentity) (RunTransitionResult, error) {
		return repo.runInTransition(ctx, releaseRunningTransition(enqueued.RunID, claimed.ExpectedStateVersion, id), nil)
	}

	first, err := release(identity)
	if err != nil {
		t.Fatalf("releaseRunningTransition: %v", err)
	}
	if first.Duplicate {
		t.Fatal("the first release reported itself a duplicate")
	}
	events := countRunEvents(t, repo, enqueued.RunID)

	// The exact replay, same worker and same attempt, is still free.
	replayed, err := release(identity)
	if err != nil {
		t.Fatalf("replayed releaseRunningTransition: %v", err)
	}
	if !replayed.Duplicate || replayed.State != first.State {
		t.Fatalf("replayed release = %+v, want the committed %+v as a duplicate", replayed, first.State)
	}
	if got := countRunEvents(t, repo, enqueued.RunID); got != events {
		t.Fatalf("the exact replay appended %d events", got-events)
	}

	for name, impostor := range map[string]runTransitionIdentity{
		"a different worker, the same attempt": {
			workerID: "worker-impostor", assignmentAttemptID: claimed.AssignmentAttemptID,
		},
		"the same worker, a different attempt": {
			workerID: "worker-release-fence", assignmentAttemptID: claimed.AssignmentAttemptID + "-impostor",
		},
	} {
		t.Run(name, func(t *testing.T) {
			before, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			beforeEvents := countRunEvents(t, repo, enqueued.RunID)

			result, err := release(impostor)

			if !errors.Is(err, ErrRunTransitionConflict) {
				t.Fatalf("%s = (%+v, %v), want %v", name, result, err, ErrRunTransitionConflict)
			}
			after, err := repo.GetRunState(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("%s changed the run: %+v, want %+v", name, after, before)
			}
			if got := countRunEvents(t, repo, enqueued.RunID); got != beforeEvents {
				t.Fatalf("%s appended %d events", name, got-beforeEvents)
			}
		})
	}
}

// TestRequeueTriggerIdentitySurvivesProcessRestart is what makes this identity
// durable rather than a fact about the process that committed it. Everything
// in memory is gone: the database is closed and reopened, and a brand new
// repository answers both questions — the exact replay and the fence against a
// different attempt — from the log alone.
func TestRequeueTriggerIdentitySurvivesProcessRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turing.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo := New(database)
	ctx := context.Background()
	enqueued, claimed, recovering := recoveringRun(t, repo, "worker-requeue-restart")

	requeue := RequeueRecoveringRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: recovering.StateVersion,
		AssignmentAttemptID:  claimed.AssignmentAttemptID,
	}
	committed, err := repo.RequeueRecoveringRun(ctx, requeue)
	if err != nil {
		t.Fatalf("RequeueRecoveringRun: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	restarted, err := db.Open(path)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	repo = New(restarted)

	replayed, err := repo.RequeueRecoveringRun(ctx, requeue)
	if err != nil {
		t.Fatalf("replayed RequeueRecoveringRun after restart: %v", err)
	}
	if !replayed.Duplicate || replayed.State != committed.State {
		t.Fatalf("replayed requeue after restart = %+v, want the committed %+v as a duplicate", replayed, committed.State)
	}

	// And the fence survives with it: the process that could have remembered
	// which attempt it requeued is gone, and the answer is the same.
	impostor := requeue
	impostor.AssignmentAttemptID = claimed.AssignmentAttemptID + "-impostor"
	if _, err := repo.RequeueRecoveringRun(ctx, impostor); !errors.Is(err, ErrRunTransitionConflict) {
		t.Fatalf("a different attempt's replay after restart = %v, want %v", err, ErrRunTransitionConflict)
	}
}
