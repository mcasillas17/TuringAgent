package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

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
func (r *Repository) PromoteMemoryCandidate(ctx context.Context, candidateID string) (MemoryNote, error) {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return MemoryNote{}, err
	}
	candidate, err := r.pendingCandidateForDecision(ctx, candidateID, MemoryCandidateKindBelief, MemoryCandidateStatePromoted)
	if err != nil {
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
	status := MemoryNoteStatusManaged
	if len(candidate.EvidenceRefs) > 0 && len(live) == 0 {
		// Every conversation behind this claim is already gone. The note is
		// still accepted, but nothing may answer with it as if it were
		// grounded in something the user can still look at.
		status = MemoryNoteStatusWithdrawn
	}
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
	if err := linkMemoryEvidenceTx(ctx, tx, note.NoteID, live); err != nil {
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

// ApplyMemoryProfileCandidate writes the user's profile on the authority of a
// profile_edit candidate, then consumes the candidate.
//
// The profile is a pinned document, not a note: it is never projected into the
// note index and never becomes searchable memory. The candidate file is
// removed through the inbox-only primitive, so this path cannot be pointed at
// anything else.
func (r *Repository) ApplyMemoryProfileCandidate(ctx context.Context, input ApplyMemoryProfileInput) (memoryfiles.ProfileDocument, error) {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return memoryfiles.ProfileDocument{}, err
	}
	candidate, err := r.pendingCandidateForDecision(ctx, input.CandidateID, MemoryCandidateKindProfileEdit, MemoryCandidateStatePromoted)
	if err != nil {
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
func (r *Repository) RejectMemoryCandidate(ctx context.Context, candidateID string) error {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return err
	}
	candidate, err := r.pendingCandidateForDecision(ctx, candidateID, "", MemoryCandidateStateRejected)
	if err != nil {
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
// the kind this decision is for, that the lifecycle allows the move, and that
// the path stored beside it is still an inbox path. The last check is not
// redundant with the vault's own gate — it is what turns a tampered row into a
// typed refusal instead of a primitive's confinement error.
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
