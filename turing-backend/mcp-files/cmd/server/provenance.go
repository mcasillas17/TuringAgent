package main

import (
	"context"
	"crypto/subtle"
	"net/http"

	"github.com/project-turing/mcp-files/internal/approval"
	"github.com/project-turing/mcp-files/internal/jsonrpc"
	"github.com/project-turing/mcp-files/internal/tools"
)

// handleSessionCleanup serves the one tool that is not a tool: removing a
// withdrawn session's namespace.
//
// It is deliberately its own path rather than a case inside the ordinary
// dispatch. Nothing about a runtime call — not the agent bearer, not an
// approval, not a provenance capability — can reach it; the only thing that
// does is the internal token the orchestrator and this server share, which no
// agent holds. It never touches the provenance guard, so there is no capability
// shape that could stand in for the token.
func handleSessionCleanup(
	w http.ResponseWriter,
	r *http.Request,
	req jsonrpc.Request,
	call tools.CallRequest,
	filesTools tools.FilesTools,
	internalToken string,
) {
	if !authorizedForInternalCleanup(call.InternalCleanupToken, internalToken) {
		// Answered exactly like an unknown tool. A caller without the token
		// learns nothing about whether this server can clean up sessions.
		writeJSONRPCForRequest(w, req, jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   map[string]any{"code": -32602, "message": "unknown tool"},
		})
		return
	}
	if call.ApprovalToken != "" || call.ProvenanceToken != "" {
		// Cleanup is not an agent action, so a call that also carries agent
		// credentials is a confused caller at best.
		writeJSONRPCForRequest(w, req, jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   map[string]any{"code": -32602, "message": "session cleanup does not accept agent tokens"},
		})
		return
	}
	result, err := filesTools.SessionCleanupContext(r.Context(), call.Args)
	if err != nil {
		code := -32000
		if tools.IsInvalidParams(err) {
			code = -32602
		}
		writeJSONRPCForRequest(w, req, jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   map[string]any{"code": code, "message": err.Error()},
		})
		return
	}
	writeJSONRPCForRequest(w, req, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: result})
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
