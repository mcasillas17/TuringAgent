package approvals

import (
	"encoding/json"
	"net/http"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestFreshReadyResponseWithoutFileCannotEnableApproval(t *testing.T) {
	h, id := previewReadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		p := readyReadSnapshot("before")
		p.File = nil
		_ = json.NewEncoder(w).Encode(p)
	})
	d := readPreviewDetails(t, h, id, false)
	if d.CanApprove || d.PreviewState != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE {
		t.Fatalf("missing fresh file snapshot enabled approval: %s", d.PreviewState)
	}
}
