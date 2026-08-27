package memoryfiles

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// `Scan` is its own door, and it has to be read-only on its own.
//
// `TestScanNeverRewritesAnIdLessNote` holds that rule for `ScanWithCache`,
// which is where the walk actually happens — but every whole-vault pass in the
// repository reaches the vault through `Scan`, and `Scan` is a separate
// exported function that a later change could give work of its own. A
// convenience wrapper that adopted id-less notes "so the index is complete"
// would be a memory system minting identities into the user's files behind a
// search, and the cached test would never see it.
//
// So this calls `Scan` directly, over both areas, and asserts the two things
// that would be untrue afterwards: that no note came back carrying an identity
// it did not have on disk, and that not one byte of either file changed.
func TestScanItselfNeverAssignsAnIdentityOrRewritesAFile(t *testing.T) {
	vault := newTestVault(t)
	beliefBody := "# The user wrote this by hand\n\nNo frontmatter at all.\n"
	inboxBody := "Just some notes jotted down. No id, no kind, no frontmatter.\n"
	paths := map[string]string{
		writeVaultFile(t, vault, "beliefs/handwritten.md", beliefBody): beliefBody,
		writeVaultFile(t, vault, "inbox/handwritten.md", inboxBody):    inboxBody,
	}

	before := make(map[string][]byte, len(paths))
	for full := range paths {
		data, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("read %q before scanning: %v", full, err)
		}
		before[full] = data
	}

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Notes) != len(paths) {
		t.Fatalf("Scan reported %d notes, want %d: %+v", len(result.Notes), len(paths), result.Notes)
	}
	for _, note := range result.Notes {
		if note.NoteID != "" {
			t.Fatalf("Scan assigned identity %q to a note that carried none: %q", note.NoteID, note.RelPath)
		}
	}

	for full, body := range paths {
		after, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("read %q after scanning: %v", full, err)
		}
		if !bytes.Equal(before[full], after) {
			t.Fatalf("Scan rewrote the user's file %q:\nbefore %q\nafter  %q", full, before[full], after)
		}
		if string(after) != body {
			t.Fatalf("file %q no longer holds what the user wrote:\nwant %q\ngot  %q", full, body, after)
		}
	}

	// A second call is the one a wrapper hiding behind "only adopt what is not
	// yet known" would survive, so it is made and checked the same way.
	second, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	for _, note := range second.Notes {
		if note.NoteID != "" {
			t.Fatalf("the second Scan assigned identity %q to %q", note.NoteID, note.RelPath)
		}
	}
	for full := range paths {
		after, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("read %q after the second scan: %v", full, err)
		}
		if !bytes.Equal(before[full], after) {
			t.Fatalf("the second Scan rewrote %q:\nbefore %q\nafter  %q", full, before[full], after)
		}
	}
}
