package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rewriteKeepingVaultMetadata replaces a note's bytes while leaving the two
// things the metadata cache keys on — modification time and length — exactly as
// they were. It is the one edit a (mtime, size) cache cannot see, and the plan
// names it as an accepted residual rather than pretending it does not exist.
func rewriteKeepingVaultMetadata(t *testing.T, root string, relPath string, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	before, err := os.Stat(full)
	if err != nil {
		t.Fatalf("stat %q: %v", relPath, err)
	}
	if int64(len(content)) != before.Size() {
		t.Fatalf("this edit changes the length (%d -> %d), so it is not the residual case", before.Size(), len(content))
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("rewrite %q: %v", relPath, err)
	}
	if err := os.Chtimes(full, before.ModTime(), before.ModTime()); err != nil {
		t.Fatalf("restore times on %q: %v", relPath, err)
	}
}

// A pass over an unchanged vault must not open the user's notes again.
//
// The seam is the filesystem itself: a note nobody can read is a note whose
// bytes a pass cannot have looked at. If the second pass reports no trouble
// with it and keeps serving its text, the only place that text can have come
// from is the cache the first pass filled.
func TestRefreshMemoryIndexDoesNotRereadANoteWhoseBytesHaveNotChanged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode says, so there is no seam here")
	}
	repo, vault, _ := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	first, found := noteRowFor(t, repo, noteID)
	if !found {
		t.Fatal("the first pass did not index the belief")
	}

	full := filepath.Join(vault.Root(), "beliefs", "note.md")
	if err := os.Chmod(full, 0o000); err != nil {
		t.Fatalf("seal the note: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(full, 0o600) })

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(report.Errors) != 0 {
		t.Fatalf("the second pass opened the note again: %v", report.Errors)
	}
	second, found := noteRowFor(t, repo, noteID)
	if !found || !strings.Contains(second.Content, "bees") {
		t.Fatalf("the note was not served from the cache: %+v", second)
	}
	if second.UpdatedAt != first.UpdatedAt {
		t.Fatalf("updated_at moved from %q to %q over a note nothing changed", first.UpdatedAt, second.UpdatedAt)
	}
}

// The residual the cache is documented to have, held as a test so it stays the
// one it says it is: two writes in the same second that leave the file exactly
// the same length are invisible to it, and one pass serves the older words.
func TestRefreshMemoryIndexAcceptsTheSameSecondSameLengthResidual(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	rewriteKeepingVaultMetadata(t, vault.Root(), "beliefs/note.md", managedBelief(noteID, nil, "The user keeps ants."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("second pass: %v", err)
	}

	note, found := noteRowFor(t, repo, noteID)
	if !found {
		t.Fatal("the note left the index")
	}
	if !strings.Contains(note.Content, "bees") {
		t.Fatalf("content = %q; the accepted residual is that this pass still says bees", note.Content)
	}
}

// A note the user deleted has to leave the cache with it, or a later pass is
// holding the only copy of a memory they asked to be rid of — and would serve
// it back to whatever is written at that path next.
func TestRefreshMemoryIndexForgetsTheBytesOfANoteThatLeftTheVault(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	relPath := "beliefs/note.md"
	writeVaultNote(t, vault, relPath, managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	full := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
	before, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(full); err != nil {
		t.Fatalf("delete the note: %v", err)
	}
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("pass after the deletion: %v", err)
	}

	// A different note lands at the same name, with the same length and the
	// same modification time the deleted one had. Only an evicted cache can
	// tell these bytes from the ones it was holding.
	replacement := newTestNoteID(t)
	writeVaultNote(t, vault, relPath, managedBelief(replacement, nil, "The user keeps ants."))
	if err := os.Chtimes(full, before.ModTime(), before.ModTime()); err != nil {
		t.Fatalf("restore times on %q: %v", relPath, err)
	}
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("pass after the replacement: %v", err)
	}

	note, found := noteRowFor(t, repo, replacement)
	if !found {
		t.Fatal("the replacement note was not indexed")
	}
	if !strings.Contains(note.Content, "ants") {
		t.Fatalf("content = %q, want the replacement's own words", note.Content)
	}
	if _, stillThere := noteRowFor(t, repo, noteID); stillThere {
		t.Fatal("the deleted note's row survived")
	}
}

// Attaching a vault is the start of a new relationship with the disk. Whatever
// the last one had read says nothing about this one — the same relative path
// under a different root is a different file — so the cache starts empty.
func TestSetMemoryVaultStartsWithAnEmptyCache(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	// The residual edit: invisible to a warm cache, and read by a cold one.
	rewriteKeepingVaultMetadata(t, vault.Root(), "beliefs/note.md", managedBelief(noteID, nil, "The user keeps ants."))
	repo.SetMemoryVault(vault)

	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("pass after re-attaching the vault: %v", err)
	}
	note, found := noteRowFor(t, repo, noteID)
	if !found || !strings.Contains(note.Content, "ants") {
		t.Fatalf("content = %q, want the bytes a freshly attached vault reads from disk", note.Content)
	}
}
