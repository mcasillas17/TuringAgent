package events

import (
	"context"
	"errors"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/safejson"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	turingv1.UnimplementedEventServiceServer
	repo *repository.Repository
	bus  *Bus
}

func NewServer(repo *repository.Repository, bus *Bus) *Server {
	return &Server{repo: repo, bus: bus}
}

func (s *Server) ListEvents(ctx context.Context, req *turingv1.ListEventsRequest) (*turingv1.ListEventsResponse, error) {
	if req == nil || req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.AfterSequence < 0 {
		return nil, status.Error(codes.InvalidArgument, "after_sequence must be non-negative")
	}
	limit := int(req.Limit)
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	events, latest, err := s.repo.ReplayEvents(ctx, req.SessionId, req.AfterSequence, limit)
	if err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			return nil, status.Error(codes.NotFound, "session not found")
		}
		return nil, status.Error(codes.Internal, "list events failed")
	}
	out := make([]*turingv1.TuringEvent, 0, len(events))
	for _, event := range events {
		out = append(out, mapEvent(event))
	}
	return &turingv1.ListEventsResponse{Events: out, LatestSequence: latest, ResyncRequired: req.AfterSequence > latest}, nil
}

func (s *Server) SubscribeSessionEvents(req *turingv1.SubscribeSessionEventsRequest, stream turingv1.EventService_SubscribeSessionEventsServer) error {
	if req == nil || req.SessionId == "" {
		return status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.AfterSequence < 0 {
		return status.Error(codes.InvalidArgument, "after_sequence must be non-negative")
	}
	ctx := stream.Context()
	_, latest, err := s.repo.ReplayEvents(ctx, req.SessionId, req.AfterSequence, 1)
	if err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			return status.Error(codes.NotFound, "session not found")
		}
		return status.Error(codes.Internal, "replay events failed")
	}
	if req.AfterSequence > latest {
		return status.Error(codes.FailedPrecondition, "resync required")
	}
	lastSent := req.AfterSequence
	if err := s.replayAvailable(ctx, req.SessionId, &lastSent, stream); err != nil {
		var sendErr *eventStreamSendError
		if errors.As(err, &sendErr) {
			return sendErr.err
		}
		if errors.Is(err, repository.ErrSessionNotFound) {
			return status.Error(codes.NotFound, "session not found")
		}
		return status.Error(codes.Internal, "replay events failed")
	}
	ch, unsubscribe := s.bus.Subscribe(req.SessionId)
	defer unsubscribe()
	if err := s.replayAvailable(ctx, req.SessionId, &lastSent, stream); err != nil {
		var sendErr *eventStreamSendError
		if errors.As(err, &sendErr) {
			return sendErr.err
		}
		if errors.Is(err, repository.ErrSessionNotFound) {
			return status.Error(codes.NotFound, "session not found")
		}
		return status.Error(codes.Internal, "replay events failed")
	}
	for {
		select {
		case <-ctx.Done():
			return status.Error(codes.Canceled, "client cancelled event stream")
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			if event.Sequence <= lastSent {
				continue
			}
			if event.Type == "session.deleted" {
				if err := stream.Send(mapBusEvent(event)); err != nil {
					return err
				}
				return status.Error(codes.NotFound, "session deleted")
			}
			if event.Sequence > lastSent+1 {
				if err := s.replayAvailable(ctx, req.SessionId, &lastSent, stream); err != nil {
					var sendErr *eventStreamSendError
					if errors.As(err, &sendErr) {
						return sendErr.err
					}
					if ctx.Err() != nil {
						return status.Error(codes.Canceled, "client cancelled event stream")
					}
					return status.Error(codes.Internal, "replay events failed")
				}
				if event.Sequence <= lastSent {
					continue
				}
			}
			if err := stream.Send(mapBusEvent(event)); err != nil {
				return err
			}
			lastSent = event.Sequence
		}
	}
}

func (s *Server) SubscribeSessionUpdates(_ *turingv1.SubscribeSessionUpdatesRequest, stream turingv1.EventService_SubscribeSessionUpdatesServer) error {
	ctx := stream.Context()
	ch, unsubscribe := s.bus.SubscribeSessionUpdates()
	defer unsubscribe()

	snapshots, err := s.repo.ListLatestSessionUpdatedEvents(ctx, 50)
	if err != nil {
		return status.Error(codes.Internal, "list session updates failed")
	}
	for _, event := range snapshots {
		if err := stream.Send(mapEvent(event)); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return status.Error(codes.Canceled, "client cancelled session update stream")
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			if event.Type != "session.updated" && event.Type != "session.deleted" {
				continue
			}
			if err := stream.Send(mapBusEvent(event)); err != nil {
				return err
			}
		}
	}
}

func (s *Server) replayAvailable(ctx context.Context, sessionID string, lastSent *int64, stream turingv1.EventService_SubscribeSessionEventsServer) error {
	const replayLimit = 500
	for {
		replayed, _, err := s.repo.ReplayEvents(ctx, sessionID, *lastSent, replayLimit)
		if err != nil {
			return err
		}
		for _, event := range replayed {
			if event.Sequence <= *lastSent {
				continue
			}
			if err := stream.Send(mapEvent(event)); err != nil {
				return &eventStreamSendError{err: err}
			}
			*lastSent = event.Sequence
		}
		if len(replayed) < replayLimit {
			return nil
		}
	}
}

type eventStreamSendError struct {
	err error
}

func (e *eventStreamSendError) Error() string {
	return e.err.Error()
}

func (e *eventStreamSendError) Unwrap() error {
	return e.err
}

func mapEvent(event repository.Event) *turingv1.TuringEvent {
	runID := ""
	if event.RunID.Valid {
		runID = event.RunID.String
	}
	safe := Decode(event.Type, runID, event.PayloadJSON)
	return &turingv1.TuringEvent{
		EventId:   event.EventID,
		SessionId: event.SessionID,
		RunId:     runID,
		TraceId:   event.TraceID,
		Sequence:  event.Sequence,
		Type:      MapEventType(event.Type),
		CreatedAt: parseEventTimestamp(event.CreatedAt),
		Payload:   mapPayload(safe.Payload),
		RunState:  safe.RunState,
	}
}

// mapBusEvent is the live half of the same projection. It decodes through the
// same function as the replay above rather than a parallel one: a client that
// watches a run live and a client that reopens it later are looking at the same
// durable row, and the whole point of this feature is that they agree.
func mapBusEvent(event Event) *turingv1.TuringEvent {
	safe := Decode(event.Type, event.RunID, event.PayloadJSON)
	return &turingv1.TuringEvent{
		EventId:   event.EventID,
		SessionId: event.SessionID,
		RunId:     event.RunID,
		TraceId:   event.TraceID,
		Sequence:  event.Sequence,
		Type:      MapEventType(event.Type),
		CreatedAt: parseEventTimestamp(event.CreatedAt),
		Payload:   mapPayload(safe.Payload),
		RunState:  safe.RunState,
	}
}

// mapPayload renders an already-allowlisted payload. A value that cannot be
// converted yields an empty struct rather than an error message: by this point
// the payload has been through the shared decoder, and there is nothing left
// worth telling a client except what the event was.
func mapPayload(payload map[string]any) *structpb.Struct {
	converted, err := safejson.ToStruct(payload)
	if err != nil {
		return &structpb.Struct{Fields: map[string]*structpb.Value{}}
	}
	return converted
}

func parseEventTimestamp(value string) *timestamppb.Timestamp {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return timestamppb.New(t)
}
