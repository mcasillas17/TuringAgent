package worker

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
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

func TestWorkerNonterminalUpdateSendTimeoutReleasesActiveRun(t *testing.T) {
	stream := &boundedUpdateStream{
		sent:    make(chan *turingv1.RuntimeUpdate, 1),
		recv:    make(chan *turingv1.RuntimeCommand, 1),
		blocked: make(chan struct{}),
		release: make(chan struct{}),
	}
	worker := New(Options{
		WorkerID:          "worker-update-timeout",
		UpdateSendTimeout: 20 * time.Millisecond,
	}, &boundedUpdateClient{stream: stream}, emitBlockingUpdateExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Cleanup(func() { stream.releaseOnce.Do(func() { close(stream.release) }) })
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	if ready := <-stream.sent; ready.GetWorkerReady() == nil {
		t.Fatalf("first update = %+v, want worker ready", ready)
	}
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{RunId: "run_update_timeout"}}}
	select {
	case <-stream.blocked:
	case <-time.After(time.Second):
		t.Fatal("nonterminal update Send did not begin")
	}
	waitForInactiveRun(t, worker, "run_update_timeout")
	stream.releaseOnce.Do(func() { close(stream.release) })
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after the blocked update was released")
	}
}

func TestWorkerHeartbeatSendTimeoutStopsBlockedConnection(t *testing.T) {
	stream := &connectionCancellationStream{
		sent:    make(chan *turingv1.RuntimeUpdate, 1),
		recv:    make(chan *turingv1.RuntimeCommand, 1),
		blocked: make(chan struct{}),
	}
	worker := New(Options{
		WorkerID:          "worker-heartbeat-timeout",
		HeartbeatInterval: time.Millisecond,
		UpdateSendTimeout: 20 * time.Millisecond,
	}, &connectionCancellationClient{stream: stream}, terminalExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	if ready := <-stream.sent; ready.GetWorkerReady() == nil {
		t.Fatalf("first update = %+v, want worker ready", ready)
	}
	select {
	case <-stream.blocked:
	case <-time.After(time.Second):
		t.Fatal("heartbeat Send did not begin")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Worker.Run returned %v, want heartbeat send deadline", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Worker.Run remained blocked after heartbeat send timeout")
	}
	waitForOutboundWriterExit(t, worker)
}

func TestWorkerDisconnectCancelsAndJoinsActiveExecutorBeforeReturning(t *testing.T) {
	stream := &disconnectingStream{
		sent:       make(chan *turingv1.RuntimeUpdate, 1),
		recv:       make(chan *turingv1.RuntimeCommand, 1),
		disconnect: make(chan struct{}),
	}
	executor := &disconnectBlockingExecutor{
		started:   make(chan struct{}),
		cancelled: make(chan error, 1),
		release:   make(chan struct{}),
		exited:    make(chan struct{}),
	}
	worker := New(Options{WorkerID: "worker-disconnect-cleanup"}, &disconnectingClient{stream: stream}, executor)
	done := make(chan error, 1)
	go func() { done <- worker.Run(context.Background()) }()

	ready := <-stream.sent
	if ready.GetWorkerReady() == nil {
		t.Fatalf("first update = %+v, want worker ready", ready)
	}
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{RunId: "run_disconnect"}}}
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}
	close(stream.disconnect)

	select {
	case cause := <-executor.cancelled:
		if cause == nil || cause.Error() != "runtime stream disconnected" {
			t.Fatalf("disconnect cancellation cause = %v, want runtime disconnect", cause)
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect did not cancel the active executor")
	}
	select {
	case err := <-done:
		t.Fatalf("Run returned %v before the active executor exited", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(executor.release)
	select {
	case <-executor.exited:
	case <-time.After(time.Second):
		t.Fatal("executor did not exit after release")
	}
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Run returned %v, want EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after executor exit")
	}
	waitForOutboundWriterExit(t, worker)
}

func TestWorkerDisconnectBoundsUncooperativeExecutorCleanup(t *testing.T) {
	stream := &disconnectingStream{
		sent:       make(chan *turingv1.RuntimeUpdate, 1),
		recv:       make(chan *turingv1.RuntimeCommand, 1),
		disconnect: make(chan struct{}),
	}
	executor := &disconnectBlockingExecutor{
		started:   make(chan struct{}),
		cancelled: make(chan error, 1),
		release:   make(chan struct{}),
		exited:    make(chan struct{}),
	}
	worker := New(Options{
		WorkerID:                 "worker-disconnect-timeout",
		DisconnectCleanupTimeout: 20 * time.Millisecond,
	}, &disconnectingClient{stream: stream}, executor)
	done := make(chan error, 1)
	go func() { done <- worker.Run(context.Background()) }()

	<-stream.sent
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{RunId: "run_disconnect_timeout"}}}
	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}
	close(stream.disconnect)
	select {
	case <-executor.cancelled:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not cancel the active executor")
	}
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Run returned %v, want EOF", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not respect the configured cleanup timeout")
	}
	close(executor.release)
	select {
	case <-executor.exited:
	case <-time.After(time.Second):
		t.Fatal("uncooperative executor did not eventually exit")
	}
	waitForInactiveRun(t, worker, "run_disconnect_timeout")
}

func TestWorkerReconnectWaitsForPriorExecutorsToDrain(t *testing.T) {
	first, second := newFakeStream(), newFakeStream()
	executor := &reconnectDrainExecutor{
		firstStarted:   make(chan struct{}),
		firstCancelled: make(chan struct{}),
		firstRelease:   make(chan struct{}),
		secondStarted:  make(chan struct{}),
	}
	worker := New(Options{
		WorkerID:                 "worker-reconnect-drain",
		MaxConcurrentRuns:        1,
		DisconnectCleanupTimeout: 20 * time.Millisecond,
	}, &fakeRuntimeClient{stream: second, queued: []*fakeStream{first}}, executor)

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { firstDone <- worker.Run(firstCtx) }()
	<-first.sent
	first.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{
		RunAssigned: &turingv1.AgentJob{RunId: "run-reconnect-drain", Attempt: 1},
	}}
	select {
	case <-executor.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first executor did not start")
	}
	cancelFirst()
	select {
	case <-executor.firstCancelled:
	case <-time.After(time.Second):
		t.Fatal("first executor was not cancelled")
	}
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first Run did not respect the cleanup timeout")
	}

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	secondDone := make(chan error, 1)
	go func() { secondDone <- worker.Run(secondCtx) }()
	select {
	case update := <-second.sent:
		t.Fatalf("reconnected worker advertised capacity before draining prior executor: %+v", update)
	case <-time.After(100 * time.Millisecond):
	}

	close(executor.firstRelease)
	select {
	case ready := <-second.sent:
		if ready.GetWorkerReady() == nil {
			t.Fatalf("first update after drain = %+v, want worker_ready", ready)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not reconnect after prior executor drained")
	}
	second.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{
		RunAssigned: &turingv1.AgentJob{RunId: "run-reconnect-drain", Attempt: 2},
	}}
	select {
	case <-executor.secondStarted:
	case <-time.After(time.Second):
		t.Fatal("reconnected worker did not accept the requeued run")
	}
	cancelSecond()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("second Run did not stop")
	}
}

func TestWorkerRunTerminationNeverClosesSendDuringBlockedOutboundSend(t *testing.T) {
	stream := &closeRaceStream{
		sent:               make(chan *turingv1.RuntimeUpdate, 1),
		recv:               make(chan *turingv1.RuntimeCommand, 1),
		blocked:            make(chan struct{}),
		cancellationSeen:   make(chan struct{}),
		releaseBlockedSend: make(chan struct{}),
		closeDuringSend:    make(chan struct{}),
	}
	worker := New(Options{WorkerID: "worker-send-close-race"}, &closeRaceClient{stream: stream}, emitBlockingUpdateExecutor{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	ready := <-stream.sent
	if ready.GetWorkerReady() == nil {
		t.Fatalf("first update = %+v, want worker ready", ready)
	}
	stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{RunId: "run_send_close_race"}}}
	select {
	case <-stream.blocked:
	case <-time.After(time.Second):
		t.Fatal("runtime Send did not block")
	}
	cancel()
	select {
	case <-stream.cancellationSeen:
	case <-time.After(time.Second):
		t.Fatal("blocked Send did not observe connection cancellation")
	}
	select {
	case <-stream.closeDuringSend:
		t.Fatal("CloseSend ran concurrently with a blocked Send")
	case <-time.After(20 * time.Millisecond):
	}
	close(stream.releaseBlockedSend)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after blocked Send was released")
	}
	select {
	case <-stream.closeDuringSend:
		t.Fatal("CloseSend ran concurrently with Send")
	default:
	}
	waitForOutboundWriterExit(t, worker)
}

type connectionCancellationClient struct {
	stream *connectionCancellationStream
}

type boundedUpdateClient struct{ stream *boundedUpdateStream }

func (c *boundedUpdateClient) ConnectWorker(ctx context.Context) (RuntimeStream, error) {
	c.stream.ctx = ctx
	return c.stream, nil
}

type boundedUpdateStream struct {
	ctx         context.Context
	sent        chan *turingv1.RuntimeUpdate
	recv        chan *turingv1.RuntimeCommand
	blocked     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	sendCount   atomic.Int32
}

func (s *boundedUpdateStream) Send(update *turingv1.RuntimeUpdate) error {
	if update.GetWorkerReady() != nil {
		s.sent <- update
		return nil
	}
	if s.sendCount.Add(1) == 1 {
		close(s.blocked)
		<-s.release
	}
	return nil
}

func (s *boundedUpdateStream) Recv() (*turingv1.RuntimeCommand, error) {
	select {
	case command := <-s.recv:
		return command, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
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

type emitBlockingUpdateExecutor struct{}

func (emitBlockingUpdateExecutor) Execute(_ context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error {
	return emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId: job.GetRunId(), Type: turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
	}}})
}

type disconnectingClient struct{ stream *disconnectingStream }

func (c *disconnectingClient) ConnectWorker(ctx context.Context) (RuntimeStream, error) {
	c.stream.ctx = ctx
	return c.stream, nil
}

type disconnectingStream struct {
	ctx        context.Context
	sent       chan *turingv1.RuntimeUpdate
	recv       chan *turingv1.RuntimeCommand
	disconnect chan struct{}
}

func (s *disconnectingStream) Send(update *turingv1.RuntimeUpdate) error {
	select {
	case <-s.disconnect:
		return io.ErrClosedPipe
	default:
	}
	s.sent <- update
	return nil
}

func (s *disconnectingStream) Recv() (*turingv1.RuntimeCommand, error) {
	select {
	case command := <-s.recv:
		return command, nil
	case <-s.disconnect:
		return nil, io.EOF
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

type disconnectBlockingExecutor struct {
	started   chan struct{}
	cancelled chan error
	release   chan struct{}
	exited    chan struct{}
}

func (e *disconnectBlockingExecutor) Execute(ctx context.Context, _ *turingv1.AgentJob, _ func(*turingv1.RuntimeUpdate) error) error {
	close(e.started)
	<-ctx.Done()
	e.cancelled <- context.Cause(ctx)
	<-e.release
	close(e.exited)
	return ctx.Err()
}

type reconnectDrainExecutor struct {
	calls          atomic.Int32
	firstStarted   chan struct{}
	firstCancelled chan struct{}
	firstRelease   chan struct{}
	secondStarted  chan struct{}
}

func (e *reconnectDrainExecutor) Execute(ctx context.Context, _ *turingv1.AgentJob, _ func(*turingv1.RuntimeUpdate) error) error {
	if e.calls.Add(1) == 1 {
		close(e.firstStarted)
		<-ctx.Done()
		close(e.firstCancelled)
		<-e.firstRelease
		return ctx.Err()
	}
	close(e.secondStarted)
	<-ctx.Done()
	return ctx.Err()
}

type closeRaceClient struct{ stream *closeRaceStream }

func (c *closeRaceClient) ConnectWorker(ctx context.Context) (RuntimeStream, error) {
	c.stream.ctx = ctx
	return c.stream, nil
}

type closeRaceStream struct {
	ctx                context.Context
	sent               chan *turingv1.RuntimeUpdate
	recv               chan *turingv1.RuntimeCommand
	blocked            chan struct{}
	cancellationSeen   chan struct{}
	releaseBlockedSend chan struct{}
	closeDuringSend    chan struct{}
	inSend             atomic.Bool
	closeOnce          sync.Once
}

func (s *closeRaceStream) Send(update *turingv1.RuntimeUpdate) error {
	if update.GetWorkerReady() != nil {
		s.sent <- update
		return nil
	}
	s.inSend.Store(true)
	defer s.inSend.Store(false)
	close(s.blocked)
	<-s.ctx.Done()
	close(s.cancellationSeen)
	<-s.releaseBlockedSend
	return s.ctx.Err()
}

func (s *closeRaceStream) Recv() (*turingv1.RuntimeCommand, error) {
	select {
	case command := <-s.recv:
		return command, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *closeRaceStream) CloseSend() error {
	if s.inSend.Load() {
		s.closeOnce.Do(func() { close(s.closeDuringSend) })
	}
	return nil
}
