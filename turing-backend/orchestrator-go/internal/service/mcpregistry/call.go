package mcpregistry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// UnattendedRefuser makes a refused call of an unattended run visible: it
// fails the run and writes its automation.tool.blocked row in one
// transaction, and publishes the failure. The runtime implements it, because
// it owns the event bus.
type UnattendedRefuser interface {
	RefuseUnattendedCall(ctx context.Context, runID string, block repository.UnattendedToolBlock) error
}

// SetUnattendedRefuser wires the refusal hook. With none wired, every call
// of an unattended run is refused.
func (s *Server) SetUnattendedRefuser(refuser UnattendedRefuser) {
	s.unattendedRefuser = refuser
}

func (s *Server) CallTool(ctx context.Context, input CallInput) (map[string]any, error) {
	if input.ServerID == "" || input.RunID == "" || input.ToolName == "" {
		return nil, errors.New("MCP server, run, and tool are required")
	}
	server, err := s.repo.GetMCPServer(ctx, input.ServerID)
	if err != nil {
		return nil, err
	}
	if repository.IsTeamServerName(server.Name) {
		// Its row survives the upgrade, but its tools are withdrawn: the name
		// belongs to the orchestrator's team pseudo-server now.
		return nil, errors.New("MCP server named `team` is withdrawn; register it under another name")
	}
	// An unattended run's grant is checked first, for every tier and before
	// anything is spent: an automation's frozen allowlist bounds every tool its
	// run calls, and the automation as it stands now may only narrow it. A run
	// that skipped the beacon is held to it here all the same.
	unattended, err := s.repo.CheckUnattendedCall(ctx, input.RunID, server.Name, input.ToolName)
	if err != nil {
		return nil, errors.New("read automation grant")
	}
	if unattended.Unattended {
		if s.unattendedRefuser == nil {
			return nil, status.Error(codes.PermissionDenied, "unattended MCP calls are refused: no refusal hook is configured")
		}
		if unattended.Refusal != "" {
			return nil, s.refuseUnattendedCall(ctx, input, server.Name, unattended)
		}
	}
	// A job that enforces its frozen set may use nothing outside it, so the
	// call ends before an approval is spent or the server is reached.
	selection, err := s.repo.RunToolSelection(ctx, input.RunID)
	if err != nil {
		return nil, errors.New("read run tool selection")
	}
	if !selection.Allows(server.Name + "/" + input.ToolName) {
		return nil, errors.New("MCP tool is not selected for this run")
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
	// The grant is read again here, for an unattended run, in the same read
	// transaction as the liveness check: a revocation committed after the
	// entry check, while the approval was handled, is refused with the
	// approval spent and nothing sent.
	dispatch, err := s.repo.MCPDispatchActive(ctx, repository.MCPDispatchCall{
		ServerID: server.ID, ServerName: server.Name, RunID: input.RunID,
		ToolName: input.ToolName, ExpectedPolicy: policy, Unattended: unattended.Unattended,
	})
	if err != nil {
		return nil, errors.New("validate MCP dispatch state")
	}
	if dispatch.Unattended.Refusal != "" {
		return nil, s.refuseUnattendedCall(ctx, input, server.Name, dispatch.Unattended)
	}
	if !dispatch.Active {
		return nil, errors.New("MCP call was revoked before dispatch")
	}
	// This final active-state check is the dispatch linearization point. A
	// cancellation, policy change or revocation of an automation's grant
	// committed later observes an already in-flight call; it does not
	// retroactively restore a consumed approval.
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
	peer := newMCPClient(current.URL, token, s.clientFor(current))
	// Detached: the peer controls how slowly it answers, and this must not
	// extend either the dispatch or the credential read lock it runs under.
	defer func() { go peer.releaseSession() }()
	result, err := peer.callTool(ctx, input.ToolName, input.Args)
	if err != nil {
		_ = s.repo.SetMCPServerStatus(ctx, server.ID, "down", boundedStatusMessage(err.Error()))
		return nil, err
	}
	if err := s.repo.SetMCPServerStatus(ctx, server.ID, "up", ""); err != nil {
		log.Printf("record successful MCP call liveness for %s: %v", server.Name, err)
	}
	return result, nil
}

// refuseUnattendedCall answers a call the grant check refused. A refusal that
// fails the run goes to the runtime's hook, which fails it, writes its
// automation.tool.blocked row and publishes the failure; the call is refused
// with PermissionDenied naming the code even when that fails, which is logged.
// The hook runs on a context of its own, so a caller that gives up on the call
// cannot take the failure down with it. A direct call carries no tool call id,
// so the row has no target.
func (s *Server) refuseUnattendedCall(ctx context.Context, input CallInput, serverName string, call repository.UnattendedCall) error {
	if call.Fatal() {
		hookCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.unattendedRefuser.RefuseUnattendedCall(hookCtx, input.RunID, repository.UnattendedToolBlock{
			AutomationID: call.Grant.AutomationID, AutomationName: call.Grant.AutomationName,
			ServerName: serverName, ToolName: input.ToolName, Code: call.Refusal,
		}); err != nil {
			log.Printf("fail run %s after its MCP call was refused (%s): %v", input.RunID, call.Refusal, err)
		}
	}
	return status.Error(codes.PermissionDenied, call.Refusal)
}
