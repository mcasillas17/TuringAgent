package chat

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// A run that was deleted is gone: there is nothing left to cancel, and
// reporting that as success is correct because GetRunState's sql.ErrNoRows is
// a semantically benign absence. A closed connection, a broken correlation, or
// any other genuine repository failure is not gone — it is unknown. Reporting
// that as success tells a caller its run stopped when nobody actually
// terminalized it, and the runtime cancellation that follows is advisory
// only: without a durable terminal state, an ack raced against the recovery
// loop can be rejected and the run is left running with nobody watching it.
//
// The database is closed before abandonRun is even called, so its very first
// GetRunState read — before any hook, any retry, any version race — sees a
// driver failure rather than the deletion's sql.ErrNoRows. That is
// deterministic: no scheduler timing or sleep is needed, and no lifecycle
// state is disturbed to produce it.
func TestAbandonmentPropagatesAGenuineGetRunStateError(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRunningRun(t, "genuine read failure")

	if err := h.database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	err := h.service.abandonRun(context.Background(), enqueued.RunID)
	if err == nil {
		t.Fatal("abandonRun returned success for a closed database, want the genuine failure propagated")
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("abandonRun reported a closed database as a benign absence: %v", err)
	}
}

// CancelRunCanonical can fail for a reason that has nothing to do with the
// run's lifecycle: the transaction itself cannot begin or commit because the
// connection underneath it is gone. That is not "already terminal" and it is
// not "deleted" — the run is exactly as active as it was before the call, and
// the caller must be told the durable half did not happen so it does not treat
// a merely-advisory runtime cancellation as sufficient.
//
// The hook fires after GetRunState's read succeeds and before
// CancelRunCanonical runs, so the version race retry is never entered and the
// only path exercised is the one guarding CancelRunCanonical's own error.
func TestAbandonmentPropagatesAGenuineCancelRunCanonicalError(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRunningRun(t, "genuine cancel failure")

	restore := setAfterRunStateReadForCancelHook(t, h, func(string) {
		if err := h.database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	defer restore()

	err := h.service.abandonRun(context.Background(), enqueued.RunID)
	if err == nil {
		t.Fatal("abandonRun returned success for a database that closed before the cancel transaction, want the genuine failure propagated")
	}
	if errors.Is(err, repository.ErrRunTransitionConflict) || errors.Is(err, repository.ErrRunNotCancellable) || errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("abandonRun reported a genuine repository failure as one of the idempotent sentinels: %v", err)
	}
}
