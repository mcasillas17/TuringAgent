package approvals

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestRedactedPreviewDoesNotExposeGuessableArgumentHash(t *testing.T) {
	h := newApprovalHarness(t)
	run := h.createRunningToolCall(t)
	args := map[string]any{"path": ".env", "content": "fixture private value"}
	raw, hash, _ := canonicalArgs(args)
	if _, err := h.database.Exec(`UPDATE tool_calls SET args_json=?,args_hash=? WHERE id='call_1'`, raw, hash); err != nil {
		t.Fatal(err)
	}
	id, err := h.service.CreateApprovalForTool(context.Background(), run.RunID, "call_1", "general_assistant", "files.update", args)
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.service.GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: id})
	if err != nil {
		t.Fatal(err)
	}
	if d.PreviewState != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED || d.CanApprove || d.ArgsHash != "" || d.FilePreview != nil {
		t.Fatalf("redacted details expose content identity: %+v", d)
	}
}

func TestPreviewStoreRefusesExpiryDuringPreparation(t *testing.T) {
	h, id, _ := thirdPartyPreviewFixture(t)
	p, err := h.repo.GetApprovalPreview(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.Exec(`UPDATE approvals SET expires_at=? WHERE id=?`, repository.FormatTimestamp(time.Now().Add(-time.Second)), id); err != nil {
		t.Fatal(err)
	}
	p.Hash = "must-not-replace-expired"
	if err := h.repo.StoreApprovalPreview(context.Background(), p, true); err == nil {
		t.Fatal("refresh committed after approval expired")
	}
}
