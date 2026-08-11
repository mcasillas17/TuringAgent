package runtime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/safejson"
	approvalsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/approvals"
	auditsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	defaultMaxConcurrentRuns = 1
	maxWorkerConcurrentRuns  = 128
)

type Server struct {
	turingv1.UnimplementedRuntimeServiceServer
	repo      *repository.Repository
	bus       *events.Bus
	approvals approvalCreator
	audit     *auditsvc.Server
	mu        sync.Mutex
	workers   map[string]*worker
}

type approvalCreator interface {
	CreateApprovalForTool(ctx context.Context, runID string, toolCallID string, agentID string, toolName string, args map[string]any) (string, error)
}

type worker struct {
	commands      chan *turingv1.RuntimeCommand
	maxConcurrent int
	assignments   map[string]string
	mu            sync.Mutex
	closed        bool
}

type assignment struct {
	jobID string
	runID string
}

func New(repo *repository.Repository, bus *events.Bus, approvalServices ...approvalCreator) *Server {
	var approvals approvalCreator
	if len(approvalServices) > 0 {
		approvals = approvalServices[0]
	}
	server := &Server{repo: repo, bus: bus, approvals: approvals, audit: auditsvc.New(repo), workers: map[string]*worker{}}
	if setter, ok := approvals.(interface{ SetNotifier(approvalsvc.Notifier) }); ok {
		setter.SetNotifier(server)
	}
	return server
}

func (s *Server) ConnectWorker(stream turingv1.RuntimeService_ConnectWorkerServer) error {
	ctx := stream.Context()
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	ready := first.GetWorkerReady()
	if ready == nil || ready.WorkerId == "" || ready.AgentId != turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT {
		return status.Error(codes.InvalidArgument, "worker_ready is required")
	}
	maxConcurrent := int(ready.MaxConcurrentRuns)
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrentRuns
	}
	if maxConcurrent > maxWorkerConcurrentRuns {
		maxConcurrent = maxWorkerConcurrentRuns
	}
	commands := make(chan *turingv1.RuntimeCommand, maxConcurrent)
	s.mu.Lock()
	if _, ok := s.workers[ready.WorkerId]; ok {
		s.mu.Unlock()
		return status.Error(codes.AlreadyExists, "worker already connected")
	}
	connectedWorker := &worker{commands: commands, maxConcurrent: maxConcurrent, assignments: map[string]string{}}
	s.workers[ready.WorkerId] = connectedWorker
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.workers, ready.WorkerId)
		s.mu.Unlock()
		s.requeueAssignments(connectedWorker.close())
	}()
	if err := stream.Send(&turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_WorkerAccepted{WorkerAccepted: &turingv1.RuntimeWorkerAccepted{WorkerId: ready.WorkerId}}}); err != nil {
		return err
	}
	if err := s.DispatchPending(ctx); err != nil {
		return err
	}
	recvErr := make(chan error, 1)
	go func() {
		for {
			update, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			if err := connectedWorker.validateUpdate(update); err != nil {
				recvErr <- err
				return
			}
			if beacon := update.GetToolBeacon(); beacon != nil {
				decision, err := s.handleToolBeacon(ctx, beacon)
				if err != nil {
					recvErr <- err
					return
				}
				if err := connectedWorker.send(ctx, &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ToolPolicyDecision{ToolPolicyDecision: decision}}); err != nil {
					recvErr <- err
					return
				}
				continue
			}
			if err := s.applyUpdate(ctx, update); err != nil {
				recvErr <- err
				return
			}
			if runID := terminalRunID(update); runID != "" {
				connectedWorker.releaseRun(runID)
				if err := s.DispatchPending(ctx); err != nil {
					recvErr <- err
					return
				}
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return status.Error(codes.Canceled, "worker stream cancelled")
		case err := <-recvErr:
			return err
		case cmd := <-commands:
			if err := stream.Send(cmd); err != nil {
				s.requeueIfAssignmentFailed(cmd, connectedWorker)
				return err
			}
		}
	}
}

func terminalRunID(update *turingv1.RuntimeUpdate) string {
	if update == nil {
		return ""
	}
	if completed := update.GetRunCompleted(); completed != nil {
		return completed.RunId
	}
	if failed := update.GetRunFailed(); failed != nil {
		return failed.RunId
	}
	if cancelled := update.GetRunCancelledAck(); cancelled != nil {
		return cancelled.RunId
	}
	return ""
}

func updateRunID(update *turingv1.RuntimeUpdate) string {
	if update == nil {
		return ""
	}
	if event := update.GetEvent(); event != nil {
		return event.RunId
	}
	if beacon := update.GetToolBeacon(); beacon != nil {
		return beacon.RunId
	}
	return terminalRunID(update)
}

func (s *Server) requeueIfAssignmentFailed(cmd *turingv1.RuntimeCommand, worker *worker) {
	assigned := cmd.GetRunAssigned()
	if assigned == nil {
		return
	}
	worker.releaseRun(assigned.RunId)
	s.requeueAssignments([]assignment{{jobID: assigned.JobId, runID: assigned.RunId}})
}

func (s *Server) requeueAssignments(assignments []assignment) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, assignment := range assignments {
		_ = s.repo.RequeueClaimedJob(ctx, assignment.jobID, assignment.runID)
	}
}

func (s *Server) DispatchPending(ctx context.Context) error {
	dispatchCtx, cancel := withDefaultTimeout(ctx, 5*time.Second)
	defer cancel()
	workers := s.snapshotWorkers()
	for _, entry := range workers {
		for {
			assigned, noJob, err := s.dispatchToWorker(dispatchCtx, entry.workerID, entry.worker)
			if err != nil {
				return err
			}
			if noJob {
				return nil
			}
			if !assigned {
				break
			}
		}
	}
	return nil
}

type workerSnapshot struct {
	workerID string
	worker   *worker
}

func (s *Server) snapshotWorkers() []workerSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	workers := make([]workerSnapshot, 0, len(s.workers))
	for workerID, worker := range s.workers {
		workers = append(workers, workerSnapshot{workerID: workerID, worker: worker})
	}
	return workers
}

func (s *Server) dispatchToWorker(ctx context.Context, workerID string, worker *worker) (assigned bool, noJob bool, err error) {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.closed || len(worker.assignments) >= worker.maxConcurrent {
		return false, false, nil
	}
	job, err := s.repo.ClaimNextJob(ctx, "general_assistant", workerID)
	if err != nil {
		return false, false, err
	}
	if job.JobID == "" {
		return false, true, nil
	}
	select {
	case worker.commands <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: mapJob(job)}}:
		worker.assignments[job.RunID] = job.JobID
		s.publishEvent(job.StartedEvent)
		return true, false, nil
	case <-ctx.Done():
		s.requeueAssignments([]assignment{{jobID: job.JobID, runID: job.RunID}})
		return false, false, ctx.Err()
	}
}

func (s *Server) CancelRun(ctx context.Context, runID string, reason string) {
	if runID == "" {
		return
	}
	owner := s.workerForRun(runID)
	if owner == nil {
		return
	}
	sendCtx, cancel := withDefaultTimeout(ctx, 5*time.Second)
	defer cancel()
	command := &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunCancelled{RunCancelled: &turingv1.RuntimeRunCancelled{RunId: runID, Reason: reason}}}
	_ = owner.send(sendCtx, command)
}

func (s *Server) workerForRun(runID string) *worker {
	for _, entry := range s.snapshotWorkers() {
		if entry.worker.hasAssignment(runID) {
			return entry.worker
		}
	}
	return nil
}

func withDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (w *worker) send(ctx context.Context, command *turingv1.RuntimeCommand) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	select {
	case w.commands <- command:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *worker) validateUpdate(update *turingv1.RuntimeUpdate) error {
	runID := updateRunID(update)
	if runID == "" {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.assignments[runID]; ok {
		return nil
	}
	return status.Error(codes.PermissionDenied, "run is not assigned to worker")
}

func (w *worker) hasAssignment(runID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.assignments[runID]
	return ok
}

func (w *worker) releaseRun(runID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.assignments, runID)
}

func (w *worker) close() []assignment {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	close(w.commands)
	assignments := make([]assignment, 0, len(w.assignments))
	for runID, jobID := range w.assignments {
		assignments = append(assignments, assignment{jobID: jobID, runID: runID})
	}
	w.assignments = map[string]string{}
	return assignments
}

func (s *Server) applyUpdate(ctx context.Context, update *turingv1.RuntimeUpdate) error {
	if update == nil {
		return status.Error(codes.InvalidArgument, "runtime update is required")
	}
	switch value := update.Update.(type) {
	case *turingv1.RuntimeUpdate_Heartbeat:
		return nil
	case *turingv1.RuntimeUpdate_Event:
		if isGenericTerminalEvent(value.Event) {
			return status.Error(codes.InvalidArgument, "terminal run events must use runtime terminal updates")
		}
		eventUpdate, err := s.normalizeRuntimeEvent(ctx, value.Event)
		if err != nil {
			return err
		}
		event, err := s.repo.AppendRuntimeEvent(ctx, eventUpdate)
		if err != nil {
			return mapRunStateError(err)
		}
		s.publishEvent(event)
		return nil
	case *turingv1.RuntimeUpdate_ToolBeacon:
		_, err := s.handleToolBeacon(ctx, value.ToolBeacon)
		return err
	case *turingv1.RuntimeUpdate_RunCompleted:
		return s.handleRunCompleted(ctx, value.RunCompleted)
	case *turingv1.RuntimeUpdate_RunFailed:
		return s.handleRunFailed(ctx, value.RunFailed)
	case *turingv1.RuntimeUpdate_RunCancelledAck:
		return s.handleRunCancelledAck(ctx, value.RunCancelledAck)
	default:
		return status.Error(codes.InvalidArgument, "unsupported runtime update")
	}
}

func (s *Server) normalizeRuntimeEvent(ctx context.Context, event *turingv1.TuringEvent) (*turingv1.TuringEvent, error) {
	if event == nil || event.RunId == "" {
		return nil, status.Error(codes.InvalidArgument, "runtime event run_id is required")
	}
	if !isKnownRuntimeEventType(event.Type) {
		return nil, status.Error(codes.InvalidArgument, "runtime event type is invalid")
	}
	run, err := s.repo.GetRun(ctx, event.RunId)
	if err != nil {
		return nil, err
	}
	if !isActiveRunStatus(run.Status) {
		return nil, status.Error(codes.FailedPrecondition, "run is not active")
	}
	if event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED {
		return nil, status.Error(codes.InvalidArgument, "message.completed must use run_completed")
	}
	// Clone rather than dereference: a generated message embeds protoimpl state
	// (including a mutex), so copying it by value is unsafe and trips govet's
	// copylocks check.
	out, _ := proto.Clone(event).(*turingv1.TuringEvent)
	out.SessionId = run.SessionID
	out.TraceId = run.TraceID
	return out, nil
}

func isKnownRuntimeEventType(eventType turingv1.TuringEventType) bool {
	switch eventType {
	case turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_QUEUED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_COMPLETED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_CANCELLED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_APPROVED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_EXPIRED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_CONSUMED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_ERROR,
		turingv1.TuringEventType_TURING_EVENT_TYPE_SYSTEM:
		return true
	default:
		return false
	}
}

func isActiveRunStatus(runStatus string) bool {
	return runStatus == "running" || runStatus == "waiting_approval"
}

func isGenericTerminalEvent(event *turingv1.TuringEvent) bool {
	if event == nil {
		return false
	}
	switch event.Type {
	case turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_COMPLETED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_CANCELLED:
		return true
	default:
		return false
	}
}

func (s *Server) handleRunCancelledAck(ctx context.Context, ack *turingv1.RuntimeCancelledAck) error {
	if ack == nil || ack.RunId == "" {
		return status.Error(codes.InvalidArgument, "run_cancelled_ack is required")
	}
	run, err := s.repo.GetRun(ctx, ack.RunId)
	if err != nil {
		return err
	}
	if run.Status != "cancelled" {
		return status.Error(codes.FailedPrecondition, "run is not cancelled")
	}
	return nil
}

func (s *Server) handleRunCompleted(ctx context.Context, completed *turingv1.RuntimeRunCompleted) error {
	if completed == nil || completed.RunId == "" {
		return status.Error(codes.InvalidArgument, "run_completed is required")
	}
	if completed.Content == "" {
		return status.Error(codes.InvalidArgument, "content is required")
	}
	run, err := s.repo.GetRun(ctx, completed.RunId)
	if err != nil {
		return err
	}
	assistantMessageID := completed.AssistantMessageId
	if assistantMessageID == "" {
		assistantMessageID = run.AssistantMessageID
	}
	if assistantMessageID == "" {
		return status.Error(codes.FailedPrecondition, "assistant message is missing")
	}
	if run.AssistantMessageID != "" && assistantMessageID != run.AssistantMessageID {
		return status.Error(codes.InvalidArgument, "assistant_message_id does not match run")
	}
	payload := map[string]any{
		"runId":              completed.RunId,
		"assistantMessageId": assistantMessageID,
	}
	if completed.Usage != nil {
		payload["usage"] = completed.Usage.AsMap()
	}
	payloadJSON, err := encodePayload(payload)
	if err != nil {
		return err
	}
	events, err := s.repo.CompleteRunWithEvent(ctx, completed.RunId, assistantMessageID, completed.Content, payloadJSON)
	if err != nil {
		return mapRunStateError(err)
	}
	for _, event := range events {
		s.publishEvent(event)
	}
	return nil
}

func (s *Server) handleRunFailed(ctx context.Context, failed *turingv1.RuntimeRunFailed) error {
	if failed == nil || failed.RunId == "" {
		return status.Error(codes.InvalidArgument, "run_failed is required")
	}
	payloadJSON, err := encodePayload(map[string]any{"runId": failed.RunId, "code": failed.Code, "message": failed.Message, "retryable": failed.Retryable})
	if err != nil {
		return err
	}
	event, err := s.repo.FailRunWithEvent(ctx, failed.RunId, failed.Code, failed.Message, payloadJSON)
	if err != nil {
		return mapRunStateError(err)
	}
	s.publishEvent(event)
	return nil
}

func encodePayload(payload map[string]any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func mapRunStateError(err error) error {
	switch {
	case errors.Is(err, repository.ErrRunNotCompletable),
		errors.Is(err, repository.ErrRunNotFailable),
		errors.Is(err, repository.ErrRunNotCancellable),
		errors.Is(err, repository.ErrRunNotActive):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return err
	}
}

func (s *Server) handleToolBeacon(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
	if beacon == nil || beacon.RunId == "" {
		return nil, status.Error(codes.InvalidArgument, "tool_beacon is required")
	}
	if beacon.ToolCallId == "" {
		return nil, status.Error(codes.InvalidArgument, "tool_call_id is required")
	}
	switch beacon.Phase {
	case turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER:
	default:
		return nil, status.Error(codes.InvalidArgument, "tool_call phase is required")
	}
	if beacon.AgentId != turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT {
		return nil, status.Error(codes.InvalidArgument, "agent_id is unsupported")
	}
	if beacon.ToolName == "" {
		return nil, status.Error(codes.InvalidArgument, "tool_name is required")
	}
	run, err := s.repo.GetRun(ctx, beacon.RunId)
	if err != nil {
		return nil, err
	}
	if !isActiveRunStatus(run.Status) {
		return nil, status.Error(codes.FailedPrecondition, "run is not active")
	}
	if beacon.TraceId != run.TraceID {
		return nil, status.Error(codes.InvalidArgument, "tool call trace_id does not match run")
	}
	switch beacon.Phase {
	case turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE:
		return s.handleToolBefore(ctx, beacon, run)
	case turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER:
		return s.handleToolAfter(ctx, beacon, run)
	default:
		return nil, status.Error(codes.InvalidArgument, "tool_call phase is required")
	}
}

func (s *Server) handleToolBefore(ctx context.Context, beacon *turingv1.ToolCallBeacon, run repository.Run) (*turingv1.ToolPolicyDecision, error) {
	args := beaconArgs(beacon)
	argsJSON, argsHash, err := canonicalArgs(args)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "tool args are not valid JSON")
	}
	policy, ok := tools.GetPolicy(beacon.ToolName)
	if !ok {
		return s.denyToolBefore(ctx, beacon, run, argsJSON, argsHash, "unknown_tool")
	}
	if policy == tools.PolicyApprovalRequired && beacon.Args == nil {
		return s.denyToolBefore(ctx, beacon, run, argsJSON, argsHash, "approval_args_missing")
	}
	if policy == tools.PolicyDisabled {
		return s.denyToolBefore(ctx, beacon, run, argsJSON, argsHash, "tool_disabled")
	}
	if policy == tools.PolicyApprovalRequired {
		if err := s.repo.RecordToolCallBefore(ctx, repository.ToolCallRecord{ToolCallID: beacon.ToolCallId, RunID: beacon.RunId}, "general_assistant", beaconServerName(beacon), beacon.ToolName, argsJSON, argsHash); err != nil {
			return nil, mapToolCallError(err)
		}
		if s.approvals == nil {
			return nil, status.Error(codes.FailedPrecondition, "approval service is not configured")
		}
		approvalID, err := s.approvals.CreateApprovalForTool(ctx, beacon.RunId, beacon.ToolCallId, "general_assistant", beacon.ToolName, args)
		if err != nil {
			return nil, err
		}
		return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED, ToolCallId: beacon.ToolCallId, ApprovalId: approvalID}, nil
	}
	statusValue := "requested"
	if policy == tools.PolicySafe {
		statusValue = "allowed"
	}
	if err := s.repo.RecordToolCallBefore(ctx, repository.ToolCallRecord{ToolCallID: beacon.ToolCallId, RunID: beacon.RunId, Status: statusValue}, "general_assistant", beaconServerName(beacon), beacon.ToolName, argsJSON, argsHash); err != nil {
		return nil, mapToolCallError(err)
	}
	event, err := s.appendToolEvent(ctx, run, "tool.call.started", map[string]any{
		"toolCallId": beacon.ToolCallId,
		"serverName": beaconServerName(beacon),
		"toolName":   beacon.ToolName,
		"args":       args,
	})
	if err != nil {
		return nil, err
	}
	s.publishEvent(event)
	if err := s.audit.Record(ctx, beacon.RunId, "runtime", "", "tool.call.before", beacon.ToolCallId, toolAuditPayload(beacon)); err != nil {
		return nil, err
	}
	switch policy {
	case tools.PolicySafe:
		return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: beacon.ToolCallId}, nil
	default:
		return s.denyToolBefore(ctx, beacon, run, argsJSON, argsHash, "unknown_policy")
	}
}

func (s *Server) denyToolBefore(ctx context.Context, beacon *turingv1.ToolCallBeacon, run repository.Run, argsJSON string, argsHash string, reason string) (*turingv1.ToolPolicyDecision, error) {
	if err := s.repo.RecordToolCallBefore(ctx, repository.ToolCallRecord{ToolCallID: beacon.ToolCallId, RunID: beacon.RunId, Status: "denied"}, "general_assistant", beaconServerName(beacon), beacon.ToolName, argsJSON, argsHash); err != nil {
		return nil, mapToolCallError(err)
	}
	event, err := s.appendToolEvent(ctx, run, "tool.call.denied", map[string]any{
		"toolCallId": beacon.ToolCallId,
		"serverName": beaconServerName(beacon),
		"toolName":   beacon.ToolName,
		"reason":     reason,
	})
	if err != nil {
		return nil, err
	}
	s.publishEvent(event)
	payload := toolAuditPayload(beacon)
	payload["reason"] = reason
	if err := s.audit.Record(ctx, beacon.RunId, "runtime", "", "tool.call.before", beacon.ToolCallId, payload); err != nil {
		return nil, err
	}
	return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_DENY, ToolCallId: beacon.ToolCallId, Reason: reason}, nil
}

func (s *Server) handleToolAfter(ctx context.Context, beacon *turingv1.ToolCallBeacon, run repository.Run) (*turingv1.ToolPolicyDecision, error) {
	statusValue, eventType, err := toolAfterStatus(beacon)
	if err != nil {
		return nil, err
	}
	errorCode, errorMessage := "", ""
	if beacon.Error != nil {
		errorCode = beacon.Error.Code
		errorMessage = beacon.Error.Message
	}
	if err := s.repo.RecordToolCallAfter(ctx, beacon.ToolCallId, beacon.RunId, statusValue, beacon.ResultSummary, errorCode, errorMessage, beacon.DurationMs); err != nil {
		return nil, mapToolCallError(err)
	}
	payload := map[string]any{
		"toolCallId":    beacon.ToolCallId,
		"serverName":    beaconServerName(beacon),
		"toolName":      beacon.ToolName,
		"status":        statusValue,
		"resultSummary": beacon.ResultSummary,
		"durationMs":    beacon.DurationMs,
	}
	if beacon.Error != nil {
		payload["error"] = map[string]any{"code": beacon.Error.Code, "message": beacon.Error.Message}
	}
	event, err := s.appendToolEvent(ctx, run, eventType, payload)
	if err != nil {
		return nil, err
	}
	s.publishEvent(event)
	if err := s.audit.Record(ctx, beacon.RunId, "runtime", "", "tool.call.after", beacon.ToolCallId, toolAuditPayload(beacon)); err != nil {
		return nil, err
	}
	return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: beacon.ToolCallId}, nil
}

func (s *Server) NotifyApprovalUpdated(ctx context.Context, runID string, approvalID string, approvalStatus string, approvalToken string) error {
	owner := s.workerForRun(runID)
	if owner == nil {
		return nil
	}
	sendCtx, cancel := withDefaultTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := owner.send(sendCtx, &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ApprovalUpdated{ApprovalUpdated: &turingv1.RuntimeApprovalUpdated{
		ApprovalId:    approvalID,
		ApprovalToken: approvalToken,
		Status:        approvalStatus,
	}}}); err != nil {
		return err
	}
	if approvalStatus == "denied" || approvalStatus == "expired" {
		owner.releaseRun(runID)
		if err := s.DispatchPending(ctx); err != nil {
			return err
		}
	}
	return nil
}

func mapToolCallError(err error) error {
	switch {
	case errors.Is(err, repository.ErrToolCallConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, repository.ErrToolCallNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return err
	}
}

func toolAfterStatus(beacon *turingv1.ToolCallBeacon) (string, string, error) {
	switch beacon.Status {
	case turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED:
		return "completed", "tool.call.completed", nil
	case turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED:
		return "failed", "tool.call.failed", nil
	case turingv1.ToolCallStatus_TOOL_CALL_STATUS_DENIED:
		return "denied", "tool.call.denied", nil
	default:
		return "", "", status.Error(codes.InvalidArgument, "tool_call status is required")
	}
}

func beaconArgs(beacon *turingv1.ToolCallBeacon) map[string]any {
	if beacon.Args == nil {
		return map[string]any{}
	}
	return beacon.Args.AsMap()
}

func canonicalArgs(args map[string]any) (string, string, error) {
	data, err := safejson.MarshalCanonical(args)
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256(data)
	return string(data), "sha256:" + fmt.Sprintf("%x", hash[:]), nil
}

func beaconServerName(beacon *turingv1.ToolCallBeacon) string {
	if beacon.ServerName != "" {
		return beacon.ServerName
	}
	for i, r := range beacon.ToolName {
		if r == '.' {
			return beacon.ToolName[:i]
		}
	}
	return ""
}

func toolAuditPayload(beacon *turingv1.ToolCallBeacon) map[string]any {
	payload := map[string]any{
		"phase":      beacon.Phase.String(),
		"toolCallId": beacon.ToolCallId,
		"agentId":    beacon.AgentId.String(),
		"serverName": beaconServerName(beacon),
		"toolName":   beacon.ToolName,
		"runId":      beacon.RunId,
		"traceId":    beacon.TraceId,
	}
	if beacon.Args != nil {
		payload["args"] = beacon.Args.AsMap()
	}
	if beacon.Status != turingv1.ToolCallStatus_TOOL_CALL_STATUS_UNSPECIFIED {
		payload["status"] = beacon.Status.String()
	}
	if beacon.ResultSummary != "" {
		payload["resultSummary"] = beacon.ResultSummary
	}
	if beacon.DurationMs != 0 {
		payload["durationMs"] = beacon.DurationMs
	}
	if beacon.Error != nil {
		payload["error"] = map[string]any{"code": beacon.Error.Code, "message": beacon.Error.Message}
	}
	return payload
}

func (s *Server) appendToolEvent(ctx context.Context, run repository.Run, eventType string, payload map[string]any) (repository.Event, error) {
	payloadJSON, err := safejson.MarshalCanonical(payload)
	if err != nil {
		return repository.Event{}, err
	}
	return s.repo.AppendEvent(ctx, repository.AppendEventInput{
		SessionID:   run.SessionID,
		RunID:       run.RunID,
		TraceID:     run.TraceID,
		Type:        eventType,
		PayloadJSON: string(payloadJSON),
	})
}

func (s *Server) publishEvent(event repository.Event) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(events.Event{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		RunID:       nullString(event.RunID),
		TraceID:     event.TraceID,
		Sequence:    event.Sequence,
		Type:        event.Type,
		CreatedAt:   event.CreatedAt,
		PayloadJSON: event.PayloadJSON,
	})
}

func nullString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func mapJob(job repository.Job) *turingv1.AgentJob {
	provider := turingv1.ModelProvider_MODEL_PROVIDER_UNSPECIFIED
	if job.ModelProvider == "ollama" {
		provider = turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA
	}
	if job.ModelProvider == "openai_compatible" {
		provider = turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
	}
	return &turingv1.AgentJob{
		JobId:              job.JobID,
		RunId:              job.RunID,
		SessionId:          job.SessionID,
		UserMessageId:      job.UserMessageID,
		AssistantMessageId: job.AssistantMessageID,
		AgentId:            turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		TraceId:            job.TraceID,
		ModelProvider:      provider,
		Model:              job.Model,
		UserText:           job.UserText,
		Attempt:            int32(job.Attempt),
	}
}
