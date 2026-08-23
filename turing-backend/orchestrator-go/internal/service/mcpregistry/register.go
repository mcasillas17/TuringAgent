package mcpregistry

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
)

// RegisterMcpServer registers one server through the same validation path as
// mcp.json import: name pattern and reserved names, URL classification (the
// tier is derived, never caller-chosen), token sealing. The server arrives
// disabled, as always. The one deliberate difference from file import: an
// explicit register CLEARS the deletion tombstone first — the user asking for
// the name by hand is exactly the consent the tombstone was waiting for,
// while file re-import must never resurrect a deletion.
func (s *Server) RegisterMcpServer(ctx context.Context, req *turingv1.RegisterMcpServerRequest) (*turingv1.McpServerDescriptor, error) {
	name := strings.TrimSpace(req.GetName())
	if !mcpServerNamePattern.MatchString(name) {
		return nil, status.Error(codes.InvalidArgument, "server name is invalid")
	}
	if name == "system" || name == "files" || name == "skills" || name == "integrations" {
		return nil, status.Error(codes.InvalidArgument, "server name is reserved by TuringAgent")
	}
	tier, canonicalURL, err := classifyImportedURL(strings.TrimSpace(req.GetUrl()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	var sealed []byte
	if token := req.GetBearerToken(); token != "" {
		sealed, err = s.sealer.Seal([]byte(token), []byte(name))
		if err != nil {
			if errors.Is(err, secretbox.ErrNoKey) {
				return nil, status.Error(codes.FailedPrecondition, "server token requires the TURING_INTEGRATION_KEY integration key so it can be stored sealed")
			}
			return nil, status.Error(codes.Internal, "seal MCP server token")
		}
	}
	if err := s.repo.ClearMCPImportTombstone(ctx, name); err != nil {
		return nil, status.Error(codes.Internal, "register MCP server failed")
	}
	server, err := s.repo.UpsertImportedMCPServer(ctx, repository.ImportedMCPServer{
		Name:        name,
		URL:         canonicalURL,
		SealedToken: sealed,
		Tier:        tier,
	})
	if err != nil {
		if errors.Is(err, repository.ErrMCPServerBundled) {
			return nil, status.Error(codes.FailedPrecondition, "bundled server registration is managed by TuringAgent")
		}
		return nil, status.Error(codes.Internal, "register MCP server failed")
	}
	s.notifyRegistryChanged()
	return s.serverDescriptor(ctx, server)
}

// ReimportMcpJson re-runs the startup import on demand. ImportJSON is already
// idempotent against user intent (enablement preserved unless URL/tier
// changed, tool policies preserved, tombstones honoured), so re-import cannot
// flip a decision the user made — only pick up file edits without a restart.
func (s *Server) ReimportMcpJson(ctx context.Context, _ *turingv1.ReimportMcpJsonRequest) (*turingv1.ReimportMcpJsonResponse, error) {
	if s.mcpConfigPath == "" {
		return nil, status.Error(codes.FailedPrecondition, "no mcp.json is mounted; set MCP_CONFIG_ROOT and restart, or register servers directly")
	}
	data, err := os.ReadFile(s.mcpConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, status.Error(codes.FailedPrecondition, "mcp.json does not exist yet; create it in the mounted config folder, then re-import")
		}
		return nil, status.Error(codes.Internal, "read mcp.json failed")
	}
	report, err := s.ImportJSON(ctx, data)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, boundedStatusMessage(err.Error()))
	}
	s.notifyRegistryChanged()
	response := &turingv1.ReimportMcpJsonResponse{Imported: report.Imported}
	names := make([]string, 0, len(report.Unsupported))
	for name := range report.Unsupported {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		response.Unsupported = append(response.Unsupported, &turingv1.UnsupportedMcpServer{
			Name: name, Reason: report.Unsupported[name],
		})
	}
	return response, nil
}

// RotateMcpServerToken replaces the sealed bearer token; an empty token
// clears it. The response is a descriptor, which carries no token field by
// construction — nothing this RPC returns can echo the secret. No registry
// notification: the toolset is unchanged, and the next dispatch reads the
// sealed token fresh (tokens are never cached with a client).
func (s *Server) RotateMcpServerToken(ctx context.Context, req *turingv1.RotateMcpServerTokenRequest) (*turingv1.McpServerDescriptor, error) {
	server, err := s.repo.GetMCPServer(ctx, req.GetServerId())
	if err != nil {
		if errors.Is(err, repository.ErrMCPServerNotFound) {
			return nil, status.Error(codes.NotFound, "MCP server not found")
		}
		return nil, status.Error(codes.Internal, "rotate MCP server token failed")
	}
	if server.Tier == repository.MCPServerTierBundled {
		return nil, status.Error(codes.FailedPrecondition, "bundled servers do not use caller-managed tokens")
	}
	var sealed []byte
	if token := req.GetBearerToken(); token != "" {
		sealed, err = s.sealer.Seal([]byte(token), []byte(server.Name))
		if err != nil {
			if errors.Is(err, secretbox.ErrNoKey) {
				return nil, status.Error(codes.FailedPrecondition, "server token requires the TURING_INTEGRATION_KEY integration key so it can be stored sealed")
			}
			return nil, status.Error(codes.Internal, "seal MCP server token")
		}
	}
	if err := s.repo.SetMCPServerSealedToken(ctx, server.ID, sealed); err != nil {
		return nil, status.Error(codes.Internal, "rotate MCP server token failed")
	}
	refreshed, err := s.repo.GetMCPServer(ctx, server.ID)
	if err != nil {
		return nil, status.Error(codes.Internal, "rotate MCP server token failed")
	}
	return s.serverDescriptor(ctx, refreshed)
}

func (s *PublicServer) RegisterMcpServer(ctx context.Context, req *turingv1.RegisterMcpServerRequest) (*turingv1.McpServerDescriptor, error) {
	return s.service.RegisterMcpServer(ctx, req)
}

func (s *PublicServer) ReimportMcpJson(ctx context.Context, req *turingv1.ReimportMcpJsonRequest) (*turingv1.ReimportMcpJsonResponse, error) {
	return s.service.ReimportMcpJson(ctx, req)
}

func (s *PublicServer) RotateMcpServerToken(ctx context.Context, req *turingv1.RotateMcpServerTokenRequest) (*turingv1.McpServerDescriptor, error) {
	return s.service.RotateMcpServerToken(ctx, req)
}

func (*InternalServer) RegisterMcpServer(context.Context, *turingv1.RegisterMcpServerRequest) (*turingv1.McpServerDescriptor, error) {
	return nil, status.Error(codes.PermissionDenied, "MCP server management is public")
}

func (*InternalServer) ReimportMcpJson(context.Context, *turingv1.ReimportMcpJsonRequest) (*turingv1.ReimportMcpJsonResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "MCP server management is public")
}

func (*InternalServer) RotateMcpServerToken(context.Context, *turingv1.RotateMcpServerTokenRequest) (*turingv1.McpServerDescriptor, error) {
	return nil, status.Error(codes.PermissionDenied, "MCP server management is public")
}
