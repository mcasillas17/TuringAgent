package chat

import (
	"context"
	"log"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// teamRosterSource is the slice of the team service that knows who is on the
// team now. A nil source is a team that is off.
type teamRosterSource interface {
	Roster(context.Context) ([]repository.TeamRosterEntry, error)
}

// SetTeamRoster gives attended Turing runs the team to delegate to.
func (s *Server) SetTeamRoster(source teamRosterSource) {
	s.team = source
}

// teamRosterFor is the team an attended run is offered: only a run on a local
// model, in a conversation not routed to an external agent, that a
// team-protocol worker can serve whole — model, requested tools, context and
// capacity, the route the enqueue will validate with the team's minimum.
// Chat runs only Turing, the general assistant, so every run that passes is
// Turing's. Every other run gets nil and is enqueued exactly as before. The
// team is optional, so failing to read it leaves the run without it rather
// than refusing the message.
func (s *Server) teamRosterFor(ctx context.Context, input repository.EnqueueUserMessageInput) ([]repository.TeamRosterEntry, error) {
	if s.team == nil || s.runtime == nil || input.ModelProvider == "openai_compatible" {
		return nil, nil
	}
	if _, routed, err := s.repo.GetSessionAgent(ctx, input.SessionID); err != nil {
		return nil, mapSessionError(ctx, err)
	} else if routed {
		return nil, nil
	}
	route := repository.EnqueueRoutingRequirements(input)
	route.MinimumTeamProtocolVersion = 1
	if s.runtime.ValidateRouting(ctx, route) != nil {
		return nil, nil
	}
	roster, err := s.team.Roster(ctx)
	if err != nil {
		log.Printf("chat: reading the team failed; the run is not offered it: %v", err)
		return nil, nil
	}
	return roster, nil
}
