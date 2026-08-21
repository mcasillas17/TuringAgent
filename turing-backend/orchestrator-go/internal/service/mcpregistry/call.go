package mcpregistry

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

type ApprovalEnforcer interface {
	ConsumeApprovalForThirdParty(
		ctx context.Context,
		approvalID string,
		runID string,
		serverName string,
		toolName string,
		args map[string]any,
	) error
}

type CallInput struct {
	ServerID   string
	RunID      string
	ApprovalID string
	ToolName   string
	Args       map[string]any
}

func (s *Server) SetApprovalEnforcer(enforcer ApprovalEnforcer) {
	s.approvals = enforcer
}

func (s *Server) CallTool(ctx context.Context, input CallInput) (map[string]any, error) {
	if input.ServerID == "" || input.RunID == "" || input.ToolName == "" {
		return nil, errors.New("MCP server, run, and tool are required")
	}
	server, err := s.repo.GetMCPServer(ctx, input.ServerID)
	if err != nil {
		return nil, err
	}
	if server.Tier == repository.MCPServerTierBundled {
		return nil, errors.New("bundled MCP calls stay on their cooperating server path")
	}
	if !server.Enabled {
		return nil, errors.New("MCP server is disabled")
	}
	if server.Tier == repository.MCPServerTierRemoteURL {
		allowed, err := s.repo.RunAllowsRemoteMCP(
			ctx,
			input.RunID,
			server.Name,
			server.URL,
			input.ToolName,
		)
		if err != nil {
			return nil, errors.New("validate remote MCP egress decision")
		}
		if !allowed {
			return nil, errors.New("remote MCP call is not covered by the run egress decision")
		}
	}
	policy, enabled, found, err := s.repo.GetToolPolicy(ctx, server.Name, input.ToolName)
	if err != nil {
		return nil, err
	}
	if !found || !enabled || policy == "disabled" {
		return nil, errors.New("MCP tool is disabled or unregistered")
	}
	token := ""
	if len(server.SealedToken) > 0 {
		opened, err := s.sealer.Open(server.SealedToken, []byte(server.Name))
		if err != nil {
			return nil, errors.New("MCP server token is unreadable")
		}
		token = string(opened)
	}
	switch policy {
	case "safe":
	case "approval_required":
		if s.approvals == nil {
			return nil, errors.New("caller-side approval enforcement is not configured")
		}
		if err := s.approvals.ConsumeApprovalForThirdParty(
			ctx,
			input.ApprovalID,
			input.RunID,
			server.Name,
			input.ToolName,
			input.Args,
		); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported MCP tool policy %q", policy)
	}
	if server.Tier == repository.MCPServerTierRemoteURL {
		allowed, err := s.repo.RunAllowsRemoteMCP(
			ctx, input.RunID, server.Name, server.URL, input.ToolName,
		)
		if err != nil {
			return nil, errors.New("revalidate remote MCP egress decision")
		}
		if !allowed {
			return nil, errors.New("remote MCP call was revoked before dispatch")
		}
	}
	active, err := s.repo.MCPDispatchActive(
		ctx, server.ID, input.RunID, input.ToolName, policy,
	)
	if err != nil {
		return nil, errors.New("validate MCP dispatch state")
	}
	if !active {
		return nil, errors.New("MCP call was revoked before dispatch")
	}
	// This final active-state check is the dispatch linearization point. A
	// cancellation or policy change committed later observes an already
	// in-flight call; it does not retroactively restore a consumed approval.
	result, err := newMCPClient(server.URL, token, s.clientFor(server)).callTool(ctx, input.ToolName, input.Args)
	if err != nil {
		_ = s.repo.SetMCPServerStatus(ctx, server.ID, "down", boundedStatusMessage(err.Error()))
		return nil, err
	}
	if err := s.repo.SetMCPServerStatus(ctx, server.ID, "up", ""); err != nil {
		log.Printf("record successful MCP call liveness for %s: %v", server.Name, err)
	}
	return result, nil
}
