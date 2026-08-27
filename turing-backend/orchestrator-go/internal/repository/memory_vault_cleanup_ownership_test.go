package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// sealInboxDirectory makes the inbox unwritable, which is what a removal that
// cannot finish looks like from the manifest's side: the file is still there,
// nothing was detached, and the vault will not let go of it.
//
// It is a real refusal rather than an injected one, so what these tests hold is
// the whole path the cleaner actually runs.
func sealInboxDirectory(t *testing.T, vault *memoryfiles.Vault) func() {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions, so the vault cannot be made to refuse")
	}
	inbox := filepath.Join(vault.Root(), memoryfiles.InboxDirName)
	if err := os.Chmod(inbox, 0o500); err != nil {
		t.Fatalf("seal the inbox: %v", err)
	}
	unsealed := false
	unseal := func() {
		if unsealed {
			return
		}
		unsealed = true
		if err := os.Chmod(inbox, 0o700); err != nil {
			t.Fatalf("unseal the inbox: %v", err)
		}
	}
	t.Cleanup(unseal)
	return unseal
}

// A cleanup that cannot remove a file keeps the row that names it, keeps the
// file where the row says it is, and stays retryable. The row is the only thing
// that can ever find the file again; dropping it is how a withdrawal reports
// completion over a note still sitting in the user's vault.
func TestVaultCleanupKeepsTheRowAndTheFileWhenTheRemovalCannotFinish(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	artifact := onlyVaultArtifact(t, repo, sessionID)
	before := readVaultNote(t, vault, candidate.InboxPath)

	unseal := sealInboxDirectory(t, vault)
	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err == nil {
		t.Fatal("cleanup reported success over a note the vault would not release")
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifact state = %q, want the row kept and marked so the withdrawal retries", state)
	}
	// Where the row says it is, byte for byte. A retry can only prove
	// ownership of a file it can find under the path the manifest names.
	if got := readVaultNote(t, vault, candidate.InboxPath); got != before {
		t.Fatalf("the note under the manifest's path = %q, want the bytes the row is bound to", got)
	}

	unseal()
	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("the retry could not finish the withdrawal: %v", err)
	}
	if removed != 1 {
		t.Fatalf("the retry removed %d file(s), want the note the first pass could not", removed)
	}
	if got := vaultArtifactRows(t, repo, sessionID); got != 0 {
		t.Fatalf("manifest rows = %d, want a drained manifest", got)
	}
	if entries := inboxEntries(t, vault); len(entries) != 0 {
		t.Fatalf("inbox = %v, want nothing left behind", entries)
	}
}

// The other half of the same rule, and the one that turns a kept row into an
// orphan if it is missed.
//
// A cleanup that failed may have left the bytes off their name — under the
// reserved name a detach put them under, which the vault walk steps over. The
// path the row names then holds nothing, and reconcile's own tidying, which
// releases reservations for paths the inbox no longer has, would drop the last
// record of a file that is still in the user's vault.
//
// So absence alone does not release a row whose cleanup is known to have
// failed. That row belongs to the cleaner, which re-verifies before it removes
// anything and drains the row when the file is really gone.
func TestReconcileKeepsAReservationWhoseCleanupFailed(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	artifact := onlyVaultArtifact(t, repo, sessionID)

	// What a failed removal can leave behind: the bytes under the reserved
	// private name inside the same directory, and nothing under the name the
	// manifest records.
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	residue := filepath.Join(vault.Root(), memoryfiles.InboxDirName, ".turing-memory-0123456789abcdef01234567")
	if err := os.Rename(full, residue); err != nil {
		t.Fatalf("stage the residue a failed removal leaves: %v", err)
	}
	if err := repo.MarkSessionVaultArtifactsDeleteFailed(
		ctx(), sessionID, []string{artifact.ArtifactID}, "vault_remove_failed",
	); err != nil {
		t.Fatalf("mark the failed cleanup: %v", err)
	}
	// The proposal itself is decided and gone; only the manifest row is left,
	// which is exactly the state reconcile is allowed to tidy.
	if _, err := repo.db.ExecContext(ctx(),
		`DELETE FROM memory_candidates WHERE id = ?`, candidate.CandidateID,
	); err != nil {
		t.Fatalf("consume the candidate row: %v", err)
	}
	repo.memoryReconcileScanAnchor = now()

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.ReservationsCleared != 0 {
		t.Fatalf("reconcile released %d row(s) whose cleanup had failed", report.ReservationsCleared)
	}
	if got := vaultArtifactRows(t, repo, sessionID); got != 1 {
		t.Fatalf("manifest rows = %d, want the failed cleanup still tracked", got)
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifact state = %q, want the failure still recorded", state)
	}
}

// Turing's own tidying after an accepted profile edit is a removal like any
// other, and it can fail like any other. The proposal's row is consumed either
// way — an applied edit is not one anybody may decide again — so the manifest
// row is the only record left that says a file may still be there.
//
// It is kept, and it is marked, because "a removal was attempted here and did
// not remove the file" is what stops reconcile releasing it later on the
// strength of a path that holds nothing.
func TestProfileApplyCleanupFailureKeepsAndMarksTheReservation(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	const profile = "# Profile\n\nWritten already.\n"
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, profile)
	candidate := profileEditCandidate(t, repo, sessionID)
	artifact := onlyVaultArtifact(t, repo, sessionID)

	sealInboxDirectory(t, vault)
	result, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash(profile),
		Content:             "# Profile\n\nThe user keeps bees.\n",
	})
	if err != nil {
		t.Fatalf("ApplyMemoryProfileCandidate: %v", err)
	}
	if !result.CleanupPending {
		t.Fatal("an apply whose tidying failed reported the proposal cleaned up")
	}
	if got := vaultArtifactRows(t, repo, sessionID); got != 1 {
		t.Fatalf("manifest rows = %d, want the file the tidying could not remove still tracked", got)
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifact state = %q, want the failed tidying recorded on the row", state)
	}
	// And the audit trail says which artifact, in the vocabulary the session
	// cleaner already uses, without naming a path or a word of the file.
	var payload string
	if err := repo.db.QueryRowContext(ctx(),
		`SELECT payload_json FROM audit_logs WHERE target = ?`, artifact.ArtifactID,
	).Scan(&payload); err != nil {
		t.Fatalf("read the audit row for the kept artifact: %v", err)
	}
	if !strings.Contains(payload, VaultArtifactStateDeleteFailed) {
		t.Fatalf("audit payload = %q, want the state the row moved to", payload)
	}
	if strings.Contains(payload, candidate.InboxPath) {
		t.Fatalf("audit payload names the path inside the user's vault: %q", payload)
	}
}
