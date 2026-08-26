package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
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
		SourceRelPath:       candidate.RelPath,
		Kind:                KindBelief,
		ExpectedContentHash: candidate.ContentHash,
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
		SourceRelPath:       candidate.RelPath,
		Kind:                KindBelief,
		ExpectedContentHash: candidate.ContentHash,
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
		SourceRelPath:       candidate.RelPath,
		DestinationRelPath:  "beliefs/preferences/dark-mode.md",
		Kind:                KindBelief,
		ExpectedContentHash: candidate.ContentHash,
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
		SourceRelPath:       candidate.RelPath,
		DestinationRelPath:  "beliefs/taken.md",
		Kind:                KindBelief,
		ExpectedContentHash: candidate.ContentHash,
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
		SourceRelPath:       candidate.RelPath,
		DestinationRelPath:  "beliefs/link.md",
		Kind:                KindBelief,
		ExpectedContentHash: candidate.ContentHash,
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
	writeVaultFile(t, vault, "inbox/huge.md", strings.Repeat("a", MaxNoteBytes+1))
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
		SourceRelPath:       "inbox/broken.md",
		Kind:                KindBelief,
		ExpectedContentHash: vaultFileHash(t, vault, "inbox/broken.md"),
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
		Mode:          PromoteUnmanagedDraft,
	})
	if err != nil {
		t.Fatalf("a candidate with no frontmatter is an unmanaged draft, promotable by file move: %v", err)
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

// overwriteInPlace edits a file the way Obsidian does: the same inode, opened
// and rewritten, never replaced. An identity check that only compares inodes
// cannot see this edit at all.
func overwriteInPlace(t *testing.T, path string, content string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		t.Fatalf("open %q for in-place edit: %v", path, err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("rewrite %q in place: %v", path, err)
	}
}

func TestPromoteToBeliefsRefusesWhenTheSourceIsEditedUnderTheSameInode(t *testing.T) {
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")
	sourcePath := filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))
	sourceInode := inodeOf(t, sourcePath)

	// The user's own words, typed into Obsidian after Turing read the file and
	// before it unlinked it. Losing them is the failure this guards.
	userEdit := strings.Replace(candidate.Content, "The user prefers dark mode.", "Actually they prefer light mode.", 1)
	if userEdit == candidate.Content {
		t.Fatal("the test edit did not change the candidate")
	}
	var once sync.Once
	recorder.setBeforeDirectorySync(func() {
		once.Do(func() { overwriteInPlace(t, sourcePath, userEdit) })
	})

	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath:       candidate.RelPath,
		Kind:                KindBelief,
		ExpectedContentHash: candidate.ContentHash,
	})
	if err == nil {
		t.Fatal("expected a source edited mid-promotion to refuse")
	}
	if !strings.Contains(err.Error(), "changed while it was being promoted") {
		t.Fatalf("refusal %q does not say what happened", err.Error())
	}
	if after := inodeOf(t, sourcePath); after != sourceInode {
		t.Fatalf("the source inode changed: %d -> %d", sourceInode, after)
	}
	onDisk, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		t.Fatalf("the user's edited candidate was deleted: %v", readErr)
	}
	if string(onDisk) != userEdit {
		t.Fatalf("the user's edit was overwritten: %q", onDisk)
	}
	entries, readErr := os.ReadDir(filepath.Join(vault.Root(), BeliefsDirName))
	if readErr != nil {
		t.Fatalf("read beliefs: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("a belief was promoted from bytes the user had already replaced: %v", entries[0].Name())
	}
}

func TestPromoteToBeliefsRefusesWhenTheSourceIsReplacedMidPromotion(t *testing.T) {
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")
	sourcePath := filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))

	var once sync.Once
	recorder.setBeforeDirectorySync(func() {
		once.Do(func() {
			// A whole-file replacement: a new inode under the same name, which
			// is what a sync client or a "save as" does.
			if err := os.Remove(sourcePath); err != nil {
				t.Errorf("remove source: %v", err)
				return
			}
			if err := os.WriteFile(sourcePath, []byte("replaced by the user\n"), 0o600); err != nil {
				t.Errorf("replace source: %v", err)
			}
		})
	})

	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath:       candidate.RelPath,
		Kind:                KindBelief,
		ExpectedContentHash: candidate.ContentHash,
	})
	if err == nil {
		t.Fatal("expected a replaced source to refuse")
	}
	onDisk, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		t.Fatalf("the replacement was deleted: %v", readErr)
	}
	if string(onDisk) != "replaced by the user\n" {
		t.Fatalf("the replacement was disturbed: %q", onDisk)
	}
	entries, readErr := os.ReadDir(filepath.Join(vault.Root(), BeliefsDirName))
	if readErr != nil {
		t.Fatalf("read beliefs: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("a belief was promoted from replaced bytes: %v", entries[0].Name())
	}
}

// The compensating delete exists to undo a promotion that never happened. If it
// runs after the original is already gone, it does not undo anything — it
// deletes the only copy of the note. The rollback therefore has to know whether
// the source actually went away, not merely that something failed.
func TestPromoteToBeliefsKeepsThePromotedCopyWhenOnlyTheFinalSyncFails(t *testing.T) {
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")
	sourcePath := filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))

	// During a promotion the directory fsyncs are, in order: the beliefs folder
	// the copy was installed into, the vault root above it, and finally the
	// inbox folder the original was just unlinked from. Failing the third one
	// fails after the source is already gone.
	recorder.setFailDirectorySyncNumber(3)

	_, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath:       candidate.RelPath,
		Mode:                PromoteManagedCandidate,
		ExpectedContentHash: candidate.ContentHash,
	})
	if err == nil {
		t.Fatal("a failed fsync must be reported, not swallowed")
	}
	if _, statErr := os.Lstat(sourcePath); !os.IsNotExist(statErr) {
		t.Fatalf("the source was expected to be gone by this point: %v", statErr)
	}
	entries, readErr := os.ReadDir(filepath.Join(vault.Root(), BeliefsDirName))
	if readErr != nil {
		t.Fatalf("read beliefs: %v", readErr)
	}
	if len(entries) != 1 {
		t.Fatalf("the promoted copy was rolled back after the original was already removed; beliefs/ holds %d files and the note is lost", len(entries))
	}
}

// The rollback deletes a file. Everything it refuses to delete is therefore
// load-bearing: if the copy it installed is already gone there is nothing to
// undo, and if the name now holds a different inode it belongs to whoever put
// it there — undoing a promotion must never turn into deleting a user's note.
func TestRemoveInstalledCopyOnlyRemovesTheFileItInstalled(t *testing.T) {
	vault := newTestVault(t)
	parent, leaf, err := vault.openParent(context.Background(), "beliefs/rolled-back.md", false)
	if err != nil {
		t.Fatalf("open beliefs: %v", err)
	}
	defer func() { _ = parent.Close() }()

	full := filepath.Join(vault.Root(), BeliefsDirName, leaf)
	if err := os.WriteFile(full, []byte("installed by this promotion\n"), 0o600); err != nil {
		t.Fatalf("write the installed copy: %v", err)
	}
	var installed unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), leaf, &installed, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		t.Fatalf("stat the installed copy: %v", err)
	}

	// Somebody else's file now occupies the name.
	if err := os.Remove(full); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.WriteFile(full, []byte("the user's own note\n"), 0o600); err != nil {
		t.Fatalf("write the user's file: %v", err)
	}
	if err := vault.removeInstalledCopy(parent, leaf, "beliefs/rolled-back.md", installed); err == nil {
		t.Fatal("expected the rollback to refuse a name it no longer owns")
	}
	onDisk, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("the rollback deleted a file it did not install: %v", readErr)
	}
	if string(onDisk) != "the user's own note\n" {
		t.Fatalf("content = %q", onDisk)
	}

	// A copy that is already gone is not an error: the undo has nothing to do.
	if err := os.Remove(full); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := vault.removeInstalledCopy(parent, leaf, "beliefs/rolled-back.md", installed); err != nil {
		t.Fatalf("a missing copy is nothing to undo: %v", err)
	}
}
