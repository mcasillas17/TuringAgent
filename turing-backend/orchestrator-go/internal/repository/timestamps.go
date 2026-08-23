package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
)

// FormatTimestamp renders the canonical persisted timestamp. The layout itself
// lives in persisttime so repository writes and migration rewrites cannot drift
// apart; every timestamp this package persists is rendered here.
//
// This is a rule about writes. Reading a legacy row still parses the older
// variable-width RFC 3339 forms, because those are what earlier code wrote and
// they are not going to be rewritten by being read.
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
	return nextSessionActivityTime(currentText, candidate, additionalAnchors...)
}

func nextSessionActivityTime(
	currentText string,
	candidate time.Time,
	additionalAnchors ...time.Time,
) (time.Time, error) {
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
