package auth

import (
	"context"
	"crypto/subtle"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	maxRequestIDLength = 128
	maxMetadataLength  = 256
	authAuditTimeout   = time.Second
)

type Failure struct {
	ActorType string
	Method    string
	RequestID string
	UserAgent string
	Peer      string
}

type FailureRecorder func(context.Context, Failure) error

type InterceptorOptions struct {
	ActorType       string
	FailureRecorder FailureRecorder
}

func TokenFromMetadata(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return "", false
	}
	raw := values[0]
	if !strings.HasPrefix(raw, "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(raw, "Bearer ")
	return token, token != ""
}

func TokenMatches(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func UnaryInterceptor(requiredToken string, configured ...InterceptorOptions) grpc.UnaryServerInterceptor {
	options := interceptorOptions(configured)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		token, ok := TokenFromMetadata(ctx)
		if !ok || !TokenMatches(token, requiredToken) {
			recordFailure(ctx, info.FullMethod, options)
			return nil, status.Error(codes.Unauthenticated, "invalid bearer token")
		}
		return handler(ctx, req)
	}
}

func StreamInterceptor(requiredToken string, configured ...InterceptorOptions) grpc.StreamServerInterceptor {
	options := interceptorOptions(configured)
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		token, ok := TokenFromMetadata(stream.Context())
		if !ok || !TokenMatches(token, requiredToken) {
			recordFailure(stream.Context(), info.FullMethod, options)
			return status.Error(codes.Unauthenticated, "invalid bearer token")
		}
		return handler(srv, stream)
	}
}

func interceptorOptions(configured []InterceptorOptions) InterceptorOptions {
	if len(configured) == 0 {
		return InterceptorOptions{}
	}
	return configured[0]
}

func recordFailure(ctx context.Context, method string, options InterceptorOptions) {
	if options.FailureRecorder == nil {
		return
	}
	failure := Failure{
		ActorType: options.ActorType,
		Method:    bounded(method, maxMetadataLength),
		RequestID: metadataValue(ctx, "x-request-id", maxRequestIDLength),
		UserAgent: metadataValue(ctx, "user-agent", maxMetadataLength),
	}
	if remote, ok := peer.FromContext(ctx); ok && remote.Addr != nil {
		failure.Peer = bounded(remote.Addr.String(), maxMetadataLength)
	}
	auditCtx, cancel := context.WithTimeout(context.Background(), authAuditTimeout)
	defer cancel()
	if err := options.FailureRecorder(auditCtx, failure); err != nil {
		log.Printf("record authentication failure for %s: %v", failure.Method, err)
	}
}

func metadataValue(ctx context.Context, key string, limit int) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return bounded(values[0], limit)
}

func bounded(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
