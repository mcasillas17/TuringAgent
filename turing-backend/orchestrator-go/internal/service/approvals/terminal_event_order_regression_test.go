package approvals

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestApprovalTerminalOutcomePublishesBeforeRunFailure(t *testing.T) {
	tests := []struct {
		name         string
		terminalType string
		terminalize  func(t *testing.T, h *approvalHarness, approvalID string)
	}{
		{
			name:         "denied",
			terminalType: "approval.denied",
			terminalize: func(t *testing.T, h *approvalHarness, approvalID string) {
				t.Helper()
				if _, err := h.service.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: "no"}); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:         "expired",
			terminalType: "approval.expired",
			terminalize: func(t *testing.T, h *approvalHarness, approvalID string) {
				t.Helper()
				if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Second).Format(time.RFC3339Nano), approvalID); err != nil {
					t.Fatal(err)
				}
				if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); status.Code(err) != codes.FailedPrecondition {
					t.Fatalf("ApproveApproval error = %v, want FailedPrecondition", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newApprovalHarness(t)
			enqueued := h.createRunningToolCall(t)
			approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
			if err != nil {
				t.Fatal(err)
			}
			stream, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
			defer unsubscribe()

			test.terminalize(t, h, approvalID)
			first := nextApprovalTerminalEvent(t, stream)
			second := nextApprovalTerminalEvent(t, stream)
			third := nextApprovalTerminalEvent(t, stream)
			wantToolType := "tool.call.failed"
			if test.name == "denied" {
				wantToolType = "tool.call.denied"
			}
			if first.Type != test.terminalType || second.Type != wantToolType || third.Type != "agent.run.failed" {
				t.Fatalf("stream order = %q, %q, %q; want %q, %q, agent.run.failed", first.Type, second.Type, third.Type, test.terminalType, wantToolType)
			}
			if first.Sequence >= second.Sequence || second.Sequence >= third.Sequence {
				t.Fatalf("stream sequences = %d, %d, %d; want approval before tool before run failure", first.Sequence, second.Sequence, third.Sequence)
			}
			if _, err := h.service.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: "retry"}); test.name == "denied" && err != nil {
				t.Fatalf("idempotent denial retry: %v", err)
			}
			var terminalEvents int
			if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, enqueued.RunID).Scan(&terminalEvents); err != nil {
				t.Fatal(err)
			}
			if terminalEvents != 1 {
				t.Fatalf("agent.run.failed count = %d, want one", terminalEvents)
			}
		})
	}
}

func nextApprovalTerminalEvent(t *testing.T, stream <-chan events.Event) events.Event {
	t.Helper()
	select {
	case event := <-stream:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal chat event")
	}
	return events.Event{}
}
