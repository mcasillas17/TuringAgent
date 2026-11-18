package runtime

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/testsupport/approvalfixture"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TUR-026c: a run sent back to the queue after it made an allowlisted
// mutation replays its history on a new attempt, and may ask for the same call
// again. Nobody is there to notice, so the approval commit refuses an
// unattended approval for a mutation the run already spent one on.

// attemptFor claims the run's next attempt for workerID and delivers it, as
// dispatch does.
func (h *harness) attemptFor(t *testing.T, runID string, workerID string) repository.Job {
	t.Helper()
	ctx := context.Background()
	claimed, err := h.repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", workerID, 0, 0, nil, nil)
	if err != nil || claimed.RunID != runID {
		t.Fatalf("claim for %s = %+v, %v; want run %s", workerID, claimed, err, runID)
	}
	assignment := repository.Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: workerID, AttemptID: claimed.AssignmentAttemptID,
	}
	if err := h.repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	return claimed
}

// spendMutation carries one approval-required call of the attempt through to
// a spent token: the beacon's decision, approve (a person's click, or nothing
// when the automation already pre-approved it), the worker's resume, and
// mcp-files consuming the token as it writes. It returns the approval's id.
func (h *harness) spendMutation(t *testing.T, beacon *turingv1.ToolCallBeacon, workerID string, attempt repository.Job, approve func(approvalID string)) string {
	t.Helper()
	ctx := context.Background()
	decision, err := h.service.handleToolBeacon(ctx, beacon)
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED || decision.GetTerminalRun() {
		t.Fatalf("decision = %+v, want approval required", decision)
	}
	approve(decision.GetApprovalId())
	waiting := h.runState(t, beacon.GetRunId())
	if _, err := h.repo.ResumeApprovedRun(ctx, repository.ResumeApprovedRunInput{
		RunID: beacon.GetRunId(), ApprovalID: decision.GetApprovalId(), WorkerID: workerID,
		AssignmentAttemptID: attempt.AssignmentAttemptID, ExpectedStateVersion: waiting.StateVersion,
	}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, err := h.repo.ConsumeApprovalWithEvent(ctx, decision.GetApprovalId(), ""); err != nil {
		t.Fatalf("consume: %v", err)
	}
	return decision.GetApprovalId()
}

// reportTransientFailure is a retryable model failure the worker reports for
// the run, from owner (zero when it can no longer name one). It must send the
// run back to the queue.
func (h *harness) reportTransientFailure(t *testing.T, runID string, owner runReleaseOwner) {
	t.Helper()
	if _, err := h.service.handleRunFailed(context.Background(), &turingv1.RuntimeRunFailed{
		RunId: runID, Code: "model_timeout",
		FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
		AutomaticRetryClass:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
		ExpectedStateVersion: h.runState(t, runID).StateVersion,
	}, owner); err != nil {
		t.Fatalf("report transient failure: %v", err)
	}
	if state := h.runState(t, runID); state.Lifecycle != "queued" {
		t.Fatalf("run after a transient failure = %q, want it requeued", state.Lifecycle)
	}
}

// retryPath is one way a run that already made a mutation goes back to the
// queue for another attempt, keeping its spent approval. Stale-lease recovery
// on its own is not one: it fails a run holding a spent approval as
// side_effect_uncertain.
type retryPath struct {
	name    string
	requeue func(t *testing.T, h *harness, runID string, workerID string, attempt repository.Job)
}

func retryPaths() []retryPath {
	return []retryPath{
		{name: "lease lost", requeue: func(t *testing.T, h *harness, runID string, workerID string, attempt repository.Job) {
			// Ownership is fenced, as a lost lease or stream fences it, and a
			// retryable failure that can no longer name an owner requeues it.
			h.fenceOwnership(t, runID, workerID, attempt.AssignmentAttemptID)
			h.reportTransientFailure(t, runID, runReleaseOwner{})
		}},
		{name: "transient failure", requeue: func(t *testing.T, h *harness, runID string, workerID string, attempt repository.Job) {
			h.reportTransientFailure(t, runID, runReleaseOwner{workerID: workerID, attemptID: attempt.AssignmentAttemptID})
		}},
	}
}

func preApproved(string) {}

// The acceptance case: after the first attempt made an allowlisted mutation
// and the run was requeued, the retried attempt's identical call is blocked.
// The run fails under the automation-policy origin with an audit row, and the
// call's approval is never approved, so no token for it exists.
func TestARetriedAttemptCannotRepeatAnAllowlistedMutationItAlreadyMade(t *testing.T) {
	for _, path := range retryPaths() {
		t.Run(path.name, func(t *testing.T) {
			h := newHarness(t)
			automation := h.createAutomation(t, "Nightly note", grantUpdate)
			fire := h.claimAutomation(t, automation)
			first := h.attemptFor(t, fire.RunID, "worker-first")
			spent := h.spendMutation(t, automationToolBeacon(fire, "call_first", grantUpdate, toolArgs(grantUpdate)), "worker-first", first, preApproved)

			path.requeue(t, h, fire.RunID, "worker-first", first)
			retry := h.attemptFor(t, fire.RunID, "worker-retry")
			if retry.AssignmentAttemptID == first.AssignmentAttemptID {
				t.Fatal("the retry reused the first attempt")
			}
			decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_retry", grantUpdate, toolArgs(grantUpdate)))
			if err != nil {
				t.Fatalf("tool beacon: %v", err)
			}

			if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || !decision.GetTerminalRun() ||
				decision.GetReason() != UnattendedRepeatBlockedCode {
				t.Fatalf("decision = %+v, want a terminal %s deny", decision, UnattendedRepeatBlockedCode)
			}
			h.assertRunFailedFor(t, fire.RunID, UnattendedRepeatBlockedCode)
			var approvalStatus string
			var token sql.NullString
			if err := h.database.QueryRowContext(context.Background(),
				`SELECT status, approval_token FROM approvals WHERE run_id = ? AND tool_call_id = ?`,
				fire.RunID, "call_retry").Scan(&approvalStatus, &token); err != nil {
				t.Fatalf("read the retried call's approval: %v", err)
			}
			if approvalStatus != "expired" || token.Valid {
				t.Fatalf("retried call's approval = %s token %v, want expired by the failure, never signed", approvalStatus, token)
			}
			if rows := h.auditRows(t, fire.RunID, "approval.approved"); len(rows) != 1 {
				t.Fatalf("approval.approved audit rows = %d, want only the first attempt's", len(rows))
			}
			if got := h.toolCallStatus(t, "call_retry"); got != "failed" {
				t.Fatalf("retried tool call = %q, want failed with the run", got)
			}
			if approval, err := h.repo.GetApproval(context.Background(), spent); err != nil || approval.Status != "consumed" {
				t.Fatalf("first approval = %+v, %v; want it still consumed", approval, err)
			}
		})
	}
}

// Different arguments are a different mutation, and the retried attempt is
// pre-approved for it as the first attempt was.
func TestARetriedAttemptMayMakeADifferentAllowlistedMutation(t *testing.T) {
	for _, path := range retryPaths() {
		t.Run(path.name, func(t *testing.T) {
			h := newHarness(t)
			automation := h.createAutomation(t, "Nightly note", grantUpdate)
			fire := h.claimAutomation(t, automation)
			first := h.attemptFor(t, fire.RunID, "worker-first")
			h.spendMutation(t, automationToolBeacon(fire, "call_first", grantUpdate, toolArgs(grantUpdate)), "worker-first", first, preApproved)

			path.requeue(t, h, fire.RunID, "worker-first", first)
			h.attemptFor(t, fire.RunID, "worker-retry")
			decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_retry", grantUpdate,
				map[string]any{"path": "note.txt", "content": "goodbye"}))
			if err != nil {
				t.Fatalf("tool beacon: %v", err)
			}

			if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED || decision.GetTerminalRun() {
				t.Fatalf("decision = %+v, want approval required", decision)
			}
			approval, err := h.repo.GetApproval(context.Background(), decision.GetApprovalId())
			if err != nil {
				t.Fatal(err)
			}
			if approval.Status != "approved" || approval.ApprovalToken == "" {
				t.Fatalf("approval = %s token %q, want pre-approved and signed", approval.Status, approval.ApprovalToken)
			}
			if rows := h.auditRows(t, fire.RunID, "automation.tool.blocked"); len(rows) != 0 {
				t.Fatalf("automation.tool.blocked audit rows = %d, want none", len(rows))
			}
		})
	}
}

// A run somebody is watching is not touched: its retried attempt's identical
// call waits for that person, and their approval goes through.
func TestAnAttendedRunMayRepeatAMutationAfterARetry(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	enqueued := h.enqueueRun(t, "update the note")
	beacon := func(toolCallID string) *turingv1.ToolCallBeacon {
		return &turingv1.ToolCallBeacon{
			RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: toolCallID,
			AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "files", ToolName: "files.update",
			Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: mustStruct(t, toolArgs(grantUpdate)),
		}
	}
	personApproves := func(approvalID string) {
		if _, err := approvalfixture.Approve(t, h.approvals, ctx, &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	}
	first := h.attemptFor(t, enqueued.RunID, "worker-first")
	h.spendMutation(t, beacon("call_first"), "worker-first", first, personApproves)

	retryPaths()[0].requeue(t, h, enqueued.RunID, "worker-first", first)
	h.attemptFor(t, enqueued.RunID, "worker-retry")
	decision, err := h.service.handleToolBeacon(ctx, beacon("call_retry"))
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED || decision.GetTerminalRun() {
		t.Fatalf("decision = %+v, want it to wait for the person", decision)
	}
	personApproves(decision.GetApprovalId())
	approval, err := h.repo.GetApproval(ctx, decision.GetApprovalId())
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "approved" || approval.ApprovalToken == "" {
		t.Fatalf("approval = %s token %q, want the person's approval signed", approval.Status, approval.ApprovalToken)
	}
	if state := h.runState(t, enqueued.RunID); state.Lifecycle == "failed" {
		t.Fatal("an attended run failed on a repeated call")
	}
}

// GrantUnattendedApproval reports the repeat as its own refusal: a
// PermissionDenied that still names the repository error, which is how the
// runtime tells it from a revoked grant and from a grant that failed. Within
// one attempt it is the same refusal: the guard is the ceiling the design
// accepts, and the run fails visibly.
func TestGrantUnattendedApprovalRefusesARepeatAsItsOwnPermissionDenied(t *testing.T) {
	h := newHarness(t)
	automation := h.createAutomation(t, "Nightly note", grantUpdate)
	fire := h.claimAutomation(t, automation)
	attempt := h.attemptFor(t, fire.RunID, "worker-first")
	h.spendMutation(t, automationToolBeacon(fire, "call_first", grantUpdate, toolArgs(grantUpdate)), "worker-first", attempt, preApproved)
	approvalID := h.pendingApprovalFor(t, fire.RunID, fire.SessionID, fire.TraceID, "call_again", "files.update")

	err := h.approvals.GrantUnattendedApproval(context.Background(), approvalID, "files", "files.update")
	if status.Code(err) != codes.PermissionDenied || !errors.Is(err, repository.ErrUnattendedRepeatBlocked) {
		t.Fatalf("grant err = %v (code %v), want a PermissionDenied repeat refusal", err, status.Code(err))
	}
	if errors.Is(err, repository.ErrAutomationGrantRevoked) {
		t.Fatal("a repeat was reported as a revoked grant")
	}
	h.assertApprovalPendingUnsigned(t, approvalID)
}
