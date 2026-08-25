package memoryfiles

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// A note with no id key at all — one the user wrote by hand, in beliefs or in
// inbox — has not been adopted by reconcile yet. Scan is a read: it may report
// that such a note carries no identity, but it must never mint one and it must
// never touch the file's bytes to do so. A scan that quietly assigns a ULID
// the first time it looks at a note is a memory system silently rewriting the
// user's own file underneath their editor.
//
// This is checked byte-for-byte, across both an uncached scan and a second,
// cached pass over the same vault, because a mutation hiding behind "only on
// the first, uncached read" would otherwise still slip past a coarser check.
func TestScanNeverRewritesAnIdLessNote(t *testing.T) {
	vault := newTestVault(t)
	beliefPath := writeVaultFile(t, vault, "beliefs/handwritten.md", "# The user wrote this by hand\n\nNo frontmatter at all.\n")
	inboxPath := writeVaultFile(t, vault, "inbox/handwritten.md", "Just some notes jotted down. No id, no kind, no frontmatter.\n")

	relPaths := []string{beliefPath, inboxPath}
	before := make(map[string][]byte, len(relPaths))
	for _, full := range relPaths {
		data, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("read %q before scanning: %v", full, err)
		}
		before[full] = data
	}

	cache := NewMetadataCache()
	firstResult, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	for _, note := range firstResult.Notes {
		if note.NoteID != "" {
			t.Fatalf("scan assigned identity %q to a note that carried none: %q", note.NoteID, note.RelPath)
		}
	}

	secondResult, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("second, cached scan: %v", err)
	}
	for _, note := range secondResult.Notes {
		if note.NoteID != "" {
			t.Fatalf("cached scan assigned identity %q to a note that carried none: %q", note.NoteID, note.RelPath)
		}
	}

	for _, full := range relPaths {
		after, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("read %q after scanning: %v", full, err)
		}
		if !bytes.Equal(before[full], after) {
			t.Fatalf("scan rewrote %q:\nbefore: %q\nafter:  %q", full, before[full], after)
		}
	}
}
