package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestMatchingTerminalUpdateRequiresPersistedIdentity(t *testing.T) {
	run := repository.Run{Status: "completed", AssistantMessageID: "message_1"}
	update := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId: "run_1", AssistantMessageId: run.AssistantMessageID,
	}}}
	update.GetRunCompleted().Content = "complete"
	if isMatchingTerminalUpdate(run, update) {
		t.Fatal("completion without persisted content matched a terminal run")
	}
	if isMatchingTerminalUpdate(repository.Run{Status: "failed"}, &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
			RunId: "run_1", Code: "runtime_error", Message: "failed", Retryable: false,
		}},
	}) {
		t.Fatal("failure without persisted payload matched a terminal run")
	}
}

func TestTerminalizedAssignedRunUsesLateTerminalUpdateAsExitAcknowledgement(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "terminalized assigned run")
	second := h.enqueueRun(t, "assigned after exit acknowledgement")
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(workerReady("worker-terminalized-assigned")); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == first.RunID
	})

	if _, err := h.repo.CancelRunWithEvent(context.Background(), first.RunID, "client_cancelled", `{"reason":"client_cancelled"}`); err != nil {
		t.Fatal(err)
	}
	payload, err := structpb.NewStruct(map[string]any{"delta": "late"})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId: first.RunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA, Payload: payload,
	}}}); err != nil {
		t.Fatalf("send late delta: %v", err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId: first.RunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_CANCELLED,
	}}}); err != nil {
		t.Fatalf("send late generic terminal event: %v", err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId: first.RunID, AssistantMessageId: "msg_conflicting", Content: "too late",
	}}}); err != nil {
		t.Fatalf("send conflicting late completion: %v", err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == second.RunID
	})

	run, err := h.repo.GetRun(context.Background(), first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "cancelled" || run.ExecutionActive || run.ExecutionState != "exited" {
		t.Fatalf("late updates mutated terminal execution fence: %+v", run)
	}
}

func TestTerminalizedAssignedRunReconcilesMatchingLateTerminalUpdate(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "failed while assigned")
	second := h.enqueueRun(t, "assigned after matching late failure")
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(workerReady("worker-terminalized-matching")); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == first.RunID
	})

	payload, err := encodePayload(map[string]any{
		"runId": first.RunID, "code": "persisted_failure", "message": "terminalized elsewhere", "retryable": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.FailRunWithEventPreservingExecution(context.Background(), first.RunID, "persisted_failure", "terminalized elsewhere", payload); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId: first.RunID, Code: "persisted_failure", Message: "terminalized elsewhere", Retryable: false,
	}}}); err != nil {
		t.Fatalf("send matching late failure: %v", err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == second.RunID
	})

	run, err := h.repo.GetRun(context.Background(), first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || run.ExecutionActive || run.ExecutionState != "exited" {
		t.Fatalf("matching terminal update did not reconcile execution exit: %+v", run)
	}
	var failedEvents int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, first.RunID).Scan(&failedEvents); err != nil {
		t.Fatal(err)
	}
	if failedEvents != 1 {
		t.Fatalf("late matching failure appended %d terminal events, want one", failedEvents)
	}
}

func TestLateTerminalUpdatesForReleasedRunKeepWorkerStreamUsable(t *testing.T) {
	tests := []struct {
		name           string
		cancelFirst    bool
		lateExitAck    bool
		duplicateFirst bool
	}{
		{name: "duplicate_cancelled_ack", cancelFirst: true, duplicateFirst: true},
		{name: "duplicate_completed", duplicateFirst: true},
		{name: "completed_then_exit_ack", lateExitAck: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			first := h.enqueueRun(t, "first terminal update")
			second := h.enqueueRun(t, "second remains assigned")
			third := h.enqueueRun(t, "third proves stream remains usable")
			client := h.runtimeClient(t)
			stream, err := client.ConnectWorker(h.internalContext())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stream.CloseSend() }()
			if err := stream.Send(workerReady("worker-late-terminal-" + test.name)); err != nil {
				t.Fatal(err)
			}
			recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
				return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == first.RunID
			})

			completedFirst := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
				RunId: first.RunID, AssistantMessageId: first.AssistantMessageID, Content: "first complete",
			}}}
			ackFirst := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{
				RunId: first.RunID,
			}}}
			if test.cancelFirst {
				if _, err := h.repo.CancelRunWithEvent(context.Background(), first.RunID, "client_cancelled", `{"reason":"client_cancelled"}`); err != nil {
					t.Fatal(err)
				}
				if err := stream.Send(ackFirst); err != nil {
					t.Fatal(err)
				}
			} else if err := stream.Send(completedFirst); err != nil {
				t.Fatal(err)
			}

			recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
				return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == second.RunID
			})

			var late *turingv1.RuntimeUpdate
			switch {
			case test.lateExitAck:
				late = ackFirst
			case test.cancelFirst:
				late = ackFirst
			default:
				late = completedFirst
			}
			if err := stream.Send(late); err != nil {
				t.Fatalf("send late terminal update: %v", err)
			}

			if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
				RunId: second.RunID, AssistantMessageId: second.AssistantMessageID, Content: "second complete",
			}}}); err != nil {
				t.Fatalf("send second completion: %v", err)
			}
			recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
				return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == third.RunID
			})
		})
	}
}
