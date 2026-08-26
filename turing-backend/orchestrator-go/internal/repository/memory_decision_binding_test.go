package repository

import (
	"errors"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// A decision's own pre-check reads the proposal and lets go of it: ReadInboxNote
// takes the vault's path lock for the read alone, so the primitive that follows
// can take it again. That release is a real window, and the user has their vault
// open in Obsidian on the other side of it.
//
// These tests stand in that window. The repository has already read the file and
// agreed the hash matches; then the file moves; then the primitive runs. Only a
// check the primitive makes against its own read can refuse now, so each of
// these fails the moment that check is deleted — the pre-check above it still
// passes.
func editCandidateAtDecisionBarrier(t *testing.T, repo *Repository, vault *memoryfiles.Vault, relPath string) {
	t.Helper()
	repo.memoryDecisionFileBarrier = func() {
		writeVaultNote(t, vault, relPath, readVaultNote(t, vault, relPath)+"\nAnd light mode on Tuesdays.\n")
		repo.memoryDecisionFileBarrier = nil
	}
}

func TestPromoteRefusesAProposalEditedAfterItsPreCheck(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	editCandidateAtDecisionBarrier(t, repo, vault, candidate.InboxPath)

	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID:           candidate.CandidateID,
		ExpectedCandidateHash: candidate.ContentHash,
	}); !errors.Is(err, memoryfiles.ErrStaleContent) {
		t.Fatalf("promote across the window = %v, want ErrStaleContent", err)
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "dark", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("a refused promotion wrote a belief anyway: %+v", hits)
	}
	if !candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("a refused promotion moved the proposal out of the inbox")
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("a refused promotion retired the row: %v", err)
	}
}

func TestApplyProfileRefusesAProposalEditedAfterItsPreCheck(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	original := "# Profile\n\nWrites Go.\n"
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, original)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindProfileEdit,
		Title: "Call me Miguel", Body: "The user goes by Miguel.",
	})
	if err != nil {
		t.Fatal(err)
	}
	editCandidateAtDecisionBarrier(t, repo, vault, candidate.InboxPath)

	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:           candidate.CandidateID,
		ExpectedContentHash:   memoryfiles.ContentHash(original),
		Content:               "# Profile\n\nWrites Go.\n\nGoes by Miguel.\n",
		ExpectedCandidateHash: candidate.ContentHash,
	}); !errors.Is(err, memoryfiles.ErrStaleContent) {
		t.Fatalf("apply across the window = %v, want ErrStaleContent", err)
	}
	if profile := vault.EditableProfile(ctx()); profile.Content != original {
		t.Fatalf("a refused apply rewrote the profile: %q", profile.Content)
	}
	if !candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("a refused apply removed the proposal")
	}
	// Nothing was written, so the claim the apply took on its way in has to go
	// back: the user must be able to decide about this proposal again.
	decided, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID)
	if err != nil {
		t.Fatalf("a refused apply retired the row: %v", err)
	}
	if decided.State != MemoryCandidateStatePending {
		t.Fatalf("a refused apply left the proposal in %q", decided.State)
	}
}

func TestRejectRefusesAProposalEditedAfterItsPreCheck(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	editCandidateAtDecisionBarrier(t, repo, vault, candidate.InboxPath)

	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID:           candidate.CandidateID,
		ExpectedCandidateHash: candidate.ContentHash,
	}); !errors.Is(err, memoryfiles.ErrStaleContent) {
		t.Fatalf("reject across the window = %v, want ErrStaleContent", err)
	}
	if !candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("a refused rejection deleted the proposal anyway")
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("a refused rejection retired the row: %v", err)
	}
}

// A rejection with no hash at all still binds, because the pre-check read the
// file and can say what it read. What it may not do is delete whatever happens
// to be there by the time the primitive looks.
func TestRejectWithoutAHashStillRefusesAProposalEditedAfterItsPreCheck(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	editCandidateAtDecisionBarrier(t, repo, vault, candidate.InboxPath)

	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID: candidate.CandidateID,
	}); !errors.Is(err, memoryfiles.ErrStaleContent) {
		t.Fatalf("hashless reject across the window = %v, want ErrStaleContent", err)
	}
	if !candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("a refused rejection deleted the proposal anyway")
	}
}

// The one rejection that may not bind: a proposal whose frontmatter nobody can
// read has no hash the pre-check could have produced. Round three gave that
// file a way out and it stays open — through the mode that says so by name.
func TestRejectStillRetiresAProposalNobodyCanRead(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeVaultNote(t, vault, candidate.InboxPath, "---\nrefs: [broken\n---\n\nUnreadable.\n")

	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID: candidate.CandidateID,
	}); err != nil {
		t.Fatalf("reject an unreadable proposal: %v", err)
	}
	if candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("the unreadable proposal is still in the inbox")
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("the rejected row survived: %v", err)
	}
}
