package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestWorkerConnectionCancellationUnblocksStartedSendAndJoinsWriter(t *testing.T) {
	stream := &connectionCancellationStream{
		sent:    make(chan *turingv1.RuntimeUpdate, 2),
		recv:    make(chan *turingv1.RuntimeCommand, 1),
		blocked: make(chan struct{}),
	}
	worker := New(Options{WorkerID: "worker-send-cancel"}, &connectionCancellationClient{stream: stream}, emitBlockingUpdateExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	ready := <-stream.sent
	if ready.GetWorkerReady() == nil {
		t.Fatalf("first update = %+v, want worker ready", ready)
	}
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{RunId: "run_1"}}}
	select {
	case <-stream.blocked:
	case <-time.After(time.Second):
		t.Fatal("runtime Send did not begin")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after connection cancellation")
	}
	waitForOutboundWriterExit(t, worker)
}

type connectionCancellationClient struct {
	stream *connectionCancellationStream
}

func (c *connectionCancellationClient) ConnectWorker(ctx context.Context) (RuntimeStream, error) {
	c.stream.ctx = ctx
	return c.stream, nil
}

type connectionCancellationStream struct {
	ctx     context.Context
	sent    chan *turingv1.RuntimeUpdate
	recv    chan *turingv1.RuntimeCommand
	blocked chan struct{}
}

func (s *connectionCancellationStream) Send(update *turingv1.RuntimeUpdate) error {
	if update.GetWorkerReady() != nil {
		s.sent <- update
		return nil
	}
	select {
	case <-s.blocked:
	default:
		close(s.blocked)
	}
	<-s.ctx.Done()
	return s.ctx.Err()
}

func (s *connectionCancellationStream) Recv() (*turingv1.RuntimeCommand, error) {
	select {
	case command := <-s.recv:
		return command, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *connectionCancellationStream) CloseSend() error { return nil }

type emitBlockingUpdateExecutor struct{}

func (emitBlockingUpdateExecutor) Execute(_ context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error {
	return emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId: job.GetRunId(), Type: turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
	}}})
}
