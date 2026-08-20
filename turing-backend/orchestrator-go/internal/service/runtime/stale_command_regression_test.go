package runtime

import (
	"context"
	"io"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc"
)

// stubWorkerStream is enough of the worker stream to observe what sendCommand
// puts on the wire. Only Send, Recv and Context are reachable from the paths
// under test; anything else would be a bug in the test, and the embedded nil
// interface makes that loud rather than silent.
type stubWorkerStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent []*turingv1.RuntimeCommand
}

func (s *stubWorkerStream) Send(cmd *turingv1.RuntimeCommand) error {
	s.sent = append(s.sent, cmd)
	return nil
}

func (s *stubWorkerStream) Recv() (*turingv1.RuntimeUpdate, error) { return nil, io.EOF }

func (s *stubWorkerStream) Context() context.Context { return s.ctx }

// A run assignment can be released — terminalized, requeued, reconciled —
// while its own RunAssigned command is still queued. Delivering it then would
// hand the worker a run it no longer owns.
//
// The command must be dropped, and the stream must survive. Returning an error
// here is what the caller turns into a stream failure, which costs the worker
// every OTHER assignment it holds over one command that merely arrived late.
func TestSendCommandDropsAnAssignmentTheWorkerNoLongerHolds(t *testing.T) {
	server := &Server{}
	stream := &stubWorkerStream{ctx: context.Background()}
	connectedWorker := &worker{
		commands:    make(chan workerCommand, 1),
		done:        make(chan struct{}),
		assignments: map[string]assignment{}, // deliberately empty: already released
	}
	command := &turingv1.RuntimeCommand{
		Command: &turingv1.RuntimeCommand_RunAssigned{
			RunAssigned: &turingv1.AgentJob{RunId: "run_released", JobId: "job_1"},
		},
	}

	err := server.sendCommand(context.Background(), stream, workerCommand{command: command}, connectedWorker, "worker-1")

	if err != nil {
		t.Fatalf("sendCommand = %v, want nil so the stream survives a stale command", err)
	}
	if len(stream.sent) != 0 {
		t.Fatalf("sent %+v, want nothing — the worker does not own that run", stream.sent)
	}
}

// The guard must be specific to a released assignment, not a blanket "ignore
// anything that is not a run assignment": commands with no assignment to check
// still have to reach the worker.
func TestSendCommandStillDeliversNonAssignmentCommands(t *testing.T) {
	server := &Server{}
	stream := &stubWorkerStream{ctx: context.Background()}
	connectedWorker := &worker{
		commands:    make(chan workerCommand, 1),
		done:        make(chan struct{}),
		assignments: map[string]assignment{},
	}
	command := &turingv1.RuntimeCommand{
		Command: &turingv1.RuntimeCommand_ToolPolicyDecision{
			ToolPolicyDecision: &turingv1.ToolPolicyDecision{ApprovalId: "appr_1"},
		},
	}

	if err := server.sendCommand(context.Background(), stream, workerCommand{command: command}, connectedWorker, "worker-1"); err != nil {
		t.Fatalf("sendCommand = %v, want nil", err)
	}
	if len(stream.sent) != 1 {
		t.Fatalf("sent %d commands, want the decision delivered", len(stream.sent))
	}
}
