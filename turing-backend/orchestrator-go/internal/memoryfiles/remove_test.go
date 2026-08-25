package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func writeVaultFile(t *testing.T, vault *Vault, relPath string, content string) string {
	t.Helper()
	full := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("prepare %q: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write %q: %v", relPath, err)
	}
	return full
}

func TestRemoveInboxNoteDeletesUnderInbox(t *testing.T) {
	vault := newTestVault(t)
	full := writeVaultFile(t, vault, "inbox/note.md", "candidate")
	if err := vault.RemoveInboxNote(context.Background(), "inbox/note.md"); err != nil {
		t.Fatalf("remove inbox note: %v", err)
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("expected the candidate to be gone, got %v", err)
	}
}

func TestRemoveInboxNoteToleratesMissingTarget(t *testing.T) {
	vault := newTestVault(t)
	if err := vault.RemoveInboxNote(context.Background(), "inbox/never-existed.md"); err != nil {
		t.Fatalf("expected a missing candidate to be tolerated for idempotent cleanup, got %v", err)
	}
}

func TestRemoveInboxNoteToleratesMissingInboxDirectory(t *testing.T) {
	root := t.TempDir()
	vault, err := Open(root)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	if err := vault.RemoveInboxNote(context.Background(), "inbox/note.md"); err != nil {
		t.Fatalf("expected a missing inbox to be tolerated, got %v", err)
	}
}

func TestRemoveInboxNoteRefusesEverythingOutsideInbox(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, PersonaFileName, "persona")
	writeVaultFile(t, vault, ProfileFileName, "profile")
	writeVaultFile(t, vault, "beliefs/kept.md", "belief")

	refused := append([]string{"beliefs/kept.md"}, escapingRelPathValues()...)
	for _, relPath := range refused {
		err := vault.RemoveInboxNote(context.Background(), relPath)
		if err == nil {
			t.Fatalf("expected removeInboxNote to refuse %q", relPath)
		}
		if !errors.Is(err, ErrConfinement) {
			t.Fatalf("expected a confinement refusal for %q, got %v", relPath, err)
		}
	}
	for _, relPath := range []string{PersonaFileName, ProfileFileName, "beliefs/kept.md"} {
		if _, err := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(relPath))); err != nil {
			t.Fatalf("%q was removed despite the refusal: %v", relPath, err)
		}
	}
}

func TestRemoveInboxNoteRefusesSymlink(t *testing.T) {
	vault := newTestVault(t)
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(vault.Root(), InboxDirName, "link.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := vault.RemoveInboxNote(context.Background(), "inbox/link.md"); err == nil {
		t.Fatal("expected a symlinked candidate to be refused")
	}
	if _, err := os.Lstat(outside); err != nil {
		t.Fatalf("the symlink target was disturbed: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("the symlink itself was removed: %v", err)
	}
}

func TestRemoveInboxNoteRefusesDirectory(t *testing.T) {
	vault := newTestVault(t)
	nested := filepath.Join(vault.Root(), InboxDirName, "folder")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("make nested dir: %v", err)
	}
	if err := vault.RemoveInboxNote(context.Background(), "inbox/folder"); err == nil {
		t.Fatal("expected a directory to be refused")
	}
	if _, err := os.Lstat(nested); err != nil {
		t.Fatalf("the directory was removed: %v", err)
	}
}

func TestRemoveInboxNoteSyncsTheParentDirectory(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/note.md", "candidate")
	var directorySyncs atomic.Int64
	realSync := vault.syncDirectory
	vault.syncDirectory = func(directory *os.File) error {
		directorySyncs.Add(1)
		return realSync(directory)
	}
	if err := vault.RemoveInboxNote(context.Background(), "inbox/note.md"); err != nil {
		t.Fatalf("remove inbox note: %v", err)
	}
	if directorySyncs.Load() == 0 {
		t.Fatal("expected the parent directory to be fsynced after the deletion")
	}
}

func TestRemoveInboxNoteHonoursContextCancellation(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/note.md", "candidate")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := vault.RemoveInboxNote(ctx, "inbox/note.md"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
