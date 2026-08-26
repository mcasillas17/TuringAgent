package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Every primitive here reads the candidate under its own path lock and then
// mutates it. A caller's earlier read — a listing, a decision's pre-check —
// released that lock before this call took it, so a proposal the user was
// editing in Obsidian can move in between. These tests hold the rule that the
// bytes each primitive acts on are the bytes the decision named, checked
// against the read the primitive itself did.

func editCandidate(t *testing.T, vault *Vault, note InboxNote) {
	t.Helper()
	writeVaultFile(t, vault, note.RelPath, note.Content+"\nAnd light mode on Tuesdays.\n")
}

// vaultFileHash is the compare-and-set token for a file a test wrote by hand,
// so a test about something else can name the bytes it is deciding on without
// restating them.
func vaultFileHash(t *testing.T, vault *Vault, relPath string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(vault.Root(), filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("read %q: %v", relPath, err)
	}
	return ContentHash(string(content))
}

func TestPromoteToBeliefsRefusesACandidateThatMovedSinceItWasNamed(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")
	editCandidate(t, vault, candidate)

	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath:       candidate.RelPath,
		Kind:                KindBelief,
		ExpectedContentHash: candidate.ContentHash,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("promote of a moved candidate = %v, want ErrStaleContent", err)
	}
	entries, readErr := os.ReadDir(filepath.Join(vault.Root(), BeliefsDirName))
	if readErr != nil {
		t.Fatalf("read beliefs: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("a refused promotion installed a belief anyway: %d entries", len(entries))
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); statErr != nil {
		t.Fatalf("a refused promotion removed the candidate: %v", statErr)
	}
}

// The managed door is the decision door, and a decision is always made about
// bytes somebody read. Leaving the hash optional there is the same hole as not
// checking it.
func TestPromoteToBeliefsRefusesAManagedCandidateThatNamesNoBytes(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: candidate.RelPath,
		Kind:          KindBelief,
	})
	if !errors.Is(err, ErrUnboundDecision) {
		t.Fatalf("promote with no expected hash = %v, want ErrUnboundDecision", err)
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); statErr != nil {
		t.Fatalf("a refused promotion removed the candidate: %v", statErr)
	}
}

// A file the user dropped into inbox/ themselves was never listed as a
// proposal, so there is no hash anybody could have shown them.
func TestPromoteToBeliefsAcceptsAnUnmanagedDraftWithNoExpectedHash(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/draft.md", "# Draft\n\nThe user wrote this by hand.\n")

	promoted, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath: "inbox/draft.md",
		Mode:          PromoteUnmanagedDraft,
	})
	if err != nil {
		t.Fatalf("promote an unmanaged draft: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(promoted.RelPath))); statErr != nil {
		t.Fatalf("the promoted draft is not under beliefs/: %v", statErr)
	}
}

func TestApplyProfileEditRefusesACandidateThatMovedSinceItWasNamed(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	original := "# Profile\n\nOld text.\n"
	profilePath := writeVaultFile(t, vault, ProfileFileName, original)
	editCandidate(t, vault, candidate)

	_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:      candidate.RelPath,
		TargetRelPath:         ProfileFileName,
		ExpectedContentHash:   ContentHash(original),
		ExpectedCandidateHash: candidate.ContentHash,
		Content:               "# Profile\n\nGoes by Miguel.\n",
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("apply from a moved candidate = %v, want ErrStaleContent", err)
	}
	onDisk, readErr := os.ReadFile(profilePath)
	if readErr != nil {
		t.Fatalf("read profile: %v", readErr)
	}
	if string(onDisk) != original {
		t.Fatalf("a refused apply rewrote the profile: %q", onDisk)
	}
}

func TestApplyProfileEditRefusesACandidateThatNamesNoBytes(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	original := "# Profile\n\nOld text.\n"
	profilePath := writeVaultFile(t, vault, ProfileFileName, original)

	_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:    candidate.RelPath,
		TargetRelPath:       ProfileFileName,
		ExpectedContentHash: ContentHash(original),
		Content:             "# Profile\n\nGoes by Miguel.\n",
	})
	if !errors.Is(err, ErrUnboundDecision) {
		t.Fatalf("apply with no expected candidate hash = %v, want ErrUnboundDecision", err)
	}
	onDisk, readErr := os.ReadFile(profilePath)
	if readErr != nil {
		t.Fatalf("read profile: %v", readErr)
	}
	if string(onDisk) != original {
		t.Fatalf("a refused apply rewrote the profile: %q", onDisk)
	}
}

func TestRemoveInboxNoteRefusesADecidedRemovalWhoseBytesMoved(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")
	editCandidate(t, vault, candidate)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             candidate.RelPath,
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: candidate.ContentHash,
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("decided removal of moved bytes = %v, want ErrStaleContent", err)
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); statErr != nil {
		t.Fatalf("a refused removal deleted the proposal anyway: %v", statErr)
	}
}

// The unstated mode is the strict one, so a caller that says nothing gets the
// door that insists on naming the bytes rather than the one that deletes
// whatever is there.
func TestRemoveInboxNoteRefusesAnUnnamedRemovalThatNamesNoBytes(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{RelPath: candidate.RelPath})
	if !errors.Is(err, ErrUnboundDecision) {
		t.Fatalf("removal with neither hash nor mode = %v, want ErrUnboundDecision", err)
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); statErr != nil {
		t.Fatalf("a refused removal deleted the proposal anyway: %v", statErr)
	}
}

func TestRemoveInboxNoteRefusesAnUnrecognisedMode(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath: candidate.RelPath,
		Mode:    InboxRemovalMode("whatever"),
	})
	if !errors.Is(err, ErrUnboundDecision) {
		t.Fatalf("removal in an unrecognised mode = %v, want ErrUnboundDecision", err)
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); statErr != nil {
		t.Fatalf("a refused removal deleted the proposal anyway: %v", statErr)
	}
}

// A proposal nobody could read has no hash to name, and refusing to let the
// user throw it away would leave them with a file they can neither accept nor
// be rid of. The escape hatch stays, and it has to be asked for by name.
func TestRemoveInboxNoteRemovesAnUnreadableCandidateWithoutAHash(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/broken.md", "---\nnot: [valid\n")

	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath: "inbox/broken.md",
		Mode:    RemoveUnreadableCandidate,
	}); err != nil {
		t.Fatalf("remove an unreadable candidate: %v", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("the unreadable candidate survived: %v", err)
	}
}

// The session cleaner and the tidying after a decision that already landed are
// idempotent housekeeping over a manifest, not a user deciding about text.
func TestRemoveInboxNoteRemovesRetiredBytesWithoutAHash(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/retired.md", "candidate")

	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath: "inbox/retired.md",
		Mode:    RemoveRetiredCandidate,
	}); err != nil {
		t.Fatalf("remove retired bytes: %v", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("the retired candidate survived: %v", err)
	}
	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath: "inbox/retired.md",
		Mode:    RemoveRetiredCandidate,
	}); err != nil {
		t.Fatalf("retired cleanup is not idempotent: %v", err)
	}
}

// A decided removal whose file has already gone is the outcome the user asked
// for. There are no bytes left to protect, so it is not a refusal.
func TestRemoveInboxNoteToleratesADecidedRemovalOfAMissingFile(t *testing.T) {
	vault := newTestVault(t)
	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/never-existed.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash("anything"),
	}); err != nil {
		t.Fatalf("decided removal of a missing file = %v, want it tolerated", err)
	}
}
