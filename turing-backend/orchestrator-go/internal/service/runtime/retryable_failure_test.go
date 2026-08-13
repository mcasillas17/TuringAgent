package runtime

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// TestWorkerBusyRejectionRequeuesWithoutImmediateRedispatch proves the fix for
// the drain-loop cascade: when a worker rejects an assignment with a retryable
// worker_busy failure, the run is requeued (not permanently failed) and the
// orchestrator does not immediately hand the same still-busy worker another
// assignment.
func TestWorkerBusyRejectionRequeuesWithoutImmediateRedispatch(t *testing.T) {
	h := newHarness(t)
	h.createSessionAndRun(t, "busy worker")
	stream, err := h.runtimeClient(t).ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-busy", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetRunAssigned() != nil
	}).GetRunAssigned()

	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
		RunId:     assigned.RunId,
		Code:      "worker_busy",
		Message:   "worker cannot accept the run",
		Retryable: true,
	}}}); err != nil {
		t.Fatal(err)
	}

	// The worker that just reported busy must NOT be handed another assignment
	// immediately; without suppression DispatchPending would re-dispatch the
	// requeued job straight back to the draining worker in a tight loop.
	if cmd := recvWithin(t, stream, 750*time.Millisecond); cmd != nil && cmd.GetRunAssigned() != nil {
		t.Fatalf("busy worker was immediately re-dispatched run %s", cmd.GetRunAssigned().RunId)
	}

	// The run must be requeued rather than permanently failed: with dispatch
	// suppressed it stays queued, waiting for a later trigger (a free worker,
	// a heartbeat, or the drain completing).
	run, err := h.repo.GetRun(context.Background(), assigned.RunId)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" {
		t.Fatalf("run %s status = %q, want queued (requeued, not failed)", assigned.RunId, run.Status)
	}
}

// recvWithin returns the next command received within timeout, or nil if none
// arrives before the timeout elapses.
func recvWithin(t *testing.T, stream turingv1.RuntimeService_ConnectWorkerClient, timeout time.Duration) *turingv1.RuntimeCommand {
	t.Helper()
	received := make(chan *turingv1.RuntimeCommand, 1)
	errs := make(chan error, 1)
	go func() {
		cmd, err := stream.Recv()
		if err != nil {
			errs <- err
			return
		}
		received <- cmd
	}()
	select {
	case cmd := <-received:
		return cmd
	case err := <-errs:
		t.Fatalf("stream recv: %v", err)
		return nil
	case <-time.After(timeout):
		return nil
	}
}
