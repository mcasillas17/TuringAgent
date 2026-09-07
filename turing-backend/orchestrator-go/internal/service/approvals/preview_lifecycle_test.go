package approvals

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func thirdPartyPreviewFixture(t *testing.T) (*approvalHarness, string, *turingv1.ApprovalDetails) {
	t.Helper()
	h := newApprovalHarness(t)
	run := h.createRunningToolCall(t)
	args := map[string]any{"path": "note.txt"}
	raw, hash, err := canonicalArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `UPDATE tool_calls SET server_name='custom',tool_name='custom.write',args_json=?,args_hash=? WHERE id='call_1'`, raw, hash); err != nil {
		t.Fatal(err)
	}
	id, err := h.service.CreateApprovalForTool(context.Background(), run.RunID, "call_1", "general_assistant", "custom.write", args)
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.service.GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: id})
	if err != nil {
		t.Fatal(err)
	}
	return h, id, d
}

func TestPreviewReadsAreStableBoundedAndNonAuthorizing(t *testing.T) {
	h, id, d := thirdPartyPreviewFixture(t)
	if !d.CanApprove || !d.CanDeny || d.PreviewState != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNSUPPORTED || d.FilePreview != nil {
		t.Fatalf("unsupported details=%+v", d)
	}
	var before int
	if err := h.database.QueryRow(`SELECT count(*) FROM events`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		current, err := h.service.GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: id})
		if err != nil || current.PreviewHash != d.PreviewHash {
			t.Fatalf("read=%+v %v", current, err)
		}
	}
	var snapshots, after, artifacts int
	if err := h.database.QueryRow(`SELECT count(*) FROM approval_previews`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRow(`SELECT count(*) FROM events`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRow(`SELECT count(*) FROM sandbox_artifacts`).Scan(&artifacts); err != nil {
		t.Fatal(err)
	}
	a, err := h.repo.GetApproval(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if snapshots != 1 || before != after || artifacts != 0 || a.Status != "pending" || a.ApprovalToken != "" {
		t.Fatal("details authorized, emitted events or retained multiple snapshots")
	}
	if _, err := h.database.Exec(`UPDATE approvals SET args_json=? WHERE id=?`, strings.Repeat("x", approvalpreview.MaxArgsBytes+1), id); err != nil {
		t.Fatal(err)
	}
	a, err = h.repo.GetApproval(context.Background(), id)
	if err != nil || a.ArgsJSON != "" {
		t.Fatal("legacy oversized JSON was returned from SQL")
	}
	oversized, err := h.service.GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: id, RefreshPreview: true})
	if err != nil || oversized.CanApprove || oversized.PreviewState != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED {
		t.Fatalf("oversized=%+v %v", oversized, err)
	}
}

func TestPreviewRefreshCannotRaceBoundDecision(t *testing.T) {
	h, id, d := thirdPartyPreviewFixture(t)
	p, err := h.repo.GetApprovalPreview(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	p.Hash = "new-preview-identity"
	if err := h.repo.StoreApprovalPreview(context.Background(), p, true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ApproveApprovalWithEvent(context.Background(), id, "must-not-commit", sql.NullString{}, "", d.PreviewHash); err == nil {
		t.Fatal("stale preview won atomic decision")
	}
	a, err := h.repo.GetApproval(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "pending" || a.ApprovalToken != "" {
		t.Fatal("stale decision persisted")
	}
}

func TestPreviewTerminalAndWithdrawalFences(t *testing.T) {
	for _, state := range []string{"denied", "consumed", "expired", "cancelled", "withdrawn", "deleted"} {
		t.Run(state, func(t *testing.T) {
			h, id, d := thirdPartyPreviewFixture(t)
			ctx := context.Background()
			switch state {
			case "denied":
				_, err := h.service.DenyApproval(ctx, &turingv1.DenyApprovalRequest{ApprovalId: id, Reason: "not approved"})
				if err != nil {
					t.Fatal(err)
				}
			case "consumed":
				if _, err := h.service.ApproveApproval(ctx, &turingv1.ApproveApprovalRequest{ApprovalId: id, PreviewHash: d.PreviewHash, ArgsHash: d.ArgsHash}); err != nil {
					t.Fatal(err)
				}
				if _, err := h.repo.ConsumeApprovalWithEvent(ctx, id, ""); err != nil {
					t.Fatal(err)
				}
			case "expired":
				if _, err := h.database.Exec(`UPDATE approvals SET expires_at=? WHERE id=?`, repository.FormatTimestamp(time.Now().Add(-time.Minute)), id); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				if _, err := h.database.Exec(`UPDATE agent_runs SET status='cancelled' WHERE id=?`, d.RunId); err != nil {
					t.Fatal(err)
				}
			case "withdrawn":
				if _, err := h.database.Exec(`UPDATE sessions SET deletion_state='deleting' WHERE id=?`, d.SessionId); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if _, err := h.database.Exec(`DELETE FROM sessions WHERE id=?`, d.SessionId); err != nil {
					t.Fatal(err)
				}
			}
			result, err := h.service.GetApprovalDetails(ctx, &turingv1.GetApprovalDetailsRequest{ApprovalId: id})
			if state == "withdrawn" || state == "deleted" {
				if err == nil {
					t.Fatal("withdrawn preview disclosed")
				}
				var n int
				if err := h.database.QueryRow(`SELECT count(*) FROM approval_previews WHERE approval_id=?`, id).Scan(&n); err != nil {
					t.Fatal(err)
				}
				if n != 0 {
					t.Fatal("withdrawal retained preview")
				}
			} else if err != nil || result.CanApprove || result.CanDeny || result.FilePreview != nil {
				t.Fatalf("terminal details=%+v %v", result, err)
			}
		})
	}
}

func TestWithdrawalDuringPreviewReadRefusesDisclosure(t *testing.T) {
	h := newApprovalHarness(t)
	run := h.createRunningToolCall(t)
	args := map[string]any{"path": "note.txt", "content": "after"}
	raw, hash, _ := canonicalArgs(args)
	if _, err := h.database.Exec(`UPDATE tool_calls SET args_json=?,args_hash=? WHERE id='call_1'`, raw, hash); err != nil {
		t.Fatal(err)
	}
	id, err := h.service.CreateApprovalForTool(context.Background(), run.RunID, "call_1", "general_assistant", "files.update", args)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_ = json.NewEncoder(w).Encode(approvalpreview.Snapshot{State: turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY, ArgumentsJSON: raw,
			File: &turingv1.FileMutationPreview{LogicalPath: "note.txt", PhysicalPath: "note.txt", BeforeText: "must not disclose", AfterText: "after"}})
	}))
	defer server.Close()
	h.service.SetPreviewEndpoint(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := h.service.GetApprovalDetails(ctx, &turingv1.GetApprovalDetailsRequest{ApprovalId: id})
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := h.database.Exec(`UPDATE sessions SET deletion_state='deleting' WHERE id=?`, run.SessionID); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("in-flight preview disclosed after withdrawal")
	}
}
