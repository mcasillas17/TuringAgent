package automations

import (
	"context"
	"errors"
	"fmt"
	"log"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// EventPublisher is the event bus, narrowed to publishing what the quiet
// drain's cancellations committed.
type EventPublisher interface {
	Publish(events.Event)
}

// SetEventPublisher wires where the drain's cancellations are published.
func (s *Server) SetEventPublisher(publisher EventPublisher) {
	s.events = publisher
}

// PendingDispatcher is the runtime, narrowed to telling workers there is
// work.
type PendingDispatcher interface {
	DispatchPending(context.Context) error
}

// SetDispatcher wires what dispatches pending work after the quiet switch
// changes what is claimable.
func (s *Server) SetDispatcher(dispatcher PendingDispatcher) {
	s.dispatcher = dispatcher
}

// dispatchPending tells workers there may be work. A failure is logged, not
// returned: whatever was made claimable stays queued for the next dispatch.
func (s *Server) dispatchPending(ctx context.Context, after string) {
	if s.dispatcher == nil {
		return
	}
	if err := s.dispatcher.DispatchPending(context.WithoutCancel(ctx)); err != nil {
		log.Printf("automations: dispatch after %s: %v", after, err)
	}
}

// SetQuietDrainTrigger wires what starts the drain once turning quiet on has
// committed: the maintenance loop's kick, so the drain runs on the one
// goroutine every background duty runs on.
func (s *Server) SetQuietDrainTrigger(trigger func()) {
	s.quietDrainTrigger = trigger
}

// GetUnattendedQuiet reads the owner's quiet switch (TUR-028a).
func (s *Server) GetUnattendedQuiet(ctx context.Context, _ *turingv1.GetUnattendedQuietRequest) (*turingv1.UnattendedQuiet, error) {
	setting, err := s.repo.GetUnattendedQuiet(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "read the quiet setting failed")
	}
	return unattendedQuietToProto(setting), nil
}

// SetUnattendedQuiet sets the owner's quiet switch, compare-and-set on the
// revision the caller read. A stale revision writes nothing and answers with
// the current setting, so the app shows that instead. Turning quiet on starts
// the drain once the switch has committed. Turning it off fires nothing, but
// dispatches what is already claimable, since nothing else may.
func (s *Server) SetUnattendedQuiet(ctx context.Context, req *turingv1.SetUnattendedQuietRequest) (*turingv1.SetUnattendedQuietResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	setting, changed, err := s.repo.SetUnattendedQuiet(ctx, req.GetQuiet(), req.GetRevision())
	switch {
	case errors.Is(err, repository.ErrUnattendedQuietStale):
		return &turingv1.SetUnattendedQuietResponse{Setting: unattendedQuietToProto(setting), Stale: true}, nil
	case err != nil:
		return nil, status.Error(codes.Internal, "set the quiet setting failed")
	}
	switch {
	case changed && setting.Quiet && s.quietDrainTrigger != nil:
		s.quietDrainTrigger()
	case changed && !setting.Quiet:
		s.dispatchPending(ctx, "turning quiet off")
	}
	return &turingv1.SetUnattendedQuietResponse{Setting: unattendedQuietToProto(setting)}, nil
}

// DrainUnattendedQuiet is the maintenance tick's duty for the quiet switch.
// It runs drain batches, each its own transaction of at most
// repository.MaxQuietDrainBatch cancellations, until one finds nothing left
// and clears the drain, publishing each batch's cancellations. The single
// database connection is released between batches, so attended work is
// claimed while a long backlog drains, and a drain cut short resumes on the
// next tick because its key is durable. A batch that cancelled something
// dispatches pending work: a cancelled run may have been the one an owner's
// turn in its conversation was waiting behind. With no drain pending it
// costs one read.
func (s *Server) DrainUnattendedQuiet(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, err := s.repo.DrainUnattendedQuiet(ctx)
		if err != nil {
			return fmt.Errorf("drain unattended work: %w", err)
		}
		if s.events != nil {
			for _, event := range batch.Events {
				s.events.Publish(events.FromRepositoryEvent(event))
			}
		}
		if len(batch.Cancelled) > 0 {
			s.dispatchPending(ctx, "a quiet drain batch")
		}
		if s.quietDrainBatchDone != nil {
			s.quietDrainBatchDone(len(batch.Cancelled))
		}
		if batch.Done {
			return nil
		}
	}
}

func unattendedQuietToProto(setting repository.UnattendedQuiet) *turingv1.UnattendedQuiet {
	return &turingv1.UnattendedQuiet{
		Quiet:       setting.Quiet,
		Revision:    setting.Revision,
		Unavailable: setting.Unavailable,
	}
}
