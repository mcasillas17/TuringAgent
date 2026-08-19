package approvals

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestDeniedApprovalEventFailureRollsBackAndRetryCommitsOnce(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_atomic_denied_event
		BEFORE INSERT ON events
		WHEN NEW.type = 'approval.denied'
		BEGIN
			SELECT RAISE(ABORT, 'approval denied event failed');
		END;
	`); err != nil {
		t.Fatal(err)
	}

	const reason = "The generated change is not the one I intended"
	if _, err := h.service.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{
		ApprovalId: approvalID,
		Reason:     reason,
	}); err == nil {
		t.Fatal("DenyApproval succeeded despite required approval.denied event failure")
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "pending" || approval.DenialReason.Valid {
		t.Fatalf("failed denial leaked state: %+v", approval)
	}
	var runStatus, jobStatus, toolStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = 'call_1'`).Scan(&toolStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "waiting_approval" || jobStatus != "in_progress" || toolStatus != "approval_required" {
		t.Fatalf("failed decision mutation leaked: run=%q job=%q tool=%q", runStatus, jobStatus, toolStatus)
	}
	if _, err := h.database.ExecContext(context.Background(), `DROP TRIGGER fail_atomic_denied_event`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{
		ApprovalId: approvalID,
		Reason:     reason,
	}); err != nil {
		t.Fatalf("DenyApproval retry: %v", err)
	}
	if _, err := h.service.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{
		ApprovalId: approvalID,
		Reason:     "replacement reason",
	}); err != nil {
		t.Fatalf("same denial retry: %v", err)
	}
	approval, err = h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "denied" || !approval.DenialReason.Valid || approval.DenialReason.String != reason {
		t.Fatalf("committed denial rationale = %+v", approval)
	}
	var terminalEvents int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, enqueued.RunID).Scan(&terminalEvents); err != nil {
		t.Fatal(err)
	}
	if terminalEvents != 1 {
		t.Fatalf("agent.run.failed count = %d, want one committed terminal event", terminalEvents)
	}
}

func TestApprovedApprovalEventFailureRollsBackCommentAndDecision(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(
		context.Background(),
		enqueued.RunID,
		"call_1",
		"general_assistant",
		"files.update",
		map[string]any{"path": "note.txt"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_atomic_approved_event
		BEFORE INSERT ON events
		WHEN NEW.type = 'approval.approved'
		BEGIN
			SELECT RAISE(ABORT, 'approval approved event failed');
		END;
	`); err != nil {
		t.Fatal(err)
	}

	const comment = "I checked the exact path"
	if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{
		ApprovalId: approvalID,
		Comment:    comment,
	}); err == nil {
		t.Fatal("ApproveApproval succeeded despite required approval.approved event failure")
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "pending" || approval.ApprovalComment.Valid {
		t.Fatalf("failed approval leaked state: %+v", approval)
	}
	if _, err := h.database.ExecContext(context.Background(), `DROP TRIGGER fail_atomic_approved_event`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{
		ApprovalId: approvalID,
		Comment:    comment,
	}); err != nil {
		t.Fatal(err)
	}
	approval, err = h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "approved" || !approval.ApprovalComment.Valid || approval.ApprovalComment.String != comment {
		t.Fatalf("committed approval rationale = %+v", approval)
	}
}

func TestApprovedApprovalIgnoresAncillaryFailuresAndRetriesIdempotently(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_approved_audit
		BEFORE INSERT ON audit_logs
		WHEN NEW.action = 'approval.approved'
		BEGIN
			SELECT RAISE(ABORT, 'approval audit unavailable');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	h.service.SetNotifier(failingApprovalNotifier{err: context.DeadlineExceeded})

	first, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatalf("ApproveApproval reported committed ancillary failure: %v", err)
	}
	if first.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED {
		t.Fatalf("first approval response = %+v", first)
	}
	second, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatalf("same approval retry: %v", err)
	}
	if second.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED {
		t.Fatalf("second approval response = %+v", second)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "approved" || approval.ApprovalToken == "" {
		t.Fatalf("approved record = %+v", approval)
	}
	var approvedEvents int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'approval.approved'`, enqueued.RunID).Scan(&approvedEvents); err != nil {
		t.Fatal(err)
	}
	if approvedEvents != 1 {
		t.Fatalf("approval.approved count = %d, want one", approvedEvents)
	}
}

func TestApprovalCreationIgnoresAncillaryAuditFailure(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_requested_audit
		BEFORE INSERT ON audit_logs
		WHEN NEW.action = 'approval.requested'
		BEGIN
			SELECT RAISE(ABORT, 'approval request audit unavailable');
		END;
	`); err != nil {
		t.Fatal(err)
	}

	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatalf("CreateApprovalForTool reported committed ancillary failure: %v", err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "pending" {
		t.Fatalf("created approval = %+v, want pending", approval)
	}
}

func TestApprovalCreationRequiredEventFailureRollsBackState(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_requested_event
		BEFORE INSERT ON events
		WHEN NEW.type = 'approval.requested'
		BEGIN
			SELECT RAISE(ABORT, 'approval request event unavailable');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"}); err == nil {
		t.Fatal("CreateApprovalForTool succeeded despite required approval event failure")
	}
	var approvals int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM approvals WHERE run_id = ?`, enqueued.RunID).Scan(&approvals); err != nil {
		t.Fatal(err)
	}
	var runStatus, toolStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = 'call_1'`).Scan(&toolStatus); err != nil {
		t.Fatal(err)
	}
	if approvals != 0 || runStatus != "running" || toolStatus != "requested" {
		t.Fatalf("required event failure leaked approval state: approvals=%d run=%q tool=%q", approvals, runStatus, toolStatus)
	}
}
