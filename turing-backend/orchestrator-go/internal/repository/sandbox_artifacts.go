package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"path"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

// Sandbox artifact states mirror the CHECK constraint on sandbox_artifacts.
// "writing" is a durable reservation taken before any bytes reach the file
// system, so a crash between reservation and finalization leaves evidence that
// a file may exist rather than silence.
const (
	SandboxArtifactStateWriting      = "writing"
	SandboxArtifactStateReady        = "ready"
	SandboxArtifactStateDeleteFailed = "delete_failed"
)

// Sandbox artifact policies decide what session deletion is allowed to do with
// a file. Everything a session writes through the provenance path is owned and
// deletable; a pre-existing sandbox-root file a session merely touched is
// retained, because the session never created it and deleting it would destroy
// data the user did not ask to lose.
const (
	SandboxArtifactPolicyDeleteOnSessionDelete = "delete_on_session_delete"
	SandboxArtifactPolicyRetainLegacyUnowned   = "retain_legacy_unowned"
)

var (
	// ErrSandboxArtifactUnowned reports a reservation or finalization whose
	// session/run pair does not own the artifact it names.
	ErrSandboxArtifactUnowned = errors.New("sandbox artifact is not owned by this session run")
	// ErrSandboxArtifactPathScope reports a physical path that is neither the
	// caller's own run-scoped location nor a legitimate legacy root path.
	ErrSandboxArtifactPathScope = errors.New("sandbox artifact path is outside the run scope")
	// ErrSandboxArtifactGenerationStale reports a capability minted against a
	// deletion generation the session has since moved past.
	ErrSandboxArtifactGenerationStale = errors.New("sandbox artifact deletion generation is stale")
	// ErrSandboxArtifactNotFound reports an artifact id with no manifest row.
	ErrSandboxArtifactNotFound = errors.New("sandbox artifact not found")
	// ErrSandboxArtifactRetained reports an attempt to delete an artifact whose
	// policy says the file outlives the session.
	ErrSandboxArtifactRetained = errors.New("sandbox artifact is retained")
)

// SandboxArtifact is one manifest row: the orchestrator's record that a
// specific session run is responsible for a specific file in the sandbox.
type SandboxArtifact struct {
	ArtifactID         string
	SessionID          string
	RunID              string
	LogicalPathHash    string
	PhysicalPath       string
	State              string
	Policy             string
	DeletionGeneration int64
	CreatedAt          string
	FinalizedAt        string
}

// ReserveSandboxArtifactInput is the server-verified description of a write
// that is about to happen. Every field is checked against durable state before
// a row exists, so the manifest cannot be widened by whoever is asking.
type ReserveSandboxArtifactInput struct {
	SessionID          string
	RunID              string
	LogicalPath        string
	PhysicalPath       string
	DeletionGeneration int64
}

// FinalizeSandboxArtifactInput identifies a reservation and the run entitled to
// close it. LogicalPath, when set, is checked inside the same transaction as
// the update, so a capability for one file cannot close another file's
// reservation through a gap between reading and writing.
type FinalizeSandboxArtifactInput struct {
	ArtifactID  string
	SessionID   string
	RunID       string
	LogicalPath string
}

// sessionsPathRoot is the server-managed subtree holding every session's
// artifacts. Nothing outside it belongs to a session.
const sessionsPathRoot = "sessions"

// OwnedSandboxPath is the single definition of where a session run's files
// live. Both the orchestrator and mcp-files derive the physical path from it,
// so a reservation and the write it authorises cannot disagree about what is
// being recorded.
func OwnedSandboxPath(sessionID string, runID string, logicalPath string) string {
	return path.Join("sessions", sessionID, "runs", runID, "files", path.Clean("/" + logicalPath)[1:])
}

// SessionOwnedSandboxPrefix is the subtree that belongs to one session. Files
// under it are deletable with the session; anything outside it predates the
// session's ownership and is retained.
func SessionOwnedSandboxPrefix(sessionID string) string {
	return path.Join(sessionsPathRoot, sessionID) + "/"
}

// SessionWithdrawalState is a session's answer to "may work still happen here,
// and against which withdrawal generation?". Both halves travel together
// because a caller that reads them separately can be told the session is active
// and then act on a generation from after a withdrawal started.
type SessionWithdrawalState struct {
	Active             bool
	DeletionGeneration int64
}

// SessionWithdrawalState reports whether a session is still accepting work.
// It is the single server-side answer that capability issuance, artifact
// reservation and the post-write check all ask, so none of them can disagree
// about whether a session is being withdrawn.
func (r *Repository) SessionWithdrawalState(ctx context.Context, sessionID string) (SessionWithdrawalState, error) {
	var state SessionWithdrawalState
	var deletionState string
	err := r.db.QueryRowContext(ctx, `
		SELECT s.deletion_state, COALESCE(
			(SELECT lifecycle_version FROM session_deletions WHERE session_id = s.id),
			0
		)
		FROM sessions s
		WHERE s.id = ?
	`, sessionID).Scan(&deletionState, &state.DeletionGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionWithdrawalState{}, ErrSessionNotFound
	}
	if err != nil {
		return SessionWithdrawalState{}, err
	}
	state.Active = deletionState == "active"
	return state, nil
}

// SessionDeletionGeneration reports which withdrawal generation a session is
// currently in. A session that has never been withdrawn is generation 0, and
// each withdrawal receipt raises it, so a capability minted before a withdrawal
// cannot be replayed after one.
func (r *Repository) SessionDeletionGeneration(ctx context.Context, sessionID string) (int64, error) {
	return sessionDeletionGeneration(ctx, r.db, sessionID)
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func sessionDeletionGeneration(ctx context.Context, q rowQuerier, sessionID string) (int64, error) {
	var generation int64
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(
			(SELECT lifecycle_version FROM session_deletions WHERE session_id = s.id),
			0
		)
		FROM sessions s
		WHERE s.id = ?
	`, sessionID).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrSessionNotFound
	}
	if err != nil {
		return 0, err
	}
	return generation, nil
}

// ReserveSandboxArtifact durably claims responsibility for a file before it is
// written.
//
// The reservation is the gate, not the finalization: it is the only point that
// can still refuse, so it verifies session ownership of the run, that the
// session is not being withdrawn, that the capability's deletion generation is
// current, and that the physical path is one of exactly two server-derivable
// values — the caller's own run-scoped location, or the legacy root path the
// logical path already named.
//
// It is idempotent per (session, run, physical path) so a retried tool call
// re-reserves rather than duplicating, and a reservation that has already been
// finalized comes back "ready" instead of being reopened for writing. The
// boolean reports whether THIS call created the row, which is what lets a
// caller undo its own reservation without discarding one an earlier call is
// still relying on.
func (r *Repository) ReserveSandboxArtifact(ctx context.Context, input ReserveSandboxArtifactInput) (SandboxArtifact, bool, error) {
	if input.SessionID == "" || input.RunID == "" || input.PhysicalPath == "" {
		return SandboxArtifact{}, false, errors.New("sandbox artifact reservation requires a session, run and physical path")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return SandboxArtifact{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var deletionState string
	if err := tx.QueryRowContext(ctx, `SELECT deletion_state FROM sessions WHERE id = ?`, input.SessionID).Scan(&deletionState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SandboxArtifact{}, false, ErrSessionNotFound
		}
		return SandboxArtifact{}, false, err
	}
	if deletionState != "active" {
		return SandboxArtifact{}, false, ErrSessionDeleting
	}
	var runSessionID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id FROM agent_runs WHERE id = ?`, input.RunID).Scan(&runSessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SandboxArtifact{}, false, ErrSandboxArtifactUnowned
		}
		return SandboxArtifact{}, false, err
	}
	if runSessionID != input.SessionID {
		return SandboxArtifact{}, false, ErrSandboxArtifactUnowned
	}
	generation, err := sessionDeletionGeneration(ctx, tx, input.SessionID)
	if err != nil {
		return SandboxArtifact{}, false, err
	}
	if generation != input.DeletionGeneration {
		return SandboxArtifact{}, false, ErrSandboxArtifactGenerationStale
	}
	// Classified only once the run's ownership is established, so a caller that
	// names someone else's run is told that rather than being told its path is
	// shaped wrongly for a run it never had.
	policy, err := sandboxArtifactPolicy(ctx, tx, input)
	if err != nil {
		return SandboxArtifact{}, false, err
	}

	existing, err := sandboxArtifactByPath(ctx, tx, input.SessionID, input.RunID, input.PhysicalPath)
	if err != nil && !errors.Is(err, ErrSandboxArtifactNotFound) {
		return SandboxArtifact{}, false, err
	}
	if err == nil {
		if err := tx.Commit(); err != nil {
			return SandboxArtifact{}, false, err
		}
		return existing, false, nil
	}

	artifact := SandboxArtifact{
		ArtifactID:         ids.New("sbxa"),
		SessionID:          input.SessionID,
		RunID:              input.RunID,
		LogicalPathHash:    hashLogicalPath(input.LogicalPath),
		PhysicalPath:       input.PhysicalPath,
		State:              SandboxArtifactStateWriting,
		Policy:             policy,
		DeletionGeneration: generation,
		CreatedAt:          now(),
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sandbox_artifacts (
			id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		artifact.ArtifactID,
		artifact.SessionID,
		artifact.RunID,
		artifact.LogicalPathHash,
		artifact.PhysicalPath,
		artifact.State,
		artifact.Policy,
		artifact.DeletionGeneration,
		artifact.CreatedAt,
	); err != nil {
		return SandboxArtifact{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return SandboxArtifact{}, false, err
	}
	return artifact, true, nil
}

// sandboxArtifactPolicy classifies the write from paths the server can derive
// itself. A caller cannot claim "legacy, so do not delete it" for a file it is
// writing into a session subtree, and cannot name a run-scoped location that
// does not belong to its own session.
//
// A later run updating a file an earlier run of the SAME session wrote is
// ordinary — every user message starts a new run — so the earlier run's
// location is accepted, and the row is owned by the run actually doing the
// write. The file is inside the session's subtree either way, so it stays
// deletable with the session.
func sandboxArtifactPolicy(ctx context.Context, q rowQuerier, input ReserveSandboxArtifactInput) (string, error) {
	if input.PhysicalPath == OwnedSandboxPath(input.SessionID, input.RunID, input.LogicalPath) {
		return SandboxArtifactPolicyDeleteOnSessionDelete, nil
	}
	if siblingRun, ok := siblingRunForOwnedPath(input); ok {
		owns, err := sessionOwnsRun(ctx, q, input.SessionID, siblingRun)
		if err != nil {
			return "", err
		}
		if !owns {
			return "", ErrSandboxArtifactPathScope
		}
		return SandboxArtifactPolicyDeleteOnSessionDelete, nil
	}
	if input.PhysicalPath != path.Clean("/" + input.LogicalPath)[1:] {
		return "", ErrSandboxArtifactPathScope
	}
	if strings.HasPrefix(input.PhysicalPath, sessionsPathRoot+"/") {
		return "", ErrSandboxArtifactPathScope
	}
	return SandboxArtifactPolicyRetainLegacyUnowned, nil
}

// siblingRunForOwnedPath reports the run whose storage a physical path names,
// when that path is this session's own run-scoped location for this logical
// path. It is a pure shape check; whether the run really belongs to the session
// is a separate question, answered against the database.
func siblingRunForOwnedPath(input ReserveSandboxArtifactInput) (string, bool) {
	prefix := path.Join(sessionsPathRoot, input.SessionID, "runs") + "/"
	if !strings.HasPrefix(input.PhysicalPath, prefix) {
		return "", false
	}
	runID, tail, found := strings.Cut(strings.TrimPrefix(input.PhysicalPath, prefix), "/")
	if !found || runID == "" {
		return "", false
	}
	if tail != path.Join("files", path.Clean("/" + input.LogicalPath)[1:]) {
		return "", false
	}
	return runID, true
}

func sessionOwnsRun(ctx context.Context, q rowQuerier, sessionID string, runID string) (bool, error) {
	var owner string
	err := q.QueryRowContext(ctx, `SELECT session_id FROM agent_runs WHERE id = ?`, runID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return owner == sessionID, nil
}

// FinalizeSandboxArtifact records that the bytes reached the file system.
//
// It deliberately does not re-check the session's deletion state: by the time
// it runs the file exists, and a manifest that refused to admit that would hand
// cleanup an incomplete list of what it has to remove.
func (r *Repository) FinalizeSandboxArtifact(ctx context.Context, input FinalizeSandboxArtifactInput) (SandboxArtifact, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return SandboxArtifact{}, err
	}
	defer func() { _ = tx.Rollback() }()

	artifact, err := sandboxArtifactByID(ctx, tx, input.ArtifactID)
	if err != nil {
		return SandboxArtifact{}, err
	}
	if artifact.SessionID != input.SessionID || artifact.RunID != input.RunID {
		return SandboxArtifact{}, ErrSandboxArtifactUnowned
	}
	if input.LogicalPath != "" && !SandboxArtifactPathCoversLogicalPath(input.SessionID, artifact.PhysicalPath, input.LogicalPath) {
		return SandboxArtifact{}, ErrSandboxArtifactPathScope
	}
	if artifact.State == SandboxArtifactStateReady {
		if err := tx.Commit(); err != nil {
			return SandboxArtifact{}, err
		}
		return artifact, nil
	}
	finalizedAt := now()
	if _, err := tx.ExecContext(ctx, `
		UPDATE sandbox_artifacts
		SET state = ?, finalized_at = ?
		WHERE id = ? AND session_id = ? AND run_id = ?
	`, SandboxArtifactStateReady, finalizedAt, input.ArtifactID, input.SessionID, input.RunID); err != nil {
		return SandboxArtifact{}, err
	}
	artifact.State = SandboxArtifactStateReady
	artifact.FinalizedAt = finalizedAt
	if err := tx.Commit(); err != nil {
		return SandboxArtifact{}, err
	}
	return artifact, nil
}

// ReleaseSandboxArtifactReservation withdraws a reservation whose write never
// happened. It refuses to touch anything already finalized, so a failed second
// write cannot erase the manifest row for a file the first write left on disk.
func (r *Repository) ReleaseSandboxArtifactReservation(ctx context.Context, artifactID string, sessionID string, runID string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM sandbox_artifacts
		WHERE id = ? AND session_id = ? AND run_id = ? AND state = ?
	`, artifactID, sessionID, runID, SandboxArtifactStateWriting)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

// SessionSandboxArtifacts lists everything a session is responsible for, with
// the files cleanup must delete before the retained ones it must not touch.
func (r *Repository) SessionSandboxArtifacts(ctx context.Context, sessionID string) ([]SandboxArtifact, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at, COALESCE(finalized_at, '')
		FROM sandbox_artifacts
		WHERE session_id = ?
		ORDER BY CASE policy WHEN ? THEN 0 ELSE 1 END, created_at, id
	`, sessionID, SandboxArtifactPolicyDeleteOnSessionDelete)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var artifacts []SandboxArtifact
	for rows.Next() {
		var artifact SandboxArtifact
		if err := rows.Scan(
			&artifact.ArtifactID,
			&artifact.SessionID,
			&artifact.RunID,
			&artifact.LogicalPathHash,
			&artifact.PhysicalPath,
			&artifact.State,
			&artifact.Policy,
			&artifact.DeletionGeneration,
			&artifact.CreatedAt,
			&artifact.FinalizedAt,
		); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

// CountRetainedLegacySandboxArtifacts reports how many distinct pre-existing
// files a session touched but does not own, which is what a withdrawal receipt
// tells the user was deliberately left behind. Files are counted, not rows:
// several runs touching one file is still one file that survives.
func (r *Repository) CountRetainedLegacySandboxArtifacts(ctx context.Context, sessionID string) (int, error) {
	var retained int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT physical_path)
		FROM sandbox_artifacts
		WHERE session_id = ? AND policy = ?
	`, sessionID, SandboxArtifactPolicyRetainLegacyUnowned).Scan(&retained)
	if err != nil {
		return 0, err
	}
	return retained, nil
}

// DeleteSandboxArtifact removes the manifest row once the file itself is gone.
// It refuses retained legacy artifacts outright, which is what keeps a session
// withdrawal from deleting files the session never created.
func (r *Repository) DeleteSandboxArtifact(ctx context.Context, artifactID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	artifact, err := sandboxArtifactByID(ctx, tx, artifactID)
	if errors.Is(err, ErrSandboxArtifactNotFound) {
		// Cleanup is retried after partial failures, so a row that is already
		// gone is the outcome the caller wanted.
		return nil
	}
	if err != nil {
		return err
	}
	if artifact.Policy == SandboxArtifactPolicyRetainLegacyUnowned {
		return ErrSandboxArtifactRetained
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sandbox_artifacts WHERE id = ?`, artifactID); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkSandboxArtifactDeleteFailed records that cleanup reached the file system
// and could not remove a file, so the withdrawal receipt can stay retryable
// instead of reporting a completion that left bytes behind.
func (r *Repository) MarkSandboxArtifactDeleteFailed(ctx context.Context, artifactID string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE sandbox_artifacts SET state = ? WHERE id = ?
	`, SandboxArtifactStateDeleteFailed, artifactID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrSandboxArtifactNotFound
	}
	return nil
}

// SandboxArtifactPathCoversLogicalPath reports whether a recorded physical path
// is a legitimate location for one logical path in one session: any of that
// session's run-scoped locations, or the legacy root path the logical path
// already names. The run is not pinned because a later run may write the file
// an earlier run of the same session created.
func SandboxArtifactPathCoversLogicalPath(sessionID string, physicalPath string, logicalPath string) bool {
	logical := path.Clean("/" + logicalPath)[1:]
	if physicalPath == logical {
		return true
	}
	_, ok := siblingRunForOwnedPath(ReserveSandboxArtifactInput{
		SessionID:    sessionID,
		PhysicalPath: physicalPath,
		LogicalPath:  logical,
	})
	return ok
}

func sandboxArtifactByID(ctx context.Context, q rowQuerier, artifactID string) (SandboxArtifact, error) {
	return scanSandboxArtifact(q.QueryRowContext(ctx, `
		SELECT id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at, COALESCE(finalized_at, '')
		FROM sandbox_artifacts
		WHERE id = ?
	`, artifactID))
}

func sandboxArtifactByPath(ctx context.Context, q rowQuerier, sessionID string, runID string, physicalPath string) (SandboxArtifact, error) {
	return scanSandboxArtifact(q.QueryRowContext(ctx, `
		SELECT id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at, COALESCE(finalized_at, '')
		FROM sandbox_artifacts
		WHERE session_id = ? AND run_id = ? AND physical_path = ?
	`, sessionID, runID, physicalPath))
}

func scanSandboxArtifact(row *sql.Row) (SandboxArtifact, error) {
	var artifact SandboxArtifact
	err := row.Scan(
		&artifact.ArtifactID,
		&artifact.SessionID,
		&artifact.RunID,
		&artifact.LogicalPathHash,
		&artifact.PhysicalPath,
		&artifact.State,
		&artifact.Policy,
		&artifact.DeletionGeneration,
		&artifact.CreatedAt,
		&artifact.FinalizedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SandboxArtifact{}, ErrSandboxArtifactNotFound
	}
	if err != nil {
		return SandboxArtifact{}, err
	}
	return artifact, nil
}

// hashLogicalPath keeps the manifest content-free: cleanup needs to know that a
// file exists and where, not what the user called it.
func hashLogicalPath(logicalPath string) string {
	sum := sha256.Sum256([]byte(logicalPath))
	return "sha256:" + hex.EncodeToString(sum[:])
}
