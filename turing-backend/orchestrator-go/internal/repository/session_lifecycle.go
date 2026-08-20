package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

const MaxSessionTitleRunes = 120

var ErrInvalidSessionTitle = errors.New("invalid session title")

type SessionMutationResult struct {
	Session Session
	Event   Event
	Changed bool
}

type sessionMutation func(Session) (Session, bool)

func (r *Repository) RenameSession(ctx context.Context, sessionID, title string) (SessionMutationResult, error) {
	normalized := strings.TrimSpace(title)
	if normalized == "" || !utf8.ValidString(normalized) || utf8.RuneCountInString(normalized) > MaxSessionTitleRunes {
		return SessionMutationResult{}, ErrInvalidSessionTitle
	}
	return r.mutateSession(ctx, sessionID, func(current Session) (Session, bool) {
		if current.Title.Valid && current.Title.String == normalized && current.TitleOrigin == "explicit" {
			return current, false
		}
		current.Title = sql.NullString{String: normalized, Valid: true}
		current.TitleOrigin = "explicit"
		return current, true
	})
}

func (r *Repository) ArchiveSession(ctx context.Context, sessionID string) (SessionMutationResult, error) {
	return r.setSessionStatus(ctx, sessionID, string(SessionListArchived))
}

func (r *Repository) RestoreSession(ctx context.Context, sessionID string) (SessionMutationResult, error) {
	return r.setSessionStatus(ctx, sessionID, string(SessionListActive))
}

func (r *Repository) setSessionStatus(ctx context.Context, sessionID, target string) (SessionMutationResult, error) {
	return r.mutateSession(ctx, sessionID, func(current Session) (Session, bool) {
		if current.Status == target {
			return current, false
		}
		current.Status = target
		return current, true
	})
}

func (r *Repository) mutateSession(
	ctx context.Context,
	sessionID string,
	mutate sessionMutation,
) (SessionMutationResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionMutationResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	current, err := getSessionTx(ctx, tx, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionMutationResult{}, ErrSessionNotFound
	}
	if err != nil {
		return SessionMutationResult{}, err
	}
	next, changed := mutate(current)
	if !changed {
		if err := tx.Commit(); err != nil {
			return SessionMutationResult{}, err
		}
		return SessionMutationResult{Session: current}, nil
	}

	activityTime, err := nextSessionActivityTime(current.UpdatedAt, time.Now().UTC())
	if err != nil {
		return SessionMutationResult{}, err
	}
	next.UpdatedAt = FormatTimestamp(activityTime)
	result, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET title = ?, title_origin = ?, status = ?, updated_at = ?
		WHERE id = ?`,
		nullableString(next.Title),
		next.TitleOrigin,
		next.Status,
		next.UpdatedAt,
		sessionID,
	)
	if err != nil {
		return SessionMutationResult{}, err
	}
	if err := expectOneRow(result, "session not found during lifecycle mutation"); err != nil {
		return SessionMutationResult{}, err
	}

	payload, err := json.Marshal(map[string]string{
		"title":     next.Title.String,
		"status":    next.Status,
		"updatedAt": next.UpdatedAt,
	})
	if err != nil {
		return SessionMutationResult{}, err
	}
	event, err := appendSessionEventTx(
		ctx,
		tx,
		sessionID,
		ids.New("trace"),
		"session.updated",
		string(payload),
		next.UpdatedAt,
	)
	if err != nil {
		return SessionMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionMutationResult{}, err
	}
	return SessionMutationResult{
		Session: next,
		Event:   event,
		Changed: true,
	}, nil
}

func getSessionTx(ctx context.Context, tx *sql.Tx, sessionID string) (Session, error) {
	var session Session
	err := tx.QueryRowContext(ctx, `
		SELECT id, title, title_origin, status, created_at, updated_at
		FROM sessions
		WHERE id = ?`,
		sessionID,
	).Scan(
		&session.SessionID,
		&session.Title,
		&session.TitleOrigin,
		&session.Status,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err != nil {
		return Session{}, err
	}
	if err := validateSession(session); err != nil {
		return Session{}, err
	}
	return session, nil
}
