package team

import (
	"context"
	"slices"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
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
