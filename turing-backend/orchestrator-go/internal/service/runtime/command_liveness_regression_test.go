package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func TestTerminalUpdateCannotOvertakeAssignmentDeliveryMark(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{LeaseDuration: time.Second})
	enqueued := h.enqueueRun(t, "fast terminal update")
	streamCtx, cancel := context.WithCancel(context.Background())
	stream := &fastTerminalUpdateStream{
		ctx:      streamCtx,
		repo:     h.repo,
		service:  h.service,
		workerID: "worker-fast-terminal",
		ready: &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: "worker-fast-terminal", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		}}},
		completed: &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
			RunId: enqueued.RunID, AssistantMessageId: enqueued.AssistantMessageID, Content: "done",
		}}},
		runID:             enqueued.RunID,
		assignmentStarted: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() { done <- h.service.ConnectWorker(stream) }()

	deadline := time.Now().Add(time.Second)
	for {
		run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run status = %q, want completed", run.Status)
		}
		time.Sleep(time.Millisecond)
	}
	if stream.deliveryWasUnserialized.Load() {
		t.Fatal("assignment send and delivery marking did not exclude terminal updates")
	}
	if stream.completedBeforeDelivery.Load() {
		t.Fatal("terminal update committed before assignment delivery marking completed")
	}
	select {
	case err := <-done:
		t.Fatalf("worker disconnected after successful terminal update: %v", err)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, errWorkerStreamCancelled) {
			t.Fatalf("ConnectWorker exit = %v, want %v", err, errWorkerStreamCancelled)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after cancellation")
	}
}

func TestClosedWorkerSendReportsDisconnected(t *testing.T) {
	worker := &worker{
		commands:    make(chan *turingv1.RuntimeCommand, 1),
		done:        make(chan struct{}),
		assignments: map[string]assignment{},
	}
	worker.close()

	err := worker.send(context.Background(), &turingv1.RuntimeCommand{
		Command: &turingv1.RuntimeCommand_ShutdownRequested{
			ShutdownRequested: &turingv1.RuntimeShutdownRequested{Reason: "test"},
		},
	})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("closed worker send error = %v, want Canceled", err)
	}
	select {
	case command := <-worker.commands:
		t.Fatalf("closed worker accepted command: %+v", command)
	default:
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
		done:          make(chan struct{}),
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
	connected.mu.Lock()
	closed := connected.closed
	connected.mu.Unlock()
	if !closed {
		t.Fatal("stale-heartbeat recovery left the ambiguous worker stream open")
	}
	h.service.mu.Lock()
	registered := h.service.workers[repoAssignment.WorkerID]
	h.service.mu.Unlock()
	if registered != nil {
		t.Fatal("stale-heartbeat recovery retained the worker registration")
	}
	if release, err := connected.beginUpdate(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{
		Event: &turingv1.TuringEvent{RunId: enqueued.RunID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA},
	}}); status.Code(err) != codes.Canceled {
		if release != nil {
			release()
		}
		t.Fatalf("stale worker update error = %v, want Canceled", err)
	}
}

func TestRecoveryReclaimsAssignmentAfterSameIDWorkerReconnects(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{LeaseDuration: time.Second})
	enqueued := h.enqueueRun(t, "same worker ID reconnect")
	const workerID = "worker-reconnected"
	claimed, err := h.repo.ClaimNextJob(context.Background(), "general_assistant", workerID)
	if err != nil {
		t.Fatal(err)
	}
	staleAssignment := repository.Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: workerID, AttemptID: claimed.AssignmentAttemptID,
	}
	if err := h.repo.BeginAssignmentSend(context.Background(), staleAssignment); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkAssignmentDelivered(context.Background(), staleAssignment); err != nil {
		t.Fatal(err)
	}
	reconnected := &worker{
		commands:      make(chan *turingv1.RuntimeCommand, 1),
		done:          make(chan struct{}),
		maxConcurrent: 1,
		assignments:   map[string]assignment{},
		lastHeartbeat: time.Now().UTC(),
	}
	h.service.mu.Lock()
	h.service.workers[workerID] = reconnected
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

	select {
	case command := <-reconnected.commands:
		assigned := command.GetRunAssigned()
		if assigned == nil || assigned.GetRunId() != enqueued.RunID || assigned.GetAttempt() != 2 {
			t.Fatalf("reconnected worker command = %+v, want second assignment for %q", command, enqueued.RunID)
		}
	default:
		run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("reconnected worker received no recovered assignment; run = %+v", run)
	}
	reconnected.mu.Lock()
	closed := reconnected.closed
	reconnected.mu.Unlock()
	if closed {
		t.Fatal("recovery closed the replacement worker stream")
	}
}

func TestHeartbeatRevivalDispatchesQueuedWorkToIdleWorker(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{LeaseDuration: time.Second})
	enqueued := h.enqueueRun(t, "dispatch after heartbeat revival")
	connected := &worker{
		commands:      make(chan *turingv1.RuntimeCommand, 1),
		done:          make(chan struct{}),
		maxConcurrent: 1,
		assignments:   map[string]assignment{},
		lastHeartbeat: time.Now().Add(-time.Minute),
	}
	const workerID = "worker-revived"
	h.service.mu.Lock()
	h.service.workers[workerID] = connected
	h.service.mu.Unlock()

	if err := h.service.renewWorkerLeases(context.Background(), workerID, connected, &turingv1.RuntimeHeartbeat{
		WorkerId: workerID,
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case command := <-connected.commands:
		if assigned := command.GetRunAssigned(); assigned == nil || assigned.GetRunId() != enqueued.RunID {
			t.Fatalf("revival command = %+v, want assignment for %q", command, enqueued.RunID)
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat revival did not dispatch queued work")
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
	if _, err := h.repo.DenyApprovalWithEvent(context.Background(), approval.ApprovalID, sql.NullString{}, ""); err != nil {
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

func TestConnectWorkerReplacesExpiredIdleSameIDRegistration(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{LeaseDuration: time.Second})
	const workerID = "worker-idle-reconnect"
	stale := &worker{
		commands:      make(chan *turingv1.RuntimeCommand, 1),
		done:          make(chan struct{}),
		maxConcurrent: 1,
		assignments:   map[string]assignment{},
		lastHeartbeat: time.Now().Add(-time.Minute),
	}
	h.service.mu.Lock()
	h.service.workers[workerID] = stale
	h.service.mu.Unlock()
	streamCtx, cancel := context.WithCancel(context.Background())
	stream := &reconnectAcceptanceStream{
		ctx: streamCtx,
		ready: &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: workerID, AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		}}},
		accepted: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() { done <- h.service.ConnectWorker(stream) }()

	select {
	case <-stream.accepted:
	case err := <-done:
		t.Fatalf("replacement worker rejected: %v", err)
	case <-time.After(time.Second):
		t.Fatal("replacement worker was not accepted")
	}
	stale.mu.Lock()
	staleClosed := stale.closed
	stale.mu.Unlock()
	if !staleClosed {
		t.Fatal("replacement admission left stale registration open")
	}
	h.service.mu.Lock()
	replacement := h.service.workers[workerID]
	h.service.mu.Unlock()
	if replacement == nil || replacement == stale {
		t.Fatalf("worker registration = %p, want replacement distinct from %p", replacement, stale)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, errWorkerStreamCancelled) {
			t.Fatalf("replacement worker exit = %v, want %v", err, errWorkerStreamCancelled)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement worker did not exit")
	}
}

// TestConnectWorkerReportsCancellationBeforeReachingCommandLoop covers the
// canonicalisation on a path the scheduler cannot influence: a cancelled
// stream whose Send fails before the command loop is ever entered, so
// ctx.Done never gets a chance to supply the error. gRPC reports that as a
// Canceled status rather than context.Canceled, and the handler must still
// report the one cancellation error.
func TestConnectWorkerReportsCancellationBeforeReachingCommandLoop(t *testing.T) {
	h := newHarness(t)
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &cancelOnAcceptStream{ctx: streamCtx, cancel: cancel, ready: workerReady("worker-cancelled-accept")}

	err := h.service.ConnectWorker(stream)

	if !stream.acceptSent.Load() {
		t.Fatal("worker was never accepted, so the cancelled send path was not exercised")
	}
	if !errors.Is(err, errWorkerStreamCancelled) {
		t.Fatalf("ConnectWorker exit = %v, want %v", err, errWorkerStreamCancelled)
	}
}

// TestConnectWorkerKeepsTeardownFailuresAlongsideCancellation pins the order
// the teardown works in: cancellation is canonicalised first and the failures
// teardown then discovers are joined on top. Collapsing them the other way
// round would make a cancelled stream the only report of a lost assignment.
func TestConnectWorkerKeepsTeardownFailuresAlongsideCancellation(t *testing.T) {
	h := newHarness(t)
	const workerID = "worker-teardown-failure"
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &reconnectAcceptanceStream{ctx: streamCtx, ready: workerReady(workerID), accepted: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- h.service.ConnectWorker(stream) }()

	select {
	case <-stream.accepted:
	case err := <-done:
		t.Fatalf("worker rejected: %v", err)
	case <-time.After(time.Second):
		t.Fatal("worker was not accepted")
	}
	// Hand the registration an assignment so teardown has something to
	// reconcile, then take the database away so reconciling it fails.
	h.service.mu.Lock()
	connected := h.service.workers[workerID]
	h.service.mu.Unlock()
	if connected == nil {
		t.Fatal("worker was accepted but not registered")
	}
	connected.mu.Lock()
	connected.assignments["run_teardown"] = assignment{jobID: "job_teardown", runID: "run_teardown"}
	connected.mu.Unlock()
	if err := h.database.Close(); err != nil {
		t.Fatal(err)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, errWorkerStreamCancelled) {
			t.Fatalf("exit = %v, want it to report %v", err, errWorkerStreamCancelled)
		}
		if !strings.Contains(err.Error(), "reconcile run run_teardown") {
			t.Fatalf("exit = %v, want it to also report the failed reconciliation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not exit")
	}
}

// TestIsStreamCancellationCanonicalisesOnlyCancelledStreams pins the predicate
// that lets ConnectWorker report one error for a cancelled stream no matter
// which of its two concurrently-ready paths observed the cancellation. The
// scheduler decides that race, so this covers deterministically what
// TestConnectWorkerReplacesExpiredIdleSameIDRegistration can only sample.
func TestIsStreamCancellationCanonicalisesOnlyCancelledStreams(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()

	for _, testCase := range []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{name: "in-process recv on cancelled stream", ctx: cancelled, err: context.Canceled, want: true},
		{name: "grpc recv on cancelled stream", ctx: cancelled, err: status.Error(codes.Canceled, "context canceled"), want: true},
		{name: "wrapped grpc recv on cancelled stream", ctx: cancelled, err: fmt.Errorf("send: %w", status.Error(codes.Canceled, "context canceled")), want: true},
		{name: "cancellation joined with nothing", ctx: cancelled, err: errors.Join(context.Canceled, nil), want: true},
		// A registration the service closed also reports Canceled, and while
		// the stream context is cancelled the command loop could have reported
		// either. Folding it in is what makes the outcome deterministic.
		{name: "closed registration on cancelled stream", ctx: cancelled, err: status.Error(codes.Canceled, "worker is disconnected"), want: true},
		// Teardown joins its own failures onto whatever the handler returned,
		// so a join must never collapse: the cancellation would swallow the
		// only report of a genuine failure.
		{name: "real failure joined onto cancellation", ctx: cancelled, err: errors.Join(context.Canceled, errors.New("reconcile worker assignments: database is closed")), want: false},
		{name: "real failure racing cancellation", ctx: cancelled, err: errors.New("apply update: database is locked"), want: false},
		{name: "deadline on cancelled stream", ctx: cancelled, err: context.DeadlineExceeded, want: false},
		{name: "cancellation on live stream", ctx: context.Background(), err: context.Canceled, want: false},
		{name: "expired stream", ctx: expired, err: context.DeadlineExceeded, want: false},
		{name: "clean exit", ctx: cancelled, err: nil, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isStreamCancellation(testCase.ctx, testCase.err); got != testCase.want {
				t.Fatalf("isStreamCancellation(%v) = %t, want %t", testCase.err, got, testCase.want)
			}
		})
	}
}

// cancelOnAcceptStream cancels its own context while delivering the acceptance
// command and then fails that send the way gRPC fails a send on a cancelled
// stream.
type cancelOnAcceptStream struct {
	grpc.ServerStream
	ctx        context.Context
	cancel     context.CancelFunc
	ready      *turingv1.RuntimeUpdate
	readySent  bool
	acceptSent atomic.Bool
}

func (s *cancelOnAcceptStream) Send(command *turingv1.RuntimeCommand) error {
	if command.GetWorkerAccepted() == nil {
		return nil
	}
	s.acceptSent.Store(true)
	s.cancel()
	return status.Error(codes.Canceled, "context canceled")
}

func (s *cancelOnAcceptStream) Recv() (*turingv1.RuntimeUpdate, error) {
	if !s.readySent {
		s.readySent = true
		return s.ready, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *cancelOnAcceptStream) Context() context.Context { return s.ctx }

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

type reconnectAcceptanceStream struct {
	grpc.ServerStream
	ctx       context.Context
	ready     *turingv1.RuntimeUpdate
	readySent bool
	accepted  chan struct{}
	once      sync.Once
}

func (s *reconnectAcceptanceStream) Send(command *turingv1.RuntimeCommand) error {
	if command.GetWorkerAccepted() != nil {
		s.once.Do(func() { close(s.accepted) })
	}
	return nil
}

func (s *reconnectAcceptanceStream) Recv() (*turingv1.RuntimeUpdate, error) {
	if !s.readySent {
		s.readySent = true
		return s.ready, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *reconnectAcceptanceStream) Context() context.Context { return s.ctx }

type fastTerminalUpdateStream struct {
	grpc.ServerStream
	ctx                     context.Context
	repo                    *repository.Repository
	service                 *Server
	workerID                string
	ready                   *turingv1.RuntimeUpdate
	completed               *turingv1.RuntimeUpdate
	runID                   string
	assignmentStarted       chan struct{}
	recvCount               int
	completedBeforeDelivery atomic.Bool
	deliveryWasUnserialized atomic.Bool
	assignmentOnce          sync.Once
}

func (s *fastTerminalUpdateStream) Send(command *turingv1.RuntimeCommand) error {
	if command.GetRunAssigned() == nil {
		return nil
	}
	s.service.mu.Lock()
	connectedWorker := s.service.workers[s.workerID]
	s.service.mu.Unlock()
	if connectedWorker != nil && connectedWorker.updateMu.TryLock() {
		s.deliveryWasUnserialized.Store(true)
		connectedWorker.updateMu.Unlock()
	}
	s.assignmentOnce.Do(func() { close(s.assignmentStarted) })
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		run, err := s.repo.GetRun(context.Background(), s.runID)
		if err == nil && run.Status == "completed" {
			s.completedBeforeDelivery.Store(true)
			break
		}
		time.Sleep(time.Millisecond)
	}
	return nil
}

func (s *fastTerminalUpdateStream) Recv() (*turingv1.RuntimeUpdate, error) {
	s.recvCount++
	switch s.recvCount {
	case 1:
		return s.ready, nil
	case 2:
		select {
		case <-s.assignmentStarted:
			return s.completed, nil
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		}
	default:
		<-s.ctx.Done()
		return nil, s.ctx.Err()
	}
}

func (s *fastTerminalUpdateStream) Context() context.Context { return s.ctx }
