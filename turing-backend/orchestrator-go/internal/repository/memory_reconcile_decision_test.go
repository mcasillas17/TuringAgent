package repository

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// A promotion is two halves: the file leaves inbox/, then one transaction
// records it. Between them the vault says one thing and the database says
// another, and that is exactly what the reconcile sweep reads as an orphan — a
// candidate row whose inbox file is gone.
//
// It is not an orphan. It is a decision in flight, and consuming it deletes the
// row the promotion is about to update: the promotion then fails after the
// user's note has already moved, and the user is told their acceptance did not
// happen while their vault says it did.
//
// So the sweep takes the same per-candidate lock every decision holds, and
// re-reads the row and the file under it. Parked here, the promotion is holding
// that lock, so the sweep waits for the decision rather than stepping into the
// middle of it.
func TestReconcileWaitsForAPromotionInFlightRatherThanConsumingIt(t *testing.T) {
	for attempt := range 4 {
		repo, _, _ := newMemoryTestRepo(t)
		sessionID := newMemoryTestSession(t, repo)
		candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
			SessionID: sessionID, Kind: MemoryCandidateKindBelief,
			Title: "Dark mode", Body: "The user prefers dark mode.",
		})
		if err != nil {
			t.Fatal(err)
		}
		// The sweep judges by what the walk found and by an anchor, so the row
		// is placed firmly in scope: without the lock this is a row the pass
		// would delete.
		repo.memoryReconcileScanAnchor = now()

		reached := make(chan struct{})
		release := make(chan struct{})
		repo.memoryPromotionBarrier = func() error {
			close(reached)
			<-release
			return nil
		}

		var running sync.WaitGroup
		var note MemoryNote
		var promoteErr error
		running.Add(1)
		go func() {
			defer running.Done()
			note, promoteErr = repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{
				CandidateID:           candidate.CandidateID,
				ExpectedCandidateHash: candidate.ContentHash,
			})
		}()
		<-reached

		// The page the user is looking at, drawn while the decision is parked:
		// ListMemoryState reconciles and then scans, through this repository.
		var reconcileErr, scanErr error
		var report MemoryReconcileReport
		running.Add(1)
		go func() {
			defer running.Done()
			report, reconcileErr = repo.ReconcileMemoryVault(ctx())
			if reconcileErr == nil {
				_, scanErr = repo.ScanMemoryVault(ctx())
			}
		}()
		// Long enough for the pass to have walked the vault and reached the
		// sweep, where it must be waiting on the decision rather than deciding.
		time.Sleep(50 * time.Millisecond)
		close(release)
		running.Wait()

		if promoteErr != nil {
			t.Fatalf("attempt %d: the promotion lost to a reconcile pass: %v", attempt, promoteErr)
		}
		if reconcileErr != nil {
			t.Fatalf("attempt %d: reconcile: %v", attempt, reconcileErr)
		}
		if scanErr != nil {
			t.Fatalf("attempt %d: scan: %v", attempt, scanErr)
		}
		if report.OrphanCandidatesRemoved != 0 {
			t.Fatalf("attempt %d: the pass consumed %d decision(s) in flight", attempt, report.OrphanCandidatesRemoved)
		}
		hits, err := repo.SearchMemoryNotes(ctx(), "dark", 10)
		if err != nil {
			t.Fatal(err)
		}
		if note.NoteID == "" || len(hits) != 1 {
			t.Fatalf("attempt %d: the accepted belief is not in memory: %d hit(s)", attempt, len(hits))
		}
		if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
			t.Fatalf("attempt %d: the promoted candidate still has a row: %v", attempt, err)
		}
		// And the pass that follows the decision still converges: nothing is
		// left for it to find.
		repo.memoryPromotionBarrier = nil
		after, err := repo.ReconcileMemoryVault(ctx())
		if err != nil {
			t.Fatalf("attempt %d: reconcile after the decision: %v", attempt, err)
		}
		if after.OrphanCandidatesRemoved != 0 {
			t.Fatalf("attempt %d: the following pass removed %d candidate(s)", attempt, after.OrphanCandidatesRemoved)
		}
	}
}

// The same window for a rejection: the file is unlinked before the transaction
// that retires the row, and a pass that stepped in between would consume the
// decision the user is in the middle of making.
func TestReconcileWaitsForARejectionInFlightRatherThanConsumingIt(t *testing.T) {
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

	// Parked in the rejection's own window: the pre-check has read the file and
	// the primitive has not deleted it yet, so the row is live and the decision
	// holds its lock.
	reached := make(chan struct{})
	release := make(chan struct{})
	repo.memoryDecisionFileBarrier = func() {
		repo.memoryDecisionFileBarrier = nil
		close(reached)
		<-release
	}

	var running sync.WaitGroup
	var rejectErr error
	running.Add(1)
	go func() {
		defer running.Done()
		rejectErr = repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{
			CandidateID:           candidate.CandidateID,
			ExpectedCandidateHash: candidate.ContentHash,
		})
	}()
	<-reached

	// The file is still in the inbox at this point, so the sweep would not call
	// it an orphan; what it must not do is delete it out from under the
	// decision once the decision proceeds. Removing the file here puts the row
	// in exactly the state the sweep acts on.
	removeVaultNote(t, vault, candidate.InboxPath)

	var reconcileErr error
	var report MemoryReconcileReport
	running.Add(1)
	go func() {
		defer running.Done()
		report, reconcileErr = repo.ReconcileMemoryVault(ctx())
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)
	running.Wait()

	if rejectErr != nil {
		t.Fatalf("the rejection lost to a reconcile pass: %v", rejectErr)
	}
	if reconcileErr != nil {
		t.Fatalf("reconcile: %v", reconcileErr)
	}
	if report.OrphanCandidatesRemoved != 0 {
		t.Fatalf("the pass consumed %d decision(s) in flight", report.OrphanCandidatesRemoved)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("the rejected candidate still has a row: %v", err)
	}
}

// A row whose file really is gone, with nobody deciding about it, is still
// retired: the lock is coordination, not a reprieve.
func TestReconcileStillRetiresACandidateNobodyIsDecidingAbout(t *testing.T) {
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
	removeVaultNote(t, vault, candidate.InboxPath)

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.OrphanCandidatesRemoved != 1 {
		t.Fatalf("orphan candidates removed = %d, want 1", report.OrphanCandidatesRemoved)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("the orphaned candidate still has a row: %v", err)
	}
}

// A candidate whose file came back between the walk and the sweep's own look is
// not an orphan either. The walk's view is a snapshot; the row is deleted on
// what is true under the lock, not on what was true when the crawl went past.
func TestReconcileDoesNotRetireACandidateWhoseFileIsBackUnderTheLock(t *testing.T) {
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

	// The pass walks an inbox without the file, and the file is put back before
	// the sweep looks at the row.
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
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("the candidate whose file came back lost its row: %v", err)
	}
	if !candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("the restored proposal is not in the inbox")
	}
}

// A claim outlives the lock that took it, so that an apply caught by a crash is
// still resolvable afterwards. The file being gone is what a claim whose write
// landed looks like, and only recoverProfileApplies may end one: a sweep that
// retired it would delete the row that says the user's profile may already
// carry these words.
func TestReconcileDoesNotRetireAProfileApplyClaimWhoseFileIsGone(t *testing.T) {
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
	// The apply claims the row, writes the profile, and dies before it can
	// retire the proposal — but after the proposal's file has gone.
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

	claimed, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID)
	if err != nil {
		t.Fatalf("the claim was not left standing: %v", err)
	}
	if claimed.State != MemoryCandidateStateProfileApplying {
		t.Fatalf("the proposal is in %q, not a standing claim", claimed.State)
	}

	// The user saves profile.md by hand, so the document now reads as neither
	// of the claim's hashes and recovery refuses to guess: the claim is left
	// standing, which is exactly the state the sweep must not act on.
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten by hand.\n")

	repo.memoryReconcileScanAnchor = now()
	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.OrphanCandidatesRemoved != 0 {
		t.Fatalf("the pass retired %d standing claim(s)", report.OrphanCandidatesRemoved)
	}
	still, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID)
	if err != nil {
		t.Fatalf("the standing claim lost its row: %v", err)
	}
	if still.State != MemoryCandidateStateProfileApplying {
		t.Fatalf("the standing claim moved to %q", still.State)
	}
}

// "The file is gone" is the only thing that makes a row stale. A note the user
// broke while editing it is a file they can still see in their vault, and
// deleting the row for it would leave a claim about them that nothing in the
// system names — with no way left to reject it.
func TestReconcileDoesNotRetireACandidateWhosePresentFileWillNotParse(t *testing.T) {
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
	removeVaultNote(t, vault, candidate.InboxPath)

	// The walk sees no file; by the time the sweep holds the lock the user has
	// saved a half-finished edit back into it.
	repo.memoryVaultPassBarrier = func() {
		repo.memoryVaultPassBarrier = nil
		repo.memoryOrphanSweepBarrier = func() {
			repo.memoryOrphanSweepBarrier = nil
			writeVaultNote(t, vault, candidate.InboxPath, "---\nrefs: [broken\n---\n\nHalf an edit.\n")
		}
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.OrphanCandidatesRemoved != 0 {
		t.Fatalf("the pass retired %d candidate(s) whose file was there but unreadable", report.OrphanCandidatesRemoved)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("the candidate whose file was unreadable lost its row: %v", err)
	}
}

// A row for a proposal the user was never going to decide — its conversation
// was deleted — is still retired once its file has gone. The lock is
// coordination with decisions in flight, not a reprieve for every other state.
func TestReconcileStillRetiresAWithdrawnCandidateWhoseFileIsGone(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.WithdrawMemoryCandidate(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	repo.memoryReconcileScanAnchor = now()
	removeVaultNote(t, vault, candidate.InboxPath)

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.OrphanCandidatesRemoved != 1 {
		t.Fatalf("orphan candidates removed = %d, want 1", report.OrphanCandidatesRemoved)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("the withdrawn candidate still has a row: %v", err)
	}
}
