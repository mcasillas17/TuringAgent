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
		OrchestratorGRPCAddr: addr,
		InternalToken:        "internal",
		JWTSecret:            "secret",
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(dialer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	}
	args := map[string]any{"content": "hello", "path": "note.txt"}
	token := signTestToken(t, "secret", Claims{Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix()})

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
	base := Claims{Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix()}
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
			consumer := Consumer{InternalToken: "internal", JWTSecret: "secret"}
			if err := consumer.Validate(signTestToken(t, "secret", tc.claims), tc.tool, tc.args, tc.agent); err == nil {
				t.Fatalf("expected validation failure")
			}
		})
	}
}

func TestValidateRejectsConsumeReplayConflict(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "note.txt"}
	_, dialer := startApprovalServer(t, &recordingApprovalService{err: status.Error(codes.FailedPrecondition, "approval is not approved")})
	consumer := Consumer{
		OrchestratorGRPCAddr: "bufnet",
		InternalToken:        "internal",
		JWTSecret:            "secret",
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(dialer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	}
	token := signTestToken(t, "secret", Claims{Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix()})
	if err := consumer.Validate(token, "files.create", args, "general_assistant"); err == nil {
		t.Fatalf("expected replay/consume conflict")
	}
}

func TestValidateContextDerivesConsumeCancellationFromCaller(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "note.txt"}
	client := &blockingApprovalClient{
		started: make(chan struct{}),
	}
	consumer := Consumer{
		InternalToken:  "internal",
		JWTSecret:      "secret",
		ApprovalClient: client,
	}
	token := signTestToken(t, "secret", Claims{
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
		InternalToken:  "internal",
		JWTSecret:      "secret",
		ApprovalClient: client,
	}
	token := signTestToken(t, "secret", Claims{
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

func (c *blockingApprovalClient) ConsumeApproval(ctx context.Context, _ *turingv1.ConsumeApprovalRequest, _ ...grpc.CallOption) (*turingv1.ApprovalResponse, error) {
	close(c.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

type recordingApprovalService struct {
	turingv1.UnimplementedApprovalServiceServer
	approvalID    string
	authorization string
	status        turingv1.ApprovalStatus
	err           error
}

func (s *recordingApprovalService) ConsumeApproval(ctx context.Context, req *turingv1.ConsumeApprovalRequest) (*turingv1.ApprovalResponse, error) {
	if s.err != nil {
		return nil, s.err
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
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
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
