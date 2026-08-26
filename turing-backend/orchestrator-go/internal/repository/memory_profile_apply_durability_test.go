package repository

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// profileEditCandidate is a proposal to rewrite profile.md, sitting in the
// inbox with its row pending, which is where every apply starts.
func profileEditCandidate(t *testing.T, repo *Repository, sessionID string) MemoryCandidate {
	t.Helper()
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindProfileEdit,
		Title:     "profile",
		Body:      "The user is a beekeeper.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	return candidate
}

func candidateState(t *testing.T, repo *Repository, candidateID string) string {
	t.Helper()
	candidate, err := repo.MemoryCandidateByID(ctx(), candidateID)
	if err != nil {
		t.Fatalf("MemoryCandidateByID: %v", err)
	}
	return candidate.State
}

// The apply has to claim the candidate before it touches profile.md, because
// the claim is the only thing that can be true *before* the write and still be
// true after a crash. A process that died mid-write would otherwise leave a
// pending proposal beside a profile that may already carry its words — and a
// rejection would then delete the proposal as though the user had refused
// something they had in fact accepted.
func TestApplyProfileClaimsTheCandidateBeforeItTouchesTheProfile(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten already.\n")
	candidate := profileEditCandidate(t, repo, sessionID)

	crash := errors.New("the process died")
	repo.memoryProfileApplyBarrier = func(stage string) error {
		if stage == memoryProfileApplyClaimed {
			return crash
		}
		return nil
	}
	_, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash("# Profile\n\nWritten already.\n"),
		Content:             "# Profile\n\nThe user is a beekeeper.\n",
	})
	repo.memoryProfileApplyBarrier = nil
	if !errors.Is(err, crash) {
		t.Fatalf("apply error = %v, want the crash", err)
	}
	if got := readVaultNote(t, vault, memoryfiles.ProfileFileName); got != "# Profile\n\nWritten already.\n" {
		t.Fatalf("profile = %q, want untouched by a claim that never wrote", got)
	}
	if got := candidateState(t, repo, candidate.CandidateID); got != MemoryCandidateStateProfileApplying {
		t.Fatalf("state = %q, want %q", got, MemoryCandidateStateProfileApplying)
	}
	// The claim is what a rejection loses to. Until the outcome is known, the
	// user's "no" cannot be applied to a proposal the system may already have
	// said yes to.
	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, ErrMemoryCandidateInvalidTransition) {
		t.Fatalf("reject error = %v, want the claimed candidate to refuse a decision", err)
	}
	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, ErrMemoryCandidateInvalidTransition) {
		t.Fatalf("promote error = %v, want the claimed candidate to refuse a decision", err)
	}
}

// A claim whose write provably never landed is not a decision the user has to
// live with. The profile still reads exactly as the apply expected it to, so
// nothing was written, and the proposal goes back to pending for them to decide
// again.
func TestReconcileResetsAClaimWhoseWriteClearlyNeverLanded(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten already.\n")
	candidate := profileEditCandidate(t, repo, sessionID)

	repo.memoryProfileApplyBarrier = func(stage string) error {
		if stage == memoryProfileApplyClaimed {
			return errors.New("the process died")
		}
		return nil
	}
	_, _ = repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash("# Profile\n\nWritten already.\n"),
		Content:             "# Profile\n\nThe user is a beekeeper.\n",
	})
	repo.memoryProfileApplyBarrier = nil

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.ProfileAppliesReset != 1 || report.ProfileAppliesFinalized != 0 {
		t.Fatalf("recovery = %+v, want one claim reset and none finalised", report)
	}
	if got := candidateState(t, repo, candidate.CandidateID); got != MemoryCandidateStatePending {
		t.Fatalf("state = %q, want the proposal decidable again", got)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
		t.Fatalf("the proposal left the inbox: %v", err)
	}
	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); err != nil {
		t.Fatalf("rejecting a reset proposal: %v", err)
	}
}

// The seam that matters most: the profile is written and the bookkeeping never
// happens. The words are in the user's document, so a rejection must not be
// able to retire the proposal as though they had refused it — and nothing may
// report a plain failure while the content sits there applied.
func TestACrashBetweenTheWriteAndTheBookkeepingCannotBeRejectedAway(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten already.\n")
	candidate := profileEditCandidate(t, repo, sessionID)

	repo.memoryProfileApplyBarrier = func(stage string) error {
		if stage == memoryProfileApplyWritten {
			return errors.New("the process died")
		}
		return nil
	}
	result, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash("# Profile\n\nWritten already.\n"),
		Content:             "# Profile\n\nThe user is a beekeeper.\n",
	})
	repo.memoryProfileApplyBarrier = nil
	if err != nil {
		t.Fatalf("apply reported a failure over a profile it had already written: %v", err)
	}
	if !result.CleanupPending {
		t.Fatal("apply reported a finished decision while its bookkeeping was still open")
	}
	if got := readVaultNote(t, vault, memoryfiles.ProfileFileName); got != "# Profile\n\nThe user is a beekeeper.\n" {
		t.Fatalf("profile = %q, want the applied document", got)
	}
	if got := candidateState(t, repo, candidate.CandidateID); got != MemoryCandidateStateProfileApplying {
		t.Fatalf("state = %q, want the claim to survive the crash", got)
	}
	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, ErrMemoryCandidateInvalidTransition) {
		t.Fatalf("reject error = %v, want a refusal over an applied profile", err)
	}
	if got := readVaultNote(t, vault, memoryfiles.ProfileFileName); got != "# Profile\n\nThe user is a beekeeper.\n" {
		t.Fatalf("the refused rejection moved the profile: %q", got)
	}
}

// And the other half: the pass that runs at startup finishes what the crash
// left, because the profile reads exactly as the claim said it would.
func TestReconcileFinishesAnApplyThatAlreadyWroteTheProfile(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten already.\n")
	candidate := profileEditCandidate(t, repo, sessionID)

	repo.memoryProfileApplyBarrier = func(stage string) error {
		if stage == memoryProfileApplyWritten {
			return errors.New("the process died")
		}
		return nil
	}
	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash("# Profile\n\nWritten already.\n"),
		Content:             "# Profile\n\nThe user is a beekeeper.\n",
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	repo.memoryProfileApplyBarrier = nil

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.ProfileAppliesFinalized != 1 || report.ProfileAppliesReset != 0 {
		t.Fatalf("recovery = %+v, want one claim finalised and none reset", report)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("candidate row error = %v, want it consumed", err)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the applied proposal is still in the inbox: %v", err)
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("reservations after recovery = %+v", artifacts)
	}
	if got := readVaultNote(t, vault, memoryfiles.ProfileFileName); got != "# Profile\n\nThe user is a beekeeper.\n" {
		t.Fatalf("profile = %q, want recovery to leave the applied document alone", got)
	}
}

// The third outcome, and the one a guess would get wrong: the profile is
// neither what the apply was replacing nor what it wrote. Someone has been in
// the file since. Resetting would hand a rejection back its power over a
// document that may already carry these words; finalising would claim an
// outcome nobody can see. It stays claimed, and the user's own text is not
// touched either way.
func TestReconcileLeavesAClaimAloneWhenTheProfileMovedOnUnderIt(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten already.\n")
	candidate := profileEditCandidate(t, repo, sessionID)

	repo.memoryProfileApplyBarrier = func(stage string) error {
		if stage == memoryProfileApplyWritten {
			return errors.New("the process died")
		}
		return nil
	}
	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash("# Profile\n\nWritten already.\n"),
		Content:             "# Profile\n\nThe user is a beekeeper.\n",
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	repo.memoryProfileApplyBarrier = nil
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nI rewrote this myself.\n")

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.ProfileAppliesFinalized != 0 || report.ProfileAppliesReset != 0 {
		t.Fatalf("recovery = %+v, want a claim it cannot resolve left alone", report)
	}
	if got := candidateState(t, repo, candidate.CandidateID); got != MemoryCandidateStateProfileApplying {
		t.Fatalf("state = %q, want the claim held", got)
	}
	if got := readVaultNote(t, vault, memoryfiles.ProfileFileName); got != "# Profile\n\nI rewrote this myself.\n" {
		t.Fatalf("recovery overwrote the user's own edit: %q", got)
	}
	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, ErrMemoryCandidateInvalidTransition) {
		t.Fatalf("reject error = %v, want a refusal while the outcome is unknown", err)
	}
}

// The write landed and the proposal could not be removed. That is not a failed
// apply: the user's profile says what they accepted. What is unfinished is the
// file, so the row goes — nothing may reject an applied proposal — and the
// reservation stays, because it is what tells the cleaner these bytes are
// Turing's to remove.
func TestAnApplyWhoseInboxCleanupFailsIsStillApplied(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten already.\n")
	candidate := profileEditCandidate(t, repo, sessionID)

	inbox := filepath.Join(vault.Root(), memoryfiles.InboxDirName)
	if err := os.Chmod(inbox, 0o500); err != nil {
		t.Fatalf("seal the inbox: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(inbox, 0o700) })

	result, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash("# Profile\n\nWritten already.\n"),
		Content:             "# Profile\n\nThe user is a beekeeper.\n",
	})
	if err != nil {
		t.Fatalf("apply reported a failure over a profile it had written: %v", err)
	}
	if !result.CleanupPending {
		t.Fatal("apply claimed a finished decision while the proposal was still in the inbox")
	}
	if got := readVaultNote(t, vault, memoryfiles.ProfileFileName); got != "# Profile\n\nThe user is a beekeeper.\n" {
		t.Fatalf("profile = %q, want the applied document", got)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("candidate row error = %v, want an applied proposal to be undecidable", err)
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].VaultPath != candidate.InboxPath {
		t.Fatalf("reservations = %+v, want the file still tracked for the cleaner", artifacts)
	}
}

// The profile compare-and-set is asked before anything is claimed. A result
// composed over a document that has since moved is refused, and the refusal
// leaves a proposal the user can still decide — not a claim nobody took.
func TestApplyRefusesAStaleProfileBeforeItClaimsAnything(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten already.\n")
	candidate := profileEditCandidate(t, repo, sessionID)

	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash("something else"),
		Content:             "# Profile\n\nThe user is a beekeeper.\n",
	}); !errors.Is(err, memoryfiles.ErrStaleContent) {
		t.Fatalf("stale apply error = %v, want ErrStaleContent", err)
	}
	if got := candidateState(t, repo, candidate.CandidateID); got != MemoryCandidateStatePending {
		t.Fatalf("state = %q, want the proposal still the user's to decide", got)
	}
	if got := readVaultNote(t, vault, memoryfiles.ProfileFileName); got != "# Profile\n\nWritten already.\n" {
		t.Fatalf("profile = %q, want untouched", got)
	}
}

// The claim is reachable only through a proposal the *file* declares a profile
// edit. The database cannot ask that question — the row records what Turing
// proposed, and the user may have rewritten it — so the gate is a file read
// under the per-candidate lock, and this is the test that holds it. Neutering
// it would let a belief enter a lifecycle whose whole premise is that the
// user's profile document may already have changed.
func TestABeliefFileCannotEnterTheProfileApplyLifecycle(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten already.\n")
	candidate := pendingBeliefCandidate(t, repo, sessionID)

	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash("# Profile\n\nWritten already.\n"),
		Content:             "# Profile\n\nThe user keeps bees.\n",
	}); !errors.Is(err, memoryfiles.ErrKind) && !errors.Is(err, ErrMemoryCandidateKind) {
		t.Fatalf("apply error = %v, want a refusal on the kind the file declares", err)
	}
	if got := candidateState(t, repo, candidate.CandidateID); got != MemoryCandidateStatePending {
		t.Fatalf("state = %q, want a belief left where it was", got)
	}
	if got := readVaultNote(t, vault, memoryfiles.ProfileFileName); got != "# Profile\n\nWritten already.\n" {
		t.Fatalf("profile = %q, want untouched by a belief", got)
	}
}

// The lifecycle rows an apply leaves are the record that a decision happened,
// and nothing more. Neither the claim, the hand-back nor the finish may carry
// the words of the proposal, the resulting document, or either hash — a
// fingerprint of a user's own profile is still a fact about their profile.
func TestTheApplyLifecycleTrailCarriesNoContentAndNoHash(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten already.\n")
	baseHash := memoryfiles.ContentHash("# Profile\n\nWritten already.\n")
	resultHash := memoryfiles.ContentHash("# Profile\n\nThe user is a beekeeper.\n")

	reset := profileEditCandidate(t, repo, sessionID)
	repo.memoryProfileApplyBarrier = func(stage string) error {
		if stage == memoryProfileApplyClaimed {
			return errors.New("the process died")
		}
		return nil
	}
	_, _ = repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         reset.CandidateID,
		ExpectedContentHash: baseHash,
		Content:             "# Profile\n\nThe user is a beekeeper.\n",
	})
	repo.memoryProfileApplyBarrier = nil
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         reset.CandidateID,
		ExpectedContentHash: baseHash,
		Content:             "# Profile\n\nThe user is a beekeeper.\n",
	}); err != nil {
		t.Fatalf("apply after the hand-back: %v", err)
	}

	trail := auditPayloads(t, database)
	for _, forbidden := range []string{"beekeeper", "Written already", baseHash, resultHash, reset.ContentHash} {
		if strings.Contains(trail, forbidden) {
			t.Fatalf("the apply trail carries %q:\n%s", forbidden, trail)
		}
	}
	if !strings.Contains(trail, memoryProfileApplyResetAction) {
		t.Fatalf("the hand-back left no record of itself:\n%s", trail)
	}
}

// auditPayloads is everything the trail holds, as one string — the shape a
// "this must appear nowhere" assertion needs.
func auditPayloads(t *testing.T, database *db.DB) string {
	t.Helper()
	var builder strings.Builder
	for _, record := range auditRows(t, database) {
		builder.WriteString(record.Action + " " + record.Target + " " + record.Payload + "\n")
	}
	return builder.String()
}
