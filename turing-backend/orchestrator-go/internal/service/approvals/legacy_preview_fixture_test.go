package approvals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc"
)

// reviewedRequest migrates the pre-preview lifecycle fixtures (which used
// placeholder tool-call hashes and sometimes omitted content) to the required
// reviewed-request protocol. These tests exercise lifecycle transactions, not
// filesystem preparation; preview_test and mcp-files tests use real operations.
func reviewedRequest(t *testing.T, s *Server, database *db.DB, req *turingv1.ApproveApprovalRequest) *turingv1.ApproveApprovalRequest {
	t.Helper()
	a, err := s.repo.GetApproval(context.Background(), req.ApprovalId)
	if err != nil || expired(a.ExpiresAt) {
		return req
	}
	if database != nil {
		if _, err := database.ExecContext(context.Background(), `UPDATE tool_calls SET args_hash=?,args_json=?,tool_name=?,agent_id=? WHERE id=?`,
			a.ArgsHash, a.ArgsJSON, a.ToolName, a.AgentID, a.ToolCallID); err != nil {
			t.Fatal(err)
		}
	}
	scope, err := s.repo.GetApprovalPreviewContext(context.Background(), a.ApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.repo.GetApprovalPreview(context.Background(), a.ApprovalID)
	var snap approvalpreview.Snapshot
	if err == nil {
		if err := json.Unmarshal([]byte(p.SnapshotJSON), &snap); err != nil {
			t.Fatal(err)
		}
	} else {
		snap = approvalpreview.Snapshot{State: turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNSUPPORTED, ArgumentsJSON: a.ArgsJSON}
		if fileMutation(a) {
			var args map[string]any
			if err := json.Unmarshal([]byte(a.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			logical, _ := args["path"].(string)
			content, _ := args["content"].(string)
			snap.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY
			snap.File = &turingv1.FileMutationPreview{LogicalPath: logical, PhysicalPath: repository.OwnedSandboxPath(scope.SessionID, a.RunID, logical),
				Operation: "update", BeforeExists: true, BeforeHash: approvalpreview.Hash("fixture before"), AfterHash: approvalpreview.Hash(content), BeforeText: "fixture before", AfterText: content}
		}
		raw, _ := json.Marshal(snap)
		p = repository.ApprovalPreviewRecord{ApprovalID: a.ApprovalID, Generation: scope.Generation, SnapshotJSON: string(raw)}
		p.Hash = s.previewIdentity(a, scope, p.SnapshotJSON)
		if err := s.repo.StoreApprovalPreview(context.Background(), p, false); err != nil {
			t.Fatal(err)
		}
	}
	if fileMutation(a) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(snap) }))
		t.Cleanup(server.Close)
		s.SetPreviewEndpoint(server.URL)
	}
	req.PreviewHash = p.Hash
	req.ArgsHash = a.ArgsHash
	return req
}

func reviewedApprove(t *testing.T, s *Server, database *db.DB, ctx context.Context, req *turingv1.ApproveApprovalRequest) (*turingv1.ApprovalResponse, error) {
	return s.ApproveApproval(ctx, reviewedRequest(t, s, database, req))
}

type reviewedClient struct {
	turingv1.ApprovalServiceClient
	t *testing.T
	h *approvalHarness
}

func newReviewedClient(t *testing.T, h *approvalHarness) reviewedClient {
	return reviewedClient{ApprovalServiceClient: turingv1.NewApprovalServiceClient(h.conn), t: t, h: h}
}

func (c reviewedClient) ApproveApproval(ctx context.Context, req *turingv1.ApproveApprovalRequest, opts ...grpc.CallOption) (*turingv1.ApprovalResponse, error) {
	return c.ApprovalServiceClient.ApproveApproval(ctx, reviewedRequest(c.t, c.h.service, c.h.database, req), opts...)
}
