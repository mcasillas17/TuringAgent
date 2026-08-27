package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// A rejection that could not be read has no hash to hold it to: the user is
// saying no to whatever is sitting there. That is the one decision path with
// nothing after the detach that would notice a request had ended, which is why
// the vault primitive checks for itself and puts the file back.
//
// This is the repository half of the same promise. A rejection that ends
// without deciding anything must leave the candidate exactly as it found it —
// the row still pending, so the page still offers the decision, and the file
// still in the inbox, so there is something for that decision to be about. A
// row consumed under a cancelled request would retire a proposal nobody
// answered; a file removed under one would delete a claim about the user their
// inbox can never show them again.
func TestCancelledUnreadableRejectionLeavesTheCandidatePendingAndItsFileInPlace(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	// Frontmatter nothing can parse is what puts this decision through the
	// hashless door: the pre-check reads nothing, so it names nothing.
	const unreadable = "---\nkind: \"belief\nunterminated\n"
	if err := os.WriteFile(full, []byte(unreadable), 0o600); err != nil {
		t.Fatalf("corrupt the proposal: %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx())
	defer cancel()
	repo.memoryDecisionFileBarrier = func() {
		cancel()
		repo.memoryDecisionFileBarrier = nil
	}

	err = repo.RejectMemoryCandidate(cancelled, MemoryCandidateDecision{CandidateID: candidate.CandidateID})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled rejection = %v, want the cancellation reported as one", err)
	}
	requireCandidateSurvivedItsUndecidedRejection(t, repo, vault, candidate, unreadable)
}

// The other way a request ends with nobody left to receive its outcome. A
// deadline is not a verdict either, and the answer has to be the same one.
func TestTimedOutUnreadableRejectionLeavesTheCandidatePendingAndItsFileInPlace(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	const unreadable = "---\nkind: \"belief\nunterminated\n"
	if err := os.WriteFile(full, []byte(unreadable), 0o600); err != nil {
		t.Fatalf("corrupt the proposal: %v", err)
	}

	expiring, cancel := context.WithTimeout(ctx(), 20*time.Millisecond)
	defer cancel()
	repo.memoryDecisionFileBarrier = func() {
		// Waiting on the deadline rather than sleeping past it, so the test
		// cannot be flaky in either direction.
		<-expiring.Done()
		repo.memoryDecisionFileBarrier = nil
	}

	err = repo.RejectMemoryCandidate(expiring, MemoryCandidateDecision{CandidateID: candidate.CandidateID})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a timed-out rejection = %v, want the expired deadline reported as one", err)
	}
	requireCandidateSurvivedItsUndecidedRejection(t, repo, vault, candidate, unreadable)
}

// requireCandidateSurvivedItsUndecidedRejection holds the state a rejection
// that decided nothing has to leave behind: the row pending, and the bytes
// still readable at the path the row names.
func requireCandidateSurvivedItsUndecidedRejection(
	t *testing.T,
	repo *Repository,
	vault *memoryfiles.Vault,
	candidate MemoryCandidate,
	expected string,
) {
	t.Helper()
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("the proposal left the inbox on a request nobody finished: %v", readErr)
	}
	if string(survived) != expected {
		t.Fatalf("the file holds %q, want the proposal %q", survived, expected)
	}
	row, rowErr := repo.MemoryCandidateByID(ctx(), candidate.CandidateID)
	if rowErr != nil {
		t.Fatalf("the row was retired by a rejection nobody finished: %v", rowErr)
	}
	if row.State != MemoryCandidateStatePending {
		t.Fatalf("the candidate is %q, want it left pending for the user to decide again", row.State)
	}
}
