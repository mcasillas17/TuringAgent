package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// vaultPassBlockedWindow is how long a pass that must be blocked is watched
// before the test concludes it really is.
//
// It only ever has to be longer than the distance between "this goroutine is
// about to call the pass" — which the test waits for explicitly — and the seam
// inside it, which is one short database read away. Unserialised, that is
// microseconds; the window is three orders of magnitude above it because the
// cost of being generous is half a second per run and the cost of being stingy
// is a green that means nothing.
const vaultPassBlockedWindow = 500 * time.Millisecond

// vaultPassDeadline bounds everything that must actually happen, so a pass that
// never returns fails the test instead of hanging the suite.
const vaultPassDeadline = 30 * time.Second

// A whole-vault pass walks the user's files and rewrites the index from what it
// saw. Two of them inside the vault at once is the bug this lock exists for:
// one pass indexing the bytes another is in the middle of rewriting, then
// retiring rows for notes that are still on disk under a name it never read.
//
// Read-only does not exempt a pass from that. RefreshMemoryIndex writes no byte
// to the vault, but it derives the entire index from one walk, so it has to see
// a vault nobody else is editing exactly as much as the writing pass does.
//
// This test parks one pass at the seam inside the pass — after its database
// transaction has closed, before its walk begins — and then proves two things
// that no timing accident can produce: a second refresh and a reconcile,
// already running and one call away from the vault, cannot get in; and while
// that pass sits there the database is free, so no transaction is being held
// across a filesystem crawl of a vault the user may be editing.
//
// Remove the lock and the failure is not a flake: the two contenders walk
// straight through the seam while the first pass is provably still inside it.
func TestVaultPassesNeverOverlapEvenWhenTheyOnlyRead(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/"+noteID+".md",
		managedBelief(noteID, []string{sessionID}, "The user keeps bees."))
	writeVaultNote(t, vault, "beliefs/by-hand.md", "# By hand\n\nThe user keeps chickens.\n")

	var inside atomic.Int32
	var peak atomic.Int32
	arrived := make(chan struct{}, 3)
	release := make(chan struct{})
	repo.memoryVaultPassBarrier = func() {
		now := inside.Add(1)
		for {
			was := peak.Load()
			if now <= was || peak.CompareAndSwap(was, now) {
				break
			}
		}
		arrived <- struct{}{}
		<-release
		inside.Add(-1)
	}

	var running sync.WaitGroup
	errs := make(chan error, 3)
	pass := func(ready chan<- struct{}, run func() error) {
		running.Add(1)
		go func() {
			defer running.Done()
			if ready != nil {
				ready <- struct{}{}
			}
			if err := run(); err != nil {
				errs <- err
			}
		}()
	}
	refresh := func() error { _, err := repo.RefreshMemoryIndex(ctx()); return err }
	reconcile := func() error { _, err := repo.ReconcileMemoryVault(ctx()); return err }

	// The first pass takes the vault and stops in the middle of it.
	pass(nil, refresh)
	select {
	case <-arrived:
	case <-time.After(vaultPassDeadline):
		close(release)
		t.Fatal("the first pass never reached the vault")
	}

	// Nothing is holding the database while a pass is walking the vault. The
	// pool is one connection wide, so a transaction left open across the walk
	// would make this write wait for the walk instead of for the disk.
	dbCtx, cancelDB := context.WithTimeout(ctx(), vaultPassBlockedWindow)
	defer cancelDB()
	if _, err := repo.CreateSession(dbCtx, "written while a vault pass is parked"); err != nil {
		close(release)
		t.Fatalf("the database was not usable while a vault pass was mid-walk: %v", err)
	}

	// Two more passes, one read-only and one that writes, both up against the
	// vault while the first one is still inside it.
	contenders := make(chan struct{}, 2)
	pass(contenders, refresh)
	pass(contenders, reconcile)
	for range 2 {
		select {
		case <-contenders:
		case <-time.After(vaultPassDeadline):
			close(release)
			t.Fatal("a contending pass never started")
		}
	}

	select {
	case <-arrived:
		close(release)
		t.Fatal("a second whole-vault pass entered the vault while another was still inside it")
	case <-time.After(vaultPassBlockedWindow):
	}

	close(release)
	finished := make(chan struct{})
	go func() {
		running.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(vaultPassDeadline):
		t.Fatal("a vault pass never finished; the passes deadlocked")
	}
	close(errs)
	for err := range errs {
		t.Fatalf("vault pass: %v", err)
	}

	if got := peak.Load(); got != 1 {
		t.Fatalf("%d passes were inside the vault at once, want never more than 1", got)
	}
	// The serialisation is not achieved by refusing to work: all three passes
	// ran, and the vault the last one left is the one every pass agreed on.
	adopted, err := repo.SearchMemoryNotes(ctx(), "chickens", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(adopted) != 1 {
		t.Fatalf("the hand-written note was indexed %d times, want exactly once", len(adopted))
	}
}
