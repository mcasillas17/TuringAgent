package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
)

// TUR-028a, at the runtime: what each path that would requeue an unattended
// run does while the owner's quiet switch is on.

// quietOn turns the owner's quiet switch on and finishes its drain, as the
// maintenance duty would.
func (h *harness) quietOn(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, _, err := h.repo.SetUnattendedQuiet(ctx, true, 0); err != nil {
		t.Fatal(err)
	}
	for {
		batch, err := h.repo.DrainUnattendedQuiet(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if batch.Done {
			return
		}
	}
}

// queuedAutomationRun fires an automation, leaving its unattended run queued.
func (h *harness) queuedAutomationRun(t *testing.T) repository.AutomationFire {
	t.Helper()
	return h.claimAutomation(t, h.createAutomation(t, "Digest"))
}

func (h *harness) liveEvents(t *testing.T, sessionID string) <-chan events.Event {
	t.Helper()
	live, unsubscribe := h.bus.Subscribe(sessionID)
	t.Cleanup(unsubscribe)
	return live
}

func waitForLiveEvent(t *testing.T, live <-chan events.Event, runID, eventType string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-live:
			if event.RunID == runID && event.Type == eventType {
				return
			}
		case <-deadline:
			t.Fatalf("no live %s for run %s", eventType, runID)
		}
	}
}

func wantNoLiveEvent(t *testing.T, live <-chan events.Event, runID, eventType string) {
	t.Helper()
	deadline := time.After(200 * time.Millisecond)
	for {
		select {
		case event := <-live:
			if event.RunID == runID && event.Type == eventType {
				t.Fatalf("a live %s for run %s, want it only on replay", eventType, runID)
			}
		case <-deadline:
			return
		}
	}
}

// wantQuietCancelled asserts runID was cancelled owner_quiet with its
// execution released, and that no retry notice was written for it.
func (h *harness) wantQuietCancelled(t *testing.T, runID string) {
	t.Helper()
	run, err := h.repo.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "cancelled" || run.CancellationReason != runoutcome.CodeOwnerQuiet ||
		run.ExecutionActive || run.ExecutionState != "exited" {
		t.Fatalf("run %s = %s/%s, execution active %v %s; want cancelled owner_quiet with its execution released",
			runID, run.Status, run.CancellationReason, run.ExecutionActive, run.ExecutionState)
	}
	var notices int
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.step'`, runID).Scan(&notices); err != nil {
		t.Fatal(err)
	}
	if notices != 0 {
		t.Fatalf("retry notices for run %s = %d, want none", runID, notices)
	}
}

func transientFailure(runID string, version int64) *turingv1.RuntimeRunFailed {
	return &turingv1.RuntimeRunFailed{
		RunId: runID, Code: "model_timeout",
		FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
		AutomaticRetryClass:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
		ExpectedStateVersion: version,
	}
}

// A retryable failure of an unattended run during quiet is cancelled instead
// of requeued, and a subscribed conversation sees the cancellation live,
// with no retry notice; the dispatch that follows a failure still runs. The
// live event carries the public, closed reason (abandoned); owner_quiet is
// the code stored on the run, which wantQuietCancelled reads.
func TestARetryableFailureDuringQuietIsACancellationSeenLive(t *testing.T) {
	for name, owned := range map[string]bool{"reported by its owner": true, "nobody can attribute": false} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			fire := h.queuedAutomationRun(t)
			attempt := h.attemptFor(t, fire.RunID, "worker-1")
			h.quietOn(t)
			live := h.liveEvents(t, fire.SessionID)

			owner := runReleaseOwner{}
			if owned {
				owner = runReleaseOwner{workerID: "worker-1", attemptID: attempt.AssignmentAttemptID}
			}
			suppress, err := h.service.handleRunFailed(context.Background(),
				transientFailure(fire.RunID, h.runState(t, fire.RunID).StateVersion), owner)
			if err != nil || suppress {
				t.Fatalf("handleRunFailed = suppress %v, %v; want the dispatch that follows a failure", suppress, err)
			}
			waitForLiveEvent(t, live, fire.RunID, "agent.run.cancelled")
			h.wantQuietCancelled(t, fire.RunID)
		})
	}
}

// Reconciliation's quiet cancellations are published live too: the release
// of a claim a disconnected worker never received, and the reaper's cutoff
// of a delivered assignment whose lease ran out.
func TestAReconciliationDuringQuietIsACancellationSeenLive(t *testing.T) {
	t.Run("a worker disconnected before the send", func(t *testing.T) {
		h := newHarness(t)
		fire := h.queuedAutomationRun(t)
		claimed, err := h.repo.ClaimNextCompatibleJobWithLimit(context.Background(), "general_assistant", "worker-1", 0, 0, nil, nil)
		if err != nil || claimed.RunID != fire.RunID {
			t.Fatalf("claim = %+v, %v", claimed, err)
		}
		h.quietOn(t)
		live := h.liveEvents(t, fire.SessionID)
		reconciled, err := h.service.reconcileAssignments([]assignment{{
			jobID: claimed.JobID, runID: claimed.RunID, attemptID: claimed.AssignmentAttemptID,
		}}, "worker-1")
		if err != nil || !reconciled {
			t.Fatalf("reconcile = %v, %v; want the run cleared, so pending work is dispatched", reconciled, err)
		}
		waitForLiveEvent(t, live, fire.RunID, "agent.run.cancelled")
		h.wantQuietCancelled(t, fire.RunID)
	})
	t.Run("a lease lost past the reaper's cutoff", func(t *testing.T) {
		h := newHarness(t)
		fire := h.queuedAutomationRun(t)
		ctx := context.Background()
		claimed, err := h.repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-1", 0, time.Millisecond, nil, nil)
		if err != nil || claimed.RunID != fire.RunID {
			t.Fatalf("claim = %+v, %v", claimed, err)
		}
		delivered := repository.Assignment{JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-1", AttemptID: claimed.AssignmentAttemptID}
		if err := h.repo.BeginAssignmentSend(ctx, delivered); err != nil {
			t.Fatal(err)
		}
		if err := h.repo.MarkAssignmentDelivered(ctx, delivered); err != nil {
			t.Fatal(err)
		}
		h.quietOn(t)
		live := h.liveEvents(t, fire.SessionID)
		time.Sleep(5 * time.Millisecond)
		if err := h.service.RecoverOrphanedAssignments(ctx); err != nil {
			t.Fatal(err)
		}
		waitForLiveEvent(t, live, fire.RunID, "agent.run.cancelled")
		h.wantQuietCancelled(t, fire.RunID)
	})
}

// An abort publishes nothing live, during quiet as before it: its
// cancellation is seen on replay, as its requeue would have been.
func TestAnAbortDuringQuietIsSeenOnReplay(t *testing.T) {
	h := newHarness(t)
	fire := h.queuedAutomationRun(t)
	ctx := context.Background()
	claimed, err := h.repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-1", 0, 0, nil, nil)
	if err != nil || claimed.RunID != fire.RunID {
		t.Fatalf("claim = %+v, %v", claimed, err)
	}
	h.quietOn(t)
	live := h.liveEvents(t, fire.SessionID)
	if err := h.service.releaseClaimedAssignment(assignment{
		jobID: claimed.JobID, runID: claimed.RunID, attemptID: claimed.AssignmentAttemptID,
	}); err != nil {
		t.Fatal(err)
	}
	wantNoLiveEvent(t, live, fire.RunID, "agent.run.cancelled")
	h.wantQuietCancelled(t, fire.RunID)
	replayed, _, err := h.repo.ReplayEvents(ctx, fire.SessionID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range replayed {
		if event.Type == "agent.run.cancelled" && event.RunID.String == fire.RunID {
			return
		}
	}
	t.Fatalf("replay = %+v, want the abort's cancellation", replayed)
}

// sendSetup is one unattended assignment claimed for worker-1 and waiting in
// its command buffer, quiet turned on after the claim, an owner's turn
// queued behind it, and worker-2 registered and idle. The dispatch allows one
// executing run at a time, so the turn is claimed only once the unattended
// run's execution is released.
type sendSetup struct {
	h        *harness
	fire     repository.AutomationFire
	attended repository.EnqueueUserMessageResult
	first    *worker
	second   *worker
	queued   workerCommand
}

func newSendSetup(t *testing.T) sendSetup {
	t.Helper()
	h := newHarnessWithDispatch(t, DispatchConfig{MaxConcurrentRuns: 1})
	fire := h.queuedAutomationRun(t)
	capabilities := modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1)
	first := registerWorkerCapabilities(t, h, "worker-1", "registration-1", capabilities)
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	queued := <-first.commands
	if queued.command.GetRunAssigned().GetRunId() != fire.RunID {
		t.Fatalf("worker-1 was handed %v, want the automation run %s", queued.command, fire.RunID)
	}
	h.quietOn(t)
	attended := h.enqueueRun(t, "Owner turn")
	second := registerWorkerCapabilities(t, h, "worker-2", "registration-2", capabilities)
	return sendSetup{h: h, fire: fire, attended: attended, first: first, second: second, queued: queued}
}

// A claim released before delivery during quiet is cancelled, and the caller
// still dispatches in the same pass: with the reaper off, the owner's turn is
// claimed into the freed slot at once.
func TestAQuietReleaseBeforeDeliveryClaimsAttendedWorkInTheSamePass(t *testing.T) {
	s := newSendSetup(t)
	s.first.mu.Lock()
	s.first.lastHeartbeat = time.Time{}
	s.first.mu.Unlock()
	if err := s.h.service.sendCommand(context.Background(), nil, s.queued, s.first, "worker-1"); err != nil {
		t.Fatalf("sendCommand = %v", err)
	}
	s.h.wantQuietCancelled(t, s.fire.RunID)
	if got := assignedRun(t, s.second); got != s.attended.RunID {
		t.Fatalf("worker-2 was handed %s, want the owner's turn %s in the same pass", got, s.attended.RunID)
	}
}

// A send that fails without a fence dispatches nothing, as before quiet: the
// run is cancelled with its slot released, and the owner's turn is claimed
// on the next dispatch trigger.
func TestAQuietCancelledSendFailureFreesTheSlotForTheNextDispatch(t *testing.T) {
	s := newSendSetup(t)
	ctx := context.Background()
	if _, err := s.h.database.ExecContext(ctx, `
		CREATE TRIGGER refuse_send BEFORE UPDATE OF execution_state ON agent_runs
		WHEN NEW.execution_state = 'sending'
		BEGIN SELECT RAISE(ABORT, 'injected send failure'); END`); err != nil {
		t.Fatal(err)
	}
	err := s.h.service.sendCommand(ctx, nil, s.queued, s.first, "worker-1")
	if err == nil || errors.Is(err, repository.ErrAssignmentFenced) {
		t.Fatalf("sendCommand = %v, want the send failure itself", err)
	}
	if _, err := s.h.database.ExecContext(ctx, `DROP TRIGGER refuse_send`); err != nil {
		t.Fatal(err)
	}
	s.h.wantQuietCancelled(t, s.fire.RunID)
	if state := s.h.runState(t, s.attended.RunID); state.Lifecycle != "queued" {
		t.Fatalf("owner's turn = %s after a failed send, want it still queued: that path dispatches nothing", state.Lifecycle)
	}
	if err := s.h.service.DispatchPending(ctx); err != nil {
		t.Fatal(err)
	}
	if state := s.h.runState(t, s.attended.RunID); state.Lifecycle != "running" {
		t.Fatalf("owner's turn = %s after the next dispatch, want it claimed into the freed slot", state.Lifecycle)
	}
}

// The fenced send keeps its same-pass dispatch during quiet: a run the owner
// cancelled before its send is acknowledged, and the owner's turn is claimed
// at once.
func TestAFencedSendStillDispatchesInTheSamePassWhileQuiet(t *testing.T) {
	s := newSendSetup(t)
	ctx := context.Background()
	if _, err := s.h.repo.CancelRunCanonical(ctx, repository.CancelRunInput{
		RunID: s.fire.RunID, ExpectedStateVersion: s.h.runState(t, s.fire.RunID).StateVersion,
		Cancellation: runoutcome.UserCancellation(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.h.service.sendCommand(ctx, nil, s.queued, s.first, "worker-1"); err != nil {
		t.Fatalf("sendCommand = %v", err)
	}
	if state := s.h.runState(t, s.attended.RunID); state.Lifecycle != "running" {
		t.Fatalf("owner's turn = %s, want it claimed in the same pass", state.Lifecycle)
	}
}

// Quiet never stops a run its worker still owns: an unattended run already
// going keeps sending its model's output and finishes.
func TestARunningUnattendedRunKeepsWorkingThroughQuiet(t *testing.T) {
	h := newHarness(t)
	fire := h.queuedAutomationRun(t)
	h.attemptFor(t, fire.RunID, "worker-1")
	h.quietOn(t)
	ctx := context.Background()
	for range 3 {
		if err := h.service.applyUpdate(ctx, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
			RunId: fire.RunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		}}}); err != nil {
			t.Fatalf("model output while quiet = %v, want it accepted", err)
		}
	}
	run, err := h.repo.GetRun(ctx, fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.service.applyUpdate(ctx, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{
		RunCompleted: &turingv1.RuntimeRunCompleted{
			RunId: fire.RunID, AssistantMessageId: run.AssistantMessageID, Content: "Here is the digest.",
			ExpectedStateVersion: h.runState(t, fire.RunID).StateVersion,
		},
	}}); err != nil {
		t.Fatalf("completion while quiet = %v", err)
	}
	if state := h.runState(t, fire.RunID); state.Lifecycle != "completed" {
		t.Fatalf("run = %s, want it completed", state.Lifecycle)
	}
}
