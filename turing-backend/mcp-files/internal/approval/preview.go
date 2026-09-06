package approval

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
)

// VerifyPreview checks a domain-separated read-only capability. Its signature
// is insufficient alone: fresh provenance and session liveness are also needed.
func (c Consumer) VerifyPreview(token string, req approvalpreview.Request) error {
	if c.JWTSecret == "" {
		return errors.New("preview signing is not configured")
	}
	payload, err := parseSignedPayload(token, c.JWTSecret)
	if err != nil {
		return err
	}
	var claims struct {
		Iss            string `json:"iss"`
		Aud            string `json:"aud"`
		Kind           string `json:"kind"`
		Sub            string `json:"sub"`
		Tool           string `json:"tool"`
		ArgsHash       string `json:"args_hash"`
		ProvenanceHash string `json:"provenance_hash"`
		Exp            int64  `json:"exp"`
	}

	if err := json.Unmarshal(payload, &claims); err != nil {
		return err
	}
	hash, err := canonicalArgsHash(req.Args)
	if err != nil {
		return err
	}
	if claims.Iss != approvalTokenIssuer || claims.Aud != "mcp-files" || claims.Kind != approvalpreview.TokenKind ||
		claims.Exp <= time.Now().Unix() || claims.Sub != req.AgentID || claims.Tool != req.Tool ||
		claims.ArgsHash != hash || claims.ProvenanceHash != approvalpreview.Hash(req.ProvenanceToken) {
		return errors.New("preview capability rejected")
	}
	_, err = c.VerifyProvenance(req.ProvenanceToken, req.Tool, req.Args, req.AgentID)
	return err
}

// VerifyWrite reads the existing approval's mutation preconditions without
// consuming it. The same binding is checked again on the consume path.
func (c Consumer) VerifyWrite(req WriteAuthorization) (approvalpreview.Binding, error) {
	claims, err := c.verifyWriteClaims(req)
	return claims.Binding, err
}

// verifyWriteClaims performs one boundary's complete verification and retains
// the verified JTI for consumption without repeating signature/argument checks.
func (c Consumer) verifyWriteClaims(req WriteAuthorization) (Claims, error) {
	claims, err := VerifyHS256(req.ApprovalToken, c.JWTSecret)
	if err != nil {
		return Claims{}, err
	}
	if err := checkApprovalBinding(claims, req.Tool, req.Args, req.AgentID); err != nil {
		return Claims{}, err
	}
	prov, err := c.VerifyProvenance(req.ProvenanceToken, req.Tool, req.Args, req.AgentID)
	if err != nil {
		return Claims{}, err
	}
	b := claims.Binding
	content, ok := req.Args["content"].(string)
	if b.PreviewHash == "" || b.ToolCallID == "" || b.SessionID != prov.SessionID || b.RunID != prov.RunID || b.Generation != prov.DeletionGeneration ||
		b.PhysicalPath == "" || b.PhysicalPath != req.PhysicalPath || !ok || b.AfterHash != approvalpreview.Hash(content) ||
		(req.Tool == "files.create" && (b.BeforeExists || b.BeforeHash != "")) ||
		(req.Tool == "files.update" && (!b.BeforeExists || b.BeforeHash == "")) {
		return Claims{}, errors.New("approval preview binding rejected")
	}
	return claims, nil
}
