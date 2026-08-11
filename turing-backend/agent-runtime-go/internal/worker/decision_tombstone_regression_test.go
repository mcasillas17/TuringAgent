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

func TestLateBeforeDecisionAfterTombstoneExpiryDoesNotSatisfyAfterWaiter(t *testing.T) {
	stream := newFakeStream()
	worker := New(Options{
		WorkerID: "worker-phase-correlation", MaxConcurrentRuns: 1, DecisionTombstoneTTL: 10 * time.Millisecond,
	}, &fakeRuntimeClient{stream: stream}, terminalExecutor{})
	worker.startOutboundWriter(stream)
	defer func() {
		worker.stopOutboundWriter()
		waitForOutboundWriterExit(t, worker)
	}()

	const toolCallID = "call_reused_across_phases"
	beforeCtx, cancelBefore := context.WithCancel(context.Background())
	beforeDone := make(chan error, 1)
	go func() {
		_, err := worker.postToolBeacon(beforeCtx, stream, &turingv1.ToolCallBeacon{
			ToolCallId: toolCallID,
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		})
		beforeDone <- err
	}()
	_ = nextSent(t, stream)
	cancelBefore()
	if err := <-beforeDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("BEFORE wait error = %v, want context.Canceled", err)
	}
	deadline := time.Now().Add(time.Second)
	for decisionWaiterCount(worker) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("BEFORE tombstone did not expire")
		}
		time.Sleep(time.Millisecond)
	}

	afterDone := make(chan *turingv1.ToolPolicyDecision, 1)
	go func() {
		decision, _ := worker.postToolBeacon(context.Background(), stream, &turingv1.ToolCallBeacon{
			ToolCallId: toolCallID,
			Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
		})
		afterDone <- decision
	}()
	_ = nextSent(t, stream)
	worker.deliverDecision(&turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: toolCallID,
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	})
	select {
	case decision := <-afterDone:
		t.Fatalf("late BEFORE decision satisfied AFTER waiter: %+v", decision)
	case <-time.After(25 * time.Millisecond):
	}

	worker.deliverDecision(&turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: toolCallID,
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
	})
	select {
	case decision := <-afterDone:
		if decision.GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
			t.Fatalf("AFTER decision phase = %s", decision.GetPhase())
		}
	case <-time.After(time.Second):
		t.Fatal("matching AFTER decision did not satisfy waiter")
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
