package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

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
