package repository

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A proposal whose frontmatter no longer parses is still a proposal the user
// has to be able to get rid of.
//
// A rejection asks nothing about the kind and, when the caller makes no claim
// about the bytes, nothing about the content either: it is the user saying no
// to whatever is sitting there. Refusing it because the file cannot be read
// would leave them with a claim about themselves that they can neither accept
// nor throw away.
func TestRejectDiscardsAProposalWhoseFileNoLongerParses(t *testing.T) {
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
	if err := os.WriteFile(full, []byte("---\nkind: \"belief\nunterminated\n"), 0o600); err != nil {
		t.Fatalf("corrupt the proposal: %v", err)
	}

	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); err != nil {
		t.Fatalf("rejecting a proposal nobody can parse: %v", err)
	}
	if _, statErr := os.Lstat(full); !os.IsNotExist(statErr) {
		t.Fatalf("the rejected proposal is still in the inbox: %v", statErr)
	}
	if _, rowErr := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(rowErr, ErrMemoryCandidateNotFound) {
		t.Fatalf("the rejected proposal kept its row: %v", rowErr)
	}
}

// The other half of the same rule. A caller that *does* name the bytes it
// decided against is making a claim, and a claim cannot be checked against a
// file nobody can read — so that rejection is refused, and the proposal and its
// row both survive for the user to look at again.
func TestRejectRefusesAClaimAboutAFileItCannotRead(t *testing.T) {
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
	if err := os.WriteFile(full, []byte("---\nkind: \"belief\nunterminated\n"), 0o600); err != nil {
		t.Fatalf("corrupt the proposal: %v", err)
	}

	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID:           candidate.CandidateID,
		ExpectedCandidateHash: candidate.ContentHash,
	}); err == nil {
		t.Fatal("a compare-and-set was accepted against bytes nobody could read")
	}
	if _, statErr := os.Lstat(full); statErr != nil {
		t.Fatalf("the refused rejection removed the file anyway: %v", statErr)
	}
	if _, rowErr := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); rowErr != nil {
		t.Fatalf("the refused rejection retired the row: %v", rowErr)
	}
}

// A promotion and an apply cannot step over an unreadable file the way a
// rejection can: both need the kind, and the kind is a question about bytes.
func TestPromoteAndApplyRefuseAProposalTheyCannotRead(t *testing.T) {
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
	if err := os.WriteFile(full, []byte("---\nkind: \"belief\nunterminated\n"), 0o600); err != nil {
		t.Fatalf("corrupt the proposal: %v", err)
	}

	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); err == nil {
		t.Fatal("a proposal nobody can parse was promoted into beliefs")
	}
	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID: candidate.CandidateID,
		Content:     "# Profile\n\nRewritten.\n",
	}); err == nil {
		t.Fatal("a proposal nobody can parse rewrote the user's profile")
	}
	if _, statErr := os.Lstat(full); statErr != nil {
		t.Fatalf("a refused decision removed the file: %v", statErr)
	}
}
