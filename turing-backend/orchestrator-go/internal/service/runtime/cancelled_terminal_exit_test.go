package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func cancelledRetriedAssignment(t *testing.T, h *harness) (repository.EnqueueUserMessageResult, turingv1.RuntimeService_ConnectWorkerClient, *turingv1.AgentJob, *turingv1.AgentJob, repository.RunState) {
	t.Helper()
	return retriedAssignmentStoppedBy(t, h, func(run repository.EnqueueUserMessageResult) repository.RunState {
		cancelled, err := h.repo.CancelUserRun(context.Background(), run.SessionID, run.RunID, "cancel:"+run.RunID)
		if err != nil || !cancelled.Accepted {
			t.Fatalf("explicit cancellation = %+v, %v", cancelled, err)
		}
		return cancelled.State
	})
}

// retriedAssignmentStoppedBy is a run retried onto a newer attempt on the same
// worker and then stopped by stop, which returns the stopped state.
func retriedAssignmentStoppedBy(t *testing.T, h *harness, stop func(repository.EnqueueUserMessageResult) repository.RunState) (repository.EnqueueUserMessageResult, turingv1.RuntimeService_ConnectWorkerClient, *turingv1.AgentJob, *turingv1.AgentJob, repository.RunState) {
	t.Helper()
	run := h.enqueueRun(t, "cancel newer attempt on same worker")
	stream, first := h.connectAssignedWorker(t, "worker-cancel-retried", run.RunID)
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{
		RunFailed: &turingv1.RuntimeRunFailed{
			RunId: run.RunID, Code: "tool_discovery_failed",
			FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_INFRASTRUCTURE,
			AutomaticRetryClass:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
			ExpectedStateVersion: first.ExpectedStateVersion,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	current := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if current.RunId != first.RunId || current.AssignmentAttemptId == first.AssignmentAttemptId ||
		current.ExpectedStateVersion <= first.ExpectedStateVersion {
		t.Fatalf("retry did not create a newer assignment: first=%v current=%v", first, current)
	}
	return run, stream, first, current, stop(run)
}

// A task stopped because its turn ended is held like a user's cancellation:
// once it has been retried, a report naming no version cannot tell its
// predecessor from its current attempt, so it releases nothing, whether it
// is an ack, a completion or a failure, through the receive loop or the
// handler-local race path. A versioned ack still does.
func TestAStoppedTasksLegacyExitAfterRetryKeepsItContained(t *testing.T) {
	stopTask := func(h *harness) func(repository.EnqueueUserMessageResult) repository.RunState {
		return func(run repository.EnqueueUserMessageResult) repository.RunState {
			stopped, err := h.repo.CancelRunCanonical(context.Background(), repository.CancelRunInput{
				RunID: run.RunID, ExpectedStateVersion: h.runState(t, run.RunID).StateVersion,
				Cancellation: runoutcome.ParentEndedCancellation(),
			})
			if err != nil {
				t.Fatal(err)
			}
			return stopped.State
		}
	}
	for _, kind := range []string{"completion", "failure", "ack", "handler-local ack"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			run, stream, _, current, stopped := retriedAssignmentStoppedBy(t, h, stopTask(h))
			if kind == "handler-local ack" {
				if err := h.service.applyUpdate(context.Background(), cancelledExitReport(run.RunID, "ack", 0)); status.Code(err) != codes.FailedPrecondition {
					t.Fatalf("ambiguous handler-local ack = %v", err)
				}
			} else {
				if err := stream.Send(cancelledExitReport(run.RunID, kind, 0)); err != nil {
					t.Fatal(err)
				}
				awaitAckApplied(t, h, run, stream, "sync-stopped-legacy-exit")
			}
			after, err := h.repo.GetRun(context.Background(), run.RunID)
			if err != nil || !after.ExecutionActive || after.ExecutionAttemptID != current.AssignmentAttemptId {
				t.Fatalf("legacy %s released the stopped task's newer attempt: %+v, %v", kind, after, err)
			}
			if owner := h.service.registeredWorker("worker-cancel-retried"); owner == nil || !owner.hasAssignment(run.RunID) {
				t.Fatal("legacy report released the current worker assignment")
			}
			if err := stream.Send(cancelledExitReport(run.RunID, "ack", stopped.StateVersion)); err != nil {
				t.Fatal(err)
			}
			awaitAckApplied(t, h, run, stream, "sync-stopped-versioned-exit")
			if after, err := h.repo.GetRun(context.Background(), run.RunID); err != nil || after.ExecutionActive {
				t.Fatalf("versioned ack did not release the stopped task: %+v, %v", after, err)
			}
		})
	}
}

func cancelledExitReport(runID string, kind string, version int64) *turingv1.RuntimeUpdate {
	if kind == "ack" {
		return &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{
			RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: runID, ObservedStateVersion: version},
		}}
	}
	if kind == "completion" {
		return &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{
			RunCompleted: &turingv1.RuntimeRunCompleted{
				RunId: runID, AssistantMessageId: "losing-assistant", Content: "losing content",
				ExpectedStateVersion: version,
			},
		}}
	}
	return &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{
		RunFailed: &turingv1.RuntimeRunFailed{
			RunId: runID, Code: "model_stream_failed",
			FailureOrigin: turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT, ExpectedStateVersion: version,
		},
	}}
}

func TestCancelledNewerAttemptRejectsPredecessorTerminalExit(t *testing.T) {
	for _, kind := range []string{"completion", "failure"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			run, stream, first, current, cancelled := cancelledRetriedAssignment(t, h)
			eventCount := h.countRunEvents(t, run.RunID)
			if err := stream.Send(cancelledExitReport(run.RunID, kind, first.ExpectedStateVersion)); err != nil {
				t.Fatal(err)
			}
			awaitAckApplied(t, h, run, stream, "sync-stale-terminal")
			after, err := h.repo.GetRun(context.Background(), run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if !after.ExecutionActive || after.ExecutionAttemptID != current.AssignmentAttemptId {
				t.Fatalf("stale predecessor %s released newer cancellation fence: %+v", kind, after)
			}
			owner := h.service.registeredWorker("worker-cancel-retried")
			if owner == nil || !owner.hasAssignment(run.RunID) {
				t.Fatal("stale predecessor released the current worker assignment")
			}
			if after.StateVersion != cancelled.StateVersion || after.OutcomeReason != "user_cancelled" ||
				after.AssistantContent != "" || h.countRunEvents(t, run.RunID) != eventCount {
				t.Fatalf("stale predecessor rewrote the committed outcome: %+v", after)
			}
			if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{
				RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: run.RunID, ObservedStateVersion: cancelled.StateVersion},
			}}); err != nil {
				t.Fatal(err)
			}
			awaitAckApplied(t, h, run, stream, "sync-matching-cancellation-ack")
			after, err = h.repo.GetRun(context.Background(), run.RunID)
			if err != nil || after.ExecutionActive || owner.hasAssignment(run.RunID) {
				t.Fatalf("matching ack failed to release current attempt: %+v, %v", after, err)
			}
		})
	}
}

func TestCancelledCurrentAttemptTerminalExitIgnoresLosingOutcome(t *testing.T) {
	for _, kind := range []string{"completion", "failure", "ack"} {
		for _, observed := range []string{"pre-cancel", "cancel-observed", "legacy", "future"} {
			t.Run(kind+"/"+observed, func(t *testing.T) {
				h := newHarness(t)
				run, stream, _, current, cancelled := cancelledRetriedAssignment(t, h)
				version := current.ExpectedStateVersion
				switch observed {
				case "cancel-observed":
					version = cancelled.StateVersion
				case "legacy":
					version = 0
				case "future":
					version = cancelled.StateVersion + 1
				}
				if err := stream.Send(cancelledExitReport(run.RunID, kind, version)); err != nil {
					t.Fatal(err)
				}
				awaitAckApplied(t, h, run, stream, "sync-terminal-exit")
				after, err := h.repo.GetRun(context.Background(), run.RunID)
				if err != nil {
					t.Fatal(err)
				}
				if after.ExecutionActive != (observed == "future" || observed == "legacy") {
					t.Fatalf("%s report execution fence = %v", observed, after.ExecutionActive)
				}
				if after.StateVersion != cancelled.StateVersion || after.OutcomeReason != "user_cancelled" || after.AssistantContent != "" {
					t.Fatalf("losing report changed cancellation: %+v", after)
				}
				if after.ExecutionActive {
					if err := stream.Send(cancelledExitReport(run.RunID, "ack", cancelled.StateVersion)); err != nil {
						t.Fatal(err)
					}
					awaitAckApplied(t, h, run, stream, "sync-versioned-exit")
					reconciled, err := h.repo.GetRun(context.Background(), run.RunID)
					if err != nil || reconciled.ExecutionActive {
						t.Fatalf("versioned ack failed after rejected legacy/future report: %+v, %v", reconciled, err)
					}
				}
			})
		}
	}
}

func TestExplicitCancellationLegacyExitRequiresKnownFirstAttempt(t *testing.T) {
	for _, kind := range []string{"completion", "failure", "ack"} {
		for _, known := range []bool{true, false} {
			t.Run(kind+"/"+map[bool]string{true: "first-attempt", false: "unknown-lineage"}[known], func(t *testing.T) {
				h := newHarness(t)
				run := h.enqueueRun(t, "legacy exit identity")
				stream, first := h.connectAssignedWorker(t, "worker-legacy-exit", run.RunID)
				if first.Attempt != 1 {
					t.Fatalf("first attempt = %d", first.Attempt)
				}
				if _, err := h.repo.CancelUserRun(context.Background(), run.SessionID, run.RunID, "cancel"); err != nil {
					t.Fatal(err)
				}
				if !known {
					if _, err := h.database.Exec(`UPDATE jobs SET assignment_attempt_id = NULL WHERE run_id = ?`, run.RunID); err != nil {
						t.Fatal(err)
					}
				}
				if err := stream.Send(cancelledExitReport(run.RunID, kind, 0)); err != nil {
					t.Fatal(err)
				}
				awaitAckApplied(t, h, run, stream, "sync-legacy-exit")
				current, err := h.repo.GetRun(context.Background(), run.RunID)
				if err != nil || current.ExecutionActive == known {
					t.Fatalf("legacy exit known=%v: %+v, %v", known, current, err)
				}
			})
		}
	}
}

func TestExplicitCancellationLegacyAckRaceWindowRetainsRetriedFence(t *testing.T) {
	h := newHarness(t)
	run, _, _, _, _ := cancelledRetriedAssignment(t, h)
	// The local handler is the fallback if cancellation commits between the
	// receive loop's initial terminal check and applyUpdate's fresh read.
	err := h.service.applyUpdate(context.Background(), cancelledExitReport(run.RunID, "ack", 0))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ambiguous handler-local ack = %v", err)
	}
	current, err := h.repo.GetRun(context.Background(), run.RunID)
	if err != nil || !current.ExecutionActive {
		t.Fatalf("ambiguous race-window ack released newer attempt: %+v, %v", current, err)
	}
}
