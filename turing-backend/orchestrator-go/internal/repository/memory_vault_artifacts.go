package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// Vault artifact states mirror the CHECK constraint on vault_artifacts.
// "writing" is a durable reservation taken before any byte reaches the vault,
// so a crash between the reservation and the write leaves evidence that a file
// may exist rather than silence. It is deliberately the same vocabulary
// sandbox_artifacts uses, over a different table: the two manifests answer
// different retention questions and must never share rows.
const (
	VaultArtifactStateWriting      = "writing"
	VaultArtifactStateReady        = "ready"
	VaultArtifactStateDeleteFailed = "delete_failed"
)

// maxVaultArtifactPathBytes bounds a manifest path. It is far below the
// vault's own path limit because everything this manifest records is a note
// the orchestrator named itself.
const maxVaultArtifactPathBytes = 512

// vaultArtifactCleanupFailedAction is the audit action a failed vault cleanup
// records, one row per artifact, mirroring session.artifact.cleanup.failed for
// the sandbox manifest.
const vaultArtifactCleanupFailedAction = "session.vault_artifact.cleanup.failed"

// The classes a removal can fail under, as the audit row records them. They are
// constants because the same three words are now written by two callers — the
// session cleaner and Turing's own tidying — and a class spelled differently in
// one of them is a failure an operator cannot find by grepping for the other.
const (
	vaultArtifactPathScopeCode         = "vault_path_scope"
	vaultArtifactRemoveFailedCode      = "vault_remove_failed"
	vaultArtifactOwnershipUnprovenCode = "vault_ownership_unproven"
)

// maxVaultPurgeErrors bounds how many underlying failures one purge report
// carries back to its caller. A pass now visits every row rather than
// abandoning the manifest at the first refusal, so the number of failures it
// can observe is the number of rows the session owns — and an error assembled
// one clause per row is a value whose size a manifest gets to choose. Every
// failed row is still marked and still audited; only the report is capped.
const maxVaultPurgeErrors = 4

// maxVaultPurgeErrorBytes is the ceiling the bounded report stays under. It is
// the cap the failure classes and the summary fit inside with room to spare,
// and it exists so the bound is a property that can be asserted rather than an
// intention.
const maxVaultPurgeErrorBytes = 1024

var (
	// ErrVaultArtifactPathScope reports a path that is not a note inside the
	// vault inbox. The manifest only ever records candidate files, so anything
	// naming beliefs/, a pinned document, or a path shaped to escape either is
	// refused before a row exists.
	ErrVaultArtifactPathScope = errors.New("vault artifact path is outside the vault inbox")
	// ErrVaultArtifactSessionUnavailable reports a reservation for a session
	// that does not exist or has stopped accepting work.
	ErrVaultArtifactSessionUnavailable = errors.New("vault artifact session is unavailable")
	// ErrVaultArtifactNotFound reports an artifact id with no manifest row in
	// this session.
	ErrVaultArtifactNotFound = errors.New("vault artifact not found")
	// ErrVaultArtifactInvalidTransition refuses a state change the lifecycle
	// does not allow, such as finalizing an artifact twice.
	ErrVaultArtifactInvalidTransition = errors.New("vault artifact state transition is not allowed")
	// ErrVaultArtifactExists reports a second reservation for a path that is
	// already claimed — by this session or by any other. A vault path belongs
	// to one session at a time, because two claims on one file means one
	// session's cleanup deletes the other's note.
	ErrVaultArtifactExists = errors.New("vault artifact is already reserved")
	// ErrVaultArtifactRemoveFailed reports a tracked file the vault would not
	// release. It stands in for the underlying error deliberately: that error
	// can name a path inside the user's vault, and this value is joined into
	// what a withdrawal reports back to its caller.
	ErrVaultArtifactRemoveFailed = errors.New("vault artifact could not be removed")
	// ErrVaultArtifactOwnershipUnproven refuses a removal whose manifest row
	// names no bytes. A row like that proves a path, and a path is not an
	// owner: the file under it may be the write this row was taken for, or it
	// may be something the user has since put there themselves. It is a
	// refusal rather than a failure of the file — nothing was touched — and the
	// row is kept, so a pass that can prove ownership still drains it.
	ErrVaultArtifactOwnershipUnproven = errors.New("vault artifact ownership is not proven")
	// ErrVaultArtifactManifestFinalize reports the opposite situation: every
	// note was removed and the rows that named them could not be dropped. It is
	// separated from a removal failure because the two demand contradictory
	// records — one says the user's notes are still on disk, the other says
	// they are gone and only Turing's bookkeeping is behind.
	ErrVaultArtifactManifestFinalize = errors.New("vault artifact manifest could not be finalized")
)

// VaultArtifact is one manifest row: the orchestrator's record that a session
// is responsible for a specific file inside the user's vault.
type VaultArtifact struct {
	ArtifactID string
	SessionID  string
	// VaultPath is the vault-relative path the user sees in Obsidian.
	VaultPath string
	// PhysicalPath is where the bytes live relative to the vault root. The
	// vault has no separate physical layout today, so it equals VaultPath; the
	// column is kept distinct because it is what the globally UNIQUE constraint
	// and a future relocation are keyed on, and collapsing them would make a
	// move indistinguishable from a rename the user did themselves.
	PhysicalPath string
	State        string
	// ExpectedContentHash is the ownership binding: a hash of the whole file
	// exactly as Turing wrote it, and the only thing that entitles a cleanup
	// pass to unlink anything at PhysicalPath.
	//
	// It is empty for a reservation, which is taken before the write and so has
	// no bytes to name, and for a row a pass could not remove. A cleanup that
	// finds it empty refuses: without it the row proves a path and a path is
	// not an owner.
	ExpectedContentHash string
	CreatedAt           string
	FinalizedAt         string
}

// ReserveVaultArtifactInput is the server-verified description of a vault write
// that is about to happen. There is no caller-supplied artifact id and no
// physical path: both are derived here, so the manifest cannot be widened by
// whoever is asking.
type ReserveVaultArtifactInput struct {
	SessionID string
	VaultPath string
}

// validateVaultInboxPath is this manifest's own gate. It refuses everything
// that is not a plain, already-normalised note path under inbox/, without
// consulting the vault: a reservation is taken before any file exists, so it
// cannot lean on the filesystem to tell it whether a path is legitimate.
func validateVaultInboxPath(vaultPath string) (string, error) {
	if vaultPath == "" || len(vaultPath) > maxVaultArtifactPathBytes {
		return "", ErrVaultArtifactPathScope
	}
	if strings.ContainsRune(vaultPath, 0) || strings.ContainsAny(vaultPath, "\\") {
		return "", ErrVaultArtifactPathScope
	}
	for _, symbol := range vaultPath {
		if symbol < 0x20 || symbol == 0x7f {
			return "", ErrVaultArtifactPathScope
		}
	}
	if vaultPath != path.Clean(vaultPath) {
		return "", ErrVaultArtifactPathScope
	}
	if !strings.HasPrefix(vaultPath, memoryfiles.InboxDirName+"/") {
		return "", ErrVaultArtifactPathScope
	}
	if !strings.HasSuffix(vaultPath, ".md") {
		return "", ErrVaultArtifactPathScope
	}
	for _, component := range strings.Split(vaultPath, "/") {
		if component == "" || component == "." || component == ".." {
			return "", ErrVaultArtifactPathScope
		}
		if len(component) > memoryfiles.MaxVaultPathComponentBytes {
			return "", ErrVaultArtifactPathScope
		}
	}
	return vaultPath, nil
}

// ReserveVaultArtifact records that a session is about to write one file into
// the vault inbox, before the write happens. Existence and liveness of the
// session are checked inside the insert itself, so a session that starts being
// deleted concurrently either loses the race and cascades this row away or
// wins it and prevents the row being created at all.
func (r *Repository) ReserveVaultArtifact(ctx context.Context, input ReserveVaultArtifactInput) (VaultArtifact, error) {
	vaultPath, err := validateVaultInboxPath(input.VaultPath)
	if err != nil {
		return VaultArtifact{}, err
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return VaultArtifact{}, ErrVaultArtifactSessionUnavailable
	}
	artifact := VaultArtifact{
		ArtifactID:   ids.New("vaultart"),
		SessionID:    input.SessionID,
		VaultPath:    vaultPath,
		PhysicalPath: vaultPath,
		State:        VaultArtifactStateWriting,
		CreatedAt:    now(),
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO vault_artifacts (
			id, session_id, vault_path, physical_path, state, created_at, finalized_at
		)
		SELECT ?, ?, ?, ?, ?, ?, NULL
		WHERE EXISTS (
			SELECT 1 FROM sessions WHERE id = ? AND deletion_state = 'active'
		)
	`,
		artifact.ArtifactID,
		artifact.SessionID,
		artifact.VaultPath,
		artifact.PhysicalPath,
		artifact.State,
		artifact.CreatedAt,
		artifact.SessionID,
	)
	if isUniqueViolation(err) {
		return VaultArtifact{}, ErrVaultArtifactExists
	}
	if err != nil {
		return VaultArtifact{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return VaultArtifact{}, err
	}
	if inserted != 1 {
		return VaultArtifact{}, ErrVaultArtifactSessionUnavailable
	}
	return artifact, nil
}

// FinalizeVaultArtifact closes a reservation once the file is on disk, and
// binds the row to the bytes that landed. It only advances a reservation that
// is still writing, so a second finalization is a refused transition rather
// than a silently rewritten timestamp.
//
// The hash is required, and it is required here rather than at the reservation
// because this is the first moment it can be true. A row that reaches 'ready'
// without one would claim a file it cannot identify, and the cleanup that reads
// it would be back to trusting a path.
func (r *Repository) FinalizeVaultArtifact(
	ctx context.Context,
	artifactID string,
	sessionID string,
	contentHash string,
) (VaultArtifact, error) {
	if strings.TrimSpace(contentHash) == "" {
		return VaultArtifact{}, ErrVaultArtifactOwnershipUnproven
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return VaultArtifact{}, err
	}
	defer func() { _ = tx.Rollback() }()

	artifact, err := vaultArtifactByID(ctx, tx, artifactID, sessionID)
	if err != nil {
		return VaultArtifact{}, err
	}
	if artifact.State != VaultArtifactStateWriting {
		return VaultArtifact{}, ErrVaultArtifactInvalidTransition
	}
	finalizedAt := now()
	if _, err := tx.ExecContext(ctx, `
		UPDATE vault_artifacts
		SET state = ?, finalized_at = ?, expected_content_hash = ?
		WHERE id = ? AND session_id = ? AND state = ?
	`, VaultArtifactStateReady, finalizedAt, contentHash, artifactID, sessionID, VaultArtifactStateWriting); err != nil {
		return VaultArtifact{}, err
	}
	artifact.State = VaultArtifactStateReady
	artifact.FinalizedAt = finalizedAt
	artifact.ExpectedContentHash = contentHash
	if err := tx.Commit(); err != nil {
		return VaultArtifact{}, err
	}
	return artifact, nil
}

// ReleaseVaultArtifactReservation withdraws a reservation whose write never
// happened. It refuses to touch anything already finalized, so a later failure
// cannot erase the manifest row for a file that is sitting in the user's vault.
func (r *Repository) ReleaseVaultArtifactReservation(ctx context.Context, artifactID string, sessionID string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM vault_artifacts
		WHERE id = ? AND session_id = ? AND state = ?
	`, artifactID, sessionID, VaultArtifactStateWriting)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

// SessionVaultArtifacts lists every vault file one session is responsible for.
func (r *Repository) SessionVaultArtifacts(ctx context.Context, sessionID string) ([]VaultArtifact, error) {
	return queryVaultArtifacts(ctx, r.db, `
		SELECT id, session_id, vault_path, physical_path, state,
			COALESCE(expected_content_hash, ''), created_at, COALESCE(finalized_at, '')
		FROM vault_artifacts
		WHERE session_id = ?
		ORDER BY created_at, id
	`, sessionID)
}

// PendingSessionVaultArtifacts is the cleaner's worklist: every manifest row
// the session still owns, including the ones a previous pass could not delete.
//
// A delete_failed row is not a closed matter — it is a file still sitting in
// the user's vault. Leaving those rows out of the worklist is how a retry
// reports a completed withdrawal while the note it was supposed to remove is
// still there, and the manifest can never drain. The row only disappears when
// the file actually does, so re-attempting a failed deletion is idempotent by
// construction: a file that is already gone removes cleanly.
func (r *Repository) PendingSessionVaultArtifacts(ctx context.Context, sessionID string) ([]VaultArtifact, error) {
	return r.SessionVaultArtifacts(ctx, sessionID)
}

// CountSessionVaultArtifacts reports how many vault files a session still owns,
// which is what a withdrawal receipt reports as outstanding work.
func (r *Repository) CountSessionVaultArtifacts(ctx context.Context, sessionID string) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM vault_artifacts WHERE session_id = ?
	`, sessionID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// MarkSessionVaultArtifactsDeleteFailed records that cleanup reached the vault
// and could not remove the named files, so a withdrawal stays retryable instead
// of reporting a completion that left the user's notes behind.
//
// It marks exactly the artifacts it is given and no others. A pass that could
// not remove some of the session's files has already deleted the rest, and
// marking those as failures would file an audit row saying Turing could not
// delete a file it just deleted — a false record of the user's own withdrawal,
// and a retry sent looking for something that is already gone.
//
// The statement names vault_artifacts and only vault_artifacts: sandbox rows
// have their own cleaner, their own policy column and their own audit action,
// and an id from that manifest matches nothing here.
func (r *Repository) MarkSessionVaultArtifactsDeleteFailed(ctx context.Context, sessionID string, artifactIDs []string, errorCode string) error {
	failures := make([]vaultArtifactFailure, 0, len(artifactIDs))
	for _, artifactID := range artifactIDs {
		failures = append(failures, vaultArtifactFailure{artifactID: artifactID, errorCode: errorCode})
	}
	return r.markVaultArtifactsDeleteFailed(ctx, sessionID, failures)
}

// vaultArtifactFailure is one row a pass could not remove, with the class it
// failed under. Each row carries its own class because one pass now visits
// every row: a manifest can hold a tampered path and an unreachable file at
// once, and collapsing them onto a single class would record one of the two
// failures as the other.
type vaultArtifactFailure struct {
	artifactID string
	errorCode  string
}

// markVaultArtifactsDeleteFailed marks each failed row and audits it once.
//
// "Once" is the load-bearing word. A stuck withdrawal is retried on a ticker,
// and every retry re-reads the same unremovable rows — so a marker that
// re-audits a row it has already marked turns one file the vault would not
// release into an unbounded stream of audit rows all saying the same thing.
// Excluding rows that are already delete_failed makes the mark the transition
// it claims to be, and the audit the record of that transition.
func (r *Repository) markVaultArtifactsDeleteFailed(ctx context.Context, sessionID string, failures []vaultArtifactFailure) error {
	if len(failures) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, failure := range failures {
		if err := markVaultArtifactDeleteFailedTx(ctx, tx, sessionID, failure); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// markVaultArtifactDeleteFailedTx is that mark for one row, inside a
// transaction the caller owns.
//
// It is its own function because a second caller needs exactly it: Turing's own
// tidying after an applied profile edit consumes the proposal's row whether or
// not the file went, so the manifest row becomes the only record that a file
// may still be there — and a row that does not say a removal was attempted is a
// row reconcile will later release for a path that holds nothing.
func markVaultArtifactDeleteFailedTx(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
	failure vaultArtifactFailure,
) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE vault_artifacts
		SET state = ?
		WHERE id = ? AND session_id = ? AND state <> ?
	`, VaultArtifactStateDeleteFailed, failure.artifactID, sessionID, VaultArtifactStateDeleteFailed)
	if err != nil {
		return err
	}
	marked, err := result.RowsAffected()
	if err != nil {
		return err
	}
	// The audit row lands only for a row this statement actually moved, in
	// the same transaction as the mark: an id naming nothing in this
	// manifest is not a vault failure and does not get recorded as one, and
	// neither is a row a previous pass already reported.
	if marked != 1 {
		return nil
	}
	payloadBytes, err := json.Marshal(map[string]string{
		"state":     VaultArtifactStateDeleteFailed,
		"errorCode": failure.errorCode,
	})
	if err != nil {
		return err
	}
	return recordAuditTx(ctx, tx, "", "system", "", vaultArtifactCleanupFailedAction, failure.artifactID, string(payloadBytes))
}

// markUnremovedVaultArtifactTx records that a removal was attempted at one
// vault path and did not remove the file, on whichever manifest row names that
// path for this session.
//
// A path rather than an id, because the callers that need it are decisions
// about a proposal and know the path the proposal was written to; the row is
// looked up here so no caller has to hold two identities for one file.
//
// A path with no row is not an error. The write may never have landed, the
// session may be mid-cascade, and a decision that has just consumed its
// proposal has nothing left to record against.
func markUnremovedVaultArtifactTx(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
	vaultPath string,
	actedHash string,
	errorCode string,
) error {
	var artifactID string
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM vault_artifacts WHERE session_id = ? AND vault_path = ?
	`, sessionID, vaultPath).Scan(&artifactID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// The bytes the decision acted on, not the ones the row was created with.
	// A user may edit a proposal in their vault before accepting it — which is
	// what a vault is for — and the decision read and verified what was
	// actually there. A row left bound to the words Turing first proposed can
	// never prove ownership of the file again, so the withdrawal that comes
	// later refuses forever and the session never finishes deleting.
	if actedHash != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE vault_artifacts
			SET expected_content_hash = ?
			WHERE id = ? AND session_id = ?
		`, actedHash, artifactID, sessionID); err != nil {
			return err
		}
	}
	return markVaultArtifactDeleteFailedTx(ctx, tx, sessionID, vaultArtifactFailure{
		artifactID: artifactID,
		errorCode:  errorCode,
	})
}

// DeleteVaultArtifacts removes manifest rows for exactly the artifacts the
// cleaner deleted, and refuses to reach into any other manifest: an id that
// names a sandbox artifact matches nothing here, so a mixed list removes the
// vault rows and leaves the sandbox rows to their own cleaner.
func (r *Repository) DeleteVaultArtifacts(ctx context.Context, artifactIDs []string) error {
	if len(artifactIDs) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, artifactID := range artifactIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM vault_artifacts WHERE id = ?`, artifactID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PurgeSessionVaultArtifacts is the hook the session-deletion vault cleaner
// calls.
//
// It removes the vault files a session left in the inbox, then removes manifest
// rows for exactly the files that were actually removed.
//
// Every row is attempted, and a row that cannot be removed does not stop the
// pass. Each row names a different file, and one unusable row — a reservation
// tampered to point outside the inbox, a note the vault will not release — says
// nothing about the note beside it. Abandoning the manifest at the first
// refusal leaves every sibling in the user's vault behind a row that can never
// drain: the retry re-reads the same worklist, fails on the same row, and stops
// in the same place, forever. So the pass keeps going, the drainable siblings
// leave, and only the rows still naming a file are kept.
//
// Order is deliberate: the rows whose files are gone leave the manifest before
// anything is filed as a failure, so no audit row claims a failure for a file
// that was deleted. Only the rows that actually failed are marked
// delete_failed, one redacted audit row each, and only the first time — a stuck
// withdrawal is retried on a ticker, and re-auditing a row already marked turns
// one broken file into an unbounded stream of identical audit rows.
//
// A pass that could not drop the rows for the notes it did remove reports
// ErrVaultArtifactManifestFinalize, whether or not another row failed beside
// them. The rows that genuinely failed are marked and audited one by one here,
// so that fact is already recorded where it can be acted on; the class the
// caller reads is the one describing the rows that are being kept despite
// naming nothing. Reporting the pass as a cleanup failure instead invites the
// caller to mark every row the session still owns, which by then includes rows
// naming notes that are gone. The notes are gone, the rows are the retry's
// worklist, and removal is idempotent, so the retry drains them rather than
// repeating the work.
//
// What it reports back is bounded and opaque. Every failed row is marked and
// audited, but the error carries at most maxVaultPurgeErrors failure classes
// plus a count: the number of failures a pass can observe is the number of rows
// the session owns, and the underlying errors can name paths inside the user's
// vault.
//
// Removal goes through RemoveInboxNote, which refuses every path outside
// inbox/. A missing file is a success: cleanup is retried after partial
// failures, and a file that is already gone is the outcome that was wanted.
//
// It deliberately does not take the vault-wide pass lock. It touches only the
// paths its own manifest names, one at a time, under the primitive's per-path
// lock — and a withdrawal that waited on a whole-vault pass could be wedged by
// one, having already been reached from a call that will itself need that lock
// to finish.
func (r *Repository) PurgeSessionVaultArtifacts(ctx context.Context, sessionID string) (int, error) {
	artifacts, err := r.PendingSessionVaultArtifacts(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	// An empty worklist is finished work, whether or not a vault is attached.
	// An install with no vault has no notes to remove, and refusing here would
	// hold every withdrawal on such an install open forever on a scope that
	// owns nothing. A vault that is genuinely missing while rows still name
	// files in it is a different matter, and falls through to the error below.
	if len(artifacts) == 0 {
		return 0, nil
	}
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return 0, err
	}
	cleared := make([]VaultArtifact, 0, len(artifacts))
	failures := make([]vaultArtifactFailure, 0, len(artifacts))
	collected := make([]error, 0, maxVaultPurgeErrors)
	for _, artifact := range artifacts {
		errorCode, failure := removeVaultArtifactFile(ctx, vault, artifact)
		if failure == nil {
			cleared = append(cleared, artifact)
			continue
		}
		failures = append(failures, vaultArtifactFailure{
			artifactID: artifact.ArtifactID,
			errorCode:  errorCode,
		})
		if len(collected) < maxVaultPurgeErrors {
			collected = append(collected, failure)
		}
	}
	// A file whose visible name is gone can still be reachable: a removal that
	// could not unlink puts the entry back under its own name and leaves the
	// reserved one it was detached to, so the same bytes have two names and the
	// retry above only ever removes one of them. Retiring the row now would end
	// the withdrawal over a note still sitting in the user's vault under a name
	// no listing shows, so the reserved copies of exactly these bytes go too —
	// and a row whose second copy would not go stays.
	removed := make([]string, 0, len(cleared))
	residue, residueErr := vault.RemoveInboxResidue(ctx, clearedContentHashes(cleared))
	for _, artifact := range cleared {
		// A row that named no bytes contributed no hash, so a sweep that could
		// not finish says nothing about it. Those rows are the crash artifact
		// whose write never landed, and they have to be able to drain or every
		// such crash strands a withdrawal for good — the more so now that
		// reconcile leaves a marked row alone.
		var leftover error
		if artifact.ExpectedContentHash != "" {
			leftover = residueErr
			if leftover == nil {
				leftover = residue[artifact.ExpectedContentHash]
			}
		}
		if leftover == nil {
			removed = append(removed, artifact.ArtifactID)
			continue
		}
		failures = append(failures, vaultArtifactFailure{
			artifactID: artifact.ArtifactID,
			errorCode:  vaultArtifactRemoveFailedCode,
		})
		if len(collected) < maxVaultPurgeErrors {
			collected = append(collected, ErrVaultArtifactRemoveFailed)
		}
	}
	forgetErr := r.DeleteVaultArtifacts(ctx, removed)
	if forgetErr != nil {
		// Whatever else this pass observed, the rows for the notes it did
		// remove are still here — and that is a finalization failure, not a
		// note that survived. It is classified as one regardless of the
		// failures beside it: those rows are marked and audited individually,
		// just below, while a caller that read only a cleanup failure here
		// would answer it by marking every row the session still owns, which
		// now includes rows naming notes that are gone.
		forgetErr = errors.Join(ErrVaultArtifactManifestFinalize, forgetErr)
	}
	if len(failures) == 0 {
		if forgetErr != nil {
			return 0, forgetErr
		}
		return len(removed), nil
	}
	return 0, errors.Join(
		forgetErr,
		vaultPurgeFailure(collected, len(failures), len(artifacts)),
		r.markVaultArtifactsDeleteFailed(ctx, sessionID, failures),
	)
}

// clearedContentHashes names the bytes a pass has already been entitled to
// delete, which is exactly what the residue sweep may act on. A row that never
// named any bytes contributes nothing: it could not authorise a removal at its
// own path and it cannot authorise one under a reserved name either.
func clearedContentHashes(cleared []VaultArtifact) []string {
	hashes := make([]string, 0, len(cleared))
	for _, artifact := range cleared {
		if artifact.ExpectedContentHash != "" {
			hashes = append(hashes, artifact.ExpectedContentHash)
		}
	}
	return hashes
}

// vaultPurgeFailure is the bounded, opaque report of a pass that could not
// finish. It joins the sampled failure classes so a caller can still recognise
// one with errors.Is, and adds the counts the sample leaves out, so a report
// truncated at maxVaultPurgeErrors still says how much it is not showing.
func vaultPurgeFailure(collected []error, failed int, total int) error {
	if failed == 0 {
		return nil
	}
	reported := make([]error, 0, len(collected)+1)
	reported = append(reported, collected...)
	reported = append(reported, fmt.Errorf(
		"vault artifact cleanup failed for %d of %d tracked files", failed, total))
	return errors.Join(reported...)
}

// removeVaultArtifactFile deletes one tracked file and names the failure in the
// vocabulary the audit row records. The path is re-checked against the inbox
// gate first: a manifest row that has been tampered with is refused here as a
// typed error rather than reaching the primitive's confinement check, and a
// tampered row can never be turned into a way to delete a belief either way.
//
// A row that names no bytes is where this splits in two, because "delete the
// file" and "there is no file" are different questions and only the first needs
// an owner. A reservation is taken before the write, so a crash between the two
// leaves a row whose file never existed — and that row has to be able to drain,
// or every such crash strands a withdrawal forever. So the empty case asks the
// confined reader whether anything is under the path at all: nothing there is
// the outcome the user asked for and the row goes. Something there is a file
// this row cannot prove it owns — the write that landed, or whatever the user
// has put there since — and it is refused untouched. Reconcile can still bind
// the first of those, and the next pass removes it.
//
// Both failures are returned as their class and nothing more. The underlying
// removal error can name a path inside the user's vault, and this value is
// joined into what a withdrawal reports back to its caller; the class is what
// the audit row records, and it is all a caller can act on anyway.
func removeVaultArtifactFile(ctx context.Context, vault *memoryfiles.Vault, artifact VaultArtifact) (string, error) {
	if _, err := validateVaultInboxPath(artifact.VaultPath); err != nil {
		return vaultArtifactPathScopeCode, ErrVaultArtifactPathScope
	}
	if artifact.ExpectedContentHash == "" {
		// Absence, and durably: this answer retires the row, and an absence
		// nobody has flushed is one a crash can undo — leaving the file back
		// under its path with nothing naming it.
		absent, err := vault.ConfirmInboxNoteAbsent(ctx, artifact.VaultPath)
		if err != nil {
			return vaultArtifactRemoveFailedCode, ErrVaultArtifactRemoveFailed
		}
		if !absent {
			return vaultArtifactOwnershipUnprovenCode, ErrVaultArtifactOwnershipUnproven
		}
		return "", nil
	}
	if err := vault.RemoveInboxNote(ctx, retiredCandidateRemoval(artifact.VaultPath, artifact.ExpectedContentHash)); err != nil {
		return vaultArtifactRemoveFailedCode, ErrVaultArtifactRemoveFailed
	}
	return "", nil
}

func vaultArtifactByID(ctx context.Context, q rowQuerier, artifactID string, sessionID string) (VaultArtifact, error) {
	var artifact VaultArtifact
	err := q.QueryRowContext(ctx, `
		SELECT id, session_id, vault_path, physical_path, state,
			COALESCE(expected_content_hash, ''), created_at, COALESCE(finalized_at, '')
		FROM vault_artifacts
		WHERE id = ? AND session_id = ?
	`, artifactID, sessionID).Scan(
		&artifact.ArtifactID,
		&artifact.SessionID,
		&artifact.VaultPath,
		&artifact.PhysicalPath,
		&artifact.State,
		&artifact.ExpectedContentHash,
		&artifact.CreatedAt,
		&artifact.FinalizedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return VaultArtifact{}, ErrVaultArtifactNotFound
	}
	if err != nil {
		return VaultArtifact{}, err
	}
	return artifact, nil
}

type contextQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryVaultArtifacts(ctx context.Context, q contextQuerier, query string, args ...any) ([]VaultArtifact, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var artifacts []VaultArtifact
	for rows.Next() {
		var artifact VaultArtifact
		if err := rows.Scan(
			&artifact.ArtifactID,
			&artifact.SessionID,
			&artifact.VaultPath,
			&artifact.PhysicalPath,
			&artifact.State,
			&artifact.ExpectedContentHash,
			&artifact.CreatedAt,
			&artifact.FinalizedAt,
		); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}
