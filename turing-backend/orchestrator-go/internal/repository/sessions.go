package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runcorrelation"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/skillfiles"
)

type Repository struct {
	db         *db.DB
	skillStore *skillfiles.Store
	// memoryVault is the user's note vault. It is nil until SetMemoryVault
	// attaches one, and every memory method refuses rather than pretending
	// there is nothing to remember.
	memoryVault *memoryfiles.Vault
	// memoryVaultMutex serialises whole-vault passes. A file-writing reconcile
	// and an index refresh both derive their answer from one scan, and two
	// passes interleaving would let one write the projection of bytes the
	// other is in the middle of rewriting. It is taken in exactly one place,
	// runVaultPass, and held across the scan and the index transaction, never
	// across an unrelated call.
	memoryVaultMutex sync.Mutex
	// memoryVaultPassBarrier, when set (test-only; always nil in production),
	// runs inside a whole-vault pass: after the short state transaction has
	// closed and before the walk begins. A test can park one pass there and
	// prove that no other pass — read-only or writing — is inside the vault at
	// the same time, and that no database transaction is held while it is.
	memoryVaultPassBarrier func()
	// memoryReconcileScanAnchor overrides the timestamp a pass treats as "when
	// this walk started" (test-only; always empty in production). It lets a
	// test place a row on either side of the anchor without racing a clock.
	memoryReconcileScanAnchor string
	// memoryPromotionBarrier, when set (test-only; always nil in production),
	// runs after a promotion has moved the file and before the transaction
	// that would record it, so a test can prove what a failure in that window
	// leaves behind and that reconcile can finish it.
	memoryPromotionBarrier func() error
	// memoryCandidateWriteBarrier, when set (test-only; always nil in
	// production), runs after a candidate's path is reserved and before the
	// vault write, so a test can delete the session in exactly that window and
	// prove the bytes that land afterwards do not survive as an untracked file
	// in the user's vault.
	memoryCandidateWriteBarrier func() error
	// memoryCandidateRecordBarrier, when set (test-only; always nil in
	// production), runs after the candidate file is written and before the
	// transaction that records it, so a test can fail that transaction and
	// prove the file is removed again rather than left behind with no row.
	memoryCandidateRecordBarrier func() error
	// memoryIndexParkBarrier, when set (test-only; always nil in production),
	// runs inside the index transaction after contested paths have been
	// vacated and before the real ones are written, so a test can fail the
	// transaction in exactly that window and prove no note is left holding a
	// value that is not a vault path.
	memoryIndexParkBarrier func() error
	// mcpRegistrySnapshotBarrier, when set (test-only; always nil in
	// production), is invoked by MCPRegistrySnapshot once its single read
	// transaction is open and its aggregate tool-byte budget guard has
	// already run, immediately before it reads any server or tool row —
	// letting a test pause a snapshot mid-flight to prove a concurrent
	// write can never interleave with (or be interleaved by) it. See
	// MCPRegistrySnapshot's own comment.
	mcpRegistrySnapshotBarrier func()
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
	// RunState is the authoritative outcome of the run that produced this
	// message, present only when both directions of the run/message link agree.
	// Its ContentSHA256 is always empty: the digest is internal duplicate-report
	// identity, and the history reader below never selects it.
	RunState *RunState
}

// historyPageColumns is the message projection a bounded page selects. The
// session ID is read rather than assumed from the request, because the
// correlation rule compares the run's session against the message's own row.
const historyPageColumns = `id, session_id, run_id, role, content, content_type, sequence, created_at`

// historyJoinQuery wraps a bounded message page in the zero-or-one run join and
// the two claimant counts that decide whether the link is ambiguous.
//
// The page subquery is where the session predicate, any anchor boundary, and
// the limit are applied, so the page is decided entirely by message rows. Only
// then does the join reach agent_runs.id — the primary key, which matches at
// most once — so an embedded outcome cannot add, drop, or duplicate a turn. The
// outer ORDER BY is restated because a join does not inherit the subquery's
// order.
//
// The two scalar subqueries count the claimants on each side of the circular
// link: how many assistant turns name this row's run, and how many runs name
// this row as their answer. They are row-local, so they see the whole database
// rather than the page — which is the point. Whether ownership is ambiguous is
// a fact about the stored rows, and a page-scoped check would report it only
// when a client happened to ask for a window wide enough to contain both
// claimants, so the same corrupt database would fail one request and answer the
// next. Counting in this statement also keeps the cost flat: no round trip is
// added per message, and each count is a covering-index probe
// (idx_messages_assistant_run_unique and idx_runs_assistant_message_unique)
// rather than the scan a follow-up lookup per row would have cost.
//
// assistant_content_sha256 is deliberately absent from the projection. It is
// internal identity for duplicate terminal reports; a reader that never selects
// it cannot leak it.
func historyJoinQuery(page string, order string) string {
	return `
		WITH active_session AS (
			SELECT id
			FROM sessions
			WHERE id = ? AND deletion_state = 'active'
		),
		page AS (` + page + `)
		SELECT COALESCE(p.id, ''), COALESCE(p.session_id, ''), COALESCE(p.run_id, ''),
			COALESCE(p.role, ''), COALESCE(p.content, ''), COALESCE(p.content_type, ''),
			COALESCE(p.sequence, 0), COALESCE(p.created_at, ''),
			COALESCE(r.id, ''), COALESCE(r.session_id, ''), COALESCE(r.user_message_id, ''),
			COALESCE(r.assistant_message_id, ''), COALESCE(r.status, ''), COALESCE(r.outcome_reason, ''),
			COALESCE(r.state_version, 0), COALESCE(r.state_updated_at, ''), r.finished_at,
			(SELECT COUNT(*) FROM messages claimant
				WHERE claimant.run_id = p.run_id AND claimant.role = ?),
			(SELECT COUNT(*) FROM agent_runs owner
				WHERE owner.assistant_message_id = p.id)
		FROM active_session active
		LEFT JOIN page p ON TRUE
		LEFT JOIN agent_runs r ON r.id = p.run_id
		ORDER BY ` + order + `
	`
}

// historyJoinArgs appends the one argument historyJoinQuery adds of its own.
//
// The active-session ID and page arguments bind first; the role binds last. It
// is bound rather than spliced into the SQL because a literal is a second place
// the predicate could be edited; SQLite still reaches the partial index through
// it, since it chooses that index against the value actually bound and
// re-prepares when the value changes.
func historyJoinArgs(pageArgs ...any) []any {
	return append(pageArgs, runcorrelation.AssistantRole)
}

// The two shipped history statements, built once. They are package-level so a
// test can EXPLAIN exactly the text production sends rather than a
// reconstruction of it that could drift from it.
var (
	newestHistoryQuery = historyJoinQuery(`
		SELECT `+historyPageColumns+`
		FROM messages
		WHERE session_id = (SELECT id FROM active_session)
		ORDER BY sequence DESC
		LIMIT ?
	`, `p.sequence DESC`)

	olderHistoryQuery = historyJoinQuery(`
		SELECT `+historyPageColumns+`
		FROM messages
		WHERE session_id = (SELECT id FROM active_session)
			AND sequence < ?
		ORDER BY sequence DESC, id DESC
		LIMIT ?
	`, `p.sequence DESC, p.id DESC`)
)

// scanHistoryPage reads a joined page newest-first and returns it in causal
// order.
//
// A run row that joined is still only a candidate: the shared correlation rule
// decides whether it is this message's owner, and a link only one side agrees
// with yields the message with no state rather than somebody else's outcome.
func scanHistoryPage(rows *sql.Rows) ([]Message, error) {
	var reversed []Message
	sessionFound := false
	for rows.Next() {
		sessionFound = true
		var (
			message               Message
			messageSessionID      string
			runID                 string
			runSessionID          string
			runUserMessageID      string
			runAssistantMessageID string
			lifecycle             string
			outcomeReason         string
			stateVersion          int64
			stateUpdatedAt        string
			finishedAt            sql.NullString
			runClaimants          int64
			messageOwners         int64
		)
		if err := rows.Scan(
			&message.MessageID, &messageSessionID, &message.RunID, &message.Role, &message.Content,
			&message.ContentType, &message.Sequence, &message.CreatedAt,
			&runID, &runSessionID, &runUserMessageID, &runAssistantMessageID, &lifecycle,
			&outcomeReason, &stateVersion, &stateUpdatedAt, &finishedAt,
			&runClaimants, &messageOwners,
		); err != nil {
			return nil, err
		}
		// Two assistant turns claiming one run, or two runs claiming one turn,
		// is ambiguous ownership rather than a mismatch: nothing here can say
		// which of them the outcome belongs to. Both directions are forbidden
		// by a unique index, so reaching this means the database predates the
		// indexes or was restored from a corrupt backup — and the report says
		// only that, because a row value in an operator's log is a leak nothing
		// downstream can take back.
		if runClaimants > 1 || messageOwners > 1 {
			return nil, runcorrelation.ErrConflict
		}
		if runID != "" {
			link := runcorrelation.Link{
				RunID:                 runID,
				RunSessionID:          runSessionID,
				RunAssistantMessageID: runAssistantMessageID,
				MessageID:             message.MessageID,
				MessageSessionID:      messageSessionID,
				MessageRunID:          message.RunID,
				MessageRole:           message.Role,
			}
			if validateRunCorrelationLink(link) == nil {
				message.RunState = &RunState{
					RunID:              runID,
					UserMessageID:      runUserMessageID,
					AssistantMessageID: runAssistantMessageID,
					Lifecycle:          lifecycle,
					OutcomeReason:      outcomeReason,
					StateVersion:       stateVersion,
					StateUpdatedAt:     stateUpdatedAt,
					FinishedAt:         finishedAt,
					// The link proves this row is the run's canonical assistant
					// message, so its content is the authority on whether the
					// run produced anything worth rendering.
					HasDisplayableContent: runoutcome.HasDisplayableContent(message.Content),
				}
			}
		}
		if message.MessageID != "" {
			reversed = append(reversed, message)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !sessionFound {
		return nil, sql.ErrNoRows
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, nil
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
	rows, err := r.db.QueryContext(ctx, newestHistoryQuery, historyJoinArgs(sessionID, limit)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanHistoryPage(rows)
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
		FROM messages m
		JOIN sessions s ON s.id = m.session_id AND s.deletion_state = 'active'
		WHERE m.session_id = ? AND m.id = ?
	`, sessionID, beforeMessageID).Scan(&boundarySequence); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, olderHistoryQuery, historyJoinArgs(sessionID, boundarySequence, limit)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanHistoryPage(rows)
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
