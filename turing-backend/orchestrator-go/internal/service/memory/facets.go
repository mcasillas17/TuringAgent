package memory

import (
	"context"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PublicServer is what a client talks to: read the vault, decide a proposal,
// turn memory on or off. It can never discover or run a memory tool.
type PublicServer struct {
	turingv1.UnimplementedMemoryServiceServer
	service *Server
}

// InternalServer is what the runtime talks to: discover the memory tools and
// dispatch one. It can never read the vault or decide anything on the user's
// behalf.
type InternalServer struct {
	turingv1.UnimplementedMemoryServiceServer
	service *Server
}

func NewPublicServer(service *Server) *PublicServer     { return &PublicServer{service: service} }
func NewInternalServer(service *Server) *InternalServer { return &InternalServer{service: service} }

func (s *PublicServer) ListMemoryState(ctx context.Context, req *turingv1.ListMemoryStateRequest) (*turingv1.ListMemoryStateResponse, error) {
	return s.service.ListMemoryState(ctx, req)
}
func (s *PublicServer) GetMemorySettings(ctx context.Context, req *turingv1.GetMemorySettingsRequest) (*turingv1.MemorySettings, error) {
	return s.service.GetMemorySettings(ctx, req)
}
func (s *PublicServer) SetMemoryEnabled(ctx context.Context, req *turingv1.SetMemoryEnabledRequest) (*turingv1.MemorySettings, error) {
	return s.service.SetMemoryEnabled(ctx, req)
}
func (s *PublicServer) ListMemoryCandidates(ctx context.Context, req *turingv1.ListMemoryCandidatesRequest) (*turingv1.ListMemoryCandidatesResponse, error) {
	return s.service.ListMemoryCandidates(ctx, req)
}
func (s *PublicServer) GetMemoryCandidate(ctx context.Context, req *turingv1.GetMemoryCandidateRequest) (*turingv1.MemoryCandidate, error) {
	return s.service.GetMemoryCandidate(ctx, req)
}
func (s *PublicServer) PromoteMemoryCandidate(ctx context.Context, req *turingv1.PromoteMemoryCandidateRequest) (*turingv1.PromoteMemoryCandidateResponse, error) {
	return s.service.PromoteMemoryCandidate(ctx, req)
}
func (s *PublicServer) RejectMemoryCandidate(ctx context.Context, req *turingv1.RejectMemoryCandidateRequest) (*turingv1.RejectMemoryCandidateResponse, error) {
	return s.service.RejectMemoryCandidate(ctx, req)
}
func (s *PublicServer) GetMemoryProfile(ctx context.Context, req *turingv1.GetMemoryProfileRequest) (*turingv1.MemoryProfile, error) {
	return s.service.GetMemoryProfile(ctx, req)
}
func (s *PublicServer) ApplyMemoryProfile(ctx context.Context, req *turingv1.ApplyMemoryProfileRequest) (*turingv1.ApplyMemoryProfileResponse, error) {
	return s.service.ApplyMemoryProfile(ctx, req)
}
func (s *PublicServer) GetMemoryPersona(ctx context.Context, req *turingv1.GetMemoryPersonaRequest) (*turingv1.MemoryPersona, error) {
	return s.service.GetMemoryPersona(ctx, req)
}
func (s *PublicServer) SaveMemoryPersona(ctx context.Context, req *turingv1.SaveMemoryPersonaRequest) (*turingv1.SaveMemoryPersonaResponse, error) {
	return s.service.SaveMemoryPersona(ctx, req)
}
func (s *PublicServer) SaveMemoryProfile(ctx context.Context, req *turingv1.SaveMemoryProfileRequest) (*turingv1.SaveMemoryProfileResponse, error) {
	return s.service.SaveMemoryProfile(ctx, req)
}
func (*PublicServer) ListMemoryTools(context.Context, *turingv1.ListMemoryToolsRequest) (*turingv1.ListMemoryToolsResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "memory tool discovery is internal")
}
func (*PublicServer) CallMemoryTool(context.Context, *turingv1.CallMemoryToolRequest) (*turingv1.CallMemoryToolResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "memory tool dispatch is internal")
}

// memoryManagementDenied is one sentence for every read or decision the runtime
// asks for. Reading the vault and deciding a proposal are the user's, and
// holding the internal token is not being the user.
func memoryManagementDenied() error {
	return status.Error(codes.PermissionDenied, "memory management is public")
}

func (*InternalServer) ListMemoryState(context.Context, *turingv1.ListMemoryStateRequest) (*turingv1.ListMemoryStateResponse, error) {
	return nil, memoryManagementDenied()
}
func (*InternalServer) GetMemorySettings(context.Context, *turingv1.GetMemorySettingsRequest) (*turingv1.MemorySettings, error) {
	return nil, memoryManagementDenied()
}
func (*InternalServer) SetMemoryEnabled(context.Context, *turingv1.SetMemoryEnabledRequest) (*turingv1.MemorySettings, error) {
	return nil, memoryManagementDenied()
}
func (*InternalServer) ListMemoryCandidates(context.Context, *turingv1.ListMemoryCandidatesRequest) (*turingv1.ListMemoryCandidatesResponse, error) {
	return nil, memoryManagementDenied()
}
func (*InternalServer) GetMemoryCandidate(context.Context, *turingv1.GetMemoryCandidateRequest) (*turingv1.MemoryCandidate, error) {
	return nil, memoryManagementDenied()
}
func (*InternalServer) PromoteMemoryCandidate(context.Context, *turingv1.PromoteMemoryCandidateRequest) (*turingv1.PromoteMemoryCandidateResponse, error) {
	return nil, memoryManagementDenied()
}
func (*InternalServer) RejectMemoryCandidate(context.Context, *turingv1.RejectMemoryCandidateRequest) (*turingv1.RejectMemoryCandidateResponse, error) {
	return nil, memoryManagementDenied()
}
func (*InternalServer) GetMemoryProfile(context.Context, *turingv1.GetMemoryProfileRequest) (*turingv1.MemoryProfile, error) {
	return nil, memoryManagementDenied()
}
func (*InternalServer) ApplyMemoryProfile(context.Context, *turingv1.ApplyMemoryProfileRequest) (*turingv1.ApplyMemoryProfileResponse, error) {
	return nil, memoryManagementDenied()
}

// The three below are the user writing their own documents, so the internal
// facet does not merely lack permission — it has no such authority to be
// granted. The persona in particular is the one file no agent path may write,
// and this is where that stops being a convention.
func (*InternalServer) GetMemoryPersona(context.Context, *turingv1.GetMemoryPersonaRequest) (*turingv1.MemoryPersona, error) {
	return nil, memoryManagementDenied()
}
func (*InternalServer) SaveMemoryPersona(context.Context, *turingv1.SaveMemoryPersonaRequest) (*turingv1.SaveMemoryPersonaResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "the persona is written by the user and by nobody else")
}
func (*InternalServer) SaveMemoryProfile(context.Context, *turingv1.SaveMemoryProfileRequest) (*turingv1.SaveMemoryProfileResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "a hand-written profile save is the user's; propose an edit instead")
}
func (s *InternalServer) ListMemoryTools(ctx context.Context, req *turingv1.ListMemoryToolsRequest) (*turingv1.ListMemoryToolsResponse, error) {
	return s.service.ListMemoryTools(ctx, req)
}
func (s *InternalServer) CallMemoryTool(ctx context.Context, req *turingv1.CallMemoryToolRequest) (*turingv1.CallMemoryToolResponse, error) {
	return s.service.CallMemoryTool(ctx, req)
}
