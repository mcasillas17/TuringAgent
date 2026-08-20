package approval

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestValidateChecksClaimsAndConsumesApprovalOverGRPC(t *testing.T) {
	server := &recordingApprovalService{status: turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED}
	addr, dialer := startApprovalServer(t, server)
	consumer := Consumer{
		OrchestratorGRPCAddr:  addr,
		ApprovalConsumerToken: "internal",
		JWTSecret:             "secret",
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(dialer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	}
	args := map[string]any{"content": "hello", "path": "note.txt"}
	token := signTestToken(t, "secret", Claims{Iss: "turing.orchestrator", Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix()})

	if err := consumer.Validate(token, "files.create", args, "general_assistant"); err != nil {
		t.Fatalf("expected valid approval: %v", err)
	}
	if server.approvalID != "appr_1" {
		t.Fatalf("expected ConsumeApproval approval_id appr_1, got %q", server.approvalID)
	}
	if server.authorization != "Bearer internal" {
		t.Fatalf("expected internal bearer metadata, got %q", server.authorization)
	}
}

func TestValidateRejectsMismatchedApprovalBinding(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "note.txt"}
	base := Claims{Iss: "turing.orchestrator", Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix()}
	cases := []struct {
		name   string
		claims Claims
		tool   string
		args   map[string]any
		agent  string
	}{
		{"audience", withClaim(base, func(c *Claims) { c.Aud = "other" }), "files.create", args, "general_assistant"},
		{"subject", withClaim(base, func(c *Claims) { c.Sub = "other_agent" }), "files.create", args, "general_assistant"},
		{"tool", withClaim(base, func(c *Claims) { c.Tool = "files.update" }), "files.create", args, "general_assistant"},
		{"args_hash", base, "files.create", map[string]any{"content": "changed", "path": "note.txt"}, "general_assistant"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			consumer := Consumer{ApprovalConsumerToken: "internal", JWTSecret: "secret"}
			if err := consumer.Validate(signTestToken(t, "secret", tc.claims), tc.tool, tc.args, tc.agent); err == nil {
				t.Fatalf("expected validation failure")
			}
		})
	}
}

func TestVerifyHS256RejectsMissingOrUnexpectedIssuer(t *testing.T) {
	base := Claims{Iss: "turing.orchestrator", Exp: time.Now().Add(time.Minute).Unix()}
	for _, issuer := range []string{"", "other.issuer"} {
		t.Run(issuer, func(t *testing.T) {
			claims := base
			claims.Iss = issuer
			if _, err := VerifyHS256(signTestToken(t, "secret", claims), "secret"); err == nil {
				t.Fatalf("VerifyHS256 accepted issuer %q", issuer)
			}
		})
	}
}

func TestVerifyHS256RejectsMissingOrUnexpectedTokenType(t *testing.T) {
	claims := Claims{Iss: "turing.orchestrator", Exp: time.Now().Add(time.Minute).Unix()}
	for _, tokenType := range []string{"", "approval+jwt"} {
		t.Run(tokenType, func(t *testing.T) {
			token := signTestTokenWithType(t, "secret", claims, tokenType)
			if _, err := VerifyHS256(token, "secret"); err == nil {
				t.Fatalf("VerifyHS256 accepted token type %q", tokenType)
			}
		})
	}
}

func TestVerifyHS256ExpirationBoundary(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	claims := Claims{Iss: "turing.orchestrator", Exp: now.Unix()}
	if _, err := verifyHS256At(signTestToken(t, "secret", claims), "secret", now); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("verifyHS256At accepted exp == now: %v", err)
	}

	claims.Exp = now.Add(time.Second).Unix()
	if _, err := verifyHS256At(signTestToken(t, "secret", claims), "secret", now); err != nil {
		t.Fatalf("verifyHS256At rejected exp one second after now: %v", err)
	}
}

func TestValidateRejectsConsumeReplayConflict(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "note.txt"}
	server := &recordingApprovalService{oneShot: true}
	_, dialer := startApprovalServer(t, server)
	consumer := Consumer{
		OrchestratorGRPCAddr:  "bufnet",
		ApprovalConsumerToken: "internal",
		JWTSecret:             "secret",
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(dialer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	}
	token := signTestToken(t, "secret", Claims{Iss: "turing.orchestrator", Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix()})
	if err := consumer.Validate(token, "files.create", args, "general_assistant"); err != nil {
		t.Fatalf("first validation failed: %v", err)
	}
	if err := consumer.Validate(token, "files.create", args, "general_assistant"); err == nil || !strings.Contains(err.Error(), "already consumed") {
		t.Fatalf("second application validation error = %v, want consumed replay rejection", err)
	}
	if server.consumeCalls != 2 {
		t.Fatalf("ConsumeApproval calls = %d, want one call per application validation", server.consumeCalls)
	}
}

func TestValidateDoesNotRetryConsumeTransportFailureWithOneShotToken(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "note.txt"}
	server := &recordingApprovalService{err: status.Error(codes.Unavailable, "transport interrupted")}
	_, dialer := startApprovalServer(t, server)
	consumer := Consumer{
		OrchestratorGRPCAddr:  "bufnet",
		ApprovalConsumerToken: "internal",
		JWTSecret:             "secret",
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(dialer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	}
	token := signTestToken(t, "secret", Claims{Iss: "turing.orchestrator", Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix()})

	if err := consumer.Validate(token, "files.create", args, "general_assistant"); err == nil {
		t.Fatal("Validate returned nil error for interrupted consume")
	}
	if server.consumeCalls != 1 {
		t.Fatalf("ConsumeApproval calls = %d, want no transport retry with a one-shot token", server.consumeCalls)
	}
}

func TestValidateContextDerivesConsumeCancellationFromCaller(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "note.txt"}
	client := &blockingApprovalClient{
		started: make(chan struct{}),
	}
	consumer := Consumer{
		ApprovalConsumerToken: "internal",
		JWTSecret:             "secret",
		ApprovalClient:        client,
	}
	token := signTestToken(t, "secret", Claims{
		Iss:      "turing.orchestrator",
		Sub:      "general_assistant",
		Aud:      "mcp-files",
		JTI:      "appr_1",
		Tool:     "files.create",
		ArgsHash: hashArgs(t, args),
		Exp:      time.Now().Add(time.Minute).Unix(),
		Iat:      time.Now().Unix(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- consumer.ValidateContext(ctx, token, "files.create", args, "general_assistant")
	}()

	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("approval consume did not start")
	}
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ValidateContext error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ValidateContext did not stop after caller cancellation")
	}
	if client.consumed {
		t.Fatal("canceled approval was consumed")
	}
}

func TestValidateContextDoesNotStartConsumeWhenAlreadyCanceled(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "note.txt"}
	client := &blockingApprovalClient{started: make(chan struct{})}
	consumer := Consumer{
		ApprovalConsumerToken: "internal",
		JWTSecret:             "secret",
		ApprovalClient:        client,
	}
	token := signTestToken(t, "secret", Claims{
		Iss:      "turing.orchestrator",
		Sub:      "general_assistant",
		Aud:      "mcp-files",
		JTI:      "appr_1",
		Tool:     "files.create",
		ArgsHash: hashArgs(t, args),
		Exp:      time.Now().Add(time.Minute).Unix(),
		Iat:      time.Now().Unix(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := consumer.ValidateContext(ctx, token, "files.create", args, "general_assistant")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateContext error = %v, want context.Canceled", err)
	}
	select {
	case <-client.started:
		t.Fatal("approval consume started for an already canceled request")
	default:
	}
}

func TestCanonicalArgsHashMatchesTypeScriptFixture(t *testing.T) {
	if got := hashArgs(t, map[string]any{"B": float64(1), "a": float64(2)}); got != "sha256:812e5e7fb7bb816dc477e91a136430192eadcf83ff303881298146e106ae0161" {
		t.Fatalf("unexpected canonical hash %s", got)
	}
}

type blockingApprovalClient struct {
	started  chan struct{}
	consumed bool
}

func (c *blockingApprovalClient) FinalizeSandboxArtifact(ctx context.Context, _ *turingv1.FinalizeSandboxArtifactRequest, _ ...grpc.CallOption) (*turingv1.FinalizeSandboxArtifactResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *blockingApprovalClient) CheckSessionCapability(ctx context.Context, _ *turingv1.CheckSessionCapabilityRequest, _ ...grpc.CallOption) (*turingv1.SessionCapabilityState, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *blockingApprovalClient) ConsumeApproval(ctx context.Context, _ *turingv1.ConsumeApprovalRequest, _ ...grpc.CallOption) (*turingv1.ApprovalResponse, error) {
	close(c.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

type recordingApprovalService struct {
	turingv1.UnimplementedApprovalServiceServer
	approvalID          string
	authorization       string
	status              turingv1.ApprovalStatus
	err                 error
	consumeCalls        int
	oneShot             bool
	finalizedArtifactID string
	capabilityActive    bool
	checkedCapability   string
}

func (s *recordingApprovalService) CheckSessionCapability(_ context.Context, req *turingv1.CheckSessionCapabilityRequest) (*turingv1.SessionCapabilityState, error) {
	s.checkedCapability = req.GetProvenanceToken()
	return &turingv1.SessionCapabilityState{Active: s.capabilityActive}, nil
}

func (s *recordingApprovalService) FinalizeSandboxArtifact(_ context.Context, req *turingv1.FinalizeSandboxArtifactRequest) (*turingv1.FinalizeSandboxArtifactResponse, error) {
	s.finalizedArtifactID = req.GetArtifactId()
	return &turingv1.FinalizeSandboxArtifactResponse{ArtifactId: req.GetArtifactId(), State: "ready"}, nil
}

func (s *recordingApprovalService) ConsumeApproval(ctx context.Context, req *turingv1.ConsumeApprovalRequest) (*turingv1.ApprovalResponse, error) {
	s.consumeCalls++
	if s.err != nil {
		return nil, s.err
	}
	if s.oneShot && s.consumeCalls > 1 {
		return nil, status.Error(codes.FailedPrecondition, "approval already consumed")
	}
	s.approvalID = req.GetApprovalId()
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		values := md.Get("authorization")
		if len(values) > 0 {
			s.authorization = values[0]
		}
	}
	status := s.status
	if status == turingv1.ApprovalStatus_APPROVAL_STATUS_UNSPECIFIED {
		status = turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED
	}
	return &turingv1.ApprovalResponse{ApprovalId: req.GetApprovalId(), Status: status}, nil
}

func startApprovalServer(t *testing.T, approvalServer turingv1.ApprovalServiceServer) (string, func(context.Context, string) (net.Conn, error)) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	turingv1.RegisterApprovalServiceServer(server, approvalServer)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	return "bufnet", func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}
}

func signTestToken(t *testing.T, secret string, claims Claims) string {
	t.Helper()
	return signTestTokenWithType(t, secret, claims, "JWT")
}

func signTestTokenWithType(t *testing.T, secret string, claims Claims, tokenType string) string {
	t.Helper()
	headerBytes, err := json.Marshal(map[string]string{"alg": "HS256", "typ": tokenType})
	if err != nil {
		t.Fatal(err)
	}
	header := base64.RawURLEncoding.EncodeToString(headerBytes)
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	input := header + "." + payload
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func hashArgs(t *testing.T, args map[string]any) string {
	t.Helper()
	canonical, err := canonicalJSON(args)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func withClaim(claims Claims, mutate func(*Claims)) Claims {
	mutate(&claims)
	return claims
}
