package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

var ErrDelegationNotFound = errors.New("delegation not found")

// DelegationRecord is a delegation as its card shows it.
type DelegationRecord struct {
	ID              string
	ParentSessionID string
	ParentRunID     string
	ChildSessionID  string
	ChildRunID      string
	ProfileID       string
	DisplayName     string
	Emoji           string
	// Task is what Turing asked for, as the model wrote it; empty for a
	// delegation made before it was recorded.
	Task string
	// State is the child run's status, with recovering shown as running.
	State string
	// ApprovalID is the approval the task is waiting on, while it waits.
	ApprovalID string
	// ErrorCode is how a failed or cancelled task ended.
	ErrorCode string
	// Joined says the task's result went back to its parent's conversation,
	// and ContinuationRunID is the run that answered from it.
	Joined            bool
	ContinuationRunID string
	CreatedAt         string
	FinishedAt        string
	// Summary is one line in the orchestrator's words, never a model's.
	Summary string

	outcomeReason string
}

// delegationViewSelect reads delegations as their cards show them. The
// specialist's name comes from the job its task was frozen into, so a later
// edit to the profile does not rewrite it.
const delegationViewSelect = `
	SELECT d.id, d.parent_session_id, d.parent_run_id, d.child_session_id, d.child_run_id, d.profile_id,
		COALESCE(json_extract(job.payload_json, '$.agentProfile.displayName'), d.profile_id),
		COALESCE(json_extract(job.payload_json, '$.agentProfile.emoji'), ''),
		d.task, child.status, child.outcome_reason,
		CASE child.status
			WHEN 'failed' THEN COALESCE(child.error_code, '')
			WHEN 'cancelled' THEN COALESCE(child.cancellation_reason, '')
			ELSE '' END,
		CASE WHEN child.status = 'waiting_approval' THEN COALESCE((
			SELECT a.id FROM approvals a WHERE a.run_id = child.id AND a.status = 'pending'
			ORDER BY a.created_at DESC, a.id DESC LIMIT 1), '') ELSE '' END,
		d.joined,
		COALESCE((SELECT c.id FROM agent_runs c WHERE c.continues_run_id = d.parent_run_id), ''),
		d.created_at, COALESCE(child.finished_at, '')
	FROM delegations d
	JOIN agent_runs child ON child.id = d.child_run_id
	LEFT JOIN jobs job ON job.run_id = child.id`

func scanDelegationRecord(scan func(...any) error) (DelegationRecord, error) {
	var record DelegationRecord
	var status string
	if err := scan(&record.ID, &record.ParentSessionID, &record.ParentRunID, &record.ChildSessionID, &record.ChildRunID,
		&record.ProfileID, &record.DisplayName, &record.Emoji, &record.Task, &status, &record.outcomeReason,
		&record.ErrorCode, &record.ApprovalID, &record.Joined, &record.ContinuationRunID,
		&record.CreatedAt, &record.FinishedAt); err != nil {
		return DelegationRecord{}, err
	}
	record.State = displayedState(status)
	record.Summary = delegationSummary(record.Emoji, record.DisplayName, record.State, record.outcomeReason, record.ErrorCode)
	return record, nil
}

// ListDelegations is every task a conversation delegated, in the order they
// were asked for. A conversation that does not exist, is being deleted, or is
// a specialist's own is not found. The check and the read are one snapshot, so
// a deletion beginning between them cannot leak what it is withdrawing.
func (r *Repository) ListDelegations(ctx context.Context, parentSessionID string) ([]DelegationRecord, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var kind string
	err = tx.QueryRowContext(ctx,
		`SELECT kind FROM sessions WHERE id = ? AND deletion_state = 'active'`, parentSessionID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && kind != "chat") {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	records, err := activeDelegationsTx(ctx, tx, parentSessionID)
	if err != nil {
		return nil, err
	}
	return records, tx.Commit()
}

// activeDelegationsTx reads a conversation's delegations, and none while the
// conversation is being deleted.
func activeDelegationsTx(ctx context.Context, tx *sql.Tx, parentSessionID string) ([]DelegationRecord, error) {
	rows, err := tx.QueryContext(ctx, delegationViewSelect+`
		JOIN sessions parent ON parent.id = d.parent_session_id AND parent.deletion_state = 'active'
		WHERE d.parent_session_id = ? ORDER BY d.created_at, d.id`, parentSessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	records := []DelegationRecord{}
	for rows.Next() {
		record, err := scanDelegationRecord(rows.Scan)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// GetDelegation is one delegation, unless its conversation is being deleted.
func (r *Repository) GetDelegation(ctx context.Context, delegationID string) (DelegationRecord, error) {
	record, err := scanDelegationRecord(r.db.QueryRowContext(ctx, delegationViewSelect+`
		JOIN sessions parent ON parent.id = d.parent_session_id AND parent.deletion_state = 'active'
		WHERE d.id = ?`, delegationID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return DelegationRecord{}, ErrDelegationNotFound
	}
	return record, err
}

func delegationByChildRunTx(ctx context.Context, tx *sql.Tx, childRunID string) (DelegationRecord, bool, error) {
	record, err := scanDelegationRecord(tx.QueryRowContext(ctx, delegationViewSelect+`
		WHERE d.child_run_id = ?`, childRunID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return DelegationRecord{}, false, nil
	}
	return record, err == nil, err
}

// displayedState is a run's status as a card shows it: a run whose worker is
// being recovered is still working, as far as anyone watching can tell.
func displayedState(status string) string {
	if status == lifecycleRecovering {
		return lifecycleRunning
	}
	return status
}

// delegationStatePhrase says how a task stands, after its specialist's name.
func delegationStatePhrase(state, outcomeReason, code string) string {
	switch state {
	case lifecycleQueued:
		return "is queued"
	case lifecycleRunning:
		return "is working"
	case lifecycleWaitingApproval:
		return "is waiting for your approval"
	case lifecycleCompleted:
		return "completed"
	case lifecycleCancelled:
		switch {
		case outcomeReason == string(runoutcome.ReasonUserCancelled):
			return "was cancelled by you"
		case code == runoutcome.CodeParentRunEnded:
			return "was stopped because Turing's turn ended"
		}
		return "was cancelled (" + code + ")"
	default:
		return "failed (" + code + ")"
	}
}

func delegationSummary(emoji, displayName, state, outcomeReason, code string) string {
	return specialistName(emoji, displayName) + " " + delegationStatePhrase(state, outcomeReason, code)
}

func specialistName(emoji, displayName string) string {
	return strings.TrimSpace(emoji + " " + displayName)
}

// delegationUpdatedTx reports a task's change of state on its parent's
// stream, in the transaction that made it: one delegation.updated whenever
// what the task's card shows changes. Recovering shows as running, so losing
// and regaining a worker reports nothing. Once the parent's conversation is
// being deleted its stream takes no new event, so nothing is reported; the
// transition itself still commits.
func delegationUpdatedTx(ctx context.Context, tx *sql.Tx, runID, from, to, at string) ([]Event, error) {
	if displayedState(from) == displayedState(to) {
		return nil, nil
	}
	return reportDelegationTx(ctx, tx, runID, at)
}

// reportDelegationTx appends one delegation.updated carrying the card of the
// task runID runs as it now stands, unless runID is no task or its parent's
// conversation is being deleted.
func reportDelegationTx(ctx context.Context, tx *sql.Tx, runID, at string) ([]Event, error) {
	record, found, err := delegationByChildRunTx(ctx, tx, runID)
	if err != nil || !found {
		return nil, err
	}
	var deletionState, traceID string
	if err := tx.QueryRowContext(ctx, `
		SELECT session.deletion_state, run.trace_id FROM agent_runs run
		JOIN sessions session ON session.id = run.session_id WHERE run.id = ?`, record.ParentRunID).
		Scan(&deletionState, &traceID); err != nil {
		return nil, err
	}
	if deletionState != "active" {
		return nil, nil
	}
	payload := map[string]any{
		"delegationId":   record.ID,
		"parentRunId":    record.ParentRunID,
		"childRunId":     record.ChildRunID,
		"childSessionId": record.ChildSessionID,
		"profileId":      record.ProfileID,
		"displayName":    record.DisplayName,
		"emoji":          record.Emoji,
		"state":          record.State,
		"summary":        record.Summary,
	}
	if record.ApprovalID != "" {
		payload["approvalId"] = record.ApprovalID
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	event, err := appendRunEventTx(ctx, tx, record.ParentSessionID, record.ParentRunID, traceID,
		"delegation.updated", string(payloadJSON), at)
	if err != nil {
		return nil, err
	}
	return []Event{event}, nil
}

// stopDelegatedTasksTx stops the tasks a run delegated that have not
// finished, inside the transaction in which the run failed or was cancelled.
// Each is cancelled as the orchestrator giving up on it, never as the user's
// doing, and reports its own last state. While the run's conversation is
// active, one notice there says whose work was stopped; a conversation being
// deleted takes no new message. Nothing is joined: only a completed run is.
//
// It returns the stopped runs, which a worker may still be executing, and
// every event the stopping appended, in any session.
func stopDelegatedTasksTx(ctx context.Context, tx *sql.Tx, runID string) ([]string, []Event, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT d.child_run_id,
			COALESCE(json_extract(job.payload_json, '$.agentProfile.displayName'), d.profile_id),
			COALESCE(json_extract(job.payload_json, '$.agentProfile.emoji'), '')
		FROM delegations d
		JOIN agent_runs child ON child.id = d.child_run_id
		LEFT JOIN jobs job ON job.run_id = child.id
		WHERE d.parent_run_id = ? AND child.status NOT IN ('completed', 'failed', 'cancelled')
		ORDER BY d.created_at, d.id`, runID)
	if err != nil {
		return nil, nil, err
	}
	var stopped, names []string
	for rows.Next() {
		var childRunID, displayName, emoji string
		if err := rows.Scan(&childRunID, &displayName, &emoji); err != nil {
			return nil, nil, errors.Join(err, rows.Close())
		}
		stopped = append(stopped, childRunID)
		if name := specialistName(emoji, displayName); !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, nil, err
	}
	if len(stopped) == 0 {
		return nil, nil, nil
	}
	var events []Event
	for _, childRunID := range stopped {
		cancelled, err := cancelRunTx(ctx, tx, CancelRunInput{
			RunID: childRunID, Cancellation: runoutcome.ParentEndedCancellation(), resolveVersionInTx: true,
		})
		if err != nil {
			return nil, nil, err
		}
		events = append(events, cancelled.Events...)
	}
	var sessionID, deletionState string
	if err := tx.QueryRowContext(ctx, `
		SELECT session.id, session.deletion_state FROM agent_runs run
		JOIN sessions session ON session.id = run.session_id WHERE run.id = ?`, runID).
		Scan(&sessionID, &deletionState); err != nil {
		return nil, nil, err
	}
	if deletionState == "active" {
		if err := insertSessionMessageTx(ctx, tx, sessionID, "system", backendegress.DelegationNoticeContentType,
			"Turing's turn ended before these specialists finished, so their work was stopped: "+
				strings.Join(names, ", ")+"."); err != nil {
			return nil, nil, err
		}
	}
	return stopped, events, nil
}

// insertSessionMessageTx writes one orchestrator-authored message at the end
// of a session, belonging to no run, and moves the session's activity time to
// it. Like an anchor, it derives no title and publishes nothing.
func insertSessionMessageTx(ctx context.Context, tx *sql.Tx, sessionID, role, contentType, content string) error {
	next, created, err := nextMessageSlotTx(ctx, tx, sessionID, time.Now().UTC())
	if err != nil {
		return err
	}
	createdAt := FormatTimestamp(created)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ids.New("msg"), sessionID, role, content, contentType, next, createdAt); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, createdAt, sessionID)
	return err
}
