package memoryfiles

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The vault is a folder Obsidian is watching and the user's own memory. A note
// that reached the page cache but not the disk is a memory the app claims to
// have and does not. These tests assert the flush by inode rather than by call
// count: the staged file that Linkat installs shares the final name's inode, so
// naming the inode proves the bytes under that name were the bytes fsynced, and
// removing either durability step from production fails one of them.

func TestCreateInboxNoteFsyncsTheNoteAndEveryDirectoryAboveIt(t *testing.T) {
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())

	note, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
		Kind:  KindBelief,
		Title: "Prefers dark mode",
		Body:  "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatalf("create inbox note: %v", err)
	}
	created := inodeOf(t, filepath.Join(vault.Root(), filepath.FromSlash(note.RelPath)))
	if !recorder.syncedFile(created) {
		t.Fatal("the staged note was linked into place without ever being fsynced")
	}
	inbox := inodeOf(t, filepath.Join(vault.Root(), InboxDirName))
	if !recorder.syncedDirectory(inbox) {
		t.Fatal("inbox/ was not fsynced, so the new name may not survive a crash")
	}
	root := inodeOf(t, vault.Root())
	if !recorder.syncedDirectory(root) {
		t.Fatal("the vault root was not fsynced, so the hierarchy above the note is not durable")
	}
}

func TestCreateInboxNoteFsyncsEveryDirectoryItHadToCreate(t *testing.T) {
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())

	if _, err := vault.createInboxNoteAt(context.Background(), "inbox/people/miguel/note.md", "content"); err != nil {
		t.Fatalf("create nested inbox note: %v", err)
	}
	for _, relPath := range []string{
		"",
		InboxDirName,
		filepath.Join(InboxDirName, "people"),
		filepath.Join(InboxDirName, "people", "miguel"),
	} {
		directory := inodeOf(t, filepath.Join(vault.Root(), relPath))
		if !recorder.syncedDirectory(directory) {
			t.Fatalf("directory %q was not fsynced; a crash could lose the path to the note", relPath)
		}
	}
	note := inodeOf(t, filepath.Join(vault.Root(), InboxDirName, "people", "miguel", "note.md"))
	if !recorder.syncedFile(note) {
		t.Fatal("the nested note itself was never fsynced")
	}
}

func TestPromoteToBeliefsFsyncsThePromotedNoteAndItsDirectories(t *testing.T) {
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	promoted, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: candidate.RelPath,
		Kind:          KindBelief,
	})
	if err != nil {
		t.Fatalf("promote to beliefs: %v", err)
	}
	installed := inodeOf(t, filepath.Join(vault.Root(), filepath.FromSlash(promoted.RelPath)))
	if !recorder.syncedFile(installed) {
		t.Fatal("the promoted note was linked into beliefs/ without being fsynced")
	}
	for _, relPath := range []string{"", BeliefsDirName, InboxDirName} {
		directory := inodeOf(t, filepath.Join(vault.Root(), relPath))
		if !recorder.syncedDirectory(directory) {
			t.Fatalf("directory %q was not fsynced; the move is not durable in both halves", relPath)
		}
	}
}

// A create that reports failure must leave nothing under the final name. The
// fsync of the parent directory is what makes the new entry durable, so a
// failure there means the name is visible but not committed — and the caller
// has just been told the write did not happen.
func TestCreateInboxNoteLeavesNoFinalNameWhenTheDirectorySyncFails(t *testing.T) {
	recorder := &syncRecorder{failDirectorySyncNumber: 1}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())

	if _, err := vault.createInboxNoteAt(context.Background(), "inbox/note.md", "content"); err == nil {
		t.Fatal("expected the failed directory fsync to fail the create")
	}
	entries, err := os.ReadDir(filepath.Join(vault.Root(), InboxDirName))
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("a create that reported failure left %q behind", entries[0].Name())
	}
}

// The same invariant for the promotion's first half: until the original is
// removed, a promotion that fails has to leave the vault exactly as it was.
func TestPromoteToBeliefsLeavesNothingBehindWhenTheHierarchySyncFails(t *testing.T) {
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")
	sourcePath := filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))

	// Sync 1 is the beliefs folder the copy lands in; sync 2 is the vault root
	// above it, which is where the hierarchy stops being durable.
	recorder.setFailDirectorySyncNumber(2)

	if _, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: candidate.RelPath,
		Mode:          PromoteManagedCandidate,
	}); err == nil {
		t.Fatal("expected the failed hierarchy fsync to fail the promotion")
	}
	if _, err := os.Lstat(sourcePath); err != nil {
		t.Fatalf("the original was disturbed by a failed promotion: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(vault.Root(), BeliefsDirName))
	if err != nil {
		t.Fatalf("read beliefs: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("a promotion that reported failure left %q in beliefs/", entries[0].Name())
	}
}
