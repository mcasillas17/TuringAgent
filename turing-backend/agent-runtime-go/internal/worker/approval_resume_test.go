package worker

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

// ---------------------------------------------------------------------------
// The worker half of the approval Ready/Accepted handshake.
//
// A worker holding an approved run knows two different things: that a token
// exists, and that the orchestrator has durably resumed the run. Only the
// second one authorizes it to act. These tests keep those apart by making the
// token available first and the acceptance arrive last, with every step driven
// by a channel rather than a clock.
// ---------------------------------------------------------------------------

const (
	approvalResumeRunID = "run_approval_resume"
	// approvalResumeWaitingVersion is the version the approval request commits,
	// which is what the worker must echo on Ready and on a typed transport
	// failure — not the older version its assignment carried.
	approvalResumeWaitingVersion int64 = 5
)

type approvalResumeExecutor struct {
	poster  func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)
	resumer func(context.Context, tools.ApprovalResume) error

	waitTimeout time.Duration
	token       chan string
	mcp         approvalResumeMCPClient

	approvalDeadlines chan time.Time
	resumeDeadlines   chan time.Time
	results           chan error
}

func newApprovalResumeExecutor(waitTimeout time.Duration) *approvalResumeExecutor {
	return &approvalResumeExecutor{
		waitTimeout:       waitTimeout,
		token:             make(chan string, 1),
		mcp:               approvalResumeMCPClient{called: make(chan struct{}, 1)},
		approvalDeadlines: make(chan time.Time, 4),
		resumeDeadlines:   make(chan time.Time, 4),
		results:           make(chan error, 1),
	}
}

func (e *approvalResumeExecutor) SetToolBeaconPoster(post func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)) {
	e.poster = post
}

func (e *approvalResumeExecutor) SetApprovalResumer(resume func(context.Context, tools.ApprovalResume) error) {
	e.resumer = resume
}

func (e *approvalResumeExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, _ func(*turingv1.RuntimeUpdate) error) error {
	runner := &tools.Runner{
		PostBeacon: e.poster,
		WaitApproval: func(ctx context.Context, _ string) (string, error) {
			if deadline, ok := ctx.Deadline(); ok {
				select {
				case e.approvalDeadlines <- deadline:
				default:
				}
			}
			select {
			case token := <-e.token:
				return token, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
		ResumeApproved: func(ctx context.Context, resume tools.ApprovalResume) error {
			select {
			case e.resumeDeadlines <- resume.Deadline:
			default:
			}
			return e.resumer(ctx, resume)
		},
		ApprovalWaitTimeout: e.waitTimeout,
	}
	_, err := runner.Run(ctx, tools.RunInput{
		AgentID:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		RunID:      job.GetRunId(),
		TraceID:    job.GetTraceId(),
		ServerName: "files",
		ToolName:   "files.create",
		MCPClient:  e.mcp,
	})
	e.results <- err
	return err
}

type approvalResumeMCPClient struct{ called chan struct{} }

func (c approvalResumeMCPClient) CallTool(context.Context, string, map[string]any, ...string) (map[string]any, error) {
	select {
	case c.called <- struct{}{}:
	default:
	}
	return map[string]any{"ok": true}, nil
}

// approvalResumeFixture drives a worker to the exact boundary this protocol is
// about: an approved tool call whose token has arrived and whose run has not
// been resumed.
type approvalResumeFixture struct {
	worker   *Worker
	stream   *fakeStream
	executor *approvalResumeExecutor
	done     chan error
	cancel   context.CancelFunc
	job      *turingv1.AgentJob
	exited   bool
}

func startApprovalResumeWorker(t *testing.T, workerID string, waitTimeout time.Duration) *approvalResumeFixture {
	t.Helper()
	executor := newApprovalResumeExecutor(waitTimeout)
	stream := newFakeStream()
	runtimeWorker := New(Options{WorkerID: workerID, MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, executor)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtimeWorker.Run(ctx) }()
	fixture := &approvalResumeFixture{
		worker: runtimeWorker, stream: stream, executor: executor, done: done, cancel: cancel,
	}
	t.Cleanup(func() {
		cancel()
		if fixture.exited {
			return
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("worker did not exit")
		}
	})
	if ready := nextSent(t, stream); ready.GetWorkerReady() == nil {
		t.Fatalf("first update = %+v, want worker ready", ready)
	}
	job := &turingv1.AgentJob{
		JobId: "job_approval_resume", RunId: approvalResumeRunID, TraceId: "trace_approval_resume",
		AssignmentAttemptId: "attempt_owned", ExpectedStateVersion: 4,
	}
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: job}}
	fixture.job = job
	return fixture
}

// awaitExit waits for the worker to stop, and records that it already has so
// the fixture's cleanup does not wait for an exit it can never observe twice.
func (f *approvalResumeFixture) awaitExit(t *testing.T) error {
	t.Helper()
	select {
	case err := <-f.done:
		f.exited = true
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit")
	}
	return nil
}

// completeToolCall answers the after-beacon the approved call posts, so the
// executor finishes instead of waiting out its reporting timeout.
func (f *approvalResumeFixture) completeToolCall(t *testing.T) {
	t.Helper()
	update := nextSent(t, f.stream)
	after := update.GetToolBeacon()
	if after == nil || after.GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
		t.Fatalf("update after the approved call = %+v, want an after beacon", update)
	}
	if after.GetStatus() != turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED {
		t.Fatalf("after beacon status = %s, want completed", after.GetStatus())
	}
	f.stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ToolPolicyDecision{
		ToolPolicyDecision: &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
			ToolCallId: after.GetToolCallId(),
			Phase:      after.GetPhase(),
		},
	}}
	select {
	case err := <-f.executor.results:
		if err != nil {
			t.Fatalf("approved tool call failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the approved tool call did not finish")
	}
}

// awaitApprovalRequired answers the tool's before-beacon with an approval
// requirement and hands the runner its token, which is deliberately NOT enough
// to continue.
func (f *approvalResumeFixture) awaitApprovalRequired(t *testing.T, approvalID string) {
	t.Helper()
	beacon := nextSent(t, f.stream).GetToolBeacon()
	if beacon == nil || beacon.GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
		t.Fatalf("first tool update = %+v, want a before beacon", beacon)
	}
	f.stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ToolPolicyDecision{
		ToolPolicyDecision: &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
			ToolCallId: beacon.GetToolCallId(),
			ApprovalId: approvalID,
			Phase:      beacon.GetPhase(),
			// The decision the orchestrator actually sends carries the version
			// the approval request committed, so the worker's idea of "waiting
			// approval" is the row's and not the assignment's.
			RunStateVersion: approvalResumeWaitingVersion,
		},
	}}
	f.executor.token <- "approval-token"
}

// decide delivers the approved decision command, which is what the worker is
// allowed to treat as permission to ask for a resume.
func (f *approvalResumeFixture) decide(approvalID string, version int64) {
	f.stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ApprovalUpdated{
		ApprovalUpdated: &turingv1.RuntimeApprovalUpdated{
			ApprovalId: approvalID, Status: "approved", ApprovalToken: "approval-token", StateVersion: version,
		},
	}}
}

func (f *approvalResumeFixture) awaitReady(t *testing.T) *turingv1.RuntimeApprovalResumeReady {
	t.Helper()
	update := nextSent(t, f.stream)
	ready := update.GetApprovalResumeReady()
	if ready == nil {
		t.Fatalf("update after the approved decision = %+v, want approval resume ready", update)
	}
	return ready
}

// accept delivers one acceptance synchronously, so a test that asserts nothing
// happened knows the command was already processed rather than still in flight.
func (f *approvalResumeFixture) accept(t *testing.T, accepted *turingv1.RuntimeApprovalResumeAccepted) {
	t.Helper()
	if err := f.worker.handleCommand(context.Background(), f.stream, &turingv1.RuntimeCommand{
		Command: &turingv1.RuntimeCommand_ApprovalResumeAccepted{ApprovalResumeAccepted: accepted},
	}); err != nil {
		t.Fatalf("handleCommand: %v", err)
	}
}

func (f *approvalResumeFixture) assertToolIdle(t *testing.T) {
	t.Helper()
	select {
	case <-f.executor.mcp.called:
		t.Fatal("the approved tool call ran before its acceptance")
	default:
	}
}

// pendingResumeDeadline reads the instant the worker's pending resume stops
// waiting, so "the deadline was not extended" is an equality rather than a
// guess about how long a test took.
func pendingResumeDeadline(t *testing.T, w *Worker, approvalID string) time.Time {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	pending, exists := w.resumes[approvalID]
	if !exists {
		t.Fatalf("no pending resume for approval %q", approvalID)
	}
	return pending.deadline
}

// TestWorkerCannotContinueToolOrModelBeforeAccepted is the whole point of the
// handshake. A token proves a person said yes; only the orchestrator's
// acceptance proves the run is durably running again, and acting on the first
// is how a side effect gets committed for a run the orchestrator has already
// given to somebody else.
func TestWorkerCannotContinueToolOrModelBeforeAccepted(t *testing.T) {
	fixture := startApprovalResumeWorker(t, "worker-resume-gate", 0)
	fixture.awaitApprovalRequired(t, "approval_gate")
	fixture.assertToolIdle(t)

	fixture.decide("approval_gate", approvalResumeWaitingVersion)

	ready := fixture.awaitReady(t)
	if ready.GetRunId() != approvalResumeRunID || ready.GetApprovalId() != "approval_gate" {
		t.Fatalf("ready identity = %+v, want run %q approval %q", ready, approvalResumeRunID, "approval_gate")
	}
	if ready.GetExpectedStateVersion() != 5 {
		t.Fatalf("ready expected version = %d, want the waiting-approval version 5", ready.GetExpectedStateVersion())
	}
	if ready.GetAssignmentAttemptId() != fixture.job.GetAssignmentAttemptId() {
		t.Fatalf("ready attempt = %q, want %q", ready.GetAssignmentAttemptId(), fixture.job.GetAssignmentAttemptId())
	}
	fixture.assertToolIdle(t)

	fixture.accept(t, &turingv1.RuntimeApprovalResumeAccepted{
		RunId: approvalResumeRunID, ApprovalId: "approval_gate", StateVersion: 6,
		AssignmentAttemptId: fixture.job.GetAssignmentAttemptId(),
	})

	select {
	case <-fixture.executor.mcp.called:
	case <-time.After(5 * time.Second):
		t.Fatal("the approved tool call did not run after its acceptance")
	}
	if got := fixture.worker.activeRun(approvalResumeRunID).expectedVersion(); got != 6 {
		t.Fatalf("run version after acceptance = %d, want the accepted 6", got)
	}
	fixture.completeToolCall(t)
}

// TestReadyWaitUsesRemainingApprovalDeadline keeps the resume inside the budget
// the approval wait already had. A fresh budget at Ready time would let a run
// hold its worker slot for twice as long as anything told the user it could.
func TestReadyWaitUsesRemainingApprovalDeadline(t *testing.T) {
	fixture := startApprovalResumeWorker(t, "worker-resume-deadline", 2*time.Second)
	fixture.awaitApprovalRequired(t, "approval_deadline")
	var approvalDeadline time.Time
	select {
	case approvalDeadline = <-fixture.executor.approvalDeadlines:
	case <-time.After(5 * time.Second):
		t.Fatal("the approval wait had no deadline")
	}

	fixture.decide("approval_deadline", approvalResumeWaitingVersion)
	fixture.awaitReady(t)

	var resumeDeadline time.Time
	select {
	case resumeDeadline = <-fixture.executor.resumeDeadlines:
	case <-time.After(5 * time.Second):
		t.Fatal("the resume had no deadline")
	}
	if !resumeDeadline.Equal(approvalDeadline) {
		t.Fatalf("resume deadline = %s, want the approval wait deadline %s", resumeDeadline, approvalDeadline)
	}
	if pending := pendingResumeDeadline(t, fixture.worker, "approval_deadline"); !pending.Equal(approvalDeadline) {
		t.Fatalf("worker resume deadline = %s, want %s", pending, approvalDeadline)
	}
}

// TestMismatchedAcceptedDoesNotExtendReadyDeadline covers the acceptance that
// belongs to somebody else. Ignoring it is not enough: restarting the wait on
// every stray command would let an unrelated stream keep this run paused
// indefinitely.
func TestMismatchedAcceptedDoesNotExtendReadyDeadline(t *testing.T) {
	fixture := startApprovalResumeWorker(t, "worker-resume-mismatch", 2*time.Second)
	fixture.awaitApprovalRequired(t, "approval_mismatch")
	fixture.decide("approval_mismatch", approvalResumeWaitingVersion)
	fixture.awaitReady(t)
	before := pendingResumeDeadline(t, fixture.worker, "approval_mismatch")

	fixture.accept(t, &turingv1.RuntimeApprovalResumeAccepted{
		RunId: approvalResumeRunID, ApprovalId: "approval_mismatch", StateVersion: 9,
		AssignmentAttemptId: "attempt_from_a_fenced_predecessor",
	})

	fixture.assertToolIdle(t)
	if after := pendingResumeDeadline(t, fixture.worker, "approval_mismatch"); !after.Equal(before) {
		t.Fatalf("resume deadline after a mismatched acceptance = %s, want the original %s", after, before)
	}
	if got := fixture.worker.activeRun(approvalResumeRunID).expectedVersion(); got != 5 {
		t.Fatalf("run version after a mismatched acceptance = %d, want the waiting 5", got)
	}

	fixture.accept(t, &turingv1.RuntimeApprovalResumeAccepted{
		RunId: approvalResumeRunID, ApprovalId: "approval_mismatch", StateVersion: 6,
		AssignmentAttemptId: fixture.job.GetAssignmentAttemptId(),
	})
	select {
	case <-fixture.executor.mcp.called:
	case <-time.After(5 * time.Second):
		t.Fatal("the approved tool call did not run after its matching acceptance")
	}
	fixture.completeToolCall(t)
}

// TestNeverAnsweredReadyFailsOrRecoversWithoutHoldingWorkerSlot covers both
// ways the handshake can go unanswered, and the one thing they share: the
// worker never sits on a paused run forever.
//
// Before Ready the row is still waiting approval and this attempt still owns
// it, so the worker can honestly end the run with the transport failure that
// actually happened. After Ready it cannot: the orchestrator may already have
// committed running, so the only honest move is to drop the stream and let the
// ownership fence decide.
func TestNeverAnsweredReadyFailsOrRecoversWithoutHoldingWorkerSlot(t *testing.T) {
	t.Run("decision never arrives", func(t *testing.T) {
		fixture := startApprovalResumeWorker(t, "worker-resume-undecided", 50*time.Millisecond)
		fixture.awaitApprovalRequired(t, "approval_undecided")

		update := nextSent(t, fixture.stream)
		failed := update.GetRunFailed()
		if failed == nil {
			t.Fatalf("update after an unanswered decision = %+v, want a run failure", update)
		}
		if failed.GetCode() != "approval_delivery_failed" {
			t.Fatalf("failure code = %q, want approval_delivery_failed", failed.GetCode())
		}
		if failed.GetFailureOrigin() != turingv1.FailureOrigin_FAILURE_ORIGIN_APPROVAL_TRANSPORT {
			t.Fatalf("failure origin = %s, want approval transport", failed.GetFailureOrigin())
		}
		if failed.GetAutomaticRetryClass() != turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_NEVER {
			t.Fatalf("retry class = %s, want never", failed.GetAutomaticRetryClass())
		}
		if failed.GetExpectedStateVersion() != approvalResumeWaitingVersion {
			t.Fatalf("failure version = %d, want the waiting-approval %d",
				failed.GetExpectedStateVersion(), approvalResumeWaitingVersion)
		}
		fixture.assertToolIdle(t)
		waitForInactiveRun(t, fixture.worker, approvalResumeRunID)
	})

	t.Run("acceptance never arrives", func(t *testing.T) {
		fixture := startApprovalResumeWorker(t, "worker-resume-unaccepted", 50*time.Millisecond)
		fixture.awaitApprovalRequired(t, "approval_unaccepted")
		fixture.decide("approval_unaccepted", approvalResumeWaitingVersion)
		fixture.awaitReady(t)

		if err := fixture.awaitExit(t); err == nil {
			t.Fatal("the worker kept a stream whose acceptance never arrived")
		}
		fixture.assertToolIdle(t)
		waitForInactiveRun(t, fixture.worker, approvalResumeRunID)
		for {
			select {
			case update := <-fixture.stream.sent:
				if update.GetRunFailed() != nil || update.GetRunCompleted() != nil || update.GetRunCancelledAck() != nil {
					t.Fatalf("worker reported %+v for a run whose resume it could not prove", update)
				}
				continue
			default:
			}
			return
		}
	})
}

// TestApprovalDecidedBeforeItsRequirementStillResumes covers the ordering an
// automation actually produces. The grant is queued ahead of the policy
// decision that names the approval, and both travel the same ordered command
// channel, so the worker is told the answer before it is told the question.
// Dropping that decision would leave every unattended approval waiting out its
// deadline for a command it had already received.
func TestApprovalDecidedBeforeItsRequirementStillResumes(t *testing.T) {
	fixture := startApprovalResumeWorker(t, "worker-resume-early-decision", 0)
	beacon := nextSent(t, fixture.stream).GetToolBeacon()
	if beacon == nil || beacon.GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
		t.Fatalf("first tool update = %+v, want a before beacon", beacon)
	}

	fixture.decide("approval_early", approvalResumeWaitingVersion)
	fixture.stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ToolPolicyDecision{
		ToolPolicyDecision: &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
			ToolCallId: beacon.GetToolCallId(),
			ApprovalId: "approval_early",
			Phase:      beacon.GetPhase(),
		},
	}}
	fixture.executor.token <- "approval-token"

	ready := fixture.awaitReady(t)
	if ready.GetApprovalId() != "approval_early" {
		t.Fatalf("ready = %+v, want the early-decided approval", ready)
	}
	fixture.assertToolIdle(t)
	fixture.accept(t, &turingv1.RuntimeApprovalResumeAccepted{
		RunId: approvalResumeRunID, ApprovalId: "approval_early", StateVersion: 6,
		AssignmentAttemptId: fixture.job.GetAssignmentAttemptId(),
	})
	select {
	case <-fixture.executor.mcp.called:
	case <-time.After(5 * time.Second):
		t.Fatal("the approved tool call did not run after its acceptance")
	}
	fixture.completeToolCall(t)
}

// awaitCancelledAck reads the acknowledgement the cancellation path owes, so a
// test proves the run ended the way the orchestrator asked rather than merely
// stopping.
func (f *approvalResumeFixture) awaitCancelledAck(t *testing.T, runID string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case update := <-f.stream.sent:
			if ack := update.GetRunCancelledAck(); ack != nil {
				if ack.GetRunId() != runID {
					t.Fatalf("cancellation ack names run %q, want %q", ack.GetRunId(), runID)
				}
				return
			}
			if update.GetRunFailed() != nil || update.GetRunCompleted() != nil {
				t.Fatalf("worker reported %+v for a run terminalization already owns", update)
			}
		case <-deadline:
			t.Fatal("timed out waiting for the cancellation ack")
		}
	}
}

// awaitResumeAbandoned waits for the paused executor to unwind and for its slot
// to be released.
func (f *approvalResumeFixture) awaitResumeAbandoned(t *testing.T, runID string) {
	t.Helper()
	select {
	case err := <-f.executor.results:
		if err == nil {
			t.Fatal("the paused executor finished as if its resume had been accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the paused executor did not exit")
	}
	f.assertToolIdle(t)
	waitForInactiveRun(t, f.worker, runID)
}

// runAnotherRun proves the stream is still usable: a second assignment arrives
// on it, its tool is allowed, and it runs to completion.
func (f *approvalResumeFixture) runAnotherRun(t *testing.T, runID string) {
	t.Helper()
	select {
	case err := <-f.done:
		f.exited = true
		t.Fatalf("the worker stream was torn down by a run terminalization already owned: %v", err)
	default:
	}
	f.stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{
		JobId: "job_after_cancellation", RunId: runID, TraceId: "trace_after_cancellation",
		AssignmentAttemptId: "attempt_after_cancellation", ExpectedStateVersion: 1,
	}}}
	beacon := nextSent(t, f.stream).GetToolBeacon()
	if beacon == nil || beacon.GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
		t.Fatalf("first update of the next run = %+v, want a before beacon", beacon)
	}
	if beacon.GetRunId() != runID {
		t.Fatalf("before beacon names run %q, want %q", beacon.GetRunId(), runID)
	}
	f.stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ToolPolicyDecision{
		ToolPolicyDecision: &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
			ToolCallId: beacon.GetToolCallId(),
			Phase:      beacon.GetPhase(),
		},
	}}
	select {
	case <-f.executor.mcp.called:
	case <-time.After(5 * time.Second):
		t.Fatal("the next run's tool never ran")
	}
	f.completeToolCall(t)
	select {
	case err := <-f.done:
		f.exited = true
		t.Fatalf("the worker stream ended while another run was proceeding: %v", err)
	default:
	}
}

// TestTerminalizationBeforeAcceptedKeepsTheWorkerStream covers the run that is
// ended by something else while its worker is holding the paused executor open
// waiting for an acceptance.
//
// The unacknowledged-resume fatal exists for a real hazard: a Ready the
// orchestrator may already have committed, whose acceptance this worker never
// saw, leaves the run's true state unknowable from here — so the stream drops
// and the ownership fence decides. None of that applies once the run has been
// terminalized. The orchestrator asked for the cancellation, or a sibling
// authorization was refused; either way the outcome is already owned and
// already being acknowledged. Dropping the whole stream then takes down every
// other run this worker is executing to re-establish something that is no
// longer in doubt.
func TestTerminalizationBeforeAcceptedKeepsTheWorkerStream(t *testing.T) {
	t.Run("the orchestrator cancels the run", func(t *testing.T) {
		fixture := startApprovalResumeWorker(t, "worker-resume-cancelled", 0)
		fixture.awaitApprovalRequired(t, "approval_cancelled")
		fixture.decide("approval_cancelled", approvalResumeWaitingVersion)
		fixture.awaitReady(t)

		fixture.stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunCancelled{
			RunCancelled: &turingv1.RuntimeRunCancelled{
				RunId: approvalResumeRunID, StateVersion: approvalResumeWaitingVersion + 1,
			},
		}}

		fixture.awaitResumeAbandoned(t, approvalResumeRunID)
		fixture.awaitCancelledAck(t, approvalResumeRunID)
		fixture.runAnotherRun(t, "run_after_cancellation")
	})

	t.Run("a sibling authorization is refused", func(t *testing.T) {
		fixture := startApprovalResumeWorker(t, "worker-resume-sibling-denied", 0)
		fixture.awaitApprovalRequired(t, "approval_sibling_waiting")
		// The run's other outstanding authorization. A model that asked for two
		// tools has two, and refusing either one ends the run.
		fixture.worker.rememberApproval("approval_sibling_denied", approvalResumeRunID)
		fixture.decide("approval_sibling_waiting", approvalResumeWaitingVersion)
		fixture.awaitReady(t)

		fixture.stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ApprovalUpdated{
			ApprovalUpdated: &turingv1.RuntimeApprovalUpdated{
				ApprovalId: "approval_sibling_denied", Status: "denied",
				StateVersion: approvalResumeWaitingVersion + 1,
			},
		}}

		fixture.awaitResumeAbandoned(t, approvalResumeRunID)
		fixture.awaitCancelledAck(t, approvalResumeRunID)
		fixture.runAnotherRun(t, "run_after_sibling_denial")
	})
}
