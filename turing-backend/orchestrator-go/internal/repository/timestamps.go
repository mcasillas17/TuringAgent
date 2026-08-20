package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
)

func FormatTimestamp(value time.Time) string {
	return persisttime.Format(value)
}

func nextSessionActivityTimeTx(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
	candidate time.Time,
	additionalAnchors ...time.Time,
) (time.Time, error) {
	var currentText string
	if err := tx.QueryRowContext(ctx, `
		SELECT updated_at FROM sessions WHERE id = ?`,
		sessionID,
	).Scan(&currentText); err != nil {
		return time.Time{}, err
	}
	current, err := persisttime.ParseCanonical(currentText)
	if err != nil {
		return time.Time{}, ErrInvalidSessionTimestamp
	}
	anchors := append([]time.Time{current}, additionalAnchors...)
	for _, anchor := range anchors {
		if !candidate.After(anchor) {
			candidate = anchor.Add(time.Nanosecond)
		}
	}
	return candidate, nil
}

func sqliteTimestampNanos(column string) string {
	return strings.ReplaceAll(`(
		CAST(strftime('%s', substr(__timestamp__, 1, 19) || 'Z') AS INTEGER) * 1000000000 +
		CASE
			WHEN instr(__timestamp__, '.') = 0 THEN 0
			ELSE CAST(substr(
				substr(__timestamp__, instr(__timestamp__, '.') + 1, length(__timestamp__) - instr(__timestamp__, '.') - 1) || '000000000',
				1,
				9
			) AS INTEGER)
		END
	)`, "__timestamp__", column)
}
