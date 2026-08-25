package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// Candidate kinds mirror the CHECK constraint on memory_candidates.kind and
// the note kinds the vault will act on. They are the same two words in both
// places on purpose: a kind that means one thing to the database and another
// to the file layer is how a profile edit ends up promoted as a belief.
const (
	MemoryCandidateKindBelief      = string(memoryfiles.KindBelief)
	MemoryCandidateKindProfileEdit = string(memoryfiles.KindProfileEdit)
)

// Candidate states mirror the CHECK constraint on memory_candidates.state.
// Only 'pending' is a live state; the other three are decisions, and a decided
// candidate is never reopened.
const (
	MemoryCandidateStatePending   = "pending"
	MemoryCandidateStatePromoted  = "promoted"
	MemoryCandidateStateRejected  = "rejected"
	MemoryCandidateStateWithdrawn = "withdrawn"
)

const (
	// maxMemoryCandidateBodyRunes is the schema's own bound on the stored body,
	// restated here so an over-long claim is refused legibly instead of
	// arriving as a CHECK constraint failure. SQLite's length() counts
	// characters, so this is a character bound; memoryfiles bounds the same
	// body in bytes. Both are refusals — a claim about the user that was
	// silently cut in half is a different claim.
	maxMemoryCandidateBodyRunes = 4096
	// maxMemoryCandidateTitleRunes bounds a model-supplied title. The vault
	// bounds it too; this stops an unbounded string reaching the file layer.
	maxMemoryCandidateTitleRunes = 200
	// maxMemoryEvidenceRefs and maxMemoryEvidenceRefBytes bound the provenance
	// list. Refs name sessions, so a well-formed list is short and each entry
	// is an identifier.
	maxMemoryEvidenceRefs     = 32
	maxMemoryEvidenceRefBytes = 128
	// maxMemoryCandidateListLimit is the hard ceiling on one candidate listing.
	maxMemoryCandidateListLimit = 200
)

var (
	// ErrMemoryVaultUnavailable reports a repository asked to touch the vault
	// before one was attached. Memory is refused rather than skipped: silently
	// doing nothing would look identical to a note that failed to save.
	ErrMemoryVaultUnavailable = errors.New("memory vault is not attached")
	// ErrMemoryCandidateKind refuses a kind that is not one of the two the
	// schema and the vault both recognise.
	ErrMemoryCandidateKind = errors.New("memory candidate kind is not recognised")
	// ErrMemoryCandidateBody refuses an empty or over-long claim.
	ErrMemoryCandidateBody = errors.New("memory candidate body is empty or too large")
	// ErrMemoryCandidateEvidence refuses a malformed provenance list.
	ErrMemoryCandidateEvidence = errors.New("memory candidate evidence refs are not well formed")
	// ErrMemoryCandidateNotFound reports a candidate id with no row.
	ErrMemoryCandidateNotFound = errors.New("memory candidate not found")
	// ErrMemoryCandidateInvalidTransition refuses a lifecycle move the state
	// machine does not allow, including any move out of a decided state.
	ErrMemoryCandidateInvalidTransition = errors.New("memory candidate state transition is not allowed")
	// ErrMemoryCandidateQuery refuses a listing whose filters or bounds are
	// outside what this repository will run.
	ErrMemoryCandidateQuery = errors.New("memory candidate query is not valid")
	// ErrMemoryVaultPathMismatch reports a write that did not land where the
	// manifest reserved it. It should be unreachable — the reservation is
	// computed from the same rule the write uses — and is checked anyway,
	// because the alternative is a file in the user's vault that the manifest
	// does not name.
	ErrMemoryVaultPathMismatch = errors.New("memory vault write did not land on the reserved path")
)

// MemoryCandidate is one proposal waiting in the vault inbox. Everything that
// identifies it — its id, its path, the session that produced it — is derived
// by the server; the model supplies only the claim and its title.
type MemoryCandidate struct {
	CandidateID     string
	SourceSessionID string
	Kind            string
	InboxPath       string
	ContentHash     string
	Body            string
	EvidenceRefs    []string
	State           string
	PromotedNoteID  string
	DecidedAt       string
	CreatedAt       string
	UpdatedAt       string
}

// CreateMemoryCandidateInput is everything a caller may supply. There is
// deliberately no candidate id, no path and no created-at: provenance is what
// makes a candidate withdrawable when a conversation is deleted, so it is
// derived from the session the run belongs to rather than accepted from
// whoever is asking.
type CreateMemoryCandidateInput struct {
	SessionID    string
	Kind         string
	Title        string
	Body         string
	EvidenceRefs []string
}

// MemoryCandidateQuery bounds one candidate listing.
type MemoryCandidateQuery struct {
	SessionID string
	State     string
	Limit     int
}

// SetMemoryVault attaches the vault this repository reads and writes notes
// through. It is set once at startup, like the skill store beside it.
func (r *Repository) SetMemoryVault(vault *memoryfiles.Vault) {
	r.memoryVault = vault
}

func (r *Repository) memoryVaultOrError() (*memoryfiles.Vault, error) {
	if r.memoryVault == nil {
		return nil, ErrMemoryVaultUnavailable
	}
	return r.memoryVault, nil
}

// CreateMemoryCandidate records a proposal in the vault inbox.
//
// The order is the point and is not an implementation detail: the manifest row
// is committed before a single byte reaches the vault. A crash in between
// leaves a reservation naming a file that does not exist, which reconcile and
// the session cleaner both tolerate; the reverse order would leave a file in
// the user's vault that nothing knows about, and no later pass can discover
// what it does not have a record of.
//
// The reserved path is not a guess. The identity is minted here and the path
// is computed from the same rule the vault write uses, so the reservation and
// the write provably name the same file.
func (r *Repository) CreateMemoryCandidate(ctx context.Context, input CreateMemoryCandidateInput) (MemoryCandidate, error) {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return MemoryCandidate{}, err
	}
	kind, err := validateMemoryCandidateKind(input.Kind)
	if err != nil {
		return MemoryCandidate{}, err
	}
	body, err := validateMemoryCandidateBody(input.Body)
	if err != nil {
		return MemoryCandidate{}, err
	}
	title, err := validateMemoryCandidateTitle(input.Title)
	if err != nil {
		return MemoryCandidate{}, err
	}
	refs, refsJSON, err := validateMemoryEvidenceRefs(input.EvidenceRefs)
	if err != nil {
		return MemoryCandidate{}, err
	}

	noteID, err := memoryfiles.NewNoteID()
	if err != nil {
		return MemoryCandidate{}, err
	}
	inboxPath := memoryfiles.InboxNoteRelPath(noteID, title)

	artifact, err := r.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{
		SessionID: input.SessionID,
		VaultPath: inboxPath,
	})
	if err != nil {
		return MemoryCandidate{}, err
	}

	note, err := vault.CreateInboxNote(ctx, memoryfiles.CreateInboxNoteRequest{
		NoteID:       noteID,
		Kind:         memoryfiles.NoteKind(kind),
		Title:        title,
		Body:         body,
		EvidenceRefs: refs,
	})
	if err != nil {
		// The reservation stays exactly as it is. It is the only durable record
		// that this session may have left bytes in the vault, and a cleaner
		// that finds no file simply has nothing to do.
		return MemoryCandidate{}, err
	}
	if note.RelPath != artifact.VaultPath {
		return MemoryCandidate{}, ErrMemoryVaultPathMismatch
	}

	createdAt := now()
	candidate := MemoryCandidate{
		CandidateID:     ids.New("memcand"),
		SourceSessionID: input.SessionID,
		Kind:            kind,
		InboxPath:       note.RelPath,
		ContentHash:     note.ContentHash,
		Body:            body,
		EvidenceRefs:    refs,
		State:           MemoryCandidateStatePending,
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MemoryCandidate{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memory_candidates (
			id, source_session_id, kind, inbox_path, content_hash, body,
			evidence_refs_json, state, promoted_note_id, decided_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)
	`,
		candidate.CandidateID,
		candidate.SourceSessionID,
		candidate.Kind,
		candidate.InboxPath,
		candidate.ContentHash,
		candidate.Body,
		refsJSON,
		candidate.State,
		candidate.CreatedAt,
		candidate.UpdatedAt,
	); err != nil {
		return MemoryCandidate{}, err
	}
	if err := finalizeVaultArtifactTx(ctx, tx, artifact.ArtifactID, artifact.SessionID); err != nil {
		return MemoryCandidate{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemoryCandidate{}, err
	}
	return candidate, nil
}

// TransitionMemoryCandidate moves a candidate that keeps its row — today only
// a withdrawal. Promotion, profile application and rejection consume the row
// instead, because a decided candidate's file has left the inbox and a row
// describing an inbox entry that is gone is a lie the cleaner would trust.
func (r *Repository) TransitionMemoryCandidate(ctx context.Context, candidateID string, toState string) (MemoryCandidate, error) {
	if toState != MemoryCandidateStateWithdrawn {
		return MemoryCandidate{}, fmt.Errorf("%w: %q is not a state a candidate row may be moved to", ErrMemoryCandidateInvalidTransition, toState)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MemoryCandidate{}, err
	}
	defer func() { _ = tx.Rollback() }()

	candidate, err := memoryCandidateByIDTx(ctx, tx, candidateID)
	if err != nil {
		return MemoryCandidate{}, err
	}
	if err := requireMemoryCandidateTransition(candidate.State, toState); err != nil {
		return MemoryCandidate{}, err
	}
	decidedAt := now()
	if _, err := tx.ExecContext(ctx, `
		UPDATE memory_candidates
		SET state = ?, decided_at = ?, updated_at = ?
		WHERE id = ? AND state = ?
	`, toState, decidedAt, decidedAt, candidateID, MemoryCandidateStatePending); err != nil {
		return MemoryCandidate{}, err
	}
	if err := recordMemoryCandidateDecisionTx(ctx, tx, candidate, toState); err != nil {
		return MemoryCandidate{}, err
	}
	candidate.State = toState
	candidate.DecidedAt = decidedAt
	candidate.UpdatedAt = decidedAt
	if err := tx.Commit(); err != nil {
		return MemoryCandidate{}, err
	}
	return candidate, nil
}

// MemoryCandidateByID reads one candidate.
func (r *Repository) MemoryCandidateByID(ctx context.Context, candidateID string) (MemoryCandidate, error) {
	return memoryCandidateByIDTx(ctx, r.db, candidateID)
}

// ListMemoryCandidates lists candidates, optionally scoped to one session or
// one state. Both filters and the limit are validated: a listing is a read of
// unreviewed claims about the user, and it does not get to be unbounded.
func (r *Repository) ListMemoryCandidates(ctx context.Context, query MemoryCandidateQuery) ([]MemoryCandidate, error) {
	if query.Limit <= 0 || query.Limit > maxMemoryCandidateListLimit {
		return nil, fmt.Errorf("%w: limit is outside 1..%d", ErrMemoryCandidateQuery, maxMemoryCandidateListLimit)
	}
	statement := `
		SELECT id, source_session_id, kind, inbox_path, content_hash, body,
			evidence_refs_json, state, COALESCE(promoted_note_id, ''),
			COALESCE(decided_at, ''), created_at, updated_at
		FROM memory_candidates
		WHERE 1 = 1`
	args := make([]any, 0, 3)
	if query.SessionID != "" {
		statement += ` AND source_session_id = ?`
		args = append(args, query.SessionID)
	}
	if query.State != "" {
		if !validMemoryCandidateState(query.State) {
			return nil, fmt.Errorf("%w: state filter is not a candidate state", ErrMemoryCandidateQuery)
		}
		statement += ` AND state = ?`
		args = append(args, query.State)
	}
	statement += ` ORDER BY created_at, id LIMIT ?`
	args = append(args, query.Limit)

	rows, err := r.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var candidates []MemoryCandidate
	for rows.Next() {
		candidate, err := scanMemoryCandidate(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

// requireMemoryCandidateTransition is the whole state machine. Only a pending
// candidate may be decided, and a decision is final: reopening one would let a
// claim the user already refused come back without them asking for it.
func requireMemoryCandidateTransition(from string, to string) error {
	allowed := from == MemoryCandidateStatePending &&
		(to == MemoryCandidateStatePromoted ||
			to == MemoryCandidateStateRejected ||
			to == MemoryCandidateStateWithdrawn)
	if !allowed {
		return fmt.Errorf("%w: %q cannot become %q", ErrMemoryCandidateInvalidTransition, from, to)
	}
	return nil
}

func validMemoryCandidateState(state string) bool {
	switch state {
	case MemoryCandidateStatePending, MemoryCandidateStatePromoted,
		MemoryCandidateStateRejected, MemoryCandidateStateWithdrawn:
		return true
	default:
		return false
	}
}

func validateMemoryCandidateKind(kind string) (string, error) {
	switch kind {
	case MemoryCandidateKindBelief, MemoryCandidateKindProfileEdit:
		return kind, nil
	default:
		return "", fmt.Errorf("%w", ErrMemoryCandidateKind)
	}
}

func validateMemoryCandidateBody(body string) (string, error) {
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("%w: a candidate must state a claim", ErrMemoryCandidateBody)
	}
	// Both bounds are refusals, and both are stated here rather than left to
	// the layer that would hit them: the vault bounds the file in bytes, the
	// row bounds the stored claim in characters, and a caller deserves the
	// same legible refusal either way.
	if len(body) > memoryfiles.MaxCandidateBodyBytes {
		return "", fmt.Errorf("%w: body exceeds %d bytes", ErrMemoryCandidateBody, memoryfiles.MaxCandidateBodyBytes)
	}
	if utf8.RuneCountInString(body) > maxMemoryCandidateBodyRunes {
		return "", fmt.Errorf("%w: body exceeds %d characters", ErrMemoryCandidateBody, maxMemoryCandidateBodyRunes)
	}
	return body, nil
}

func validateMemoryCandidateTitle(title string) (string, error) {
	if utf8.RuneCountInString(title) > maxMemoryCandidateTitleRunes {
		return "", fmt.Errorf("%w: title exceeds %d characters", ErrMemoryCandidateBody, maxMemoryCandidateTitleRunes)
	}
	return title, nil
}

// validateMemoryEvidenceRefs checks the provenance list and renders it as the
// JSON array the schema stores. Refs name sessions and end up inside YAML
// frontmatter, so anything with a control character, a line break or no
// content at all is refused rather than escaped and hoped for.
func validateMemoryEvidenceRefs(refs []string) ([]string, string, error) {
	if len(refs) > maxMemoryEvidenceRefs {
		return nil, "", fmt.Errorf("%w: more than %d refs", ErrMemoryCandidateEvidence, maxMemoryEvidenceRefs)
	}
	checked := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if err := validateMemoryEvidenceRef(ref); err != nil {
			return nil, "", err
		}
		if _, duplicate := seen[ref]; duplicate {
			continue
		}
		seen[ref] = struct{}{}
		checked = append(checked, ref)
	}
	encoded, err := json.Marshal(checked)
	if err != nil {
		return nil, "", err
	}
	return checked, string(encoded), nil
}

func validateMemoryEvidenceRef(ref string) error {
	if ref == "" || len(ref) > maxMemoryEvidenceRefBytes {
		return fmt.Errorf("%w: a ref must be a bounded identifier", ErrMemoryCandidateEvidence)
	}
	if !utf8.ValidString(ref) {
		return fmt.Errorf("%w: a ref must be valid UTF-8", ErrMemoryCandidateEvidence)
	}
	for _, symbol := range ref {
		if symbol < 0x20 || symbol == 0x7f {
			return fmt.Errorf("%w: a ref may not contain control characters", ErrMemoryCandidateEvidence)
		}
	}
	return nil
}

func decodeMemoryEvidenceRefs(encoded string) ([]string, error) {
	var refs []string
	if err := json.Unmarshal([]byte(encoded), &refs); err != nil {
		return nil, fmt.Errorf("%w: stored refs are not a JSON array", ErrMemoryCandidateEvidence)
	}
	// Stored data is not trusted on the way back in either: a row written by an
	// older build, or edited by hand, does not get to reach the file layer
	// unchecked.
	checked := make([]string, 0, len(refs))
	for _, ref := range refs {
		if err := validateMemoryEvidenceRef(ref); err != nil {
			continue
		}
		checked = append(checked, ref)
	}
	return checked, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMemoryCandidate(scanner rowScanner) (MemoryCandidate, error) {
	var candidate MemoryCandidate
	var refsJSON string
	if err := scanner.Scan(
		&candidate.CandidateID,
		&candidate.SourceSessionID,
		&candidate.Kind,
		&candidate.InboxPath,
		&candidate.ContentHash,
		&candidate.Body,
		&refsJSON,
		&candidate.State,
		&candidate.PromotedNoteID,
		&candidate.DecidedAt,
		&candidate.CreatedAt,
		&candidate.UpdatedAt,
	); err != nil {
		return MemoryCandidate{}, err
	}
	refs, err := decodeMemoryEvidenceRefs(refsJSON)
	if err != nil {
		return MemoryCandidate{}, err
	}
	candidate.EvidenceRefs = refs
	return candidate, nil
}

func memoryCandidateByIDTx(ctx context.Context, q rowQuerier, candidateID string) (MemoryCandidate, error) {
	candidate, err := scanMemoryCandidate(q.QueryRowContext(ctx, `
		SELECT id, source_session_id, kind, inbox_path, content_hash, body,
			evidence_refs_json, state, COALESCE(promoted_note_id, ''),
			COALESCE(decided_at, ''), created_at, updated_at
		FROM memory_candidates
		WHERE id = ?
	`, candidateID))
	if errors.Is(err, sql.ErrNoRows) {
		return MemoryCandidate{}, ErrMemoryCandidateNotFound
	}
	return candidate, err
}

// finalizeVaultArtifactTx closes a reservation inside a caller's transaction,
// so the manifest row and the candidate row that describes the same file land
// together or not at all.
func finalizeVaultArtifactTx(ctx context.Context, tx *sql.Tx, artifactID string, sessionID string) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE vault_artifacts
		SET state = ?, finalized_at = ?
		WHERE id = ? AND session_id = ? AND state = ?
	`, VaultArtifactStateReady, now(), artifactID, sessionID, VaultArtifactStateWriting)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrVaultArtifactNotFound
	}
	return nil
}
