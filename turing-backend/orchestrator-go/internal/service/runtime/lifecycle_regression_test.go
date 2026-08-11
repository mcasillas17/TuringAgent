package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAmbiguousAssignmentSendKeepsAttemptFenced(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "ambiguous assignment")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &deliveredThenFailedAssignmentStream{
		ctx:       ctx,
		ready:     workerReady("worker-ambiguous-send"),
		delivered: make(chan *turingv1.RuntimeCommand, 1),
	}

	done := make(chan error, 1)
	go func() { done <- h.service.ConnectWorker(stream) }()
	select {
	case command := <-stream.delivered:
		if command.GetRunAssigned() == nil || command.GetRunAssigned().GetRunId() != enqueued.RunID {
			t.Fatalf("delivered command = %+v, want assignment for %q", command, enqueued.RunID)
		}
	case <-time.After(time.Second):
		t.Fatal("assignment was not delivered before the send error")
	}
	if err := <-done; err == nil {
		t.Fatal("ConnectWorker succeeded after an ambiguous assignment send failure")
	}

	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" || !run.ExecutionActive {
		t.Fatalf("ambiguous assignment run = %+v, want active running fence", run)
	}
	claimed, err := h.repo.ClaimNextJob(context.Background(), "general_assistant", "worker-fresh")
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != "" {
		t.Fatalf("fresh worker claimed ambiguous assignment %+v before the original attempt exited", claimed)
	}
}

func TestDisconnectRedispatchesRequeuedAssignmentToIdleWorker(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "redispatch after disconnect")
	client := h.runtimeClient(t)
	first, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = first.CloseSend() }()
	if err := first.Send(workerReady("worker-disconnect-owner")); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, first, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == enqueued.RunID
	})

	second, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.CloseSend() }()
	if err := second.Send(workerReady("worker-disconnect-idle")); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, second, func(command *turingv1.RuntimeCommand) bool {
		return command.GetWorkerAccepted() != nil
	})

	if err := first.CloseSend(); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, second, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetRunId() != enqueued.RunID {
		t.Fatalf("redispatched run = %q, want %q", assigned.GetRunId(), enqueued.RunID)
	}
}

func TestDisconnectReconciliationRacingDenialLeavesNoExecutionGate(t *testing.T) {
	h := newHarness(t)
	enqueued := h.enqueueRun(t, "disconnect and deny")
	claimed, err := h.repo.ClaimNextJob(context.Background(), "general_assistant", "worker-disconnect-race")
	if err != nil {
		t.Fatal(err)
	}
	before := &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_disconnect_deny_race",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       mustStruct(t, map[string]any{"path": "note.txt", "content": "hello"}),
	}
	decision, err := h.service.handleToolBeacon(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := h.repo.ReconcileAssignment(context.Background(), repository.Assignment{
			JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-disconnect-race", AttemptID: claimed.AssignmentAttemptID,
		})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := h.approvals.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: decision.GetApprovalId(), Reason: "no"})
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("racing reconciliation/denial: %v", err)
		}
	}

	var approvalStatus, toolStatus, runStatus, jobStatus string
	var active int
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM approvals WHERE id = ?`, decision.GetApprovalId()).Scan(&approvalStatus); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = ?`, before.ToolCallId).Scan(&toolStatus); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, execution_active FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus, &active); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if approvalStatus != "denied" || toolStatus == "approval_required" || runStatus != "failed" || jobStatus != "failed" || active != 0 {
		t.Fatalf("disconnect/denial race left lifecycle inconsistent: approval=%q tool=%q run=%q job=%q active=%d", approvalStatus, toolStatus, runStatus, jobStatus, active)
	}
	var failedEvents int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, enqueued.RunID).Scan(&failedEvents); err != nil {
		t.Fatal(err)
	}
	if failedEvents != 1 {
		t.Fatalf("disconnect/denial failed events = %d, want one", failedEvents)
	}
}

func TestRuntimeFailureTerminalizesPendingApprovalInOneLifecycle(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "runtime failure while awaiting approval")
	before := &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_runtime_failure",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       mustStruct(t, map[string]any{"path": "note.txt", "content": "hello"}),
	}
	decision, err := h.service.handleToolBeacon(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if decision.GetApprovalId() == "" {
		t.Fatalf("before decision = %+v, want approval", decision)
	}

	if err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
			RunId: enqueued.RunID, Code: "runtime_error", Message: "approval polling failed",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	approval, err := h.repo.GetApproval(context.Background(), decision.GetApprovalId())
	if err != nil {
		t.Fatal(err)
	}
	var toolStatus, jobStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = ?`, before.ToolCallId).Scan(&toolStatus); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status == "pending" || toolStatus == "approval_required" || run.Status != "failed" || jobStatus != "failed" {
		t.Fatalf("runtime failure left lifecycle open: approval=%q tool=%q run=%q job=%q", approval.Status, toolStatus, run.Status, jobStatus)
	}
	var failedEvents int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, enqueued.RunID).Scan(&failedEvents); err != nil {
		t.Fatal(err)
	}
	if failedEvents != 1 {
		t.Fatalf("run failure event count = %d, want 1", failedEvents)
	}
	cleanup := &turingv1.ToolCallBeacon{
		RunId:      before.RunId,
		TraceId:    before.TraceId,
		ToolCallId: before.ToolCallId,
		AgentId:    before.AgentId,
		ServerName: before.ServerName,
		ToolName:   before.ToolName,
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
		Status:     turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
		Args:       before.Args,
		Error:      &turingv1.ToolCallError{Code: "approval_wait_failed", Message: "approval polling failed"},
	}
	if _, err := h.service.handleToolBeacon(context.Background(), cleanup); err != nil {
		t.Fatalf("late failed AFTER should reconcile idempotently: %v", err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, enqueued.RunID).Scan(&failedEvents); err != nil {
		t.Fatal(err)
	}
	if failedEvents != 1 {
		t.Fatalf("late cleanup appended %d failure events, want exactly one", failedEvents)
	}
}

func TestToolAfterRejectsImmutableArgumentMismatch(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "immutable AFTER args")
	before := &turingv1.ToolCallBeacon{
		RunId:      enqueued.RunID,
		TraceId:    enqueued.TraceID,
		ToolCallId: "call_immutable_args",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "system",
		ToolName:   "system.time",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       mustStruct(t, map[string]any{"timezone": "UTC"}),
	}
	if _, err := h.service.handleToolBeacon(context.Background(), before); err != nil {
		t.Fatal(err)
	}
	after := &turingv1.ToolCallBeacon{
		RunId:      before.RunId,
		TraceId:    before.TraceId,
		ToolCallId: before.ToolCallId,
		AgentId:    before.AgentId,
		ServerName: before.ServerName,
		ToolName:   before.ToolName,
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
		Status:     turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED,
		Args:       mustStruct(t, map[string]any{"timezone": "America/Los_Angeles"}),
	}
	if _, err := h.service.handleToolBeacon(context.Background(), after); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("mismatched AFTER error = %v, want FailedPrecondition", err)
	}
	var toolStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = ?`, before.ToolCallId).Scan(&toolStatus); err != nil {
		t.Fatal(err)
	}
	if toolStatus != "allowed" {
		t.Fatalf("mismatched AFTER mutated tool call to %q, want allowed", toolStatus)
	}
}

func TestDelayedIdenticalAfterDoesNotCloseWorkerStream(t *testing.T) {
	h := newHarness(t)
	first := h.enqueueRun(t, "late cleanup")
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(workerReady("worker-late-cleanup")); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil && command.GetRunAssigned().GetRunId() == first.RunID
	})
	beacon := &turingv1.ToolCallBeacon{
		RunId:      first.RunID,
		TraceId:    first.TraceID,
		ToolCallId: "call_late_cleanup",
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "system",
		ToolName:   "system.time",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       mustStruct(t, map[string]any{"timezone": "UTC"}),
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: beacon}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetToolPolicyDecision() != nil && command.GetToolPolicyDecision().GetToolCallId() == beacon.ToolCallId
	})
	after := *beacon
	after.Phase = turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER
	after.Status = turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED
	after.Error = &turingv1.ToolCallError{Code: "cancelled", Message: "cancelled"}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &after}}); err != nil {
		t.Fatal(err)
	}
	recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetToolPolicyDecision() != nil && command.GetToolPolicyDecision().GetToolCallId() == beacon.ToolCallId
	})
	if _, err := h.repo.CancelRunWithEvent(context.Background(), first.RunID, "client_cancelled", `{"reason":"client_cancelled"}`); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: first.RunID}}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &after}}); err != nil {
		t.Fatal(err)
	}
	decision := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetToolPolicyDecision() != nil && command.GetToolPolicyDecision().GetToolCallId() == beacon.ToolCallId
	}).GetToolPolicyDecision()
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("late cleanup decision = %+v, want allow", decision)
	}
	after.Error = &turingv1.ToolCallError{Code: "cancelled", Message: "conflicting cleanup"}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &after}}); err != nil {
		t.Fatal(err)
	}
	conflict := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetToolPolicyDecision() != nil && command.GetToolPolicyDecision().GetToolCallId() == beacon.ToolCallId
	}).GetToolPolicyDecision()
	if conflict.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY {
		t.Fatalf("conflicting cleanup decision = %+v, want deny without closing stream", conflict)
	}

	second := h.enqueueRun(t, "stream stays usable")
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetRunId() != second.RunID {
		t.Fatalf("stream assigned %q after late cleanup, want %q", assigned.GetRunId(), second.RunID)
	}
}

func workerReady(workerID string) *turingv1.RuntimeUpdate {
	return &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{
		WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: workerID, AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		},
	}}
}

type deliveredThenFailedAssignmentStream struct {
	grpc.ServerStream
	ctx       context.Context
	ready     *turingv1.RuntimeUpdate
	readySent bool
	delivered chan *turingv1.RuntimeCommand
}

func (s *deliveredThenFailedAssignmentStream) Context() context.Context { return s.ctx }

func (s *deliveredThenFailedAssignmentStream) Recv() (*turingv1.RuntimeUpdate, error) {
	if !s.readySent {
		s.readySent = true
		return s.ready, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *deliveredThenFailedAssignmentStream) Send(command *turingv1.RuntimeCommand) error {
	if command.GetRunAssigned() != nil {
		s.delivered <- command
		return errors.New("assignment reached executor but Send returned an error")
	}
	return nil
}
