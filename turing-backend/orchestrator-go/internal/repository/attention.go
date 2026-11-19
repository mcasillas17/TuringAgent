package repository

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/attention"
)

// MaxAttentionPageLimit is the Needs-you queue's page ceiling, the audit
// API's.
const MaxAttentionPageLimit = 100

var (
	ErrAttentionInvalidLimit = errors.New("attention page limit must be between 1 and 100")
	// ErrAttentionOccurrenceStale is a dismissal naming an occurrence that is
	// not the automation's latest failing one: a stale screen must not hide a
	// newer failure.
	ErrAttentionOccurrenceStale = errors.New("that occurrence is no longer the automation's latest failure")
)

// AttentionCursor is a position in the queue's total order: stage rank, then
// since descending, then source kind, then source id.
type AttentionCursor struct {
	Rank       int
	SinceNanos int64
	Kind       attention.Kind
	ID         string
}

// AttentionQuery is one page's read. After and Watermark come from the
// previous page; the first page has neither.
type AttentionQuery struct {
	After *AttentionCursor
	// Watermark is the first page's read time. A later page leaves out every
	// row whose since is after it.
	Watermark int64
	Limit     int
}

// AttentionRow is one owner obligation: the ids its action needs, the
// conditions its source established, and its sort key. It holds no tool
// arguments, message content or transcripts.
type AttentionRow struct {
	Kind       attention.Kind
	Conditions attention.Conditions
	SinceNanos int64
	// ID is the source id the order breaks ties by: the approval, delegation
	// or automation id, or the deletion receipt's session.
	ID             string
	SessionID      string
	SessionTitle   string
	ApprovalID     string
	RunID          string
	DelegationID   string
	AutomationID   string
	AutomationName string
	OccurrenceKey  string
	Cause          string
}

// Cursor is the row's place in the queue's total order. An unclassifiable
// row keeps its source's place.
func (row AttentionRow) Cursor() AttentionCursor {
	return AttentionCursor{
		Rank: attention.Rank(attention.SourceStage(row.Kind)), SinceNanos: row.SinceNanos, Kind: row.Kind, ID: row.ID,
	}
}

func (row AttentionRow) before(other AttentionRow) bool {
	a, b := row.Cursor(), other.Cursor()
	switch {
	case a.Rank != b.Rank:
		return a.Rank < b.Rank
	case a.SinceNanos != b.SinceNanos:
		return a.SinceNanos > b.SinceNanos
	case a.Kind != b.Kind:
		return a.Kind < b.Kind
	default:
		return a.ID < b.ID
	}
}

// AttentionPage is at most Limit rows in order, whether more follow, and the
// watermark every later page of the pass applies.
type AttentionPage struct {
	Rows      []AttentionRow
	More      bool
	Watermark int64
}

// ListAttention reads one page of the queue in one read transaction. Each
// source reads at most Limit+1 rows after the cursor in its own bounded,
// indexed read, and the page is the first Limit of their merge.
//
// The first page records the current time as the pass's watermark. A later
// page reads the same sources with the same predicates but leaves out rows
// whose since is after the watermark, so a row that became due mid-pass is
// left to the next first page, which its attention.changed hint triggers,
// and never lands among pages already shown. Every page re-reads every row's
// current state: the cursor carries no authority.
func (r *Repository) ListAttention(ctx context.Context, query AttentionQuery) (AttentionPage, error) {
	if query.Limit <= 0 || query.Limit > MaxAttentionPageLimit {
		return AttentionPage{}, ErrAttentionInvalidLimit
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AttentionPage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().UnixNano()
	if query.After == nil {
		query.Watermark = now
	}
	var rows []AttentionRow
	for _, source := range attentionSources {
		statement, args, read := source.statement(query, now)
		if !read {
			continue
		}
		sourceRows, err := readAttentionRows(ctx, tx, source.kind, statement, args)
		if err != nil {
			return AttentionPage{}, err
		}
		rows = append(rows, sourceRows...)
	}
	if err := tx.Commit(); err != nil {
		return AttentionPage{}, err
	}
	slices.SortFunc(rows, func(a, b AttentionRow) int {
		switch {
		case a.before(b):
			return -1
		case b.before(a):
			return 1
		default:
			return 0
		}
	})
	page := AttentionPage{Rows: rows, Watermark: query.Watermark}
	if len(rows) > query.Limit {
		page.Rows, page.More = rows[:query.Limit], true
	}
	return page, nil
}

// DismissAttention hides one automation's failing occurrence from the queue
// until a newer one. In one transaction it reads the automation's latest
// occurrence and refuses a key that is not it, or an occurrence that is not
// failing, so a stale screen cannot hide a newer failure. Only automations
// can be dismissed: an id that names no automation is not found.
func (r *Repository) DismissAttention(ctx context.Context, automationID string, occurrenceKey string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var failing bool
	var current string
	err = tx.QueryRowContext(ctx, automationOccurrenceQuery, automationID).Scan(&failing, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAutomationNotFound
	}
	if err != nil {
		return err
	}
	if !failing || current == "" || current != occurrenceKey {
		return ErrAttentionOccurrenceStale
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE automations SET attention_dismissed_occurrence = ? WHERE id = ?`, occurrenceKey, automationID); err != nil {
		return err
	}
	return tx.Commit()
}

// latestRemoteEgressBlockRowID selects automation a's latest
// automation.remote_egress_blocked record newer than its last run: one seek
// on idx_audit_remote_egress_blocked. ClaimDueAutomation writes one when it
// blocks an occurrence before any run exists.
const latestRemoteEgressBlockRowID = `(
		SELECT candidate.rowid
		FROM audit_logs candidate
		WHERE candidate.action = 'automation.remote_egress_blocked'
			AND candidate.actor_id = a.id
			AND candidate.actor_type = 'automation'
			AND candidate.created_at > COALESCE(a.last_run_at, '')
		ORDER BY candidate.created_at DESC, candidate.rowid DESC
		LIMIT 1
	)`

// automationOccurrenceFrom joins automation a's latest occurrence: its latest
// remote-egress block newer than its last run (blocked) when there is one,
// otherwise its last run (run), whose conversation must still be active.
const automationOccurrenceFrom = `
	FROM automations a
	LEFT JOIN audit_logs blocked ON blocked.rowid = ` + latestRemoteEgressBlockRowID + `
	LEFT JOIN agent_runs run ON blocked.id IS NULL AND run.id = a.last_run_id
	LEFT JOIN sessions run_session ON run_session.id = run.session_id AND run_session.deletion_state = 'active'`

// automationOccurrenceKey is the occurrence key, stated once: the latest
// block's audit id, else the last run's id. Run and audit ids carry distinct
// prefixes, so the two kinds of key never collide.
const automationOccurrenceKey = `COALESCE(blocked.id, a.last_run_id, '')`

// automationOccurrenceFailing says the latest occurrence failed or was
// blocked. A run the owner cancelled did neither.
const automationOccurrenceFailing = `(blocked.id IS NOT NULL OR (run_session.id IS NOT NULL AND (
		run.status = 'failed' OR (run.status = 'cancelled'
			AND COALESCE(run.outcome_reason, '') <> 'user_cancelled'
			AND COALESCE(run.cancellation_reason, '') <> 'user_cancelled'))))`

// automationOccurrenceQuery is DismissAttention's read of one automation:
// whether it is enabled with a failing latest occurrence, and that
// occurrence's key.
const automationOccurrenceQuery = `
	SELECT a.enabled = 1 AND ` + automationOccurrenceFailing + `, ` + automationOccurrenceKey +
	automationOccurrenceFrom + `
	WHERE a.id = ?`

// attentionColumns is every source's select list, in readAttentionRows'
// order. qualifies is the source's condition as its read established it.
const attentionColumns = `source_id, since_ns, qualifies, session_id, session_title, approval_id, run_id,
	delegation_id, automation_id, automation_name, occurrence_key, cause`

type attentionSource struct {
	kind attention.Kind
	// read is the source's select of attentionColumns, with its arguments for
	// the read time now.
	read func(now int64) (string, []any)
}

// attentionSources are the queue's sources in kind order. PRO-001 adds the
// watch and proposal sources here.
var attentionSources = []attentionSource{
	{kind: attention.KindApproval, read: approvalAttentionRead},
	{kind: attention.KindDelegation, read: consentAttentionRead},
	{kind: attention.KindAutomation, read: automationAttentionRead},
	{kind: attention.KindDeletion, read: deletionAttentionRead},
}

// approvalAttentionRead lists pending approvals by idx_approvals_status. A
// delegated task's appears under its parent conversation. An expiry that
// cannot be read keeps the approval listed, unclassified, because whether it
// is still pending cannot be told.
func approvalAttentionRead(now int64) (string, []any) {
	expires := sqliteTimestampNanos("a.expires_at")
	return `
		SELECT a.id AS source_id,
			COALESCE(` + sqliteTimestampNanos("a.created_at") + `, 0) AS since_ns,
			COALESCE(` + expires + ` > ?, 0) AS qualifies,
			s.id AS session_id, COALESCE(s.title, '') AS session_title,
			a.id AS approval_id, a.run_id AS run_id, COALESCE(d.id, '') AS delegation_id,
			'' AS automation_id, '' AS automation_name, '' AS occurrence_key, '' AS cause
		FROM approvals a
		JOIN agent_runs run ON run.id = a.run_id
		LEFT JOIN delegations d ON d.child_run_id = a.run_id
		JOIN sessions s ON s.id = COALESCE(d.parent_session_id, run.session_id)
		WHERE a.status = 'pending'
			AND s.deletion_state = 'active'
			AND (` + expires + ` IS NULL OR ` + expires + ` > ?)`, []any{now, now}
}

// consentAttentionRead lists tasks held for consent by
// idx_delegations_consent_pending, named because the planner would otherwise
// drive from every queued run. A task that stopped waiting keeps its pending
// mark but is no longer queued.
func consentAttentionRead(int64) (string, []any) {
	return `
		SELECT d.id AS source_id,
			COALESCE(` + sqliteTimestampNanos("d.created_at") + `, 0) AS since_ns,
			1 AS qualifies,
			s.id AS session_id, COALESCE(s.title, '') AS session_title,
			'' AS approval_id, d.child_run_id AS run_id, d.id AS delegation_id,
			'' AS automation_id, '' AS automation_name, '' AS occurrence_key, '' AS cause
		FROM delegations d INDEXED BY idx_delegations_consent_pending
		JOIN agent_runs child ON child.id = d.child_run_id
		JOIN sessions s ON s.id = d.parent_session_id
		WHERE d.egress_consent = 'pending'
			AND child.status = 'queued'
			AND s.deletion_state = 'active'`, nil
}

// automationAttentionRead lists enabled automations whose latest occurrence
// failed or was blocked, unless the owner dismissed that occurrence. A
// failure stays until it is succeeded, dismissed or its automation disabled,
// however old it is.
func automationAttentionRead(int64) (string, []any) {
	return `
		SELECT a.id AS source_id,
			COALESCE(` + sqliteTimestampNanos("COALESCE(blocked.created_at, run.finished_at, run.created_at)") + `, 0) AS since_ns,
			1 AS qualifies,
			COALESCE(s.id, '') AS session_id, COALESCE(s.title, '') AS session_title,
			'' AS approval_id, CASE WHEN blocked.id IS NULL THEN COALESCE(a.last_run_id, '') ELSE '' END AS run_id,
			'' AS delegation_id, a.id AS automation_id, a.name AS automation_name,
			` + automationOccurrenceKey + ` AS occurrence_key,
			CASE
				WHEN blocked.id IS NOT NULL THEN
					COALESCE(CASE WHEN json_valid(blocked.payload_json) THEN json_extract(blocked.payload_json, '$.code') END, '')
				WHEN run.status = 'failed' THEN COALESCE(run.error_code, '')
				ELSE COALESCE(run.cancellation_reason, '')
			END AS cause` + automationOccurrenceFrom + `
		LEFT JOIN sessions s ON s.id = a.session_id AND s.deletion_state = 'active'
		WHERE a.enabled = 1
			AND ` + automationOccurrenceFailing + `
			AND COALESCE(a.attention_dismissed_occurrence, '') <> ` + automationOccurrenceKey, nil
}

// deletionAttentionRead lists the owner's deletion receipts held failed and
// retryable, the ones PendingSessionDeletionReceipts lists; a child's is its
// parent's to finish. A receipt's since is its begin: its quiesce deadline
// less the quiesce lease.
func deletionAttentionRead(int64) (string, []any) {
	return `
		SELECT d.session_id AS source_id,
			COALESCE(` + sqliteTimestampNanos("d.quiesce_deadline_at") + ` - ?, 0) AS since_ns,
			1 AS qualifies,
			d.session_id AS session_id, '' AS session_title,
			'' AS approval_id, '' AS run_id, '' AS delegation_id,
			'' AS automation_id, '' AS automation_name, '' AS occurrence_key, COALESCE(d.error_code, '') AS cause
		FROM session_deletions d
		WHERE d.state = 'failed_external'
			AND d.retryable = 1
			AND d.parent_session_id IS NULL`, []any{sessionDeletionQuiesceLease.Nanoseconds()}
}

// statement is the source's read for one page: its rows after the cursor in
// the total order, at or before the watermark on a later page, at most
// Limit+1. Within a source the stage rank and kind are constants, so it
// compares its rank with the cursor's first: an earlier stage reads nothing
// and a later one every row. In the cursor's stage it compares since before
// kind, because since is the order's second key and kind its third. read is
// false when the source has nothing after the cursor.
func (s attentionSource) statement(query AttentionQuery, now int64) (string, []any, bool) {
	inner, args := s.read(now)
	var where []string
	if after := query.After; after != nil {
		rank := attention.Rank(attention.SourceStage(s.kind))
		switch {
		case rank < after.Rank:
			return "", nil, false
		case rank > after.Rank:
		case s.kind < after.Kind:
			where = append(where, `since_ns < ?`)
			args = append(args, after.SinceNanos)
		case s.kind == after.Kind:
			where = append(where, `(since_ns < ? OR (since_ns = ? AND source_id > ?))`)
			args = append(args, after.SinceNanos, after.SinceNanos, after.ID)
		default:
			where = append(where, `since_ns <= ?`)
			args = append(args, after.SinceNanos)
		}
		where = append(where, `since_ns <= ?`)
		args = append(args, query.Watermark)
	}
	statement := `SELECT ` + attentionColumns + ` FROM (` + inner + `)`
	if len(where) > 0 {
		statement += ` WHERE ` + strings.Join(where, ` AND `)
	}
	statement += ` ORDER BY since_ns DESC, source_id ASC LIMIT ?`
	return statement, append(args, query.Limit+1), true
}

func readAttentionRows(ctx context.Context, tx *sql.Tx, kind attention.Kind, statement string, args []any) ([]AttentionRow, error) {
	rows, err := tx.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var read []AttentionRow
	for rows.Next() {
		row := AttentionRow{Kind: kind}
		var qualifies bool
		if err := rows.Scan(&row.ID, &row.SinceNanos, &qualifies, &row.SessionID, &row.SessionTitle, &row.ApprovalID,
			&row.RunID, &row.DelegationID, &row.AutomationID, &row.AutomationName, &row.OccurrenceKey, &row.Cause); err != nil {
			return nil, err
		}
		switch kind {
		case attention.KindApproval:
			row.Conditions.PendingApproval = qualifies
		case attention.KindDelegation:
			row.Conditions.PendingConsent = qualifies
		case attention.KindAutomation:
			row.Conditions.BlockedAutomation = qualifies
		case attention.KindDeletion:
			row.Conditions.RetryableDeletion = qualifies
		}
		read = append(read, row)
	}
	return read, rows.Err()
}
