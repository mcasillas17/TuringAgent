package integrations

import (
	"context"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type PublicServer struct {
	turingv1.UnimplementedIntegrationServiceServer
	service *Server
}

type InternalServer struct {
	turingv1.UnimplementedIntegrationServiceServer
	service *Server
}

func NewPublicServer(service *Server) *PublicServer     { return &PublicServer{service: service} }
func NewInternalServer(service *Server) *InternalServer { return &InternalServer{service: service} }

func (s *PublicServer) ListProviders(ctx context.Context, req *turingv1.ListProvidersRequest) (*turingv1.ListProvidersResponse, error) {
	return s.service.ListProviders(ctx, req)
}
func (s *PublicServer) ConnectAccount(ctx context.Context, req *turingv1.ConnectAccountRequest) (*turingv1.Connection, error) {
	return s.service.ConnectAccount(ctx, req)
}
func (s *PublicServer) ListConnections(ctx context.Context, req *turingv1.ListConnectionsRequest) (*turingv1.ListConnectionsResponse, error) {
	return s.service.ListConnections(ctx, req)
}
func (s *PublicServer) GetConnection(ctx context.Context, req *turingv1.GetConnectionRequest) (*turingv1.Connection, error) {
	return s.service.GetConnection(ctx, req)
}
func (s *PublicServer) RevokeConnection(ctx context.Context, req *turingv1.RevokeConnectionRequest) (*turingv1.Connection, error) {
	return s.service.RevokeConnection(ctx, req)
}
func (s *PublicServer) DeleteConnection(ctx context.Context, req *turingv1.DeleteConnectionRequest) (*turingv1.DeleteConnectionResponse, error) {
	return s.service.DeleteConnection(ctx, req)
}
func (*PublicServer) ListIntegrationTools(context.Context, *turingv1.ListIntegrationToolsRequest) (*turingv1.ListIntegrationToolsResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "integration tool discovery is internal")
}
func (*PublicServer) CallIntegrationTool(context.Context, *turingv1.CallIntegrationToolRequest) (*turingv1.CallIntegrationToolResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "integration tool dispatch is internal")
}

func integrationManagementDenied() error {
	return status.Error(codes.PermissionDenied, "integration management is public")
}
func (*InternalServer) ListProviders(context.Context, *turingv1.ListProvidersRequest) (*turingv1.ListProvidersResponse, error) {
	return nil, integrationManagementDenied()
}
func (*InternalServer) ConnectAccount(context.Context, *turingv1.ConnectAccountRequest) (*turingv1.Connection, error) {
	return nil, integrationManagementDenied()
}
func (*InternalServer) ListConnections(context.Context, *turingv1.ListConnectionsRequest) (*turingv1.ListConnectionsResponse, error) {
	return nil, integrationManagementDenied()
}
func (*InternalServer) GetConnection(context.Context, *turingv1.GetConnectionRequest) (*turingv1.Connection, error) {
	return nil, integrationManagementDenied()
}
func (*InternalServer) RevokeConnection(context.Context, *turingv1.RevokeConnectionRequest) (*turingv1.Connection, error) {
	return nil, integrationManagementDenied()
}
func (*InternalServer) DeleteConnection(context.Context, *turingv1.DeleteConnectionRequest) (*turingv1.DeleteConnectionResponse, error) {
	return nil, integrationManagementDenied()
}
func (s *InternalServer) ListIntegrationTools(ctx context.Context, req *turingv1.ListIntegrationToolsRequest) (*turingv1.ListIntegrationToolsResponse, error) {
	return s.service.ListIntegrationTools(ctx, req)
}
func (s *InternalServer) CallIntegrationTool(ctx context.Context, req *turingv1.CallIntegrationToolRequest) (*turingv1.CallIntegrationToolResponse, error) {
	return s.service.CallIntegrationTool(ctx, req)
}
