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
	// Continuation is what the continuation of a run on route may use.
	Continuation(context.Context, repository.RoutingRequirements) (repository.TeamContinuation, error)
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
// than refusing the message. The run's continuation is frozen with it, and a
// team whose continuation cannot be worked out is not offered either: its
// results would have nowhere to go.
func (s *Server) teamRosterFor(ctx context.Context, input repository.EnqueueUserMessageInput) ([]repository.TeamRosterEntry, repository.TeamContinuation, error) {
	none := repository.TeamContinuation{}
	if s.team == nil || s.runtime == nil || input.ModelProvider == "openai_compatible" {
		return nil, none, nil
	}
	if _, routed, err := s.repo.GetSessionAgent(ctx, input.SessionID); err != nil {
		return nil, none, mapSessionError(ctx, err)
	} else if routed {
		return nil, none, nil
	}
	route := repository.EnqueueRoutingRequirements(input)
	route.MinimumTeamProtocolVersion = 1
	if s.runtime.ValidateRouting(ctx, route) != nil {
		return nil, none, nil
	}
	roster, err := s.team.Roster(ctx)
	if err != nil {
		log.Printf("chat: reading the team failed; the run is not offered it: %v", err)
		return nil, none, nil
	}
	if len(roster) == 0 {
		return nil, none, nil
	}
	continuation, err := s.team.Continuation(ctx, route)
	if err != nil {
		log.Printf("chat: working out the team's continuation failed; the run is not offered the team: %v", err)
		return nil, none, nil
	}
	return roster, continuation, nil
}
