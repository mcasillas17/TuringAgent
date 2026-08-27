package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// A promotion that cannot finish deletes a file, and both halves of it do: the
// copy it installed under beliefs/, and the original under inbox/ once the copy
// is committed. Each of those unlinks names a *name*, and the entry under that
// name is not a fact anybody holds — Obsidian, a sync client and Turing's own
// writer all replace a file by writing a new one beside it and renaming over the
// top, so between the check and the unlink the name can be somebody else's file.
//
// These tests stand at the three moments that window is open — before the entry
// leaves its name, while it is off its name, and while it is going back — and
// hold both removals to the same rule the rejection path already keeps: what is
// deleted is the entry that was verified, or nothing is deleted at all.

// vaultDirEntries lists one confined vault directory by name.
func vaultDirEntries(t *testing.T, vault *Vault, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(vault.Root(), dir))
	if err != nil {
		t.Fatalf("read %q: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// stagingResidueIn lists the reserved private names a vault directory is left
// holding. A detach that finished leaves none.
func stagingResidueIn(t *testing.T, vault *Vault, dir string) []string {
	t.Helper()
	var staged []string
	for _, name := range vaultDirEntries(t, vault, dir) {
		if strings.HasPrefix(name, stagingPrefix) {
			staged = append(staged, name)
		}
	}
	return staged
}

func requireNoStagingResidueIn(t *testing.T, vault *Vault, dir string) {
	t.Helper()
	if staged := stagingResidueIn(t, vault, dir); len(staged) != 0 {
		t.Fatalf("%s/ was left holding staging entries %v", dir, staged)
	}
}

// promotionVault opens a vault whose durability hooks, detach barrier and
// no-clobber link are all under the test's control, which is what lets one test
// fail a specific fsync *and* stand inside the detach that failure triggers.
func promotionVault(t *testing.T, hooks syncHooks, barrier detachHook, link linkHook) *Vault {
	t.Helper()
	vault, err := openVaultWithDetachSeams(newTestVaultRoot(t), hooks, barrier, link)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return vault
}

// inBeliefs and inInbox scope a barrier to one half of the promotion, so a test
// can interfere with the copy without also interfering with the original.
func inBeliefs(clean string) bool { return strings.HasPrefix(clean, BeliefsDirName+"/") }

func inInbox(clean string) bool { return strings.HasPrefix(clean, InboxDirName+"/") }

// replaceVaultEntry puts a different inode under a name, the way a sync client
// or a "save as" does.
func replaceVaultEntry(t *testing.T, vault *Vault, clean string, content string) {
	t.Helper()
	full := filepath.Join(vault.Root(), filepath.FromSlash(clean))
	staged := filepath.Join(filepath.Dir(full), "replacement-in-flight.md")
	if err := os.WriteFile(staged, []byte(content), 0o600); err != nil {
		t.Fatalf("write the replacement: %v", err)
	}
	if err := os.Rename(staged, full); err != nil {
		t.Fatalf("install the replacement: %v", err)
	}
}

// takeTheName is the third writer arriving at a name while it is free.
func takeTheName(t *testing.T, vault *Vault, clean string, content string) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(vault.Root(), filepath.FromSlash(clean)),
		[]byte(content),
		0o600,
	); err != nil {
		t.Fatalf("take the name %q: %v", clean, err)
	}
}

// seedBelief writes the candidate every promotion test starts from. It runs
// before any failure is armed, so what a test breaks is the promotion and never
// the setup.
func seedBelief(t *testing.T, vault *Vault) InboxNote {
	t.Helper()
	return seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")
}

// promoteCandidate runs one managed promotion bound to the bytes it was seeded
// with.
func promoteCandidate(ctx context.Context, vault *Vault, candidate InboxNote) error {
	_, err := vault.PromoteToBeliefs(ctx, PromoteToBeliefsRequest{
		SourceRelPath:       candidate.RelPath,
		Mode:                PromoteManagedCandidate,
		ExpectedContentHash: candidate.ContentHash,
	})
	return err
}

// The rollback's whole licence is that the file under the name is the copy this
// promotion installed. An editor saving in place keeps the inode, so identity
// alone says yes to words the user typed after the install — and the rollback
// would delete them. This exercises the production function, not the predicate
// underneath it: drop the hash from either and this must fail.
func TestPromotionRollbackRefusesACopyRewrittenUnderTheSameInode(t *testing.T) {
	// The same length on purpose. A rewrite that is also a different size is
	// caught by the read bound before the hash is ever consulted, and this test
	// exists to hold the hash itself.
	const installed = "installed by this promotion\n"
	const rewritten = "the user typed over it here\n"
	vault := newTestVault(t)
	if len(rewritten) != len(installed) {
		t.Fatalf("the fixtures differ in length (%d vs %d), so this proves nothing about the bytes", len(rewritten), len(installed))
	}
	parent, leaf, err := vault.openParent(context.Background(), "beliefs/rolled-back.md", false)
	if err != nil {
		t.Fatalf("open beliefs: %v", err)
	}
	defer func() { _ = parent.Close() }()

	full := filepath.Join(vault.Root(), BeliefsDirName, leaf)
	if err := os.WriteFile(full, []byte(installed), 0o600); err != nil {
		t.Fatalf("write the installed copy: %v", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), leaf, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		t.Fatalf("stat the installed copy: %v", err)
	}

	overwriteInPlace(t, full, rewritten)
	if after := inodeOf(t, full); after != uint64(stat.Ino) {
		t.Fatalf("the in-place edit changed the inode (%d -> %d), so this proves nothing", stat.Ino, after)
	}

	if err := vault.removeInstalledCopy(parent, leaf, "beliefs/rolled-back.md", stat, installed); err == nil {
		t.Fatal("the rollback accepted an inode carrying bytes this promotion never installed")
	}
	onDisk, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("the rollback deleted words the promotion never wrote: %v", readErr)
	}
	if string(onDisk) != rewritten {
		t.Fatalf("content = %q, want the rewritten note %q", onDisk, rewritten)
	}
	requireNoStagingResidueIn(t, vault, BeliefsDirName)
}

// The other half of the same licence. Identical bytes are not the same file: a
// different inode under the name is another writer's, it may have links,
// descriptors and a history of its own, and this promotion did not put it there.
// The hash cannot see that at all, so the inode check is what answers it.
func TestPromotionRollbackRefusesACopyReplacedByAnIdenticalFile(t *testing.T) {
	const installed = "installed by this promotion\n"
	vault := newTestVault(t)
	parent, leaf, err := vault.openParent(context.Background(), "beliefs/rolled-back.md", false)
	if err != nil {
		t.Fatalf("open beliefs: %v", err)
	}
	defer func() { _ = parent.Close() }()

	full := filepath.Join(vault.Root(), BeliefsDirName, leaf)
	if err := os.WriteFile(full, []byte(installed), 0o600); err != nil {
		t.Fatalf("write the installed copy: %v", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), leaf, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		t.Fatalf("stat the installed copy: %v", err)
	}

	replaceVaultEntry(t, vault, "beliefs/rolled-back.md", installed)
	if after := inodeOf(t, full); after == uint64(stat.Ino) {
		t.Fatalf("the replacement kept inode %d, so this proves nothing about identity", after)
	}

	if err := vault.removeInstalledCopy(parent, leaf, "beliefs/rolled-back.md", stat, installed); err == nil {
		t.Fatal("the rollback deleted a file it never installed because the bytes happened to match")
	}
	onDisk, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("the rollback deleted another writer's file: %v", readErr)
	}
	if string(onDisk) != installed {
		t.Fatalf("content = %q", onDisk)
	}
	requireNoStagingResidueIn(t, vault, BeliefsDirName)
}

// The same rule on the inbox side, through the whole promotion: the original is
// removed because it is the file that was promoted, and a byte-identical file
// that took its name is not that file.
func TestPromotedSourceRemovalRefusesAnOriginalReplacedByAnIdenticalFile(t *testing.T) {
	var vault *Vault
	var replaced string
	vault = promotionVault(t, realSyncHooks(), func(phase detachPhase, clean string) {
		if !inInbox(clean) || phase != detachPhaseBeforeDetach || replaced == clean {
			return
		}
		replaced = clean
		content, err := os.ReadFile(filepath.Join(vault.Root(), filepath.FromSlash(clean)))
		if err != nil {
			t.Errorf("read the original: %v", err)
			return
		}
		replaceVaultEntry(t, vault, clean, string(content))
	}, nil)

	candidate := seedBelief(t, vault)
	sourcePath := filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))
	before := inodeOf(t, sourcePath)
	if err := promoteCandidate(context.Background(), vault, candidate); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("expected the promotion to be abandoned, got %v", err)
	}
	if after := inodeOf(t, sourcePath); after == before {
		t.Fatalf("the replacement kept inode %d, so this proves nothing about identity", after)
	}
	onDisk, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		t.Fatalf("another writer's file was deleted: %v", readErr)
	}
	if string(onDisk) != candidate.Content {
		t.Fatalf("the replacement was disturbed: %q", onDisk)
	}
	if names := vaultDirEntries(t, vault, BeliefsDirName); len(names) != 0 {
		t.Fatalf("a belief was kept from a move that was abandoned: %v", names)
	}
	requireNoStagingResidueIn(t, vault, InboxDirName)
}

// A replacement that arrives before the copy leaves its name must be put back,
// not unlinked. The barrier is the proof that the removal is a detach at all: a
// rollback that unlinks by name never reaches it.
func TestPromotionRollbackPutsBackABeliefThatTookTheNameBeforeTheDetach(t *testing.T) {
	const replacement = "a belief the user wrote themselves\n"
	recorder := &syncRecorder{}
	var vault *Vault
	saw := map[detachPhase]bool{}
	vault = promotionVault(t, recorder.hooks(), func(phase detachPhase, clean string) {
		if !inBeliefs(clean) {
			return
		}
		saw[phase] = true
		if phase == detachPhaseBeforeDetach {
			replaceVaultEntry(t, vault, clean, replacement)
		}
	}, nil)
	candidate := seedBelief(t, vault)
	// Sync 1 is beliefs/, sync 2 is the vault root above it: failing the second
	// aborts the promotion while the original is still untouched, which is the
	// one moment the rollback runs.
	recorder.setFailDirectorySyncNumber(2)

	err := promoteCandidate(context.Background(), vault, candidate)
	if err == nil {
		t.Fatal("expected the failed hierarchy fsync to fail the promotion")
	}
	if !saw[detachPhaseBeforeDetach] {
		t.Fatal("the rollback removed the name without atomically detaching it first")
	}
	names := vaultDirEntries(t, vault, BeliefsDirName)
	if len(names) != 1 {
		t.Fatalf("beliefs/ holds %v, want only the file that took the name", names)
	}
	held, readErr := os.ReadFile(filepath.Join(vault.Root(), BeliefsDirName, names[0]))
	if readErr != nil || string(held) != replacement {
		t.Fatalf("the file that took the name was disturbed: %q, %v", held, readErr)
	}
	requireNoStagingResidueIn(t, vault, BeliefsDirName)
	if !strings.Contains(err.Error(), "could not be removed") {
		t.Fatalf("the refusal does not say the copy is still there: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))); statErr != nil {
		t.Fatalf("the original was disturbed by a promotion that never happened: %v", statErr)
	}
}

// The name is free while the copy is detached, so a file can arrive under it
// then. The rollback holds a verified descriptor on its own copy and removes
// exactly that; the newcomer stays, and the promotion says the copy went away
// because it did.
func TestPromotionRollbackLeavesABeliefThatTookTheNameAfterTheDetach(t *testing.T) {
	const newcomer = "a belief that arrived while the copy was off its name\n"
	recorder := &syncRecorder{}
	var vault *Vault
	saw := map[detachPhase]bool{}
	vault = promotionVault(t, recorder.hooks(), func(phase detachPhase, clean string) {
		if !inBeliefs(clean) {
			return
		}
		saw[phase] = true
		if phase == detachPhaseBeforeVerify {
			takeTheName(t, vault, clean, newcomer)
		}
	}, nil)
	candidate := seedBelief(t, vault)
	recorder.setFailDirectorySyncNumber(2)

	err := promoteCandidate(context.Background(), vault, candidate)
	if err == nil {
		t.Fatal("expected the failed hierarchy fsync to fail the promotion")
	}
	if !saw[detachPhaseBeforeVerify] {
		t.Fatal("the rollback never had the copy off its name, so nothing verified what it unlinked")
	}
	names := vaultDirEntries(t, vault, BeliefsDirName)
	if len(names) != 1 {
		t.Fatalf("beliefs/ holds %v, want only the file that took the name", names)
	}
	held, readErr := os.ReadFile(filepath.Join(vault.Root(), BeliefsDirName, names[0]))
	if readErr != nil || string(held) != newcomer {
		t.Fatalf("the file that took the freed name was disturbed: %q, %v", held, readErr)
	}
	requireNoStagingResidueIn(t, vault, BeliefsDirName)
	if strings.Contains(err.Error(), "could not be removed") {
		t.Fatalf("the refusal claims the copy is still there when it is not: %v", err)
	}
}

// The contested case on the beliefs side. The detached file is not this
// promotion's copy, so it may not be deleted; its own name has been taken, so it
// cannot go back. It may not be published as a belief either — reconcile would
// index it as one the user never accepted — so it is kept under the reserved
// name the walk steps over, and the refusal says exactly where.
func TestPromotionRollbackKeepsAContestedBeliefUnderAReservedNameAndSaysWhere(t *testing.T) {
	const replacement = "a belief the user wrote themselves\n"
	const contender = "a third file, under the same name\n"
	recorder := &syncRecorder{}
	var vault *Vault
	vault = promotionVault(t, recorder.hooks(), func(phase detachPhase, clean string) {
		if !inBeliefs(clean) {
			return
		}
		switch phase {
		case detachPhaseBeforeDetach:
			replaceVaultEntry(t, vault, clean, replacement)
		case detachPhaseBeforeRestore:
			takeTheName(t, vault, clean, contender)
		}
	}, nil)
	candidate := seedBelief(t, vault)
	recorder.setFailDirectorySyncNumber(2)

	err := promoteCandidate(context.Background(), vault, candidate)
	if err == nil {
		t.Fatal("expected the failed hierarchy fsync to fail the promotion")
	}
	staged := stagingResidueIn(t, vault, BeliefsDirName)
	if len(staged) != 1 {
		t.Fatalf("expected the contested file to be kept under one reserved name, found %v", staged)
	}
	kept, readErr := os.ReadFile(filepath.Join(vault.Root(), BeliefsDirName, staged[0]))
	if readErr != nil || string(kept) != replacement {
		t.Fatalf("the reserved name holds %q, want the detached replacement %q (%v)", kept, replacement, readErr)
	}
	if !strings.Contains(err.Error(), staged[0]) {
		t.Fatalf("the refusal does not name where the file was kept: %v", err)
	}
	if strings.Contains(err.Error(), replacement) {
		t.Fatalf("the refusal leaked what was in the file: %v", err)
	}
	for _, name := range vaultDirEntries(t, vault, BeliefsDirName) {
		if IsRecoveryDraftName(name) {
			t.Fatalf("a belief nobody accepted was published under the indexed name %q", name)
		}
	}
	// The contender kept its name and its bytes.
	var visible []string
	for _, name := range vaultDirEntries(t, vault, BeliefsDirName) {
		if !strings.HasPrefix(name, stagingPrefix) {
			visible = append(visible, name)
		}
	}
	if len(visible) != 1 {
		t.Fatalf("beliefs/ shows %v, want only the file that took the name", visible)
	}
	held, readErr := os.ReadFile(filepath.Join(vault.Root(), BeliefsDirName, visible[0]))
	if readErr != nil || string(held) != contender {
		t.Fatalf("the file that took the name was disturbed: %q, %v", held, readErr)
	}
}

// The bytes kept under the reserved name are not a belief anybody accepted. The
// walk has to keep stepping over them, so nothing downstream reconciles them
// into memory as an active note.
func TestABeliefKeptUnderAReservedNameIsNotIndexedAsActive(t *testing.T) {
	const replacement = "a belief the user wrote themselves\n"
	recorder := &syncRecorder{}
	var vault *Vault
	vault = promotionVault(t, recorder.hooks(), func(phase detachPhase, clean string) {
		if !inBeliefs(clean) {
			return
		}
		switch phase {
		case detachPhaseBeforeDetach:
			replaceVaultEntry(t, vault, clean, replacement)
		case detachPhaseBeforeRestore:
			takeTheName(t, vault, clean, "a third file, under the same name\n")
		}
	}, nil)
	candidate := seedBelief(t, vault)
	recorder.setFailDirectorySyncNumber(2)

	if err := promoteCandidate(context.Background(), vault, candidate); err == nil {
		t.Fatal("expected the failed hierarchy fsync to fail the promotion")
	}
	staged := stagingResidueIn(t, vault, BeliefsDirName)
	if len(staged) != 1 {
		t.Fatalf("expected one reserved name, found %v", staged)
	}
	relPath := BeliefsDirName + "/" + staged[0]

	scan, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan the vault: %v", err)
	}
	for _, note := range scan.Notes {
		if note.RelPath == relPath {
			t.Fatalf("the reserved recovery entry was indexed as an active belief: %+v", note)
		}
	}
	skipped := false
	for _, entry := range scan.Skipped {
		if entry.RelPath == relPath {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("the reserved recovery entry was neither indexed nor reported as skipped: %+v", scan.Skipped)
	}
}

// The inbox half of the move. A file that took the original's name before the
// detach is the user's, and the promotion is abandoned rather than deleting it.
func TestPromotedSourceRemovalPutsBackAnOriginalReplacedBeforeTheDetach(t *testing.T) {
	const replacement = "the user replaced their own draft\n"
	var vault *Vault
	saw := map[detachPhase]bool{}
	vault = promotionVault(t, realSyncHooks(), func(phase detachPhase, clean string) {
		if !inInbox(clean) {
			return
		}
		saw[phase] = true
		if phase == detachPhaseBeforeDetach {
			replaceVaultEntry(t, vault, clean, replacement)
		}
	}, nil)

	candidate := seedBelief(t, vault)
	err := promoteCandidate(context.Background(), vault, candidate)
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("expected the promotion to be abandoned, got %v", err)
	}
	if !saw[detachPhaseBeforeDetach] {
		t.Fatal("the source removal unlinked by name without atomically detaching it first")
	}
	sourcePath := filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))
	onDisk, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		t.Fatalf("the user's replacement was deleted: %v", readErr)
	}
	if string(onDisk) != replacement {
		t.Fatalf("the user's replacement was disturbed: %q", onDisk)
	}
	if names := vaultDirEntries(t, vault, BeliefsDirName); len(names) != 0 {
		t.Fatalf("a belief was promoted from bytes the user had already replaced: %v", names)
	}
	requireNoStagingResidueIn(t, vault, InboxDirName)
}

// A file that arrives at the original's name *after* it left is somebody else's
// too — and this time the promotion really did happen, because the original was
// verified off its name and removed. Both facts have to be true at once.
func TestPromotedSourceRemovalLeavesAFileThatTookTheNameAfterTheDetach(t *testing.T) {
	const newcomer = "a draft that arrived while the original was off its name\n"
	var vault *Vault
	saw := map[detachPhase]bool{}
	vault = promotionVault(t, realSyncHooks(), func(phase detachPhase, clean string) {
		if !inInbox(clean) {
			return
		}
		saw[phase] = true
		if phase == detachPhaseBeforeVerify {
			takeTheName(t, vault, clean, newcomer)
		}
	}, nil)

	candidate := seedBelief(t, vault)
	if err := promoteCandidate(context.Background(), vault, candidate); err != nil {
		t.Fatalf("the original was the promoted file, so the move must finish: %v", err)
	}
	if !saw[detachPhaseBeforeVerify] {
		t.Fatal("the source removal never had the original off its name")
	}
	sourcePath := filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))
	onDisk, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		t.Fatalf("the file that took the freed name was deleted: %v", readErr)
	}
	if string(onDisk) != newcomer {
		t.Fatalf("the file that took the freed name was disturbed: %q", onDisk)
	}
	if names := vaultDirEntries(t, vault, BeliefsDirName); len(names) != 1 {
		t.Fatalf("beliefs/ holds %v, want the promoted note", names)
	}
	requireNoStagingResidueIn(t, vault, InboxDirName)
}

// The contested case on the inbox side. Here the bytes may be published: an
// inbox draft nobody has read is exactly what the visible recovery name is for,
// and the user can see it and delete it like any other file in their inbox.
func TestPromotedSourceRemovalRescuesAContestedOriginalIntoAVisibleDraft(t *testing.T) {
	const replacement = "the user replaced their own draft\n"
	const contender = "a third file, under the same name\n"
	var vault *Vault
	vault = promotionVault(t, realSyncHooks(), func(phase detachPhase, clean string) {
		if !inInbox(clean) {
			return
		}
		switch phase {
		case detachPhaseBeforeDetach:
			replaceVaultEntry(t, vault, clean, replacement)
		case detachPhaseBeforeRestore:
			takeTheName(t, vault, clean, contender)
		}
	}, nil)

	candidate := seedBelief(t, vault)
	err := promoteCandidate(context.Background(), vault, candidate)
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("expected the promotion to be abandoned, got %v", err)
	}
	sourcePath := filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))
	held, readErr := os.ReadFile(sourcePath)
	if readErr != nil || string(held) != contender {
		t.Fatalf("the file that took the name was disturbed: %q, %v", held, readErr)
	}
	draft, kept := requireOneRecoveryDraft(t, vault)
	if kept != replacement {
		t.Fatalf("the recovery draft holds %q, want the detached replacement %q", kept, replacement)
	}
	if !strings.Contains(err.Error(), draft) {
		t.Fatalf("the refusal does not name where the draft was kept: %v", err)
	}
	if strings.Contains(err.Error(), replacement) {
		t.Fatalf("the refusal leaked what was in the file: %v", err)
	}
	requireNoStagingResidue(t, vault)
	if names := vaultDirEntries(t, vault, BeliefsDirName); len(names) != 0 {
		t.Fatalf("a belief was promoted from bytes the user had already replaced: %v", names)
	}
}

// The source half of the same-inode rule, through the whole promotion. The user
// types into Obsidian while the move is in flight: same inode, same length, and
// every word different. Only the bytes can answer that, and the original is
// theirs to keep.
func TestPromotedSourceRemovalRefusesAnOriginalRewrittenUnderTheSameInode(t *testing.T) {
	var vault *Vault
	var edited string
	var rewritten string
	vault = promotionVault(t, realSyncHooks(), func(phase detachPhase, clean string) {
		if !inInbox(clean) || phase != detachPhaseBeforeDetach || edited == clean {
			return
		}
		edited = clean
		overwriteInPlace(t, filepath.Join(vault.Root(), filepath.FromSlash(clean)), rewritten)
	}, nil)

	candidate := seedBelief(t, vault)
	// The user's own words, exactly as long as the ones they replaced, so what
	// refuses here is the hash rather than the read bound above it.
	rewritten = strings.Repeat("x", len(candidate.Content)-1) + "\n"
	sourcePath := filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))
	before := inodeOf(t, sourcePath)

	if err := promoteCandidate(context.Background(), vault, candidate); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("expected the promotion to be abandoned, got %v", err)
	}
	if after := inodeOf(t, sourcePath); after != before {
		t.Fatalf("the edit changed the inode (%d -> %d), so this proves nothing about the bytes", before, after)
	}
	onDisk, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		t.Fatalf("the user's edited candidate was deleted: %v", readErr)
	}
	if string(onDisk) != rewritten {
		t.Fatalf("the user's edit was disturbed: %q", onDisk)
	}
	if names := vaultDirEntries(t, vault, BeliefsDirName); len(names) != 0 {
		t.Fatalf("a belief was promoted from bytes the user had already replaced: %v", names)
	}
	requireNoStagingResidueIn(t, vault, InboxDirName)
}

// An original that stops existing while the move is in flight is not a move this
// can report as finished: there is nothing left to verify the removal against.
// The promotion is abandoned whole, so the copy it had installed goes too and
// the vault is left the way whoever deleted the original left it.
func TestPromotedSourceRemovalAbandonsTheMoveWhenTheOriginalVanishes(t *testing.T) {
	var vault *Vault
	var removed string
	vault = promotionVault(t, realSyncHooks(), func(phase detachPhase, clean string) {
		if !inInbox(clean) || phase != detachPhaseBeforeDetach || removed == clean {
			return
		}
		removed = clean
		if err := os.Remove(filepath.Join(vault.Root(), filepath.FromSlash(clean))); err != nil {
			t.Errorf("remove the original: %v", err)
		}
	}, nil)

	candidate := seedBelief(t, vault)
	if err := promoteCandidate(context.Background(), vault, candidate); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("expected the promotion to be abandoned, got %v", err)
	}
	if names := vaultDirEntries(t, vault, BeliefsDirName); len(names) != 0 {
		t.Fatalf("a belief was kept from a move that could never be verified: %v", names)
	}
	if names := vaultDirEntries(t, vault, InboxDirName); len(names) != 0 {
		t.Fatalf("the inbox holds %v after the original was deleted from under the move", names)
	}
	requireNoStagingResidueIn(t, vault, InboxDirName)
	requireNoStagingResidueIn(t, vault, BeliefsDirName)
}

// A request that ends while the original is off its name is not a verdict about
// anything. The bytes go back under their own name before the cancellation is
// what this reports, and the promoted copy is rolled back with it.
func TestPromotedSourceRemovalRestoresTheOriginalWhenTheRequestIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var vault *Vault
	saw := map[detachPhase]bool{}
	vault = promotionVault(t, realSyncHooks(), func(phase detachPhase, clean string) {
		if !inInbox(clean) {
			return
		}
		saw[phase] = true
		if phase == detachPhaseBeforeVerify {
			cancel()
		}
	}, nil)

	candidate := seedBelief(t, vault)
	err := promoteCandidate(ctx, vault, candidate)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancellation to be reported, got %v", err)
	}
	if errors.Is(err, ErrSourceChanged) {
		t.Fatalf("a cancelled request claimed the candidate had changed: %v", err)
	}
	if !saw[detachPhaseBeforeVerify] {
		t.Fatal("the source removal never had the original off its name")
	}
	sourcePath := filepath.Join(vault.Root(), filepath.FromSlash(candidate.RelPath))
	onDisk, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		t.Fatalf("a cancelled request deleted the user's candidate: %v", readErr)
	}
	if string(onDisk) != candidate.Content {
		t.Fatalf("the restored candidate holds %q, want %q", onDisk, candidate.Content)
	}
	if names := vaultDirEntries(t, vault, BeliefsDirName); len(names) != 0 {
		t.Fatalf("a cancelled promotion left %v in beliefs/", names)
	}
	requireNoStagingResidue(t, vault)
	if drafts := recoveryDrafts(t, vault); len(drafts) != 0 {
		t.Fatalf("a restore that succeeded left recovery drafts behind: %v", drafts)
	}
}
