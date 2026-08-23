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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const maxMCPStatusMessageBytes = 512

// mcpRegistryOverBudgetNoticeMessage is the fixed, generic
// RegistryDegradationReason ListMcpServers sets when
// repository.MCPRegistrySnapshot reports OverBudget: the registry-wide
// tool-byte total (repository.MaxMCPRegistryToolBytes) already exceeds
// budget — a state every tool-reconciliation write path (
// replaceServerToolsTx, and UpsertTools) already refuses to create, so
// this should be unreachable in practice, but a database that somehow
// already carries one (e.g. a state predating one of those guards, or a
// future regression reintroducing an unguarded write path) must not make
// the registry unmanageable. Every server row is still listed — an
// operator retains enough to identify and delete an offending one — but
// every one of them is returned with its own Tools list empty rather
// than attempting to read, let alone marshal and send, a schema-heavy
// result sized against an unbounded aggregate. See
// repository.MCPRegistrySnapshot's own OverBudget doc comment and
// docs/mcp-security-and-integration.md for the recovery story: delete
// the offending server (its tool rows cascade-delete with it), and a
// later ListMcpServers call — once the aggregate is back under budget —
// recovers full tool listing automatically.
//
// This is surfaced through ListMcpServersResponse's own explicit
// registry_degraded/registry_degradation_reason fields, never as a
// synthetic "_registry"-named entry in Unsupported: Unsupported names
// only ordinary, per-entry mcp.json import refusals, and a systemic,
// registry-wide condition like this one is not one of those. A real
// mcp.json entry literally named "_registry" cannot collide with this:
// its leading "_" already fails mcpServerNamePattern, so it is refused
// through the ordinary synthetic-invalid-entry path (see
// mcpregistry.invalidMCPEntryLabel) under an "_invalid_server_N" label,
// never under "_registry" itself.
const mcpRegistryOverBudgetNoticeMessage = "MCP registry aggregate tool budget is exhausted; tool schemas are hidden until an oversized or excess server is deleted"

// mcpRegistryServerCountOverCapMessage is
// mcpRegistryOverBudgetNoticeMessage's counterpart for
// repository.MCPRegistrySnapshot's ServersOverCap: the registry already
// holds more mcp_servers rows than repository.MaxMCPRegistryServers.
// Every live write path (RegisterMcpServer, ImportJSON) already refuses
// to create such a state, so — like OverBudget — this should be
// unreachable in practice, but a database that somehow already exceeds
// it degrades the same bounded, recoverable way: a truncated (never
// unbounded) set of server descriptors, each with its own Tools empty,
// rather than either failing outright or growing this response without
// limit.
const mcpRegistryServerCountOverCapMessage = "MCP registry server count exceeds its operating limit; only a bounded subset is listed until excess servers are deleted"

// mcpRegistryIssueCountOverCapMessage is
// mcpRegistryOverBudgetNoticeMessage's counterpart for
// repository.MCPRegistrySnapshot's IssuesOverCap: mcp_import_issues
// already holds more rows than repository.MaxMCPImportIssues. A single
// ImportJSON call can never itself persist more than that many (see
// recordUnsupported's own defensive bound), so this should likewise be
// unreachable in practice, but a database that somehow already exceeds
// it degrades the same bounded way: a truncated set of Unsupported
// entries, and — for the same shared-rule reason every degraded
// condition empties Tools regardless of which specific bound tripped —
// every server's own Tools is left empty here too.
const mcpRegistryIssueCountOverCapMessage = "MCP registry import issue count exceeds its operating limit; only a bounded subset is listed"

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

func (s *PublicServer) UpdateToolPolicyByName(ctx context.Context, req *turingv1.UpdateToolPolicyByNameRequest) (*turingv1.McpToolDescriptor, error) {
	return s.service.UpdateToolPolicyByName(ctx, req)
}

func (s *PublicServer) ListPseudoServerTools(ctx context.Context, req *turingv1.ListPseudoServerToolsRequest) (*turingv1.ListPseudoServerToolsResponse, error) {
	return s.service.ListPseudoServerTools(ctx, req)
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

func (*InternalServer) UpdateToolPolicyByName(context.Context, *turingv1.UpdateToolPolicyByNameRequest) (*turingv1.McpToolDescriptor, error) {
	return nil, status.Error(codes.PermissionDenied, "MCP tool policy management is public")
}

func (*InternalServer) ListPseudoServerTools(context.Context, *turingv1.ListPseudoServerToolsRequest) (*turingv1.ListPseudoServerToolsResponse, error) {
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
	// MCPRegistrySnapshot reads every server, its own tools (unless
	// degraded), and every import issue from a single SQLite read
	// transaction, so this can never observe a mix of before-and-after
	// state for a concurrent tool reconciliation (replaceServerToolsTx,
	// UpsertTools) or server insert/delete — unlike three-plus
	// separately-acquired queries (an aggregate-budget/count read, then
	// the server list, then each server's own tools, then the issue
	// list), where a write landing between any two of them could make an
	// earlier guard's decision disagree with the rows a later query in
	// the same call actually returns. The same aggregate present-and-
	// absent tool-byte budget guard (see repository.MaxMCPRegistryToolBytes)
	// and the server/issue row-count guards (repository.
	// MaxMCPRegistryServers/MaxMCPImportIssues) still run first, inside
	// that same transaction, before any tool row is read: every write
	// path already refuses to push any of the three over its own bound,
	// so none of this should ever actually trip — but if it somehow did,
	// a degraded (rather than failed) snapshot is what lets this still
	// build a response: every server descriptor below, with its own
	// Tools always empty while degraded, plus the explicit
	// RegistryDegraded/RegistryDegradationReason fields below, rather
	// than either refusing the whole call (leaving an operator with no
	// way to see and delete whichever server is responsible) or
	// overloading the per-entry Unsupported list with a systemic,
	// non-per-entry notice.
	snapshot, err := s.repo.MCPRegistrySnapshot(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list MCP servers failed")
	}
	response := &turingv1.ListMcpServersResponse{Servers: make([]*turingv1.McpServerDescriptor, 0, len(snapshot.Servers))}
	for _, entry := range snapshot.Servers {
		descriptor, err := buildServerDescriptor(entry.Server, entry.Tools)
		if err != nil {
			return nil, status.Error(codes.Internal, "list MCP servers failed")
		}
		response.Servers = append(response.Servers, descriptor)
	}
	for _, issue := range snapshot.Issues {
		response.Unsupported = append(response.Unsupported, &turingv1.UnsupportedMcpServer{
			Name: issue.Name, Reason: issue.Reason,
		})
	}
	// Priority order when more than one condition trips at once (an
	// extreme, likely-unreachable case, since each is independently
	// unreachable through any live write path on its own): OverBudget
	// first, since it was the first of the three this registry ever
	// guarded against, then ServersOverCap, then IssuesOverCap. Only one
	// reason is ever reported — never concatenated — since every one of
	// these already implies the exact same visible behavior (every
	// server's Tools empty), so there is nothing a caller would do
	// differently for knowing about a second, simultaneous cause.
	switch {
	case snapshot.OverBudget:
		response.RegistryDegraded = true
		response.RegistryDegradationReason = mcpRegistryOverBudgetNoticeMessage
	case snapshot.ServersOverCap:
		response.RegistryDegraded = true
		response.RegistryDegradationReason = mcpRegistryServerCountOverCapMessage
	case snapshot.IssuesOverCap:
		response.RegistryDegraded = true
		response.RegistryDegradationReason = mcpRegistryIssueCountOverCapMessage
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

func (s *Server) UpdateToolPolicyByName(ctx context.Context, req *turingv1.UpdateToolPolicyByNameRequest) (*turingv1.McpToolDescriptor, error) {
	if req == nil || req.GetServerName() == "" || req.GetToolName() == "" {
		return nil, status.Error(codes.InvalidArgument, "server_name and tool_name are required")
	}
	policy, err := policyFromProto(req.GetPolicy())
	if err != nil {
		return nil, err
	}
	if policy == "safe" && toolpolicy.BundledToolRequiresApproval(req.GetServerName(), req.GetToolName()) {
		return nil, status.Error(codes.FailedPrecondition, "bundled mutating tools require approval at their server")
	}
	if req.GetServerName() == "integrations" && policy == "safe" &&
		!toolpolicy.ToolReadOnly(req.GetServerName(), req.GetToolName()) {
		return nil, status.Error(codes.FailedPrecondition, "integration mutating tools require approval")
	}
	if err := s.repo.SetToolPolicyByName(ctx, req.GetServerName(), req.GetToolName(), policy); err != nil {
		if errors.Is(err, repository.ErrMCPToolNotFound) {
			return nil, status.Error(codes.NotFound, "MCP tool not found")
		}
		return nil, status.Error(codes.Internal, "update MCP tool policy failed")
	}
	tool, err := s.repo.GetToolByName(ctx, req.GetServerName(), req.GetToolName())
	if err != nil {
		if errors.Is(err, repository.ErrMCPToolNotFound) {
			return nil, status.Error(codes.NotFound, "MCP tool not found")
		}
		return nil, status.Error(codes.Internal, "read MCP tool failed")
	}
	descriptor, err := toolDescriptor(tool)
	if err == nil {
		s.notifyRegistryChanged()
	}
	return descriptor, err
}

func (s *Server) ListPseudoServerTools(ctx context.Context, req *turingv1.ListPseudoServerToolsRequest) (*turingv1.ListPseudoServerToolsResponse, error) {
	if req == nil || (req.GetServerName() != "skills" && req.GetServerName() != "integrations") {
		return nil, status.Error(codes.InvalidArgument, "server_name must be skills or integrations")
	}
	tools, err := s.repo.ListPseudoServerTools(ctx, req.GetServerName())
	if err != nil {
		return nil, status.Error(codes.Internal, "list pseudo-server tools failed")
	}
	response := &turingv1.ListPseudoServerToolsResponse{Tools: make([]*turingv1.McpToolDescriptor, 0, len(tools))}
	for _, tool := range tools {
		descriptor, err := toolDescriptor(tool)
		if err != nil {
			return nil, err
		}
		response.Tools = append(response.Tools, descriptor)
	}
	return response, nil
}

func (s *Server) DeleteMcpServer(ctx context.Context, req *turingv1.DeleteMcpServerRequest) (*turingv1.DeleteMcpServerResponse, error) {
	if req == nil || req.GetServerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "server_id is required")
	}
	// DeleteMCPServer performs the tier check, the tombstone insert, and
	// the delete itself all inside its own single transaction, and
	// returns the exact record it deleted — read from inside that same
	// transaction — so this needs no separate pre-read of the row before
	// calling it: an outer GetMCPServer followed by a second, independent
	// DeleteMCPServer call would leave a race window between the two
	// (nothing tying the row this reads to the row that transaction
	// actually deletes), which returning the deleted record here closes
	// entirely. A missing server or a bundled one still map to the same
	// NotFound/FailedPrecondition statuses as before.
	server, err := s.repo.DeleteMCPServer(ctx, req.GetServerId())
	if err != nil {
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
	// only the name and tier from the record DeleteMCPServer itself just
	// returned: no URL, no token-related key — narrower than
	// RegisterMcpServer's own payload, matching the reviewed policy in
	// audit/service.go.
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

// requestedTierFromProto maps a RegisterMcpServerRequest's declared tier to
// the optional assertion validateServerDefinition takes. The tier a server
// actually gets is always derived from its hardened URL — never chosen by
// the caller — so this only decides whether the caller is *also* asserting
// what that derivation must produce:
//
//   - UNSPECIFIED returns nil: the caller is not asserting anything, and the
//     URL's own classification stands. This is what a client built against
//     the tier-less version of this RPC sends, so those clients keep working
//     unchanged.
//   - LOCAL_CONTAINER / REMOTE_URL returns that tier: the URL must classify
//     to exactly it, or validateServerDefinition refuses the registration.
//     This is what the in-app registration form sends, so a user who picked
//     the wrong kind for a URL is told rather than silently corrected.
//   - BUNDLED is refused outright: it is TuringAgent's own tier, never
//     something an operator registers into.
func requestedTierFromProto(tier turingv1.McpServerTier) (*repository.MCPServerTier, error) {
	switch tier {
	case turingv1.McpServerTier_MCP_SERVER_TIER_UNSPECIFIED:
		return nil, nil
	case turingv1.McpServerTier_MCP_SERVER_TIER_LOCAL_CONTAINER:
		requested := repository.MCPServerTierLocalContainer
		return &requested, nil
	case turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL:
		requested := repository.MCPServerTierRemoteURL
		return &requested, nil
	default:
		return nil, status.Error(codes.InvalidArgument, "tier must be local-container or remote-url")
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

// errMCPBearerTokenBlank is the refusal for a bearer_token that carries
// something but normalizes to nothing (a paste that was all whitespace, or
// a stray tab). Both RegisterMcpServer and RotateMcpServerToken treat a
// genuinely empty bearer_token as "no token" / "clear the token", so a
// blank one would otherwise be silently indistinguishable from that
// deliberate choice — a mistake worth naming rather than absorbing.
var errMCPBearerTokenBlank = errors.New("bearer token must not be only whitespace; send an empty token to leave the server without one")

// requireNonBlankBearerToken refuses a bearer that is present on the wire
// but empty after normalization. It is applied at the RPC boundary only:
// mcp.json import reaches the same refusal through bearerFromHeaders' own
// errMCPAuthorizationHeaderMalformed, which can additionally name the
// header the operator must fix.
func requireNonBlankBearerToken(raw string) error {
	if raw == "" {
		return nil
	}
	normalized, err := normalizeBearerToken(raw)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if normalized == "" {
		return status.Error(codes.InvalidArgument, errMCPBearerTokenBlank.Error())
	}
	return nil
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
	tier, err := requestedTierFromProto(req.GetTier())
	if err != nil {
		return nil, err
	}
	if err := requireNonBlankBearerToken(req.GetBearerToken()); err != nil {
		return nil, err
	}
	validated, err := validateServerDefinition(req.GetName(), req.GetUrl(), tier, req.GetBearerToken())
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
	if err := requireNonBlankBearerToken(req.GetBearerToken()); err != nil {
		return nil, err
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
			// The row was deleted between the unlocked precheck above and
			// this re-read — possibly by a DeleteMcpServer that has
			// already forgotten this id's credentialLocks entry (its own
			// forget only ever runs once, on its own successful delete),
			// meaning credentialLock above may itself have just
			// (re-)created the entry this cleans back up: see
			// forgetCredentialLockIfCurrent's own doc comment.
			s.forgetCredentialLockIfCurrent(serverID, lock)
			return repository.MCPServerRecord{}, status.Error(codes.NotFound, "MCP server not found")
		}
		return repository.MCPServerRecord{}, status.Error(codes.Internal, "read MCP server failed")
	}
	if server.Tier == repository.MCPServerTierBundled {
		return repository.MCPServerRecord{}, status.Error(codes.FailedPrecondition, bundledServerRegistrationMessage)
	}
	// Symmetric with validateServerDefinition's own check for
	// register/import: there is no new name or URL being set during a
	// rotation, only a new token, so this compares that new token
	// against the *existing* row's own name/url — both exactly as
	// public as they are for a fresh registration (see
	// tokenAppearsInPublicMetadata's own doc comment for why either
	// makes the token unable to be secret).
	if tokenAppearsInPublicMetadata(token, server.Name, server.URL) {
		return repository.MCPServerRecord{}, status.Error(codes.InvalidArgument, errMCPTokenMatchesPublicMetadata.Error())
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

// ReimportMcpJson re-reads and imports mcp.json from the configured config
// root on demand, the same way app startup does via ReimportConfiguredJSON.
// notify and audit both run immediately once that mutation has already
// committed — before anything below could still fail — and exactly once
// per call, so a later Internal mapping (see below) is never a second,
// separate audit of the same run. notify is conditional: it only fires
// when this reimport actually imported something, since nothing else about
// the registry changed for a run that only skipped or refused entries.
// audit always runs, unconditionally, recording only counts; unlike notify
// it is never skipped by a later failure, and two overlapping
// ReimportMcpJson calls can never suppress or delay each other's audit
// record. The one exception is errMCPConfigRootNotConfigured: that
// precondition failure means no work was even attempted (the report is
// always empty), so neither notify nor audit fires for it.
//
// ReimportConfiguredJSON's own named-report contract (see ImportJSON and
// recordDocumentRefusal) means report.Imported/Skipped/Unsupported are
// accurate even when it also returns a non-nil err: every name in Imported
// has already committed through its own per-entry transaction by the time
// any later, whole-document failure (a canceled context, or a repository
// failure between entries) is discovered. notify and audit therefore run
// from whatever ImportReport was returned before this ever maps that err
// to a final Internal status below — not only on the success path — so a
// partially-completed run's real, already-committed effect is never
// silently unreported.
//
// The response's Unsupported list is built directly from this call's own
// report.Unsupported, sorted by name, rather than by re-reading the shared
// mcp_import_issues table (which ListMcpServers still uses for its own
// Unsupported list): a concurrent reimport can freely overwrite that table
// with its own, different refusals without ever being able to leak into —
// or borrow from — this call's response.
func (s *Server) ReimportMcpJson(ctx context.Context, _ *turingv1.ReimportMcpJsonRequest) (*turingv1.ReimportMcpJsonResponse, error) {
	report, err := s.ReimportConfiguredJSON(ctx)
	if err != nil && errors.Is(err, errMCPConfigRootNotConfigured) {
		return nil, status.Error(codes.FailedPrecondition, "MCP config root is not configured")
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
	if err != nil {
		return nil, status.Error(codes.Internal, "reimport mcp.json failed")
	}
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
		Imported:    report.Imported,
		Skipped:     report.Skipped,
		Unsupported: refused,
	}, nil
}

// maxMCPToolResultWireBytes mirrors internal/app's own unexported
// maxGRPCMessageSize (4 * 1024 * 1024, the value grpc.MaxSendMsgSize
// configures both gRPC servers with): mcpregistry cannot import
// internal/app (app depends on mcpregistry, not the reverse — the same
// constraint aggregate_response_budget_test.go's own
// maxGRPCMessageSizeForTest documents for the same reason), so this is
// the one place that value is duplicated for CallRegisteredMcpTool's own
// pre-send guard, rather than the gRPC server configuration itself
// living here.
//
// A vendor's raw tools/call JSON-RPC result is already bounded at the
// HTTP layer (mcpClient.request's own maxMCPResponseBytes, 1MiB) before
// this package ever sees it — but that bound is on the *raw JSON* text,
// not on what structpb.NewStruct converts it into. A JSON number array
// converts to a repeated google.protobuf.Value, each carrying a fixed
// 8-byte double plus its own field/wire-type framing: measured (see
// numberArraySchemaJSON and TestListMcpServersResponseWorstCaseStaysUnderGRPCMessageSize's
// own doc comment) at roughly a 5.5x expansion over the raw JSON for
// that adversarial shape — enough that a maxMCPResponseBytes-sized
// result already comfortably within the 1MiB HTTP bound can still
// convert to a protobuf message well past this 4MiB cap by itself,
// before gRPC's own send path ever sees it. CallRegisteredMcpTool checks
// the fully-built response against this value with proto.Size — the
// same size grpc-go's own uncompressed send path measures a message
// against (this app configures no compressor) — before ever returning
// it, so an oversized result is refused with a fixed, generic
// ResourceExhausted status here rather than only ever surfacing once
// gRPC's own maxSendMessageSize check rejects the send.
const maxMCPToolResultWireBytes = 4 * 1024 * 1024

// mcpToolResultTooLargeMessage is the fixed, generic reason
// CallRegisteredMcpTool returns when a result's converted wire size
// exceeds maxMCPToolResultWireBytes — never the result's own content
// (which could be arbitrarily large or carry vendor-controlled data) and
// never the tool's arguments or the server's bearer token.
const mcpToolResultTooLargeMessage = "MCP tool result exceeds the maximum supported size"

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
	response := &turingv1.CallRegisteredMcpToolResponse{Result: value}
	// This guard applies only to the *gRPC response* CallRegisteredMcpTool
	// itself returns — checked after the full response is built (so it
	// measures exactly what would be marshaled and sent, not an estimate
	// from the raw result alone) but before it is ever returned to the
	// gRPC layer. CallTool's own return value — the map[string]any
	// `result` this method received above — is a completely separate,
	// unaffected code path: CallRegisteredMcpTool is CallTool's only
	// direct caller today, so there is no other in-process caller for
	// this check to (correctly or incorrectly) claim is unaffected.
	if proto.Size(response) > maxMCPToolResultWireBytes {
		return nil, status.Error(codes.ResourceExhausted, mcpToolResultTooLargeMessage)
	}
	return response, nil
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
	if s.discoverCredentialLockBarrier != nil {
		s.discoverCredentialLockBarrier()
	}
	lock := s.credentialLock(serverID)
	lock.RLock()
	defer lock.RUnlock()
	discoverErr := s.discover(ctx, serverID)
	if discoverErr != nil {
		if errors.Is(discoverErr, repository.ErrMCPServerNotFound) {
			// The server was deleted between whatever precheck the
			// caller (SetMcpServerEnabled) already ran and this call
			// reaching credentialLock above — which may be exactly what
			// (re-)created this lock's entry, for an id DeleteMcpServer
			// will never forget again (see
			// forgetCredentialLockIfCurrent's own doc comment). There is
			// no status to record for a server row that no longer
			// exists, so this returns directly rather than also
			// attempting SetMCPServerStatus below.
			s.forgetCredentialLockIfCurrent(serverID, lock)
			return false, nil
		}
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

// serverDescriptor builds a single server's descriptor via its own,
// independent repository read of that server's tools — used by every
// single-server RPC (SetMcpServerEnabled, RegisterMcpServer,
// RotateMcpServerToken, UpdateMcpToolPolicy) where one extra query for one
// server's own tools carries none of the cross-server coherence risk a
// whole-registry listing does. ListMcpServers does not use this: it reads
// every server's tools from its own single snapshot transaction (see
// repository.MCPRegistrySnapshot) and calls buildServerDescriptor directly
// with tools already in hand, rather than one more per-server query each.
func (s *Server) serverDescriptor(ctx context.Context, server repository.MCPServerRecord) (*turingv1.McpServerDescriptor, error) {
	tools, err := s.repo.ListMCPServerTools(ctx, server.ID)
	if err != nil {
		return nil, err
	}
	return buildServerDescriptor(server, tools)
}

// buildServerDescriptor is the pure conversion serverDescriptor and
// ListMcpServers both need: a server record plus the tools already read
// for it, with no repository access of its own.
func buildServerDescriptor(server repository.MCPServerRecord, tools []repository.MCPServerTool) (*turingv1.McpServerDescriptor, error) {
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
