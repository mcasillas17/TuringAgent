package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

// ---------------------------------------------------------------------------
// What a Ready that did not arrive is allowed to claim.
//
// The worker has exactly two honest answers here and they are not
// interchangeable. If the Ready never left this process, the row is still
// waiting approval and this attempt still owns it, so the run can be failed
// with the transport failure that actually happened — and every OTHER run on
// the stream carries on. If the Ready may have escaped, the orchestrator may
// already have committed running, so nothing this worker says about the run is
// knowable: the stream drops and the common ownership fence decides.
//
// Which of the two applies is not a guess. It is whether the outbound send had
// begun, which the writer already records. Everything below drives one of those
// two states deliberately rather than by racing a clock.
// ---------------------------------------------------------------------------

// resumeDeliveryHarness is resumeApproval's exact world with nothing else in
// it: one active run, one pending resume, one outbound writer over a stream the
// test owns, and the fatal channel Run would otherwise be reading.
//
// It exists because the interesting states — a send that never begins, a send
// that begins and then fails — are properties of the writer, and driving them
// through a full worker loop would mean racing the executor to produce them.
type resumeDeliveryHarness struct {
	worker *Worker
	stream *fakeStream
	entry  *activeRun
	fatal  chan error
	// pending is held as a pointer rather than looked up by approval, because
	// the slot is deliberately dropped with the run: the deadline still has to
	// be readable afterwards to prove nothing extended it.
	pending *approvalResume
	started chan *turingv1.RuntimeUpdate
	release chan struct{}
	once    sync.Once
}

// waitDeadline is the instant the worker recorded this resume's wait as ending.
func (h *resumeDeliveryHarness) waitDeadline() time.Time {
	h.worker.mu.Lock()
	defer h.worker.mu.Unlock()
	return h.pending.deadline
}

const (
	resumeDeliveryRunID    = "run_resume_delivery"
	resumeDeliveryApproval = "approval_resume_delivery"
	resumeDeliveryAttempt  = "attempt_resume_delivery"
	resumeDeliveryVersion  = int64(7)
)

// newResumeDeliveryHarness leaves the worker exactly where resumeApproval is
// entered: the run is executing, the approval's requirement is known, and the
// decision has already arrived, so only the Ready exchange is left.
func newResumeDeliveryHarness(t *testing.T) *resumeDeliveryHarness {
	t.Helper()
	stream := newFakeStream()
	stream.ctx = context.Background()
	executor := &blockingExecutor{started: make(chan string, 4)}
	runtimeWorker := New(Options{
		WorkerID: "worker-resume-delivery", MaxConcurrentRuns: 1, UpdateSendTimeout: 5 * time.Second,
	}, &fakeRuntimeClient{stream: stream}, executor)
	runtimeWorker.startOutboundWriter(stream)
	fatal := make(chan error, 4)
	runtimeWorker.setFatalChannel(fatal)

	runCtx, cancel := context.WithCancelCause(context.Background())
	entry := &activeRun{
		runID:     resumeDeliveryRunID,
		cancel:    cancel,
		done:      make(chan struct{}),
		attemptID: resumeDeliveryAttempt,
		version:   resumeDeliveryVersion,
	}
	// The executor unwinding on cancellation, which is what releases the slot.
	go func() {
		<-runCtx.Done()
		close(entry.done)
	}()

	runtimeWorker.mu.Lock()
	runtimeWorker.active[resumeDeliveryRunID] = entry
	runtimeWorker.mu.Unlock()
	runtimeWorker.rememberApproval(resumeDeliveryApproval, resumeDeliveryRunID)
	runtimeWorker.markApprovalDecided(resumeDeliveryApproval)

	runtimeWorker.mu.Lock()
	pending := runtimeWorker.resumes[resumeDeliveryApproval]
	runtimeWorker.mu.Unlock()
	if pending == nil {
		t.Fatalf("no pending resume for approval %q", resumeDeliveryApproval)
	}
	harness := &resumeDeliveryHarness{
		worker: runtimeWorker, stream: stream, entry: entry, fatal: fatal, pending: pending,
		started: make(chan *turingv1.RuntimeUpdate, 4), release: make(chan struct{}),
	}
	t.Cleanup(func() {
		harness.releaseWriter()
		cancel(context.Canceled)
		runtimeWorker.stopOutboundWriter()
	})
	return harness
}

// blockWriter occupies the outbound writer with one update it will not finish
// sending, so the next send queues behind it and cannot begin.
func (h *resumeDeliveryHarness) blockWriter(t *testing.T) {
	t.Helper()
	entered := make(chan struct{})
	h.stream.sendFn = func(update *turingv1.RuntimeUpdate) error {
		if update.GetHeartbeat() != nil {
			close(entered)
			<-h.release
			return nil
		}
		h.started <- update
		return nil
	}
	go func() {
		_ = h.worker.send(context.Background(), h.stream, &turingv1.RuntimeUpdate{
			Update: &turingv1.RuntimeUpdate_Heartbeat{Heartbeat: &turingv1.RuntimeHeartbeat{WorkerId: "worker-resume-delivery"}},
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the outbound writer never started the blocking update")
	}
}

func (h *resumeDeliveryHarness) releaseWriter() {
	h.once.Do(func() { close(h.release) })
}

// failReadySend makes the Ready's send fail after the writer has begun it,
// which is the state that leaves the Ready's fate unknowable.
func (h *resumeDeliveryHarness) failReadySend(cause error) {
	h.stream.sendFn = func(update *turingv1.RuntimeUpdate) error {
		h.started <- update
		if update.GetApprovalResumeReady() != nil {
			return cause
		}
		return nil
	}
}

func (h *resumeDeliveryHarness) resume(deadline time.Time) error {
	return h.worker.resumeApproval(context.Background(), h.stream, tools.ApprovalResume{
		RunID:      resumeDeliveryRunID,
		ApprovalID: resumeDeliveryApproval,
		Deadline:   deadline,
	})
}

// resumeAsync runs the resume off the test goroutine, for the cases where the
// writer is deliberately occupied and the test has to release it before the
// resume can finish reporting.
func (h *resumeDeliveryHarness) resumeAsync(deadline time.Time) <-chan error {
	result := make(chan error, 1)
	go func() { result <- h.resume(deadline) }()
	return result
}

// awaitSpentBudget waits for the resume to give up on its Ready, which it does
// only after the send returned an error. Waiting on this rather than on a clock
// is what makes "the writer was still blocked, so the Ready never began" a fact
// rather than a hope: the blocking send is released only after this returns.
func (h *resumeDeliveryHarness) awaitSpentBudget(t *testing.T) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if h.entry.isStopping() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("the resume never gave up on a Ready it could not send")
		case <-ticker.C:
		}
	}
}

func (h *resumeDeliveryHarness) awaitResume(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the resume never returned")
	}
	return nil
}

// sentUpdates is everything that actually reached the stream.
func (h *resumeDeliveryHarness) sentUpdates() []*turingv1.RuntimeUpdate {
	var updates []*turingv1.RuntimeUpdate
	for {
		select {
		case update := <-h.started:
			updates = append(updates, update)
			continue
		case update := <-h.stream.sent:
			updates = append(updates, update)
			continue
		default:
		}
		return updates
	}
}

func (h *resumeDeliveryHarness) fatalErr() error {
	select {
	case err := <-h.fatal:
		return err
	default:
		return nil
	}
}

// assertTypedDeliveryFailure pins the report a still-owned, still-waiting run
// is allowed to make: the transport failure that happened, at the version this
// worker knows the row is sitting at.
func assertTypedDeliveryFailure(t *testing.T, updates []*turingv1.RuntimeUpdate) {
	t.Helper()
	var failed *turingv1.RuntimeRunFailed
	for _, update := range updates {
		if update.GetApprovalResumeReady() != nil {
			t.Fatalf("a Ready reached the stream after its budget was spent: %+v", update)
		}
		if update.GetRunFailed() != nil {
			if failed != nil {
				t.Fatal("the resume reported the run failed twice")
			}
			failed = update.GetRunFailed()
		}
	}
	if failed == nil {
		t.Fatalf("updates = %+v, want a typed approval delivery failure", updates)
	}
	if failed.GetRunId() != resumeDeliveryRunID || failed.GetCode() != "approval_delivery_failed" {
		t.Fatalf("failure = %+v, want %q for run %q", failed, "approval_delivery_failed", resumeDeliveryRunID)
	}
	if failed.GetFailureOrigin() != turingv1.FailureOrigin_FAILURE_ORIGIN_APPROVAL_TRANSPORT {
		t.Fatalf("failure origin = %s, want approval transport", failed.GetFailureOrigin())
	}
	if failed.GetAutomaticRetryClass() != turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_NEVER {
		t.Fatalf("retry class = %s, want never", failed.GetAutomaticRetryClass())
	}
	if failed.GetExpectedStateVersion() != resumeDeliveryVersion {
		t.Fatalf("failure version = %d, want the waiting version %d", failed.GetExpectedStateVersion(), resumeDeliveryVersion)
	}
}

// TestReadyBudgetSpentBeforeTheSendBeginsFailsGracefully is the distinction the
// old code could not make: a Ready that never began going out is a Ready the
// orchestrator cannot have committed anything for.
//
// The writer is occupied, so the Ready queues and is abandoned unstarted when
// the approval budget runs out. The run is still waiting approval and this
// attempt still owns it, so the honest answer is the typed transport failure —
// and dropping the whole stream here would take every other run on this worker
// down to re-establish something that was never in doubt.
func TestReadyBudgetSpentBeforeTheSendBeginsFailsGracefully(t *testing.T) {
	harness := newResumeDeliveryHarness(t)
	harness.blockWriter(t)
	deadline := time.Now().Add(150 * time.Millisecond)

	result := harness.resumeAsync(deadline)
	// The writer is still occupied here, so the Ready cannot have begun; the
	// resume has already abandoned it and is now reporting the failure.
	harness.awaitSpentBudget(t)
	harness.releaseWriter()
	err := harness.awaitResume(t, result)

	if !runWasTerminalized(err) {
		t.Fatalf("resume error = %v, want a terminal run error", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("resume error = %v, want the spent budget", err)
	}
	if fatal := harness.fatalErr(); fatal != nil {
		t.Fatalf("an unstarted Ready dropped the whole worker stream: %v", fatal)
	}
	waitForInactiveRun(t, harness.worker, resumeDeliveryRunID)
	assertTypedDeliveryFailure(t, harness.sentUpdates())
	if pending := harness.waitDeadline(); !pending.Equal(deadline) {
		t.Fatalf("resume deadline = %s, want the approval budget's own %s", pending, deadline)
	}
}

// TestReadySendThatBeganDropsTheStream is the other half. The bytes were handed
// to the transport, so the orchestrator may have committed running and answered
// an acceptance this worker will never see. Nothing here can say what the run's
// state is, so it says nothing about it: the stream drops and the fence decides.
func TestReadySendThatBeganDropsTheStream(t *testing.T) {
	harness := newResumeDeliveryHarness(t)
	harness.failReadySend(errors.New("ready send failed mid-flight"))

	err := harness.resume(time.Now().Add(5 * time.Second))

	if !runWasTerminalized(err) {
		t.Fatalf("resume error = %v, want a terminal run error", err)
	}
	fatal := harness.fatalErr()
	if !errors.Is(fatal, errApprovalResumeUnacknowledged) {
		t.Fatalf("fatal = %v, want %v", fatal, errApprovalResumeUnacknowledged)
	}
	waitForInactiveRun(t, harness.worker, resumeDeliveryRunID)
	for _, update := range harness.sentUpdates() {
		if update.GetRunFailed() != nil || update.GetRunCompleted() != nil || update.GetRunCancelledAck() != nil {
			t.Fatalf("the worker reported %+v for a run whose Ready may have been committed", update)
		}
	}
}

// TestReadyWithheldByPausedNarrationFailsGracefully is the third way a Ready
// can fail to leave, and it must be answered like the others.
//
// A run whose narration is withheld had an earlier update fail to arrive, so
// this Ready is refused before anything is queued. The orchestrator therefore
// cannot have committed a resume on it — whatever else is wrong with the
// connection, this run never asked. So the typed transport failure is still
// the honest answer, and dropping the whole stream would punish every other run
// on it for a message that was never sent.
func TestReadyWithheldByPausedNarrationFailsGracefully(t *testing.T) {
	harness := newResumeDeliveryHarness(t)
	harness.entry.pauseOutbound()

	err := harness.resume(time.Now().Add(5 * time.Second))

	if !runWasTerminalized(err) {
		t.Fatalf("resume error = %v, want a terminal run error", err)
	}
	if !errors.Is(err, errRunNarrationPaused) {
		t.Fatalf("resume error = %v, want the withheld narration", err)
	}
	if fatal := harness.fatalErr(); fatal != nil {
		t.Fatalf("a Ready that was never queued dropped the whole worker stream: %v", fatal)
	}
	waitForInactiveRun(t, harness.worker, resumeDeliveryRunID)
	assertTypedDeliveryFailure(t, harness.sentUpdates())
}

// TestReadyDeadlineAndDecisionTogetherAreDeterministic removes the coin flip.
//
// A decision that arrives in the same instant the budget expires makes both
// arms of the wait ready at once, and select chooses between them at random. If
// the two arms disagreed about what happens next — one failing the run
// gracefully, the other sending a Ready and then dropping the stream — the
// user-visible outcome of a late decision would be decided by the scheduler.
// Repeating the race proves both orders reach the same answer, and that the
// answer is the honest one for a budget that is already spent: nothing goes out
// and nothing is claimed about a run that never left waiting approval.
func TestReadyDeadlineAndDecisionTogetherAreDeterministic(t *testing.T) {
	for attempt := 0; attempt < 64; attempt++ {
		harness := newResumeDeliveryHarness(t)
		// Already spent when the wait is entered, so the decision — delivered
		// before resumeApproval is even called — and the deadline are both
		// ready and select has a free choice between them.
		deadline := time.Now().Add(-time.Millisecond)

		err := harness.resume(deadline)

		if !runWasTerminalized(err) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("attempt %d: resume error = %v, want a terminal spent budget", attempt, err)
		}
		if fatal := harness.fatalErr(); fatal != nil {
			t.Fatalf("attempt %d: a spent budget dropped the whole worker stream: %v", attempt, fatal)
		}
		waitForInactiveRun(t, harness.worker, resumeDeliveryRunID)
		assertTypedDeliveryFailure(t, harness.sentUpdates())
		if pending := harness.waitDeadline(); !pending.Equal(deadline) {
			t.Fatalf("attempt %d: resume deadline = %s, want %s", attempt, pending, deadline)
		}
	}
}

// TestFailedResumeRemembersItsAttemptOnEveryOwnedPath closes the window a
// resume failure opens between releasing the run and reporting it.
//
// Both failure paths claim the run's terminal report and then release the slot.
// The orchestrator, meanwhile, proves ownership and sends a same-attempt
// refresh — the ordinary shape of "here is the version I committed while your
// run's ownership was in doubt". If the attempt is not remembered before the
// entry goes, that refresh looks like a brand new assignment: the whole job
// runs again, commits its side effects again, and reports a second outcome.
func TestFailedResumeRemembersItsAttemptOnEveryOwnedPath(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		drive func(*testing.T, *resumeDeliveryHarness)
	}{
		{
			name: "the send never began",
			drive: func(t *testing.T, harness *resumeDeliveryHarness) {
				t.Helper()
				harness.blockWriter(t)
				result := harness.resumeAsync(time.Now().Add(100 * time.Millisecond))
				harness.awaitSpentBudget(t)
				harness.releaseWriter()
				harness.awaitResume(t, result)
			},
		},
		{
			name: "the send began and failed",
			drive: func(t *testing.T, harness *resumeDeliveryHarness) {
				t.Helper()
				harness.failReadySend(errors.New("ready send failed mid-flight"))
				_ = harness.resume(time.Now().Add(5 * time.Second))
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			harness := newResumeDeliveryHarness(t)

			scenario.drive(t, harness)

			waitForInactiveRun(t, harness.worker, resumeDeliveryRunID)
			harness.worker.mu.Lock()
			remembered := harness.worker.terminalAttemptReportedLocked(resumeDeliveryRunID, resumeDeliveryAttempt)
			harness.worker.mu.Unlock()
			if !remembered {
				t.Fatal("the failed resume released the run without remembering the attempt that reported it")
			}
			// The consequence, driven the way the orchestrator produces it.
			harness.worker.startRun(context.Background(), harness.stream, &turingv1.AgentJob{
				JobId: "job_resume_delivery", RunId: resumeDeliveryRunID, TraceId: "trace_resume_delivery",
				AssignmentAttemptId: resumeDeliveryAttempt, ExpectedStateVersion: resumeDeliveryVersion + 1,
			})
			select {
			case runID := <-harness.worker.executor.(*blockingExecutor).started:
				t.Fatalf("a same-attempt refresh restarted the executor for run %q", runID)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}
