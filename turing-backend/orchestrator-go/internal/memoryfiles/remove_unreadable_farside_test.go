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
// broad refusal, and now a descriptor to ask it through: a failed open answers
// neither question, and the round after this one closed that last gap. Nothing
// here is authorised by a second failure to open.
//
// These are exercised directly against a detached entry. The primitive above
// refuses a hashless rejection it cannot open before anything leaves its name,
// so this side is a guard rather than a road — and a guard that is only ever
// reached through another guard is one the next reader deletes.

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
func TestReopenedUnreadableEntryRefusesACandidateOpenedInsideTheDetachWindow(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	const unread = "unreadable, unparseable, unwanted"
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unread)
	closeToEveryReader(t, full)
	detached := detachForTest(t, vault, "inbox/note.md")
	identity := detachedIdentity(t, detached)

	// Off its name and under the reserved one: the entry the far side has to
	// go and look at, rather than the name it used to be under.
	staged := filepath.Join(vault.Root(), InboxDirName, stagingNameIn(t, vault, InboxDirName))
	if err := os.Chmod(staged, 0o600); err != nil {
		t.Fatalf("open the proposal back up mid-flight: %v", err)
	}

	objection := detached.objectionToReopenedUnreadableEntry(
		context.Background(), unreadableFailureUnopenable, identity,
	)
	if !strings.Contains(objection, "can be read now") {
		t.Fatalf("the reopened entry says %q, want the state answer the reopen exists to ask", objection)
	}
	if held, readErr := os.ReadFile(staged); readErr != nil || string(held) != unread {
		t.Fatalf("the detached proposal holds %q, want it untouched (%v)", held, readErr)
	}
}

// The same window, the other way the state can move: the file opens again and
// what is behind it is past the bound every reader here works under. That is a
// different refusal from the one the pre-check answered about, so it is a
// different file state, and the licence does not stretch to it.
func TestReopenedUnreadableEntryRefusesACandidateThatGrewInsideTheDetachWindow(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	grown := strings.Repeat("y", MaxNoteBytes+1)
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "unreadable, unparseable, unwanted")
	closeToEveryReader(t, full)
	detached := detachForTest(t, vault, "inbox/note.md")
	identity := detachedIdentity(t, detached)

	staged := filepath.Join(vault.Root(), InboxDirName, stagingNameIn(t, vault, InboxDirName))
	if err := os.Chmod(staged, 0o600); err != nil {
		t.Fatalf("open the proposal back up mid-flight: %v", err)
	}
	if err := os.WriteFile(staged, []byte(grown), 0o600); err != nil {
		t.Fatalf("grow the proposal mid-flight: %v", err)
	}

	objection := detached.objectionToReopenedUnreadableEntry(
		context.Background(), unreadableFailureUnopenable, identity,
	)
	if !strings.Contains(objection, "unreadable in a different way") {
		t.Fatalf("the reopened entry says %q, want it refused as a different state", objection)
	}
}

// The far side reopens the entry it detached, not the name it detached it from.
// A replacement that lands on the name while the rejection is mid-flight is a
// file this removal never looked at, and reading *it* to decide about the
// detached entry would be the same mistake in a new place: the answer must come
// from the reserved name, and the replacement must be left exactly as it is.
func TestReopenedUnreadableEntryAnswersAboutItselfAndNotTheNameItLeft(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no file is unreadable")
	}
	const arrival = "a readable proposal that landed on the name"
	const unread = "unreadable, unparseable, unwanted"
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", unread)
	closeToEveryReader(t, full)
	detached := detachForTest(t, vault, "inbox/note.md")
	identity := detachedIdentity(t, detached)
	takeTheName(t, vault, "inbox/note.md", arrival)

	objection := detached.objectionToReopenedUnreadableEntry(
		context.Background(), unreadableFailureUnopenable, identity,
	)
	// The arrival is readable, so an answer drawn from the name would be "it
	// can be read now". The detached entry is not readable and never became
	// so: what is true of it is that nothing here can open it.
	if !strings.Contains(objection, unprovableDetachedEntry) {
		t.Fatalf("the far side says %q, want the answer about the entry it detached", objection)
	}
	if held, readErr := os.ReadFile(full); readErr != nil || string(held) != arrival {
		t.Fatalf("the file that took the name was disturbed: %q, %v", held, readErr)
	}
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

// A cancelled request that arrives while the candidate is off its name is still
// a request that ended. It must not be read as "the file refuses to be read in
// a new way" — that is a claim about the vault — and it must not walk into the
// unlink either.
//
// The candidate is one past the size bound rather than one nothing can open,
// because that is the hashless binding that still reaches the detach: a
// proposal nothing can open is refused before anything leaves its name.
func TestHashlessRejectionCancelledInsideTheDetachWindowReportsTheCancellation(t *testing.T) {
	unread := strings.Repeat("x", MaxNoteBytes+1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	vault := vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeVerify {
			cancel()
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", unread)
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
		t.Fatalf("the proposal holds %d bytes, want the %d it was left with", len(got), len(unread))
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
