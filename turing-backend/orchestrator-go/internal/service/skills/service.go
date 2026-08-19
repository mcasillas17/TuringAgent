// Package skills serves the user's library of named instructions and the
// attachment of those instructions to individual conversations.
package skills

import (
	"context"
	"errors"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	turingv1.UnimplementedSkillServiceServer
	repo *repository.Repository
}

func New(repo *repository.Repository) *Server {
	return &Server{repo: repo}
}

func (s *Server) CreateSkill(ctx context.Context, req *turingv1.CreateSkillRequest) (*turingv1.Skill, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	skill, err := s.repo.CreateSkill(ctx, req.GetName(), req.GetInstructions())
	if err != nil {
		return nil, skillError(err, "create skill failed")
	}
	return toProto(skill), nil
}

func (s *Server) UpdateSkill(ctx context.Context, req *turingv1.UpdateSkillRequest) (*turingv1.Skill, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	skill, err := s.repo.UpdateSkill(ctx, req.GetSkillId(), req.GetName(), req.GetInstructions())
	if err != nil {
		return nil, skillError(err, "update skill failed")
	}
	return toProto(skill), nil
}

func (s *Server) DeleteSkill(ctx context.Context, req *turingv1.DeleteSkillRequest) (*turingv1.DeleteSkillResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.repo.DeleteSkill(ctx, req.GetSkillId()); err != nil {
		return nil, skillError(err, "delete skill failed")
	}
	return &turingv1.DeleteSkillResponse{}, nil
}

func (s *Server) ListSkills(ctx context.Context, _ *turingv1.ListSkillsRequest) (*turingv1.ListSkillsResponse, error) {
	skills, err := s.repo.ListSkills(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list skills failed")
	}
	return &turingv1.ListSkillsResponse{Skills: toProtoList(skills)}, nil
}

func (s *Server) AttachSkill(ctx context.Context, req *turingv1.AttachSkillRequest) (*turingv1.SessionSkillsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	skills, err := s.repo.AttachSkill(ctx, req.GetSessionId(), req.GetSkillId())
	if err != nil {
		return nil, skillError(err, "attach skill failed")
	}
	return &turingv1.SessionSkillsResponse{Skills: toProtoList(skills)}, nil
}

func (s *Server) DetachSkill(ctx context.Context, req *turingv1.DetachSkillRequest) (*turingv1.SessionSkillsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	skills, err := s.repo.DetachSkill(ctx, req.GetSessionId(), req.GetSkillId())
	if err != nil {
		return nil, skillError(err, "detach skill failed")
	}
	return &turingv1.SessionSkillsResponse{Skills: toProtoList(skills)}, nil
}

func (s *Server) ListSessionSkills(ctx context.Context, req *turingv1.ListSessionSkillsRequest) (*turingv1.SessionSkillsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	skills, err := s.repo.ListSessionSkills(ctx, req.GetSessionId())
	if err != nil {
		return nil, status.Error(codes.Internal, "list session skills failed")
	}
	return &turingv1.SessionSkillsResponse{Skills: toProtoList(skills)}, nil
}

// skillError maps the repository's named failures to codes a client can act
// on. Everything else collapses to Internal with a fixed message, so a storage
// error never leaks its text to the caller.
func skillError(err error, fallback string) error {
	switch {
	case errors.Is(err, repository.ErrSkillNotFound):
		return status.Error(codes.NotFound, "skill not found")
	case errors.Is(err, repository.ErrSessionNotFound):
		return status.Error(codes.NotFound, "conversation not found")
	case errors.Is(err, repository.ErrSkillNameTooLong):
		return status.Error(codes.InvalidArgument, "skill name is too long")
	case errors.Is(err, repository.ErrSkillInstructionsLong):
		return status.Error(codes.InvalidArgument, "skill instructions are too long")
	case errors.Is(err, repository.ErrSkillNotAttached):
		return status.Error(codes.NotFound, "skill is not attached to that conversation")
	case errors.Is(err, repository.ErrSkillNameTaken):
		return status.Error(codes.AlreadyExists, "a skill with that name already exists")
	case errors.Is(err, repository.ErrSkillNameEmpty):
		return status.Error(codes.InvalidArgument, "skill name is required")
	case errors.Is(err, repository.ErrSkillNoContent):
		return status.Error(codes.InvalidArgument, "skill instructions are required")
	default:
		return status.Error(codes.Internal, fallback)
	}
}

func toProtoList(skills []repository.Skill) []*turingv1.Skill {
	converted := make([]*turingv1.Skill, 0, len(skills))
	for _, skill := range skills {
		converted = append(converted, toProto(skill))
	}
	return converted
}

func toProto(skill repository.Skill) *turingv1.Skill {
	return &turingv1.Skill{
		SkillId:      skill.SkillID,
		Name:         skill.Name,
		Instructions: skill.Instructions,
		CreatedAt:    parseTimestamp(skill.CreatedAt),
		UpdatedAt:    parseTimestamp(skill.UpdatedAt),
	}
}

func parseTimestamp(value string) *timestamppb.Timestamp {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return timestamppb.New(parsed)
}
