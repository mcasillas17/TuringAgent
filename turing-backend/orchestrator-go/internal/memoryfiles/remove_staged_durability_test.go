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

// The last place a detached file can end up is the reserved staging name it
// was moved to when it left its own name. Two roads reach it. Under beliefs/
// and at the vault root it is the answer by design — publishing a file this
// package could not prove was its own would fabricate a belief — and that road
// flushes the directory before it says where the bytes are. In the inbox it is
// the fallback: the rescue that would have given them a visible name never got
// one, and the bytes stay staged.
//
// That second road said "it is at ..." on the strength of a rename nobody had
// flushed. A rename lives in the directory's page cache, so after a crash the
// name a refusal sent the user to can be a name that never existed, with the
// file back under the one the refusal said it had left. The two roads end in
// the same placement and owe the same fsync, and the caveat when that fsync
// fails is what keeps "it is not lost" from being a guess.
//
// There are three ways the rescue comes back with no name, and all three land
// here: the mint failed, the link failed for a reason that is not the name
// being taken, and sixteen minted names were all taken.

// errRecoveryNameMint is the failure a test injects into the minting of a
// visible recovery name. The mint draws on the system's entropy, so no
// arrangement of files can fail it.
var errRecoveryNameMint = errors.New("simulated failure minting a recovery name")

// vaultWithRescueSeams opens a vault whose detach barrier, links, staging
// unlink and recovery-name mint are all under the test's control.
func vaultWithRescueSeams(
	t *testing.T,
	hooks syncHooks,
	barrier detachHook,
	link linkHook,
	recoveryName recoveryNameHook,
) *Vault {
	t.Helper()
	vault, err := openVaultWithRescueSeams(newTestVaultRoot(t), hooks, barrier, link, nil, recoveryName)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return vault
}

// failEveryMint is the rescue that never gets a name to link to.
func failEveryMint() recoveryNameHook {
	return func(func() (string, error)) (string, error) { return "", errRecoveryNameMint }
}

// failEveryLink is the rescue whose link fails for a reason that is not the
// name being taken, so retrying a different name would not help.
func failEveryLink() linkHook {
	return func(string, func() error) error { return unix.EIO }
}

// everyRecoveryNameTaken is the rescue that mints sixteen names and finds every
// one of them already held. The restore's own link fails outright, which is
// what puts the bytes on the rescue road in the first place.
func everyRecoveryNameTaken() linkHook {
	return func(target string, _ func() error) error {
		if IsRecoveryDraftName(target) {
			return unix.EEXIST
		}
		return unix.EIO
	}
}

// directoryInode is the inode of one vault directory, so a durability test can
// name the directory that had to reach the disk instead of counting fsyncs and
// hoping they were the right ones.
func directoryInode(t *testing.T, vault *Vault, dir string) uint64 {
	t.Helper()
	var stat unix.Stat_t
	if err := unix.Stat(filepath.Join(vault.Root(), dir), &stat); err != nil {
		t.Fatalf("inspect %s/: %v", dir, err)
	}
	return uint64(stat.Ino)
}

// rescueFailure is one of the three ways a rescue comes back with no visible
// name, as the seams that arrange it.
type rescueFailure struct {
	name         string
	link         linkHook
	recoveryName recoveryNameHook
}

func rescueFailures() []rescueFailure {
	return []rescueFailure{
		{name: "the recovery name could not be minted", link: failEveryLink(), recoveryName: failEveryMint()},
		{name: "the recovery link failed outright", link: failEveryLink()},
		{name: "every minted recovery name was taken", link: everyRecoveryNameTaken()},
	}
}

// stageAnInboxRefusal runs one rejection that must refuse, with the restore's
// own link failing so the bytes are on the staged road, and answers with the
// refusal and the inbox directory's inode.
func stageAnInboxRefusal(
	t *testing.T,
	failure rescueFailure,
	recorder *syncRecorder,
	arm func(recorder *syncRecorder),
) (*Vault, uint64, error) {
	t.Helper()
	const decided = "the proposal the user read"
	const replacement = "a newer claim nobody has read yet"
	var vault *Vault
	link := failure.link
	vault = vaultWithRescueSeams(t, recorder.hooks(), func(phase detachPhase, _ string) {
		switch phase {
		case detachPhaseBeforeDetach:
			replaceInboxEntry(t, vault, "inbox/note.md", replacement)
		case detachPhaseBeforeRestore:
			arm(recorder)
		}
	}, link, failure.recoveryName)
	writeVaultFile(t, vault, "inbox/note.md", decided)
	inbox := directoryInode(t, vault, InboxDirName)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("rejection = %v, want a stale-content refusal", err)
	}
	return vault, inbox, err
}

// requireStagedBytes holds the reserved name to actually holding the detached
// bytes, and answers with the name so a refusal can be checked against it.
func requireStagedBytes(t *testing.T, vault *Vault, want string) string {
	t.Helper()
	staged, held := stagingResidueContent(t, vault, InboxDirName)
	if held != want {
		t.Fatalf("the staging entry holds %q, want the detached bytes", held)
	}
	if drafts := recoveryDrafts(t, vault); len(drafts) != 0 {
		t.Fatalf("a rescue that never got a name still published %v", drafts)
	}
	return staged
}

// The flush the staged road owed and never made. Each of the three rescue
// failures leaves the bytes under the reserved name the detach put them under,
// and the directory holding that name has to reach the disk before a refusal
// tells the user where their file is.
func TestARefusalThatKeepsBytesStagedFlushesTheDirectoryFirst(t *testing.T) {
	const replacement = "a newer claim nobody has read yet"
	for _, failure := range rescueFailures() {
		t.Run(failure.name, func(t *testing.T) {
			recorder := &syncRecorder{}
			vault, inbox, err := stageAnInboxRefusal(t, failure, recorder, func(*syncRecorder) {})

			staged := requireStagedBytes(t, vault, replacement)
			if !recorder.syncedDirectory(inbox) {
				t.Fatal("the bytes were left under a reserved name the directory fsync never established")
			}
			stale := requireBoundedRefusal(t, err, replacement)
			if !strings.Contains(stale.Detail, staged) {
				t.Fatalf("the refusal does not say where the bytes are: %q", stale.Detail)
			}
			// Nothing failed after the placement, so there is no caveat to
			// make: a refusal that hedges a placement it did flush is as
			// unusable as one that promises a placement it did not.
			if strings.Contains(stale.Detail, "survived a crash") {
				t.Fatalf("the refusal hedges a placement that did reach the disk: %q", stale.Detail)
			}
		})
	}
}

// And the same three when that flush fails. The bytes are still under the
// reserved name — nothing here moves them — and what the user is told is that
// the name they are being sent to is not one a crash is guaranteed to keep.
func TestARefusalThatKeepsBytesStagedSaysWhenItCouldNotFlush(t *testing.T) {
	const replacement = "a newer claim nobody has read yet"
	for _, failure := range rescueFailures() {
		t.Run(failure.name, func(t *testing.T) {
			recorder := &syncRecorder{}
			vault, _, err := stageAnInboxRefusal(t, failure, recorder, func(recorder *syncRecorder) {
				// The restore's link is about to fail, so the only directory
				// fsync left in this call is the one that answers for where
				// the bytes were left.
				recorder.setFailDirectorySyncNumber(1)
			})

			staged := requireStagedBytes(t, vault, replacement)
			stale := requireBoundedRefusal(t, err, replacement)
			if !strings.Contains(stale.Detail, staged) {
				t.Fatalf("the refusal does not say where the bytes are: %q", stale.Detail)
			}
			if !strings.Contains(stale.Detail, "survived a crash") {
				t.Fatalf("the refusal claims a placement it never flushed: %q", stale.Detail)
			}
			if !strings.Contains(stale.Detail, "flush") {
				t.Fatalf("the refusal does not say the flush failed: %q", stale.Detail)
			}
			// The operator's half. "The directory would not sync" and "another
			// writer took the name" are the same sentence to a person and
			// different problems to whoever has to fix it.
			if !strings.Contains(err.Error(), "simulated directory fsync failure") {
				t.Fatalf("the refusal does not carry the fsync failure underneath it: %v", err)
			}
		})
	}
}

// The same placement reached from the vault root, where the reserved name is
// the answer by design rather than a fallback. Both roads end in the same
// sentence, so neither may be the one that guesses.
func TestARollbackThatKeepsBytesStagedSaysWhenItCouldNotFlush(t *testing.T) {
	const existing = "a belief the user already had"
	const contender = "a belief another writer put under the name"
	recorder := &syncRecorder{}
	var vault *Vault
	vault = vaultWithRescueSeams(t, recorder.hooks(), func(phase detachPhase, clean string) {
		if !inBeliefs(clean) {
			return
		}
		switch phase {
		case detachPhaseBeforeDetach:
			rewriteInPlace(t, vault, clean, "the words the user typed while the install was in flight")
		case detachPhaseBeforeRestore:
			takeTheName(t, vault, clean, contender)
			recorder.setFailDirectorySyncNumber(1)
		}
	}, nil, failEveryMint())

	parent, err := os.Open(filepath.Join(vault.Root(), BeliefsDirName))
	if err != nil {
		t.Fatalf("open beliefs: %v", err)
	}
	defer func() { _ = parent.Close() }()
	full := filepath.Join(vault.Root(), BeliefsDirName, "installed.md")
	if err := os.WriteFile(full, []byte(existing), 0o600); err != nil {
		t.Fatalf("write the installed copy: %v", err)
	}
	var installed unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), "installed.md", &installed, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		t.Fatalf("inspect the installed copy: %v", err)
	}

	undoErr := vault.undoInstall(parent, "installed.md", "beliefs/installed.md", installed, existing)
	if undoErr == nil {
		t.Fatal("the undo removed an entry it could not prove it installed")
	}
	staged, held := stagingResidueContent(t, vault, BeliefsDirName)
	if held == "" {
		t.Fatal("the reserved name holds nothing")
	}
	if !strings.Contains(undoErr.Error(), staged) {
		t.Fatalf("the undo does not say where it left the bytes: %v", undoErr)
	}
	if !strings.Contains(undoErr.Error(), "survived a crash") {
		t.Fatalf("the undo claims a placement it never flushed: %v", undoErr)
	}
}
