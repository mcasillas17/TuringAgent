// Package skills exposes the filesystem skill library and the user's separate
// enablement and per-capability consent decisions.
package skills

import (
	"context"
	"errors"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	turingv1.UnimplementedSkillServiceServer
	repo *repository.Repository
}

func New(repo *repository.Repository) *Server {
	return &Server{repo: repo}
}

func (s *Server) ListSkills(ctx context.Context, _ *turingv1.ListSkillsRequest) (*turingv1.ListSkillsResponse, error) {
	skills, err := s.repo.ListSkills(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list skills failed")
	}
	return &turingv1.ListSkillsResponse{Skills: toProtoList(skills)}, nil
}

func (s *Server) GetSkill(ctx context.Context, req *turingv1.GetSkillRequest) (*turingv1.Skill, error) {
	if req == nil || strings.TrimSpace(req.GetSkillId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "skill id is required")
	}
	skill, err := s.repo.GetSkill(ctx, req.GetSkillId())
	if err != nil {
		return nil, skillError(err, "get skill failed")
	}
	return toProto(skill), nil
}

func (s *Server) SetSkillEnabled(ctx context.Context, req *turingv1.SetSkillEnabledRequest) (*turingv1.Skill, error) {
	if req == nil || strings.TrimSpace(req.GetSkillId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "skill id is required")
	}
	skill, err := s.repo.SetSkillEnabled(ctx, req.GetSkillId(), req.GetEnabled())
	if err != nil {
		return nil, skillError(err, "set skill enabled failed")
	}
	return toProto(skill), nil
}

func (s *Server) SetSkillCapabilityGrant(ctx context.Context, req *turingv1.SetSkillCapabilityGrantRequest) (*turingv1.Skill, error) {
	if req == nil || strings.TrimSpace(req.GetSkillId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "skill id is required")
	}
	if strings.TrimSpace(req.GetCapability()) == "" {
		return nil, status.Error(codes.InvalidArgument, "capability is required")
	}
	skill, err := s.repo.SetSkillGrant(ctx, req.GetSkillId(), req.GetCapability(), req.GetGranted())
	if err != nil {
		return nil, skillError(err, "set skill capability grant failed")
	}
	return toProto(skill), nil
}

func skillError(err error, fallback string) error {
	switch {
	case errors.Is(err, repository.ErrSkillNotFound):
		return status.Error(codes.NotFound, "skill not found")
	case errors.Is(err, repository.ErrSkillCapabilityNotDeclared):
		return status.Error(codes.InvalidArgument, "skill does not declare that capability")
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
		SkillId:             skill.SkillID,
		Name:                skill.Name,
		Description:         skill.Description,
		Body:                skill.Body,
		Category:            skill.Category,
		Version:             skill.Version,
		Author:              skill.Author,
		License:             skill.License,
		Requires:            append([]string(nil), skill.Requires...),
		GrantedCapabilities: append([]string(nil), skill.GrantedCapabilities...),
		MissingCapabilities: append([]string(nil), skill.MissingCapabilities...),
		Enabled:             skill.Enabled,
		ParseError:          skill.ParseError,
		FolderPath:          skill.FolderPath,
	}
}
