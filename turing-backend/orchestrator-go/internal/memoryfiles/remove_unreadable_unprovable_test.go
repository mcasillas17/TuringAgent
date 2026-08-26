package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The weakest door in this package was still one door too wide.
//
// A hashless rejection of a proposal nothing can open has no bytes to compare,
// so what was left was an inode number and the fact that the file went on
// refusing to be read. The far side of the detach stated the reserved name,
// found the inode it expected, tried to open the entry, failed — and read that
// second failure as agreement: same class of unreadability, so unlink.
//
// A failed open proves nothing about which inode is under the name. The stat
// and the open are two syscalls with a gap between them, and the reserved name
// is random but not secret: anything that can list the directory can read it
// and rename over it. A replacement that is unreadable in the same way answers
// the class question exactly as the candidate would, so the unlink deleted a
// file nobody here had ever held a descriptor to.
//
// So the rule is now the one the hashed door already keeps: a descriptor whose
// own fstat matches, or nothing is removed. For a proposal nothing can open
// that means the rejection cannot delete it until it can be opened — the honest
// answer, and one the user can act on, instead of an unlink authorised by a
// second failure.

// swapStagingEntry stands in for the writer that renames over a reserved name
// between the stat and the open. The candidate is linked out first, so the test
// can still prove what happened to it, and the replacement is a regular file
// that refuses to be read in exactly the way the candidate does.
func swapStagingEntry(t *testing.T, vault *Vault, staging string, keeper string, replacement string) {
	t.Helper()
	inbox := filepath.Join(vault.Root(), InboxDirName)
	staged := filepath.Join(inbox, staging)
	if err := os.Link(staged, filepath.Join(inbox, keeper)); err != nil {
		t.Fatalf("keep a link to the candidate: %v", err)
	}
	decoy := filepath.Join(inbox, "decoy-in-flight")
	if err := os.WriteFile(decoy, []byte(replacement), 0o600); err != nil {
		t.Fatalf("write the replacement: %v", err)
	}
	closeToEveryReader(t, decoy)
	if err := os.Rename(decoy, staged); err != nil {
		t.Fatalf("rename the replacement over the reserved name: %v", err)
	}
}

// The finding, arranged so it cannot be missed. The barrier stands between the
// stat that proved the identity and the open that would have to prove it
// again, and a file of somebody else's takes the reserved name there. It is
// unreadable in the same broad way, which is the whole trap: the class question
// says yes about a file this rejection has never looked at.
func TestReopenedUnreadableEntryRefusesAStagingSwapOfTheSameClass(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	const unread = "unreadable, unparseable, unwanted"
	const replacement = "somebody else's unreadable words"
	var vault *Vault
	var detached *detachedEntry
	vault = vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase != detachPhaseBeforeReopen {
			return
		}
		swapStagingEntry(t, vault, detached.staging, "keeper.md", replacement)
	})
	full := writeVaultFile(t, vault, "inbox/note.md", unread)
	closeToEveryReader(t, full)
	detached = detachForTest(t, vault, "inbox/note.md")
	identity := detachedIdentity(t, detached)

	objection := detached.objectionToDetachedEntry(
		context.Background(), "", unreadableFailureUnopenable, nil, identity,
	)
	if objection == "" {
		t.Fatal("a rejection authorised the unlink of an entry it never held a descriptor to")
	}
	// The replacement is somebody's file and nothing here may delete it. It is
	// still under the reserved name, and the restore this refusal runs puts it
	// back under the candidate's own name rather than throwing it away.
	inbox := filepath.Join(vault.Root(), InboxDirName)
	if held := closedFileContent(t, filepath.Join(inbox, detached.staging)); held != replacement {
		t.Fatalf("the reserved name holds %q, want the replacement", held)
	}
	if placement := detached.putBack(); !placement.clean() {
		t.Fatalf("the restore did not put the replacement back cleanly: %+v", placement)
	}
	if held := closedFileContent(t, full); held != replacement {
		t.Fatalf("the candidate name holds %q, want the replacement", held)
	}
	// And the candidate itself was not deleted either: it is the entry this
	// rejection was bound to, and it is still exactly where the swap left it.
	if held := closedFileContent(t, filepath.Join(inbox, "keeper.md")); held != unread {
		t.Fatalf("the candidate holds %q, want the proposal nobody could read", held)
	}
}

// The same rule with nothing swapped at all, which is the case the old code
// called proof. Nothing can open the entry on either side; the identity is the
// one the pre-check named; and there is still no descriptor, so there is still
// nothing that says what an unlink would remove.
func TestReopenedUnreadableEntryIsRefusedWhenNothingCanOpenIt(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	closeToEveryReader(t, full)
	detached := detachForTest(t, vault, "inbox/note.md")

	objection := detached.objectionToReopenedUnreadableEntry(
		context.Background(), unreadableFailureUnopenable, detachedIdentity(t, detached),
	)
	if objection == "" {
		t.Fatal("a rejection authorised an unlink on a failed open alone")
	}
	if !strings.Contains(objection, unprovableDetachedEntry) {
		t.Fatalf("the refusal says %q, want it to say nothing here can prove what it would remove", objection)
	}
}

// What the user is told, through the primitive they actually reach. The
// rejection refuses before the file is ever taken off its name — the answer is
// already settled, and a removal with no standing has no business moving the
// user's bytes around to discover it has none — and the sentence says what to
// do about it rather than claiming the file changed.
func TestHashlessRejectionRefusesACandidateNothingCanOpenAndSaysWhat(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	const unread = "unreadable, unparseable, unwanted"
	vault, detached := vaultRefusingBeforeAnyDetach(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unread)
	closeToEveryReader(t, full)
	identity := unhashableIdentity(t, vault, "inbox/note.md")

	err := rejectUnreadable(vault, "inbox/note.md", identity)
	if !errors.Is(err, ErrUnprovableEntry) {
		t.Fatalf("rejection of a proposal nothing can open = %v, want it refused as unprovable", err)
	}
	// Still a refusal every existing caller recognises: the row stays pending
	// and the user is told to look again, which is what the whole rejection
	// path answers with.
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("the refusal = %v, want it to stay a refusal callers already handle", err)
	}
	requireNothingWasDetached(t, detached)
	requireNoStagingResidue(t, vault)
	if got := closedFileContent(t, full); got != unread {
		t.Fatalf("the proposal holds %q, want it left exactly alone", got)
	}
	requireBoundedRefusal(t, err, unread)
	// The one thing the user can do about it. A refusal that does not say this
	// is a dead end with a button beside it.
	if !strings.Contains(err.Error(), "readable") {
		t.Fatalf("the refusal does not say how to get out of it: %v", err)
	}
}

// The door that stays open, and the reason the rule is about descriptors rather
// than about unreadability. A proposal past the size bound opens perfectly
// well: the removal holds a descriptor, the entry it detached answers to it,
// and the user gets rid of the claim they can neither read nor accept.
func TestHashlessRejectionStillRemovesAnOverLimitCandidateItCanOpen(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", strings.Repeat("x", MaxNoteBytes+1))
	identity := unhashableIdentity(t, vault, "inbox/note.md")

	if err := rejectUnreadable(vault, "inbox/note.md", identity); err != nil {
		t.Fatalf("rejection of an over-sized proposal = %v, want it removed", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("the over-sized proposal is still there: %v", err)
	}
	requireNoStagingResidue(t, vault)
}
