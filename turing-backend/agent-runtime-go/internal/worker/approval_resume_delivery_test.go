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
	harness := newUndecidedResumeDeliveryHarness(t)
	harness.worker.markApprovalDecided(resumeDeliveryApproval)
	return harness
}

// newUndecidedResumeDeliveryHarness is newResumeDeliveryHarness stopped one
// step earlier: the approval's requirement is known, but the approved
// decision command has not arrived, so pending.decided has not fired and
// beginReadySend can never have run. It exists for tests that need to prove
// something about that specific gap — an acceptance delivered into it must
// have no effect — rather than about the Ready exchange the decided harness
// starts past.
func newUndecidedResumeDeliveryHarness(t *testing.T) *resumeDeliveryHarness {
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
	return h.resumeWithContext(context.Background(), deadline)
}

// resumeWithContext is resume with an explicit, caller-owned context. Tests
// that need to end the wait themselves — rather than letting a deadline
// expire on the clock — cancel this context directly, which is what waitCtx
// inside resumeApproval becomes when deadline is the zero value.
func (h *resumeDeliveryHarness) resumeWithContext(ctx context.Context, deadline time.Time) error {
	return h.worker.resumeApproval(ctx, h.stream, tools.ApprovalResume{
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

// ---------------------------------------------------------------------------
// A rejected or absent acceptance must never move the run's observed version.
//
// deliverResumeAcceptance validates identity and version before
// pending.recordAccepted ever runs, and that validation must never itself
// adopt anything: only completeAcceptedResume, reached exclusively through a
// claimed accepted outcome, is allowed to move the version forward. The tests
// below drive both ways a version could otherwise have moved for nothing —
// once through a rejected delivery, once through no delivery at all.
// ---------------------------------------------------------------------------

// TestRejectedAcceptanceLeavesTheRunsVersionUnchanged is the fix for a version
// that used to move even when the acceptance carrying it was ultimately
// refused: deliverResumeAcceptance validated the version through the same
// call that adopted it, before recordAccepted ever got a say, so a late
// acceptance arriving after the resume had already been abandoned still
// dragged the run's observed version forward on its way to being dropped.
// Adoption now happens exactly once, in completeAcceptedResume, and only for
// an outcome recordAccepted or abandonOrClaim actually committed — so a
// rejected acceptance must leave the version exactly where it was.
func TestRejectedAcceptanceLeavesTheRunsVersionUnchanged(t *testing.T) {
	harness := newResumeDeliveryHarness(t)
	// Settle the resume to abandoned directly, without driving resumeApproval
	// through a whole failure path: recordAccepted must refuse anything for
	// it from here on, which is exactly the rejection this test needs.
	if accepted, ok := harness.pending.abandonOrClaim(); ok {
		t.Fatalf("abandonOrClaim on a fresh resume found an existing acceptance: %v", accepted)
	}
	before := harness.entry.expectedVersion()
	late := &turingv1.RuntimeApprovalResumeAccepted{
		RunId: resumeDeliveryRunID, ApprovalId: resumeDeliveryApproval,
		StateVersion: before + 1, AssignmentAttemptId: resumeDeliveryAttempt,
	}

	harness.worker.deliverResumeAcceptance(late)

	if got := harness.entry.expectedVersion(); got != before {
		t.Fatalf("run version after a rejected acceptance = %d, want unchanged %d", got, before)
	}
	if accepted, ok := harness.pending.abandonOrClaim(); ok {
		t.Fatalf("a rejected acceptance was still claimable: %v", accepted)
	}
}

// TestNoAcceptanceLeavesTheRunsVersionUnchanged is the companion case: a
// resume that never receives any acceptance at all — its wait simply expires
// — must leave the run's observed version exactly where it started. Nothing
// in the failing path has ever had reason to touch it, and this pins that
// down as an explicit invariant rather than an accident of what the failing
// path happens not to call.
func TestNoAcceptanceLeavesTheRunsVersionUnchanged(t *testing.T) {
	harness := newResumeDeliveryHarness(t)
	before := harness.entry.expectedVersion()

	err := harness.resume(time.Now().Add(50 * time.Millisecond))

	if !runWasTerminalized(err) {
		t.Fatalf("resume error = %v, want a terminal run error", err)
	}
	if got := harness.entry.expectedVersion(); got != before {
		t.Fatalf("run version after a resume with no acceptance = %d, want unchanged %d", got, before)
	}
}

// TestAcceptanceDeliveredBeforeTheDecisionCannotResumeTheRun proves the
// decision-select's failing branch is unconditional now that its claim branch
// is gone: using newUndecidedResumeDeliveryHarness so the "before decision"
// gap is exact rather than a race against the worker's own command loop,
// pending.decided never fires here, so the wait ends purely on its own
// deadline and a premature acceptance delivered into that gap must not have
// changed the outcome.
//
// Before this fix, resumeApproval's decision-select had its own claim branch
// that called abandonOrClaim directly on a wait-context timeout, without
// requiring the Ready to have ever been sent. That branch is what let a
// premature acceptance recorded here — dropped now, by recordAccepted's
// readyStarted gate — resume the run anyway once the deadline fired.
func TestAcceptanceDeliveredBeforeTheDecisionCannotResumeTheRun(t *testing.T) {
	harness := newUndecidedResumeDeliveryHarness(t)
	premature := &turingv1.RuntimeApprovalResumeAccepted{
		RunId: resumeDeliveryRunID, ApprovalId: resumeDeliveryApproval,
		StateVersion: resumeDeliveryVersion + 1, AssignmentAttemptId: resumeDeliveryAttempt,
	}

	harness.worker.deliverResumeAcceptance(premature)

	err := harness.resume(time.Now().Add(50 * time.Millisecond))

	if !runWasTerminalized(err) {
		t.Fatalf("resume error = %v, want a terminal run error: a premature acceptance must not have resumed the run", err)
	}
	assertTypedDeliveryFailure(t, harness.sentUpdates())
	if got := harness.entry.expectedVersion(); got != resumeDeliveryVersion {
		t.Fatalf("run version after a premature acceptance = %d, want unchanged %d", got, resumeDeliveryVersion)
	}
	if fatal := harness.fatalErr(); fatal != nil {
		t.Fatalf("a premature acceptance dropped the whole worker stream instead of failing the resume alone: %v", fatal)
	}
}

// TestAcceptanceDeliveredBeforeTheDecisionCannotSatisfyTheSubsequentReady is
// the scenario the readyStarted gate most directly defends in production, and
// a stricter one than the test above: the decision arrives AFTER the
// premature acceptance, so the Ready this resume goes on to send actually
// succeeds, and resumeApproval reaches its real final select — the one on
// pending.signal and waitCtx.Done(), not the decision-select's removed claim
// branch.
//
// Without the readyStarted gate, the premature acceptance recorded here would
// have closed pending.signal well before that final select is ever reached,
// so the select would find it already closed and claim it immediately —
// wrongly resuming the run, and adopting a version, on an acceptance that
// answers no Ready this process had sent yet. With the gate, the premature
// delivery is dropped, so the final select has nothing recorded and waits out
// its own deadline instead.
func TestAcceptanceDeliveredBeforeTheDecisionCannotSatisfyTheSubsequentReady(t *testing.T) {
	harness := newUndecidedResumeDeliveryHarness(t)
	premature := &turingv1.RuntimeApprovalResumeAccepted{
		RunId: resumeDeliveryRunID, ApprovalId: resumeDeliveryApproval,
		StateVersion: resumeDeliveryVersion + 1, AssignmentAttemptId: resumeDeliveryAttempt,
	}

	// Delivered before the decision: readyStarted is false, so recordAccepted
	// must refuse it rather than merely defer it.
	harness.worker.deliverResumeAcceptance(premature)

	// The decision arrives now, so resumeApproval's first select proceeds and
	// the Ready send that follows actually succeeds — reaching the exact
	// final select a surviving stale acceptance would otherwise have already
	// won before this resume ever asked for anything.
	harness.worker.markApprovalDecided(resumeDeliveryApproval)

	err := harness.resume(time.Now().Add(50 * time.Millisecond))

	if !runWasTerminalized(err) {
		t.Fatalf("resume error = %v, want a terminal run error: a premature acceptance must not satisfy the Ready this resume goes on to send", err)
	}
	// The Ready this resume sends here is real and legitimately reaches the
	// stream, so — unlike the "decision never arrives" test above — the
	// failure is the unacknowledged-resume fatal, not a typed pre-Ready
	// transport failure: the row may have been durably resumed for all this
	// process can tell, so the stream drops rather than reporting anything
	// about the run itself.
	fatal := harness.fatalErr()
	if !errors.Is(fatal, errApprovalResumeUnacknowledged) {
		t.Fatalf("fatal = %v, want %v: a premature acceptance must not have satisfied the Ready this resume actually sent", fatal, errApprovalResumeUnacknowledged)
	}
	if got := harness.entry.expectedVersion(); got != resumeDeliveryVersion {
		t.Fatalf("run version after a premature acceptance = %d, want unchanged %d", got, resumeDeliveryVersion)
	}
	waitForInactiveRun(t, harness.worker, resumeDeliveryRunID)
	for _, update := range harness.sentUpdates() {
		if update.GetRunFailed() != nil || update.GetRunCompleted() != nil || update.GetRunCancelledAck() != nil {
			t.Fatalf("the worker reported %+v for a run whose Ready may have been committed", update)
		}
	}
}

// ---------------------------------------------------------------------------
// A committed acceptance already in hand outranks a Ready send error or a
// simultaneously expiring wait.
//
// Both failure arms above are honest ONLY because, as far as this process can
// tell, nothing proves the orchestrator ever saw the Ready. That stops being
// true the instant an acceptance for this exact resume has already been
// recorded at its single commit point: the orchestrator cannot have produced
// that acceptance without having received and committed the Ready it names,
// so the send error or the expired wait are now moot. The tests below drive
// that acceptance in through the same barrier the delivery tests above use for
// a send failure - the fake stream's sendFn - so the ordering is forced rather
// than hoped for, and never through a sleep.
// ---------------------------------------------------------------------------

// TestBufferedAcceptanceOutranksReadySendError is the send-error half of the
// race the old code got wrong. The Ready's send fails from this process's own
// point of view, but the barrier below records the acceptance before that
// failure is returned to resumeApproval - exactly the shape of an
// acknowledgement racing back ahead of a local deadline or transport hiccup.
//
// Before the fix this reported errApprovalResumeUnacknowledged and dropped the
// stream. The narrower bug the fix also has to close: sendRunUpdateReportingPause
// pauses the run's outbound narration the instant the send returns an error,
// before resumeApproval ever learns the acceptance is about to overrule it, so
// simply returning nil here is not enough - the winning acceptance must also
// refresh the assignment and clear that pause, or a durably-resumed run is
// left permanently muted. The second half of this test proves that: it forces
// the stream to "reconnect" (a fresh writer over a fresh fake stream, exactly
// what a real reconnect looks like from this run's entry) and then sends a
// follow-up update through it. If the pause were still set, sendRunUpdate would
// silently swallow that update as errRunNarrationPaused and it would never
// reach the new stream at all.
func TestBufferedAcceptanceOutranksReadySendError(t *testing.T) {
	harness := newResumeDeliveryHarness(t)
	accepted := &turingv1.RuntimeApprovalResumeAccepted{
		RunId: resumeDeliveryRunID, ApprovalId: resumeDeliveryApproval,
		StateVersion: resumeDeliveryVersion + 1, AssignmentAttemptId: resumeDeliveryAttempt,
	}
	// The barrier: the acceptance is recorded before the send's error is ever
	// returned to resumeApproval.
	harness.stream.sendFn = func(update *turingv1.RuntimeUpdate) error {
		harness.started <- update
		if update.GetApprovalResumeReady() != nil {
			harness.pending.recordAccepted(accepted)
			return context.DeadlineExceeded
		}
		return nil
	}

	err := harness.resume(time.Now().Add(5 * time.Second))

	if err != nil {
		t.Fatalf("resume error = %v, want nil: a recorded acceptance is durable proof the Ready was committed", err)
	}
	if fatal := harness.fatalErr(); fatal != nil {
		t.Fatalf("a proven acceptance still dropped the whole worker stream: %v", fatal)
	}
	if entry := harness.worker.activeRun(resumeDeliveryRunID); entry == nil {
		t.Fatal("a proven acceptance forced the durably running run into recovery")
	}
	for _, update := range harness.sentUpdates() {
		if update.GetRunFailed() != nil || update.GetRunCancelledAck() != nil {
			t.Fatalf("worker reported %+v for a run whose Ready was durably accepted", update)
		}
	}
	if harness.entry.outboundPaused() {
		t.Fatal("a winning acceptance left the resumed run's outbound narration paused")
	}
	if got := harness.entry.expectedVersion(); got != resumeDeliveryVersion+1 {
		t.Fatalf("run version after a winning acceptance = %d, want the accepted %d", got, resumeDeliveryVersion+1)
	}

	// Simulate the stream reconnect that follows any send failure (the writer
	// that just returned context.DeadlineExceeded from stream.Send has already
	// stopped itself), and prove the run is not left silently muted on it.
	harness.worker.stopOutboundWriter()
	reconnected := newFakeStream()
	harness.worker.startOutboundWriter(reconnected)
	t.Cleanup(func() { harness.worker.stopOutboundWriter() })
	followUp := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Heartbeat{
		Heartbeat: &turingv1.RuntimeHeartbeat{WorkerId: "worker-resume-delivery"},
	}}
	if err := harness.worker.sendRunUpdate(context.Background(), harness.entry, reconnected, followUp); err != nil {
		t.Fatalf("follow-up send after a winning acceptance = %v, want nil", err)
	}
	select {
	case got := <-reconnected.sent:
		if got != followUp {
			t.Fatalf("follow-up update reached the new stream as %+v, want %+v", got, followUp)
		}
	default:
		t.Fatal("a winning acceptance left the run's narration silently dropped on the reconnect")
	}
}

// TestAcceptedRecordedBeforeTheWaitBeginsIsClaimedWithoutBlocking is the
// "pre-select" case a separate nonblocking peek used to exist for: an
// acceptance recorded while the Ready's send was still in flight, with nothing
// racing the wait context at all. There is no coincidence to force here - the
// send succeeds and the acceptance is on file before resumeApproval ever
// reaches its blocking select, so the select must claim it immediately rather
// than making the resume sit out a wait it has already won. A channel that is
// already closed by the time a select evaluates it is always the case chosen,
// so pending.signal already being closed is enough on its own — no separate
// peek is needed to avoid blocking here.
func TestAcceptedRecordedBeforeTheWaitBeginsIsClaimedWithoutBlocking(t *testing.T) {
	harness := newResumeDeliveryHarness(t)
	accepted := &turingv1.RuntimeApprovalResumeAccepted{
		RunId: resumeDeliveryRunID, ApprovalId: resumeDeliveryApproval,
		StateVersion: resumeDeliveryVersion + 1, AssignmentAttemptId: resumeDeliveryAttempt,
	}
	harness.stream.sendFn = func(update *turingv1.RuntimeUpdate) error {
		harness.started <- update
		if update.GetApprovalResumeReady() != nil {
			harness.pending.recordAccepted(accepted)
		}
		return nil
	}

	// A deadline far in the future: if the resume actually blocked waiting for
	// anything instead of claiming the already-recorded acceptance, this test
	// would hang rather than pass, which is exactly the point of using a
	// context this test itself never cancels.
	done := make(chan error, 1)
	go func() { done <- harness.resume(time.Now().Add(time.Hour)) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("resume error = %v, want nil: the acceptance was already on file", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resume blocked instead of claiming an acceptance recorded before the wait began")
	}
	if fatal := harness.fatalErr(); fatal != nil {
		t.Fatalf("a proven acceptance still dropped the whole worker stream: %v", fatal)
	}
	if harness.entry.outboundPaused() {
		t.Fatal("a winning acceptance left the resumed run's outbound narration paused")
	}
}

// TestBufferedAcceptanceOutranksSimultaneousDeadline covers the select the old
// code left to the scheduler: an acceptance recorded for a resume racing, at
// essentially the same instant, the cancellation of its own wait - the final
// select resumeApproval blocks on once the Ready has been sent.
//
// An earlier version of this test recorded the acceptance and cancelled the
// wait from inside the Ready's own sendFn, which reliably raced the send's
// own context down to a send-started error instead (see
// TestBufferedAcceptanceOutranksReadySendError for that path). A later
// version fixed that by waiting for the Ready to leave the writer first, but
// still issued the recording and the cancellation back to back from the
// test's own goroutine — sequential calls with no actual concurrency between
// them, which proves nothing about a race, only about whichever of the two
// the test itself chose to do first.
//
// This version puts each side on its own goroutine, released together from a
// shared barrier, so the two are actually contending for outcomeMu rather
// than being ordered by construction. That is the same commit point
// TestAcceptanceRacingAbandonmentIsLinearized exercises directly against the
// bare approvalResume; repeating it here, through the full resumeApproval
// call and its own select, additionally proves that contention surfaces
// correctly at that layer too — the resume must land on exactly one of the
// two outcomes, and the two sides racing for it must never observably
// disagree about which one won. Repeated many times to exercise both
// lock-acquisition orders; nothing here waits on a clock, only on channels and
// a WaitGroup.
func TestBufferedAcceptanceOutranksSimultaneousDeadline(t *testing.T) {
	const iterations = 100
	for attempt := 0; attempt < iterations; attempt++ {
		harness := newResumeDeliveryHarness(t)
		ctx, cancel := context.WithCancel(context.Background())
		accepted := &turingv1.RuntimeApprovalResumeAccepted{
			RunId: resumeDeliveryRunID, ApprovalId: resumeDeliveryApproval,
			StateVersion: resumeDeliveryVersion + 1, AssignmentAttemptId: resumeDeliveryAttempt,
		}

		result := make(chan error, 1)
		go func() { result <- harness.resumeWithContext(ctx, time.Time{}) }()

		// Wait for the Ready to actually leave the writer before racing
		// anything. This harness uses the fake stream's default Send (no
		// custom sendFn), so the update lands on stream.sent. Only past this
		// point has beginReadySend made the resume eligible to accept
		// anything at all, and only past this point is resumeApproval
		// provably about to enter (or already inside) its final select.
		select {
		case update := <-harness.stream.sent:
			if update.GetApprovalResumeReady() == nil {
				t.Fatalf("attempt %d: first update sent = %+v, want the approval Ready", attempt, update)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("attempt %d: the Ready never reached the stream", attempt)
		}

		// The barrier: two goroutines, released together, one recording the
		// acceptance directly on the pending resume and the other cancelling
		// the wait — a real race for outcomeMu rather than a sequence the
		// test itself imposed.
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			harness.pending.recordAccepted(accepted)
		}()
		go func() {
			defer wg.Done()
			<-start
			cancel()
		}()
		close(start)
		wg.Wait()

		err := <-result
		cancel()

		switch {
		case err == nil:
			if fatal := harness.fatalErr(); fatal != nil {
				t.Fatalf("attempt %d: resume succeeded but the worker still reported fatal: %v", attempt, fatal)
			}
			if entry := harness.worker.activeRun(resumeDeliveryRunID); entry == nil {
				t.Fatalf("attempt %d: resume succeeded but the run was released as if it had failed", attempt)
			}
			if harness.entry.outboundPaused() {
				t.Fatalf("attempt %d: a winning acceptance left the resumed run's outbound narration paused", attempt)
			}
		case runWasTerminalized(err):
			fatal := harness.fatalErr()
			if !errors.Is(fatal, errApprovalResumeUnacknowledged) {
				t.Fatalf("attempt %d: resume failed but fatal = %v, want %v", attempt, fatal, errApprovalResumeUnacknowledged)
			}
			waitForInactiveRun(t, harness.worker, resumeDeliveryRunID)
		default:
			t.Fatalf("attempt %d: resume error = %v, want either nil or a terminal run error", attempt, err)
		}
	}
}

// TestAcceptanceForAnotherApprovalCannotSatisfyResume proves the new priority
// check is still bound to the resume it belongs to. An acceptance recorded on
// a DIFFERENT approval's pending resume is not "an acceptance already in hand"
// for this one — deliverResumeAcceptance would never have routed it there for
// this approval in the first place — and a resume that never actually
// received its own acceptance must still fail exactly as before.
func TestAcceptanceForAnotherApprovalCannotSatisfyResume(t *testing.T) {
	harness := newResumeDeliveryHarness(t)
	const otherApproval = "approval_resume_delivery_other"
	harness.worker.rememberApproval(otherApproval, resumeDeliveryRunID)
	harness.worker.markApprovalDecided(otherApproval)
	harness.worker.mu.Lock()
	other := harness.worker.resumes[otherApproval]
	harness.worker.mu.Unlock()
	if other == nil {
		t.Fatal("no pending resume for the unrelated approval")
	}
	// A resume this far along would have already called beginReadySend
	// itself; simulating that here is what makes other.recordAccepted a
	// legitimate acceptance rather than one recordAccepted would refuse on
	// its own gate, keeping this test about approval-scoping and nothing else.
	other.beginReadySend()
	if !other.recordAccepted(&turingv1.RuntimeApprovalResumeAccepted{
		RunId: resumeDeliveryRunID, ApprovalId: otherApproval, StateVersion: resumeDeliveryVersion + 1,
		AssignmentAttemptId: resumeDeliveryAttempt,
	}) {
		t.Fatal("could not record the unrelated approval's acceptance")
	}

	err := harness.resume(time.Now().Add(50 * time.Millisecond))

	if !runWasTerminalized(err) {
		t.Fatalf("resume error = %v, want a terminal run error", err)
	}
	fatal := harness.fatalErr()
	if !errors.Is(fatal, errApprovalResumeUnacknowledged) {
		t.Fatalf("fatal = %v, want %v: an unrelated approval's acceptance must not satisfy this resume", fatal, errApprovalResumeUnacknowledged)
	}
	waitForInactiveRun(t, harness.worker, resumeDeliveryRunID)
	for _, update := range harness.sentUpdates() {
		if update.GetRunFailed() != nil || update.GetRunCompleted() != nil || update.GetRunCancelledAck() != nil {
			t.Fatalf("the worker reported %+v for a run whose Ready may have been committed", update)
		}
	}
}

// TestAcceptanceAfterAbandonmentCannotReviveTheResume drives a resume all the
// way to abandonment - a deadline that expires with no acceptance ever
// recorded - and then delivers a perfectly matching acceptance directly to the
// SAME pending resume object resumeApproval just abandoned. It must be
// dropped: once abandonOrClaim has committed to abandonment, the failing path
// may already be reporting fatal or sending a terminal update, and reviving
// the resume out from under that would contradict an outcome already being
// acted on.
func TestAcceptanceAfterAbandonmentCannotReviveTheResume(t *testing.T) {
	harness := newResumeDeliveryHarness(t)
	harness.blockWriter(t)
	deadline := time.Now().Add(100 * time.Millisecond)

	result := harness.resumeAsync(deadline)
	harness.awaitSpentBudget(t)
	harness.releaseWriter()
	err := harness.awaitResume(t, result)
	if !runWasTerminalized(err) {
		t.Fatalf("resume error = %v, want a terminal run error", err)
	}

	late := &turingv1.RuntimeApprovalResumeAccepted{
		RunId: resumeDeliveryRunID, ApprovalId: resumeDeliveryApproval,
		StateVersion: resumeDeliveryVersion + 1, AssignmentAttemptId: resumeDeliveryAttempt,
	}
	if harness.pending.recordAccepted(late) {
		t.Fatal("an acceptance recorded after abandonment was accepted instead of dropped")
	}
	// abandonOrClaim is the single commit point: calling it again after
	// abandonment must observe the same settled abandonment, not the late
	// acceptance that recordAccepted above just refused to record.
	if accepted, ok := harness.pending.abandonOrClaim(); ok {
		t.Fatalf("abandonOrClaim after abandonment = (%v, true), want (nil, false)", accepted)
	}
}

// TestApprovalResumeOutcomeCommitPoint pins the resume's single linearizable
// commit point directly: recordAccepted and abandonOrClaim share one lock and
// one outcome, so whichever of them runs first decides the resume's fate and
// the other only ever observes it. These are pure, sequential calls with no
// goroutines and nothing to race - the point is that the commit point itself
// is deterministic given an explicit order, which is what the concurrent test
// below then relies on under actual contention.
func TestApprovalResumeOutcomeCommitPoint(t *testing.T) {
	t.Run("nothing recorded, abandonOrClaim abandons and a later acceptance cannot revive it", func(t *testing.T) {
		pending := newApprovalResume("run_commit_abandon_first")
		pending.beginReadySend()
		accepted, ok := pending.abandonOrClaim()
		if ok || accepted != nil {
			t.Fatalf("abandonOrClaim with nothing recorded = (%v, %v), want (nil, false)", accepted, ok)
		}
		if pending.recordAccepted(&turingv1.RuntimeApprovalResumeAccepted{RunId: "run_commit_abandon_first"}) {
			t.Fatal("recordAccepted after abandonment reported success")
		}
		if again, ok := pending.abandonOrClaim(); ok {
			t.Fatalf("abandonOrClaim after abandonment claimed %v", again)
		}
	})

	t.Run("an acceptance recorded first is claimed instead of abandoned", func(t *testing.T) {
		pending := newApprovalResume("run_commit_accept_first")
		pending.beginReadySend()
		want := &turingv1.RuntimeApprovalResumeAccepted{RunId: "run_commit_accept_first"}
		if !pending.recordAccepted(want) {
			t.Fatal("recordAccepted on a pending resume reported failure")
		}
		got, ok := pending.abandonOrClaim()
		if !ok || got != want {
			t.Fatalf("abandonOrClaim after a recorded acceptance = (%v, %v), want (%v, true)", got, ok, want)
		}
		// Claiming it does not consume it: a second observer (a defensive
		// second call on the same code path, say) sees the same durable fact.
		if again, ok := pending.abandonOrClaim(); !ok || again != want {
			t.Fatalf("second abandonOrClaim = (%v, %v), want (%v, true)", again, ok, want)
		}
	})

	t.Run("a second acceptance is ignored once one is already recorded", func(t *testing.T) {
		pending := newApprovalResume("run_commit_duplicate")
		pending.beginReadySend()
		first := &turingv1.RuntimeApprovalResumeAccepted{RunId: "run_commit_duplicate", StateVersion: 1}
		second := &turingv1.RuntimeApprovalResumeAccepted{RunId: "run_commit_duplicate", StateVersion: 2}
		if !pending.recordAccepted(first) {
			t.Fatal("recordAccepted of the first acceptance reported failure")
		}
		if pending.recordAccepted(second) {
			t.Fatal("recordAccepted of a second acceptance reported success")
		}
		if got, ok := pending.abandonOrClaim(); !ok || got != first {
			t.Fatalf("abandonOrClaim after a duplicate = (%v, %v), want (%v, true)", got, ok, first)
		}
	})

	// An acceptance before the Ready send has started cannot satisfy the
	// resume, no matter what outcome is otherwise in play: recordAccepted's
	// readyStarted gate is what makes resumeApproval's own decision-select
	// failing branch provably unable to observe a recorded acceptance,
	// because beginReadySend only ever runs after that select returns.
	t.Run("an acceptance before the Ready send has started cannot satisfy the resume", func(t *testing.T) {
		pending := newApprovalResume("run_commit_ready_not_started")
		premature := &turingv1.RuntimeApprovalResumeAccepted{RunId: "run_commit_ready_not_started"}
		if pending.recordAccepted(premature) {
			t.Fatal("recordAccepted succeeded before the Ready send had started")
		}
		// abandonOrClaim is the same commit point recordAccepted uses, and it
		// abandons whatever is still pending here — so it confirms the
		// rejection above was final rather than merely a miss.
		if accepted, ok := pending.abandonOrClaim(); ok {
			t.Fatalf("abandonOrClaim found a premature acceptance claimable: %v", accepted)
		}
		// The same acceptance on a FRESH resume whose Ready send has already
		// started succeeds, proving the earlier rejection was about timing —
		// readyStarted — and not about the acceptance value itself.
		fresh := newApprovalResume("run_commit_ready_started_after")
		fresh.beginReadySend()
		if !fresh.recordAccepted(premature) {
			t.Fatal("recordAccepted failed once the Ready send had started")
		}
	})
}

// TestAcceptanceRacingAbandonmentIsLinearized stresses the same commit point
// under actual concurrency: one goroutine tries to record an acceptance while
// another tries to abandon the resume, released by a shared barrier so both
// start as close to simultaneously as the scheduler allows, repeated many
// times to exercise both lock-acquisition orders. Nothing here waits on a
// clock — the barrier is a channel close, and the assertion is an invariant
// that must hold no matter which goroutine's lock acquisition the runtime
// happens to schedule first: recording and abandoning must never both
// "succeed" for the same resume, and whichever one loses must observe exactly
// the winner's outcome rather than a torn or default one.
func TestAcceptanceRacingAbandonmentIsLinearized(t *testing.T) {
	const iterations = 500
	for i := 0; i < iterations; i++ {
		pending := newApprovalResume("run_commit_race")
		// The Ready send is already underway for every iteration of this
		// race: this test is about the recordAccepted/abandonOrClaim commit
		// point itself, not about the readyStarted gate that a separate test
		// covers on its own.
		pending.beginReadySend()
		accepted := &turingv1.RuntimeApprovalResumeAccepted{RunId: "run_commit_race", StateVersion: int64(i + 1)}
		start := make(chan struct{})
		var recordedOK bool
		var claimed *turingv1.RuntimeApprovalResumeAccepted
		var claimedOK bool
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			recordedOK = pending.recordAccepted(accepted)
		}()
		go func() {
			defer wg.Done()
			<-start
			claimed, claimedOK = pending.abandonOrClaim()
		}()
		close(start)
		wg.Wait()

		switch {
		case recordedOK && claimedOK:
			if claimed != accepted {
				t.Fatalf("iteration %d: abandonOrClaim claimed %v after a successful record of %v", i, claimed, accepted)
			}
		case !recordedOK && !claimedOK:
			// abandonOrClaim won the race first; the recording was correctly
			// refused and nothing was claimed.
		default:
			t.Fatalf("iteration %d: recordAccepted ok=%v, abandonOrClaim ok=%v — exactly one of these must be true together with the other, never split", i, recordedOK, claimedOK)
		}
		// Whichever won, the outcome is now fixed: neither call can ever
		// change what a later observer sees. abandonOrClaim is idempotent —
		// calling it again is a read, not a second commit — so it doubles as
		// that observer here.
		if got, ok := pending.abandonOrClaim(); recordedOK && claimedOK && (!ok || got != accepted) {
			t.Fatalf("iteration %d: abandonOrClaim after a settled acceptance = (%v, %v), want (%v, true)", i, got, ok, accepted)
		}
	}
}

// TestAcceptanceDeliveryRacingTheFailureCommitNeverProducesAMixedOutcome is the
// same race one layer up, through the worker's own deliverResumeAcceptance and
// resumeApproval's failing path, rather than the bare approvalResume methods.
// resumeApproval is parked in its final blocking select — proven by observing
// the Ready actually reach the stream, never by a sleep — and is then released
// by a barrier that fires a context cancellation and a durable delivery at
// once, from two separate goroutines. Repeating this many times exercises both
// lock-acquisition orders at the worker level; either the resume comes all the
// way up as an accepted success (unpaused, no fatal, run still active) or all
// the way up as the existing abandonment failure (fatal reported, run
// released) — and it must always be exactly one of those two, never a mix of
// both or neither.
func TestAcceptanceDeliveryRacingTheFailureCommitNeverProducesAMixedOutcome(t *testing.T) {
	const iterations = 100
	for i := 0; i < iterations; i++ {
		harness := newResumeDeliveryHarness(t)
		ctx, cancel := context.WithCancel(context.Background())
		accepted := &turingv1.RuntimeApprovalResumeAccepted{
			RunId: resumeDeliveryRunID, ApprovalId: resumeDeliveryApproval,
			StateVersion: resumeDeliveryVersion + 1, AssignmentAttemptId: resumeDeliveryAttempt,
		}

		result := make(chan error, 1)
		go func() { result <- harness.resumeWithContext(ctx, time.Time{}) }()

		// Wait for the Ready to actually reach the stream before racing
		// anything: only then is resumeApproval provably parked in its final
		// select rather than still somewhere earlier in the handshake. This
		// harness uses the fake stream's default Send (no custom sendFn), so
		// the update lands on stream.sent rather than harness.started.
		select {
		case update := <-harness.stream.sent:
			if update.GetApprovalResumeReady() == nil {
				t.Fatalf("iteration %d: first update sent = %+v, want the approval Ready", i, update)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: the Ready never reached the stream", i)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			cancel()
		}()
		go func() {
			defer wg.Done()
			<-start
			harness.worker.deliverResumeAcceptance(accepted)
		}()
		close(start)
		wg.Wait()

		err := harness.awaitResume(t, result)
		cancel()
		fatal := harness.fatalErr()
		active := harness.worker.activeRun(resumeDeliveryRunID) != nil

		switch {
		case err == nil:
			if fatal != nil {
				t.Fatalf("iteration %d: resume succeeded but the worker still reported fatal: %v", i, fatal)
			}
			if !active {
				t.Fatalf("iteration %d: resume succeeded but the run was released as if it had failed", i)
			}
			if harness.entry.outboundPaused() {
				t.Fatalf("iteration %d: a winning acceptance left the run's outbound narration paused", i)
			}
		case runWasTerminalized(err):
			if !errors.Is(fatal, errApprovalResumeUnacknowledged) {
				t.Fatalf("iteration %d: resume failed but fatal = %v, want %v", i, fatal, errApprovalResumeUnacknowledged)
			}
			waitForInactiveRun(t, harness.worker, resumeDeliveryRunID)
		default:
			t.Fatalf("iteration %d: resume error = %v, want either nil or a terminal run error", i, err)
		}
	}
}
