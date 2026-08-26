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

// Putting a detached entry back is two names and two flushes, and the order
// between them is the whole of whether a crash can lose the user's file.
//
// The entry is off its name under a reserved staging one. The restore links it
// back and then drops the staging link — and until this round the drop happened
// before anything was flushed. A directory rename and a directory link both
// live in the page cache until an fsync, so a crash between the two left a
// directory in which the restore had never happened *and* the only name the
// bytes had ever had since the detach was gone. The file was reachable from
// nowhere.
//
// The rescue path next door already had the right order — link, flush, drop,
// flush — and these tests hold the restore to the same one, and hold every
// failure in it to being reported rather than swallowed. A refusal that says
// "it was left alone" while a flush failed underneath it is a sentence nobody
// can act on.

// unlinkFailure is the error a test injects into the drop of a staging name.
// It is a plain sentinel: what matters is that it comes back out in the
// placement rather than being lost.
var errStagingUnlink = errors.New("simulated failure dropping the staging name")

// vaultWithRemovalSeams opens a vault whose detach barrier, no-clobber link and
// staging unlink are all under the test's control, over durability hooks the
// test supplies. It is the only way to stand inside the restore and fail one
// step of it.
func vaultWithRemovalSeams(t *testing.T, hooks syncHooks, barrier detachHook, link linkHook, unlink unlinkHook) *Vault {
	t.Helper()
	vault, err := openVaultWithRemovalSeams(newTestVaultRoot(t), hooks, barrier, link, unlink)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return vault
}

// failStagingUnlink fails every drop of a reserved staging name and leaves
// every other unlink alone.
func failStagingUnlink() unlinkHook {
	return func(name string, _ func() error) error {
		if strings.HasPrefix(name, stagingPrefix) {
			return errStagingUnlink
		}
		return nil
	}
}

// rewriteInPlace is the editor that already had the note open: same inode,
// different words. It is what makes a rejection refuse without anybody taking
// the name, which is the only way to reach the restore's own link.
func rewriteInPlace(t *testing.T, vault *Vault, relPath string, content string) {
	t.Helper()
	full := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("rewrite %q in place: %v", relPath, err)
	}
}

// stagingResidueContent answers with what a leftover staging entry holds, so a
// test can prove the bytes are reachable under it rather than merely that a
// name exists.
func stagingResidueContent(t *testing.T, vault *Vault, dir string) (string, string) {
	t.Helper()
	staged := stagingResidueIn(t, vault, dir)
	if len(staged) != 1 {
		t.Fatalf("expected one staging entry in %s/, found %v", dir, staged)
	}
	content, err := os.ReadFile(filepath.Join(vault.Root(), dir, staged[0]))
	if err != nil {
		t.Fatalf("read the staging entry: %v", err)
	}
	return staged[0], string(content)
}

// The first flush. The entry is linked back under its own name and the
// directory will not reach the disk, so nothing is known to have survived a
// crash yet — and the staging name is the one name that might have. It must
// still be there, and the refusal must say so instead of reporting a clean
// restore.
func TestRefusedRejectionKeepsTheStagingNameWhenTheRestoreCannotBeFlushed(t *testing.T) {
	const decided = "the proposal the user read"
	const rewritten = "the words the user typed after they decided"
	recorder := &syncRecorder{}
	var vault *Vault
	vault = vaultWithRemovalSeams(t, recorder.hooks(), func(phase detachPhase, _ string) {
		switch phase {
		case detachPhaseBeforeDetach:
			rewriteInPlace(t, vault, "inbox/note.md", rewritten)
		case detachPhaseBeforeRestore:
			// Armed here rather than counted from the start of the call: the
			// restore's own first flush is the step this test is about, and a
			// number counted from further back is a number that changes every
			// time anything else learns to fsync.
			recorder.setFailDirectorySyncNumber(1)
		}
	}, nil, nil)
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("rejection = %v, want a stale-content refusal", err)
	}
	// Both names, because that is the state the failed flush left behind and
	// the only honest thing to report about it.
	onName, readErr := os.ReadFile(full)
	if readErr != nil || string(onName) != rewritten {
		t.Fatalf("the entry is not back under its own name: %q, %v", onName, readErr)
	}
	staged, held := stagingResidueContent(t, vault, InboxDirName)
	if held != rewritten {
		t.Fatalf("the staging entry holds %q, want the detached bytes", held)
	}
	if drafts := recoveryDrafts(t, vault); len(drafts) != 0 {
		t.Fatalf("a file already under its own name was published a second time as %v", drafts)
	}
	stale := requireBoundedRefusal(t, err, rewritten)
	if !strings.Contains(stale.Detail, staged) {
		t.Fatalf("the refusal does not name the copy it left behind: %q", stale.Detail)
	}
	if !strings.Contains(stale.Detail, "flush") {
		t.Fatalf("the refusal does not say the restore was not flushed: %q", stale.Detail)
	}
	// And a retry decides about what is actually there rather than tripping
	// over the residue: the same rejection is refused again, and one bound to
	// the bytes on disk removes the entry under its own name.
	if again := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	}); !errors.Is(again, ErrStaleContent) {
		t.Fatalf("the retry of a stale rejection = %v, want the same refusal", again)
	}
	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(rewritten),
	}); err != nil {
		t.Fatalf("a rejection bound to the bytes on disk = %v, want it removed", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("the decided proposal is still under its own name: %v", err)
	}
}

// The drop. The entry is durably back under its own name and the staging link
// will not go away, so what is left is a second name for a file the user
// already has — residue, not a rescue. It is named, and it is never published
// as a second proposal: one claim the user can see twice is a claim they have
// to decide about twice.
func TestRefusedRejectionNamesAStagingLinkItCouldNotDrop(t *testing.T) {
	const decided = "the proposal the user read"
	const rewritten = "the words the user typed after they decided"
	var vault *Vault
	vault = vaultWithRemovalSeams(t, realSyncHooks(), func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeDetach {
			rewriteInPlace(t, vault, "inbox/note.md", rewritten)
		}
	}, nil, failStagingUnlink())
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("rejection = %v, want a stale-content refusal", err)
	}
	onName, readErr := os.ReadFile(full)
	if readErr != nil || string(onName) != rewritten {
		t.Fatalf("the entry is not back under its own name: %q, %v", onName, readErr)
	}
	staged, held := stagingResidueContent(t, vault, InboxDirName)
	if held != rewritten {
		t.Fatalf("the staging entry holds %q, want the detached bytes", held)
	}
	if drafts := recoveryDrafts(t, vault); len(drafts) != 0 {
		t.Fatalf("a file already under its own name was published a second time as %v", drafts)
	}
	stale := requireBoundedRefusal(t, err, rewritten)
	if !strings.Contains(stale.Detail, staged) {
		t.Fatalf("the refusal does not name the second link: %q", stale.Detail)
	}
	if !errors.Is(err, errStagingUnlink) {
		t.Fatalf("the refusal does not carry the failure underneath it: %v", err)
	}
}

// The second flush. The staging name is gone from the directory as this process
// sees it, and the removal of it has not reached the disk — so after a crash
// the second link can come back. The restore itself is durable, so this is not
// a refusal to restore, and it is not a duplicate anybody can go and look at
// today either: saying "a second link remains at ..." would send the user after
// a file that is not there. What is true is that the drop was not flushed.
func TestRefusedRejectionReportsAStagingLinkItCouldNotFlushAway(t *testing.T) {
	const decided = "the proposal the user read"
	const rewritten = "the words the user typed after they decided"
	recorder := &syncRecorder{}
	var vault *Vault
	vault = vaultWithRemovalSeams(t, recorder.hooks(), func(phase detachPhase, _ string) {
		switch phase {
		case detachPhaseBeforeDetach:
			rewriteInPlace(t, vault, "inbox/note.md", rewritten)
		case detachPhaseBeforeRestore:
			// The second flush of the restore: the one that would make the
			// dropped staging name stay dropped.
			recorder.setFailDirectorySyncNumber(2)
		}
	}, nil, nil)
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("rejection = %v, want a stale-content refusal", err)
	}
	onName, readErr := os.ReadFile(full)
	if readErr != nil || string(onName) != rewritten {
		t.Fatalf("the entry is not back under its own name: %q, %v", onName, readErr)
	}
	// The drop happened; only its flush did not. A refusal that says the link
	// is still there is pointing at nothing.
	requireNoStagingResidue(t, vault)
	stale := requireBoundedRefusal(t, err, rewritten)
	if !strings.Contains(stale.Detail, stagingPrefix) {
		t.Fatalf("the refusal does not name the link whose removal was not flushed: %q", stale.Detail)
	}
	if strings.Contains(stale.Detail, "could not be dropped") {
		t.Fatalf("the refusal claims a link that was dropped is still there: %q", stale.Detail)
	}
	if !strings.Contains(stale.Detail, "come back") {
		t.Fatalf("the refusal does not say the name can come back after a crash: %q", stale.Detail)
	}
}

// The same distinction on the rescue side. The bytes were moved under a visible
// recovery name, the staging link was dropped, and the flush that would keep it
// dropped failed. The user is told where their file is and that the reserved
// name can reappear — not that there is a copy under it now.
func TestRescuedFileReportsAStagingLinkItCouldNotFlushAway(t *testing.T) {
	const decided = "the proposal the user read"
	const contender = "a third file, under the same name"
	recorder := &syncRecorder{}
	var vault *Vault
	vault = vaultWithRemovalSeams(t, recorder.hooks(), func(phase detachPhase, _ string) {
		switch phase {
		case detachPhaseBeforeDetach:
			rewriteInPlace(t, vault, "inbox/note.md", "the words the user typed after they decided")
		case detachPhaseBeforeRestore:
			contestTheName(t, vault, contender)
			// The rescue links the visible name, flushes, drops the staging
			// name and flushes again; the last of those is this one.
			recorder.setFailDirectorySyncNumber(2)
		}
	}, nil, nil)
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("rejection = %v, want a stale-content refusal", err)
	}
	if held, readErr := os.ReadFile(full); readErr != nil || string(held) != contender {
		t.Fatalf("the file that took the name was disturbed: %q, %v", held, readErr)
	}
	draft, _ := requireOneRecoveryDraft(t, vault)
	requireNoStagingResidue(t, vault)
	stale := requireBoundedRefusal(t, err, decided)
	if !strings.Contains(stale.Detail, draft) {
		t.Fatalf("the refusal does not say where the bytes were kept: %q", stale.Detail)
	}
	if strings.Contains(stale.Detail, "a copy remains") {
		t.Fatalf("the refusal claims a copy that was dropped is still there: %q", stale.Detail)
	}
	if !strings.Contains(stale.Detail, "come back") {
		t.Fatalf("the refusal does not say the reserved name can come back: %q", stale.Detail)
	}
}

// The same shared restore, reached through the promotion's own removal of the
// original it moved. A promotion that will not delete its source has to leave
// that source somewhere the user can find, and a flush it could not do is part
// of what it owes them.
func TestPromotedSourceKeptUnderBothNamesWhenTheRestoreCannotBeFlushed(t *testing.T) {
	recorder := &syncRecorder{}
	var vault *Vault
	var rewritten bool
	vault = vaultWithRemovalSeams(t, recorder.hooks(), func(phase detachPhase, clean string) {
		if !inInbox(clean) {
			return
		}
		switch {
		case phase == detachPhaseBeforeDetach && !rewritten:
			rewritten = true
			rewriteInPlace(t, vault, clean, "the words the user typed after Turing read it")
		case phase == detachPhaseBeforeRestore:
			recorder.setFailDirectorySyncNumber(1)
		}
	}, nil, nil)
	candidate := seedBelief(t, vault)
	sourcePath := filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))

	err := promoteCandidate(context.Background(), vault, candidate)
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("promotion = %v, want it abandoned", err)
	}
	if _, readErr := os.ReadFile(sourcePath); readErr != nil {
		t.Fatalf("the original is not back under its own name: %v", readErr)
	}
	staged, _ := stagingResidueContent(t, vault, InboxDirName)
	if !strings.Contains(err.Error(), staged) {
		t.Fatalf("the refusal does not name the copy it left behind: %v", err)
	}
}

// And through the install rollback, where the entry being put back is one this
// vault wrote a moment ago and could not commit. The rule does not change with
// the caller: nothing is dropped before the link that replaces it is on disk.
func TestInstallRollbackKeepsTheStagingNameWhenTheRestoreCannotBeFlushed(t *testing.T) {
	const existing = "a belief the user already had"
	var vault *Vault
	vault = vaultWithRemovalSeams(t, realSyncHooks(), func(phase detachPhase, clean string) {
		if phase == detachPhaseBeforeDetach && inBeliefs(clean) {
			rewriteInPlace(t, vault, clean, "the words the user typed while the install was in flight")
		}
	}, nil, failStagingUnlink())
	writeVaultFile(t, vault, "beliefs/kept.md", existing)

	parent, err := os.Open(filepath.Join(vault.Root(), BeliefsDirName))
	if err != nil {
		t.Fatalf("open beliefs: %v", err)
	}
	defer func() { _ = parent.Close() }()

	// An install that cannot be committed undoes itself, and the undo is the
	// same detach-verify-restore. Here the entry has been rewritten in place,
	// so the undo refuses — and the staging link it cannot drop is residue it
	// has to name rather than a file to publish.
	full := filepath.Join(vault.Root(), BeliefsDirName, "installed.md")
	if err := os.WriteFile(full, []byte(existing), 0o600); err != nil {
		t.Fatalf("write the installed copy: %v", err)
	}
	var installed unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), "installed.md", &installed, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		t.Fatalf("inspect the installed copy: %v", err)
	}
	undoErr := vault.undoInstall(parent, "installed.md", "beliefs/installed.md", installed, existing)
	if undoErr == nil {
		t.Fatal("the undo removed an entry it could not prove it installed")
	}
	staged, _ := stagingResidueContent(t, vault, BeliefsDirName)
	if !strings.Contains(undoErr.Error(), staged) {
		t.Fatalf("the undo does not name the second link it left behind: %v", undoErr)
	}
	if _, readErr := os.ReadFile(full); readErr != nil {
		t.Fatalf("the entry is not back under its own name: %v", readErr)
	}
}

// The flush that keeps a preserved entry findable. When the restore cannot drop
// its staging link the bytes stay under two names, and the directory that holds
// both of them has to reach the disk — or the caller has to be told it did not.
// A rollback that swallows that fsync is one whose "it is at ..." is a guess.
func TestInstallRollbackReportsAPreservedEntryItCouldNotFlush(t *testing.T) {
	const existing = "a belief the user already had"
	recorder := &syncRecorder{}
	var vault *Vault
	vault = vaultWithRemovalSeams(t, recorder.hooks(), func(phase detachPhase, clean string) {
		if !inBeliefs(clean) {
			return
		}
		switch phase {
		case detachPhaseBeforeDetach:
			rewriteInPlace(t, vault, clean, "the words the user typed while the install was in flight")
		case detachPhaseBeforeRestore:
			// The restore links the entry back and flushes; the drop then
			// fails, and the flush that answers for where the bytes are is the
			// second one.
			recorder.setFailDirectorySyncNumber(2)
		}
	}, nil, failStagingUnlink())

	parent, err := os.Open(filepath.Join(vault.Root(), BeliefsDirName))
	if err != nil {
		t.Fatalf("open beliefs: %v", err)
	}
	defer func() { _ = parent.Close() }()

	full := filepath.Join(vault.Root(), BeliefsDirName, "installed.md")
	if err := os.WriteFile(full, []byte(existing), 0o600); err != nil {
		t.Fatalf("write the installed copy: %v", err)
	}
	var installed unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), "installed.md", &installed, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		t.Fatalf("inspect the installed copy: %v", err)
	}

	undoErr := vault.undoInstall(parent, "installed.md", "beliefs/installed.md", installed, existing)
	if undoErr == nil {
		t.Fatal("the undo removed an entry it could not prove it installed")
	}
	if !strings.Contains(undoErr.Error(), "flush") {
		t.Fatalf("the undo does not say a flush failed underneath it: %v", undoErr)
	}
	if _, readErr := os.ReadFile(full); readErr != nil {
		t.Fatalf("the entry is not back under its own name: %v", readErr)
	}
}

// The flush underneath a preserved entry, which is the one placement that never
// gets a second chance: the name is taken, the bytes are staying under the
// reserved name, and if the directory holding it does not reach the disk then
// "it is at ..." is a guess. A rollback that swallows that fsync tells the user
// where their file is on the strength of nothing.
func TestInstallRollbackReportsAContestedEntryItCouldNotFlush(t *testing.T) {
	const existing = "a belief the user already had"
	const contender = "a belief another writer put under the name"
	recorder := &syncRecorder{}
	var vault *Vault
	vault = vaultWithRemovalSeams(t, recorder.hooks(), func(phase detachPhase, clean string) {
		if !inBeliefs(clean) {
			return
		}
		switch phase {
		case detachPhaseBeforeDetach:
			rewriteInPlace(t, vault, clean, "the words the user typed while the install was in flight")
		case detachPhaseBeforeRestore:
			takeTheName(t, vault, clean, contender)
			// The link is about to fail, so the only flush this restore makes
			// is the one that answers for where the bytes were left.
			recorder.setFailDirectorySyncNumber(1)
		}
	}, nil, nil)

	parent, err := os.Open(filepath.Join(vault.Root(), BeliefsDirName))
	if err != nil {
		t.Fatalf("open beliefs: %v", err)
	}
	defer func() { _ = parent.Close() }()

	full := filepath.Join(vault.Root(), BeliefsDirName, "installed.md")
	if err := os.WriteFile(full, []byte(existing), 0o600); err != nil {
		t.Fatalf("write the installed copy: %v", err)
	}
	var installed unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), "installed.md", &installed, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		t.Fatalf("inspect the installed copy: %v", err)
	}

	undoErr := vault.undoInstall(parent, "installed.md", "beliefs/installed.md", installed, existing)
	if undoErr == nil {
		t.Fatal("the undo removed an entry it could not prove it installed")
	}
	staged, held := stagingResidueContent(t, vault, BeliefsDirName)
	if !strings.Contains(undoErr.Error(), staged) {
		t.Fatalf("the undo does not name where it left the bytes: %v", undoErr)
	}
	if !strings.Contains(undoErr.Error(), "flush") {
		t.Fatalf("the undo does not say the placement was never flushed: %v", undoErr)
	}
	if held == "" {
		t.Fatal("the reserved name holds nothing")
	}
	if onName, readErr := os.ReadFile(full); readErr != nil || string(onName) != contender {
		t.Fatalf("the file that took the name was disturbed: %q, %v", onName, readErr)
	}
	// Nothing under beliefs/ was published for a file this rollback could not
	// prove was its own: an indexed note there is a belief the user is told
	// Turing holds.
	for _, name := range vaultDirEntries(t, vault, BeliefsDirName) {
		if !strings.HasPrefix(name, stagingPrefix) && name != "installed.md" {
			t.Fatalf("the rollback published %q under beliefs/", name)
		}
	}
}

// The deletion that was authorised and then could not finish. The entry is off
// its name, the unlink of the staging name fails, and the flush that would at
// least pin the directory as it stands fails too. Nothing is lost — the bytes
// are under the reserved name — and the caller has to be told both that the
// removal did not happen and that where the file is now is not something a
// crash is guaranteed to keep.
func TestRefusedDiscardReportsBothTheUnlinkAndTheFlushItCouldNotDo(t *testing.T) {
	const decided = "the proposal the user read"
	recorder := &syncRecorder{}
	vault := vaultWithRemovalSeams(t, recorder.hooks(), func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeVerify {
			// The verify is about to pass, so the next directory flush is the
			// one the discard makes after its failed unlink.
			recorder.setFailDirectorySyncNumber(1)
		}
	}, nil, failStagingUnlink())
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if err == nil {
		t.Fatal("a rejection whose unlink failed reported itself as a removal")
	}
	if !errors.Is(err, errStagingUnlink) {
		t.Fatalf("the failure does not carry the unlink underneath it: %v", err)
	}
	if !strings.Contains(err.Error(), "flushed") {
		t.Fatalf("the failure does not say the directory could not be flushed either: %v", err)
	}
	staged, held := stagingResidueContent(t, vault, InboxDirName)
	if held != decided {
		t.Fatalf("the staging entry holds %q, want the proposal", held)
	}
	if !strings.Contains(err.Error(), staged) {
		t.Fatalf("the failure does not say where the bytes are: %v", err)
	}
	if _, statErr := os.Lstat(full); !os.IsNotExist(statErr) {
		t.Fatalf("the entry is under its own name after a detach that never went back: %v", statErr)
	}
}
