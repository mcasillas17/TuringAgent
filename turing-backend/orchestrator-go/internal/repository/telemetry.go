package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Telemetry: aggregation over rows this orchestrator already holds.
//
// Everything here is a read. There is no telemetry table, no collector, no
// sampling and no identifier, because there is nowhere for any of it to go —
// the result is answered to the user's own client over the same local port as
// everything else. A future change that sends any of this anywhere would break
// the project's central commitment, so the absence of a write path is a
// deliberate property of this file rather than an omission.
//
// Nothing selected below is content. No prompt, no reply, no tool argument, no
// credential: only counts, statuses, durations, and names the user chose.

const (
	// MinTelemetryWindowDays and MaxTelemetryWindowDays bound the window a
	// caller may ask for. The upper bound is not about load — a personal
	// database is small — but about the daily series, which returns one row
	// per day and would otherwise be unbounded at the client's discretion.
	MinTelemetryWindowDays = 1
	MaxTelemetryWindowDays = 365

	// maxTelemetryGroups caps the "most used" lists. The question being asked
	// is which few dominate; a full listing of every tool ever called answers a
	// different one and makes the answer harder to read.
	maxTelemetryGroups = 20
)

// ErrTelemetryWindowOutOfRange reports a window a caller may not ask for. It is
// deliberately not clamped: silently answering a different window than the one
// requested would put a label on the screen that does not describe the numbers
// under it.
var ErrTelemetryWindowOutOfRange = errors.New("telemetry window is out of range")

// TelemetryWindow is the span a summary covers, sent back with it so a client
// renders the window the server actually used.
type TelemetryWindow struct {
	Days  int
	Start time.Time
	End   time.Time
}

// TelemetryRunTotals counts runs created inside the window by outcome.
type TelemetryRunTotals struct {
	Total     int64
	Completed int64
	Failed    int64
	Cancelled int64
	// InFlight is queued, running or waiting on an approval. Reported
	// separately because a run that has not finished has not failed.
	InFlight int64
	// AverageDurationMs is DERIVED from started_at and finished_at, over the
	// runs that recorded both. nil when none did.
	AverageDurationMs *int64
}

// TelemetryTokenTotals is the measured token usage and the provenance that
// makes it readable. The totals cover only RunsWithUsage; the runs in
// RunsWithoutUsage contributed nothing and were not estimated.
type TelemetryTokenTotals struct {
	InputTokens      *int64
	OutputTokens     *int64
	RunsWithUsage    int64
	RunsWithoutUsage int64
}

type TelemetryToolUsage struct {
	ServerName string
	ToolName   string
	Calls      int64
	Failed     int64
	Denied     int64
	// AverageDurationMs is nil when no call in the window recorded a duration.
	// A tool whose calls were all denied never ran, and 0 ms would say it did.
	AverageDurationMs *int64
}

type TelemetryModelUsage struct {
	Provider     string
	Model        string
	Runs         int64
	InputTokens  *int64
	OutputTokens *int64
	// RunsWithoutUsage shares Runs' denominator: it counts every run in this
	// group with no recorded token usage, whatever its status. Counting only
	// completed ones would let a group of 40 runs where 12 failed report 40
	// runs, a token total covering 28, and nothing without usage — which reads
	// as a complete measurement of all 40.
	RunsWithoutUsage int64
}

// TelemetryExternalAgentUsage is what left the machine, read from the
// attribution frozen onto each run rather than from where the conversation
// currently points.
type TelemetryExternalAgentUsage struct {
	DisplayName  string
	EndpointHost string
	Runs         int64
	InputTokens  *int64
	OutputTokens *int64
	// Same denominator as Runs — see TelemetryModelUsage.RunsWithoutUsage.
	RunsWithoutUsage int64
}

type TelemetryAutomationTotals struct {
	Runs      int64
	Completed int64
	Failed    int64
	// UnattendedApprovals are approvals an automation's allowlist decided
	// rather than a person. Counted because consent given in advance is weaker
	// than consent given in the moment, and the weaker kind should be visible.
	UnattendedApprovals int64
}

// TelemetryIntegrationTotals is current state, NOT windowed activity. Nothing
// in a run reads a connection yet, so there is no usage to measure and none is
// implied.
type TelemetryIntegrationTotals struct {
	Connected int64
	Revoked   int64
}

// TelemetryDailyActivity is one UTC day. Days with no activity are present
// with zeroes so a client can draw a continuous axis without inventing gaps.
//
// The window starts on a day boundary, so there are exactly Days entries. The
// LAST one is the day still in progress and is expected to read low.
type TelemetryDailyActivity struct {
	Date         string
	Runs         int64
	ToolCalls    int64
	InputTokens  *int64
	OutputTokens *int64
}

type TelemetrySummary struct {
	Window         TelemetryWindow
	Runs           TelemetryRunTotals
	Tokens         TelemetryTokenTotals
	Tools          []TelemetryToolUsage
	Models         []TelemetryModelUsage
	ExternalAgents []TelemetryExternalAgentUsage
	Automations    TelemetryAutomationTotals
	Integrations   TelemetryIntegrationTotals
	Daily          []TelemetryDailyActivity
}

// TelemetrySummary aggregates the window ending at asOf.
//
// asOf is a parameter rather than a call to time.Now inside, so the window is
// decided by the caller and a test can pin it. A "last 7 days" figure whose
// correctness depends on what time the suite happens to run is not a tested
// figure.
func (r *Repository) TelemetrySummary(ctx context.Context, asOf time.Time, days int) (TelemetrySummary, error) {
	if days < MinTelemetryWindowDays || days > MaxTelemetryWindowDays {
		return TelemetrySummary{}, fmt.Errorf("%w: %d", ErrTelemetryWindowOutOfRange, days)
	}
	end := asOf.UTC()
	// Snapped to a whole UTC day rather than rolled back by an exact multiple
	// of 24 hours. The daily series buckets by calendar day, so a window
	// starting at 12:00 would produce one bucket more than the label promises
	// and a first bucket covering only half a day — a busy morning would draw
	// as a short bar for a reason nothing on the page could explain.
	//
	// "Last 7 days" therefore means today plus the six before it. The final
	// bucket is the day in progress, which is the one partial bucket a reader
	// expects and can account for.
	start := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))
	window := TelemetryWindow{Days: days, Start: start, End: end}

	summary := TelemetrySummary{Window: window}
	var err error
	if summary.Runs, err = r.telemetryRunTotals(ctx, window); err != nil {
		return TelemetrySummary{}, err
	}
	if summary.Tokens, err = r.telemetryTokenTotals(ctx, window); err != nil {
		return TelemetrySummary{}, err
	}
	if summary.Tools, err = r.telemetryToolUsage(ctx, window); err != nil {
		return TelemetrySummary{}, err
	}
	if summary.Models, err = r.telemetryModelUsage(ctx, window); err != nil {
		return TelemetrySummary{}, err
	}
	if summary.ExternalAgents, err = r.telemetryExternalAgentUsage(ctx, window); err != nil {
		return TelemetrySummary{}, err
	}
	if summary.Automations, err = r.telemetryAutomationTotals(ctx, window); err != nil {
		return TelemetrySummary{}, err
	}
	if summary.Integrations, err = r.telemetryIntegrationTotals(ctx); err != nil {
		return TelemetrySummary{}, err
	}
	if summary.Daily, err = r.telemetryDailyActivity(ctx, window); err != nil {
		return TelemetrySummary{}, err
	}
	return summary, nil
}

// telemetryWindowBounds converts the window into the normalised nanosecond
// values the timestamp columns are compared on.
//
// Comparing the stored text directly would be wrong for rows written before
// 0005, which use a shorter layout; sqliteTimestampNanos is the repository's
// existing answer to that and is reused here rather than assuming every row in
// a long-lived database was written by the current build.
func telemetryWindowBounds(window TelemetryWindow) (int64, int64) {
	return window.Start.UnixNano(), window.End.UnixNano()
}

func telemetryWindowPredicate(column string) string {
	nanos := sqliteTimestampNanos(column)
	return nanos + " >= ? AND " + nanos + " < ?"
}

func (r *Repository) telemetryRunTotals(ctx context.Context, window TelemetryWindow) (TelemetryRunTotals, error) {
	start, end := telemetryWindowBounds(window)
	// SUM over no rows is NULL, not 0, and AVG over no rows is NULL — which is
	// exactly right for the average and exactly wrong for a count. The counts
	// are read through nullable scans and defaulted to zero; the average is
	// left absent, because "no run recorded a duration" is not "runs took no
	// time".
	query := `
		SELECT
			COUNT(*),
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status NOT IN ('completed','failed','cancelled') THEN 1 ELSE 0 END),
			AVG(CASE
				WHEN started_at IS NOT NULL AND finished_at IS NOT NULL
					AND ` + sqliteTimestampNanos("finished_at") + ` >= ` + sqliteTimestampNanos("started_at") + `
				THEN (` + sqliteTimestampNanos("finished_at") + ` - ` + sqliteTimestampNanos("started_at") + `) / 1000000.0
			END)
		FROM agent_runs
		WHERE ` + telemetryWindowPredicate("created_at")

	var totals TelemetryRunTotals
	var completed, failed, cancelled, inFlight sql.NullInt64
	var averageMs sql.NullFloat64
	if err := r.db.QueryRowContext(ctx, query, start, end).Scan(
		&totals.Total, &completed, &failed, &cancelled, &inFlight, &averageMs); err != nil {
		return TelemetryRunTotals{}, err
	}
	totals.Completed = completed.Int64
	totals.Failed = failed.Int64
	totals.Cancelled = cancelled.Int64
	totals.InFlight = inFlight.Int64
	totals.AverageDurationMs = roundedMilliseconds(averageMs)
	return totals, nil
}

func (r *Repository) telemetryTokenTotals(ctx context.Context, window TelemetryWindow) (TelemetryTokenTotals, error) {
	start, end := telemetryWindowBounds(window)
	// Restricted to completed runs, which is the honest denominator: a run that
	// failed or was cancelled had no completion for usage to be recorded at, so
	// counting it as "reported nothing" would blame the provider for something
	// it was never asked.
	query := `
		SELECT
			SUM(input_tokens),
			SUM(output_tokens),
			SUM(CASE WHEN input_tokens IS NOT NULL OR output_tokens IS NOT NULL THEN 1 ELSE 0 END),
			SUM(CASE WHEN input_tokens IS NULL AND output_tokens IS NULL THEN 1 ELSE 0 END)
		FROM agent_runs
		WHERE status = 'completed' AND ` + telemetryWindowPredicate("created_at")

	var input, output, with, without sql.NullInt64
	if err := r.db.QueryRowContext(ctx, query, start, end).Scan(&input, &output, &with, &without); err != nil {
		return TelemetryTokenTotals{}, err
	}
	return TelemetryTokenTotals{
		InputTokens:      int64OrNil(input),
		OutputTokens:     int64OrNil(output),
		RunsWithUsage:    with.Int64,
		RunsWithoutUsage: without.Int64,
	}, nil
}

func (r *Repository) telemetryToolUsage(ctx context.Context, window TelemetryWindow) ([]TelemetryToolUsage, error) {
	start, end := telemetryWindowBounds(window)
	// Ordered by call count, because "what does it use the most" is the
	// question. The name tiebreak keeps the answer stable between two calls
	// that saw the same data, which a client comparing two refreshes will
	// otherwise read as movement.
	query := `
		SELECT
			server_name,
			tool_name,
			COUNT(*),
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'denied' THEN 1 ELSE 0 END),
			AVG(CASE WHEN duration_ms IS NOT NULL AND duration_ms >= 0 THEN duration_ms END)
		FROM tool_calls
		WHERE ` + telemetryWindowPredicate("created_at") + `
		GROUP BY server_name, tool_name
		ORDER BY COUNT(*) DESC, server_name ASC, tool_name ASC
		LIMIT ?`

	rows, err := r.db.QueryContext(ctx, query, start, end, maxTelemetryGroups)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	usage := make([]TelemetryToolUsage, 0)
	for rows.Next() {
		var entry TelemetryToolUsage
		var failed, denied sql.NullInt64
		var averageMs sql.NullFloat64
		if err := rows.Scan(&entry.ServerName, &entry.ToolName, &entry.Calls, &failed, &denied, &averageMs); err != nil {
			return nil, err
		}
		entry.Failed = failed.Int64
		entry.Denied = denied.Int64
		entry.AverageDurationMs = roundedMilliseconds(averageMs)
		usage = append(usage, entry)
	}
	return usage, rows.Err()
}

func (r *Repository) telemetryModelUsage(ctx context.Context, window TelemetryWindow) ([]TelemetryModelUsage, error) {
	start, end := telemetryWindowBounds(window)
	query := `
		SELECT
			model_provider,
			model_name,
			COUNT(*),
			SUM(input_tokens),
			SUM(output_tokens),
			SUM(CASE WHEN input_tokens IS NULL AND output_tokens IS NULL THEN 1 ELSE 0 END)
		FROM agent_runs
		WHERE ` + telemetryWindowPredicate("created_at") + `
		GROUP BY model_provider, model_name
		ORDER BY COUNT(*) DESC, model_provider ASC, model_name ASC
		LIMIT ?`

	rows, err := r.db.QueryContext(ctx, query, start, end, maxTelemetryGroups)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	usage := make([]TelemetryModelUsage, 0)
	for rows.Next() {
		var entry TelemetryModelUsage
		var input, output, without sql.NullInt64
		if err := rows.Scan(&entry.Provider, &entry.Model, &entry.Runs, &input, &output, &without); err != nil {
			return nil, err
		}
		entry.InputTokens = int64OrNil(input)
		entry.OutputTokens = int64OrNil(output)
		entry.RunsWithoutUsage = without.Int64
		usage = append(usage, entry)
	}
	return usage, rows.Err()
}

func (r *Repository) telemetryExternalAgentUsage(ctx context.Context, window TelemetryWindow) ([]TelemetryExternalAgentUsage, error) {
	start, end := telemetryWindowBounds(window)
	query := `
		SELECT
			external_agent_name,
			COALESCE(external_agent_host, ''),
			COUNT(*),
			SUM(input_tokens),
			SUM(output_tokens),
			SUM(CASE WHEN input_tokens IS NULL AND output_tokens IS NULL THEN 1 ELSE 0 END)
		FROM agent_runs
		WHERE external_agent_name IS NOT NULL AND ` + telemetryWindowPredicate("created_at") + `
		GROUP BY external_agent_name, external_agent_host
		ORDER BY COUNT(*) DESC, external_agent_name ASC
		LIMIT ?`

	rows, err := r.db.QueryContext(ctx, query, start, end, maxTelemetryGroups)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	usage := make([]TelemetryExternalAgentUsage, 0)
	for rows.Next() {
		var entry TelemetryExternalAgentUsage
		var input, output, without sql.NullInt64
		if err := rows.Scan(&entry.DisplayName, &entry.EndpointHost, &entry.Runs, &input, &output, &without); err != nil {
			return nil, err
		}
		entry.InputTokens = int64OrNil(input)
		entry.OutputTokens = int64OrNil(output)
		entry.RunsWithoutUsage = without.Int64
		usage = append(usage, entry)
	}
	return usage, rows.Err()
}

func (r *Repository) telemetryAutomationTotals(ctx context.Context, window TelemetryWindow) (TelemetryAutomationTotals, error) {
	start, end := telemetryWindowBounds(window)
	runQuery := `
		SELECT
			COUNT(*),
			SUM(CASE WHEN agent_runs.status = 'completed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN agent_runs.status = 'failed' THEN 1 ELSE 0 END)
		FROM automation_runs
		JOIN agent_runs ON agent_runs.id = automation_runs.run_id
		WHERE ` + telemetryWindowPredicate("automation_runs.fired_at")

	var totals TelemetryAutomationTotals
	var completed, failed sql.NullInt64
	if err := r.db.QueryRowContext(ctx, runQuery, start, end).Scan(&totals.Runs, &completed, &failed); err != nil {
		return TelemetryAutomationTotals{}, err
	}
	totals.Completed = completed.Int64
	totals.Failed = failed.Int64

	// An approval an automation granted itself is still an approval row; what
	// distinguishes it is that its run was one nobody was present for. Joining
	// on automation_runs is what asks that question, and 'consumed' counts
	// because a consumed approval was granted first.
	approvalQuery := `
		SELECT COUNT(*)
		FROM approvals
		JOIN automation_runs ON automation_runs.run_id = approvals.run_id
		WHERE approvals.status IN ('approved','consumed') AND ` + telemetryWindowPredicate("approvals.created_at")

	if err := r.db.QueryRowContext(ctx, approvalQuery, start, end).Scan(&totals.UnattendedApprovals); err != nil {
		return TelemetryAutomationTotals{}, err
	}
	return totals, nil
}

func (r *Repository) telemetryIntegrationTotals(ctx context.Context) (TelemetryIntegrationTotals, error) {
	var totals TelemetryIntegrationTotals
	var connected, revoked sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END)
		FROM integration_connections`,
		ConnectionStatusConnected, ConnectionStatusRevoked).Scan(&connected, &revoked)
	if err != nil {
		return TelemetryIntegrationTotals{}, err
	}
	totals.Connected = connected.Int64
	totals.Revoked = revoked.Int64
	return totals, nil
}

func (r *Repository) telemetryDailyActivity(ctx context.Context, window TelemetryWindow) ([]TelemetryDailyActivity, error) {
	start, end := telemetryWindowBounds(window)
	// UTC days, because that is what the rows are stored in. Grouping by local
	// time would move activity across day boundaries in a way no stored value
	// could justify, and the boundary would shift under a traveller.
	//
	// substr over the first ten characters reads the date out of both the
	// current fixed-width layout and the older one; both begin YYYY-MM-DD.
	runQuery := `
		SELECT
			substr(created_at, 1, 10),
			COUNT(*),
			SUM(input_tokens),
			SUM(output_tokens)
		FROM agent_runs
		WHERE ` + telemetryWindowPredicate("created_at") + `
		GROUP BY substr(created_at, 1, 10)`

	type dayTotals struct {
		runs         int64
		toolCalls    int64
		inputTokens  sql.NullInt64
		outputTokens sql.NullInt64
	}
	byDate := make(map[string]*dayTotals)

	rows, err := r.db.QueryContext(ctx, runQuery, start, end)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var date string
		var totals dayTotals
		if err := rows.Scan(&date, &totals.runs, &totals.inputTokens, &totals.outputTokens); err != nil {
			_ = rows.Close()
			return nil, err
		}
		byDate[date] = &dayTotals{runs: totals.runs, inputTokens: totals.inputTokens, outputTokens: totals.outputTokens}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	toolQuery := `
		SELECT substr(created_at, 1, 10), COUNT(*)
		FROM tool_calls
		WHERE ` + telemetryWindowPredicate("created_at") + `
		GROUP BY substr(created_at, 1, 10)`

	toolRows, err := r.db.QueryContext(ctx, toolQuery, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = toolRows.Close() }()
	for toolRows.Next() {
		var date string
		var calls int64
		if err := toolRows.Scan(&date, &calls); err != nil {
			return nil, err
		}
		if existing, ok := byDate[date]; ok {
			existing.toolCalls = calls
			continue
		}
		byDate[date] = &dayTotals{toolCalls: calls}
	}
	if err := toolRows.Err(); err != nil {
		return nil, err
	}

	// Dense, in order, from the window rather than from the data: a series
	// drawn only from days that had activity compresses a quiet week into a
	// line that looks busy.
	//
	// The window starts on a day boundary, so this yields exactly window.Days
	// entries and the label over the chart matches the bars under it.
	daily := make([]TelemetryDailyActivity, 0, window.Days)
	firstDay := time.Date(window.Start.Year(), window.Start.Month(), window.Start.Day(), 0, 0, 0, 0, time.UTC)
	lastDay := time.Date(window.End.Year(), window.End.Month(), window.End.Day(), 0, 0, 0, 0, time.UTC)
	for day := firstDay; !day.After(lastDay); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		entry := TelemetryDailyActivity{Date: date}
		if totals, ok := byDate[date]; ok {
			entry.Runs = totals.runs
			entry.ToolCalls = totals.toolCalls
			entry.InputTokens = int64OrNil(totals.inputTokens)
			entry.OutputTokens = int64OrNil(totals.outputTokens)
		}
		daily = append(daily, entry)
	}
	return daily, nil
}

// int64OrNil is the read direction of nonNegativeNullInt64 in runs.go: a
// NULL column becomes an absent count rather than a zero one.
func int64OrNil(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	count := value.Int64
	return &count
}

// roundedMilliseconds turns an AVG over no rows into "unknown" rather than
// zero, and rounds the rest to whole milliseconds — a mean latency reported to
// the nanosecond claims a precision the measurement does not have.
func roundedMilliseconds(value sql.NullFloat64) *int64 {
	if !value.Valid {
		return nil
	}
	rounded := int64(value.Float64 + 0.5)
	return &rounded
}
