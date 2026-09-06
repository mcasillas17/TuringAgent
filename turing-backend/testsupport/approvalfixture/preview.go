// Package approvalfixture supports lifecycle tests that do not run a file
// server. Its simulated filesystem response is not a substitute for the real
// protected-write end-to-end tests.
package approvalfixture

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
)

type Service interface {
	GetApprovalDetails(context.Context, *turingv1.GetApprovalDetailsRequest) (*turingv1.ApprovalDetails, error)
	ApproveApproval(context.Context, *turingv1.ApproveApprovalRequest) (*turingv1.ApprovalResponse, error)
}

func Request(t *testing.T, s Service, req *turingv1.ApproveApprovalRequest) *turingv1.ApproveApprovalRequest {
	t.Helper()
	d, err := s.GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: req.ApprovalId})
	if err != nil {
		t.Fatal(err)
	}
	req.ArgsHash = d.ArgsHash
	req.PreviewHash = d.PreviewHash
	return req
}

func Approve(t *testing.T, s Service, ctx context.Context, req *turingv1.ApproveApprovalRequest) (*turingv1.ApprovalResponse, error) {
	t.Helper()
	return s.ApproveApproval(ctx, Request(t, s, req))
}

// Endpoint represents a readable, writable file with fixed before content in
// this run's owned directory. Preview and approval still traverse real durable
// service/repository logic; only the cross-module filesystem read is simulated.
func Endpoint(t *testing.T, s interface{ SetPreviewEndpoint(string) }) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req approvalpreview.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid fixture request", 400)
			return
		}
		parts := strings.Split(req.ProvenanceToken, ".")
		if len(parts) != 3 {
			http.Error(w, "missing provenance", 400)
			return
		}
		raw, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			http.Error(w, "invalid provenance", 400)
			return
		}
		var prov struct {
			SID string `json:"sid"`
			RID string `json:"rid"`
		}
		if err := json.Unmarshal(raw, &prov); err != nil {
			http.Error(w, "invalid provenance", 400)
			return
		}
		logical, _ := req.Args["path"].(string)
		after, _ := req.Args["content"].(string)
		safe, state := approvalpreview.SanitizeArguments(req.Args)
		snap := approvalpreview.Snapshot{State: state, ArgumentsJSON: safe}
		if state == turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY {
			file := &turingv1.FileMutationPreview{LogicalPath: logical, PhysicalPath: path.Join("sessions", prov.SID, "runs", prov.RID, "files", logical),
				Operation: strings.TrimPrefix(req.Tool, "files."), AfterHash: approvalpreview.Hash(after), AfterText: after}
			if req.Tool == "files.update" {
				file.BeforeExists = true
				file.BeforeText = "fixture before"
				file.BeforeHash = approvalpreview.Hash(file.BeforeText)
			}
			file.UnifiedDiff = approvalpreview.Diff(file.BeforeText, after)
			snap.File = file
		}
		_ = json.NewEncoder(w).Encode(snap)
	}))
	t.Cleanup(server.Close)
	s.SetPreviewEndpoint(server.URL)
}
