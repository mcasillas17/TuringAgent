package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The weakest door in this package is the rejection of a proposal nothing could
// open: no bytes were ever read, so the binding is the entry's identity plus
// the fact that it goes on refusing to be read. The near side asks both. The
// far side — after the entry has been taken off its name and before it is
// unlinked — asked only the first, because the descriptor it re-reads through
// is the one the pre-check opened, and in this one case there is no descriptor
// at all.
//
// So "nothing to re-read" was being treated as "nothing to check", and the
// window between the near-side answer and the unlink was the one place where a
// file that had just become readable could be deleted with nobody having read a
// word of it. Permissions coming back is not an exotic event: it is a sync
// client finishing a copy, an editor rewriting a file it had locked, or the
// user themselves running chmod after Turing told them it could not read their
// proposal.
//
// The entry is still reachable while it is off its name — under the reserved
// staging name, through the same confined parent descriptor — so the far side
// opens *that* and asks the same question again. Same identity and the same
// broad refusal, or the file goes back.

// stagingNameIn answers with the single reserved name a detach has left in a
// vault directory, so a test can reach an entry while it is off its name.
func stagingNameIn(t *testing.T, vault *Vault, dir string) string {
	t.Helper()
	staged := stagingResidueIn(t, vault, dir)
	if len(staged) != 1 {
		t.Fatalf("expected one entry off its name in %s/, found %v", dir, staged)
	}
	return staged[0]
}

// The finding, at its plainest. Nothing could open the proposal when it was
// rejected; the permissions come back while it is off its name, under the same
// inode and holding the same bytes it always held. Nobody has read those bytes
// — the pre-check could not — so there is nothing to delete them on the
// authority of.
func TestHashlessRejectionRefusesAnUnopenableCandidateOpenedInsideTheDetachWindow(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	const unread = "unreadable, unparseable, unwanted"
	var vault *Vault
	vault = vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase != detachPhaseBeforeVerify {
			return
		}
		// Off its name and under the reserved one: the entry the far side has
		// to go and look at, rather than the name it used to be under.
		staged := stagingNameIn(t, vault, InboxDirName)
		if err := os.Chmod(filepath.Join(vault.Root(), InboxDirName, staged), 0o600); err != nil {
			t.Errorf("open the proposal back up mid-flight: %v", err)
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", unread)
	closeToEveryReader(t, full)
	identity := unhashableIdentity(t, vault, "inbox/note.md")
	dev, ino := entryIdentity(t, full)

	err := rejectUnreadable(vault, "inbox/note.md", identity)
	requireRefusedAndKept(t, vault, full, err, unread, dev, ino)
	requireRefusalSays(t, err, "can be read now")
}

// The same window, the other way the state can move: the file opens again and
// what is behind it is past the bound every reader here works under. That is a
// different refusal from the one the pre-check answered about, so it is a
// different file state, and the licence does not stretch to it.
func TestHashlessRejectionRefusesAnUnopenableCandidateThatGrewInsideTheDetachWindow(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	grown := strings.Repeat("y", MaxNoteBytes+1)
	var vault *Vault
	vault = vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase != detachPhaseBeforeVerify {
			return
		}
		staged := filepath.Join(vault.Root(), InboxDirName, stagingNameIn(t, vault, InboxDirName))
		if err := os.Chmod(staged, 0o600); err != nil {
			t.Errorf("open the proposal back up mid-flight: %v", err)
			return
		}
		if err := os.WriteFile(staged, []byte(grown), 0o600); err != nil {
			t.Errorf("grow the proposal mid-flight: %v", err)
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	closeToEveryReader(t, full)
	identity := unhashableIdentity(t, vault, "inbox/note.md")
	dev, ino := entryIdentity(t, full)

	err := rejectUnreadable(vault, "inbox/note.md", identity)
	requireRefusedAndKept(t, vault, full, err, grown, dev, ino)
	requireRefusalSays(t, err, "unreadable in a different way")
}

// And the control that keeps the door open where it belongs. Nothing could open
// the proposal at the pre-check, nothing can open it now, and it is the same
// entry: the user's only way out of a claim about themselves they can neither
// read nor accept, and it goes through.
func TestHashlessRejectionStillRemovesAnEntryThatWillNotOpenOnEitherSide(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	closeToEveryReader(t, full)
	identity := unhashableIdentity(t, vault, "inbox/note.md")

	if err := rejectUnreadable(vault, "inbox/note.md", identity); err != nil {
		t.Fatalf("rejection of a proposal nothing can open = %v, want it removed", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("the unopenable proposal is still there: %v", err)
	}
	requireNoStagingResidue(t, vault)
}

// The far side reopens the entry it detached, not the name it detached it from.
// A replacement that lands on the name while the rejection is mid-flight is a
// file this removal never looked at, and reading *it* to decide about the
// detached entry would be the same mistake in a new place: the answer must come
// from the reserved name, and the replacement must be left exactly as it is.
func TestHashlessRejectionReclassifiesTheDetachedEntryAndNotTheNameItLeft(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	const arrival = "a readable proposal that landed on the name"
	const unread = "unreadable, unparseable, unwanted"
	var vault *Vault
	vault = vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase != detachPhaseBeforeVerify {
			return
		}
		takeTheName(t, vault, "inbox/note.md", arrival)
	})
	full := writeVaultFile(t, vault, "inbox/note.md", unread)
	closeToEveryReader(t, full)
	identity := unhashableIdentity(t, vault, "inbox/note.md")

	if err := rejectUnreadable(vault, "inbox/note.md", identity); err != nil {
		t.Fatalf("rejection = %v, want the entry it was bound to removed", err)
	}
	held, readErr := os.ReadFile(full)
	if readErr != nil || string(held) != arrival {
		t.Fatalf("the file that took the name was disturbed: %q, %v", held, readErr)
	}
	requireNoStagingResidue(t, vault)
}

// The far side is a check of its own, reachable on its own terms. A detached
// entry whose identity is not the one the removal was bound to is refused even
// when it refuses to be read in exactly the same way — identity and state are
// two questions, and answering one of them twice is not answering both.
func TestDetachedUnreadableEntryOfAnotherIdentityIsRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	const replacement = "a different file nobody can open either"
	var vault *Vault
	vault = vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase != detachPhaseBeforeDetach {
			return
		}
		replaceInboxEntry(t, vault, "inbox/note.md", replacement)
		closeToEveryReader(t, filepath.Join(vault.Root(), InboxDirName, "note.md"))
	})
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	closeToEveryReader(t, full)
	identity := unhashableIdentity(t, vault, "inbox/note.md")

	err := rejectUnreadable(vault, "inbox/note.md", identity)
	if err == nil {
		t.Fatal("a hashless rejection deleted an entry that was not the one it was bound to")
	}
	requireRefusalSays(t, err, "taken its name")
	if got := closedFileContent(t, full); got != replacement {
		t.Fatalf("the replacement holds %q, want it untouched", got)
	}
	requireNoStagingResidue(t, vault)
}

// A cancelled request that arrives while the far side is reclassifying is still
// a request that ended. It must not be read as "the file refuses to be read in
// a new way" — that is a claim about the vault — and it must not walk into the
// unlink either.
func TestHashlessRejectionCancelledWhileReclassifyingReportsTheCancellation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	const unread = "unreadable, unparseable, unwanted"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	vault := vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeVerify {
			cancel()
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", unread)
	closeToEveryReader(t, full)
	identity := unhashableIdentity(t, vault, "inbox/note.md")

	err := vault.RemoveInboxNote(ctx, RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled hashless rejection = %v, want the cancellation reported", err)
	}
	if got := closedFileContent(t, full); got != unread {
		t.Fatalf("the proposal holds %q, want it left alone", got)
	}
	requireNoStagingResidue(t, vault)
}

// The reopen is a second look at the directory, so it asks the identity
// question again rather than inheriting the answer from the stat above it. The
// reserved name is random but it is not secret — anything that can list the
// vault directory can read it — so an entry that has been swapped under it
// between the stat and the open is a file this rejection never looked at.
//
// It is exercised directly: the two moments are consecutive statements in the
// production path, and a check that can only be reached by winning a race is a
// check the next reader deletes. What this pins is the order — identity before
// state — because the entry here is perfectly readable, and answering "it can
// be read now" would be answering about somebody else's file.
func TestReopenedUnreadableEntryIsCheckedForIdentityBeforeState(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/note.md", "a proposal anybody can read")
	detached := detachForTest(t, vault, "inbox/note.md")
	moved := detachedIdentity(t, detached)
	moved.Ino++

	objection := detached.objectionToReopenedUnreadableEntry(
		context.Background(), unreadableFailureUnopenable, moved,
	)
	if !strings.Contains(objection, "taken its name") {
		t.Fatalf("the reopened entry was judged on its state rather than its identity: %q", objection)
	}
}

// A read that stops because the request behind it ended says nothing about the
// file. Reporting it as "unreadable in a different way" would turn a
// cancellation into a claim that something wrote to the user's proposal, and
// the caller would be told to go and look at a change that never happened.
//
// The predicate is exercised on its own because the primitive refuses a
// cancelled request before it ever gets here; this is the guard for a context
// that ends *during* the far-side read, which no test can schedule from
// outside.
func TestUnreadableStateObjectionReportsAnEndedRequestAsItself(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := unreadableStateObjection(ctx, unreadableFailureUnopenable, nil); got != endedBeforeVerification {
		t.Fatalf("a cancelled verification says %q, want it reported as the request ending", got)
	}
	if got := unreadableStateObjection(ctx, unreadableFailureUnopenable, os.ErrPermission); got != endedBeforeVerification {
		t.Fatalf("a cancelled verification says %q, want it reported as the request ending", got)
	}
}
