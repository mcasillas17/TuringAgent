package repository

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

func candidateFileExists(t *testing.T, vault *memoryfiles.Vault, relPath string) bool {
	t.Helper()
	_, err := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(relPath)))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %q: %v", relPath, err)
	return false
}

// Two decisions about one proposal, arriving at once. Exactly one may act.
//
// Without serialisation both read the row, both pass their checks, and then one
// moves the file while the other deletes it: whichever loses the race still
// reports whatever its own half managed to do, and the user is told a claim was
// both accepted and refused.
func TestPromoteAndRejectRacingOverOneCandidateLeaveExactlyOneWinner(t *testing.T) {
	for attempt := range 8 {
		repo, vault, _ := newMemoryTestRepo(t)
		sessionID := newMemoryTestSession(t, repo)
		candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
			SessionID: sessionID, Kind: MemoryCandidateKindBelief,
			Title: "Dark mode", Body: "The user prefers dark mode.",
		})
		if err != nil {
			t.Fatal(err)
		}

		var waiting sync.WaitGroup
		var promoteErr, rejectErr error
		var note MemoryNote
		waiting.Add(2)
		go func() {
			defer waiting.Done()
			note, promoteErr = repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID})
		}()
		go func() {
			defer waiting.Done()
			rejectErr = repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID})
		}()
		waiting.Wait()

		if promoteErr == nil && rejectErr == nil {
			t.Fatalf("attempt %d: both the promotion and the rejection succeeded", attempt)
		}
		if promoteErr != nil && rejectErr != nil {
			t.Fatalf("attempt %d: neither decision took effect (promote=%v reject=%v)", attempt, promoteErr, rejectErr)
		}
		if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
			t.Fatalf("attempt %d: the decided candidate still has a row: %v", attempt, err)
		}
		if candidateFileExists(t, vault, candidate.InboxPath) {
			t.Fatalf("attempt %d: the decided proposal is still in the inbox", attempt)
		}
		hits, err := repo.SearchMemoryNotes(ctx(), "dark", 10)
		if err != nil {
			t.Fatal(err)
		}
		if promoteErr == nil {
			// The promotion won: the belief exists, and nothing may resurrect
			// the rejection afterwards.
			if note.NoteID == "" || len(hits) != 1 {
				t.Fatalf("attempt %d: promotion won but produced %d searchable belief(s)", attempt, len(hits))
			}
		} else if len(hits) != 0 {
			// The rejection won: no belief was written at all.
			t.Fatalf("attempt %d: rejection won and a belief was written anyway: %+v", attempt, hits)
		}

		// Whatever happened, a later pass must not undo it.
		if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
			t.Fatalf("attempt %d: reconcile after the race: %v", attempt, err)
		}
		if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
			t.Fatalf("attempt %d: reconcile resurrected the decided candidate: %v", attempt, err)
		}
	}
}

// The same race over a profile edit, where losing it is worse: an apply that
// loses to a rejection must not have rewritten the user's profile on the way.
func TestApplyAndRejectRacingOverOneProfileEditLeaveExactlyOneWinner(t *testing.T) {
	original := "# Profile\n\nWrites Go.\n"
	result := "# Profile\n\nWrites Go.\n\nGoes by Miguel.\n"
	for attempt := range 8 {
		repo, vault, _ := newMemoryTestRepo(t)
		sessionID := newMemoryTestSession(t, repo)
		writeVaultNote(t, vault, memoryfiles.ProfileFileName, original)
		candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
			SessionID: sessionID, Kind: MemoryCandidateKindProfileEdit,
			Title: "Call me Miguel", Body: "The user goes by Miguel.",
		})
		if err != nil {
			t.Fatal(err)
		}

		var waiting sync.WaitGroup
		var applyErr, rejectErr error
		waiting.Add(2)
		go func() {
			defer waiting.Done()
			_, applyErr = repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
				CandidateID:         candidate.CandidateID,
				ExpectedContentHash: memoryfiles.ContentHash(original),
				Content:             result,
			})
		}()
		go func() {
			defer waiting.Done()
			rejectErr = repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID})
		}()
		waiting.Wait()

		if applyErr == nil && rejectErr == nil {
			t.Fatalf("attempt %d: both the apply and the rejection succeeded", attempt)
		}
		if applyErr != nil && rejectErr != nil {
			t.Fatalf("attempt %d: neither decision took effect (apply=%v reject=%v)", attempt, applyErr, rejectErr)
		}
		profile := vault.EditableProfile(ctx())
		if applyErr == nil {
			if profile.Content != result {
				t.Fatalf("attempt %d: the apply won but the profile says %q", attempt, profile.Content)
			}
		} else if profile.Content != original {
			t.Fatalf("attempt %d: the rejection won and the profile was rewritten anyway: %q", attempt, profile.Content)
		}
		if candidateFileExists(t, vault, candidate.InboxPath) {
			t.Fatalf("attempt %d: the decided proposal is still in the inbox", attempt)
		}
	}
}

// The hash a decision names is compared against the file's own bytes at the
// moment of the decision, inside the same serialisation as the mutation — so a
// proposal edited between the listing and the decision is refused rather than
// acted on as the text the user read.
func TestDecisionsRefuseAStaleCandidateFileHash(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	stale := candidate.ContentHash
	writeVaultNote(t, vault, candidate.InboxPath, readVaultNote(t, vault, candidate.InboxPath)+"\nAnd light mode on Tuesdays.\n")

	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID: candidate.CandidateID, ExpectedCandidateHash: stale,
	}); !errors.Is(err, ErrMemoryCandidateChanged) {
		t.Fatalf("promote with a stale file hash = %v, want ErrMemoryCandidateChanged", err)
	}
	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID: candidate.CandidateID, ExpectedCandidateHash: stale,
	}); !errors.Is(err, ErrMemoryCandidateChanged) {
		t.Fatalf("reject with a stale file hash = %v, want ErrMemoryCandidateChanged", err)
	}
	if !candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("a refused decision removed the proposal anyway")
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("a refused decision retired the row: %v", err)
	}
}
