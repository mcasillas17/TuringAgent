package approval

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/metadata"
)

// ProvenanceTokenKind is the claim that keeps a provenance capability from
// standing in for an approval, and an approval token from standing in for a
// capability. Both are HS256 tokens for the same audience signed with the same
// secret, so the kind is the only thing separating them.
const ProvenanceTokenKind = "provenance"

const provenanceCallTimeout = 10 * time.Second

// ProvenanceClaims is the verified scope of one file tool call: which session
// and run it belongs to, which withdrawal generation it was authorised in, and
// exactly which tool, arguments and path it may act on.
type ProvenanceClaims struct {
	CapabilityID       string
	SessionID          string
	RunID              string
	AgentID            string
	Tool               string
	ArgsHash           string
	LogicalPath        string
	DeletionGeneration int64
	ExpiresAt          int64
}

type provenancePayload struct {
	Iss      string `json:"iss"`
	Sub      string `json:"sub"`
	Aud      string `json:"aud"`
	JTI      string `json:"jti"`
	Kind     string `json:"kind"`
	SID      string `json:"sid"`
	RID      string `json:"rid"`
	Gen      int64  `json:"gen"`
	Tool     string `json:"tool"`
	ArgsHash string `json:"args_hash"`
	Path     string `json:"path"`
	Iat      int64  `json:"iat"`
	Exp      int64  `json:"exp"`
}

// WriteAuthorization is everything the orchestrator needs to decide whether one
// write may happen: the human decision, the capability, and the location the
// bytes are about to land in.
type WriteAuthorization struct {
	ApprovalToken   string
	ProvenanceToken string
	Tool            string
	Args            map[string]any
	AgentID         string
	PhysicalPath    string
}

// Reservation is the orchestrator's durable record that the write is accounted
// for before it happens.
type Reservation struct {
	ArtifactID         string
	PhysicalPath       string
	Policy             string
	DeletionGeneration int64
}

// VerifyProvenance checks a capability against the call being made. Every claim
// is compared, not just the signature: a validly signed capability for another
// tool, another agent or other arguments is not a capability for this call.
func (c Consumer) VerifyProvenance(token string, tool string, args map[string]any, agentID string) (ProvenanceClaims, error) {
	return c.verifyProvenanceAt(token, tool, args, agentID, time.Now())
}

func (c Consumer) verifyProvenanceAt(token string, tool string, args map[string]any, agentID string, now time.Time) (ProvenanceClaims, error) {
	if token == "" {
		return ProvenanceClaims{}, errors.New("provenance capability required")
	}
	payload, err := parseSignedPayload(token, c.JWTSecret)
	if err != nil {
		return ProvenanceClaims{}, err
	}
	var claims provenancePayload
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ProvenanceClaims{}, err
	}
	if claims.Iss != approvalTokenIssuer {
		return ProvenanceClaims{}, errors.New("invalid provenance issuer")
	}
	if claims.Aud != "mcp-files" {
		return ProvenanceClaims{}, errors.New("invalid provenance audience")
	}
	if claims.Kind != ProvenanceTokenKind {
		return ProvenanceClaims{}, errors.New("token is not a provenance capability")
	}
	if claims.Exp <= now.Unix() {
		return ProvenanceClaims{}, errors.New("provenance capability expired")
	}
	if claims.SID == "" || claims.RID == "" {
		return ProvenanceClaims{}, errors.New("provenance capability has no session or run scope")
	}
	if claims.Sub != agentID {
		return ProvenanceClaims{}, errors.New("provenance subject does not match agent")
	}
	if claims.Tool != tool {
		return ProvenanceClaims{}, errors.New("provenance tool does not match call")
	}
	argsHash, err := canonicalArgsHash(args)
	if err != nil {
		return ProvenanceClaims{}, err
	}
	if claims.ArgsHash != argsHash {
		return ProvenanceClaims{}, errors.New("provenance args_hash does not match call")
	}
	return ProvenanceClaims{
		CapabilityID:       claims.JTI,
		SessionID:          claims.SID,
		RunID:              claims.RID,
		AgentID:            claims.Sub,
		Tool:               claims.Tool,
		ArgsHash:           claims.ArgsHash,
		LogicalPath:        claims.Path,
		DeletionGeneration: claims.Gen,
		ExpiresAt:          claims.Exp,
	}, nil
}

// AuthorizeWrite spends the approval and takes the artifact reservation in one
// round trip, because they are one decision: the orchestrator will not hand
// back a reservation for a write it is not also willing to approve, and it will
// not spend an approval it cannot record an artifact for.
//
// The returned reservation is checked against the path this process is about to
// write. Both sides derive that path independently, so a disagreement means one
// of them is wrong about what is being recorded, and the write does not happen.
func (c Consumer) AuthorizeWrite(ctx context.Context, req WriteAuthorization) (Reservation, error) {
	if err := ctx.Err(); err != nil {
		return Reservation{}, err
	}
	if req.ApprovalToken == "" {
		return Reservation{}, errors.New("approval token required")
	}
	if req.PhysicalPath == "" {
		return Reservation{}, errors.New("physical path required")
	}
	claims, err := VerifyHS256(req.ApprovalToken, c.JWTSecret)
	if err != nil {
		return Reservation{}, err
	}
	if err := checkApprovalBinding(claims, req.Tool, req.Args, req.AgentID); err != nil {
		return Reservation{}, err
	}
	if _, err := c.VerifyProvenance(req.ProvenanceToken, req.Tool, req.Args, req.AgentID); err != nil {
		return Reservation{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, provenanceCallTimeout)
	defer cancel()
	client, closeClient, err := c.approvalClient()
	if err != nil {
		return Reservation{}, err
	}
	if closeClient != nil {
		defer func() { _ = closeClient() }()
	}
	callCtx = c.withInternalCredentials(callCtx)
	response, err := client.ConsumeApproval(callCtx, &turingv1.ConsumeApprovalRequest{
		ApprovalId:      claims.JTI,
		ProvenanceToken: req.ProvenanceToken,
		PhysicalPath:    req.PhysicalPath,
	})
	if err != nil {
		return Reservation{}, mapConsumeError(err)
	}
	if response.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED {
		return Reservation{}, fmt.Errorf("approval consume returned unexpected status: %s", response.GetStatus())
	}
	reservation := response.GetReservation()
	if reservation.GetArtifactId() == "" {
		return Reservation{}, errors.New("approval consume returned no artifact reservation")
	}
	if reservation.GetPhysicalPath() != req.PhysicalPath {
		return Reservation{}, fmt.Errorf("artifact reservation is for %q, not the path being written", reservation.GetPhysicalPath())
	}
	return Reservation{
		ArtifactID:         reservation.GetArtifactId(),
		PhysicalPath:       reservation.GetPhysicalPath(),
		Policy:             reservation.GetPolicy(),
		DeletionGeneration: reservation.GetDeletionGeneration(),
	}, nil
}

// FinalizeWrite reports the outcome of a reserved write over the same
// authenticated internal channel the approval was consumed on. committed=false
// withdraws a reservation whose bytes never landed; it is never used after a
// successful write.
func (c Consumer) FinalizeWrite(ctx context.Context, artifactID string, provenanceToken string, committed bool) error {
	if artifactID == "" {
		return errors.New("artifact id required")
	}
	// Detached from the caller's deadline on purpose: the bytes are already on
	// disk by the time this runs, and a cancelled request must not be the
	// reason the manifest never learns about a file that exists.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provenanceCallTimeout)
	defer cancel()
	client, closeClient, err := c.approvalClient()
	if err != nil {
		return err
	}
	if closeClient != nil {
		defer func() { _ = closeClient() }()
	}
	finalizer, ok := client.(ArtifactFinalizer)
	if !ok {
		return errors.New("orchestrator client cannot finalize sandbox artifacts")
	}
	callCtx = c.withInternalCredentials(callCtx)
	if _, err := finalizer.FinalizeSandboxArtifact(callCtx, &turingv1.FinalizeSandboxArtifactRequest{
		ArtifactId:      artifactID,
		ProvenanceToken: provenanceToken,
		Committed:       committed,
	}); err != nil {
		return fmt.Errorf("finalize sandbox artifact %s: %w", artifactID, err)
	}
	return nil
}

// CheckSession asks the orchestrator whether the capability's session is still
// accepting work.
//
// It exists because a capability cannot answer that question about itself: it
// was signed before the withdrawal it might be racing. Reads call it on both
// sides of the I/O, so a session withdrawn mid-read does not hand back its
// contents. Any failure to get an answer is a refusal — an orchestrator that
// cannot be reached is not permission to read a session that may already be
// gone.
func (c Consumer) CheckSession(ctx context.Context, provenanceToken string) error {
	if provenanceToken == "" {
		return errors.New("provenance capability required")
	}
	callCtx, cancel := context.WithTimeout(ctx, provenanceCallTimeout)
	defer cancel()
	client, closeClient, err := c.approvalClient()
	if err != nil {
		return err
	}
	if closeClient != nil {
		defer func() { _ = closeClient() }()
	}
	checker, ok := client.(SessionCapabilityChecker)
	if !ok {
		return errors.New("orchestrator client cannot check session capabilities")
	}
	state, err := checker.CheckSessionCapability(c.withInternalCredentials(callCtx), &turingv1.CheckSessionCapabilityRequest{
		ProvenanceToken: provenanceToken,
	})
	if err != nil {
		return fmt.Errorf("check session capability: %w", err)
	}
	if !state.GetActive() {
		return errors.New("session deletion is in progress")
	}
	return nil
}

func (c Consumer) withInternalCredentials(ctx context.Context) context.Context {
	if c.ApprovalConsumerToken == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.ApprovalConsumerToken)
}

func parseSignedPayload(token string, secret string) ([]byte, error) {
	if secret == "" {
		return nil, errors.New("token verification is not configured")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, err
	}
	if header.Alg != "HS256" {
		return nil, errors.New("invalid token algorithm")
	}
	if header.Typ != approvalTokenType {
		return nil, errors.New("invalid token type")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, errors.New("invalid signature")
	}
	return base64.RawURLEncoding.DecodeString(parts[1])
}
