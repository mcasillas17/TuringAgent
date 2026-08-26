package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// The listing serves the file. A decision has to accept what the listing
// served, or the page shows a proposal, offers a button, and the server refuses
// every press of it.
//
// This is the whole of the round-2 finding: the listing overlays the inbox
// file's bytes, and the decision was comparing that token against the database
// row's copy of what Turing originally wrote. Any proposal the user edited in
// Obsidian was therefore undecidable — permanently, since re-reading the page
// only ever produced the same token the server would reject.
func TestPromoteAcceptsTheHashTheListingServedAfterAVaultEdit(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", "The user prefers dark mode.")
	editInboxFile(t, vault, candidate.InboxPath, "The user prefers dark mode.", "The user prefers light mode.")

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := candidateByID(t, state, candidate.CandidateID)

	promoted, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId: candidate.CandidateID,
		// Exactly what the page was handed, in both fields: a client cannot
		// know that one of them once meant something else.
		ExpectedContentHash:   listed.GetContentHash(),
		ExpectedCandidateHash: listed.GetContentHash(),
	})
	if err != nil {
		t.Fatalf("promoting the proposal the listing served: %v", err)
	}
	if !strings.Contains(promoted.GetNote().GetContent(), "light mode") {
		t.Fatalf("promoted note = %q, want the words the user actually accepted", promoted.GetNote().GetContent())
	}
}

func TestRejectAcceptsTheHashTheListingServedAfterAVaultEdit(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", "The user prefers dark mode.")
	editInboxFile(t, vault, candidate.InboxPath, "The user prefers dark mode.", "The user prefers light mode.")

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := candidateByID(t, state, candidate.CandidateID)

	if _, err := service.RejectMemoryCandidate(ctx, &turingv1.RejectMemoryCandidateRequest{
		CandidateId:           candidate.CandidateID,
		ExpectedContentHash:   listed.GetContentHash(),
		ExpectedCandidateHash: listed.GetContentHash(),
	}); err != nil {
		t.Fatalf("rejecting the proposal the listing served: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(statErr) {
		t.Fatalf("the rejected proposal is still in the inbox: %v", statErr)
	}
}

// The kind decides which decision is offered, and the listing already reads it
// from the file. So must the decision: a proposal the user rewrote into a
// profile edit is a profile edit, and the row saying "belief" is a record of
// what Turing wrote rather than a claim about what the file says now.
func TestApplyAcceptsAProposalTheFileNowDeclaresAProfileEdit(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Call me Miguel", "The user goes by Miguel.")
	writeVaultDocument(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWrites Go.\n")
	editInboxFile(t, vault, candidate.InboxPath, `kind: "belief"`, `kind: "profile_edit"`)

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := candidateByID(t, state, candidate.CandidateID)
	if listed.GetKind() != turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_PROFILE_EDIT {
		t.Fatalf("listed kind = %v, want the kind the file declares", listed.GetKind())
	}
	reviewed := "# Profile\n\nWrites Go.\n\nThe user goes by Miguel.\n"

	applied, err := service.ApplyMemoryProfile(ctx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId:           candidate.CandidateID,
		Content:               reviewed,
		ExpectedContentHash:   state.GetProfile().GetContentHash(),
		ExpectedCandidateHash: listed.GetContentHash(),
	})
	if err != nil {
		t.Fatalf("applying the proposal the file now calls a profile edit: %v", err)
	}
	if applied.GetProfile().GetContent() != reviewed {
		t.Fatalf("applied profile = %q, want the document the user reviewed", applied.GetProfile().GetContent())
	}
}

// The mirror of the case above, and the reason the kind cannot simply be
// ignored: a proposal the file now calls a profile edit must not be promotable
// into beliefs/ as though it were a claim the user accepted about themselves.
func TestPromoteRefusesAProposalTheFileNowDeclaresAProfileEdit(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Call me Miguel", "The user goes by Miguel.")
	editInboxFile(t, vault, candidate.InboxPath, `kind: "belief"`, `kind: "profile_edit"`)

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := candidateByID(t, state, candidate.CandidateID)

	if _, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId:           candidate.CandidateID,
		ExpectedCandidateHash: listed.GetContentHash(),
	}); err == nil {
		t.Fatal("a proposal the file calls a profile edit was promoted into beliefs anyway")
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); statErr != nil {
		t.Fatalf("the refused promotion moved the file anyway: %v", statErr)
	}
}

// And the other mirror: a proposal the file still calls a belief cannot be
// applied to the profile, whatever the row says.
func TestApplyRefusesAProposalTheFileStillDeclaresABelief(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindProfileEdit, "Call me Miguel", "The user goes by Miguel.")
	writeVaultDocument(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWrites Go.\n")
	editInboxFile(t, vault, candidate.InboxPath, `kind: "profile_edit"`, `kind: "belief"`)

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := candidateByID(t, state, candidate.CandidateID)

	if _, err := service.ApplyMemoryProfile(ctx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId:           candidate.CandidateID,
		Content:               "# Profile\n\nWrites Go.\n\nThe user goes by Miguel.\n",
		ExpectedContentHash:   state.GetProfile().GetContentHash(),
		ExpectedCandidateHash: listed.GetContentHash(),
	}); err == nil {
		t.Fatal("a proposal the file calls a belief rewrote the user's profile")
	}
	profile := vault.EditableProfile(ctx)
	if profile.Content != "# Profile\n\nWrites Go.\n" {
		t.Fatalf("a refused apply rewrote the profile: %q", profile.Content)
	}
}

// The other direction of the same rule: a token from a listing taken *before*
// the user edited the file is stale, and every decision carrying it is refused
// until they read the page again.
func TestAListingTakenBeforeAVaultEditRefusesEveryDecision(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	belief := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", "The user prefers dark mode.")
	edit := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindProfileEdit, "Call me Miguel", "The user goes by Miguel.")
	writeVaultDocument(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWrites Go.\n")

	before, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	staleBelief := candidateByID(t, before, belief.CandidateID).GetContentHash()
	staleEdit := candidateByID(t, before, edit.CandidateID).GetContentHash()
	editInboxFile(t, vault, belief.InboxPath, "The user prefers dark mode.", "The user prefers light mode.")
	editInboxFile(t, vault, edit.InboxPath, "The user goes by Miguel.", "The user goes by Miguelito.")

	_, promoteErr := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId:           belief.CandidateID,
		ExpectedCandidateHash: staleBelief,
	})
	assertFailedPrecondition(t, promoteErr, "promote against a listing taken before the edit")

	_, rejectErr := service.RejectMemoryCandidate(ctx, &turingv1.RejectMemoryCandidateRequest{
		CandidateId:           belief.CandidateID,
		ExpectedCandidateHash: staleBelief,
	})
	assertFailedPrecondition(t, rejectErr, "reject against a listing taken before the edit")

	_, applyErr := service.ApplyMemoryProfile(ctx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId:           edit.CandidateID,
		Content:               "# Profile\n\nWrites Go.\n\nThe user goes by Miguel.\n",
		ExpectedContentHash:   before.GetProfile().GetContentHash(),
		ExpectedCandidateHash: staleEdit,
	})
	assertFailedPrecondition(t, applyErr, "apply against a listing taken before the edit")

	for _, candidate := range []repository.MemoryCandidate{belief, edit} {
		if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); statErr != nil {
			t.Fatalf("a refused decision removed %q: %v", candidate.InboxPath, statErr)
		}
		if _, rowErr := repo.MemoryCandidateByID(ctx, candidate.CandidateID); rowErr != nil {
			t.Fatalf("a refused decision retired the row for %q: %v", candidate.InboxPath, rowErr)
		}
	}
	if profile := vault.EditableProfile(ctx); profile.Content != "# Profile\n\nWrites Go.\n" {
		t.Fatalf("a refused apply rewrote the profile: %q", profile.Content)
	}
}

// expected_content_hash on a decision is the deprecated spelling of the same
// question, kept only so an older client is not left with no compare-and-set at
// all. It names the candidate file, exactly like expected_candidate_hash, and a
// stale one is refused rather than waved through.
func TestADecisionCarryingOnlyTheDeprecatedTokenIsStillCompareAndSet(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", "The user prefers dark mode.")
	stale := candidate.ContentHash
	editInboxFile(t, vault, candidate.InboxPath, "The user prefers dark mode.", "The user prefers light mode.")

	_, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId:         candidate.CandidateID,
		ExpectedContentHash: stale,
	})
	assertFailedPrecondition(t, err, "promote carrying only the deprecated token")
}
