package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc"
)

func TestConnectWorkerBoundsBlockedAssignmentSendAndRecovers(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{LeaseDuration: time.Second})
	enqueued := h.enqueueRun(t, "blocked assignment send")
	streamCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	stream := &blockedAssignmentSendStream{
		ctx: streamCtx, release: make(chan struct{}), assignmentStarted: make(chan struct{}), assignmentExited: make(chan struct{}),
		ready: &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: "worker-blocked-send", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		}}},
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(stream.release) }) }
	t.Cleanup(func() {
		release()
		select {
		case <-stream.assignmentExited:
		case <-time.After(time.Second):
			t.Error("blocked assignment send did not exit after connection teardown")
		}
	})

	done := make(chan error, 1)
	go func() { done <- h.service.ConnectWorker(stream) }()
	select {
	case <-stream.assignmentStarted:
	case <-time.After(time.Second):
		t.Fatal("assignment send did not start")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ConnectWorker error = %v, want send deadline", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("blocked assignment send held ConnectWorker past its deadline")
	}

	release()
	select {
	case <-stream.assignmentExited:
	case <-time.After(time.Second):
		t.Fatal("blocked assignment send leaked after handler teardown")
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" || !run.ExecutionActive || run.ExecutionState != "uncertain" {
		t.Fatalf("blocked-send run = %+v, want active uncertain fence", run)
	}
	expired := time.Now().Add(-time.Second)
	if _, err := h.database.ExecContext(context.Background(), `
		UPDATE agent_runs
		SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
		WHERE id = ?
	`, expired.Format("2006-01-02T15:04:05.000000000Z"), expired.UnixNano(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := h.service.RecoverOrphanedAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err = h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" || run.ExecutionActive {
		t.Fatalf("recovered blocked-send run = %+v, want queued inactive", run)
	}
}

func TestSendCommandSerializesConcurrentStreamSends(t *testing.T) {
	h := newHarness(t)
	stream := &serialBlockingCommandStream{
		ctx: context.Background(), entered: make(chan struct{}, 2), release: make(chan struct{}),
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(stream.release) }) }
	t.Cleanup(release)
	connected := &worker{commands: make(chan *turingv1.RuntimeCommand, 1), assignments: map[string]assignment{}}
	results := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			results <- h.service.sendCommand(context.Background(), stream, &turingv1.RuntimeCommand{
				Command: &turingv1.RuntimeCommand_WorkerAccepted{WorkerAccepted: &turingv1.RuntimeWorkerAccepted{WorkerId: "worker-serial"}},
			}, connected, "worker-serial")
		}()
	}
	close(start)
	select {
	case <-stream.entered:
	case <-time.After(time.Second):
		t.Fatal("first command did not enter stream Send")
	}
	select {
	case <-stream.entered:
		release()
		<-results
		<-results
		t.Fatal("concurrent command entered stream Send")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("serialized command send: %v", err)
		}
	}
	if stream.maxActive.Load() != 1 {
		t.Fatalf("concurrent stream sends = %d, want one", stream.maxActive.Load())
	}
}

func TestRecoveryReclaimsAssignmentWithoutTimelyWorkerHeartbeat(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{LeaseDuration: time.Second})
	enqueued := h.enqueueRun(t, "stale heartbeat")
	claimed, err := h.repo.ClaimNextJob(context.Background(), "general_assistant", "worker-stale-heartbeat")
	if err != nil {
		t.Fatal(err)
	}
	repoAssignment := repository.Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-stale-heartbeat", AttemptID: claimed.AssignmentAttemptID,
	}
	if err := h.repo.BeginAssignmentSend(context.Background(), repoAssignment); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkAssignmentDelivered(context.Background(), repoAssignment); err != nil {
		t.Fatal(err)
	}
	connected := &worker{
		commands:      make(chan *turingv1.RuntimeCommand, 1),
		maxConcurrent: 1,
		assignments: map[string]assignment{
			repoAssignment.RunID: {jobID: repoAssignment.JobID, runID: repoAssignment.RunID, attemptID: repoAssignment.AttemptID},
		},
	}
	h.service.mu.Lock()
	h.service.workers[repoAssignment.WorkerID] = connected
	h.service.mu.Unlock()
	expired := time.Now().Add(-time.Second)
	if _, err := h.database.ExecContext(context.Background(), `
		UPDATE agent_runs
		SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
		WHERE id = ?
	`, expired.Format("2006-01-02T15:04:05.000000000Z"), expired.UnixNano(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}

	if err := h.service.RecoverOrphanedAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" || run.ExecutionActive {
		t.Fatalf("stale-heartbeat assignment = %+v, want queued inactive", run)
	}
	if connected.hasAssignment(enqueued.RunID) {
		t.Fatal("stale-heartbeat recovery retained the worker's in-memory assignment")
	}
}

func TestHeartbeatReconcilesAssignmentThatDatabaseCannotRenew(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{LeaseDuration: time.Minute})
	enqueued := h.enqueueRun(t, "terminalized approval heartbeat")
	claimed, err := h.repo.ClaimNextJob(context.Background(), "general_assistant", "worker-terminal-heartbeat")
	if err != nil {
		t.Fatal(err)
	}
	repoAssignment := repository.Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-terminal-heartbeat", AttemptID: claimed.AssignmentAttemptID,
	}
	if err := h.repo.BeginAssignmentSend(context.Background(), repoAssignment); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkAssignmentDelivered(context.Background(), repoAssignment); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.RecordToolCallBefore(context.Background(), repository.ToolCallRecord{
		ToolCallID: "call_terminal_heartbeat", RunID: enqueued.RunID, Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:test"); err != nil {
		t.Fatal(err)
	}
	approval, err := h.repo.CreateApproval(context.Background(), enqueued.RunID, "call_terminal_heartbeat", "general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:test", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.DenyApprovalWithEvent(context.Background(), approval.ApprovalID, ""); err != nil {
		t.Fatal(err)
	}
	connected := &worker{
		commands:      make(chan *turingv1.RuntimeCommand, 1),
		maxConcurrent: 1,
		assignments: map[string]assignment{
			repoAssignment.RunID: {jobID: repoAssignment.JobID, runID: repoAssignment.RunID, attemptID: repoAssignment.AttemptID},
		},
		lastHeartbeat: time.Now().UTC(),
	}

	if err := h.service.renewWorkerLeases(context.Background(), repoAssignment.WorkerID, connected, &turingv1.RuntimeHeartbeat{
		WorkerId: repoAssignment.WorkerID,
	}); err != nil {
		t.Fatal(err)
	}
	if connected.hasAssignment(enqueued.RunID) {
		t.Fatal("heartbeat retained an assignment that the database could not renew")
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionActive {
		t.Fatalf("terminalized run remained execution-active after heartbeat reconciliation: %+v", run)
	}
}

type blockedAssignmentSendStream struct {
	grpc.ServerStream
	ctx               context.Context
	ready             *turingv1.RuntimeUpdate
	readySent         bool
	release           chan struct{}
	assignmentStarted chan struct{}
	assignmentExited  chan struct{}
	startOnce         sync.Once
	exitOnce          sync.Once
}

func (s *blockedAssignmentSendStream) Send(command *turingv1.RuntimeCommand) error {
	if command.GetRunAssigned() == nil {
		return nil
	}
	s.startOnce.Do(func() { close(s.assignmentStarted) })
	<-s.release
	s.exitOnce.Do(func() { close(s.assignmentExited) })
	return errors.New("blocked assignment send released")
}

func (s *blockedAssignmentSendStream) Recv() (*turingv1.RuntimeUpdate, error) {
	if !s.readySent {
		s.readySent = true
		return s.ready, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *blockedAssignmentSendStream) Context() context.Context { return s.ctx }

type serialBlockingCommandStream struct {
	grpc.ServerStream
	ctx       context.Context
	entered   chan struct{}
	release   chan struct{}
	active    atomic.Int32
	maxActive atomic.Int32
}

func (s *serialBlockingCommandStream) Send(*turingv1.RuntimeCommand) error {
	active := s.active.Add(1)
	for {
		maximum := s.maxActive.Load()
		if active <= maximum || s.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	s.entered <- struct{}{}
	<-s.release
	s.active.Add(-1)
	return nil
}

func (s *serialBlockingCommandStream) Recv() (*turingv1.RuntimeUpdate, error) {
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *serialBlockingCommandStream) Context() context.Context { return s.ctx }
