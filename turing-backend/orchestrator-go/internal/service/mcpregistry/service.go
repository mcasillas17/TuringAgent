package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
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
	// A non-bundled server with no configured endpoint (the shape a
	// legacy migration-0016 placeholder starts in, and stays in until an
	// operator supplies a real one via import or explicit registration)
	// can never be enabled: refused here, before the repository mutation
	// below and therefore before notify/audit/discovery ever run, so a
	// refused attempt leaves no trace at all — no repository mutation, no
	// registry-change notification, no audit record, and (since enabling
	// a local-container or remote-url server would otherwise attempt live
	// discovery) no network contact. Without this, the enable mutation
	// would commit first and only then fail discovery against an empty
	// URL, leaving the server enabled with whatever stale tool snapshot
	// it happened to carry looking available to a client despite never
	// having a real endpoint. Explicitly scoped to non-bundled (matching
	// the Flutter MCPs page's own switch-disabling condition): a bundled
	// server's url is never empty in practice (seeded by migration 0016),
	// so this is currently only a clarity/defense-in-depth distinction,
	// not an active behavior change for any reachable bundled state.
	if server.Tier != repository.MCPServerTierBundled && server.URL == "" {
		return nil, status.Error(codes.FailedPrecondition, "MCP server has no endpoint configured; register or import one before enabling it")
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
		//
		// discoverLocked holds this server's own credential lock for
		// reading across discover's read/decrypt-token-through-network-
		// call-and-status-recording and its own fallback "down" write on
		// failure — see its own comment for why both need to share one
		// lock acquisition. Because the lock is keyed by server id, none
		// of this can ever block (or be blocked by) a concurrent call,
		// discovery, or rotation against a *different* server.
		discoverCtx, cancel := context.WithTimeout(ctx, s.discoveryTimeout())
		discoverySucceeded, statusErr = s.discoverLocked(discoverCtx, server.ID)
		cancel()
	}
	// Notify and audit immediately once every repository mutation above
	// has been attempted — before checking statusErr and before building
	// the response descriptor — so neither an unrecorded status nor an
	// unexpected descriptor/schema failure below can ever leave a real,
	// already-persisted enable/disable unannounced or unaudited. This
	// also means a discovery failure still produces an audit record: the
	// enable itself succeeded and committed even though the server came
	// back down. This enable/disable payload never carries a token, URL,
	// or status/error text — only the name, tier, and whether discovery
	// was attempted and whether it succeeded. That is a rule about this
	// payload, not every audit call in this file: RegisterMcpServer's own
	// payload below legitimately includes the server's URL, because by
	// the time it is audited there validateServerDefinition has already
	// canonicalized and hardened it, so it is auditing what was actually
	// registered rather than raw, untrusted operator input.
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
		// serverDescriptor/toolDescriptor's own error (e.g. a stored
		// schema that fails to unmarshal) is never returned as-is: it is
		// neither a gRPC status (so it would surface as the unhelpful
		// default codes.Unknown) nor safe to assume is free of anything
		// sensitive, so it is mapped to the same fixed, generic Internal
		// status the read failure just above already uses — matching
		// UpdateMcpToolPolicy's own descriptor-failure handling. Notify
		// and audit have already run (see above), so this only affects
		// what the caller sees from this one response.
		return nil, status.Error(codes.Internal, "read MCP server failed")
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
	// Notify and audit immediately once the policy mutation above has
	// committed — before the fallible list/descriptor mapping below — so
	// a failure reading the tool list back or mapping it to a descriptor
	// (e.g. a corrupted stored schema) can never leave an already-
	// persisted policy change unannounced or unaudited, the same
	// reasoning SetMcpServerEnabled/RegisterMcpServer/RotateMcpServerToken/
	// DeleteMcpServer already apply to their own post-commit notify/audit.
	// The payload carries only the server name, the tool name, and the
	// canonical policy string SetMCPToolPolicy just committed — never the
	// tool's schema, call arguments, or any token.
	s.notifyRegistryChanged()
	s.auditMCPEvent(ctx, "mcp.server.tool_policy_changed", server.ID, map[string]any{
		"name":       server.Name,
		"toolName":   req.GetToolName(),
		"toolPolicy": policy,
	})
	tools, err := s.repo.ListMCPServerTools(ctx, req.GetServerId())
	if err != nil {
		return nil, status.Error(codes.Internal, "read MCP tool failed")
	}
	for _, tool := range tools {
		if tool.Name == req.GetToolName() {
			descriptor, err := toolDescriptor(tool)
			if err != nil {
				// toolDescriptor's own error (e.g. a schema that fails
				// to unmarshal) is never returned as-is: it is neither
				// a gRPC status (so it would surface as the unhelpful
				// default codes.Unknown) nor safe to assume is free of
				// anything sensitive, so it is mapped to the same fixed,
				// generic Internal status read failures above use.
				return nil, status.Error(codes.Internal, "read MCP tool failed")
			}
			return descriptor, nil
		}

	}
	return nil, status.Error(codes.NotFound, "MCP tool not found")
}

func (s *Server) DeleteMcpServer(ctx context.Context, req *turingv1.DeleteMcpServerRequest) (*turingv1.DeleteMcpServerResponse, error) {
	if req == nil || req.GetServerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "server_id is required")
	}
	// Read the server before deleting it: DeleteMCPServer's own error
	// mapping below (NotFound/Bundled/Internal) remains the sole authority
	// over whether the delete actually happens, so this pre-read changes
	// nothing about atomicity — it exists only to capture the name/tier a
	// deleted row can no longer be read back from, for the audit payload
	// below. A missing server fails here first, mapped to the same NotFound
	// DeleteMCPServer would have returned anyway; a bundled server still
	// passes this read (GetMCPServer does not consider tier) and is refused,
	// as always, by the delete call itself.
	server, err := s.repo.GetMCPServer(ctx, req.GetServerId())
	if err != nil {
		if errors.Is(err, repository.ErrMCPServerNotFound) {
			return nil, status.Error(codes.NotFound, "MCP server not found")
		}
		return nil, status.Error(codes.Internal, "read MCP server failed")
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
	// Forgets this server's credential lock entry (see credentialLocks'
	// own comment on why this is what keeps that map's steady-state size
	// bounded by the registry's own row count): only reached once the
	// delete above has actually succeeded, so this never removes an entry
	// for an id that still names a live server.
	s.forgetCredentialLock(req.GetServerId())
	// Notify and audit immediately once the delete above has committed —
	// the same reasoning every other mutation in this file already
	// applies to its own post-commit notify/audit. The payload carries
	// only the name and tier captured by the pre-read above: no URL, no
	// token-related key — narrower than RegisterMcpServer's own payload,
	// matching the reviewed policy in audit/service.go.
	s.notifyRegistryChanged()
	s.auditMCPEvent(ctx, "mcp.server.deleted", server.ID, map[string]any{
		"name": server.Name,
		"tier": string(server.Tier),
	})
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
	registration, err := s.repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
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
		case errors.Is(err, repository.ErrMCPServerRegistryFull):
			return nil, status.Error(codes.ResourceExhausted, mcpServerRegistryFullMessage)
		case errors.Is(err, repository.ErrMCPServerToolsNotAllowed):
			// This RPC never sets Tools on the ImportedMCPServer above,
			// so reaching this case would mean this handler itself
			// regressed — not anything an operator's request could
			// trigger — and it is mapped to the same generic Internal
			// status the default case below already uses, named
			// explicitly here so that regression fails loudly in a test
			// rather than silently falling through.
			return nil, status.Error(codes.Internal, "register MCP server failed")
		default:
			return nil, status.Error(codes.Internal, "register MCP server failed")
		}
	}
	server := registration.Server
	// Notify and audit immediately once the repository mutation has
	// committed — before building the response descriptor — so an
	// unexpected descriptor/schema failure below can never leave a real,
	// already-persisted registration unannounced or unaudited. Unlike
	// SetMcpServerEnabled's payload, this one legitimately includes the
	// server's URL: validateServerDefinition has already canonicalized
	// and hardened it (no userinfo, query, or fragment; classified into
	// exactly one tier), so this audits what was actually registered,
	// not raw operator input. `adopted` records whether this call reused
	// an existing migration-0016 (or otherwise legacy) placeholder row
	// (true) or inserted a genuinely new one (false) — repository-computed
	// inside the same transaction that decided which happened, so it can
	// never diverge from what actually got committed.
	s.notifyRegistryChanged()
	s.auditMCPEvent(ctx, "mcp.server.registered", server.ID, map[string]any{
		"name":    server.Name,
		"tier":    string(server.Tier),
		"url":     server.URL,
		"adopted": registration.Adopted,
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
	token, err := normalizeBearerToken(req.GetBearerToken())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	updated, err := s.rotateServerTokenLocked(ctx, req.GetServerId(), token)
	if err != nil {
		return nil, err
	}
	// Notify and audit immediately once the repository mutation has
	// committed — before building the response descriptor — so an
	// unexpected descriptor/schema failure below can never leave a real,
	// already-persisted rotation unannounced or unaudited. Both run after
	// this server's own credential lock (held only across the mutation
	// itself, inside rotateServerTokenLocked) has already been released,
	// so a slow notifier/audit sink/descriptor build never extends how
	// long a concurrent CallTool/discover for this server (or, since the
	// lock is keyed by server id, any other server entirely) is blocked.
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

// rotateServerTokenLocked performs every credential-sensitive step of a
// rotation — reading the current server row, sealing the new token, and
// atomically replacing it (with its liveness status reset) in the
// repository — entirely under the target server's own credential lock
// (see credentialLock), so it can never interleave with a CallTool or
// discoverLocked critical section for the *same* server that reads the
// prior token, performs its own network operation, and records its own
// liveness/tool status under that lock — a rotation of any other server
// is entirely unaffected. The lock is released as soon as this returns:
// notify/audit/descriptor-building happen in the caller afterward, so a
// slow audit sink or descriptor build never extends how long every other
// in-flight or future CallTool/discoverLocked for this server is blocked.
// rotateBarrier (test-only, nil in production) runs first, while the lock
// is already held, letting a test prove a concurrent CallTool/discover
// genuinely blocks on a rotation that is itself mid-flight.
//
// serverID's existence is checked once, unlocked, before the lock is even
// requested: credentialLock lazily creates a map entry for any id it is
// asked about, and this RPC's serverID is raw caller input, never
// pre-validated by anything upstream (unlike CallTool's and
// discoverLocked's own serverID, both of which are only ever called with
// an id a prior repository read in the same call has already confirmed
// exists). Without this pre-check, a caller could grow credentialLocks
// without the bound its own comment documents simply by rotating a long
// stream of distinct server ids that were never real. The authoritative
// existence/bundled check that actually decides whether the rotation
// proceeds still happens again below, under the lock — this pre-check
// only decides whether creating a lock entry is worth doing at all.
func (s *Server) rotateServerTokenLocked(ctx context.Context, serverID, token string) (repository.MCPServerRecord, error) {
	if _, err := s.repo.GetMCPServer(ctx, serverID); err != nil {
		if errors.Is(err, repository.ErrMCPServerNotFound) {
			return repository.MCPServerRecord{}, status.Error(codes.NotFound, "MCP server not found")
		}
		return repository.MCPServerRecord{}, status.Error(codes.Internal, "read MCP server failed")
	}
	lock := s.credentialLock(serverID)
	lock.Lock()
	defer lock.Unlock()
	if s.rotateBarrier != nil {
		s.rotateBarrier()
	}
	server, err := s.repo.GetMCPServer(ctx, serverID)
	if err != nil {
		if errors.Is(err, repository.ErrMCPServerNotFound) {
			return repository.MCPServerRecord{}, status.Error(codes.NotFound, "MCP server not found")
		}
		return repository.MCPServerRecord{}, status.Error(codes.Internal, "read MCP server failed")
	}
	if server.Tier == repository.MCPServerTierBundled {
		return repository.MCPServerRecord{}, status.Error(codes.FailedPrecondition, bundledServerRegistrationMessage)
	}
	sealed, err := s.sealServerToken(server.Name, token)
	if err != nil {
		return repository.MCPServerRecord{}, err
	}
	updated, err := s.repo.ReplaceMCPServerToken(ctx, server.ID, sealed)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrMCPServerNotFound):
			return repository.MCPServerRecord{}, status.Error(codes.NotFound, "MCP server not found")
		case errors.Is(err, repository.ErrMCPServerBundled):
			return repository.MCPServerRecord{}, status.Error(codes.FailedPrecondition, bundledServerRegistrationMessage)
		default:
			return repository.MCPServerRecord{}, status.Error(codes.Internal, "rotate MCP server token failed")
		}
	}
	return updated, nil
}

// ReimportMcpJson re-reads mcp.json from the configured config root the
// same way app startup does. Both run immediately once
// ReimportConfiguredJSON's mutation has already committed — before
// anything below that could still fail. notify is conditional: it only
// fires when this reimport actually imported something, since nothing
// else about the registry changed for a reimport that only skipped or
// refused entries. audit always runs, unconditionally, recording only
// counts; unlike notify it is never skipped by a later failure, and two
// overlapping ReimportMcpJson calls can never suppress or delay each
// other's audit record. The response's Refused
// list is built directly from this call's own report.Unsupported, sorted
// by name, rather than by re-reading the shared mcp_import_issues table
// (which ListMcpServers still uses for its own Unsupported list): a
// concurrent reimport can freely overwrite that table with its own,
// different refusals without ever being able to leak into — or borrow
// from — this call's response.
func (s *Server) ReimportMcpJson(ctx context.Context, _ *turingv1.ReimportMcpJsonRequest) (*turingv1.ReimportMcpJsonResponse, error) {
	report, err := s.ReimportConfiguredJSON(ctx)
	if err != nil {
		if errors.Is(err, errMCPConfigRootNotConfigured) {
			return nil, status.Error(codes.FailedPrecondition, "MCP config root is not configured")
		}
		return nil, status.Error(codes.Internal, "reimport mcp.json failed")
	}
	if len(report.Imported) > 0 {
		s.notifyRegistryChanged()
	}
	// Only counts are recorded — never names or reasons.
	s.auditMCPEvent(ctx, "mcp.server.reimported", "mcp.json", map[string]any{
		"imported": len(report.Imported),
		"skipped":  len(report.Skipped),
		"refused":  len(report.Unsupported),
	})
	if s.reimportBarrier != nil {
		s.reimportBarrier()
	}
	names := make([]string, 0, len(report.Unsupported))
	for name := range report.Unsupported {
		names = append(names, name)
	}
	sort.Strings(names)
	refused := make([]*turingv1.UnsupportedMcpServer, 0, len(names))
	for _, name := range names {
		refused = append(refused, &turingv1.UnsupportedMcpServer{
			Name: name, Reason: report.Unsupported[name],
		})
	}
	return &turingv1.ReimportMcpJsonResponse{
		Imported: report.Imported,
		Skipped:  report.Skipped,
		Refused:  refused,
	}, nil
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

// discoverLocked runs discover for serverID while holding that server's
// credential lock for reading (see credentialLock), released via defer so
// it can never be leaked if discover — or the repository call below —
// panics, unlike a bare Lock/Unlock pair around the single call site this
// replaces. The lock spans not just discover's own read/decrypt-token-
// through-network-call-and-status-recording, but also this function's own
// fallback "down" write on failure: a concurrent RotateMcpServerToken for
// the *same* server must never be able to reset its liveness status while
// a discovery still using the pre-rotation token could still overwrite
// that reset with either outcome (see rotateServerTokenLocked and the
// fence tests in rotation_fence_test.go). It returns whether discovery
// itself succeeded and, if it failed, whether recording that failure as
// "down" also failed — SetMcpServerEnabled needs both to decide its own
// audit payload and final response.
func (s *Server) discoverLocked(ctx context.Context, serverID string) (discoverySucceeded bool, statusErr error) {
	lock := s.credentialLock(serverID)
	lock.RLock()
	defer lock.RUnlock()
	discoverErr := s.discover(ctx, serverID)
	if discoverErr != nil {
		// Detached from ctx for the same reason SetMcpServerEnabled's
		// disable branch already is: discovery failing (including via
		// context cancellation) must not suppress recording that the
		// server is now down.
		statusCtx, statusCancel := detachedBoundedContext(ctx, postCommitStatusTimeout)
		statusErr = s.repo.SetMCPServerStatus(statusCtx, serverID, "down", boundedStatusMessage(discoverErr.Error()))
		statusCancel()
		return false, statusErr
	}
	return true, nil
}

// discover reads the server's current token, lists its tools over the
// network, and records the result (tools and, on success, "up" liveness).
// Its sole caller, discoverLocked, must hold that server's credential lock
// for reading across this entire call: discover takes no lock of its own
// so that a single RLock covers both what happens inside here and what
// the caller still needs to do with the outcome, without a second, nested
// RLock acquisition by the same goroutine ever risking a self-deadlock
// against a pending RotateMcpServerToken writer for the same server.
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

// boundedUTF8 truncates message to at most limit bytes, trimming back
// further if needed so the result never ends mid-codepoint. It is the one
// shared implementation boundedStatusMessage (bounding an unsupported
// reason or a discovery/status error) and boundedMCPServerNameForDisplay
// (bounding an unsupported entry's own untrusted name/key) both use, so
// the same trim-to-valid-UTF-8 behavior applies regardless of which limit
// is in effect.
func boundedUTF8(message string, limit int) string {
	message = strings.ToValidUTF8(message, "\uFFFD")
	if len(message) <= limit {
		return message
	}
	message = message[:limit]
	for !utf8.ValidString(message) {
		message = message[:len(message)-1]
	}
	return message
}

func boundedStatusMessage(message string) string {
	return boundedUTF8(message, maxMCPStatusMessageBytes)
}
