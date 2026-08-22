package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestHandleRunCancelledAckVersionGuardEnforcesRaceWindow calls applyUpdate
// directly — bypassing reconcileLateAssignedUpdate and the ConnectWorker recv
// loop entirely — to pin down handleRunCancelledAck's own version check in
// isolation.
//
// That check is NOT what a real acknowledgement exercises. Production always
// routes a run_cancelled_ack through reconcileLateAssignedUpdate first, and by
// the time a worker acknowledges a cancellation the run is already terminal,
// so reconcileLateAssignedUpdate's own call to isMatchingTerminalUpdate (and,
// through it, acknowledgedVersionMatches) is the check a real ack takes —
// exactly what TestRunCancelledAckMustNameAVersionTheRunReached in
// command_version_test.go proves. This second, handler-local copy of the same
// rule only matters for the narrow race window between
// reconcileLateAssignedUpdate's GetRun (which can still observe the run as
// not yet terminal) and handleRunCancelledAck's own GetRun a moment later
// (which can now find it terminalized): only in that window does
// reconcileLateAssignedUpdate fall through to applyUpdate with no version
// check yet applied, leaving this copy as the only thing left enforcing it.
// Nothing in the recv loop can be made to land in that exact window
// deterministically, so this test drives the handler directly instead. It is
// not, and must not be read as, coverage of normal dispatch.
func TestHandleRunCancelledAckVersionGuardEnforcesRaceWindow(t *testing.T) {
	for _, test := range []struct {
		name     string
		observed func(committed int64) int64
		accepted bool
	}{
		{
			name:     "absent_from_a_worker_that_sends_none",
			observed: func(int64) int64 { return 0 },
			accepted: true,
		},
		{
			name:     "the_committed_version",
			observed: func(committed int64) int64 { return committed },
			accepted: true,
		},
		{
			name:     "the_version_the_run_terminalized_from",
			observed: func(committed int64) int64 { return committed - 1 },
			accepted: true,
		},
		{
			name:     "a_predecessor_version_the_run_has_left",
			observed: func(committed int64) int64 { return committed - 2 },
			accepted: false,
		},
		{
			name:     "a_version_the_run_never_reached",
			observed: func(committed int64) int64 { return committed + 1 },
			accepted: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			enqueued := h.enqueueRun(t, "race window ack "+test.name)
			// A real dispatch, so the execution fence this acknowledgement
			// might release is genuinely held by a live assignment rather
			// than one this test wrote by hand.
			_, assigned := h.connectAssignedWorker(t, "worker-race-ack", enqueued.RunID)
			// Two committed transitions before the terminal one, so
			// "committed-2" is a version the run genuinely held and genuinely
			// left rather than an impossible number.
			h.fenceOwnership(t, enqueued.RunID, "worker-race-ack", assigned.GetAssignmentAttemptId())
			resumeRecovering(t, h, enqueued.RunID)
			cancelRunFixture(t, h, enqueued.RunID)
			committed := h.runState(t, enqueued.RunID).StateVersion
			if committed < 4 {
				t.Fatalf("committed version %d is too small for this fixture", committed)
			}
			eventCountBefore := h.countRunEvents(t, enqueued.RunID)

			err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
				Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{
					RunId:                enqueued.RunID,
					ObservedStateVersion: test.observed(committed),
				}},
			})

			run, getErr := h.repo.GetRun(context.Background(), enqueued.RunID)
			if getErr != nil {
				t.Fatalf("GetRun: %v", getErr)
			}
			if got := h.runState(t, enqueued.RunID).StateVersion; got != committed {
				t.Fatalf("ack changed the run's state version: got %d, want unchanged %d", got, committed)
			}
			if got := h.countRunEvents(t, enqueued.RunID); got != eventCountBefore {
				t.Fatalf("ack appended run events: got %d, want unchanged %d", got, eventCountBefore)
			}

			if test.accepted {
				if err != nil {
					t.Fatalf("accepted ack returned an error: %v", err)
				}
				if run.ExecutionActive {
					t.Fatal("accepted ack left the execution fence held")
				}
				return
			}
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("refused ack error = %v, want FailedPrecondition", err)
			}
			if want := "run_cancelled_ack observed_state_version does not match run"; err.Error() != status.Error(codes.FailedPrecondition, want).Error() {
				t.Fatalf("refused ack error = %q, want %q", err.Error(), want)
			}
			if !run.ExecutionActive {
				t.Fatal("refused ack released the execution fence anyway")
			}
		})
	}
}

// countRunEvents reports how many durable events a run has accumulated, so a
// guard that refuses an acknowledgement can be proven not to have written
// anything on its way to refusing.
func (h *harness) countRunEvents(t *testing.T, runID string) int {
	t.Helper()
	var count int
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE run_id = ?`, runID,
	).Scan(&count); err != nil {
		t.Fatalf("count run events: %v", err)
	}
	return count
}
