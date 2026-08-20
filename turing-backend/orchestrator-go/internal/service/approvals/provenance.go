package approvals

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path"
	"strings"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// provenanceTokenKind separates a provenance capability from an approval token.
// Both are HS256 tokens signed with the same secret for the same audience, so
// without a kind claim one could be presented where the other is expected —
// an approval token would then authorise an unbounded write, and a provenance
// capability would stand in for a decision nobody made.
const provenanceTokenKind = "provenance"

// defaultProvenanceTTL has to outlive the whole tool call, including the wait
// for a person to answer an approval (65s by default) and the write itself
// (mcp-files allows two minutes), while staying short enough that a capability
// is not worth stealing minutes later.
const defaultProvenanceTTL = 5 * time.Minute

// ProvenanceRequest is what the runtime knows about a file tool call. Nothing
// here is taken from the model or the worker beyond the tool identity and
// arguments the orchestrator already recorded; the session, the deletion
// generation and the expiry are the server's own.
type ProvenanceRequest struct {
	SessionID   string
	RunID       string
	AgentID     string
	ToolName    string
	ArgsHash    string
	LogicalPath string
}

// ProvenanceClaims is the verified content of a capability.
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

// IssueToolProvenance mints the capability a file tool call carries to
// mcp-files.
//
// The deletion generation is read here rather than accepted from the caller,
// which is what makes the capability answer "was this session still itself when
// the call was authorised?". A capability minted before a withdrawal cannot be
// replayed after one, because the generation it carries no longer matches.
func (s *Server) IssueToolProvenance(ctx context.Context, req ProvenanceRequest) (string, error) {
	if req.SessionID == "" || req.RunID == "" || req.AgentID == "" || req.ToolName == "" {
		return "", status.Error(codes.InvalidArgument, "provenance requires a session, run, agent and tool")
	}
	if s.jwtSecret == "" {
		return "", status.Error(codes.FailedPrecondition, "provenance signing is not configured")
	}
	state, err := s.repo.SessionWithdrawalState(ctx, req.SessionID)
	if errors.Is(err, repository.ErrSessionNotFound) {
		return "", status.Error(codes.NotFound, "session not found")
	}
	if err != nil {
		return "", status.Error(codes.Internal, "read session withdrawal state failed")
	}
	// A withdrawn session gets no new capabilities at all. Issuing one and
	// refusing it later would mean a tool call that looks authorised right up
	// to the moment it touches the file system.
	if !state.Active {
		return "", status.Error(codes.FailedPrecondition, "session deletion is in progress")
	}
	generation := state.DeletionGeneration
	now := time.Now()
	ttl := max(defaultProvenanceTTL, s.approvalTTL)
	payload := provenancePayload{
		Iss:      "turing.orchestrator",
		Sub:      req.AgentID,
		Aud:      "mcp-files",
		JTI:      ids.New("prov"),
		Kind:     provenanceTokenKind,
		SID:      req.SessionID,
		RID:      req.RunID,
		Gen:      generation,
		Tool:     req.ToolName,
		ArgsHash: req.ArgsHash,
		Path:     normalizeProvenancePath(req.LogicalPath),
		Iat:      now.Unix(),
		Exp:      now.Add(ttl).Unix(),
	}
	return signHS256(payload, s.jwtSecret)
}

func signHS256(payload any, secret string) (string, error) {
	headerJSON, err := json.Marshal(map[string]any{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Server) verifyProvenanceToken(token string) (ProvenanceClaims, error) {
	if s.jwtSecret == "" {
		return ProvenanceClaims{}, errors.New("provenance signing is not configured")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ProvenanceClaims{}, errors.New("invalid provenance token")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ProvenanceClaims{}, err
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return ProvenanceClaims{}, err
	}
	if header.Alg != "HS256" || header.Typ != "JWT" {
		return ProvenanceClaims{}, errors.New("invalid provenance token header")
	}
	mac := hmac.New(sha256.New, []byte(s.jwtSecret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return ProvenanceClaims{}, errors.New("invalid provenance signature")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ProvenanceClaims{}, err
	}
	var payload provenancePayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return ProvenanceClaims{}, err
	}
	if payload.Iss != "turing.orchestrator" || payload.Aud != "mcp-files" {
		return ProvenanceClaims{}, errors.New("invalid provenance token issuer or audience")
	}
	if payload.Kind != provenanceTokenKind {
		return ProvenanceClaims{}, errors.New("token is not a provenance capability")
	}
	if payload.Exp <= time.Now().Unix() {
		return ProvenanceClaims{}, errors.New("provenance capability expired")
	}
	return ProvenanceClaims{
		CapabilityID:       payload.JTI,
		SessionID:          payload.SID,
		RunID:              payload.RID,
		AgentID:            payload.Sub,
		Tool:               payload.Tool,
		ArgsHash:           payload.ArgsHash,
		LogicalPath:        payload.Path,
		DeletionGeneration: payload.Gen,
		ExpiresAt:          payload.Exp,
	}, nil
}

// reserveArtifactForConsume turns a verified capability into a durable manifest
// row, and is the reason a consume can refuse: the write has not happened yet,
// so this is the last point where "this session is being withdrawn" or "that
// path is not yours" can still be a refusal rather than an orphaned file.
func (s *Server) reserveArtifactForConsume(ctx context.Context, approval repository.ApprovalRecord, token string, physicalPath string) (repository.SandboxArtifact, bool, error) {
	if token == "" {
		return repository.SandboxArtifact{}, false, status.Error(codes.FailedPrecondition, "provenance capability is required")
	}
	claims, err := s.verifyProvenanceToken(token)
	if err != nil {
		return repository.SandboxArtifact{}, false, status.Error(codes.FailedPrecondition, "provenance capability is not valid: "+err.Error())
	}
	if claims.RunID != approval.RunID || claims.AgentID != approval.AgentID ||
		claims.Tool != approval.ToolName || claims.ArgsHash != approval.ArgsHash {
		return repository.SandboxArtifact{}, false, status.Error(codes.FailedPrecondition, "provenance capability does not match the approval")
	}
	if physicalPath == "" {
		return repository.SandboxArtifact{}, false, status.Error(codes.FailedPrecondition, "physical path is required")
	}
	artifact, created, err := s.repo.ReserveSandboxArtifact(ctx, repository.ReserveSandboxArtifactInput{
		SessionID:          claims.SessionID,
		RunID:              claims.RunID,
		LogicalPath:        claims.LogicalPath,
		PhysicalPath:       physicalPath,
		DeletionGeneration: claims.DeletionGeneration,
	})
	if err != nil {
		return repository.SandboxArtifact{}, false, mapSandboxArtifactError(err)
	}
	return artifact, created, nil
}

// FinalizeSandboxArtifact records what actually happened to a reserved write.
//
// It runs over the same authenticated internal channel as ConsumeApproval, so
// mcp-files reports the outcome without exposing a new listener, and it accepts
// only the capability the reservation was taken under.
func (s *Server) FinalizeSandboxArtifact(ctx context.Context, req *turingv1.FinalizeSandboxArtifactRequest) (*turingv1.FinalizeSandboxArtifactResponse, error) {
	if req == nil || req.GetArtifactId() == "" {
		return nil, status.Error(codes.InvalidArgument, "artifact_id is required")
	}
	if req.GetProvenanceToken() == "" {
		return nil, status.Error(codes.FailedPrecondition, "provenance capability is required")
	}
	claims, err := s.verifyProvenanceToken(req.GetProvenanceToken())
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "provenance capability is not valid: "+err.Error())
	}
	if !req.GetCommitted() {
		released, err := s.repo.ReleaseSandboxArtifactReservation(ctx, req.GetArtifactId(), claims.SessionID, claims.RunID)
		if err != nil {
			return nil, status.Error(codes.Internal, "release sandbox artifact reservation failed")
		}
		if !released {
			// Either the capability does not own this reservation or the write
			// was already recorded. Both are refusals, never a silent success,
			// because a caller that believes it released a finalized artifact
			// would stop tracking a file that is still on disk.
			return nil, status.Error(codes.FailedPrecondition, "sandbox artifact reservation could not be released")
		}
		return &turingv1.FinalizeSandboxArtifactResponse{ArtifactId: req.GetArtifactId(), State: "released"}, nil
	}
	// Recorded FIRST, and only then judged. The bytes are already on disk by
	// the time this runs, so the manifest has to admit the file exists whatever
	// the answer turns out to be — a withdrawal that started mid-write needs
	// this row to find the file, and refusing to write it would hide the file
	// from the very cleanup that has to remove it.
	artifact, err := s.repo.FinalizeSandboxArtifact(ctx, repository.FinalizeSandboxArtifactInput{
		ArtifactID:  req.GetArtifactId(),
		SessionID:   claims.SessionID,
		RunID:       claims.RunID,
		LogicalPath: normalizeProvenancePath(claims.LogicalPath),
	})
	if err != nil {
		return nil, mapSandboxArtifactError(err)
	}
	if err := s.requireCapabilityStillActive(ctx, claims); err != nil {
		// The artifact stays ready; the caller learns its write raced a
		// withdrawal and must not be reported as a success.
		return nil, err
	}
	return &turingv1.FinalizeSandboxArtifactResponse{ArtifactId: artifact.ArtifactID, State: artifact.State}, nil
}

// CheckSessionCapability answers, server-side, whether a capability's session is
// still accepting work.
//
// Reads need this because nothing about a read touches the artifact manifest:
// without it, the only thing standing between a withdrawn session and its
// contents would be a token minted before the withdrawal, which is exactly the
// thing that cannot be trusted to know the session's current state.
func (s *Server) CheckSessionCapability(ctx context.Context, req *turingv1.CheckSessionCapabilityRequest) (*turingv1.SessionCapabilityState, error) {
	if req == nil || req.GetProvenanceToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "provenance_token is required")
	}
	claims, err := s.verifyProvenanceToken(req.GetProvenanceToken())
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "provenance capability is not valid: "+err.Error())
	}
	state, err := s.repo.SessionWithdrawalState(ctx, claims.SessionID)
	if errors.Is(err, repository.ErrSessionNotFound) {
		// A session that is gone is not active, and saying so is more useful
		// than an error the caller would have to interpret the same way.
		return &turingv1.SessionCapabilityState{Active: false}, nil
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "read session withdrawal state failed")
	}
	return &turingv1.SessionCapabilityState{
		Active:             state.Active && state.DeletionGeneration == claims.DeletionGeneration,
		DeletionGeneration: state.DeletionGeneration,
	}, nil
}

// requireCapabilityStillActive is the real post-write check: it asks storage,
// not the token, whether the session survived the write.
func (s *Server) requireCapabilityStillActive(ctx context.Context, claims ProvenanceClaims) error {
	state, err := s.repo.SessionWithdrawalState(ctx, claims.SessionID)
	if errors.Is(err, repository.ErrSessionNotFound) {
		return status.Error(codes.FailedPrecondition, "session deletion is in progress")
	}
	if err != nil {
		return status.Error(codes.Internal, "read session withdrawal state failed")
	}
	if !state.Active {
		return status.Error(codes.FailedPrecondition, "session deletion is in progress")
	}
	if state.DeletionGeneration != claims.DeletionGeneration {
		return status.Error(codes.FailedPrecondition, "session deletion generation changed during the operation")
	}
	return nil
}

func normalizeProvenancePath(logicalPath string) string {
	if logicalPath == "" {
		return ""
	}
	return path.Clean("/" + logicalPath)[1:]
}

func mapSandboxArtifactError(err error) error {
	switch {
	case errors.Is(err, repository.ErrSessionDeleting):
		return status.Error(codes.FailedPrecondition, "session deletion is in progress")
	case errors.Is(err, repository.ErrSessionNotFound):
		return status.Error(codes.NotFound, "session not found")
	case errors.Is(err, repository.ErrSandboxArtifactNotFound):
		return status.Error(codes.NotFound, "sandbox artifact not found")
	case errors.Is(err, repository.ErrSandboxArtifactUnowned),
		errors.Is(err, repository.ErrSandboxArtifactPathScope),
		errors.Is(err, repository.ErrSandboxArtifactGenerationStale),
		errors.Is(err, repository.ErrSandboxArtifactRetained):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, fmt.Sprintf("sandbox artifact reservation failed: %v", err))
	}
}

// releaseReservationAfterFailedConsume undoes a reservation whose approval
// turned out not to be spendable. The write cannot happen, so leaving the row
// would report a file that will never exist and would block the session's
// withdrawal on cleanup that has nothing to clean.
//
// It only withdraws a row THIS call created. A replayed consume finds the
// reservation an earlier, successful consume took, and dropping that one would
// erase the manifest entry for a file already on disk.
func (s *Server) releaseReservationAfterFailedConsume(ctx context.Context, artifact repository.SandboxArtifact, reservedHere bool) {
	if artifact.ArtifactID == "" || !reservedHere {
		return
	}
	if _, err := s.repo.ReleaseSandboxArtifactReservation(ctx, artifact.ArtifactID, artifact.SessionID, artifact.RunID); err != nil {
		// Logged rather than returned: the caller is already failing for a
		// better reason, and the leftover row is the safe direction — it keeps
		// the withdrawal retryable instead of silently completing.
		log.Printf("release sandbox artifact reservation %s after failed consume: %v", artifact.ArtifactID, err)
	}
}
