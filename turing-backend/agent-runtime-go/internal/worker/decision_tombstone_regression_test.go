package worker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestTimedOutDecisionTombstonesAreReapedWithoutResponses(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{WorkerID: "worker-tombstone", MaxConcurrentRuns: 1}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	worker.startOutboundWriter(stream)
	defer func() {
		worker.stopOutboundWriter()
		waitForOutboundWriterExit(t, worker)
	}()

	for index := 0; index < 64; index++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func(toolCallID string) {
			_, err := worker.postToolBeacon(ctx, stream, &turingv1.ToolCallBeacon{ToolCallId: toolCallID})
			done <- err
		}(fmt.Sprintf("call_timeout_%d", index))
		_ = nextSent(t, stream)
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("timed-out beacon %d error = %v, want context.Canceled", index, err)
		}
	}
	if count := decisionWaiterCount(worker); count != 64 {
		t.Fatalf("tombstone count = %d, want one per sent timeout before reaping", count)
	}

	deadline := time.Now().Add(750 * time.Millisecond)
	for {
		if count := decisionWaiterCount(worker); count == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed-out tombstones remained after TTL: %d", decisionWaiterCount(worker))
		}
		time.Sleep(time.Millisecond)
	}
}

func decisionWaiterCount(worker *Worker) int {
	worker.decisionMu.Lock()
	defer worker.decisionMu.Unlock()
	count := 0
	for _, waiters := range worker.decisions {
		count += len(waiters)
	}
	return count
}
