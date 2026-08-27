package memoryfiles

import (
	"context"
	"errors"
	"fmt"
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

// A reserved entry the sweep cannot read is not evidence that the bytes are
// gone. Reading it is how this decides whether it may remove it, so a failure
// to read one means the sweep cannot say the vault holds no second copy — and
// the record naming those bytes has to be kept rather than retired over a file
// nobody could look at.
func TestResidueSweepRefusesToConcludeOverAnEntryItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file regardless of mode")
	}
	vault := newTestVault(t)
	const mine = "the note this session wrote"
	sealed := filepath.Join(vault.Root(), InboxDirName, stagingPrefix+"dddddddddddddddddddddddd")
	if err := os.WriteFile(sealed, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sealed, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sealed, 0o600) })

	if _, err := vault.RemoveInboxResidue(context.Background(), []string{ContentHash(mine)}); err == nil {
		t.Fatal("the sweep reported a clear vault over an entry it could not read")
	}
	if _, err := os.Lstat(sealed); err != nil {
		t.Fatalf("the unreadable entry was disturbed: %v", err)
	}
}

// The bound is on what the sweep reads, not on what it happens to match. An
// inbox somebody has filled is read up to the bound and then refused, so a
// withdrawal keeps its rows instead of walking an unbounded directory.
func TestResidueSweepRefusesAnInboxPastItsBound(t *testing.T) {
	vault := newTestVault(t)
	inbox := filepath.Join(vault.Root(), InboxDirName)
	for index := 0; index <= maxInboxResidueEntries; index++ {
		name := filepath.Join(inbox, fmt.Sprintf("note-%05d.md", index))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := vault.RemoveInboxResidue(context.Background(), []string{ContentHash("anything")}); err == nil {
		t.Fatal("the sweep read an unbounded directory rather than refusing")
	}
}

// A vault whose root is not there is not a vault that is empty. Reporting a
// missing mount as "the file is gone" would retire every record naming a note
// that is sitting on a disk nobody has attached.
func TestRemovalRefusesWhenTheVaultRootIsNotThere(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/note.md", "a proposal")
	if err := os.RemoveAll(vault.Root()); err != nil {
		t.Fatal(err)
	}

	if err := vault.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", "a proposal")); err == nil {
		t.Fatal("a vault that is not mounted was reported as a file that is gone")
	}
	absent, err := vault.ConfirmInboxNoteAbsent(context.Background(), "inbox/note.md")
	if err == nil {
		t.Fatal("a vault that is not mounted answered an absence")
	}
	if absent {
		t.Fatal("a missing vault root was reported as a proven absence")
	}
}

// A failure that leaves the bytes reachable under a name only this package can
// spell has to say so in a way a caller can match on. The record that names the
// file is what a later sweep works from, and a caller that cannot tell "the
// removal did nothing and left a copy" from "the removal was refused and
// nothing moved" is a caller that retires that record.
func TestAFailureThatLeavesBytesUnderAReservedNameSaysSo(t *testing.T) {
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
	writeVaultFile(t, vault, "inbox/note.md", decided)

	removeErr := vault.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", decided))
	if removeErr == nil {
		t.Fatal("a removal whose unlink failed reported success")
	}
	if !errors.Is(removeErr, ErrVaultResidue) {
		t.Fatalf("the failure does not say a copy was left behind: %v", removeErr)
	}

	// And the refusal beside it: the file is rewritten in place after the check
	// and something else takes its name while it is detached, so it cannot go
	// back and is kept under a name of this package's own making.
	var contested *Vault
	contested, err = openVaultWithRemovalSeams(
		newTestVaultRoot(t), realSyncHooks(),
		func(phase detachPhase, _ string) {
			switch phase {
			case detachPhaseBeforeDetach:
				rewriteInPlace(t, contested, "inbox/note.md", "the words the user typed after they decided")
			case detachPhaseBeforeRestore:
				writeVaultFile(t, contested, "inbox/note.md", "a file the user saved under that name")
			}
		},
		nil, nil,
	)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	writeVaultFile(t, contested, "inbox/note.md", decided)
	refusal := contested.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", decided))
	if refusal == nil {
		t.Fatal("a removal of bytes it could not prove were its own reported success")
	}
	if !errors.Is(refusal, ErrVaultResidue) {
		t.Fatalf("the refusal does not say where the bytes were kept: %v", refusal)
	}
}

// A request that ends while the bytes are off their name leaves them under a
// name only this package can spell, exactly as a failed unlink does. The caller
// holding the record has the same problem and needs the same answer, so the
// cancellation carries the marker too.
func TestACancellationThatLeftBytesStagedSaysSo(t *testing.T) {
	const decided = "the proposal this session wrote"
	ctx, cancel := context.WithCancel(context.Background())
	var vault *Vault
	vault, err := openVaultWithRemovalSeams(
		newTestVaultRoot(t), realSyncHooks(),
		func(phase detachPhase, _ string) {
			switch phase {
			case detachPhaseBeforeVerify:
				cancel()
			case detachPhaseBeforeRestore:
				// The name is taken while the request is ending, so the bytes
				// cannot go back and are kept under the reserved one.
				writeVaultFile(t, vault, "inbox/note.md", "a file the user saved under that name")
			}
		},
		nil, nil,
	)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	writeVaultFile(t, vault, "inbox/note.md", decided)

	removeErr := vault.RemoveInboxNote(ctx, retiredRemoval("inbox/note.md", decided))
	if !errors.Is(removeErr, context.Canceled) {
		t.Fatalf("removal error = %v, want the cancellation", removeErr)
	}
	if !errors.Is(removeErr, ErrVaultResidue) {
		t.Fatalf("the cancellation does not say a copy was left behind: %v", removeErr)
	}
}

// A reserved name holding something that is not a file cannot hold a note's
// bytes, so it is stepped over rather than treated as an entry the sweep could
// not read. Treating it as unreadable would wedge every withdrawal on the
// install for good: the sweep would abort, every row it was asked about would
// be kept, and the next pass would abort in exactly the same place.
func TestResidueSweepStepsOverAReservedNameThatIsNotAFile(t *testing.T) {
	vault := newTestVault(t)
	const mine = "the note this session wrote"
	inbox := filepath.Join(vault.Root(), InboxDirName)
	if err := os.Mkdir(filepath.Join(inbox, stagingPrefix+"eeeeeeeeeeeeeeeeeeeeeeee"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../beliefs", filepath.Join(inbox, stagingPrefix+"ffffffffffffffffffffffff")); err != nil {
		t.Fatal(err)
	}
	residue := filepath.Join(inbox, stagingPrefix+"111111111111111111111111")
	if err := os.WriteFile(residue, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}

	failures, err := vault.RemoveInboxResidue(context.Background(), []string{ContentHash(mine)})
	if err != nil {
		t.Fatalf("RemoveInboxResidue: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("residue sweep failures = %v, want none", failures)
	}
	if _, err := os.Lstat(residue); !os.IsNotExist(err) {
		t.Fatalf("the real residue was not removed: %v", err)
	}
}

// The unlink happened and the flush that would make it survive a crash did
// not. The detach that put the bytes under the reserved name was not flushed
// either, so a crash there can bring that name back with the original path
// still empty — which is a copy under a name no listing shows, and the record
// that could find it is about to be retired for exactly that absence.
func TestAFailedFlushAfterTheUnlinkSaysACopyMayComeBack(t *testing.T) {
	const decided = "the proposal this session wrote"
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	writeVaultFile(t, vault, "inbox/note.md", decided)

	recorder.setFailDirectorySyncNumber(1)
	err := vault.RemoveInboxNote(context.Background(), retiredRemoval("inbox/note.md", decided))
	if err == nil {
		t.Fatal("a removal whose post-unlink flush failed reported success")
	}
	if !errors.Is(err, ErrVaultResidue) {
		t.Fatalf("the failure does not say a copy may come back: %v", err)
	}
}
