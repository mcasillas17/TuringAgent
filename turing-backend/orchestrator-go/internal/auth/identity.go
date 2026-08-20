package auth

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ServiceIdentity is a least-privilege internal caller: a bearer token paired
// with the exact set of gRPC methods it may invoke. Identity is derived
// solely from which registered token matches the presented bearer — never
// from a caller-supplied claim — so a compromised process cannot elevate
// itself by asserting a different name.
type ServiceIdentity struct {
	Name    string
	Token   string
	Methods map[string]struct{}
}

// NewServiceIdentity builds a ServiceIdentity authorized to call exactly the
// given full gRPC method names (for example
// "/turing.v1.RuntimeService/ConnectWorker").
func NewServiceIdentity(name string, token string, methods ...string) ServiceIdentity {
	set := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		set[method] = struct{}{}
	}
	return ServiceIdentity{Name: name, Token: token, Methods: set}
}

// NewInternalIdentities validates a set of identities before they are wired
// into a server: every token must be non-empty, and no two identities may
// share a token. A shared token would silently merge two identities'
// privileges into one; an empty token could never match a presented bearer,
// silently locking that identity out entirely. Both are configuration
// mistakes best caught at startup rather than discovered as an authorization
// bug in production.
func NewInternalIdentities(identities []ServiceIdentity) ([]ServiceIdentity, error) {
	seen := make(map[string]string, len(identities))
	for _, identity := range identities {
		if identity.Token == "" {
			return nil, fmt.Errorf("service identity %q has an empty token", identity.Name)
		}
		if owner, ok := seen[identity.Token]; ok {
			return nil, fmt.Errorf("service identities %q and %q share a token", owner, identity.Name)
		}
		seen[identity.Token] = identity.Name
	}
	return identities, nil
}

// UnknownIdentityActorType is recorded for a failure attributed to no
// registered identity — either no bearer was presented or it matched no
// configured token — so audit rows never confuse an unauthenticated caller
// with a legitimate but overreaching one.
const UnknownIdentityActorType = "internal-unknown"

// UnaryIdentityInterceptor authenticates the bearer token against a fixed set
// of internal identities and authorizes the call only if the resolved
// identity's allowlist contains the invoked method.
func UnaryIdentityInterceptor(identities []ServiceIdentity, options InterceptorOptions) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		identity, ok := authorizeIdentity(ctx, identities, info.FullMethod, options)
		if !ok {
			return nil, identityDenialError(identity, info.FullMethod)
		}
		return handler(ctx, req)
	}
}

// StreamIdentityInterceptor is StreamUnaryInterceptor's streaming
// counterpart, used for the internal server's ConnectWorker stream.
func StreamIdentityInterceptor(identities []ServiceIdentity, options InterceptorOptions) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		identity, ok := authorizeIdentity(stream.Context(), identities, info.FullMethod, options)
		if !ok {
			return identityDenialError(identity, info.FullMethod)
		}
		return handler(srv, stream)
	}
}

// authorizeIdentity resolves the caller's identity and checks its allowlist.
// It returns the matched identity (zero value if none matched) and whether
// the call is authorized; on any denial it records an audit failure
// attributed to the resolved identity name, or UnknownIdentityActorType when
// no token matched at all.
func authorizeIdentity(ctx context.Context, identities []ServiceIdentity, fullMethod string, options InterceptorOptions) (ServiceIdentity, bool) {
	token, ok := TokenFromMetadata(ctx)
	if !ok {
		recordIdentityFailure(ctx, UnknownIdentityActorType, fullMethod, options)
		return ServiceIdentity{}, false
	}
	identity, matched := matchIdentity(identities, token)
	if !matched {
		recordIdentityFailure(ctx, UnknownIdentityActorType, fullMethod, options)
		return ServiceIdentity{}, false
	}
	if _, allowed := identity.Methods[fullMethod]; !allowed {
		recordIdentityFailure(ctx, identity.Name, fullMethod, options)
		return identity, false
	}
	return identity, true
}

// matchIdentity compares the presented token against every configured
// identity's token, rather than stopping at the first match, so the number
// of comparisons performed does not vary with which (if any) identity holds
// the matching token.
func matchIdentity(identities []ServiceIdentity, token string) (ServiceIdentity, bool) {
	matchedIndex := -1
	for index, identity := range identities {
		if TokenMatches(token, identity.Token) {
			matchedIndex = index
		}
	}
	if matchedIndex == -1 {
		return ServiceIdentity{}, false
	}
	return identities[matchedIndex], true
}

// identityDenialError reports Unauthenticated when no identity was resolved
// at all, and PermissionDenied when a real identity's token was valid but the
// method was outside its allowlist — the same distinction the rest of this
// package draws between "who are you" and "what are you allowed to do".
func identityDenialError(identity ServiceIdentity, fullMethod string) error {
	if identity.Name == "" {
		return status.Error(codes.Unauthenticated, "invalid bearer token")
	}
	return status.Errorf(codes.PermissionDenied, "identity %q is not authorized to call %s", identity.Name, fullMethod)
}

func recordIdentityFailure(ctx context.Context, actorType string, method string, options InterceptorOptions) {
	scoped := options
	scoped.ActorType = actorType
	recordFailure(ctx, method, scoped)
}
