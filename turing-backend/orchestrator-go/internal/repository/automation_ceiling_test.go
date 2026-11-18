package repository

import (
	"context"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// TUR-026d: an automation's frozen allowlist bounds every tool call its run
// makes, safe reads included. The skill loaders are exempt; memory and team
// tools are refused without failing the run; everything else off the list
// fails it.
func TestUnattendedGrantCheckBoundsEveryToolCall(t *testing.T) {
	listed := AutomationTool{ServerName: "files", ToolName: "files.update"}
	for _, test := range []struct {
		name    string
		disable bool
		pair    AutomationTool
		want    string
	}{
		{name: "a listed tool", pair: listed},
		{name: "an unlisted safe read", pair: AutomationTool{ServerName: "files", ToolName: "files.read"}, want: AutomationToolNotAllowlistedCode},
		{name: "an unlisted system tool", pair: AutomationTool{ServerName: "system", ToolName: "system.time"}, want: AutomationToolNotAllowlistedCode},
		{name: "an unlisted third-party tool", pair: AutomationTool{ServerName: "vendor", ToolName: "vendor.lookup"}, want: AutomationToolNotAllowlistedCode},
		{name: "a GitHub read", pair: AutomationTool{ServerName: "integrations", ToolName: "github.list_issues"}, want: AutomationToolNotAllowlistedCode},
		{name: "a GitHub write", pair: AutomationTool{ServerName: "integrations", ToolName: "github.create_comment"}, want: AutomationToolNotAllowlistedCode},
		{name: "a skill loader that is not exempt", pair: AutomationTool{ServerName: "skills", ToolName: "skill_install"}, want: AutomationToolNotAllowlistedCode},
		{name: "skills_list", pair: AutomationTool{ServerName: "skills", ToolName: "skills_list"}},
		{name: "skill_view", pair: AutomationTool{ServerName: "skills", ToolName: "skill_view"}},
		{name: "a memory tool", pair: AutomationTool{ServerName: "memory", ToolName: "memory.search"}, want: AutomationToolUnsupportedCode},
		{name: "a team tool", pair: AutomationTool{ServerName: "team", ToolName: "team.delegate"}, want: AutomationToolUnsupportedCode},
		{name: "a listed tool, disabled", disable: true, pair: listed, want: AutomationGrantRevokedCode},
		{name: "an unlisted tool, disabled", disable: true, pair: AutomationTool{ServerName: "files", ToolName: "files.read"}, want: AutomationGrantRevokedCode},
		{name: "a skill loader, disabled", disable: true, pair: AutomationTool{ServerName: "skills", ToolName: "skill_view"}},
		{name: "a memory tool, disabled", disable: true, pair: AutomationTool{ServerName: "memory", ToolName: "memory.search"}, want: AutomationToolUnsupportedCode},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, ctx := newTitleTestRepo(t)
			automation, err := repo.CreateAutomation(ctx, AutomationInput{
				Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true,
				AllowedTools: []AutomationTool{listed},
			})
			if err != nil {
				t.Fatal(err)
			}
			fire, _, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), automationDefaults)
			if err != nil {
				t.Fatal(err)
			}
			if test.disable {
				if _, err := repo.SetAutomationEnabled(ctx, automation.AutomationID, false); err != nil {
					t.Fatal(err)
				}
			}

			call, err := repo.CheckUnattendedCall(ctx, fire.RunID, test.pair.ServerName, test.pair.ToolName)
			if err != nil {
				t.Fatal(err)
			}
			if !call.Unattended {
				t.Fatal("an automation's run was not reported unattended")
			}
			if call.Refusal != test.want {
				t.Fatalf("refusal = %q, want %q", call.Refusal, test.want)
			}
			if call.Grant.AutomationID != automation.AutomationID || call.Grant.AutomationName != "Digest" {
				t.Fatalf("grant = %+v, want the automation as it fired", call.Grant)
			}
		})
	}
}

// A run the user typed is not checked at all, whatever it calls.
func TestUnattendedGrantCheckLeavesAHandDrivenRunAlone(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	runID := handDrivenRun(t, repo, ctx)
	for _, pair := range []AutomationTool{
		{ServerName: "files", ToolName: "files.read"},
		{ServerName: "memory", ToolName: "memory.search"},
		{ServerName: "vendor", ToolName: "vendor.lookup"},
	} {
		call, err := repo.CheckUnattendedCall(ctx, runID, pair.ServerName, pair.ToolName)
		if err != nil {
			t.Fatal(err)
		}
		if call.Unattended || call.Refusal != "" {
			t.Fatalf("%s: a hand-driven run was checked: %+v", pair.ToolName, call)
		}
	}
}

func handDrivenRun(t *testing.T, repo *Repository, ctx context.Context) string {
	t.Helper()
	session, err := repo.CreateSession(ctx, "By hand")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "read a file", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "qwen2.5:7b",
	})
	if err != nil {
		t.Fatal(err)
	}
	return enqueued.RunID
}

// runningAutomationRun fires an automation and delivers its run to a worker,
// so the run is running with an active execution, the way a refused tool
// call finds it.
func runningAutomationRun(t *testing.T, repo *Repository, ctx context.Context, allowed ...AutomationTool) (Automation, AutomationFire) {
	t.Helper()
	if len(allowed) == 0 {
		allowed = []AutomationTool{{ServerName: "files", ToolName: "files.update"}}
	}
	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true,
		AllowedTools: allowed,
	})
	if err != nil {
		t.Fatal(err)
	}
	fire, _, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), automationDefaults)
	if err != nil {
		t.Fatal(err)
	}
	job, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-1", 0, time.Hour, nil, nil)
	if err != nil || job.RunID != fire.RunID {
		t.Fatalf("claim = %+v, %v; want the automation's run", job, err)
	}
	assignment := Assignment{JobID: job.JobID, RunID: job.RunID, WorkerID: "worker-1", AttemptID: job.AssignmentAttemptID}
	if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	return automation, fire
}

func blockedAuditRows(t *testing.T, repo *Repository, ctx context.Context, runID string) []string {
	t.Helper()
	rows, err := repo.db.QueryContext(ctx, `
		SELECT actor_type || '|' || COALESCE(actor_id, '') || '|' || COALESCE(target, '') || '|' || COALESCE(payload_json, '')
		FROM audit_logs WHERE correlation_id = ? AND action = 'automation.tool.blocked' ORDER BY rowid`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var found []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		found = append(found, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

func notAllowlistedFailure() runoutcome.Failure {
	return runoutcome.NormalizeFailure(runoutcome.OriginAutomationPolicy, AutomationToolNotAllowlistedCode, runoutcome.RetryClassNever)
}

// One repository call fails the run and writes its automation.tool.blocked
// row, in one transaction, with the version resolved inside it: the run's
// execution stays active and is marked uncertain until the worker
// acknowledges, and the row names the server, tool, automation and code,
// never the arguments.
func TestFailUnattendedRunFailsTheRunAndWritesItsAuditRowTogether(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	automation, fire := runningAutomationRun(t, repo, ctx)
	block := &UnattendedToolBlock{
		AutomationID: automation.AutomationID, AutomationName: "Digest",
		ServerName: "vendor", ToolName: "vendor.lookup", ToolCallID: "call_1",
		Code: AutomationToolNotAllowlistedCode,
	}

	result, moved, err := repo.FailUnattendedRun(ctx, fire.RunID, notAllowlistedFailure(), block)
	if err != nil || !moved {
		t.Fatalf("FailUnattendedRun = moved %v, %v; want the run failed", moved, err)
	}
	var failed bool
	for _, event := range result.Events {
		failed = failed || event.Type == "agent.run.failed"
	}
	if !failed {
		t.Fatalf("events = %+v, want agent.run.failed to publish", result.Events)
	}
	run, err := repo.GetRun(ctx, fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var errorCode string
	if err := repo.db.QueryRowContext(ctx, `SELECT COALESCE(error_code, '') FROM agent_runs WHERE id = ?`, fire.RunID).Scan(&errorCode); err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || errorCode != AutomationToolNotAllowlistedCode {
		t.Fatalf("run = %s / %q, want failed / %s", run.Status, errorCode, AutomationToolNotAllowlistedCode)
	}
	if !run.ExecutionActive || run.ExecutionState != "uncertain" {
		t.Fatalf("execution = active %v, %q; want still active and uncertain until the worker acknowledges", run.ExecutionActive, run.ExecutionState)
	}
	state, err := repo.GetRunState(ctx, fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.OutcomeReason != string(runoutcome.ReasonPolicyDenied) {
		t.Fatalf("outcome = %q, want policy_denied", state.OutcomeReason)
	}
	rows := blockedAuditRows(t, repo, ctx, fire.RunID)
	want := `automation|` + automation.AutomationID + `|call_1|{"automationId":"` + automation.AutomationID +
		`","automationName":"Digest","code":"automation_tool_not_allowlisted","serverName":"vendor","toolName":"vendor.lookup"}`
	if len(rows) != 1 || rows[0] != want {
		t.Fatalf("automation.tool.blocked rows = %q, want exactly [%s]", rows, want)
	}

	// A second refusal on the failed run writes nothing and changes nothing.
	again, moved, err := repo.FailUnattendedRun(ctx, fire.RunID, notAllowlistedFailure(), block)
	if err != nil || moved || len(again.Events) != 0 {
		t.Fatalf("repeat = moved %v, events %d, %v; want a no-op", moved, len(again.Events), err)
	}
	if rows := blockedAuditRows(t, repo, ctx, fire.RunID); len(rows) != 1 {
		t.Fatalf("automation.tool.blocked rows after a repeat = %d, want 1", len(rows))
	}
	after, err := repo.GetRunState(ctx, fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after.StateVersion != state.StateVersion {
		t.Fatalf("state version = %d after a repeat, want %d", after.StateVersion, state.StateVersion)
	}
}

// A refusal that loses to another terminal transition, here the owner's
// cancel, writes no row: the run's own terminal record stands.
func TestFailUnattendedRunWritesNothingOnARunAlreadyEnded(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	automation, fire := runningAutomationRun(t, repo, ctx)
	state, err := repo.GetRunState(ctx, fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CancelRunCanonical(ctx, CancelRunInput{
		RunID: fire.RunID, ExpectedStateVersion: state.StateVersion, Cancellation: runoutcome.UserCancellation(),
	}); err != nil {
		t.Fatal(err)
	}
	result, moved, err := repo.FailUnattendedRun(ctx, fire.RunID, notAllowlistedFailure(), &UnattendedToolBlock{
		AutomationID: automation.AutomationID, AutomationName: "Digest", ServerName: "files", ToolName: "files.read",
		Code: AutomationToolNotAllowlistedCode,
	})
	if err != nil || moved || len(result.Events) != 0 {
		t.Fatalf("FailUnattendedRun on a cancelled run = moved %v, events %d, %v; want a no-op", moved, len(result.Events), err)
	}
	if rows := blockedAuditRows(t, repo, ctx, fire.RunID); len(rows) != 0 {
		t.Fatalf("automation.tool.blocked rows = %q, want none", rows)
	}
	run, err := repo.GetRun(ctx, fire.RunID)
	if err != nil || run.Status != "cancelled" {
		t.Fatalf("run = %+v, %v; want it still cancelled", run, err)
	}
}

// A failure that is not a blocked tool, such as automation_approval_failed,
// passes no audit entry and writes no automation.tool.blocked row.
func TestFailUnattendedRunWithoutAnEntryWritesNoRow(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	_, fire := runningAutomationRun(t, repo, ctx)
	_, moved, err := repo.FailUnattendedRun(ctx, fire.RunID, runoutcome.NormalizeFailure(
		runoutcome.OriginAutomationPolicy, "automation_approval_failed", runoutcome.RetryClassNever,
	), nil)
	if err != nil || !moved {
		t.Fatalf("FailUnattendedRun = moved %v, %v", moved, err)
	}
	if rows := blockedAuditRows(t, repo, ctx, fire.RunID); len(rows) != 0 {
		t.Fatalf("automation.tool.blocked rows = %q, want none", rows)
	}
}

// The failure and its row commit together or not at all: an audit insert
// that fails rolls the failure back with it.
func TestFailUnattendedRunRollsTheFailureBackWhenItsRowCannotBeWritten(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	automation, fire := runningAutomationRun(t, repo, ctx)
	before, err := repo.GetRunState(ctx, fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `
		CREATE TRIGGER refuse_blocked_audit BEFORE INSERT ON audit_logs
		WHEN NEW.action = 'automation.tool.blocked'
		BEGIN SELECT RAISE(ABORT, 'audit insert refused'); END`); err != nil {
		t.Fatal(err)
	}
	result, moved, err := repo.FailUnattendedRun(ctx, fire.RunID, notAllowlistedFailure(), &UnattendedToolBlock{
		AutomationID: automation.AutomationID, AutomationName: "Digest", ServerName: "files", ToolName: "files.read",
		Code: AutomationToolNotAllowlistedCode,
	})
	if err == nil || moved || len(result.Events) != 0 {
		t.Fatalf("FailUnattendedRun = moved %v, events %d, %v; want the forced audit failure", moved, len(result.Events), err)
	}
	run, err := repo.GetRun(ctx, fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := repo.GetRunState(ctx, fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" || after.StateVersion != before.StateVersion {
		t.Fatalf("run = %s at version %d, want still running at %d", run.Status, after.StateVersion, before.StateVersion)
	}
	var events int
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, fire.RunID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("agent.run.failed events stored = %d, want none", events)
	}
}

// At registered-MCP dispatch the grant is read again, in the read transaction
// that holds the dispatch fence, beside the liveness read: a revocation
// committed before the fence refuses the call there. A call the entry check
// found attended is not read again.
func TestMCPDispatchActiveRechecksAnUnattendedRunsGrantAtTheFence(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	server, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(ctx, server.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, server.Server.ID, []MCPServerTool{{
		Name: "vendor.lookup", Policy: "safe", SchemaJSON: `{"type":"object"}`,
	}}); err != nil {
		t.Fatal(err)
	}
	automation, fire := runningAutomationRun(t, repo, ctx, AutomationTool{ServerName: "vendor", ToolName: "vendor.lookup"})
	call := MCPDispatchCall{
		ServerID: server.Server.ID, ServerName: "vendor", RunID: fire.RunID,
		ToolName: "vendor.lookup", ExpectedPolicy: "safe", Unattended: true,
	}

	dispatch, err := repo.MCPDispatchActive(ctx, call)
	if err != nil {
		t.Fatal(err)
	}
	if !dispatch.Active || dispatch.Unattended.Refusal != "" {
		t.Fatalf("dispatch = %+v, want an allowlisted call active and unrefused", dispatch)
	}
	if dispatch.Unattended.Grant.AutomationID != automation.AutomationID {
		t.Fatalf("grant = %+v, want the run's automation", dispatch.Unattended.Grant)
	}

	if _, err := repo.SetAutomationEnabled(ctx, automation.AutomationID, false); err != nil {
		t.Fatal(err)
	}
	dispatch, err = repo.MCPDispatchActive(ctx, call)
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.Unattended.Refusal != AutomationGrantRevokedCode {
		t.Fatalf("refusal = %q, want %s once the automation is disabled", dispatch.Unattended.Refusal, AutomationGrantRevokedCode)
	}

	call.Unattended = false
	dispatch, err = repo.MCPDispatchActive(ctx, call)
	if err != nil {
		t.Fatal(err)
	}
	if !dispatch.Active || dispatch.Unattended.Refusal != "" {
		t.Fatalf("dispatch = %+v, want only the liveness read for a call found attended", dispatch)
	}
}

// The ceiling is enforced at the beacon and at dispatch, not by the job: a
// scheduled automation's job carries no selected set and no enforcement, so
// its routing, its skills and its prompt size are what they were. The
// payload is byte-for-byte the one the fire wrote before TUR-026d.
func TestAFiredAutomationsJobPayloadIsUnchanged(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Digest", Prompt: "Summarise the sandbox.", Schedule: everyFiveMinutes(), Enabled: true,
		AllowedTools: []AutomationTool{{ServerName: "files", ToolName: "files.read"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fire, _, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), automationDefaults)
	if err != nil {
		t.Fatal(err)
	}
	run, err := repo.GetRun(ctx, fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var userMessageID, payload string
	if err := repo.db.QueryRowContext(ctx, `SELECT id FROM messages WHERE session_id = ? AND role = 'user'`, fire.SessionID).Scan(&userMessageID); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.QueryRowContext(ctx, `SELECT payload_json FROM jobs WHERE run_id = ?`, fire.RunID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	want := `{"assistantMessageId":"` + run.AssistantMessageID + `","egressDecision":null,"externalAgent":null,` +
		`"memorySnapshotFingerprint":"3c755b55291c032b26a2ef753e438e26039f536d9d75d42f7f5a19df424b9fb5",` +
		`"minimumWorkerMaxConcurrentRuns":0,"model":"qwen2.5:7b","modelProvider":"ollama",` +
		`"pinnedPersona":{"personaId":"persona.md","displayName":"persona.md","body":"","contentHash":"","withheld":true},` +
		`"pinnedProfile":{"profileId":"profile.md","body":"","contentHash":"","withheld":true},` +
		`"requestedTools":[],"requiredContextTokens":0,"selectedTools":null,"sessionId":"` + fire.SessionID +
		`","skills":null,"traceId":"` + fire.TraceID + `","userMessageId":"` + userMessageID +
		`","userText":"Summarise the sandbox."}`
	if payload != want {
		t.Fatalf("job payload =\n%s\nwant\n%s", payload, want)
	}
}
