package approvals

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A job that enforces its frozen set can get no approval for a tool outside
// it, so there is nothing the user could approve that the run may not use.
// The tool is identified by the tool call it is for, server included.
func TestNoApprovalIsCreatedForAToolOutsideAnEnforcedSet(t *testing.T) {
	for _, test := range []struct {
		name     string
		selected string
		allowed  bool
	}{
		{"outside the set", `["files/files.read"]`, false},
		{"same tool on another server", `["vendor/files.update"]`, false},
		{"empty set", `[]`, false},
		{"inside the set", `["files/files.update"]`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newApprovalHarness(t)
			ctx := context.Background()
			enqueued := h.createRunningToolCall(t)
			if _, err := h.database.ExecContext(ctx, `
				UPDATE jobs SET payload_json = json_set(payload_json,
					'$.enforceSelectedTools', json('true'), '$.selectedTools', json(?))
				WHERE run_id = ?`, test.selected, enqueued.RunID); err != nil {
				t.Fatal(err)
			}

			approvalID, err := h.service.CreateApprovalForTool(ctx, enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
			if test.allowed {
				if err != nil || approvalID == "" {
					t.Fatalf("in-set approval = %q, %v", approvalID, err)
				}
				return
			}
			if status.Code(err) != codes.PermissionDenied || status.Convert(err).Message() != "tool is not selected for this run" {
				t.Fatalf("out-of-set approval = %q, %v; want PermissionDenied not selected", approvalID, err)
			}
			if _, err := h.repo.GetApprovalByToolCall(ctx, enqueued.RunID, "call_1"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("approval lookup = %v, want none created", err)
			}
		})
	}
}

// Without a recorded tool call there is no server to check the tool against,
// so an enforcing job is refused rather than guessed about.
func TestAnEnforcedRunGetsNoApprovalForAnUnrecordedToolCall(t *testing.T) {
	h := newApprovalHarness(t)
	ctx := context.Background()
	enqueued := h.createRunningToolCall(t)
	if _, err := h.database.ExecContext(ctx, `
		UPDATE jobs SET payload_json = json_set(payload_json,
			'$.enforceSelectedTools', json('true'), '$.selectedTools', json('["files/files.update"]'))
		WHERE run_id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	_, err := h.service.CreateApprovalForTool(ctx, enqueued.RunID, "call_unrecorded", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("approval for an unrecorded call = %v, want PermissionDenied", err)
	}
}
