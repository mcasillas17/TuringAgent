package repository

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// The hashless rejection is the one door out for a proposal nobody can parse,
// and it used to be the one door that named nothing at all.
//
// A decision's pre-check reads the candidate under the vault's path lock and
// gives that lock back before the primitive takes it again. Every other door
// closes that window with a hash. This one had none — nothing could parse the
// file to produce one — so whatever was under the name when the primitive
// looked is what it deleted. The user's editor, a sync client and Turing's own
// writer all replace a file by renaming a new one over the top, so the window
// is the ordinary way this vault gets written to.
//
// These tests stand in that window with a proposal that will not parse. What
// the pre-check now carries across it is the identity of the exact entry it
// failed on, and the removal is held to that entry, still unreadable, or it
// removes nothing.
const unreadableProposal = "---\nrefs: [broken\n---\n\nNobody can read this.\n"

func replaceCandidateAtDecisionBarrier(t *testing.T, repo *Repository, vault *memoryfiles.Vault, relPath string, content string) {
	t.Helper()
	repo.memoryDecisionFileBarrier = func() {
		repo.memoryDecisionFileBarrier = nil
		full := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
		staged := filepath.Join(filepath.Dir(full), "replacement-in-flight.md")
		if err := os.WriteFile(staged, []byte(content), 0o600); err != nil {
			t.Errorf("write the replacement: %v", err)
			return
		}
		// Rename, not truncate-and-write: this is how an editor replaces a
		// file, and it is what makes the new bytes a new inode under the name.
		if err := os.Rename(staged, full); err != nil {
			t.Errorf("install the replacement: %v", err)
		}
	}
}

func seedUnreadableCandidate(t *testing.T, repo *Repository, vault *memoryfiles.Vault) MemoryCandidate {
	t.Helper()
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeVaultNote(t, vault, candidate.InboxPath, unreadableProposal)
	return candidate
}

func requireRefusedHashlessRejection(t *testing.T, repo *Repository, vault *memoryfiles.Vault, candidate MemoryCandidate, want string) {
	t.Helper()
	err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID})
	if !errors.Is(err, memoryfiles.ErrStaleContent) {
		t.Fatalf("hashless rejection across the window = %v, want ErrStaleContent", err)
	}
	if got := readVaultNote(t, vault, candidate.InboxPath); got != want {
		t.Fatalf("the file at the candidate path holds %q, want %q", got, want)
	}
	row, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID)
	if err != nil {
		t.Fatalf("a refused rejection retired the row: %v", err)
	}
	if row.State != MemoryCandidateStatePending {
		t.Fatalf("a refused rejection left the proposal in %q", row.State)
	}
}

// The replacement reads perfectly well. It is a claim about the user that
// nobody has seen, and a rejection of the broken file it replaced is not a
// decision about it.
func TestHashlessRejectRefusesAReadableProposalThatReplacedTheUnreadableOne(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	candidate := seedUnreadableCandidate(t, repo, vault)
	const replacement = "---\nkind: belief\n---\n\nA newer claim nobody has read yet.\n"
	replaceCandidateAtDecisionBarrier(t, repo, vault, candidate.InboxPath, replacement)

	requireRefusedHashlessRejection(t, repo, vault, candidate, replacement)
}

// The replacement is broken too, and that changes nothing. "Still unreadable"
// says whether a file can be parsed, not whose bytes it is.
func TestHashlessRejectRefusesAnotherUnreadableProposalThatTookTheName(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	candidate := seedUnreadableCandidate(t, repo, vault)
	const replacement = "---\nalso: [broken\n---\n\nA different unreadable claim.\n"
	replaceCandidateAtDecisionBarrier(t, repo, vault, candidate.InboxPath, replacement)

	requireRefusedHashlessRejection(t, repo, vault, candidate, replacement)
}

// The user opened the proposal and fixed the frontmatter. Same inode, new
// bytes, and it is no longer the thing the hashless door exists for: they can
// read it now, so they decide about it by reading it.
func TestHashlessRejectRefusesAProposalRepairedAfterItsPreCheck(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	candidate := seedUnreadableCandidate(t, repo, vault)
	const repaired = "---\nkind: belief\n---\n\nThe user fixed the frontmatter.\n"
	repo.memoryDecisionFileBarrier = func() {
		repo.memoryDecisionFileBarrier = nil
		full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
		if err := os.WriteFile(full, []byte(repaired), 0o600); err != nil {
			t.Errorf("repair the proposal in place: %v", err)
		}
	}

	requireRefusedHashlessRejection(t, repo, vault, candidate, repaired)
}

// Nothing moved in the window, so the user gets what they asked for: the
// unreadable proposal leaves the inbox and the row goes with it.
func TestHashlessRejectStillRemovesAnUntouchedUnreadableProposal(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	candidate := seedUnreadableCandidate(t, repo, vault)

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
