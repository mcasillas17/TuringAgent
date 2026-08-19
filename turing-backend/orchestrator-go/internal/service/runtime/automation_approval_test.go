package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	approvalsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/approvals"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// fireAutomation creates an automation with the given allowlist, claims it and
// puts its run into the running state — the state a tool beacon arrives in.
func (h *harness) fireAutomation(t *testing.T, name string, allowed []repository.AutomationTool) repository.AutomationFire {
	t.Helper()
	ctx := context.Background()
	automation, err := h.repo.CreateAutomation(ctx, repository.AutomationInput{
		Name: name, Prompt: "Write today's note.",
		Schedule: repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute},
		Enabled:  true, AllowedTools: allowed,
	})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}
	due, err := time.Parse(time.RFC3339Nano, automation.NextDueAt)
	if err != nil {
		t.Fatal(err)
	}
	fire, found, err := h.repo.ClaimDueAutomation(ctx, due, repository.AutomationRunDefaults{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
	})
	if err != nil || !found {
		t.Fatalf("claim due automation = found %v err %v", found, err)
	}
	if err := h.repo.MarkRunRunning(ctx, fire.RunID); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}
	return fire
}

func filesUpdateBeacon(t *testing.T, fire repository.AutomationFire, toolCallID string) *turingv1.ToolCallBeacon {
	t.Helper()
	args, err := structpb.NewStruct(map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	return &turingv1.ToolCallBeacon{
		RunId:      fire.RunID,
		TraceId:    fire.TraceID,
		ToolCallId: toolCallID,
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "files",
		ToolName:   "files.update",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
		Args:       args,
	}
}

// pendingApprovalFor produces a real pending approval the way the tool path
// does — the tool_calls row first, because approvals reference it.
func (h *harness) pendingApprovalFor(t *testing.T, runID string, sessionID string, traceID string, toolCallID string, toolName string) string {
	t.Helper()
	ctx := context.Background()
	args := map[string]any{"path": "note.txt", "content": "hello"}
	argsJSON, argsHash, err := canonicalArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.repo.RecordToolCallBeforeWithEvent(ctx,
		repository.ToolCallRecord{ToolCallID: toolCallID, RunID: runID},
		"general_assistant", "files", toolName, argsJSON, argsHash,
		repository.ToolCallBeforeEvent{SessionID: sessionID, TraceID: traceID, Type: "tool.call.started", PayloadJSON: "{}"},
	); err != nil {
		t.Fatalf("record tool call: %v", err)
	}
	approvalID, err := h.approvals.CreateApprovalForTool(ctx, runID, toolCallID, "general_assistant", toolName, args)
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}
	return approvalID
}

func (h *harness) auditRows(t *testing.T, runID string, action string) []struct {
	ActorType string
	ActorID   string
	Payload   string
} {
	t.Helper()
	rows, err := h.database.QueryContext(context.Background(), `
		SELECT actor_type, COALESCE(actor_id, ''), COALESCE(payload_json, '')
		FROM audit_logs
		WHERE correlation_id = ? AND action = ?
		ORDER BY rowid
	`, runID, action)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var found []struct {
		ActorType string
		ActorID   string
		Payload   string
	}
	for rows.Next() {
		var row struct {
			ActorType string
			ActorID   string
			Payload   string
		}
		if err := rows.Scan(&row.ActorType, &row.ActorID, &row.Payload); err != nil {
			t.Fatal(err)
		}
		found = append(found, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

// The pre-approval must be a real approval: the approval row reaches
// "approved", carrying a signed token bound to these arguments, without anyone
// clicking anything. That is what lets mcp-files consume it exactly once.
func TestAnAllowlistedToolIsApprovedWithoutAnyoneWatching(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.update"},
	})

	decision, err := h.service.handleToolBeacon(context.Background(), filesUpdateBeacon(t, fire, "call_allowed"))
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED || decision.GetApprovalId() == "" {
		t.Fatalf("decision = %+v, want approval required with an id", decision)
	}

	approval, err := h.repo.GetApproval(context.Background(), decision.GetApprovalId())
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "approved" {
		t.Fatalf("approval status = %q, want approved before anyone was asked", approval.Status)
	}
	if approval.ApprovalComment.Valid || approval.DenialReason.Valid {
		t.Fatalf("unattended approval has human rationale: %+v", approval)
	}
	// The token is the invariant that survives: single-use, and bound to these
	// exact arguments by args_hash inside it.
	if strings.Count(approval.ApprovalToken, ".") != 2 {
		t.Fatalf("approval token = %q, want a signed JWT", approval.ApprovalToken)
	}
	if approval.ArgsHash == "" || !strings.HasPrefix(approval.ArgsHash, "sha256:") {
		t.Fatalf("approval args hash = %q, want a hash of the call's arguments", approval.ArgsHash)
	}
	// And it is still the runtime's job to consume it — the pre-approval did
	// not skip a step, it answered one.
	if _, err := h.repo.ConsumeApproval(context.Background(), approval.ApprovalID, ""); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if _, err := h.repo.ConsumeApproval(context.Background(), approval.ApprovalID, ""); err == nil {
		t.Fatal("the pre-approved token was consumable twice")
	}
}

// An operator has to be able to ask "what did this thing do while I was
// asleep". The audit row is where that is answered, so it must name the
// automation and be distinguishable from a person's approval.
func TestAnUnattendedApprovalIsAuditedAsAnAutomationsAndNotAPersons(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.update"},
	})

	if _, err := h.service.handleToolBeacon(context.Background(), filesUpdateBeacon(t, fire, "call_allowed")); err != nil {
		t.Fatalf("tool beacon: %v", err)
	}

	rows := h.auditRows(t, fire.RunID, "approval.approved")
	if len(rows) != 1 {
		t.Fatalf("approval.approved audit rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.ActorType != "automation" {
		t.Fatalf("audit actor = %q, want automation (a person's approval records \"client\")", row.ActorType)
	}
	if row.ActorID != fire.AutomationID {
		t.Fatalf("audit actor id = %q, want the automation %q", row.ActorID, fire.AutomationID)
	}
	for _, want := range []string{`"unattended":true`, `"automationName":"Nightly note"`, `"toolName":"files.update"`} {
		if !strings.Contains(row.Payload, want) {
			t.Fatalf("audit payload %s does not contain %s", row.Payload, want)
		}
	}
	for _, forbidden := range []string{`"comment"`, `"reason"`} {
		if strings.Contains(row.Payload, forbidden) {
			t.Fatalf("unattended audit payload %s contains human rationale field %s", row.Payload, forbidden)
		}
	}
}

// The audit trail is readable through the redacted audit API, but that is a
// separate operator-grade query surface: a notice in the conversation is the
// only place a person reading it in context sees that an approval was not
// theirs.
func TestAnUnattendedApprovalIsAnnouncedInTheConversation(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.update"},
	})

	if _, err := h.service.handleToolBeacon(context.Background(), filesUpdateBeacon(t, fire, "call_allowed")); err != nil {
		t.Fatalf("tool beacon: %v", err)
	}

	replayed, _, err := h.repo.ReplayEvents(context.Background(), fire.SessionID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var notice string
	for _, event := range replayed {
		if event.Type == "agent.run.step" && strings.Contains(event.PayloadJSON, "unattended") {
			notice = event.PayloadJSON
		}
	}
	if notice == "" {
		t.Fatalf("no unattended-approval notice among %d events", len(replayed))
	}
	// The client renders only "note", so the sentence has to carry the whole
	// meaning on its own.
	for _, want := range []string{"Approved automatically", "files.update", "Nightly note", "nobody was asked"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("notice %s does not contain %q", notice, want)
		}
	}
}

// The failure this feature exists to avoid: a run waiting on someone who is
// asleep. A tool outside the allowlist must terminalise the run instead.
func TestAToolOutsideTheAllowlistFailsTheRunInsteadOfWaiting(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.create"},
	})

	decision, err := h.service.handleToolBeacon(context.Background(), filesUpdateBeacon(t, fire, "call_blocked"))
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || !decision.GetTerminalRun() {
		t.Fatalf("decision = %+v, want a terminal deny", decision)
	}
	if decision.GetReason() != AutomationNotAllowlistedCode {
		t.Fatalf("decision reason = %q, want %q", decision.GetReason(), AutomationNotAllowlistedCode)
	}

	// No approval was created, so nothing can sit pending waiting to expire.
	if _, err := h.repo.GetPendingApprovalForRun(context.Background(), fire.RunID); err == nil {
		t.Fatal("a pending approval was left waiting for a person who is not there")
	}

	run, err := h.repo.GetRun(context.Background(), fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
	var errorCode, errorMessage string
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT COALESCE(error_code, ''), COALESCE(error_message, '') FROM agent_runs WHERE id = ?`,
		fire.RunID).Scan(&errorCode, &errorMessage); err != nil {
		t.Fatal(err)
	}
	if errorCode != AutomationNotAllowlistedCode {
		t.Fatalf("run error code = %q, want %q", errorCode, AutomationNotAllowlistedCode)
	}
	// The message is what the user reads, so it has to name both the
	// automation and the tool it was stopped on.
	if !strings.Contains(errorMessage, "Nightly note") || !strings.Contains(errorMessage, "files.update") {
		t.Fatalf("run error message = %q, want it to name the automation and the tool", errorMessage)
	}
}

// The user must be able to see it happened, which means events in the
// conversation, not only a row in a table nothing reads.
func TestABlockedToolIsVisibleInTheConversation(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", nil)

	if _, err := h.service.handleToolBeacon(context.Background(), filesUpdateBeacon(t, fire, "call_blocked")); err != nil {
		t.Fatalf("tool beacon: %v", err)
	}

	replayed, _, err := h.repo.ReplayEvents(context.Background(), fire.SessionID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, event := range replayed {
		seen[event.Type] = event.PayloadJSON
	}
	if payload, ok := seen["tool.call.denied"]; !ok || !strings.Contains(payload, AutomationNotAllowlistedCode) {
		t.Fatalf("tool.call.denied = %q, want it to name the reason", payload)
	}
	if payload, ok := seen["agent.run.failed"]; !ok || !strings.Contains(payload, AutomationNotAllowlistedCode) {
		t.Fatalf("agent.run.failed = %q, want it to name the reason", payload)
	}

	rows := h.auditRows(t, fire.RunID, "automation.tool.blocked")
	if len(rows) != 1 || rows[0].ActorType != "automation" || rows[0].ActorID != fire.AutomationID {
		t.Fatalf("automation.tool.blocked audit = %+v, want one row naming the automation", rows)
	}
}

// The allowlist is scoped to one automation and must never leak into a
// conversation the user drives by hand — that conversation still stops and
// asks.
func TestAHandDrivenRunStillStopsAndAsks(t *testing.T) {
	h := newHarness(t)
	// An automation exists and allows the tool, which is exactly the state in
	// which a leak would be invisible.
	h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.update"},
	})
	enqueued := h.createRunningRunResult(t, "please update the note")

	args, err := structpb.NewStruct(map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := h.service.handleToolBeacon(context.Background(), &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: "call_by_hand",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "files",
		ToolName: "files.update", Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: args,
	})
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED {
		t.Fatalf("decision = %+v, want approval required", decision)
	}
	approval, err := h.repo.GetApproval(context.Background(), decision.GetApprovalId())
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "pending" {
		t.Fatalf("a hand-driven run's approval = %q, want pending until the user answers", approval.Status)
	}
	if approval.ApprovalToken != "" {
		t.Fatal("a hand-driven run was handed a token nobody approved")
	}
}

// A safe tool needs no approval, and an automation must not change that in
// either direction.
func TestAnAutomationRunsSafeToolsWithoutAnApproval(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", nil)

	args, err := structpb.NewStruct(map[string]any{"path": "."})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := h.service.handleToolBeacon(context.Background(), &turingv1.ToolCallBeacon{
		RunId: fire.RunID, TraceId: fire.TraceID, ToolCallId: "call_list",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "files",
		ToolName: "files.list", Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: args,
	})
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("decision = %+v, want allow", decision)
	}
	run, err := h.repo.GetRun(context.Background(), fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running", run.Status)
	}
}

// A tool the orchestrator disables outright stays disabled: an automation's
// allowlist grants pre-approval, it does not grant policy.
func TestAnAllowlistCannotEnableAPermanentlyDisabledTool(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.delete"},
	})

	args, err := structpb.NewStruct(map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := h.service.handleToolBeacon(context.Background(), &turingv1.ToolCallBeacon{
		RunId: fire.RunID, TraceId: fire.TraceID, ToolCallId: "call_delete",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "files",
		ToolName: "files.delete", Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: args,
	})
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || decision.GetReason() != "tool_disabled" {
		t.Fatalf("decision = %+v, want a plain tool_disabled denial", decision)
	}
}

// The last gate: GrantUnattendedApproval re-reads the allowlist from storage
// rather than trusting whatever its caller believes, so it cannot become a
// second, laxer way to approve something.
func TestGrantUnattendedApprovalRefusesToolsOutsideTheStoredAllowlist(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.update"},
	})
	// A pending approval for a tool the automation was NOT granted. It exists
	// only so there is something for the grant call to try to approve.
	approvalID := h.pendingApprovalFor(t, fire.RunID, fire.SessionID, fire.TraceID, "call_create", "files.create")

	err := h.approvals.GrantUnattendedApproval(context.Background(), approvalID, "files", "files.create")
	if err == nil {
		t.Fatal("granting a tool outside the stored allowlist succeeded")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("error = %v, want it to name the allowlist", err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "pending" || approval.ApprovalToken != "" {
		t.Fatalf("refused approval = %q with token %q, want it left pending and unsigned",
			approval.Status, approval.ApprovalToken)
	}
}

// A run nobody scheduled has no grant to find, so it cannot be approved
// through this door at all — however convinced the caller is.
func TestGrantUnattendedApprovalRefusesAHandDrivenRun(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "please update the note")
	approvalID := h.pendingApprovalFor(t, enqueued.RunID, enqueued.SessionID, enqueued.TraceID, "call_by_hand", "files.update")

	err := h.approvals.GrantUnattendedApproval(context.Background(), approvalID, "files", "files.update")
	if err == nil {
		t.Fatal("a hand-driven run was granted an unattended approval")
	}
	if !strings.Contains(err.Error(), "automation") {
		t.Fatalf("error = %v, want it to say the run was not an automation's", err)
	}
}

// The approval and the tool being allowed have to be the same tool, or the
// allowlist check would be checking something other than what gets signed.
func TestGrantUnattendedApprovalRefusesAMismatchedApproval(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.create"},
		{ServerName: "files", ToolName: "files.update"},
	})
	approvalID := h.pendingApprovalFor(t, fire.RunID, fire.SessionID, fire.TraceID, "call_create", "files.create")

	// Both tools are allowlisted, so only the approval/tool mismatch can stop
	// this.
	err := h.approvals.GrantUnattendedApproval(context.Background(), approvalID, "files", "files.update")
	if err == nil {
		t.Fatal("an approval for a different tool was granted")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want a mismatch", err)
	}
}

// Granting the same approval twice is what a retried beacon looks like. It
// must be a no-op rather than a second signature.
func TestGrantingAnAlreadyApprovedApprovalIsANoOp(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.update"},
	})
	decision, err := h.service.handleToolBeacon(context.Background(), filesUpdateBeacon(t, fire, "call_allowed"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := h.repo.GetApproval(context.Background(), decision.GetApprovalId())
	if err != nil {
		t.Fatal(err)
	}

	if err := h.approvals.GrantUnattendedApproval(context.Background(), decision.GetApprovalId(), "files", "files.update"); err != nil {
		t.Fatalf("re-granting: %v", err)
	}
	after, err := h.repo.GetApproval(context.Background(), decision.GetApprovalId())
	if err != nil {
		t.Fatal(err)
	}
	if after.ApprovalToken != before.ApprovalToken {
		t.Fatal("re-granting minted a second token for the same approval")
	}
}

// The audit row is the only record that nobody was awake for this. If it
// cannot be written, the grant does not happen — over-recording is the safe
// direction, silently unrecorded consent is not.
func TestAnUnrecordableGrantIsNotGranted(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.update"},
	})
	approvalID := h.pendingApprovalFor(t, fire.RunID, fire.SessionID, fire.TraceID, "call_allowed", "files.update")
	// Removing the audit table is the cheapest way to make recording fail
	// while leaving everything the approval itself needs intact.
	if _, err := h.database.ExecContext(context.Background(), `DROP TABLE audit_logs`); err != nil {
		t.Fatal(err)
	}

	if err := h.approvals.GrantUnattendedApproval(context.Background(), approvalID, "files", "files.update"); err == nil {
		t.Fatal("an unrecordable grant succeeded")
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "pending" || approval.ApprovalToken != "" {
		t.Fatalf("approval = %q with token %q, want it left pending and unsigned",
			approval.Status, approval.ApprovalToken)
	}
}

func TestUnattendedGrantWithoutRunIsNotGranted(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.update"},
	})
	approvalID := h.pendingApprovalFor(t, fire.RunID, fire.SessionID, fire.TraceID, "call_allowed", "files.update")
	if _, err := h.database.ExecContext(context.Background(), `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(
		context.Background(),
		`DELETE FROM agent_runs WHERE id = ?`,
		fire.RunID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}

	err := h.approvals.GrantUnattendedApproval(context.Background(), approvalID, "files", "files.update")
	if status.Code(err) != codes.Internal {
		t.Fatalf("GrantUnattendedApproval error = %v, want Internal", err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "pending" || approval.ApprovalToken != "" {
		t.Fatalf(
			"approval = %q with token %q, want it left pending and unsigned",
			approval.Status,
			approval.ApprovalToken,
		)
	}
}

// An approval service that cannot grant unattended approvals is a possible
// state. It must fail the run rather than leave it waiting for a person who
// is not there.
func TestAnApprovalServiceThatCannotGrantUnattendedFailsTheRun(t *testing.T) {
	h := newHarness(t)
	// A creator with no GrantUnattendedApproval method at all.
	h.service.approvals = plainApprovalCreator{inner: h.approvals}
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{
		{ServerName: "files", ToolName: "files.update"},
	})

	decision, err := h.service.handleToolBeacon(context.Background(), filesUpdateBeacon(t, fire, "call_allowed"))
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || !decision.GetTerminalRun() {
		t.Fatalf("decision = %+v, want a terminal deny rather than a wait", decision)
	}
	if decision.GetReason() != "automation_approval_failed" {
		t.Fatalf("reason = %q, want automation_approval_failed", decision.GetReason())
	}
	var runStatus, errorCode string
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT status, COALESCE(error_code, '') FROM agent_runs WHERE id = ?`, fire.RunID).Scan(&runStatus, &errorCode); err != nil {
		t.Fatal(err)
	}
	if runStatus != "failed" || errorCode != "automation_approval_failed" {
		t.Fatalf("run = %q / %q, want failed / automation_approval_failed", runStatus, errorCode)
	}
}

// plainApprovalCreator satisfies approvalCreator and nothing else, so the
// optional unattendedApprover assertion fails.
type plainApprovalCreator struct {
	inner *approvalsvc.Server
}

func (c plainApprovalCreator) CreateApprovalForTool(ctx context.Context, runID string, toolCallID string, agentID string, toolName string, args map[string]any) (string, error) {
	return c.inner.CreateApprovalForTool(ctx, runID, toolCallID, agentID, toolName, args)
}
