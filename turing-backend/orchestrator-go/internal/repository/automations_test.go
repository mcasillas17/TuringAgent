package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

var automationDefaults = AutomationRunDefaults{
	AgentID:       "general_assistant",
	ModelProvider: "ollama",
	Model:         "qwen2.5:7b",
}

func everyFiveMinutes() Schedule {
	return Schedule{Kind: ScheduleInterval, Interval: 5 * time.Minute}
}

func TestCreateAutomationTrimsBoundsAndRejectsEmptyFields(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name:     "  Morning digest  ",
		Prompt:   "  Summarise the sandbox.  ",
		Schedule: everyFiveMinutes(),
	})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}
	if automation.Name != "Morning digest" || automation.Prompt != "Summarise the sandbox." {
		t.Fatalf("stored %q / %q, want both trimmed", automation.Name, automation.Prompt)
	}

	if _, err := repo.CreateAutomation(ctx, AutomationInput{Name: "  ", Prompt: "x", Schedule: everyFiveMinutes()}); !errors.Is(err, ErrAutomationNameEmpty) {
		t.Fatalf("empty name error = %v, want ErrAutomationNameEmpty", err)
	}
	// An automation with no prompt would fire on time and send nothing, which
	// is indistinguishable from the scheduler being broken.
	if _, err := repo.CreateAutomation(ctx, AutomationInput{Name: "Named", Prompt: " \n ", Schedule: everyFiveMinutes()}); !errors.Is(err, ErrAutomationNoPrompt) {
		t.Fatalf("empty prompt error = %v, want ErrAutomationNoPrompt", err)
	}
	if _, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: strings.Repeat("n", maxAutomationNameRunes+1), Prompt: "x", Schedule: everyFiveMinutes(),
	}); !errors.Is(err, ErrAutomationNameTooLong) {
		t.Fatalf("long name error = %v, want ErrAutomationNameTooLong", err)
	}
	if _, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Long prompt", Prompt: strings.Repeat("p", maxAutomationPromptRunes+1), Schedule: everyFiveMinutes(),
	}); !errors.Is(err, ErrAutomationPromptLong) {
		t.Fatalf("long prompt error = %v, want ErrAutomationPromptLong", err)
	}
}

func TestCreateAutomationRejectsDuplicateNamesRegardlessOfCase(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	if _, err := repo.CreateAutomation(ctx, AutomationInput{Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes()}); err != nil {
		t.Fatalf("create automation: %v", err)
	}
	if _, err := repo.CreateAutomation(ctx, AutomationInput{Name: "digest", Prompt: "y", Schedule: everyFiveMinutes()}); !errors.Is(err, ErrAutomationNameTaken) {
		t.Fatalf("duplicate error = %v, want ErrAutomationNameTaken", err)
	}
}

func TestAutomationAllowlistRefusesIntegrationTools(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	_, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "GitHub digest", Prompt: "List issues", Schedule: everyFiveMinutes(),
		AllowedTools: []AutomationTool{{ServerName: "integrations", ToolName: "github.list_issues"}},
	})
	if !errors.Is(err, ErrAutomationIntegrationToolUnsupported) {
		t.Fatalf("error = %v, want ErrAutomationIntegrationToolUnsupported", err)
	}
}

func TestCreateAutomationRejectsUnusableSchedules(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	cases := []struct {
		name     string
		schedule Schedule
		want     error
	}{
		{"no kind", Schedule{}, ErrScheduleKindUnknown},
		{"sub-minute interval", Schedule{Kind: ScheduleInterval, Interval: time.Second}, ErrScheduleIntervalRange},
		{"month-long interval", Schedule{Kind: ScheduleInterval, Interval: 30 * 24 * time.Hour}, ErrScheduleIntervalRange},
		{"minute past the day", Schedule{Kind: ScheduleDaily, DailyMinuteUTC: 1440}, ErrScheduleDailyMinute},
		{"negative minute", Schedule{Kind: ScheduleDaily, DailyMinuteUTC: -1}, ErrScheduleDailyMinute},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := repo.CreateAutomation(ctx, AutomationInput{Name: testCase.name, Prompt: "x", Schedule: testCase.schedule})
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestAllowedToolsAreDeduplicatedOrderedAndValidated(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(),
		AllowedTools: []AutomationTool{
			{ServerName: "files", ToolName: "files.update"},
			{ServerName: "files", ToolName: "files.create"},
			{ServerName: "files", ToolName: "files.create"},
		},
	})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}
	want := []AutomationTool{
		{ServerName: "files", ToolName: "files.create"},
		{ServerName: "files", ToolName: "files.update"},
	}
	if len(automation.AllowedTools) != len(want) {
		t.Fatalf("allowed tools = %+v, want %+v", automation.AllowedTools, want)
	}
	for index := range want {
		if automation.AllowedTools[index] != want[index] {
			t.Fatalf("allowed tools = %+v, want %+v", automation.AllowedTools, want)
		}
	}

	if _, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Half named", Prompt: "x", Schedule: everyFiveMinutes(),
		AllowedTools: []AutomationTool{{ServerName: "files", ToolName: "  "}},
	}); !errors.Is(err, ErrAutomationToolInvalid) {
		t.Fatalf("half-named tool error = %v, want ErrAutomationToolInvalid", err)
	}

	many := make([]AutomationTool, 0, maxAutomationAllowedTools+1)
	for index := 0; index <= maxAutomationAllowedTools; index++ {
		many = append(many, AutomationTool{ServerName: "files", ToolName: string(rune('a'+index%26)) + strings.Repeat("x", index)})
	}
	if _, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Too many", Prompt: "x", Schedule: everyFiveMinutes(), AllowedTools: many,
	}); !errors.Is(err, ErrAutomationTooManyTools) {
		t.Fatalf("oversized allowlist error = %v, want ErrAutomationTooManyTools", err)
	}
}

// A disabled automation has no next run. Storing one anyway would mean
// re-enabling it a month later fires it immediately, for a schedule it slept
// through.
func TestDisablingClearsTheNextRunAndEnablingArmsAFreshOne(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true,
	})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}
	if automation.NextDueAt == "" {
		t.Fatal("an enabled automation has no next run time")
	}

	disabled, err := repo.SetAutomationEnabled(ctx, automation.AutomationID, false)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if disabled.Enabled || disabled.NextDueAt != "" {
		t.Fatalf("disabled automation = enabled %v next %q, want false and empty", disabled.Enabled, disabled.NextDueAt)
	}

	reenabled, err := repo.SetAutomationEnabled(ctx, automation.AutomationID, true)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !reenabled.Enabled || reenabled.NextDueAt == "" {
		t.Fatalf("re-enabled automation = enabled %v next %q", reenabled.Enabled, reenabled.NextDueAt)
	}
	next, err := time.Parse(time.RFC3339Nano, reenabled.NextDueAt)
	if err != nil {
		t.Fatal(err)
	}
	if !next.After(time.Now().UTC()) {
		t.Fatalf("re-enabled next run %s is not in the future", next)
	}
}

// Renaming an automation or narrowing its allowlist must not push its next run
// forward: that would be an edit quietly cancelling a run the user expected.
func TestUpdatingWithoutChangingTheScheduleKeepsTheNextRun(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true,
	})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}
	renamed, err := repo.UpdateAutomation(ctx, automation.AutomationID, AutomationInput{
		Name: "Digest, renamed", Prompt: "y", Schedule: everyFiveMinutes(),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if renamed.NextDueAt != automation.NextDueAt {
		t.Fatalf("next run moved from %q to %q on a rename", automation.NextDueAt, renamed.NextDueAt)
	}

	rescheduled, err := repo.UpdateAutomation(ctx, automation.AutomationID, AutomationInput{
		Name: "Digest, renamed", Prompt: "y", Schedule: Schedule{Kind: ScheduleInterval, Interval: time.Hour},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if rescheduled.NextDueAt == automation.NextDueAt {
		t.Fatal("changing the schedule left the next run where it was")
	}
}

func TestUpdateAndDeleteReportMissingAutomations(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	if _, err := repo.UpdateAutomation(ctx, "auto_nope", AutomationInput{Name: "n", Prompt: "p", Schedule: everyFiveMinutes()}); !errors.Is(err, ErrAutomationNotFound) {
		t.Fatalf("update error = %v, want ErrAutomationNotFound", err)
	}
	if _, err := repo.SetAutomationEnabled(ctx, "auto_nope", true); !errors.Is(err, ErrAutomationNotFound) {
		t.Fatalf("enable error = %v, want ErrAutomationNotFound", err)
	}
	if err := repo.DeleteAutomation(ctx, "auto_nope"); !errors.Is(err, ErrAutomationNotFound) {
		t.Fatalf("delete error = %v, want ErrAutomationNotFound", err)
	}
}

func TestClaimDueAutomationFiresWhenDueAndNotBefore(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation := mustCreateDueAutomation(t, repo, ctx, "Digest", everyFiveMinutes())
	due := mustParse(t, automation.NextDueAt)

	// One nanosecond early is still early.
	if _, found, err := repo.ClaimDueAutomation(ctx, due.Add(-time.Nanosecond), automationDefaults); err != nil || found {
		t.Fatalf("claim before due = found %v err %v, want not found", found, err)
	}

	fire, found, err := repo.ClaimDueAutomation(ctx, due, automationDefaults)
	if err != nil || !found {
		t.Fatalf("claim at due = found %v err %v, want found", found, err)
	}
	if fire.RunID == "" || fire.SessionID == "" || fire.SessionUpdatedEvent.EventID == "" || fire.QueuedEvent.EventID == "" {
		t.Fatalf("fire = %+v, want a run, a conversation and its committed events", fire)
	}
	if fire.SessionUpdatedEvent.Sequence >= fire.QueuedEvent.Sequence {
		t.Fatalf("session update sequence %d, want before queued event %d",
			fire.SessionUpdatedEvent.Sequence, fire.QueuedEvent.Sequence)
	}
	var title, titleOrigin string
	if err := repo.db.QueryRowContext(ctx,
		`SELECT title, title_origin FROM sessions WHERE id = ?`,
		fire.SessionID).Scan(&title, &titleOrigin); err != nil {
		t.Fatalf("read automation session: %v", err)
	}
	if title != "Digest" || titleOrigin != "explicit" {
		t.Fatalf("automation session title = %q origin = %q, want explicit Digest", title, titleOrigin)
	}
	if payload := decodeSessionUpdatedPayload(t, fire.SessionUpdatedEvent); payload.Title != "Digest" {
		t.Fatalf("session.updated title = %q, want automation name", payload.Title)
	}
	messages, err := repo.ListMessages(ctx, fire.SessionID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 || messages[0].Content != "Summarise the sandbox." {
		t.Fatalf("automation sent %+v, want the saved prompt", messages)
	}
	var nonTextMessages int
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM messages
		WHERE session_id = ? AND content_type <> 'text'
	`, fire.SessionID).Scan(&nonTextMessages); err != nil {
		t.Fatal(err)
	}
	if nonTextMessages != 0 {
		t.Fatalf("automation created %d messages with a non-text content type", nonTextMessages)
	}
}

// The whole anti-duplication argument, tested at the level it is made: a
// second claim at the same instant must find nothing, because the first
// already advanced next_due_at.
func TestClaimDueAutomationDoesNotFireTwiceOnTwoTicks(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation := mustCreateDueAutomation(t, repo, ctx, "Digest", everyFiveMinutes())
	due := mustParse(t, automation.NextDueAt)

	if _, found, err := repo.ClaimDueAutomation(ctx, due, automationDefaults); err != nil || !found {
		t.Fatalf("first claim = found %v err %v", found, err)
	}
	if _, found, err := repo.ClaimDueAutomation(ctx, due, automationDefaults); err != nil || found {
		t.Fatalf("second claim at the same instant = found %v err %v, want not found", found, err)
	}

	// And the schedule really did advance rather than merely being locked.
	reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	next := mustParse(t, reloaded.NextDueAt)
	if !next.Equal(due.Add(5 * time.Minute)) {
		t.Fatalf("next due = %s, want %s", next, due.Add(5*time.Minute))
	}
}

// Restarting the process re-reads next_due_at from storage, so the state that
// prevents a re-fire is the row, not anything the old process held. Claiming
// through a second Repository over the same database is that restart.
func TestClaimDueAutomationSurvivesARestartWithoutRefiring(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	repo := New(database)

	automation := mustCreateDueAutomation(t, repo, ctx, "Digest", everyFiveMinutes())
	due := mustParse(t, automation.NextDueAt)
	if _, found, err := repo.ClaimDueAutomation(ctx, due, automationDefaults); err != nil || !found {
		t.Fatalf("first claim = found %v err %v", found, err)
	}

	restarted := New(database)
	if _, found, err := restarted.ClaimDueAutomation(ctx, due, automationDefaults); err != nil || found {
		t.Fatalf("claim after restart = found %v err %v, want not found", found, err)
	}
}

func TestClaimDueAutomationIgnoresDisabledAutomations(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation := mustCreateDueAutomation(t, repo, ctx, "Digest", everyFiveMinutes())
	due := mustParse(t, automation.NextDueAt)
	if _, err := repo.SetAutomationEnabled(ctx, automation.AutomationID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if _, found, err := repo.ClaimDueAutomation(ctx, due.Add(time.Hour), automationDefaults); err != nil || found {
		t.Fatalf("claim of a disabled automation = found %v err %v, want not found", found, err)
	}
}

// Concurrent claims race on the same row. SQLite serialises the writes, but
// the test exists because the correctness argument is the compare-and-set, not
// the connection pool: exactly one caller may come away with a run.
func TestConcurrentClaimsFireExactlyOnce(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation := mustCreateDueAutomation(t, repo, ctx, "Digest", everyFiveMinutes())
	due := mustParse(t, automation.NextDueAt)

	const claimers = 8
	var wait sync.WaitGroup
	results := make(chan bool, claimers)
	errs := make(chan error, claimers)
	wait.Add(claimers)
	for range claimers {
		go func() {
			defer wait.Done()
			_, found, err := repo.ClaimDueAutomation(ctx, due, automationDefaults)
			if err != nil {
				errs <- err
				return
			}
			results <- found
		}()
	}
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent claim: %v", err)
	}
	fires := 0
	for found := range results {
		if found {
			fires++
		}
	}
	if fires != 1 {
		t.Fatalf("concurrent claims fired %d times, want exactly 1", fires)
	}
}

// Missed occurrences are skipped, not replayed. Three hours of downtime on an
// hourly automation must not produce three runs in a row.
func TestClaimDueAutomationSkipsMissedOccurrencesRatherThanBackfilling(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation := mustCreateDueAutomation(t, repo, ctx, "Hourly", Schedule{Kind: ScheduleInterval, Interval: time.Hour})
	due := mustParse(t, automation.NextDueAt)
	wokeUp := due.Add(3*time.Hour + 10*time.Minute)

	if _, found, err := repo.ClaimDueAutomation(ctx, wokeUp, automationDefaults); err != nil || !found {
		t.Fatalf("first claim = found %v err %v", found, err)
	}
	if _, found, err := repo.ClaimDueAutomation(ctx, wokeUp, automationDefaults); err != nil || found {
		t.Fatalf("backfilled a missed occurrence: found %v err %v", found, err)
	}
	reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	next := mustParse(t, reloaded.NextDueAt)
	if !next.After(wokeUp) {
		t.Fatalf("next due %s is not after %s", next, wokeUp)
	}
}

// The conversation is where the user goes to see what happened, so it must be
// the same one every time rather than a new row in the sidebar per fire.
func TestFiringTwiceReusesTheSameConversation(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation := mustCreateDueAutomation(t, repo, ctx, "Digest", everyFiveMinutes())
	due := mustParse(t, automation.NextDueAt)

	first, _, err := repo.ClaimDueAutomation(ctx, due, automationDefaults)
	if err != nil {
		t.Fatal(err)
	}
	finishRun(t, repo, first.RunID)
	second, found, err := repo.ClaimDueAutomation(ctx, due.Add(5*time.Minute), automationDefaults)
	if err != nil || !found {
		t.Fatalf("second claim = found %v err %v", found, err)
	}
	if first.SessionID != second.SessionID {
		t.Fatalf("second fire used a different conversation: %q then %q", first.SessionID, second.SessionID)
	}
	reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SessionID != first.SessionID || reloaded.LastRunID != second.RunID {
		t.Fatalf("automation session=%q lastRun=%q, want %q / %q", reloaded.SessionID, reloaded.LastRunID, first.SessionID, second.RunID)
	}
}

// Deleting the conversation is a request to forget what was said, not to
// delete the schedule that said it.
func TestDeletingTheConversationLetsTheNextFireMakeAFreshOne(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation := mustCreateDueAutomation(t, repo, ctx, "Digest", everyFiveMinutes())
	due := mustParse(t, automation.NextDueAt)
	first, _, err := repo.ClaimDueAutomation(ctx, due, automationDefaults)
	if err != nil {
		t.Fatal(err)
	}
	finishRun(t, repo, first.RunID)
	if _, err := repo.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, first.SessionID); err != nil {
		t.Fatal(err)
	}

	second, found, err := repo.ClaimDueAutomation(ctx, due.Add(5*time.Minute), automationDefaults)
	if err != nil || !found {
		t.Fatalf("claim after the conversation was deleted = found %v err %v", found, err)
	}
	if second.SessionID == "" || second.SessionID == first.SessionID {
		t.Fatalf("second conversation = %q, want a fresh one (was %q)", second.SessionID, first.SessionID)
	}
}

func TestAutomationUsesFreshConversationWhenPreviousIsWithdrawing(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	automation := mustCreateDueAutomation(
		t,
		repo,
		ctx,
		"Digest",
		everyFiveMinutes(),
	)
	due := mustParse(t, automation.NextDueAt)
	first, found, err := repo.ClaimDueAutomation(ctx, due, automationDefaults)
	if err != nil || !found {
		t.Fatalf("first claim = found %v err %v", found, err)
	}
	finishRun(t, repo, first.RunID)
	beforeWithdrawal, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, first.SessionID); err != nil {
		t.Fatal(err)
	}
	var messagesBefore, runsBefore int
	if err := repo.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM messages WHERE session_id = ?`,
		first.SessionID,
	).Scan(&messagesBefore); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM agent_runs WHERE session_id = ?`,
		first.SessionID,
	).Scan(&runsBefore); err != nil {
		t.Fatal(err)
	}

	second, found, err := repo.ClaimDueAutomation(
		ctx,
		due.Add(5*time.Minute),
		automationDefaults,
	)
	if err != nil || !found {
		t.Fatalf("claim during withdrawal = found %v err %v", found, err)
	}
	if second.SessionID == "" || second.SessionID == first.SessionID {
		t.Fatalf(
			"second conversation = %q, want fresh session after withdrawing %q",
			second.SessionID,
			first.SessionID,
		)
	}
	for table, before := range map[string]int{
		"messages":   messagesBefore,
		"agent_runs": runsBefore,
	} {
		var after int
		if err := repo.db.QueryRowContext(
			ctx,
			"SELECT COUNT(*) FROM "+table+" WHERE session_id = ?",
			first.SessionID,
		).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatalf("%s count = %d, want unchanged %d", table, after, before)
		}
	}
	reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.NextDueAt == beforeWithdrawal.NextDueAt {
		t.Fatalf(
			"next due did not advance after fresh-session fire: %q",
			reloaded.NextDueAt,
		)
	}
	if reloaded.SessionID != second.SessionID {
		t.Fatalf(
			"automation session = %q, want fresh session %q",
			reloaded.SessionID,
			second.SessionID,
		)
	}
}

// The allowlist a run is judged against is the one that existed when it fired.
// Widening it mid-run must not widen what that run may already be doing.
func TestTheRunGrantIsFrozenAtFireTime(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Digest", Prompt: "Summarise the sandbox.", Schedule: everyFiveMinutes(), Enabled: true,
		AllowedTools: []AutomationTool{{ServerName: "files", ToolName: "files.create"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fire, _, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), automationDefaults)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.UpdateAutomation(ctx, automation.AutomationID, AutomationInput{
		Name: "Digest", Prompt: "Summarise the sandbox.", Schedule: everyFiveMinutes(),
		AllowedTools: []AutomationTool{
			{ServerName: "files", ToolName: "files.create"},
			{ServerName: "files", ToolName: "files.update"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	grant, found, err := repo.GetAutomationRunGrant(ctx, fire.RunID)
	if err != nil || !found {
		t.Fatalf("grant = found %v err %v, want found", found, err)
	}
	if !grant.Allows("files", "files.create") {
		t.Fatal("the grant lost a tool it was created with")
	}
	if grant.Allows("files", "files.update") {
		t.Fatal("widening the allowlist reached back into a run already in flight")
	}
	if grant.AutomationName != "Digest" {
		t.Fatalf("grant automation name = %q, want Digest", grant.AutomationName)
	}
}

// A run the user typed has no grant, so nothing about the ordinary "stop and
// ask" path can be reached through this door.
func TestAHandDrivenRunHasNoGrant(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	session, err := repo.CreateSession(ctx, "By hand")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "write a file", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "qwen2.5:7b",
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, found, err := repo.GetAutomationRunGrant(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("a hand-driven run reported a grant: %+v", grant)
	}
}

// Deleting an automation stops future fires, and must not cascade the record
// of what it already did out from under a run still executing.
func TestDeletingAnAutomationLeavesAnInFlightRunItsGrant(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true,
		AllowedTools: []AutomationTool{{ServerName: "files", ToolName: "files.create"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fire, _, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), automationDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAutomation(ctx, automation.AutomationID); err != nil {
		t.Fatal(err)
	}

	grant, found, err := repo.GetAutomationRunGrant(ctx, fire.RunID)
	if err != nil || !found {
		t.Fatalf("grant after delete = found %v err %v, want found", found, err)
	}
	if !grant.Allows("files", "files.create") {
		t.Fatal("the in-flight run lost the consent it was already given")
	}
}

func TestListAutomationsReportsTheLastRunOutcome(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation := mustCreateDueAutomation(t, repo, ctx, "Digest", everyFiveMinutes())
	fire, _, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), automationDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE agent_runs SET status = 'failed', error_message = 'no one was watching' WHERE id = ?`, fire.RunID); err != nil {
		t.Fatal(err)
	}

	listed, err := repo.ListAutomations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed %d automations, want 1", len(listed))
	}
	if listed[0].LastRunStatus != "failed" || listed[0].LastRunError != "no one was watching" {
		t.Fatalf("last run = %q / %q, want failed / no one was watching", listed[0].LastRunStatus, listed[0].LastRunError)
	}
	if listed[0].LastRunAt == "" {
		t.Fatal("a fired automation reports no last run time")
	}
}

// An automation whose previous run is still going passes the occurrence by.
// Firing anyway would queue work faster than it drains, and every queued
// automation job sits ahead of whatever the user types next — so a slow
// automation would quietly stop the user's own conversations from running.
func TestAnUnfinishedPreviousRunSkipsTheOccurrenceInsteadOfQueueingAnother(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation := mustCreateDueAutomation(t, repo, ctx, "Digest", everyFiveMinutes())
	due := mustParse(t, automation.NextDueAt)
	first, _, err := repo.ClaimDueAutomation(ctx, due, automationDefaults)
	if err != nil {
		t.Fatal(err)
	}

	// The first run is still queued, so the next three occurrences pass.
	at := due
	for range 3 {
		at = at.Add(5 * time.Minute)
		fire, found, err := repo.ClaimDueAutomation(ctx, at, automationDefaults)
		if err != nil || !found {
			t.Fatalf("claim while the previous run is unfinished = found %v err %v", found, err)
		}
		if !fire.Skipped || fire.SkippedReason != SkippedPreviousRunUnfinished {
			t.Fatalf("fire = %+v, want a skip for an unfinished previous run", fire)
		}
		if fire.RunID != "" {
			t.Fatalf("a skipped occurrence queued run %q", fire.RunID)
		}
	}
	if got := countAutomationRuns(t, repo, ctx, automation.AutomationID); got != 1 {
		t.Fatalf("queued %d runs while one was unfinished, want 1", got)
	}
	// And it is not stuck: the schedule kept moving, so it does not re-fire
	// three times the moment the run ends.
	reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if !mustParse(t, reloaded.NextDueAt).After(at) {
		t.Fatalf("next due %s did not advance past %s", reloaded.NextDueAt, at)
	}

	finishRun(t, repo, first.RunID)
	fire, found, err := repo.ClaimDueAutomation(ctx, at.Add(10*time.Minute), automationDefaults)
	if err != nil || !found {
		t.Fatalf("claim after the run finished = found %v err %v", found, err)
	}
	if fire.Skipped || fire.RunID == "" {
		t.Fatalf("fire after the previous run finished = %+v, want a real run", fire)
	}
}

func TestUnavailableAutomationRouteAdvancesWithoutPersistingWork(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	automation := mustCreateDueAutomation(t, repo, ctx, "Unavailable", everyFiveMinutes())
	due := mustParse(t, automation.NextDueAt)
	defaults := automationDefaults
	defaults.ValidateRouting = func(context.Context, RoutingRequirements) error {
		return errors.New("no compatible worker")
	}

	fire, found, err := repo.ClaimDueAutomation(ctx, due, defaults)
	if err != nil || !found {
		t.Fatalf("claim unavailable route = found %v err %v", found, err)
	}
	if !fire.Skipped || fire.SkippedReason != SkippedRoutingUnavailable || fire.RunID != "" {
		t.Fatalf("fire = %+v, want routing-unavailable skip", fire)
	}
	for _, table := range []string{"sessions", "messages", "agent_runs", "jobs"} {
		var count int
		if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want no persisted work", table, count)
		}
	}
	var actorType, actorID, action, target, payload string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT actor_type, COALESCE(actor_id, ''), action, COALESCE(target, ''), COALESCE(payload_json, '')
		FROM audit_logs
		WHERE action = 'automation.routing_unavailable' AND target = ?
	`, automation.AutomationID).Scan(&actorType, &actorID, &action, &target, &payload); err != nil {
		t.Fatalf("read durable routing-unavailable occurrence: %v", err)
	}
	if actorType != "automation" || actorID != automation.AutomationID ||
		action != "automation.routing_unavailable" || target != automation.AutomationID ||
		!strings.Contains(payload, `"reason":"routing_unavailable"`) {
		t.Fatalf("routing-unavailable audit = %q/%q/%q/%q %s", actorType, actorID, action, target, payload)
	}
	reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if !mustParse(t, reloaded.NextDueAt).After(due) {
		t.Fatalf("next due = %s, want after %s", reloaded.NextDueAt, due)
	}
}

func TestAutomationFailsItsExistingExternalAgentRouteBeforeWorkerValidation(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	automation := mustCreateDueAutomation(t, repo, ctx, "External route", everyFiveMinutes())
	firstDue := mustParse(t, automation.NextDueAt)
	first, found, err := repo.ClaimDueAutomation(ctx, firstDue, automationDefaults)
	if err != nil || !found || first.Skipped {
		t.Fatalf("first claim = %+v, found %v, err %v", first, found, err)
	}
	finishRun(t, repo, first.RunID)
	reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := repo.CreateExternalAgent(ctx, ExternalAgentInput{
		DisplayName: "External", Provider: "anthropic", BaseURL: "https://example.com",
		Model: "external-model", CredentialRef: "external",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSessionAgent(ctx, reloaded.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	countsBefore := map[string]int{}
	for _, table := range []string{"sessions", "messages", "agent_runs", "jobs"} {
		var count int
		if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		countsBefore[table] = count
	}
	defaults := automationDefaults
	defaults.ValidateRouting = func(_ context.Context, route RoutingRequirements) error {
		t.Fatalf("remote automation reached worker validation: %+v", route)
		return nil
	}

	fire, found, err := repo.ClaimDueAutomation(ctx, mustParse(t, reloaded.NextDueAt), defaults)
	if err != nil || !found {
		t.Fatalf("external route claim = found %v err %v", found, err)
	}
	if fire.Skipped || !fire.Failed ||
		fire.FailureCode != AutomationFailureRemoteEgressRequiresInteractiveConsent ||
		fire.RunID != "" {
		t.Fatalf("fire = %+v, want durable remote-egress failure", fire)
	}
	for table, before := range countsBefore {
		var after int
		if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatalf("%s count = %d, want unchanged %d", table, after, before)
		}
	}
}

func TestAutomationValidatesFreshDefaultRouteWhenPreviousSessionIsWithdrawing(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	automation := mustCreateDueAutomation(t, repo, ctx, "Fresh route", everyFiveMinutes())
	firstDue := mustParse(t, automation.NextDueAt)
	first, found, err := repo.ClaimDueAutomation(ctx, firstDue, automationDefaults)
	if err != nil || !found || first.Skipped {
		t.Fatalf("first claim = %+v, found %v, err %v", first, found, err)
	}
	finishRun(t, repo, first.RunID)
	agent, err := repo.CreateExternalAgent(ctx, ExternalAgentInput{
		DisplayName: "External", Provider: "anthropic", BaseURL: "https://example.com",
		Model: "external-model", CredentialRef: "external",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSessionAgent(ctx, first.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, first.SessionID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	defaults := automationDefaults
	validated := false
	defaults.ValidateRouting = func(_ context.Context, route RoutingRequirements) error {
		validated = true
		if route.ExternalAgent ||
			route.ModelProvider != automationDefaults.ModelProvider ||
			route.Model != automationDefaults.Model {
			t.Fatalf("validated route = %+v, want fresh default route", route)
		}
		return nil
	}

	fire, found, err := repo.ClaimDueAutomation(
		ctx,
		mustParse(t, reloaded.NextDueAt),
		defaults,
	)
	if err != nil || !found || fire.Skipped {
		t.Fatalf("fresh route claim = %+v, found %v, err %v", fire, found, err)
	}
	if !validated {
		t.Fatal("fresh default route was not validated")
	}
	if fire.SessionID == first.SessionID {
		t.Fatalf("fire reused withdrawing session %q", first.SessionID)
	}
}

func TestClaimDueAutomationRecordsRemoteEgressFailureAuditWithoutRun(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	automation := mustCreateDueAutomation(t, repo, ctx, "External event", everyFiveMinutes())
	session, err := repo.CreateSession(ctx, automation.Name)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.db.ExecContext(ctx,
		`UPDATE automations SET session_id = ? WHERE id = ?`,
		session.SessionID, automation.AutomationID); err != nil {
		t.Fatal(err)
	}
	agent, err := repo.CreateExternalAgent(ctx, ExternalAgentInput{
		DisplayName: "External", Provider: "anthropic", BaseURL: "https://example.com",
		Model: "external-model", CredentialRef: "external",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}

	fire, found, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), automationDefaults)
	if err != nil || !found || fire.Skipped {
		t.Fatalf("claim = %+v, found %v, err %v", fire, found, err)
	}
	if !fire.Failed || fire.FailureCode != AutomationFailureRemoteEgressRequiresInteractiveConsent ||
		fire.RunID != "" || fire.JobID != "" {
		t.Fatalf("fire = %+v, want remote-egress failure without a run", fire)
	}
	var auditCount int
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM audit_logs
		WHERE actor_type = 'automation' AND actor_id = ?
			AND action = 'automation.remote_egress_blocked'
	`, automation.AutomationID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("remote egress audit rows = %d, want 1", auditCount)
	}
	var lastRunAt sql.NullString
	if err := repo.db.QueryRowContext(ctx,
		`SELECT last_run_at FROM automations WHERE id = ?`, automation.AutomationID).Scan(&lastRunAt); err != nil {
		t.Fatal(err)
	}
	if lastRunAt.Valid {
		t.Fatalf("blocked occurrence set last_run_at = %q", lastRunAt.String)
	}
	reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LastRunStatus != "" || reloaded.LastRunError != "" {
		t.Fatalf("blocked occurrence rewrote last run = %q/%q", reloaded.LastRunStatus, reloaded.LastRunError)
	}
	if reloaded.LastOccurrenceFailureCode != AutomationFailureRemoteEgressRequiresInteractiveConsent ||
		reloaded.LastOccurrenceFailedAt == "" {
		t.Fatalf("durable blocked occurrence = %q/%q",
			reloaded.LastOccurrenceFailureCode, reloaded.LastOccurrenceFailedAt)
	}
	if err := repo.ClearSessionAgent(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}
	successful, found, err := repo.ClaimDueAutomation(
		ctx,
		mustParse(t, reloaded.NextDueAt),
		automationDefaults,
	)
	if err != nil || !found || successful.Failed || successful.Skipped {
		t.Fatalf("local recovery fire = %+v found=%v err=%v", successful, found, err)
	}
	finishRun(t, repo, successful.RunID)
	recovered, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.LastRunStatus != "completed" ||
		recovered.LastOccurrenceFailureCode != "" ||
		recovered.LastOccurrenceFailedAt != "" {
		t.Fatalf("successful run left stale blocked state: %+v", recovered)
	}
}

func TestClaimDueAutomationAdvancesPastLegacyInsecureExternalRoute(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	automation := mustCreateDueAutomation(t, repo, ctx, "Legacy external", everyFiveMinutes())
	session, err := repo.CreateSession(ctx, automation.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE automations SET session_id = ? WHERE id = ?`,
		session.SessionID, automation.AutomationID); err != nil {
		t.Fatal(err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE external_agents SET base_url = 'http://host.docker.internal:4000/v1' WHERE id = ?`,
		agent.AgentID); err != nil {
		t.Fatal(err)
	}
	due := automation.NextDueAt
	fire, found, err := repo.ClaimDueAutomation(ctx, mustParse(t, due), automationDefaults)
	if err != nil || !found {
		t.Fatalf("claim = %+v found=%v err=%v", fire, found, err)
	}
	if !fire.Failed || fire.FailureCode != AutomationFailureRemoteEgressConfigurationInvalid {
		t.Fatalf("fire = %+v, want durable configuration failure", fire)
	}
	reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.NextDueAt == due {
		t.Fatal("legacy insecure route did not advance next_due_at")
	}
}

// A row whose schedule cannot be read has no next due time to advance to, so
// leaving it enabled would re-select it on every tick and starve every
// automation behind it.
func TestAnUnreadableScheduleDisablesTheAutomationRatherThanWedgingTheRest(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	poisoned := mustCreateDueAutomation(t, repo, ctx, "Poisoned", everyFiveMinutes())
	healthy := mustCreateDueAutomation(t, repo, ctx, "Healthy", everyFiveMinutes())
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE automations SET schedule_kind = 'lunar', interval_seconds = NULL WHERE id = ?`,
		poisoned.AutomationID); err != nil {
		t.Fatal(err)
	}
	at := mustParse(t, healthy.NextDueAt).Add(time.Minute)

	fire, found, err := repo.ClaimDueAutomation(ctx, at, automationDefaults)
	if err != nil || !found {
		t.Fatalf("claim of the poisoned row = found %v err %v, want a reported skip", found, err)
	}
	if !fire.Skipped || fire.SkippedReason != SkippedScheduleUnreadable {
		t.Fatalf("fire = %+v, want a skip for an unreadable schedule", fire)
	}

	// The next claim reaches the automation that was behind it.
	next, found, err := repo.ClaimDueAutomation(ctx, at, automationDefaults)
	if err != nil || !found {
		t.Fatalf("claim after the poisoned row = found %v err %v", found, err)
	}
	if next.Skipped || next.AutomationID != healthy.AutomationID {
		t.Fatalf("fire = %+v, want the healthy automation to run", next)
	}

	// And the poisoned one is off, which is visible on its card rather than
	// only in a log line.
	reloaded, err := repo.GetAutomation(ctx, poisoned.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Enabled || reloaded.NextDueAt != "" {
		t.Fatalf("poisoned automation = enabled %v next %q, want disabled with no next run",
			reloaded.Enabled, reloaded.NextDueAt)
	}
}

// The daily path goes through different columns and a different next-due
// branch than the interval one, and only an end-to-end claim proves the two
// halves agree.
func TestADailyAutomationFiresAndRearmsForTheFollowingDay(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Daily digest", Prompt: "Summarise the sandbox.",
		Schedule: Schedule{Kind: ScheduleDaily, DailyMinuteUTC: 7*60 + 30}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	due := mustParse(t, automation.NextDueAt)
	if due.Hour() != 7 || due.Minute() != 30 {
		t.Fatalf("first due %s is not at 07:30Z", due)
	}

	fire, found, err := repo.ClaimDueAutomation(ctx, due, automationDefaults)
	if err != nil || !found || fire.Skipped {
		t.Fatalf("claim = found %v skipped %v err %v", found, fire.Skipped, err)
	}
	reloaded, err := repo.GetAutomation(ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Schedule.Kind != ScheduleDaily || reloaded.Schedule.DailyMinuteUTC != 7*60+30 {
		t.Fatalf("stored schedule = %+v, want a daily 07:30Z", reloaded.Schedule)
	}
	next := mustParse(t, reloaded.NextDueAt)
	if !next.Equal(due.AddDate(0, 0, 1)) {
		t.Fatalf("next due = %s, want %s", next, due.AddDate(0, 0, 1))
	}
}

func countAutomationRuns(t *testing.T, repo *Repository, ctx context.Context, automationID string) int {
	t.Helper()
	var count int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM automation_runs WHERE automation_id = ?`, automationID).Scan(&count); err != nil {
		t.Fatalf("count automation runs: %v", err)
	}
	return count
}

func mustCreateDueAutomation(t *testing.T, repo *Repository, ctx context.Context, name string, schedule Schedule) Automation {
	t.Helper()
	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: name, Prompt: "Summarise the sandbox.", Schedule: schedule, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}
	if automation.NextDueAt == "" {
		t.Fatal("an enabled automation has no next run time")
	}
	return automation
}

func mustParse(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed.UTC()
}
