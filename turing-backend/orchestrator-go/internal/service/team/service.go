// Package team serves the specialist profiles under team/ and the user's
// decisions about them, renders the team tool a Turing run is offered, and
// queues the specialist's run when that run delegates a task.
package team

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/teamfiles"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A delegated run is a general-assistant child on a local Ollama model.
const (
	childAgentID  = "general_assistant"
	childProvider = "ollama"
)

// Routes is the slice of the runtime the team needs: which workers are live,
// and telling them about a child it queued. The runtime server satisfies it;
// a nil Routes means no worker is live.
type Routes interface {
	ProviderCapabilities() map[turingv1.ModelProvider][]*turingv1.ModelCapability
	ValidateRouting(ctx context.Context, route repository.RoutingRequirements) error
	EgressToolNames(route repository.RoutingRequirements) []string
	DispatchPending(context.Context) error
	RefreshPendingRoutingState(context.Context, string) error
}

// Server is the team service behind both facets. It is not itself a
// TeamServiceServer, so only PublicServer or InternalServer can be registered.
type Server struct {
	repo         *repository.Repository
	routes       Routes
	defaultModel string
	// enabled is TURING_AGENT_TEAM_ENABLED. Off, no run is offered the team;
	// the profiles themselves stay manageable.
	enabled bool
	// approvals consumes the approval an approval-gated delegation needs.
	approvals ApprovalEnforcer
	// events publishes a delegation's events after it commits.
	events EventPublisher
	// maxDelegationsPerRun is TURING_MAX_DELEGATIONS_PER_RUN.
	maxDelegationsPerRun int
	// delegationResultMaxBytes is TURING_DELEGATION_RESULT_MAX_BYTES.
	delegationResultMaxBytes int
}

func New(repo *repository.Repository, routes Routes, defaultModel string, enabled bool) *Server {
	return &Server{
		repo: repo, routes: routes, defaultModel: defaultModel, enabled: enabled,
		delegationResultMaxBytes: backendegress.DefaultDelegationResultMaxBytes,
	}
}

func (s *Server) ListAgentProfiles(ctx context.Context, _ *turingv1.ListAgentProfilesRequest) (*turingv1.ListAgentProfilesResponse, error) {
	profiles, err := s.repo.ListAgentProfiles(ctx)
	if err != nil {
		return nil, statusError(err)
	}
	resolver, err := s.newResolver(ctx)
	if err != nil {
		return nil, statusError(err)
	}
	response := &turingv1.ListAgentProfilesResponse{Profiles: make([]*turingv1.AgentProfile, 0, len(profiles))}
	for _, profile := range profiles {
		response.Profiles = append(response.Profiles, resolver.resolve(profile))
	}
	return response, nil
}

func (s *Server) SetAgentProfileEnabled(ctx context.Context, req *turingv1.SetAgentProfileEnabledRequest) (*turingv1.AgentProfile, error) {
	id, err := requireField("profile_id", req.GetProfileId())
	if err != nil {
		return nil, err
	}
	profile, err := s.repo.SetAgentProfileEnabled(ctx, id, req.GetEnabled())
	if err != nil {
		return nil, statusError(err)
	}
	return s.resolveOne(ctx, profile)
}

func (s *Server) GrantAgentProfile(ctx context.Context, req *turingv1.GrantAgentProfileRequest) (*turingv1.AgentProfile, error) {
	id, err := requireField("profile_id", req.GetProfileId())
	if err != nil {
		return nil, err
	}
	revision, err := requireField("revision", req.GetRevision())
	if err != nil {
		return nil, err
	}
	profile, err := s.repo.GrantAgentProfile(ctx, id, revision)
	if err != nil {
		return nil, statusError(err)
	}
	return s.resolveOne(ctx, profile)
}

func (s *Server) resolveOne(ctx context.Context, profile repository.AgentProfile) (*turingv1.AgentProfile, error) {
	resolver, err := s.newResolver(ctx)
	if err != nil {
		return nil, statusError(err)
	}
	return resolver.resolve(profile), nil
}

func requireField(name, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", status.Errorf(codes.InvalidArgument, "%s is required", name)
	}
	return value, nil
}

func statusError(err error) error {
	switch {
	case errors.Is(err, repository.ErrAgentProfileNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, repository.ErrAgentProfileInvalid), errors.Is(err, repository.ErrAgentProfileRevisionStale):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, "team profiles are unavailable")
	}
}

// resolver holds the facts one call shares across every profile it resolves.
type resolver struct {
	routes Routes
	// defaultModel is the model a profile that names none runs on now, or
	// empty when no team-protocol worker serves a local model.
	defaultModel string
	// collision is why delegation is off for every profile, or "".
	collision  string
	catalog    []repository.ToolCatalogEntry
	models     map[string]bool
	routeTools map[string]map[string]bool
}

func (s *Server) newResolver(ctx context.Context) (*resolver, error) {
	catalog, err := s.repo.ListToolCatalog(ctx)
	if err != nil {
		return nil, err
	}
	collision, err := s.repo.TeamNameCollision(ctx)
	if err != nil {
		return nil, err
	}
	r := &resolver{
		routes:     s.routes,
		collision:  collision,
		catalog:    catalog,
		models:     map[string]bool{},
		routeTools: map[string]map[string]bool{},
	}
	if s.routes != nil {
		// A child runs only on a team-protocol worker, so a model only older
		// workers serve is no model for one, and the default falls to a
		// model one of them does serve.
		var served []string
		for _, capability := range s.routes.ProviderCapabilities()[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA] {
			if model := capability.GetModel(); s.routes.ValidateRouting(ctx, childRoute(model)) == nil {
				r.models[model] = true
				served = append(served, model)
			}
		}
		switch {
		case r.models[s.defaultModel]:
			r.defaultModel = s.defaultModel
		case len(served) > 0:
			r.defaultModel = served[0]
		}
	}
	return r, nil
}

// childRoute is the route a delegated run on model takes: only workers that
// honor the specialist-job contract can claim it.
func childRoute(model string) repository.RoutingRequirements {
	return repository.RoutingRequirements{
		AgentID: childAgentID, ModelProvider: childProvider, Model: model,
		MinimumTeamProtocolVersion: 1,
	}
}

// model returns the model a delegated run would use now, or a reason why
// there is none.
func (r *resolver) model(declared string) (string, string) {
	if declared != "" {
		if r.models[declared] {
			return declared, ""
		}
		return "", fmt.Sprintf("model `%s` is not served by any team-protocol worker", declared)
	}
	if r.defaultModel != "" {
		return r.defaultModel, ""
	}
	return "", "no team-protocol worker serves a local model"
}

// toolsOn returns the qualified tools every live worker on the model's route
// can run. The route is a child's, so only workers that honor the
// specialist-job contract count: an older one could never claim the child.
func (r *resolver) toolsOn(model string) map[string]bool {
	if model == "" || r.routes == nil {
		return nil
	}
	if tools, ok := r.routeTools[model]; ok {
		return tools
	}
	tools := map[string]bool{}
	for _, name := range r.routes.EgressToolNames(childRoute(model)) {
		tools[name] = true
	}
	r.routeTools[model] = tools
	return tools
}

func (r *resolver) resolve(profile repository.AgentProfile) *turingv1.AgentProfile {
	out := &turingv1.AgentProfile{
		ProfileId:       profile.ID,
		Name:            profile.Name,
		Emoji:           profile.Emoji,
		Description:     profile.Description,
		Version:         profile.Version,
		Model:           profile.Model,
		Tools:           slices.Clone(profile.Tools),
		Skills:          slices.Clone(profile.Skills),
		Memory:          memoryAccess(profile.Memory),
		Requires:        slices.Clone(profile.Requires),
		MaxToolCalls:    int32(profile.MaxToolCalls), // the loader rejects anything wider
		Revision:        profile.Revision,
		Enabled:         profile.Enabled,
		GrantedRevision: profile.GrantedRevision,
		ParseError:      profile.ParseError,
	}
	if profile.ParseError != "" {
		out.Revision = ""
		out.State = turingv1.AgentProfileState_AGENT_PROFILE_STATE_PARSE_ERROR
		return out
	}

	var reasons []string
	if r.collision != "" {
		reasons = append(reasons, r.collision)
	}
	model, modelReason := r.model(profile.Model)
	out.ResolvedModel = model
	if modelReason != "" {
		reasons = append(reasons, modelReason)
	}
	routeTools := r.toolsOn(model)

	// excluded maps each declared-but-withheld qualified tool to its reason.
	// With no model there is no route to compare against: a tool that passes
	// every other check waits on the model, whose reason already explains it,
	// rather than being blamed on the route.
	excluded := map[string]string{}
	noRoute := modelReason != ""
	var resolved, waiting []string
	for _, entry := range r.catalog {
		if !teamfiles.MatchAny(profile.Tools, entry.ToolName) {
			continue
		}
		qualified := entry.ServerName + "/" + entry.ToolName
		if reason := exclusionReason(entry, profile.Memory, noRoute || routeTools[qualified]); reason != "" {
			excluded[qualified] = reason
			out.ExcludedTools = append(out.ExcludedTools, &turingv1.AgentProfileToolExclusion{Tool: qualified, Reason: reason})
			continue
		}
		if noRoute {
			waiting = append(waiting, qualified)
			continue
		}
		resolved = append(resolved, qualified)
	}
	slices.Sort(resolved)
	slices.SortFunc(out.ExcludedTools, func(a, b *turingv1.AgentProfileToolExclusion) int {
		return strings.Compare(a.GetTool(), b.GetTool())
	})
	out.ResolvedTools = resolved

	for _, pattern := range profile.Requires {
		if reason := r.requirementReason(pattern, profile.Tools, slices.Concat(resolved, waiting), excluded); reason != "" {
			reasons = append(reasons, reason)
		}
	}
	out.UnavailableReasons = reasons

	switch {
	case !profile.Enabled:
		out.State = turingv1.AgentProfileState_AGENT_PROFILE_STATE_DISABLED
	case profile.GrantedRevision != profile.Revision:
		out.State = turingv1.AgentProfileState_AGENT_PROFILE_STATE_NEEDS_GRANT
	case len(reasons) > 0:
		out.State = turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE
	default:
		out.State = turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE
	}
	return out
}

// exclusionReason says why a declared tool is withheld, or "" when a delegated
// run would receive it. The first applicable reason wins.
func exclusionReason(entry repository.ToolCatalogEntry, memory string, onRoute bool) string {
	switch {
	case strings.HasPrefix(entry.ToolName, "team."):
		return "nested delegation is not allowed: specialists cannot delegate"
	case entry.Policy == "disabled":
		return "disabled by policy"
	case !entry.Enabled:
		return "its server is disabled"
	case entry.ServerName == "memory" && !memoryAllows(memory, entry.ToolName):
		return fmt.Sprintf("beyond the profile's memory level (%s)", memory)
	case entry.ServerName == "integrations" || entry.ServerTier == string(repository.MCPServerTierRemoteURL):
		return "declared, needs per-delegation consent"
	case !onRoute:
		return "not served by every live worker on the route"
	}
	return ""
}

func memoryAllows(level, tool string) bool {
	switch tool {
	case "memory.search", "memory.read":
		return level == teamfiles.MemoryRead || level == teamfiles.MemoryPropose
	case "memory.remember":
		return level == teamfiles.MemoryPropose
	}
	return false
}

// requirementReason says why pattern is unmet, or "" when a tool in satisfied
// meets it. satisfied holds the resolved tools and those waiting only on the
// model, which the model's own reason already covers.
func (r *resolver) requirementReason(pattern string, declared, satisfied []string, excluded map[string]string) string {
	for _, qualified := range satisfied {
		if teamfiles.MatchPattern(pattern, toolPart(qualified)) {
			return ""
		}
	}
	var withheld []string
	undeclared := false
	for _, entry := range r.catalog {
		if !teamfiles.MatchPattern(pattern, entry.ToolName) {
			continue
		}
		if reason, ok := excluded[entry.ServerName+"/"+entry.ToolName]; ok {
			if !slices.Contains(withheld, reason) {
				withheld = append(withheld, reason)
			}
			continue
		}
		if !teamfiles.MatchAny(declared, entry.ToolName) {
			undeclared = true
		}
	}
	switch {
	case len(withheld) > 0:
		return fmt.Sprintf("needs `%s` — %s", pattern, strings.Join(withheld, "; "))
	case undeclared:
		return fmt.Sprintf("needs `%s` — not in the profile's tools", pattern)
	default:
		return fmt.Sprintf("needs `%s` — not connected or registered", pattern)
	}
}

func toolPart(qualified string) string {
	_, tool, _ := strings.Cut(qualified, "/")
	return tool
}

func memoryAccess(level string) turingv1.AgentProfileMemoryAccess {
	switch level {
	case teamfiles.MemoryNone:
		return turingv1.AgentProfileMemoryAccess_AGENT_PROFILE_MEMORY_ACCESS_NONE
	case teamfiles.MemoryRead:
		return turingv1.AgentProfileMemoryAccess_AGENT_PROFILE_MEMORY_ACCESS_READ
	case teamfiles.MemoryPropose:
		return turingv1.AgentProfileMemoryAccess_AGENT_PROFILE_MEMORY_ACCESS_PROPOSE
	}
	return turingv1.AgentProfileMemoryAccess_AGENT_PROFILE_MEMORY_ACCESS_UNSPECIFIED
}
