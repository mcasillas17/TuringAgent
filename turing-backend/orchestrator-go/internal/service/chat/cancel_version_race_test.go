package chat

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// setAfterRunStateReadForCancelHook installs the barrier the abandonment path
// exposes for exactly this regression, and returns the restore the test defers.
//
// The barrier lives on the server rather than on the package, so two harnesses
// in the same run cannot install it over each other and a forgotten restore
// cannot leak into an unrelated test.
func setAfterRunStateReadForCancelHook(t *testing.T, h *harness, hook func(runID string)) func() {
	t.Helper()
	h.service.afterRunStateReadForCancel = hook
	return func() { h.service.afterRunStateReadForCancel = nil }
}

// A client stream can go away while the run it started is still moving. The
// abandonment therefore reads the run's version, then guards its cancellation
// with it — and between those two steps any other writer may commit. The
// version the abandonment holds is stale from that moment on, the guarded
// update loses, and the old code discarded that loss silently: no event, no
// retry, and a run that was supposed to stop still executing with nobody
// watching it.
//
// The barrier below makes that window deterministic instead of waiting for a
// scheduler to land inside it.
func TestAbandonmentTerminalizesAfterALostVersionRace(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRunningRun(t, "abandon me")

	var once sync.Once
	restore := setAfterRunStateReadForCancelHook(t, h, func(runID string) {
		// Exactly one bump: the abandonment must lose its first guarded update
		// and then succeed against the version it re-reads. Bumping on every
		// read would only prove that an unbounded retry loop spins forever.
		once.Do(func() {
			state, err := h.repo.GetRunState(context.Background(), runID)
			if err != nil {
				t.Errorf("read run state: %v", err)
				return
			}
			if _, err := h.repo.FenceRunOwnership(context.Background(), repository.FenceRunOwnershipInput{
				RunID: runID, ExpectedStateVersion: state.StateVersion,
			}); err != nil {
				t.Errorf("bump run version: %v", err)
			}
		})
	})
	defer restore()

	h.service.cancelRun(enqueued.RunID)

	state, err := h.repo.GetRunState(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Lifecycle != "cancelled" {
		t.Fatalf("lifecycle = %q, want the abandonment to have terminalized the run", state.Lifecycle)
	}
	if state.OutcomeReason != "abandoned" {
		t.Fatalf("outcome = %q, want abandoned", state.OutcomeReason)
	}
	assertCancelledEventPublished(t, h, enqueued.RunID, 1)
}

// Losing to a writer that already terminalized the run is not a race to retry:
// there is nothing left to cancel, and the abandonment must simply stop. It
// must not publish a second cancellation for a run that already has one.
func TestAbandonmentIsIdempotentAgainstAnAlreadyTerminalRun(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRunningRun(t, "already finished")

	var once sync.Once
	restore := setAfterRunStateReadForCancelHook(t, h, func(runID string) {
		once.Do(func() {
			state, err := h.repo.GetRunState(context.Background(), runID)
			if err != nil {
				t.Errorf("read run state: %v", err)
				return
			}
			if _, err := h.repo.CancelRunCanonical(context.Background(), repository.CancelRunInput{
				RunID:                runID,
				ExpectedStateVersion: state.StateVersion,
				Cancellation:         runoutcome.AbandonedCancellation(),
			}); err != nil {
				t.Errorf("terminalize ahead of the abandonment: %v", err)
			}
		})
	})
	defer restore()

	h.service.cancelRun(enqueued.RunID)

	state, err := h.repo.GetRunState(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Lifecycle != "cancelled" {
		t.Fatalf("lifecycle = %q, want cancelled", state.Lifecycle)
	}
	assertCancelledEventPublished(t, h, enqueued.RunID, 1)
}

// A run that disappears underneath the abandonment is gone, not contended.
// Deletion wins over the retry: re-reading a row nobody will ever write again
// would spend the whole budget on a certainty, and there is no run left to
// tell the runtime to stop.
func TestAbandonmentStopsWhenTheRunIsDeletedMidRace(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRunningRun(t, "deleted mid-abandonment")

	var once sync.Once
	restore := setAfterRunStateReadForCancelHook(t, h, func(runID string) {
		once.Do(func() {
			// The version moves first, so the guarded cancellation loses and
			// the retry is the code under test; the session then goes away
			// before that retry can re-read it.
			state, err := h.repo.GetRunState(context.Background(), runID)
			if err != nil {
				t.Errorf("read run state: %v", err)
				return
			}
			if _, err := h.repo.FenceRunOwnership(context.Background(), repository.FenceRunOwnershipInput{
				RunID: runID, ExpectedStateVersion: state.StateVersion,
			}); err != nil {
				t.Errorf("bump run version: %v", err)
				return
			}
			if _, err := h.database.ExecContext(context.Background(),
				`DELETE FROM sessions WHERE id = ?`, enqueued.SessionID); err != nil {
				t.Errorf("delete session: %v", err)
			}
		})
	})
	defer restore()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.service.cancelRun(enqueued.RunID)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("abandonment kept retrying a run that no longer exists")
	}

	if _, err := h.repo.GetRunState(context.Background(), enqueued.RunID); err == nil {
		t.Fatal("deleted run still readable")
	}
}

// A run being rewritten continuously is not a race the abandonment can win, and
// spinning against it would hold a goroutine and a database connection until
// the process died. The budget is exact and small, and it is spent rather than
// extended.
func TestAbandonmentGivesUpAfterItsRetryBudget(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRunningRun(t, "never settles")

	reads := 0
	restore := setAfterRunStateReadForCancelHook(t, h, func(runID string) {
		reads++
		state, err := h.repo.GetRunState(context.Background(), runID)
		if err != nil {
			t.Errorf("read run state: %v", err)
			return
		}
		transition := h.repo.FenceRunOwnership
		if state.Lifecycle == "recovering" {
			// Alternate the fence and the resume so every attempt finds a
			// freshly moved version rather than an exhausted lifecycle.
			if _, err := h.repo.ResumeRecoveringRun(context.Background(), repository.ResumeRecoveringRunInput{
				RunID: runID, ExpectedStateVersion: state.StateVersion,
			}); err != nil {
				t.Errorf("resume run: %v", err)
			}
			return
		}
		if _, err := transition(context.Background(), repository.FenceRunOwnershipInput{
			RunID: runID, ExpectedStateVersion: state.StateVersion,
		}); err != nil {
			t.Errorf("bump run version: %v", err)
		}
	})
	defer restore()

	done := make(chan error, 1)
	go func() {
		done <- h.service.abandonRun(context.Background(), enqueued.RunID)
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("abandonment never gave up on a run whose version kept moving")
	}

	if reads != maxCancelVersionAttempts {
		t.Fatalf("run state reads = %d, want the exact budget %d", reads, maxCancelVersionAttempts)
	}
	assertCancelledEventPublished(t, h, enqueued.RunID, 0)
	// Giving up is a real outcome: a run that was supposed to stop is still
	// running with nobody watching it. Returning nil here made that
	// indistinguishable from a clean abandonment, so nothing logged it and no
	// test could see it.
	if !errors.Is(err, errAbandonmentContended) {
		t.Fatalf("exhausted abandonment returned %v, want the contended sentinel", err)
	}
	// Safe by construction: the sentinel is a fixed sentence, so a run ID, a
	// session, or a database error cannot ride out on it.
	if err.Error() != errAbandonmentContended.Error() {
		t.Fatalf("abandonment error text = %q, want the bare sentinel", err.Error())
	}
}

// Giving up on the durable half does not mean giving up on the run. The runtime
// still has to be told to stop executing it, or the exhaustion turns a run
// nobody is watching into a run nobody is watching that also keeps burning a
// worker slot.
func TestAbandonmentStillNotifiesTheRuntimeAfterExhaustingItsBudget(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRunningRun(t, "contended but still running")
	notifier := &cancelNotificationRecorder{}
	h.service.runtime = notifier

	restore := setAfterRunStateReadForCancelHook(t, h, func(runID string) {
		state, err := h.repo.GetRunState(context.Background(), runID)
		if err != nil {
			t.Errorf("read run state: %v", err)
			return
		}
		if state.Lifecycle == "recovering" {
			if _, err := h.repo.ResumeRecoveringRun(context.Background(), repository.ResumeRecoveringRunInput{
				RunID: runID, ExpectedStateVersion: state.StateVersion,
			}); err != nil {
				t.Errorf("resume run: %v", err)
			}
			return
		}
		if _, err := h.repo.FenceRunOwnership(context.Background(), repository.FenceRunOwnershipInput{
			RunID: runID, ExpectedStateVersion: state.StateVersion,
		}); err != nil {
			t.Errorf("bump run version: %v", err)
		}
	})
	defer restore()

	h.service.cancelRun(enqueued.RunID)

	if notifier.runIDs() != 1 {
		t.Fatalf("runtime cancellations = %d, want the run still told to stop", notifier.runIDs())
	}
}

type cancelNotificationRecorder struct {
	mu        sync.Mutex
	cancelled []string
}

func (r *cancelNotificationRecorder) DispatchPending(context.Context) error { return nil }

func (r *cancelNotificationRecorder) CancelRun(_ context.Context, runID string, _ string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelled = append(r.cancelled, runID)
}

func (r *cancelNotificationRecorder) ValidateRouting(context.Context, repository.RoutingRequirements) error {
	return nil
}

func (r *cancelNotificationRecorder) RefreshPendingRoutingState(context.Context, string) error {
	return nil
}

func (r *cancelNotificationRecorder) RoutableDefaultModel(_ string, configured string) string {
	return configured
}

func (r *cancelNotificationRecorder) runIDs() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cancelled)
}

// assertCancelledEventPublished checks the durable log rather than the bus, so
// a republished event cannot hide behind subscriber timing.
func assertCancelledEventPublished(t *testing.T, h *harness, runID string, want int) {
	t.Helper()
	var count int
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.cancelled'`, runID,
	).Scan(&count); err != nil {
		t.Fatalf("count cancellation events: %v", err)
	}
	if count != want {
		t.Fatalf("agent.run.cancelled events = %d, want %d", count, want)
	}
}

func (h *harness) enqueueRunningRun(t *testing.T, content string) repository.EnqueueUserMessageResult {
	t.Helper()
	session, err := h.repo.CreateSession(context.Background(), "Abandonment")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: content, AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	return enqueued
}
