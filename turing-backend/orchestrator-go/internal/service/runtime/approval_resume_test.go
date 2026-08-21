package runtime

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ---------------------------------------------------------------------------
// The approval Ready/Accepted handshake.
//
// Every test below drives the real worker stream through an explicit gate: a
// command send is blocked, the durable row is read while it is blocked, and
// only then is the send released or failed. Nothing here waits on a clock,
// because what is being proved is an ordering — the row commits before the
// worker is told, and a worker that is never told never sees the row revert.
// ---------------------------------------------------------------------------

// approvalResumeStream is a worker stream whose command delivery a test owns.
//
// gate blocks the first matching command until the test releases it, and fail
// makes that command's delivery fail the way a dead connection does. Both are
// installed before the command can be produced, so no test has to hope it got
// there first.
type approvalResumeStream struct {
	grpc.ServerStream
	ctx      context.Context
	updates  chan *turingv1.RuntimeUpdate
	commands chan *turingv1.RuntimeCommand

	mu       sync.Mutex
	match    func(*turingv1.RuntimeCommand) bool
	gate     chan struct{}
	entered  chan *turingv1.RuntimeCommand
	failWith error
}

func newApprovalResumeStream(ctx context.Context, workerID string) *approvalResumeStream {
	stream := &approvalResumeStream{
		ctx:      ctx,
		updates:  make(chan *turingv1.RuntimeUpdate, 8),
		commands: make(chan *turingv1.RuntimeCommand, 16),
	}
	stream.updates <- workerReady(workerID)
	return stream
}

// gateCommand arms the stream to stop at the next matching command. The
// returned channels report the arrival and release it.
func (s *approvalResumeStream) gateCommand(match func(*turingv1.RuntimeCommand) bool) (<-chan *turingv1.RuntimeCommand, chan<- struct{}) {
	entered := make(chan *turingv1.RuntimeCommand, 1)
	gate := make(chan struct{})
	s.mu.Lock()
	s.match, s.entered, s.gate, s.failWith = match, entered, gate, nil
	s.mu.Unlock()
	return entered, gate
}

// failCommand arms the stream to fail the next matching command's delivery.
func (s *approvalResumeStream) failCommand(match func(*turingv1.RuntimeCommand) bool, cause error) <-chan *turingv1.RuntimeCommand {
	entered := make(chan *turingv1.RuntimeCommand, 1)
	s.mu.Lock()
	s.match, s.entered, s.gate, s.failWith = match, entered, nil, cause
	s.mu.Unlock()
	return entered
}

func (s *approvalResumeStream) Send(command *turingv1.RuntimeCommand) error {
	s.mu.Lock()
	match, entered, gate, failWith := s.match, s.entered, s.gate, s.failWith
	if match != nil && match(command) {
		s.match, s.entered, s.gate, s.failWith = nil, nil, nil, nil
	} else {
		match = nil
	}
	s.mu.Unlock()
	if match != nil {
		select {
		case entered <- command:
		default:
		}
		if gate != nil {
			select {
			case <-gate:
			case <-s.ctx.Done():
				return s.ctx.Err()
			}
		}
		if failWith != nil {
			return failWith
		}
	}
	select {
	case s.commands <- command:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *approvalResumeStream) Recv() (*turingv1.RuntimeUpdate, error) {
	select {
	case update := <-s.updates:
		return update, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *approvalResumeStream) Context() context.Context { return s.ctx }

// approvalResumeFixture is one worker holding one run that is waiting for an
// approval decision it has already been told about.
type approvalResumeFixture struct {
	h          *harness
	workerID   string
	runID      string
	sessionID  string
	approvalID string
	job        *turingv1.AgentJob
	stream     *approvalResumeStream
	done       chan error
	cancel     context.CancelFunc
	waiting    repository.RunState
	exited     bool
}

func newApprovalResumeFixture(t *testing.T, h *harness, workerID string, content string) *approvalResumeFixture {
	t.Helper()
	enqueued := h.enqueueRun(t, content)
	fixture := connectApprovalResumeWorker(t, h, workerID)
	fixture.runID = enqueued.RunID
	fixture.sessionID = enqueued.SessionID
	fixture.job = approvalResumeCommand(t, fixture.stream, func(cmd *turingv1.RuntimeCommand) bool {
		job := cmd.GetRunAssigned()
		return job != nil && job.GetRunId() == enqueued.RunID
	}).GetRunAssigned()
	fixture.approvalID = fixture.awaitApproval(t)
	fixture.waiting = h.runState(t, enqueued.RunID)
	if fixture.waiting.Lifecycle != waitingApprovalRunStatus {
		t.Fatalf("run lifecycle = %q, want %q", fixture.waiting.Lifecycle, waitingApprovalRunStatus)
	}
	return fixture
}

// connectApprovalResumeWorker starts one worker stream and waits for its
// acceptance, so every fixture below begins from a registered worker.
func connectApprovalResumeWorker(t *testing.T, h *harness, workerID string) *approvalResumeFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stream := newApprovalResumeStream(ctx, workerID)
	done := make(chan error, 1)
	go func() { done <- h.service.ConnectWorker(stream) }()
	fixture := &approvalResumeFixture{h: h, workerID: workerID, stream: stream, done: done, cancel: cancel}
	t.Cleanup(func() {
		cancel()
		if fixture.exited {
			return
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("worker stream did not exit")
		}
	})
	approvalResumeCommand(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetWorkerAccepted() != nil
	})
	return fixture
}

// awaitExit waits for the worker stream to end and records that it has, so the
// fixture's cleanup does not wait for an exit it can never observe twice.
func (f *approvalResumeFixture) awaitExit(t *testing.T) error {
	t.Helper()
	select {
	case err := <-f.done:
		f.exited = true
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("worker stream did not exit")
	}
	return nil
}

// awaitApproval puts the run where an approved decision leaves it: still
// waiting approval, because the decision is durable but the worker has not yet
// proven it restored the paused attempt.
func (f *approvalResumeFixture) awaitApproval(t *testing.T) string {
	t.Helper()
	approval, _, err := f.h.repo.CreateApprovalWithEvent(context.Background(), f.runID, "", "general_assistant",
		"files.create", `{"path":"notes.txt"}`, "sha256:approval-resume", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("CreateApprovalWithEvent: %v", err)
	}
	if _, err := f.h.repo.ApproveApproval(context.Background(), approval.ApprovalID, "approval-token", sql.NullString{}, ""); err != nil {
		t.Fatalf("ApproveApproval: %v", err)
	}
	return approval.ApprovalID
}

func (f *approvalResumeFixture) connectedWorker(t *testing.T) *worker {
	t.Helper()
	connected := f.h.service.registeredWorker(f.workerID)
	if connected == nil {
		t.Fatalf("worker %q is not registered", f.workerID)
	}
	return connected
}

func (f *approvalResumeFixture) ready() *turingv1.RuntimeApprovalResumeReady {
	return &turingv1.RuntimeApprovalResumeReady{
		RunId:                f.runID,
		ApprovalId:           f.approvalID,
		ExpectedStateVersion: f.waiting.StateVersion,
		AssignmentAttemptId:  f.job.GetAssignmentAttemptId(),
	}
}

func (f *approvalResumeFixture) sendReady(t *testing.T, ready *turingv1.RuntimeApprovalResumeReady) {
	t.Helper()
	select {
	case f.stream.updates <- &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_ApprovalResumeReady{ApprovalResumeReady: ready},
	}:
	case <-time.After(time.Second):
		t.Fatal("timed out queueing approval resume ready")
	}
}

// approvalResumeCommand waits for the next command matching want.
func approvalResumeCommand(t *testing.T, stream *approvalResumeStream, want func(*turingv1.RuntimeCommand) bool) *turingv1.RuntimeCommand {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case command := <-stream.commands:
			if want(command) {
				return command
			}
		case <-deadline:
			t.Fatal("timed out waiting for worker command")
		}
	}
}

func approvalResumeStateEvents(t *testing.T, h *harness, runID string) int {
	t.Helper()
	return countRunEvents(t, h, runID, "agent.run.state_changed")
}

// TestApprovalDecisionDeliveryFailureBeforeReadyKeepsWaitingWhileOwned proves
// the one thing the old code left implicit: persisting the decision, minting
// its token, and queueing the command are all things the orchestrator does to
// itself. None of them is the worker saying it is ready, so failing to hand the
// decision over cannot move the run, and while this attempt still owns the run
// the honest answer stays waiting-approval at the version it already committed.
func TestApprovalDecisionDeliveryFailureBeforeReadyKeepsWaitingWhileOwned(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-decision-owned", "decision delivery fails while owned")
	before := h.runState(t, fixture.runID)
	events := approvalResumeStateEvents(t, h, fixture.runID)

	if err := h.service.handleUndeliveredCommand(context.Background(), &turingv1.RuntimeCommand{
		Command: &turingv1.RuntimeCommand_ApprovalUpdated{ApprovalUpdated: &turingv1.RuntimeApprovalUpdated{
			ApprovalId: fixture.approvalID, Status: "approved", ApprovalToken: "token", StateVersion: before.StateVersion,
		}},
	}, fixture.workerID, fixture.connectedWorker(t)); err != nil {
		t.Fatalf("handleUndeliveredCommand: %v", err)
	}

	after := h.runState(t, fixture.runID)
	if after != before {
		t.Fatalf("undelivered decision changed the run: %+v, want %+v", after, before)
	}
	if got := approvalResumeStateEvents(t, h, fixture.runID); got != events {
		t.Fatalf("undelivered decision appended %d state events", got-events)
	}
}

// TestApprovalDecisionDeliveryFailureWithUncertainOwnerEntersRecovering is the
// other half: once nobody can prove they still own the attempt, a row that goes
// on saying "waiting for your answer" is describing a conversation that has no
// second party. It takes the same ownership-loss fence every other lost worker
// takes.
func TestApprovalDecisionDeliveryFailureWithUncertainOwnerEntersRecovering(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-decision-uncertain", "decision delivery fails with uncertain owner")
	before := h.runState(t, fixture.runID)
	events := approvalResumeStateEvents(t, h, fixture.runID)
	connected := fixture.connectedWorker(t)
	// Ownership is no longer provable: this attempt is not held by any live
	// registration, which is exactly what a lost worker looks like.
	connected.releaseRun(fixture.runID)
	// A command only goes undelivered because its stream failed, so the context
	// this path is handed is usually already dead. The fence must not depend on
	// it.
	dead, cancelDead := context.WithCancel(context.Background())
	cancelDead()

	if err := h.service.handleUndeliveredCommand(dead, &turingv1.RuntimeCommand{
		Command: &turingv1.RuntimeCommand_ApprovalUpdated{ApprovalUpdated: &turingv1.RuntimeApprovalUpdated{
			ApprovalId: fixture.approvalID, Status: "approved", ApprovalToken: "token", StateVersion: before.StateVersion,
		}},
	}, fixture.workerID, connected); err != nil {
		t.Fatalf("handleUndeliveredCommand: %v", err)
	}

	after := h.runState(t, fixture.runID)
	if after.Lifecycle != recoveringRunStatus {
		t.Fatalf("lifecycle = %q, want %q", after.Lifecycle, recoveringRunStatus)
	}
	if after.StateVersion != before.StateVersion+1 {
		t.Fatalf("state version = %d, want %d", after.StateVersion, before.StateVersion+1)
	}
	if got := approvalResumeStateEvents(t, h, fixture.runID); got != events+1 {
		t.Fatalf("fence appended %d state events, want exactly 1", got-events)
	}
}

// TestApprovalReadyCommitsRunningBeforeAccepted pins the order the whole
// protocol depends on. Accepted names a commit that already happened; if it
// were sent first, a delivery failure would leave the orchestrator promising a
// resume it never durably made.
func TestApprovalReadyCommitsRunningBeforeAccepted(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-ready-commits", "ready commits before accepted")
	before := h.runState(t, fixture.runID)
	events := approvalResumeStateEvents(t, h, fixture.runID)
	entered, release := fixture.stream.gateCommand(func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetApprovalResumeAccepted() != nil
	})

	fixture.sendReady(t, fixture.ready())

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the accepted command to be sent")
	}
	committed := h.runState(t, fixture.runID)
	if committed.Lifecycle != runningRunStatus {
		t.Fatalf("lifecycle while accepted is undelivered = %q, want %q", committed.Lifecycle, runningRunStatus)
	}
	if committed.StateVersion != before.StateVersion+1 {
		t.Fatalf("state version = %d, want %d", committed.StateVersion, before.StateVersion+1)
	}
	if got := approvalResumeStateEvents(t, h, fixture.runID); got != events+1 {
		t.Fatalf("ready appended %d state events, want exactly 1", got-events)
	}
	close(release)

	accepted := approvalResumeCommand(t, fixture.stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetApprovalResumeAccepted() != nil
	}).GetApprovalResumeAccepted()
	if accepted.GetRunId() != fixture.runID || accepted.GetApprovalId() != fixture.approvalID {
		t.Fatalf("accepted identity = %+v, want run %q approval %q", accepted, fixture.runID, fixture.approvalID)
	}
	if accepted.GetAssignmentAttemptId() != fixture.job.GetAssignmentAttemptId() {
		t.Fatalf("accepted attempt = %q, want %q", accepted.GetAssignmentAttemptId(), fixture.job.GetAssignmentAttemptId())
	}
	if accepted.GetStateVersion() != committed.StateVersion {
		t.Fatalf("accepted version = %d, want the committed %d", accepted.GetStateVersion(), committed.StateVersion)
	}
}

// TestLostAcceptedSameAttemptReadyReplaysExactResponse covers the worker that
// never saw an Accepted the orchestrator successfully sent. Its only honest
// move is to ask again, and the only honest answer is the same acceptance —
// not a second transition, and not a refusal that would strand a run the
// orchestrator has already resumed.
func TestLostAcceptedSameAttemptReadyReplaysExactResponse(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-lost-accepted", "lost accepted replays")
	fixture.sendReady(t, fixture.ready())
	first := approvalResumeCommand(t, fixture.stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetApprovalResumeAccepted() != nil
	}).GetApprovalResumeAccepted()
	committed := h.runState(t, fixture.runID)
	events := approvalResumeStateEvents(t, h, fixture.runID)

	fixture.sendReady(t, fixture.ready())

	second := approvalResumeCommand(t, fixture.stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetApprovalResumeAccepted() != nil
	}).GetApprovalResumeAccepted()
	if second.GetRunId() != first.GetRunId() ||
		second.GetApprovalId() != first.GetApprovalId() ||
		second.GetAssignmentAttemptId() != first.GetAssignmentAttemptId() ||
		second.GetStateVersion() != first.GetStateVersion() {
		t.Fatalf("replayed accepted = %+v, want %+v", second, first)
	}
	after := h.runState(t, fixture.runID)
	if after != committed {
		t.Fatalf("replayed ready changed the run: %+v, want %+v", after, committed)
	}
	if got := approvalResumeStateEvents(t, h, fixture.runID); got != events {
		t.Fatalf("replayed ready appended %d state events", got-events)
	}
}

// The four fencing tests below ask the Ready handler itself, because what they
// are about is what it must refuse to write. A refused Ready still closes its
// stream, and the fence that follows is a different transition with a different
// cause — covered on its own further down.
func assertReadyIsFenced(t *testing.T, fixture *approvalResumeFixture, ready *turingv1.RuntimeApprovalResumeReady) {
	t.Helper()
	before := fixture.h.runState(t, fixture.runID)
	events := approvalResumeStateEvents(t, fixture.h, fixture.runID)

	accepted, err := fixture.h.service.resumeApprovedRun(context.Background(), ready, fixture.workerID, fixture.connectedWorker(t))

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("conflicting ready error = %v, want FailedPrecondition", err)
	}
	if accepted != nil {
		t.Fatalf("conflicting ready returned %+v, want no acceptance", accepted)
	}
	after := fixture.h.runState(t, fixture.runID)
	if after != before {
		t.Fatalf("conflicting ready changed the run: %+v, want %+v", after, before)
	}
	if got := approvalResumeStateEvents(t, fixture.h, fixture.runID); got != events {
		t.Fatalf("conflicting ready appended %d state events", got-events)
	}
}

func TestReadyWithConflictingApprovalIsFenced(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-conflicting-approval", "ready names another approval")
	other := h.createRunningRunResult(t, "another run's approval")
	foreign, _, err := h.repo.CreateApprovalWithEvent(context.Background(), other.RunID, "", "general_assistant",
		"files.create", `{}`, "sha256:foreign", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("CreateApprovalWithEvent: %v", err)
	}

	ready := fixture.ready()
	ready.ApprovalId = foreign.ApprovalID
	assertReadyIsFenced(t, fixture, ready)
}

func TestReadyWithConflictingWorkerIsFenced(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-conflicting-owner", "ready arrives on another worker's stream")
	intruder := connectApprovalResumeWorker(t, h, "worker-conflicting-intruder")
	intruder.runID = fixture.runID
	intruder.approvalID = fixture.approvalID
	intruder.job = fixture.job
	intruder.waiting = fixture.waiting

	assertReadyIsFenced(t, intruder, fixture.ready())
}

func TestReadyWithConflictingAttemptIsFenced(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-conflicting-attempt", "ready names another attempt")

	ready := fixture.ready()
	ready.AssignmentAttemptId = "attempt_from_a_fenced_predecessor"
	assertReadyIsFenced(t, fixture, ready)
}

func TestReadyWithConflictingExpectedVersionIsFenced(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-conflicting-version", "ready names a version the run left")

	ready := fixture.ready()
	ready.ExpectedStateVersion = fixture.waiting.StateVersion + 3
	assertReadyIsFenced(t, fixture, ready)
}

// TestReadyGateRefusalKeepsItsOwnKind pins the two refusals the update gate can
// produce, and keeps them apart.
//
// The gate says one of two different things. "This worker does not hold that
// run" is a precondition the Ready itself violated, and the handshake's own
// FailedPrecondition is the right answer. "This worker is disconnected" is not
// about the Ready at all — the stream is already going, and its teardown
// reports that same cancellation from the other side. Relabelling it as a
// precondition failure makes the handler's outcome depend on which goroutine
// noticed the disconnect first, and buries a cancellation the caller can
// recognise inside an error that looks like a protocol violation.
//
// Both inputs are taken from the gate itself rather than written out here, so
// the mapping cannot drift away from what it maps.
func TestReadyGateRefusalKeepsItsOwnKind(t *testing.T) {
	ready := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ApprovalResumeReady{
		ApprovalResumeReady: &turingv1.RuntimeApprovalResumeReady{RunId: "run_ready_gate"},
	}}

	unassigned := &worker{assignments: map[string]assignment{}, done: make(chan struct{})}
	if _, err := unassigned.beginUpdate(ready); err == nil {
		t.Fatal("the gate admitted a Ready for a run this worker does not hold")
	} else if got := approvalResumeGateError(err); status.Code(got) != codes.FailedPrecondition {
		t.Fatalf("unassigned refusal = %v, want FailedPrecondition", got)
	}

	disconnected := &worker{
		assignments: map[string]assignment{"run_ready_gate": {runID: "run_ready_gate"}},
		done:        make(chan struct{}),
	}
	disconnected.close()
	refused, err := disconnected.beginUpdate(ready)
	if refused != nil || err == nil {
		t.Fatal("the gate admitted a Ready on a disconnected worker")
	}
	mapped := approvalResumeGateError(err)
	if status.Code(mapped) != codes.Canceled {
		t.Fatalf("disconnected refusal = %v, want the gate's own Canceled", mapped)
	}
	if !errors.Is(mapped, err) {
		t.Fatalf("disconnected refusal = %v, want the gate's own error %v", mapped, err)
	}
}

// TestDetectedAcceptedDeliveryFailureMovesRunningToRecovering is the case the
// commit ordering creates: the row is already running and the worker was never
// told. Reverting to waiting-approval would be a lie about a decision that has
// been made, so the run takes the same ownership-loss fence as any other worker
// the orchestrator can no longer reach.
func TestDetectedAcceptedDeliveryFailureMovesRunningToRecovering(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-accepted-undelivered", "accepted delivery fails after commit")
	before := h.runState(t, fixture.runID)
	events := approvalResumeStateEvents(t, h, fixture.runID)
	entered := fixture.stream.failCommand(func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetApprovalResumeAccepted() != nil
	}, errors.New("accepted send failed"))

	fixture.sendReady(t, fixture.ready())

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the accepted command to be sent")
	}
	if err := fixture.awaitExit(t); err == nil {
		t.Fatal("worker stream survived an undelivered acceptance")
	}
	after := h.runState(t, fixture.runID)
	if after.Lifecycle != recoveringRunStatus {
		t.Fatalf("lifecycle = %q, want %q", after.Lifecycle, recoveringRunStatus)
	}
	if after.StateVersion != before.StateVersion+2 {
		t.Fatalf("state version = %d, want %d (ready then fence)", after.StateVersion, before.StateVersion+2)
	}
	if got := approvalResumeStateEvents(t, h, fixture.runID); got != events+2 {
		t.Fatalf("ready and fence appended %d state events, want exactly 2", got-events)
	}
}

// TestUnobservedAcceptedThenOwnershipLossMovesRunningToRecovering is the same
// destination reached the other way: delivery reported success, the worker
// still never acted on it, and the loss is only discovered when the stream
// goes. Nothing special happens for approvals here on purpose — it is the
// common fence.
func TestUnobservedAcceptedThenOwnershipLossMovesRunningToRecovering(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-accepted-unobserved", "accepted delivered but never observed")
	before := h.runState(t, fixture.runID)
	events := approvalResumeStateEvents(t, h, fixture.runID)

	fixture.sendReady(t, fixture.ready())
	approvalResumeCommand(t, fixture.stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetApprovalResumeAccepted() != nil
	})
	resumed := h.runState(t, fixture.runID)
	if resumed.Lifecycle != runningRunStatus {
		t.Fatalf("lifecycle after ready = %q, want %q", resumed.Lifecycle, runningRunStatus)
	}

	fixture.cancel()
	fixture.awaitExit(t)

	waitForRunLifecycle(t, h, fixture.runID, recoveringRunStatus)
	after := h.runState(t, fixture.runID)
	if after.StateVersion != before.StateVersion+2 {
		t.Fatalf("state version = %d, want %d (ready then fence)", after.StateVersion, before.StateVersion+2)
	}
	if got := approvalResumeStateEvents(t, h, fixture.runID); got != events+2 {
		t.Fatalf("ready and fence appended %d state events, want exactly 2", got-events)
	}
}

// TestConflictingReadyClosesStreamFencesRecoveryAndReleasesSlot covers the
// stream-level consequence of a Ready nobody can honour: the offending stream
// goes, the run it was holding takes the ownership fence rather than staying
// frozen mid-handshake, and the worker slot is released instead of being left
// paused forever waiting for an acceptance that will never come.
func TestConflictingReadyClosesStreamFencesRecoveryAndReleasesSlot(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-conflicting-stream", "conflicting ready closes the stream")
	fixture.sendReady(t, fixture.ready())
	approvalResumeCommand(t, fixture.stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetApprovalResumeAccepted() != nil
	})
	resumed := h.runState(t, fixture.runID)
	events := approvalResumeStateEvents(t, h, fixture.runID)

	conflicting := fixture.ready()
	conflicting.AssignmentAttemptId = "attempt_that_never_owned_this_run"
	fixture.sendReady(t, conflicting)

	if err := fixture.awaitExit(t); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("worker stream error = %v, want FailedPrecondition", err)
	}
	after := h.runState(t, fixture.runID)
	if after.Lifecycle != recoveringRunStatus {
		t.Fatalf("lifecycle = %q, want %q", after.Lifecycle, recoveringRunStatus)
	}
	if after.StateVersion != resumed.StateVersion+1 {
		t.Fatalf("state version = %d, want %d (fence only)", after.StateVersion, resumed.StateVersion+1)
	}
	if got := approvalResumeStateEvents(t, h, fixture.runID); got != events+1 {
		t.Fatalf("conflicting ready appended %d state events, want exactly the fence", got-events)
	}
	if h.service.registeredWorker(fixture.workerID) != nil {
		t.Fatal("worker registration survived a conflicting ready")
	}
}

// siblingApproval records a second approval on the same run — the ordinary
// shape of a model asking for two tools — and optionally answers it. The run
// does not move: it was already waiting, and a second request or decision is
// news about an approval rather than a lifecycle change.
func (f *approvalResumeFixture) siblingApproval(t *testing.T, decided bool) string {
	t.Helper()
	approval, _, err := f.h.repo.CreateApprovalWithEvent(context.Background(), f.runID, "", "general_assistant",
		"files.update", `{"path":"sibling.txt"}`, "sha256:approval-resume-sibling",
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("sibling CreateApprovalWithEvent: %v", err)
	}
	if decided {
		if _, err := f.h.repo.ApproveApproval(context.Background(), approval.ApprovalID, "sibling-token", sql.NullString{}, ""); err != nil {
			t.Fatalf("sibling ApproveApproval: %v", err)
		}
	}
	if state := f.h.runState(t, f.runID); state != f.waiting {
		t.Fatalf("the sibling approval moved the run: %+v, want %+v", state, f.waiting)
	}
	return approval.ApprovalID
}

// TestReadyForASecondSameRunApprovalAfterCommitIsFenced is the case a run with
// two outstanding authorizations produces, and the one nothing but the
// approval's own identity can tell apart.
//
// Both Readys name this run, this worker, this attempt and the same waiting
// version, and both arrive at a row that is already running one version on.
// Every guard the transition has in common with a replay therefore matches. If
// the approval that actually moved the run is not part of what is compared, the
// second Ready is answered with an acceptance and the worker proceeds to make a
// call the orchestrator never resumed it for.
func TestReadyForASecondSameRunApprovalAfterCommitIsFenced(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-second-approval", "a second approval on the same run")
	sibling := fixture.siblingApproval(t, true)
	if _, err := h.service.resumeApprovedRun(context.Background(), fixture.ready(), fixture.workerID, fixture.connectedWorker(t)); err != nil {
		t.Fatalf("the first resume was refused: %v", err)
	}

	ready := fixture.ready()
	ready.ApprovalId = sibling
	assertReadyIsFenced(t, fixture, ready)
}

// TestReadyNamingAnUndecidedApprovalIsFenced covers the approval nobody has
// answered. Its row exists and belongs to this run, so ownership proves
// nothing; what is missing is the only thing that authorizes a resume at all.
// Restarting the run here would be the orchestrator acting on a question it
// asked and never got an answer to.
func TestReadyNamingAnUndecidedApprovalIsFenced(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-undecided-approval", "ready names an undecided approval")
	undecided := fixture.siblingApproval(t, false)

	ready := fixture.ready()
	ready.ApprovalId = undecided
	assertReadyIsFenced(t, fixture, ready)

	if state := h.runState(t, fixture.runID); state.Lifecycle != waitingApprovalRunStatus {
		t.Fatalf("lifecycle after an undecided ready = %q, want %q", state.Lifecycle, waitingApprovalRunStatus)
	}
}

// TestSecondSameRunApprovalReadyDeliversNoAcceptance is the same refusal seen
// from the worker's side, which is where it matters: what must never reach the
// stream is a second acceptance. An acceptance is the worker's entire authority
// to act, so one handed out for an approval that resumed nothing is exactly the
// unauthorized side effect this handshake exists to prevent.
func TestSecondSameRunApprovalReadyDeliversNoAcceptance(t *testing.T) {
	h := newHarness(t)
	fixture := newApprovalResumeFixture(t, h, "worker-second-approval-stream", "a second approval reaches the stream")
	sibling := fixture.siblingApproval(t, true)
	fixture.sendReady(t, fixture.ready())
	first := approvalResumeCommand(t, fixture.stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetApprovalResumeAccepted() != nil
	}).GetApprovalResumeAccepted()
	if first.GetApprovalId() != fixture.approvalID {
		t.Fatalf("first acceptance = %+v, want the resumed approval %q", first, fixture.approvalID)
	}
	resumed := h.runState(t, fixture.runID)
	events := approvalResumeStateEvents(t, h, fixture.runID)

	conflicting := fixture.ready()
	conflicting.ApprovalId = sibling
	fixture.sendReady(t, conflicting)

	if err := fixture.awaitExit(t); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("worker stream error = %v, want FailedPrecondition", err)
	}
	for {
		select {
		case command := <-fixture.stream.commands:
			if accepted := command.GetApprovalResumeAccepted(); accepted != nil {
				t.Fatalf("the second approval was accepted: %+v", accepted)
			}
			continue
		default:
		}
		break
	}
	after := h.runState(t, fixture.runID)
	if after.Lifecycle != recoveringRunStatus {
		t.Fatalf("lifecycle = %q, want %q", after.Lifecycle, recoveringRunStatus)
	}
	// The fence the closing stream owes, and nothing else: the refused Ready
	// itself must not have written a thing.
	if after.StateVersion != resumed.StateVersion+1 {
		t.Fatalf("state version = %d, want %d (fence only)", after.StateVersion, resumed.StateVersion+1)
	}
	if got := approvalResumeStateEvents(t, h, fixture.runID); got != events+1 {
		t.Fatalf("the refused ready appended %d state events, want exactly the fence", got-events)
	}
}
