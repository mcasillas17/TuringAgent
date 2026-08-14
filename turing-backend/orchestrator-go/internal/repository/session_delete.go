package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ErrSessionNotFound reports a delete for a session id that does not exist, so
// the caller can answer NotFound rather than reporting a success that removed
// nothing.
var ErrSessionNotFound = errors.New("session not found")

// ErrSessionHasActiveRun reports a delete refused because the session still has
// work in flight. Removing the rows would leave the runtime finishing a run
// whose session, messages and job no longer exist, and reconciliation would
// then operate on nothing.
var ErrSessionHasActiveRun = errors.New("session has an active run")

// scrubbedAuditPayload replaces audit content on deletion. It is a tombstone
// rather than NULL so a reader can tell "scrubbed because the user deleted the
// session" from "never carried a payload".
const scrubbedAuditPayload = `{"scrubbed":true}`

// DeleteSession removes a session and everything it produced, and scrubs the
// content out of the audit rows it leaves behind.
//
// Two things about this are load-bearing and easy to get wrong:
//
// It is a real DELETE, never a soft-delete flag. `messages_fts_ad` is an AFTER
// DELETE trigger, so only a real delete removes the rows from the FTS index. A
// flag would leave every "deleted" message searchable, and therefore still
// reachable by cross-session recall — the system would keep remembering exactly
// what the user asked it to forget.
//
// The run ids must be CAPTURED before the cascade. audit_logs has no foreign
// keys; its only link to a session is correlation_id, which is the run id, and
// that resolves to a session only through agent_runs.session_id — a row the
// cascade deletes. Read those ids afterwards and you get an empty set, so the
// rows are never scrubbed and become unscrubbable orphans forever.
//
// Note it is the *capture* that is order-sensitive, not the scrub: audit_logs
// has no FK, so nothing cascades into it and the UPDATE works either side of
// the DELETE. Mutation-tested both ways — moving the scrub alone changes
// nothing, moving the capture breaks it.
func (r *Repository) DeleteSession(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return ErrSessionNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrSessionNotFound
	}

	// Refuse before mutating anything, so a rejected delete leaves no trace.
	var active int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agent_runs
		WHERE session_id = ? AND status IN ('queued','running','waiting_approval')
	`, sessionID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return ErrSessionHasActiveRun
	}

	// Capture the run ids first: after the DELETE below they are gone, and with
	// them the only path from an audit row back to this session.
	runIDs, err := sessionRunIDsTx(ctx, tx, sessionID)
	if err != nil {
		return err
	}
	if err := scrubAuditForRunsTx(ctx, tx, runIDs); err != nil {
		return err
	}

	// Let the FK graph do the deleting. Foreign keys are enforced
	// (`_foreign_keys=on` in the DSN), so this cascades to messages, agent_runs,
	// and from there to jobs, events, tool_calls and approvals. Hand-deleting
	// those here would duplicate the schema in Go and drift from it.
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
		return err
	}

	// The deletion is itself auditable, and is not scrubbed.
	if err := recordAuditTx(ctx, tx, sessionID, "client", "", "session.deleted", sessionID, ""); err != nil {
		return err
	}
	return tx.Commit()
}

func sessionRunIDsTx(ctx context.Context, tx *sql.Tx, sessionID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM agent_runs WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var runIDs []string
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			return nil, err
		}
		runIDs = append(runIDs, runID)
	}
	return runIDs, rows.Err()
}

// scrubAuditForRunsTx clears content from the audit rows correlated with these
// runs, leaving action, actor, target and timestamp intact. Correlated, not
// global: another session's audit must be untouched.
func scrubAuditForRunsTx(ctx context.Context, tx *sql.Tx, runIDs []string) error {
	for _, runID := range runIDs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE audit_logs SET payload_json = ? WHERE correlation_id = ?`,
			scrubbedAuditPayload, runID,
		); err != nil {
			return err
		}
	}
	return nil
}
