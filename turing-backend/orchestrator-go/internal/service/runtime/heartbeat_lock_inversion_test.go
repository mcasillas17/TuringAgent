package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc"
)

// A heartbeat proves ownership of the runs a reconnected worker still holds and
// hands each proof back on a same-attempt refresh. Both halves used to happen
// under the worker's update lock, and the refresh is queued on the same bounded
// command buffer the command loop drains — while that loop needs the very same
// lock to deliver an assignment.
//
// At the maximum concurrency a worker may declare, those two facts close a
// cycle: the heartbeat holds the lock and waits for buffer space, and the only
// goroutine that can make buffer space waits for the lock. Nothing times out,
// because the stream context that reaches both has no deadline. The worker
// stops answering and every run it holds is stranded until the reaper notices.
//
// The barrier below puts the two goroutines in exactly that order rather than
// waiting for a scheduler to find it.
func TestHeartbeatProofRefreshSurvivesASaturatedCommandBuffer(t *testing.T) {
	const workerID = "worker-saturated-refresh"
	h := newHarnessWithDispatch(t, DispatchConfig{
		MaxConcurrentRuns: maxWorkerConcurrentRuns,
		LeaseDuration:     time.Minute,
	})

	// Two recovering assignments: one freed buffer slot is then not enough, so
	// the second refresh is the one that has to wait for the drain.
	first := h.deliverRecoveringAssignment(t, workerID, "saturated refresh one")
	second := h.deliverRecoveringAssignment(t, workerID, "saturated refresh two")

	commands := make(chan workerCommand, maxWorkerConcurrentRuns)
	connected := &worker{
		commands:      commands,
		done:          make(chan struct{}),
		capabilities:  testWorkerCapabilities(maxWorkerConcurrentRuns),
		maxConcurrent: maxWorkerConcurrentRuns,
		assignments: map[string]assignment{
			first.runID:  first.held,
			second.runID: second.held,
		},
		lastHeartbeat: time.Now().UTC(),
	}
	h.service.mu.Lock()
	h.service.workers[workerID] = connected
	h.service.mu.Unlock()

	stream := &parkingCommandStream{
		ctx:    context.Background(),
		parked: make(chan struct{}, 1),
		resume: make(chan struct{}),
	}

	// The first queued command needs no update lock, so the drain parks inside
	// the stream and leaves the lock free for the heartbeat to take. Every
	// other command is an assignment for a run this worker does not hold: the
	// delivery path takes the update lock and then drops it, which is exactly
	// the contention being tested without needing real delivery bookkeeping.
	commands <- workerCommand{command: &turingv1.RuntimeCommand{
		Command: &turingv1.RuntimeCommand_WorkerAccepted{
			WorkerAccepted: &turingv1.RuntimeWorkerAccepted{WorkerId: workerID},
		},
	}}
	for len(commands) < cap(commands) {
		commands <- unheldAssignmentCommand()
	}

	drained := &atomic.Int32{}
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			select {
			case <-connected.done:
				return
			case queued := <-commands:
				if err := h.service.sendCommand(context.Background(), stream, queued, connected, workerID); err != nil {
					return
				}
				drained.Add(1)
			}
		}
	}()
	t.Cleanup(func() {
		close(connected.done)
		select {
		case <-drainDone:
		case <-time.After(time.Second):
			// The drain is wedged on the update lock. The assertion below has
			// already reported that; waiting on it here would only replace a
			// readable failure with a package-wide test timeout.
		}
	})

	select {
	case <-stream.parked:
	case <-time.After(5 * time.Second):
		t.Fatal("command drain never reached the stream")
	}
	// Refill the slot the drain just took, so the buffer is full again at the
	// moment the heartbeat needs to queue its refreshes.
	commands <- unheldAssignmentCommand()

	heartbeat := make(chan error, 1)
	go func() {
		heartbeat <- h.service.renewWorkerLeases(
			context.Background(), workerID, connected,
			&turingv1.RuntimeHeartbeat{WorkerId: workerID},
		)
	}()

	// The proof commits before its refresh is queued, so a run that has left
	// recovering is proof that the heartbeat is inside the window under test.
	waitForRunLifecycle(t, h, first.runID, "running")
	close(stream.resume)

	select {
	case err := <-heartbeat:
		if err != nil {
			t.Fatalf("heartbeat: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("heartbeat deadlocked against a full command buffer")
	}

	for _, runID := range []string{first.runID, second.runID} {
		waitForRunLifecycle(t, h, runID, "running")
	}
	if got := drained.Load(); got == 0 {
		t.Fatal("command loop never delivered anything after the heartbeat")
	}
}

// unheldAssignmentCommand names a run this worker does not hold, so delivery
// takes the update lock, finds nothing to claim, and returns without touching
// the database.
func unheldAssignmentCommand() workerCommand {
	return workerCommand{command: &turingv1.RuntimeCommand{
		Command: &turingv1.RuntimeCommand_RunAssigned{
			RunAssigned: &turingv1.AgentJob{RunId: "run_not_held_by_this_worker"},
		},
	}}
}

type deliveredAssignment struct {
	runID string
	held  assignment
}

// deliverRecoveringAssignment builds one assignment the way the dispatch path
// does — claimed, sent, delivered — and then fences it into recovering, which
// is the state the heartbeat's ownership proof exists to resolve.
func (h *harness) deliverRecoveringAssignment(t *testing.T, workerID string, content string) deliveredAssignment {
	t.Helper()
	enqueued := h.enqueueRun(t, content)
	claimed, err := h.repo.ClaimNextJob(context.Background(), "general_assistant", workerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	repositoryAssignment := repository.Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: workerID, AttemptID: claimed.AssignmentAttemptID,
	}
	if err := h.repo.BeginAssignmentSend(context.Background(), repositoryAssignment); err != nil {
		t.Fatalf("BeginAssignmentSend: %v", err)
	}
	if err := h.repo.MarkAssignmentDelivered(context.Background(), repositoryAssignment); err != nil {
		t.Fatalf("MarkAssignmentDelivered: %v", err)
	}
	h.fenceOwnership(t, enqueued.RunID, workerID, claimed.AssignmentAttemptID)
	return deliveredAssignment{
		runID: enqueued.RunID,
		held: assignment{
			jobID:     claimed.JobID,
			runID:     claimed.RunID,
			attemptID: claimed.AssignmentAttemptID,
			job:       mapJob(claimed),
		},
	}
}

func waitForRunLifecycle(t *testing.T, h *harness, runID string, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state := h.runState(t, runID)
		if state.Lifecycle == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s lifecycle = %q, want %q", runID, state.Lifecycle, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// parkingCommandStream holds the first command inside Send until the test lets
// it go, and passes everything afterwards straight through.
type parkingCommandStream struct {
	grpc.ServerStream
	ctx    context.Context
	parked chan struct{}
	resume chan struct{}
	sends  atomic.Int32
}

func (s *parkingCommandStream) Send(*turingv1.RuntimeCommand) error {
	if s.sends.Add(1) == 1 {
		s.parked <- struct{}{}
		<-s.resume
	}
	return nil
}

func (s *parkingCommandStream) Recv() (*turingv1.RuntimeUpdate, error) {
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *parkingCommandStream) Context() context.Context { return s.ctx }
