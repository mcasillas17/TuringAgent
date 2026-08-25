package memoryfiles

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two different questions are asked of the same two files, and answering them
// with one projection is what broke the editor.
//
// The runtime asks "what reaches a prompt": bounded at the budget, cut on a
// rune boundary, with a notice in the document's own voice saying so. The
// editor asks "what does the file say, and what do I compare-and-set against":
// the whole document, and a hash over exactly the bytes on disk. A hash of the
// pin can never match the file, so an editor handed one could read a long
// document and never save it again.

func TestEditablePersonaServesTheWholeDocumentPastThePinBudget(t *testing.T) {
	vault := newTestVault(t)
	long := "# Persona\n\n" + strings.Repeat("be direct. ", 900)
	if len(long) <= MaxPersonaBytes {
		t.Fatalf("fixture is %d bytes; it has to exceed the %d byte pin budget", len(long), MaxPersonaBytes)
	}
	writeVaultFile(t, vault, PersonaFileName, long)

	editable := vault.EditablePersona(context.Background())
	if !editable.Available || editable.Reason != UnavailableNone {
		t.Fatalf("editable persona = %+v, want an available document", editable)
	}
	if editable.Content != long {
		t.Fatalf("editable persona content is %d bytes, want the whole %d byte document", len(editable.Content), len(long))
	}
	if editable.ContentHash != ContentHash(long) {
		t.Fatal("the editor's compare-and-set token is not a hash of the bytes on disk, so a save can never match")
	}
	if strings.Contains(editable.Content, truncationNoticeMarker) {
		t.Fatalf("the editor was handed the runtime's synthetic notice as if the user had typed it: %q", tail(editable.Content, 160))
	}
	if !editable.PinnedTruncated {
		t.Fatal("a document past the budget must say the pin will be cut; silence reads as 'all of this reaches a run'")
	}
	if editable.PinnedBytes <= 0 || editable.PinnedBytes > MaxPersonaBytes {
		t.Fatalf("pinned bytes = %d, want the rune-safe cut at or below %d", editable.PinnedBytes, MaxPersonaBytes)
	}
	if editable.SizeBytes != int64(len(long)) {
		t.Fatalf("size = %d, want %d", editable.SizeBytes, len(long))
	}
}

// The pin the runtime carries is a different artefact and must not move: its
// bytes and its hash are the preimage of the egress fingerprint.
func TestEditableReadLeavesThePinnedProjectionAlone(t *testing.T) {
	vault := newTestVault(t)
	long := "# Profile\n\n" + strings.Repeat("they bike to work. ", 500)
	writeVaultFile(t, vault, ProfileFileName, long)

	pinned := vault.LoadProfile(context.Background())
	if !pinned.Truncated {
		t.Fatal("fixture must be past the budget for this test to say anything")
	}
	body, notice := splitTruncationNotice(t, pinned)
	if notice != truncationNotice(ProfileFileName, len(body)) {
		t.Fatalf("notice = %q", notice)
	}
	if pinned.ContentHash != ContentHash(pinned.Content) {
		t.Fatal("the pinned hash must stay a hash of the post-truncation bytes the model is shown")
	}
	if pinned.ContentHash == ContentHash(long) {
		t.Fatal("the pinned hash must not become a hash of the file; the fingerprint is a claim about what was sent")
	}

	editable := vault.EditableProfile(context.Background())
	if editable.ContentHash == pinned.ContentHash {
		t.Fatal("the editor's compare-and-set token and the egress fingerprint preimage are the same value again")
	}
}

// The whole point of separating them: a document longer than the budget can be
// read, edited and saved. Under the old shared hash the save was refused
// forever, and the refusal told the user to re-read a file that would hand
// back the same unusable token.
func TestALongDocumentCanBeReadEditedAndSaved(t *testing.T) {
	vault := newTestVault(t)
	long := "# Persona\n\n" + strings.Repeat("be direct. ", 900)
	personaPath := writeVaultFile(t, vault, PersonaFileName, long)

	editable := vault.EditablePersona(context.Background())
	edited := editable.Content + "\nOne more line.\n"
	saved, err := vault.SavePersona(context.Background(), SavePersonaRequest{
		ExpectedContentHash: editable.ContentHash,
		Content:             edited,
	})
	if err != nil {
		t.Fatalf("save a long persona the editor had just read: %v", err)
	}
	if saved.ContentHash != ContentHash(edited) {
		t.Fatalf("saved hash = %q, want a hash of the whole saved document", saved.ContentHash)
	}
	if !saved.PinnedTruncated {
		t.Fatal("the save receipt must still say the pin will be cut")
	}
	assertFileContent(t, personaPath, edited)
}

// Whitespace is a clear the user typed. The pin is empty because nothing
// survives trimming, but the file holds those bytes and the next save has to
// be able to name them.
func TestAWhitespaceOnlyDocumentStillHandsBackASaveableToken(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, ProfileFileName, "   \n\t\n")

	editable := vault.EditableProfile(context.Background())
	if editable.Content != "   \n\t\n" {
		t.Fatalf("editable content = %q, want exactly the bytes on disk", editable.Content)
	}
	if editable.ContentHash != ContentHash("   \n\t\n") {
		t.Fatal("a whitespace-only document handed back a hash of the empty pin, so the next save can never match")
	}
	if editable.PinnedTruncated {
		t.Fatal("nothing was cut; the pin is empty because whitespace is not content")
	}
	if editable.PinnedBytes != 0 {
		t.Fatalf("pinned bytes = %d, want 0: whitespace reaches no prompt", editable.PinnedBytes)
	}

	if _, err := vault.SaveProfile(context.Background(), SaveProfileRequest{
		ExpectedContentHash: editable.ContentHash,
		Content:             "# Profile\n\nSomething real.\n",
	}); err != nil {
		t.Fatalf("save over a whitespace-only profile: %v", err)
	}
}

// Past the safety ceiling there is no editor at all. Serving the first 512 KiB
// would let a save truncate the user's file to whatever the editor happened to
// hold, which is the one outcome a compare-and-set exists to prevent.
func TestAnOverCeilingDocumentIsRefusedRatherThanPartiallyEdited(t *testing.T) {
	vault := newTestVault(t)
	huge := strings.Repeat("x", MaxAuthoredDocumentBytes+1)
	writeVaultFile(t, vault, PersonaFileName, huge)

	editable := vault.EditablePersona(context.Background())
	if editable.Available {
		t.Fatal("a document past the read ceiling must not be presented as editable")
	}
	if editable.Reason != UnavailableContentTooLarge {
		t.Fatalf("reason = %q, want %q", editable.Reason, UnavailableContentTooLarge)
	}
	if editable.Content != "" {
		t.Fatalf("a partial editor was served: %d bytes", len(editable.Content))
	}
	if editable.ContentHash != "" {
		t.Fatal("a hash was handed out for content that was never read in full")
	}
	if editable.Detail == "" {
		t.Fatal("the refusal says nothing the user could act on")
	}
}

// A file the user has not written yet is a first run, not a failure. It is
// reported as missing and carries no token, so the save that follows says "I
// expect nothing to be there" and creates it.
func TestAnAbsentDocumentIsReportedMissingWithNoToken(t *testing.T) {
	vault := newTestVault(t)

	editable := vault.EditablePersona(context.Background())
	if editable.Available {
		t.Fatal("a persona that does not exist is not available")
	}
	if editable.Reason != UnavailableVaultMissing {
		t.Fatalf("reason = %q, want %q", editable.Reason, UnavailableVaultMissing)
	}
	if editable.ContentHash != "" {
		t.Fatalf("expected no compare-and-set token for an absent document, got %q", editable.ContentHash)
	}

	if _, err := vault.SavePersona(context.Background(), SavePersonaRequest{
		ExpectedContentHash: editable.ContentHash,
		Content:             "# Persona\n\nFirst ever.\n",
	}); err != nil {
		t.Fatalf("first-run save: %v", err)
	}
	assertFileContent(t, filepath.Join(vault.Root(), PersonaFileName), "# Persona\n\nFirst ever.\n")
}

// The editor read is as closed as the pinned one: no path argument, so there
// is no way to aim it at the inbox or at a belief.
func TestTheEditableReadRefusesASymlinkedDocument(t *testing.T) {
	vault := newTestVault(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(outside, []byte("someone else's file"), 0o600); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(vault.Root(), ProfileFileName)); err != nil {
		t.Fatalf("plant symlink: %v", err)
	}

	editable := vault.EditableProfile(context.Background())
	if editable.Available || editable.Content != "" {
		t.Fatalf("a symlinked profile was served as editable: %+v", editable)
	}
	if editable.Reason != UnavailableVaultUnreadable {
		t.Fatalf("reason = %q, want %q", editable.Reason, UnavailableVaultUnreadable)
	}
}

// A document that fits the budget says nothing was cut, and reports the bytes
// that reach a run so a client never has to guess.
func TestAShortDocumentReportsNoTruncationAndItsWholeLengthAsPinned(t *testing.T) {
	vault := newTestVault(t)
	short := "# Persona\n\nBe direct.\n"
	writeVaultFile(t, vault, PersonaFileName, short)

	editable := vault.EditablePersona(context.Background())
	if editable.PinnedTruncated {
		t.Fatal("a document inside the budget is not truncated")
	}
	if editable.PinnedBytes != len(short) {
		t.Fatalf("pinned bytes = %d, want %d", editable.PinnedBytes, len(short))
	}
	if editable.Content != short || editable.ContentHash != ContentHash(short) {
		t.Fatalf("editable = %+v, want the document and its own hash", editable)
	}
}
