package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A rejection deletes the proposal the user read. It must not delete the file
// that arrived after it.
//
// The primitive opens the candidate, checks it against the hash the decision
// named, and then unlinks — and the unlink names a *name*, not the inode it
// just checked. Obsidian, a sync client and Turing's own writer all replace a
// file by writing a new one beside it and renaming over the top, so the entry
// under that name between the check and the unlink can be a different file
// carrying a claim about the user that nobody has seen. Deleting it is the one
// outcome a rejection must never produce: the user said no to something they
// read, and what disappeared was something they did not.
func replaceInboxEntry(t *testing.T, vault *Vault, relPath string, content string) {
	t.Helper()
	full := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
	staged := filepath.Join(filepath.Dir(full), "replacement-in-flight.md")
	if err := os.WriteFile(staged, []byte(content), 0o600); err != nil {
		t.Fatalf("write the replacement: %v", err)
	}
	// Rename, not truncate-and-write: this is how an editor replaces a file,
	// and it is what makes the new bytes a new inode under the same name.
	if err := os.Rename(staged, full); err != nil {
		t.Fatalf("install the replacement: %v", err)
	}
}

func inboxEntries(t *testing.T, vault *Vault) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(vault.Root(), InboxDirName))
	if err != nil {
		t.Fatalf("read the inbox: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func requireNoStagingResidue(t *testing.T, vault *Vault) {
	t.Helper()
	for _, name := range inboxEntries(t, vault) {
		if strings.HasPrefix(name, stagingPrefix) {
			t.Fatalf("the inbox was left holding a staging entry %q", name)
		}
	}
}

// vaultWithDetachBarrier opens a vault whose rejection detach can be
// interrupted at each of its two moments: just before the file is detached from
// its name, and just before a file that turned out not to be the decided one is
// put back.
func vaultWithDetachBarrier(t *testing.T, barrier detachHook) *Vault {
	t.Helper()
	vault, err := openVaultWithDetachBarrier(newTestVaultRoot(t), realSyncHooks(), barrier)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return vault
}

func TestRejectionDoesNotUnlinkAReplacementOfTheDecidedProposal(t *testing.T) {
	const decided = "the proposal the user read"
	const replacement = "a newer claim nobody has read yet"
	var vault *Vault
	vault = vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase != detachPhaseBeforeDetach {
			return
		}
		replaceInboxEntry(t, vault, "inbox/note.md", replacement)
	})
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if err == nil {
		t.Fatal("expected the rejection to refuse rather than delete a file nobody decided about")
	}
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a stale-content refusal the caller can turn into 'read it again', got %v", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("the replacement was deleted by a rejection of the file it replaced: %v", readErr)
	}
	if string(survived) != replacement {
		t.Fatalf("the file at the candidate path holds %q, want the replacement %q", survived, replacement)
	}
	requireNoStagingResidue(t, vault)
}

// The same barrier, for a proposal whose frontmatter nobody could parse. There
// is no hash to name here — that is the whole reason this mode exists — so the
// identity of the file that was opened is the only thing standing between the
// user's rejection and somebody else's file.
func TestHashlessRejectionDoesNotUnlinkAReplacementOfTheOpenedFile(t *testing.T) {
	const malformed = unparseableCandidate
	const replacement = "a newer claim nobody has read yet"
	var vault *Vault
	vault = vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase != detachPhaseBeforeDetach {
			return
		}
		replaceInboxEntry(t, vault, "inbox/note.md", replacement)
	})
	full := writeVaultFile(t, vault, "inbox/note.md", malformed)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if err == nil {
		t.Fatal("expected the hashless rejection to refuse rather than delete a file it never opened")
	}
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a stale-content refusal, got %v", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("the replacement was deleted by a hashless rejection: %v", readErr)
	}
	if string(survived) != replacement {
		t.Fatalf("the file at the candidate path holds %q, want the replacement %q", survived, replacement)
	}
	requireNoStagingResidue(t, vault)
}

// A file rewritten in place keeps its inode, which is exactly what Obsidian
// does when the user edits the proposal and saves. Identity alone would call
// that unchanged and delete their words, so the bytes are checked again against
// the inode that was detached before anything is unlinked.
func TestRejectionRefusesAnInPlaceRewriteOfTheDecidedProposal(t *testing.T) {
	const decided = "the proposal the user read"
	var vault *Vault
	vault = vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase != detachPhaseBeforeDetach {
			return
		}
		full := filepath.Join(vault.Root(), InboxDirName, "note.md")
		if err := os.WriteFile(full, []byte("rewritten in place, same file"), 0o600); err != nil {
			t.Fatalf("rewrite in place: %v", err)
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a stale-content refusal for a rewritten proposal, got %v", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("the rewritten proposal was deleted: %v", readErr)
	}
	if string(survived) != "rewritten in place, same file" {
		t.Fatalf("the file holds %q, want the rewritten text", survived)
	}
	requireNoStagingResidue(t, vault)
}

// The contested case — the replacement is detached, and by the time it is put
// back somebody has taken the name — is in remove_recovery_test.go. Nothing may
// be deleted there, and where the bytes go is the whole question, so the
// assertion lives beside the rest of the recovery discipline.

// The ordinary path, with nothing racing it: the decided file goes, and the
// detach leaves nothing behind.
func TestRejectionRemovesTheDecidedProposalAndCleansUpItsStaging(t *testing.T) {
	const decided = "the proposal the user read"
	recorder := &syncRecorder{}
	vault, err := openVaultWithDetachBarrier(newTestVaultRoot(t), recorder.hooks(), nil)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	full := writeVaultFile(t, vault, "inbox/note.md", decided)
	inbox := inodeOf(t, filepath.Join(vault.Root(), InboxDirName))

	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	}); err != nil {
		t.Fatalf("remove the decided proposal: %v", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("expected the decided proposal to be gone, got %v", err)
	}
	requireNoStagingResidue(t, vault)
	if !recorder.syncedDirectory(inbox) {
		t.Fatal("expected the inbox to be fsynced after the deletion")
	}
}

func TestHashlessRejectionRemovesTheOpenedFileAndCleansUpItsStaging(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	}); err != nil {
		t.Fatalf("remove the unreadable proposal: %v", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("expected the unreadable proposal to be gone, got %v", err)
	}
	requireNoStagingResidue(t, vault)
}

// A candidate too large for the reader still has to be rejectable: the hashless
// mode exists precisely for files nothing can parse, and a size bound is one of
// the ways a file gets there. It is deleted on its identity, which needs no
// read at all.
func TestHashlessRejectionRemovesAFileTooLargeToRead(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", strings.Repeat("x", MaxNoteBytes+1))
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	}); err != nil {
		t.Fatalf("remove the over-sized proposal: %v", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("expected the over-sized proposal to be gone, got %v", err)
	}
	requireNoStagingResidue(t, vault)
}

// A proposal the user cannot even open is exactly what the hashless mode is
// for: a claim about them they can neither read nor accept, and refusing to
// delete it would leave them with no way out at all. It is still deleted on the
// identity of the entry that was inspected, so the barrier holds.
func TestHashlessRejectionRemovesAFileNobodyCanOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	if err := os.Chmod(full, 0o000); err != nil {
		t.Fatalf("close the file to every reader: %v", err)
	}
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	}); err != nil {
		t.Fatalf("remove the unopenable proposal: %v", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("expected the unopenable proposal to be gone, got %v", err)
	}
	requireNoStagingResidue(t, vault)
}

// A rejection that named bytes is held to them, and bytes nobody can read are
// bytes nobody decided about. This one is refused rather than deleted — the
// hashless door above is the way out, and it has to be asked for by name.
func TestDecidedRejectionRefusesAFileItCannotRead(t *testing.T) {
	vault := newTestVault(t)
	const decided = "the proposal the user read"
	full := writeVaultFile(t, vault, "inbox/note.md", decided)
	if err := os.Chmod(full, 0o000); err != nil {
		t.Fatalf("close the file to every reader: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if err == nil {
		t.Fatal("expected a decision naming bytes nobody could read to be refused")
	}
	if _, statErr := os.Lstat(full); statErr != nil {
		t.Fatalf("the file was removed despite the refusal: %v", statErr)
	}
	requireNoStagingResidue(t, vault)
}

// A request that is cancelled while the file is between names puts it back and
// says it was cancelled. "It changed since you read it" is the wrong sentence
// when nothing changed and nobody finished looking — and the file has to be
// under its own name again either way, because a proposal that vanished from
// the user's inbox on a cancelled request is a proposal they can never decide.
func TestRejectionCancelledMidDetachRestoresTheFileAndSaysSo(t *testing.T) {
	const decided = "the proposal the user read"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	vault := vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeVerify {
			cancel()
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(ctx, RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancellation to be reported as one, got %v", err)
	}
	if errors.Is(err, ErrStaleContent) {
		t.Fatalf("a cancelled request claimed the file had changed: %v", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("a cancelled rejection left the proposal off its own name: %v", readErr)
	}
	if string(survived) != decided {
		t.Fatalf("the restored file holds %q, want the proposal %q", survived, decided)
	}
	requireNoStagingResidue(t, vault)
}

// Turing's own tidying keeps the plain, idempotent unlink: it is not a decision
// about text, the outcome it follows is already recorded elsewhere, and leaving
// the file behind is the failure. It stays confined to the inbox, which is the
// property that keeps it from being a way out of the vault.
func TestRetiredCleanupStaysIdempotentAndConfined(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "already accounted for")

	for attempt := 0; attempt < 2; attempt++ {
		if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
			RelPath: "inbox/note.md",
			Mode:    RemoveRetiredCandidate,
		}); err != nil {
			t.Fatalf("retired cleanup attempt %d: %v", attempt, err)
		}
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("expected the retired file to be gone, got %v", err)
	}
	requireNoStagingResidue(t, vault)

	for _, relPath := range append([]string{"beliefs/kept.md"}, escapingRelPathValues()...) {
		err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
			RelPath: relPath,
			Mode:    RemoveRetiredCandidate,
		})
		if !errors.Is(err, ErrConfinement) {
			t.Fatalf("expected the cleaner to be refused %q, got %v", relPath, err)
		}
	}
}
