package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// overfillVault pushes the vault past the scan bound, so every whole-vault walk
// over it refuses outright. Stated as a precondition so the test cannot quietly
// degrade into one over a vault the walk was happy with.
func overfillVault(t *testing.T, vault *memoryfiles.Vault) {
	t.Helper()
	beliefs := filepath.Join(vault.Root(), memoryfiles.BeliefsDirName)
	for index := range memoryfiles.MaxVaultIndexedFiles + 1 {
		name := filepath.Join(beliefs, fmt.Sprintf("filler-%05d.md", index))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatalf("seed filler note %d: %v", index, err)
		}
	}
	if _, err := vault.Scan(context.Background()); !errors.Is(err, memoryfiles.ErrVaultTooLarge) {
		t.Fatalf("the fixture is not over the index bound: %v", err)
	}
}

// A walk that refused says nothing about what is waiting in the inbox. The
// proposals are rows, they are still pending, and a page that answered with an
// empty list would tell the user there is nothing to decide about themselves
// while a claim sits in their vault waiting on them.
func TestListMemoryStateStillListsProposalsWhenTheWalkRefuses(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	_, sessionID := newRun(t, repo, callCtx)
	candidate := seedCandidateRow(t, repo, callCtx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", unreachableClaim)
	overfillVault(t, vault)

	state, err := service.ListMemoryState(callCtx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := listedCandidate(t, state.GetCandidates(), candidate.CandidateID)
	if listed.GetInboxPath() != candidate.InboxPath {
		t.Fatalf("inbox path = %q, want the row's %q", listed.GetInboxPath(), candidate.InboxPath)
	}
	if !listed.GetManaged() {
		t.Fatal("a proposal Turing wrote was listed as something it does not own")
	}
	if listed.GetState() != turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PENDING {
		t.Fatalf("state = %v, want a proposal still waiting on the user", listed.GetState())
	}
	if listed.GetKind() != turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_BELIEF {
		t.Fatalf("kind = %v, want the row's belief", listed.GetKind())
	}
	// Where the claim came from, which is the row's to know and survives a
	// walk that never happened.
	sources := make([]string, 0, len(listed.GetProvenance()))
	for _, provenance := range listed.GetProvenance() {
		sources = append(sources, provenance.GetSourceSessionId())
	}
	if len(sources) != 1 || sources[0] != sessionID {
		t.Fatalf("provenance sources = %v, want the conversation %q that proposed it", sources, sessionID)
	} // The whole-vault walk is a discovery bound; one confined read of one
	// proposal is not, so this proposal is still readable and the page says
	// what the file says.
	if listed.GetContent() != unreachableClaim {
		t.Fatalf("content = %q, want the proposal's own words", listed.GetContent())
	}
}

// The unfiltered listing takes the same branch as the page and must answer the
// same way.
func TestListMemoryCandidatesStillListsProposalsWhenTheWalkRefuses(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	_, sessionID := newRun(t, repo, callCtx)
	candidate := seedCandidateRow(t, repo, callCtx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", unreachableClaim)
	overfillVault(t, vault)

	listing, err := service.ListMemoryCandidates(callCtx, &turingv1.ListMemoryCandidatesRequest{})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	listed := listedCandidate(t, listing.GetCandidates(), candidate.CandidateID)

	// And the single fetch agrees with it, token for token: the two are the
	// same proposal read the same way, and a page whose list and whose card
	// disagree about the compare-and-set is a page whose buttons are refused.
	fetched, err := service.GetMemoryCandidate(callCtx, &turingv1.GetMemoryCandidateRequest{
		CandidateId: candidate.CandidateID,
	})
	if err != nil {
		t.Fatalf("GetMemoryCandidate: %v", err)
	}
	if listed.GetContent() != fetched.GetContent() {
		t.Fatalf("listed content %q disagrees with the fetched %q", listed.GetContent(), fetched.GetContent())
	}
	if listed.GetContentHash() != fetched.GetContentHash() {
		t.Fatalf("listed token %q disagrees with the fetched %q", listed.GetContentHash(), fetched.GetContentHash())
	}
	if listed.GetUnavailableReason() != fetched.GetUnavailableReason() {
		t.Fatalf("listed reason %v disagrees with the fetched %v",
			listed.GetUnavailableReason(), fetched.GetUnavailableReason())
	}
}

// A vault the walk refused *and* whose files nobody can open: the proposal is
// still listed, because something is waiting, and it carries no text and no
// token, because nothing read it.
func TestListMemoryStateWithholdsBodiesWhenTheInboxCannotBeOpened(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	_, sessionID := newRun(t, repo, callCtx)
	candidate := seedCandidateRow(t, repo, callCtx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", unreachableClaim)
	overfillVault(t, vault)
	sealDirectory(t, filepath.Join(vault.Root(), memoryfiles.InboxDirName))

	state, err := service.ListMemoryState(callCtx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := listedCandidate(t, state.GetCandidates(), candidate.CandidateID)
	if listed.GetInboxPath() != candidate.InboxPath {
		t.Fatalf("inbox path = %q, want the row's %q", listed.GetInboxPath(), candidate.InboxPath)
	}
	assertNoReadableBody(t, listed)
}

// The walk succeeded over part of the vault and could not list the inbox. A
// proposal it did not see is not a proposal that is gone — nobody looked — so
// the page says the folder could not be read rather than claiming the file is
// missing, and offers no decision against bytes it never had.
func TestListMemoryStateSaysUnreadableRatherThanMissingOnAnIncompleteInboxWalk(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	_, sessionID := newRun(t, repo, callCtx)
	candidate := seedCandidateRow(t, repo, callCtx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", unreachableClaim)
	sealDirectory(t, filepath.Join(vault.Root(), memoryfiles.InboxDirName))

	scan, err := repo.ScanMemoryVault(callCtx)
	if err != nil {
		t.Fatalf("the fixture made the whole walk fail rather than leaving the inbox unlisted: %v", err)
	}
	if scan.Completeness.Inbox.Complete {
		t.Fatal("the fixture did not stop the inbox from being listed")
	}

	state, err := service.ListMemoryState(callCtx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := listedCandidate(t, state.GetCandidates(), candidate.CandidateID)
	assertNoReadableBody(t, listed)
	if listed.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_UNREADABLE {
		t.Fatalf("unavailable reason = %v, want the folder said to be unreadable rather than the file said to be gone",
			listed.GetUnavailableReason())
	}
}

// sealDirectory takes every permission off a directory, which is what a vault
// on a volume the user's account cannot read looks like from here.
func sealDirectory(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions do not refuse anything")
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("seal %q: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}
