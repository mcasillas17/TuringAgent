package approvals

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestApproveMissingPreviewBindingFailsClosed(t *testing.T) {
	h := newApprovalHarness(t)
	run := h.createRunningToolCall(t)
	id, err := h.service.CreateApprovalForTool(context.Background(), run.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt", "content": "new"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: id})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("old client approve = %v", err)
	}
}

func TestPreviewStableRefreshAndBoundDecision(t *testing.T) {
	h := newApprovalHarness(t)
	run := h.createRunningToolCall(t)
	args := map[string]any{"path": "note.txt", "content": "replacement <&>\n"}
	canonical, hash, err := canonicalArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `UPDATE tool_calls SET args_json=?,args_hash=? WHERE id='call_1'`, canonical, hash); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	before := "original"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req approvalpreview.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if req.Tool != "files.update" || req.Args["content"] != args["content"] {
			t.Error("preview request not stored operation")
		}
		mu.Lock()
		text := before
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(approvalpreview.Snapshot{State: turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY, ArgumentsJSON: canonical,
			File: &turingv1.FileMutationPreview{LogicalPath: "note.txt", PhysicalPath: "note.txt", Operation: "update", BeforeExists: true, BeforeHash: approvalpreview.Hash(text), AfterHash: approvalpreview.Hash(args["content"].(string)), BeforeText: text, AfterText: args["content"].(string)}})
	}))
	defer server.Close()
	h.service.SetPreviewEndpoint(server.URL + "/mcp")
	id, err := h.service.CreateApprovalForTool(context.Background(), run.RunID, "call_1", "general_assistant", "files.update", args)
	if err != nil {
		t.Fatal(err)
	}
	get := func(refresh bool) *turingv1.ApprovalDetails {
		t.Helper()
		d, err := h.service.GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: id, RefreshPreview: refresh})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	first := get(false)
	if !first.CanApprove || first.ArgsHash != hash || first.PreviewHash == "" {
		t.Fatalf("first = %+v", first)
	}
	if again := get(false); again.PreviewHash != first.PreviewHash {
		t.Fatal("read changed identity")
	}
	restarted := New(h.repo, h.bus, "approval-secret")
	restarted.SetPreviewEndpoint(server.URL)
	h.service = restarted
	if again := get(false); again.PreviewHash != first.PreviewHash {
		t.Fatal("restart changed identity")
	}
	mu.Lock()
	before = "externally changed"
	mu.Unlock()
	stale := get(false)
	if stale.PreviewState != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE || stale.PreviewHash != first.PreviewHash || stale.CanApprove {
		t.Fatalf("stale=%+v", stale)
	}
	if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: id, PreviewHash: first.PreviewHash, ArgsHash: hash}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale approve=%v", err)
	}
	refreshed := get(true)
	if !refreshed.CanApprove || refreshed.PreviewHash == first.PreviewHash {
		t.Fatalf("refreshed=%+v", refreshed)
	}
	req := &turingv1.ApproveApprovalRequest{ApprovalId: id, PreviewHash: refreshed.PreviewHash, ArgsHash: hash}
	if _, err := h.service.ApproveApproval(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.ApproveApproval(context.Background(), req); err != nil {
		t.Fatalf("idempotent decision: %v", err)
	}
	record, err := h.repo.GetApproval(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.Split(record.ApprovalToken, ".")[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["preview_hash"] != refreshed.PreviewHash || claims["before_hash"] != approvalpreview.Hash("externally changed") || claims["args_hash"] != hash || claims["physical_path"] != "note.txt" {
		t.Fatalf("claims=%+v", claims)
	}
	if _, err := h.service.GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: id, RefreshPreview: true}); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("approved preview refreshed")
	}
}

func TestInternalDetailsDenied(t *testing.T) {
	h := newApprovalHarness(t)
	_, err := NewInternalServer(h.service).GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: "missing"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("internal details = %v", err)
	}
}
