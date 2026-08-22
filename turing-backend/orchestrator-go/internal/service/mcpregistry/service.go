package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	toolpolicy "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const maxMCPStatusMessageBytes = 512

// enableDiscoveryTimeout bounds the entire enable-time discovery operation
// (every tools/list page, not just a single HTTP request) so a vendor that
// never stops paginating cannot force SetMcpServerEnabled to hang for up to
// maxMCPToolPages times the HTTP client's own 30s timeout. It matches that
// existing per-request timeout, since one bounded round trip's worth of
// budget for the whole operation is the intended behavior, not an
// additional allowance on top of it.
const enableDiscoveryTimeout = 30 * time.Second

type RegistryChangeNotifier interface {
	NotifyMCPRegistryChanged(context.Context) error
}

func (s *Server) SetRegistryChangeNotifier(notifier RegistryChangeNotifier) {
	s.notifier = notifier
}

type PublicServer struct {
	turingv1.UnimplementedMcpRegistryServiceServer
	service *Server
}

type InternalServer struct {
	turingv1.UnimplementedMcpRegistryServiceServer
	service *Server
}

func NewPublicServer(service *Server) *PublicServer {
	return &PublicServer{service: service}
}

func NewInternalServer(service *Server) *InternalServer {
	return &InternalServer{service: service}
}

func (s *PublicServer) ListMcpServers(ctx context.Context, req *turingv1.ListMcpServersRequest) (*turingv1.ListMcpServersResponse, error) {
	return s.service.ListMcpServers(ctx, req)
}

func (s *PublicServer) SetMcpServerEnabled(ctx context.Context, req *turingv1.SetMcpServerEnabledRequest) (*turingv1.McpServerDescriptor, error) {
	return s.service.SetMcpServerEnabled(ctx, req)
}

func (s *PublicServer) UpdateMcpToolPolicy(ctx context.Context, req *turingv1.UpdateMcpToolPolicyRequest) (*turingv1.McpToolDescriptor, error) {
	return s.service.UpdateMcpToolPolicy(ctx, req)
}

func (s *PublicServer) DeleteMcpServer(ctx context.Context, req *turingv1.DeleteMcpServerRequest) (*turingv1.DeleteMcpServerResponse, error) {
	return s.service.DeleteMcpServer(ctx, req)
}

func (s *PublicServer) RegisterMcpServer(ctx context.Context, req *turingv1.RegisterMcpServerRequest) (*turingv1.McpServerDescriptor, error) {
	return s.service.RegisterMcpServer(ctx, req)
}

func (s *PublicServer) RotateMcpServerToken(ctx context.Context, req *turingv1.RotateMcpServerTokenRequest) (*turingv1.McpServerDescriptor, error) {
	return s.service.RotateMcpServerToken(ctx, req)
}

func (s *PublicServer) ReimportMcpJson(ctx context.Context, req *turingv1.ReimportMcpJsonRequest) (*turingv1.ReimportMcpJsonResponse, error) {
	return s.service.ReimportMcpJson(ctx, req)
}

func (*PublicServer) CallRegisteredMcpTool(context.Context, *turingv1.CallRegisteredMcpToolRequest) (*turingv1.CallRegisteredMcpToolResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "MCP tool dispatch is internal")
}

func (s *InternalServer) ListMcpServers(ctx context.Context, req *turingv1.ListMcpServersRequest) (*turingv1.ListMcpServersResponse, error) {
	return s.service.ListMcpServers(ctx, req)
}

func (*InternalServer) SetMcpServerEnabled(context.Context, *turingv1.SetMcpServerEnabledRequest) (*turingv1.McpServerDescriptor, error) {
	return nil, status.Error(codes.PermissionDenied, "MCP server management is public")
}

func (*InternalServer) UpdateMcpToolPolicy(context.Context, *turingv1.UpdateMcpToolPolicyRequest) (*turingv1.McpToolDescriptor, error) {
	return nil, status.Error(codes.PermissionDenied, "MCP tool policy management is public")
}

func (*InternalServer) DeleteMcpServer(context.Context, *turingv1.DeleteMcpServerRequest) (*turingv1.DeleteMcpServerResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "MCP server management is public")
}

func (*InternalServer) RegisterMcpServer(context.Context, *turingv1.RegisterMcpServerRequest) (*turingv1.McpServerDescriptor, error) {
	return nil, status.Error(codes.PermissionDenied, "MCP server management is public")
}

func (*InternalServer) RotateMcpServerToken(context.Context, *turingv1.RotateMcpServerTokenRequest) (*turingv1.McpServerDescriptor, error) {
	return nil, status.Error(codes.PermissionDenied, "MCP server management is public")
}

func (*InternalServer) ReimportMcpJson(context.Context, *turingv1.ReimportMcpJsonRequest) (*turingv1.ReimportMcpJsonResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "MCP server management is public")
}

func (s *InternalServer) CallRegisteredMcpTool(ctx context.Context, req *turingv1.CallRegisteredMcpToolRequest) (*turingv1.CallRegisteredMcpToolResponse, error) {
	return s.service.CallRegisteredMcpTool(ctx, req)
}

func (s *Server) ListMcpServers(ctx context.Context, _ *turingv1.ListMcpServersRequest) (*turingv1.ListMcpServersResponse, error) {
	servers, err := s.repo.ListMCPServers(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list MCP servers failed")
	}
	response := &turingv1.ListMcpServersResponse{Servers: make([]*turingv1.McpServerDescriptor, 0, len(servers))}
	for _, server := range servers {
		descriptor, err := s.serverDescriptor(ctx, server)
		if err != nil {
			return nil, status.Error(codes.Internal, "list MCP servers failed")
		}
		response.Servers = append(response.Servers, descriptor)
	}
	issues, err := s.repo.ListMCPImportIssues(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list MCP import issues failed")
	}
	for _, issue := range issues {
		response.Unsupported = append(response.Unsupported, &turingv1.UnsupportedMcpServer{
			Name: issue.Name, Reason: issue.Reason,
		})
	}
	return response, nil
}

func (s *Server) SetMcpServerEnabled(ctx context.Context, req *turingv1.SetMcpServerEnabledRequest) (*turingv1.McpServerDescriptor, error) {
	if req == nil || req.GetServerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "server_id is required")
	}
	server, err := s.repo.GetMCPServer(ctx, req.GetServerId())
	if errors.Is(err, repository.ErrMCPServerNotFound) {
		return nil, status.Error(codes.NotFound, "MCP server not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "read MCP server failed")
	}
	if server.Tier == repository.MCPServerTierBundled && !req.GetEnabled() {
		return nil, status.Error(codes.FailedPrecondition, "bundled MCP servers remain enabled; disable individual tools instead")
	}
	if err := s.repo.SetMCPServerEnabled(ctx, server.ID, req.GetEnabled()); err != nil {
		return nil, status.Error(codes.Internal, "update MCP server failed")
	}
	// remoteDiscoveryAttempted records whether this call attempted to
	// contact the server for live discovery: true only when enabling a
	// remote-url server, since that is the one case where the user's
	// explicit action is a real network contact rather than a purely
	// local state flip (disabling never contacts anything, and a
	// local-container server is already reached over the sandboxed
	// internal network rather than egress). It records that discovery
	// was *attempted*, not that a response packet actually arrived —
	// discoverySucceeded is the separate, narrower signal for that.
	remoteDiscoveryAttempted := req.GetEnabled() && server.Tier == repository.MCPServerTierRemoteURL
	discoverySucceeded := false
	// statusErr captures a failure to persist the post-commit liveness
	// status (disabled -> "unknown", or a failed discovery -> "down").
	// It is deliberately not returned immediately: the enable/disable
	// mutation above already committed, so the notify/audit steps below
	// must still run for it before the caller ever sees an error.
	var statusErr error
	switch {
	case !req.GetEnabled():
		// Detached from ctx (same principle as auditMCPEvent): a client
		// that cancels after the enable/disable already committed must
		// not be able to suppress the status write that describes it.
		disableCtx, cancel := detachedBoundedContext(ctx, postCommitStatusTimeout)
		statusErr = s.repo.SetMCPServerStatus(disableCtx, server.ID, "unknown", "")
		cancel()
	case server.Tier == repository.MCPServerTierLocalContainer || server.Tier == repository.MCPServerTierRemoteURL:
		// The user's explicit enable action is the first liveness contact
		// for a directly registered server of either non-bundled tier:
		// registration itself stays zero-network (see RegisterMcpServer),
		// so a remote-URL server discovers its tools here for the first
		// time exactly like a local-container one always has. The whole
		// operation — not each individual HTTP request — is bounded by
		// enableDiscoveryTimeout so a vendor cannot force up to
		// maxMCPToolPages x the HTTP client's own timeout.
		discoverCtx, cancel := context.WithTimeout(ctx, s.discoveryTimeout())
		discoverErr := s.discover(discoverCtx, server.ID)
		cancel()
		if discoverErr != nil {
			// Detached from ctx for the same reason as the disable case
			// above: discovery failing (including via context
			// cancellation) must not suppress recording that the server
			// is now down.
			statusCtx, statusCancel := detachedBoundedContext(ctx, postCommitStatusTimeout)
			statusErr = s.repo.SetMCPServerStatus(statusCtx, server.ID, "down", boundedStatusMessage(discoverErr.Error()))
			statusCancel()
		} else {
			discoverySucceeded = true
		}
	}
	// Notify and audit immediately once every repository mutation above
	// has been attempted — before checking statusErr and before building
	// the response descriptor — so neither an unrecorded status nor an
	// unexpected descriptor/schema failure below can ever leave a real,
	// already-persisted enable/disable unannounced or unaudited. This
	// also means a discovery failure still produces an audit record: the
	// enable itself succeeded and committed even though the server came
	// back down. The payload never carries a token, URL, or status/error
	// text — only the name, tier, and whether discovery was attempted and
	// whether it succeeded.
	s.notifyRegistryChanged()
	action := "mcp.server.disabled"
	if req.GetEnabled() {
		action = "mcp.server.enabled"
	}
	s.auditMCPEvent(ctx, action, server.ID, map[string]any{
		"name":                     server.Name,
		"tier":                     string(server.Tier),
		"remoteDiscoveryAttempted": remoteDiscoveryAttempted,
		"discoverySucceeded":       discoverySucceeded,
	})
	if statusErr != nil {
		return nil, status.Error(codes.Internal, "record MCP server status failed")
	}
	updated, err := s.repo.GetMCPServer(ctx, server.ID)
	if err != nil {
		return nil, status.Error(codes.Internal, "read MCP server failed")
	}
	descriptor, err := s.serverDescriptor(ctx, updated)
	if err != nil {
		return nil, err
	}
	return descriptor, nil
}

func (s *Server) UpdateMcpToolPolicy(ctx context.Context, req *turingv1.UpdateMcpToolPolicyRequest) (*turingv1.McpToolDescriptor, error) {
	if req == nil || req.GetServerId() == "" || req.GetToolName() == "" {
		return nil, status.Error(codes.InvalidArgument, "server_id and tool_name are required")
	}

	policy, err := policyFromProto(req.GetPolicy())
	if err != nil {
		return nil, err
	}
	server, err := s.repo.GetMCPServer(ctx, req.GetServerId())
	if err != nil {
		if errors.Is(err, repository.ErrMCPServerNotFound) {
			return nil, status.Error(codes.NotFound, "MCP server not found")
		}
		return nil, status.Error(codes.Internal, "read MCP server failed")
	}
	if server.Tier == repository.MCPServerTierBundled &&
		policy == "safe" &&
		toolpolicy.BundledToolRequiresApproval(server.Name, req.GetToolName()) {
		return nil, status.Error(codes.FailedPrecondition, "bundled mutating tools require approval at their server")
	}
	if err := s.repo.SetMCPToolPolicy(ctx, req.GetServerId(), req.GetToolName(), policy); err != nil {
		if errors.Is(err, repository.ErrMCPToolNotFound) {
			return nil, status.Error(codes.NotFound, "MCP tool not found")
		}
		return nil, status.Error(codes.Internal, "update MCP tool policy failed")
	}
	tools, err := s.repo.ListMCPServerTools(ctx, req.GetServerId())
	if err != nil {
		return nil, status.Error(codes.Internal, "read MCP tool failed")
	}
	for _, tool := range tools {
		if tool.Name == req.GetToolName() {
			descriptor, err := toolDescriptor(tool)
			if err == nil {
				s.notifyRegistryChanged()
			}
			return descriptor, err
		}

	}
	return nil, status.Error(codes.NotFound, "MCP tool not found")
}

func (s *Server) DeleteMcpServer(ctx context.Context, req *turingv1.DeleteMcpServerRequest) (*turingv1.DeleteMcpServerResponse, error) {
	if req == nil || req.GetServerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "server_id is required")
	}
	if err := s.repo.DeleteMCPServer(ctx, req.GetServerId()); err != nil {
		switch {
		case errors.Is(err, repository.ErrMCPServerNotFound):
			return nil, status.Error(codes.NotFound, "MCP server not found")
		case errors.Is(err, repository.ErrMCPServerBundled):
			return nil, status.Error(codes.FailedPrecondition, "bundled MCP servers cannot be deleted")
		default:
			return nil, status.Error(codes.Internal, "delete MCP server failed")
		}
	}
	s.notifyRegistryChanged()
	return &turingv1.DeleteMcpServerResponse{}, nil
}

func (s *Server) notifyRegistryChanged() {
	if s.notifier == nil {
		return
	}
	if err := s.notifier.NotifyMCPRegistryChanged(context.Background()); err != nil {
		log.Printf("notify runtime of MCP registry change: %v", err)
	}
}

// tierFromProto validates that a RegisterMcpServerRequest names one of the
// two tiers an operator may explicitly request. BUNDLED is TuringAgent's
// own tier and UNSPECIFIED leaves the caller not actually deciding, so both
// are refused rather than defaulted to something else.
func tierFromProto(tier turingv1.McpServerTier) (repository.MCPServerTier, error) {
	switch tier {
	case turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER:
		return repository.MCPServerTierLocalContainer, nil
	case turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL:
		return repository.MCPServerTierRemoteURL, nil
	default:
		return "", status.Error(codes.InvalidArgument, "tier must be local-container or remote-url")
	}
}

// mapMCPValidationError maps the errors validateServerDefinition can return
// to a gRPC status: a reserved bundled name is FailedPrecondition (it names
// a real, if narrower, precondition about who owns that name), and every
// other validation failure (bad name shape, bad URL, tier/URL mismatch, a
// malformed bearer) is InvalidArgument.
func mapMCPValidationError(err error) error {
	if errors.Is(err, errMCPServerNameReserved) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return status.Error(codes.InvalidArgument, err.Error())
}

// RegisterMcpServer registers a server directly, without an mcp.json file.
// It runs the same name/URL/token validation as a file import so the two
// paths can never diverge, seals the token with the same sealer, and never
// contacts the endpoint: "tools discovered on first liveness contact" is
// satisfied by SetMcpServerEnabled (the user's explicit enable action, for
// both local-container and remote-url tiers), not by a side effect of
// registration.
func (s *Server) RegisterMcpServer(ctx context.Context, req *turingv1.RegisterMcpServerRequest) (*turingv1.McpServerDescriptor, error) {
	if req == nil || req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	tier, err := tierFromProto(req.GetTier())
	if err != nil {
		return nil, err
	}
	validated, err := validateServerDefinition(req.GetName(), req.GetUrl(), &tier, req.GetBearerToken())
	if err != nil {
		return nil, mapMCPValidationError(err)
	}
	sealed, err := s.sealServerToken(validated.Name, validated.Token)
	if err != nil {
		return nil, err
	}
	server, err := s.repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name:        validated.Name,
		URL:         validated.URL,
		SealedToken: sealed,
		Tier:        validated.Tier,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrMCPServerBundled):
			return nil, status.Error(codes.FailedPrecondition, bundledServerRegistrationMessage)
		case errors.Is(err, repository.ErrMCPServerNameTaken):
			return nil, status.Error(codes.AlreadyExists, "MCP server name is already registered")
		default:
			return nil, status.Error(codes.Internal, "register MCP server failed")
		}
	}
	// Notify and audit immediately once the repository mutation has
	// committed — before building the response descriptor — so an
	// unexpected descriptor/schema failure below can never leave a real,
	// already-persisted registration unannounced or unaudited.
	s.notifyRegistryChanged()
	s.auditMCPEvent(ctx, "mcp.server.registered", server.ID, map[string]any{
		"name": server.Name,
		"tier": string(server.Tier),
		"url":  server.URL,
	})
	descriptor, err := s.serverDescriptor(ctx, server)
	if err != nil {
		return nil, status.Error(codes.Internal, "read MCP server failed")
	}
	return descriptor, nil
}

// RotateMcpServerToken replaces (or, given an empty bearer_token, clears)
// the sealed token stored for a server, using the server's own name as the
// sealing AAD the same way registration and import do.
func (s *Server) RotateMcpServerToken(ctx context.Context, req *turingv1.RotateMcpServerTokenRequest) (*turingv1.McpServerDescriptor, error) {
	if req == nil || req.GetServerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "server_id is required")
	}
	server, err := s.repo.GetMCPServer(ctx, req.GetServerId())
	if err != nil {
		if errors.Is(err, repository.ErrMCPServerNotFound) {
			return nil, status.Error(codes.NotFound, "MCP server not found")
		}
		return nil, status.Error(codes.Internal, "read MCP server failed")
	}
	if server.Tier == repository.MCPServerTierBundled {
		return nil, status.Error(codes.FailedPrecondition, bundledServerRegistrationMessage)
	}
	token, err := normalizeBearerToken(req.GetBearerToken())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	sealed, err := s.sealServerToken(server.Name, token)
	if err != nil {
		return nil, err
	}
	updated, err := s.repo.ReplaceMCPServerToken(ctx, server.ID, sealed)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrMCPServerNotFound):
			return nil, status.Error(codes.NotFound, "MCP server not found")
		case errors.Is(err, repository.ErrMCPServerBundled):
			return nil, status.Error(codes.FailedPrecondition, bundledServerRegistrationMessage)
		default:
			return nil, status.Error(codes.Internal, "rotate MCP server token failed")
		}
	}
	// Notify and audit immediately once the repository mutation has
	// committed — before building the response descriptor — so an
	// unexpected descriptor/schema failure below can never leave a real,
	// already-persisted rotation unannounced or unaudited.
	s.notifyRegistryChanged()
	action := "mcp.server.token_cleared"
	if token != "" {
		action = "mcp.server.token_rotated"
	}
	s.auditMCPEvent(ctx, action, updated.ID, map[string]any{
		"name":            updated.Name,
		"tokenConfigured": token != "",
	})
	descriptor, err := s.serverDescriptor(ctx, updated)
	if err != nil {
		return nil, status.Error(codes.Internal, "read MCP server failed")
	}
	return descriptor, nil
}

// ReimportMcpJson re-reads mcp.json from the configured config root the
// same way app startup does, and reports what happened the same way
// ListMcpServers reports Unsupported entries: sorted by name, from the
// mcp_import_issues table rather than from ImportReport's map so ordering
// stays consistent with the rest of the registry surface.
func (s *Server) ReimportMcpJson(ctx context.Context, _ *turingv1.ReimportMcpJsonRequest) (*turingv1.ReimportMcpJsonResponse, error) {
	report, err := s.ReimportConfiguredJSON(ctx)
	if err != nil {
		if errors.Is(err, errMCPConfigRootNotConfigured) {
			return nil, status.Error(codes.FailedPrecondition, "MCP config root is not configured")
		}
		return nil, status.Error(codes.Internal, "reimport mcp.json failed")
	}
	// The mutation is already complete at this point: report.Unsupported
	// is the same map ReimportConfiguredJSON just wrote to the
	// mcp_import_issues table, so its length is exactly the refused
	// count without a second (fallible) repository read. Auditing here,
	// before the ListMCPImportIssues call below that only re-derives the
	// response's Refused list, means a later failure to read that table
	// back can never cause a real, already-committed reimport to go
	// unaudited. Only counts are recorded — never names or reasons.
	s.auditMCPEvent(ctx, "mcp.server.reimported", "mcp.json", map[string]any{
		"imported": len(report.Imported),
		"skipped":  len(report.Skipped),
		"refused":  len(report.Unsupported),
	})
	issues, err := s.repo.ListMCPImportIssues(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list MCP import issues failed")
	}
	response := &turingv1.ReimportMcpJsonResponse{
		Imported: report.Imported,
		Skipped:  report.Skipped,
	}
	for _, issue := range issues {
		response.Refused = append(response.Refused, &turingv1.UnsupportedMcpServer{
			Name: issue.Name, Reason: issue.Reason,
		})
	}
	if len(report.Imported) > 0 {
		s.notifyRegistryChanged()
	}
	return response, nil
}

func (s *Server) CallRegisteredMcpTool(ctx context.Context, req *turingv1.CallRegisteredMcpToolRequest) (*turingv1.CallRegisteredMcpToolResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	args := map[string]any{}
	if req.GetArgs() != nil {
		args = req.GetArgs().AsMap()
	}
	result, err := s.CallTool(ctx, CallInput{
		ServerID:   req.GetServerId(),
		RunID:      req.GetRunId(),
		ApprovalID: req.GetApprovalId(),
		ToolName:   req.GetToolName(),
		Args:       args,
	})
	if err != nil {
		return nil, err
	}
	value, err := structpb.NewStruct(result)
	if err != nil {
		return nil, status.Error(codes.Internal, "MCP result is invalid")
	}
	return &turingv1.CallRegisteredMcpToolResponse{Result: value}, nil
}

func (s *Server) discover(ctx context.Context, serverID string) (err error) {
	server, err := s.repo.GetMCPServer(ctx, serverID)
	if err != nil {
		return err
	}
	token := ""
	if len(server.SealedToken) > 0 {
		opened, err := s.sealer.Open(server.SealedToken, []byte(server.Name))
		if err != nil {
			return errors.New("server token is unreadable")
		}
		token = string(opened)
	}
	defer func() {
		err = redactMCPErrorValue(err, token)
	}()
	rawTools, err := newMCPClient(server.URL, token, s.clientFor(server)).listTools(ctx)
	if err != nil {
		return err
	}
	discovered := make([]DiscoveredTool, 0, len(rawTools))
	seen := make(map[string]struct{}, len(rawTools))
	for index, raw := range rawTools {
		name, ok := raw["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return fmt.Errorf("tool %d has an invalid name", index)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("tool %q is duplicated", name)
		}
		seen[name] = struct{}{}
		schema := map[string]any{"type": "object"}
		if value, present := raw["inputSchema"]; present {
			var valid bool
			schema, valid = value.(map[string]any)
			if !valid || schema == nil {
				return fmt.Errorf("tool %q inputSchema must be an object", name)
			}
		}
		if rootType, present := schema["type"]; present && rootType != "object" {
			return fmt.Errorf("tool %q inputSchema root type must be object", name)
		}
		schema["type"] = "object"
		encoded, err := json.Marshal(schema)
		if err != nil {
			return fmt.Errorf("tool %q inputSchema is invalid", name)
		}
		discovered = append(discovered, DiscoveredTool{Name: name, SchemaJSON: string(encoded)})
	}
	if err := s.RecordDiscovery(ctx, server.ID, discovered); err != nil {
		return err
	}
	return s.repo.SetMCPServerStatus(ctx, server.ID, "up", "")
}

func (s *Server) serverDescriptor(ctx context.Context, server repository.MCPServerRecord) (*turingv1.McpServerDescriptor, error) {
	tools, err := s.repo.ListMCPServerTools(ctx, server.ID)
	if err != nil {
		return nil, err
	}
	descriptors := make([]*turingv1.McpToolDescriptor, 0, len(tools))
	for _, tool := range tools {
		descriptor, err := toolDescriptor(tool)
		if err != nil {
			return nil, err
		}
		descriptors = append(descriptors, descriptor)
	}
	return &turingv1.McpServerDescriptor{
		ServerId:        server.ID,
		Name:            server.Name,
		Transport:       server.Transport,
		Url:             server.URL,
		Tier:            tierToProto(server.Tier),
		Enabled:         server.Enabled,
		Liveness:        livenessToProto(server.Status),
		StatusMessage:   server.StatusError,
		SandboxConfined: server.Tier == repository.MCPServerTierBundled,
		Tools:           descriptors,
	}, nil
}

func toolDescriptor(tool repository.MCPServerTool) (*turingv1.McpToolDescriptor, error) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(tool.SchemaJSON), &schema); err != nil {
		return nil, err
	}
	value, err := structpb.NewStruct(schema)
	if err != nil {
		return nil, err
	}
	return &turingv1.McpToolDescriptor{
		ToolName: tool.Name,
		Policy:   policyToProto(tool.Policy),
		Schema:   value,
		Enabled:  tool.Enabled,
		Present:  tool.Present,
	}, nil
}

func tierToProto(tier repository.MCPServerTier) turingv1.McpServerTier {
	switch tier {
	case repository.MCPServerTierBundled:
		return turingv1.McpServerTier_MCP_SERVER_TIER_BUNDLED
	case repository.MCPServerTierLocalContainer:
		return turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER
	case repository.MCPServerTierRemoteURL:
		return turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL
	default:
		return turingv1.McpServerTier_MCP_SERVER_TIER_UNSPECIFIED
	}
}

func livenessToProto(value string) turingv1.McpServerLiveness {
	switch value {
	case "up":
		return turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UP
	case "down":
		return turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_DOWN
	case "unknown":
		return turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UNKNOWN
	default:
		return turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UNSPECIFIED
	}
}

func policyFromProto(policy turingv1.ToolPolicy) (string, error) {
	switch policy {
	case turingv1.ToolPolicy_TOOL_POLICY_SAFE:
		return "safe", nil
	case turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED:
		return "approval_required", nil
	case turingv1.ToolPolicy_TOOL_POLICY_DISABLED:
		return "disabled", nil
	default:
		return "", status.Error(codes.InvalidArgument, "policy is required")
	}
}

func policyToProto(policy string) turingv1.ToolPolicy {
	switch policy {
	case "safe":
		return turingv1.ToolPolicy_TOOL_POLICY_SAFE
	case "approval_required":
		return turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED
	case "disabled":
		return turingv1.ToolPolicy_TOOL_POLICY_DISABLED
	default:
		return turingv1.ToolPolicy_TOOL_POLICY_UNSPECIFIED
	}
}

func boundedStatusMessage(message string) string {
	message = strings.ToValidUTF8(message, "\uFFFD")
	if len(message) <= maxMCPStatusMessageBytes {
		return message
	}
	message = message[:maxMCPStatusMessageBytes]
	for !utf8.ValidString(message) {
		message = message[:len(message)-1]
	}
	return message
}
