package repository

import (
	"context"
	"database/sql"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

type Event struct {
	EventID     string
	SessionID   string
	RunID       sql.NullString
	TraceID     string
	Sequence    int64
	Type        string
	PayloadJSON string
	CreatedAt   string
}

type AppendEventInput struct {
	SessionID   string
	RunID       string
	TraceID     string
	Type        string
	PayloadJSON string
}

func (r *Repository) AppendEvent(ctx context.Context, input AppendEventInput) (Event, error) {
	createdAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM events WHERE session_id = ?`, input.SessionID).Scan(&next); err != nil {
		return Event{}, err
	}
	event := Event{EventID: ids.New("evt"), SessionID: input.SessionID, TraceID: input.TraceID, Sequence: next, Type: input.Type, PayloadJSON: input.PayloadJSON, CreatedAt: createdAt}
	var runID any
	if input.RunID != "" {
		event.RunID = sql.NullString{String: input.RunID, Valid: true}
		runID = input.RunID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events (id, session_id, run_id, trace_id, sequence, type, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, event.EventID, event.SessionID, runID, event.TraceID, event.Sequence, event.Type, event.PayloadJSON, event.CreatedAt); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (r *Repository) ReplayEvents(ctx context.Context, sessionID string, afterSequence int64, limit int) ([]Event, int64, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	if _, err := r.GetSession(ctx, sessionID); err != nil {
		if err == sql.ErrNoRows {
			return nil, 0, ErrSessionNotFound
		}
		return nil, 0, err
	}

	var latest int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(e.sequence), 0)
		FROM events e
		JOIN sessions s ON s.id = e.session_id AND s.deletion_state = 'active'
		WHERE e.session_id = ?
	`, sessionID).Scan(&latest); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.id, e.session_id, e.run_id, e.trace_id, e.sequence, e.type, e.payload_json, e.created_at
		FROM events e
		JOIN sessions s ON s.id = e.session_id AND s.deletion_state = 'active'
		WHERE e.session_id = ? AND e.sequence > ?
		ORDER BY e.sequence
		LIMIT ?
	`, sessionID, afterSequence, limit)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var events []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.EventID, &event.SessionID, &event.RunID, &event.TraceID, &event.Sequence, &event.Type, &event.PayloadJSON, &event.CreatedAt); err != nil {
			return nil, 0, err
		}
		events = append(events, event)
	}
	return events, latest, rows.Err()
}

func (r *Repository) ListLatestSessionUpdatedEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	query := `
		WITH page AS (
			SELECT id, updated_at
			FROM sessions
			WHERE deletion_state = 'active'
			ORDER BY ` + sqliteTimestampNanos("updated_at") + ` DESC, id DESC
			LIMIT ?
		),
		latest AS (
			SELECT e.session_id, MAX(e.sequence) AS sequence
			FROM events e
			JOIN page ON page.id = e.session_id
			WHERE e.type = 'session.updated'
			GROUP BY e.session_id
		)
		SELECT e.id, e.session_id, e.run_id, e.trace_id, e.sequence, e.type, e.payload_json, e.created_at
		FROM events e
		JOIN latest
			ON latest.session_id = e.session_id
			AND latest.sequence = e.sequence
		JOIN page ON page.id = e.session_id
		ORDER BY ` + sqliteTimestampNanos("page.updated_at") + ` DESC, page.id DESC
	`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(
			&event.EventID,
			&event.SessionID,
			&event.RunID,
			&event.TraceID,
			&event.Sequence,
			&event.Type,
			&event.PayloadJSON,
			&event.CreatedAt,
		); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
