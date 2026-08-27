package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// installStagedFile promises all-or-nothing: when it reports failure, the final
// name was not installed by that call. It keeps that promise by removing the
// name it linked — and that removal is a deletion like any other, so it has to
// prove the entry under the name is still the bytes it wrote there.
//
// Two things happen inside its window that inode identity cannot see. An editor
// that already had the note open saves in place, which keeps the inode and
// changes every word; and once the entry is off its name for verification, the
// freed name can be taken by somebody else's file. Neither is this call's to
// delete, and a failed install that also eats the user's text is worse than the
// failure it was reporting.

// The sync-failure rollback, against an editor that saved in place. Identity
// says yes here and the bytes say no, and the bytes are the user's.
func TestInstallRollbackRefusesAnEntryRewrittenUnderTheSameInode(t *testing.T) {
	// The same length as what the call writes, so what refuses here is the hash
	// rather than the read bound above it.
	const written = "the note this call wrote\n"
	const rewritten = "the user's own words hey\n"
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	full := filepath.Join(vault.Root(), InboxDirName, "note.md")
	if len(rewritten) != len(written) {
		t.Fatalf("the fixtures differ in length (%d vs %d), so this proves nothing about the bytes", len(rewritten), len(written))
	}

	var once sync.Once
	recorder.setBeforeDirectorySync(func() {
		once.Do(func() { overwriteInPlace(t, full, rewritten) })
	})
	recorder.setFailDirectorySyncNumber(1)

	_, err := vault.createInboxNoteAt(context.Background(), "inbox/note.md", written)
	if err == nil {
		t.Fatal("expected the failed directory fsync to fail the create")
	}
	onDisk, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("the rollback deleted words this call never wrote: %v", readErr)
	}
	if string(onDisk) != rewritten {
		t.Fatalf("content = %q, want the rewritten note %q", onDisk, rewritten)
	}
	if !strings.Contains(err.Error(), "inbox/note.md") {
		t.Fatalf("the failure does not name the entry it could not undo: %v", err)
	}
	if !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("the failure does not say the undo could not be carried out: %v", err)
	}
	if strings.Contains(err.Error(), rewritten) {
		t.Fatalf("the failure leaked what was in the file: %v", err)
	}
	requireNoStagingResidue(t, vault)
}

// The same rollback, reached the other way: the write finished and the request
// did not. A caller that has gone is nobody to delete the user's words for.
func TestInstallRollbackOnACancelledRequestKeepsAnEntryRewrittenInPlace(t *testing.T) {
	const written = "the note this call wrote\n"
	const rewritten = "the user's own words hey\n"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	full := filepath.Join(vault.Root(), InboxDirName, "note.md")
	if len(rewritten) != len(written) {
		t.Fatalf("the fixtures differ in length (%d vs %d), so this proves nothing about the bytes", len(rewritten), len(written))
	}

	var once sync.Once
	recorder.setBeforeDirectorySync(func() {
		once.Do(func() {
			overwriteInPlace(t, full, rewritten)
			cancel()
		})
	})

	_, err := vault.createInboxNoteAt(ctx, "inbox/note.md", written)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancellation to be reported, got %v", err)
	}
	onDisk, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("a cancelled create deleted words it never wrote: %v", readErr)
	}
	if string(onDisk) != rewritten {
		t.Fatalf("content = %q, want the rewritten note %q", onDisk, rewritten)
	}
	if !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("the failure does not say the undo could not be carried out: %v", err)
	}
	if strings.Contains(err.Error(), rewritten) {
		t.Fatalf("the failure leaked what was in the file: %v", err)
	}
	requireNoStagingResidue(t, vault)
}

// While the entry is off its name for verification, the name is free. What
// arrives under it belongs to whoever put it there; the rollback removes the
// entry it verified and leaves the newcomer alone.
func TestInstallRollbackLeavesAnEntryThatTookTheNameAfterTheDetach(t *testing.T) {
	const newcomer = "a note that arrived while the name was free\n"
	recorder := &syncRecorder{}
	var vault *Vault
	saw := map[detachPhase]bool{}
	barrier := func(phase detachPhase, clean string) {
		if clean != "inbox/note.md" {
			return
		}
		saw[phase] = true
		if phase == detachPhaseBeforeVerify {
			takeTheName(t, vault, clean, newcomer)
		}
	}
	root := newTestVaultRoot(t)
	created, err := openVaultWithDetachSeams(root, recorder.hooks(), barrier, nil)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	vault = created
	recorder.setFailDirectorySyncNumber(1)

	if _, err := vault.createInboxNoteAt(context.Background(), "inbox/note.md", "the note this call wrote\n"); err == nil {
		t.Fatal("expected the failed directory fsync to fail the create")
	}
	if !saw[detachPhaseBeforeVerify] {
		t.Fatal("the rollback unlinked by name without atomically detaching the entry first")
	}
	onDisk, readErr := os.ReadFile(filepath.Join(vault.Root(), InboxDirName, "note.md"))
	if readErr != nil {
		t.Fatalf("the rollback deleted the file that took the freed name: %v", readErr)
	}
	if string(onDisk) != newcomer {
		t.Fatalf("content = %q, want the newcomer %q", onDisk, newcomer)
	}
	requireNoStagingResidue(t, vault)
}

// The ordinary rollback still has to work: an install this call really did make,
// undone because the call failed, leaves the inbox exactly as it found it.
func TestInstallRollbackRemovesTheEntryItInstalled(t *testing.T) {
	recorder := &syncRecorder{failDirectorySyncNumber: 1}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())

	if _, err := vault.createInboxNoteAt(context.Background(), "inbox/note.md", "the note this call wrote\n"); err == nil {
		t.Fatal("expected the failed directory fsync to fail the create")
	}
	if names := vaultDirEntries(t, vault, InboxDirName); len(names) != 0 {
		t.Fatalf("a create that reported failure left %v behind", names)
	}
}

// The same install runs into beliefs/ during a promotion, and there the visible
// recovery name is the wrong answer: the walk indexes it, so a file this call
// could not even prove was its own would be reconciled into memory as a belief
// the user never accepted. Under beliefs/ the bytes stay under the reserved name
// the walk steps over, and the failure says where they are.
func TestInstallRollbackKeepsAContestedBeliefOutOfTheIndex(t *testing.T) {
	const replacement = "a belief the user wrote themselves\n"
	const contender = "a third file, under the same name\n"
	recorder := &syncRecorder{}
	var vault *Vault
	barrier := func(phase detachPhase, clean string) {
		if !inBeliefs(clean) {
			return
		}
		switch phase {
		case detachPhaseBeforeDetach:
			replaceVaultEntry(t, vault, clean, replacement)
		case detachPhaseBeforeRestore:
			takeTheName(t, vault, clean, contender)
		}
	}
	created, err := openVaultWithDetachSeams(newTestVaultRoot(t), recorder.hooks(), barrier, nil)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	vault = created
	candidate := seedBelief(t, vault)
	// Sync 1 is the beliefs folder the copy is linked into, which is the fsync
	// that makes the install committed at all.
	recorder.setFailDirectorySyncNumber(1)

	promoteErr := promoteCandidate(context.Background(), vault, candidate)
	if promoteErr == nil {
		t.Fatal("expected the failed directory fsync to fail the promotion")
	}
	for _, name := range vaultDirEntries(t, vault, BeliefsDirName) {
		if IsRecoveryDraftName(name) {
			t.Fatalf("a belief nobody accepted was published under the indexed name %q", name)
		}
	}
	staged := stagingResidueIn(t, vault, BeliefsDirName)
	if len(staged) != 1 {
		t.Fatalf("expected the contested file to be kept under one reserved name, found %v", staged)
	}
	kept, readErr := os.ReadFile(filepath.Join(vault.Root(), BeliefsDirName, staged[0]))
	if readErr != nil || string(kept) != replacement {
		t.Fatalf("the reserved name holds %q, want the detached replacement %q (%v)", kept, replacement, readErr)
	}
	if !strings.Contains(promoteErr.Error(), staged[0]) {
		t.Fatalf("the failure does not name where the file was kept: %v", promoteErr)
	}
	if strings.Contains(promoteErr.Error(), replacement) {
		t.Fatalf("the failure leaked what was in the file: %v", promoteErr)
	}
	scan, scanErr := vault.Scan(context.Background())
	if scanErr != nil {
		t.Fatalf("scan the vault: %v", scanErr)
	}
	for _, note := range scan.Notes {
		if note.RelPath == BeliefsDirName+"/"+staged[0] {
			t.Fatalf("the reserved recovery entry was indexed as an active belief: %+v", note)
		}
	}
}
