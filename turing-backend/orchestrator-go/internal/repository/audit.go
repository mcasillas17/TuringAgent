package repository

import (
	"context"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

func (r *Repository) RecordAudit(ctx context.Context, correlationID string, actorType string, actorID string, action string, target string, payloadJSON string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs (id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, ids.New("audit"), nullableText(correlationID), actorType, nullableText(actorID), action, nullableText(target), nullableText(payloadJSON), now()); err != nil {
		return err
	}
	return tx.Commit()
}
