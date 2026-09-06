package approvals

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
)

func TestNonReviewablePreviewStatesOmitUnkeyedArgumentHash(t *testing.T) {
	for _, state := range []turingv1.ApprovalPreviewState{
		turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNSPECIFIED,
		turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE,
		turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED,
		turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED,
		turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_BINARY,
		turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE,
		turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_EXPIRED,
		turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_TERMINAL,
	} {
		t.Run(state.String(), func(t *testing.T) {
			h, id := previewReadFixture(t, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(approvalpreview.Snapshot{State: state, ArgumentsJSON: "{}"})
			})
			d := readPreviewDetails(t, h, id, false)
			if d.ArgsHash != "" || d.PreviewHash == "" || d.CanApprove {
				t.Fatalf("hidden state exposes argument fingerprint: %s", d.PreviewState)
			}
		})
	}
}

func TestUnsupportedArgumentsRemainStructuredAndHashBound(t *testing.T) {
	h, id, d := thirdPartyPreviewFixture(t)
	a, err := h.repo.GetApproval(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if d.PreviewState != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNSUPPORTED || !d.CanApprove ||
		d.ArgumentsJson != a.ArgsJSON || d.ArgumentsJson == "{}" || d.ArgsHash != a.ArgsHash {
		t.Fatal("non-file safe arguments were hidden or lost their canonical binding")
	}
}
