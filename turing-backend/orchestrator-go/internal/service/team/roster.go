package team

import (
	"context"
	"slices"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/teamfiles"
)

// Roster is the team a Turing run enqueued now may delegate to: the active
// profiles, ordered by ID, each with the tools its child would receive. It is
// nil while the team is off.
func (s *Server) Roster(ctx context.Context) ([]repository.TeamRosterEntry, error) {
	if !s.enabled {
		return nil, nil
	}
	profiles, err := s.repo.ListAgentProfiles(ctx)
	if err != nil {
		return nil, err
	}
	resolver, err := s.newResolver(ctx)
	if err != nil {
		return nil, err
	}
	var roster []repository.TeamRosterEntry
	for _, profile := range profiles {
		resolved := resolver.resolve(profile)
		if resolved.GetState() != turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE {
			continue
		}
		roster = append(roster, repository.TeamRosterEntry{
			ProfileID: resolved.GetProfileId(), Revision: resolved.GetRevision(),
			Name: resolved.GetName(), Emoji: resolved.GetEmoji(), Description: resolved.GetDescription(),
			Tools: resolved.GetResolvedTools(),
		})
	}
	slices.SortFunc(roster, func(a, b repository.TeamRosterEntry) int { return strings.Compare(a.ProfileID, b.ProfileID) })
	return roster, nil
}

// Continuation is what the continuation of a Turing run on route may use if
// the run delegates: the tools every team-protocol worker on that route
// serves, less the team itself, tools that leave the machine, and tools a
// policy or a disabled server turned off. Turing's own memory tools stay;
// which of them it may call is the user's choice, not a profile's. It is
// frozen onto the run beside its roster, so the join reads only the database.
func (s *Server) Continuation(ctx context.Context, route repository.RoutingRequirements) (repository.TeamContinuation, error) {
	continuation := repository.TeamContinuation{Tools: []string{}, ResultMaxBytes: s.delegationResultMaxBytes}
	if s.routes == nil {
		return continuation, nil
	}
	onRoute := map[string]bool{}
	for _, name := range s.routes.EgressToolNames(route) {
		onRoute[name] = true
	}
	if len(onRoute) == 0 {
		return continuation, nil
	}
	catalog, err := s.repo.ListToolCatalog(ctx)
	if err != nil {
		return repository.TeamContinuation{}, err
	}
	for _, entry := range catalog {
		qualified := entry.ServerName + "/" + entry.ToolName
		if onRoute[qualified] && exclusionReason(entry, teamfiles.MemoryPropose, true) == "" {
			continuation.Tools = append(continuation.Tools, qualified)
		}
	}
	slices.Sort(continuation.Tools)
	return continuation, nil
}
