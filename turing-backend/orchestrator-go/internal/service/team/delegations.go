package team

import (
	"context"
	"errors"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ListDelegations is every task a conversation delegated, as their cards
// show them.
func (s *Server) ListDelegations(ctx context.Context, req *turingv1.ListDelegationsRequest) (*turingv1.ListDelegationsResponse, error) {
	sessionID, err := requireField("parent_session_id", req.GetParentSessionId())
	if err != nil {
		return nil, err
	}
	records, err := s.repo.ListDelegations(ctx, sessionID)
	if errors.Is(err, repository.ErrSessionNotFound) {
		return nil, status.Error(codes.NotFound, "conversation not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "list delegations failed")
	}
	response := &turingv1.ListDelegationsResponse{Delegations: make([]*turingv1.Delegation, 0, len(records))}
	for _, record := range records {
		delegation, err := delegationProto(record)
		if err != nil {
			return nil, status.Error(codes.Internal, "list delegations failed")
		}
		response.Delegations = append(response.Delegations, delegation)
	}
	return response, nil
}

// GetDelegation is one delegation, as its card shows it.
func (s *Server) GetDelegation(ctx context.Context, req *turingv1.GetDelegationRequest) (*turingv1.Delegation, error) {
	delegationID, err := requireField("delegation_id", req.GetDelegationId())
	if err != nil {
		return nil, err
	}
	record, err := s.repo.GetDelegation(ctx, delegationID)
	if errors.Is(err, repository.ErrDelegationNotFound) {
		return nil, status.Error(codes.NotFound, "delegation not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "read delegation failed")
	}
	delegation, err := delegationProto(record)
	if err != nil {
		return nil, status.Error(codes.Internal, "read delegation failed")
	}
	return delegation, nil
}

func delegationProto(record repository.DelegationRecord) (*turingv1.Delegation, error) {
	createdAt, err := persisttime.ParseCanonical(record.CreatedAt)
	if err != nil {
		return nil, err
	}
	delegation := &turingv1.Delegation{
		DelegationId:      record.ID,
		ParentSessionId:   record.ParentSessionID,
		ParentRunId:       record.ParentRunID,
		ChildSessionId:    record.ChildSessionID,
		ChildRunId:        record.ChildRunID,
		ProfileId:         record.ProfileID,
		DisplayName:       record.DisplayName,
		Emoji:             record.Emoji,
		Task:              record.Task,
		State:             record.State,
		ApprovalId:        record.ApprovalID,
		ErrorCode:         record.ErrorCode,
		Joined:            record.Joined,
		ContinuationRunId: record.ContinuationRunID,
		CreatedAt:         timestamppb.New(createdAt),
		EgressTools:       record.EgressTools,
		EgressConsent:     record.EgressConsent,
		EgressRequired:    record.EgressRequired,
		Summary:           record.Summary,
	}
	if record.FinishedAt != "" {
		finishedAt, err := persisttime.ParseCanonical(record.FinishedAt)
		if err != nil {
			return nil, err
		}
		delegation.FinishedAt = timestamppb.New(finishedAt)
	}
	return delegation, nil
}
