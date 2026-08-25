package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %q: %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("no unix stat for %q", path)
	}
	return uint64(stat.Ino)
}

func seedProfileEditCandidate(t *testing.T, vault *Vault) InboxNote {
	t.Helper()
	return seedCandidate(t, vault, KindProfileEdit, "Call me Miguel", "The user goes by Miguel.")
}

func TestApplyProfileEditUpdatesProfileInPlace(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	profilePath := writeVaultFile(t, vault, ProfileFileName, "# Profile\n\nOld text.\n")
	before := inodeOf(t, profilePath)

	applied, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:    candidate.RelPath,
		TargetRelPath:       ProfileFileName,
		ExpectedContentHash: ContentHash("# Profile\n\nOld text.\n"),
		Content:             "# Profile\n\nGoes by Miguel.\n",
	})
	if err != nil {
		t.Fatalf("apply profile edit: %v", err)
	}
	if applied.ContentHash != ContentHash("# Profile\n\nGoes by Miguel.\n") {
		t.Fatalf("content hash = %q", applied.ContentHash)
	}
	onDisk, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if string(onDisk) != "# Profile\n\nGoes by Miguel.\n" {
		t.Fatalf("profile content = %q", onDisk)
	}
	if after := inodeOf(t, profilePath); after != before {
		t.Fatalf("profile.md was replaced (inode %d -> %d); Obsidian keeps the old inode open", before, after)
	}
}

func TestApplyProfileEditTruncatesLeftoverBytes(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	original := "# Profile\n\n" + strings.Repeat("long ", 200) + "\n"
	profilePath := writeVaultFile(t, vault, ProfileFileName, original)

	if _, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:    candidate.RelPath,
		TargetRelPath:       ProfileFileName,
		ExpectedContentHash: ContentHash(original),
		Content:             "short\n",
	}); err != nil {
		t.Fatalf("apply profile edit: %v", err)
	}
	onDisk, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if string(onDisk) != "short\n" {
		t.Fatalf("leftover bytes survived the shorter write: %q", onDisk)
	}
}

func TestApplyProfileEditRefusesAStaleHashWithoutLosingUserText(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	userText := "# Profile\n\nThe user finished typing this before Turing applied anything.\n"
	profilePath := writeVaultFile(t, vault, ProfileFileName, userText)

	_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:    candidate.RelPath,
		TargetRelPath:       ProfileFileName,
		ExpectedContentHash: ContentHash("# Profile\n\nWhat Turing read a minute ago.\n"),
		Content:             "# Profile\n\nWhat Turing wanted to write.\n",
	})
	if err == nil {
		t.Fatal("expected a stale compare-and-set to be refused")
	}
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a typed stale-content error, got %v", err)
	}
	message := err.Error()
	if !strings.Contains(message, "the file changed") {
		t.Fatalf("refusal %q does not say the file changed", message)
	}
	if !strings.Contains(message, "re-read") {
		t.Fatalf("refusal %q does not tell the caller to re-read", message)
	}
	onDisk, readErr := os.ReadFile(profilePath)
	if readErr != nil {
		t.Fatalf("read profile: %v", readErr)
	}
	if string(onDisk) != userText {
		t.Fatalf("the user's finished edit was lost by a refusal: %q", onDisk)
	}
}

func TestApplyProfileEditCreatesProfileWhenAbsentAndNoHashExpected(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)

	applied, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath: candidate.RelPath,
		TargetRelPath:    ProfileFileName,
		Content:          "# Profile\n\nFirst ever.\n",
	})
	if err != nil {
		t.Fatalf("apply profile edit: %v", err)
	}
	if applied.RelPath != ProfileFileName {
		t.Fatalf("rel path = %q", applied.RelPath)
	}
	onDisk, err := os.ReadFile(filepath.Join(vault.Root(), ProfileFileName))
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if string(onDisk) != "# Profile\n\nFirst ever.\n" {
		t.Fatalf("profile content = %q", onDisk)
	}
}

func TestApplyProfileEditRefusesAMissingProfileWhenAHashWasExpected(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)

	_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:    candidate.RelPath,
		TargetRelPath:       ProfileFileName,
		ExpectedContentHash: ContentHash("something"),
		Content:             "# Profile\n",
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a stale-content refusal for a vanished profile, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), ProfileFileName)); !os.IsNotExist(err) {
		t.Fatalf("a refused apply created the profile anyway: %v", err)
	}
}

func TestApplyProfileEditRefusesPersonaAndEveryOtherTarget(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	personaPath := writeVaultFile(t, vault, PersonaFileName, "persona text")

	targets := []string{
		PersonaFileName,
		"inbox/profile.md",
		"beliefs/profile.md",
		"../profile.md",
		"/etc/profile.md",
		"data/turing.db",
		"",
	}
	for _, target := range targets {
		_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
			CandidateRelPath: candidate.RelPath,
			TargetRelPath:    target,
			Content:          "overwritten",
		})
		if !errors.Is(err, ErrConfinement) {
			t.Fatalf("expected target %q to be refused, got %v", target, err)
		}
	}
	onDisk, err := os.ReadFile(personaPath)
	if err != nil {
		t.Fatalf("read persona: %v", err)
	}
	if string(onDisk) != "persona text" {
		t.Fatalf("persona.md was written: %q", onDisk)
	}
}

func TestApplyProfileEditRefusesABeliefCandidate(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")
	writeVaultFile(t, vault, ProfileFileName, "original")

	_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:    candidate.RelPath,
		TargetRelPath:       ProfileFileName,
		ExpectedContentHash: ContentHash("original"),
		Content:             "rewritten",
	})
	if !errors.Is(err, ErrKind) {
		t.Fatalf("expected a belief candidate to be refused, got %v", err)
	}
	onDisk, readErr := os.ReadFile(filepath.Join(vault.Root(), ProfileFileName))
	if readErr != nil {
		t.Fatalf("read profile: %v", readErr)
	}
	if string(onDisk) != "original" {
		t.Fatalf("profile was written by a belief candidate: %q", onDisk)
	}
}

func TestApplyProfileEditRefusesACandidateWithoutADeclaredKind(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/hand.md", "# Hand written\n\nNo frontmatter at all.\n")
	writeVaultFile(t, vault, ProfileFileName, "original")

	_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:    "inbox/hand.md",
		TargetRelPath:       ProfileFileName,
		ExpectedContentHash: ContentHash("original"),
		Content:             "rewritten",
	})
	if !errors.Is(err, ErrKind) {
		t.Fatalf("expected an undeclared candidate kind to be refused, got %v", err)
	}
}

func TestApplyProfileEditRefusesACandidateOutsideInbox(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/candidate.md", "---\nkind: \"profile_edit\"\n---\nbody\n")
	writeVaultFile(t, vault, ProfileFileName, "original")

	candidates := append([]string{"beliefs/candidate.md"}, escapingRelPathValues()...)
	for _, source := range candidates {
		_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
			CandidateRelPath:    source,
			TargetRelPath:       ProfileFileName,
			ExpectedContentHash: ContentHash("original"),
			Content:             "rewritten",
		})
		if !errors.Is(err, ErrConfinement) {
			t.Fatalf("expected candidate %q to be refused, got %v", source, err)
		}
	}
}

func TestApplyProfileEditRefusesASymlinkedProfile(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(outside, []byte("untouched"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(vault.Root(), ProfileFileName)); err != nil {
		t.Fatalf("symlink profile: %v", err)
	}
	if _, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath: candidate.RelPath,
		TargetRelPath:    ProfileFileName,
		Content:          "written through the link",
	}); err == nil {
		t.Fatal("expected a symlinked profile.md to be refused")
	}
	onDisk, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read outside file: %v", err)
	}
	if string(onDisk) != "untouched" {
		t.Fatalf("the symlink target was written through: %q", onDisk)
	}
}

func TestApplyProfileEditFsyncsTheFileAndItsParent(t *testing.T) {
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	candidate := seedProfileEditCandidate(t, vault)
	profilePath := writeVaultFile(t, vault, ProfileFileName, "original")
	profile := inodeOf(t, profilePath)
	root := inodeOf(t, vault.Root())

	if _, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:    candidate.RelPath,
		TargetRelPath:       ProfileFileName,
		ExpectedContentHash: ContentHash("original"),
		Content:             "rewritten",
	}); err != nil {
		t.Fatalf("apply profile edit: %v", err)
	}
	if !recorder.syncedFile(profile) {
		t.Fatal("expected profile.md itself to be fsynced")
	}
	if !recorder.syncedDirectory(root) {
		t.Fatal("expected the vault root to be fsynced")
	}
}

func TestApplyProfileEditRefusesOverLargeContent(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath: candidate.RelPath,
		TargetRelPath:    ProfileFileName,
		Content:          strings.Repeat("a", MaxNoteBytes+1),
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected over-large profile content to be refused, got %v", err)
	}
}

func TestApplyProfileEditHonoursContextCancellation(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := vault.ApplyProfileEdit(ctx, ApplyProfileEditRequest{
		CandidateRelPath: candidate.RelPath,
		TargetRelPath:    ProfileFileName,
		Content:          "x",
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
