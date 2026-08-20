package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/project-turing/mcp-files/internal/approval"
	"github.com/project-turing/mcp-files/internal/tools"
)

func handleInternalSessionCleanup(w http.ResponseWriter, r *http.Request, filesTools tools.FilesTools, approvalConsumerToken string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	const bearer = "Bearer "
	presented := strings.TrimPrefix(r.Header.Get("authorization"), bearer)
	if !strings.HasPrefix(r.Header.Get("authorization"), bearer) ||
		!authorizedForInternalCleanup(presented, approvalConsumerToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var args map[string]any
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, "invalid cleanup request", http.StatusBadRequest)
		return
	}
	result, err := filesTools.SessionCleanupContext(r.Context(), args)
	if err != nil {
		status := http.StatusInternalServerError
		if tools.IsInvalidParams(err) {
			status = http.StatusBadRequest
		}
		http.Error(w, "session cleanup failed", status)
		return
	}
	w.Header().Set("content-type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, "encode cleanup response", http.StatusInternalServerError)
	}
}

// authorizedForInternalCleanup fails closed on an unset token: a deploy that
// forgot to configure one must not expose a delete to anyone who can reach the
// endpoint. The comparison is time independent of the token's content, matching
// how the MCP bearer is checked.
func authorizedForInternalCleanup(presented string, configured string) bool {
	if configured == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(configured)) == 1
}

// provenanceGuard adapts the orchestrator client to what the file tools need,
// so the file-system layer stays unaware of JWTs and gRPC and the approval
// layer stays unaware of paths.
type provenanceGuard struct {
	consumer approval.Consumer
}

func (g provenanceGuard) Verify(token string, tool string, args map[string]any, agentID string) (tools.Provenance, error) {
	claims, err := g.consumer.VerifyProvenance(token, tool, args, agentID)
	if err != nil {
		return tools.Provenance{}, err
	}
	return tools.Provenance{
		SessionID:          claims.SessionID,
		RunID:              claims.RunID,
		LogicalPath:        claims.LogicalPath,
		DeletionGeneration: claims.DeletionGeneration,
	}, nil
}

func (g provenanceGuard) AuthorizeWrite(ctx context.Context, req tools.WriteAuthorization) (tools.Reservation, error) {
	reservation, err := g.consumer.AuthorizeWrite(ctx, approval.WriteAuthorization{
		ApprovalToken:   req.ApprovalToken,
		ProvenanceToken: req.ProvenanceToken,
		Tool:            req.Tool,
		Args:            req.Args,
		AgentID:         req.AgentID,
		PhysicalPath:    req.PhysicalPath,
	})
	if err != nil {
		return tools.Reservation{}, err
	}
	return tools.Reservation{
		ArtifactID:   reservation.ArtifactID,
		PhysicalPath: reservation.PhysicalPath,
		Policy:       reservation.Policy,
	}, nil
}

func (g provenanceGuard) FinalizeWrite(ctx context.Context, artifactID string, provenanceToken string, committed bool) error {
	return g.consumer.FinalizeWrite(ctx, artifactID, provenanceToken, committed)
}

func (g provenanceGuard) CheckSession(ctx context.Context, provenanceToken string) error {
	return g.consumer.CheckSession(ctx, provenanceToken)
}
