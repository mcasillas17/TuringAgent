package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A hashless rejection is the one door out for a proposal nobody can parse, and
// until now it was the one door that named nothing at all.
//
// The pre-check reads the candidate, fails to read it, and gives the vault's
// path lock back. The primitive takes that lock again and deletes whatever is
// under the name. Between the two the user's editor, a sync client or Turing's
// own writer can put a different file there — and a hashless removal, having
// nothing to compare, would delete it. The bytes it removed would be a claim
// about the user that nobody had ever seen.
//
// So the pre-check now hands the primitive the identity of the exact entry it
// failed to read, and the primitive deletes that entry or it deletes nothing.
const unparseableCandidate = "---\nrefs: [broken\n---\n\nNobody can read this.\n"

// unreadableIdentity is what a decision's pre-check comes away with when the
// candidate will not read. It is the only thing that can authorise a hashless
// removal, and there is no way to make one except by asking the vault.
func unreadableIdentity(t *testing.T, vault *Vault, relPath string) UnreadableCandidateEntry {
	t.Helper()
	reading, err := vault.ReadInboxCandidate(context.Background(), relPath)
	if err != nil {
		t.Fatalf("pre-check %q: %v", relPath, err)
	}
	if reading.Readable {
		t.Fatalf("pre-check read %q as a proposal; this test needs one nobody can read", relPath)
	}
	return reading.Unreadable
}

// The pre-check reads a file it cannot parse; the entry is then replaced by a
// perfectly good proposal; the rejection must not delete it. Somebody wrote
// that file, and nobody has decided about it.
func TestBoundHashlessRejectionRefusesAReadableReplacement(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	const replacement = "---\nkind: belief\n---\n\nA newer claim nobody has read yet.\n"
	replaceInboxEntry(t, vault, "inbox/note.md", replacement)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hashless rejection of a replaced entry = %v, want ErrStaleContent", err)
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

// The same, when the replacement is *also* unparseable. Identity is the whole
// question here: "it is still unreadable" says nothing about whose bytes they
// are, and a second broken file is still a file the user never saw.
func TestBoundHashlessRejectionRefusesAnotherUnreadableReplacement(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	const replacement = "---\nalso: [broken\n---\n\nA different unreadable claim.\n"
	replaceInboxEntry(t, vault, "inbox/note.md", replacement)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hashless rejection of a second broken file = %v, want ErrStaleContent", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("a hashless rejection deleted a replacement it never read: %v", readErr)
	}
	if string(survived) != replacement {
		t.Fatalf("the file holds %q, want the replacement %q", survived, replacement)
	}
	requireNoStagingResidue(t, vault)
}

// An editor saving in place keeps the inode, so identity alone would call a
// repaired proposal unchanged. If the user fixed the frontmatter, the file is
// no longer the thing this door exists for: it reads as a proposal now, and a
// proposal is decided about by reading it.
func TestBoundHashlessRejectionRefusesAProposalRepairedInPlace(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	const repaired = "---\nkind: belief\n---\n\nThe user fixed the frontmatter.\n"
	if err := os.WriteFile(full, []byte(repaired), 0o600); err != nil {
		t.Fatalf("repair the proposal in place: %v", err)
	}

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hashless rejection of a repaired proposal = %v, want ErrStaleContent", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("a hashless rejection deleted a proposal the user had just repaired: %v", readErr)
	}
	if string(survived) != repaired {
		t.Fatalf("the file holds %q, want the repaired proposal %q", survived, repaired)
	}
	requireNoStagingResidue(t, vault)
}

// Rewritten in place and still unparseable is still not the file that was read.
// The bytes are the user's own newer words, and nobody has decided about them.
func TestBoundHashlessRejectionRefusesAnInPlaceRewrite(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	const rewritten = "---\nstill: [broken\n---\n\nDifferent broken words.\n"
	if err := os.WriteFile(full, []byte(rewritten), 0o600); err != nil {
		t.Fatalf("rewrite the proposal in place: %v", err)
	}

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hashless rejection of an in-place rewrite = %v, want ErrStaleContent", err)
	}
	if survived, readErr := os.ReadFile(full); readErr != nil || string(survived) != rewritten {
		t.Fatalf("the rewritten proposal did not survive: %q, %v", survived, readErr)
	}
	requireNoStagingResidue(t, vault)
}

// Nothing moved, so the user gets what they asked for.
func TestBoundHashlessRejectionRemovesTheEntryItWasBoundTo(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	}); err != nil {
		t.Fatalf("remove the unreadable proposal it was bound to: %v", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("expected the unreadable proposal to be gone, got %v", err)
	}
	requireNoStagingResidue(t, vault)
}

// A hashless removal that names no entry at all is the unbound decision this
// package refuses everywhere else. The identity cannot be forged: it only comes
// from a pre-check that really failed to read a real file.
func TestHashlessRejectionRefusesToActWithoutAnIdentity(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath: "inbox/note.md",
		Mode:    RemoveUnreadableCandidate,
	})
	if !errors.Is(err, ErrUnboundDecision) {
		t.Fatalf("unbound hashless rejection = %v, want ErrUnboundDecision", err)
	}
	if _, statErr := os.Lstat(full); statErr != nil {
		t.Fatalf("an unbound hashless rejection deleted the proposal anyway: %v", statErr)
	}
	requireNoStagingResidue(t, vault)
}

// The pre-check found nothing, and by the time the primitive looked there was a
// file. A rejection of a proposal that was not there is not a rejection of
// whatever arrived afterwards.
func TestBoundHashlessRejectionRefusesAFileThatArrivedAfterAnEmptyPreCheck(t *testing.T) {
	vault := newTestVault(t)
	reading, err := vault.ReadInboxCandidate(context.Background(), "inbox/note.md")
	if err != nil {
		t.Fatalf("pre-check a candidate that is not there: %v", err)
	}
	if reading.Readable {
		t.Fatal("the pre-check claimed to read a file that does not exist")
	}
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)

	removeErr := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: reading.Unreadable,
	})
	if !errors.Is(removeErr, ErrStaleContent) {
		t.Fatalf("hashless rejection of a file that arrived afterwards = %v, want ErrStaleContent", removeErr)
	}
	// The sentence matters as much as the refusal. "Another file took its name"
	// would be a claim about a proposal that never existed; what happened is
	// that there was nothing to reject and something turned up afterwards, and
	// that is what the user has to be told before they look again.
	if !strings.Contains(removeErr.Error(), "there was no proposal under that name when it was read") {
		t.Fatalf("the refusal does not say the proposal was never there: %v", removeErr)
	}
	if _, statErr := os.Lstat(full); statErr != nil {
		t.Fatalf("the arriving file was deleted by a rejection of nothing: %v", statErr)
	}
	requireNoStagingResidue(t, vault)
}

// A candidate that was already gone stays the outcome the user asked for: the
// rejection is not an error, and there is nothing to delete.
func TestBoundHashlessRejectionToleratesAnAlreadyMissingEntry(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")
	if err := os.Remove(filepath.Join(vault.Root(), InboxDirName, "note.md")); err != nil {
		t.Fatalf("remove the proposal behind the decision's back: %v", err)
	}

	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	}); err != nil {
		t.Fatalf("hashless rejection of an already-missing proposal = %v, want no error", err)
	}
	requireNoStagingResidue(t, vault)
}

// The same file, replaced between the pre-check and the primitive by one that
// can be opened. Identity is all there is here, and it is enough.
func TestBoundHashlessRejectionRefusesAReplacementOfAnUnopenableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	if err := os.Chmod(full, 0o000); err != nil {
		t.Fatalf("close the file to every reader: %v", err)
	}
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	const replacement = "---\nkind: belief\n---\n\nA newer claim nobody has read yet.\n"
	replaceInboxEntry(t, vault, "inbox/note.md", replacement)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hashless rejection of a replaced unopenable file = %v, want ErrStaleContent", err)
	}
	if survived, readErr := os.ReadFile(full); readErr != nil || string(survived) != replacement {
		t.Fatalf("the replacement did not survive: %q, %v", survived, readErr)
	}
	requireNoStagingResidue(t, vault)
}

// The window the pre-detach check cannot speak for. Everything above rewrites
// the file before the call; this one rewrites it *inside* the call, after the
// identity and the bytes have already been agreed and before the entry is off
// its name. Only the re-read after the detach can refuse here, and it can only
// do so because the hashless door now carries a hash of its own — the bytes
// that would not parse — into that check.
func TestBoundHashlessRejectionRefusesARewriteInsideTheDetachWindow(t *testing.T) {
	const rewritten = "---\nstill: [broken\n---\n\nWritten while the rejection was mid-flight.\n"
	var vault *Vault
	vault = vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase != detachPhaseBeforeDetach {
			return
		}
		// In place, so the inode does not move: identity has already passed and
		// cannot be what refuses this.
		full := filepath.Join(vault.Root(), InboxDirName, "note.md")
		if err := os.WriteFile(full, []byte(rewritten), 0o600); err != nil {
			t.Errorf("rewrite the proposal mid-flight: %v", err)
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hashless rejection of a rewrite inside the detach window = %v, want ErrStaleContent", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("a hashless rejection deleted words written while it was in flight: %v", readErr)
	}
	if string(survived) != rewritten {
		t.Fatalf("the file holds %q, want the mid-flight rewrite %q", survived, rewritten)
	}
	requireNoStagingResidue(t, vault)
}

// The case identity alone answers. Both files are past the size bound, so
// neither can be hashed and neither will ever parse: "still unreadable" is true
// of the replacement too. Only the inode says these are different bytes, and
// the ones under the name now are a claim about the user nobody has read.
func TestBoundHashlessRejectionRefusesAnUnreadableReplacementOfAnUnhashableFile(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", strings.Repeat("x", MaxNoteBytes+1))
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	replacement := strings.Repeat("y", MaxNoteBytes+1)
	replaceInboxEntry(t, vault, "inbox/note.md", replacement)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hashless rejection of a second over-sized file = %v, want ErrStaleContent", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("a hashless rejection deleted an over-sized file it never read: %v", readErr)
	}
	if string(survived) != replacement {
		t.Fatal("the file at the candidate path is not the replacement")
	}
	requireNoStagingResidue(t, vault)
}

// The case the "still unreadable" question answers on its own. A proposal past
// the size bound has no hash to be held to, so identity is all that survives
// the window — and identity says nothing about the bytes. If the user trimmed
// the file back into something that reads as a proposal, the inode has not
// moved and there is nothing to compare it against; the only thing left that
// can refuse is asking the file again whether it is still the unreadable thing
// this door exists for.
func TestBoundHashlessRejectionRefusesAnUnhashableFileTrimmedIntoAProposal(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", strings.Repeat("x", MaxNoteBytes+1))
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	const trimmed = "---\nkind: belief\n---\n\nThe user trimmed it down to a proposal.\n"
	if err := os.WriteFile(full, []byte(trimmed), 0o600); err != nil {
		t.Fatalf("trim the proposal in place: %v", err)
	}

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hashless rejection of a file that now reads as a proposal = %v, want ErrStaleContent", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("a hashless rejection deleted a proposal the user had just made readable: %v", readErr)
	}
	if string(survived) != trimmed {
		t.Fatalf("the file holds %q, want the trimmed proposal %q", survived, trimmed)
	}
	requireNoStagingResidue(t, vault)
}

// The pre-check itself: a candidate that reads and parses comes back as one,
// carries no identity, and is not something a hashless removal may be built
// from. This is what keeps the hashless door from becoming a way to delete a
// proposal the user could have read.
func TestReadInboxCandidateAnswersWithTheProposalWhenItParses(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	reading, err := vault.ReadInboxCandidate(context.Background(), candidate.RelPath)
	if err != nil {
		t.Fatalf("read a parseable candidate: %v", err)
	}
	if !reading.Readable {
		t.Fatalf("a parseable candidate came back unreadable: %v", reading.ReadErr)
	}
	if reading.Note.ContentHash != candidate.ContentHash {
		t.Fatalf("the pre-check hashed %q, want %q", reading.Note.ContentHash, candidate.ContentHash)
	}
	if reading.Unreadable.Bound() {
		t.Fatal("a readable proposal handed out an identity a hashless removal could act on")
	}
}
