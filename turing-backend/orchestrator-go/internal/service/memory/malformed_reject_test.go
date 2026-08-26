package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/status"
)

// corruptedProposal is a managed proposal whose frontmatter no longer parses —
// the state a user reaches by opening the vault and editing the file Turing
// wrote. The row is intact and still pending; the bytes are not a note.
func corruptedProposal(
	t *testing.T,
	ctx context.Context,
	repo *repository.Repository,
	vaultRoot string,
	sessionID string,
) repository.MemoryCandidate {
	t.Helper()
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      repository.MemoryCandidateKindBelief,
		Title:     "Dark mode",
		Body:      "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	full := filepath.Join(vaultRoot, filepath.FromSlash(candidate.InboxPath))
	if err := os.WriteFile(full, []byte("---\nkind: \"belief\nunterminated\n"), 0o600); err != nil {
		t.Fatalf("corrupt the proposal: %v", err)
	}
	return candidate
}

// The page has to be able to say what a proposal it could not read *is*: a
// managed, pending claim about the user, with the reason it cannot be shown
// whole. Anything less and the card renders as an unmanaged draft or as an
// already-decided row, neither of which is true, and the one action that is
// still safe — throwing it away — cannot be offered.
func TestListMemoryStateKeepsAnUnreadableProposalManagedAndPending(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	_, sessionID := newRun(t, repo, callCtx)
	candidate := corruptedProposal(t, callCtx, repo, vault.Root(), sessionID)

	state, err := service.ListMemoryState(callCtx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := candidateByID(t, state, candidate.CandidateID)
	if !listed.GetManaged() {
		t.Fatal("a proposal Turing wrote was listed as something it does not own")
	}
	if listed.GetState() != turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PENDING {
		t.Fatalf("state = %v, want a proposal still waiting on the user", listed.GetState())
	}
	if listed.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED {
		t.Fatalf("unavailable reason = %v, want the page told why it cannot show this whole", listed.GetUnavailableReason())
	}
}

// A proposal whose file no longer parses is still one the user has to be able
// to be rid of. The page cannot make a claim about bytes it could not read, so
// it sends no compare-and-set, and the server takes the rejection on the same
// terms: no hash, no kind, no readable file — just the user saying no to
// whatever is sitting in their inbox.
func TestRejectMemoryCandidateDiscardsAProposalThePageCouldNotRead(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	_, sessionID := newRun(t, repo, callCtx)
	candidate := corruptedProposal(t, callCtx, repo, vault.Root(), sessionID)
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))

	response, err := service.RejectMemoryCandidate(callCtx, &turingv1.RejectMemoryCandidateRequest{
		CandidateId: candidate.CandidateID,
	})
	if err != nil {
		t.Fatalf("rejecting a proposal nobody can parse: %v", err)
	}
	if response.GetCandidate().GetState() != turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_REJECTED {
		t.Fatalf("state = %v, want rejected", response.GetCandidate().GetState())
	}
	if _, statErr := os.Lstat(full); !os.IsNotExist(statErr) {
		t.Fatalf("the rejected proposal is still in the inbox: %v", statErr)
	}
	if _, rowErr := repo.MemoryCandidateByID(callCtx, candidate.CandidateID); rowErr == nil {
		t.Fatal("the rejected proposal kept its row")
	}
}

// The other half of the same rule, at the RPC boundary: a caller that names
// the bytes is making a claim, and a claim cannot be checked against a file
// nobody can read. The refusal keeps both the file and the row, and says
// nothing about what the proposal claimed.
func TestRejectMemoryCandidateRefusesAHashClaimAgainstAFileNobodyCanRead(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	_, sessionID := newRun(t, repo, callCtx)
	candidate := corruptedProposal(t, callCtx, repo, vault.Root(), sessionID)
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))

	_, err := service.RejectMemoryCandidate(callCtx, &turingv1.RejectMemoryCandidateRequest{
		CandidateId:           candidate.CandidateID,
		ExpectedCandidateHash: candidate.ContentHash,
	})
	if err == nil {
		t.Fatal("a compare-and-set was accepted against bytes nobody could read")
	}
	if strings.Contains(status.Convert(err).Message(), "dark mode") {
		t.Fatalf("the refusal carries the claim: %q", status.Convert(err).Message())
	}
	if _, statErr := os.Lstat(full); statErr != nil {
		t.Fatalf("the refused rejection removed the file anyway: %v", statErr)
	}
	if _, rowErr := repo.MemoryCandidateByID(callCtx, candidate.CandidateID); rowErr != nil {
		t.Fatalf("the refused rejection retired the row: %v", rowErr)
	}
}

// And the two acceptances stay refused, hash or no hash: both need the kind,
// and the kind is a question about bytes nobody can read. This is why the page
// offers exactly one button on an unreadable proposal.
func TestPromoteAndApplyStayRefusedOnAProposalNobodyCanRead(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	_, sessionID := newRun(t, repo, callCtx)
	candidate := corruptedProposal(t, callCtx, repo, vault.Root(), sessionID)
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))

	if _, err := service.PromoteMemoryCandidate(callCtx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId: candidate.CandidateID,
	}); err == nil {
		t.Fatal("a proposal nobody can parse was promoted into beliefs")
	}
	if _, err := service.ApplyMemoryProfile(callCtx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId: candidate.CandidateID,
		Content:     "# Profile\n\nRewritten.\n",
	}); err == nil {
		t.Fatal("a proposal nobody can parse rewrote the user's profile")
	}
	if _, statErr := os.Lstat(full); statErr != nil {
		t.Fatalf("a refused decision removed the file: %v", statErr)
	}
	if _, rowErr := repo.MemoryCandidateByID(callCtx, candidate.CandidateID); rowErr != nil {
		t.Fatalf("a refused decision retired the row: %v", rowErr)
	}
}
