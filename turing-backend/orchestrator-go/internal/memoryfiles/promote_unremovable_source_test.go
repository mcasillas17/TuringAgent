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
