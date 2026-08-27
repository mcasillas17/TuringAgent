package repository

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// A manifest row is a licence to delete a file in somebody's vault. Until this
// round the only thing it named was a path, and a path is not an owner.
//
// The user can move a proposal out of the inbox — into a folder of their own,
// or into Obsidian's trash — and then save something of theirs under the name
// it used to have. Or they can open the proposal and rewrite it in place, which
// keeps the inode and changes every word. In both cases the row still named a
// path, and the cleaner still unlinked whatever was under it: a file Turing
// never wrote, deleted on the strength of a coincidence of names, with the row
// removed afterwards so nothing recorded that it had happened.
//
// So the row now carries the bytes it is entitled to remove, and the removal
// refuses anything else. Refusing is not the same as giving up: the file stays,
// the row stays, the withdrawal stays retryable, and a user who puts their own
// file somewhere else gets the proposal removed on the next pass.

// vaultArtifactHash reads the ownership binding one manifest row carries, and
// whether it carries one at all.
func vaultArtifactHash(t *testing.T, repo *Repository, artifactID string) (string, bool) {
	t.Helper()
	var hash *string
	if err := repo.db.QueryRowContext(ctx(),
		`SELECT expected_content_hash FROM vault_artifacts WHERE id = ?`, artifactID,
	).Scan(&hash); err != nil {
		t.Fatalf("read the artifact binding: %v", err)
	}
	if hash == nil {
		return "", false
	}
	return *hash, true
}

func vaultArtifactState(t *testing.T, repo *Repository, artifactID string) string {
	t.Helper()
	var state string
	if err := repo.db.QueryRowContext(ctx(),
		`SELECT state FROM vault_artifacts WHERE id = ?`, artifactID,
	).Scan(&state); err != nil {
		t.Fatalf("read the artifact state: %v", err)
	}
	return state
}

func onlyVaultArtifact(t *testing.T, repo *Repository, sessionID string) VaultArtifact {
	t.Helper()
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifact rows = %d, want the candidate's file tracked once", len(artifacts))
	}
	return artifacts[0]
}

// The creation flow binds the row to the exact bytes it wrote, and does it in
// the finalization rather than the reservation: the reservation is taken before
// the write, so a hash there would be a guess about bytes that do not exist.
func TestCreatingACandidateBindsItsManifestRowToTheBytesItWrote(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")

	artifact := onlyVaultArtifact(t, repo, sessionID)
	if artifact.State != VaultArtifactStateReady {
		t.Fatalf("artifact state = %q, want a finalized row", artifact.State)
	}
	bound, ok := vaultArtifactHash(t, repo, artifact.ArtifactID)
	if !ok {
		t.Fatal("a finalized manifest row named no bytes")
	}
	raw, err := os.ReadFile(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath)))
	if err != nil {
		t.Fatalf("read the written candidate: %v", err)
	}
	// The whole file exactly as it stands, not the body and not the row's idea
	// of it. What the cleaner will compare against is the bytes on disk.
	if bound != memoryfiles.ContentHash(string(raw)) {
		t.Fatalf("manifest binding = %q, want the hash of the raw file", bound)
	}
	if artifact.ExpectedContentHash != bound {
		t.Fatalf("VaultArtifact.ExpectedContentHash = %q, want %q", artifact.ExpectedContentHash, bound)
	}
}

// The case this whole binding exists for. The user moved the proposal away and
// put a file of their own under the name it had. The cleaner must not touch it,
// and must keep the row so the withdrawal stays honest about being unfinished.
func TestVaultCleanupRefusesAFileTheUserPutAtTheCandidatesOldPath(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	artifact := onlyVaultArtifact(t, repo, sessionID)

	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	moved := filepath.Join(vault.Root(), memoryfiles.InboxDirName, "the-users-own-copy.md")
	if err := os.Rename(full, moved); err != nil {
		t.Fatalf("move the candidate away: %v", err)
	}
	const theirs = "---\ntitle: my own note\n---\n\nSomething I wrote myself.\n"
	if err := os.WriteFile(full, []byte(theirs), 0o600); err != nil {
		t.Fatalf("write the user's own file: %v", err)
	}

	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err == nil {
		t.Fatal("cleanup reported success over a file it could not prove it wrote")
	}
	held, err := os.ReadFile(full)
	if err != nil || string(held) != theirs {
		t.Fatalf("the user's own file was disturbed: %q, %v", held, err)
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifact state = %q, want the row kept and marked so the withdrawal retries", state)
	}
	// And the retry is not a dead end: the row is still in the worklist, so
	// once the user's file is out of the way the proposal is removed.
	pending, err := repo.PendingSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PendingSessionVaultArtifacts: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending rows = %d, want the refused row retried", len(pending))
	}
	if err := os.Remove(full); err != nil {
		t.Fatalf("clear the user's file: %v", err)
	}
	if err := os.Rename(moved, full); err != nil {
		t.Fatalf("put the candidate back: %v", err)
	}
	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err != nil {
		t.Fatalf("cleanup of the restored candidate: %v", err)
	}
	if countRows(t, repo, `SELECT COUNT(*) FROM vault_artifacts WHERE session_id = ?`, sessionID) != 0 {
		t.Fatal("the manifest could not drain once the file was provably Turing's again")
	}
}

// The other half of the binding, and the one an inode alone cannot catch: the
// user opened the proposal in their editor and rewrote it. Same file, same
// inode, different words — and those words are theirs.
func TestVaultCleanupRefusesACandidateTheUserRewroteInPlace(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	artifact := onlyVaultArtifact(t, repo, sessionID)

	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	before, err := os.Stat(full)
	if err != nil {
		t.Fatalf("inspect the candidate: %v", err)
	}
	const rewritten = "---\nid: whatever\n---\n\nActually the user keeps chickens.\n"
	if err := os.WriteFile(full, []byte(rewritten), 0o600); err != nil {
		t.Fatalf("rewrite the candidate in place: %v", err)
	}
	after, err := os.Stat(full)
	if err != nil {
		t.Fatalf("inspect the rewritten candidate: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("the rewrite replaced the file; this test is about the same inode")
	}

	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err == nil {
		t.Fatal("cleanup removed words the user wrote themselves")
	}
	held, readErr := os.ReadFile(full)
	if readErr != nil || string(held) != rewritten {
		t.Fatalf("the user's own words were removed: %q, %v", held, readErr)
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifact state = %q, want the row kept", state)
	}
}

// A reservation whose write never landed. There is no hash and never will be,
// and there is no file either — so the row drains, because a file the user
// asked not to have is a file that is not there.
func TestVaultCleanupDrainsAReservationWhoseWriteNeverLanded(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	artifact, err := repo.ReserveVaultArtifact(ctx(), ReserveVaultArtifactInput{
		SessionID: sessionID,
		VaultPath: "inbox/01M10B8CV5M99GCK4JFF2M3WWB-never-written.md",
	})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}
	if _, ok := vaultArtifactHash(t, repo, artifact.ArtifactID); ok {
		t.Fatal("a reservation taken before the write named bytes that do not exist")
	}

	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err != nil {
		t.Fatalf("cleanup of a reservation with no file: %v", err)
	}
	if countRows(t, repo, `SELECT COUNT(*) FROM vault_artifacts WHERE session_id = ?`, sessionID) != 0 {
		t.Fatal("a reservation naming nothing could not drain")
	}
}

// A reservation whose file *is* there and which nothing ever finalized. The row
// names no bytes, so it proves nothing about the file under the path — it may
// be the write that landed a moment before the crash, or it may be whatever the
// user has put there since. Without a binding the cleaner refuses, and the row
// stays for a pass that can prove ownership.
func TestVaultCleanupRefusesAnUnfinalizedRowWhoseFileIsThere(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	const relPath = "inbox/01M10B8CV5M99GCK4JFF2M3WWB-half-written.md"
	artifact, err := repo.ReserveVaultArtifact(ctx(), ReserveVaultArtifactInput{
		SessionID: sessionID,
		VaultPath: relPath,
	})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}
	full := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
	const present = "---\nid: 01M10B8CV5M99GCK4JFF2M3WWB\n---\n\nsomething\n"
	if err := os.WriteFile(full, []byte(present), 0o600); err != nil {
		t.Fatalf("write the file the crash left: %v", err)
	}

	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err == nil {
		t.Fatal("cleanup removed a file no row could prove it owned")
	}
	if _, readErr := os.ReadFile(full); readErr != nil {
		t.Fatalf("the unprovable file was removed anyway: %v", readErr)
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifact state = %q, want the row kept", state)
	}
}

// The crash-heal. A write that landed and a finalization that did not is the
// one state reconcile can close, and only when the file under the reserved path
// proves it is Turing's: a managed note whose own identity is the identity in
// the name this server minted. The heal is a fresh confined read, and the hash
// is of exactly the bytes it read.
func TestReconcileFinalizesAManagedWriteWhoseBookkeepingNeverLanded(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	note, err := vault.CreateInboxNote(ctx(), memoryfiles.CreateInboxNoteRequest{
		Kind:  memoryfiles.KindBelief,
		Title: "bees",
		Body:  "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateInboxNote: %v", err)
	}
	artifact, err := repo.ReserveVaultArtifact(ctx(), ReserveVaultArtifactInput{
		SessionID: sessionID,
		VaultPath: note.RelPath,
	})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}

	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	bound, ok := vaultArtifactHash(t, repo, artifact.ArtifactID)
	if !ok {
		t.Fatal("reconcile left a landed write with no ownership binding")
	}
	if bound != note.ContentHash {
		t.Fatalf("healed binding = %q, want the hash of the bytes on disk", bound)
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateReady {
		t.Fatalf("healed artifact state = %q, want it finalized", state)
	}
	// And now it can be withdrawn, which is the point of healing it.
	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err != nil {
		t.Fatalf("cleanup of a healed row: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(note.RelPath))); !os.IsNotExist(statErr) {
		t.Fatalf("the healed row's file survived its withdrawal: %v", statErr)
	}
}

// The heal is not a way to adopt somebody else's file. A reservation naming a
// path the user has since filled with something of their own stays unfinalized,
// because nothing about that file says Turing wrote it.
func TestReconcileRefusesToAdoptAFileTheUserWrote(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	const relPath = "inbox/01M10B8CV5M99GCK4JFF2M3WWB-not-ours.md"
	artifact, err := repo.ReserveVaultArtifact(ctx(), ReserveVaultArtifactInput{
		SessionID: sessionID,
		VaultPath: relPath,
	})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}
	full := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
	if err := os.WriteFile(full, []byte("Just a note I made.\n"), 0o600); err != nil {
		t.Fatalf("write the user's file: %v", err)
	}

	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if _, ok := vaultArtifactHash(t, repo, artifact.ArtifactID); ok {
		t.Fatal("reconcile adopted a file the user wrote as one Turing may delete")
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateWriting {
		t.Fatalf("artifact state = %q, want the reservation left unfinalized", state)
	}
}

// What a refused cleanup writes down. The audit trail is read by people who are
// not the user, so it records the class and the artifact identity and nothing
// about the file: not its path, not a word of it, not the hash that would let a
// holder confirm a guess at its contents.
func TestRefusedVaultCleanupAuditsNoPathContentOrHash(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "beekeeping", "The user keeps bees in Oaxaca.")
	artifact := onlyVaultArtifact(t, repo, sessionID)
	bound, _ := vaultArtifactHash(t, repo, artifact.ArtifactID)

	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	if err := os.WriteFile(full, []byte("The user keeps chickens in Puebla.\n"), 0o600); err != nil {
		t.Fatalf("rewrite the candidate: %v", err)
	}
	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err == nil {
		t.Fatal("cleanup reported success over a file it could not prove it wrote")
	}

	rows, err := repo.db.QueryContext(ctx(),
		`SELECT payload_json FROM audit_logs WHERE target = ?`, artifact.ArtifactID)
	if err != nil {
		t.Fatalf("read audit rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatalf("scan audit payload: %v", err)
		}
		seen++
		for _, forbidden := range []string{
			candidate.InboxPath, "beekeeping", "Oaxaca", "chickens", "Puebla", bound,
		} {
			if forbidden != "" && strings.Contains(payload, forbidden) {
				t.Fatalf("the audit row leaked %q: %s", forbidden, payload)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit rows: %v", err)
	}
	if seen != 1 {
		t.Fatalf("audit rows for the refused artifact = %d, want exactly one", seen)
	}
}

// The profile-apply path retires a proposal whose words are already in the
// user's own profile, and it is bound the same way. The window is real: the
// apply's compare-and-set on the proposal happens before the profile is
// written, and the tidying happens after. A user who rewrites the proposal in
// between has words of their own under that name, and the tidying must leave
// them there and report itself unfinished rather than delete them.
func TestProfileApplyCleanupRefusesAProposalRewrittenAfterTheWrite(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	const profileBefore = "# Profile\n\nWritten already.\n"
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, profileBefore)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindProfileEdit,
		Title:     "timezone",
		Body:      "The user is in Mexico City.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	const theirs = "---\nid: mine\n---\n\nActually, do not write this down.\n"

	repo.memoryProfileApplyBarrier = func(stage string) error {
		if stage == memoryProfileApplyWritten {
			if err := os.WriteFile(full, []byte(theirs), 0o600); err != nil {
				t.Errorf("rewrite the proposal: %v", err)
			}
		}
		return nil
	}
	result, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash(profileBefore),
		Content:             "# Profile\n\n- The user is in Mexico City.\n",
	})
	repo.memoryProfileApplyBarrier = nil
	if err != nil {
		t.Fatalf("ApplyMemoryProfileCandidate: %v", err)
	}
	if !result.CleanupPending {
		t.Fatal("the apply reported its tidying finished while the user's file is still there")
	}
	held, readErr := os.ReadFile(full)
	if readErr != nil || string(held) != theirs {
		t.Fatalf("the user's own words were removed: %q, %v", held, readErr)
	}
}

// The unbound door is gone. Nothing in this package may ask for a removal that
// names only a path, because that request cannot be answered truthfully: the
// entry under a name is not something the caller can hold still.
func TestRetiredRemovalRequiresTheBytesItIsEntitledToRemove(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	_, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")

	err := vault.RemoveInboxNote(ctx(), memoryfiles.RemoveInboxNoteRequest{
		RelPath: candidate.InboxPath,
		Mode:    memoryfiles.RemoveRetiredCandidate,
	})
	if !errors.Is(err, memoryfiles.ErrUnboundDecision) {
		t.Fatalf("hashless retired removal = %v, want it refused as unbound", err)
	}
	if _, statErr := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); statErr != nil {
		t.Fatalf("the refused removal deleted the file anyway: %v", statErr)
	}
}

// The user may edit a proposal before accepting it — a vault exists so they
// can. The tidying afterwards is bound to the bytes the apply actually acted
// on, which is the file as it was read, and not the row's record of what Turing
// originally proposed. Binding it to the row would refuse every edited
// proposal, leaving an applied one sitting in the inbox looking decidable.
func TestProfileApplyRetiresAProposalTheUserEditedBeforeAcceptingIt(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	const profileBefore = "# Profile\n\nWritten already.\n"
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, profileBefore)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindProfileEdit,
		Title:     "timezone",
		Body:      "The user is in Mexico City.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	// The same proposal, reworded by the user in their editor and still a
	// profile edit. This is the ordinary case, not an attack.
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	original, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read the proposal: %v", err)
	}
	edited := strings.Replace(string(original), "Mexico City", "Guadalajara", 1)
	if edited == string(original) {
		t.Fatal("this test needs the proposal body to actually change")
	}
	if err := os.WriteFile(full, []byte(edited), 0o600); err != nil {
		t.Fatalf("edit the proposal: %v", err)
	}

	result, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:           candidate.CandidateID,
		ExpectedContentHash:   memoryfiles.ContentHash(profileBefore),
		ExpectedCandidateHash: memoryfiles.ContentHash(edited),
		Content:               "# Profile\n\n- The user is in Guadalajara.\n",
	})
	if err != nil {
		t.Fatalf("ApplyMemoryProfileCandidate: %v", err)
	}
	if result.CleanupPending {
		t.Fatal("the tidying refused a proposal the user edited and then accepted")
	}
	if _, statErr := os.Stat(full); !os.IsNotExist(statErr) {
		t.Fatalf("the applied proposal is still in the inbox: %v", statErr)
	}
	if countRows(t, repo, `SELECT COUNT(*) FROM vault_artifacts WHERE session_id = ?`, sessionID) != 0 {
		t.Fatal("the reservation for a file that is gone was kept")
	}
}

// Crash recovery finishing an apply whose file the crash had already taken
// away. There is nothing to remove, which is the outcome the apply wanted, so
// the reservation goes rather than being kept forever against a missing file.
func TestRecoveredProfileApplyReleasesTheReservationForAFileThatIsGone(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	const profileBefore = "# Profile\n\nWritten already.\n"
	const profileAfter = "# Profile\n\n- The user keeps bees.\n"
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, profileBefore)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindProfileEdit,
		Title:     "bees",
		Body:      "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	// The process dies after the profile is written and before the bookkeeping,
	// and the proposal file is gone by the time recovery runs.
	repo.memoryProfileApplyBarrier = func(stage string) error {
		if stage == memoryProfileApplyWritten {
			return errors.New("the process died")
		}
		return nil
	}
	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash(profileBefore),
		Content:             profileAfter,
	}); err != nil {
		t.Fatalf("ApplyMemoryProfileCandidate: %v", err)
	}
	repo.memoryProfileApplyBarrier = nil
	if err := os.Remove(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
		t.Fatalf("take the proposal away: %v", err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.ProfileAppliesFinalized != 1 {
		t.Fatalf("recovery = %+v, want the apply finished", report)
	}
	if countRows(t, repo, `SELECT COUNT(*) FROM vault_artifacts WHERE session_id = ?`, sessionID) != 0 {
		t.Fatal("the reservation for a file that is gone was kept")
	}
}

// And the same recovery when the user has put a file of their own under that
// path. Nothing proves it is Turing's, so it stays — and so does the
// reservation that says somebody still has to deal with it.
func TestRecoveredProfileApplyLeavesAFileItCannotProveIsTuringsAlone(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	const profileBefore = "# Profile\n\nWritten already.\n"
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, profileBefore)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindProfileEdit,
		Title:     "bees",
		Body:      "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	repo.memoryProfileApplyBarrier = func(stage string) error {
		if stage == memoryProfileApplyWritten {
			return errors.New("the process died")
		}
		return nil
	}
	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash(profileBefore),
		Content:             "# Profile\n\n- The user keeps bees.\n",
	}); err != nil {
		t.Fatalf("ApplyMemoryProfileCandidate: %v", err)
	}
	repo.memoryProfileApplyBarrier = nil

	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	const theirs = "Something I wrote myself, under a name Turing had used.\n"
	if err := os.WriteFile(full, []byte(theirs), 0o600); err != nil {
		t.Fatalf("write the user's own file: %v", err)
	}

	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	held, readErr := os.ReadFile(full)
	if readErr != nil || string(held) != theirs {
		t.Fatalf("the user's own file was removed: %q, %v", held, readErr)
	}
	if countRows(t, repo, `SELECT COUNT(*) FROM vault_artifacts WHERE session_id = ?`, sessionID) != 1 {
		t.Fatal("the reservation for a file that is still there was released")
	}
}

// A creation is reserve, write, record — three steps that are deliberately not
// one transaction, because the reservation has to be durable before any byte
// reaches the vault. That leaves a window, and reconcile's crash-heal walks
// straight into it: the reservation is older than the pass's anchor, the file
// is on disk and provably Turing's, and no candidate row exists yet to
// coordinate against.
//
// Both parties are then trying to bind the same row to the same bytes, which is
// not a conflict — it is agreement. Whichever gets there first is right, and the
// other must not fail: a creation that failed here would delete the note it had
// just written and hand the caller an error over work that was actually done.
func TestACreationWhoseRowReconcileBoundFirstStillSucceeds(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	// Stand between the write and the record, and run the whole reconcile pass
	// there. This is the race as a fixed ordering rather than a coin toss.
	var healed bool
	repo.memoryCandidateRecordBarrier = func() error {
		if healed {
			return nil
		}
		healed = true
		if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
			return err
		}
		return nil
	}
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "bees",
		Body:      "The user keeps bees.",
	})
	repo.memoryCandidateRecordBarrier = nil
	if err != nil {
		t.Fatalf("a creation whose row reconcile bound first: %v", err)
	}

	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	if _, statErr := os.Stat(full); statErr != nil {
		t.Fatalf("the written proposal was thrown away: %v", statErr)
	}
	artifact := onlyVaultArtifact(t, repo, sessionID)
	if artifact.State != VaultArtifactStateReady {
		t.Fatalf("artifact state = %q, want a finalized row", artifact.State)
	}
	raw, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("read the written candidate: %v", readErr)
	}
	if artifact.ExpectedContentHash != memoryfiles.ContentHash(string(raw)) {
		t.Fatalf("binding = %q, want the hash of the raw file", artifact.ExpectedContentHash)
	}
	if countRows(t, repo, `SELECT COUNT(*) FROM memory_candidates WHERE id = ?`, candidate.CandidateID) != 1 {
		t.Fatal("the candidate row was not recorded")
	}
}

// A landed write that a cleanup pass reached before reconcile could bind it.
// The purge refuses it — nothing proved it was Turing's yet — and marks the row
// delete_failed, which is where it used to stay forever: reconcile only looked
// at reservations, so the row could never gain the binding that would let a
// retry drain it, and a note Turing wrote sat in the user's vault behind a row
// nothing could act on.
func TestReconcileBindsALandedWriteAPurgeAlreadyRefused(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	note, err := vault.CreateInboxNote(ctx(), memoryfiles.CreateInboxNoteRequest{
		Kind:  memoryfiles.KindBelief,
		Title: "bees",
		Body:  "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateInboxNote: %v", err)
	}
	artifact, err := repo.ReserveVaultArtifact(ctx(), ReserveVaultArtifactInput{
		SessionID: sessionID,
		VaultPath: note.RelPath,
	})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}

	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err == nil {
		t.Fatal("the purge removed a file no row could prove it owned")
	}
	if state := vaultArtifactState(t, repo, artifact.ArtifactID); state != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifact state = %q, want the refused row marked", state)
	}

	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	bound, ok := vaultArtifactHash(t, repo, artifact.ArtifactID)
	if !ok || bound != note.ContentHash {
		t.Fatalf("binding = (%q, %v), want the hash of the bytes on disk", bound, ok)
	}
	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err != nil {
		t.Fatalf("the retry could not drain a row reconcile had bound: %v", err)
	}
	if countRows(t, repo, `SELECT COUNT(*) FROM vault_artifacts WHERE session_id = ?`, sessionID) != 0 {
		t.Fatal("the manifest could not drain")
	}
	if _, statErr := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(note.RelPath))); !os.IsNotExist(statErr) {
		t.Fatalf("the bound row's file survived its withdrawal: %v", statErr)
	}
}
