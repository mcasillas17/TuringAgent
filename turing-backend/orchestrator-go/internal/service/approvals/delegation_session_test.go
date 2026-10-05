package approvals

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// A specialist's write stops for the user's decision like any other, so
// deciding an approval must keep working in a delegation session even though
// every public mutation of that session is refused.
func TestApprovalDecisionsStillWorkInADelegationSession(t *testing.T) {
	for _, decision := range []string{"approved", "denied"} {
		t.Run(decision, func(t *testing.T) {
			h := newApprovalHarness(t)
			ctx := context.Background()
			enqueued := h.createRunningToolCall(t)
			if _, err := h.database.ExecContext(ctx, `UPDATE sessions SET kind = 'delegation' WHERE id = ?`, enqueued.SessionID); err != nil {
				t.Fatal(err)
			}
			approvalID, err := h.service.CreateApprovalForTool(ctx, enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
			if err != nil {
				t.Fatalf("creating an approval in a delegation session: %v", err)
			}
			if decision == "approved" {
				_, err = reviewedApprove(t, h.service, h.database, ctx, &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
			} else {
				_, err = h.service.DenyApproval(ctx, &turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: "not this file"})
			}
			if err != nil {
				t.Fatalf("deciding an approval in a delegation session: %v", err)
			}
			approval, err := h.repo.GetApproval(ctx, approvalID)
			if err != nil || approval.Status != decision {
				t.Fatalf("approval = %+v, %v; want %s", approval, err, decision)
			}
		})
	}
}
