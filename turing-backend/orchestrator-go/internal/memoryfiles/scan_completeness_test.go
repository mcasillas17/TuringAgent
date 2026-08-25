package memoryfiles

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A walk that could not look is not a walk that found nothing. Every test here
// exists because the caller of a scan deletes rows for notes it did not see,
// and "did not see" must mean "looked and it was gone" rather than "could not
// look at all".

func requireComplete(t *testing.T, area string, scan AreaScan) {
	t.Helper()
	if !scan.Complete {
		t.Fatalf("%s enumeration = incomplete (%q), want complete", area, scan.Reason)
	}
	if scan.Reason != "" {
		t.Fatalf("%s is complete but carries reason %q", area, scan.Reason)
	}
}

func requireIncomplete(t *testing.T, area string, scan AreaScan, mustMention string) {
	t.Helper()
	if scan.Complete {
		t.Fatalf("%s enumeration = complete, want it reported incomplete", area)
	}
	if scan.Reason == "" {
		t.Fatalf("%s was marked incomplete with no reason", area)
	}
	if mustMention != "" && !strings.Contains(scan.Reason, mustMention) {
		t.Fatalf("%s incomplete reason = %q, want it to mention %q", area, scan.Reason, mustMention)
	}
}

// The positive proof the guard rests on: a vault the walk read end to end says
// so, and only then may a caller act on what it did not find.
func TestScanReportsEveryAreaCompleteOnAVaultItReadWhole(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/kept.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nkept\n")
	writeVaultFile(t, vault, "beliefs/nested/deeper.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAW\"\n---\ndeeper\n")
	writeVaultFile(t, vault, "inbox/candidate.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAX\"\nkind: \"belief\"\n---\nc\n")

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	requireComplete(t, "root", result.Completeness.Root)
	requireComplete(t, "beliefs", result.Completeness.Beliefs)
	requireComplete(t, "inbox", result.Completeness.Inbox)
	requireComplete(t, "beliefs by area", result.Completeness.Area(AreaBeliefs))
	requireComplete(t, "inbox by area", result.Completeness.Area(AreaInbox))
}

// An area that was never created is not an area the walk failed to read. The
// vault genuinely holds nothing there, and a caller may act on that.
func TestScanReportsAreasCompleteWhenTheyDoNotExistAtAll(t *testing.T) {
	root := t.TempDir()
	vault := openTestVault(t, root, realSyncHooks())

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	requireComplete(t, "root", result.Completeness.Root)
	requireComplete(t, "beliefs", result.Completeness.Beliefs)
	requireComplete(t, "inbox", result.Completeness.Inbox)
}

// The vault root going away under a live Vault — an unmounted volume, a synced
// folder mid-rebuild — is the loudest version of this bug: every area at once
// looks empty, and a caller reading that as deletion erases the user's memory.
func TestScanMarksEveryAreaIncompleteWhenTheRootIsGoneAfterOpen(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/kept.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nkept\n")
	if err := os.RemoveAll(vault.Root()); err != nil {
		t.Fatalf("remove the vault root: %v", err)
	}

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(result.Notes) != 0 {
		t.Fatalf("notes = %v, want none from a root that is gone", scanPaths(result))
	}
	requireIncomplete(t, "root", result.Completeness.Root, "")
	requireIncomplete(t, "beliefs", result.Completeness.Beliefs, "")
	requireIncomplete(t, "inbox", result.Completeness.Inbox, "")
}

// The root replaced by a file is the same hazard through a different errno.
func TestScanMarksEveryAreaIncompleteWhenTheRootIsNoLongerADirectory(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/kept.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nkept\n")
	if err := os.RemoveAll(vault.Root()); err != nil {
		t.Fatalf("remove the vault root: %v", err)
	}
	if err := os.WriteFile(vault.Root(), []byte("not a vault"), 0o600); err != nil {
		t.Fatalf("replace the vault root with a file: %v", err)
	}

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(result.Notes) != 0 {
		t.Fatalf("notes = %v, want none", scanPaths(result))
	}
	requireIncomplete(t, "root", result.Completeness.Root, "")
	requireIncomplete(t, "beliefs", result.Completeness.Beliefs, "")
	requireIncomplete(t, "inbox", result.Completeness.Inbox, "")
}

// beliefs/ replaced by a regular file: nothing under it can be enumerated, and
// the walk must say so instead of reporting an empty beliefs area. The inbox
// was read whole, so it stays complete — one broken area does not blind the
// caller to the other.
func TestScanMarksOneAreaIncompleteWhenItIsReplacedByAFile(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/kept.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nkept\n")
	writeVaultFile(t, vault, "inbox/candidate.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAX\"\nkind: \"belief\"\n---\nc\n")
	beliefs := filepath.Join(vault.Root(), BeliefsDirName)
	if err := os.RemoveAll(beliefs); err != nil {
		t.Fatalf("remove beliefs: %v", err)
	}
	if err := os.WriteFile(beliefs, []byte("not a folder"), 0o600); err != nil {
		t.Fatalf("replace beliefs with a file: %v", err)
	}

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	requireComplete(t, "root", result.Completeness.Root)
	requireIncomplete(t, "beliefs", result.Completeness.Beliefs, BeliefsDirName)
	requireComplete(t, "inbox", result.Completeness.Inbox)
	if got := strings.Join(scanPaths(result), ","); got != "inbox/candidate.md" {
		t.Fatalf("notes = %q, want the inbox still read whole", got)
	}
	// The refusal stays visible as well: a user whose notes stopped appearing
	// is owed the reason, not just a flag on a struct.
	if !strings.Contains(strings.Join(skippedPaths(result), ","), BeliefsDirName) {
		t.Fatalf("skipped = %v, want beliefs named", skippedPaths(result))
	}
}

// The mirror of the case above, so neither area's guard can be implemented by
// accident in terms of the other.
func TestScanMarksTheInboxIncompleteWhenItIsReplacedByAFile(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/kept.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nkept\n")
	inbox := filepath.Join(vault.Root(), InboxDirName)
	if err := os.RemoveAll(inbox); err != nil {
		t.Fatalf("remove inbox: %v", err)
	}
	if err := os.WriteFile(inbox, []byte("not a folder"), 0o600); err != nil {
		t.Fatalf("replace inbox with a file: %v", err)
	}

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	requireComplete(t, "beliefs", result.Completeness.Beliefs)
	requireIncomplete(t, "inbox", result.Completeness.Inbox, InboxDirName)
	if got := strings.Join(scanPaths(result), ","); got != "beliefs/kept.md" {
		t.Fatalf("notes = %q, want beliefs still read whole", got)
	}
}

// An area replaced by a symlink is not an area either: the walk never follows
// links, so it has no idea what is behind it.
func TestScanMarksAnAreaIncompleteWhenItIsReplacedByASymlink(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/candidate.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAX\"\nkind: \"belief\"\n---\nc\n")
	beliefs := filepath.Join(vault.Root(), BeliefsDirName)
	if err := os.RemoveAll(beliefs); err != nil {
		t.Fatalf("remove beliefs: %v", err)
	}
	if err := os.Symlink(t.TempDir(), beliefs); err != nil {
		t.Fatalf("replace beliefs with a symlink: %v", err)
	}

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	requireIncomplete(t, "beliefs", result.Completeness.Beliefs, BeliefsDirName)
	requireComplete(t, "inbox", result.Completeness.Inbox)
}

// A directory the walk is refused entry to is the plainest "could not look".
func TestScanMarksAnAreaIncompleteWhenItCannotBeOpened(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which is never refused by directory permissions")
	}
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/kept.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nkept\n")
	writeVaultFile(t, vault, "inbox/candidate.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAX\"\nkind: \"belief\"\n---\nc\n")
	beliefs := filepath.Join(vault.Root(), BeliefsDirName)
	if err := os.Chmod(beliefs, 0o000); err != nil {
		t.Fatalf("close beliefs: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(beliefs, 0o700) })

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	requireIncomplete(t, "beliefs", result.Completeness.Beliefs, BeliefsDirName)
	requireComplete(t, "inbox", result.Completeness.Inbox)
	if got := strings.Join(scanPaths(result), ","); got != "inbox/candidate.md" {
		t.Fatalf("notes = %q, want only what was readable", got)
	}
}

// A folder deep inside beliefs/ that cannot be opened leaves the whole area
// unaccounted for: the walk does not know which notes live under it.
func TestScanMarksAnAreaIncompleteWhenASubfolderCannotBeOpened(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which is never refused by directory permissions")
	}
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/kept.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nkept\n")
	writeVaultFile(t, vault, "beliefs/nested/deeper.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAW\"\n---\ndeeper\n")
	nested := filepath.Join(vault.Root(), BeliefsDirName, "nested")
	if err := os.Chmod(nested, 0o000); err != nil {
		t.Fatalf("close the nested folder: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(nested, 0o700) })

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	requireIncomplete(t, "beliefs", result.Completeness.Beliefs, "beliefs/nested")
	requireComplete(t, "inbox", result.Completeness.Inbox)
	if got := strings.Join(scanPaths(result), ","); got != "beliefs/kept.md" {
		t.Fatalf("notes = %q, want the readable note only", got)
	}
}

// A folder outside both areas that cannot be read says nothing about either of
// them. Marking every area incomplete on any failure anywhere would turn the
// guard into a permanent refusal to reconcile.
func TestScanKeepsAreasCompleteWhenAnUnrelatedFolderCannotBeOpened(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which is never refused by directory permissions")
	}
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/kept.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nkept\n")
	writeVaultFile(t, vault, "projects/plan.md", "# plan\n")
	projects := filepath.Join(vault.Root(), "projects")
	if err := os.Chmod(projects, 0o000); err != nil {
		t.Fatalf("close the projects folder: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(projects, 0o700) })

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	requireComplete(t, "beliefs", result.Completeness.Beliefs)
	requireComplete(t, "inbox", result.Completeness.Inbox)
	requireComplete(t, "root", result.Completeness.Root)
}

// The classifier the walk marks through, exercised directly, because a partial
// directory listing is the one failure mode a test cannot conjure from a
// filesystem. Root failures have to reach both areas: a root that could not be
// listed says nothing about what is under it.
func TestAreaCompletenessTrackerAttributesFailuresToTheRightAreas(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		relPath string
		root    bool
		beliefs bool
		inbox   bool
	}{
		{name: "root", relPath: "", root: true, beliefs: true, inbox: true},
		{name: "beliefs root", relPath: BeliefsDirName, beliefs: true},
		{name: "beliefs subfolder", relPath: BeliefsDirName + "/nested", beliefs: true},
		{name: "inbox root", relPath: InboxDirName, inbox: true},
		{name: "inbox subfolder", relPath: InboxDirName + "/nested", inbox: true},
		{name: "unrelated folder", relPath: "projects"},
		{name: "look-alike prefix", relPath: BeliefsDirName + "-archive"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tracker := newCompletenessTracker()
			tracker.markIncomplete(testCase.relPath, "listing failed")
			got := tracker.snapshot()
			if got.Root.Complete == testCase.root {
				t.Fatalf("root complete = %v, want incomplete=%v", got.Root.Complete, testCase.root)
			}
			if got.Beliefs.Complete == testCase.beliefs {
				t.Fatalf("beliefs complete = %v, want incomplete=%v", got.Beliefs.Complete, testCase.beliefs)
			}
			if got.Inbox.Complete == testCase.inbox {
				t.Fatalf("inbox complete = %v, want incomplete=%v", got.Inbox.Complete, testCase.inbox)
			}
		})
	}
}

// A pass that fails outright decides nothing at all, so the result it hands
// back must not read as "everything was enumerated and found empty".
func TestScanResultZeroValueIsIncompleteEverywhere(t *testing.T) {
	empty := ScanResult{}
	for area, scan := range map[string]AreaScan{
		"root":    empty.Completeness.Root,
		"beliefs": empty.Completeness.Beliefs,
		"inbox":   empty.Completeness.Inbox,
	} {
		if scan.Complete {
			t.Fatalf("the zero ScanResult reports %s complete; an empty result must never authorise a deletion", area)
		}
	}
	if empty.Completeness.Area(AreaBeliefs).Complete || empty.Completeness.Area(AreaInbox).Complete {
		t.Fatal("the zero ScanResult reports an area complete through Area()")
	}
}

// A cancelled pass returns no result at all, which is the same refusal by a
// different route.
func TestCancelledScanReturnsNoCompleteness(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/kept.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nkept\n")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := vault.Scan(cancelled)
	if err == nil {
		t.Fatal("a cancelled scan returned no error")
	}
	if result.Completeness.Beliefs.Complete || result.Completeness.Inbox.Complete {
		t.Fatalf("a cancelled scan reported completeness: %+v", result.Completeness)
	}
}
