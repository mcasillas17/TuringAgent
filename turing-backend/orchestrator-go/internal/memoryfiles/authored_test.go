package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The two primitives below are the user's own hands on their own documents.
// They are deliberately separate from ApplyProfileEdit: that one writes on the
// authority of a proposal a model wrote, and persona.md is a document no
// proposal may ever reach.

func TestSavePersonaWritesTheUsersTextInPlace(t *testing.T) {
	vault := newTestVault(t)
	personaPath := writeVaultFile(t, vault, PersonaFileName, "# Persona\n\nOld.\n")
	before := inodeOf(t, personaPath)

	saved, err := vault.SavePersona(context.Background(), SavePersonaRequest{
		ExpectedContentHash: ContentHash("# Persona\n\nOld.\n"),
		Content:             "# Persona\n\nBe direct.\n",
	})
	if err != nil {
		t.Fatalf("save persona: %v", err)
	}
	if saved.RelPath != PersonaFileName {
		t.Fatalf("rel path = %q, want %q", saved.RelPath, PersonaFileName)
	}
	if saved.ContentHash != ContentHash("# Persona\n\nBe direct.\n") {
		t.Fatalf("content hash = %q", saved.ContentHash)
	}
	onDisk, err := os.ReadFile(personaPath)
	if err != nil {
		t.Fatalf("read persona: %v", err)
	}
	if string(onDisk) != "# Persona\n\nBe direct.\n" {
		t.Fatalf("persona content = %q", onDisk)
	}
	if after := inodeOf(t, personaPath); after != before {
		t.Fatalf("persona.md was replaced (inode %d -> %d); an open editor keeps the old inode", before, after)
	}
}

func TestSavePersonaTruncatesLeftoverBytes(t *testing.T) {
	vault := newTestVault(t)
	original := "# Persona\n\n" + strings.Repeat("long ", 200) + "\n"
	personaPath := writeVaultFile(t, vault, PersonaFileName, original)

	if _, err := vault.SavePersona(context.Background(), SavePersonaRequest{
		ExpectedContentHash: ContentHash(original),
		Content:             "short\n",
	}); err != nil {
		t.Fatalf("save persona: %v", err)
	}
	onDisk, err := os.ReadFile(personaPath)
	if err != nil {
		t.Fatalf("read persona: %v", err)
	}
	if string(onDisk) != "short\n" {
		t.Fatalf("leftover bytes survived the shorter write: %q", onDisk)
	}
}

func TestSavePersonaRefusesAStaleHashWithoutLosingUserText(t *testing.T) {
	vault := newTestVault(t)
	userText := "# Persona\n\nTyped in Obsidian a second ago.\n"
	personaPath := writeVaultFile(t, vault, PersonaFileName, userText)

	_, err := vault.SavePersona(context.Background(), SavePersonaRequest{
		ExpectedContentHash: ContentHash("# Persona\n\nWhat the editor loaded.\n"),
		Content:             "# Persona\n\nWhat the editor tried to save.\n",
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a typed stale-content refusal, got %v", err)
	}
	message := err.Error()
	if !strings.Contains(message, "the file changed") || !strings.Contains(message, "re-read") {
		t.Fatalf("refusal %q does not tell the user the file changed and to re-read it", message)
	}
	onDisk, readErr := os.ReadFile(personaPath)
	if readErr != nil {
		t.Fatalf("read persona: %v", readErr)
	}
	if string(onDisk) != userText {
		t.Fatalf("a refused save damaged the user's text: %q", onDisk)
	}
}

func TestSavePersonaCreatesTheDocumentWhenAbsentAndNoHashExpected(t *testing.T) {
	vault := newTestVault(t)

	saved, err := vault.SavePersona(context.Background(), SavePersonaRequest{
		Content: "# Persona\n\nFirst ever.\n",
	})
	if err != nil {
		t.Fatalf("save persona: %v", err)
	}
	if saved.Content != "# Persona\n\nFirst ever.\n" {
		t.Fatalf("saved content = %q", saved.Content)
	}
	onDisk, err := os.ReadFile(filepath.Join(vault.Root(), PersonaFileName))
	if err != nil {
		t.Fatalf("read persona: %v", err)
	}
	if string(onDisk) != "# Persona\n\nFirst ever.\n" {
		t.Fatalf("persona content = %q", onDisk)
	}
}

func TestSavePersonaRefusesAMissingDocumentWhenAHashWasExpected(t *testing.T) {
	vault := newTestVault(t)

	_, err := vault.SavePersona(context.Background(), SavePersonaRequest{
		ExpectedContentHash: ContentHash("something"),
		Content:             "# Persona\n",
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a stale-content refusal for a vanished persona, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), PersonaFileName)); !os.IsNotExist(err) {
		t.Fatalf("a refused save created the persona anyway: %v", err)
	}
}

func TestSavePersonaRefusesASymlinkedDocument(t *testing.T) {
	vault := newTestVault(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(outside, []byte("someone else's file"), 0o600); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(vault.Root(), PersonaFileName)); err != nil {
		t.Fatalf("plant symlink: %v", err)
	}

	if _, err := vault.SavePersona(context.Background(), SavePersonaRequest{
		Content: "written through a link",
	}); err == nil {
		t.Fatal("expected a symlinked persona.md to be refused")
	}
	onDisk, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read the linked-to file: %v", err)
	}
	if string(onDisk) != "someone else's file" {
		t.Fatalf("a save followed the link out of the vault: %q", onDisk)
	}
}

func TestSavePersonaRefusesContentPastTheCeilingItCouldReadBack(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, PersonaFileName, "small")

	_, err := vault.SavePersona(context.Background(), SavePersonaRequest{
		ExpectedContentHash: ContentHash("small"),
		Content:             strings.Repeat("x", MaxAuthoredDocumentBytes+1),
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected an over-limit refusal, got %v", err)
	}
	onDisk, readErr := os.ReadFile(filepath.Join(vault.Root(), PersonaFileName))
	if readErr != nil {
		t.Fatalf("read persona: %v", readErr)
	}
	if string(onDisk) != "small" {
		t.Fatalf("an over-limit save wrote anyway: %q", onDisk)
	}
}

// The confinement of these two primitives is structural, not a check a caller
// can be talked out of: neither request carries a path, so there is no target
// to aim somewhere else. A refactor that reintroduced one — a generic
// "save this document at this path" — fails here before any behaviour changes.
func TestAuthoredSaveRequestsCarryNoTarget(t *testing.T) {
	for _, request := range []any{SavePersonaRequest{}, SaveProfileRequest{}} {
		requestType := reflect.TypeOf(request)
		for index := 0; index < requestType.NumField(); index++ {
			name := requestType.Field(index).Name
			if strings.Contains(strings.ToLower(name), "path") {
				t.Fatalf("%s.%s makes the target a caller's choice; these primitives write one document each",
					requestType.Name(), name)
			}
		}
	}
}

func TestSavePersonaLeavesEveryOtherDocumentAlone(t *testing.T) {
	vault := newTestVault(t)
	profilePath := writeVaultFile(t, vault, ProfileFileName, "profile text")
	inboxPath := writeVaultFile(t, vault, "inbox/proposal.md", "candidate text")

	if _, err := vault.SavePersona(context.Background(), SavePersonaRequest{
		Content: "# Persona\n\nOnly this file.\n",
	}); err != nil {
		t.Fatalf("save persona: %v", err)
	}
	assertFileContent(t, profilePath, "profile text")
	assertFileContent(t, inboxPath, "candidate text")
}

func TestSaveProfileWritesWithoutAProposal(t *testing.T) {
	vault := newTestVault(t)
	profilePath := writeVaultFile(t, vault, ProfileFileName, "# Profile\n\nOld.\n")
	before := inodeOf(t, profilePath)

	saved, err := vault.SaveProfile(context.Background(), SaveProfileRequest{
		ExpectedContentHash: ContentHash("# Profile\n\nOld.\n"),
		Content:             "# Profile\n\nWritten by hand.\n",
	})
	if err != nil {
		t.Fatalf("save profile: %v", err)
	}
	if saved.RelPath != ProfileFileName {
		t.Fatalf("rel path = %q, want %q", saved.RelPath, ProfileFileName)
	}
	assertFileContent(t, profilePath, "# Profile\n\nWritten by hand.\n")
	if after := inodeOf(t, profilePath); after != before {
		t.Fatalf("profile.md was replaced (inode %d -> %d); an open editor keeps the old inode", before, after)
	}
}

func TestSaveProfileRefusesAStaleHashWithoutLosingUserText(t *testing.T) {
	vault := newTestVault(t)
	userText := "# Profile\n\nEdited in Obsidian.\n"
	profilePath := writeVaultFile(t, vault, ProfileFileName, userText)

	_, err := vault.SaveProfile(context.Background(), SaveProfileRequest{
		ExpectedContentHash: ContentHash("# Profile\n\nWhat this editor loaded.\n"),
		Content:             "# Profile\n\nWhat this editor tried to save.\n",
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a typed stale-content refusal, got %v", err)
	}
	assertFileContent(t, profilePath, userText)
}

func TestSaveProfileLeavesThePersonaAlone(t *testing.T) {
	vault := newTestVault(t)
	personaPath := writeVaultFile(t, vault, PersonaFileName, "persona text")

	if _, err := vault.SaveProfile(context.Background(), SaveProfileRequest{
		Content: "# Profile\n\nOnly this file.\n",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	assertFileContent(t, personaPath, "persona text")
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(onDisk) != want {
		t.Fatalf("%q = %q, want %q", path, onDisk, want)
	}
}

// Emptying a pinned document is a thing the user is allowed to decide.
//
// The primitive writes what it was handed, including nothing at all: persona.md
// is the user's own instruction channel, and a vault that could only ever add
// to it would leave them unable to take back words they had already given a
// model. The compare-and-set still applies, so the clear is made against the
// text they were looking at.
func TestSavePersonaAcceptsAnIntentionalClear(t *testing.T) {
	vault := newTestVault(t)
	personaPath := writeVaultFile(t, vault, PersonaFileName, "# Persona\n\nOld.\n")

	saved, err := vault.SavePersona(context.Background(), SavePersonaRequest{
		ExpectedContentHash: ContentHash("# Persona\n\nOld.\n"),
		Content:             "",
	})
	if err != nil {
		t.Fatalf("clear persona: %v", err)
	}
	if saved.Content != "" || saved.ContentHash != ContentHash("") {
		t.Fatalf("saved = %+v, want an empty document", saved)
	}
	onDisk, err := os.ReadFile(personaPath)
	if err != nil {
		t.Fatalf("read persona: %v", err)
	}
	if len(onDisk) != 0 {
		t.Fatalf("persona on disk = %q, want it emptied", onDisk)
	}
	// The pinned loader is the other half of the promise: a cleared document
	// pins nothing, rather than reading back as unavailable.
	pinned := vault.LoadPersona(context.Background())
	if !pinned.Available || pinned.Reason != UnavailableNone {
		t.Fatalf("pinned persona = %+v, want an available, empty document", pinned)
	}
	if pinned.Content != "" {
		t.Fatalf("pinned persona content = %q, want nothing pinned", pinned.Content)
	}
}

// Whitespace is the same decision typed differently. The file keeps the bytes
// the user wrote, and the pin — which is what a model would see — is empty.
func TestSaveProfileAcceptsWhitespaceAsAClear(t *testing.T) {
	vault := newTestVault(t)
	profilePath := writeVaultFile(t, vault, ProfileFileName, "# Profile\n\nOld.\n")

	if _, err := vault.SaveProfile(context.Background(), SaveProfileRequest{
		ExpectedContentHash: ContentHash("# Profile\n\nOld.\n"),
		Content:             "   \n\t\n",
	}); err != nil {
		t.Fatalf("clear profile: %v", err)
	}
	onDisk, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if string(onDisk) != "   \n\t\n" {
		t.Fatalf("profile on disk = %q, want exactly what was typed", onDisk)
	}
	pinned := vault.LoadProfile(context.Background())
	if !pinned.Available || pinned.Content != "" {
		t.Fatalf("pinned profile = %+v, want an available document pinning nothing", pinned)
	}
}
