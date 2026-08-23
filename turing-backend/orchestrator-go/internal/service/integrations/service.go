// Package integrations serves the user's connected third-party accounts.
//
// Three rules shape everything here:
//
//  1. A stored credential is never returned. No response message in
//     integrations.proto has a field for one. The internal dispatch path reads
//     and unseals exactly the named credential for one provider call; public
//     management paths expose only provider metadata and a redaction.
//  2. Connecting requires consent to that provider's grants, in the same
//     request. A missing field is not agreement.
//  3. Revoking destroys the credential rather than hiding it.
package integrations

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A credential is a token or an app password. Long enough for anything a
// provider issues, short enough that this is not a place to paste a file.
const maxCredentialBytes = 4096

// Said the same way wherever it is said: on the catalogue, so a client can
// warn before the form, and on the refusal, for a client that asked anyway.
const unconfiguredReason = "integrations are not configured: set TURING_INTEGRATION_KEY in turing-backend/.env (run scripts/init.sh) so credentials can be stored sealed"

// AuditRecorder is the audit service, narrowed to the one method this package
// needs. Connecting an account is a consequential state change — it hands a
// third party standing access to the user's data — so it leaves a record, the
// same way session deletion does. The record names the provider and the
// connection; it never carries the credential or its hint.
type AuditRecorder interface {
	Record(ctx context.Context, correlationID, actorType, actorID, action, target string, payload map[string]any) error
}

type CredentialSealer interface {
	Seal(plaintext, boundTo []byte) ([]byte, error)
	Open(sealed, boundTo []byte) ([]byte, error)
	SealedWithThisKey(sealedPrefix []byte) bool
}

type ApprovalEnforcer interface {
	ConsumeApprovalForThirdParty(ctx context.Context, approvalID, runID, serverName, toolName string, args map[string]any) error
}

type RegistryChangeNotifier interface {
	NotifyMCPRegistryChanged(context.Context) error
}

type Server struct {
	turingv1.UnimplementedIntegrationServiceServer
	repo  *repository.Repository
	audit AuditRecorder
	// sealer is nil when TURING_INTEGRATION_KEY is unset. Connecting then
	// fails with a reason, rather than quietly writing the credential in the
	// clear — a feature that silently downgrades its own protection is worse
	// than one that says it is not configured.
	sealer     CredentialSealer
	approvals  ApprovalEnforcer
	notifier   RegistryChangeNotifier
	httpClient *http.Client
	lookupIP   backendegress.LookupIP
}

func New(repo *repository.Repository, sealer CredentialSealer, audit AuditRecorder) *Server {
	return &Server{repo: repo, sealer: sealer, audit: audit, lookupIP: net.DefaultResolver.LookupIPAddr}
}

func (s *Server) SetApprovalEnforcer(enforcer ApprovalEnforcer)             { s.approvals = enforcer }
func (s *Server) SetRegistryChangeNotifier(notifier RegistryChangeNotifier) { s.notifier = notifier }
func (s *Server) SetHTTPClient(client *http.Client)                         { s.httpClient = client }

func (s *Server) notifyRegistryChanged() {
	if s.notifier != nil {
		if err := s.notifier.NotifyMCPRegistryChanged(context.Background()); err != nil {
			log.Printf("notify runtime of integration registry change: %v", err)
		}
	}
}

func (s *Server) ListProviders(context.Context, *turingv1.ListProvidersRequest) (*turingv1.ListProvidersResponse, error) {
	descriptors := make([]*turingv1.ProviderDescriptor, 0, len(catalogue))
	for _, entry := range catalogue {
		descriptors = append(descriptors, entry.descriptor())
	}
	// Whether anything can be stored travels with the catalogue rather than
	// being discovered on submit, so a client can say "this cannot hold a
	// credential yet" before it asks somebody to paste a live app password
	// into a form that will fail.
	response := &turingv1.ListProvidersResponse{
		Providers:                   descriptors,
		CredentialStorageConfigured: s.sealer != nil,
	}
	if s.sealer == nil {
		response.StorageUnconfiguredReason = unconfiguredReason
	}
	return response, nil
}

func (s *Server) ConnectAccount(ctx context.Context, req *turingv1.ConnectAccountRequest) (*turingv1.Connection, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	entry, known := lookupProvider(req.GetProvider())
	if !known {
		return nil, status.Error(codes.InvalidArgument, "unknown provider")
	}
	if !entry.supported {
		// The reason travels with the refusal: a client that only knew
		// "unsupported" would have to invent an explanation.
		return nil, status.Errorf(codes.FailedPrecondition, "%s cannot be connected: %s", entry.displayName, entry.unsupportedReason)
	}
	// Consent first, so a request that forgot it cannot half-succeed, then
	// the key: picking over the form of a request we could not store either
	// way wastes the user's time.
	if !req.GetConsentAcknowledged() {
		return nil, status.Error(codes.PermissionDenied, "connecting an account requires agreeing to what it grants")
	}
	if s.sealer == nil {
		return nil, status.Error(codes.FailedPrecondition, unconfiguredReason)
	}
	credential := strings.TrimSpace(req.GetCredential())
	if credential == "" {
		return nil, status.Errorf(codes.InvalidArgument, "%s is required", strings.ToLower(entry.secretLabel))
	}
	if len(credential) > maxCredentialBytes {
		return nil, status.Error(codes.InvalidArgument, "credential is too long")
	}
	// A credential is eventually handed to a line-based protocol — IMAP, or
	// CalDAV over HTTP — where an embedded newline is not a typo but an
	// injected command. Refused at the boundary while the storage format is
	// still new, rather than left to the tool that dials the server.
	if strings.IndexFunc(credential, isForbiddenInCredential) >= 0 {
		return nil, status.Error(codes.InvalidArgument, "credential contains characters that are not part of a token")
	}
	endpoint := strings.TrimSpace(req.GetEndpoint())
	if entry.requiresEndpoint && endpoint == "" {
		return nil, status.Errorf(codes.InvalidArgument, "%s is required", strings.ToLower(entry.endpointLabel))
	}
	if !entry.requiresEndpoint {
		// A hosted provider has one address and it is not the user's to set.
		// Dropped rather than stored, so a form that left a stale value behind
		// cannot make a Notion connection claim to live on somebody's IMAP
		// server.
		endpoint = ""
	}
	// The id is chosen here so the credential can be sealed against it: the
	// sealed value is bound to this row and will not open under another, which
	// is what stops someone with write access to the database moving a token
	// into a connection pointing at a server they control.
	connectionID := ids.New("conn")
	sealed, err := s.sealer.Seal([]byte(credential), []byte(connectionID))
	if err != nil {
		// Deliberately not %v: an error from the sealing path is the last
		// place a credential should be able to reach a log line.
		return nil, status.Error(codes.Internal, "could not store the credential")
	}
	connection, err := s.repo.CreateConnection(ctx, repository.NewConnection{
		ConnectionID:         connectionID,
		Provider:             entry.storageKey,
		DisplayName:          req.GetDisplayName(),
		AccountLabel:         req.GetAccountLabel(),
		Endpoint:             endpoint,
		CredentialCiphertext: sealed,
		CredentialHint:       redact(credential),
		GrantedScopes:        entry.grants,
	})
	if err != nil {
		return nil, connectionError(err, "connect account failed")
	}
	s.record(ctx, "integration.connected", connection)
	s.notifyRegistryChanged()
	return s.toProto(connection), nil
}

func (s *Server) ListConnections(ctx context.Context, _ *turingv1.ListConnectionsRequest) (*turingv1.ListConnectionsResponse, error) {
	connections, err := s.repo.ListConnections(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list connections failed")
	}
	return &turingv1.ListConnectionsResponse{Connections: s.toProtoList(connections)}, nil
}

func (s *Server) GetConnection(ctx context.Context, req *turingv1.GetConnectionRequest) (*turingv1.Connection, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	connection, err := s.repo.GetConnection(ctx, req.GetConnectionId())
	if err != nil {
		return nil, connectionError(err, "get connection failed")
	}
	return s.toProto(connection), nil
}

func (s *Server) RevokeConnection(ctx context.Context, req *turingv1.RevokeConnectionRequest) (*turingv1.Connection, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	connection, err := s.repo.RevokeConnection(ctx, req.GetConnectionId())
	if err != nil {
		return nil, connectionError(err, "revoke connection failed")
	}
	s.record(ctx, "integration.revoked", connection)
	s.notifyRegistryChanged()
	return s.toProto(connection), nil
}

func (s *Server) DeleteConnection(ctx context.Context, req *turingv1.DeleteConnectionRequest) (*turingv1.DeleteConnectionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	// Read first: once the row is gone there is nothing left to describe in
	// the record of its going.
	connection, err := s.repo.GetConnection(ctx, req.GetConnectionId())
	if err != nil {
		return nil, connectionError(err, "delete connection failed")
	}
	if err := s.repo.DeleteConnection(ctx, req.GetConnectionId()); err != nil {
		return nil, connectionError(err, "delete connection failed")
	}
	s.record(ctx, "integration.deleted", connection)
	s.notifyRegistryChanged()
	return &turingv1.DeleteConnectionResponse{}, nil
}

// connectionError maps the repository's named failures to codes a client can
// act on. Everything else collapses to Internal with a fixed message, so a
// storage error never leaks its text — which for this table would mean
// leaking a SQL statement that names the credential column.
func connectionError(err error, fallback string) error {
	switch {
	case errors.Is(err, repository.ErrConnectionNotFound):
		return status.Error(codes.NotFound, "connection not found")
	case errors.Is(err, repository.ErrConnectionAlreadyRevoked):
		return status.Error(codes.FailedPrecondition, "that connection is already revoked")
	case errors.Is(err, repository.ErrConnectionNameTaken):
		return status.Error(codes.AlreadyExists, "a connection with that name already exists")
	case errors.Is(err, repository.ErrConnectionNameEmpty):
		return status.Error(codes.InvalidArgument, "connection name is required")
	case errors.Is(err, repository.ErrConnectionNameTooLong):
		return status.Error(codes.InvalidArgument, "connection name is too long")
	case errors.Is(err, repository.ErrConnectionAccountTooLong):
		return status.Error(codes.InvalidArgument, "account label is too long")
	case errors.Is(err, repository.ErrConnectionEndpointTooLong):
		return status.Error(codes.InvalidArgument, "endpoint is too long")
	case errors.Is(err, repository.ErrConnectionEndpointInvalid):
		return status.Error(codes.InvalidArgument, "endpoint must be a host name, without spaces")
	case errors.Is(err, repository.ErrConnectionProviderRequired):
		return status.Error(codes.InvalidArgument, "provider is required")
	case errors.Is(err, repository.ErrConnectionSecretRequired):
		return status.Error(codes.InvalidArgument, "a credential is required")
	case errors.Is(err, repository.ErrConnectionConsentRequired):
		return status.Error(codes.PermissionDenied, "connecting an account requires agreeing to what it grants")
	default:
		return status.Error(codes.Internal, fallback)
	}
}

// redact produces the only thing a client is ever told about a credential.
//
// Four trailing characters are enough to tell two tokens apart when you are
// looking at the one you just pasted, and useless to anyone else. Anything
// short enough that four characters would be a meaningful fraction of it gets
// nothing but bullets.
func redact(credential string) string {
	const revealed = 4
	const minLengthToReveal = 12
	if utf8.RuneCountInString(credential) < minLengthToReveal {
		return "••••••••"
	}
	runes := []rune(credential)
	return "••••••••" + string(runes[len(runes)-revealed:])
}

func (s *Server) toProtoList(connections []repository.Connection) []*turingv1.Connection {
	converted := make([]*turingv1.Connection, 0, len(connections))
	for _, connection := range connections {
		converted = append(converted, s.toProto(connection))
	}
	return converted
}

// toProto is the whole read mapping, and it is written out field by field on
// purpose: there is no struct copy here that could pick up a credential if one
// were ever added to the repository type.
func (s *Server) toProto(connection repository.Connection) *turingv1.Connection {
	kind := turingv1.IntegrationProvider_INTEGRATION_PROVIDER_UNSPECIFIED
	if entry, known := providerByStorageKey(connection.Provider); known {
		kind = entry.kind
	}
	return &turingv1.Connection{
		ConnectionId:     connection.ConnectionID,
		Provider:         kind,
		DisplayName:      connection.DisplayName,
		AccountLabel:     connection.AccountLabel,
		Endpoint:         connection.Endpoint,
		CredentialHint:   connection.CredentialHint,
		Status:           statusToProto(connection.Status),
		GrantedScopes:    append([]string(nil), connection.GrantedScopes...),
		ConsentGrantedAt: parseTimestamp(connection.ConsentGrantedAt),
		ConnectedAt:      parseTimestamp(connection.ConnectedAt),
		RevokedAt:        parseTimestamp(connection.RevokedAt),
		UpdatedAt:        parseTimestamp(connection.UpdatedAt),
		// Answered from five bytes of header, without opening anything. A
		// connection whose key is gone is unusable, and saying so beats
		// leaving it to claim access it no longer has.
		CredentialUnreadable: len(connection.CredentialHeader) > 0 &&
			(s.sealer == nil || !s.sealer.SealedWithThisKey(connection.CredentialHeader)),
	}
}

// record writes the audit row for a change to standing third-party access.
//
// The payload is deliberately thin: the provider and the name the user gave
// it, so the record says what happened without retaining anything about the
// credential. An audit failure does not fail the request — the change has
// already happened, and refusing to report it would be the second problem —
// but it is not swallowed silently either.
func (s *Server) record(ctx context.Context, action string, connection repository.Connection) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(ctx, "", "client", "", action, connection.ConnectionID, map[string]any{
		"provider":    connection.Provider,
		"displayName": connection.DisplayName,
	}); err != nil {
		log.Printf("record %s for %s: %v", action, connection.ConnectionID, err)
	}
}

// isForbiddenInCredential reports characters no provider puts in a token and
// that a line-based protocol would read as structure.
func isForbiddenInCredential(r rune) bool {
	return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
}

func statusToProto(value string) turingv1.ConnectionStatus {
	switch value {
	case repository.ConnectionStatusConnected:
		return turingv1.ConnectionStatus_CONNECTION_STATUS_CONNECTED
	case repository.ConnectionStatusRevoked:
		return turingv1.ConnectionStatus_CONNECTION_STATUS_REVOKED
	default:
		// A status this build does not know is not reported as connected.
		// Claiming an account is live when we cannot tell is the one wrong
		// answer here.
		return turingv1.ConnectionStatus_CONNECTION_STATUS_UNSPECIFIED
	}
}

func parseTimestamp(value string) *timestamppb.Timestamp {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return timestamppb.New(parsed)
}
