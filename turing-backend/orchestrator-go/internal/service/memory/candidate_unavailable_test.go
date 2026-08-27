package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// The claim as the user last saw it, which is the one string that must not
// come back off a database row once the file holding it is unreachable.
const unreachableClaim = "The user prefers dark mode."

// A proposal's body lives in the vault. The row beside it is Turing's record of
// what it once wrote, kept so a decision can be audited — not a second copy to
// serve when the vault is gone.
//
// Serving it anyway is two failures at once. The user is shown text nobody can
// confirm is still in their inbox, above buttons that compare a hash against a
// file the server cannot open — so every decision the page offers is refused —
// and a vault the user unmounted deliberately keeps answering with its
// contents.
func TestListMemoryCandidatesServesNoRowCopyWithNoVault(t *testing.T) {
	_, repo, _, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", unreachableClaim)

	// The vault the app could not open at startup, over a database that still
	// remembers everything the last run proposed.
	vaultless := New(repo, nil, nil)
	listing, err := vaultless.ListMemoryCandidates(ctx, &turingv1.ListMemoryCandidatesRequest{})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	listed := listedCandidate(t, listing.GetCandidates(), candidate.CandidateID)
	assertNoReadableBody(t, listed)
}

// The same rule on the filtered listing, which takes the other branch: it has
// no whole-vault walk behind it and reads one proposal at a time.
func TestFilteredListMemoryCandidatesServesNoRowCopyWithNoVault(t *testing.T) {
	_, repo, _, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", unreachableClaim)

	vaultless := New(repo, nil, nil)
	listing, err := vaultless.ListMemoryCandidates(ctx, &turingv1.ListMemoryCandidatesRequest{
		State: turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PENDING,
	})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	listed := listedCandidate(t, listing.GetCandidates(), candidate.CandidateID)
	assertNoReadableBody(t, listed)
}

func TestGetMemoryCandidateServesNoRowCopyWithNoVault(t *testing.T) {
	_, repo, _, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", unreachableClaim)

	vaultless := New(repo, nil, nil)
	fetched, err := vaultless.GetMemoryCandidate(ctx, &turingv1.GetMemoryCandidateRequest{
		CandidateId: candidate.CandidateID,
	})
	if err != nil {
		t.Fatalf("GetMemoryCandidate: %v", err)
	}
	assertNoReadableBody(t, fetched)
}

// The whole page keeps the proposal visible — the user has something waiting in
// an inbox they cannot currently reach, and hiding it would say the opposite —
// but visible is all it is: no text, no token, and the reason said out loud.
func TestListMemoryStateShowsTheProposalWithoutItsBodyWithNoVault(t *testing.T) {
	_, repo, _, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", unreachableClaim)

	vaultless := New(repo, nil, nil)
	state, err := vaultless.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := listedCandidate(t, state.GetCandidates(), candidate.CandidateID)
	assertNoReadableBody(t, listed)
	if listed.GetInboxPath() != candidate.InboxPath {
		t.Fatalf("listed path = %q, want the proposal still identifiable", listed.GetInboxPath())
	}
	if listed.GetState() != turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PENDING {
		t.Fatalf("listed state = %v, want the lifecycle the row still knows", listed.GetState())
	}
}

// A vault that is attached and cannot answer for one proposal is the same
// situation one file at a time. The read failed, so the bytes it would have
// carried are not the row's to supply.
func TestCandidateReadFailureServesNoRowCopy(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", unreachableClaim)
	if err := os.Remove(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
		t.Fatalf("remove the proposal file: %v", err)
	}

	fetched, err := service.GetMemoryCandidate(ctx, &turingv1.GetMemoryCandidateRequest{
		CandidateId: candidate.CandidateID,
	})
	if err != nil {
		t.Fatalf("GetMemoryCandidate: %v", err)
	}
	assertNoReadableBody(t, fetched)
}

func listedCandidate(t *testing.T, candidates []*turingv1.MemoryCandidate, candidateID string) *turingv1.MemoryCandidate {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.GetCandidateId() == candidateID {
			return candidate
		}
	}
	t.Fatalf("candidate %q is not in the listing", candidateID)
	return nil
}

// assertNoReadableBody is the whole rule in one place: a proposal whose file
// nobody could read carries no text, no compare-and-set token, and a reason
// that is not "nothing is wrong".
func assertNoReadableBody(t *testing.T, candidate *turingv1.MemoryCandidate) {
	t.Helper()
	if strings.Contains(candidate.GetContent(), unreachableClaim) {
		t.Fatalf("the response served the row's copy of an unreachable proposal: %q", candidate.GetContent())
	}
	if candidate.GetContent() != "" {
		t.Fatalf("content = %q, want nothing for a proposal nobody could read", candidate.GetContent())
	}
	if candidate.GetContentHash() != "" {
		t.Fatalf("content hash = %q, want no token for bytes nobody read", candidate.GetContentHash())
	}
	switch candidate.GetUnavailableReason() {
	case turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE,
		turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_UNSPECIFIED:
		t.Fatalf("unavailable reason = %v, want the vault problem said out loud",
			candidate.GetUnavailableReason())
	}
}
