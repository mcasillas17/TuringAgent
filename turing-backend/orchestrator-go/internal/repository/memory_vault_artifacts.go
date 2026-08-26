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
	CreatedAt    string
	FinalizedAt  string
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

// FinalizeVaultArtifact closes a reservation once the file is on disk. It only
// advances a reservation that is still writing, so a second finalization is a
// refused transition rather than a silently rewritten timestamp.
func (r *Repository) FinalizeVaultArtifact(ctx context.Context, artifactID string, sessionID string) (VaultArtifact, error) {
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
		SET state = ?, finalized_at = ?
		WHERE id = ? AND session_id = ? AND state = ?
	`, VaultArtifactStateReady, finalizedAt, artifactID, sessionID, VaultArtifactStateWriting); err != nil {
		return VaultArtifact{}, err
	}
	artifact.State = VaultArtifactStateReady
	artifact.FinalizedAt = finalizedAt
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
		SELECT id, session_id, vault_path, physical_path, state, created_at, COALESCE(finalized_at, '')
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
			continue
		}
		payloadBytes, err := json.Marshal(map[string]string{
			"state":     VaultArtifactStateDeleteFailed,
			"errorCode": failure.errorCode,
		})
		if err != nil {
			return err
		}
		if err := recordAuditTx(ctx, tx, "", "system", "", vaultArtifactCleanupFailedAction, failure.artifactID, string(payloadBytes)); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	removed := make([]string, 0, len(artifacts))
	failures := make([]vaultArtifactFailure, 0, len(artifacts))
	collected := make([]error, 0, maxVaultPurgeErrors)
	for _, artifact := range artifacts {
		errorCode, failure := removeVaultArtifactFile(ctx, vault, artifact)
		if failure == nil {
			removed = append(removed, artifact.ArtifactID)
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
// Both failures are returned as their class and nothing more. The underlying
// removal error can name a path inside the user's vault, and this value is
// joined into what a withdrawal reports back to its caller; the class is what
// the audit row records, and it is all a caller can act on anyway.
func removeVaultArtifactFile(ctx context.Context, vault *memoryfiles.Vault, artifact VaultArtifact) (string, error) {
	if _, err := validateVaultInboxPath(artifact.VaultPath); err != nil {
		return "vault_path_scope", ErrVaultArtifactPathScope
	}
	if err := vault.RemoveInboxNote(ctx, retiredCandidateRemoval(artifact.VaultPath)); err != nil {
		return "vault_remove_failed", ErrVaultArtifactRemoveFailed
	}
	return "", nil
}

func vaultArtifactByID(ctx context.Context, q rowQuerier, artifactID string, sessionID string) (VaultArtifact, error) {
	var artifact VaultArtifact
	err := q.QueryRowContext(ctx, `
		SELECT id, session_id, vault_path, physical_path, state, created_at, COALESCE(finalized_at, '')
		FROM vault_artifacts
		WHERE id = ? AND session_id = ?
	`, artifactID, sessionID).Scan(
		&artifact.ArtifactID,
		&artifact.SessionID,
		&artifact.VaultPath,
		&artifact.PhysicalPath,
		&artifact.State,
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
			&artifact.CreatedAt,
			&artifact.FinalizedAt,
		); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}
