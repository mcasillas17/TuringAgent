package repository

import (
	"context"
	"errors"
	"fmt"
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

// The bytes a failed removal left under a reserved name are still the session's
// to withdraw, and the record that names them is the only thing that can find
// them. A retry that removes the entry under the visible name and retires the
// row would leave the note in the user's vault under a name no listing shows —
// a withdrawal reporting completion over a file that is still there.
func TestVaultCleanupTakesBytesLeftUnderAReservedName(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	artifact := onlyVaultArtifact(t, repo, sessionID)

	// What a failed unlink leaves: the same bytes under the reserved private
	// name, with nothing under the name the manifest records.
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

	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	if removed != 1 {
		t.Fatalf("the cleaner removed %d file(s), want the note it could reach under either name", removed)
	}
	if entries := inboxEntries(t, vault); len(entries) != 0 {
		t.Fatalf("inbox = %v, want the withdrawn note gone under every name", entries)
	}
	if got := vaultArtifactRows(t, repo, sessionID); got != 0 {
		t.Fatalf("manifest rows = %d, want a drained manifest", got)
	}
}

// And a reserved entry the session cannot name is not its business. Somebody
// else's half-written file stays exactly where it is, and the withdrawal still
// finishes, because the note this row was about really is gone.
func TestVaultCleanupLeavesAReservedEntryItCannotName(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")

	const theirs = "half a note another writer is staging"
	stranger := filepath.Join(vault.Root(), memoryfiles.InboxDirName, ".turing-memory-fedcba9876543210fedcba98")
	if err := os.WriteFile(stranger, []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	if removed != 1 {
		t.Fatalf("the cleaner removed %d file(s), want the session's own note", removed)
	}
	if candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("the withdrawn note is still in the inbox")
	}
	held, err := os.ReadFile(stranger)
	if err != nil || string(held) != theirs {
		t.Fatalf("the reserved entry the session could not name was disturbed: %q, %v", held, err)
	}
}

// A decision retires the manifest row for the file it removed, and a removal
// that failed once may have left the same bytes under a reserved name. So the
// decision that finally succeeds takes those with it too — otherwise the row
// goes, the visible file goes, and the copy nothing can name stays in the
// user's vault forever.
func TestRejectionTakesResidueOfTheSameBytesWithIt(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	content := readVaultNote(t, vault, candidate.InboxPath)

	// What an earlier failed attempt leaves behind: a second link to the same
	// bytes under the reserved private name.
	residue := filepath.Join(vault.Root(), memoryfiles.InboxDirName, ".turing-memory-89abcdef0123456789abcdef")
	if err := os.WriteFile(residue, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = sessionID

	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID:           candidate.CandidateID,
		ExpectedCandidateHash: memoryfiles.ContentHash(content),
	}); err != nil {
		t.Fatalf("RejectMemoryCandidate: %v", err)
	}
	if entries := inboxEntries(t, vault); len(entries) != 0 {
		t.Fatalf("inbox = %v, want the rejected proposal gone under every name", entries)
	}
}

// The bytes a decision acts on are not always the bytes the row was created
// with: a user may edit a proposal in their vault before accepting it, which is
// what a vault is for. When the tidying that follows cannot remove the file,
// the row it keeps has to name what is actually there — a row still bound to
// the words Turing first proposed can never prove ownership again, so the
// withdrawal that comes later refuses forever.
func TestProfileApplyCleanupFailureBindsTheRowToTheBytesItActedOn(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	const profile = "# Profile\n\nWritten already.\n"
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, profile)
	candidate := profileEditCandidate(t, repo, sessionID)
	artifact := onlyVaultArtifact(t, repo, sessionID)

	// The user edits the proposal in Obsidian before accepting it.
	edited := readVaultNote(t, vault, candidate.InboxPath) + "\nAnd keeps chickens.\n"
	writeVaultNote(t, vault, candidate.InboxPath, edited)

	unseal := sealInboxDirectory(t, vault)
	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash(profile),
		Content:             "# Profile\n\nThe user keeps bees.\n",
	}); err != nil {
		t.Fatalf("ApplyMemoryProfileCandidate: %v", err)
	}
	bound, ok := vaultArtifactHash(t, repo, artifact.ArtifactID)
	if !ok || bound != memoryfiles.ContentHash(edited) {
		t.Fatalf("artifact binding = %q (present=%v), want the bytes the apply acted on", bound, ok)
	}

	// And that is what makes the withdrawal able to finish at all.
	unseal()
	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	if removed != 1 {
		t.Fatalf("the cleaner removed %d file(s), want the applied proposal", removed)
	}
	if entries := inboxEntries(t, vault); len(entries) != 0 {
		t.Fatalf("inbox = %v, want the applied proposal gone", entries)
	}
}

// A decision whose vault call fails before any row is written still has to
// leave a record when that failure left the bytes under a name only the vault
// can spell. Without one the manifest row stays `ready`, the path it names
// holds nothing, and reconcile releases it — the last thing that could ever
// find those bytes.
func TestAVaultFailureThatLeftACopyIsRecordedOnTheRow(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	artifact := onlyVaultArtifact(t, repo, sessionID)

	left := fmt.Errorf("the removal did not finish: %w", memoryfiles.ErrVaultResidue)
	repo.recordUnremovedVaultFile(ctx(), candidate, candidate.ContentHash, left)

	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifact state = %q, want the row kept for the copy that was left", state)
	}

	// A failure that moved nothing is not that, and must not mark the row:
	// every mark is an audit row and a row reconcile will stop tidying.
	other, second := seedVaultDeletableSession(t, repo, "cats", "The user has two cats.")
	untouched := onlyVaultArtifact(t, repo, other)
	repo.recordUnremovedVaultFile(ctx(), second, second.ContentHash, errors.New("the request ended"))
	if state := vaultArtifactState(t, repo, untouched.ArtifactID); state != VaultArtifactStateReady {
		t.Fatalf("artifact state = %q, want a row nothing happened to left alone", state)
	}
}

// A proposal whose frontmatter will not parse is rejected through the door that
// binds to the bytes the pre-check hashed rather than to a note it could read.
// Those bytes are still bytes: a copy an earlier attempt left behind is one
// this rejection can name, and must take with it.
func TestHashlessRejectionTakesResidueOfTheBytesItRead(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	_ = sessionID

	// The user breaks the proposal's frontmatter in their editor.
	const broken = "---\nnot: [valid\n---\n\nThe user keeps bees.\n"
	writeVaultNote(t, vault, candidate.InboxPath, broken)
	residue := filepath.Join(vault.Root(), memoryfiles.InboxDirName, ".turing-memory-1111111111111111aaaaaaaa")
	if err := os.WriteFile(residue, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID: candidate.CandidateID,
	}); err != nil {
		t.Fatalf("RejectMemoryCandidate: %v", err)
	}
	if entries := inboxEntries(t, vault); len(entries) != 0 {
		t.Fatalf("inbox = %v, want the rejected proposal gone under every name", entries)
	}
}

// A sweep that cannot finish says nothing about a row that never named any
// bytes. Those rows are the crash artifact whose write never landed, and they
// have to be able to drain or every such crash strands a withdrawal forever.
func TestASweepFailureDoesNotStrandARowThatNamesNoBytes(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file regardless of mode")
	}
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	reservation, err := repo.ReserveVaultArtifact(ctx(), ReserveVaultArtifactInput{
		SessionID: sessionID, VaultPath: "inbox/01M0000000000000000000000X-never-written.md",
	})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}

	sealed := filepath.Join(vault.Root(), memoryfiles.InboxDirName, ".turing-memory-2222222222222222bbbbbbbb")
	if err := os.WriteFile(sealed, []byte("something nobody can read"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sealed, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sealed, 0o600) })

	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	if removed != 1 {
		t.Fatalf("the cleaner drained %d row(s), want the reservation whose write never landed", removed)
	}
	if got := vaultArtifactRows(t, repo, sessionID); got != 0 {
		t.Fatalf("manifest rows = %d, want the unwritten reservation drained", got)
	}
	_ = reservation
}

// When the sweep after a hashless rejection cannot finish, the row it keeps has
// to name the bytes that decision was actually bound to. Left on the hash the
// row was created with, the next withdrawal sweeps for words nobody has and
// retires the row over the copy it was kept for.
func TestHashlessRejectionKeepsARowBoundToTheBytesItRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file regardless of mode")
	}
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	artifact := onlyVaultArtifact(t, repo, sessionID)

	const broken = "---\nnot: [valid\n---\n\nThe user keeps bees.\n"
	writeVaultNote(t, vault, candidate.InboxPath, broken)
	// A reserved entry nobody can read stops the sweep, which is what makes the
	// rejection keep its row.
	sealed := filepath.Join(vault.Root(), memoryfiles.InboxDirName, ".turing-memory-3333333333333333cccccccc")
	if err := os.WriteFile(sealed, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sealed, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sealed, 0o600) })

	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID: candidate.CandidateID,
	}); err != nil {
		t.Fatalf("RejectMemoryCandidate: %v", err)
	}
	bound, ok := vaultArtifactHash(t, repo, artifact.ArtifactID)
	if !ok || bound != memoryfiles.ContentHash(broken) {
		t.Fatalf("artifact binding = %q (present=%v), want the bytes the rejection read", bound, ok)
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifact state = %q, want the row kept for the copy the sweep could not clear", state)
	}
}

// A row whose visible path now holds somebody else's file cannot be drained —
// that is the trade this manifest already makes — but the copy of Turing's own
// note that a failed removal left under a reserved name is still Turing's to
// take. Sweeping only for the rows that drained would leave those bytes in the
// vault for as long as the user's file sits at that path, which may be forever.
func TestVaultCleanupSweepsResidueEvenWhenThePathIsContested(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	artifact := onlyVaultArtifact(t, repo, sessionID)
	content := readVaultNote(t, vault, candidate.InboxPath)

	// What a failed removal into a taken name leaves: Turing's bytes under the
	// reserved name, somebody else's file under the visible one.
	residue := filepath.Join(vault.Root(), memoryfiles.InboxDirName, ".turing-memory-4444444444444444dddddddd")
	if err := os.WriteFile(residue, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	const theirs = "---\ntitle: my own note\n---\n\nSomething I wrote myself.\n"
	writeVaultNote(t, vault, candidate.InboxPath, theirs)

	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err == nil {
		t.Fatal("cleanup reported success over a file it could not prove it wrote")
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifact state = %q, want the row kept for the contested path", state)
	}
	if got := readVaultNote(t, vault, candidate.InboxPath); got != theirs {
		t.Fatalf("the user's own file was disturbed: %q", got)
	}
	if _, err := os.Lstat(residue); !os.IsNotExist(err) {
		t.Fatalf("the reserved copy of Turing's own note is still there: %v", err)
	}
}

// The window a mark cannot close: a process that dies between the removal that
// left a copy and the row it would have marked leaves an ordinary-looking
// `ready` row whose path holds nothing. The state says nothing, so the vault is
// asked instead — a reserved entry holding exactly the bytes this row names is
// the row's own file, and releasing the row would take the last thing that
// could find it.
func TestReconcileKeepsAReadyRowWhoseBytesAreUnderAReservedName(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	artifact := onlyVaultArtifact(t, repo, sessionID)

	// Exactly what a crash after a failed removal leaves: bytes under the
	// reserved name, nothing under the manifest's path, and a row nobody got
	// to mark.
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	residue := filepath.Join(vault.Root(), memoryfiles.InboxDirName, ".turing-memory-5555555555555555eeeeeeee")
	if err := os.Rename(full, residue); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx(),
		`DELETE FROM memory_candidates WHERE id = ?`, candidate.CandidateID,
	); err != nil {
		t.Fatal(err)
	}
	repo.memoryReconcileScanAnchor = now()

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.ReservationsCleared != 0 {
		t.Fatalf("reconcile released %d row(s) whose bytes are still in the vault", report.ReservationsCleared)
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateReady {
		t.Fatalf("artifact state = %q, want the row kept as it was", state)
	}

	// And the withdrawal still finishes, because the cleaner can name those
	// bytes and take them.
	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	if removed != 1 {
		t.Fatalf("the cleaner drained %d row(s), want the one naming the reserved copy", removed)
	}
	if entries := inboxEntries(t, vault); len(entries) != 0 {
		t.Fatalf("inbox = %v, want nothing left behind", entries)
	}
}

// A request that ends is one of the ways a removal leaves a copy, and the
// record of it must not be stopped by the same cancellation.
func TestACancelledDecisionStillRecordsTheCopyItLeft(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	artifact := onlyVaultArtifact(t, repo, sessionID)

	ended, cancel := context.WithCancel(context.Background())
	cancel()
	left := fmt.Errorf("the request ended: %w", errors.Join(context.Canceled, memoryfiles.ErrVaultResidue))
	repo.recordUnremovedVaultFile(ended, candidate, candidate.ContentHash, left)

	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifact state = %q, want the row kept for the copy the cancellation left", state)
	}
}
