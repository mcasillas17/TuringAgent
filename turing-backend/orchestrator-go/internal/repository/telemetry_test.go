package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Every window in these tests is measured from one pinned instant. A "last 7
// days" assertion that reads the wall clock passes or fails depending on when
// CI happens to run, which makes it worse than no assertion.
var telemetryAsOf = time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)

func TestTelemetrySummaryOnAnEmptyDatabaseReportsZeroesAndNoAverages(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	// SUM over no rows is NULL and AVG over no rows is NULL. A count that came
	// back as "unknown" would render as blank where zero is the truth, and an
	// average that came back as 0 would claim runs took no time.
	if summary.Runs.Total != 0 || summary.Runs.Completed != 0 || summary.Runs.Failed != 0 ||
		summary.Runs.Cancelled != 0 || summary.Runs.InFlight != 0 {
		t.Fatalf("run totals = %+v, want all zero", summary.Runs)
	}
	if summary.Runs.AverageDurationMs != nil {
		t.Fatalf("average duration = %d, want absent", *summary.Runs.AverageDurationMs)
	}
	if summary.Tokens.InputTokens != nil || summary.Tokens.OutputTokens != nil {
		t.Fatalf("token totals = %+v, want absent", summary.Tokens)
	}
	if summary.Tokens.RunsWithUsage != 0 || summary.Tokens.RunsWithoutUsage != 0 {
		t.Fatalf("token provenance = %+v, want zero counts", summary.Tokens)
	}
	if len(summary.Tools) != 0 || len(summary.Models) != 0 || len(summary.ExternalAgents) != 0 {
		t.Fatalf("groups = %d tools, %d models, %d agents; want none",
			len(summary.Tools), len(summary.Models), len(summary.ExternalAgents))
	}
	if summary.Automations.Runs != 0 || summary.Automations.UnattendedApprovals != 0 {
		t.Fatalf("automation totals = %+v, want zero", summary.Automations)
	}
	if summary.Integrations.Connected != 0 || summary.Integrations.Revoked != 0 {
		t.Fatalf("integration totals = %+v, want zero", summary.Integrations)
	}
	// Dense from the window, not from the data: an empty week still has to
	// draw as an empty week, and exactly one bar per day the label promises.
	if len(summary.Daily) != 7 {
		t.Fatalf("daily entries = %d, want one per day of the window", len(summary.Daily))
	}
	if summary.Daily[0].Date != "2026-03-09" || summary.Daily[6].Date != "2026-03-15" {
		t.Fatalf("daily span = %s..%s, want 2026-03-09..2026-03-15", summary.Daily[0].Date, summary.Daily[6].Date)
	}
	for _, day := range summary.Daily {
		if day.Runs != 0 || day.ToolCalls != 0 || day.InputTokens != nil {
			t.Fatalf("empty day %s = %+v", day.Date, day)
		}
	}
}

// One row is the other place SUM/AVG go wrong: a single value must not be
// averaged against a phantom zero.
func TestTelemetrySummaryWithASingleRun(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_only", createdAt: telemetryAsOf.Add(-time.Hour), status: "completed",
		startedAt: telemetryAsOf.Add(-time.Hour), finishedAt: telemetryAsOf.Add(-time.Hour).Add(2 * time.Second),
		inputTokens: intPointer(300), outputTokens: intPointer(40),
		provider: "ollama", model: "qwen2.5:7b",
	})

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	if summary.Runs.Total != 1 || summary.Runs.Completed != 1 {
		t.Fatalf("run totals = %+v, want one completed run", summary.Runs)
	}
	if summary.Runs.AverageDurationMs == nil || *summary.Runs.AverageDurationMs != 2000 {
		t.Fatalf("average duration = %v, want 2000ms", summary.Runs.AverageDurationMs)
	}
	if summary.Tokens.InputTokens == nil || *summary.Tokens.InputTokens != 300 ||
		summary.Tokens.OutputTokens == nil || *summary.Tokens.OutputTokens != 40 {
		t.Fatalf("token totals = %+v, want 300/40", summary.Tokens)
	}
	if summary.Tokens.RunsWithUsage != 1 || summary.Tokens.RunsWithoutUsage != 0 {
		t.Fatalf("token provenance = %+v, want 1 with usage", summary.Tokens)
	}
	if len(summary.Models) != 1 || summary.Models[0].Provider != "ollama" || summary.Models[0].Model != "qwen2.5:7b" {
		t.Fatalf("models = %+v, want one ollama entry", summary.Models)
	}
}

// The boundary is what makes a window a window. Both ends are checked because
// an off-by-one at either would silently change every number on the page.
func TestTelemetrySummaryCountsOnlyRunsInsideTheWindow(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	// Snapped to a UTC day boundary: "last 7 days" is today plus the six
	// before it, so the window opens at midnight on the 9th.
	start := time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC)
	if got := telemetryAsOf.AddDate(0, 0, -6).Truncate(24 * time.Hour); !got.Equal(start) {
		t.Fatalf("test's own window start = %s, want %s", got, start)
	}
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_before", createdAt: start.Add(-time.Second), status: "completed"})
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_at_start", createdAt: start, status: "completed"})
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_inside", createdAt: telemetryAsOf.Add(-time.Hour), status: "failed"})
	// A row timestamped after the report was computed — a clock skew, or a
	// scheduler that wrote ahead — is outside the window and stays outside it.
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_after", createdAt: telemetryAsOf.Add(time.Second), status: "completed"})

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	if summary.Runs.Total != 2 {
		t.Fatalf("runs in window = %d, want 2 (inclusive start, exclusive end)", summary.Runs.Total)
	}
	if summary.Runs.Completed != 1 || summary.Runs.Failed != 1 {
		t.Fatalf("run outcomes = %+v, want one completed and one failed", summary.Runs)
	}
}

// The provenance rule, at the level that decides what the client draws.
func TestTelemetrySummarySeparatesRunsThatReportedTokensFromThoseThatDidNot(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	inside := telemetryAsOf.Add(-time.Hour)
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_reported", createdAt: inside, status: "completed",
		inputTokens: intPointer(120), outputTokens: intPointer(30),
	})
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_silent", createdAt: inside, status: "completed"})
	// A run that failed never reached the completion where usage is recorded,
	// so counting it as "the provider reported nothing" would blame the
	// provider for something it was never asked.
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_failed", createdAt: inside, status: "failed"})
	// Neither has an in-flight run, which has not finished at all.
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_running", createdAt: inside, status: "running"})

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	if summary.Tokens.RunsWithUsage != 1 {
		t.Fatalf("runs with usage = %d, want 1", summary.Tokens.RunsWithUsage)
	}
	if summary.Tokens.RunsWithoutUsage != 1 {
		t.Fatalf("runs without usage = %d, want 1 (only the completed silent run)", summary.Tokens.RunsWithoutUsage)
	}
	if summary.Tokens.InputTokens == nil || *summary.Tokens.InputTokens != 120 {
		t.Fatalf("input tokens = %v, want 120", summary.Tokens.InputTokens)
	}
	if summary.Runs.InFlight != 1 {
		t.Fatalf("in-flight runs = %d, want 1", summary.Runs.InFlight)
	}
}

// Every completed run being silent is the ordinary case on an older Ollama.
// The totals must be ABSENT, not zero: a zero would read as "this cost you
// nothing".
func TestTelemetrySummaryLeavesTokensAbsentWhenNoRunReportedAny(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_1", createdAt: telemetryAsOf.Add(-time.Hour), status: "completed"})
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_2", createdAt: telemetryAsOf.Add(-time.Hour), status: "completed"})

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	if summary.Tokens.InputTokens != nil || summary.Tokens.OutputTokens != nil {
		t.Fatalf("token totals = %+v, want absent rather than zero", summary.Tokens)
	}
	if summary.Tokens.RunsWithoutUsage != 2 {
		t.Fatalf("runs without usage = %d, want 2", summary.Tokens.RunsWithoutUsage)
	}
}

func TestTelemetrySummaryRanksToolsByUseAndReportsFailuresAndLatency(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	inside := telemetryAsOf.Add(-time.Hour)
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_1", createdAt: inside, status: "completed"})
	insertTelemetryToolCall(t, ctx, repo, "call_1", "run_1", "files", "files.read", "completed", intPointer(10), inside)
	insertTelemetryToolCall(t, ctx, repo, "call_2", "run_1", "files", "files.read", "failed", intPointer(30), inside)
	insertTelemetryToolCall(t, ctx, repo, "call_3", "run_1", "system", "system.time", "completed", intPointer(5), inside)
	// A denied call never ran, so it has no duration. Its tool must not
	// therefore report an average of zero.
	insertTelemetryToolCall(t, ctx, repo, "call_4", "run_1", "files", "files.create", "denied", nil, inside)
	// Outside the window entirely.
	insertTelemetryToolCall(t, ctx, repo, "call_old", "run_1", "system", "system.time", "completed", intPointer(1000), telemetryAsOf.AddDate(0, 0, -30))

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	if len(summary.Tools) != 3 {
		t.Fatalf("tools = %+v, want three entries", summary.Tools)
	}
	top := summary.Tools[0]
	if top.ServerName != "files" || top.ToolName != "files.read" || top.Calls != 2 || top.Failed != 1 {
		t.Fatalf("most-used tool = %+v, want files.read with 2 calls and 1 failure", top)
	}
	if top.AverageDurationMs == nil || *top.AverageDurationMs != 20 {
		t.Fatalf("files.read average = %v, want 20ms", top.AverageDurationMs)
	}
	var denied TelemetryToolUsage
	for _, tool := range summary.Tools {
		if tool.ToolName == "files.create" {
			denied = tool
		}
	}
	if denied.Denied != 1 {
		t.Fatalf("files.create = %+v, want one denial", denied)
	}
	if denied.AverageDurationMs != nil {
		t.Fatalf("files.create average = %d, want absent for a tool that never ran", *denied.AverageDurationMs)
	}
	// The old call must not have leaked into system.time's numbers.
	for _, tool := range summary.Tools {
		if tool.ToolName == "system.time" && tool.Calls != 1 {
			t.Fatalf("system.time calls = %d, want 1 (the 30-day-old call is outside the window)", tool.Calls)
		}
	}
}

// What left the machine, attributed per run rather than to wherever the
// conversation currently points.
func TestTelemetrySummaryGroupsRunsThatLeftTheMachine(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	inside := telemetryAsOf.Add(-time.Hour)
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_remote_1", createdAt: inside, status: "completed",
		externalName: "Claude", externalHost: "api.anthropic.com",
		inputTokens: intPointer(500), outputTokens: intPointer(60),
	})
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_remote_2", createdAt: inside, status: "completed",
		externalName: "Claude", externalHost: "api.anthropic.com",
	})
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_local", createdAt: inside, status: "completed"})

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	if len(summary.ExternalAgents) != 1 {
		t.Fatalf("external agents = %+v, want one group", summary.ExternalAgents)
	}
	agent := summary.ExternalAgents[0]
	if agent.DisplayName != "Claude" || agent.EndpointHost != "api.anthropic.com" || agent.Runs != 2 {
		t.Fatalf("external agent = %+v, want Claude with 2 runs", agent)
	}
	if agent.InputTokens == nil || *agent.InputTokens != 500 {
		t.Fatalf("external input tokens = %v, want 500", agent.InputTokens)
	}
	if agent.RunsWithoutUsage != 1 {
		t.Fatalf("external runs without usage = %d, want 1", agent.RunsWithoutUsage)
	}
}

func TestTelemetrySummaryCountsAutomationRunsAndUnattendedApprovals(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	inside := telemetryAsOf.Add(-time.Hour)
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_auto_1", createdAt: inside, status: "completed"})
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_auto_2", createdAt: inside, status: "failed"})
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_person", createdAt: inside, status: "completed"})
	insertTelemetryAutomationRun(t, ctx, repo, "run_auto_1", "auto_1", "Morning digest", inside)
	insertTelemetryAutomationRun(t, ctx, repo, "run_auto_2", "auto_1", "Morning digest", inside)
	insertTelemetryApproval(t, ctx, repo, "appr_1", "run_auto_1", "consumed", inside)
	insertTelemetryApproval(t, ctx, repo, "appr_2", "run_auto_2", "approved", inside)
	// Denied, so nothing was granted.
	insertTelemetryApproval(t, ctx, repo, "appr_3", "run_auto_2", "denied", inside)
	// A person's own approval is not unattended, however it was decided.
	insertTelemetryApproval(t, ctx, repo, "appr_4", "run_person", "consumed", inside)

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	if summary.Automations.Runs != 2 || summary.Automations.Completed != 1 || summary.Automations.Failed != 1 {
		t.Fatalf("automation totals = %+v, want 2 runs split one/one", summary.Automations)
	}
	if summary.Automations.UnattendedApprovals != 2 {
		t.Fatalf("unattended approvals = %d, want 2", summary.Automations.UnattendedApprovals)
	}
}

// Not windowed, and labelled as current state rather than as activity.
func TestTelemetrySummaryReportsIntegrationConnectionsAsCurrentState(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	insertTelemetryConnection(t, ctx, repo, "conn_1", ConnectionStatusConnected, telemetryAsOf.AddDate(-1, 0, 0))
	insertTelemetryConnection(t, ctx, repo, "conn_2", ConnectionStatusConnected, telemetryAsOf.AddDate(-1, 0, 0))
	insertTelemetryConnection(t, ctx, repo, "conn_3", ConnectionStatusRevoked, telemetryAsOf.AddDate(-1, 0, 0))

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	// Connected a year ago and still counted: this is state, not a window.
	if summary.Integrations.Connected != 2 || summary.Integrations.Revoked != 1 {
		t.Fatalf("integration totals = %+v, want 2 connected and 1 revoked", summary.Integrations)
	}
}

func TestTelemetrySummaryBuildsADenseDailySeriesInUTC(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	// 2026-03-13 in UTC, twice; and one tool call on a different day so the
	// two series have to be merged rather than one overwriting the other.
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_a", createdAt: time.Date(2026, time.March, 13, 8, 0, 0, 0, time.UTC),
		status: "completed", inputTokens: intPointer(10), outputTokens: intPointer(2),
	})
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_b", createdAt: time.Date(2026, time.March, 13, 20, 0, 0, 0, time.UTC),
		status: "completed", inputTokens: intPointer(5), outputTokens: intPointer(1),
	})
	insertTelemetryToolCall(t, ctx, repo, "call_1", "run_a", "system", "system.time", "completed", intPointer(3),
		time.Date(2026, time.March, 11, 9, 0, 0, 0, time.UTC))

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	byDate := make(map[string]TelemetryDailyActivity, len(summary.Daily))
	previous := ""
	for _, day := range summary.Daily {
		if previous != "" && day.Date <= previous {
			t.Fatalf("daily series is not ascending: %s after %s", day.Date, previous)
		}
		previous = day.Date
		byDate[day.Date] = day
	}
	busy := byDate["2026-03-13"]
	if busy.Runs != 2 || busy.InputTokens == nil || *busy.InputTokens != 15 {
		t.Fatalf("2026-03-13 = %+v, want 2 runs and 15 input tokens", busy)
	}
	toolDay := byDate["2026-03-11"]
	if toolDay.Runs != 0 || toolDay.ToolCalls != 1 {
		t.Fatalf("2026-03-11 = %+v, want a tool-only day", toolDay)
	}
	quiet := byDate["2026-03-12"]
	if quiet.Runs != 0 || quiet.ToolCalls != 0 || quiet.InputTokens != nil {
		t.Fatalf("2026-03-12 = %+v, want a present but empty day", quiet)
	}
}

func TestTelemetrySummaryRejectsWindowsItCannotAnswer(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	for _, days := range []int{0, -1, MaxTelemetryWindowDays + 1} {
		if _, err := repo.TelemetrySummary(ctx, telemetryAsOf, days); !errors.Is(err, ErrTelemetryWindowOutOfRange) {
			t.Fatalf("window %d error = %v, want ErrTelemetryWindowOutOfRange", days, err)
		}
	}
	if _, err := repo.TelemetrySummary(ctx, telemetryAsOf, MaxTelemetryWindowDays); err != nil {
		t.Fatalf("the largest allowed window failed: %v", err)
	}
}

// Deleting a conversation is a request to forget it. Telemetry must not be the
// place its runs survive.
func TestTelemetrySummaryForgetsRunsFromADeletedSession(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	inside := telemetryAsOf.Add(-time.Hour)
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_kept", createdAt: inside, status: "completed", inputTokens: intPointer(10),
		sessionID: "sess_keep",
	})
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_forgotten", createdAt: inside, status: "completed", inputTokens: intPointer(999),
		sessionID: "sess_forget",
	})

	if _, err := repo.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = 'sess_forget'`); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}
	if summary.Runs.Total != 1 {
		t.Fatalf("runs after deletion = %d, want 1", summary.Runs.Total)
	}
	if summary.Tokens.InputTokens == nil || *summary.Tokens.InputTokens != 10 {
		t.Fatalf("input tokens after deletion = %v, want 10", summary.Tokens.InputTokens)
	}
}

// The columns are written by the enqueue path, not by a later backfill, so a
// message sent to an agent that is subsequently deleted still says where it
// went.
func TestEnqueueRecordsWhereARoutedRunWasSent(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Routed")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	agent, err := repo.CreateExternalAgent(ctx, ExternalAgentInput{
		DisplayName:   "Claude",
		Provider:      "anthropic",
		BaseURL:       "https://api.anthropic.com/v1/messages",
		Model:         "claude-sonnet-4",
		CredentialRef: "ANTHROPIC",
	})
	if err != nil {
		t.Fatalf("create external agent: %v", err)
	}
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatalf("route session: %v", err)
	}

	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var name, host string
	if err := repo.db.QueryRowContext(ctx,
		`SELECT external_agent_name, external_agent_host FROM agent_runs WHERE id = ?`,
		enqueued.RunID).Scan(&name, &host); err != nil {
		t.Fatalf("read routing attribution: %v", err)
	}
	if name != "Claude" {
		t.Fatalf("external agent name = %q, want Claude", name)
	}
	// Host only. The path is dropped: a base URL is where a credential gets
	// pasted by accident, and this row is read straight back into a report.
	if host != "api.anthropic.com" {
		t.Fatalf("external agent host = %q, want api.anthropic.com with no path", host)
	}

	// Deleting the agent returns the conversation to the local assistant but
	// must not rewrite where the message already went.
	if err := repo.DeleteExternalAgent(ctx, agent.AgentID); err != nil {
		t.Fatalf("delete external agent: %v", err)
	}
	summary, err := repo.TelemetrySummary(ctx, time.Now().UTC(), 1)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}
	if len(summary.ExternalAgents) != 1 || summary.ExternalAgents[0].DisplayName != "Claude" {
		t.Fatalf("external agents after deletion = %+v, want the record to survive", summary.ExternalAgents)
	}
}

// A local run must not be attributed to anyone.
func TestEnqueueLeavesRoutingAttributionEmptyForALocalRun(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Local")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var name, host any
	if err := repo.db.QueryRowContext(ctx,
		`SELECT external_agent_name, external_agent_host FROM agent_runs WHERE id = ?`,
		enqueued.RunID).Scan(&name, &host); err != nil {
		t.Fatalf("read routing attribution: %v", err)
	}
	if name != nil || host != nil {
		t.Fatalf("local run attribution = %v / %v, want NULL", name, host)
	}
}

// Token usage is recorded by the same transaction that completes the run, and
// a completion carrying nothing leaves the columns NULL rather than zero.
func TestCompleteRunWithEventStoresReportedTokensAndNothingElse(t *testing.T) {
	for _, tt := range []struct {
		name       string
		usage      *RunTokenUsage
		wantInput  any
		wantOutput any
	}{
		{name: "both reported", usage: &RunTokenUsage{InputTokens: intPointer(80), OutputTokens: intPointer(9)}, wantInput: int64(80), wantOutput: int64(9)},
		{name: "nothing reported", usage: nil, wantInput: nil, wantOutput: nil},
		{name: "only output reported", usage: &RunTokenUsage{OutputTokens: intPointer(4)}, wantInput: nil, wantOutput: int64(4)},
		{name: "a reported zero is a measurement", usage: &RunTokenUsage{InputTokens: intPointer(0), OutputTokens: intPointer(0)}, wantInput: int64(0), wantOutput: int64(0)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo, ctx := newTitleTestRepo(t)
			session, err := repo.CreateSession(ctx, "Chat")
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
				SessionID: session.SessionID, Content: "hi", AgentID: "general_assistant",
				ModelProvider: "ollama", Model: "qwen2.5:7b",
			})
			if err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
				t.Fatalf("mark running: %v", err)
			}
			if _, err := completeRunAtCurrentVersion(t, repo, enqueued.RunID, enqueued.AssistantMessageID, "done", tt.usage); err != nil {
				t.Fatalf("complete run: %v", err)
			}

			var input, output any
			if err := repo.db.QueryRowContext(ctx,
				`SELECT input_tokens, output_tokens FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&input, &output); err != nil {
				t.Fatalf("read tokens: %v", err)
			}
			if input != tt.wantInput || output != tt.wantOutput {
				t.Fatalf("stored tokens = %v / %v, want %v / %v", input, output, tt.wantInput, tt.wantOutput)
			}
		})
	}
}

// A group's token total and its run count must share a denominator. Counting
// only completed runs as "without usage" would let this group report 4 runs, a
// total covering 2, and nothing unmeasured — which reads as a complete
// measurement of all four.
func TestTelemetrySummaryCountsEveryUnmeasuredRunInAModelGroup(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	inside := telemetryAsOf.Add(-time.Hour)
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_1", createdAt: inside, status: "completed",
		inputTokens: intPointer(100), outputTokens: intPointer(10),
	})
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_2", createdAt: inside, status: "completed",
		inputTokens: intPointer(50), outputTokens: intPointer(5),
	})
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_3", createdAt: inside, status: "failed"})
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_4", createdAt: inside, status: "running"})

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	if len(summary.Models) != 1 {
		t.Fatalf("models = %+v, want one group", summary.Models)
	}
	model := summary.Models[0]
	if model.Runs != 4 {
		t.Fatalf("runs = %d, want 4", model.Runs)
	}
	if model.RunsWithoutUsage != 2 {
		t.Fatalf("runs without usage = %d, want 2 (the failed and the running one)", model.RunsWithoutUsage)
	}
	if model.InputTokens == nil || *model.InputTokens != 150 {
		t.Fatalf("input tokens = %v, want 150", model.InputTokens)
	}
}

func TestTelemetrySummaryCountsEveryUnmeasuredRunForAnExternalAgent(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	inside := telemetryAsOf.Add(-time.Hour)
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_ok", createdAt: inside, status: "completed",
		externalName: "Claude", externalHost: "api.anthropic.com",
		inputTokens: intPointer(80), outputTokens: intPointer(8),
	})
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_failed", createdAt: inside, status: "failed",
		externalName: "Claude", externalHost: "api.anthropic.com",
	})

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	agent := summary.ExternalAgents[0]
	if agent.Runs != 2 || agent.RunsWithoutUsage != 1 {
		t.Fatalf("external agent = %+v, want 2 runs with 1 unmeasured", agent)
	}
}

// A run that never completes never records tokens, because completion is the
// only place they are written. That is what makes NULL mean "unknown" rather
// than "we forgot".
func TestOnlyCompletionWritesTokenColumns(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Chat")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hi", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if _, err := failRunAtCurrentVersion(t, repo, enqueued.RunID, testFailure("model_error")); err != nil {
		t.Fatalf("fail run: %v", err)
	}

	var input, output any
	if err := repo.db.QueryRowContext(ctx,
		`SELECT input_tokens, output_tokens FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&input, &output); err != nil {
		t.Fatalf("read tokens: %v", err)
	}
	if input != nil || output != nil {
		t.Fatalf("failed run tokens = %v / %v, want NULL", input, output)
	}
}

// The average is over runs that actually recorded a span. A run still in
// flight has no end, and a backwards clock has no believable span at all.
func TestTelemetrySummaryAveragesOnlyBelievableDurations(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	base := telemetryAsOf.Add(-2 * time.Hour)
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_measured", createdAt: base, status: "completed",
		startedAt: base, finishedAt: base.Add(4 * time.Second),
	})
	// Started but never finished: no span, so it must not pull the mean down.
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_in_flight", createdAt: base, status: "running", startedAt: base,
	})
	// Finished before it started, which only a clock change can produce. A
	// negative span would drag the mean below anything that happened.
	insertTelemetryRun(t, ctx, repo, telemetryRun{
		id: "run_backwards", createdAt: base, status: "completed",
		startedAt: base, finishedAt: base.Add(-time.Hour),
	})

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	if summary.Runs.AverageDurationMs == nil || *summary.Runs.AverageDurationMs != 4000 {
		t.Fatalf("average duration = %v, want the one believable span of 4000ms", summary.Runs.AverageDurationMs)
	}
}

// Only a granted approval counts as consent given in advance.
func TestTelemetrySummaryCountsOnlyGrantedUnattendedApprovals(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	inside := telemetryAsOf.Add(-time.Hour)
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_auto", createdAt: inside, status: "completed"})
	insertTelemetryAutomationRun(t, ctx, repo, "run_auto", "auto_1", "Digest", inside)
	for index, status := range []string{"pending", "denied", "expired"} {
		insertTelemetryApproval(t, ctx, repo, fmt.Sprintf("appr_%d", index), "run_auto", status, inside)
	}

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	if summary.Automations.UnattendedApprovals != 0 {
		t.Fatalf("unattended approvals = %d, want 0 — none was granted", summary.Automations.UnattendedApprovals)
	}
}

// "Most used" is the question, so the list is capped and ordered by use. The
// name tiebreak is what keeps two refreshes over the same data from looking
// like movement.
func TestTelemetrySummaryCapsAndOrdersTheMostUsedTools(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	inside := telemetryAsOf.Add(-time.Hour)
	insertTelemetryRun(t, ctx, repo, telemetryRun{id: "run_1", createdAt: inside, status: "completed"})
	// 25 distinct tools, every one called exactly once except the last, so the
	// cap has something to drop and the tiebreak has something to order.
	for index := range 25 {
		insertTelemetryToolCall(t, ctx, repo,
			fmt.Sprintf("call_%02d", index), "run_1", "system",
			fmt.Sprintf("system.tool_%02d", index), "completed", intPointer(1), inside)
	}
	insertTelemetryToolCall(t, ctx, repo, "call_busy", "run_1", "system", "system.tool_24", "completed", intPointer(1), inside)

	summary, err := repo.TelemetrySummary(ctx, telemetryAsOf, 7)
	if err != nil {
		t.Fatalf("telemetry summary: %v", err)
	}

	if len(summary.Tools) != maxTelemetryGroups {
		t.Fatalf("tools = %d, want the list capped at %d", len(summary.Tools), maxTelemetryGroups)
	}
	if summary.Tools[0].ToolName != "system.tool_24" || summary.Tools[0].Calls != 2 {
		t.Fatalf("most-used tool = %+v, want system.tool_24 with 2 calls", summary.Tools[0])
	}
	// Everything after the leader is a one-call tie, broken by name so the
	// order is the same every time the same rows are read.
	for index := 1; index < len(summary.Tools); index++ {
		want := fmt.Sprintf("system.tool_%02d", index-1)
		if summary.Tools[index].ToolName != want {
			t.Fatalf("tool %d = %q, want %q", index, summary.Tools[index].ToolName, want)
		}
	}
}

type telemetryRun struct {
	id           string
	sessionID    string
	createdAt    time.Time
	startedAt    time.Time
	finishedAt   time.Time
	status       string
	provider     string
	model        string
	inputTokens  *int64
	outputTokens *int64
	externalName string
	externalHost string
}

func insertTelemetryRun(t *testing.T, ctx context.Context, repo *Repository, run telemetryRun) {
	t.Helper()
	sessionID := run.sessionID
	if sessionID == "" {
		sessionID = "sess_telemetry"
	}
	if _, err := repo.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO sessions (id, status, created_at, updated_at) VALUES (?, 'active', ?, ?)`,
		sessionID, FormatTimestamp(run.createdAt), FormatTimestamp(run.createdAt)); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	messageID := "msg_" + run.id
	if _, err := repo.db.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		 VALUES (?, ?, 'user', 'x', 'text', (SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE session_id = ?), ?)`,
		messageID, sessionID, sessionID, FormatTimestamp(run.createdAt)); err != nil {
		t.Fatalf("insert message: %v", err)
	}
	provider := run.provider
	if provider == "" {
		provider = "ollama"
	}
	model := run.model
	if model == "" {
		model = "qwen2.5:7b"
	}
	var startedAt, finishedAt, externalName, externalHost any
	if !run.startedAt.IsZero() {
		startedAt = FormatTimestamp(run.startedAt)
	}
	if !run.finishedAt.IsZero() {
		finishedAt = FormatTimestamp(run.finishedAt)
	}
	if run.externalName != "" {
		externalName = run.externalName
		externalHost = run.externalHost
	}
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO agent_runs (
			id, session_id, user_message_id, agent_id, trace_id, status,
			model_provider, model_name, input_tokens, output_tokens,
			external_agent_name, external_agent_host, created_at, started_at, finished_at,
			state_version, state_updated_at, outcome_reason, assistant_content_sha256)
		VALUES (?, ?, ?, 'general_assistant', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		run.id, sessionID, messageID, "trace_"+run.id, run.status, provider, model,
		nonNegativeNullInt64(run.inputTokens), nonNegativeNullInt64(run.outputTokens),
		externalName, externalHost,
		FormatTimestamp(run.createdAt), startedAt, finishedAt,
		FormatTimestamp(run.createdAt), telemetryOutcomeReason(run.status), emptyAssistantContentSHA256); err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

// telemetryOutcomeReason gives these direct fixtures an outcome that reads
// sensibly next to their status. What the schema actually enforces is the
// closed outcome_reason vocabulary, not the pairing of an outcome with a
// lifecycle: that cross-column rule belongs to the versioned transitions, and
// no constraint here would reject a mismatched pair today. Telemetry does not
// read the outcome at all; the fixtures only have to be legal rows.
func telemetryOutcomeReason(status string) string {
	switch status {
	case "completed":
		return "completed_no_content"
	case "failed":
		return "internal_failure"
	case "cancelled":
		return "abandoned"
	default:
		return "none"
	}
}

func insertTelemetryToolCall(t *testing.T, ctx context.Context, repo *Repository, id, runID, server, tool, status string, durationMs *int64, createdAt time.Time) {
	t.Helper()
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO tool_calls (id, run_id, agent_id, server_name, tool_name, args_json, args_hash, status, duration_ms, created_at)
		VALUES (?, ?, 'general_assistant', ?, ?, '{}', 'sha256:test', ?, ?, ?)`,
		id, runID, server, tool, status, nonNegativeNullInt64(durationMs), FormatTimestamp(createdAt)); err != nil {
		t.Fatalf("insert tool call: %v", err)
	}
}

func insertTelemetryAutomationRun(t *testing.T, ctx context.Context, repo *Repository, runID, automationID, name string, firedAt time.Time) {
	t.Helper()
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO automation_runs (run_id, automation_id, automation_name, allowed_tools_json, fired_at)
		VALUES (?, ?, ?, '[]', ?)`,
		runID, automationID, name, FormatTimestamp(firedAt)); err != nil {
		t.Fatalf("insert automation run: %v", err)
	}
}

func insertTelemetryApproval(t *testing.T, ctx context.Context, repo *Repository, id, runID, status string, createdAt time.Time) {
	t.Helper()
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO approvals (id, run_id, agent_id, tool_name, args_json, args_hash, status, expires_at, created_at)
		VALUES (?, ?, 'general_assistant', 'files.create', '{}', 'sha256:test', ?, ?, ?)`,
		id, runID, status, FormatTimestamp(createdAt.Add(time.Minute)), FormatTimestamp(createdAt)); err != nil {
		t.Fatalf("insert approval: %v", err)
	}
}

func insertTelemetryConnection(t *testing.T, ctx context.Context, repo *Repository, id, status string, connectedAt time.Time) {
	t.Helper()
	stamp := FormatTimestamp(connectedAt)
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO integration_connections (
			id, provider, display_name, status, granted_scopes_json,
			consent_granted_at, connected_at, created_at, updated_at)
		VALUES (?, 'imap', ?, ?, '[]', ?, ?, ?, ?)`,
		id, "account "+id, status, stamp, stamp, stamp, stamp); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
}

func intPointer(value int64) *int64 { return &value }
