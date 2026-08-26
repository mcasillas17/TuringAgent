package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// A hashless rejection is bound to what its pre-check actually managed to learn
// about the entry, and those are two different bindings wearing one name.
//
// A proposal whose bytes could be read but not parsed is bound to a hash of
// those bytes: it is the malformed-file case, and it is the case the door was
// built for. A proposal nothing could open, or one past the size bound, has no
// bytes at all, and identity is the whole of what can be asked about it.
//
// The two must not be allowed to swap places. A file the pre-check hashed and
// the primitive cannot open is not an unhashable file — it is a hashed file
// whose binding cannot be checked, and an inode number is not that check. An
// editor writing new words in place keeps the inode, and a permission change
// over the top is enough to make a hash-bound rejection fall back to asking
// only "same inode?" — which those new words pass, and which would delete a
// claim about the user that nobody has ever read.

// closedFileContent reads a file whose permissions were taken away, by putting
// them back first. A test that wants to prove bytes survived has to be able to
// look at them.
func closedFileContent(t *testing.T, full string) string {
	t.Helper()
	if err := os.Chmod(full, 0o600); err != nil {
		t.Fatalf("reopen %q to read it: %v", full, err)
	}
	content, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read %q: %v", full, err)
	}
	return string(content)
}

// closeToEveryReader takes every permission off a file, which is how a hashed
// candidate stops being openable without its inode moving.
func closeToEveryReader(t *testing.T, full string) {
	t.Helper()
	if err := os.Chmod(full, 0o000); err != nil {
		t.Fatalf("close %q to every reader: %v", full, err)
	}
}

func entryIdentity(t *testing.T, full string) (uint64, uint64) {
	t.Helper()
	var stat unix.Stat_t
	if err := unix.Lstat(full, &stat); err != nil {
		t.Fatalf("inspect %q: %v", full, err)
	}
	return uint64(stat.Dev), uint64(stat.Ino)
}

// The finding. The pre-check read the malformed proposal and hashed it, so the
// removal is bound to those bytes. Then the user's editor wrote different
// broken words in place — same inode — and the file lost its permissions. The
// primitive can no longer open it, and the only question it could still answer
// is the one that cannot tell those two files apart. It has to refuse, and it
// has to refuse before the file is ever taken off its name: a rejection with no
// standing has no business moving the bytes around to find that out.
func TestBoundHashlessRejectionRefusesAHashedCandidateItCanNoLongerOpen(t *testing.T) {
	detached := false
	vault := vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeDetach {
			detached = true
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")
	if !identity.hashed {
		t.Fatal("this test needs a pre-check that hashed the bytes it could not parse")
	}
	beforeDev, beforeIno := entryIdentity(t, full)

	const rewritten = "---\nstill: [broken\n---\n\nWritten after the pre-check, then closed.\n"
	if err := os.WriteFile(full, []byte(rewritten), 0o600); err != nil {
		t.Fatalf("rewrite the proposal in place: %v", err)
	}
	closeToEveryReader(t, full)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hash-bound rejection of an entry it cannot open = %v, want ErrStaleContent", err)
	}
	if os.Geteuid() != 0 {
		// The sentence and the moment both matter. Running as root every file
		// is openable, the ordinary in-place-rewrite check answers this, and
		// neither says anything about the guard under test.
		if !strings.Contains(err.Error(), "cannot be read again to check they are still the same bytes") {
			t.Fatalf("the refusal does not say the binding could not be checked: %v", err)
		}
		if detached {
			t.Fatal("a rejection it had no standing to make still took the file off its name")
		}
	}
	if got := closedFileContent(t, full); got != rewritten {
		t.Fatalf("the file holds %q, want the words written after the pre-check %q", got, rewritten)
	}
	// The inode never moved, which is the whole point: identity alone would
	// have called this the file the pre-check read.
	afterDev, afterIno := entryIdentity(t, full)
	if afterDev != beforeDev || afterIno != beforeIno {
		t.Fatal("the test rewrote the file by replacing it; this case is about a rewrite that keeps the inode")
	}
	requireNoStagingResidue(t, vault)
}

// The same hole reached by truncation rather than a rewrite. The bytes the
// pre-check hashed are gone, the inode is not, and nothing can open what is
// left to say so.
func TestBoundHashlessRejectionRefusesAHashedCandidateTruncatedAndClosed(t *testing.T) {
	detached := false
	vault := vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeDetach {
			detached = true
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	if err := os.Truncate(full, 0); err != nil {
		t.Fatalf("truncate the proposal in place: %v", err)
	}
	closeToEveryReader(t, full)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hash-bound rejection of a truncated, closed entry = %v, want ErrStaleContent", err)
	}
	if os.Geteuid() != 0 {
		if !strings.Contains(err.Error(), "cannot be read again to check they are still the same bytes") {
			t.Fatalf("the refusal does not say the binding could not be checked: %v", err)
		}
		if detached {
			t.Fatal("a rejection it had no standing to make still took the file off its name")
		}
	}
	if got := closedFileContent(t, full); got != "" {
		t.Fatalf("the truncated file holds %q, want it left exactly as it was", got)
	}
	requireNoStagingResidue(t, vault)
}

// A replacement that is also unopenable. Identity answers this one on its own —
// the inode moved — and it must go on answering it, whatever the hash binding
// says.
func TestBoundHashlessRejectionRefusesAnUnopenableReplacementOfAHashedCandidate(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	const replacement = "---\nalso: [broken\n---\n\nSomebody else's broken words.\n"
	replaceInboxEntry(t, vault, "inbox/note.md", replacement)
	closeToEveryReader(t, full)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hash-bound rejection of an unopenable replacement = %v, want ErrStaleContent", err)
	}
	if got := closedFileContent(t, full); got != replacement {
		t.Fatalf("the file holds %q, want the replacement %q", got, replacement)
	}
	requireNoStagingResidue(t, vault)
}

// The control, and the reason the fallback exists at all. Here the pre-check
// itself could not open the file, so there never were bytes to be bound to and
// identity is not a weakened check but the whole of the one available. Nothing
// moved, and the user gets rid of the claim they can neither read nor accept.
func TestBoundHashlessRejectionRemovesAnUnopenableCandidateItsPreCheckCouldNotHash(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	closeToEveryReader(t, full)
	identity := unreadableIdentity(t, vault, "inbox/note.md")
	if identity.hashed {
		t.Fatal("a file nothing could open came back with a hash of its bytes")
	}

	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	}); err != nil {
		t.Fatalf("hashless rejection of an unopenable proposal nothing changed = %v, want it removed", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("the proposal the user could not read is still there: %v", err)
	}
	requireNoStagingResidue(t, vault)
}

// The other half of that control. An unhashable pre-check still cannot delete
// whatever turned up under the name afterwards, even when the newcomer is just
// as unopenable as what it displaced.
func TestBoundHashlessRejectionRefusesAnUnopenableReplacementOfAnUnhashableCandidate(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	closeToEveryReader(t, full)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	const replacement = "somebody else's unreadable words"
	replaceInboxEntry(t, vault, "inbox/note.md", replacement)
	closeToEveryReader(t, full)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hashless rejection of an unopenable replacement = %v, want ErrStaleContent", err)
	}
	if !strings.Contains(err.Error(), "another file has taken its name") {
		t.Fatalf("the refusal does not say another file took the name: %v", err)
	}
	if got := closedFileContent(t, full); got != replacement {
		t.Fatalf("the file holds %q, want the replacement %q", got, replacement)
	}
	requireNoStagingResidue(t, vault)
}

// The same rule on the far side of the detach, where the entry is off its name
// and the only thing that can speak for it is the descriptor this removal
// opened. A binding to bytes is checked by reading those bytes; when there is
// no descriptor to read them from, the answer is not "the inode matches, go
// ahead" but a refusal that puts the file back.
//
// The guard is exercised directly because the path above it now refuses first,
// and a check nothing can reach is a check the next reader deletes. It is
// exercised against a real detached entry, because that is what the production
// check inspects: the entry under the reserved name, not a number a test made
// up.

// detachForTest takes an inbox entry off its name exactly as a rejection does,
// so the far-side checks can be run on their own terms against something that
// is really off its name.
func detachForTest(t *testing.T, vault *Vault, relPath string) *detachedEntry {
	t.Helper()
	clean, err := requireInboxRelPath(relPath)
	if err != nil {
		t.Fatalf("confine %q: %v", relPath, err)
	}
	parent, leaf, err := vault.openParent(context.Background(), clean, false)
	if err != nil {
		t.Fatalf("open the parent of %q: %v", clean, err)
	}
	t.Cleanup(func() { _ = parent.Close() })
	detached, err := vault.detachEntry(context.Background(), parent, leaf, clean)
	if err != nil || detached == nil {
		t.Fatalf("detach %q: %v", clean, err)
	}
	return detached
}

// detachedIdentity is what the entry says about itself once it is off its name.
// Every far-side check starts from it, so a test that wants to reach the state
// questions has to pass it rather than invent one.
func detachedIdentity(t *testing.T, detached *detachedEntry) unix.Stat_t {
	t.Helper()
	stat, err := detached.stat()
	if err != nil {
		t.Fatalf("inspect the detached entry: %v", err)
	}
	return stat
}

func TestDetachedHashBindingRefusesAnEntryThatCannotBeReadAgain(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/note.md", "the proposal the user read")
	detached := detachForTest(t, vault, "inbox/note.md")

	objection := detached.objectionToDetachedEntry(
		context.Background(), "a-hash-of-bytes-somebody-read", unreadableFailureNone, nil, detachedIdentity(t, detached),
	)
	if objection == "" {
		t.Fatal("a removal bound to bytes it cannot read again was allowed to delete the file on an inode match alone")
	}
	// The sentence is the check, not a side effect of reading through a
	// descriptor that is not there. A refusal that reaches the user as
	// "invalid argument" is one nobody can act on.
	if !strings.Contains(objection, "could not be read again before removal (nothing here can open it any more)") {
		t.Fatalf("the refusal says %q, want it to say the entry cannot be opened any more", objection)
	}
}

// And the control beside it, which is no longer a shortcut. There is no
// descriptor here — this is the file nothing could open — so the check goes and
// opens the detached entry under its reserved name and asks it the same
// question the pre-check asked. Still nothing can open it, and it is still the
// entry that was detached: the licence holds and the user gets rid of the claim.
func TestDetachedIdentityBindingIsCheckedAgainstTheEntryItDetached(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	closeToEveryReader(t, full)
	detached := detachForTest(t, vault, "inbox/note.md")
	identity := detachedIdentity(t, detached)

	if objection := detached.objectionToDetachedEntry(
		context.Background(), "", unreadableFailureUnopenable, nil, identity,
	); objection != "" {
		t.Fatalf("an unhashable removal of the entry it was bound to was refused: %q", objection)
	}
	moved := identity
	moved.Ino++
	if objection := detached.objectionToDetachedEntry(
		context.Background(), "", unreadableFailureUnopenable, nil, moved,
	); objection == "" {
		t.Fatal("an unhashable removal deleted an entry that was not the one it was bound to")
	}
	// And the state question the reopen exists to ask: the permissions come
	// back while the entry is off its name, so what is behind it can be read
	// and nobody has read it.
	staged := filepath.Join(vault.Root(), InboxDirName, detached.staging)
	if err := os.Chmod(staged, 0o600); err != nil {
		t.Fatalf("open the detached entry back up: %v", err)
	}
	objection := detached.objectionToDetachedEntry(
		context.Background(), "", unreadableFailureUnopenable, nil, identity,
	)
	if !strings.Contains(objection, "it can be read now") {
		t.Fatalf("the reopened entry was deleted on an inode match alone: %q", objection)
	}
}

// The far side of the detach for a removal bound to no bytes at all, exercised
// directly through the descriptor the removal opened before the detach. The
// path above it refuses most of these first, and a guard that is only ever
// reached through another guard is one the next reader deletes: these have to
// be able to fail on their own terms.
//
// The descriptor is the one the removal opened, so it is the same inode however
// the name has been used since. What it is asked is the question the near side
// asked: can this be read now, and if not, is it unreadable the same way it was.
func TestDetachedUnreadableBindingChecksTheFileStillWillNotRead(t *testing.T) {
	detached := func(t *testing.T, failure unreadableFailure, content string) string {
		t.Helper()
		vault := newTestVault(t)
		writeVaultFile(t, vault, "inbox/note.md", content)
		parent, leaf, err := vault.openParent(context.Background(), "inbox/note.md", false)
		if err != nil {
			t.Fatalf("open the inbox: %v", err)
		}
		defer func() { _ = parent.Close() }()
		opened, openedStat, err := openConfinedEntry(parent, leaf, "inbox/note.md")
		if err != nil {
			t.Fatalf("open the candidate: %v", err)
		}
		defer func() { _ = opened.Close() }()
		entry, err := vault.detachEntry(context.Background(), parent, leaf, "inbox/note.md")
		if err != nil || entry == nil {
			t.Fatalf("detach the candidate: %v", err)
		}
		return entry.objectionToDetachedEntry(context.Background(), "", failure, opened, openedStat)
	}

	// It could not be read when it was rejected and it can be read now. Those
	// words arrived after the pre-check and nobody has seen them.
	objection := detached(t, unreadableFailureUnopenable, unparseableCandidate)
	if !strings.Contains(objection, "it can be read now") {
		t.Fatalf("a removal bound to a file nobody could read deleted bytes it could read: %q", objection)
	}
	// It is unreadable, but not the way it was: it was too big to read and now
	// nothing can be got out of it at all, or the other way about.
	objection = detached(t, unreadableFailureUnopenable, strings.Repeat("x", MaxNoteBytes+1))
	if !strings.Contains(objection, "unreadable in a different way") {
		t.Fatalf("a removal bound to an unopenable file deleted an over-sized one: %q", objection)
	}
	// And the control: still past the bound, as it was, so the user gets rid
	// of the claim they can neither read nor accept.
	if objection := detached(t, unreadableFailureOverLimit, strings.Repeat("x", MaxNoteBytes+1)); objection != "" {
		t.Fatalf("a removal of the over-sized file it was bound to was refused: %q", objection)
	}
}

// A rewrite that lands inside the detach window and closes the file behind it.
// The descriptor is already open, so the re-read still happens and still
// refuses; what this pins is that a permission change mid-flight cannot turn a
// hash-bound removal back into an inode-only one.
func TestBoundHashlessRejectionRefusesARewriteAndCloseInsideTheDetachWindow(t *testing.T) {
	const rewritten = "---\nstill: [broken\n---\n\nWritten and closed while the rejection was mid-flight.\n"
	var vault *Vault
	vault = vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase != detachPhaseBeforeDetach {
			return
		}
		full := filepath.Join(vault.Root(), InboxDirName, "note.md")
		if err := os.WriteFile(full, []byte(rewritten), 0o600); err != nil {
			t.Errorf("rewrite the proposal mid-flight: %v", err)
			return
		}
		if err := os.Chmod(full, 0o000); err != nil {
			t.Errorf("close the proposal mid-flight: %v", err)
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
		t.Fatalf("hash-bound rejection of a mid-flight rewrite = %v, want ErrStaleContent", err)
	}
	if got := closedFileContent(t, full); got != rewritten {
		t.Fatalf("the file holds %q, want the mid-flight rewrite %q", got, rewritten)
	}
	requireNoStagingResidue(t, vault)
}

// The other way a hashed binding stops being checkable: the file is still
// openable, so nothing refuses before the detach, but it has grown past the
// bound every reader here works under and the re-read cannot answer either. An
// unverifiable binding is an unverifiable binding wherever it is discovered, so
// what happens is a restore rather than an unlink — the entry goes back under
// its own name and the refusal says the bytes could not be read again.
func TestBoundHashlessRejectionRestoresAHashedCandidateGrownPastTheBound(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unparseableCandidate)
	identity := unreadableIdentity(t, vault, "inbox/note.md")
	if !identity.hashed {
		t.Fatal("this test needs a pre-check that hashed the bytes it could not parse")
	}
	beforeDev, beforeIno := entryIdentity(t, full)

	grown := unparseableCandidate + strings.Repeat("x", MaxNoteBytes)
	if err := os.WriteFile(full, []byte(grown), 0o600); err != nil {
		t.Fatalf("grow the proposal in place: %v", err)
	}

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("hash-bound rejection of a candidate grown past the bound = %v, want ErrStaleContent", err)
	}
	if !strings.Contains(err.Error(), "could not be read again before removal") {
		t.Fatalf("the refusal does not say the bytes could not be read again: %v", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("the proposal was not put back under its own name: %v", readErr)
	}
	if string(survived) != grown {
		t.Fatalf("the file holds %d bytes, want the %d it grew to", len(survived), len(grown))
	}
	afterDev, afterIno := entryIdentity(t, full)
	if afterDev != beforeDev || afterIno != beforeIno {
		t.Fatal("the restore put back something other than the entry it detached")
	}
	requireNoStagingResidue(t, vault)
}
