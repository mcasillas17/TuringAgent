package worker

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestActiveRunClaimsExactlyOneTerminalReportAcrossConcurrentExitAndCancellation(t *testing.T) {
	const interleavings = 200

	for attempt := 0; attempt < interleavings; attempt++ {
		entry := &activeRun{}
		start := make(chan struct{})
		var winners atomic.Int32
		var wait sync.WaitGroup
		wait.Add(2)
		for range 2 {
			go func() {
				defer wait.Done()
				<-start
				if entry.claimTerminalReport() {
					winners.Add(1)
				}
			}()
		}
		close(start)
		wait.Wait()

		if got := winners.Load(); got != 1 {
			t.Fatalf("interleaving %d claimed %d terminal reports, want 1", attempt, got)
		}
	}
}

func TestWorkerSendsOneTerminalUpdateAcrossConcurrentExitAndCancellation(t *testing.T) {
	const interleavings = 200

	for attempt := 0; attempt < interleavings; attempt++ {
		executor := &terminalBlockingExecutor{reported: make(chan struct{}), release: make(chan struct{})}
		stream := newFakeStream()
		worker := New(Options{WorkerID: "worker-terminal-race", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, executor)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- worker.Run(ctx) }()

		_ = nextSent(t, stream)
		runID := "run_terminal_race"
		stream.recv <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: &turingv1.AgentJob{
			JobId: "job_terminal_race", RunId: runID, AssistantMessageId: "msg_terminal_race",
		}}}
		select {
		case <-executor.reported:
		case <-time.After(time.Second):
			t.Fatalf("interleaving %d executor did not report terminal update", attempt)
		}

		entry := worker.activeRun(runID)
		if entry == nil {
			t.Fatalf("interleaving %d run was not active", attempt)
		}
		entry.mu.Lock()
		cancelStarted := make(chan struct{})
		go func() {
			close(cancelStarted)
			_ = worker.cancelRunWithCause(context.Background(), stream, runID, context.Canceled)
		}()
		<-cancelStarted
		for range 10 {
			runtime.Gosched()
		}
		close(executor.release)
		entry.mu.Unlock()

		first := nextSent(t, stream)
		if !isTerminalRunUpdate(first) && first.GetRunCancelledAck() == nil {
			t.Fatalf("interleaving %d first terminal update = %+v", attempt, first)
		}
		terminalCount := 1
		timer := time.NewTimer(10 * time.Millisecond)
		for {
			select {
			case update := <-stream.sent:
				if isTerminalRunUpdate(update) || update.GetRunCancelledAck() != nil {
					terminalCount++
				}
			case <-timer.C:
				if terminalCount != 1 {
					t.Fatalf("interleaving %d sent %d terminal updates, want 1", attempt, terminalCount)
				}
				goto complete
			}
		}

	complete:
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("interleaving %d worker Run returned %v", attempt, err)
		}
		waitForOutboundWriterExit(t, worker)
	}
}
