package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/skillfiles"
)

type Repository struct {
	db         *db.DB
	skillStore *skillfiles.Store
}

func New(database *db.DB) *Repository {
	return &Repository{db: database}
}

func (r *Repository) SetSkillStore(store *skillfiles.Store) {
	r.skillStore = store
}

type Session struct {
	SessionID string
	Title     sql.NullString
	Status    string
	CreatedAt string
	UpdatedAt string
}

type SessionListFilter string

const (
	SessionListActive   SessionListFilter = "active"
	SessionListArchived SessionListFilter = "archived"
	SessionListAll      SessionListFilter = "all"
)

var (
	ErrInvalidSessionStatus    = errors.New("invalid persisted session status")
	ErrInvalidSessionTimestamp = errors.New("invalid persisted session timestamp")
	ErrInvalidSessionFilter    = errors.New("invalid session list filter")
	ErrInvalidSessionPage      = errors.New("invalid session page")
)

type SessionCursor struct {
	UpdatedAt string
	SessionID string
}

type ListSessionsInput struct {
	Filter SessionListFilter
	After  *SessionCursor
	Limit  int
}

type Message struct {
	MessageID   string
	SessionID   string
	RunID       string
	Role        string
	Content     string
	ContentType string
	Sequence    int64
	CreatedAt   string
}

func now() string {
	return FormatTimestamp(time.Now())
}

func (r *Repository) CreateSession(ctx context.Context, title string) (Session, error) {
	createdAt := now()
	session := Session{SessionID: ids.New("sess"), Status: "active", CreatedAt: createdAt, UpdatedAt: createdAt}
	titleOrigin := "unset"
	if title != "" {
		session.Title = sql.NullString{String: title, Valid: true}
		titleOrigin = "explicit"
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO sessions (id, title, title_origin, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, session.SessionID, nullableString(session.Title), titleOrigin, createdAt, createdAt)
	return session, err
}

func (r *Repository) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 50
	}
	return r.ListSessionsPage(ctx, ListSessionsInput{
		Filter: SessionListActive,
		Limit:  limit,
	})
}

func (r *Repository) ListSessionsPage(ctx context.Context, input ListSessionsInput) ([]Session, error) {
	query, args, err := listSessionsQuery(input)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var sessions []Session
	for rows.Next() {
		var session Session
		if err := rows.Scan(&session.SessionID, &session.Title, &session.Status, &session.CreatedAt, &session.UpdatedAt); err != nil {
			return nil, err
		}
		if err := validateSession(session); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func listSessionsQuery(input ListSessionsInput) (string, []any, error) {
	if input.Limit <= 0 {
		return "", nil, ErrInvalidSessionPage
	}

	var query string
	var args []any
	switch input.Filter {
	case SessionListActive, SessionListArchived:
		query = `
			SELECT id, title, status, created_at, updated_at
			FROM sessions INDEXED BY idx_sessions_status_updated
			WHERE status = ?`
		args = append(args, string(input.Filter))
	case SessionListAll:
		query = `
			SELECT id, title, status, created_at, updated_at
			FROM sessions INDEXED BY idx_sessions_updated
			WHERE 1 = 1`
	default:
		return "", nil, ErrInvalidSessionFilter
	}
	if input.After != nil {
		if input.After.SessionID == "" {
			return "", nil, ErrInvalidSessionPage
		}
		if _, err := persisttime.ParseCanonical(input.After.UpdatedAt); err != nil {
			return "", nil, ErrInvalidSessionPage
		}
		query += ` AND (updated_at, id) < (?, ?)`
		args = append(args, input.After.UpdatedAt, input.After.SessionID)
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, input.Limit)
	return query, args, nil
}

func (r *Repository) GetSession(ctx context.Context, sessionID string) (Session, error) {
	var session Session
	err := r.db.QueryRowContext(ctx, `SELECT id, title, status, created_at, updated_at FROM sessions WHERE id = ?`, sessionID).Scan(&session.SessionID, &session.Title, &session.Status, &session.CreatedAt, &session.UpdatedAt)
	if err == nil {
		err = validateSession(session)
	}
	return session, err
}

func validateSession(session Session) error {
	switch session.Status {
	case string(SessionListActive), string(SessionListArchived):
	default:
		return ErrInvalidSessionStatus
	}
	if _, err := persisttime.ParseCanonical(session.CreatedAt); err != nil {
		return ErrInvalidSessionTimestamp
	}
	if _, err := persisttime.ParseCanonical(session.UpdatedAt); err != nil {
		return ErrInvalidSessionTimestamp
	}
	return nil
}

func (r *Repository) ListMessages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, COALESCE(run_id, ''), role, content, content_type, sequence, created_at FROM messages WHERE session_id = ? ORDER BY sequence DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var reversed []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.MessageID, &msg.RunID, &msg.Role, &msg.Content, &msg.ContentType, &msg.Sequence, &msg.CreatedAt); err != nil {
			return nil, err
		}
		reversed = append(reversed, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, nil
}

func (r *Repository) ListMessagesBefore(ctx context.Context, sessionID, beforeMessageID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	if beforeMessageID == "" {
		return r.ListMessages(ctx, sessionID, limit)
	}
	var boundarySequence int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT sequence
		FROM messages
		WHERE session_id = ? AND id = ?
	`, sessionID, beforeMessageID).Scan(&boundarySequence); err != nil {
		return nil, err
	}
	query := `
		SELECT m.id, COALESCE(m.run_id, ''), m.role, m.content, m.content_type, m.sequence, m.created_at
		FROM messages m
		WHERE m.session_id = ?
			AND m.sequence < ?
		ORDER BY m.sequence DESC, m.id DESC
		LIMIT ?
	`
	rows, err := r.db.QueryContext(ctx, query, sessionID, boundarySequence, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var reversed []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.MessageID, &msg.RunID, &msg.Role, &msg.Content, &msg.ContentType, &msg.Sequence, &msg.CreatedAt); err != nil {
			return nil, err
		}
		reversed = append(reversed, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, nil
}

func (r *Repository) SearchMessages(
	ctx context.Context,
	sessionID string,
	excludedSessionID string,
	query string,
	limit int,
) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query = strings.ReplaceAll(query, "\x00", " ")
	if !hasFTS5Token(query) {
		return []Message{}, nil
	}

	sqlQuery := `
		SELECT m.id, m.session_id, COALESCE(m.run_id, ''), m.role, m.content, m.content_type, m.sequence, m.created_at
		FROM messages_fts
		JOIN messages m ON m.rowid = messages_fts.rowid
		WHERE messages_fts MATCH ?`
	args := []any{fts5Phrase(query)}
	if sessionID != "" {
		sqlQuery += ` AND m.session_id = ?`
		args = append(args, sessionID)
	}
	if excludedSessionID != "" {
		sqlQuery += ` AND m.session_id <> ?`
		args = append(args, excludedSessionID)
	}
	sqlQuery += ` ORDER BY bm25(messages_fts), m.id LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var messages []Message
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.MessageID, &message.SessionID, &message.RunID, &message.Role, &message.Content, &message.ContentType, &message.Sequence, &message.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func fts5Phrase(query string) string {
	return `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
}

func hasFTS5Token(query string) bool {
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.In(r, unicode.Co) {
			return true
		}
	}
	return false
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
