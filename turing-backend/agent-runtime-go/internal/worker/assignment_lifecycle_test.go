package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

// A run's terminal report and the orchestrator's same-attempt refresh race each
// other by construction: the refresh is sent the moment ownership is proven
// again, and the report is sent the moment execution ends. If the refresh lands
// after the run left the active map, the worker used to read it as a brand new
// assignment and start a SECOND executor for a run that had already reported
// how it ended — duplicating every side effect the second run performs, and
// producing a second terminal report the orchestrator then has to refuse.
//
// Sequencing here is by observation, not by waiting: the terminal update
// appearing on the wire proves the report was claimed and sent, and the next
// run starting proves the refresh ahead of it was fully processed.
func TestSameAttemptRefreshAfterTerminalReportStartsNoSecondExecutor(t *testing.T) {
	const runID = "run_terminalized"
	const attemptID = "attempt-terminal"
	stream := newFakeStream()
	executor := newRefreshProbeExecutor(runID)
	worker, stop := startScriptedWorker(t, executor, stream)
	defer stop()

	assignJob(t, stream, runID, attemptID, 3)
	_ = executor.waitForStart(t)
	emit := executor.firstEmit(t)
	if err := emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{
		RunCompleted: &turingv1.RuntimeRunCompleted{RunId: runID, AssistantMessageId: "msg_" + runID, Content: "done"},
	}}); err != nil {
		t.Fatalf("emit terminal: %v", err)
	}
	close(executor.release)
	if terminal := nextSent(t, stream); terminal.GetRunCompleted().GetRunId() != runID {
		t.Fatalf("terminal update = %+v, want the completion for %s", terminal, runID)
	}

	// The refresh the orchestrator sends after proving ownership: same attempt,
	// a version it has since committed. A second executor started here would
	// park and hold the worker's only slot.
	assignJob(t, stream, runID, attemptID, 5)
	// The probe then asks for that slot. Which of the two things it can say on
	// the wire — "started" or "busy" — is the whole assertion, and both are
	// positive observations rather than a wait for silence.
	assignJob(t, stream, refreshProbeRunID, "attempt-probe", 1)

	update := nextSent(t, stream)
	if failed := update.GetRunFailed(); failed != nil {
		t.Fatalf("the same-attempt refresh started a second executor and held the slot: %+v", failed)
	}
	if event := update.GetEvent(); event == nil || event.GetRunId() != refreshProbeRunID {
		t.Fatalf("next update = %+v, want the probe run's step event", update)
	}
	if got := executor.startCount(runID); got != 1 {
		t.Fatalf("executor started %d times for %s, want exactly once", got, runID)
	}
	if entry := worker.activeRun(runID); entry != nil {
		t.Fatalf("terminalized run %s is active again", runID)
	}
}

const refreshProbeRunID = "run_refresh_probe"

// refreshProbeExecutor makes a second executor for the same run impossible to
// miss: the duplicate parks instead of exiting, so it holds the worker's slot
// until the test is over rather than tidying up before the assertion runs.
type refreshProbeExecutor struct {
	firstRunID string
	mu         sync.Mutex
	starts     map[string]int
	started    chan *turingv1.AgentJob
	emitted    chan func(*turingv1.RuntimeUpdate) error
	release    chan struct{}
}

func newRefreshProbeExecutor(firstRunID string) *refreshProbeExecutor {
	return &refreshProbeExecutor{
		firstRunID: firstRunID,
		starts:     map[string]int{},
		started:    make(chan *turingv1.AgentJob, 4),
		emitted:    make(chan func(*turingv1.RuntimeUpdate) error, 1),
		release:    make(chan struct{}),
	}
}

func (e *refreshProbeExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error {
	e.mu.Lock()
	e.starts[job.GetRunId()]++
	count := e.starts[job.GetRunId()]
	e.mu.Unlock()
	e.started <- job
	if job.GetRunId() == e.firstRunID && count == 1 {
		e.emitted <- emit
		<-e.release
		return nil
	}
	if job.GetRunId() == refreshProbeRunID {
		// Says so on the wire, then parks so the slot stays taken.
		_ = emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
			RunId: refreshProbeRunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
		}}})
	}
	<-ctx.Done()
	return ctx.Err()
}

func (e *refreshProbeExecutor) firstEmit(t *testing.T) func(*turingv1.RuntimeUpdate) error {
	t.Helper()
	select {
	case emit := <-e.emitted:
		return emit
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first executor to start")
	}
	return nil
}

// waitForStart returns the next job this executor was handed. startRun inserts
// the run and clears its terminal memory synchronously before spawning the
// goroutine that calls Execute, so observing a start proves both already
// happened.
func (e *refreshProbeExecutor) waitForStart(t *testing.T) *turingv1.AgentJob {
	t.Helper()
	select {
	case job := <-e.started:
		return job
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an executor to start")
	}
	return nil
}

func (e *refreshProbeExecutor) startCount(runID string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.starts[runID]
}

// An executor can exit without reporting anything — a clean return with no
// terminal update, which is what a run that produced nothing looks like. That
// attempt is just as finished as one that reported, and the same refresh can
// still be in flight for it, so it has to be retired the same way. Tying the
// memory to the terminal report instead left this path unguarded, and the
// window it opened on the reporting paths — between the run leaving the active
// map and the report being claimed — is the same hole seen from the other side.
//
// entry.done is the barrier: the run goroutine closes it last, so waiting on it
// proves every cleanup step ahead of it already ran.
func TestExecutorExitWithoutATerminalReportStillRetiresTheAttempt(t *testing.T) {
	const runID = "run_silent_exit"
	const attemptID = "attempt-silent"
	stream := newFakeStream()
	executor := newRefreshProbeExecutor(runID)
	worker, stop := startScriptedWorker(t, executor, stream)
	defer stop()

	assignJob(t, stream, runID, attemptID, 3)
	_ = executor.waitForStart(t)
	_ = executor.firstEmit(t)
	entry := worker.activeRun(runID)
	if entry == nil {
		t.Fatal("assigned run is not active")
	}
	close(executor.release)
	<-entry.done

	assignJob(t, stream, runID, attemptID, 5)
	assignJob(t, stream, refreshProbeRunID, "attempt-probe", 1)

	update := nextSent(t, stream)
	if failed := update.GetRunFailed(); failed != nil {
		t.Fatalf("a refresh for an exited attempt started a second executor and held the slot: %+v", failed)
	}
	if event := update.GetEvent(); event == nil || event.GetRunId() != refreshProbeRunID {
		t.Fatalf("next update = %+v, want the probe run's step event", update)
	}
	if got := executor.startCount(runID); got != 1 {
		t.Fatalf("executor started %d times for %s, want exactly once", got, runID)
	}
}

// An orchestrator that names no attempt cannot be told a redelivery from a
// re-dispatch, so nothing may be suppressed on its behalf. Refusing there would
// strand every run such an orchestrator ever retries.
func TestAssignmentWithoutAnAttemptIDIsNeverSuppressed(t *testing.T) {
	const runID = "run_no_attempt"
	stream := newFakeStream()
	executor := newScriptedExecutor(nil)
	_, stop := startScriptedWorker(t, executor, stream, func(options *Options) {
		options.MaxConcurrentRuns = 2
	})
	defer stop()

	assignJob(t, stream, runID, "", 0)
	<-executor.started
	emit := <-executor.emitted
	if err := emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{
		RunFailed: &turingv1.RuntimeRunFailed{RunId: runID, Code: "runtime_error"},
	}}); err != nil {
		t.Fatalf("emit terminal: %v", err)
	}
	close(executor.release)
	if terminal := nextSent(t, stream); terminal.GetRunFailed().GetRunId() != runID {
		t.Fatalf("terminal update = %+v, want the failure for %s", terminal, runID)
	}

	assignJob(t, stream, runID, "", 0)
	select {
	case started := <-executor.started:
		if started.GetRunId() != runID {
			t.Fatalf("started run = %q, want %s", started.GetRunId(), runID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an attempt-less re-dispatch was suppressed, stranding the run")
	}
}

// A run that starts again has nothing left to remember, and the entry has to go
// rather than sit there naming an attempt nobody will mention again.
func TestStartingANewAttemptForgetsTheRunsTerminalMemory(t *testing.T) {
	const runID = "run_forgotten"
	stream := newFakeStream()
	// The second attempt parks, so the memory is observed while that attempt is
	// genuinely running rather than after it has retired itself again.
	executor := newRefreshProbeExecutor(runID)
	worker, stop := startScriptedWorker(t, executor, stream, func(options *Options) {
		options.MaxConcurrentRuns = 2
	})
	defer stop()

	assignJob(t, stream, runID, "attempt-first", 3)
	_ = executor.waitForStart(t)
	emit := executor.firstEmit(t)
	if err := emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{
		RunFailed: &turingv1.RuntimeRunFailed{RunId: runID, Code: "runtime_error"},
	}}); err != nil {
		t.Fatalf("emit terminal: %v", err)
	}
	close(executor.release)
	_ = nextSent(t, stream)
	if !worker.remembersTerminalAttempt(runID) {
		t.Fatal("a reported attempt was not remembered")
	}

	assignJob(t, stream, runID, "attempt-second", 7)
	if started := executor.waitForStart(t); started.GetAssignmentAttemptId() != "attempt-second" {
		t.Fatalf("started attempt %q, want attempt-second", started.GetAssignmentAttemptId())
	}
	if worker.remembersTerminalAttempt(runID) {
		t.Fatal("a run that started again still holds a terminal memory for an attempt nobody will mention")
	}
}

// remembersTerminalAttempt reports whether the ledger still holds this run.
func (w *Worker) remembersTerminalAttempt(runID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, exists := w.terminalAttempts[runID]
	return exists
}

// The suppression is scoped to the attempt that reported, not to the run. A new
// attempt is the orchestrator re-dispatching a run it decided to try again, and
// refusing it would strand that run forever.
func TestNewAttemptAfterTerminalReportStillStartsAnExecutor(t *testing.T) {
	const runID = "run_redispatched"
	stream := newFakeStream()
	executor := newScriptedExecutor(nil)
	_, stop := startScriptedWorker(t, executor, stream, func(options *Options) {
		options.MaxConcurrentRuns = 2
	})
	defer stop()

	assignJob(t, stream, runID, "attempt-first", 3)
	<-executor.started
	emit := <-executor.emitted
	if err := emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{
		RunFailed: &turingv1.RuntimeRunFailed{RunId: runID, Code: "runtime_error"},
	}}); err != nil {
		t.Fatalf("emit terminal: %v", err)
	}
	close(executor.release)
	if terminal := nextSent(t, stream); terminal.GetRunFailed().GetRunId() != runID {
		t.Fatalf("terminal update = %+v, want the failure for %s", terminal, runID)
	}

	assignJob(t, stream, runID, "attempt-second", 7)
	select {
	case started := <-executor.started:
		if started.GetRunId() != runID || started.GetAssignmentAttemptId() != "attempt-second" {
			t.Fatalf("started %q/%q, want the second attempt of %s",
				started.GetRunId(), started.GetAssignmentAttemptId(), runID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a re-dispatched run was refused because an earlier attempt had reported")
	}
}

// The terminal memory is bounded, and bounded oldest-first. Nothing reaps it on
// a timer, so the bound has to hold under a stream of terminalized runs rather
// than depending on one — and WHICH entry goes matters as much as how many: the
// newest entry is the one whose refresh is most likely still in flight, so
// evicting from that end would spend the memory on the runs that no longer need
// it and drop the ones that do.
func TestTerminalAttemptMemoryIsBoundedAndEvictsOldestFirst(t *testing.T) {
	worker := New(Options{WorkerID: "worker-bounded", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: newFakeStream()}, newScriptedExecutor(nil))
	const remembered = maxRememberedTerminalAttempts * 4
	runIDs := make([]string, 0, remembered)
	for index := range remembered {
		runID := fmt.Sprintf("run_%04d", index)
		runIDs = append(runIDs, runID)
		worker.rememberTerminalAttempt(&activeRun{runID: runID, attemptID: "attempt"})
	}
	worker.mu.Lock()
	held := len(worker.terminalAttempts)
	order := len(worker.terminalOrder)
	worker.mu.Unlock()
	if held > maxRememberedTerminalAttempts {
		t.Fatalf("remembered %d terminal attempts, want at most %d", held, maxRememberedTerminalAttempts)
	}
	if order != held {
		t.Fatalf("eviction order holds %d entries and the map holds %d; they must stay in step", order, held)
	}
	for _, runID := range runIDs[:remembered-maxRememberedTerminalAttempts] {
		if worker.remembersTerminalAttempt(runID) {
			t.Fatalf("the oldest entry %s survived eviction", runID)
		}
	}
	for _, runID := range runIDs[remembered-maxRememberedTerminalAttempts:] {
		if !worker.remembersTerminalAttempt(runID) {
			t.Fatalf("the recent entry %s was evicted ahead of an older one", runID)
		}
	}
}

// A run whose narration is paused has no way to reach the orchestrator, and a
// tool beacon is narration that expects an answer. Reporting the send as
// successful and then waiting for a decision nobody will send holds the
// executor until the tool timeout fires — with a side effect possibly pending
// behind it. The beacon has to fail immediately instead, with an error the
// runner already knows how to read as "never posted".
func TestPausedRunToolBeaconFailsInsteadOfWaitingForADecision(t *testing.T) {
	const runID = "run_paused_beacon"
	stream := newFakeStream()
	// Deliberately no outbound writer: if the paused check were removed, the
	// send would fail loudly rather than quietly appearing to work.
	worker := New(Options{WorkerID: "worker-paused-beacon", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, newScriptedExecutor(nil))
	entry := &activeRun{
		runID:     runID,
		cancel:    func(error) {},
		done:      make(chan struct{}),
		attemptID: "attempt-paused",
		version:   4,
	}
	entry.pauseOutbound()
	worker.mu.Lock()
	worker.active[runID] = entry
	worker.mu.Unlock()

	type result struct {
		decision *turingv1.ToolPolicyDecision
		err      error
	}
	results := make(chan result, 1)
	go func() {
		decision, err := worker.postToolBeacon(context.Background(), stream, &turingv1.ToolCallBeacon{
			RunId:      runID,
			ToolCallId: "call_paused",
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
			ToolName:   "system.write",
		})
		results <- result{decision: decision, err: err}
	}()

	var got result
	select {
	case got = <-results:
	case <-time.After(2 * time.Second):
		t.Fatal("a paused run's tool beacon waited for a decision that can never arrive")
	}
	if got.decision != nil {
		t.Fatalf("paused beacon returned the decision %+v", got.decision)
	}
	if !errors.Is(got.err, errRunNarrationPaused) {
		t.Fatalf("paused beacon returned %v, want the paused-narration sentinel", got.err)
	}
	if tools.BeaconWasPosted(got.err) {
		t.Fatal("paused beacon reported itself as posted, so the runner would report an after-beacon for it")
	}
	if got.err.Error() != errRunNarrationPaused.Error() {
		t.Fatalf("paused beacon error text = %q, want the bare sentinel with no run or tool identity", got.err.Error())
	}
	select {
	case update := <-stream.sent:
		t.Fatalf("paused run narrated a beacon anyway: %+v", update)
	default:
	}
	worker.decisionMu.Lock()
	waiters := len(worker.decisions)
	worker.decisionMu.Unlock()
	if waiters != 0 {
		t.Fatalf("paused beacon left %d decision waiters behind", waiters)
	}
	worker.mu.Lock()
	toolCalls := len(worker.toolCalls)
	worker.mu.Unlock()
	if toolCalls != 0 {
		t.Fatalf("paused beacon left %d tool call correlations behind", toolCalls)
	}
}

// A cancellation the orchestrator computed against a state this worker has
// already been told to leave must not stop a run the orchestrator has since
// moved forward; the one computed against the current state must.
func TestWorkerRejectsStaleRunCancelledAndObeysTheCurrentOne(t *testing.T) {
	const runID = "run_versioned_cancel"
	stream := newFakeStream()
	executor := newScriptedExecutor(nil)
	worker, stop := startScriptedWorker(t, executor, stream, func(options *Options) {
		options.MaxConcurrentRuns = 2
	})
	defer stop()

	assignJob(t, stream, runID, "attempt-cancel", 5)
	<-executor.started
	<-executor.emitted

	cancelRun(t, stream, runID, 3)
	// Once the following assignment is running, the recv loop has finished with
	// the stale cancellation ahead of it.
	assignJob(t, stream, "run_after_stale", "attempt-after", 1)
	select {
	case started := <-executor.started:
		if started.GetRunId() != "run_after_stale" {
			t.Fatalf("started run = %q, want run_after_stale", started.GetRunId())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the assignment after the stale cancellation")
	}
	entry := worker.activeRun(runID)
	if entry == nil {
		t.Fatal("a stale cancellation removed the run it named")
	}
	if entry.isStopping() {
		t.Fatal("a stale cancellation stopped the run it named")
	}
	if got := entry.expectedVersion(); got != 5 {
		t.Fatalf("worker version = %d, want the assignment's 5 after a stale command", got)
	}

	cancelRun(t, stream, runID, 6)
	ack := nextCancelledAck(t, stream, runID)
	if got := ack.GetObservedStateVersion(); got != 6 {
		t.Fatalf("ack observed version = %d, want the version the cancellation carried", got)
	}
}

func cancelRun(t *testing.T, stream *fakeStream, runID string, version int64) {
	t.Helper()
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunCancelled{
		RunCancelled: &turingv1.RuntimeRunCancelled{
			RunId: runID, Reason: "client_cancelled", StateVersion: version,
		},
	}}
}

// nextCancelledAck reads updates until the acknowledgement for runID arrives, so
// an unrelated run's traffic on the same stream cannot make the assertion
// order-dependent.
func nextCancelledAck(t *testing.T, stream *fakeStream, runID string) *turingv1.RuntimeCancelledAck {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case update := <-stream.sent:
			if ack := update.GetRunCancelledAck(); ack != nil && ack.GetRunId() == runID {
				return ack
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the cancellation acknowledgement for %s", runID)
		}
	}
}
