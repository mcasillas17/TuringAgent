package events

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
		return status.Error(codes.Internal, "replay events failed")
	}
	ch, unsubscribe := s.bus.Subscribe(req.SessionId)
	defer unsubscribe()
	if err := s.replayAvailable(ctx, req.SessionId, &lastSent, stream); err != nil {
		var sendErr *eventStreamSendError
		if errors.As(err, &sendErr) {
			return sendErr.err
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
			if event.Type != "session.updated" {
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
	return &turingv1.TuringEvent{
		EventId:   event.EventID,
		SessionId: event.SessionID,
		RunId:     runID,
		TraceId:   event.TraceID,
		Sequence:  event.Sequence,
		Type:      mapEventType(event.Type),
		CreatedAt: parseEventTimestamp(event.CreatedAt),
		Payload:   mapPayload(event.PayloadJSON),
	}
}

func mapBusEvent(event Event) *turingv1.TuringEvent {
	return &turingv1.TuringEvent{
		EventId:   event.EventID,
		SessionId: event.SessionID,
		RunId:     event.RunID,
		TraceId:   event.TraceID,
		Sequence:  event.Sequence,
		Type:      mapEventType(event.Type),
		CreatedAt: parseEventTimestamp(event.CreatedAt),
		Payload:   mapPayload(event.PayloadJSON),
	}
}

func mapEventType(value string) turingv1.TuringEventType {
	normalized := strings.ToLower(value)
	normalized = strings.TrimPrefix(normalized, "turing_event_type_")
	normalized = strings.ReplaceAll(normalized, "_", ".")
	switch normalized {
	case "message.started":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED
	case "message.delta":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA
	case "message.completed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED
	case "agent.run.queued":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_QUEUED
	case "agent.run.started":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STARTED
	case "agent.run.step":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP
	case "agent.run.completed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_COMPLETED
	case "agent.run.failed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED
	case "agent.run.cancelled":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_CANCELLED
	case "tool.call.started":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED
	case "tool.call.completed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED
	case "tool.call.failed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED
	case "tool.call.denied":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED
	case "approval.requested":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED
	case "approval.approved":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_APPROVED
	case "approval.denied":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED
	case "approval.expired":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_EXPIRED
	case "approval.consumed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_CONSUMED
	case "error":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_ERROR
	case "system":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_SYSTEM
	case "session.updated":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_UPDATED
	default:
		return turingv1.TuringEventType_TURING_EVENT_TYPE_UNSPECIFIED
	}
}

func mapPayload(payloadJSON string) *structpb.Struct {
	if payloadJSON == "" {
		return &structpb.Struct{Fields: map[string]*structpb.Value{}}
	}
	value, err := safejson.DecodeObject(json.NewDecoder(strings.NewReader(payloadJSON)))
	if err != nil {
		return &structpb.Struct{Fields: map[string]*structpb.Value{}}
	}
	payload, err := safejson.ToStruct(value)
	if err != nil {
		return &structpb.Struct{Fields: map[string]*structpb.Value{}}
	}
	return payload
}

func parseEventTimestamp(value string) *timestamppb.Timestamp {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return timestamppb.New(t)
}
