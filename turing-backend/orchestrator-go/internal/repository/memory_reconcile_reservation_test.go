package repository

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// vaultArtifactRows counts the manifest rows a session still owns, whatever
// state they are in — which is the question "is this file still tracked?" in
// the shape the session cleaner asks it.
func vaultArtifactRows(t *testing.T, repo *Repository, sessionID string) int {
	t.Helper()
	return countRows(t, repo, `SELECT COUNT(*) FROM vault_artifacts WHERE session_id = ?`, sessionID)
}

// A reservation is the only durable record that a session left bytes in the
// user's vault. Releasing one on the strength of a walk that has since been
// contradicted is how a file becomes untracked: the row that named it is gone,
// so no cleaner will ever find it, and the user is left with a claim about
// themselves nothing in the system can withdraw.
//
// Here the walk misses the file and it is back before the sweep decides. The
// candidate row is kept — that much the sweep already got right — and the
// reservation has to be kept with it, or the file the row still describes is
// tracked by nothing.
func TestReconcileKeepsAReservationWhoseFileIsBackUnderTheLock(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.memoryReconcileScanAnchor = now()
	content := readVaultNote(t, vault, candidate.InboxPath)
	removeVaultNote(t, vault, candidate.InboxPath)

	// The walk sees an inbox without the file; the file is back before the
	// sweep looks at either the row or the reservation that tracks it.
	repo.memoryVaultPassBarrier = func() {
		repo.memoryVaultPassBarrier = nil
		repo.memoryOrphanSweepBarrier = func() {
			repo.memoryOrphanSweepBarrier = nil
			writeVaultNote(t, vault, candidate.InboxPath, content)
		}
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.OrphanCandidatesRemoved != 0 {
		t.Fatalf("the pass retired %d candidate(s) whose file was back", report.OrphanCandidatesRemoved)
	}
	if report.ReservationsCleared != 0 {
		t.Fatalf("the pass released %d reservation(s) for a file that is back in the inbox", report.ReservationsCleared)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("the candidate whose file came back lost its row: %v", err)
	}
	if got := vaultArtifactRows(t, repo, sessionID); got != 1 {
		t.Fatalf("vault manifest rows = %d, want the restored file still tracked", got)
	}
	if !candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("the restored proposal is not in the inbox")
	}

	// The point of keeping the row: the session cleaner can still find the
	// file. Deleting the conversation has to take the note with it rather than
	// leaving an untracked claim about the user in their own vault.
	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	if removed != 1 {
		t.Fatalf("the cleaner removed %d file(s), want the restored proposal", removed)
	}
	if candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("the deleted session's note is still in the vault")
	}
	if entries := inboxEntries(t, vault); len(entries) != 0 {
		t.Fatalf("inbox = %v, want nothing left untracked", entries)
	}
}

// A standing apply claim says the user's profile may already carry these words,
// and only recoverProfileApplies may end one. The sweep already refuses to
// retire the row; the reservation is the same fact about the same file, and
// releasing it would strand the bytes the claim is still about.
func TestReconcileKeepsAReservationHeldByAStandingProfileApplyClaim(t *testing.T) {
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
	repo.memoryProfileApplyBarrier = func(stage string) error {
		if stage == memoryProfileApplyWritten {
			return errors.New("the process died")
		}
		return nil
	}
	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:           candidate.CandidateID,
		ExpectedContentHash:   memoryfiles.ContentHash(original),
		Content:               "# Profile\n\nWrites Go.\n\nGoes by Miguel.\n",
		ExpectedCandidateHash: candidate.ContentHash,
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	repo.memoryProfileApplyBarrier = nil
	removeVaultNote(t, vault, candidate.InboxPath)
	// Saved by hand, so recovery can answer for neither side of the write and
	// leaves the claim standing — the state the sweep must not act on.
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten by hand.\n")

	repo.memoryReconcileScanAnchor = now()
	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.OrphanCandidatesRemoved != 0 {
		t.Fatalf("the pass retired %d standing claim(s)", report.OrphanCandidatesRemoved)
	}
	if report.ReservationsCleared != 0 {
		t.Fatalf("the pass released %d reservation(s) held by a standing claim", report.ReservationsCleared)
	}
	standing, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID)
	if err != nil {
		t.Fatalf("the standing claim lost its row: %v", err)
	}
	if standing.State != MemoryCandidateStateProfileApplying {
		t.Fatalf("the standing claim moved to %q", standing.State)
	}
	if got := vaultArtifactRows(t, repo, sessionID); got != 1 {
		t.Fatalf("vault manifest rows = %d, want the claim's file still tracked", got)
	}
}

// The two re-checks are separate questions, and this one isolates the second.
// The file is back while the candidate is examined and gone again by the time
// the reservation is, so nothing about the file keeps the row's manifest entry:
// only the live proposal still naming that path does.
func TestReconcileKeepsAReservationAPendingProposalStillNames(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.memoryReconcileScanAnchor = now()
	content := readVaultNote(t, vault, candidate.InboxPath)
	removeVaultNote(t, vault, candidate.InboxPath)

	repo.memoryVaultPassBarrier = func() {
		repo.memoryVaultPassBarrier = nil
		repo.memoryOrphanSweepBarrier = func() {
			repo.memoryOrphanSweepBarrier = nil
			writeVaultNote(t, vault, candidate.InboxPath, content)
		}
		repo.memoryReservationSweepBarrier = func() {
			repo.memoryReservationSweepBarrier = nil
			removeVaultNote(t, vault, candidate.InboxPath)
		}
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.ReservationsCleared != 0 {
		t.Fatalf("the pass released %d reservation(s) a pending proposal still names", report.ReservationsCleared)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("the pending proposal lost its row: %v", err)
	}
	if got := vaultArtifactRows(t, repo, sessionID); got != 1 {
		t.Fatalf("vault manifest rows = %d, want the pending proposal's path still tracked", got)
	}
}

// The file coming back after its row was already retired is the case nothing
// but the file itself can speak for: the proposal is gone, so no row names the
// path, and only looking again keeps the manifest naming the bytes now sitting
// in the user's inbox. Released here, that file is an untracked claim about the
// user that no cleaner can ever reach.
func TestReconcileKeepsAReservationWhoseFileReturnsAfterItsRowWasRetired(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.memoryReconcileScanAnchor = now()
	content := readVaultNote(t, vault, candidate.InboxPath)
	removeVaultNote(t, vault, candidate.InboxPath)

	// The row is retired — the file really was gone while the sweep held its
	// lock — and the file lands again before the reservation is considered.
	repo.memoryVaultPassBarrier = func() {
		repo.memoryVaultPassBarrier = nil
		repo.memoryReservationSweepBarrier = func() {
			repo.memoryReservationSweepBarrier = nil
			writeVaultNote(t, vault, candidate.InboxPath, content)
		}
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.OrphanCandidatesRemoved != 1 {
		t.Fatalf("orphan candidates removed = %d, want the row whose file was gone retired",
			report.OrphanCandidatesRemoved)
	}
	if report.ReservationsCleared != 0 {
		t.Fatalf("the pass released %d reservation(s) for a file back in the inbox", report.ReservationsCleared)
	}
	if got := vaultArtifactRows(t, repo, sessionID); got != 1 {
		t.Fatalf("vault manifest rows = %d, want the returned file still tracked", got)
	}

	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	if removed != 1 {
		t.Fatalf("the cleaner removed %d file(s), want the returned note", removed)
	}
	if entries := inboxEntries(t, vault); len(entries) != 0 {
		t.Fatalf("inbox = %v, want nothing left untracked", entries)
	}
}

// The other direction, and the reason the sweep exists at all: a reservation
// naming a path nothing holds any more is released, and the release is audited.
func TestReconcileStillReleasesAReservationNothingHolds(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
		t.Fatalf("remove the candidate file: %v", err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.OrphanCandidatesRemoved != 1 || report.ReservationsCleared != 1 {
		t.Fatalf("orphans=%d reservations=%d, want 1 and 1",
			report.OrphanCandidatesRemoved, report.ReservationsCleared)
	}
	if got := vaultArtifactRows(t, repo, sessionID); got != 0 {
		t.Fatalf("vault manifest rows = %d, want the stale reservation released", got)
	}
	if got := countRows(t, repo,
		`SELECT COUNT(*) FROM audit_logs WHERE action = ?`, memoryReservationReleasedAction,
	); got != 1 {
		t.Fatalf("reservation release audit rows = %d, want 1", got)
	}
}
