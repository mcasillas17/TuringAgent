package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	toolpolicy "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const maxMCPStatusMessageBytes = 512

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
	if !req.GetEnabled() {
		if err := s.repo.SetMCPServerStatus(ctx, server.ID, "unknown", ""); err != nil {
			return nil, status.Error(codes.Internal, "update MCP server status failed")
		}
	} else if server.Tier == repository.MCPServerTierLocalContainer {
		if err := s.discover(ctx, server.ID); err != nil {
			if statusErr := s.repo.SetMCPServerStatus(ctx, server.ID, "down", boundedStatusMessage(err.Error())); statusErr != nil {
				return nil, status.Error(codes.Internal, "record MCP discovery failure failed")
			}
		}
	}
	updated, err := s.repo.GetMCPServer(ctx, server.ID)
	if err != nil {
		return nil, status.Error(codes.Internal, "read MCP server failed")
	}
	descriptor, err := s.serverDescriptor(ctx, updated)
	if err != nil {
		return nil, err
	}
	s.notifyRegistryChanged()
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
