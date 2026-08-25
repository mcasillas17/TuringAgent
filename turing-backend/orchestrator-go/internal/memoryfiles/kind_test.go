package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Promotion has two doors, and they are not the same door.
//
// A managed candidate is something Turing wrote about the user, and it may
// become a belief only when it says so itself: kind: "belief", in its own
// frontmatter. A hand-dropped inbox file the user made in Obsidian has no
// candidate row and no kind at all, and the plan keeps it promotable by file
// move — but through a mode the caller states out loud, never by a kind that
// failed to parse and quietly read as empty.

func TestPromoteToBeliefsAcceptsAManagedBeliefCandidate(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	promoted, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: candidate.RelPath,
		Mode:          PromoteManagedCandidate,
		Kind:          KindBelief,
	})
	if err != nil {
		t.Fatalf("a managed belief candidate is exactly what this door is for: %v", err)
	}
	if !strings.HasPrefix(promoted.RelPath, BeliefsDirName+"/") {
		t.Fatalf("promoted to %q", promoted.RelPath)
	}
}

func TestPromoteToBeliefsRefusesAManagedCandidateWithNoKind(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/managed.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\nmanaged: true\n---\nA claim about the user.\n")

	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: "inbox/managed.md",
		Mode:          PromoteManagedCandidate,
	})
	if !errors.Is(err, ErrKind) {
		t.Fatalf("a managed promotion needs an explicit belief kind, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), InboxDirName, "managed.md")); err != nil {
		t.Fatalf("the refused candidate was disturbed: %v", err)
	}
}

func TestPromoteToBeliefsRefusesAnUnrecognisedKindAsAParseError(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/unknown-kind.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\nkind: \"observation\"\n---\nA claim about the user.\n")

	for _, mode := range []PromotionMode{PromoteManagedCandidate, PromoteUnmanagedDraft} {
		_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
			SourceRelPath: "inbox/unknown-kind.md",
			Mode:          mode,
		})
		if !errors.Is(err, ErrNoteParse) {
			t.Fatalf("mode %q: an unreadable kind must be a parse error, not a silent empty kind, got %v", mode, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(vault.Root(), BeliefsDirName))
	if err != nil {
		t.Fatalf("read beliefs: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("a note with an unreadable kind reached beliefs/: %v", entries[0].Name())
	}
}

func TestPromoteToBeliefsRefusesAProfileEditThroughEitherDoor(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindProfileEdit, "Call me Miguel", "The user goes by Miguel.")

	for _, mode := range []PromotionMode{PromoteManagedCandidate, PromoteUnmanagedDraft} {
		_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
			SourceRelPath: candidate.RelPath,
			Mode:          mode,
		})
		if !errors.Is(err, ErrKind) {
			t.Fatalf("mode %q: a profile edit is not a belief, got %v", mode, err)
		}
	}
}

func TestPromoteToBeliefsRefusesAKindlessDraftWithoutTheUnmanagedMode(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/hand.md", "# Hand written\n\nSomething the user typed.\n")

	for _, mode := range []PromotionMode{"", PromoteManagedCandidate} {
		_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
			SourceRelPath: "inbox/hand.md",
			Mode:          mode,
			Kind:          KindBelief,
		})
		if !errors.Is(err, ErrKind) {
			t.Fatalf("mode %q: a kindless draft must not promote as a managed candidate, got %v", mode, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), InboxDirName, "hand.md")); err != nil {
		t.Fatalf("the user's draft was disturbed: %v", err)
	}
}

func TestPromoteToBeliefsAcceptsAKindlessDraftThroughTheUnmanagedMode(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/hand.md", "# Hand written\n\nSomething the user typed.\n")

	promoted, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: "inbox/hand.md",
		Mode:          PromoteUnmanagedDraft,
	})
	if err != nil {
		t.Fatalf("a hand-dropped draft is promotable by file move: %v", err)
	}
	if !strings.HasPrefix(promoted.RelPath, BeliefsDirName+"/") {
		t.Fatalf("promoted to %q", promoted.RelPath)
	}
	if promoted.NoteID == "" {
		t.Fatal("expected a stable identity to be assigned for the destination name")
	}
}

func TestPromoteToBeliefsRefusesAManagedCandidateThroughTheUnmanagedDoor(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: candidate.RelPath,
		Mode:          PromoteUnmanagedDraft,
	})
	if !errors.Is(err, ErrKind) {
		t.Fatalf("a candidate Turing wrote is not an unmanaged draft, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); err != nil {
		t.Fatalf("the refused candidate was disturbed: %v", err)
	}
}

// A file that says managed: true is Turing's, whatever else its frontmatter
// lost. Letting it through the unmanaged door would turn a candidate whose kind
// was stripped — by a plugin, by a merge, by hand — into a promotable draft.
func TestPromoteToBeliefsRefusesAManagedFileWithNoKindThroughTheUnmanagedDoor(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/stripped.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\nmanaged: true\n---\nA claim about the user.\n")

	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: "inbox/stripped.md",
		Mode:          PromoteUnmanagedDraft,
	})
	if !errors.Is(err, ErrKind) {
		t.Fatalf("a managed file is not a hand-dropped draft, got %v", err)
	}
	entries, readErr := os.ReadDir(filepath.Join(vault.Root(), BeliefsDirName))
	if readErr != nil {
		t.Fatalf("read beliefs: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("a managed file was promoted through the unmanaged door: %v", entries[0].Name())
	}
}

func TestPromoteToBeliefsRefusesAnUnknownMode(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: candidate.RelPath,
		Mode:          PromotionMode("whatever"),
	})
	if !errors.Is(err, ErrKind) {
		t.Fatalf("expected an unrecognised promotion mode to be refused, got %v", err)
	}
}

func TestCreateInboxNoteRefusesEveryKindOutsideTheTwoKnownOnes(t *testing.T) {
	vault := newTestVault(t)
	for _, kind := range []NoteKind{"", "Belief", "belief ", "profile-edit", "unknown"} {
		if _, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
			Kind: kind,
			Body: "a claim",
		}); !errors.Is(err, ErrKind) {
			t.Fatalf("kind %q should be refused, got %v", kind, err)
		}
	}
}

// The caller's own claim about a candidate's kind is checked before the file is
// even opened. Its own frontmatter is authoritative afterwards — that is the
// gate TestPromoteToBeliefsRefusesProfileEditDeclaredAsBelief covers — but a
// caller that says "this is a profile edit" and points promotion at a belief is
// asking for two different things at once, and neither of them is a promotion.
// Without this leg the request's Kind field would be decoration: settable to
// anything, read by nothing.
func TestPromoteToBeliefsRefusesACallerClaimedProfileEditOverABeliefFile(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Keeps bees", "The user keeps bees.")

	for _, claimed := range []NoteKind{KindProfileEdit, "profile-edit", "Belief", "anything"} {
		_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
			SourceRelPath: candidate.RelPath,
			Kind:          claimed,
		})
		if !errors.Is(err, ErrKind) {
			t.Fatalf("claimed kind %q: expected the promotion to be refused, got %v", claimed, err)
		}
		if _, err := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); err != nil {
			t.Fatalf("claimed kind %q: the refused candidate was disturbed: %v", claimed, err)
		}
		if _, err := os.Lstat(filepath.Join(vault.Root(), BeliefsDirName)); err != nil {
			t.Fatalf("claimed kind %q: beliefs/ was disturbed: %v", claimed, err)
		}
	}
}
