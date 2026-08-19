package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

// The prompt is resent verbatim on every fire and travels into a local model's
// finite context, so it is bounded here rather than in whichever client
// happens to be editing it.
const (
	maxAutomationNameRunes   = 120
	maxAutomationPromptRunes = 8000
	// An allowlist long enough to be unreadable is not consent. The number is a
	// bound on a list a person is expected to check before saving, not on
	// anything the storage cares about.
	maxAutomationAllowedTools = 32
)

var (
	ErrAutomationNotFound     = errors.New("automation not found")
	ErrAutomationNameTaken    = errors.New("an automation with that name already exists")
	ErrAutomationNameEmpty    = errors.New("automation name is required")
	ErrAutomationNameTooLong  = errors.New("automation name is too long")
	ErrAutomationNoPrompt     = errors.New("automation prompt is required")
	ErrAutomationPromptLong   = errors.New("automation prompt is too long")
	ErrAutomationToolInvalid  = errors.New("an allowed tool needs both a server and a tool name")
	ErrAutomationTooManyTools = errors.New("too many allowed tools")
)

// AutomationTool names a tool by the same (server, tool) pair the
// orchestrator's policy lookup uses, so an allowlist entry cannot match a
// same-named tool served by something else.
type AutomationTool struct {
	ServerName string `json:"serverName"`
	ToolName   string `json:"toolName"`
}

type Automation struct {
	AutomationID string
	Name         string
	Prompt       string
	Schedule     Schedule
	Enabled      bool
	AllowedTools []AutomationTool
	// Empty when disabled.
	NextDueAt string
	LastRunAt string
	LastRunID string
	// Empty until the automation has fired once.
	SessionID string
	// The outcome of LastRunID, joined from agent_runs so "what happened while
	// I was asleep" is answerable without opening the conversation.
	LastRunStatus string
	LastRunError  string
	CreatedAt     string
	UpdatedAt     string
}

type AutomationInput struct {
	Name         string
	Prompt       string
	Schedule     Schedule
	Enabled      bool
	AllowedTools []AutomationTool
}

// AutomationRunGrant is what an unattended run was permitted to do, as frozen
// at the moment it fired.
type AutomationRunGrant struct {
	AutomationID   string
	AutomationName string
	AllowedTools   []AutomationTool
}

// Allows answers the only question the tool-policy path asks of it. There is
// no wildcard branch, and adding one would have to be written here on purpose.
func (g AutomationRunGrant) Allows(serverName string, toolName string) bool {
	for _, tool := range g.AllowedTools {
		if tool.ServerName == serverName && tool.ToolName == toolName {
			return true
		}
	}
	return false
}

// AutomationRunDefaults is the routing an automation's run inherits. The
// scheduler has no user in front of it to pick a provider, so the process
// defaults stand in.
type AutomationRunDefaults struct {
	AgentID       string
	ModelProvider string
	Model         string
}

// AutomationFire describes what one claim did. A claim either queues a run or
// deliberately declines to, and the caller has to tell those apart without
// mistaking "declined" for "nothing was due" — the latter is how it knows to
// stop looping.
type AutomationFire struct {
	AutomationID        string
	Name                string
	SessionID           string
	RunID               string
	JobID               string
	TraceID             string
	SessionUpdatedEvent Event
	QueuedEvent         Event

	// Set when the occurrence was passed over rather than run. The schedule
	// still moved on, so the automation is not stuck; SkippedReason says why.
	Skipped       bool
	SkippedReason string
}

// Reasons an occurrence is passed over.
const (
	// The previous run has not finished. Firing anyway would queue work faster
	// than it drains, and a queued automation job sits ahead of whatever the
	// user types next — so a slow automation would quietly stop the user's own
	// conversations from running at all.
	SkippedPreviousRunUnfinished = "previous_run_unfinished"
	// The stored schedule could not be read. Nothing can compute a next due
	// time from it, so the automation is disabled rather than left to be
	// re-selected by every future tick, which would block every automation
	// behind it forever.
	SkippedScheduleUnreadable = "schedule_unreadable"
)

func validateAutomation(input AutomationInput) (AutomationInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Prompt = strings.TrimSpace(input.Prompt)
	switch {
	case input.Name == "":
		return AutomationInput{}, ErrAutomationNameEmpty
	case len([]rune(input.Name)) > maxAutomationNameRunes:
		return AutomationInput{}, ErrAutomationNameTooLong
	case input.Prompt == "":
		return AutomationInput{}, ErrAutomationNoPrompt
	case len([]rune(input.Prompt)) > maxAutomationPromptRunes:
		return AutomationInput{}, ErrAutomationPromptLong
	}
	if err := input.Schedule.Validate(); err != nil {
		return AutomationInput{}, err
	}
	tools, err := normalizeAllowedTools(input.AllowedTools)
	if err != nil {
		return AutomationInput{}, err
	}
	input.AllowedTools = tools
	return input, nil
}

// normalizeAllowedTools trims, de-duplicates and orders the allowlist so the
// sentence the UI shows before saving and the set the scheduler freezes are
// the same set, in the same order, every time.
func normalizeAllowedTools(tools []AutomationTool) ([]AutomationTool, error) {
	seen := make(map[AutomationTool]struct{}, len(tools))
	normalized := make([]AutomationTool, 0, len(tools))
	for _, tool := range tools {
		tool.ServerName = strings.TrimSpace(tool.ServerName)
		tool.ToolName = strings.TrimSpace(tool.ToolName)
		if tool.ServerName == "" || tool.ToolName == "" {
			return nil, ErrAutomationToolInvalid
		}
		if _, duplicate := seen[tool]; duplicate {
			continue
		}
		seen[tool] = struct{}{}
		normalized = append(normalized, tool)
	}
	if len(normalized) > maxAutomationAllowedTools {
		return nil, ErrAutomationTooManyTools
	}
	sort.Slice(normalized, func(i int, j int) bool {
		if normalized[i].ServerName == normalized[j].ServerName {
			return normalized[i].ToolName < normalized[j].ToolName
		}
		return normalized[i].ServerName < normalized[j].ServerName
	})
	return normalized, nil
}

func (r *Repository) CreateAutomation(ctx context.Context, input AutomationInput) (Automation, error) {
	input, err := validateAutomation(input)
	if err != nil {
		return Automation{}, err
	}
	createdAt := now()
	automationID := ids.New("auto")
	nextDue := ""
	if input.Enabled {
		due, err := input.Schedule.FirstDueAfter(time.Now())
		if err != nil {
			return Automation{}, err
		}
		nextDue = FormatTimestamp(due)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Automation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	intervalSeconds, dailyMinute := scheduleColumns(input.Schedule)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO automations (id, name, prompt, schedule_kind, interval_seconds, daily_minute_utc, enabled, next_due_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, automationID, input.Name, input.Prompt, string(input.Schedule.Kind), intervalSeconds, dailyMinute,
		boolToInt(input.Enabled), nullableText(nextDue), createdAt, createdAt); err != nil {
		if isUniqueViolation(err) {
			return Automation{}, ErrAutomationNameTaken
		}
		return Automation{}, err
	}
	if err := replaceAllowedToolsTx(ctx, tx, automationID, input.AllowedTools); err != nil {
		return Automation{}, err
	}
	automation, err := automationByIDTx(ctx, tx, automationID)
	if err != nil {
		return Automation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Automation{}, err
	}
	return automation, nil
}

func (r *Repository) UpdateAutomation(ctx context.Context, automationID string, input AutomationInput) (Automation, error) {
	input, err := validateAutomation(input)
	if err != nil {
		return Automation{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Automation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	existing, err := automationByIDTx(ctx, tx, automationID)
	if err != nil {
		return Automation{}, err
	}
	// The next due time is only recomputed when the schedule itself changed.
	// Renaming an automation or narrowing its allowlist should not silently
	// push its next run forward — that would be an edit quietly cancelling a
	// run the user was expecting.
	nextDue := existing.NextDueAt
	// The empty check is a repair, not a normal path: an enabled automation
	// with no next run would sit there looking armed and never fire, and an
	// edit is a reasonable place to notice.
	if existing.Enabled && (existing.Schedule != input.Schedule || nextDue == "") {
		due, err := input.Schedule.FirstDueAfter(time.Now())
		if err != nil {
			return Automation{}, err
		}
		nextDue = FormatTimestamp(due)
	}
	intervalSeconds, dailyMinute := scheduleColumns(input.Schedule)
	result, err := tx.ExecContext(ctx, `
		UPDATE automations
		SET name = ?, prompt = ?, schedule_kind = ?, interval_seconds = ?, daily_minute_utc = ?, next_due_at = ?, updated_at = ?
		WHERE id = ?
	`, input.Name, input.Prompt, string(input.Schedule.Kind), intervalSeconds, dailyMinute, nullableText(nextDue), now(), automationID)
	if err != nil {
		if isUniqueViolation(err) {
			return Automation{}, ErrAutomationNameTaken
		}
		return Automation{}, err
	}
	if err := expectOneRowErr(result, ErrAutomationNotFound); err != nil {
		return Automation{}, err
	}
	if err := replaceAllowedToolsTx(ctx, tx, automationID, input.AllowedTools); err != nil {
		return Automation{}, err
	}
	automation, err := automationByIDTx(ctx, tx, automationID)
	if err != nil {
		return Automation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Automation{}, err
	}
	return automation, nil
}

// SetAutomationEnabled is the only place next_due_at is armed or cleared
// wholesale. Disabling clears it rather than remembering it: a disabled
// automation has no next run, and re-enabling one after a month should not
// fire it immediately for every occurrence it slept through.
func (r *Repository) SetAutomationEnabled(ctx context.Context, automationID string, enabled bool) (Automation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Automation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	existing, err := automationByIDTx(ctx, tx, automationID)
	if err != nil {
		return Automation{}, err
	}
	nextDue := ""
	if enabled {
		if existing.Enabled && existing.NextDueAt != "" {
			nextDue = existing.NextDueAt
		} else {
			due, err := existing.Schedule.FirstDueAfter(time.Now())
			if err != nil {
				return Automation{}, err
			}
			nextDue = FormatTimestamp(due)
		}
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE automations SET enabled = ?, next_due_at = ?, updated_at = ? WHERE id = ?`,
		boolToInt(enabled), nullableText(nextDue), now(), automationID)
	if err != nil {
		return Automation{}, err
	}
	if err := expectOneRowErr(result, ErrAutomationNotFound); err != nil {
		return Automation{}, err
	}
	automation, err := automationByIDTx(ctx, tx, automationID)
	if err != nil {
		return Automation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Automation{}, err
	}
	return automation, nil
}

// DeleteAutomation stops future fires. It deliberately does not touch a run
// already underway, or the conversation the automation produced: the record of
// what it did while nobody was watching outlives the schedule that caused it.
func (r *Repository) DeleteAutomation(ctx context.Context, automationID string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM automations WHERE id = ?`, automationID)
	if err != nil {
		return err
	}
	return expectOneRowErr(result, ErrAutomationNotFound)
}

func (r *Repository) GetAutomation(ctx context.Context, automationID string) (Automation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Automation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	automation, err := automationByIDTx(ctx, tx, automationID)
	if err != nil {
		return Automation{}, err
	}
	return automation, tx.Commit()
}

// ListAutomations orders by name so the list reads the same way every visit.
func (r *Repository) ListAutomations(ctx context.Context) ([]Automation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, automationSelect+` ORDER BY a.name COLLATE NOCASE, a.id`)
	if err != nil {
		return nil, err
	}
	automations, err := scanAutomations(rows)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	for index := range automations {
		tools, err := allowedToolsTx(ctx, tx, automations[index].AutomationID)
		if err != nil {
			return nil, err
		}
		automations[index].AllowedTools = tools
	}
	return automations, tx.Commit()
}

// ClaimDueAutomation fires at most one due automation, and does the claiming
// and the queueing in a single transaction.
//
// That single transaction is the anti-duplication argument:
//   - Two ticks racing cannot both fire one automation. The first commits an
//     advanced next_due_at, and the second's select — inside its own
//     transaction, after the first's — no longer sees the row as due.
//   - A crash cannot fire one twice, because the run and the advanced
//     next_due_at commit together or not at all. There is no window where the
//     schedule says "already fired" but no run exists, or the reverse.
//
// The compare-and-set on next_due_at in the claiming UPDATE is a second guard
// rather than the load-bearing one: SQLite serialises writers here, so the
// select already cannot observe a stale value. It is written anyway because
// the correctness of the claim should not depend on a pool setting made
// elsewhere for another reason — the same argument the enqueue path makes for
// its untitled-session check.
//
// It returns found=false when nothing is due, which is also how the caller
// knows to stop looping.
func (r *Repository) ClaimDueAutomation(ctx context.Context, at time.Time, defaults AutomationRunDefaults) (AutomationFire, bool, error) {
	at = at.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AutomationFire{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var automationID, name, prompt, scheduleKind, dueAt string
	var intervalSeconds, dailyMinute sql.NullInt64
	var sessionID, lastRunStatus sql.NullString
	// next_due_at is compared and ordered as plain text rather than through
	// sqliteTimestampNanos: every value in this column is written by
	// FormatTimestamp, whose layout is fixed-width and zero-padded, so lexical
	// order IS chronological order — and only a bare column can use the
	// automations_due index.
	dueQuery := automationDueSelect + `
		WHERE a.enabled = 1
			AND a.next_due_at IS NOT NULL
			AND a.next_due_at <= ?
		ORDER BY a.next_due_at, a.id
		LIMIT 1`
	err = tx.QueryRowContext(ctx, dueQuery, FormatTimestamp(at)).Scan(
		&automationID, &name, &prompt, &scheduleKind, &intervalSeconds, &dailyMinute, &dueAt, &sessionID, &lastRunStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return AutomationFire{}, false, nil
	}
	if err != nil {
		return AutomationFire{}, false, err
	}
	firedAt := FormatTimestamp(at)
	schedule, scheduleErr := scheduleFromColumns(scheduleKind, intervalSeconds, dailyMinute)
	firedDue, parseErr := time.Parse(time.RFC3339Nano, dueAt)
	if scheduleErr != nil || parseErr != nil {
		// Disabled rather than skipped: there is no next due time to advance
		// to, so leaving it enabled would re-select this same row on every
		// tick and starve every automation behind it. Disabling shows up on
		// the card, which is where a person can act on it.
		if _, err := tx.ExecContext(ctx,
			`UPDATE automations SET enabled = 0, next_due_at = NULL, updated_at = ? WHERE id = ?`,
			firedAt, automationID); err != nil {
			return AutomationFire{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return AutomationFire{}, false, err
		}
		return AutomationFire{
			AutomationID: automationID, Name: name,
			Skipped: true, SkippedReason: SkippedScheduleUnreadable,
		}, true, nil
	}
	nextDue, err := schedule.NextDueAfterFiring(firedDue, at)
	if err != nil {
		return AutomationFire{}, false, err
	}

	// An automation whose previous run has not finished passes this occurrence
	// by. The schedule still advances, so it is neither stuck nor primed to
	// fire the instant the run ends: it simply runs no more often than it can
	// finish.
	if lastRunStatus.Valid && !isTerminalRunStatus(lastRunStatus.String) {
		result, err := tx.ExecContext(ctx, `
			UPDATE automations
			SET next_due_at = ?, updated_at = ?
			WHERE id = ? AND enabled = 1 AND next_due_at = ?
		`, FormatTimestamp(nextDue), firedAt, automationID, dueAt)
		if err != nil {
			return AutomationFire{}, false, err
		}
		skipped, err := result.RowsAffected()
		if err != nil {
			return AutomationFire{}, false, err
		}
		if skipped != 1 {
			return AutomationFire{}, false, nil
		}
		if err := tx.Commit(); err != nil {
			return AutomationFire{}, false, err
		}
		return AutomationFire{
			AutomationID: automationID, Name: name,
			Skipped: true, SkippedReason: SkippedPreviousRunUnfinished,
		}, true, nil
	}
	// The compare-and-set described above: next_due_at must still be the value
	// this transaction read.
	result, err := tx.ExecContext(ctx, `
		UPDATE automations
		SET next_due_at = ?, last_run_at = ?, updated_at = ?
		WHERE id = ? AND enabled = 1 AND next_due_at = ?
	`, FormatTimestamp(nextDue), firedAt, firedAt, automationID, dueAt)
	if err != nil {
		return AutomationFire{}, false, err
	}
	claimed, err := result.RowsAffected()
	if err != nil {
		return AutomationFire{}, false, err
	}
	if claimed != 1 {
		return AutomationFire{}, false, nil
	}

	runSessionID, err := automationSessionTx(ctx, tx, automationID, name, sessionID)
	if err != nil {
		return AutomationFire{}, false, err
	}
	enqueued, err := enqueueUserMessageTx(ctx, tx, EnqueueUserMessageInput{
		SessionID:     runSessionID,
		Content:       prompt,
		AgentID:       defaults.AgentID,
		ModelProvider: defaults.ModelProvider,
		Model:         defaults.Model,
	})
	if err != nil {
		return AutomationFire{}, false, err
	}
	tools, err := allowedToolsTx(ctx, tx, automationID)
	if err != nil {
		return AutomationFire{}, false, err
	}
	// Frozen here rather than read live when a tool asks: editing the
	// allowlist while this run is in flight must not change what it may
	// already be doing.
	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return AutomationFire{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO automation_runs (run_id, automation_id, automation_name, allowed_tools_json, fired_at)
		VALUES (?, ?, ?, ?, ?)
	`, enqueued.RunID, automationID, name, string(toolsJSON), firedAt); err != nil {
		return AutomationFire{}, false, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE automations SET last_run_id = ? WHERE id = ?`, enqueued.RunID, automationID); err != nil {
		return AutomationFire{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AutomationFire{}, false, err
	}
	return AutomationFire{
		AutomationID:        automationID,
		Name:                name,
		SessionID:           runSessionID,
		RunID:               enqueued.RunID,
		JobID:               enqueued.JobID,
		TraceID:             enqueued.TraceID,
		SessionUpdatedEvent: enqueued.SessionUpdatedEvent,
		QueuedEvent:         enqueued.QueuedEvent,
	}, true, nil
}

// automationSessionTx returns the conversation this automation's runs land in,
// creating it on the first fire. One conversation per automation rather than
// one per run: "the conversation this thing produced" should be a single place
// to read, not a new row in the sidebar every five minutes.
//
// A conversation the user deleted leaves session_id NULL, and the next fire
// makes a fresh one rather than resurrecting what was withdrawn.
func automationSessionTx(ctx context.Context, tx *sql.Tx, automationID string, name string, existing sql.NullString) (string, error) {
	if existing.Valid && existing.String != "" {
		var found string
		err := tx.QueryRowContext(ctx, `SELECT id FROM sessions WHERE id = ?`, existing.String).Scan(&found)
		if err == nil {
			return found, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	createdAt := now()
	sessionID := ids.New("sess")
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		sessionID, name, createdAt, createdAt); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE automations SET session_id = ? WHERE id = ?`, sessionID, automationID); err != nil {
		return "", err
	}
	return sessionID, nil
}

// GetAutomationRunGrant reports what an unattended run was permitted to do.
// found=false means the run was started by a person, and the ordinary "stop
// and ask" path applies unchanged.
func (r *Repository) GetAutomationRunGrant(ctx context.Context, runID string) (AutomationRunGrant, bool, error) {
	var grant AutomationRunGrant
	var toolsJSON string
	err := r.db.QueryRowContext(ctx, `
		SELECT automation_id, automation_name, allowed_tools_json
		FROM automation_runs
		WHERE run_id = ?
	`, runID).Scan(&grant.AutomationID, &grant.AutomationName, &toolsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return AutomationRunGrant{}, false, nil
	}
	if err != nil {
		return AutomationRunGrant{}, false, err
	}
	if err := json.Unmarshal([]byte(toolsJSON), &grant.AllowedTools); err != nil {
		return AutomationRunGrant{}, false, err
	}
	return grant, true, nil
}

const automationDueSelect = `
	SELECT a.id, a.name, a.prompt, a.schedule_kind, a.interval_seconds, a.daily_minute_utc,
		a.next_due_at, a.session_id, r.status
	FROM automations a
	LEFT JOIN agent_runs r ON r.id = a.last_run_id`

const automationSelect = `
	SELECT a.id, a.name, a.prompt, a.schedule_kind, a.interval_seconds, a.daily_minute_utc, a.enabled,
		a.next_due_at, a.last_run_at, a.last_run_id, a.session_id,
		COALESCE(r.status, ''), COALESCE(r.error_message, ''),
		a.created_at, a.updated_at
	FROM automations a
	LEFT JOIN agent_runs r ON r.id = a.last_run_id`

func automationByIDTx(ctx context.Context, tx *sql.Tx, automationID string) (Automation, error) {
	rows, err := tx.QueryContext(ctx, automationSelect+` WHERE a.id = ?`, automationID)
	if err != nil {
		return Automation{}, err
	}
	automations, err := scanAutomations(rows)
	_ = rows.Close()
	if err != nil {
		return Automation{}, err
	}
	if len(automations) == 0 {
		return Automation{}, ErrAutomationNotFound
	}
	automation := automations[0]
	automation.AllowedTools, err = allowedToolsTx(ctx, tx, automationID)
	if err != nil {
		return Automation{}, err
	}
	return automation, nil
}

func scanAutomations(rows *sql.Rows) ([]Automation, error) {
	var automations []Automation
	for rows.Next() {
		var automation Automation
		var scheduleKind string
		var intervalSeconds, dailyMinute sql.NullInt64
		var enabled int
		var nextDue, lastRunAt, lastRunID, sessionID sql.NullString
		if err := rows.Scan(&automation.AutomationID, &automation.Name, &automation.Prompt, &scheduleKind,
			&intervalSeconds, &dailyMinute, &enabled, &nextDue, &lastRunAt, &lastRunID, &sessionID,
			&automation.LastRunStatus, &automation.LastRunError,
			&automation.CreatedAt, &automation.UpdatedAt); err != nil {
			return nil, err
		}
		// A schedule that cannot be read leaves Schedule zero-valued rather
		// than failing the read. One unreadable row must not make the whole
		// library unlistable — that row is precisely the one the user needs to
		// see in order to fix or delete it, and the scheduler has already
		// switched it off.
		if schedule, err := scheduleFromColumns(scheduleKind, intervalSeconds, dailyMinute); err == nil {
			automation.Schedule = schedule
		}
		automation.Enabled = enabled == 1
		automation.NextDueAt = nullableStringValue(nextDue)
		automation.LastRunAt = nullableStringValue(lastRunAt)
		automation.LastRunID = nullableStringValue(lastRunID)
		automation.SessionID = nullableStringValue(sessionID)
		automations = append(automations, automation)
	}
	return automations, rows.Err()
}

func allowedToolsTx(ctx context.Context, tx *sql.Tx, automationID string) ([]AutomationTool, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT server_name, tool_name
		FROM automation_allowed_tools
		WHERE automation_id = ?
		ORDER BY server_name, tool_name
	`, automationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	tools := make([]AutomationTool, 0)
	for rows.Next() {
		var tool AutomationTool
		if err := rows.Scan(&tool.ServerName, &tool.ToolName); err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, rows.Err()
}

func replaceAllowedToolsTx(ctx context.Context, tx *sql.Tx, automationID string, tools []AutomationTool) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM automation_allowed_tools WHERE automation_id = ?`, automationID); err != nil {
		return err
	}
	for _, tool := range tools {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO automation_allowed_tools (automation_id, server_name, tool_name)
			VALUES (?, ?, ?)
		`, automationID, tool.ServerName, tool.ToolName); err != nil {
			return err
		}
	}
	return nil
}

func scheduleColumns(schedule Schedule) (any, any) {
	switch schedule.Kind {
	case ScheduleInterval:
		return int64(schedule.Interval / time.Second), nil
	case ScheduleDaily:
		return nil, int64(schedule.DailyMinuteUTC)
	default:
		return nil, nil
	}
}

func scheduleFromColumns(kind string, intervalSeconds sql.NullInt64, dailyMinute sql.NullInt64) (Schedule, error) {
	switch ScheduleKind(kind) {
	case ScheduleInterval:
		if !intervalSeconds.Valid {
			return Schedule{}, errScheduleColumnsInvalid
		}
		return Schedule{Kind: ScheduleInterval, Interval: time.Duration(intervalSeconds.Int64) * time.Second}, nil
	case ScheduleDaily:
		if !dailyMinute.Valid {
			return Schedule{}, errScheduleColumnsInvalid
		}
		return Schedule{Kind: ScheduleDaily, DailyMinuteUTC: int(dailyMinute.Int64)}, nil
	default:
		return Schedule{}, errScheduleColumnsInvalid
	}
}

// isTerminalRunStatus is the same set agent_runs uses everywhere else: a run
// in any other state is still capable of doing something.
func isTerminalRunStatus(runStatus string) bool {
	switch runStatus {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
