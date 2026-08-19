package repository

import (
	"context"
	"database/sql"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

func (r *Repository) RecordAudit(ctx context.Context, correlationID string, actorType string, actorID string, action string, target string, payloadJSON string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := recordAuditTx(ctx, tx, correlationID, actorType, actorID, action, target, payloadJSON); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordAuditForExistingRun inserts only while correlationID still names a
// stored run. The existence check and insert share one SQLite statement, so a
// concurrent session deletion either scrubs this row afterward or wins first
// and prevents the row from being recreated.
func (r *Repository) RecordAuditForExistingRun(ctx context.Context, correlationID string, actorType string, actorID string, action string, target string, payloadJSON string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO audit_logs (
			id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at
		)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?
		WHERE EXISTS (SELECT 1 FROM agent_runs WHERE id = ?)
	`,
		ids.New("audit"),
		correlationID,
		actorType,
		nullableText(actorID),
		action,
		nullableText(target),
		nullableText(payloadJSON),
		now(),
		correlationID,
	)
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return inserted == 1, nil
}

// recordAuditTx is the transactional half, for callers that must record an
// action atomically with the change it describes — DeleteSession scrubs audit
// and deletes a session in one transaction, and its own audit row has to
// either land with that or not at all.
func recordAuditTx(ctx context.Context, tx *sql.Tx, correlationID string, actorType string, actorID string, action string, target string, payloadJSON string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_logs (id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ids.New("audit"), nullableText(correlationID), actorType, nullableText(actorID), action, nullableText(target), nullableText(payloadJSON), now())
	return err
}
