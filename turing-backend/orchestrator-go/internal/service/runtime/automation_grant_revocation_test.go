package runtime

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/testsupport/approvalfixture"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// TUR-026a: an automation run's consent is re-read against the automation as
// it stands at every tool call, and again in the transaction that commits an
// unattended approval. It may narrow or end; it never widens.

var (
	grantUpdate = repository.AutomationTool{ServerName: "files", ToolName: "files.update"}
	grantCreate = repository.AutomationTool{ServerName: "files", ToolName: "files.create"}
	grantRead   = repository.AutomationTool{ServerName: "files", ToolName: "files.read"}
)

// automationRevocation is one way the owner withdraws consent. Narrowing keeps
// files.create so the automation is still enabled and still has an allowlist:
// only the pairs the run is about to use leave it.
//
// revoke returns its error rather than failing the test itself, because the
// barrier test calls it from the preview fixture's handler goroutine, where
// t.Fatal is not allowed.
type automationRevocation struct {
	name   string
	revoke func(h *harness, automation repository.Automation) error
}

func (r automationRevocation) apply(t *testing.T, h *harness, automation repository.Automation) {
	t.Helper()
	if err := r.revoke(h, automation); err != nil {
		t.Fatalf("%s: %v", r.name, err)
	}
}

func automationRevocations() []automationRevocation {
	return []automationRevocation{
		{name: "deleted", revoke: func(h *harness, automation repository.Automation) error {
			return h.repo.DeleteAutomation(context.Background(), automation.AutomationID)
		}},
		{name: "disabled", revoke: func(h *harness, automation repository.Automation) error {
			_, err := h.repo.SetAutomationEnabled(context.Background(), automation.AutomationID, false)
			return err
		}},
		{name: "narrowed", revoke: func(h *harness, automation repository.Automation) error {
			return h.updateAutomationAllowlist(automation, automation.Name, grantCreate)
		}},
	}
}

func (h *harness) createAutomation(t *testing.T, name string, allowed ...repository.AutomationTool) repository.Automation {
	t.Helper()
	automation, err := h.repo.CreateAutomation(context.Background(), repository.AutomationInput{
		Name: name, Prompt: "Write today's note.",
		Schedule: repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute},
		Enabled:  true, AllowedTools: allowed,
	})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}
	return automation
}

// claimAutomation fires the automation, leaving its run queued. The model is
// one the harness's legacy worker profile serves, so a connected worker is
// assigned it.
func (h *harness) claimAutomation(t *testing.T, automation repository.Automation) repository.AutomationFire {
	t.Helper()
	due, err := time.Parse(time.RFC3339Nano, automation.NextDueAt)
	if err != nil {
		t.Fatal(err)
	}
	fire, found, err := h.repo.ClaimDueAutomation(context.Background(), due, repository.AutomationRunDefaults{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil || !found {
		t.Fatalf("claim due automation = found %v err %v", found, err)
	}
	return fire
}

func (h *harness) updateAutomationAllowlist(automation repository.Automation, name string, allowed ...repository.AutomationTool) error {
	_, err := h.repo.UpdateAutomation(context.Background(), automation.AutomationID, repository.AutomationInput{
		Name: name, Prompt: automation.Prompt, Schedule: automation.Schedule, AllowedTools: allowed,
	})
	return err
}

func (h *harness) saveAutomationAllowlist(t *testing.T, automation repository.Automation, name string, allowed ...repository.AutomationTool) {
	t.Helper()
	if err := h.updateAutomationAllowlist(automation, name, allowed...); err != nil {
		t.Fatalf("update automation: %v", err)
	}
}

func automationToolBeacon(fire repository.AutomationFire, toolCallID string, tool repository.AutomationTool, args map[string]any) *turingv1.ToolCallBeacon {
	beacon := &turingv1.ToolCallBeacon{
		RunId: fire.RunID, TraceId: fire.TraceID, ToolCallId: toolCallID,
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: tool.ServerName, ToolName: tool.ToolName,
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	}
	if args != nil {
		structArgs, err := structpb.NewStruct(args)
		if err != nil {
			panic(err)
		}
		beacon.Args = structArgs
	}
	return beacon
}

func toolArgs(tool repository.AutomationTool) map[string]any {
	if tool == grantRead {
		return map[string]any{"path": "note.txt"}
	}
	return map[string]any{"path": "note.txt", "content": "hello"}
}

// sendBeaconOnStream sends a beacon the way a real worker does and returns the
// decision the orchestrator answered it with.
func sendBeaconOnStream(t *testing.T, stream turingv1.RuntimeService_ConnectWorkerClient, beacon *turingv1.ToolCallBeacon) *turingv1.ToolPolicyDecision {
	t.Helper()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: beacon}}); err != nil {
		t.Fatalf("send tool beacon: %v", err)
	}
	return recvUntil(t, stream, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetToolPolicyDecision() != nil && cmd.GetToolPolicyDecision().GetToolCallId() == beacon.GetToolCallId()
	}).GetToolPolicyDecision()
}

// assertRunFailedFor checks the run ended the way an automation policy refusal
// ends it, with one blocked-tool audit row naming the code.
func (h *harness) assertRunFailedFor(t *testing.T, runID string, code string) {
	t.Helper()
	var runStatus, errorCode string
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT status, COALESCE(error_code, '') FROM agent_runs WHERE id = ?`, runID).Scan(&runStatus, &errorCode); err != nil {
		t.Fatal(err)
	}
	if runStatus != "failed" || errorCode != code {
		t.Fatalf("run = %s / %q, want failed / %q", runStatus, errorCode, code)
	}
	state, err := h.repo.GetRunState(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if state.OutcomeReason != "policy_denied" {
		t.Fatalf("outcome reason = %q, want policy_denied", state.OutcomeReason)
	}
	blocked := h.auditRows(t, runID, "automation.tool.blocked")
	if len(blocked) != 1 {
		t.Fatalf("automation.tool.blocked audit rows = %d, want 1", len(blocked))
	}
	if !strings.Contains(blocked[0].Payload, `"code":"`+code+`"`) {
		t.Fatalf("blocked audit payload %s does not name %q", blocked[0].Payload, code)
	}
	if blocked[0].ActorType != "automation" {
		t.Fatalf("blocked audit actor = %q, want automation", blocked[0].ActorType)
	}
}

func (h *harness) automationRunRecords(t *testing.T, runID string) int {
	t.Helper()
	var count int
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM automation_runs WHERE run_id = ?`, runID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func (h *harness) toolCallStatus(t *testing.T, toolCallID string) string {
	t.Helper()
	var value string
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT status FROM tool_calls WHERE id = ?`, toolCallID).Scan(&value); err != nil {
		t.Fatalf("read tool call %s: %v", toolCallID, err)
	}
	return value
}

// The acceptance matrix: delete, disable or narrow while the run is queued,
// running or recovering, and the next tool call — a safe read as well as an
// allowlisted mutation — is refused with automation_grant_revoked. The run
// fails with an audit row, nothing executes, and the run's automation_runs
// record stays.
func TestARevokedAutomationGrantBlocksTheRunsNextToolCall(t *testing.T) {
	lifecycles := []struct {
		name string
		// drive takes a freshly fired (queued) run to the lifecycle under
		// test, revoking at the point that lifecycle names, and returns the
		// owning worker's stream.
		drive func(t *testing.T, h *harness, fire repository.AutomationFire, revoke func()) turingv1.RuntimeService_ConnectWorkerClient
	}{
		{name: "queued", drive: func(t *testing.T, h *harness, fire repository.AutomationFire, revoke func()) turingv1.RuntimeService_ConnectWorkerClient {
			revoke()
			stream, _ := h.connectAssignedWorker(t, "worker-revoked-queued", fire.RunID)
			return stream
		}},
		{name: "running", drive: func(t *testing.T, h *harness, fire repository.AutomationFire, revoke func()) turingv1.RuntimeService_ConnectWorkerClient {
			stream, _ := h.connectAssignedWorker(t, "worker-revoked-running", fire.RunID)
			if state := h.runState(t, fire.RunID); state.Lifecycle != "running" {
				t.Fatalf("assigned run lifecycle = %q, want running", state.Lifecycle)
			}
			revoke()
			return stream
		}},
		{name: "recovering", drive: func(t *testing.T, h *harness, fire repository.AutomationFire, revoke func()) turingv1.RuntimeService_ConnectWorkerClient {
			stream, assigned := h.connectAssignedWorker(t, "worker-revoked-recovering", fire.RunID)
			h.fenceOwnership(t, fire.RunID, "worker-revoked-recovering", assigned.GetAssignmentAttemptId())
			revoke()
			return stream
		}},
	}
	for _, revocation := range automationRevocations() {
		for _, lifecycle := range lifecycles {
			for _, tool := range []repository.AutomationTool{grantRead, grantUpdate} {
				t.Run(revocation.name+"/"+lifecycle.name+"/"+tool.ToolName, func(t *testing.T) {
					h := newHarness(t)
					automation := h.createAutomation(t, "Nightly note", grantRead, grantUpdate, grantCreate)
					fire := h.claimAutomation(t, automation)

					stream := lifecycle.drive(t, h, fire, func() { revocation.apply(t, h, automation) })
					decision := sendBeaconOnStream(t, stream, automationToolBeacon(fire, "call_after_revocation", tool, toolArgs(tool)))

					if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || !decision.GetTerminalRun() {
						t.Fatalf("decision = %+v, want a terminal deny", decision)
					}
					if decision.GetReason() != AutomationGrantRevokedCode {
						t.Fatalf("decision reason = %q, want %q", decision.GetReason(), AutomationGrantRevokedCode)
					}
					h.assertRunFailedFor(t, fire.RunID, AutomationGrantRevokedCode)
					// Nothing executes: the read was denied rather than
					// allowed, and the mutation never got as far as an
					// approval, so there is no token to spend.
					if got := h.toolCallStatus(t, "call_after_revocation"); got == "allowed" || got == "approval_required" {
						t.Fatalf("tool call status = %q, want it refused", got)
					}
					if _, err := h.repo.GetApprovalByToolCall(context.Background(), fire.RunID, "call_after_revocation"); !errors.Is(err, sql.ErrNoRows) {
						t.Fatalf("approval lookup err = %v, want none created", err)
					}
					if got := h.automationRunRecords(t, fire.RunID); got != 1 {
						t.Fatalf("automation_runs rows = %d, want the run's record kept", got)
					}
				})
			}
		}
	}
}

// Deleting or disabling the automation ends the run's authority over every
// tool, not only the ones its allowlist named: a safe read it never listed is
// refused too.
func TestARevokedAutomationCannotReadWithAnUnlistedSafeTool(t *testing.T) {
	for _, revocation := range automationRevocations()[:2] {
		t.Run(revocation.name, func(t *testing.T) {
			h := newHarness(t)
			automation := h.createAutomation(t, "Nightly note", grantUpdate)
			fire := h.claimAutomation(t, automation)
			if err := h.repo.MarkRunRunning(context.Background(), fire.RunID); err != nil {
				t.Fatal(err)
			}
			revocation.apply(t, h, automation)

			list := repository.AutomationTool{ServerName: "files", ToolName: "files.list"}
			decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_list", list, map[string]any{"path": "."}))
			if err != nil {
				t.Fatalf("tool beacon: %v", err)
			}
			if !decision.GetTerminalRun() || decision.GetReason() != AutomationGrantRevokedCode {
				t.Fatalf("decision = %+v, want a terminal %s deny", decision, AutomationGrantRevokedCode)
			}
			h.assertRunFailedFor(t, fire.RunID, AutomationGrantRevokedCode)
		})
	}
}

// The barrier: the beacon's check passes, the approval is created and its
// preview refreshed, and only then — before the token is committed — the
// owner revokes. The commit transaction re-reads the automation, so the
// approval is never approved, no token is issued, no audit row claims it was,
// and the run fails as revoked.
func TestARevocationAfterThePreviewRefreshLeavesTheApprovalUnapproved(t *testing.T) {
	for _, revocation := range automationRevocations() {
		t.Run(revocation.name, func(t *testing.T) {
			h := newHarness(t)
			automation := h.createAutomation(t, "Nightly note", grantUpdate, grantCreate)
			fire := h.claimAutomation(t, automation)
			if err := h.repo.MarkRunRunning(context.Background(), fire.RunID); err != nil {
				t.Fatal(err)
			}
			var barrier sync.Once
			var previewCalls atomic.Int32
			var barrierErr error
			approvalfixture.EndpointWithHook(t, h.approvals, func(approvalpreview.Request) {
				previewCalls.Add(1)
				barrier.Do(func() {
					// The approval exists and is still pending: the
					// beacon's own check passed and the grant is underway.
					approval, err := h.repo.GetApprovalByToolCall(context.Background(), fire.RunID, "call_barrier")
					if err != nil || approval.Status != "pending" {
						barrierErr = errors.Join(err, errors.New("approval at the barrier is "+approval.Status+", want pending"))
						return
					}
					barrierErr = revocation.revoke(h, automation)
				})
			})

			decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_barrier", grantUpdate, toolArgs(grantUpdate)))
			if err != nil {
				t.Fatalf("tool beacon: %v", err)
			}
			if previewCalls.Load() == 0 {
				t.Fatal("the preview was never refreshed, so the barrier never ran")
			}
			if barrierErr != nil {
				t.Fatalf("barrier: %v", barrierErr)
			}
			if !decision.GetTerminalRun() || decision.GetReason() != AutomationGrantRevokedCode {
				t.Fatalf("decision = %+v, want a terminal %s deny", decision, AutomationGrantRevokedCode)
			}
			var approvalStatus string
			var token, jti sql.NullString
			if err := h.database.QueryRowContext(context.Background(),
				`SELECT status, approval_token, approval_jti FROM approvals WHERE run_id = ? AND tool_call_id = ?`,
				fire.RunID, "call_barrier").Scan(&approvalStatus, &token, &jti); err != nil {
				t.Fatalf("read approval: %v", err)
			}
			// The run's failure expired the pending approval; it was never
			// approved, and no token was ever stored for it.
			if approvalStatus != "expired" || token.Valid || jti.Valid {
				t.Fatalf("approval = %s token %v jti %v, want expired with no token", approvalStatus, token, jti)
			}
			if rows := h.auditRows(t, fire.RunID, "approval.approved"); len(rows) != 0 {
				t.Fatalf("approval.approved audit rows = %d, want none for an approval that never happened", len(rows))
			}
			h.assertRunFailedFor(t, fire.RunID, AutomationGrantRevokedCode)
			replayed, _, err := h.repo.ReplayEvents(context.Background(), fire.SessionID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			var terminalToolEvents []string
			for _, event := range replayed {
				if event.Type == "approval.approved" || strings.Contains(event.PayloadJSON, "Approved automatically") {
					t.Fatalf("conversation announces an approval that never happened: %s %s", event.Type, event.PayloadJSON)
				}
				if strings.Contains(event.PayloadJSON, `"toolCallId":"call_barrier"`) &&
					(event.Type == "tool.call.denied" || event.Type == "tool.call.failed" || event.Type == "tool.call.completed") {
					terminalToolEvents = append(terminalToolEvents, event.Type)
				}
			}
			// The call was recorded as needing approval, and the run's
			// failure fails it. It was never denied, so the stream must not
			// say it was.
			if len(terminalToolEvents) != 1 || terminalToolEvents[0] != "tool.call.failed" {
				t.Fatalf("terminal events for the call = %v, want exactly one tool.call.failed", terminalToolEvents)
			}
			if got := h.toolCallStatus(t, "call_barrier"); got != "failed" {
				t.Fatalf("tool call status = %q, want failed", got)
			}
		})
	}
}

// Committing an unattended approval that is already approved changes nothing
// and records nothing: the grant that approved it wrote the one audit row.
func TestCommittingAnAlreadyApprovedUnattendedApprovalIsANoOp(t *testing.T) {
	h := newHarness(t)
	automation := h.createAutomation(t, "Nightly note", grantUpdate)
	fire := h.claimAutomation(t, automation)
	if err := h.repo.MarkRunRunning(context.Background(), fire.RunID); err != nil {
		t.Fatal(err)
	}
	decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_once", grantUpdate, toolArgs(grantUpdate)))
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	before, err := h.repo.GetApproval(context.Background(), decision.GetApprovalId())
	if err != nil || before.Status != "approved" {
		t.Fatalf("approval = %+v err %v, want approved", before, err)
	}
	details, err := h.approvals.GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: before.ApprovalID})
	if err != nil {
		t.Fatal(err)
	}

	again, err := h.repo.ApproveUnattendedApprovalWithEvent(context.Background(), before.ApprovalID, "a-second-token", details.GetPreviewHash())
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if again.Changed || again.ApprovalEvent.EventID != "" {
		t.Fatalf("second commit = %+v, want nothing changed", again)
	}
	after, err := h.repo.GetApproval(context.Background(), before.ApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ApprovalToken != before.ApprovalToken {
		t.Fatal("a second commit replaced the approval's token")
	}
	if rows := h.auditRows(t, fire.RunID, "approval.approved"); len(rows) != 1 {
		t.Fatalf("approval.approved audit rows = %d, want the one the first commit wrote", len(rows))
	}
}

// An automation the owner has not touched in any way that matters still
// pre-approves: renaming it, or narrowing away a different tool, leaves the
// pair the run is using where it was.
func TestAnAutomationLeftIntactStillPreApproves(t *testing.T) {
	h := newHarness(t)
	automation := h.createAutomation(t, "Nightly note", grantUpdate, grantCreate)
	fire := h.claimAutomation(t, automation)
	if err := h.repo.MarkRunRunning(context.Background(), fire.RunID); err != nil {
		t.Fatal(err)
	}
	h.saveAutomationAllowlist(t, automation, "Renamed note", grantUpdate)

	decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_kept", grantUpdate, toolArgs(grantUpdate)))
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
	if approval.Status != "approved" || strings.Count(approval.ApprovalToken, ".") != 2 {
		t.Fatalf("approval = %q token %q, want approved with a signed token", approval.Status, approval.ApprovalToken)
	}
	// The audit row is written in the commit's own transaction now, and
	// still names the automation as it fired.
	rows := h.auditRows(t, fire.RunID, "approval.approved")
	if len(rows) != 1 || rows[0].ActorType != "automation" || rows[0].ActorID != fire.AutomationID ||
		!strings.Contains(rows[0].Payload, `"automationName":"Nightly note"`) {
		t.Fatalf("approval.approved audit rows = %+v, want one automation row naming the frozen grant", rows)
	}

	// The pair narrowed away is the one that is now refused.
	decision, err = h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_narrowed", grantCreate, toolArgs(grantCreate)))
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	if !decision.GetTerminalRun() || decision.GetReason() != AutomationGrantRevokedCode {
		t.Fatalf("decision = %+v, want a terminal %s deny", decision, AutomationGrantRevokedCode)
	}
}

// Widening the allowlist mid-run widens nothing: the frozen grant still bounds
// the run, at the beacon and at the grant itself, and the refusal is the
// ordinary not-allowlisted one, not a revocation.
func TestWideningAnAutomationMidRunWidensNothing(t *testing.T) {
	t.Run("beacon", func(t *testing.T) {
		h := newHarness(t)
		automation := h.createAutomation(t, "Nightly note", grantCreate)
		fire := h.claimAutomation(t, automation)
		if err := h.repo.MarkRunRunning(context.Background(), fire.RunID); err != nil {
			t.Fatal(err)
		}
		h.saveAutomationAllowlist(t, automation, automation.Name, grantCreate, grantUpdate)

		decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_widened", grantUpdate, toolArgs(grantUpdate)))
		if err != nil {
			t.Fatalf("tool beacon: %v", err)
		}
		if !decision.GetTerminalRun() || decision.GetReason() != AutomationNotAllowlistedCode {
			t.Fatalf("decision = %+v, want a terminal %s deny", decision, AutomationNotAllowlistedCode)
		}
		h.assertRunFailedFor(t, fire.RunID, AutomationNotAllowlistedCode)
	})
	t.Run("grant", func(t *testing.T) {
		h := newHarness(t)
		automation := h.createAutomation(t, "Nightly note", grantCreate)
		fire := h.claimAutomation(t, automation)
		if err := h.repo.MarkRunRunning(context.Background(), fire.RunID); err != nil {
			t.Fatal(err)
		}
		approvalID := h.pendingApprovalFor(t, fire.RunID, fire.SessionID, fire.TraceID, "call_widened", "files.update")
		h.saveAutomationAllowlist(t, automation, automation.Name, grantCreate, grantUpdate)

		err := h.approvals.GrantUnattendedApproval(context.Background(), approvalID, "files", "files.update")
		if status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "allowlist") {
			t.Fatalf("grant err = %v, want a PermissionDenied naming the allowlist", err)
		}
		if errors.Is(err, repository.ErrAutomationGrantRevoked) {
			t.Fatal("a tool the run was never granted was reported as revoked")
		}
		h.assertApprovalPendingUnsigned(t, approvalID)
	})
}

// A grant the owner has already withdrawn is refused up front, before any
// preview I/O, with an error the runtime can tell apart from a grant failure.
func TestGrantUnattendedApprovalRefusesARevokedGrantBeforeAnyPreview(t *testing.T) {
	for _, revocation := range automationRevocations() {
		t.Run(revocation.name, func(t *testing.T) {
			h := newHarness(t)
			automation := h.createAutomation(t, "Nightly note", grantUpdate, grantCreate)
			fire := h.claimAutomation(t, automation)
			if err := h.repo.MarkRunRunning(context.Background(), fire.RunID); err != nil {
				t.Fatal(err)
			}
			approvalID := h.pendingApprovalFor(t, fire.RunID, fire.SessionID, fire.TraceID, "call_revoked", "files.update")
			var previewCalls atomic.Int32
			approvalfixture.EndpointWithHook(t, h.approvals, func(approvalpreview.Request) { previewCalls.Add(1) })
			revocation.apply(t, h, automation)

			err := h.approvals.GrantUnattendedApproval(context.Background(), approvalID, "files", "files.update")
			if !errors.Is(err, repository.ErrAutomationGrantRevoked) || status.Code(err) != codes.PermissionDenied {
				t.Fatalf("grant err = %v (code %v), want a PermissionDenied revocation", err, status.Code(err))
			}
			if previewCalls.Load() != 0 {
				t.Fatalf("preview calls = %d, want none for a grant already withdrawn", previewCalls.Load())
			}
			h.assertApprovalPendingUnsigned(t, approvalID)
			if rows := h.auditRows(t, fire.RunID, "approval.approved"); len(rows) != 0 {
				t.Fatalf("approval.approved audit rows = %d, want none", len(rows))
			}
		})
	}
}

// The automation's approval.approved row is written in the commit's own
// transaction. When it cannot be written there — the insert fails, or writes
// nothing — the whole decision rolls back: an unrecordable grant is still not
// granted.
func TestAnUnattendedApprovalWhoseAuditRowCannotBeWrittenIsRolledBack(t *testing.T) {
	for _, test := range []struct {
		name    string
		trigger string
	}{
		{name: "insert fails", trigger: `SELECT RAISE(ABORT, 'audit unavailable')`},
		{name: "insert writes nothing", trigger: `SELECT RAISE(IGNORE)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			automation := h.createAutomation(t, "Nightly note", grantUpdate)
			fire := h.claimAutomation(t, automation)
			if err := h.repo.MarkRunRunning(context.Background(), fire.RunID); err != nil {
				t.Fatal(err)
			}
			approvalID := h.pendingApprovalFor(t, fire.RunID, fire.SessionID, fire.TraceID, "call_unrecorded", "files.update")
			if _, err := h.database.ExecContext(context.Background(), `
				CREATE TRIGGER refuse_unattended_approval_audit
				BEFORE INSERT ON audit_logs
				WHEN NEW.action = 'approval.approved'
				BEGIN `+test.trigger+`; END`); err != nil {
				t.Fatal(err)
			}

			err := h.approvals.GrantUnattendedApproval(context.Background(), approvalID, "files", "files.update")
			if status.Code(err) != codes.Internal || !strings.Contains(err.Error(), "could not be recorded") {
				t.Fatalf("grant err = %v, want Internal: could not be recorded", err)
			}
			h.assertApprovalPendingUnsigned(t, approvalID)
			if rows := h.auditRows(t, fire.RunID, "approval.approved"); len(rows) != 0 {
				t.Fatalf("approval.approved audit rows = %d, want none", len(rows))
			}
		})
	}
}

// Two kinds of tool keep today's behaviour on a revoked automation: the skill
// loaders, which read only the job's pinned owner-granted skills, and memory
// and team tools, which their own services refuse for any automation run
// without failing it. Neither fails the run here; the next ordinary call does.
func TestRevocationLeavesSkillLoadersAndMemoryAndTeamToolsAsTheyAre(t *testing.T) {
	h := newHarness(t)
	automation := h.createAutomation(t, "Nightly note", grantRead)
	fire := h.claimAutomation(t, automation)
	if err := h.repo.MarkRunRunning(context.Background(), fire.RunID); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.DeleteAutomation(context.Background(), automation.AutomationID); err != nil {
		t.Fatal(err)
	}

	for i, tool := range []repository.AutomationTool{
		{ServerName: "skills", ToolName: "skills_list"},
		{ServerName: "skills", ToolName: "skill_view"},
		{ServerName: "memory", ToolName: "memory.search"},
		{ServerName: "team", ToolName: "team.delegate"},
	} {
		decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_exempt_"+string(rune('a'+i)), tool, map[string]any{}))
		if err != nil {
			t.Fatalf("%s beacon: %v", tool.ToolName, err)
		}
		if decision.GetTerminalRun() || decision.GetReason() == AutomationGrantRevokedCode {
			t.Fatalf("%s decision = %+v, want today's non-terminal decision", tool.ToolName, decision)
		}
		if state := h.runState(t, fire.RunID); state.Lifecycle != "running" {
			t.Fatalf("after %s the run is %q, want running", tool.ToolName, state.Lifecycle)
		}
	}
	if rows := h.auditRows(t, fire.RunID, "automation.tool.blocked"); len(rows) != 0 {
		t.Fatalf("automation.tool.blocked audit rows = %d, want none for the exempt tools", len(rows))
	}

	decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_read", grantRead, toolArgs(grantRead)))
	if err != nil {
		t.Fatalf("read beacon: %v", err)
	}
	if !decision.GetTerminalRun() || decision.GetReason() != AutomationGrantRevokedCode {
		t.Fatalf("read decision = %+v, want a terminal %s deny", decision, AutomationGrantRevokedCode)
	}
}

// A run nobody scheduled has no automation to revoke: its safe reads are
// allowed and its mutations stop and ask, exactly as before.
func TestAHandDrivenRunIsUntouchedByAutomationRevocation(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "read the note")
	decision, err := h.service.handleToolBeacon(context.Background(), &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_hand_read",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "files", ToolName: "files.read",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: mustStruct(t, map[string]any{"path": "note.txt"}),
	})
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("decision = %+v, want allow", decision)
	}
}

func (h *harness) assertApprovalPendingUnsigned(t *testing.T, approvalID string) {
	t.Helper()
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "pending" || approval.ApprovalToken != "" {
		t.Fatalf("approval = %q with token %q, want it left pending and unsigned", approval.Status, approval.ApprovalToken)
	}
}
