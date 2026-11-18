package runtime

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/tools"
)

// TUR-026d: an automation's frozen allowlist is a ceiling on every tool its
// run calls, safe reads included, decided at the beacon ahead of the policy.

// seedPseudoTools records the bundled and pseudo-server tools at their
// default policies, as a connected worker's discovery would: the files and
// system reads and the memory and team reads safe, and the skill loaders and
// the GitHub tools needing approval.
func (h *harness) seedPseudoTools(t *testing.T) {
	t.Helper()
	discovered := []repository.DiscoveredTool{}
	for _, tool := range tools.LegacyPolicyDefaults() {
		discovered = append(discovered, repository.DiscoveredTool{
			ServerName: tool.ServerName, ToolName: tool.ToolName, SchemaJSON: `{}`, Policy: string(tool.Policy),
		})
	}
	if err := h.repo.UpsertTools(context.Background(), append(discovered, []repository.DiscoveredTool{
		{ServerName: "skills", ToolName: "skills_list", SchemaJSON: `{}`, Policy: "approval_required"},
		{ServerName: "skills", ToolName: "skill_view", SchemaJSON: `{}`, Policy: "approval_required"},
		{ServerName: "memory", ToolName: "memory.search", SchemaJSON: `{}`, Policy: "safe"},
		{ServerName: "team", ToolName: "team.delegate", SchemaJSON: `{}`, Policy: "safe"},
		{ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required"},
		{ServerName: "integrations", ToolName: "github.create_comment", SchemaJSON: `{}`, Policy: "approval_required"},
	}...)); err != nil {
		t.Fatal(err)
	}
}

// safeVendorTool registers a third-party tool the owner has marked safe.
func (h *harness) safeVendorTool(t *testing.T) repository.AutomationTool {
	t.Helper()
	ctx := context.Background()
	server, err := h.repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetMCPServerEnabled(ctx, server.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.ReplaceMCPServerTools(ctx, server.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.lookup", Policy: "safe", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}
	return repository.AutomationTool{ServerName: "vendor", ToolName: "vendor.lookup"}
}

// A tool the automation never listed fails its run, whatever its policy: the
// files and system reads, a third-party tool the owner marked safe, and the
// GitHub tools, read and write alike. Nothing is allowed, no approval is
// created, and one automation.tool.blocked row names the code.
func TestAnUnlistedToolFailsAnAutomationsRunWhateverItsPolicy(t *testing.T) {
	for _, test := range []struct {
		name string
		tool func(t *testing.T, h *harness) repository.AutomationTool
	}{
		{name: "files.read", tool: func(*testing.T, *harness) repository.AutomationTool { return grantRead }},
		{name: "files.list", tool: func(*testing.T, *harness) repository.AutomationTool {
			return repository.AutomationTool{ServerName: "files", ToolName: "files.list"}
		}},
		{name: "system.time", tool: func(*testing.T, *harness) repository.AutomationTool {
			return repository.AutomationTool{ServerName: "system", ToolName: "system.time"}
		}},
		{name: "a safe third-party tool", tool: func(t *testing.T, h *harness) repository.AutomationTool { return h.safeVendorTool(t) }},
		{name: "github.list_issues", tool: func(*testing.T, *harness) repository.AutomationTool {
			return repository.AutomationTool{ServerName: "integrations", ToolName: "github.list_issues"}
		}},
		{name: "github.create_comment", tool: func(*testing.T, *harness) repository.AutomationTool {
			return repository.AutomationTool{ServerName: "integrations", ToolName: "github.create_comment"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			h.seedPseudoTools(t)
			tool := test.tool(t, h)
			fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{grantUpdate})

			decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_unlisted", tool, map[string]any{"path": "note.txt"}))
			if err != nil {
				t.Fatalf("tool beacon: %v", err)
			}
			if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || !decision.GetTerminalRun() ||
				decision.GetReason() != AutomationNotAllowlistedCode {
				t.Fatalf("decision = %+v, want a terminal %s deny", decision, AutomationNotAllowlistedCode)
			}
			h.assertRunFailedFor(t, fire.RunID, AutomationNotAllowlistedCode)
			blocked := h.auditRows(t, fire.RunID, "automation.tool.blocked")
			if !strings.Contains(blocked[0].Payload, `"toolName":"`+tool.ToolName+`"`) ||
				!strings.Contains(blocked[0].Payload, `"serverName":"`+tool.ServerName+`"`) ||
				strings.Contains(blocked[0].Payload, "note.txt") {
				t.Fatalf("blocked audit payload = %s, want the server and tool and no arguments", blocked[0].Payload)
			}
			if got := h.toolCallStatus(t, "call_unlisted"); got != "denied" {
				t.Fatalf("tool call status = %q, want denied", got)
			}
			if _, err := h.repo.GetApprovalByToolCall(context.Background(), fire.RunID, "call_unlisted"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("approval lookup err = %v, want none created", err)
			}
		})
	}
}

// A listed safe tool still runs without an approval, and a listed third-party
// safe tool too.
func TestAListedSafeToolRunsWithoutAnApproval(t *testing.T) {
	h := newHarness(t)
	vendor := h.safeVendorTool(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{grantRead, vendor})
	for i, tool := range []repository.AutomationTool{grantRead, vendor} {
		decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_listed_"+string(rune('a'+i)), tool, map[string]any{"path": "note.txt"}))
		if err != nil {
			t.Fatalf("%s beacon: %v", tool.ToolName, err)
		}
		if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW || decision.GetApprovalId() != "" {
			t.Fatalf("%s decision = %+v, want allow with no approval", tool.ToolName, decision)
		}
	}
	if state := h.runState(t, fire.RunID); state.Lifecycle != "running" {
		t.Fatalf("run lifecycle = %q, want running", state.Lifecycle)
	}
}

// A memory or team tool is refused before the allowlist is read, as a
// recorded tool.call.denied the model reads as a tool error, and the run
// carries on: no automation.tool.blocked row, no failure.
func TestMemoryAndTeamToolsAreRefusedWithoutFailingTheRun(t *testing.T) {
	h := newHarness(t)
	h.seedPseudoTools(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{grantRead})
	for _, tool := range []repository.AutomationTool{
		{ServerName: "memory", ToolName: "memory.search"},
		{ServerName: "team", ToolName: "team.delegate"},
	} {
		toolCallID := "call_" + strings.ReplaceAll(tool.ToolName, ".", "_")
		decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, toolCallID, tool, map[string]any{"query": "plans"}))
		if err != nil {
			t.Fatalf("%s beacon: %v", tool.ToolName, err)
		}
		if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || decision.GetTerminalRun() ||
			decision.GetReason() != repository.AutomationToolUnsupportedCode {
			t.Fatalf("%s decision = %+v, want a non-terminal %s deny", tool.ToolName, decision, repository.AutomationToolUnsupportedCode)
		}
		if got := h.toolCallStatus(t, toolCallID); got != "denied" {
			t.Fatalf("%s tool call status = %q, want denied", tool.ToolName, got)
		}
		if !h.hasEvent(t, fire.SessionID, "tool.call.denied", toolCallID) {
			t.Fatalf("%s: no tool.call.denied event recorded", tool.ToolName)
		}
	}
	if rows := h.auditRows(t, fire.RunID, "automation.tool.blocked"); len(rows) != 0 {
		t.Fatalf("automation.tool.blocked rows = %d, want none", len(rows))
	}
	if state := h.runState(t, fire.RunID); state.Lifecycle != "running" {
		t.Fatalf("run lifecycle = %q, want it still running", state.Lifecycle)
	}
	decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_after", grantRead, map[string]any{"path": "note.txt"}))
	if err != nil || decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("listed read after the refusals = %+v, %v; want the run to carry on", decision, err)
	}
}

// The skill loaders read only the owner-granted skills pinned on the job, so
// the ceiling exempts them: an automation keeps them with no skills tool on
// its allowlist. They default to approval_required, which an unattended run
// reaches only for a listed tool, so at that default an unlisted loader fails
// the run as it did before the ceiling; marked safe, it runs.
func TestSkillLoadersStillWorkInAnAutomationWithNoSkillsToolListed(t *testing.T) {
	loaders := []repository.AutomationTool{
		{ServerName: "skills", ToolName: "skills_list"},
		{ServerName: "skills", ToolName: "skill_view"},
	}
	t.Run("marked safe", func(t *testing.T) {
		h := newHarness(t)
		h.seedPseudoTools(t)
		for _, tool := range loaders {
			if err := h.repo.SetToolPolicyByName(context.Background(), tool.ServerName, tool.ToolName, "safe"); err != nil {
				t.Fatal(err)
			}
		}
		fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{grantRead})
		for _, tool := range loaders {
			decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_"+tool.ToolName, tool, map[string]any{}))
			if err != nil {
				t.Fatalf("%s beacon: %v", tool.ToolName, err)
			}
			if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
				t.Fatalf("%s decision = %+v, want allow", tool.ToolName, decision)
			}
		}
	})
	t.Run("at the default policy", func(t *testing.T) {
		h := newHarness(t)
		h.seedPseudoTools(t)
		fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{grantRead})
		decision, err := h.service.handleToolBeacon(context.Background(), automationToolBeacon(fire, "call_skills_list", loaders[0], map[string]any{}))
		if err != nil {
			t.Fatalf("skills_list beacon: %v", err)
		}
		if !decision.GetTerminalRun() || decision.GetReason() != AutomationNotAllowlistedCode {
			t.Fatalf("decision = %+v, want the terminal %s refusal it got before the ceiling", decision, AutomationNotAllowlistedCode)
		}
		h.assertRunFailedFor(t, fire.RunID, AutomationNotAllowlistedCode)
	})
}

// A refusing beacon replayed on the run it already failed writes no second
// row, and neither does a refusal that loses a race to another terminal
// transition, such as the owner's cancel: the run's own record stands.
func TestARepeatedRefusalWritesNoSecondRow(t *testing.T) {
	t.Run("replayed beacon", func(t *testing.T) {
		h := newHarness(t)
		fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{grantUpdate})
		beacon := automationToolBeacon(fire, "call_replayed", grantRead, map[string]any{"path": "note.txt"})
		if _, err := h.service.handleToolBeacon(context.Background(), beacon); err != nil {
			t.Fatalf("tool beacon: %v", err)
		}
		h.assertRunFailedFor(t, fire.RunID, AutomationNotAllowlistedCode)
		before := h.runState(t, fire.RunID)
		_, _ = h.service.handleToolBeacon(context.Background(), beacon)
		h.assertRunFailedFor(t, fire.RunID, AutomationNotAllowlistedCode)
		if after := h.runState(t, fire.RunID); after.StateVersion != before.StateVersion {
			t.Fatalf("state version = %d, want %d unchanged", after.StateVersion, before.StateVersion)
		}
	})
	t.Run("lost to a cancel", func(t *testing.T) {
		h := newHarness(t)
		fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{grantUpdate})
		run, err := h.repo.GetRun(context.Background(), fire.RunID)
		if err != nil {
			t.Fatal(err)
		}
		cancelRunFixture(t, h, fire.RunID)
		beacon := automationToolBeacon(fire, "call_raced", grantRead, map[string]any{"path": "note.txt"})
		argsJSON, argsHash, err := canonicalArgs(map[string]any{"path": "note.txt"})
		if err != nil {
			t.Fatal(err)
		}
		decision, err := h.service.blockUnattendedTool(context.Background(), beacon, run, argsJSON, argsHash,
			repository.AutomationRunGrant{AutomationID: fire.AutomationID, AutomationName: "Nightly note"}, AutomationNotAllowlistedCode)
		if err != nil || !decision.GetTerminalRun() {
			t.Fatalf("decision = %+v, %v; want the runtime told to stop", decision, err)
		}
		if rows := h.auditRows(t, fire.RunID, "automation.tool.blocked"); len(rows) != 0 {
			t.Fatalf("automation.tool.blocked rows = %d, want none for a refusal that lost to the cancel", len(rows))
		}
		if state := h.runState(t, fire.RunID); state.Lifecycle != "cancelled" {
			t.Fatalf("run lifecycle = %q, want it still cancelled", state.Lifecycle)
		}
	})
}

// The refusal hook a registered-MCP dispatch calls fails the run and writes
// its row through the same repository call, and publishes the run's failure on
// the bus, so a session already subscribed sees it. A second refusal on the
// failed run writes, publishes and changes nothing.
func TestRefuseUnattendedCallFailsPublishesAndAuditsOnce(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{grantUpdate})
	published, unsubscribe := h.bus.Subscribe(fire.SessionID)
	defer unsubscribe()
	block := repository.UnattendedToolBlock{
		AutomationID: fire.AutomationID, AutomationName: "Nightly note",
		ServerName: "vendor", ToolName: "vendor.lookup", Code: AutomationNotAllowlistedCode,
	}

	if err := h.service.RefuseUnattendedCall(context.Background(), fire.RunID, block); err != nil {
		t.Fatalf("RefuseUnattendedCall: %v", err)
	}
	h.assertRunFailedFor(t, fire.RunID, AutomationNotAllowlistedCode)
	if event := waitBusEvent(t, published, "agent.run.failed"); !strings.Contains(event.PayloadJSON, AutomationNotAllowlistedCode) {
		t.Fatalf("published failure = %s, want the code", event.PayloadJSON)
	}

	before := h.runState(t, fire.RunID)
	if err := h.service.RefuseUnattendedCall(context.Background(), fire.RunID, block); err != nil {
		t.Fatalf("second RefuseUnattendedCall: %v", err)
	}
	h.assertRunFailedFor(t, fire.RunID, AutomationNotAllowlistedCode)
	if after := h.runState(t, fire.RunID); after.StateVersion != before.StateVersion {
		t.Fatalf("state version = %d, want %d unchanged", after.StateVersion, before.StateVersion)
	}
	assertNoBusEvent(t, published)
}

// An audit row that cannot be written rolls the failure back, and nothing is
// published.
func TestRefuseUnattendedCallPublishesNothingWhenItsRowCannotBeWritten(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{grantUpdate})
	published, unsubscribe := h.bus.Subscribe(fire.SessionID)
	defer unsubscribe()
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER refuse_blocked_audit BEFORE INSERT ON audit_logs
		WHEN NEW.action = 'automation.tool.blocked'
		BEGIN SELECT RAISE(ABORT, 'audit insert refused'); END`); err != nil {
		t.Fatal(err)
	}
	err := h.service.RefuseUnattendedCall(context.Background(), fire.RunID, repository.UnattendedToolBlock{
		AutomationID: fire.AutomationID, AutomationName: "Nightly note",
		ServerName: "vendor", ToolName: "vendor.lookup", Code: AutomationNotAllowlistedCode,
	})
	if err == nil {
		t.Fatal("RefuseUnattendedCall succeeded with its audit row refused")
	}
	if state := h.runState(t, fire.RunID); state.Lifecycle != "running" {
		t.Fatalf("run lifecycle = %q, want the failure rolled back", state.Lifecycle)
	}
	assertNoBusEvent(t, published)
}

// A grant failure is not a blocked tool: it fails the run and writes no
// automation.tool.blocked row.
func TestAGrantFailureWritesNoBlockedRow(t *testing.T) {
	h := newHarness(t)
	h.service.approvals = plainApprovalCreator{inner: h.approvals}
	fire := h.fireAutomation(t, "Nightly note", []repository.AutomationTool{grantUpdate})
	decision, err := h.service.handleToolBeacon(context.Background(), filesUpdateBeacon(t, fire, "call_grant_failed"))
	if err != nil {
		t.Fatalf("tool beacon: %v", err)
	}
	if decision.GetReason() != "automation_approval_failed" || !decision.GetTerminalRun() {
		t.Fatalf("decision = %+v, want a terminal automation_approval_failed", decision)
	}
	if state := h.runState(t, fire.RunID); state.Lifecycle != "failed" {
		t.Fatalf("run lifecycle = %q, want failed", state.Lifecycle)
	}
	if rows := h.auditRows(t, fire.RunID, "automation.tool.blocked"); len(rows) != 0 {
		t.Fatalf("automation.tool.blocked rows = %d, want none for a grant failure", len(rows))
	}
}

func (h *harness) hasEvent(t *testing.T, sessionID string, eventType string, toolCallID string) bool {
	t.Helper()
	replayed, _, err := h.repo.ReplayEvents(context.Background(), sessionID, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range replayed {
		if event.Type == eventType && strings.Contains(event.PayloadJSON, `"toolCallId":"`+toolCallID+`"`) {
			return true
		}
	}
	return false
}

func waitBusEvent(t *testing.T, published <-chan events.Event, eventType string) events.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-published:
			if event.Type == eventType {
				return event
			}
		case <-deadline:
			t.Fatalf("no %s published", eventType)
		}
	}
}

func assertNoBusEvent(t *testing.T, published <-chan events.Event) {
	t.Helper()
	select {
	case event := <-published:
		t.Fatalf("published %s, want nothing", event.Type)
	case <-time.After(100 * time.Millisecond):
	}
}
