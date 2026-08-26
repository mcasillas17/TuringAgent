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
	const rewritten = "the user's own words, typed over it\n"
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	full := filepath.Join(vault.Root(), InboxDirName, "note.md")

	var once sync.Once
	recorder.setBeforeDirectorySync(func() {
		once.Do(func() { overwriteInPlace(t, full, rewritten) })
	})
	recorder.setFailDirectorySyncNumber(1)

	_, err := vault.createInboxNoteAt(context.Background(), "inbox/note.md", "the note this call wrote\n")
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
		t.Fatalf("the failure does not name the entry it had to leave behind: %v", err)
	}
	if !strings.Contains(err.Error(), "left in place") {
		t.Fatalf("the failure does not say the entry is still there: %v", err)
	}
	if strings.Contains(err.Error(), rewritten) {
		t.Fatalf("the failure leaked what was in the file: %v", err)
	}
	requireNoStagingResidue(t, vault)
}

// The same rollback, reached the other way: the write finished and the request
// did not. A caller that has gone is nobody to delete the user's words for.
func TestInstallRollbackOnACancelledRequestKeepsAnEntryRewrittenInPlace(t *testing.T) {
	const rewritten = "the user's own words, typed over it\n"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	full := filepath.Join(vault.Root(), InboxDirName, "note.md")

	var once sync.Once
	recorder.setBeforeDirectorySync(func() {
		once.Do(func() {
			overwriteInPlace(t, full, rewritten)
			cancel()
		})
	})

	_, err := vault.createInboxNoteAt(ctx, "inbox/note.md", "the note this call wrote\n")
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
	if !strings.Contains(err.Error(), "left in place") {
		t.Fatalf("the failure does not say the entry is still there: %v", err)
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
