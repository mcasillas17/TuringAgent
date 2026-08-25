package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedCandidate(t *testing.T, vault *Vault, kind NoteKind, title string, body string) InboxNote {
	t.Helper()
	note, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
		Kind:         kind,
		Title:        title,
		Body:         body,
		EvidenceRefs: []string{"sess_a"},
	})
	if err != nil {
		t.Fatalf("seed candidate: %v", err)
	}
	return note
}

func TestPromoteToBeliefsMovesTheFileIntoBeliefs(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	promoted, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: candidate.RelPath,
		Kind:          KindBelief,
	})
	if err != nil {
		t.Fatalf("promote to beliefs: %v", err)
	}
	if !strings.HasPrefix(promoted.RelPath, BeliefsDirName+"/") {
		t.Fatalf("promoted note %q is not under beliefs/", promoted.RelPath)
	}
	if promoted.NoteID != candidate.NoteID {
		t.Fatalf("identity changed: %q -> %q", candidate.NoteID, promoted.NoteID)
	}
	onDisk, err := os.ReadFile(filepath.Join(vault.Root(), filepath.FromSlash(promoted.RelPath)))
	if err != nil {
		t.Fatalf("read promoted note: %v", err)
	}
	if string(onDisk) != candidate.Content {
		t.Fatalf("content changed during promotion:\nwant %q\ngot  %q", candidate.Content, onDisk)
	}
	if promoted.ContentHash != candidate.ContentHash {
		t.Fatalf("content hash changed: %q -> %q", candidate.ContentHash, promoted.ContentHash)
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); !os.IsNotExist(err) {
		t.Fatalf("the candidate was copied rather than moved: %v", err)
	}
}

func TestPromoteToBeliefsRefusesProfileEditCandidate(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindProfileEdit, "Call me Miguel", "The user goes by Miguel.")

	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: candidate.RelPath,
		Kind:          KindProfileEdit,
	})
	if !errors.Is(err, ErrKind) {
		t.Fatalf("expected a profile_edit candidate to be refused, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); err != nil {
		t.Fatalf("the refused candidate was disturbed: %v", err)
	}
}

func TestPromoteToBeliefsRefusesProfileEditDeclaredAsBelief(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindProfileEdit, "Call me Miguel", "The user goes by Miguel.")

	// The caller lies about the kind. The primitive reads the file's own
	// frontmatter, so the lie changes nothing.
	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: candidate.RelPath,
		Kind:          KindBelief,
	})
	if !errors.Is(err, ErrKind) {
		t.Fatalf("expected the file's own kind to refuse the promotion, got %v", err)
	}
}

func TestPromoteToBeliefsRefusesSourceOutsideInbox(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, PersonaFileName, "persona")
	writeVaultFile(t, vault, "beliefs/existing.md", "belief")

	sources := append([]string{"beliefs/existing.md"}, escapingRelPathValues()...)
	for _, source := range sources {
		_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
			SourceRelPath: source,
			Kind:          KindBelief,
		})
		if !errors.Is(err, ErrConfinement) {
			t.Fatalf("expected source %q to be refused, got %v", source, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), PersonaFileName)); err != nil {
		t.Fatalf("persona.md was disturbed: %v", err)
	}
}

func TestPromoteToBeliefsRefusesDestinationOutsideBeliefs(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	destinations := append([]string{"inbox/other.md", PersonaFileName, ProfileFileName}, escapingRelPathValues()...)
	for _, destination := range destinations {
		if destination == "" {
			// An unset destination is the "name it for me" case, covered
			// elsewhere; it is not a path to refuse.
			continue
		}
		_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
			SourceRelPath:      candidate.RelPath,
			DestinationRelPath: destination,
			Kind:               KindBelief,
		})
		if !errors.Is(err, ErrConfinement) {
			t.Fatalf("expected destination %q to be refused, got %v", destination, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); err != nil {
		t.Fatalf("the candidate was disturbed by a refused destination: %v", err)
	}
}

func TestPromoteToBeliefsHonoursAnExplicitDestination(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	promoted, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath:      candidate.RelPath,
		DestinationRelPath: "beliefs/preferences/dark-mode.md",
		Kind:               KindBelief,
	})
	if err != nil {
		t.Fatalf("promote to beliefs: %v", err)
	}
	if promoted.RelPath != "beliefs/preferences/dark-mode.md" {
		t.Fatalf("destination = %q", promoted.RelPath)
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), "beliefs", "preferences", "dark-mode.md")); err != nil {
		t.Fatalf("nested destination was not created: %v", err)
	}
}

func TestPromoteToBeliefsIsExclusiveAndLeavesTheSourceIntact(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")
	writeVaultFile(t, vault, "beliefs/taken.md", "already here")

	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath:      candidate.RelPath,
		DestinationRelPath: "beliefs/taken.md",
		Kind:               KindBelief,
	})
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected an exclusivity refusal, got %v", err)
	}
	existing, readErr := os.ReadFile(filepath.Join(vault.Root(), "beliefs", "taken.md"))
	if readErr != nil {
		t.Fatalf("read destination: %v", readErr)
	}
	if string(existing) != "already here" {
		t.Fatalf("destination was overwritten: %q", existing)
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); err != nil {
		t.Fatalf("the source was removed despite the refusal: %v", err)
	}
}

func TestPromoteToBeliefsRefusesSymlinkedSource(t *testing.T) {
	vault := newTestVault(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("---\nkind: \"belief\"\n---\nsmuggled\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(vault.Root(), InboxDirName, "link.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: "inbox/link.md",
		Kind:          KindBelief,
	}); err == nil {
		t.Fatal("expected a symlinked source to be refused")
	}
	entries, err := os.ReadDir(filepath.Join(vault.Root(), BeliefsDirName))
	if err != nil {
		t.Fatalf("read beliefs: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("smuggled content reached beliefs/: %v", entries)
	}
}

func TestPromoteToBeliefsRefusesSymlinkedDestination(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")
	outside := filepath.Join(t.TempDir(), "target.md")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(vault.Root(), BeliefsDirName, "link.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath:      candidate.RelPath,
		DestinationRelPath: "beliefs/link.md",
		Kind:               KindBelief,
	}); err == nil {
		t.Fatal("expected a symlinked destination to be refused")
	}
	content, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read outside file: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("the symlink target was written through: %q", content)
	}
}

func TestPromoteToBeliefsRefusesAMissingSource(t *testing.T) {
	vault := newTestVault(t)
	if _, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: "inbox/never-existed.md",
		Kind:          KindBelief,
	}); err == nil {
		t.Fatal("expected a missing source to be refused")
	}
}

func TestPromoteToBeliefsRefusesAnOverLargeSource(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/huge.md", strings.Repeat("a", MaxNoteFileBytes+1))
	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: "inbox/huge.md",
		Kind:          KindBelief,
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected an over-large source to be refused, got %v", err)
	}
}

func TestPromoteToBeliefsRefusesAnUnparsableSource(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/broken.md", "---\nkind: \"belief\nunclosed\n")
	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: "inbox/broken.md",
		Kind:          KindBelief,
	})
	if !errors.Is(err, ErrNoteParse) {
		t.Fatalf("expected a per-note parse refusal, got %v", err)
	}
}

func TestPromoteToBeliefsAcceptsAHandWrittenCandidateWithoutFrontmatter(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/hand.md", "# Hand written\n\nSomething the user typed.\n")
	promoted, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: "inbox/hand.md",
		Kind:          KindBelief,
	})
	if err != nil {
		t.Fatalf("a candidate with no frontmatter is still a belief candidate: %v", err)
	}
	if !strings.HasPrefix(promoted.RelPath, BeliefsDirName+"/") {
		t.Fatalf("promoted note %q is not under beliefs/", promoted.RelPath)
	}
	if promoted.NoteID == "" {
		t.Fatal("expected a stable identity to be assigned for the destination name")
	}
}

func TestPromoteToBeliefsHonoursContextCancellation(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := vault.PromoteToBeliefs(ctx, PromoteToBeliefsRequest{
		SourceRelPath: candidate.RelPath,
		Kind:          KindBelief,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
