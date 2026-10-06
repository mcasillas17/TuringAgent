package team

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/approvals"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// ApprovalEnforcer consumes the approval an approval-gated delegation depends
// on. The orchestrator creates the child, so the check happens on this side.
type ApprovalEnforcer interface {
	ConsumeApprovalForThirdParty(ctx context.Context, approvalID string, runID string, serverName string, serverID string, toolName string, args map[string]any) error
}

// EventPublisher delivers committed events to live subscribers.
type EventPublisher interface {
	Publish(events.Event)
}

func (s *Server) SetApprovalEnforcer(enforcer ApprovalEnforcer) { s.approvals = enforcer }
func (s *Server) SetEventPublisher(publisher EventPublisher)    { s.events = publisher }
func (s *Server) SetMaxDelegationsPerRun(limit int)             { s.maxDelegationsPerRun = limit }

// CallTeamTool delegates one task for the run executing it.
//
// The gates run in the order of CallMemoryTool: who is calling, then whether
// this call already delegated, then what the run and the specialist allow,
// then the policy and its approval, then everything again immediately before
// the effect, because an approval wait can outlast any of it. The run names
// itself; the team, the specialist and the child's tools all come from the
// orchestrator's own tables and the roster frozen onto the run's job.
func (s *Server) CallTeamTool(ctx context.Context, req *turingv1.CallTeamToolRequest) (*turingv1.CallTeamToolResponse, error) {
	if req.GetRunId() == "" || req.GetAssignmentAttemptId() == "" || req.GetToolCallId() == "" || req.GetArgs() == nil {
		return nil, status.Error(codes.InvalidArgument, "run_id, assignment_attempt_id, tool_call_id and args are required")
	}
	if req.GetToolName() != delegateToolName {
		return nil, status.Error(codes.NotFound, "team tool not found")
	}
	runID, args := req.GetRunId(), req.GetArgs().AsMap()
	// The hash approvals bind to, so a replay is checked against the same
	// canonical form an approval was granted over.
	argsHash, err := approvals.ArgumentsHash(args)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "team.delegate arguments are not valid JSON")
	}

	current, err := s.repo.DelegationAssignmentCurrent(ctx, runID, req.GetAssignmentAttemptId())
	if err != nil {
		return nil, status.Error(codes.Internal, "read run assignment failed")
	}
	if !current {
		return nil, status.Error(codes.FailedPrecondition, "this run no longer holds its assignment")
	}
	// A retry of a call that already delegated answers with what it created,
	// whatever has changed since, and spends nothing.
	if existing, found, err := s.repo.DelegationForToolCall(ctx, runID, req.GetToolCallId()); err != nil {
		return nil, status.Error(codes.Internal, "read delegation failed")
	} else if found {
		if existing.ArgsHash != argsHash {
			return nil, status.Error(codes.FailedPrecondition, repository.ErrDelegationArgumentsChanged.Error())
		}
		return delegationResult(existing)
	}

	child, err := s.admitDelegation(ctx, runID, args)
	if err != nil {
		return nil, err
	}

	// An unregistered tool reads as no policy, and is refused with a
	// disabled one.
	policy, _, err := s.repo.PseudoServerToolPolicy(ctx, serverName, delegateToolName)
	if err != nil {
		return nil, status.Error(codes.Internal, "read team.delegate policy failed")
	}
	switch policy {
	case "safe":
	case "approval_required":
		if s.approvals == nil {
			return nil, status.Error(codes.FailedPrecondition, "caller-side approval enforcement is not configured")
		}
		// "team" is a pseudo-server with no mcp_servers row, so its tool
		// calls record no server ID and the empty one is the one that
		// matches.
		if err := s.approvals.ConsumeApprovalForThirdParty(ctx, req.GetApprovalId(), runID, serverName, "", delegateToolName, args); err != nil {
			return nil, err
		}
	default:
		return nil, status.Error(codes.FailedPrecondition, "team.delegate is disabled or unregistered")
	}

	// Resolved again now: the profile file and the live workers are not in
	// the database, so the creating transaction cannot read them. What the
	// database holds it reads again itself.
	if child, err = s.admitDelegation(ctx, runID, args); err != nil {
		return nil, err
	}
	child.AssignmentAttemptID = req.GetAssignmentAttemptId()
	child.ToolCallID = req.GetToolCallId()
	child.ArgsHash = argsHash
	child.Policy = policy
	created, err := s.repo.CreateDelegation(ctx, child)
	switch {
	case errors.Is(err, repository.ErrAssignmentFenced):
		return nil, status.Error(codes.FailedPrecondition, "this run no longer holds its assignment")
	case errors.Is(err, repository.ErrDelegationArgumentsChanged), errors.Is(err, repository.ErrDelegationPolicyChanged),
		errors.Is(err, repository.ErrDelegationProfileChanged), errors.Is(err, repository.ErrDelegationCapReached),
		errors.Is(err, repository.ErrTeamNameCollision):
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	case err != nil:
		return nil, status.Error(codes.Internal, "create delegation failed")
	}
	if s.events != nil {
		for _, event := range created.Events {
			s.events.Publish(events.FromRepositoryEvent(event))
		}
	}
	// The child is durable either way; this only lets a free worker claim it
	// now instead of at the next dispatch. Both are idempotent.
	if err := s.routes.DispatchPending(context.WithoutCancel(ctx)); err != nil {
		log.Printf("team: dispatch delegated run %s: %v", created.Delegation.ChildRunID, err)
	}
	if err := s.routes.RefreshPendingRoutingState(context.WithoutCancel(ctx), "delegation enqueued"); err != nil {
		log.Printf("team: refresh routing state for delegated run %s: %v", created.Delegation.ChildRunID, err)
	}
	return delegationResult(created.Delegation)
}

// admitDelegation runs the read-only gates and resolves the child the call
// would create: the specialist as the run's roster froze it, its model and
// tools as they resolve now, and the framed brief.
func (s *Server) admitDelegation(ctx context.Context, runID string, args map[string]any) (repository.CreateDelegationInput, error) {
	if !s.enabled {
		return repository.CreateDelegationInput{}, status.Error(codes.FailedPrecondition, "the team is turned off")
	}
	agent, task, briefContext, err := delegationArguments(args)
	if err != nil {
		return repository.CreateDelegationInput{}, status.Error(codes.InvalidArgument, err.Error())
	}
	// The repository freezes a roster only onto an attended run on a local
	// model, never an external agent's, and, when a decision froze the run's
	// tools, only if that set names team.delegate. The run's roster therefore
	// carries those gates: a run that was not offered the team has no one on
	// it to delegate to.
	roster, err := s.repo.RunTeamRoster(ctx, runID)
	if err != nil {
		return repository.CreateDelegationInput{}, status.Error(codes.Internal, "read run roster failed")
	}
	index := slices.IndexFunc(roster, func(entry repository.TeamRosterEntry) bool { return entry.ProfileID == agent })
	if index < 0 {
		return repository.CreateDelegationInput{}, status.Error(codes.FailedPrecondition, "that specialist is not on this run's team")
	}
	entry := roster[index]
	profiles, err := s.repo.ListAgentProfiles(ctx)
	if err != nil {
		return repository.CreateDelegationInput{}, status.Error(codes.Internal, "read team profiles failed")
	}
	profileIndex := slices.IndexFunc(profiles, func(profile repository.AgentProfile) bool { return profile.ID == agent })
	if profileIndex < 0 {
		return repository.CreateDelegationInput{}, status.Error(codes.FailedPrecondition, fmt.Sprintf("%s is no longer on the team", entry.Name))
	}
	profile := profiles[profileIndex]
	resolver, err := s.newResolver(ctx)
	if err != nil {
		return repository.CreateDelegationInput{}, status.Error(codes.Internal, "resolve team profiles failed")
	}
	// A `team` name collision, the profile's enablement, its grant and its
	// requires all land in its state.
	resolved := resolver.resolve(profile)
	if resolved.GetState() != turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE {
		reason := "it is not active"
		if reasons := resolved.GetUnavailableReasons(); len(reasons) > 0 {
			reason = strings.Join(reasons, "; ")
		}
		return repository.CreateDelegationInput{}, status.Error(codes.FailedPrecondition, fmt.Sprintf("%s is not available: %s", entry.Name, reason))
	}
	if resolved.GetRevision() != entry.Revision {
		return repository.CreateDelegationInput{}, status.Error(codes.FailedPrecondition, fmt.Sprintf("%s changed since this run was offered it", entry.Name))
	}
	delegated, err := s.repo.DelegationCount(ctx, runID)
	if err != nil {
		return repository.CreateDelegationInput{}, status.Error(codes.Internal, "read delegations failed")
	}
	if delegated >= s.maxDelegationsPerRun {
		return repository.CreateDelegationInput{}, status.Error(codes.FailedPrecondition, repository.ErrDelegationCapReached.Error())
	}
	tools := resolved.GetResolvedTools()
	// A child no worker could claim is never queued just to wait out the
	// queue bound: its whole route has to be served now.
	if err := s.routes.ValidateRouting(ctx, repository.RoutingRequirements{
		AgentID: childAgentID, ModelProvider: childProvider, Model: resolved.GetResolvedModel(),
		RequestedTools: tools, SelectedTools: tools, MinimumTeamProtocolVersion: 1,
	}); err != nil {
		return repository.CreateDelegationInput{}, err
	}
	brief, err := backendegress.FrameDelegationBrief(task, briefContext)
	if err != nil {
		return repository.CreateDelegationInput{}, status.Error(codes.Internal, "frame the brief failed")
	}
	return repository.CreateDelegationInput{
		ParentRunID: runID,
		MaxPerRun:   s.maxDelegationsPerRun,
		Profile: repository.AgentProfileSnapshot{
			ProfileID: profile.ID, Revision: entry.Revision, DisplayName: profile.Name, Emoji: profile.Emoji,
			Instructions: profile.Instructions, MaxToolCalls: profile.MaxToolCalls,
		},
		SkillPatterns: profile.Skills,
		ModelProvider: childProvider,
		Model:         resolved.GetResolvedModel(),
		SelectedTools: tools,
		Brief:         brief,
	}, nil
}

// delegationArguments refuses anything team.delegate does not declare. The
// messages are this package's own words, never the caller's bytes.
func delegationArguments(args map[string]any) (agent, task, briefContext string, err error) {
	for name := range args {
		if name != "agent" && name != "task" && name != "context" {
			return "", "", "", errors.New("team.delegate takes only these arguments: agent, task, context")
		}
	}
	agent, ok := args["agent"].(string)
	if !ok || agent == "" {
		return "", "", "", errors.New("agent must name a specialist on the team")
	}
	task, ok = args["task"].(string)
	if !ok || strings.TrimSpace(task) == "" {
		return "", "", "", errors.New("task is required")
	}
	if len(task) > backendegress.DelegationBriefMaxTaskBytes {
		return "", "", "", fmt.Errorf("task is at most %d bytes of UTF-8", backendegress.DelegationBriefMaxTaskBytes)
	}
	if raw, present := args["context"]; present {
		if briefContext, ok = raw.(string); !ok {
			return "", "", "", errors.New("context must be text")
		}
	}
	if len(briefContext) > backendegress.DelegationBriefMaxContextBytes {
		return "", "", "", fmt.Errorf("context is at most %d bytes of UTF-8", backendegress.DelegationBriefMaxContextBytes)
	}
	return agent, task, briefContext, nil
}

func delegationResult(delegation repository.Delegation) (*turingv1.CallTeamToolResponse, error) {
	result, err := structpb.NewStruct(map[string]any{
		"delegation_id": delegation.ID, "agent": delegation.ProfileID, "state": delegation.State,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, "build delegation result failed")
	}
	return &turingv1.CallTeamToolResponse{Result: result}, nil
}
