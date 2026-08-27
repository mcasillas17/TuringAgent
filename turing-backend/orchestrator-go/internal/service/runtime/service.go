package runtime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
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
	"google.golang.org/protobuf/types/known/timestamppb"
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
	registryMu         sync.RWMutex
	dispatch           DispatchConfig
	// afterAssignmentSend is a test seam fired between a successful RunAssigned
	// send and its delivered bookkeeping. The window between the two is where a
	// concurrent fence lands, and nothing else can hold a test open inside it.
	afterAssignmentSend func(repository.Assignment)
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
	commands       chan workerCommand
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
	// job is the exact assignment this worker was handed. It is retained so a
	// same-attempt refresh can carry the committed version forward without
	// rebuilding a partial job the worker would have to guess at.
	job *turingv1.AgentJob
}

// workerCommand is one command queued for a worker, plus whether it is a
// same-attempt assignment refresh.
//
// A refresh reuses the RunAssigned shape because that is the only command the
// contract has for carrying an assignment's version, but it is not a dispatch:
// the worker already owns this attempt, the delivery bookkeeping already ran,
// and running it again would abort a live assignment. The flag is set only by
// the ownership-proof path in this package, never derived from the command.
type workerCommand struct {
	command *turingv1.RuntimeCommand
	refresh bool
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
	s.registryMu.RLock()
	registryLocked := true
	defer func() {
		if registryLocked {
			s.registryMu.RUnlock()
		}
	}()
	capabilities, discovered, err = s.filterRegisteredWorkerTools(ctx, capabilities, discovered)
	if err != nil {
		return status.Error(codes.Internal, "filter worker tool capabilities")
	}
	s.refreshPendingCapabilityStateAdvisory(ctx, "registry seed", "", false, false)
	commands := make(chan workerCommand, maxWorkerConcurrentRuns)
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
	s.registryMu.RUnlock()
	registryLocked = false
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
				_, release, beginErr := connectedWorker.beginUpdate(update)
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
			if resumeReady := update.GetApprovalResumeReady(); resumeReady != nil {
				_, release, beginErr := connectedWorker.beginUpdate(update)
				if beginErr != nil {
					recvErr <- approvalResumeGateError(beginErr)
					return
				}
				accepted, resumeErr := s.resumeApprovedRun(ctx, resumeReady, ready.WorkerId, connectedWorker)
				// Released before the acceptance goes out: the command loop
				// takes this same lock to deliver an assignment and then takes
				// the sender, so holding it across a send would order the two
				// the other way round.
				release()
				if resumeErr != nil {
					recvErr <- resumeErr
					return
				}
				if err := s.deliverApprovalResumeAcceptance(ctx, stream, accepted, ready.WorkerId, connectedWorker); err != nil {
					recvErr <- err
					return
				}
				continue
			}
			owned, release, err := connectedWorker.beginUpdate(update)
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
				suppressDispatch, err := s.applyUpdateForWorker(ctx, update, ready.WorkerId, owned)
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
				return errors.Join(err, s.handleUndeliveredCommand(ctx, cmd.command, ready.WorkerId, connectedWorker))
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

func (s *Server) filterRegisteredWorkerTools(
	ctx context.Context,
	capabilities *registeredWorkerCapabilities,
	discovered []repository.DiscoveredTool,
) (*registeredWorkerCapabilities, []repository.DiscoveredTool, error) {
	filteredCapabilities := cloneRegisteredWorkerCapabilities(capabilities)
	filtered := make([]repository.DiscoveredTool, 0, len(discovered))
	for _, tool := range discovered {
		var available bool
		var err error
		if repository.IsPseudoServerName(tool.ServerName) {
			available, err = s.repo.PseudoServerToolAvailable(ctx, tool.ServerName, tool.ToolName)
		} else {
			available, err = s.repo.MCPToolAvailable(ctx, tool.ServerName, tool.ToolName)
		}
		if err != nil {
			return nil, nil, err
		}
		if available {
			filtered = append(filtered, tool)
			continue
		}
		delete(filteredCapabilities.tools, tool.ServerName+"/"+tool.ToolName)
	}
	return filteredCapabilities, filtered, nil
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
	refreshes, reconciled, err := s.reconcileHeartbeatState(ctx, workerID, connectedWorker)
	if err != nil {
		return err
	}
	// Queueing happens with the update lock released. The command buffer is
	// bounded, and the only goroutine that drains it needs that same lock to
	// deliver an assignment: queueing while holding it lets a full buffer close
	// a cycle neither side can leave, on a stream context that never expires.
	//
	// The proofs are already committed at this point, so a refresh that cannot
	// be delivered still fails this heartbeat, and the recovering truth stays
	// durable through the ordinary reconciliation fence when the stream tears
	// down. Order is preserved: the refreshes go out in the order the proofs
	// were committed.
	for _, refresh := range refreshes {
		if err := connectedWorker.sendAssignmentRefresh(ctx, refresh); err != nil {
			return err
		}
	}
	if reconciled {
		return s.DispatchPending(ctx)
	}
	return nil
}

// reconcileHeartbeatState does everything a heartbeat decides, under the update
// lock, and hands back the refreshes that still have to reach the worker.
//
// It returns the refresh commands rather than sending them because sending can
// block, and nothing that can block may run while this lock is held.
func (s *Server) reconcileHeartbeatState(
	ctx context.Context,
	workerID string,
	connectedWorker *worker,
) ([]*turingv1.RuntimeCommand, bool, error) {
	connectedWorker.updateMu.Lock()
	defer connectedWorker.updateMu.Unlock()
	// A run this worker still owns may have been fenced into recovering while
	// its ownership was in doubt. The heartbeat resolves that doubt, so the
	// proof runs before recovery: recovering a run whose owner is demonstrably
	// alive would throw away work nobody needed to lose.
	refreshes, err := s.proveOwnedRecoveringAssignments(ctx, workerID, connectedWorker)
	if err != nil {
		return nil, false, err
	}
	assignments := connectedWorker.assignmentSnapshot(workerID)
	renewed, err := s.repo.RenewAssignments(ctx, assignments, time.Now().UTC().Add(s.dispatch.LeaseDuration))
	if err != nil {
		return nil, false, err
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
			return nil, false, err
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
	return refreshes, reconciled || revived, nil
}

// proveOwnedRecoveringAssignments commits recovering -> running for every
// assignment this worker can still prove it owns, and collects the same-attempt
// RunAssigned refresh that hands each proof back.
//
// The refresh is what makes the transition legitimate. A lifecycle change the
// worker never learns about is the defect the version fence exists to remove:
// the worker would keep reporting against the state it last saw, and every one
// of those reports would be refused. So the proof and the reply belong to one
// step — but only the proof happens here. Queueing a refresh can block on the
// worker's bounded command buffer, and this runs under the update lock the
// command loop needs to drain that buffer, so the caller sends them once the
// lock is released. A refresh that cannot be delivered still fails the
// heartbeat, and the recovering truth stays in place through the ordinary
// reconciliation fence when the stream tears down.
func (s *Server) proveOwnedRecoveringAssignments(ctx context.Context, workerID string, connectedWorker *worker) ([]*turingv1.RuntimeCommand, error) {
	var refreshes []*turingv1.RuntimeCommand
	for _, held := range connectedWorker.assignmentEntries() {
		if held.job == nil {
			continue
		}
		version, proven, err := s.proveRecoveringOwnership(ctx, held.runID, workerID, held.attemptID)
		if err != nil {
			return nil, err
		}
		if !proven {
			continue
		}
		refresh := proto.Clone(held.job).(*turingv1.AgentJob)
		refresh.ExpectedStateVersion = version
		refreshes = append(refreshes, &turingv1.RuntimeCommand{
			Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: refresh},
		})
	}
	return refreshes, nil
}

// proveRecoveringOwnership returns the version a recovering run commits when
// the named attempt proves it still owns it, and whether anything moved.
//
// Ownership is proved by the guarded transition, not by this function: worker
// and assignment attempt are the transition's identity, so an attempt that no
// longer owns the row loses the update rather than being trusted here.
func (s *Server) proveRecoveringOwnership(ctx context.Context, runID string, workerID string, attemptID string) (int64, bool, error) {
	if runID == "" || workerID == "" || attemptID == "" {
		return 0, false, nil
	}
	state, err := s.repo.GetRunState(ctx, runID)
	if err != nil {
		return 0, false, err
	}
	if state.Lifecycle != recoveringRunStatus {
		return 0, false, nil
	}
	result, err := s.repo.ResumeRecoveringRun(ctx, repository.ResumeRecoveringRunInput{
		RunID:                runID,
		ExpectedStateVersion: state.StateVersion,
		WorkerID:             workerID,
		AssignmentAttemptID:  attemptID,
	})
	if err != nil {
		// Losing the guarded update means something else owns this run now.
		// That is the fence working, not an error this stream should die on.
		if errors.Is(err, repository.ErrRunTransitionConflict) {
			return 0, false, nil
		}
		return 0, false, err
	}
	for _, event := range result.Events {
		s.publishEvent(event)
	}
	return result.State.StateVersion, true, nil
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
	// A resume names its run for the same reason a beacon does: the update lock
	// this feeds has to serialize the commit against the dispatch bookkeeping
	// for that exact run, and the assignment check it performs is the first
	// half of proving the stream owns what it is asking about.
	if ready := update.GetApprovalResumeReady(); ready != nil {
		return ready.GetRunId()
	}
	return terminalRunID(update)
}

// handleUndeliveredCommand answers a command that never reached its worker.
//
// Each kind owes something different. A tool-policy decision carrying an
// approval is the authorization itself, so losing it ends the approval. An
// approval decision is not: it is news about a decision already durably made,
// and losing the news does not by itself end anything.
//
// A resume acceptance never arrives here. It is handed to the worker directly
// from the Ready handler rather than queued, because the row is already
// committed running by then and the delivery has to be answered in the same
// breath — see deliverApprovalResumeAcceptance, which fences the run itself
// when its send fails.
func (s *Server) handleUndeliveredCommand(ctx context.Context, cmd *turingv1.RuntimeCommand, workerID string, owner *worker) error {
	if decision := cmd.GetToolPolicyDecision(); decision != nil && decision.GetApprovalId() != "" {
		return s.terminalizeApprovalDeliveryFailure(ctx, decision.GetApprovalId(), owner)
	}
	if updated := cmd.GetApprovalUpdated(); updated != nil {
		return s.handleUndeliveredApprovalDecision(ctx, updated, workerID, owner)
	}
	return nil
}

// approvalResumeGateError is what a Ready owes when the update gate refuses it.
//
// The gate refuses for two unrelated reasons and they must not be flattened
// into one. A run this worker does not hold is a precondition the Ready itself
// violated, and the handshake says so in its own words. A worker that is
// already disconnected is not about the Ready at all: the stream is going, its
// teardown reports that same cancellation, and relabelling it here would make
// the handler's outcome depend on which goroutine noticed first while hiding a
// cancellation callers recognise inside what looks like a protocol violation.
func approvalResumeGateError(err error) error {
	if status.Code(err) == codes.Canceled {
		return err
	}
	return status.Error(codes.FailedPrecondition, "approval resume does not name a live owned assignment")
}

// handleUndeliveredApprovalDecision answers an approval decision that never
// reached its worker.
//
// Delivering the decision is not what resumes the run — only a Ready/Accepted
// exchange is — so failing to deliver it must not move the run either. While
// this attempt still provably owns the run, the honest answer stays
// waiting-approval at the version already committed, and the worker can still
// ask again. Once ownership can no longer be proven, the run takes the same
// fence as any other lost worker: a row that goes on saying it is waiting for
// an answer nobody will act on is describing a conversation with no second
// party.
func (s *Server) handleUndeliveredApprovalDecision(
	_ context.Context,
	updated *turingv1.RuntimeApprovalUpdated,
	workerID string,
	owner *worker,
) error {
	if updated.GetApprovalId() == "" || workerID == "" {
		return nil
	}
	// The caller's context is the stream's, and the stream is exactly what has
	// just failed. Reading the run through it would report the dead connection
	// instead of deciding what the run's state should be, so this runs on its
	// own short-lived one — the same shape every other recovery path here uses.
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	approval, err := s.repo.GetApproval(recoveryCtx, updated.GetApprovalId())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	run, err := s.repo.GetRun(recoveryCtx, approval.RunID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	// A run this stream no longer owns is not this stream's to fence: some
	// other attempt is answering for it now.
	if run.WorkerID != workerID {
		return nil
	}
	if s.ownershipProven(workerID, owner, run.RunID, run.ExecutionAttemptID) {
		return nil
	}
	return s.fenceRunOwnership(run.RunID, workerID, run.ExecutionAttemptID)
}

// ownershipProven reports whether this worker still demonstrably holds the
// named attempt: it is the current registration, it has not closed, and its
// lease has not lapsed.
func (s *Server) ownershipProven(workerID string, owner *worker, runID string, attemptID string) bool {
	if owner == nil || workerID == "" || runID == "" {
		return false
	}
	s.mu.Lock()
	current := s.workers[workerID]
	s.mu.Unlock()
	if current != owner {
		return false
	}
	return owner.hasLiveAssignmentAttempt(runID, attemptID, time.Now().UTC(), s.dispatch.LeaseDuration)
}

// fenceRunOwnership is the one ownership-loss transition every caller here
// shares: an active run whose worker can no longer be reached moves to
// recovering, one version forward, with its state projection.
//
// It runs on its own short-lived context because every caller reaches it
// exactly when something has already gone wrong — a send failed, a stream is
// closing — and the context those callers hold is often the one that just died.
//
// Losing the guarded transition is not an error. It means another writer owns
// this run now, which is the fence working rather than failing.
func (s *Server) fenceRunOwnership(runID string, workerID string, attemptID string) error {
	if runID == "" || workerID == "" || attemptID == "" {
		return nil
	}
	fenceCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state, err := s.repo.GetRunState(fenceCtx, runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if !isActiveRunStatus(state.Lifecycle) {
		return nil
	}
	result, err := s.repo.FenceRunOwnership(fenceCtx, repository.FenceRunOwnershipInput{
		RunID:                runID,
		ExpectedStateVersion: state.StateVersion,
		WorkerID:             workerID,
		AssignmentAttemptID:  attemptID,
	})
	if err != nil {
		if errors.Is(err, repository.ErrRunTransitionConflict) {
			return nil
		}
		return err
	}
	for _, event := range result.Events {
		s.publishEvent(event)
	}
	return nil
}

// resumeApprovedRun commits one approval resume, or refuses it.
//
// It writes nothing but the transition itself. A refusal deliberately does not
// fence: the Ready handler's job is to decide whether this exact trigger may
// move the run, and a stream that asked for something it cannot have is fenced
// by its own teardown, under the cause that is actually true — lost ownership,
// not a resume that happened.
//
// The worker ID is the server-authenticated one from the stream rather than
// anything the message carried, so a worker cannot name somebody else.
func (s *Server) resumeApprovedRun(
	ctx context.Context,
	ready *turingv1.RuntimeApprovalResumeReady,
	workerID string,
	connectedWorker *worker,
) (*turingv1.RuntimeApprovalResumeAccepted, error) {
	if ready == nil || ready.GetRunId() == "" || ready.GetApprovalId() == "" ||
		ready.GetAssignmentAttemptId() == "" || ready.GetExpectedStateVersion() < 1 {
		return nil, status.Error(codes.FailedPrecondition, "approval resume is incomplete")
	}
	if !s.ownershipProven(workerID, connectedWorker, ready.GetRunId(), ready.GetAssignmentAttemptId()) {
		return nil, status.Error(codes.FailedPrecondition, "approval resume does not name a live owned assignment")
	}
	result, err := s.repo.ResumeApprovedRun(ctx, repository.ResumeApprovedRunInput{
		RunID:                ready.GetRunId(),
		ApprovalID:           ready.GetApprovalId(),
		WorkerID:             workerID,
		AssignmentAttemptID:  ready.GetAssignmentAttemptId(),
		ExpectedStateVersion: ready.GetExpectedStateVersion(),
	})
	if err != nil {
		if errors.Is(err, repository.ErrRunTransitionConflict) ||
			errors.Is(err, repository.ErrRunTransitionUnsupported) ||
			errors.Is(err, repository.ErrRunStateVersionInvalid) ||
			errors.Is(err, repository.ErrRunStateVersionExhausted) ||
			errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.FailedPrecondition, "approval resume was fenced")
		}
		return nil, err
	}
	for _, event := range result.Events {
		s.publishEvent(event)
	}
	// Reconstructed from what the transition durably holds, so a replay of the
	// same Ready produces the identical acceptance rather than a second one.
	return &turingv1.RuntimeApprovalResumeAccepted{
		RunId:               ready.GetRunId(),
		ApprovalId:          ready.GetApprovalId(),
		StateVersion:        result.State.StateVersion,
		AssignmentAttemptId: ready.GetAssignmentAttemptId(),
	}, nil
}

// deliverApprovalResumeAcceptance hands the worker the acceptance the commit
// already made.
//
// The row cannot go back. Waiting-approval was left the moment the transition
// committed, and claiming it again would tell every reader the user's decision
// is still outstanding. So a failed delivery takes the ownership fence instead,
// and the stream fails with it.
func (s *Server) deliverApprovalResumeAcceptance(
	ctx context.Context,
	stream turingv1.RuntimeService_ConnectWorkerServer,
	accepted *turingv1.RuntimeApprovalResumeAccepted,
	workerID string,
	connectedWorker *worker,
) error {
	sendCtx, cancel := withDefaultTimeout(ctx, commandSendTimeout)
	defer cancel()
	err := connectedWorker.commandSender(stream).send(sendCtx, &turingv1.RuntimeCommand{
		Command: &turingv1.RuntimeCommand_ApprovalResumeAccepted{ApprovalResumeAccepted: accepted},
	})
	if err == nil {
		return nil
	}
	return errors.Join(err, s.fenceRunOwnership(accepted.GetRunId(), workerID, accepted.GetAssignmentAttemptId()))
}

func (s *Server) sendCommand(ctx context.Context, stream turingv1.RuntimeService_ConnectWorkerServer, queued workerCommand, connectedWorker *worker, workerID string) error {
	cmd := queued.command
	if cmd == nil {
		return status.Error(codes.Canceled, "worker command queue closed")
	}
	sendCtx, cancel := withDefaultTimeout(ctx, commandSendTimeout)
	defer cancel()
	assigned := cmd.GetRunAssigned()
	// A refresh names an assignment this worker already holds and already had
	// delivered. Re-running the claim bookkeeping below would abort a live
	// assignment, so it goes straight out.
	if assigned == nil || queued.refresh {
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
		if err := s.releaseClaimedAssignment(currentAssignment); err != nil {
			return err
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
			if err := s.acknowledgeFencedExecutionExit(assigned.RunId); err != nil {
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
	if s.afterAssignmentSend != nil {
		s.afterAssignmentSend(repositoryAssignment)
	}
	return s.finishAssignmentDelivery(ctx, repositoryAssignment)
}

// finishAssignmentDelivery commits the delivered bookkeeping for an assignment
// whose send already succeeded.
//
// A fence here is not a stream error. The run can be fenced into recovering
// between the send and this write — a reconciliation that lost this worker's
// heartbeat, or anything else that put its ownership in doubt — and the guarded
// UPDATE losing its row is that fence working. The worker holds the job it was
// just sent, and that job is exactly the proof vehicle: its next heartbeat or
// beacon resolves the doubt through the ordinary ownership-proof path — and
// ResumeRecoveringRun performs the very write skipped here, committing
// execution_state = 'delivered' as part of the proof. Tearing
// the stream down instead cost the worker every other assignment it was
// holding, for a write whose only claim was bookkeeping — the same defect the
// dropped-command comment in sendCommand records for a release that landed
// mid-dispatch.
func (s *Server) finishAssignmentDelivery(ctx context.Context, repositoryAssignment repository.Assignment) error {
	err := s.repo.MarkAssignmentDelivered(ctx, repositoryAssignment)
	if err == nil || errors.Is(err, repository.ErrAssignmentFenced) {
		return nil
	}
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.repo.MarkAssignmentDeliveryUncertain(recoveryCtx, repositoryAssignment)
	return err
}

func (s *Server) acknowledgeFencedExecutionExit(runID string) error {
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

func (s *Server) releaseClaimedAssignment(claimed assignment) error {
	err := s.requeueAssignments([]assignment{claimed})
	if !errors.Is(err, repository.ErrAssignmentFenced) {
		return err
	}
	return s.acknowledgeFencedExecutionExit(claimed.runID)
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
	for _, candidate := range assignments {
		if s.hasLiveAssignment(candidate) {
			continue
		}
		if connected := s.registeredWorker(candidate.WorkerID); connected != nil {
			closedAssignments, closed, live := connected.closeForStaleAssignment(
				candidate.RunID, candidate.AttemptID, cutoff, s.dispatch.LeaseDuration,
			)
			if live {
				continue
			}
			if closed {
				s.removeWorkerRegistration(candidate.WorkerID, connected)
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
	}
	s.refreshPendingCapabilityStateAdvisory(recoveryCtx, "worker heartbeat expired", "", true, false)
	return s.DispatchPending(recoveryCtx)
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
	assignedJob := mapJob(job)
	claimedAssignment := assignment{jobID: job.JobID, runID: job.RunID, attemptID: job.AssignmentAttemptID, job: assignedJob}
	if worker.closed ||
		worker.lastHeartbeat.IsZero() ||
		!time.Now().UTC().Before(worker.lastHeartbeat.Add(s.dispatch.LeaseDuration)) ||
		len(worker.assignments) >= worker.maxConcurrent ||
		!workerCapabilitiesSupportRoute(worker.capabilities, routingRequirementsForJob(job)) {
		worker.mu.Unlock()
		if err := s.releaseClaimedAssignment(claimedAssignment); err != nil {
			return false, false, false, err
		}
		return false, false, true, nil
	}
	worker.assignments[job.RunID] = claimedAssignment
	select {
	case worker.commands <- workerCommand{command: &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: assignedJob}}}:
		worker.mu.Unlock()
		s.publishEvent(job.StartedEvent)
		return true, false, false, nil
	case <-ctx.Done():
		delete(worker.assignments, job.RunID)
		worker.mu.Unlock()
		return false, false, false, errors.Join(ctx.Err(), s.releaseClaimedAssignment(claimedAssignment))
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
		RemoteEgressDecisionVersion: capabilities.remoteEgressDecisionVersion,
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
		SelectedTools:                  job.SelectedTools,
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
		SelectedTools:                  append([]string(nil), job.GetSelectedTools()...),
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
	command := &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunCancelled{RunCancelled: &turingv1.RuntimeRunCancelled{
		RunId: runID, Reason: reason, StateVersion: s.committedVersion(ctx, runID, 0),
	}}}
	_ = owner.send(sendCtx, command)
}

// CancelSessionRuns sends the existing cancellation command to every still
// executing run owned by a deleting session. The repository has already fenced
// its durable state; this command asks the worker to acknowledge execution
// exit so finalization can proceed.
func (s *Server) CancelSessionRuns(ctx context.Context, sessionID string, reason string) {
	runIDs, err := s.repo.SessionExecutionRunIDs(ctx, sessionID)
	if err != nil {
		return
	}
	for _, runID := range runIDs {
		s.CancelRun(ctx, runID, reason)
	}
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
	return w.enqueue(ctx, workerCommand{command: command})
}

// sendAssignmentRefresh queues a same-attempt refresh. It is separate from send
// so the refresh flag cannot be set by anything that merely holds a command.
func (w *worker) sendAssignmentRefresh(ctx context.Context, command *turingv1.RuntimeCommand) error {
	return w.enqueue(ctx, workerCommand{command: command, refresh: true})
}

// enqueue queues one command for the command loop to deliver.
//
// A buffer with room is filled under the worker lock, exactly as it always was:
// closing the worker needs that lock too, so a command cannot be queued into a
// registration that closed in between, and a caller that terminalizes on an
// undelivered command still gets told.
//
// Only a full buffer waits, and that wait happens with the lock released.
// Holding it across the wait closed a cycle: the command loop takes this same
// lock to deliver an assignment, so a full buffer left the queueing side
// waiting for space that only the loop could make and the loop waiting for a
// lock only the queueing side could release. Closing the worker needs the lock
// too, so even the teardown that should have broken the tie could not run.
//
// While waiting, a close is observed through the done channel. Nothing drains
// the buffer once the command loop has exited, so a worker that closes mid-wait
// reports disconnection rather than waiting out the caller's context.
func (w *worker) enqueue(ctx context.Context, queued workerCommand) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return status.Error(codes.Canceled, "worker is disconnected")
	}
	commands := w.commands
	done := w.done
	select {
	case commands <- queued:
		w.mu.Unlock()
		return nil
	default:
	}
	w.mu.Unlock()
	select {
	case commands <- queued:
		return nil
	case <-done:
		return status.Error(codes.Canceled, "worker is disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// beginUpdate gates one update on the worker still being connected and still
// owning the run the update is about, and returns the assignment it checked.
//
// updateMu serializes updates on this stream, so two updates from the same
// worker do not interleave. The assignment itself is read under w.mu, and that
// snapshot is advisory: beginUpdate releases w.mu before returning, and a
// concurrent dispatch installs assignments under w.mu without taking updateMu,
// so the assignment can be replaced after the gate has read it even while this
// update is still serialized. The returned value may therefore already be
// superseded by the time a downstream write runs.
//
// Returning the snapshot rather than leaving callers to look it up again is
// what lets a downstream write name the attempt this update claims to belong
// to. Correctness does not rest on that name being current: the repository
// re-reads the run inside its transaction and fences on worker, assignment
// attempt, and expected state version, so a superseded snapshot is rejected
// there rather than applied.
func (w *worker) beginUpdate(update *turingv1.RuntimeUpdate) (assignment, func(), error) {
	runID := updateRunID(update)
	w.updateMu.Lock()
	if runID == "" {
		w.mu.Lock()
		closed := w.closed
		w.mu.Unlock()
		if closed {
			w.updateMu.Unlock()
			return assignment{}, nil, status.Error(codes.Canceled, "worker is disconnected")
		}
		return assignment{}, w.updateMu.Unlock, nil
	}
	w.mu.Lock()
	owned, assigned := w.assignments[runID]
	closed := w.closed
	w.mu.Unlock()
	if closed {
		w.updateMu.Unlock()
		return assignment{}, nil, status.Error(codes.Canceled, "worker is disconnected")
	}
	if !assigned {
		if beacon := update.GetToolBeacon(); beacon != nil && beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
			return assignment{}, w.updateMu.Unlock, nil
		}
		w.updateMu.Unlock()
		return assignment{}, nil, status.Error(codes.PermissionDenied, "run is not assigned to worker")
	}
	return owned, w.updateMu.Unlock, nil
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

// assignmentEntries snapshots the assignments this worker holds, including the
// exact job each was handed. The ownership proof needs the job itself, which
// the repository-shaped snapshot deliberately does not carry.
func (w *worker) assignmentEntries() []assignment {
	w.mu.Lock()
	defer w.mu.Unlock()
	entries := make([]assignment, 0, len(w.assignments))
	for _, held := range w.assignments {
		entries = append(entries, held)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].runID < entries[j].runID })
	return entries
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
		// applyUpdate has no authenticated stream behind it, so it can name no
		// owner: a release it cannot attribute is an uncertain one.
		_, err := s.handleRunFailed(ctx, value.RunFailed, runReleaseOwner{})
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
//
// workerID and owned are the identity beginUpdate already established: the
// authenticated worker on this stream, and the assignment it still holds for
// this run. They are carried down rather than rediscovered because a retryable
// failure is the one report that releases a run without ending it, and the
// repository can only commit that release directly if somebody proves who was
// holding it.
func (s *Server) applyUpdateForWorker(
	ctx context.Context,
	update *turingv1.RuntimeUpdate,
	workerID string,
	owned assignment,
) (suppressDispatch bool, err error) {
	if failed := update.GetRunFailed(); failed != nil {
		return s.handleRunFailed(ctx, failed, releasedBy(workerID, owned))
	}
	return false, s.applyUpdate(ctx, update)
}

// runReleaseOwner is the proven holder of a run at the moment it was released.
// Both halves are present or neither is: half an identity proves nothing, and
// the repository refuses it rather than treating it as an anonymous report.
type runReleaseOwner struct {
	workerID  string
	attemptID string
}

// releasedBy pairs the authenticated worker with the attempt it was gated on.
// An update that arrived without a live assignment — a late report, or an
// internal caller with no stream behind it — yields no owner at all, which is
// the honest input for a release nobody can vouch for.
func releasedBy(workerID string, owned assignment) runReleaseOwner {
	if workerID == "" || owned.attemptID == "" {
		return runReleaseOwner{}
	}
	return runReleaseOwner{workerID: workerID, attemptID: owned.attemptID}
}

func (s *Server) normalizeRuntimeEvent(ctx context.Context, event *turingv1.TuringEvent) (*turingv1.TuringEvent, error) {
	if event == nil || event.RunId == "" {
		return nil, status.Error(codes.InvalidArgument, "runtime event run_id is required")
	}
	if !isKnownRuntimeEventType(event.Type) {
		return nil, status.Error(codes.InvalidArgument, "runtime event type is invalid")
	}
	switch event.Type {
	case turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_QUEUED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STARTED:
		return nil, status.Error(codes.InvalidArgument, "run lifecycle projections are repository-authored")
	case turingv1.TuringEventType_TURING_EVENT_TYPE_SYSTEM,
		turingv1.TuringEventType_TURING_EVENT_TYPE_ERROR:
		// Neither type has a dedicated writer anywhere in this product: every
		// real condition already has a typed, bounded event of its own — a
		// worker's own narration is agent.run.step, a tool outcome is
		// tool.call.failed, a terminal run outcome is agent.run.failed. A
		// generic SYSTEM or ERROR event exists to carry whatever free-form
		// shape a caller invents — message, error, stack, a token, tool args,
		// a tool result, a path — and unlike TOOL_CALL_STARTED/
		// TOOL_CALL_FAILED below, there is no bounded identity to normalize
		// it into: the type itself promises nothing a client should trust.
		// So both are refused before the run is even read, and before
		// anything is persisted.
		return nil, status.Error(codes.InvalidArgument, "system and error events have no dedicated writer on the generic channel")
	case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_APPROVED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_EXPIRED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_CONSUMED:
		// Tool call completion/denial and every approval outcome are settled
		// by their own guarded flows — handleToolBeacon for a tool call, the
		// approval service for a decision — each of which proves the run it
		// acts on and writes a payload that flow controls. The generic
		// channel is a worker narrating its own run; it proves nothing about
		// a tool policy decision or an approval outcome. Accepting one of
		// these types here would let a worker's own payload — a token, tool
		// args, a tool result, a path, free-form prose — stand in for a
		// decision only the dedicated flow may author, republished by every
		// public reader that trusts the type to say what it claims.
		//
		// TOOL_CALL_STARTED and TOOL_CALL_FAILED are deliberately not refused
		// here: agent-runtime's emitAssistantToolCallFailed is a legitimate
		// producer of both on this same generic channel, reporting a tool
		// failure that happens before any beacon exists (an unknown tool, no
		// tool runner, or a non-beacon execution error). Refusing them
		// outright would tear down the whole AgentStream over that
		// legitimate report, so instead their payload is rebuilt onto a
		// bounded safe shape further down rather than accepted verbatim.
		return nil, status.Error(codes.InvalidArgument, "tool call and approval events are authored by dedicated beacon and approval flows")
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
	// The worker narrates its run; it does not decide canonical state or
	// repository-owned retry projections. Drop both before anything durable
	// exists rather than leaving every reader to defend itself.
	events.StripRepositoryAuthoredEventFields(out)
	if event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED ||
		event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED {
		safePayload, err := sanitizeGenericToolCallPayload(event.Type, out.GetPayload())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "runtime event payload is invalid")
		}
		out.Payload = safePayload
	}
	return out, nil
}

// sanitizeGenericToolCallPayload builds the durable payload for the two
// TOOL_CALL_* types the generic channel still accepts from a worker:
// TOOL_CALL_STARTED and TOOL_CALL_FAILED, as emitted by agent-runtime's
// emitAssistantToolCallFailed before any tool beacon exists. Every other
// TOOL_CALL_* and APPROVAL_* type stays refused above; this only narrows what
// these two may carry.
//
// The payload is rebuilt from an allowlist rather than filtered from a
// denylist, so a hostile field survives only by first being added to the
// allowlist. It reuses events.ToolCallIdentityKeys — the same bounded identity
// the public read boundary already grants a dedicated tool.call.failed or
// tool.call.denied row — so a worker-authored row and a beacon-authored row
// are held to one contract instead of two that could drift apart. A
// TOOL_CALL_FAILED event's category is always the server's own
// runoutcome.ReasonToolFailure, never whatever the worker's payload named,
// because the worker's own account of why is exactly the free-form narration
// this boundary exists to keep off the durable log and the public wire.
func sanitizeGenericToolCallPayload(eventType turingv1.TuringEventType, payload *structpb.Struct) (*structpb.Struct, error) {
	source := payload.AsMap()
	safe := make(map[string]any, len(events.ToolCallIdentityKeys)+1)
	for _, key := range events.ToolCallIdentityKeys {
		if text, ok := source[key].(string); ok && text != "" {
			safe[key] = text
		}
	}
	if eventType == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED {
		safe["category"] = string(runoutcome.ReasonToolFailure)
	}
	return structpb.NewStruct(safe)
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

// isActiveRunStatus reports whether a run is proven to be owned by a live
// worker and therefore allowed to narrate itself.
//
// Recovering deliberately does not count. It means nobody can currently prove
// which worker owns the run, so a generic event or a before-phase tool beacon
// arriving under it is an assertion of exactly the thing that is in doubt.
// Accepting it would let a fenced worker keep writing the run's story and
// reopen the window the fence exists to close. A recovering run moves on
// through the specific guarded paths instead — recovery, terminal reports, and
// approval closure — each of which proves its own identity.
func isActiveRunStatus(runStatus string) bool {
	return runStatus == runningRunStatus || runStatus == waitingApprovalRunStatus
}

const (
	runningRunStatus         = "running"
	waitingApprovalRunStatus = "waiting_approval"
	recoveringRunStatus      = "recovering"
)

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
	// Normal dispatch never reaches this check: a real ack always arrives
	// through reconcileLateAssignedUpdate first, which — by the time a worker
	// acknowledges a cancellation — finds the run already terminal and
	// enforces this same version rule itself before this function is ever
	// called. This copy only matters for the race window between the two
	// GetRun calls: if the run terminalizes after reconcileLateAssignedUpdate
	// read it as still non-terminal but before this handler's own GetRun
	// above, its fallthrough reaches here with no version check yet applied,
	// and this is the only thing left to enforce it.
	if !acknowledgedVersionMatches(ack.GetObservedStateVersion(), run) {
		return status.Error(codes.FailedPrecondition, "run_cancelled_ack observed_state_version does not match run")
	}
	if err := s.repo.AcknowledgeExecutionExit(ctx, ack.RunId); err != nil {
		return mapRunStateError(err)
	}
	return nil
}

// acknowledgedVersionMatches compares the version an execution-exit
// acknowledgement observed with the versions this run actually reached.
//
// The acknowledgement is the last thing a worker says about a run, and what it
// releases is the execution fence — the guard that keeps a terminal run's
// identity closed until the process that was running it is known to be gone. A
// fenced predecessor still holds the version it was assigned at, several
// transitions behind, so accepting any version at all would let the attempt
// that LOST the run declare execution finished on behalf of the attempt that
// owns it.
//
// Two versions are legitimate. Normally the worker observes the run's current
// version: every command that terminalizes a run — the cancellation, the
// approval decision — is sent after that transition commits, so what the worker
// was last told is what the run holds. One version of slack is allowed on top of
// that for a report computed just before the terminal transition, which names
// the version the run terminalized FROM; that is the same pre-terminal
// convention matchesPreTerminalVersion applies to terminal reports. Nothing
// further back belongs to this attempt, and nothing ahead exists.
//
// Zero is protobuf absence, not a claim: a worker built before the field
// existed names no version, and is judged on the terminal status alone exactly
// as before. This is the same absence rule matchesPreTerminalVersion and
// terminalExpectation apply to terminal reports.
func acknowledgedVersionMatches(observed int64, run repository.Run) bool {
	return observed == 0 || observed == run.StateVersion || observed == run.StateVersion-1
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

// isMatchingTerminalUpdate reports whether a late terminal update is the exact
// outcome the run already holds.
//
// Every part of the canonical identity is compared, because a late report that
// differs in any of them is a second, different claim about how the run ended —
// and treating it as a duplicate would let it release the execution fence a
// conflicting outcome is meant to keep. That means the version the report was
// computed against, the terminal lifecycle and its outcome reason, the
// assistant message the content belongs to, and, for a completion, the exact
// bytes together with the hash and displayability derived from them. A
// cancellation ack has no outcome of its own to compare against — the run's
// terminal identity was decided independently of it — so its identity is the
// version it observed, held to acknowledgedVersionMatches.
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
			matchesPreTerminalVersion(completed.GetExpectedStateVersion(), run) &&
			run.OutcomeReason == string(completionReason(completed.Content)) &&
			assistantMessageID != "" &&
			assistantMessageID == run.AssistantMessageID &&
			completed.Content == run.AssistantContent &&
			runoutcome.ContentSHA256(completed.Content) == run.ContentSHA256 &&
			runoutcome.HasDisplayableContent(completed.Content) == run.HasDisplayableContent()
	case update.GetRunFailed() != nil:
		failed := update.GetRunFailed()
		// Compared against what the canonical writer publishes today: the
		// normalized code and the always-false compatibility flag. The reported
		// code is normalized first, so a late duplicate of a report whose code
		// fell back to unknown still recognizes itself.
		failure := normalizeRuntimeFailure(failed)
		return run.Status == "failed" &&
			run.TerminalEventType == "agent.run.failed" &&
			matchesPreTerminalVersion(failed.GetExpectedStateVersion(), run) &&
			run.OutcomeReason == string(failure.Reason()) &&
			matchesReportedTerminalPayload(run.TerminalEventPayload, map[string]any{
				"runId": failed.RunId, "code": failure.Code(), "retryable": false,
			})
	case update.GetRunCancelledAck() != nil:
		// A cancellation ack carries no outcome of its own to compare — the
		// run's terminal identity was already decided independently of
		// anything the worker reports. What it does carry is the version it
		// observed, and that has to be held to the same rule
		// acknowledgedVersionMatches already applies for handleRunCancelledAck:
		// a fenced predecessor of the very same attempt can still be carrying a
		// version from before it was fenced, and treating that as a match would
		// let it release a fence the current attempt is still holding.
		return isTerminalRunStatus(run.Status) &&
			acknowledgedVersionMatches(update.GetRunCancelledAck().GetObservedStateVersion(), run)
	default:
		return false
	}
}

// matchesReportedTerminalPayload compares a durable terminal payload with what
// this update would have written.
//
// The durable payload also carries the canonical run state the repository
// merges into every lifecycle projection. That part is derived from the row,
// not from the report, so it is excluded here: comparing raw bytes would make
// every late duplicate look like a conflict the moment the state gained a
// version.
//
// matchesPreTerminalVersion compares the version a report was computed against
// with the version the run terminalized from.
//
// A terminal transition increments exactly once, so the state the report
// expected is the run's current version minus one. A report that names no
// version comes from a worker built before the field existed and is compared on
// its other identity alone rather than rejected.
func matchesPreTerminalVersion(reported int64, run repository.Run) bool {
	return reported == 0 || reported == run.StateVersion-1
}

// completionReason is the outcome a completion of exactly these bytes commits.
// It mirrors the repository writer rather than guessing, so a duplicate report
// is compared against the reason its own content would have produced.
func completionReason(content string) runoutcome.Reason {
	if runoutcome.HasDisplayableContent(content) {
		return runoutcome.ReasonNone
	}
	return runoutcome.ReasonCompletedNoContent
}

// What remains is the normalized code and the always-false retryable flag the
// canonical writer publishes. There is no message key to compare, because there
// is no message: the caller builds the same map the writer would have.
func matchesReportedTerminalPayload(durablePayload string, reported map[string]any) bool {
	var durable map[string]any
	if err := json.Unmarshal([]byte(durablePayload), &durable); err != nil {
		return false
	}
	delete(durable, "runState")
	rebuilt, err := encodePayload(reported)
	if err != nil {
		return false
	}
	var expected map[string]any
	if err := json.Unmarshal([]byte(rebuilt), &expected); err != nil {
		return false
	}
	return reflect.DeepEqual(durable, expected)
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
	// A cancellation is decided independently of anything a worker reports, so
	// once the run is already cancelled, any OTHER terminal-shaped report — a
	// completion, a failure — is accepted as exit proof without comparing its
	// content: that outcome already lost, and content it disagrees on cannot
	// still be the persisted one. A cancellation ack is different: its whole
	// identity is the version it observed, so it stays behind
	// isMatchingTerminalUpdate even when the run is cancelled — otherwise a
	// fenced predecessor of the very same attempt, still carrying a version
	// from before it was fenced, would release a fence the current attempt is
	// still holding.
	//
	// A version-mismatched ack is deliberately handled — silently ignored,
	// not surfaced as an error — even though beginUpdate proves the worker
	// still holds this exact assignment: the mismatch alone means it cannot
	// release a fence it does not match, so it is treated exactly like an
	// already-established mismatched late RunCompleted/RunFailed and the
	// stream stays up for whatever else this worker is still executing. That
	// leniency is specific to a worker that is still assigned; once the
	// assignment is gone entirely, the equivalent mismatch instead reaches
	// isLateMatchingTerminalUpdate through beginUpdate's own ownership error,
	// where an unassigned or wrong-attempt report remains the protocol
	// violation it always was.
	if (run.Status != "cancelled" || update.GetRunCancelledAck() != nil) && !isMatchingTerminalUpdate(run, update) {
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
	// Empty content is not a malformed report. A model that explicitly finished
	// with nothing to say produced an empty answer, and the run commits
	// completed/completed_no_content rather than being rejected or rewritten
	// into filler. What may not complete a run is the absence of a report,
	// which is a different thing entirely and never reaches this handler.
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
	result, err := s.repo.CompleteRunCanonical(ctx, repository.CompleteRunInput{
		RunID:                completed.RunId,
		AssistantMessageID:   assistantMessageID,
		Content:              completed.Content,
		ExpectedStateVersion: terminalExpectation(completed.GetExpectedStateVersion(), run),
		Usage:                runTokenUsage(completed.GetTokenUsage()),
	})
	if err != nil {
		return mapRunStateError(err)
	}
	for _, event := range result.Events {
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

// terminalExpectation resolves the version a terminal transition must commit
// from.
//
// A worker that names a version is held to it exactly: the guarded update
// refuses a report whose premises the run has already left. Absent is not
// wrong — zero is protobuf absence, so a worker built before this field existed
// names nothing and the run's own current version is used, with the same guard
// still fencing a concurrent writer.
func terminalExpectation(reported int64, run repository.Run) int64 {
	if reported > 0 {
		return reported
	}
	return run.StateVersion
}

func (s *Server) handleRunFailed(ctx context.Context, failed *turingv1.RuntimeRunFailed, owner runReleaseOwner) (bool, error) {
	if failed == nil || failed.RunId == "" {
		return false, status.Error(codes.InvalidArgument, "run_failed is required")
	}
	run, err := s.repo.GetRun(ctx, failed.RunId)
	if err != nil {
		return false, err
	}
	failure := normalizeRuntimeFailure(failed)
	// The same legacy-zero resolution a terminal report gets: a worker built
	// before the field existed names no version, and the run's own current
	// version stands in for it under the same guard.
	expected := terminalExpectation(failed.GetExpectedStateVersion(), run)
	if failure.RetryClass() == runoutcome.RetryClassSameRunTransient {
		decision, err := s.repo.RequeueOrFailRetryableRun(ctx, repository.RetryableRunFailureInput{
			RunID:                failed.RunId,
			ExpectedStateVersion: expected,
			WorkerID:             owner.workerID,
			AssignmentAttemptID:  owner.attemptID,
			Failure:              failure,
			MaxAttempts:          s.dispatch.MaxAttempts,
		})
		if err != nil {
			return false, mapRunStateError(err)
		}
		for _, event := range decision.Events {
			s.publishEvent(event)
		}
		// Every retryable failure is requeued, but only an assignment rejection
		// may skip the dispatch that follows. See workerBusyFailureCode.
		return decision.Requeued && failure.Code() == workerBusyFailureCode, nil
	}
	// An abandoned outcome is a cancellation, not a failure: it says the client
	// went away, which is the one thing this transport can honestly report and
	// is not something the run did wrong. Routing it onto the failed lifecycle
	// would ask for an outcome that lifecycle does not allow.
	var result repository.RunTransitionResult
	if failure.Reason() == runoutcome.ReasonAbandoned {
		result, err = s.repo.CancelRunCanonical(ctx, repository.CancelRunInput{
			RunID:                failed.RunId,
			ExpectedStateVersion: expected,
			Cancellation:         runoutcome.AbandonedCancellation(),
		})
	} else {
		result, err = s.repo.FailRunCanonical(ctx, repository.FailRunInput{
			RunID:                failed.RunId,
			ExpectedStateVersion: expected,
			Failure:              failure,
		})
	}
	if err != nil {
		return false, mapRunStateError(err)
	}
	for _, event := range result.Events {
		s.publishEvent(event)
	}
	return false, nil
}

// normalizeRuntimeFailure closes the ingestion boundary for one worker report.
//
// The legacy retryable bool is untrusted and is ignored outright, never
// translated into a retry request: only the typed automatic_retry_class field
// can ask for one, and NormalizeRuntimeFailure still fails closed on an
// unrecognized origin or an unspecified class. Nothing here reads
// failed.Message.
func normalizeRuntimeFailure(failed *turingv1.RuntimeRunFailed) runoutcome.Failure {
	return runoutcome.NormalizeRuntimeFailure(failed.GetFailureOrigin(), failed.GetCode(), failed.GetAutomaticRetryClass())
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
	case errors.Is(err, repository.ErrAssignmentFenced):
		// A report the repository fenced was computed against a premise the run
		// does not hold — a version it never reached, or an attempt it no longer
		// owns. That is a precondition failure like the others, and reporting it
		// as one is what tells a worker its report was refused rather than lost.
		// The sentinel's own text is sent, not the error's: a wrapped fence can
		// carry the run, job, or worker it was about, and none of that belongs
		// on a stream the orchestrator is refusing.
		return status.Error(codes.FailedPrecondition, repository.ErrAssignmentFenced.Error())
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
	// An authenticated beacon from the attempt that still owns a recovering run
	// is the one thing that can settle who owns it, because the decision this
	// call returns carries the committed version back before any tool or model
	// work continues. The generic helper below stays exactly as strict: it
	// answers for updates that have no reply to carry a version on.
	provenVersion := int64(0)
	if !isActiveRunStatus(run.Status) && beacon.Phase == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
		version, proven, err := s.proveBeaconOwnership(ctx, run, workerID, owner)
		if err != nil {
			return nil, err
		}
		if proven {
			provenVersion = version
			run, err = s.repo.GetRun(ctx, beacon.RunId)
			if err != nil {
				return nil, err
			}
		}
	}
	if !isActiveRunStatus(run.Status) && beacon.Phase != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
		return nil, status.Error(codes.FailedPrecondition, "run is not active")
	}
	if beacon.TraceId != run.TraceID {
		return nil, status.Error(codes.InvalidArgument, "tool call trace_id does not match run")
	}
	decision, err := s.dispatchToolBeacon(ctx, beacon, run, workerID, owner)
	if decision != nil {
		// Every decision carries the run's committed version forward, not just
		// the ones that proved ownership. A beacon is the orchestrator's reply
		// to work that is about to continue, and the lifecycle may well have
		// moved since this worker was assigned — an approval alone takes the run
		// through waiting-approval and back. Without the version here the
		// worker's next terminal report would be computed against a state the
		// run has already left, and correctly refused.
		decision.RunStateVersion = s.committedVersion(ctx, beacon.GetRunId(), provenVersion)
	}
	return decision, err
}

// committedVersion reads the version a run holds now, falling back to a version
// this call already committed if the read fails.
func (s *Server) committedVersion(ctx context.Context, runID string, fallback int64) int64 {
	state, err := s.repo.GetRunState(ctx, runID)
	if err != nil {
		return fallback
	}
	if state.StateVersion > fallback {
		return state.StateVersion
	}
	return fallback
}

// proveBeaconOwnership resolves a recovering run in favour of the attempt this
// beacon arrived on, when that attempt is the one the worker still holds.
func (s *Server) proveBeaconOwnership(ctx context.Context, run repository.Run, workerID string, owner *worker) (int64, bool, error) {
	if workerID == "" || owner == nil {
		return 0, false, nil
	}
	held, assigned := owner.assignmentForRun(run.RunID)
	if !assigned {
		return 0, false, nil
	}
	return s.proveRecoveringOwnership(ctx, run.RunID, workerID, held.attemptID)
}

func (s *Server) dispatchToolBeacon(ctx context.Context, beacon *turingv1.ToolCallBeacon, run repository.Run, workerID string, owner *worker) (*turingv1.ToolPolicyDecision, error) {
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
	if policy == tools.PolicyApprovalRequired && beaconServerName(beacon) == "integrations" && !tools.ToolReadOnly("integrations", beacon.ToolName) {
		render, renderErr := s.repo.IntegrationApprovalRender(ctx, beacon.ToolName, args)
		if renderErr != nil {
			return s.denyToolBefore(ctx, beacon, run, argsJSON, argsHash, "integration_approval_render_invalid")
		}
		if len([]byte(render)) > repository.MaxIntegrationApprovalRenderBytes {
			return s.denyToolBefore(ctx, beacon, run, argsJSON, argsHash, "integration_approval_render_too_large")
		}
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
			if decision, handled := existingToolBeforeDecision(recorded.Record, "tool_denied", beaconServerName(beacon), beacon.ToolName); handled {
				return s.withToolProvenance(ctx, decision, beacon, run, argsHash), nil
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
				return s.failUnattendedRun(ctx, beacon, runoutcome.NormalizeFailure(
					runoutcome.OriginAutomationPolicy, "automation_approval_failed", runoutcome.RetryClassNever,
				))
			}
		}
		return &turingv1.ToolPolicyDecision{
			Decision:        turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
			ToolCallId:      beacon.ToolCallId,
			ApprovalId:      approvalID,
			ProvenanceToken: s.issueToolProvenance(ctx, beacon, run, argsHash),
			ReadOnly:        tools.ToolReadOnly(beaconServerName(beacon), beacon.ToolName),
		}, nil
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
		if decision, handled := existingToolBeforeDecision(recorded.Record, "tool_denied", beaconServerName(beacon), beacon.ToolName); handled {
			return s.withToolProvenance(ctx, decision, beacon, run, argsHash), nil
		}
	}
	if recorded.Inserted {
		if _, err := s.audit.RecordForExistingRun(ctx, beacon.RunId, "runtime", "", "tool.call.before", beacon.ToolCallId, toolAuditPayload(beacon)); err != nil {
			log.Printf("record tool before audit for %s: %v", beacon.ToolCallId, err)
		}
	}
	switch policy {
	case tools.PolicySafe:
		return &turingv1.ToolPolicyDecision{
			Decision:        turingv1.ToolPolicyDecision_DECISION_ALLOW,
			ToolCallId:      beacon.ToolCallId,
			ProvenanceToken: s.issueToolProvenance(ctx, beacon, run, argsHash),
			ReadOnly:        tools.ToolReadOnly(beaconServerName(beacon), beacon.ToolName),
		}, nil
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
// denied and the run is failed under the automation-policy origin, which is
// what the automation's last-run status reports and what the public
// policy-denied outcome is projected from. Which automation and which tool are
// in the audit record, not in the run's failure — see below.
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
	// The automation's name and the tool's name are in the audit record above,
	// where they belong. They are deliberately not carried into the run's
	// failure: agent_runs.error_message is a public diagnostic column, and a
	// sentence naming an automation and a tool is exactly the kind of content
	// TUR-009 stops persisting there. What is published instead is the
	// policy-denied outcome, from which Flutter derives localized copy.
	//
	// The decision is discarded rather than merged: on a replayed beacon
	// denyToolBefore reports why the tool call is ALREADY terminal
	// ("tool_call_already_completed"), and letting that overwrite the reason
	// here would make the runtime's terminal error disagree with the run's
	// stored error_code.
	_ = decision
	return s.failUnattendedRun(ctx, beacon, runoutcome.NormalizeFailure(
		runoutcome.OriginAutomationPolicy, AutomationNotAllowlistedCode, runoutcome.RetryClassNever,
	))
}

// failUnattendedRun terminalises a run nobody is watching, and returns the
// decision that tells the runtime to stop rather than keep calling tools.
//
// The failure arrives already normalized, because this is a reporting site the
// orchestrator owns: it knows the run stopped on automation policy, and that
// typed fact is what decides the public outcome. The sentence a person would
// read is derived by the client from that outcome, not persisted here.
func (s *Server) failUnattendedRun(ctx context.Context, beacon *turingv1.ToolCallBeacon, failure runoutcome.Failure) (*turingv1.ToolPolicyDecision, error) {
	version, err := s.currentStateVersion(ctx, beacon.GetRunId())
	if err != nil {
		return nil, err
	}
	result, err := s.repo.FailRunCanonical(ctx, repository.FailRunInput{
		RunID:                beacon.GetRunId(),
		ExpectedStateVersion: version,
		Failure:              failure,
		PreserveExecution:    true,
	})
	if err != nil {
		// The run is already terminal (cancelled, or failed by another path),
		// or another writer moved it between the read above and this update.
		// Telling the runtime to stop is still right; failing the RPC would
		// only leave it waiting.
		if errors.Is(err, repository.ErrRunNotFailable) || errors.Is(err, repository.ErrRunTransitionConflict) {
			return terminalRunDecision(beacon, failure.Code()), nil
		}
		return nil, err
	}
	for _, event := range result.Events {
		s.publishEvent(event)
	}
	return terminalRunDecision(beacon, failure.Code()), nil
}

func (s *Server) terminalizePostCommitApprovalFailure(_ context.Context, runID string, toolCallID string) error {
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	approval, err := s.repo.GetApprovalByToolCall(recoveryCtx, runID, toolCallID)
	if errors.Is(err, sql.ErrNoRows) {
		version, versionErr := s.currentStateVersion(recoveryCtx, runID)
		if versionErr != nil {
			return versionErr
		}
		result, failErr := s.repo.FailRunCanonical(recoveryCtx, repository.FailRunInput{
			RunID:                runID,
			ExpectedStateVersion: version,
			Failure: runoutcome.NormalizeFailure(
				runoutcome.OriginApprovalTransport, "approval_delivery_failed", runoutcome.RetryClassNever,
			),
			PreserveExecution: true,
		})
		if failErr != nil {
			return failErr
		}
		for _, event := range result.Events {
			s.publishEvent(event)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("find approval after creation failure: %w", err)
	}
	return s.terminalizeApprovalDeliveryFailure(recoveryCtx, approval.ApprovalID, nil)
}

// currentStateVersion reads the version a run holds right now, for a writer
// whose decision to terminalize came from somewhere other than the run row.
//
// The read is not the guard: the guarded update is, and it refuses the write if
// another writer moved the row in between. This only supplies the expectation
// that guard compares against, so no terminal transition is ever unversioned.
func (s *Server) currentStateVersion(ctx context.Context, runID string) (int64, error) {
	state, err := s.repo.GetRunState(ctx, runID)
	if err != nil {
		return 0, err
	}
	return state.StateVersion, nil
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

// denyToolBefore refuses a tool call and records the refusal.
//
// reason stays internal: it is the decision the runtime is told, and the
// rationale the audit log keeps. The durable event gets the identity a client
// was already promised plus the one category a denial can carry, so a client
// reading tool.call.denied cannot tell a live denial from a migrated one, and
// no policy string reaches it under any key.
func (s *Server) denyToolBefore(ctx context.Context, beacon *turingv1.ToolCallBeacon, run repository.Run, argsJSON string, argsHash string, reason string) (*turingv1.ToolPolicyDecision, error) {
	category, _ := runoutcome.ToolCallFailureCategory("tool.call.denied")
	deniedPayload, err := safejson.MarshalCanonical(map[string]any{
		"toolCallId": beacon.ToolCallId,
		"serverName": beaconServerName(beacon),
		"toolName":   beacon.ToolName,
		"category":   string(category),
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
		if decision, handled := existingToolBeforeDecision(recorded.Record, reason, beaconServerName(beacon), beacon.ToolName); handled {
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

func existingToolBeforeDecision(record repository.ToolCallRecord, deniedReason, serverName, toolName string) (*turingv1.ToolPolicyDecision, bool) {
	decision := &turingv1.ToolPolicyDecision{ToolCallId: record.ToolCallID, ReadOnly: tools.ToolReadOnly(serverName, toolName)}
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
	// Normalization happens here, before the tool call row and before the
	// event, because both are durable and both are read back to clients. What
	// the beacon actually said — a path, a key, a provider's prose in an error
	// message — never reaches either.
	payload := map[string]any{
		"toolCallId": beacon.ToolCallId,
		"serverName": beaconServerName(beacon),
		"toolName":   beacon.ToolName,
	}
	if category, isFailure := runoutcome.ToolCallFailureCategory(eventType); isFailure {
		payload["category"] = string(category)
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
		ErrorCode:       toolAfterErrorCode(eventType, beacon),
		// Deliberately empty on every terminal shape. error_message is the
		// column a tool's or a provider's sentence used to reach a client
		// through, and nothing the runtime can say belongs in it.
		ErrorMessage: "",
		DurationMS:   beacon.DurationMs,
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
		// An approval decision moves the run's lifecycle, so the worker is told
		// which version that landed on. Delivering the decision does not by
		// itself resume the run; it only stops the worker reporting against a
		// state that no longer exists.
		StateVersion: s.committedVersion(ctx, runID, 0),
	}}}); err != nil {
		return err
	}
	return nil
}

func (s *Server) NotifyMCPRegistryChanged(ctx context.Context) error {
	s.registryMu.Lock()
	defer s.registryMu.Unlock()
	enabled, err := s.repo.ListEnabledTools(ctx)
	if err != nil {
		return err
	}
	enabledTools := make(map[string]struct{}, len(enabled))
	for _, tool := range enabled {
		enabledTools[tool.ServerName+"/"+tool.ToolName] = struct{}{}
	}

	s.toolsMu.Lock()
	for workerID, toolset := range s.toolsets {
		filtered := toolset.tools[:0]
		for _, tool := range toolset.tools {
			if _, keep := enabledTools[tool.ServerName+"/"+tool.ToolName]; keep {
				filtered = append(filtered, tool)
			}
		}
		toolset.tools = filtered
		s.toolsets[workerID] = toolset
	}
	s.toolsMu.Unlock()

	s.mu.Lock()
	workers := make([]*worker, 0, len(s.workers))
	for _, connected := range s.workers {
		workers = append(workers, connected)
	}
	s.mu.Unlock()

	var notifyErr error
	for _, connected := range workers {
		connected.mu.Lock()
		if connected.capabilities != nil {
			filtered := cloneRegisteredWorkerCapabilities(connected.capabilities)
			for tool := range filtered.tools {
				if _, keep := enabledTools[tool]; !keep {
					delete(filtered.tools, tool)
				}
			}
			connected.capabilities = filtered
		}
		connected.mu.Unlock()
		sendCtx, cancel := withDefaultTimeout(ctx, 5*time.Second)
		err := connected.send(sendCtx, &turingv1.RuntimeCommand{
			Command: &turingv1.RuntimeCommand_McpRegistryChanged{
				McpRegistryChanged: &turingv1.RuntimeMcpRegistryChanged{
					RegistrationId: connected.registrationID,
				},
			},
		})
		cancel()
		if err != nil {
			notifyErr = errors.Join(notifyErr, err)
		}
	}
	return notifyErr
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

// toolAfterErrorCode is the only thing a tool-after beacon's error object is
// allowed to contribute to durable storage, and even that survives only if it
// is on the approved matrix.
//
// The beacon has no origin field, so the origin comes from what this call site
// knows: it is ingesting a tool call's terminal report, and the runtime's tools
// package reports from a closed vocabulary whose approved origins are fixed. A
// code outside that vocabulary gets the origin its event type implies and then
// fails closed to CodeUnknown, so an unrecognized worker cannot widen the
// column by inventing a code. A non-failure terminal report contributes no code
// at all.
func toolAfterErrorCode(eventType string, beacon *turingv1.ToolCallBeacon) string {
	if _, isFailure := runoutcome.ToolCallFailureCategory(eventType); !isFailure {
		return ""
	}
	code := beacon.GetError().GetCode()
	return runoutcome.NormalizeFailure(
		toolAfterFailureOrigin(eventType, code), code, runoutcome.RetryClassNever,
	).Code()
}

// toolAfterFailureOrigin names the origin the approved subsidiary mapping gives
// each code the runtime's tools package can report on an after beacon, and
// falls back to the origin the event type itself establishes: a denial came
// from policy, and anything else that ended a tool call ended while it ran.
func toolAfterFailureOrigin(eventType string, code string) runoutcome.Origin {
	if eventType == "tool.call.denied" {
		return runoutcome.OriginToolPolicy
	}
	switch code {
	case "tool_policy_decision_failed", "tool_policy_decision_invalid":
		return runoutcome.OriginToolPolicy
	case "approval_wait_failed":
		return runoutcome.OriginApprovalTransport
	case "cancelled":
		return runoutcome.OriginClientLifecycle
	default:
		return runoutcome.OriginToolExecution
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
	if strings.HasPrefix(beacon.ToolName, "github.") {
		return "integrations"
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
		ExpectedStateVersion:           job.ExpectedStateVersion,
		AssignmentAttemptId:            job.AssignmentAttemptID,
		EgressDecision:                 toProtoEgressDecision(job.EgressDecision),
		SelectedTools:                  append([]string(nil), job.SelectedTools...),
		PinnedPersona:                  toProtoPinnedPersona(job.PinnedPersona),
		PinnedProfile:                  toProtoPinnedProfile(job.PinnedProfile),
		MemorySnapshotFingerprint:      job.MemorySnapshotFingerprint,
	}
}

// toProtoPinnedPersona and toProtoPinnedProfile keep nil as nil. A job enqueued
// before the vault existed was never offered a persona, and inventing an empty
// one would tell the runtime the user wrote nothing — a claim about them that
// nobody made.
func toProtoPinnedPersona(snapshot *repository.PinnedPersonaSnapshot) *turingv1.PinnedPersonaSnapshot {
	if snapshot == nil {
		return nil
	}
	return &turingv1.PinnedPersonaSnapshot{
		PersonaId:   snapshot.PersonaID,
		DisplayName: snapshot.DisplayName,
		Body:        snapshot.Body,
		ContentHash: snapshot.ContentHash,
		Withheld:    snapshot.Withheld,
	}
}

func toProtoPinnedProfile(snapshot *repository.PinnedProfileSnapshot) *turingv1.PinnedProfileSnapshot {
	if snapshot == nil {
		return nil
	}
	return &turingv1.PinnedProfileSnapshot{
		ProfileId:   snapshot.ProfileID,
		Body:        snapshot.Body,
		ContentHash: snapshot.ContentHash,
		Withheld:    snapshot.Withheld,
	}
}

func toProtoEgressDecision(decision *repository.RunEgressDecision) *turingv1.RunEgressDecision {
	if decision == nil {
		return nil
	}
	categories := make([]turingv1.EgressDataCategory, 0, len(decision.DataCategories))
	for _, name := range decision.DataCategories {
		value, ok := turingv1.EgressDataCategory_value[name]
		if !ok {
			categories = append(categories, turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_UNSPECIFIED)
			continue
		}
		categories = append(categories, turingv1.EgressDataCategory(value))
	}
	consentedAt, _ := time.Parse(time.RFC3339Nano, decision.ConsentGrantedAt)
	remoteMCPServers := make([]*turingv1.RemoteMcpEgressDestination, len(decision.RemoteMCPServers))
	for index, destination := range decision.RemoteMCPServers {
		remoteMCPServers[index] = &turingv1.RemoteMcpEgressDestination{
			ServerName:   destination.ServerName,
			Endpoint:     destination.Endpoint,
			EndpointHost: destination.EndpointHost,
		}
	}
	integrationEndpoints := make([]*turingv1.IntegrationEgressDestination, len(decision.IntegrationEndpoints))
	for index, destination := range decision.IntegrationEndpoints {
		integrationEndpoints[index] = &turingv1.IntegrationEgressDestination{
			Endpoint: destination.Endpoint, EndpointHost: destination.EndpointHost,
			ConnectionId: destination.ConnectionID, DisplayName: destination.DisplayName,
			Tools: append([]string(nil), destination.Tools...),
		}
	}
	return &turingv1.RunEgressDecision{
		DecisionId:                decision.DecisionID,
		Version:                   int32(decision.Version),
		Provider:                  providerToProto(decision.Provider),
		Model:                     decision.Model,
		Endpoint:                  decision.Endpoint,
		EndpointHost:              decision.EndpointHost,
		ExternalAgentId:           decision.ExternalAgentID,
		DataCategories:            categories,
		ConsentGrantedAt:          timestamppb.New(consentedAt),
		ChallengeFingerprint:      decision.ChallengeFingerprint,
		SelectedTools:             append([]string(nil), decision.SelectedTools...),
		SkillSnapshotFingerprint:  decision.SkillSnapshotFingerprint,
		RecallApplicable:          decision.RecallApplicable,
		MemoryProfileApplicable:   decision.MemoryProfileApplicable,
		ExternalCredentialRefHash: decision.ExternalCredentialRefHash,
		RequestDigest:             decision.RequestDigest,
		RemoteMcpServers:          remoteMCPServers,
		IntegrationEndpoints:      integrationEndpoints,
		MemorySnapshotFingerprint: decision.MemorySnapshotFingerprint,
	}
}

func providerToProto(provider string) turingv1.ModelProvider {
	if provider == "openai_compatible" {
		return turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
	}
	if provider == "ollama" {
		return turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA
	}
	return turingv1.ModelProvider_MODEL_PROVIDER_UNSPECIFIED
}

// toProtoExternalAgent keeps nil as nil. An empty target would look like a
// routed run with no endpoint, which is a worse failure than an unrouted one:
// the runtime would try to reach "" instead of just running locally.
func toProtoExternalAgent(target *repository.ExternalAgentTarget) *turingv1.ExternalAgentTarget {
	if target == nil {
		return nil
	}
	return &turingv1.ExternalAgentTarget{
		AgentId:       target.AgentID,
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
