package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// Note statuses mirror the CHECK constraint on memory_notes.status. 'managed'
// notes were written by Turing and may be rewritten by it, 'unmanaged' notes
// were hand-written in the vault and are read-only to Turing, and 'withdrawn'
// marks a note whose supporting evidence is gone.
const (
	MemoryNoteStatusManaged   = string(memoryfiles.NoteStatusManaged)
	MemoryNoteStatusUnmanaged = string(memoryfiles.NoteStatusUnmanaged)
	MemoryNoteStatusWithdrawn = "withdrawn"
)

const (
	// maxMemorySearchLimit caps one search page. Search is discovery over the
	// user's own memory, not an export of it.
	maxMemorySearchLimit = 50
	// maxMemorySearchQueryBytes bounds the query text before it reaches FTS5.
	maxMemorySearchQueryBytes = 512
)

var (
	// ErrMemoryNoteNotFound reports a note identity with no row in the index.
	ErrMemoryNoteNotFound = errors.New("memory note not found")
	// ErrMemoryNoteWithdrawn refuses a read of a note the index does not
	// answer from. It is the read-side half of the withdrawal search already
	// performs: a note whose conversations were deleted is kept, because the
	// user accepted it, but nothing may answer with it as if it were still
	// grounded — including a caller that still holds its identity from before
	// the withdrawal.
	ErrMemoryNoteWithdrawn = errors.New("memory note is not in a state a read answers from")

	// ErrMemorySearchQuery refuses a search whose bounds are outside what this
	// repository will run. A refusal is deliberate: silently clamping a limit
	// makes a caller believe it saw everything.
	ErrMemorySearchQuery = errors.New("memory search query is not valid")
)

// MemoryNote is one row of the note index: a copy of what a file said the last
// time a pass read it, plus the identity and status the sidecar owns. It is
// deliberately disposable — the file is the memory, and this can be rebuilt
// from the vault at any time.
type MemoryNote struct {
	NoteID      string
	Path        string
	Content     string
	ContentHash string
	Status      string
	CreatedAt   string
	UpdatedAt   string
}

// SearchMemoryNotes searches accepted memory and nothing else.
//
// Two scopes are enforced here rather than assumed upstream: only notes under
// beliefs/ (a candidate is unreviewed model output about the user and is never
// projected into this table at all, but the predicate says so anyway), and only
// notes that are not withdrawn (a note whose evidence is gone is not something
// to answer with).
func (r *Repository) SearchMemoryNotes(ctx context.Context, query string, limit int) ([]MemoryNote, error) {
	if limit <= 0 || limit > maxMemorySearchLimit {
		return nil, fmt.Errorf("%w: limit is outside 1..%d", ErrMemorySearchQuery, maxMemorySearchLimit)
	}
	if len(query) > maxMemorySearchQueryBytes {
		return nil, fmt.Errorf("%w: query exceeds %d bytes", ErrMemorySearchQuery, maxMemorySearchQueryBytes)
	}
	// NUL bytes cannot reach FTS5, and a query with no token at all matches
	// nothing — that is an empty result, not a failure.
	sanitized := strings.ReplaceAll(query, "\x00", " ")
	if !hasFTS5Token(sanitized) {
		return []MemoryNote{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT n.id, n.path, n.content, n.content_hash, n.status, n.created_at, n.updated_at
		FROM memory_notes_fts
		JOIN memory_notes n ON n.rowid = memory_notes_fts.rowid
		WHERE memory_notes_fts MATCH ?
		  AND n.status IN (?, ?)
		  AND n.path LIKE ? ESCAPE '\'
		ORDER BY bm25(memory_notes_fts), n.id
		LIMIT ?
	`,
		fts5Phrase(sanitized),
		MemoryNoteStatusManaged,
		MemoryNoteStatusUnmanaged,
		memoryfiles.BeliefsDirName+"/%",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	notes := make([]MemoryNote, 0, limit)
	for rows.Next() {
		note, err := scanMemoryNote(rows)
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, rows.Err()
}

// MemoryNoteByID reads one index row.
func (r *Repository) MemoryNoteByID(ctx context.Context, noteID string) (MemoryNote, error) {
	return memoryNoteByID(ctx, r.db, noteID)
}

// ReadMemoryBelief serves a belief's current bytes.
//
// There is no path argument and no scope argument. The caller names a stable
// identity; this resolves it through the index it owns, and the vault re-checks
// whatever path comes back against its own beliefs gate before opening
// anything. The bytes come from the file, never from the projection, because
// the user may have edited the note since the last pass.
//
// The index row also decides *whether* there is anything to serve. Search
// already answers only from notes that are managed or unmanaged; a read has to
// hold the same line, because a model that saw a belief before its conversation
// was deleted still has the identity in its context and could hand it straight
// back here. Withdrawing a memory from discovery and leaving it readable by id
// is not withdrawing it. The check is stated as "this is a status a read
// answers from" rather than "this is not withdrawn", so a status this build
// does not recognise fails closed too.
func (r *Repository) ReadMemoryBelief(ctx context.Context, noteID string) (memoryfiles.BeliefDocument, error) {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return memoryfiles.BeliefDocument{}, err
	}
	if strings.TrimSpace(noteID) == "" {
		return memoryfiles.BeliefDocument{}, fmt.Errorf("%w: a belief is read by identity", ErrMemoryNoteNotFound)
	}
	note, err := memoryNoteByID(ctx, r.db, noteID)
	if err != nil {
		return memoryfiles.BeliefDocument{}, err
	}
	if !memoryNoteStatusIsReadable(note.Status) {
		return memoryfiles.BeliefDocument{}, fmt.Errorf("%w: %s", ErrMemoryNoteWithdrawn, note.Status)
	}
	return vault.ReadBeliefByID(ctx, noteID, func(requested string) (string, bool) {
		return resolveMemoryBeliefPath(noteID, note.Path, requested)
	})
}

// memoryNoteStatusIsReadable is the allowlist, not a denylist. Two statuses are
// memory the user has: one Turing wrote and may rewrite, and one they wrote
// themselves. Everything else — a withdrawal, a status from a newer build, a
// damaged row — is something a read must not answer from.
func memoryNoteStatusIsReadable(status string) bool {
	return status == MemoryNoteStatusManaged || status == MemoryNoteStatusUnmanaged
}

// resolveMemoryBeliefPath answers the vault's path question for exactly one
// identity: the one that was looked up and so authorised by this read.
// Anything else means the vault asked a different question than the one that
// was authorised, and the answer to that is nothing at all.
func resolveMemoryBeliefPath(authorisedID string, authorisedPath string, requested string) (string, bool) {
	if requested != authorisedID {
		return "", false
	}
	return authorisedPath, true
}

// MemoryNoteEvidence is one conversation's support for a note, and how many
// times that conversation cited it. The count is what the vault page shows
// under a belief: "one message said this once" and "five messages said it over
// a year" are different reasons to keep believing something.
type MemoryNoteEvidence struct {
	SessionID string
	Count     int
}

// MemoryNoteEvidence lists the sessions still supporting a note together with
// how much each one supports it. Rows are deleted with their session, so a note
// whose conversations are all gone comes back empty rather than stale.
func (r *Repository) MemoryNoteEvidence(ctx context.Context, noteID string) ([]MemoryNoteEvidence, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT session_id, COUNT(*)
		FROM memory_evidence
		WHERE note_id = ?
		GROUP BY session_id
		ORDER BY session_id
	`, noteID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var evidence []MemoryNoteEvidence
	for rows.Next() {
		var row MemoryNoteEvidence
		if err := rows.Scan(&row.SessionID, &row.Count); err != nil {
			return nil, err
		}
		evidence = append(evidence, row)
	}
	return evidence, rows.Err()
}

// MemoryNoteEvidenceSessions lists the sessions still supporting a note, which
// is the sidecar's answer and the one a frontmatter rewrite is derived from.
func (r *Repository) MemoryNoteEvidenceSessions(ctx context.Context, noteID string) ([]string, error) {
	return memoryNoteEvidenceSessions(ctx, r.db, noteID)
}

func memoryNoteEvidenceSessions(ctx context.Context, q contextQuerier, noteID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT DISTINCT session_id
		FROM memory_evidence
		WHERE note_id = ?
		ORDER BY session_id
	`, noteID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var sessions []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return nil, err
		}
		sessions = append(sessions, sessionID)
	}
	return sessions, rows.Err()
}

func memoryNoteByID(ctx context.Context, q rowQuerier, noteID string) (MemoryNote, error) {
	note, err := scanMemoryNote(q.QueryRowContext(ctx, `
		SELECT id, path, content, content_hash, status, created_at, updated_at
		FROM memory_notes
		WHERE id = ?
	`, noteID))
	if errors.Is(err, sql.ErrNoRows) {
		return MemoryNote{}, ErrMemoryNoteNotFound
	}
	return note, err
}

func scanMemoryNote(scanner rowScanner) (MemoryNote, error) {
	var note MemoryNote
	if err := scanner.Scan(
		&note.NoteID,
		&note.Path,
		&note.Content,
		&note.ContentHash,
		&note.Status,
		&note.CreatedAt,
		&note.UpdatedAt,
	); err != nil {
		return MemoryNote{}, err
	}
	return note, nil
}

// upsertMemoryNoteTx writes one note's projection. Identity is the key, not the
// path: a note the user renamed in Obsidian is the same memory at a new name,
// and keying on the path would delete it and take its evidence with it.
func upsertMemoryNoteTx(ctx context.Context, tx *sql.Tx, note MemoryNote) error {
	timestamp := now()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO memory_notes (id, path, content, content_hash, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			path = excluded.path,
			content = excluded.content,
			content_hash = excluded.content_hash,
			status = excluded.status,
			updated_at = excluded.updated_at
	`, note.NoteID, note.Path, note.Content, note.ContentHash, note.Status, timestamp, timestamp)
	return err
}

// withdrawMemoryNotesLosingLastEvidenceTx marks every belief the session being
// deleted was the last support for, inside the transaction that removes it.
//
// The vault pass already does this, and doing it here as well is not
// duplication — it is the difference between a promise the database keeps and
// one it owes to a filesystem walk. `memory_evidence` cascades with the
// session, so the moment this transaction commits the citations are gone; the
// pass that would notice runs afterwards, outside the transaction, and refuses
// outright on a vault that is unreadable, past the scan bound or simply not
// attached. Between those two facts sits a claim about the user whose every
// conversation they deleted, still `managed` in the index, still returned by
// search, and still readable by identity — which is not a withdrawal at all.
//
// Only the index status is written here. The file in the vault keeps its own
// frontmatter until a pass can rewrite it, because that is a write into the
// user's folder and it is not something to hold a deletion open for. The index
// is what search and reads answer from, and it is the half that can be made
// true transactionally.
//
// "No surviving evidence from another session" is asked of the rows rather than
// of the file: a note two conversations support does not lose its grounding
// because one of them was deleted, and withdrawing it early would take a
// belief the user accepted away over a conversation they were entitled to
// remove.
// It returns the notes it withdrew, which is what the deletion transaction's
// test barrier is handed: a barrier that could only say "something ran" would
// leave the atomicity claim resting on a rollback that looks identical whether
// this ran at all.
func withdrawMemoryNotesLosingLastEvidenceTx(ctx context.Context, tx *sql.Tx, sessionID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT evidence.note_id
		FROM memory_evidence evidence
		JOIN memory_notes note ON note.id = evidence.note_id
		WHERE evidence.session_id = ?
			AND note.status <> ?
			AND NOT EXISTS (
				SELECT 1 FROM memory_evidence surviving
				WHERE surviving.note_id = evidence.note_id
					AND surviving.session_id <> ?
			)
		ORDER BY evidence.note_id
	`, sessionID, MemoryNoteStatusWithdrawn, sessionID)
	if err != nil {
		return nil, err
	}
	var noteIDs []string
	for rows.Next() {
		var noteID string
		if err := rows.Scan(&noteID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		noteIDs = append(noteIDs, noteID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// Collected first and written second so the audit says exactly what
	// changed. A retry after an unfinished completion re-enters with the
	// evidence already cascaded away, matches nothing, and writes nothing —
	// a log that counted attempts as withdrawals would be one nobody could
	// read the real events out of.
	for _, noteID := range noteIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE memory_notes SET status = ?, updated_at = ? WHERE id = ?
		`, MemoryNoteStatusWithdrawn, now(), noteID); err != nil {
			return nil, err
		}
		if err := recordMemoryReconcileTx(ctx, tx, memoryNoteWithdrawnAction, noteID, "evidence_gone"); err != nil {
			return nil, err
		}
	}
	return noteIDs, nil
}

// still exist. A ref naming a deleted session is dropped here rather than
// attempted: a foreign key failure would abort a heal that is trying to rescue
// a note the user accepted.
func liveSessionRefsTx(ctx context.Context, tx *sql.Tx, refs []string) ([]string, error) {
	live := make([]string, 0, len(refs))
	for _, ref := range sortedUniqueStrings(refs) {
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, ref).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		live = append(live, ref)
	}
	return live, nil
}

// linkMemoryEvidenceTx records what a note is grounded in, keeping only a hash
// of what each citation supports — never the excerpt itself. It must run after
// the note row exists: evidence is owned by the note, and the foreign key says
// so.
//
// excerptHash digests the content the citation stands behind. It is
// deliberately not a digest of the session id: that would be a fingerprint of
// the conversation, which is already stored in plain text in the column beside
// it, and would say nothing at all about the claim the citation is supposed to
// support. Hashing the note's own bytes keeps the row non-reversible and makes
// it mean something — it names which version of the claim was cited.
//
// Linking is idempotent per (note, session). Two writers legitimately reach the
// same citation — an index refresh healing a belief in the window between a
// promotion moving the file and the transaction that records it, and then the
// promotion itself — and a second row for the same conversation would inflate
// every count of what a memory rests on. The schema has no unique constraint to
// lean on, so the check is stated in the same statement as the insert rather
// than in a read the other writer could interleave with.
func linkMemoryEvidenceTx(ctx context.Context, tx *sql.Tx, noteID string, excerptHash string, liveRefs []string) error {
	for _, ref := range liveRefs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memory_evidence (id, note_id, session_id, excerpt_hash, created_at)
			SELECT ?, ?, ?, ?, ?
			WHERE NOT EXISTS (
				SELECT 1 FROM memory_evidence WHERE note_id = ? AND session_id = ?
			)
		`, ids.New("memev"), noteID, ref, excerptHash, now(), noteID, ref); err != nil {
			return err
		}
	}
	return nil
}

// updateMemoryNoteContentTx catches the projection up with a file this pass
// rewrote, without touching the status the same pass just decided.
func updateMemoryNoteContentTx(ctx context.Context, tx *sql.Tx, noteID string, content string, contentHash string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE memory_notes
		SET content = ?, content_hash = ?, updated_at = ?
		WHERE id = ?
	`, content, contentHash, now(), noteID)
	return err
}

func sortedUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func equalStringSets(first []string, second []string) bool {
	left := sortedUniqueStrings(first)
	right := sortedUniqueStrings(second)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
