package repository

import (
	"context"
	"database/sql"
	"errors"
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

// nextMessageSlotTx returns the sequence and creation time for the next
// message pair at the end of a session. The time is after both the session's
// activity time and its latest message, so ordering by either stays stable.
func nextMessageSlotTx(ctx context.Context, tx *sql.Tx, sessionID string, candidate time.Time) (int64, time.Time, error) {
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE session_id = ?`, sessionID).Scan(&next); err != nil {
		return 0, time.Time{}, err
	}
	var latestCreatedAt string
	err := tx.QueryRowContext(ctx, `SELECT created_at FROM messages WHERE session_id = ? ORDER BY `+
		sqliteTimestampNanos("created_at")+` DESC, id DESC LIMIT 1`, sessionID).Scan(&latestCreatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, err
	}
	var anchors []time.Time
	if latestCreatedAt != "" {
		latest, parseErr := time.Parse(time.RFC3339Nano, latestCreatedAt)
		if parseErr != nil {
			return 0, time.Time{}, parseErr
		}
		anchors = append(anchors, latest)
	}
	created, err := nextSessionActivityTimeTx(ctx, tx, sessionID, candidate, anchors...)
	if err != nil {
		return 0, time.Time{}, err
	}
	return next, created, nil
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
