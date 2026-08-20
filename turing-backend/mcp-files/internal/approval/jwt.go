package approval

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	approvalTokenIssuer = "turing.orchestrator"
	approvalTokenType   = "JWT"
)

type Claims struct {
	Iss      string `json:"iss"`
	Sub      string `json:"sub"`
	Aud      string `json:"aud"`
	JTI      string `json:"jti"`
	Tool     string `json:"tool"`
	ArgsHash string `json:"args_hash"`
	Exp      int64  `json:"exp"`
	Iat      int64  `json:"iat"`
	// Kind is empty on an approval token and set on a provenance capability.
	// It is read here so a capability cannot be presented where a decision is
	// required — the two are otherwise indistinguishable to a verifier.
	Kind string `json:"kind"`
}

func VerifyHS256(token string, secret string) (Claims, error) {
	return verifyHS256At(token, secret, time.Now())
}

func verifyHS256At(token string, secret string, now time.Time) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("invalid token")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, err
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return Claims{}, err
	}
	if header.Alg != "HS256" {
		return Claims{}, errors.New("invalid token algorithm")
	}
	if header.Typ != approvalTokenType {
		return Claims{}, errors.New("invalid token type")
	}
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return Claims{}, errors.New("invalid signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, err
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, err
	}
	if claims.Iss != approvalTokenIssuer {
		return Claims{}, errors.New("invalid token issuer")
	}
	if claims.Exp <= now.Unix() {
		return Claims{}, errors.New("token expired")
	}
	if claims.Kind != "" {
		return Claims{}, fmt.Errorf("token is a %s capability, not an approval", claims.Kind)
	}
	return claims, nil
}

type Consumer struct {
	OrchestratorGRPCAddr string
	InternalToken        string
	JWTSecret            string
	ApprovalClient       ApprovalClient
	DialOptions          []grpc.DialOption
}

type ApprovalClient interface {
	ConsumeApproval(ctx context.Context, in *turingv1.ConsumeApprovalRequest, opts ...grpc.CallOption) (*turingv1.ApprovalResponse, error)
}

// ArtifactFinalizer is the second half of the same internal channel: the report
// of what a reserved write actually did. It is a separate interface so a client
// that cannot finalize is a refusal at the call site rather than a compile-time
// requirement on every fake.
type ArtifactFinalizer interface {
	FinalizeSandboxArtifact(ctx context.Context, in *turingv1.FinalizeSandboxArtifactRequest, opts ...grpc.CallOption) (*turingv1.FinalizeSandboxArtifactResponse, error)
}

// SessionCapabilityChecker is the read-side half of the same channel: the one
// question a read has to ask storage rather than its own token.
type SessionCapabilityChecker interface {
	CheckSessionCapability(ctx context.Context, in *turingv1.CheckSessionCapabilityRequest, opts ...grpc.CallOption) (*turingv1.SessionCapabilityState, error)
}

func (c Consumer) Validate(token string, tool string, args map[string]any, agentID string) error {
	return c.ValidateContext(context.Background(), token, tool, args, agentID)
}

func (c Consumer) ValidateContext(ctx context.Context, token string, tool string, args map[string]any, agentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	claims, err := VerifyHS256(token, c.JWTSecret)
	if err != nil {
		return err
	}
	if err := checkApprovalBinding(claims, tool, args, agentID); err != nil {
		return err
	}
	return c.consume(ctx, claims.JTI)
}

// checkApprovalBinding compares every claim against the call being made. A
// validly signed approval for another agent, tool or set of arguments is not an
// approval for this one.
func checkApprovalBinding(claims Claims, tool string, args map[string]any, agentID string) error {
	if claims.Aud != "mcp-files" {
		return errors.New("invalid approval audience")
	}
	if claims.Sub != agentID {
		return errors.New("approval subject does not match agent")
	}
	if claims.Tool != tool {
		return errors.New("approval tool does not match call")
	}
	argsHash, err := canonicalArgsHash(args)
	if err != nil {
		return err
	}
	if claims.ArgsHash != argsHash {
		return errors.New("approval args_hash does not match call")
	}
	return nil
}

func (c Consumer) consume(parent context.Context, jti string) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	client, closeClient, err := c.approvalClient()
	if err != nil {
		return err
	}
	if closeClient != nil {
		// The connection is short-lived and the approval result is already
		// determined by the time it closes, so a close error is not actionable.
		defer func() { _ = closeClient() }()
	}
	if c.InternalToken != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.InternalToken)
	}
	resp, err := client.ConsumeApproval(ctx, &turingv1.ConsumeApprovalRequest{ApprovalId: jti})
	if err != nil {
		return mapConsumeError(err)
	}
	if resp.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED {
		return fmt.Errorf("approval consume returned unexpected status: %s", resp.GetStatus())
	}
	return nil
}

func (c Consumer) approvalClient() (ApprovalClient, func() error, error) {
	if c.ApprovalClient != nil {
		return c.ApprovalClient, nil, nil
	}
	if c.OrchestratorGRPCAddr == "" {
		return nil, nil, errors.New("orchestrator gRPC address is required")
	}
	options := c.DialOptions
	if len(options) == 0 {
		options = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}
	// "passthrough:///" preserves DialContext's resolver behaviour: the address
	// (a Docker service name, ORCHESTRATOR_GRPC_ADDR) is handed to the dialer
	// verbatim instead of going through NewClient's default DNS resolver.
	conn, err := grpc.NewClient("passthrough:///"+c.OrchestratorGRPCAddr, options...)
	if err != nil {
		return nil, nil, err
	}
	// NewClient starts the channel IDLE; DialContext connected eagerly. Connect()
	// keeps that, so the approval consume RPC does not also pay for the
	// handshake inside its 10s budget.
	conn.Connect()
	return turingv1.NewApprovalServiceClient(conn), conn.Close, nil
}

func mapConsumeError(err error) error {
	if status.Code(err) == codes.FailedPrecondition {
		return fmt.Errorf("approval consume refused: %w", err)
	}
	return fmt.Errorf("approval consume failed: %w", err)
}

func canonicalArgsHash(args map[string]any) (string, error) {
	canonical, err := canonicalJSON(args)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(args map[string]any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(args); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}
