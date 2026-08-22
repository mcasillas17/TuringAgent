package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// ---------------------------------------------------------------------------
// Version plumbing on the commands the orchestrator sends and the
// acknowledgement it accepts back.
//
// A worker refuses a command computed against a state it has already been told
// to leave, and stamps the version it was last told about onto its terminal
// reports. Both halves only work if the orchestrator actually puts the
// committed version on the wire and actually reads the one that comes back —
// neither of which any test observed, so either could have been deleted
// silently while the worker-side guards kept passing against zeros.
// ---------------------------------------------------------------------------

func TestRunCancelledCommandCarriesTheCommittedStateVersion(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "cancel with a version")
	stream, assigned := h.connectAssignedWorker(t, "worker-cancel-version", enqueued.RunID)

	// The cancellation commits first, exactly as the abandonment path does, so
	// the version the command has to carry is the one that transition left
	// behind rather than the one the assignment was made at.
	cancelRunFixture(t, h, enqueued.RunID)
	committed := h.runState(t, enqueued.RunID).StateVersion
	if committed <= assigned.GetExpectedStateVersion() {
		t.Fatalf("committed version %d did not move past the assignment's %d",
			committed, assigned.GetExpectedStateVersion())
	}

	h.service.CancelRun(context.Background(), enqueued.RunID, "client_cancelled")

	command := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunCancelled() != nil
	}).GetRunCancelled()
	if command.GetRunId() != enqueued.RunID {
		t.Fatalf("cancellation named run %q, want %q", command.GetRunId(), enqueued.RunID)
	}
	if got := command.GetStateVersion(); got != committed {
		t.Fatalf("cancellation carried version %d, want the committed %d", got, committed)
	}
	// The seam: a worker accepts this command's version and stamps it onto the
	// acknowledgement it sends back. Pinning each half against a number chosen
	// by hand would leave the join untested, so the version this command
	// actually carried is fed straight back in — over the real ConnectWorker
	// stream this assignment already holds, not a direct applyUpdate call.
	// Production always routes a run_cancelled_ack through
	// reconcileLateAssignedUpdate first (see the comment on
	// TestRunCancelledAckMustNameAVersionTheRunReached below), so exercising
	// applyUpdate directly here would prove nothing about what dispatch
	// actually enforces.
	if err := stream.Send(&turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{
			RunId:                enqueued.RunID,
			ObservedStateVersion: command.GetStateVersion(),
		}},
	}); err != nil {
		t.Fatalf("send run_cancelled_ack: %v", err)
	}
	awaitAckApplied(t, h, enqueued, stream, "sync-probe-cancelled-command-version")
	if run, err := h.repo.GetRun(context.Background(), enqueued.RunID); err != nil {
		t.Fatalf("GetRun: %v", err)
	} else if run.ExecutionActive {
		t.Fatal("acknowledgement echoing this command's own version was refused: execution fence still held")
	}
}

func TestApprovalUpdatedCommandCarriesTheResultingStateVersion(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "approve with a version")
	stream, assigned := h.connectAssignedWorker(t, "worker-approval-version", enqueued.RunID)

	// The approvals service notifies AFTER its decision commits, and a denial
	// terminalizes the run in that same transaction. So the version this command
	// has to carry is the one the run holds now, not the one it was handed over
	// at — which is exactly what a worker then stamps onto its acknowledgement.
	cancelRunFixture(t, h, enqueued.RunID)
	committed := h.runState(t, enqueued.RunID).StateVersion
	if committed <= assigned.GetExpectedStateVersion() {
		t.Fatalf("committed version %d did not move past the assignment's %d",
			committed, assigned.GetExpectedStateVersion())
	}

	if err := h.service.NotifyApprovalUpdated(
		context.Background(), enqueued.RunID, "approval_versioned", "denied", "",
	); err != nil {
		t.Fatalf("NotifyApprovalUpdated: %v", err)
	}

	command := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetApprovalUpdated() != nil
	}).GetApprovalUpdated()
	if command.GetApprovalId() != "approval_versioned" {
		t.Fatalf("approval update named %q, want approval_versioned", command.GetApprovalId())
	}
	if got := command.GetStateVersion(); got != committed {
		t.Fatalf("approval update carried version %d, want the resulting %d", got, committed)
	}
	// The seam: a worker cancelled by this command stamps the version the
	// command carried onto its acknowledgement. Pinning each half against a
	// number chosen by hand would leave the join untested, so the version this
	// command actually carried is fed straight back in — over the real
	// ConnectWorker stream this assignment already holds, not a direct
	// applyUpdate call. See the comment on
	// TestRunCancelledAckMustNameAVersionTheRunReached below for why a direct
	// applyUpdate call here would exercise code the dispatch loop never
	// reaches for a real run_cancelled_ack.
	if err := stream.Send(&turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{
			RunId:                enqueued.RunID,
			ObservedStateVersion: command.GetStateVersion(),
		}},
	}); err != nil {
		t.Fatalf("send run_cancelled_ack: %v", err)
	}
	awaitAckApplied(t, h, enqueued, stream, "sync-probe-approval-command-version")
	if run, err := h.repo.GetRun(context.Background(), enqueued.RunID); err != nil {
		t.Fatalf("GetRun: %v", err)
	} else if run.ExecutionActive {
		t.Fatal("acknowledgement echoing the approval command's version was refused: execution fence still held")
	}
}

// awaitAckApplied sends a synchronizing tool-beacon probe right behind an
// update already sent on stream and waits for its own decision, proving the
// update ahead of it finished being applied before the caller inspects run
// state. ConnectWorker serializes every update from one worker through a
// single receive loop, so the probe's reply cannot arrive before the ack
// ahead of it does — ignored outright, or used to release the fence,
// whichever the run does with it.
func awaitAckApplied(t *testing.T, h *harness, enqueued repository.EnqueueUserMessageResult, stream turingv1.RuntimeService_ConnectWorkerClient, probeID string) {
	t.Helper()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: probeID,
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "system", ToolName: "system.time",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:  mustStruct(t, map[string]any{"timezone": "UTC"}),
	}}}); err != nil {
		t.Fatalf("send sync probe: %v", err)
	}
	recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetToolPolicyDecision() != nil && cmd.GetToolPolicyDecision().GetToolCallId() == probeID
	})
}

// The acknowledgement releases the execution fence, which is the last thing
// holding a terminal run's identity open. A fenced predecessor still holds an
// old version, so an acknowledgement naming one has to be refused — otherwise
// the attempt that lost the run gets to declare its execution finished on
// behalf of the attempt that owns it.
//
// This drives the acknowledgement through the real ConnectWorker stream
// rather than calling applyUpdate directly. Production dispatch always routes
// a terminalRunID update — which a run_cancelled_ack always is — through
// reconcileLateAssignedUpdate first, and that path only ever falls through to
// applyUpdate (and so to handleRunCancelledAck's own version check) while the
// run is not yet terminal. By the time a worker acknowledges a cancellation
// the run is already cancelled, so reconcileLateAssignedUpdate is the only
// path a real ack ever takes: a test that calls applyUpdate straight through
// exercises code the dispatch loop never reaches for this message and proves
// nothing about what production actually enforces.
func TestRunCancelledAckMustNameAVersionTheRunReached(t *testing.T) {
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
			enqueued := h.enqueueRun(t, "ack "+test.name)
			// A real dispatch, so the execution fence this acknowledgement
			// releases is genuinely held by a live ConnectWorker assignment
			// rather than one this test built by hand.
			stream, assigned := h.connectAssignedWorker(t, "worker-ack-version", enqueued.RunID)
			// Two committed transitions before the terminal one, so
			// "committed-2" is a version the run genuinely held and genuinely
			// left rather than an impossible number.
			h.fenceOwnership(t, enqueued.RunID, "worker-ack-version", assigned.GetAssignmentAttemptId())
			resumeRecovering(t, h, enqueued.RunID)
			cancelRunFixture(t, h, enqueued.RunID)
			committed := h.runState(t, enqueued.RunID).StateVersion
			if committed < 4 {
				t.Fatalf("committed version %d is too small for this fixture", committed)
			}

			if err := stream.Send(&turingv1.RuntimeUpdate{
				Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{
					RunId:                enqueued.RunID,
					ObservedStateVersion: test.observed(committed),
				}},
			}); err != nil {
				t.Fatalf("send run_cancelled_ack: %v", err)
			}
			awaitAckApplied(t, h, enqueued, stream, "sync-probe-"+test.name)

			run, getErr := h.repo.GetRun(context.Background(), enqueued.RunID)
			if getErr != nil {
				t.Fatalf("GetRun: %v", getErr)
			}
			if test.accepted {
				if run.ExecutionActive {
					t.Fatal("accepted acknowledgement left the execution fence held")
				}
				return
			}
			if !run.ExecutionActive {
				t.Fatal("refused acknowledgement released the execution fence anyway")
			}
		})
	}
}

// resumeRecovering commits the recovering -> running transition, so a fixture
// can move a run's version through a real lifecycle step rather than by writing
// the column.
func resumeRecovering(t *testing.T, h *harness, runID string) {
	t.Helper()
	if _, err := h.repo.ResumeRecoveringRun(context.Background(), repository.ResumeRecoveringRunInput{
		RunID:                runID,
		ExpectedStateVersion: h.runState(t, runID).StateVersion,
	}); err != nil {
		t.Fatalf("ResumeRecoveringRun: %v", err)
	}
}
