package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A promotion is a copy and then a removal. When the removal will not happen —
// the entry is verified, and the filesystem refuses the unlink — the original
// goes back under its own name, so nothing has moved.
//
// What that must not be reported as is the user's file changing. "It changed
// while it was being promoted" sends them to re-read a proposal nobody edited,
// and a caller matching on staleness retries a race that is not one. The vault
// is left exactly as it was, and the sentence says what actually stopped.
func TestPromotionRefusedByAnUnremovableSourceIsNotReportedAsStaleness(t *testing.T) {
	failures := 0
	vault, err := openVaultWithRemovalSeams(
		newTestVaultRoot(t), realSyncHooks(), nil, nil,
		func(name string, unlink func() error) error {
			// Only the first drop fails: that is the promoted original's own
			// removal. The rollback of the copy that was written has to be
			// able to finish, or the test would be about two failures.
			if strings.HasPrefix(name, stagingPrefix) && failures == 0 {
				failures++
				return errStagingUnlink
			}
			return unlink()
		},
	)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	candidate := seedBelief(t, vault)
	before := readVaultEntry(t, vault, candidate.RelPath)

	promoteErr := promoteCandidate(context.Background(), vault, candidate)
	if promoteErr == nil {
		t.Fatal("a promotion whose original would not go away reported success")
	}
	if errors.Is(promoteErr, ErrSourceChanged) {
		t.Fatalf("a removal that failed was reported as the user's file changing: %v", promoteErr)
	}
	if !errors.Is(promoteErr, errStagingUnlink) {
		t.Fatalf("the failure does not carry what the filesystem said: %v", promoteErr)
	}

	// The proposal is still in the inbox, word for word, so the user can
	// promote it again once whatever refused the unlink is fixed.
	if got := readVaultEntry(t, vault, candidate.RelPath); got != before {
		t.Fatalf("the proposal = %q, want the bytes it was promoted from", got)
	}
	// And nothing was published under beliefs/: a belief beside a proposal
	// still sitting in the inbox is one claim the user would be asked about
	// twice, and one they would be told Turing already holds.
	for _, name := range vaultDirEntries(t, vault, BeliefsDirName) {
		if !strings.HasPrefix(name, stagingPrefix) {
			t.Fatalf("the abandoned promotion left %q under beliefs/", name)
		}
	}
}

// readVaultEntry reads one vault file whole, for tests that care that the bytes
// are the same bytes rather than that a name exists.
func readVaultEntry(t *testing.T, vault *Vault, relPath string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(vault.Root(), filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("read %q: %v", relPath, err)
	}
	return string(content)
}

// The rollback of a promotion that is being abandoned is not a removal of a
// file anything names. Nothing durable records `beliefs/<id>.md` — the
// promotion never committed — so putting that entry back under its own name
// publishes a belief the user never accepted, in the one directory whose
// contents the app presents as what Turing holds.
//
// So when the unlink refuses there, the bytes stay under the reserved name the
// detach put them under, where the walk steps over them, and the failure says
// where they are.
func TestAbandonedPromotionNeverPublishesTheCopyItCouldNotRemove(t *testing.T) {
	vault, err := openVaultWithRemovalSeams(
		newTestVaultRoot(t), realSyncHooks(), nil, nil,
		func(name string, unlink func() error) error {
			// Both drops refuse: the source's, which abandons the move, and
			// the rollback's, which is the one this test is about.
			if strings.HasPrefix(name, stagingPrefix) {
				return errStagingUnlink
			}
			return unlink()
		},
	)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	candidate := seedBelief(t, vault)
	before := readVaultEntry(t, vault, candidate.RelPath)

	promoteErr := promoteCandidate(context.Background(), vault, candidate)
	if promoteErr == nil {
		t.Fatal("a promotion whose original would not go away reported success")
	}
	// The proposal is still the user's to decide on.
	if got := readVaultEntry(t, vault, candidate.RelPath); got != before {
		t.Fatalf("the proposal = %q, want the bytes it was promoted from", got)
	}
	// And nothing the walk indexes was left under beliefs/.
	for _, name := range vaultDirEntries(t, vault, BeliefsDirName) {
		if !strings.HasPrefix(name, stagingPrefix) {
			t.Fatalf("the abandoned promotion published %q under beliefs/", name)
		}
	}
	staged := stagingResidueIn(t, vault, BeliefsDirName)
	if len(staged) != 1 {
		t.Fatalf("beliefs/ reserved entries = %v, want the copy kept where nothing indexes it", staged)
	}
	if !strings.Contains(promoteErr.Error(), staged[0]) {
		t.Fatalf("the failure does not say where the copy is: %v", promoteErr)
	}
}
