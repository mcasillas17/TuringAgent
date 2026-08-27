package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The weak half of the hashless door is the one for a proposal that had no
// bytes to name: nothing could open it, or it was past the size bound, so the
// pre-check came away with an identity and nothing else.
//
// That licence is "this entry, and still unreadable in the same broad way",
// and until now the second half of it was barely asked. A file the pre-check
// could not read at all, which the primitive *can* read, was deleted as long as
// the bytes still would not parse — and those bytes were never read by anybody.
// The pre-check never saw them; the user was shown a failure, not a proposal.
// Deleting them throws away a claim about the user on the strength of an inode
// number.
//
// So the rule is narrow. The rejection may proceed only where the fresh
// confined read under the primitive's own lock fails in the same broad way the
// pre-check failed. Readable is a refusal, malformed or not. A different
// failure class is a refusal too: it is not the file the pre-check answered
// about, or it is not in the state that answer was made in.

// unhashableIdentity is a pre-check that came away with no bytes at all: the
// weak binding, and the only one these tests are about.
func unhashableIdentity(t *testing.T, vault *Vault, relPath string) UnreadableCandidateEntry {
	t.Helper()
	identity := unreadableIdentity(t, vault, relPath)
	if identity.hashed {
		t.Fatalf("the pre-check of %q hashed its bytes; this test needs one that could not read them at all", relPath)
	}
	return identity
}

// vaultRefusingBeforeAnyDetach opens a vault that reports whether the removal
// ever took the file off its name. Every refusal below is one the check before
// the detach owes an answer to: the guard on the far side would catch these
// too, and a guard that is only ever reached through another guard is one the
// next reader deletes. A rejection with no standing has no business moving the
// user's bytes around to discover it has none.
func vaultRefusingBeforeAnyDetach(t *testing.T) (*Vault, *bool) {
	t.Helper()
	detached := false
	vault := vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeDetach {
			detached = true
		}
	})
	return vault, &detached
}

func rejectUnreadable(vault *Vault, relPath string, identity UnreadableCandidateEntry) error {
	return vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:    relPath,
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
}

// requireRefusedAndKept holds the whole shape of a refusal in one place: the
// stale sentence, the bytes still on disk, the inode not moved, and nothing
// left staged. The inode matters because every case here keeps it — identity
// alone would have said yes to all of them.
func requireRefusedAndKept(t *testing.T, vault *Vault, full string, err error, want string, wantDev uint64, wantIno uint64) {
	t.Helper()
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("rejection = %v, want ErrStaleContent", err)
	}
	if got := closedFileContent(t, full); got != want {
		t.Fatalf("the file holds %d bytes, want the %d nobody has read", len(got), len(want))
	}
	dev, ino := entryIdentity(t, full)
	if dev != wantDev || ino != wantIno {
		t.Fatal("the entry moved; these cases are about a file whose inode never changes")
	}
	requireNoStagingResidue(t, vault)
}

// requireRefusalSays holds a refusal to the sentence that names what actually
// happened. "It changed since you read it" is not an answer a user can act on
// when the change is that the file can be read at all now.
func requireRefusalSays(t *testing.T, err error, want string) {
	t.Helper()
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal says %v, want it to say %q", err, want)
	}
}

// requireNothingWasDetached holds a refusal to the near side of the detach.
func requireNothingWasDetached(t *testing.T, detached *bool) {
	t.Helper()
	if *detached {
		t.Fatal("a rejection with no standing still took the file off its name")
	}
}

// The finding. The pre-check found a file past the size bound, so it hashed
// nothing and bound the rejection to identity alone. The user then trimmed it
// in place — same inode — and what is there now can be read. It still will not
// parse, which used to be enough to delete it, and it must not be: nobody has
// ever read those bytes, and there is nothing to compare them against.
func TestBoundHashlessRejectionRefusesAnUnhashableCandidateThatBecameReadable(t *testing.T) {
	vault, detached := vaultRefusingBeforeAnyDetach(t)
	full := writeVaultFile(t, vault, "inbox/note.md", strings.Repeat("x", MaxNoteBytes+1))
	identity := unhashableIdentity(t, vault, "inbox/note.md")
	dev, ino := entryIdentity(t, full)

	const trimmed = "---\nstill: [broken\n---\n\nTrimmed after the pre-check, still unparseable.\n"
	if err := os.WriteFile(full, []byte(trimmed), 0o600); err != nil {
		t.Fatalf("trim the proposal in place: %v", err)
	}

	err := rejectUnreadable(vault, "inbox/note.md", identity)
	requireRefusedAndKept(t, vault, full, err, trimmed, dev, ino)
	requireRefusalSays(t, err, "can be read now")
	requireNothingWasDetached(t, detached)
}

// The same rule reached from the other unhashable case. Nothing could open the
// file at pre-check, so identity was the whole binding; then the user's editor
// wrote different words into it and the permissions came back. Those words are
// readable and nobody has read them.
func TestBoundHashlessRejectionRefusesAnUnopenableCandidateThatBecameReadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	vault, detached := vaultRefusingBeforeAnyDetach(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	closeToEveryReader(t, full)
	identity := unhashableIdentity(t, vault, "inbox/note.md")
	dev, ino := entryIdentity(t, full)

	const rewritten = "---\nstill: [broken\n---\n\nOpened up again after the pre-check.\n"
	if err := os.Chmod(full, 0o600); err != nil {
		t.Fatalf("open the proposal back up: %v", err)
	}
	if err := os.WriteFile(full, []byte(rewritten), 0o600); err != nil {
		t.Fatalf("rewrite the proposal in place: %v", err)
	}

	err := rejectUnreadable(vault, "inbox/note.md", identity)
	requireRefusedAndKept(t, vault, full, err, rewritten, dev, ino)
	requireRefusalSays(t, err, "can be read now")
	requireNothingWasDetached(t, detached)
}

// A different failure class is a different file state, and the licence was for
// the one the pre-check answered about. Nothing could open this candidate; now
// it opens and is too big to read. Whatever is in it arrived after the
// pre-check, and no reader has been through it.
func TestBoundHashlessRejectionRefusesAnUnopenableCandidateThatGrewPastTheBound(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	vault, detached := vaultRefusingBeforeAnyDetach(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	closeToEveryReader(t, full)
	identity := unhashableIdentity(t, vault, "inbox/note.md")
	dev, ino := entryIdentity(t, full)

	grown := strings.Repeat("y", MaxNoteBytes+1)
	if err := os.Chmod(full, 0o600); err != nil {
		t.Fatalf("open the proposal back up: %v", err)
	}
	if err := os.WriteFile(full, []byte(grown), 0o600); err != nil {
		t.Fatalf("grow the proposal in place: %v", err)
	}

	err := rejectUnreadable(vault, "inbox/note.md", identity)
	requireRefusedAndKept(t, vault, full, err, grown, dev, ino)
	requireRefusalSays(t, err, "unreadable in a different way")
	requireNothingWasDetached(t, detached)
}

// And the reverse crossing. The pre-check could open the file and found it past
// the bound; by the time the primitive looked, nothing could open it. Same
// inode, different state, and the identity licence does not stretch across it.
func TestBoundHashlessRejectionRefusesAnOverLimitCandidateNothingCanOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	vault, detached := vaultRefusingBeforeAnyDetach(t)
	full := writeVaultFile(t, vault, "inbox/note.md", strings.Repeat("x", MaxNoteBytes+1))
	identity := unhashableIdentity(t, vault, "inbox/note.md")
	dev, ino := entryIdentity(t, full)

	const closed = "small enough to read, if anything could open it"
	if err := os.WriteFile(full, []byte(closed), 0o600); err != nil {
		t.Fatalf("rewrite the proposal in place: %v", err)
	}
	closeToEveryReader(t, full)

	err := rejectUnreadable(vault, "inbox/note.md", identity)
	requireRefusedAndKept(t, vault, full, err, closed, dev, ino)
	requireRefusalSays(t, err, "unreadable in a different way")
	requireNothingWasDetached(t, detached)
}

// The far side of the detach, where the pre-detach check has already said yes.
// The file was past the bound when it was checked and is trimmed to readable
// bytes while it is off its name. The two guards have to say the same thing:
// bytes that became readable are bytes nobody has read, wherever the removal
// notices it, so this one is put back rather than unlinked.
func TestBoundHashlessRejectionRefusesAnUnhashableCandidateTrimmedInsideTheDetachWindow(t *testing.T) {
	const trimmed = "---\nstill: [broken\n---\n\nTrimmed while the rejection was mid-flight.\n"
	var vault *Vault
	vault = vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase != detachPhaseBeforeDetach {
			return
		}
		full := filepath.Join(vault.Root(), InboxDirName, "note.md")
		if err := os.WriteFile(full, []byte(trimmed), 0o600); err != nil {
			t.Errorf("trim the proposal mid-flight: %v", err)
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", strings.Repeat("x", MaxNoteBytes+1))
	identity := unhashableIdentity(t, vault, "inbox/note.md")
	dev, ino := entryIdentity(t, full)

	err := rejectUnreadable(vault, "inbox/note.md", identity)
	requireRefusedAndKept(t, vault, full, err, trimmed, dev, ino)
	requireRefusalSays(t, err, "can be read now")
}

// The control that keeps the weak door open where it belongs. The file was past
// the bound and it still is, under the same inode. Nobody has read it either
// time and nobody ever could: the rejection is the user's only way out of a
// claim about themselves, and it goes through.
func TestBoundHashlessRejectionRemovesAnOverLimitCandidateStillPastTheBound(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", strings.Repeat("x", MaxNoteBytes+1))
	identity := unhashableIdentity(t, vault, "inbox/note.md")

	if err := os.WriteFile(full, []byte(strings.Repeat("y", MaxNoteBytes+64)), 0o600); err != nil {
		t.Fatalf("grow the proposal further in place: %v", err)
	}
	if err := rejectUnreadable(vault, "inbox/note.md", identity); err != nil {
		t.Fatalf("rejection of an over-sized proposal that is still over-sized = %v, want it removed", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("the over-sized proposal is still there: %v", err)
	}
	requireNoStagingResidue(t, vault)
}
