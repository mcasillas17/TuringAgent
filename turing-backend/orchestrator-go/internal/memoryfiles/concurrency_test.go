package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// The per-path lock is the only thing standing between two accepted profile
// edits and a lost one. A compare-and-set that reads, compares and writes
// without holding the path means two callers can both read the same bytes,
// both find their expected hash intact, and both write — the second silently
// erasing the first, with no stale refusal anywhere to tell the user.
//
// This test fails if the lock is stubbed out: with no mutual exclusion the
// second writer's compare-and-set still passes, and more than one apply
// reports success.
func TestApplyProfileEditSerialisesConcurrentWritersOnTheSamePath(t *testing.T) {
	const writers = 8

	vault := newTestVault(t)
	original := "# Profile\n\nOriginal text.\n"
	profilePath := writeVaultFile(t, vault, ProfileFileName, original)
	expected := ContentHash(original)

	// Every writer gets its own candidate, so the only path they contend on is
	// profile.md itself. Sharing one candidate would serialise them on the
	// candidate's lock and hide a target lock that does nothing.
	candidates := make([]InboxNote, 0, writers)
	for writer := 0; writer < writers; writer++ {
		candidates = append(candidates, seedCandidate(t, vault, KindProfileEdit, fmt.Sprintf("Edit %d", writer), fmt.Sprintf("Proposal %d.", writer)))
	}

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	results := make(chan error, writers)
	contents := make(chan string, writers)
	for writer := 0; writer < writers; writer++ {
		done.Add(1)
		go func(writer int) {
			defer done.Done()
			content := fmt.Sprintf("# Profile\n\nWriter %d won.\n", writer)
			start.Wait()
			_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
				CandidateRelPath:      candidates[writer].RelPath,
				TargetRelPath:         ProfileFileName,
				ExpectedContentHash:   expected,
				ExpectedCandidateHash: candidates[writer].ContentHash,
				Content:               content,
			})
			results <- err
			if err == nil {
				contents <- content
			}
		}(writer)
	}
	start.Done()
	done.Wait()
	close(results)
	close(contents)

	applied := 0
	for err := range results {
		if err == nil {
			applied++
			continue
		}
		if !errors.Is(err, ErrStaleContent) {
			t.Fatalf("a losing writer must be told to re-read, got %v", err)
		}
	}
	if applied != 1 {
		t.Fatalf("%d of %d concurrent writers applied against one expected hash; exactly one may win", applied, writers)
	}
	winner := <-contents
	onDisk, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if string(onDisk) != winner {
		t.Fatalf("the file holds %q, but the only reported success wrote %q", onDisk, winner)
	}
}

// The same property for creates: two callers racing for one name must not both
// believe they made the file, and the loser must be told it already exists
// rather than quietly overwriting the winner's bytes.
func TestCreateInboxNoteIsExclusiveUnderConcurrency(t *testing.T) {
	const writers = 8

	vault := newTestVault(t)
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	results := make(chan error, writers)
	for writer := 0; writer < writers; writer++ {
		done.Add(1)
		go func(writer int) {
			defer done.Done()
			start.Wait()
			_, err := vault.createInboxNoteAt(context.Background(), "inbox/contended.md", fmt.Sprintf("writer %d\n", writer))
			results <- err
		}(writer)
	}
	start.Done()
	done.Wait()
	close(results)

	created := 0
	for err := range results {
		switch {
		case err == nil:
			created++
		case errors.Is(err, ErrAlreadyExists):
		default:
			t.Fatalf("unexpected refusal: %v", err)
		}
	}
	if created != 1 {
		t.Fatalf("%d of %d writers created the same name", created, writers)
	}
	entries, err := os.ReadDir(filepath.Join(vault.Root(), InboxDirName))
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("inbox holds %d entries; staging residue or a second file survived", len(entries))
	}
}
