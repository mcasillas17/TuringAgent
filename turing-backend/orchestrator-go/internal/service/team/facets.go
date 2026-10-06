package team

import (
	"context"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PublicServer is what a client talks to: list, enable and grant profiles.
// It can never discover the team tool a run is offered.
type PublicServer struct {
	turingv1.UnimplementedTeamServiceServer
	service *Server
}

// InternalServer is what the runtime talks to: the team tool of the run it
// is executing, and delegating through it. It can never read, enable or grant
// a profile.
type InternalServer struct {
	turingv1.UnimplementedTeamServiceServer
	service *Server
}

func NewPublicServer(service *Server) *PublicServer     { return &PublicServer{service: service} }
func NewInternalServer(service *Server) *InternalServer { return &InternalServer{service: service} }

func (s *PublicServer) ListAgentProfiles(ctx context.Context, req *turingv1.ListAgentProfilesRequest) (*turingv1.ListAgentProfilesResponse, error) {
	return s.service.ListAgentProfiles(ctx, req)
}
func (s *PublicServer) SetAgentProfileEnabled(ctx context.Context, req *turingv1.SetAgentProfileEnabledRequest) (*turingv1.AgentProfile, error) {
	return s.service.SetAgentProfileEnabled(ctx, req)
}
func (s *PublicServer) GrantAgentProfile(ctx context.Context, req *turingv1.GrantAgentProfileRequest) (*turingv1.AgentProfile, error) {
	return s.service.GrantAgentProfile(ctx, req)
}
func (*PublicServer) ListTeamTools(context.Context, *turingv1.ListTeamToolsRequest) (*turingv1.ListTeamToolsResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "team tool discovery is internal")
}
func (*PublicServer) CallTeamTool(context.Context, *turingv1.CallTeamToolRequest) (*turingv1.CallTeamToolResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "team tool dispatch is internal")
}

// teamManagementDenied answers every profile decision the runtime asks for:
// enabling and granting a specialist are the user's, and holding the
// internal token is not being the user.
func teamManagementDenied() error {
	return status.Error(codes.PermissionDenied, "team management is public")
}

func (*InternalServer) ListAgentProfiles(context.Context, *turingv1.ListAgentProfilesRequest) (*turingv1.ListAgentProfilesResponse, error) {
	return nil, teamManagementDenied()
}
func (*InternalServer) SetAgentProfileEnabled(context.Context, *turingv1.SetAgentProfileEnabledRequest) (*turingv1.AgentProfile, error) {
	return nil, teamManagementDenied()
}
func (*InternalServer) GrantAgentProfile(context.Context, *turingv1.GrantAgentProfileRequest) (*turingv1.AgentProfile, error) {
	return nil, teamManagementDenied()
}
func (s *InternalServer) ListTeamTools(ctx context.Context, req *turingv1.ListTeamToolsRequest) (*turingv1.ListTeamToolsResponse, error) {
	return s.service.ListTeamTools(ctx, req)
}
func (s *InternalServer) CallTeamTool(ctx context.Context, req *turingv1.CallTeamToolRequest) (*turingv1.CallTeamToolResponse, error) {
	return s.service.CallTeamTool(ctx, req)
}
