package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
)

const sessionTimestampMigrationBatchSize = 256

type legacySessionTimestamps struct {
	id        string
	createdAt string
	updatedAt string
}

func normalizeSessionTimestamps(ctx context.Context, tx *sql.Tx) error {
	afterID := ""
	for {
		rows, err := tx.QueryContext(ctx, `
			SELECT id, created_at, updated_at
			FROM sessions
			WHERE id > ?
			ORDER BY id
			LIMIT ?`,
			afterID,
			sessionTimestampMigrationBatchSize,
		)
		if err != nil {
			return fmt.Errorf("read session timestamps: %w", err)
		}

		batch := make([]legacySessionTimestamps, 0, sessionTimestampMigrationBatchSize)
		for rows.Next() {
			var row legacySessionTimestamps
			if err := rows.Scan(&row.id, &row.createdAt, &row.updatedAt); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan session timestamps: %w", err)
			}
			batch = append(batch, row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate session timestamps: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close session timestamps: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}

		for _, row := range batch {
			createdAt, err := persisttime.ParseLegacy(row.createdAt)
			if err != nil {
				return fmt.Errorf("invalid sessions.created_at: %w", err)
			}
			updatedAt, err := persisttime.ParseLegacy(row.updatedAt)
			if err != nil {
				return fmt.Errorf("invalid sessions.updated_at: %w", err)
			}
			result, err := tx.ExecContext(ctx, `
				UPDATE sessions
				SET created_at = ?, updated_at = ?
				WHERE id = ? AND created_at = ? AND updated_at = ?`,
				persisttime.Format(createdAt),
				persisttime.Format(updatedAt),
				row.id,
				row.createdAt,
				row.updatedAt,
			)
			if err != nil {
				return fmt.Errorf("normalize session timestamps: %w", err)
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("inspect session timestamp update: %w", err)
			}
			if affected != 1 {
				return fmt.Errorf("session timestamp changed during migration")
			}
		}

		afterID = batch[len(batch)-1].id
		if len(batch) < sessionTimestampMigrationBatchSize {
			return nil
		}
	}
}
