package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// A rejection that detaches a file it may not delete has to put it back. When
// it cannot — the name is already held by somebody else's file, or the link
// itself failed — the bytes it is holding are the only copy of a claim about
// the user, and until now they stayed under the private staging name.
//
// That name begins with a dot. The vault walk skips dot entries by design, so
// the file was on disk and off every page: not in a scan, not in
// ListMemoryState, not in the inbox the user is looking at. "Recoverable"
// meant recoverable by someone who knew to go and look in a folder Turing
// tells nobody about. These tests hold the file to being visible.

// recoveryDrafts lists the visible names a rejection left behind for recovery.
func recoveryDrafts(t *testing.T, vault *Vault) []string {
	t.Helper()
	var drafts []string
	for _, name := range inboxEntries(t, vault) {
		if IsRecoveryDraftName(name) {
			drafts = append(drafts, name)
		}
	}
	return drafts
}

// requireOneRecoveryDraft asserts exactly one visible recovery draft exists and
// answers with its name and bytes.
func requireOneRecoveryDraft(t *testing.T, vault *Vault) (string, string) {
	t.Helper()
	drafts := recoveryDrafts(t, vault)
	if len(drafts) != 1 {
		t.Fatalf("expected one visible recovery draft, found %v in %v", drafts, inboxEntries(t, vault))
	}
	if strings.HasPrefix(drafts[0], ".") {
		t.Fatalf("the recovery draft %q is hidden from the vault walk", drafts[0])
	}
	content, err := os.ReadFile(filepath.Join(vault.Root(), InboxDirName, drafts[0]))
	if err != nil {
		t.Fatalf("read the recovery draft: %v", err)
	}
	return drafts[0], string(content)
}

// requireBoundedRefusal holds a refusal to the two properties that make it safe
// to log and safe to show: it is bounded, and it says nothing about what was in
// the file.
func requireBoundedRefusal(t *testing.T, err error, secret string) *StaleContentError {
	t.Helper()
	var stale *StaleContentError
	if !errors.As(err, &stale) {
		t.Fatalf("expected a stale-content refusal, got %v", err)
	}
	if len(stale.Detail) > maxRefusalDetailBytes+len("…") {
		t.Fatalf("the refusal detail is %d bytes, past the %d-byte bound: %q",
			len(stale.Detail), maxRefusalDetailBytes, stale.Detail)
	}
	if !utf8.ValidString(stale.Detail) {
		t.Fatalf("the refusal detail is not valid UTF-8: %q", stale.Detail)
	}
	if secret != "" && strings.Contains(err.Error(), secret) {
		t.Fatalf("the refusal leaked what was in the file: %v", err)
	}
	return stale
}

// vaultWithDetachSeams opens a vault whose rejection detach can be interrupted
// at each of its moments and whose no-clobber links can be failed by target.
func vaultWithDetachSeams(t *testing.T, barrier detachHook, link linkHook) *Vault {
	t.Helper()
	vault, err := openVaultWithDetachSeams(newTestVaultRoot(t), realSyncHooks(), barrier, link)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return vault
}

// contestTheName is the third writer taking the candidate's name while the
// detached file is off it. It is what makes the no-clobber restore impossible.
func contestTheName(t *testing.T, vault *Vault, contender string) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(vault.Root(), InboxDirName, "note.md"),
		[]byte(contender),
		0o600,
	); err != nil {
		t.Fatalf("contest the name: %v", err)
	}
}

// The contested case, which used to end with the only copy of an unread claim
// about the user under a dot name the vault walk skips. It ends under a visible
// one now, and the refusal says so.
func TestRejectionMovesADetachedFileItCannotPutBackIntoAVisibleRecoveryDraft(t *testing.T) {
	const decided = "the proposal the user read"
	const replacement = "a newer claim nobody has read yet"
	const contender = "a third file, under the same name"
	var vault *Vault
	vault = vaultWithDetachSeams(t, func(phase detachPhase, _ string) {
		switch phase {
		case detachPhaseBeforeDetach:
			replaceInboxEntry(t, vault, "inbox/note.md", replacement)
		case detachPhaseBeforeRestore:
			contestTheName(t, vault, contender)
		}
	}, nil)
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a stale-content refusal, got %v", err)
	}
	held, readErr := os.ReadFile(full)
	if readErr != nil || string(held) != contender {
		t.Fatalf("the file that took the name was disturbed: %q, %v", held, readErr)
	}
	requireNoStagingResidue(t, vault)
	draft, kept := requireOneRecoveryDraft(t, vault)
	if kept != replacement {
		t.Fatalf("the recovery draft holds %q, want the detached replacement %q", kept, replacement)
	}
	stale := requireBoundedRefusal(t, err, replacement)
	if !strings.Contains(stale.Detail, draft) {
		t.Fatalf("the refusal does not name where the file was kept: %q", stale.Detail)
	}
	if !strings.Contains(stale.Detail, "recovery") {
		t.Fatalf("the refusal does not say the file is recoverable: %q", stale.Detail)
	}
}

// The point of being visible: the next pass over the vault sees it. A file the
// walk skips is a file the user is never told about, which is the same outcome
// as deleting it for everyone except a forensic reader.
func TestARecoveryDraftIsOnTheNextScanRatherThanSkipped(t *testing.T) {
	const decided = "the proposal the user read"
	const replacement = "a newer claim nobody has read yet, and nothing can parse"
	var vault *Vault
	vault = vaultWithDetachSeams(t, func(phase detachPhase, _ string) {
		switch phase {
		case detachPhaseBeforeDetach:
			replaceInboxEntry(t, vault, "inbox/note.md", replacement)
		case detachPhaseBeforeRestore:
			contestTheName(t, vault, "a third file, under the same name")
		}
	}, nil)
	writeVaultFile(t, vault, "inbox/note.md", decided)

	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	}); !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a stale-content refusal, got %v", err)
	}
	draft, _ := requireOneRecoveryDraft(t, vault)
	relPath := InboxDirName + "/" + draft

	scan, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan the vault: %v", err)
	}
	for _, skipped := range scan.Skipped {
		if skipped.RelPath == relPath {
			t.Fatalf("the recovery draft was skipped by the walk: %s", skipped.Reason)
		}
	}
	var listed *NoteRow
	for index, note := range scan.Notes {
		if note.RelPath == relPath {
			listed = &scan.Notes[index]
		}
	}
	if listed == nil {
		t.Fatalf("the recovery draft is not on the scan: %+v", scan.Notes)
	}
	if listed.Area != AreaInbox {
		t.Fatalf("the recovery draft is in area %q, want the inbox", listed.Area)
	}
	if listed.Status == NoteStatusManaged {
		t.Fatalf("the recovery draft is presented as a proposal Turing is managing: %+v", listed)
	}
	if listed.NoteID != "" {
		t.Fatalf("the recovery draft carries identity %q, which no row can answer for", listed.NoteID)
	}
	if listed.Content != replacement {
		t.Fatalf("the scanned recovery draft holds %q, want %q", listed.Content, replacement)
	}
	// The name is Turing's, not a proposal's: it must not be read back as an
	// identity any row could be correlated to.
	if identity := NoteIDFromInboxRelPath(relPath); identity != "" {
		t.Fatalf("the recovery name reads back as the minted identity %q", identity)
	}
}

// A restore that fails for a reason other than the name being taken is the same
// problem wearing a different error: the bytes are detached, they are not the
// user's decided file, and nothing may delete them. The seam is how that branch
// is reachable at all — no test can make a link fail with EIO by arranging the
// filesystem.
func TestRejectionRecoversADetachedFileWhenTheRestoreLinkFailsOutright(t *testing.T) {
	const decided = "the proposal the user read"
	const replacement = "a newer claim nobody has read yet"
	var vault *Vault
	vault = vaultWithDetachSeams(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeDetach {
			replaceInboxEntry(t, vault, "inbox/note.md", replacement)
		}
	}, func(target string, link func() error) error {
		if target == "note.md" {
			return unix.EIO
		}
		return link()
	})
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected the rejection to refuse, got %v", err)
	}
	if !errors.Is(err, unix.EIO) {
		t.Fatalf("the failure that stopped the restore was not surfaced: %v", err)
	}
	if _, statErr := os.Lstat(full); !os.IsNotExist(statErr) {
		t.Fatalf("the candidate name holds something unexpected: %v", statErr)
	}
	requireNoStagingResidue(t, vault)
	draft, kept := requireOneRecoveryDraft(t, vault)
	if kept != replacement {
		t.Fatalf("the recovery draft holds %q, want the detached replacement %q", kept, replacement)
	}
	stale := requireBoundedRefusal(t, err, replacement)
	if !strings.Contains(stale.Detail, draft) {
		t.Fatalf("the refusal does not name where the file was kept: %q", stale.Detail)
	}
}

// When nothing can be linked at all the file stays under the staging name,
// because the one thing this must never do is unlink it. The refusal has to say
// where it is, and it still may not say what is in it.
func TestRejectionKeepsTheStagedFileWhenNothingCanBeLinkedAndSaysWhere(t *testing.T) {
	const decided = "the proposal the user read"
	const replacement = "a newer claim nobody has read yet"
	var vault *Vault
	vault = vaultWithDetachSeams(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeDetach {
			replaceInboxEntry(t, vault, "inbox/note.md", replacement)
		}
	}, func(_ string, _ func() error) error {
		return unix.EIO
	})
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected the rejection to refuse, got %v", err)
	}
	if !errors.Is(err, unix.EIO) {
		t.Fatalf("the failure that stopped every link was not surfaced: %v", err)
	}
	if _, statErr := os.Lstat(full); !os.IsNotExist(statErr) {
		t.Fatalf("the candidate name holds something unexpected: %v", statErr)
	}
	if drafts := recoveryDrafts(t, vault); len(drafts) != 0 {
		t.Fatalf("a recovery draft was reported that no link could have made: %v", drafts)
	}
	staged := ""
	for _, name := range inboxEntries(t, vault) {
		if strings.HasPrefix(name, stagingPrefix) {
			staged = name
		}
	}
	if staged == "" {
		t.Fatal("the detached file was deleted rather than kept where it could be found")
	}
	kept, readErr := os.ReadFile(filepath.Join(vault.Root(), InboxDirName, staged))
	if readErr != nil || string(kept) != replacement {
		t.Fatalf("the staged file holds %q, want the replacement %q (%v)", kept, replacement, readErr)
	}
	stale := requireBoundedRefusal(t, err, replacement)
	if !strings.Contains(stale.Detail, staged) {
		t.Fatalf("the refusal does not say where the file was left: %q", stale.Detail)
	}
}

// The refusal is written into logs and handed to a caller. A filesystem error
// text is not a bound, so the sentence is clipped to one — on a rune boundary,
// so what is logged is still text.
func TestRefusalDetailIsClippedToItsBoundOnARuneBoundary(t *testing.T) {
	if boundRefusalDetail("short enough") != "short enough" {
		t.Fatal("a detail inside the bound must be left exactly as written")
	}
	long := strings.Repeat("é", maxRefusalDetailBytes)
	bounded := boundRefusalDetail(long)
	if len(bounded) > maxRefusalDetailBytes+len("…") {
		t.Fatalf("bounded detail is %d bytes, past the %d-byte bound", len(bounded), maxRefusalDetailBytes)
	}
	if !utf8.ValidString(bounded) {
		t.Fatalf("bounded detail is not valid UTF-8: %q", bounded)
	}
	if !strings.HasSuffix(bounded, "…") {
		t.Fatalf("a clipped detail must say it was clipped: %q", bounded)
	}
}

// The same bound, reached the way production would reach it: through an error
// whose own text is unbounded.
func TestARefusalCarryingAnUnboundedFailureStaysBounded(t *testing.T) {
	const decided = "the proposal the user read"
	const replacement = "a newer claim nobody has read yet"
	verbose := errors.New(strings.Repeat("filesystem said no; ", 200))
	var vault *Vault
	vault = vaultWithDetachSeams(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeDetach {
			replaceInboxEntry(t, vault, "inbox/note.md", replacement)
		}
	}, func(_ string, _ func() error) error {
		return verbose
	})
	writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	requireBoundedRefusal(t, err, replacement)
	if !errors.Is(err, verbose) {
		t.Fatalf("the failure behind the refusal was not surfaced: %v", err)
	}
}

// The staging names stay reserved on both sides: the walk keeps stepping over
// them, and no caller can name one — including the cleaner, whose whole mode is
// a hashless unlink and which must never become a way to reach one.
func TestReservedStagingNamesStaySkippedAndUnnameable(t *testing.T) {
	vault := newTestVault(t)
	reserved := stagingPrefix + "0123456789abcdef"
	writeVaultFile(t, vault, InboxDirName+"/"+reserved, "half a write nobody committed")

	scan, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan the vault: %v", err)
	}
	relPath := InboxDirName + "/" + reserved
	for _, note := range scan.Notes {
		if note.RelPath == relPath {
			t.Fatalf("a staging name was indexed as a note: %+v", note)
		}
	}
	skipped := false
	for _, entry := range scan.Skipped {
		if entry.RelPath == relPath {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("the staging name was neither indexed nor reported as skipped: %+v", scan.Skipped)
	}
	for _, mode := range []InboxRemovalMode{
		RemoveDecidedCandidate, RemoveUnreadableCandidate, RemoveRetiredCandidate,
	} {
		err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
			RelPath:             relPath,
			Mode:                mode,
			ExpectedContentHash: ContentHash("half a write nobody committed"),
		})
		if !errors.Is(err, ErrConfinement) {
			t.Fatalf("mode %q was allowed to name a staging entry: %v", mode, err)
		}
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), InboxDirName, reserved)); statErr != nil {
		t.Fatalf("the staging entry was removed by a refused request: %v", statErr)
	}
}

// Turing's own tidying goes through the same two-step detach every other
// deletion here does, and for the same reason: it is a deletion in the user's
// vault, and an unlink names a name. What it must not do is leave anything
// behind on the ordinary path — a tidying that deletes exactly the bytes it was
// recorded against ends with an inbox holding only the user's own files.
func TestRetiredCleanupDetachesAndLeavesNothingBehind(t *testing.T) {
	detached := false
	vault := vaultWithDetachSeams(t, func(detachPhase, string) {
		detached = true
	}, func(_ string, link func() error) error {
		return link()
	})

	const accounted = "already accounted for"
	full := writeVaultFile(t, vault, "inbox/note.md", accounted)
	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveRetiredCandidate,
		ExpectedContentHash: ContentHash(accounted),
	}); err != nil {
		t.Fatalf("retired cleanup: %v", err)
	}
	if !detached {
		t.Fatal("the cleaner unlinked a name instead of verifying a detached entry")
	}
	if _, err := os.Lstat(full); !os.IsNotExist(err) {
		t.Fatalf("expected the retired file to be gone, got %v", err)
	}
	requireNoStagingResidue(t, vault)
	if drafts := recoveryDrafts(t, vault); len(drafts) != 0 {
		t.Fatalf("the cleaner left recovery drafts behind: %v", drafts)
	}
}

// Every ordinary path still leaves nothing: a rejection that deletes, and one
// that puts the file back, both end with an inbox holding only the user's own
// files.
func TestSuccessfulRejectionPathsLeaveNoStagingAndNoRecoveryDraft(t *testing.T) {
	const decided = "the proposal the user read"
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/note.md", decided)
	if err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	}); err != nil {
		t.Fatalf("remove the decided proposal: %v", err)
	}
	requireNoStagingResidue(t, vault)
	if drafts := recoveryDrafts(t, vault); len(drafts) != 0 {
		t.Fatalf("a clean deletion left recovery drafts behind: %v", drafts)
	}

	var restoring *Vault
	restoring = vaultWithDetachSeams(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeDetach {
			replaceInboxEntry(t, restoring, "inbox/note.md", "a newer claim")
		}
	}, nil)
	full := writeVaultFile(t, restoring, "inbox/note.md", decided)
	if err := restoring.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	}); !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a stale-content refusal, got %v", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil || string(survived) != "a newer claim" {
		t.Fatalf("the replacement was not put back under its own name: %q, %v", survived, readErr)
	}
	requireNoStagingResidue(t, restoring)
	if drafts := recoveryDrafts(t, restoring); len(drafts) != 0 {
		t.Fatalf("a restore that succeeded left recovery drafts behind: %v", drafts)
	}
}

// Two rescues in one inbox must not collide, and the second must not overwrite
// the first: each detached file is the only copy of what it says.
func TestRecoveryDraftNamesDoNotCollide(t *testing.T) {
	seen := map[string]struct{}{}
	for attempt := 0; attempt < 64; attempt++ {
		name, err := RecoveryDraftFileName()
		if err != nil {
			t.Fatalf("mint a recovery name: %v", err)
		}
		if !IsRecoveryDraftName(name) {
			t.Fatalf("minted %q, which is not recognised as a recovery draft", name)
		}
		if strings.HasPrefix(name, ".") || strings.Contains(name, "/") {
			t.Fatalf("minted %q, which is hidden or path-shaped", name)
		}
		if !strings.HasSuffix(name, noteFileExtension) {
			t.Fatalf("minted %q, which the walk would not index", name)
		}
		if _, repeated := seen[name]; repeated {
			t.Fatalf("minted %q twice", name)
		}
		seen[name] = struct{}{}
	}
}
