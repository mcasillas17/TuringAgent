package chat

import (
	"context"
	"database/sql"
	"errors"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runstate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) CancelRun(ctx context.Context, req *turingv1.CancelRunRequest) (*turingv1.CancelRunResponse, error) {
	if req == nil || req.SessionId == "" || req.RunId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id and run_id are required")
	}
	result, err := s.repo.CancelUserRun(ctx, req.SessionId, req.RunId, req.IdempotencyKey)
	if cancellationUnavailable(err) {
		return &turingv1.CancelRunResponse{Result: turingv1.CancelRunResult_CANCEL_RUN_RESULT_UNAVAILABLE}, nil
	}
	if err != nil {
		return nil, cancellationError(err)
	}
	for _, event := range result.Events {
		s.bus.Publish(busEventFromRepository(event))
	}
	// Acceptance is already durable. Neither a failed send nor a lost response
	// rolls it back; reconciliation can resend the same terminal command.
	if result.Accepted && s.runtime != nil {
		notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		s.runtime.CancelRun(notifyCtx, req.RunId, runoutcome.CodeUserCancelled)
		cancel()
	}
	current, err := s.repo.ReadRunCancellation(ctx, req.SessionId, req.RunId)
	if cancellationUnavailable(err) {
		return &turingv1.CancelRunResponse{Result: turingv1.CancelRunResult_CANCEL_RUN_RESULT_UNAVAILABLE}, nil
	}
	if err != nil {
		return nil, cancellationError(err)
	}
	outcome := turingv1.CancelRunResult_CANCEL_RUN_RESULT_ALREADY_TERMINAL
	if result.Accepted {
		outcome = turingv1.CancelRunResult_CANCEL_RUN_RESULT_ACCEPTED
	}
	return &turingv1.CancelRunResponse{Result: outcome, RunState: runstate.Project(result.State), Progress: cancellationProgress(current)}, nil
}

func (s *Server) GetRunCancellation(ctx context.Context, req *turingv1.GetRunCancellationRequest) (*turingv1.GetRunCancellationResponse, error) {
	if req == nil || req.SessionId == "" || req.RunId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id and run_id are required")
	}
	current, err := s.repo.ReadRunCancellation(ctx, req.SessionId, req.RunId)
	if cancellationUnavailable(err) {
		return &turingv1.GetRunCancellationResponse{}, nil
	}
	if err != nil {
		return nil, cancellationError(err)
	}
	return &turingv1.GetRunCancellationResponse{
		Available: true, RunState: runstate.Project(current.State), Progress: cancellationProgress(current),
	}, nil
}

func cancellationUnavailable(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrSessionNotFound) || errors.Is(err, repository.ErrSessionDeleting)
}

func cancellationError(err error) error {
	switch {
	case errors.Is(err, repository.ErrCancelKeyInvalid):
		return status.Error(codes.InvalidArgument, "invalid idempotency_key")
	case errors.Is(err, repository.ErrCancelKeyConflict):
		return status.Error(codes.AlreadyExists, "idempotency_key already used")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	default:
		return status.Error(codes.Internal, "cancellation unavailable")
	}
}

func cancellationProgress(current repository.RunCancellation) turingv1.CancellationProgress {
	if current.State.Lifecycle != "cancelled" {
		return turingv1.CancellationProgress_CANCELLATION_PROGRESS_NOT_CANCELLED
	}
	if current.ExecutionActive {
		return turingv1.CancellationProgress_CANCELLATION_PROGRESS_STOPPING
	}
	return turingv1.CancellationProgress_CANCELLATION_PROGRESS_RECONCILED
}
