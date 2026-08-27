package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The write is the decision. If profile.md now holds the document the user
// reviewed, the answer is yes — and an apply that reported a failure because it
// could not afterwards delete the proposal would tell them their edit did not
// happen while their own file says it did. What it says instead is: applied,
// and there is tidying outstanding.
func TestApplyMemoryProfileSaysWhenOnlyTheTidyingIsOutstanding(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	_, sessionID := newRun(t, repo, callCtx)
	writeVaultDocument(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWrites Go.\n")
	candidate := profileEditProposal(t, callCtx, repo, sessionID)
	reviewed := "# Profile\n\nWrites Go.\n\nThe user is a beekeeper.\n"

	inbox := filepath.Join(vault.Root(), memoryfiles.InboxDirName)
	if err := os.Chmod(inbox, 0o500); err != nil {
		t.Fatalf("seal the inbox: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(inbox, 0o700) })

	applied, err := service.ApplyMemoryProfile(callCtx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId:         candidate.CandidateID,
		Content:             reviewed,
		ExpectedContentHash: memoryfiles.ContentHash("# Profile\n\nWrites Go.\n"),
	})
	if err != nil {
		t.Fatalf("apply reported a failure over a profile it had written: %v", err)
	}
	if applied.GetProfile().GetContent() != reviewed {
		t.Fatalf("applied profile = %q, want the document the user reviewed", applied.GetProfile().GetContent())
	}
	if !applied.GetCleanupPending() {
		t.Fatal("the response claimed a finished decision while the proposal was still in the inbox")
	}
	if _, err := repo.MemoryCandidateByID(callCtx, candidate.CandidateID); err == nil {
		t.Fatal("an applied proposal was left decidable")
	}
}

// A crash between the write and the bookkeeping leaves the claim standing. The
// page has to be able to say so: the proposal is not pending, it is not
// decided, and every decision RPC refuses it. Rendering it as a proposal with
// buttons would offer the user a choice the server will not take, and leaving
// it off the page would hide a decision of theirs that is still in flight.
func TestListMemoryStateShowsAClaimedApplyAndOffersNoDecision(t *testing.T) {
	service, repo, vault, database, callCtx := newMemoryServiceStack(
		t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), nil)
	_, sessionID := newRun(t, repo, callCtx)
	writeVaultDocument(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWrites Go.\n")
	candidate := profileEditProposal(t, callCtx, repo, sessionID)
	claimApply(t, callCtx, database, candidate.CandidateID)
	// And then the user opened profile.md and wrote in it themselves, so the
	// document answers for neither side of the claim. Nothing may conclude
	// what the interrupted write did, which is precisely when the claim has to
	// stay standing and visible.
	writeVaultDocument(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nI rewrote this myself.\n")

	state, err := service.ListMemoryState(callCtx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := candidateByID(t, state, candidate.CandidateID)
	if listed.GetState() != turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PROFILE_APPLYING {
		t.Fatalf("state = %v, want the claim said out loud", listed.GetState())
	}

	rejected, err := service.RejectMemoryCandidate(callCtx, &turingv1.RejectMemoryCandidateRequest{
		CandidateId: candidate.CandidateID,
	})
	if err == nil {
		t.Fatalf("a rejection won over an apply that may already have landed: %v", rejected)
	}
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("reject code = %v, want FailedPrecondition", got)
	}
	if _, err := service.PromoteMemoryCandidate(callCtx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId: candidate.CandidateID,
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("promote error = %v, want FailedPrecondition", err)
	}
	if got := readVaultDocument(t, vault, memoryfiles.ProfileFileName); got != "# Profile\n\nI rewrote this myself.\n" {
		t.Fatalf("a refused decision moved the profile: %q", got)
	}
}

func profileEditProposal(
	t *testing.T,
	ctx context.Context,
	repo *repository.Repository,
	sessionID string,
) repository.MemoryCandidate {
	t.Helper()
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      repository.MemoryCandidateKindProfileEdit,
		Title:     "profile",
		Body:      "The user is a beekeeper.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	return candidate
}

// claimApply is the row a process leaves behind when it dies mid-apply: the
// candidate claimed, carrying the hash of the document it was replacing and the
// one it was going to write.
func claimApply(t *testing.T, ctx context.Context, database *db.DB, candidateID string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		UPDATE memory_candidates
		SET state = 'profile_applying', decided_at = ?, apply_base_hash = ?, apply_result_hash = ?
		WHERE id = ?
	`, "2026-08-26T00:00:00.000000000Z",
		memoryfiles.ContentHash("# Profile\n\nWrites Go.\n"),
		memoryfiles.ContentHash("# Profile\n\nWrites Go.\n\nThe user is a beekeeper.\n"),
		candidateID); err != nil {
		t.Fatalf("stage the claim: %v", err)
	}
}

func readVaultDocument(t *testing.T, vault *memoryfiles.Vault, relPath string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(vault.Root(), filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("read %q: %v", relPath, err)
	}
	return string(content)
}
