package repository

import (
	"context"
	"database/sql"
	"strings"
)

// legacyPlaceholderTitle is the literal the client used to send on every
// create, before naming moved to the backend. It was never a choice a user
// made — there was no way to type it — so a session carrying it is untitled
// in every sense except the database's.
const legacyPlaceholderTitle = "New chat"

// maxTitleRunes bounds a derived title to about one line of the client's
// conversation list. Titles are counted in runes rather than bytes, so a
// message in a non-Latin script is not cut to a third of the length of an
// English one. Runes are not grapheme clusters and CJK is double-width, so
// this is a bound on code points, not on rendered width: a combining accent
// or an emoji ZWJ sequence sitting exactly on the boundary can still be
// split. Fixing that properly needs a grapheme library the repo does not
// carry, and the failure mode is a slightly odd final character in a title.
const maxTitleRunes = 60

// minWordBoundaryRunes is the shortest title we will accept from a word-boundary
// cut. Without it, a first "word" longer than the budget (a URL, a pasted
// stack frame, a base64 blob) would collapse the title to whatever tiny
// fragment preceded it, or to nothing at all.
const minWordBoundaryRunes = maxTitleRunes / 2

// DeriveSessionTitle turns the first thing a user said into a conversation
// title. It returns "" when the message carries no usable text, which callers
// treat as "leave this session untitled" rather than as a title of "".
//
// The result is deliberately a truncation of the user's own words rather than
// a model-generated summary: it is instant, deterministic, costs no tokens,
// and cannot hallucinate. A summarising pass could be layered on later without
// changing the storage contract.
func DeriveSessionTitle(content string) string {
	// strings.Fields splits on every unicode space, so this collapses newlines,
	// tabs and runs of spaces in one step. A pasted multi-line message becomes
	// a single line instead of a title with a line break buried in it.
	collapsed := strings.Join(strings.Fields(content), " ")
	if collapsed == "" {
		return ""
	}

	runes := []rune(collapsed)
	if len(runes) <= maxTitleRunes {
		return collapsed
	}

	truncated := string(runes[:maxTitleRunes])
	// Cut back to the last space so the title ends on a whole word — but only
	// when doing so leaves something worth reading.
	if idx := strings.LastIndex(truncated, " "); idx > 0 {
		if candidate := truncated[:idx]; len([]rune(candidate)) >= minWordBoundaryRunes {
			truncated = candidate
		}
	}
	return strings.TrimRight(truncated, " ") + "…"
}

// BackfillSessionTitles names conversations that predate backend naming.
//
// Without it, every conversation a user already had stays called "New chat"
// forever: naming happens when a message is enqueued, so a session they never
// write to again would never be reached. Run at startup; it is idempotent,
// because a session it names no longer matches the query that selects it.
//
// It reuses DeriveSessionTitle rather than approximating the same truncation
// in SQL, so a title assigned here is byte-identical to one assigned live.
func (r *Repository) BackfillSessionTitles(ctx context.Context) (int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, (
			SELECT m.content
			FROM messages m
			WHERE m.session_id = s.id AND m.role = 'user'
			ORDER BY m.sequence
			LIMIT 1
		)
		FROM sessions s
		WHERE s.title IS NULL OR s.title = '' OR s.title = ?
	`, legacyPlaceholderTitle)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	titles := map[string]string{}
	for rows.Next() {
		var sessionID string
		var content sql.NullString
		if err := rows.Scan(&sessionID, &content); err != nil {
			return 0, err
		}
		if !content.Valid {
			// Never had a user message, so there is nothing to name it after.
			// It keeps showing the client's placeholder, which is accurate.
			continue
		}
		if title := DeriveSessionTitle(content.String); title != "" {
			titles[sessionID] = title
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	// Collected first, then written: SQLite will not let this connection write
	// through a cursor it is still reading from.
	for sessionID, title := range titles {
		if _, err := r.db.ExecContext(ctx, `UPDATE sessions SET title = ? WHERE id = ?`, title, sessionID); err != nil {
			return 0, err
		}
	}
	return len(titles), nil
}
