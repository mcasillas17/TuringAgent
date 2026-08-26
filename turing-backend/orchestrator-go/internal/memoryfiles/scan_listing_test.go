package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

// listRecorder watches every directory listing the walk performs. It exists to
// hold one rule a compiling change could quietly delete: the walk never asks a
// directory for all of its entries at once.
//
// Readdirnames(-1) pulls a whole directory into memory before a single bound is
// consulted, so a vault with a million files is loaded in full only to be told
// it is too large. The bound has to be enforced while the listing is still in
// progress, which means the listing has to arrive in batches.
type listRecorder struct {
	mutex     sync.Mutex
	requested []int
	batches   int
}

func (r *listRecorder) hook() readDirNamesHook {
	return func(directory *os.File, count int) ([]string, error) {
		r.mutex.Lock()
		r.requested = append(r.requested, count)
		r.batches++
		r.mutex.Unlock()
		return directory.Readdirnames(count)
	}
}

func (r *listRecorder) unbounded() bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for _, count := range r.requested {
		if count <= 0 {
			return true
		}
	}
	return false
}

func openWalkStubVault(t *testing.T, root string, recorder *listRecorder) *Vault {
	t.Helper()
	vault, err := openVaultWithListing(root, realSyncHooks(), nil, recorder.hook())
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return vault
}

func seedBeliefFiles(t *testing.T, root string, count int) []string {
	t.Helper()
	paths := make([]string, 0, count)
	for index := 0; index < count; index++ {
		relPath := fmt.Sprintf("%s/note-%03d.md", BeliefsDirName, index)
		full := filepath.Join(root, filepath.FromSlash(relPath))
		body := fmt.Sprintf("---\nid: %s\nkind: belief\nmanaged: true\n---\n\nnote %d\n", mustNoteID(t), index)
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write %q: %v", relPath, err)
		}
		paths = append(paths, relPath)
	}
	sort.Strings(paths)
	return paths
}

func mustNoteID(t *testing.T) string {
	t.Helper()
	id, err := NewNoteID()
	if err != nil {
		t.Fatalf("mint note id: %v", err)
	}
	return id
}

func TestWalkVaultNeverAsksForAWholeDirectoryAtOnce(t *testing.T) {
	root := newTestVaultRoot(t)
	seedBeliefFiles(t, root, vaultListingBatchSize+7)
	recorder := &listRecorder{}
	vault := openWalkStubVault(t, root, recorder)

	if _, err := vault.Scan(context.Background()); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if recorder.unbounded() {
		t.Fatalf("the walk asked for an unbounded listing (%v); every request must be a positive batch", recorder.requested)
	}
	if recorder.batches < 2 {
		t.Fatalf("a directory holding %d entries was listed in %d call(s); the listing is not batched", vaultListingBatchSize+7, recorder.batches)
	}
}

// Batching must not cost the walk its answer: the same notes, in the same
// order, on every pass.
func TestWalkVaultInBatchesStillReportsEveryNoteInOrder(t *testing.T) {
	root := newTestVaultRoot(t)
	want := seedBeliefFiles(t, root, vaultListingBatchSize*2+3)
	recorder := &listRecorder{}
	vault := openWalkStubVault(t, root, recorder)

	first, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := make([]string, 0, len(first.Notes))
	for _, note := range first.Notes {
		got = append(got, note.RelPath)
	}
	if len(got) != len(want) {
		t.Fatalf("scan returned %d notes, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("note %d = %q, want %q; the batched walk is not deterministic", index, got[index], want[index])
		}
	}
	if !first.Completeness.Beliefs.Complete {
		t.Fatalf("beliefs/ was reported incomplete after a batched listing: %q", first.Completeness.Beliefs.Reason)
	}
}

// The point of batching is that the bound is reached without reading the rest.
// A vault past the ceiling is refused after a bounded number of entries, not
// after the whole directory has been pulled into memory.
func TestWalkVaultStopsAtTheBoundWithoutListingTheWholeVault(t *testing.T) {
	root := newTestVaultRoot(t)
	seedBeliefFiles(t, root, MaxVaultIndexedFiles+vaultListingBatchSize+5)
	recorder := &listRecorder{}
	vault := openWalkStubVault(t, root, recorder)

	_, err := vault.Scan(context.Background())
	if !errors.Is(err, ErrVaultTooLarge) {
		t.Fatalf("scan of an over-large vault = %v, want ErrVaultTooLarge", err)
	}
	if recorder.unbounded() {
		t.Fatalf("the over-large vault was listed unbounded: %v", recorder.requested)
	}
}
