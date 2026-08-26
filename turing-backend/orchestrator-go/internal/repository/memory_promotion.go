package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// ApplyMemoryProfileInput carries a reviewed profile edit. Content is the whole
// resulting document rather than a patch, because the user may have edited the
// proposal before accepting it, and ExpectedContentHash is the compare-and-set
// token that keeps two accepted edits from silently overwriting each other.
type ApplyMemoryProfileInput struct {
	CandidateID         string
	ExpectedContentHash string
	Content             string
	// ExpectedCandidateHash binds the resulting document to the exact proposal
	// it was composed from. See MemoryCandidateDecision.
	ExpectedCandidateHash string
}

// MemoryCandidateDecision is one user decision about one proposal.
//
// ExpectedCandidateHash is the compare-and-set token over the candidate file's
// own bytes, which is a different question from the profile's compare-and-set:
// this one asks "is the claim still the one I read?", and it is answered
// against the file rather than the row because the file is what the user was
// shown and what they may have edited. Empty means the caller is making no
// claim about the file, which is how a decision taken before the listing
// carried a hash still works.
type MemoryCandidateDecision struct {
	CandidateID           string
	ExpectedCandidateHash string
}

// memoryCandidateLocks serialises every decision about one candidate.
//
// Process-wide on purpose, for the same reason the vault's path locks are: two
// Repository values over one database must contend on the same key, or the
// serialisation is only as good as the number of callers that happen to share
// an instance. A decision holds it across the whole of read, check, file
// mutation and transaction, so a promotion and a rejection racing over one
// proposal cannot both believe they were the one that decided — and the loser
// finds the row already gone rather than deleting a file the winner just moved.
var memoryCandidateLocks = newMemoryCandidateLockTable()

type memoryCandidateLockEntry struct {
	token chan struct{}
	refs  int
}

type memoryCandidateLockTable struct {
	mutex sync.Mutex
	locks map[string]*memoryCandidateLockEntry
}

func newMemoryCandidateLockTable() *memoryCandidateLockTable {
	return &memoryCandidateLockTable{locks: make(map[string]*memoryCandidateLockEntry)}
}

func (t *memoryCandidateLockTable) lockContext(ctx context.Context, key string) (func(), error) {
	t.mutex.Lock()
	entry := t.locks[key]
	if entry == nil {
		entry = &memoryCandidateLockEntry{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		t.locks[key] = entry
	}
	entry.refs++
	t.mutex.Unlock()

	releaseReference := func() {
		t.mutex.Lock()
		entry.refs--
		if entry.refs == 0 && t.locks[key] == entry {
			delete(t.locks, key)
		}
		t.mutex.Unlock()
	}
	if err := ctx.Err(); err != nil {
		releaseReference()
		return nil, err
	}
	select {
	case <-entry.token:
		if err := ctx.Err(); err != nil {
			entry.token <- struct{}{}
			releaseReference()
			return nil, err
		}
		var once sync.Once
		return func() {
			once.Do(func() {
				entry.token <- struct{}{}
				releaseReference()
			})
		}, nil
	case <-ctx.Done():
		releaseReference()
		return nil, ctx.Err()
	}
}

// lockMemoryCandidateDecision takes the per-candidate lock. The key is the
// candidate id alone, which is a globally unique server-minted identifier — so
// two Repository values in one process contend exactly when they are deciding
// about the same proposal, and never otherwise.
func (r *Repository) lockMemoryCandidateDecision(ctx context.Context, candidateID string) (func(), error) {
	if candidateID == "" {
		return nil, ErrMemoryCandidateNotFound
	}
	return memoryCandidateLocks.lockContext(ctx, candidateID)
}

// requireUnchangedCandidateFile is the compare-and-set the user's decision
// rests on, taken against the file rather than the row.
//
// An empty expectation is accepted as "I am making no claim about the file",
// which keeps a caller that never read one working. A file that cannot be read
// at all is refused rather than treated as unchanged: a decision cannot be
// verified against bytes nobody has.
func requireUnchangedCandidateFile(
	ctx context.Context,
	vault *memoryfiles.Vault,
	candidate MemoryCandidate,
	expectedHash string,
) error {
	if expectedHash == "" {
		return nil
	}
	current, err := vault.ReadInboxNote(ctx, candidate.InboxPath)
	if err != nil {
		return err
	}
	if current.ContentHash != expectedHash {
		return fmt.Errorf("%w: %s", ErrMemoryCandidateChanged, candidate.InboxPath)
	}
	return nil
}

// PromoteMemoryCandidate accepts a belief into memory.
//
// The file moves first, then one transaction records it: the note projection,
// the citations that are still live, the removal of the candidate row and the
// release of the reservation that tracked its inbox entry all land together or
// not at all.
//
// If that transaction fails after the file has moved, what is left behind is a
// belief in the vault, a candidate row whose inbox entry is gone, and a
// reservation for a path the inbox no longer holds — which is exactly the
// state ReconcileMemoryVault knows how to finish. It is deliberately not
// rolled back by moving the file back: a rollback that fails halfway is how a
// note the user accepted disappears.
//
// The whole of it runs under the per-candidate lock, so the read, the check,
// the move and the transaction are one decision rather than four steps another
// decision can step between.
func (r *Repository) PromoteMemoryCandidate(ctx context.Context, decision MemoryCandidateDecision) (MemoryNote, error) {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return MemoryNote{}, err
	}
	unlock, err := r.lockMemoryCandidateDecision(ctx, decision.CandidateID)
	if err != nil {
		return MemoryNote{}, err
	}
	defer unlock()

	candidate, err := r.pendingCandidateForDecision(ctx, decision.CandidateID, MemoryCandidateKindBelief, MemoryCandidateStatePromoted)
	if err != nil {
		return MemoryNote{}, err
	}
	if err := requireUnchangedCandidateFile(ctx, vault, candidate, decision.ExpectedCandidateHash); err != nil {
		return MemoryNote{}, err
	}

	belief, err := vault.PromoteToBeliefs(ctx, memoryfiles.PromoteToBeliefsRequest{
		SourceRelPath: candidate.InboxPath,
		Mode:          memoryfiles.PromoteManagedCandidate,
		Kind:          memoryfiles.KindBelief,
	})
	if err != nil {
		return MemoryNote{}, err
	}
	if r.memoryPromotionBarrier != nil {
		if err := r.memoryPromotionBarrier(); err != nil {
			return MemoryNote{}, err
		}
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MemoryNote{}, err
	}
	defer func() { _ = tx.Rollback() }()

	live, err := liveSessionRefsTx(ctx, tx, candidate.EvidenceRefs)
	if err != nil {
		return MemoryNote{}, err
	}
	status := promotedNoteStatus(candidate.EvidenceRefs, live)
	note := MemoryNote{
		NoteID:      belief.NoteID,
		Path:        belief.RelPath,
		Content:     belief.Content,
		ContentHash: belief.ContentHash,
		Status:      status,
	}
	if err := upsertMemoryNoteTx(ctx, tx, note); err != nil {
		return MemoryNote{}, err
	}
	if err := linkMemoryEvidenceTx(ctx, tx, note.NoteID, note.ContentHash, live); err != nil {
		return MemoryNote{}, err
	}
	if err := consumeMemoryCandidateTx(ctx, tx, candidate, MemoryCandidateStatePromoted); err != nil {
		return MemoryNote{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemoryNote{}, err
	}
	return note, nil
}

// promotedNoteStatus decides what an accepted belief may be answered with,
// given the conversations it cites and the ones among those that still exist.
//
// Every conversation behind the claim already being gone does not undo the
// acceptance — the note is kept — but nothing may answer with it as if it were
// grounded in something the user can still look at. A note that cites nothing
// never claimed that grounding, so it keeps its standing.
func promotedNoteStatus(evidenceRefs []string, live []string) string {
	if len(evidenceRefs) > 0 && len(live) == 0 {
		return MemoryNoteStatusWithdrawn
	}
	return MemoryNoteStatusManaged
}

// ApplyMemoryProfileCandidate writes the user's profile on the authority of a
// profile_edit candidate, then consumes the candidate.
//
// The profile is a pinned document, not a note: it is never projected into the
// note index and never becomes searchable memory. The candidate file is
// removed through the inbox-only primitive, so this path cannot be pointed at
// anything else.
//
// It runs under the per-candidate lock for the same reason a promotion does,
// and the stakes are higher here: without it a rejection could retire the
// proposal while this call was between its own check and its write, and the
// user's profile would be rewritten from a claim they had just refused.
func (r *Repository) ApplyMemoryProfileCandidate(ctx context.Context, input ApplyMemoryProfileInput) (memoryfiles.ProfileDocument, error) {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return memoryfiles.ProfileDocument{}, err
	}
	unlock, err := r.lockMemoryCandidateDecision(ctx, input.CandidateID)
	if err != nil {
		return memoryfiles.ProfileDocument{}, err
	}
	defer unlock()

	candidate, err := r.pendingCandidateForDecision(ctx, input.CandidateID, MemoryCandidateKindProfileEdit, MemoryCandidateStatePromoted)
	if err != nil {
		return memoryfiles.ProfileDocument{}, err
	}
	if err := requireUnchangedCandidateFile(ctx, vault, candidate, input.ExpectedCandidateHash); err != nil {
		return memoryfiles.ProfileDocument{}, err
	}

	document, err := vault.ApplyProfileEdit(ctx, memoryfiles.ApplyProfileEditRequest{
		CandidateRelPath:    candidate.InboxPath,
		TargetRelPath:       memoryfiles.ProfileFileName,
		ExpectedContentHash: input.ExpectedContentHash,
		Content:             input.Content,
	})
	if err != nil {
		return memoryfiles.ProfileDocument{}, err
	}
	if err := vault.RemoveInboxNote(ctx, candidate.InboxPath); err != nil {
		return memoryfiles.ProfileDocument{}, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return memoryfiles.ProfileDocument{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := consumeMemoryCandidateTx(ctx, tx, candidate, MemoryCandidateStatePromoted); err != nil {
		return memoryfiles.ProfileDocument{}, err
	}
	if err := tx.Commit(); err != nil {
		return memoryfiles.ProfileDocument{}, err
	}
	return document, nil
}

// RejectMemoryCandidate refuses a proposal: the file leaves the inbox and the
// row leaves the database, so neither the user nor a later pass is left with
// half of a claim they already said no to.
//
// The file is removed through RemoveInboxNote and nothing else. That primitive
// refuses every path outside inbox/, which is what keeps a rejection — or a
// tampered candidate row — from being turned into a way to delete a belief.
//
// Like the two acceptances, it holds the per-candidate lock across the whole
// decision, so a rejection racing an acceptance is resolved by one of them
// finding the row already gone rather than by both acting.
func (r *Repository) RejectMemoryCandidate(ctx context.Context, decision MemoryCandidateDecision) error {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return err
	}
	unlock, err := r.lockMemoryCandidateDecision(ctx, decision.CandidateID)
	if err != nil {
		return err
	}
	defer unlock()

	candidate, err := r.pendingCandidateForDecision(ctx, decision.CandidateID, "", MemoryCandidateStateRejected)
	if err != nil {
		return err
	}
	if err := requireUnchangedCandidateFile(ctx, vault, candidate, decision.ExpectedCandidateHash); err != nil {
		return err
	}
	if err := vault.RemoveInboxNote(ctx, candidate.InboxPath); err != nil {
		return err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := consumeMemoryCandidateTx(ctx, tx, candidate, MemoryCandidateStateRejected); err != nil {
		return err
	}
	return tx.Commit()
}

// pendingCandidateForDecision loads a candidate and checks everything a
// decision depends on before any file is touched: that it exists, that it is
// the kind this decision is for, that the lifecycle allows the move, that the
// provenance stored beside it is the one the server derived, and that the path
// stored beside it is still an inbox path. The last two are not redundant with
// the layers below — they turn a tampered row into a typed refusal instead of
// a forged citation or a primitive's confinement error.
func (r *Repository) pendingCandidateForDecision(ctx context.Context, candidateID string, kind string, to string) (MemoryCandidate, error) {
	candidate, err := memoryCandidateByIDTx(ctx, r.db, candidateID)
	if err != nil {
		return MemoryCandidate{}, err
	}
	if kind != "" && candidate.Kind != kind {
		return MemoryCandidate{}, fmt.Errorf("%w: candidate is a %s", ErrMemoryCandidateKind, candidate.Kind)
	}
	if err := requireMemoryCandidateTransition(candidate.State, to); err != nil {
		return MemoryCandidate{}, err
	}
	if err := requireServerDerivedEvidence(candidate); err != nil {
		return MemoryCandidate{}, err
	}
	if _, err := validateVaultInboxPath(candidate.InboxPath); err != nil {
		return MemoryCandidate{}, err
	}
	return candidate, nil
}

// consumeMemoryCandidateTx retires a decided candidate and the reservation that
// tracked its inbox entry. The delete is guarded on the state it was read in,
// so two decisions racing cannot both believe they were the one that decided.
//
// The audit row lands in the same transaction as the decision, for the same
// reason session deletion records its own: a decision that is recorded
// separately can be lost while the change it describes survives. It carries the
// candidate's identity, its kind and the decision — never the claim it made,
// the file it lived in, or the conversation that produced it.
func consumeMemoryCandidateTx(ctx context.Context, tx *sql.Tx, candidate MemoryCandidate, decision string) error {
	result, err := tx.ExecContext(ctx, `
		DELETE FROM memory_candidates WHERE id = ? AND state = ?
	`, candidate.CandidateID, MemoryCandidateStatePending)
	if err != nil {
		return err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if deleted != 1 {
		return ErrMemoryCandidateInvalidTransition
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM vault_artifacts WHERE session_id = ? AND vault_path = ?
	`, candidate.SourceSessionID, candidate.InboxPath); err != nil {
		return err
	}
	return recordMemoryCandidateDecisionTx(ctx, tx, candidate, decision)
}

// recordMemoryCandidateDecisionTx writes the one redacted lifecycle row a
// candidate decision leaves behind.
func recordMemoryCandidateDecisionTx(ctx context.Context, tx *sql.Tx, candidate MemoryCandidate, decision string) error {
	payload, err := json.Marshal(map[string]string{
		"kind":  candidate.Kind,
		"state": decision,
	})
	if err != nil {
		return err
	}
	return recordAuditTx(ctx, tx, "", "system", "", memoryCandidateAuditAction(decision), candidate.CandidateID, string(payload))
}

// memoryCandidateAuditAction names the action for one decision. The decision is
// always one of this package's own state constants, never caller text.
func memoryCandidateAuditAction(decision string) string {
	return "memory.candidate." + decision
}
