package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
// It marks exactly the artifacts it is given and no others. A pass that fails
// partway through has already deleted some of the session's files, and marking
// those as failures would file an audit row saying Turing could not delete a
// file it just deleted — a false record of the user's own withdrawal, and a
// retry sent looking for something that is already gone.
//
// The statement names vault_artifacts and only vault_artifacts: sandbox rows
// have their own cleaner, their own policy column and their own audit action,
// and an id from that manifest matches nothing here.
func (r *Repository) MarkSessionVaultArtifactsDeleteFailed(ctx context.Context, sessionID string, artifactIDs []string, errorCode string) error {
	if len(artifactIDs) == 0 {
		return nil
	}
	payloadBytes, err := json.Marshal(map[string]string{
		"state":     VaultArtifactStateDeleteFailed,
		"errorCode": errorCode,
	})
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, artifactID := range artifactIDs {
		result, err := tx.ExecContext(ctx, `
			UPDATE vault_artifacts
			SET state = ?
			WHERE id = ? AND session_id = ?
		`, VaultArtifactStateDeleteFailed, artifactID, sessionID)
		if err != nil {
			return err
		}
		marked, err := result.RowsAffected()
		if err != nil {
			return err
		}
		// The audit row lands only for a row this statement actually marked, in
		// the same transaction as the mark: an id naming nothing in this
		// manifest is not a vault failure and does not get recorded as one.
		if marked != 1 {
			continue
		}
		if err := recordAuditTx(ctx, tx, "", "system", "", vaultArtifactCleanupFailedAction, artifactID, string(payloadBytes)); err != nil {
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
// rows for exactly the files that were actually removed. A failure stops the
// pass: the rows whose files are already gone leave the manifest first, and
// only the rows still naming a file — the one that failed and the ones this
// pass never reached — are marked delete_failed, with one redacted audit row
// each. A withdrawal that could not finish stays visible and retryable, and no
// audit row claims a failure for a file that was deleted.
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
	for index, artifact := range artifacts {
		errorCode, err := removeVaultArtifactFile(ctx, vault, artifact)
		if err == nil {
			removed = append(removed, artifact.ArtifactID)
			continue
		}
		// Order is the point. The rows already removed leave the manifest
		// before anything is filed as a failure, so the failure report names
		// only files that are still in the user's vault.
		failure := errors.Join(err, r.DeleteVaultArtifacts(ctx, removed))
		return 0, errors.Join(failure, r.MarkSessionVaultArtifactsDeleteFailed(
			ctx, sessionID, vaultArtifactIDs(artifacts[index:]), errorCode))
	}
	if err := r.DeleteVaultArtifacts(ctx, removed); err != nil {
		return 0, err
	}
	return len(removed), nil
}

// removeVaultArtifactFile deletes one tracked file and names the failure in the
// vocabulary the audit row records. The path is re-checked against the inbox
// gate first: a manifest row that has been tampered with is refused here as a
// typed error rather than reaching the primitive's confinement check, and a
// tampered row can never be turned into a way to delete a belief either way.
func removeVaultArtifactFile(ctx context.Context, vault *memoryfiles.Vault, artifact VaultArtifact) (string, error) {
	if _, err := validateVaultInboxPath(artifact.VaultPath); err != nil {
		return "vault_path_scope", err
	}
	if err := vault.RemoveInboxNote(ctx, artifact.VaultPath); err != nil {
		return "vault_remove_failed", err
	}
	return "", nil
}

func vaultArtifactIDs(artifacts []VaultArtifact) []string {
	artifactIDs := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifactIDs = append(artifactIDs, artifact.ArtifactID)
	}
	return artifactIDs
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
