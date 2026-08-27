package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A removal that has proved what it is about to delete is entitled to delete
// it. When the unlink itself fails, the entry is already off its name, under a
// reserved private name nothing outside this package knows — and every record
// that says somebody is answerable for those bytes names the *original* path.
//
// Leaving it there is how a file becomes an orphan nothing can ever reach: the
// manifest names a path that holds nothing, the retry finds nothing and reports
// the withdrawal complete, and the bytes sit under a hidden name in the user's
// vault forever. So a failed discard puts the entry back where the records say
// it is, without clobbering whatever may have taken the name, and says where
// everything ended up.

// TestFailedDiscardPutsTheEntryBackUnderItsOwnName holds the first pass to
// leaving the file findable, and the retry to removing it.
func TestFailedDiscardPutsTheEntryBackUnderItsOwnName(t *testing.T) {
	const decided = "the proposal this session wrote"
	failUnlink := true
	vault, err := openVaultWithRemovalSeams(
		newTestVaultRoot(t), realSyncHooks(), nil, nil,
		func(name string, unlink func() error) error {
			if failUnlink && strings.HasPrefix(name, stagingPrefix) {
				return errStagingUnlink
			}
			return unlink()
		},
	)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	removeErr := vault.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", decided))
	if removeErr == nil {
		t.Fatal("a removal whose unlink failed reported success")
	}
	if !errors.Is(removeErr, errStagingUnlink) {
		t.Fatalf("removal error = %v, want the unlink failure it carries", removeErr)
	}
	// Back under its own name, with the bytes the caller was entitled to
	// delete: that is what makes the retry able to prove ownership again.
	content, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("the entry was not put back under its own name: %v", err)
	}
	if string(content) != decided {
		t.Fatalf("restored content = %q, want the bytes the removal verified", content)
	}
	if !strings.Contains(removeErr.Error(), "back under its own name") {
		t.Fatalf("the refusal does not say the entry was put back: %v", removeErr)
	}
	// The staging link could not be dropped either — it is the same unlink —
	// so it is still there and the sentence has to name it rather than claim a
	// tidy restore.
	staged := stagingResidueIn(t, vault, InboxDirName)
	if len(staged) != 1 {
		t.Fatalf("staging residue = %v, want the link the failed drop left", staged)
	}
	if !strings.Contains(removeErr.Error(), staged[0]) {
		t.Fatalf("the refusal does not name the staging residue %q: %v", staged[0], removeErr)
	}

	// The retry is the point of all of it: the file is where the record says
	// it is, so ownership can be proved again and the removal can finish.
	failUnlink = false
	if err := vault.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", decided)); err != nil {
		t.Fatalf("the retry could not remove the restored entry: %v", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("the retry left the note behind: %v", err)
	}
	// The link the first pass could not drop is still exactly the one it
	// named, and no second one was invented. A name somebody was told about is
	// a name they can go and remove; the thing this whole path exists to
	// prevent is bytes under a name nobody was ever told about.
	after := stagingResidueIn(t, vault, InboxDirName)
	if len(after) != 1 || after[0] != staged[0] {
		t.Fatalf("staging residue after the retry = %v, want only the link the failure named (%q)", after, staged[0])
	}
}

// TestFailedDiscardKeepsTheEntryStagedWhenItsNameIsTaken holds the contested
// case to naming both places rather than publishing a decided proposal back
// into the inbox as a fresh draft.
func TestFailedDiscardKeepsTheEntryStagedWhenItsNameIsTaken(t *testing.T) {
	const decided = "the proposal this session wrote"
	const somebodyElses = "a file the user saved under that name"
	var vault *Vault
	vault, err := openVaultWithRemovalSeams(
		newTestVaultRoot(t), realSyncHooks(),
		func(phase detachPhase, _ string) {
			if phase == detachPhaseBeforeVerify {
				// The name is free the moment the detach vacates it, and this
				// is what an editor or a sync client does with it.
				writeVaultFile(t, vault, "inbox/note.md", somebodyElses)
			}
		},
		nil,
		func(name string, unlink func() error) error {
			if strings.HasPrefix(name, stagingPrefix) {
				return errStagingUnlink
			}
			return unlink()
		},
	)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	removeErr := vault.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", decided))
	if removeErr == nil {
		t.Fatal("a removal whose unlink failed reported success")
	}
	// The other writer's file is untouched. Nothing on this path may clobber a
	// name it does not hold.
	content, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read the contested name: %v", err)
	}
	if string(content) != somebodyElses {
		t.Fatalf("the contested name holds %q, want the other writer's file", content)
	}
	staged := stagingResidueIn(t, vault, InboxDirName)
	if len(staged) != 1 {
		t.Fatalf("staging residue = %v, want the bytes kept under the reserved name", staged)
	}
	kept, err := os.ReadFile(filepath.Join(vault.Root(), InboxDirName, staged[0]))
	if err != nil {
		t.Fatalf("read the kept entry: %v", err)
	}
	if string(kept) != decided {
		t.Fatalf("kept content = %q, want the bytes the removal verified", kept)
	}
	if !strings.Contains(removeErr.Error(), staged[0]) {
		t.Fatalf("the refusal does not name where the bytes are: %v", removeErr)
	}
	// A decided proposal must not come back as a visible draft: the user has
	// already answered it, and a second copy under a name the walk indexes is
	// the same claim asked again.
	if drafts := recoveryDrafts(t, vault); len(drafts) != 0 {
		t.Fatalf("a decided proposal was republished as %v", drafts)
	}
}

// An unlink that reached the directory but was never flushed is not an absence
// a crash will honour. The pass that fails there reports it and keeps whatever
// record names the file — and the *next* pass, which finds nothing under the
// name, must not retire that record on the strength of a page-cache absence.
//
// So "it is not there" is only ever reported once the directory holding the
// name has reached the disk.
func TestRemovalProvesAnAbsenceIsDurableBeforeReportingIt(t *testing.T) {
	const decided = "the proposal this session wrote"
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	// First pass: the unlink happens, the flush that would make it durable
	// does not. The name is gone in this process and the failure is reported.
	recorder.setFailDirectorySyncNumber(1)
	first := vault.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", decided))
	if first == nil {
		t.Fatal("a removal whose post-unlink flush failed reported success")
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("the unlink did not happen: %v", err)
	}

	// Second pass: nothing is under the name, and the flush still fails. The
	// absence is exactly as unproven as it was, so it must not be reported as
	// the outcome the caller asked for.
	recorder.setFailDirectorySyncNumber(1)
	second := vault.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", decided))
	if second == nil {
		t.Fatal("a missing entry was reported gone without the directory reaching the disk")
	}

	// Third pass: the flush works, so the absence is now a fact a crash cannot
	// undo, and the record that named the file may finally be retired.
	recorder.setFailDirectorySyncNumber(0)
	if err := vault.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", decided)); err != nil {
		t.Fatalf("a durable absence was not reported as one: %v", err)
	}
	if !recorder.syncedDirectory(directoryInode(t, vault, InboxDirName)) {
		t.Fatal("the directory that would hold the missing entry was never flushed")
	}
}

// The same rule, asked of the answer the manifest actually consults: a cleaner
// that retires a row because the path holds nothing needs that nothing to have
// reached the disk.
func TestConfirmInboxNoteAbsentRequiresTheDirectoryToReachTheDisk(t *testing.T) {
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())

	writeVaultFile(t, vault, "inbox/here.md", "a proposal that is still there")
	absent, err := vault.ConfirmInboxNoteAbsent(context.Background(), "inbox/here.md")
	if err != nil {
		t.Fatalf("ConfirmInboxNoteAbsent: %v", err)
	}
	if absent {
		t.Fatal("a file that is in the inbox was reported absent")
	}

	recorder.setFailDirectorySyncNumber(1)
	absent, err = vault.ConfirmInboxNoteAbsent(context.Background(), "inbox/gone.md")
	if err == nil {
		t.Fatal("an unflushed absence was reported without its failure")
	}
	if absent {
		t.Fatal("an absence nobody flushed was reported as proven")
	}

	recorder.setFailDirectorySyncNumber(0)
	absent, err = vault.ConfirmInboxNoteAbsent(context.Background(), "inbox/gone.md")
	if err != nil {
		t.Fatalf("ConfirmInboxNoteAbsent: %v", err)
	}
	if !absent {
		t.Fatal("a durable absence was not reported as one")
	}
	if _, err := vault.ConfirmInboxNoteAbsent(context.Background(), "beliefs/note.md"); err == nil {
		t.Fatal("a path outside the inbox was answered rather than refused")
	}
}
