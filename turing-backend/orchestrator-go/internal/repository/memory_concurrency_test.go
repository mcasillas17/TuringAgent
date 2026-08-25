package repository

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The two passes run on different schedules — one on a timer behind reads, one
// at startup and after a deletion — so they will overlap in production. They
// must not corrupt each other's view or race on the vault.
func TestMemoryPassesAreSafeToRunConcurrently(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	for index := range 4 {
		noteID := newTestNoteID(t)
		writeVaultNote(t, vault,
			"beliefs/"+noteID+".md",
			managedBelief(noteID, []string{sessionID}, "The user keeps bees, note "+string(rune('a'+index))+"."))
	}
	writeVaultNote(t, vault, "beliefs/handwritten.md", "# By hand\n\nThe user keeps chickens.\n")

	var waiting sync.WaitGroup
	errs := make(chan error, 12)
	for range 4 {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
				errs <- err
			}
		}()
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
				errs <- err
			}
		}()
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			if _, err := repo.SearchMemoryNotes(ctx(), "bees", 10); err != nil {
				errs <- err
			}
		}()
	}
	waiting.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent memory pass: %v", err)
	}

	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 4 {
		t.Fatalf("hits = %d, want the four managed beliefs", len(hits))
	}
	adopted, err := repo.SearchMemoryNotes(ctx(), "chickens", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(adopted) != 1 {
		t.Fatalf("the hand-written note was adopted %d times, want exactly once", len(adopted))
	}
}

// Creating candidates while a pass is running must not lose either of them:
// the pass must not sweep a candidate it never saw, and the creation must not
// be blocked into failing.
func TestCandidateCreationDuringReconcileSurvives(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	var waiting sync.WaitGroup
	errs := make(chan error, 8)
	for range 4 {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			if _, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
				SessionID: sessionID,
				Kind:      MemoryCandidateKindBelief,
				Title:     "bees",
				Body:      "The user keeps bees.",
			}); err != nil {
				errs <- err
			}
		}()
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
				errs <- err
			}
		}()
	}
	waiting.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent candidate creation: %v", err)
	}

	candidates, err := repo.ListMemoryCandidates(ctx(), MemoryCandidateQuery{SessionID: sessionID, Limit: 50})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	if len(candidates) != 4 {
		t.Fatalf("candidates = %d, want all four to survive", len(candidates))
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 4 {
		t.Fatalf("reservations = %d, want one per candidate", len(artifacts))
	}
}

// The cleaner hook the session-deletion path will call: files first, then the
// manifest rows for exactly the files that were removed.
func TestPurgeSessionVaultArtifactsRemovesFilesThenRows(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	first := pendingBeliefCandidate(t, repo, sessionID)
	second := pendingBeliefCandidate(t, repo, sessionID)
	// One file is already gone, which is what a retry after a partial failure
	// looks like. Removing it again must still count as success.
	if err := os.Remove(filepath.Join(vault.Root(), filepath.FromSlash(second.InboxPath))); err != nil {
		t.Fatalf("remove one candidate file: %v", err)
	}

	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(first.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("a purged candidate file survived: %v", err)
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("manifest rows after the purge = %+v", artifacts)
	}
}

// When the vault cannot be reached, the rows stay — marked as a failed
// deletion, with one redacted audit row each — so the withdrawal stays
// retryable instead of reporting a completion that left the user's files.
func TestPurgeSessionVaultArtifactsKeepsRowsWhenTheVaultRefuses(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate := pendingBeliefCandidate(t, repo, sessionID)

	inbox := filepath.Join(vault.Root(), "inbox")
	if err := os.RemoveAll(inbox); err != nil {
		t.Fatalf("remove inbox: %v", err)
	}
	if err := os.WriteFile(inbox, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block the inbox: %v", err)
	}

	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err == nil {
		t.Fatalf("PurgeSessionVaultArtifacts succeeded with an unreachable vault")
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].State != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifacts = %+v, want one marked delete_failed", artifacts)
	}
	var payload string
	if err := database.QueryRowContext(ctx(), `
		SELECT payload_json FROM audit_logs WHERE action = ? AND target = ?
	`, "session.vault_artifact.cleanup.failed", artifacts[0].ArtifactID).Scan(&payload); err != nil {
		t.Fatalf("audit row: %v", err)
	}
	if strings.Contains(payload, candidate.InboxPath) || strings.Contains(payload, "bees") {
		t.Fatalf("audit payload leaked vault content: %q", payload)
	}
	if !strings.Contains(payload, VaultArtifactStateDeleteFailed) {
		t.Fatalf("audit payload = %q, want the failed state recorded", payload)
	}
}

func TestPurgeSessionVaultArtifactsRefusesWithoutAVault(t *testing.T) {
	repo := New(openTestDB(t))
	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), "sess_x"); !errors.Is(err, ErrMemoryVaultUnavailable) {
		t.Fatalf("error = %v, want ErrMemoryVaultUnavailable", err)
	}
}
