package team

import (
	"context"
	"fmt"
	"log"
	"slices"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
)

// DelegatedProfileActive reports whether a delegated task's specialist still
// resolves Active at the revision the task was created under. It is resolved
// from team/<id>/AGENT.md exactly as CallTeamTool resolves one, so a deleted
// file, an edited authority field, a disabled or re-granted profile, a `team`
// name collision and an unmet requirement all answer false. It reads files,
// so it runs outside every transaction and lock.
func (s *Server) DelegatedProfileActive(ctx context.Context, profileID, revision string) (bool, error) {
	_, resolved, found, err := s.resolveProfile(ctx, profileID)
	if err != nil || !found {
		return false, err
	}
	return resolved.GetState() == turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE && resolved.GetRevision() == revision, nil
}

// resolveProfile reads profileID's file, with the user's settings, and
// resolves it as a delegated run would get it now. found is false when no
// file has that ID. A context that ends during the resolution is an error:
// the resolver's routing reads fail on it, which would otherwise read as no
// model served.
func (s *Server) resolveProfile(ctx context.Context, profileID string) (repository.AgentProfile, *turingv1.AgentProfile, bool, error) {
	profiles, err := s.repo.ListAgentProfiles(ctx)
	if err != nil {
		return repository.AgentProfile{}, nil, false, err
	}
	index := slices.IndexFunc(profiles, func(profile repository.AgentProfile) bool { return profile.ID == profileID })
	if index < 0 {
		return repository.AgentProfile{}, nil, false, nil
	}
	resolver, err := s.newResolver(ctx)
	if err != nil {
		return repository.AgentProfile{}, nil, false, err
	}
	resolved := resolver.resolve(profiles[index])
	if err := ctx.Err(); err != nil {
		return repository.AgentProfile{}, nil, false, err
	}
	return profiles[index], resolved, true, nil
}

// CancelRevokedDelegations is the maintenance tick's duty for revoked
// specialists. Disabling a profile, or granting it at another revision,
// writes only its setting row, which already keeps every claim off its queued
// tasks; this withdraws them, in transactions of at most
// repository.MaxRevokedDelegationBatch, until a batch comes back short. The
// single database connection is released between batches, so other work is
// claimed while a long backlog drains. Nothing marks progress: the fence is
// the cursor, so an interrupted pass resumes on the next tick.
func (s *Server) CancelRevokedDelegations(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, err := s.repo.CancelRevokedDelegations(ctx)
		if err != nil {
			return fmt.Errorf("withdraw tasks of revoked specialists: %w", err)
		}
		if s.events != nil {
			for _, event := range batch.Events {
				s.events.Publish(events.FromRepositoryEvent(event))
			}
		}
		// Withdrawing a turn's last task joins it; nothing else dispatches a
		// continuation no worker has been told about.
		if len(batch.ContinuationRunIDs) > 0 && s.routes != nil {
			if err := s.routes.DispatchPending(context.WithoutCancel(ctx)); err != nil {
				log.Printf("team: dispatch continuations after withdrawing revoked tasks: %v", err)
			}
		}
		if s.revocationBatchDone != nil {
			s.revocationBatchDone(len(batch.Cancelled))
		}
		if len(batch.Cancelled) < repository.MaxRevokedDelegationBatch {
			return nil
		}
	}
}
