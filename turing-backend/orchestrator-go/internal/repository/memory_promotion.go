package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// ApplyMemoryProfileInput carries a reviewed profile edit.
//
// Content is the whole resulting document rather than a patch, because the user
// may have edited the proposal before accepting it.
//
// The two hashes name two different documents and are not interchangeable.
// ExpectedContentHash is the compare-and-set over *profile.md* — "is the
// document I am replacing still the one I was shown?" — and keeps two accepted
// edits from silently overwriting each other. ExpectedCandidateHash is the
// compare-and-set over the *candidate file*, and binds the resulting document
// to the exact proposal it was composed from. See MemoryCandidateDecision.
type ApplyMemoryProfileInput struct {
	CandidateID           string
	ExpectedContentHash   string
	Content               string
	ExpectedCandidateHash string
}

// MemoryCandidateDecision is one user decision about one proposal.
//
// ExpectedCandidateHash is the compare-and-set token over the candidate file's
// own bytes as they read *now*, which is the only candidate compare-and-set
// there is. It is answered against the file rather than the row because the
// file is what the user was shown, what every listing serves, and what they may
// have rewritten in their editor since Turing proposed it. Empty means the
// caller is making no claim about the file, which is how a decision taken
// before the listing carried a hash still works.
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

// decidedCandidateFile is what a decision's pre-check comes away with, and it
// is what the primitive below it is bound to.
//
// ContentHash names the bytes that were read, and is what a rejection or a
// promotion compares against under the primitive's own lock. Unreadable is the
// other half, and only one of the two is ever set: a proposal nobody could
// parse has no bytes to name, so what stands in for them is the identity of the
// exact entry the read failed on. Neither is ever handed to a client — the hash
// a client sends is its own claim, and this is the server's.
type decidedCandidateFile struct {
	ContentHash string
	Unreadable  memoryfiles.UnreadableCandidateEntry
}

// decideAboutCandidateFile reads the proposal as it stands *now* and answers
// the two questions every decision rests on: is this still the text the user
// read, and is this still the kind of thing this decision is for.
//
// Both are answered against the file rather than the row, and for the same
// reason. The row is Turing's record of what it proposed; the file is what the
// vault holds, what every listing serves, and what the user may have rewritten
// in their editor — and a vault is a vault precisely so they can. Deciding on
// the row would refuse every proposal they edited (its hash moved) and would
// promote into beliefs/ a proposal they had rewritten into a profile edit.
//
// The read is unconditional, because the kind has to come from somewhere even
// when the caller made no claim about the bytes. A file that cannot be read or
// parsed is refused rather than treated as unchanged: neither question can be
// answered against bytes nobody has.
//
// requiredKind is empty for a rejection. A rejection is the user saying no to
// whatever is there, so it needs neither the kind nor a readable file — and
// refusing to let them throw away a proposal whose frontmatter no longer parses
// would leave them with a file they can neither accept nor be rid of. It still
// honours a compare-and-set they did supply.
//
// What it returns is a name for the bytes it actually read — not the caller's
// token, which may be empty, and not the row's, which is history. The two are
// the same value whenever the caller supplied one; what matters is that the
// primitive is given something to be bound to even when the caller gave
// nothing. A file that could not be read at all, at a door that allows it,
// comes back carrying the identity of that entry instead, which is the only
// thing such a proposal can be bound to and is why the hashless removal is no
// longer a licence to delete whatever turns up under the name.
func decideAboutCandidateFile(
	ctx context.Context,
	vault *memoryfiles.Vault,
	candidate MemoryCandidate,
	expectedHash string,
	requiredKind string,
) (decidedCandidateFile, error) {
	reading, err := vault.ReadInboxCandidate(ctx, candidate.InboxPath)
	if err != nil {
		return decidedCandidateFile{}, err
	}
	if !reading.Readable {
		if requiredKind == "" && expectedHash == "" {
			return decidedCandidateFile{Unreadable: reading.Unreadable}, nil
		}
		return decidedCandidateFile{}, reading.ReadErr
	}
	current := reading.Note
	if expectedHash != "" && current.ContentHash != expectedHash {
		return decidedCandidateFile{}, fmt.Errorf("%w: %s", ErrMemoryCandidateChanged, candidate.InboxPath)
	}
	if requiredKind != "" && string(current.Kind) != requiredKind {
		return decidedCandidateFile{}, fmt.Errorf("%w: the file declares %q", ErrMemoryCandidateKind, string(current.Kind))
	}
	return decidedCandidateFile{ContentHash: current.ContentHash}, nil
}

// decisionFileBarrier is the seam between a decision's pre-check and the
// primitive that acts on it — the window ReadInboxNote opens by giving the
// vault's path lock back before the primitive takes it again. It is nil in
// production and never does anything else.
func (r *Repository) decisionFileBarrier() {
	if r.memoryDecisionFileBarrier != nil {
		r.memoryDecisionFileBarrier()
	}
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

	candidate, err := r.pendingCandidateForDecision(ctx, decision.CandidateID, MemoryCandidateStatePromoted)
	if err != nil {
		return MemoryNote{}, err
	}
	decided, err := decideAboutCandidateFile(ctx, vault, candidate, decision.ExpectedCandidateHash, MemoryCandidateKindBelief)
	if err != nil {
		return MemoryNote{}, err
	}
	r.decisionFileBarrier()

	belief, err := vault.PromoteToBeliefs(ctx, memoryfiles.PromoteToBeliefsRequest{
		SourceRelPath:       candidate.InboxPath,
		Mode:                memoryfiles.PromoteManagedCandidate,
		Kind:                memoryfiles.KindBelief,
		ExpectedContentHash: decided.ContentHash,
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
	if err := consumeMemoryCandidateTx(ctx, tx, candidate, MemoryCandidateStatePromoted, true); err != nil {
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

// ApplyMemoryProfileResult is what an apply leaves behind: the profile as it
// now stands, and whether the proposal it came from is still sitting in the
// inbox.
//
// The two are reported separately because they fail separately and only one of
// them is the decision. Once profile.md holds the reviewed document, the user
// has been answered; a proposal file that could not be removed afterwards is
// housekeeping, and reporting it as a failed apply would tell them their edit
// did not land while their own document says otherwise.
type ApplyMemoryProfileResult struct {
	Document memoryfiles.ProfileDocument
	// CleanupPending is true when the write landed but the proposal, its row
	// or its reservation could not all be retired in the same breath. The
	// candidate is never left decidable when this is true: what is pending is
	// Turing's own tidying, not the user's decision.
	CleanupPending bool
}

// The two seams a profile apply can die at, named so a test can stage a crash
// at each. They are strings rather than an enum because the only thing that
// ever reads them is the test-only barrier.
const (
	memoryProfileApplyClaimed = "claimed"
	memoryProfileApplyWritten = "written"
)

// ApplyMemoryProfileCandidate writes the user's profile on the authority of a
// profile_edit candidate, then consumes the candidate.
//
// The order is the whole design. Everything that can refuse this apply is
// asked first — the row's lifecycle, the candidate file's compare-and-set and
// kind, and the profile's own compare-and-set — and then the candidate is
// *claimed* into 'profile_applying', carrying the hash of the document about to
// be written and the hash of the one being replaced. Only then is profile.md
// touched.
//
// That ordering is what makes a crash survivable. Before the claim, nothing has
// happened and the proposal is still the user's to decide. After it, the row
// says the user's document may already carry these words, so no rejection can
// retire the proposal as though they had refused it — and the recovery pass can
// read profile.md and tell which side of the write the process died on, because
// it knows both hashes.
//
// The profile is a pinned document, not a note: it is never projected into the
// note index and never becomes searchable memory. The candidate file is removed
// through the inbox-only primitive, so this path cannot be pointed at anything
// else.
//
// It runs under the per-candidate lock for the same reason a promotion does,
// and the stakes are higher here: without it a rejection could retire the
// proposal while this call was between its own check and its write, and the
// user's profile would be rewritten from a claim they had just refused.
func (r *Repository) ApplyMemoryProfileCandidate(ctx context.Context, input ApplyMemoryProfileInput) (ApplyMemoryProfileResult, error) {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return ApplyMemoryProfileResult{}, err
	}
	unlock, err := r.lockMemoryCandidateDecision(ctx, input.CandidateID)
	if err != nil {
		return ApplyMemoryProfileResult{}, err
	}
	defer unlock()

	candidate, err := r.pendingCandidateForDecision(ctx, input.CandidateID, MemoryCandidateStateProfileApplying)
	if err != nil {
		return ApplyMemoryProfileResult{}, err
	}
	decided, err := decideAboutCandidateFile(ctx, vault, candidate, input.ExpectedCandidateHash, MemoryCandidateKindProfileEdit)
	if err != nil {
		return ApplyMemoryProfileResult{}, err
	}
	r.decisionFileBarrier()
	// The profile's compare-and-set, asked here rather than only inside the
	// write, because the claim has to record the document it is replacing and
	// a claim over a document that has already moved is a claim about nothing.
	// The write asks again, through the descriptor it writes; this is not a
	// substitute for that check, it is what the claim is made of.
	base, err := currentProfileHash(ctx, vault, input.ExpectedContentHash)
	if err != nil {
		return ApplyMemoryProfileResult{}, err
	}
	if err := r.claimProfileApply(ctx, candidate, base, memoryfiles.ContentHash(input.Content)); err != nil {
		return ApplyMemoryProfileResult{}, err
	}
	candidate.State = MemoryCandidateStateProfileApplying
	if err := r.profileApplyBarrier(memoryProfileApplyClaimed); err != nil {
		// The claim stands and nothing was written. The recovery pass will
		// find profile.md still reading as the base hash and hand the proposal
		// back to the user.
		return ApplyMemoryProfileResult{}, err
	}

	document, err := vault.ApplyProfileEdit(ctx, memoryfiles.ApplyProfileEditRequest{
		CandidateRelPath:      candidate.InboxPath,
		TargetRelPath:         memoryfiles.ProfileFileName,
		ExpectedContentHash:   input.ExpectedContentHash,
		ExpectedCandidateHash: decided.ContentHash,
		Content:               input.Content,
	})
	if err != nil {
		// A claim whose write was refused is resolved here rather than left for
		// the recovery pass, because the pass may no longer be able to resolve
		// it. The base hash is read before the claim and the write's own
		// compare-and-set reads the document again, so a second writer — another
		// apply, or the user's own hand-authored save — can move profile.md
		// between the two. The claim is then over a document that reads as
		// neither of its hashes, which is exactly the case recovery refuses to
		// guess about, and the proposal would sit claimed and undecidable
		// forever. Nothing was written, so nothing is owed: it goes back.
		return ApplyMemoryProfileResult{}, r.abandonProfileApplyClaim(ctx, vault, candidate, base, err)
	}
	result := ApplyMemoryProfileResult{Document: document}
	if barrierErr := r.profileApplyBarrier(memoryProfileApplyWritten); barrierErr != nil {
		// The write landed. Reporting the failure of what comes after it as a
		// failed apply would tell the user their edit did not happen while
		// their own document says it did, so the answer is the truthful one:
		// applied, tidying outstanding, claim still held so nothing can reject
		// it in the meantime.
		log.Printf("memory profile apply left unfinished for %s: %v", candidate.CandidateID, barrierErr)
		result.CleanupPending = true
		return result, nil
	}
	result.CleanupPending = !r.finishProfileApply(ctx, vault, candidate)
	return result, nil
}

// finishProfileApply retires the proposal an apply has already written, and
// reports whether it got all the way.
//
// The file is removed first and the reservation is released only if it went:
// the reservation is the manifest entry that says these bytes are Turing's to
// clean up, and dropping it beside a file that is still there would leave the
// user's vault holding a claim about them that nothing in the system knows
// about. The row goes either way, because an applied proposal is not one
// anybody may decide again.
func (r *Repository) finishProfileApply(ctx context.Context, vault *memoryfiles.Vault, candidate MemoryCandidate) bool {
	removed := true
	if err := vault.RemoveInboxNote(ctx, retiredCandidateRemoval(candidate.InboxPath)); err != nil {
		log.Printf("remove applied memory proposal %s: %v", candidate.CandidateID, err)
		removed = false
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("finish memory profile apply %s: %v", candidate.CandidateID, err)
		return false
	}
	defer func() { _ = tx.Rollback() }()
	if err := consumeMemoryCandidateTx(ctx, tx, candidate, MemoryCandidateStatePromoted, removed); err != nil {
		log.Printf("finish memory profile apply %s: %v", candidate.CandidateID, err)
		return false
	}
	if err := tx.Commit(); err != nil {
		log.Printf("finish memory profile apply %s: %v", candidate.CandidateID, err)
		return false
	}
	return removed
}

func (r *Repository) profileApplyBarrier(stage string) error {
	if r.memoryProfileApplyBarrier == nil {
		return nil
	}
	return r.memoryProfileApplyBarrier(stage)
}

// abandonProfileApplyClaim gives a claim back when the write it was taken for
// provably never happened, and returns the caller's own failure either way.
//
// "Provably" has two forms, and both are answered from the vault rather than
// from optimism. A compare-and-set refusal is the strong one: that check runs
// before a single byte is written, so a stale error is proof on its own,
// whatever the document says by the time this reads it. Otherwise the document
// itself is asked, against the base hash the claim recorded — the same question
// the recovery pass asks, so the two can never disagree about one apply.
//
// Anything else — a document that is neither, a read that failed, a reset that
// failed — keeps the claim. A claim held is a proposal nothing can reject while
// its outcome is unknown, which is the safe side of this to be wrong on.
func (r *Repository) abandonProfileApplyClaim(
	ctx context.Context,
	vault *memoryfiles.Vault,
	candidate MemoryCandidate,
	baseHash string,
	cause error,
) error {
	if !profileApplyLeftNothingBehind(ctx, vault, baseHash, cause) {
		return cause
	}
	if err := r.resetProfileApplyClaim(ctx, candidate); err != nil {
		log.Printf("hand back refused memory profile apply %s: %v", candidate.CandidateID, err)
	}
	return cause
}

func profileApplyLeftNothingBehind(
	ctx context.Context,
	vault *memoryfiles.Vault,
	baseHash string,
	cause error,
) bool {
	if errors.Is(cause, memoryfiles.ErrStaleContent) {
		return true
	}
	document := vault.EditableProfile(ctx)
	switch {
	case document.Available:
		return document.ContentHash == baseHash
	case document.Reason == memoryfiles.UnavailableVaultMissing:
		return baseHash == ""
	default:
		return false
	}
}

// currentProfileHash is the compare-and-set over profile.md, asked before
// anything is claimed.
//
// A profile that is simply not there yet is not a failure: that is the
// first-run state, and the empty token is the same one the vault's own writer
// reads as "I am creating this". A profile that exists and could not be read is
// refused outright — a claim recording a base hash nobody could compute would
// be a claim the recovery pass could never resolve.
func currentProfileHash(ctx context.Context, vault *memoryfiles.Vault, expected string) (string, error) {
	document := vault.EditableProfile(ctx)
	switch {
	case document.Available:
	case document.Reason == memoryfiles.UnavailableVaultMissing:
		document.ContentHash = ""
	default:
		return "", fmt.Errorf("read %s: %s", memoryfiles.ProfileFileName, document.Detail)
	}
	if document.ContentHash != expected {
		return "", &memoryfiles.StaleContentError{RelPath: memoryfiles.ProfileFileName}
	}
	return document.ContentHash, nil
}

// claimProfileApply moves a pending profile edit into 'profile_applying' and
// records the two hashes the recovery pass reads. The update is guarded on
// pending, so two applies racing over one proposal are resolved by one of them
// finding the row already claimed rather than by both writing the profile.
func (r *Repository) claimProfileApply(ctx context.Context, candidate MemoryCandidate, baseHash string, resultHash string) error {
	claimedAt := now()
	result, err := r.db.ExecContext(ctx, `
		UPDATE memory_candidates
		SET state = ?, decided_at = ?, updated_at = ?,
			apply_base_hash = ?, apply_result_hash = ?
		WHERE id = ? AND state = ?
	`, MemoryCandidateStateProfileApplying, claimedAt, claimedAt,
		baseHash, resultHash, candidate.CandidateID, MemoryCandidateStatePending)
	if err != nil {
		return err
	}
	claimed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if claimed != 1 {
		return fmt.Errorf("%w: %q cannot become %q",
			ErrMemoryCandidateInvalidTransition, candidate.State, MemoryCandidateStateProfileApplying)
	}
	return nil
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

	candidate, err := r.pendingCandidateForDecision(ctx, decision.CandidateID, MemoryCandidateStateRejected)
	if err != nil {
		return err
	}
	decided, err := decideAboutCandidateFile(ctx, vault, candidate, decision.ExpectedCandidateHash, "")
	if err != nil {
		return err
	}
	r.decisionFileBarrier()
	if err := vault.RemoveInboxNote(ctx, rejectionRemoval(candidate.InboxPath, decided)); err != nil {
		return err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := consumeMemoryCandidateTx(ctx, tx, candidate, MemoryCandidateStateRejected, true); err != nil {
		return err
	}
	return tx.Commit()
}

// rejectionRemoval turns what the pre-check managed to read into the removal a
// rejection is allowed to make.
//
// A proposal that read back is deleted by name: the primitive is handed the
// hash of exactly those bytes and refuses if the file has moved since, which is
// the user being told to look again rather than having something they did not
// read thrown away for them. A proposal that could not be read at all has no
// such name — nothing could parse it to produce one — so what goes down instead
// is the identity of the entry the read failed on. The primitive deletes that
// entry, still unreadable, or it deletes nothing; a file that took the name in
// between, even another broken one, is somebody's words nobody has decided
// about.
func rejectionRemoval(inboxPath string, decided decidedCandidateFile) memoryfiles.RemoveInboxNoteRequest {
	if decided.ContentHash == "" {
		return memoryfiles.RemoveInboxNoteRequest{
			RelPath:    inboxPath,
			Mode:       memoryfiles.RemoveUnreadableCandidate,
			Unreadable: decided.Unreadable,
		}
	}
	return memoryfiles.RemoveInboxNoteRequest{
		RelPath:             inboxPath,
		Mode:                memoryfiles.RemoveDecidedCandidate,
		ExpectedContentHash: decided.ContentHash,
	}
}

// retiredCandidateRemoval names Turing's own tidying: bytes whose outcome is
// already recorded somewhere else — an applied profile edit whose write landed,
// a candidate no row will ever describe, a file the session cleaner's manifest
// still names. None of them is a user deciding about text, so none of them has
// a hash to name, and leaving the file behind would be the failure rather than
// the safe side. It is separate from a rejection by name, so nothing can reach
// a hashless deletion by way of a decision.
func retiredCandidateRemoval(inboxPath string) memoryfiles.RemoveInboxNoteRequest {
	return memoryfiles.RemoveInboxNoteRequest{
		RelPath: inboxPath,
		Mode:    memoryfiles.RemoveRetiredCandidate,
	}
}

// pendingCandidateForDecision loads a candidate and checks everything the *row*
// can answer for before any file is touched: that it exists, that the lifecycle
// allows the move, that the provenance stored beside it is the one the server
// derived, and that the path stored beside it is still an inbox path. The last
// two are not redundant with the layers below — they turn a tampered row into a
// typed refusal instead of a forged citation or a primitive's confinement
// error.
//
// The kind is deliberately not among them. A row records what Turing proposed;
// which decision applies is a question about the file the user is looking at,
// and it is asked in decideAboutCandidateFile, under the same lock and against
// the same bytes as the compare-and-set. What the row keeps owning is what only
// it knows: identity, source session, provenance and lifecycle.
func (r *Repository) pendingCandidateForDecision(ctx context.Context, candidateID string, to string) (MemoryCandidate, error) {
	candidate, err := memoryCandidateByIDTx(ctx, r.db, candidateID)
	if err != nil {
		return MemoryCandidate{}, err
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

// consumeMemoryCandidateTx retires a decided candidate and, when the file it
// described is really gone, the reservation that tracked it. The delete is
// guarded on the state the candidate was read in, so two decisions racing
// cannot both believe they were the one that decided.
//
// releaseReservation is false in exactly one case: an apply whose write landed
// and whose proposal file could not be removed. The reservation is the manifest
// entry that says those bytes are Turing's to clean up, so releasing it beside
// a file that is still there would leave a claim about the user in their vault
// with nothing in the database naming it.
//
// The audit row lands in the same transaction as the decision, for the same
// reason session deletion records its own: a decision that is recorded
// separately can be lost while the change it describes survives. It carries the
// candidate's identity, its kind and the decision — never the claim it made,
// the file it lived in, the conversation that produced it, or any hash.
func consumeMemoryCandidateTx(
	ctx context.Context,
	tx *sql.Tx,
	candidate MemoryCandidate,
	decision string,
	releaseReservation bool,
) error {
	result, err := tx.ExecContext(ctx, `
		DELETE FROM memory_candidates WHERE id = ? AND state = ?
	`, candidate.CandidateID, candidate.State)
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
	if releaseReservation {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM vault_artifacts WHERE session_id = ? AND vault_path = ?
		`, candidate.SourceSessionID, candidate.InboxPath); err != nil {
			return err
		}
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
