package repository

import (
	"context"
	"database/sql"
	"log"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// maxProfileApplyClaimsPerPass bounds one recovery sweep. There is at most one
// live claim in an ordinary system — an apply holds the per-candidate lock for
// the length of one write — so a page this size is already far past anything a
// healthy vault produces, and the bound is here so a corrupted store cannot
// turn startup into an unbounded loop.
const maxProfileApplyClaimsPerPass = 64

// profileApplyClaim is one candidate caught mid-apply: the row, plus the two
// hashes that say what the write was going to do.
type profileApplyClaim struct {
	candidate  MemoryCandidate
	baseHash   string
	resultHash string
}

// recoverProfileApplies finishes or hands back every apply a crash left
// claimed.
//
// It is the exit from 'profile_applying', and it is deliberately the only one.
// A claim says the user accepted a profile edit and that profile.md may already
// carry it, so the question is not what a caller believes but what the document
// says, and there are exactly three answers:
//
//   - it reads as the result the claim recorded: the write landed, so the
//     proposal is retired and its file and reservation are cleaned up. This is
//     the same finish the apply itself would have done.
//   - it reads as the base the claim recorded: the write provably never
//     happened — the document is byte-for-byte what the apply was going to
//     replace — so the proposal goes back to pending and is the user's to
//     decide again.
//   - anything else: someone has been in the file since. Handing the proposal
//     back would give a rejection power over a document that may already say
//     these words, and finishing would claim an outcome nobody can see. The
//     claim is left standing, visible and undecidable, and the user's own text
//     is not touched either way.
//
// It runs before the vault pass rather than inside it, so it takes no
// vault-wide lock and cannot be blocked by a vault the pass refuses to walk —
// a claim must be resolvable even when the vault is past the scan bound.
func (r *Repository) recoverProfileApplies(ctx context.Context) (int, int, error) {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return 0, 0, err
	}
	claims, err := r.loadProfileApplyClaims(ctx)
	if err != nil {
		return 0, 0, err
	}
	finalized, reset := 0, 0
	for _, claim := range claims {
		outcome, err := r.recoverOneProfileApply(ctx, vault, claim)
		if err != nil {
			return finalized, reset, err
		}
		switch outcome {
		case profileApplyFinalized:
			finalized++
		case profileApplyReset:
			reset++
		}
	}
	return finalized, reset, nil
}

type profileApplyOutcome int

const (
	profileApplyUnresolved profileApplyOutcome = iota
	profileApplyFinalized
	profileApplyReset
)

func (r *Repository) recoverOneProfileApply(
	ctx context.Context,
	vault *memoryfiles.Vault,
	claim profileApplyClaim,
) (profileApplyOutcome, error) {
	// The same lock an apply holds, so recovery cannot land between a live
	// apply's claim and its write and decide the outcome out from under it.
	unlock, err := r.lockMemoryCandidateDecision(ctx, claim.candidate.CandidateID)
	if err != nil {
		return profileApplyUnresolved, err
	}
	defer unlock()

	// Re-read under the lock: the apply that took this claim may have finished
	// it in the meantime, in which case there is nothing here to resolve.
	current, err := memoryCandidateByIDTx(ctx, r.db, claim.candidate.CandidateID)
	if err != nil {
		if err == ErrMemoryCandidateNotFound {
			return profileApplyUnresolved, nil
		}
		return profileApplyUnresolved, err
	}
	if current.State != MemoryCandidateStateProfileApplying {
		return profileApplyUnresolved, nil
	}

	document := vault.EditableProfile(ctx)
	hash := document.ContentHash
	switch {
	case document.Available:
	case document.Reason == memoryfiles.UnavailableVaultMissing:
		// No profile at all. That is a real answer when the claim was creating
		// one: it means the write never landed.
		hash = ""
	default:
		// The document exists and could not be read. Nothing can be concluded
		// from bytes nobody has, so the claim is left exactly as it is.
		log.Printf("memory profile apply recovery deferred for %s: %s", current.CandidateID, document.Detail)
		return profileApplyUnresolved, nil
	}

	switch hash {
	case claim.resultHash:
		if err := r.finalizeRecoveredApply(ctx, vault, current); err != nil {
			return profileApplyUnresolved, err
		}
		return profileApplyFinalized, nil
	case claim.baseHash:
		if err := r.resetProfileApplyClaim(ctx, current); err != nil {
			return profileApplyUnresolved, err
		}
		return profileApplyReset, nil
	default:
		return profileApplyUnresolved, nil
	}
}

// finalizeRecoveredApply is the apply's own finish, run late. The file goes,
// then the row, and the reservation only if the file really went — the same
// rule the live path follows, for the same reason.
func (r *Repository) finalizeRecoveredApply(ctx context.Context, vault *memoryfiles.Vault, candidate MemoryCandidate) error {
	removed := true
	if err := vault.RemoveInboxNote(ctx, retiredCandidateRemoval(candidate.InboxPath)); err != nil {
		log.Printf("remove applied memory proposal %s: %v", candidate.CandidateID, err)
		removed = false
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Crash recovery, and deliberately not gated on the source session. The
	// words are already in the user's own profile; the row is the only thing
	// that records it, and refusing to finish would leave a proposal claimed
	// and undecidable until the cascade happened to remove it.
	if err := consumeMemoryCandidateTx(ctx, tx, candidate, MemoryCandidateStatePromoted, removed, false); err != nil {
		return err
	}
	return tx.Commit()
}

// resetProfileApplyClaim hands a proposal back to the user. The claim's hashes
// go with it: a pending row may not carry them, and a claim nobody is holding
// is not a claim.
func (r *Repository) resetProfileApplyClaim(ctx context.Context, candidate MemoryCandidate) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE memory_candidates
		SET state = ?, decided_at = NULL, updated_at = ?,
			apply_base_hash = NULL, apply_result_hash = NULL
		WHERE id = ? AND state = ?
	`, MemoryCandidateStatePending, now(), candidate.CandidateID, MemoryCandidateStateProfileApplying)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return nil
	}
	if err := recordMemoryReconcileTx(ctx, tx,
		memoryProfileApplyResetAction, candidate.CandidateID, "profile_unchanged"); err != nil {
		return err
	}
	return tx.Commit()
}

// memoryProfileApplyResetAction names the one lifecycle row a hand-back
// leaves. Like every other memory audit row it carries an identity and a
// reason, and never a hash or a word of the claim.
const memoryProfileApplyResetAction = "memory.candidate.profile_apply_reset"

func (r *Repository) loadProfileApplyClaims(ctx context.Context) ([]profileApplyClaim, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, source_session_id, kind, inbox_path, content_hash, body,
			evidence_refs_json, state, COALESCE(promoted_note_id, ''),
			COALESCE(decided_at, ''), created_at, updated_at,
			COALESCE(apply_base_hash, ''), COALESCE(apply_result_hash, '')
		FROM memory_candidates
		WHERE state = ?
		ORDER BY created_at
		LIMIT ?
	`, MemoryCandidateStateProfileApplying, maxProfileApplyClaimsPerPass)
	if err != nil {
		return nil, err
	}
	var claims []profileApplyClaim
	for rows.Next() {
		claim, err := scanProfileApplyClaim(rows)
		if err != nil {
			return nil, closeRowsWith(rows, err)
		}
		claims = append(claims, claim)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	return claims, nil
}

func scanProfileApplyClaim(rows *sql.Rows) (profileApplyClaim, error) {
	var claim profileApplyClaim
	var refsJSON string
	if err := rows.Scan(
		&claim.candidate.CandidateID, &claim.candidate.SourceSessionID, &claim.candidate.Kind,
		&claim.candidate.InboxPath, &claim.candidate.ContentHash, &claim.candidate.Body,
		&refsJSON, &claim.candidate.State, &claim.candidate.PromotedNoteID,
		&claim.candidate.DecidedAt, &claim.candidate.CreatedAt, &claim.candidate.UpdatedAt,
		&claim.baseHash, &claim.resultHash,
	); err != nil {
		return profileApplyClaim{}, err
	}
	refs, err := decodeMemoryEvidenceRefs(refsJSON)
	if err != nil {
		return profileApplyClaim{}, err
	}
	claim.candidate.EvidenceRefs = refs
	return claim, nil
}
