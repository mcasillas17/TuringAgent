package runtime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	defaultMaxConcurrentRuns = 1
	defaultJobMaxAttempts    = 3
	maxWorkerConcurrentRuns  = 128
	commandSendTimeout       = 5 * time.Second
	commandSendCancelGrace   = 50 * time.Millisecond
)

type Server struct {
	turingv1.UnimplementedRuntimeServiceServer
	repo               *repository.Repository
	bus                *events.Bus
	approvals          approvalCreator
	audit              *auditsvc.Server
	mu                 sync.Mutex
	workers            map[string]*worker
	availabilityMu     sync.Mutex
	unavailablePending map[string]unavailableRoutingState
	workerStreams      sync.WaitGroup
	toolsMu            sync.Mutex
	toolsets           map[string]workerToolset
	dispatch           DispatchConfig
}

type approvalCreator interface {
	CreateApprovalForTool(ctx context.Context, runID string, toolCallID string, agentID string, toolName string, args map[string]any) (string, error)
}

// unattendedApprover is satisfied by the real approval service and is how an
// automation's pre-approval reaches the ordinary approval path.
//
// It is a separate, optional interface rather than a second method on
// approvalCreator so that an approval service which cannot grant unattended
// approvals is a possible state rather than a compile error. When it is
// missing, an automation's approval-required tool is blocked rather than
// silently waiting — the safe direction.
type unattendedApprover interface {
	GrantUnattendedApproval(ctx context.Context, approvalID string, serverName string, toolName string) error
}

// AutomationNotAllowlistedCode is the run error an automation gets when it
// reaches a tool it was not pre-approved for. Named because the client shows
// it and a test asserts on it.
const AutomationNotAllowlistedCode = "automation_tool_not_allowlisted"

type worker struct {
	commands       chan *turingv1.RuntimeCommand
	done           chan struct{}
	registrationID string
	capabilities   *registeredWorkerCapabilities
	maxConcurrent  int
	assignments    map[string]assignment
	pendingClaims  int
	updateMu       sync.Mutex
	mu             sync.Mutex
	closed         bool
	lastHeartbeat  time.Time
	sender         *runtimeCommandSender
}

type workerToolset struct {
	owner *worker
	tools []repository.DiscoveredTool
}

type assignment struct {
	jobID     string
	runID     string
	attemptID string
}

type DispatchConfig struct {
	MaxConcurrentRuns  int
	LeaseDuration      time.Duration
	MaxAttempts        int
	LegacyCapabilities *LegacyCapabilityProfile
}

var errRuntimeCommandSenderClosed = errors.New("runtime command sender closed")

// errWorkerStreamCancelled is the single error ConnectWorker reports when the
// worker stream's context is cancelled. Cancellation reaches the handler over
// several paths that report it differently — a failed Recv or Send, an
// interrupted dispatch, or the command loop's own ctx.Done — so the handler
// canonicalises them onto this value. Without that, the error a cancelled
// stream produced depended on which goroutine the scheduler happened to run
// first.
var errWorkerStreamCancelled = status.Error(codes.Canceled, "worker stream cancelled")

type runtimeCommandSender struct {
	stream turingv1.RuntimeService_ConnectWorkerServer
	gate   chan struct{}
	closed atomic.Bool
}

func newRuntimeCommandSender(stream turingv1.RuntimeService_ConnectWorkerServer) *runtimeCommandSender {
	sender := &runtimeCommandSender{stream: stream, gate: make(chan struct{}, 1)}
	sender.gate <- struct{}{}
	return sender
}

func (s *runtimeCommandSender) send(ctx context.Context, command *turingv1.RuntimeCommand) error {
	_, err := s.sendTracked(ctx, command)
	return err
}

func (s *runtimeCommandSender) sendTracked(ctx context.Context, command *turingv1.RuntimeCommand) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.closed.Load() {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return false, errRuntimeCommandSenderClosed
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-s.gate:
	}
	if s.closed.Load() {
		s.gate <- struct{}{}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return false, errRuntimeCommandSenderClosed
	}
	result := make(chan error, 1)
	go func() {
		result <- s.stream.Send(command)
		s.gate <- struct{}{}
	}()
	select {
	case err := <-result:
		return true, err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.Canceled) {
			timer := time.NewTimer(commandSendCancelGrace)
			select {
			case err := <-result:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return true, err
			case <-timer.C:
			}
		}
		s.closed.Store(true)
		return true, ctx.Err()
	}
}

func (s *runtimeCommandSender) close() {
	if s != nil {
		s.closed.Store(true)
	}
}

func New(repo *repository.Repository, bus *events.Bus, approvalServices ...approvalCreator) *Server {
	return NewWithConfig(repo, bus, DispatchConfig{}, approvalServices...)
}

func NewWithConfig(repo *repository.Repository, bus *events.Bus, dispatch DispatchConfig, approvalServices ...approvalCreator) *Server {
	var approvals approvalCreator
	if len(approvalServices) > 0 {
		approvals = approvalServices[0]
	}
	if dispatch.LeaseDuration <= 0 {
		dispatch.LeaseDuration = 5 * time.Minute
	}
	if dispatch.MaxAttempts <= 0 {
		dispatch.MaxAttempts = defaultJobMaxAttempts
	}
	server := &Server{
		repo: repo, bus: bus, approvals: approvals, audit: auditsvc.New(repo),
		workers:            map[string]*worker{},
		unavailablePending: map[string]unavailableRoutingState{},
		toolsets:           map[string]workerToolset{},
		dispatch:           dispatch,
	}
	if setter, ok := approvals.(interface{ SetNotifier(approvalsvc.Notifier) }); ok {
		setter.SetNotifier(server)
	}
	return server
}

func (s *Server) ConnectWorker(stream turingv1.RuntimeService_ConnectWorkerServer) (returnErr error) {
	ctx := stream.Context()
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	ready := first.GetWorkerReady()
	if ready == nil || ready.WorkerId == "" {
		return status.Error(codes.InvalidArgument, "worker_ready is required")
	}
	var (
		capabilities  *registeredWorkerCapabilities
		discovered    []repository.DiscoveredTool
		maxConcurrent int
	)
	if ready.GetCapabilities() != nil {
		switch ready.GetToolDiscoveryStatus() {
		case turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_UNSPECIFIED,
			turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE:
		case turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_FAILED:
			return status.Error(codes.FailedPrecondition, "worker capabilities are unavailable: worker tool discovery failed")
		default:
			return status.Error(codes.InvalidArgument, "worker tool discovery status is invalid")
		}
		if ready.GetRegistrationId() == "" {
			return status.Error(codes.InvalidArgument, "worker registration_id is required")
		}
		capabilities, discovered, err = decodeWorkerCapabilities(ready.GetCapabilities())
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "worker capabilities are invalid: %v", err)
		}
		maxConcurrent = capabilities.maxConcurrentRuns
	} else {
		if s.dispatch.LegacyCapabilities == nil {
			return status.Error(codes.FailedPrecondition, "legacy worker capabilities are unavailable: legacy capability profile is not configured")
		}
		if ready.GetToolDiscoveryStatus() == turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_FAILED {
			return status.Error(codes.FailedPrecondition, "legacy worker capabilities are unavailable: worker tool discovery failed")
		}
		capabilities, discovered, err = decodeLegacyWorkerCapabilities(s.dispatch.LegacyCapabilities, ready)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "legacy worker capabilities are invalid: %v", err)
		}
		maxConcurrent = capabilities.maxConcurrentRuns
	}
	s.refreshPendingCapabilityStateAdvisory(ctx, "registry seed", "", false, false)
	commands := make(chan *turingv1.RuntimeCommand, maxWorkerConcurrentRuns)
	connectedWorker := &worker{
		commands:       commands,
		done:           make(chan struct{}),
		registrationID: ready.GetRegistrationId(),
		capabilities:   capabilities,
		maxConcurrent:  maxConcurrent,
		assignments:    map[string]assignment{},
		lastHeartbeat:  time.Now().UTC(),
	}
	for {
		s.mu.Lock()
		existing := s.workers[ready.WorkerId]
		if existing == nil {
			s.workers[ready.WorkerId] = connectedWorker
			s.mu.Unlock()
			break
		}
		s.mu.Unlock()
		if !existing.closeIfExpiredIdle(time.Now().UTC(), s.dispatch.LeaseDuration) {
			return status.Error(codes.AlreadyExists, "worker already connected")
		}
		s.mu.Lock()
		if s.workers[ready.WorkerId] == existing {
			s.workers[ready.WorkerId] = connectedWorker
			s.mu.Unlock()
			break
		}
		s.mu.Unlock()
	}
	s.workerStreams.Add(1)
	defer s.workerStreams.Done()
	defer func() {
		// Cancelling the stream makes several exit paths ready at once — the
		// receive goroutine's Recv, an in-flight dispatch, and the command
		// loop's ctx.Done — each of which reports cancellation differently.
		// Collapse them here so the handler's outcome describes the event
		// rather than whichever goroutine the scheduler ran first. Errors the
		// teardown below discovers are still joined on afterwards.
		if isStreamCancellation(ctx, returnErr) {
			returnErr = errWorkerStreamCancelled
		}
		assignments := connectedWorker.close()
		s.removeWorkerRegistration(ready.WorkerId, connectedWorker)
		if err := s.removeDiscoveredTools(ready.GetWorkerId(), connectedWorker); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("remove worker tool capabilities: %w", err))
		}
		reconciled, err := s.reconcileAssignments(assignments, ready.WorkerId)
		if err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("reconcile worker assignments: %w", err))
		}
		availabilityCtx, cancelAvailability := context.WithTimeout(context.Background(), 5*time.Second)
		s.refreshPendingCapabilityStateAdvisory(availabilityCtx, "worker disconnected", ready.GetWorkerId(), true, false)
		cancelAvailability()
		if reconciled {
			recoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.DispatchPending(recoveryCtx); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("dispatch reconciled assignments: %w", err))
			}
		}
	}()
	if err := s.persistDiscoveredTools(ctx, ready.GetWorkerId(), connectedWorker, discovered); err != nil {
		return status.Error(codes.Internal, "persist worker tool capabilities")
	}
	s.refreshPendingCapabilityStateAdvisory(ctx, "worker connected", ready.GetWorkerId(), true, true)
	acceptedCtx, cancelAccepted := withDefaultTimeout(ctx, commandSendTimeout)
	err = connectedWorker.commandSender(stream).send(acceptedCtx, &turingv1.RuntimeCommand{
		Command: &turingv1.RuntimeCommand_WorkerAccepted{WorkerAccepted: &turingv1.RuntimeWorkerAccepted{
			WorkerId: ready.WorkerId, RegistrationId: ready.GetRegistrationId(),
		}},
	})
	cancelAccepted()
	if err != nil {
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
			if heartbeat := update.GetHeartbeat(); heartbeat != nil {
				if err := s.renewWorkerLeases(ctx, ready.WorkerId, connectedWorker, heartbeat); err != nil {
					recvErr <- err
					return
				}
				continue
			}
			if capabilitiesUpdated := update.GetWorkerCapabilitiesUpdated(); capabilitiesUpdated != nil {
				connectedWorker.updateMu.Lock()
				err := s.replaceWorkerCapabilities(ctx, ready.GetWorkerId(), connectedWorker, capabilitiesUpdated)
				connectedWorker.updateMu.Unlock()
				if err != nil {
					recvErr <- err
					return
				}
				continue
			}
			if beacon := update.GetToolBeacon(); beacon != nil {
				release, beginErr := connectedWorker.beginUpdate(update)
				if beginErr != nil {
					if err := s.sendBeaconDecision(ctx, connectedWorker, beacon, protocolErrorDecision(beacon)); err != nil {
						recvErr <- err
						return
					}
					continue
				}
				decision, beaconErr := s.handleToolBeaconForWorker(ctx, beacon, ready.WorkerId, connectedWorker)
				release()
				if beaconErr != nil && decision == nil {
					decision = protocolErrorDecision(beacon)
				}
				if err := s.sendBeaconDecision(ctx, connectedWorker, beacon, decision); err != nil {
					recvErr <- err
					return
				}
				continue
			}
			release, err := connectedWorker.beginUpdate(update)
			if err != nil {
				ignored, duplicateErr := s.isLateMatchingTerminalUpdate(ctx, update)
				if duplicateErr != nil {
					recvErr <- duplicateErr
					return
				}
				if ignored {
					continue
				}
				recvErr <- err
				return
			}
			err = func() error {
				defer release()
				if terminalRunID(update) != "" {
					handled, reconcileErr := s.reconcileLateAssignedUpdate(ctx, connectedWorker, ready.WorkerId, update)
					if reconcileErr != nil {
						return reconcileErr
					}
					if handled {
						return nil
					}
				}
				suppressDispatch, err := s.applyUpdateForWorker(ctx, update)
				if err != nil {
					handled, reconcileErr := s.reconcileLateAssignedUpdate(ctx, connectedWorker, ready.WorkerId, update)
					if reconcileErr != nil {
						return reconcileErr
					}
					if handled {
						return nil
					}
					return err
				}
				if runID := terminalRunID(update); runID != "" {
					connectedWorker.releaseRun(runID)
					if suppressDispatch {
						// A worker_busy rejection just requeued this run.
						// Dispatching now would hand the queue straight back to
						// the still-draining worker in a tight loop. Its drain
						// completion emits a terminal update, and that path
						// dispatches — see workerBusyFailureCode.
						return nil
					}
					if err := s.DispatchPending(ctx); err != nil {
						return err
					}
				}

				return nil
			}()
			if err != nil {
				recvErr <- err
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return errWorkerStreamCancelled
		case <-connectedWorker.done:
			return status.Error(codes.Canceled, "worker is disconnected")
		case err := <-recvErr:
			return err
		case cmd := <-commands:
			if err := s.sendCommand(ctx, stream, cmd, connectedWorker, ready.WorkerId); err != nil {
				return errors.Join(err, s.handleUndeliveredCommand(ctx, cmd, connectedWorker))
			}
		}
	}
}

func (s *Server) WaitForWorkerStreams() {
	s.workerStreams.Wait()
}

// isStreamCancellation reports whether err carries nothing but a cancelled
// worker stream, so ConnectWorker can report every shape of that one event as
// errWorkerStreamCancelled. It is asked during teardown, once the stream
// context has already ended:
//
//   - Cancellation arrives as context.Canceled from an in-process stream and as
//     a Canceled status from gRPC, so both forms count.
//   - A registration the service itself closed reports a Canceled status too.
//     While the stream context is also cancelled the command loop could have
//     reported either, so folding those in is what removes the coin flip.
//   - Anything else is a distinct failure and must be reported as itself —
//     including a real failure that an errors.Join carries alongside the
//     cancellation, which teardown would otherwise drop on the floor.
func isStreamCancellation(ctx context.Context, err error) bool {
	if err == nil || !errors.Is(ctx.Err(), context.Canceled) {
		return false
	}
	return onlyCancellations(err)
}

// onlyCancellations reports whether every error joined into err is a
// cancellation. errors.Is alone cannot answer that: it matches when any branch
// of a join matches, which would let a genuine failure ride along unseen.
func onlyCancellations(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		branches := joined.Unwrap()
		if len(branches) == 0 {
			return false
		}
		for _, branch := range branches {
			if !onlyCancellations(branch) {
				return false
			}
		}
		return true
	}
	return errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled
}

func decodeDiscoveredTools(reported []*turingv1.DiscoveredTool) ([]repository.DiscoveredTool, error) {
	discovered := make([]repository.DiscoveredTool, 0, len(reported))
	type toolKey struct {
		serverName string
		toolName   string
	}
	seen := make(map[toolKey]struct{}, len(reported))
	for _, tool := range reported {
		if tool == nil || tool.GetServerName() == "" || tool.GetServerName() != strings.TrimSpace(tool.GetServerName()) ||
			tool.GetToolName() == "" || tool.GetToolName() != strings.TrimSpace(tool.GetToolName()) || tool.GetSchema() == nil {
			return nil, errors.New("tool identity and schema are required")
		}
		key := toolKey{serverName: tool.GetServerName(), toolName: tool.GetToolName()}
		if _, ok := seen[key]; ok {
			return nil, errors.New("duplicate discovered tool")
		}
		seen[key] = struct{}{}
		schemaJSON, err := protojson.Marshal(tool.GetSchema())
		if err != nil {
			return nil, err
		}
		discovered = append(discovered, repository.DiscoveredTool{
			ServerName: tool.GetServerName(),
			ToolName:   tool.GetToolName(),
			SchemaJSON: string(schemaJSON),
			Policy:     string(tools.DefaultPolicyFor(tool.GetServerName(), tool.GetToolName())),
		})
	}
	return discovered, nil
}

// LegacyPolicyToolCapabilities returns the exact rollout fallback that callers
// may opt into through LegacyCapabilityProfile.
func LegacyPolicyToolCapabilities() []*turingv1.DiscoveredTool {
	defaults := tools.LegacyPolicyDefaults()
	capabilities := make([]*turingv1.DiscoveredTool, 0, len(defaults))
	for _, tool := range defaults {
		capabilities = append(capabilities, &turingv1.DiscoveredTool{
			ServerName: tool.ServerName,
			ToolName:   tool.ToolName,
			Schema:     &structpb.Struct{},
		})
	}
	return capabilities
}

func (s *Server) persistDiscoveredTools(ctx context.Context, workerID string, owner *worker, discovered []repository.DiscoveredTool) error {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()
	previous, hadPrevious := s.toolsets[workerID]
	s.toolsets[workerID] = workerToolset{owner: owner, tools: discovered}
	if err := s.repo.UpsertTools(ctx, unionToolsets(s.toolsets)); err != nil {
		if hadPrevious {
			s.toolsets[workerID] = previous
		} else {
			delete(s.toolsets, workerID)
		}
		return err
	}
	return nil
}

func (s *Server) removeDiscoveredTools(workerID string, owner *worker) error {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()
	toolset, ok := s.toolsets[workerID]
	if !ok || toolset.owner != owner {
		return nil
	}
	delete(s.toolsets, workerID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.repo.UpsertTools(ctx, unionToolsets(s.toolsets)); err != nil {
		s.toolsets[workerID] = toolset
		return err
	}
	return nil
}

func unionToolsets(toolsets map[string]workerToolset) []repository.DiscoveredTool {
	workerIDs := make([]string, 0, len(toolsets))
	for workerID := range toolsets {
		workerIDs = append(workerIDs, workerID)
	}
	sort.Strings(workerIDs)

	type key struct{ serverName, toolName string }
	union := make(map[key]repository.DiscoveredTool)
	for _, workerID := range workerIDs {
		for _, tool := range toolsets[workerID].tools {
			union[key{serverName: tool.ServerName, toolName: tool.ToolName}] = tool
		}
	}
	keys := make([]key, 0, len(union))
	for toolKey := range union {
		keys = append(keys, toolKey)
	}
	sort.Slice(keys, func(i int, j int) bool {
		if keys[i].serverName == keys[j].serverName {
			return keys[i].toolName < keys[j].toolName
		}
		return keys[i].serverName < keys[j].serverName
	})
	out := make([]repository.DiscoveredTool, 0, len(keys))
	for _, toolKey := range keys {
		out = append(out, union[toolKey])
	}
	return out
}

func (s *Server) workerHasTool(workerID string, owner *worker, serverName string, toolName string) bool {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()
	toolset, ok := s.toolsets[workerID]
	if !ok || toolset.owner != owner {
		return false
	}
	for _, tool := range toolset.tools {
		if tool.ServerName == serverName && tool.ToolName == toolName {
			return true
		}
	}
	return false
}

func (s *Server) renewWorkerLeases(ctx context.Context, workerID string, connectedWorker *worker, heartbeat *turingv1.RuntimeHeartbeat) error {
	if heartbeat == nil || heartbeat.GetWorkerId() == "" {
		return status.Error(codes.InvalidArgument, "heartbeat worker_id is required")
	}
	if heartbeat.GetWorkerId() != workerID {
		return status.Error(codes.PermissionDenied, "heartbeat worker_id does not match connected worker")
	}
	connectedWorker.updateMu.Lock()
	defer connectedWorker.updateMu.Unlock()
	assignments := connectedWorker.assignmentSnapshot(workerID)
	renewed, err := s.repo.RenewAssignments(ctx, assignments, time.Now().UTC().Add(s.dispatch.LeaseDuration))
	if err != nil {
		return err
	}
	renewedSet := make(map[repository.Assignment]struct{}, len(renewed))
	for _, assignment := range renewed {
		renewedSet[assignment] = struct{}{}
	}
	reconciled := false
	for _, assignment := range assignments {
		if _, ok := renewedSet[assignment]; ok {
			continue
		}
		result, err := s.repo.RecoverAssignmentWithLimit(ctx, assignment, s.dispatch.MaxAttempts)
		if err != nil {
			return err
		}
		released := connectedWorker.releaseAssignmentAttempt(assignment.RunID, assignment.AttemptID)
		for _, event := range result.Events {
			s.publishEvent(event)
		}
		reconciled = reconciled || result.Requeued || result.Cleared || released
	}
	revived := connectedWorker.recordHeartbeat(time.Now().UTC(), s.dispatch.LeaseDuration)
	if revived {
		s.refreshPendingCapabilityStateAdvisory(ctx, "worker heartbeat restored", workerID, false, true)
	}
	if reconciled || revived {
		return s.DispatchPending(ctx)
	}
	return nil
}

func (s *Server) sendBeaconDecision(ctx context.Context, connectedWorker *worker, beacon *turingv1.ToolCallBeacon, decision *turingv1.ToolPolicyDecision) error {
	if decision == nil {
		decision = protocolErrorDecision(beacon)
	} else {
		decision = proto.Clone(decision).(*turingv1.ToolPolicyDecision)
	}
	decision.Phase = beacon.GetPhase()
	if err := connectedWorker.send(ctx, &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_ToolPolicyDecision{ToolPolicyDecision: decision}}); err != nil {
		return errors.Join(err, s.terminalizeApprovalDeliveryFailure(ctx, decision.GetApprovalId(), connectedWorker))
	}
	return nil
}

func protocolErrorDecision(beacon *turingv1.ToolCallBeacon) *turingv1.ToolPolicyDecision {
	return &turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_DENY,
		ToolCallId: beacon.GetToolCallId(),
		Reason:     "invalid_tool_beacon",
	}
}

func terminalRunDecision(beacon *turingv1.ToolCallBeacon, reason string) *turingv1.ToolPolicyDecision {
	return &turingv1.ToolPolicyDecision{
		Decision:    turingv1.ToolPolicyDecision_DECISION_DENY,
		ToolCallId:  beacon.GetToolCallId(),
		Reason:      reason,
		TerminalRun: true,
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

func (s *Server) handleUndeliveredCommand(ctx context.Context, cmd *turingv1.RuntimeCommand, worker *worker) error {
	if decision := cmd.GetToolPolicyDecision(); decision != nil && decision.GetApprovalId() != "" {
		return s.terminalizeApprovalDeliveryFailure(ctx, decision.GetApprovalId(), worker)
	}
	return nil
}

func (s *Server) sendCommand(ctx context.Context, stream turingv1.RuntimeService_ConnectWorkerServer, cmd *turingv1.RuntimeCommand, connectedWorker *worker, workerID string) error {
	if cmd == nil {
		return status.Error(codes.Canceled, "worker command queue closed")
	}
	sendCtx, cancel := withDefaultTimeout(ctx, commandSendTimeout)
	defer cancel()
	assigned := cmd.GetRunAssigned()
	if assigned == nil {
		return connectedWorker.commandSender(stream).send(sendCtx, cmd)
	}
	connectedWorker.updateMu.Lock()
	defer connectedWorker.updateMu.Unlock()
	currentAssignment, ok := connectedWorker.assignmentForRun(assigned.RunId)
	if !ok {
		// The command was queued while this worker held the run; by the time it
		// reached the front of the queue the assignment had been released —
		// terminalized, requeued, or reconciled by another path. Handing the
		// worker a run it no longer owns would be wrong, so the command is
		// dropped and the loop continues.
		//
		// This used to return ErrAssignmentFenced, which the caller turns into
		// a stream error: one command that merely arrived too late tore down
		// the whole connection, costing the worker every OTHER assignment it
		// was holding and orphaning their runs until the reaper noticed. That
		// is what made TestTerminalizedAssignedRunReconcilesMatchingLateTerminal
		// Update fail intermittently under CI's slower scheduling — the late
		// terminal update released the run while its own dispatch was still in
		// flight.
		//
		// Nothing is lost by dropping it: handleUndeliveredCommand only acts on
		// tool-policy decisions, and whichever path released the assignment
		// already owns requeueing or terminalizing the run.
		return nil
	}
	s.mu.Lock()
	ownsRegistration := s.workers[workerID] == connectedWorker
	s.mu.Unlock()
	now := time.Now().UTC()
	connectedWorker.mu.Lock()
	supported := ownsRegistration &&
		!connectedWorker.closed &&
		!connectedWorker.lastHeartbeat.IsZero() &&
		now.Before(connectedWorker.lastHeartbeat.Add(s.dispatch.LeaseDuration)) &&
		workerCapabilitiesSupportRoute(connectedWorker.capabilities, routingRequirementsForAgentJob(assigned))
	connectedWorker.mu.Unlock()
	if !supported {
		_ = connectedWorker.releaseRun(assigned.RunId)
		if err := s.requeueAssignments([]assignment{currentAssignment}); err != nil {
			if !errors.Is(err, repository.ErrAssignmentFenced) {
				return err
			}
			if err := s.acknowledgeFencedAssignment(assigned.RunId); err != nil {
				return err
			}
		}
		s.refreshPendingCapabilityStateAdvisory(
			ctx, "worker route changed before assignment delivery", workerID, true, false,
		)
		return s.DispatchPending(ctx)
	}
	repositoryAssignment := repository.Assignment{
		JobID: currentAssignment.jobID, RunID: currentAssignment.runID, WorkerID: workerID, AttemptID: currentAssignment.attemptID,
	}
	if err := s.repo.BeginAssignmentSend(ctx, repositoryAssignment); err != nil {
		_ = connectedWorker.releaseRun(assigned.RunId)
		if errors.Is(err, repository.ErrAssignmentFenced) {
			if err := s.acknowledgeFencedAssignment(assigned.RunId); err != nil {
				return err
			}
			return s.DispatchPending(ctx)
		}
		_ = s.repo.AbortPendingAssignment(context.Background(), repositoryAssignment)
		return err
	}
	sendStarted, err := connectedWorker.commandSender(stream).sendTracked(sendCtx, cmd)
	if err != nil {
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !sendStarted {
			abortErr := s.repo.AbortUnsentAssignment(recoveryCtx, repositoryAssignment)
			if abortErr != nil && !errors.Is(abortErr, repository.ErrAssignmentFenced) {
				return errors.Join(err, abortErr)
			}
			return errors.Join(err, s.DispatchPending(recoveryCtx))
		}
		return errors.Join(err, s.repo.MarkAssignmentDeliveryUncertain(recoveryCtx, repositoryAssignment))
	}
	if err := s.repo.MarkAssignmentDelivered(ctx, repositoryAssignment); err != nil {
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.MarkAssignmentDeliveryUncertain(recoveryCtx, repositoryAssignment)
		return err
	}
	return nil
}

func (s *Server) acknowledgeFencedAssignment(runID string) error {
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.repo.AcknowledgeExecutionExit(recoveryCtx, runID)
	if errors.Is(err, repository.ErrRunNotActive) {
		return nil
	}
	return err
}

func (s *Server) terminalizeApprovalDeliveryFailure(_ context.Context, approvalID string, _ *worker) error {
	if approvalID == "" {
		return nil
	}
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	transition, err := s.repo.FailApprovalDeliveryWithEvent(recoveryCtx, approvalID, "")
	if err != nil {
		return fmt.Errorf("terminalize undelivered approval: %w", err)
	}
	if transition.Changed {
		if transition.ApprovalEvent.EventID != "" {
			s.publishEvent(transition.ApprovalEvent)
		}
		if transition.ToolEvent.EventID != "" {
			s.publishEvent(transition.ToolEvent)
		}
		if transition.RunFailedEvent.EventID != "" {
			s.publishEvent(transition.RunFailedEvent)
		}
	}
	return nil
}

func (s *Server) requeueAssignments(assignments []assignment) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var errs []error
	for _, assignment := range assignments {
		if err := s.repo.AbortPendingAssignment(ctx, repository.Assignment{
			JobID: assignment.jobID, RunID: assignment.runID, AttemptID: assignment.attemptID,
		}); err != nil {
			errs = append(errs, fmt.Errorf("requeue job %s for run %s: %w", assignment.jobID, assignment.runID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Server) reconcileAssignments(assignments []assignment, workerID string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var errs []error
	reconciled := false
	for _, assignment := range assignments {
		result, err := s.repo.ReconcileAssignmentWithLimit(ctx, repository.Assignment{
			JobID: assignment.jobID, RunID: assignment.runID, WorkerID: workerID, AttemptID: assignment.attemptID,
		}, s.dispatch.MaxAttempts)
		if err != nil {
			errs = append(errs, fmt.Errorf("reconcile run %s for job %s: %w", assignment.runID, assignment.jobID, err))
			continue
		}
		for _, event := range result.Events {
			s.publishEvent(event)
		}
		reconciled = reconciled || result.Requeued || result.Cleared
	}
	return reconciled, errors.Join(errs...)
}

func (s *Server) DispatchPending(ctx context.Context) error {
	dispatchCtx, cancel := withDefaultTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		restart := false
		workers := s.snapshotWorkers()
		for _, entry := range workers {
			for {
				assigned, noJob, restartDispatch, err := s.dispatchToWorker(dispatchCtx, entry.workerID, entry.worker)
				if err != nil {
					return err
				}
				if restartDispatch {
					restart = true
					break
				}
				if noJob || !assigned {
					break
				}
			}
			if restart {
				break
			}
		}
		if !restart {
			return nil
		}
	}
}

func (s *Server) RefreshPendingRoutingState(ctx context.Context, cause string) error {
	return s.refreshPendingCapabilityState(ctx, cause, "", true, true)
}

func (s *Server) RecoverOrphanedAssignments(ctx context.Context) error {
	recoveryCtx, cancel := withDefaultTimeout(ctx, 5*time.Second)
	defer cancel()
	cutoff := time.Now().UTC()
	assignments, err := s.repo.RecoverableAssignments(recoveryCtx, cutoff)
	if err != nil {
		return err
	}
	recovered := false
	for _, candidate := range assignments {
		if s.hasLiveAssignment(candidate) {
			continue
		}
		released := false
		if connected := s.registeredWorker(candidate.WorkerID); connected != nil {
			closedAssignments, closed, live := connected.closeForStaleAssignment(
				candidate.RunID, candidate.AttemptID, cutoff, s.dispatch.LeaseDuration,
			)
			if live {
				continue
			}
			if closed {
				s.removeWorkerRegistration(candidate.WorkerID, connected)
				released = true
				var remaining []assignment
				for _, assigned := range closedAssignments {
					if assigned.runID != candidate.RunID || assigned.attemptID != candidate.AttemptID {
						remaining = append(remaining, assigned)
					}
				}
				if _, err := s.reconcileAssignments(remaining, candidate.WorkerID); err != nil {
					return err
				}
			}
		}
		result, err := s.repo.RecoverAssignmentAtCutoffWithLimit(recoveryCtx, candidate, cutoff, s.dispatch.MaxAttempts)
		if err != nil {
			return err
		}
		for _, event := range result.Events {
			s.publishEvent(event)
		}
		recovered = recovered || result.Requeued || result.Cleared || released
	}
	s.refreshPendingCapabilityStateAdvisory(recoveryCtx, "worker heartbeat expired", "", true, false)
	if recovered {
		return s.DispatchPending(recoveryCtx)
	}
	return nil
}

func (s *Server) registeredWorker(workerID string) *worker {
	if workerID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workers[workerID]
}

func (s *Server) removeWorkerRegistration(workerID string, target *worker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workers[workerID] == target {
		delete(s.workers, workerID)
	}
}

func (s *Server) RunRecoveryLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.RecoverOrphanedAssignments(ctx)
		}
	}
}

func (s *Server) refreshPendingCapabilityStateAdvisory(
	ctx context.Context,
	cause string,
	workerID string,
	publishLosses bool,
	publishRestorations bool,
) {
	if err := s.refreshPendingCapabilityState(ctx, cause, workerID, publishLosses, publishRestorations); err != nil {
		log.Printf("refresh pending routing state after %s: %v", cause, err)
	}
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

func (s *Server) hasLiveAssignment(candidate repository.Assignment) bool {
	now := time.Now().UTC()
	for _, entry := range s.snapshotWorkers() {
		if entry.workerID != candidate.WorkerID {
			continue
		}
		if entry.worker.hasLiveAssignmentAttempt(candidate.RunID, candidate.AttemptID, now, s.dispatch.LeaseDuration) {
			return true
		}
	}
	return false
}

func (s *Server) dispatchToWorker(
	ctx context.Context,
	workerID string,
	worker *worker,
) (assigned bool, noJob bool, restartDispatch bool, err error) {
	worker.mu.Lock()
	if worker.closed ||
		worker.lastHeartbeat.IsZero() ||
		!time.Now().UTC().Before(worker.lastHeartbeat.Add(s.dispatch.LeaseDuration)) ||
		len(worker.assignments)+worker.pendingClaims >= worker.maxConcurrent {
		worker.mu.Unlock()
		return false, false, false, nil
	}
	capabilities := worker.capabilities
	worker.pendingClaims++
	worker.mu.Unlock()
	job, err := s.repo.ClaimNextCompatibleJobWithLimit(
		ctx,
		"general_assistant",
		workerID,
		s.dispatch.MaxConcurrentRuns,
		s.dispatch.LeaseDuration,
		repositoryRoutingCapabilities(capabilities),
		func(route repository.RoutingRequirements) bool {
			return workerCapabilitiesSupportRoute(capabilities, route)
		},
	)
	worker.mu.Lock()
	worker.pendingClaims--
	if err != nil {
		worker.mu.Unlock()
		return false, false, false, err
	}
	if job.JobID == "" {
		worker.mu.Unlock()
		return false, true, false, nil
	}
	claimedAssignment := assignment{jobID: job.JobID, runID: job.RunID, attemptID: job.AssignmentAttemptID}
	if worker.closed ||
		worker.lastHeartbeat.IsZero() ||
		!time.Now().UTC().Before(worker.lastHeartbeat.Add(s.dispatch.LeaseDuration)) ||
		len(worker.assignments) >= worker.maxConcurrent ||
		!workerCapabilitiesSupportRoute(worker.capabilities, routingRequirementsForJob(job)) {
		worker.mu.Unlock()
		if err := s.requeueAssignments([]assignment{claimedAssignment}); err != nil {
			return false, false, false, err
		}
		return false, false, true, nil
	}
	worker.assignments[job.RunID] = claimedAssignment
	select {
	case worker.commands <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: mapJob(job)}}:
		worker.mu.Unlock()
		s.publishEvent(job.StartedEvent)
		return true, false, false, nil
	case <-ctx.Done():
		delete(worker.assignments, job.RunID)
		worker.mu.Unlock()
		return false, false, false, errors.Join(ctx.Err(), s.requeueAssignments([]assignment{claimedAssignment}))
	}
}

func repositoryRoutingCapabilities(capabilities *registeredWorkerCapabilities) *repository.WorkerRoutingCapabilities {
	if capabilities == nil {
		return &repository.WorkerRoutingCapabilities{}
	}
	models := make([]repository.RoutingModelCapability, 0, len(capabilities.models))
	for _, model := range capabilities.models {
		models = append(models, repository.RoutingModelCapability{
			Provider: model.provider, Model: model.model, MaxContextTokens: model.maxContextTokens,
		})
	}
	tools := make([]string, 0, len(capabilities.tools))
	for tool := range capabilities.tools {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	credentialRefs := make([]string, 0, len(capabilities.externalAgentCredentialRefs))
	for credentialRef := range capabilities.externalAgentCredentialRefs {
		credentialRefs = append(credentialRefs, credentialRef)
	}
	sort.Strings(credentialRefs)
	return &repository.WorkerRoutingCapabilities{
		Models: models, Tools: tools, MaxConcurrentRuns: capabilities.maxConcurrentRuns,
		ExternalAgentCredentialRefs: credentialRefs,
	}
}

func routingRequirementsForJob(job repository.Job) repository.RoutingRequirements {
	externalAgentCredentialRef := ""
	if job.ExternalAgent != nil {
		externalAgentCredentialRef = job.ExternalAgent.CredentialRef
	}
	return repository.RoutingRequirements{
		AgentID:                        job.AgentID,
		ModelProvider:                  job.ModelProvider,
		Model:                          job.Model,
		RequestedTools:                 job.RequestedTools,
		RequiredContextTokens:          job.RequiredContextTokens,
		MinimumWorkerMaxConcurrentRuns: job.MinimumWorkerMaxConcurrentRuns,
		ExternalAgent:                  job.ExternalAgent != nil,
		ExternalAgentCredentialRef:     externalAgentCredentialRef,
	}
}

func routingRequirementsForAgentJob(job *turingv1.AgentJob) repository.RoutingRequirements {
	if job == nil {
		return repository.RoutingRequirements{}
	}
	externalAgentCredentialRef := ""
	if job.GetExternalAgent() != nil {
		externalAgentCredentialRef = job.GetExternalAgent().GetCredentialRef()
	}
	return repository.RoutingRequirements{
		AgentID:                        agentIDName(job.GetAgentId()),
		ModelProvider:                  modelProviderName(job.GetModelProvider()),
		Model:                          job.GetModel(),
		RequestedTools:                 append([]string(nil), job.GetRequestedTools()...),
		RequiredContextTokens:          int(job.GetRequiredContextTokens()),
		MinimumWorkerMaxConcurrentRuns: int(job.GetMinimumWorkerMaxConcurrentRuns()),
		ExternalAgent:                  job.GetExternalAgent() != nil,
		ExternalAgentCredentialRef:     externalAgentCredentialRef,
	}
}

func (s *Server) CancelRun(ctx context.Context, runID string, reason string) {
	if runID == "" {
		return
	}
	owner := s.workerForRun(runID)
	if owner == nil {
		s.releaseUnownedTerminalRun(ctx, runID)
		return
	}
	sendCtx, cancel := withDefaultTimeout(ctx, 5*time.Second)
	defer cancel()
	command := &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunCancelled{RunCancelled: &turingv1.RuntimeRunCancelled{RunId: runID, Reason: reason}}}
	_ = owner.send(sendCtx, command)
}

func (s *Server) releaseUnownedTerminalRun(ctx context.Context, runID string) {
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil || !isTerminalRunStatus(run.Status) {
		return
	}
	if run.ExecutionActive && run.ExecutionState != "pending_send" {
		return
	}
	if err := s.repo.AcknowledgeExecutionExit(ctx, runID); err != nil {
		return
	}
	_ = s.DispatchPending(ctx)
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
	if w.closed {
		w.mu.Unlock()
		return status.Error(codes.Canceled, "worker is disconnected")
	}
	commands := w.commands
	select {
	case commands <- command:
		w.mu.Unlock()
		return nil
	case <-ctx.Done():
		w.mu.Unlock()
		return ctx.Err()
	}
}

func (w *worker) beginUpdate(update *turingv1.RuntimeUpdate) (func(), error) {
	runID := updateRunID(update)
	w.updateMu.Lock()
	if runID == "" {
		w.mu.Lock()
		closed := w.closed
		w.mu.Unlock()
		if closed {
			w.updateMu.Unlock()
			return nil, status.Error(codes.Canceled, "worker is disconnected")
		}
		return w.updateMu.Unlock, nil
	}
	w.mu.Lock()
	_, assigned := w.assignments[runID]
	closed := w.closed
	w.mu.Unlock()
	if closed {
		w.updateMu.Unlock()
		return nil, status.Error(codes.Canceled, "worker is disconnected")
	}
	if !assigned {
		if beacon := update.GetToolBeacon(); beacon != nil && beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
			return w.updateMu.Unlock, nil
		}
		w.updateMu.Unlock()
		return nil, status.Error(codes.PermissionDenied, "run is not assigned to worker")
	}
	return w.updateMu.Unlock, nil
}

func (w *worker) hasAssignment(runID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.assignments[runID]
	return ok
}

func (w *worker) hasLiveAssignmentAttempt(runID string, attemptID string, now time.Time, leaseDuration time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.lastHeartbeat.IsZero() {
		return false
	}
	assignment, ok := w.assignments[runID]
	if !ok || (attemptID != "" && assignment.attemptID != attemptID) {
		return false
	}
	return now.Before(w.lastHeartbeat.Add(leaseDuration))
}

func (w *worker) recordHeartbeat(at time.Time, leaseDuration time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false
	}
	revived := w.lastHeartbeat.IsZero() || !at.Before(w.lastHeartbeat.Add(leaseDuration))
	w.lastHeartbeat = at
	return revived
}

func (w *worker) commandSender(stream turingv1.RuntimeService_ConnectWorkerServer) *runtimeCommandSender {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sender == nil {
		w.sender = newRuntimeCommandSender(stream)
	}
	return w.sender
}

func (w *worker) assignmentForRun(runID string) (assignment, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	assignment, ok := w.assignments[runID]
	return assignment, ok
}

func (w *worker) assignmentSnapshot(workerID string) []repository.Assignment {
	w.mu.Lock()
	defer w.mu.Unlock()
	assignments := make([]repository.Assignment, 0, len(w.assignments))
	for _, assigned := range w.assignments {
		assignments = append(assignments, repository.Assignment{
			JobID: assigned.jobID, RunID: assigned.runID, WorkerID: workerID, AttemptID: assigned.attemptID,
		})
	}
	return assignments
}

func (w *worker) releaseRun(runID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, existed := w.assignments[runID]
	delete(w.assignments, runID)
	return existed
}

func (w *worker) releaseAssignmentAttempt(runID string, attemptID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	assigned, ok := w.assignments[runID]
	if !ok || (attemptID != "" && assigned.attemptID != attemptID) {
		return false
	}
	delete(w.assignments, runID)
	return true
}

func (w *worker) close() []assignment {
	w.updateMu.Lock()
	defer w.updateMu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeLocked()
}

func (w *worker) closeForStaleAssignment(runID string, attemptID string, now time.Time, leaseDuration time.Duration) ([]assignment, bool, bool) {
	w.updateMu.Lock()
	defer w.updateMu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	assigned, ok := w.assignments[runID]
	if w.closed || !ok || (attemptID != "" && assigned.attemptID != attemptID) {
		return nil, false, false
	}
	if !w.lastHeartbeat.IsZero() && now.Before(w.lastHeartbeat.Add(leaseDuration)) {
		return nil, false, true
	}
	return w.closeLocked(), true, false
}

func (w *worker) closeIfExpiredIdle(now time.Time, leaseDuration time.Duration) bool {
	w.updateMu.Lock()
	defer w.updateMu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return true
	}
	if len(w.assignments) != 0 {
		return false
	}
	if !w.lastHeartbeat.IsZero() && now.Before(w.lastHeartbeat.Add(leaseDuration)) {
		return false
	}
	w.closeLocked()
	return true
}

func (w *worker) closeLocked() []assignment {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.done != nil {
		close(w.done)
	}
	if w.sender != nil {
		w.sender.close()
	}
	assignments := make([]assignment, 0, len(w.assignments))
	for runID, assignment := range w.assignments {
		assignment.runID = runID
		assignments = append(assignments, assignment)
	}
	w.assignments = map[string]assignment{}
	return assignments
}

func (s *Server) applyUpdate(ctx context.Context, update *turingv1.RuntimeUpdate) error {
	if update == nil {
		return status.Error(codes.InvalidArgument, "runtime update is required")
	}
	switch value := update.Update.(type) {
	case *turingv1.RuntimeUpdate_Heartbeat:
		return status.Error(codes.InvalidArgument, "heartbeat is only valid on an authenticated worker stream")
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
		_, err := s.handleRunFailed(ctx, value.RunFailed)
		return err
	case *turingv1.RuntimeUpdate_RunCancelledAck:
		return s.handleRunCancelledAck(ctx, value.RunCancelledAck)
	default:
		return status.Error(codes.InvalidArgument, "unsupported runtime update")
	}
}

// applyUpdateForWorker applies a runtime update on behalf of the connected
// worker and reports whether the recv loop should SKIP the re-dispatch it
// normally runs after a terminal update. That is true only for a worker_busy
// rejection, so a worker that just said it was full is not immediately handed
// the whole queue again — see workerBusyFailureCode for why no other retryable
// failure may skip it.
func (s *Server) applyUpdateForWorker(ctx context.Context, update *turingv1.RuntimeUpdate) (suppressDispatch bool, err error) {
	if failed := update.GetRunFailed(); failed != nil {
		return s.handleRunFailed(ctx, failed)
	}
	return false, s.applyUpdate(ctx, update)
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
	out := proto.Clone(event).(*turingv1.TuringEvent)
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
	if !isTerminalRunStatus(run.Status) {
		return status.Error(codes.FailedPrecondition, "run is not cancelled")
	}
	if err := s.repo.AcknowledgeExecutionExit(ctx, ack.RunId); err != nil {
		return mapRunStateError(err)
	}
	return nil
}

func (s *Server) isLateMatchingTerminalUpdate(ctx context.Context, update *turingv1.RuntimeUpdate) (bool, error) {
	runID := terminalRunID(update)
	if runID == "" {
		return false, nil
	}
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return false, err
	}
	return isMatchingTerminalUpdate(run, update), nil
}

func isMatchingTerminalUpdate(run repository.Run, update *turingv1.RuntimeUpdate) bool {
	switch {
	case update.GetRunCompleted() != nil:
		completed := update.GetRunCompleted()
		assistantMessageID := completed.AssistantMessageId
		if assistantMessageID == "" {
			assistantMessageID = run.AssistantMessageID
		}
		return run.Status == "completed" &&
			run.TerminalEventType == "agent.run.completed" &&
			assistantMessageID != "" &&
			assistantMessageID == run.AssistantMessageID &&
			completed.Content != "" &&
			completed.Content == run.AssistantContent
	case update.GetRunFailed() != nil:
		failed := update.GetRunFailed()
		payload, err := encodePayload(map[string]any{
			"runId": failed.RunId, "code": failed.Code, "message": failed.Message, "retryable": failed.Retryable,
		})
		return err == nil &&
			run.Status == "failed" &&
			run.TerminalEventType == "agent.run.failed" &&
			payload == run.TerminalEventPayload
	case update.GetRunCancelledAck() != nil:
		return isTerminalRunStatus(run.Status)
	default:
		return false
	}
}

func (s *Server) reconcileLateAssignedUpdate(ctx context.Context, connectedWorker *worker, workerID string, update *turingv1.RuntimeUpdate) (bool, error) {
	runID := updateRunID(update)
	if runID == "" {
		return false, nil
	}
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return false, err
	}
	if !isTerminalRunStatus(run.Status) {
		return false, nil
	}
	if terminalRunID(update) == "" {
		return true, nil
	}
	if run.Status != "cancelled" && !isMatchingTerminalUpdate(run, update) {
		return true, nil
	}
	assignment, assigned := connectedWorker.assignmentForRun(runID)
	if !assigned ||
		workerID == "" ||
		run.WorkerID != workerID ||
		run.ExecutionAttemptID == "" ||
		assignment.attemptID != run.ExecutionAttemptID ||
		!run.ExecutionActive {
		return true, nil
	}
	// A terminal update from the worker that still owns this exact assignment
	// proves execution exited even when another request won the persisted run
	// outcome (for example, cancellation racing completion).
	if err := s.repo.AcknowledgeExecutionExit(ctx, runID); err != nil {
		return false, mapRunStateError(err)
	}
	connectedWorker.releaseRun(runID)
	if err := s.DispatchPending(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func isTerminalRunStatus(runStatus string) bool {
	switch runStatus {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

// runTokenUsage carries the runtime's reported counts into storage without
// filling anything in. A worker that reports nothing yields nil, which the
// repository stores as NULL and every reader treats as unknown rather than as
// zero. Presence is preserved per field: a provider that reported only output
// tokens must not have an input count invented for it here.
func runTokenUsage(reported *turingv1.RunTokenUsage) *repository.RunTokenUsage {
	if reported == nil {
		return nil
	}
	// An empty message carries no numbers, so it is silence with an envelope
	// around it.
	if reported.InputTokens == nil && reported.OutputTokens == nil {
		return nil
	}
	// Copied rather than aliased: the proto message belongs to the stream and
	// must not be able to change a value already handed to storage.
	usage := &repository.RunTokenUsage{}
	if reported.InputTokens != nil {
		input := *reported.InputTokens
		usage.InputTokens = &input
	}
	if reported.OutputTokens != nil {
		output := *reported.OutputTokens
		usage.OutputTokens = &output
	}
	return usage
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
	events, err := s.repo.CompleteRunWithEvent(ctx, completed.RunId, assistantMessageID, completed.Content, payloadJSON, runTokenUsage(completed.GetTokenUsage()))
	if err != nil {
		return mapRunStateError(err)
	}
	for _, event := range events {
		s.publishEvent(event)
	}
	return nil
}

// handleRunFailed terminalizes a failed run. A retryable failure (for example a
// worker rejecting an assignment with worker_busy) is requeued for another
// attempt instead of failing permanently, until the attempt budget is spent.
// The returned bool reports whether the caller should skip its re-dispatch, so
// suppress the immediate re-dispatch that would otherwise hand the queue
// straight back to the busy worker.
// workerBusyFailureCode is the one retryable failure whose re-dispatch may be
// suppressed. The worker reports it when it cannot accept an assignment, so it
// still holds a draining run that will emit a terminal update — and that update
// triggers DispatchPending, which is what gets the requeued run moving again.
//
// No other retryable failure has that guarantee: they happen mid-run and leave
// the worker idle, sending nothing further. A heartbeat only dispatches when a
// lease is reconciled or revived, which a healthy worker never is, and there is
// no periodic dispatcher — so suppressing those would strand the run in queued
// forever.
const workerBusyFailureCode = "worker_busy"

func (s *Server) handleRunFailed(ctx context.Context, failed *turingv1.RuntimeRunFailed) (bool, error) {
	if failed == nil || failed.RunId == "" {
		return false, status.Error(codes.InvalidArgument, "run_failed is required")
	}
	if failed.Retryable {
		decision, err := s.repo.RequeueOrFailRetryableRun(ctx, failed.RunId, failed.Code, failed.Message, s.dispatch.MaxAttempts)
		if err != nil {
			return false, mapRunStateError(err)
		}
		for _, event := range decision.Events {
			s.publishEvent(event)
		}
		// Every retryable failure is requeued, but only an assignment rejection
		// may skip the dispatch that follows. See workerBusyFailureCode.
		return decision.Requeued && failed.Code == workerBusyFailureCode, nil
	}
	payloadJSON, err := encodePayload(map[string]any{"runId": failed.RunId, "code": failed.Code, "message": failed.Message, "retryable": failed.Retryable})
	if err != nil {
		return false, err
	}
	events, err := s.repo.FailRunWithEvent(ctx, failed.RunId, failed.Code, failed.Message, payloadJSON)
	if err != nil {
		return false, mapRunStateError(err)
	}
	for _, event := range events {
		s.publishEvent(event)
	}
	return false, nil
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
	return s.handleToolBeaconForWorker(ctx, beacon, "", nil)
}

func (s *Server) handleToolBeaconForWorker(ctx context.Context, beacon *turingv1.ToolCallBeacon, workerID string, owner *worker) (*turingv1.ToolPolicyDecision, error) {
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
	if !isActiveRunStatus(run.Status) && beacon.Phase != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
		return nil, status.Error(codes.FailedPrecondition, "run is not active")
	}
	if beacon.TraceId != run.TraceID {
		return nil, status.Error(codes.InvalidArgument, "tool call trace_id does not match run")
	}
	switch beacon.Phase {
	case turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE:
		return s.handleToolBefore(ctx, beacon, run, workerID, owner)
	case turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER:
		return s.handleToolAfter(ctx, beacon, run, workerID)
	default:
		return nil, status.Error(codes.InvalidArgument, "tool_call phase is required")
	}
}

func (s *Server) handleToolBefore(ctx context.Context, beacon *turingv1.ToolCallBeacon, run repository.Run, workerID string, owner *worker) (*turingv1.ToolPolicyDecision, error) {
	args := beaconArgs(beacon)
	argsJSON, argsHash, err := canonicalArgs(args)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "tool args are not valid JSON")
	}
	if workerID != "" && !s.workerHasTool(workerID, owner, beaconServerName(beacon), beacon.ToolName) {
		return s.denyToolBefore(ctx, beacon, run, argsJSON, argsHash, "unknown_tool")
	}
	policy, ok, err := tools.GetPolicy(ctx, s.repo, beaconServerName(beacon), beacon.ToolName)
	if err != nil {
		return nil, status.Error(codes.Internal, "get tool policy failed")
	}
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
		// Whether anyone is awake to answer decides what "approval required"
		// can mean here, so it is read before the approval is created rather
		// than discovered while something waits.
		grant, unattended, err := s.repo.GetAutomationRunGrant(ctx, beacon.RunId)
		if err != nil {
			return nil, status.Error(codes.Internal, "read automation grant failed")
		}
		if unattended && !grant.Allows(beaconServerName(beacon), beacon.ToolName) {
			return s.blockUnattendedTool(ctx, beacon, run, argsJSON, argsHash, grant)
		}
		eventInput, err := toolStartedEventInput(beacon, run)
		if err != nil {
			return nil, err
		}
		recorded, event, err := s.repo.RecordToolCallBeforeWithEvent(ctx, repository.ToolCallRecord{ToolCallID: beacon.ToolCallId, RunID: beacon.RunId, ModelToolCallID: beacon.ModelToolCallId}, "general_assistant", beaconServerName(beacon), beacon.ToolName, argsJSON, argsHash, eventInput)
		if err != nil {
			return nil, mapToolCallError(err)
		}
		if event.EventID != "" {
			s.publishEvent(event)
		}
		if !recorded.Inserted {
			if decision, handled := existingToolBeforeDecision(recorded.Record, "tool_denied"); handled {
				return decision, nil
			}
		}
		if s.approvals == nil {
			return nil, status.Error(codes.FailedPrecondition, "approval service is not configured")
		}
		approvalID, err := s.approvals.CreateApprovalForTool(ctx, beacon.RunId, beacon.ToolCallId, "general_assistant", beacon.ToolName, args)
		if err != nil {
			if terminalizeErr := s.terminalizePostCommitApprovalFailure(ctx, beacon.RunId, beacon.ToolCallId); terminalizeErr != nil {
				return nil, errors.Join(err, terminalizeErr)
			}
			return terminalRunDecision(beacon, "approval_delivery_failed"), err
		}
		if unattended {
			// Granted before the decision goes back, so the runtime's first
			// poll already finds it approved. Nothing about the wait changes;
			// there is simply no wait.
			if err := s.grantUnattendedApproval(ctx, approvalID, beacon); err != nil {
				log.Printf("grant unattended approval %s for automation %s: %v", approvalID, grant.AutomationID, err)
				return s.failUnattendedRun(ctx, beacon, "automation_approval_failed",
					"This automation could not be granted the approval it was pre-authorised for, so the run stopped rather than waiting for someone to answer.")
			}
		}
		return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED, ToolCallId: beacon.ToolCallId, ApprovalId: approvalID}, nil
	}
	statusValue := "requested"
	if policy == tools.PolicySafe {
		statusValue = "allowed"
	}
	eventInput, err := toolStartedEventInput(beacon, run)
	if err != nil {
		return nil, err
	}
	recorded, event, err := s.repo.RecordToolCallBeforeWithEvent(ctx, repository.ToolCallRecord{ToolCallID: beacon.ToolCallId, RunID: beacon.RunId, Status: statusValue, ModelToolCallID: beacon.ModelToolCallId}, "general_assistant", beaconServerName(beacon), beacon.ToolName, argsJSON, argsHash, eventInput)
	if err != nil {
		return nil, mapToolCallError(err)
	}
	if event.EventID != "" {
		s.publishEvent(event)
	}
	if !recorded.Inserted {
		if decision, handled := existingToolBeforeDecision(recorded.Record, "tool_denied"); handled {
			return decision, nil
		}
	}
	if recorded.Inserted {
		if _, err := s.audit.RecordForExistingRun(ctx, beacon.RunId, "runtime", "", "tool.call.before", beacon.ToolCallId, toolAuditPayload(beacon)); err != nil {
			log.Printf("record tool before audit for %s: %v", beacon.ToolCallId, err)
		}
	}
	switch policy {
	case tools.PolicySafe:
		return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: beacon.ToolCallId}, nil
	default:
		return s.denyToolBefore(ctx, beacon, run, argsJSON, argsHash, "unknown_policy")
	}
}

// grantUnattendedApproval routes an automation's pre-approval through the
// approval service's own approve path, so the token it produces is the same
// single-use, argument-bound token a person's click produces.
func (s *Server) grantUnattendedApproval(ctx context.Context, approvalID string, beacon *turingv1.ToolCallBeacon) error {
	approver, ok := s.approvals.(unattendedApprover)
	if !ok {
		return errors.New("approval service cannot grant unattended approvals")
	}
	// Only the tool identity crosses this call. The allowlist it is checked
	// against is re-read from storage on the other side, so this path cannot
	// be the one that decides what an automation may do.
	return approver.GrantUnattendedApproval(ctx, approvalID, beaconServerName(beacon), beacon.ToolName)
}

// blockUnattendedTool is what happens when an automation reaches a tool it was
// not pre-approved for.
//
// The alternative — creating the approval and letting it sit — is the failure
// mode this whole feature exists to avoid: a run waiting on a person who is
// asleep, silent until the approval TTL quietly expires. So the tool call is
// denied and the run is failed with a reason naming the automation and the
// tool, which is what the client renders and what the automation's last-run
// status reports.
func (s *Server) blockUnattendedTool(ctx context.Context, beacon *turingv1.ToolCallBeacon, run repository.Run, argsJSON string, argsHash string, grant repository.AutomationRunGrant) (*turingv1.ToolPolicyDecision, error) {
	decision, err := s.denyToolBefore(ctx, beacon, run, argsJSON, argsHash, AutomationNotAllowlistedCode)
	if err != nil {
		return nil, err
	}
	if _, err := s.audit.RecordForExistingRun(ctx, beacon.RunId, "automation", grant.AutomationID, "automation.tool.blocked", beacon.ToolCallId, map[string]any{
		"toolName":       beacon.ToolName,
		"serverName":     beaconServerName(beacon),
		"automationId":   grant.AutomationID,
		"automationName": grant.AutomationName,
	}); err != nil {
		log.Printf("record blocked automation tool audit for %s: %v", beacon.ToolCallId, err)
	}
	message := fmt.Sprintf(
		"%q needs approval to run %s, and that tool is not on this automation's allowlist. Nobody was asked, because an automation runs unattended; the run stopped here instead of waiting.",
		grant.AutomationName, beacon.ToolName)
	// The decision is discarded rather than merged: on a replayed beacon
	// denyToolBefore reports why the tool call is ALREADY terminal
	// ("tool_call_already_completed"), and letting that overwrite the reason
	// here would make the runtime's terminal error disagree with the run's
	// stored error_code.
	_ = decision
	return s.failUnattendedRun(ctx, beacon, AutomationNotAllowlistedCode, message)
}

// failUnattendedRun terminalises a run nobody is watching, and returns the
// decision that tells the runtime to stop rather than keep calling tools.
func (s *Server) failUnattendedRun(ctx context.Context, beacon *turingv1.ToolCallBeacon, code string, message string) (*turingv1.ToolPolicyDecision, error) {
	payloadJSON, err := encodePayload(map[string]any{
		"runId": beacon.GetRunId(), "code": code, "message": message, "retryable": false,
	})
	if err != nil {
		return nil, err
	}
	events, err := s.repo.FailRunWithEventPreservingExecution(ctx, beacon.GetRunId(), code, message, payloadJSON)
	if err != nil {
		// The run is already terminal (cancelled, or failed by another path).
		// Telling the runtime to stop is still right; failing the RPC would
		// only leave it waiting.
		if errors.Is(err, repository.ErrRunNotFailable) {
			return terminalRunDecision(beacon, code), nil
		}
		return nil, err
	}
	for _, event := range events {
		s.publishEvent(event)
	}
	return terminalRunDecision(beacon, code), nil
}

func (s *Server) terminalizePostCommitApprovalFailure(_ context.Context, runID string, toolCallID string) error {
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	approval, err := s.repo.GetApprovalByToolCall(recoveryCtx, runID, toolCallID)
	if errors.Is(err, sql.ErrNoRows) {
		payloadJSON, payloadErr := encodePayload(map[string]any{
			"runId": runID, "code": "approval_delivery_failed", "message": "Approval lifecycle event could not be recorded", "retryable": false,
		})
		if payloadErr != nil {
			return payloadErr
		}
		events, failErr := s.repo.FailRunWithEventPreservingExecution(recoveryCtx, runID, "approval_delivery_failed", "Approval lifecycle event could not be recorded", payloadJSON)
		if failErr != nil {
			return failErr
		}
		for _, event := range events {
			s.publishEvent(event)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("find approval after creation failure: %w", err)
	}
	return s.terminalizeApprovalDeliveryFailure(recoveryCtx, approval.ApprovalID, nil)
}

func toolStartedEventInput(beacon *turingv1.ToolCallBeacon, run repository.Run) (repository.ToolCallBeforeEvent, error) {
	payload := map[string]any{
		"toolCallId": beacon.ToolCallId,
		"serverName": beaconServerName(beacon),
		"toolName":   beacon.ToolName,
	}
	payloadJSON, err := safejson.MarshalCanonical(payload)
	if err != nil {
		return repository.ToolCallBeforeEvent{}, err
	}
	return repository.ToolCallBeforeEvent{
		SessionID: run.SessionID, TraceID: run.TraceID, Type: "tool.call.started", PayloadJSON: string(payloadJSON),
	}, nil
}

func (s *Server) denyToolBefore(ctx context.Context, beacon *turingv1.ToolCallBeacon, run repository.Run, argsJSON string, argsHash string, reason string) (*turingv1.ToolPolicyDecision, error) {
	deniedPayload, err := safejson.MarshalCanonical(map[string]any{
		"toolCallId": beacon.ToolCallId,
		"serverName": beaconServerName(beacon),
		"toolName":   beacon.ToolName,
		"error":      reason,
	})
	if err != nil {
		return nil, err
	}
	recorded, event, err := s.repo.RecordToolCallBeforeWithEvent(
		ctx,
		repository.ToolCallRecord{
			ToolCallID: beacon.ToolCallId, RunID: beacon.RunId, Status: "denied",
			ModelToolCallID: beacon.ModelToolCallId,
		},
		"general_assistant", beaconServerName(beacon), beacon.ToolName, argsJSON, argsHash,
		repository.ToolCallBeforeEvent{
			SessionID: run.SessionID, TraceID: run.TraceID,
			Type: "tool.call.denied", PayloadJSON: string(deniedPayload),
		},
	)
	if err != nil {
		return nil, mapToolCallError(err)
	}
	if event.EventID != "" {
		s.publishEvent(event)
	}
	if !recorded.Inserted {
		if decision, handled := existingToolBeforeDecision(recorded.Record, reason); handled {
			return decision, nil
		}
	}
	payload := toolAuditPayload(beacon)
	payload["reason"] = reason
	if _, err := s.audit.RecordForExistingRun(ctx, beacon.RunId, "runtime", "", "tool.call.before", beacon.ToolCallId, payload); err != nil {
		log.Printf("record denied tool before audit for %s: %v", beacon.ToolCallId, err)
	}
	return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_DENY, ToolCallId: beacon.ToolCallId, Reason: reason}, nil
}

func existingToolBeforeDecision(record repository.ToolCallRecord, deniedReason string) (*turingv1.ToolPolicyDecision, bool) {
	decision := &turingv1.ToolPolicyDecision{ToolCallId: record.ToolCallID}
	switch record.Status {
	case "allowed":
		decision.Decision = turingv1.ToolPolicyDecision_DECISION_ALLOW
	case "approval_required":
		decision.Decision = turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED
		decision.ApprovalId = record.ApprovalID
	case "denied":
		decision.Decision = turingv1.ToolPolicyDecision_DECISION_DENY
		decision.Reason = deniedReason
	case "completed":
		decision.Decision = turingv1.ToolPolicyDecision_DECISION_DENY
		decision.Reason = "tool_call_already_completed"
	case "failed":
		decision.Decision = turingv1.ToolPolicyDecision_DECISION_DENY
		decision.Reason = "tool_call_already_failed"
	default:
		return nil, false
	}
	return decision, true
}

func (s *Server) handleToolAfter(ctx context.Context, beacon *turingv1.ToolCallBeacon, run repository.Run, workerID string) (*turingv1.ToolPolicyDecision, error) {
	statusValue, eventType, err := toolAfterStatus(beacon)
	if err != nil {
		return nil, err
	}
	_, argsHash, err := canonicalArgs(beaconArgs(beacon))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "tool args are not valid JSON")
	}
	errorCode, errorMessage := "", ""
	if beacon.Error != nil {
		errorCode = beacon.Error.Code
		errorMessage = beacon.Error.Message
	}
	payload := map[string]any{
		"toolCallId": beacon.ToolCallId,
		"serverName": beaconServerName(beacon),
		"toolName":   beacon.ToolName,
	}
	if errorText := publicToolEventError(statusValue, beacon.Error); errorText != "" {
		payload["error"] = errorText
	}
	payloadJSON, err := safejson.MarshalCanonical(payload)
	if err != nil {
		return nil, err
	}
	changed, event, err := s.repo.RecordToolCallAfterWithEvent(ctx, repository.ToolCallAfterRecord{
		ToolCallID:      beacon.ToolCallId,
		RunID:           beacon.RunId,
		ServerName:      beaconServerName(beacon),
		ToolName:        beacon.ToolName,
		ModelToolCallID: beacon.ModelToolCallId,
		ArgsHash:        argsHash,
		WorkerID:        workerID,
		Status:          statusValue,
		ResultSummary:   beacon.ResultSummary,
		ErrorCode:       errorCode,
		ErrorMessage:    errorMessage,
		DurationMS:      beacon.DurationMs,
	}, eventType, string(payloadJSON))
	if err != nil {
		return nil, mapToolCallError(err)
	}
	if !changed {
		return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: beacon.ToolCallId}, nil
	}
	s.publishEvent(event)
	if _, err := s.audit.RecordForExistingRun(ctx, beacon.RunId, "runtime", "", "tool.call.after", beacon.ToolCallId, toolAuditPayload(beacon)); err != nil {
		log.Printf("record tool after audit for %s: %v", beacon.ToolCallId, err)
	}
	return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: beacon.ToolCallId}, nil
}

func (s *Server) NotifyApprovalUpdated(ctx context.Context, runID string, approvalID string, approvalStatus string, approvalToken string) error {
	owner := s.workerForRun(runID)
	if owner == nil {
		if approvalStatus == "denied" || approvalStatus == "expired" {
			s.releaseUnownedTerminalRun(ctx, runID)
		}
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
	return nil
}

func mapToolCallError(err error) error {
	switch {
	case errors.Is(err, repository.ErrToolCallConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, repository.ErrToolCallNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, repository.ErrToolCallInvalidTransition):
		return status.Error(codes.FailedPrecondition, err.Error())
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

func publicToolEventError(statusValue string, toolError *turingv1.ToolCallError) string {
	if toolError != nil {
		if toolError.Message != "" {
			return toolError.Message
		}
		if toolError.Code != "" {
			return toolError.Code
		}
	}
	switch statusValue {
	case "failed":
		return "Tool call failed"
	case "denied":
		return "Tool call denied"
	default:
		return ""
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
	addModelToolCallID(payload, beacon)
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

func addModelToolCallID(payload map[string]any, beacon *turingv1.ToolCallBeacon) {
	if beacon.GetModelToolCallId() != "" {
		payload["modelToolCallId"] = beacon.GetModelToolCallId()
	}
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
	agentID := turingv1.AgentId_AGENT_ID_UNSPECIFIED
	if job.AgentID == "general_assistant" {
		agentID = turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT
	}
	return &turingv1.AgentJob{
		JobId:                          job.JobID,
		RunId:                          job.RunID,
		SessionId:                      job.SessionID,
		UserMessageId:                  job.UserMessageID,
		AssistantMessageId:             job.AssistantMessageID,
		AgentId:                        agentID,
		TraceId:                        job.TraceID,
		ModelProvider:                  provider,
		Model:                          job.Model,
		UserText:                       job.UserText,
		RequestedTools:                 append([]string(nil), job.RequestedTools...),
		Attempt:                        int32(job.Attempt),
		Skills:                         toProtoSkills(job.Skills),
		ExternalAgent:                  toProtoExternalAgent(job.ExternalAgent),
		RequiredContextTokens:          int32(job.RequiredContextTokens),
		MinimumWorkerMaxConcurrentRuns: int32(job.MinimumWorkerMaxConcurrentRuns),
	}
}

// toProtoExternalAgent keeps nil as nil. An empty target would look like a
// routed run with no endpoint, which is a worse failure than an unrouted one:
// the runtime would try to reach "" instead of just running locally.
func toProtoExternalAgent(target *repository.ExternalAgentTarget) *turingv1.ExternalAgentTarget {
	if target == nil {
		return nil
	}
	return &turingv1.ExternalAgentTarget{
		DisplayName:   target.DisplayName,
		BaseUrl:       target.BaseURL,
		CredentialRef: target.CredentialRef,
	}
}

func toProtoSkills(skills []repository.SkillSnapshot) []*turingv1.SkillSnapshot {
	if len(skills) == 0 {
		return nil
	}
	converted := make([]*turingv1.SkillSnapshot, 0, len(skills))
	for _, skill := range skills {
		body := skill.Body
		if body == "" {
			body = skill.Instructions
		}
		converted = append(converted, &turingv1.SkillSnapshot{
			Name:                skill.Name,
			Instructions:        body,
			SkillId:             skill.SkillID,
			Description:         skill.Description,
			Category:            skill.Category,
			References:          cloneStringMap(skill.References),
			Withheld:            skill.Withheld,
			MissingCapabilities: append([]string(nil), skill.MissingCapabilities...),
		})
	}
	return converted
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
