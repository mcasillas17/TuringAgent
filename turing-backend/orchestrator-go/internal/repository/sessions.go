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
	SessionID   string
	Title       sql.NullString
	TitleOrigin string
	Status      string
	CreatedAt   string
	UpdatedAt   string
}

type SessionListFilter string

const (
	SessionListActive   SessionListFilter = "active"
	SessionListArchived SessionListFilter = "archived"
	SessionListAll      SessionListFilter = "all"
)

var (
	ErrInvalidSessionStatus      = errors.New("invalid persisted session status")
	ErrInvalidSessionTimestamp   = errors.New("invalid persisted session timestamp")
	ErrInvalidSessionTitleOrigin = errors.New("invalid persisted session title origin")
	ErrInvalidSessionFilter      = errors.New("invalid session list filter")
	ErrInvalidSessionPage        = errors.New("invalid session page")
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
	session.TitleOrigin = titleOrigin
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
		if err := rows.Scan(&session.SessionID, &session.Title, &session.TitleOrigin, &session.Status, &session.CreatedAt, &session.UpdatedAt); err != nil {
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
			SELECT id, title, title_origin, status, created_at, updated_at
			FROM sessions INDEXED BY idx_sessions_status_updated
			WHERE deletion_state = 'active' AND status = ?`
		args = append(args, string(input.Filter))
	case SessionListAll:
		query = `
			SELECT id, title, title_origin, status, created_at, updated_at
			FROM sessions INDEXED BY idx_sessions_updated
			WHERE deletion_state = 'active'`
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
	err := r.db.QueryRowContext(ctx, `
		SELECT id, title, title_origin, status, created_at, updated_at
		FROM sessions
		WHERE id = ? AND deletion_state = 'active'`,
		sessionID,
	).Scan(
		&session.SessionID,
		&session.Title,
		&session.TitleOrigin,
		&session.Status,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err == nil {
		err = validateSession(session)
	}
	return session, err
}

func validateSession(session Session) error {
	switch session.TitleOrigin {
	case "unset", "explicit", "derived":
	default:
		return ErrInvalidSessionTitleOrigin
	}
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

func requireActiveSessionTx(ctx context.Context, tx *sql.Tx, sessionID string) error {
	var deletionState string
	err := tx.QueryRowContext(ctx, `SELECT deletion_state FROM sessions WHERE id = ?`, sessionID).Scan(&deletionState)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSessionNotFound
	}
	if err != nil {
		return err
	}
	if deletionState != "active" {
		return ErrSessionDeleting
	}
	return nil
}

func (r *Repository) ListMessages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	if _, err := r.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.id, COALESCE(m.run_id, ''), m.role, m.content, m.content_type, m.sequence, m.created_at
		FROM messages m
		JOIN sessions s ON s.id = m.session_id AND s.deletion_state = 'active'
		WHERE m.session_id = ?
		ORDER BY m.sequence DESC
		LIMIT ?
	`, sessionID, limit)
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
	if _, err := r.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	if beforeMessageID == "" {
		return r.ListMessages(ctx, sessionID, limit)
	}
	var boundarySequence int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT sequence
		FROM messages m
		JOIN sessions s ON s.id = m.session_id AND s.deletion_state = 'active'
		WHERE m.session_id = ? AND m.id = ?
	`, sessionID, beforeMessageID).Scan(&boundarySequence); err != nil {
		return nil, err
	}
	query := `
		SELECT m.id, COALESCE(m.run_id, ''), m.role, m.content, m.content_type, m.sequence, m.created_at
		FROM messages m
		JOIN sessions s ON s.id = m.session_id AND s.deletion_state = 'active'
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

// searchMessagesInput is the caller-supplied part of a message search. Both
// projections take exactly these knobs; anything else would be a difference in
// what the two searches are allowed to see.
type searchMessagesInput struct {
	sessionID         string
	excludedSessionID string
	query             string
	limit             int
}

// searchMessagesPredicate builds the FROM/WHERE/ORDER/LIMIT fragment shared by
// the legacy message projection and the scored hit projection, together with
// its bound arguments in placeholder order.
//
// Sharing it is the point: lifecycle visibility, literal-phrase handling, scope,
// exclusion, ordering, and the limit domain are search's authorization and
// determinism rules, and two copies of them could drift apart silently. Each
// projection only chooses its own SELECT list and prepends its own arguments,
// so the returned arguments always come last in that order.
//
// The false result means the query has no FTS5 token at all. That is a
// successful empty search, not an error, and callers return their own empty
// result rather than executing a statement that cannot match anything.
func searchMessagesPredicate(input searchMessagesInput) (string, []any, bool) {
	limit := input.limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query := strings.ReplaceAll(input.query, "\x00", " ")
	if !hasFTS5Token(query) {
		return "", nil, false
	}

	// The status predicate is redundant with today's schema CHECK and is stated
	// anyway: a future lifecycle status must decide explicitly whether its
	// conversations are searchable instead of becoming searchable by default.
	predicate := `
		FROM messages_fts
		JOIN messages m ON m.rowid = messages_fts.rowid
		JOIN sessions s
		  ON s.id = m.session_id
		 AND s.deletion_state = 'active'
		 AND s.status IN ('active', 'archived')
		WHERE messages_fts MATCH ?`
	args := []any{fts5Phrase(query)}
	if input.sessionID != "" {
		predicate += ` AND m.session_id = ?`
		args = append(args, input.sessionID)
	}
	if input.excludedSessionID != "" {
		predicate += ` AND m.session_id <> ?`
		args = append(args, input.excludedSessionID)
	}
	predicate += ` ORDER BY bm25(messages_fts), m.id LIMIT ?`
	args = append(args, limit)
	return predicate, args, true
}

func (r *Repository) SearchMessages(
	ctx context.Context,
	sessionID string,
	excludedSessionID string,
	query string,
	limit int,
) ([]Message, error) {
	predicate, args, ok := searchMessagesPredicate(searchMessagesInput{
		sessionID:         sessionID,
		excludedSessionID: excludedSessionID,
		query:             query,
		limit:             limit,
	})
	if !ok {
		return []Message{}, nil
	}

	sqlQuery := `
		SELECT m.id, m.session_id, COALESCE(m.run_id, ''), m.role, m.content, m.content_type, m.sequence, m.created_at` +
		predicate

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
