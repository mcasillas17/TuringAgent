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

// recordAuditTx is the transactional half, for callers that must record an
// action atomically with the change it describes — DeleteSession scrubs audit
// and deletes a session in one transaction, and its own audit row has to
// either land with that or not at all.
func recordAuditTx(ctx context.Context, tx *sql.Tx, correlationID string, actorType string, actorID string, action string, target string, payloadJSON string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_logs (id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ids.New("audit"), nullableText(correlationID), actorType, nullableText(actorID), action, nullableText(target), nullableText(payloadJSON), now())
	return err
}
