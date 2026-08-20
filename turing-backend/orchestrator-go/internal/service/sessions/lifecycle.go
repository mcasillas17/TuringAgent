package sessions

import (
	"context"
	"errors"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	eventsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) RenameSession(
	ctx context.Context,
	req *turingv1.RenameSessionRequest,
) (*turingv1.RenameSessionResponse, error) {
	if req == nil || !validSessionID(req.SessionId) {
		return nil, status.Error(codes.InvalidArgument, "session_id is invalid")
	}
	if !utf8.ValidString(req.Title) {
		return nil, status.Error(codes.InvalidArgument, "title is invalid")
	}
	result, err := s.repo.RenameSession(ctx, req.SessionId, req.Title)
	if err != nil {
		return nil, lifecycleError(err, "rename session failed")
	}
	session, err := mapSession(result.Session)
	if err != nil {
		return nil, status.Error(codes.Internal, "rename session failed")
	}
	s.publishLifecycleEvent(result)
	return &turingv1.RenameSessionResponse{Session: session}, nil
}

func (s *Server) ArchiveSession(
	ctx context.Context,
	req *turingv1.ArchiveSessionRequest,
) (*turingv1.ArchiveSessionResponse, error) {
	if req == nil || !validSessionID(req.SessionId) {
		return nil, status.Error(codes.InvalidArgument, "session_id is invalid")
	}
	result, err := s.repo.ArchiveSession(ctx, req.SessionId)
	if err != nil {
		return nil, lifecycleError(err, "archive session failed")
	}
	session, err := mapSession(result.Session)
	if err != nil {
		return nil, status.Error(codes.Internal, "archive session failed")
	}
	s.publishLifecycleEvent(result)
	return &turingv1.ArchiveSessionResponse{Session: session}, nil
}

func (s *Server) RestoreSession(
	ctx context.Context,
	req *turingv1.RestoreSessionRequest,
) (*turingv1.RestoreSessionResponse, error) {
	if req == nil || !validSessionID(req.SessionId) {
		return nil, status.Error(codes.InvalidArgument, "session_id is invalid")
	}
	result, err := s.repo.RestoreSession(ctx, req.SessionId)
	if err != nil {
		return nil, lifecycleError(err, "restore session failed")
	}
	session, err := mapSession(result.Session)
	if err != nil {
		return nil, status.Error(codes.Internal, "restore session failed")
	}
	s.publishLifecycleEvent(result)
	return &turingv1.RestoreSessionResponse{Session: session}, nil
}

func (s *Server) publishLifecycleEvent(result repository.SessionMutationResult) {
	if !result.Changed || s.bus == nil {
		return
	}
	s.bus.Publish(eventsvc.FromRepositoryEvent(result.Event))
}

func lifecycleError(err error, internalMessage string) error {
	switch {
	case errors.Is(err, repository.ErrSessionNotFound):
		return status.Error(codes.NotFound, "session not found")
	case errors.Is(err, repository.ErrSessionDeleting):
		return status.Error(codes.FailedPrecondition, "session deletion is in progress")
	case errors.Is(err, repository.ErrInvalidSessionTitle):
		return status.Error(codes.InvalidArgument, "title is invalid")
	default:
		return status.Error(codes.Internal, internalMessage)
	}
}
