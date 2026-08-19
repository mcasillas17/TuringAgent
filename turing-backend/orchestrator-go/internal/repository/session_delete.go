package repository

import (
	"context"
	"encoding/json"
	"errors"
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
// The audit scrub runs BEFORE the cascade, as a single statement whose
// subquery resolves the run ids inline. audit_logs has no foreign key — most
// of its rows link to a session only through correlation_id, which is the run
// id, resolvable only through agent_runs.session_id, and the cascade deletes
// that. Doing it in one statement ahead of the DELETE means there is no
// snapshot to forget to take; an earlier version captured the ids into a slice
// first, which worked but left a real ordering trap for the next reader. The
// same statement also scrubs the routing rows, which link by target instead —
// see the call site.
func (r *Repository) DeleteSession(ctx context.Context, sessionID string) error {
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
	//
	// Status alone is not enough — two paths leave a run terminal-by-status with
	// execution still live:
	//   - CancelRun / CancelRunWithEvent set status='cancelled' and never touch
	//     execution_active at all (runs.go). This is the user-reachable one.
	//   - failRunWithEventTx(preserveExecution=true) keeps execution_active = 1
	//     with execution_state = 'uncertain'.
	// The recovery machinery treats both as in flight (assignments.go queries
	// execution_active = 1 alongside terminal statuses). Deleting then cascades
	// rows out from under a worker that has not acknowledged exit, and the
	// resulting unmapped sql.ErrNoRows tears down the whole ConnectWorker
	// stream — taking every other run that worker holds with it.
	var active int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agent_runs
		WHERE session_id = ?
			AND (status IN ('queued','running','waiting_approval') OR execution_active = 1)
	`, sessionID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return ErrSessionHasActiveRun
	}

	var runCount, messageCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs WHERE session_id = ?`, sessionID).Scan(&runCount); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&messageCount); err != nil {
		return err
	}

	// Two disjoint sets of rows, scrubbed in one statement so they cannot
	// diverge: rows correlated by one of this session's run ids, and the
	// routing rows. Routing is the exception the correlation rule misses —
	// SetSessionAgent / ClearSessionAgent write correlation_id NULL and put
	// the session id in target (external_agents.go), so a run-id scrub never
	// reaches them and the third-party endpoint, model, and agent display name
	// would outlive the conversation they describe. The action match is exact,
	// not a prefix, so it can only ever cover the two writers that use this
	// target convention; session.deleted is inserted after this statement and
	// is deliberately left PRESENT as the evidence of the deletion.
	if _, err := tx.ExecContext(ctx, `
		UPDATE audit_logs SET payload_json = ?
		WHERE correlation_id IN (SELECT id FROM agent_runs WHERE session_id = ?)
			OR (target = ? AND action IN ('session.routed', 'session.unrouted'))
	`, scrubbedAuditPayload, sessionID, sessionID); err != nil {
		return err
	}

	// Let the FK graph do the deleting. Foreign keys are enforced
	// (`_foreign_keys=on` in the DSN), so this cascades to messages, agent_runs,
	// and from there to jobs, events, tool_calls and approvals. Hand-deleting
	// those here would duplicate the schema in Go and drift from it.
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
		return err
	}

	// The deletion is itself auditable, and is not scrubbed. correlation_id is
	// left empty: every other call site puts a RUN id there, and reusing the
	// column for a session id would conflate two kinds under one index. The
	// session id is the target, which is what it means.
	//
	// Counts make the row evidence rather than a marker — "something was
	// deleted" is far less useful than how much.
	payload, err := json.Marshal(map[string]any{"runs": runCount, "messages": messageCount})
	if err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, "", "client", "", "session.deleted", sessionID, string(payload)); err != nil {
		return err
	}
	return tx.Commit()
}
