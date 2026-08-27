package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
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
// candidate is never reopened. 'profile_applying' is the fifth and is neither:
// it is the claim an apply takes before it writes profile.md, so the row can
// say "the user's document may already carry this" on both sides of a crash.
const (
	MemoryCandidateStatePending = "pending"
	// MemoryCandidateStateProfileApplying is claimed under the per-candidate
	// lock before profile.md is touched and released only once the write and
	// its bookkeeping are both done. No user decision may be taken from it: a
	// rejection cannot win over a document that may already say these words.
	MemoryCandidateStateProfileApplying = "profile_applying"
	MemoryCandidateStatePromoted        = "promoted"
	MemoryCandidateStateRejected        = "rejected"
	MemoryCandidateStateWithdrawn       = "withdrawn"
)

const (
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
	// abandonedCandidateRemovalTimeout bounds the removal of bytes that reached
	// the vault but that no row will describe. It runs outside the caller's
	// cancellation, so it needs a deadline of its own.
	abandonedCandidateRemovalTimeout = 5 * time.Second
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
	// ErrMemoryCandidateChanged refuses a decision composed against text the
	// candidate file no longer holds.
	//
	// It is checked against the file's own bytes, read again at decision time
	// and inside the same serialisation as the mutation, because the row
	// records what Turing wrote and the user may have rewritten the proposal in
	// their editor since. Accepting the decision anyway would apply their
	// "yes" to a claim they never read.
	ErrMemoryCandidateChanged = errors.New("the memory candidate changed since it was read")
)

// MemoryCandidate is one proposal waiting in the vault inbox. Everything that
// identifies it — its id, its path, the session that produced it — is derived
// by the server; the model supplies only the claim and its title.
//
// Kind, ContentHash and Body are a record of what Turing wrote, not a
// description of what the file says now: the vault is a vault so the user can
// open a proposal and rewrite it, and the moment they do, these three are
// history. Nothing decides anything on them. Every listing overlays them from
// the file, and every decision re-reads the file under its own lock. What the
// row keeps owning is what only it can know — identity, source session,
// provenance and lifecycle.
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
// deliberately no candidate id, no path and no evidence: provenance is what
// makes a candidate withdrawable when a conversation is deleted, so it is
// derived from the session the run belongs to rather than accepted from
// whoever is asking. A model that could name the conversations its claim rests
// on could name someone else's, and the belief that came out of it would then
// survive — or be withdrawn by — a conversation it has nothing to do with.
type CreateMemoryCandidateInput struct {
	SessionID string
	Kind      string
	Title     string
	Body      string
}

// MemoryCandidateQuery bounds one candidate listing.
type MemoryCandidateQuery struct {
	SessionID string
	State     string
	Limit     int
}

// SetMemoryVault attaches the vault this repository reads and writes notes
// through. It is set once at startup, like the skill store beside it.
//
// The scan cache is replaced rather than kept, because it is a cache of one
// vault's files: the same relative path under a different root is a different
// note, and a warm entry carried across would let a new vault be served with an
// old one's words. It is taken under the vault-wide pass lock so a pass already
// inside the vault finishes against the cache it started with.
func (r *Repository) SetMemoryVault(vault *memoryfiles.Vault) {
	r.memoryVaultMutex.Lock()
	defer r.memoryVaultMutex.Unlock()
	r.memoryVault = vault
	r.memoryScanCache = nil
	if vault != nil {
		r.memoryScanCache = memoryfiles.NewMetadataCache()
	}
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
	refs, refsJSON, err := validateMemoryEvidenceRefs(serverDerivedEvidenceRefs(input.SessionID))
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
	if r.memoryCandidateWriteBarrier != nil {
		if err := r.memoryCandidateWriteBarrier(); err != nil {
			return MemoryCandidate{}, err
		}
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
		//
		// Unless the write says it left a copy under a name only the vault can
		// spell. Then there is something to do, and a reservation that names no
		// bytes cannot ask for it — so it is bound to what was written and
		// marked, which is what puts those bytes in the cleaner's reach.
		r.bindAbandonedVaultWrite(ctx, artifact, memoryfiles.ResidueContentHash(err))
		return MemoryCandidate{}, err
	}
	if note.RelPath != artifact.VaultPath {
		return MemoryCandidate{}, r.abandonWrittenCandidate(ctx, vault, artifact, note.RelPath, note.ContentHash, ErrMemoryVaultPathMismatch)
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
	if err := r.recordMemoryCandidate(ctx, candidate, refsJSON, artifact); err != nil {
		return MemoryCandidate{}, r.abandonWrittenCandidate(ctx, vault, artifact, note.RelPath, note.ContentHash, err)
	}
	return candidate, nil
}

// recordMemoryCandidate is the database half of a creation: the candidate row
// and the closing of the reservation that tracked its file, in one transaction
// so the manifest and the index never disagree about the same bytes.
func (r *Repository) recordMemoryCandidate(ctx context.Context, candidate MemoryCandidate, refsJSON string, artifact VaultArtifact) error {
	if r.memoryCandidateRecordBarrier != nil {
		if err := r.memoryCandidateRecordBarrier(); err != nil {
			return err
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
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
		return err
	}
	if err := finalizeVaultArtifactTx(ctx, tx, artifact.ArtifactID, artifact.SessionID, candidate.ContentHash); err != nil {
		return err
	}
	return tx.Commit()
}

// abandonWrittenCandidate removes bytes that reached the vault but that no row
// will ever describe.
//
// A file nothing has a record of is worse than either half alone: the manifest
// cannot name it, so no cleaner can find it, and the user is left with a claim
// about themselves that Turing has no way to withdraw. The removal goes through
// RemoveInboxNote and nothing else, so this path can never be pointed at a
// belief or at a pinned document.
//
// The reservation is deliberately left alone. If it is still there it is the
// record that this path may hold a file, and a cleaner that finds none has
// nothing to do; if the session was deleted underneath this write, the cascade
// already took it and there is nothing left to leave behind.
func (r *Repository) abandonWrittenCandidate(
	ctx context.Context,
	vault *memoryfiles.Vault,
	artifact VaultArtifact,
	relPath string,
	contentHash string,
	cause error,
) error {
	// The removal runs even when the caller's context is already done: the
	// bytes are in the user's vault either way, and a cancelled context is not
	// a reason to leave them there. It is bounded so a hung filesystem cannot
	// hold the caller open indefinitely.
	removal, cancel := context.WithTimeout(context.WithoutCancel(ctx), abandonedCandidateRemovalTimeout)
	defer cancel()
	removeErr := vault.RemoveInboxNote(removal, retiredCandidateRemoval(relPath, contentHash))
	if errors.Is(removeErr, memoryfiles.ErrVaultResidue) {
		// The bytes are under a name only the vault can spell, and the
		// reservation that tracks this path names no bytes — it was taken
		// before the write, so it never could. Left like that the reservation
		// drains on the empty path and the copy is stranded. Binding it to what
		// was written, and marking it, is what lets the cleaner find those
		// bytes and take them.
		r.bindAbandonedVaultWrite(removal, artifact, contentHash)
	}
	return errors.Join(cause, removeErr)
}

// bindAbandonedVaultWrite records the bytes an abandoned creation left behind
// on the reservation that already names their path.
//
// It is best-effort by construction: the caller is already returning a failure,
// the session may be cascading away underneath it, and there is nothing useful
// to say to a user about bookkeeping for a write that was abandoned. What it
// buys is a row the withdrawal can act on instead of one that drains over a
// copy nobody can name.
func (r *Repository) bindAbandonedVaultWrite(ctx context.Context, artifact VaultArtifact, contentHash string) {
	if artifact.ArtifactID == "" || contentHash == "" {
		return
	}
	// Its own deadline, because the caller's may be the one that just expired
	// on the removal above — and this is the step that decides whether anything
	// can ever find those bytes again.
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), abandonedCandidateRemovalTimeout)
	defer cancel()
	ctx = record
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("record the vault copy an abandoned write left: %v", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	if err := markUnremovedVaultArtifactTx(
		ctx, tx, artifact.SessionID, artifact.VaultPath, contentHash, vaultArtifactRemoveFailedCode,
	); err != nil {
		log.Printf("record the vault copy an abandoned write left: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("record the vault copy an abandoned write left: %v", err)
	}
}

// WithdrawMemoryCandidate retires a proposal without deciding it.
//
// It is the only lifecycle move that keeps the row, and it is a method rather
// than an argument to a generic transition on purpose. A caller that could name
// the state a candidate moves to could mark one promoted or rejected while its
// file is still sitting in the inbox and its row still claims to be a live
// proposal — a decision the user never sees, on a claim they never reviewed.
// Promotion, profile application and rejection consume the row instead, because
// a decided candidate's file has left the inbox and a row describing an inbox
// entry that is gone is a lie the cleaner would trust.
//
// The UPDATE is the state machine, not a step after it: it moves only a row
// that is still pending, and the audit row is written only once that statement
// reports it changed exactly one row. Auditing on the strength of the read
// instead would record a decision that never happened — the loudest possible
// version of losing a candidate silently.
func (r *Repository) WithdrawMemoryCandidate(ctx context.Context, candidateID string) (MemoryCandidate, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MemoryCandidate{}, err
	}
	defer func() { _ = tx.Rollback() }()

	candidate, err := memoryCandidateByIDTx(ctx, tx, candidateID)
	if err != nil {
		return MemoryCandidate{}, err
	}
	decidedAt := now()
	result, err := tx.ExecContext(ctx, `
		UPDATE memory_candidates
		SET state = ?, decided_at = ?, updated_at = ?
		WHERE id = ? AND state = ?
	`, MemoryCandidateStateWithdrawn, decidedAt, decidedAt, candidateID, MemoryCandidateStatePending)
	if err != nil {
		return MemoryCandidate{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return MemoryCandidate{}, err
	}
	if changed != 1 {
		return MemoryCandidate{}, fmt.Errorf("%w: %q cannot become %q",
			ErrMemoryCandidateInvalidTransition, candidate.State, MemoryCandidateStateWithdrawn)
	}
	if err := recordMemoryCandidateDecisionTx(ctx, tx, candidate, MemoryCandidateStateWithdrawn); err != nil {
		return MemoryCandidate{}, err
	}
	candidate.State = MemoryCandidateStateWithdrawn
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

// requireMemoryCandidateTransition is the whole state machine for a *user
// decision*. Only a pending candidate may be decided, and a decision is final:
// reopening one would let a claim the user already refused come back without
// them asking for it.
//
// An apply takes 'profile_applying' from pending like any other decision,
// because that is what it is — the moment the user accepted it. What no
// decision may do is come *out* of that state: the recovery pass owns the exit,
// and it takes it against the profile document rather than against a caller's
// word. That is why a rejection over a claimed apply lands here and is refused.
func requireMemoryCandidateTransition(from string, to string) error {
	allowed := from == MemoryCandidateStatePending &&
		(to == MemoryCandidateStatePromoted ||
			to == MemoryCandidateStateRejected ||
			to == MemoryCandidateStateWithdrawn ||
			to == MemoryCandidateStateProfileApplying)
	if !allowed {
		return fmt.Errorf("%w: %q cannot become %q", ErrMemoryCandidateInvalidTransition, from, to)
	}
	return nil
}

func validMemoryCandidateState(state string) bool {
	switch state {
	case MemoryCandidateStatePending, MemoryCandidateStateProfileApplying,
		MemoryCandidateStatePromoted, MemoryCandidateStateRejected,
		MemoryCandidateStateWithdrawn:
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
	// One bound, in bytes, stated here rather than left to the layer that would
	// hit it: the vault refuses the same number of bytes and the row's CHECK
	// counts the same bytes, so a caller gets the same legible refusal wherever
	// it would have landed. It is a refusal and never a truncation — a claim
	// about the user that was silently cut in half is a different claim.
	if len([]byte(body)) > memoryfiles.MaxCandidateBodyBytes {
		return "", fmt.Errorf("%w: body exceeds %d bytes", ErrMemoryCandidateBody, memoryfiles.MaxCandidateBodyBytes)
	}
	return body, nil
}

func validateMemoryCandidateTitle(title string) (string, error) {
	if utf8.RuneCountInString(title) > maxMemoryCandidateTitleRunes {
		return "", fmt.Errorf("%w: title exceeds %d characters", ErrMemoryCandidateBody, maxMemoryCandidateTitleRunes)
	}
	return title, nil
}

// serverDerivedEvidenceRefs is the whole of Phase 1's provenance rule: a
// candidate is grounded in the conversation that produced it and in nothing
// else. It is a function rather than an inline literal because
// requireServerDerivedEvidence checks decisions against exactly this rule, and
// the two must never be able to disagree about what the server derives.
func serverDerivedEvidenceRefs(sessionID string) []string {
	return []string{sessionID}
}

// requireServerDerivedEvidence refuses a stored provenance that is not the one
// the server derived.
//
// Creation only ever writes serverDerivedEvidenceRefs, so anything else has
// been edited underneath the orchestrator, and both directions matter. A row
// citing another conversation would ground a belief in one that never produced
// it — and deleting that conversation would then withdraw a claim it had
// nothing to do with. A row citing nothing at all is the quieter forgery: it
// promotes as grounded memory that no deletion can ever reach, because nothing
// links it to a conversation. Equality against the derived list refuses both.
func requireServerDerivedEvidence(candidate MemoryCandidate) error {
	derived := serverDerivedEvidenceRefs(candidate.SourceSessionID)
	if len(candidate.EvidenceRefs) != len(derived) {
		return fmt.Errorf("%w: a candidate must cite the conversation that produced it, and only that one", ErrMemoryCandidateEvidence)
	}
	for index, ref := range candidate.EvidenceRefs {
		if ref != derived[index] {
			return fmt.Errorf("%w: a candidate may only cite the conversation that produced it", ErrMemoryCandidateEvidence)
		}
	}
	return nil
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

// decodeMemoryEvidenceRefs reads the stored provenance strictly.
//
// Stored data is not trusted on the way back in — a row written by an older
// build, or edited by hand, does not get to reach the file layer unchecked —
// but "not trusted" means refused, never quietly repaired. Dropping the
// entries that fail to parse would let a poisoned row promote with less
// provenance than it claims: a belief the user accepts as grounded in three
// conversations, silently grounded in one, and surviving the deletion of the
// other two.
func decodeMemoryEvidenceRefs(encoded string) ([]string, error) {
	var refs []string
	if err := json.Unmarshal([]byte(encoded), &refs); err != nil {
		return nil, fmt.Errorf("%w: stored refs are not a JSON array of identifiers", ErrMemoryCandidateEvidence)
	}
	for _, ref := range refs {
		if err := validateMemoryEvidenceRef(ref); err != nil {
			return nil, err
		}
	}
	return refs, nil
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
//
// It binds the row to the bytes that landed in the same statement. The two facts
// are inseparable: "the file is on disk" and "this is which file" are what a
// later cleanup needs together, and a row that reached 'ready' with only the
// first would be a licence to delete whatever is under a path.
func finalizeVaultArtifactTx(
	ctx context.Context,
	tx *sql.Tx,
	artifactID string,
	sessionID string,
	contentHash string,
) error {
	if strings.TrimSpace(contentHash) == "" {
		return ErrVaultArtifactOwnershipUnproven
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE vault_artifacts
		SET state = ?, finalized_at = ?, expected_content_hash = ?
		WHERE id = ? AND session_id = ? AND state = ?
	`, VaultArtifactStateReady, now(), contentHash, artifactID, sessionID, VaultArtifactStateWriting)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		return nil
	}
	// Nothing moved, which has two very different causes. The row may be gone,
	// and that is the failure this reports. Or somebody else already bound it
	// to exactly these bytes — which is what reconcile's crash-heal does when
	// its pass falls inside the window between this creation's write and this
	// statement, since the reservation is older than the pass's anchor and no
	// candidate row exists yet to coordinate against.
	//
	// The second is agreement, not conflict: two readers of the same file
	// naming the same hash. Failing it would have this creation delete the note
	// it just wrote and report an error over work that actually happened, so
	// whoever arrives second accepts the answer it was going to give.
	bound, err := vaultArtifactByID(ctx, tx, artifactID, sessionID)
	if err != nil {
		return err
	}
	if bound.State != VaultArtifactStateReady || bound.ExpectedContentHash != contentHash {
		return ErrVaultArtifactNotFound
	}
	return nil
}
