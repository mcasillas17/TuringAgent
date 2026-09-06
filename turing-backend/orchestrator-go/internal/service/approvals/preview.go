package approvals

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) SetPreviewEndpoint(baseURL string) { s.previewBaseURL = baseURL }

func (s *PublicServer) GetApprovalDetails(ctx context.Context, req *turingv1.GetApprovalDetailsRequest) (*turingv1.ApprovalDetails, error) {
	return s.service.GetApprovalDetails(ctx, req)
}
func (*InternalServer) GetApprovalDetails(context.Context, *turingv1.GetApprovalDetailsRequest) (*turingv1.ApprovalDetails, error) {
	return nil, status.Error(codes.PermissionDenied, "human approval details are public")
}

func fileMutation(a repository.ApprovalRecord) bool {
	return a.ServerName == "files" && (a.ToolName == "files.create" || a.ToolName == "files.update")
}

func (s *Server) GetApprovalDetails(ctx context.Context, req *turingv1.GetApprovalDetailsRequest) (details *turingv1.ApprovalDetails, err error) {
	defer func() {
		if err == nil {
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			err = status.FromContextError(err).Err()
		} else if status.Code(err) == codes.Unknown {
			err = status.Error(codes.Internal, "approval details unavailable")
		}
	}()
	if req == nil || req.ApprovalId == "" {
		return nil, status.Error(codes.InvalidArgument, "approval_id is required")
	}
	a, err := s.repo.GetApproval(ctx, req.ApprovalId)
	if err != nil {
		return nil, mapApprovalError(err)
	}
	scope, err := s.repo.GetApprovalPreviewContext(ctx, a.ApprovalID)
	if err != nil {
		return nil, mapApprovalError(err)
	}
	d := &turingv1.ApprovalDetails{ApprovalId: a.ApprovalID, SessionId: scope.SessionID, RunId: a.RunID, ToolCallId: a.ToolCallID,
		ToolName: a.ToolName, ServerName: a.ServerName, ExpiresAt: a.ExpiresAt, Status: mapApprovalStatus(a.Status)}
	if req.RefreshPreview && a.Status != "pending" {
		return nil, status.Error(codes.FailedPrecondition, "only pending previews can be refreshed")
	}
	if expired(a.ExpiresAt) {
		d.PreviewState = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_EXPIRED
		return d, nil
	}
	if (a.Status != "pending" && a.Status != "approved") || (scope.Lifecycle != "running" && scope.Lifecycle != "waiting_approval") {
		d.PreviewState = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_TERMINAL
		return d, nil
	}
	d.CanDeny = a.Status == "pending"
	if scope.ToolName != a.ToolName || scope.ArgsHash != a.ArgsHash || scope.AgentID != a.AgentID {
		d.PreviewState = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
		return d, nil
	}
	p, err := s.repo.GetApprovalPreview(ctx, a.ApprovalID)
	var preparedHash, preparedJSON string
	if (errors.Is(err, sql.ErrNoRows) || req.RefreshPreview) && a.Status == "pending" {
		snapshot, prepErr := s.preparePreview(ctx, a, scope)
		if prepErr != nil {
			snapshot = approvalpreview.Snapshot{State: turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE, ArgumentsJSON: "{}"}
		}
		raw, marshalErr := json.Marshal(snapshot)
		if marshalErr != nil {
			return nil, marshalErr
		}
		p = repository.ApprovalPreviewRecord{ApprovalID: a.ApprovalID, SnapshotJSON: string(raw), Generation: scope.Generation}
		p.Hash = s.previewIdentity(a, scope, p.SnapshotJSON)
		preparedHash, preparedJSON = p.Hash, p.SnapshotJSON
		if err := s.repo.StoreApprovalPreview(ctx, p, req.RefreshPreview); err != nil {
			return nil, status.Error(codes.FailedPrecondition, "approval changed while preparing preview")
		}
		p, err = s.repo.GetApprovalPreview(ctx, a.ApprovalID)
	}
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, mapApprovalError(err)
		}
		d.PreviewState = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
		return d, nil
	}
	var snap approvalpreview.Snapshot
	if json.Unmarshal([]byte(p.SnapshotJSON), &snap) != nil {
		return nil, status.Error(codes.FailedPrecondition, "stored preview unavailable")
	}
	d.PreviewHash = p.Hash
	d.ArgumentsJson = snap.ArgumentsJSON
	d.FilePreview = snap.File
	d.PreviewState = snap.State
	// A concurrent first reader or refresh can win storage while this request
	// is preparing. Only skip the second read when we actually retained this
	// request's freshly prepared identity AND snapshot. Existing reads and
	// decisions always perform a fresh filesystem check.
	if fileMutation(a) && snap.State == turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY &&
		(preparedHash != p.Hash || preparedJSON != p.SnapshotJSON) {
		current, err := s.preparePreview(ctx, a, scope)
		if err != nil {
			d.PreviewState = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
		} else {
			switch current.State {
			case turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY:
				if current.File == nil {
					d.PreviewState = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
				} else if !sameFilePreview(snap.File, current.File) {
					d.PreviewState = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE
				}
			case turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE,
				turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_BINARY,
				turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED,
				turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED:
				// A readable, bounded, safe reviewed file no longer satisfies
				// that precondition. Unlike an I/O error, this is observed.
				d.PreviewState = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE
			default:
				d.PreviewState = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
			}
		}
	}
	// After private I/O, recheck both withdrawal and decision state before any
	// sanitized snapshot can leave the service.
	latest, err := s.repo.GetApprovalPreviewContext(ctx, a.ApprovalID)
	if err != nil || latest.Generation != scope.Generation {
		return nil, status.Error(codes.NotFound, "approval unavailable")
	}
	current, err := s.repo.GetApproval(ctx, a.ApprovalID)
	if err != nil {
		return nil, mapApprovalError(err)
	}
	d.Status = mapApprovalStatus(current.Status)
	if expired(current.ExpiresAt) {
		d.PreviewState = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_EXPIRED
	}
	if (current.Status != "pending" && current.Status != "approved") || (latest.Lifecycle != "running" && latest.Lifecycle != "waiting_approval") {
		d.PreviewState = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_TERMINAL
	}
	// Only safely disclosed, reviewable arguments get an unkeyed fingerprint.
	// Binary/size/error states can hide credentials too; the keyed preview
	// identity remains available without creating a dictionary oracle.
	if d.PreviewState == turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY ||
		d.PreviewState == turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNSUPPORTED {
		d.ArgsHash = a.ArgsHash
	}
	d.CanDeny = current.Status == "pending" && !expired(current.ExpiresAt) && (latest.Lifecycle == "running" || latest.Lifecycle == "waiting_approval")
	d.CanApprove = d.CanDeny && (d.PreviewState == turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY ||
		(!fileMutation(a) && d.PreviewState == turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNSUPPORTED))
	return d, nil
}

func sameFilePreview(a, b *turingv1.FileMutationPreview) bool {
	return a != nil && b != nil && a.PhysicalPath == b.PhysicalPath && a.BeforeExists == b.BeforeExists &&
		a.BeforeHash == b.BeforeHash && a.AfterHash == b.AfterHash && a.Operation == b.Operation && a.LogicalPath == b.LogicalPath
}

func (s *Server) previewIdentity(a repository.ApprovalRecord, scope repository.ApprovalPreviewContext, snapshot string) string {
	// HMAC avoids a public dictionary oracle over redacted low-entropy values.
	raw, _ := json.Marshal([]any{a.ApprovalID, a.RunID, a.ToolCallID, a.AgentID, a.ToolName, a.ServerName, a.MCPServerID, a.ArgsHash, scope.SessionID, scope.Generation, snapshot})
	mac := hmac.New(sha256.New, []byte(s.jwtSecret))
	_, _ = mac.Write([]byte("approval-preview-v1\x00"))
	_, _ = mac.Write(raw)
	return "sha256:" + hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) preparePreview(ctx context.Context, a repository.ApprovalRecord, scope repository.ApprovalPreviewContext) (approvalpreview.Snapshot, error) {
	var args map[string]any
	if a.ArgsJSON == "" {
		return approvalpreview.Snapshot{State: turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED, ArgumentsJSON: "{}"}, nil
	}
	if err := json.Unmarshal([]byte(a.ArgsJSON), &args); err != nil {
		return approvalpreview.Snapshot{}, err
	}
	_, hash, err := canonicalArgs(args)
	if err != nil || hash != a.ArgsHash {
		return approvalpreview.Snapshot{}, errors.New("stored argument binding mismatch")
	}
	safe, state := approvalpreview.SanitizeArguments(args)
	if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY {
		if fileMutation(a) {
			safe = "{}"
		}
		return approvalpreview.Snapshot{State: state, ArgumentsJSON: safe}, nil
	}
	if !fileMutation(a) {
		return approvalpreview.Snapshot{State: turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNSUPPORTED, ArgumentsJSON: safe}, nil
	}
	base, err := url.Parse(s.previewBaseURL)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		return approvalpreview.Snapshot{}, errors.New("preview endpoint unavailable")
	}
	base.Path = "/internal/approval-preview"
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	logical, _ := args["path"].(string)
	provenance, err := s.IssueToolProvenance(ctx, ProvenanceRequest{SessionID: scope.SessionID, RunID: a.RunID, AgentID: a.AgentID, ToolName: a.ToolName, ArgsHash: a.ArgsHash, LogicalPath: logical})
	if err != nil {
		return approvalpreview.Snapshot{}, err
	}
	req := approvalpreview.Request{Tool: a.ToolName, AgentID: a.AgentID, Args: args, ProvenanceToken: provenance}
	token, err := signHS256(map[string]any{"iss": "turing.orchestrator", "aud": "mcp-files", "kind": approvalpreview.TokenKind,
		"sub": a.AgentID, "tool": a.ToolName, "args_hash": a.ArgsHash, "provenance_hash": approvalpreview.Hash(provenance), "exp": time.Now().Add(10 * time.Second).Unix()}, s.jwtSecret)
	if err != nil {
		return approvalpreview.Snapshot{}, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return approvalpreview.Snapshot{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(body))
	if err != nil {
		return approvalpreview.Snapshot{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("preview redirect refused") }}
	response, err := client.Do(httpReq)
	if err != nil {
		return approvalpreview.Snapshot{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return approvalpreview.Snapshot{}, errors.New("file preview unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, approvalpreview.MaxSnapshotBytes+1))
	if err != nil || len(raw) > approvalpreview.MaxSnapshotBytes {
		return approvalpreview.Snapshot{}, errors.New("preview response too large")
	}
	var p approvalpreview.Snapshot
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, err
	}
	if p.State == turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY && p.File == nil {
		return p, errors.New("ready file preview has no mutation snapshot")
	}
	return p, nil
}

func previewBinding(d *turingv1.ApprovalDetails, generation int64) approvalpreview.Binding {
	b := approvalpreview.Binding{PreviewHash: d.PreviewHash, SessionID: d.SessionId, RunID: d.RunId, ToolCallID: d.ToolCallId, Generation: generation}
	if f := d.FilePreview; f != nil {
		b.PhysicalPath = f.PhysicalPath
		b.BeforeExists = f.BeforeExists
		b.BeforeHash = f.BeforeHash
		b.AfterHash = f.AfterHash
	}
	return b
}
