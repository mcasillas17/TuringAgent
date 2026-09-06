package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"github.com/project-turing/mcp-files/internal/approval"
	"github.com/project-turing/mcp-files/internal/tools"
)

func handleApprovalPreview(w http.ResponseWriter, r *http.Request, f tools.FilesTools, c approval.Consumer) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, approvalpreview.MaxArgsBytes+16*1024)
	var req approvalpreview.Request
	decoder := json.NewDecoder(r.Body)
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if decoder.Decode(&req) != nil || c.VerifyPreview(token, req) != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	p, err := f.PreparePreview(ctx, tools.CallRequest{Name: req.Tool, Args: req.Args, AgentID: req.AgentID, ProvenanceToken: req.ProvenanceToken})
	if err != nil {
		http.Error(w, "preview unavailable", http.StatusConflict)
		return
	}
	// Even an early non-ready response must not disclose arguments after a
	// concurrent withdrawal.
	if err := c.CheckSession(ctx, req.ProvenanceToken); err != nil {
		http.Error(w, "preview unavailable", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(p)
}
