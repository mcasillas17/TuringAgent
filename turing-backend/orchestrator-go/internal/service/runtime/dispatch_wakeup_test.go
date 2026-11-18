package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// A dispatch that finds every free slot on a worker held by claims in flight
// passes the worker over. A claim that read the queue before the job that
// dispatch was for, and so comes back empty, starts the dispatch over: the job
// is assigned now rather than left queued for the next reaper tick. With two
// slots, whichever held claim is let go first takes the job, and it is
// assigned once.
func TestAJobQueuedDuringAnEmptyClaimIsStillDispatched(t *testing.T) {
	for _, tc := range []struct {
		name    string
		slots   int32
		release []int
	}{
		{name: "one slot", slots: 1, release: []int{0}},
		{name: "two slots, first claim let go first", slots: 2, release: []int{0, 1}},
		{name: "two slots, second claim let go first", slots: 2, release: []int{1, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			connected := registerWorkerCapabilities(t, h, "worker-wakeup", "registration-wakeup", modelCapabilities(
				turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, tc.slots,
			))
			holds := make([]chan struct{}, tc.slots)
			releases := make([]func(), tc.slots)
			for i := range holds {
				holds[i] = make(chan struct{})
				releases[i] = sync.OnceFunc(func() { close(holds[i]) })
				// A test that stops early leaves no dispatch held behind it.
				t.Cleanup(releases[i])
			}
			claimed := make(chan struct{}, tc.slots)
			var emptyClaims atomic.Int32
			h.service.afterClaim = func(job repository.Job) {
				if job.JobID != "" {
					return
				}
				if n := emptyClaims.Add(1) - 1; n < tc.slots {
					claimed <- struct{}{}
					<-holds[n]
				}
			}

			// Each dispatch finds the queue empty and is held with its claim
			// still holding one of the worker's slots, until all are held.
			dispatches := make([]chan error, tc.slots)
			for i := range dispatches {
				dispatches[i] = make(chan error, 1)
				go func() { dispatches[i] <- h.service.DispatchPending(context.Background()) }()
				select {
				case <-claimed:
				case err := <-dispatches[i]:
					t.Fatalf("dispatch %d returned %v before it claimed", i, err)
				case <-time.After(5 * time.Second):
					t.Fatalf("dispatch %d never claimed", i)
				}
			}
			queued := h.enqueueRun(t, "arrived during the claims")
			// The dispatch the enqueue asks for meets only held slots and moves on.
			if err := h.service.DispatchPending(context.Background()); err != nil {
				t.Fatal(err)
			}
			select {
			case command := <-connected.commands:
				t.Fatalf("assigned %v while every slot was held, want the worker passed over", command.command)
			default:
			}

			for _, i := range tc.release {
				releases[i]()
				if err := <-dispatches[i]; err != nil {
					t.Fatal(err)
				}
			}
			if got := assignedRun(t, connected); got != queued.RunID {
				t.Fatalf("assigned %q, want the run queued during the claims %q", got, queued.RunID)
			}
			select {
			case command := <-connected.commands:
				t.Fatalf("assigned again: %v, want the run assigned once", command.command)
			default:
			}
		})
	}
}

// An empty claim that no dispatch passed over ends the dispatch: the worker is
// asked once.
func TestAnEmptyClaimNobodyPassedOverEndsTheDispatch(t *testing.T) {
	h := newHarness(t)
	registerWorkerCapabilities(t, h, "worker-idle", "registration-idle", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	var claims atomic.Int32
	h.service.afterClaim = func(repository.Job) { claims.Add(1) }
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := claims.Load(); got != 1 {
		t.Fatalf("claims = %d, want one", got)
	}
}
