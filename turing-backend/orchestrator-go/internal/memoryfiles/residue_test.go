package memoryfiles

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A removal that could not unlink leaves the bytes reachable under a reserved
// name, and the retry that follows it removes the entry under the *visible*
// name and nothing else. The record that tracked the file is then retired for a
// file that is still there, under a name no listing shows.
//
// So a withdrawal has to be able to take the reserved copies with it. What
// entitles it to is the same thing that entitles every other removal here: the
// bytes. A reserved entry whose whole content is exactly what the caller is
// allowed to delete is the caller's own file under a name it cannot name; one
// whose content is anything else belongs to somebody else and is left alone.
func TestResidueSweepRemovesOnlyBytesTheCallerCanName(t *testing.T) {
	vault := newTestVault(t)
	const mine = "the note this session wrote"
	const theirs = "a file somebody else staged"

	minePath := filepath.Join(vault.Root(), InboxDirName, stagingPrefix+"aaaaaaaaaaaaaaaaaaaaaaaa")
	theirsPath := filepath.Join(vault.Root(), InboxDirName, stagingPrefix+"bbbbbbbbbbbbbbbbbbbbbbbb")
	visible := writeVaultFile(t, vault, "inbox/note.md", mine)
	if err := os.WriteFile(minePath, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(theirsPath, []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}

	failures, err := vault.RemoveInboxResidue(context.Background(), []string{ContentHash(mine)})
	if err != nil {
		t.Fatalf("RemoveInboxResidue: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("residue sweep failures = %v, want none", failures)
	}
	if _, err := os.Lstat(minePath); !os.IsNotExist(err) {
		t.Fatalf("the reserved copy of the caller's own bytes is still there: %v", err)
	}
	if _, err := os.Lstat(theirsPath); err != nil {
		t.Fatalf("the sweep removed a reserved entry it could not name: %v", err)
	}
	// A visible note is not residue, whatever it holds: it is the file the
	// caller's own removal is about, decided under its own lock and its own
	// rules.
	if _, err := os.Lstat(visible); err != nil {
		t.Fatalf("the sweep removed a visible note: %v", err)
	}
}

// And the sweep is what makes the retry finish the job: a failed unlink leaves
// a second link, the retry removes the entry under the visible name, and the
// bytes must not survive under the reserved one.
func TestResidueSweepClearsWhatAFailedUnlinkLeftBehind(t *testing.T) {
	const decided = "the proposal this session wrote"
	failUnlink := true
	vault, err := openVaultWithRemovalSeams(
		newTestVaultRoot(t), realSyncHooks(), nil, nil,
		func(name string, unlink func() error) error {
			if failUnlink && strings.HasPrefix(name, stagingPrefix) {
				return errStagingUnlink
			}
			return unlink()
		},
	)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	writeVaultFile(t, vault, "inbox/note.md", decided)

	if err := vault.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", decided)); err == nil {
		t.Fatal("a removal whose unlink failed reported success")
	}
	failUnlink = false
	if err := vault.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", decided)); err != nil {
		t.Fatalf("the retry could not remove the restored entry: %v", err)
	}

	failures, err := vault.RemoveInboxResidue(context.Background(), []string{ContentHash(decided)})
	if err != nil {
		t.Fatalf("RemoveInboxResidue: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("residue sweep failures = %v, want none", failures)
	}
	if entries := vaultDirEntries(t, vault, InboxDirName); len(entries) != 0 {
		t.Fatalf("inbox = %v, want nothing left under any name", entries)
	}
}

// A sweep that cannot finish says which bytes it could not account for, so the
// record naming them is kept rather than retired over a file that is still
// there.
func TestResidueSweepReportsWhatItCouldNotRemove(t *testing.T) {
	const decided = "the proposal this session wrote"
	vault, err := openVaultWithRemovalSeams(
		newTestVaultRoot(t), realSyncHooks(), nil, nil,
		func(name string, unlink func() error) error {
			if strings.HasPrefix(name, stagingPrefix) {
				return errStagingUnlink
			}
			return unlink()
		},
	)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	residue := filepath.Join(vault.Root(), InboxDirName, stagingPrefix+"cccccccccccccccccccccccc")
	if err := os.WriteFile(residue, []byte(decided), 0o600); err != nil {
		t.Fatal(err)
	}

	failures, err := vault.RemoveInboxResidue(context.Background(), []string{ContentHash(decided)})
	if err != nil {
		t.Fatalf("RemoveInboxResidue: %v", err)
	}
	if failures[ContentHash(decided)] == nil {
		t.Fatalf("the sweep reported no failure for bytes it could not remove: %v", failures)
	}
	if _, err := os.Lstat(residue); err != nil {
		t.Fatalf("the bytes the sweep could not remove are gone: %v", err)
	}
}
