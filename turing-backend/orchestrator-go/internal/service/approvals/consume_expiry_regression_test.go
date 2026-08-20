package approvals

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestConsumeApprovalAfterExpiryRejectsAndTerminalizes(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Second).Format(time.RFC3339Nano), approvalID); err != nil {
		t.Fatal(err)
	}

	_, err = h.service.ConsumeApproval(context.Background(), h.consumeRequest(t, enqueued, approvalID, "note.txt"))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ConsumeApproval error = %v, want FailedPrecondition", err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "expired" || approval.ApprovalToken != "" {
		t.Fatalf("approval after expired consume = %+v", approval)
	}
}
