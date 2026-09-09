package worker

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestExplicitCancelRedeliveryAcknowledgesAlreadyExitedAttempt(t *testing.T) {
	provider := &blockingProvider{started: make(chan struct{}), cancelled: make(chan struct{})}
	stream := newFakeStream()
	worker, stop := startScriptedWorker(t, providerExecutor{provider: provider}, stream)
	defer stop()
	assignJob(t, stream, "run-explicit", "attempt-explicit", 2)
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}
	command := &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunCancelled{RunCancelled: &turingv1.RuntimeRunCancelled{
		RunId: "run-explicit", StateVersion: 3,
	}}}
	for range 2 {
		stream.recv <- command
		ack := nextSent(t, stream).GetRunCancelledAck()
		if ack == nil || ack.RunId != "run-explicit" || ack.ObservedStateVersion != 3 {
			t.Fatalf("cancellation ack = %v", ack)
		}
	}

	if worker.activeRun("run-explicit") != nil {
		t.Fatal("acknowledged executor is still active")
	}
	// Unversioned targets and commands preceding the finished attempt cannot
	// manufacture a versioned exit acknowledgement.
	for _, req := range []*turingv1.RuntimeRunCancelled{
		{RunId: "unknown"}, {RunId: "run-explicit", StateVersion: 1},
	} {
		if err := worker.handleCommand(context.Background(), stream, &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunCancelled{RunCancelled: req}}); err != nil {
			t.Fatal(err)
		}
		select {
		case extra := <-stream.sent:
			t.Fatalf("unproven stop manufactured ack: %v", extra)
		default:
		}
	}
	worker.mu.Lock()
	worker.forgetTerminalAttemptLocked("run-explicit")
	worker.mu.Unlock()
	stream.recv <- command
	if ack := nextSent(t, stream).GetRunCancelledAck(); ack == nil || ack.ObservedStateVersion != 3 {
		t.Fatalf("evicted receipt stranded a stopped run: %v", ack)
	}
}

func TestExplicitCancelRedeliveryCannotAcknowledgeInFlightSideEffect(t *testing.T) {
	release := make(chan struct{})
	effect := make(chan struct{})
	executor := newScriptedExecutor(func(_ *turingv1.AgentJob, _ func(*turingv1.RuntimeUpdate) error) error {
		<-release
		close(effect)
		return context.Canceled
	})
	stream := newFakeStream()
	worker, stop := startScriptedWorker(t, executor, stream)
	defer stop()
	assignJob(t, stream, "run-in-flight", "attempt-in-flight", 4)
	<-executor.started
	command := &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunCancelled{RunCancelled: &turingv1.RuntimeRunCancelled{RunId: "run-in-flight", StateVersion: 5}}}
	for range 2 {
		// Direct dispatch is a deterministic command barrier; the executor is
		// deliberately uncooperative until the already-started effect exits.
		if err := worker.handleCommand(context.Background(), stream, command); err != nil {
			t.Fatal(err)
		}
		select {
		case update := <-stream.sent:
			t.Fatalf("early exit claim: %v", update)
		default:
		}
	}
	if worker.activeRun("run-in-flight") == nil {
		t.Fatal("side effect lost containment before exit")
	}
	close(release)
	ack := nextSent(t, stream).GetRunCancelledAck()
	if ack == nil || ack.ObservedStateVersion != 5 {
		t.Fatalf("exit ack = %v", ack)
	}
	select {
	case <-effect:
	default:
		t.Fatal("ack claimed effect was rolled back instead of waiting for exit")
	}
}
