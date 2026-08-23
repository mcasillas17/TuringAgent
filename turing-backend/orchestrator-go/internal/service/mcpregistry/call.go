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
		serverID string,
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
			// server.ID (not merely re-derived from input.ServerID,
			// though they are the same value here — GetMCPServer looked
			// this row up by exactly that id): this is the immutable
			// binding compared against the approval's own
			// ApprovalRecord.MCPServerID, so a server deleted and
			// re-registered under server.Name cannot let an approval
			// meant for the original row be consumed against this new
			// one — see ConsumeApprovalForThirdParty's own doc comment.
			server.ID,
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
	//
	// The credential-fence critical section starts here, deliberately
	// after every step above that either does not touch the server's
	// token at all or can legitimately block for an unbounded time
	// (caller-side approval enforcement): RotateMcpServerToken excludes
	// every reader of this same server's credential lock for its own repo
	// read/seal/atomic-replace/status-reset (see rotateServerTokenLocked),
	// so from this point through the network call and either outcome of
	// recording this call's own liveness status below, no concurrent
	// rotation of *this* server can be silently finishing underneath it
	// — a rotation of any other server is entirely unaffected, since the
	// lock is keyed by server id (see credentialLock).
	if s.callCredentialLockBarrier != nil {
		s.callCredentialLockBarrier()
	}
	lock := s.credentialLock(server.ID)
	lock.RLock()
	defer lock.RUnlock()
	// The server row is re-read here — token, url, and tier alike, not
	// just the token — rather than reused from the server fetched at the
	// top of this function, so that a rotation completing during the
	// (possibly long) approval wait above is never missed: this always
	// uses whatever RotateMcpServerToken most recently committed, never a
	// value captured before that wait started. A concurrent
	// DeleteMcpServer can equally have committed during that same wait
	// (or even just before the credentialLock call above, if this is the
	// first time this server's lock was ever requested since a prior
	// forget — see forgetCredentialLockIfCurrent): this re-read is what
	// notices that, and the cleanup below is what keeps credentialLocks
	// itself from leaking the entry credentialLock just (re-)created for
	// an id nothing else will ever forget again.
	current, err := s.repo.GetMCPServer(ctx, server.ID)
	if err != nil {
		if errors.Is(err, repository.ErrMCPServerNotFound) {
			s.forgetCredentialLockIfCurrent(server.ID, lock)
		}
		return nil, err
	}
	token := ""
	if len(current.SealedToken) > 0 {
		opened, err := s.sealer.Open(current.SealedToken, []byte(current.Name))
		if err != nil {
			return nil, errors.New("MCP server token is unreadable")
		}
		token = string(opened)
	}
	result, err := newMCPClient(current.URL, token, s.clientFor(current)).callTool(ctx, input.ToolName, input.Args)
	if err != nil {
		_ = s.repo.SetMCPServerStatus(ctx, server.ID, "down", boundedStatusMessage(err.Error()))
		return nil, err
	}
	if err := s.repo.SetMCPServerStatus(ctx, server.ID, "up", ""); err != nil {
		log.Printf("record successful MCP call liveness for %s: %v", server.Name, err)
	}
	return result, nil
}
