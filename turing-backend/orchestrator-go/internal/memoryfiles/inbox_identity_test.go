package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A caller that has to record a candidate in a durable manifest before the
// file exists needs to know the name the vault will use. It gets that by
// minting the identity itself and handing it in — the vault still names the
// file, from the same rule it uses when it mints the identity for itself.
func TestCreateInboxNoteUsesSuppliedIdentity(t *testing.T) {
	vault := newTestVault(t)
	noteID, err := NewNoteID()
	if err != nil {
		t.Fatalf("mint note id: %v", err)
	}
	planned := InboxNoteRelPath(noteID, "Prefers dark mode")

	note, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
		NoteID: noteID,
		Kind:   KindBelief,
		Title:  "Prefers dark mode",
		Body:   "The user asked for dark mode twice.",
	})
	if err != nil {
		t.Fatalf("CreateInboxNote: %v", err)
	}
	if note.NoteID != noteID {
		t.Fatalf("note id = %q, want the supplied %q", note.NoteID, noteID)
	}
	if note.RelPath != planned {
		t.Fatalf("rel path = %q, want the planned %q", note.RelPath, planned)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), planned)); err != nil {
		t.Fatalf("stat the planned path: %v", err)
	}
	if !strings.Contains(note.Content, noteID) {
		t.Fatalf("frontmatter does not carry the supplied identity")
	}
}

// An identity is a ULID and nothing else. Anything path-shaped, empty-ish or
// merely unparseable is refused before a file is created, so a caller that
// reserved a path cannot be talked into writing somewhere else.
func TestCreateInboxNoteRefusesForgedIdentity(t *testing.T) {
	for _, noteID := range []string{
		"../escape",
		"has/slash",
		"has\x00nul",
		"not-a-ulid",
		"   ",
	} {
		vault := newTestVault(t)
		_, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
			NoteID: noteID,
			Kind:   KindBelief,
			Title:  "title",
			Body:   "body",
		})
		if err == nil {
			t.Fatalf("note id %q was accepted", noteID)
		}
		entries, readErr := os.ReadDir(filepath.Join(vault.Root(), InboxDirName))
		if readErr != nil {
			t.Fatalf("read inbox: %v", readErr)
		}
		if len(entries) != 0 {
			t.Fatalf("refused identity %q still wrote %d inbox entries", noteID, len(entries))
		}
	}
}

// The planning rule and the write have to agree, including for a title that is
// hostile or empty: a reservation computed from a different rule than the one
// the write uses is a manifest that names a file nobody wrote.
func TestInboxNoteRelPathMatchesWhatIsWritten(t *testing.T) {
	for _, title := range []string{
		"",
		"../../etc/passwd",
		"Ünïcödé  title\twith\nbreaks",
		strings.Repeat("long", 200),
	} {
		vault := newTestVault(t)
		noteID, err := NewNoteID()
		if err != nil {
			t.Fatalf("mint note id: %v", err)
		}
		note, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
			NoteID: noteID,
			Kind:   KindBelief,
			Title:  title,
			Body:   "body",
		})
		if err != nil {
			t.Fatalf("CreateInboxNote(title=%q): %v", title, err)
		}
		if planned := InboxNoteRelPath(noteID, title); planned != note.RelPath {
			t.Fatalf("planned %q but wrote %q", planned, note.RelPath)
		}
	}
}

// Omitting the identity keeps the behaviour every existing caller relies on:
// the vault mints one.
func TestCreateInboxNoteStillMintsItsOwnIdentity(t *testing.T) {
	vault := newTestVault(t)
	note, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
		Kind:  KindBelief,
		Title: "no identity supplied",
		Body:  "body",
	})
	if err != nil {
		t.Fatalf("CreateInboxNote: %v", err)
	}
	if note.NoteID == "" {
		t.Fatalf("vault minted no identity")
	}
	if note.RelPath != InboxNoteRelPath(note.NoteID, "no identity supplied") {
		t.Fatalf("minted path %q does not follow the shared naming rule", note.RelPath)
	}
}

// Two candidates cannot share one identity: the second write finds the name
// taken and is refused rather than replacing the first candidate.
func TestCreateInboxNoteRefusesDuplicateIdentity(t *testing.T) {
	vault := newTestVault(t)
	noteID, err := NewNoteID()
	if err != nil {
		t.Fatalf("mint note id: %v", err)
	}
	request := CreateInboxNoteRequest{NoteID: noteID, Kind: KindBelief, Title: "same", Body: "first"}
	if _, err := vault.CreateInboxNote(context.Background(), request); err != nil {
		t.Fatalf("first CreateInboxNote: %v", err)
	}
	request.Body = "second"
	if _, err := vault.CreateInboxNote(context.Background(), request); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second CreateInboxNote error = %v, want ErrAlreadyExists", err)
	}
}
