package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// A candidate belongs to the conversation that produced it, and deleting that
// conversation withdraws everything it was the only support for.
//
// A decision about the candidate and the start of that withdrawal used to be
// two unrelated pieces of work over the same rows. The decision reads the row,
// checks the file, writes the vault and then commits; the withdrawal flips the
// session to 'deleting' and cascades. Nothing made either wait for the other,
// so a promotion could turn an unreviewed claim into a belief the user had just
// asked to be rid of — one whose evidence the cascade then removed, leaving a
// note that reads as grounded in a conversation nobody can look at. Worse, an
// apply could write the user's *profile* out of a session that was already
// being deleted: the proposal is gone, the row is gone, and the words are in a
// pinned document they will carry into every future run.
//
// So a decision now holds the source session's own lock for its whole mutation
// window and re-reads the session under it. The order is candidate, then
// session — always, in every path — and the withdrawal takes the session lock
// only for the transaction that flips the state, releasing it long before any
// cleaner or reconcile touches the vault.

func requireSessionDeletionBegan(t *testing.T, repo *Repository, sessionID string) {
	t.Helper()
	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("begin session deletion: %v", err)
	}
}

func seedDecidableCandidate(t *testing.T, repo *Repository, kind string) (string, MemoryCandidate) {
	t.Helper()
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: kind,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return sessionID, candidate
}

// Deletion first. Every decision has to fail, and it has to fail before it has
// touched a file or the profile — a refusal that has already moved the bytes is
// not a refusal.
func TestPromoteRefusesACandidateWhoseSessionIsBeingDeleted(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedDecidableCandidate(t, repo, MemoryCandidateKindBelief)
	requireSessionDeletionBegan(t, repo, sessionID)

	_, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID: candidate.CandidateID,
	})
	if !errors.Is(err, ErrSessionDeleting) {
		t.Fatalf("promote out of a deleting session = %v, want ErrSessionDeleting", err)
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "dark", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("a promotion out of a deleting session wrote a belief: %+v", hits)
	}
	if !candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("a refused promotion moved the proposal out of the inbox")
	}
	if left := beliefEntries(t, vault); len(left) != 0 {
		t.Fatalf("a refused promotion left %v in beliefs/", left)
	}
	// Nothing for a later pass to finish, either. A promotion that moved the
	// file and then failed is exactly the shape reconcile heals into a note, so
	// the refusal has to leave the vault with nothing that looks like one.
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("reconcile after a refused promotion: %v", err)
	}
	hits, err = repo.SearchMemoryNotes(ctx(), "dark", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("reconcile healed a refused promotion into a belief: %+v", hits)
	}
}

// The one that reaches furthest into the user's own documents. A profile apply
// out of a deleting session must never write profile.md: the proposal and every
// row that describes it are about to be withdrawn, and a pinned document is
// carried into every future run.
func TestApplyProfileRefusesACandidateWhoseSessionIsBeingDeleted(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	original := "# Profile\n\nWrites Go.\n"
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, original)
	sessionID, candidate := seedDecidableCandidate(t, repo, MemoryCandidateKindProfileEdit)
	requireSessionDeletionBegan(t, repo, sessionID)

	_, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash(original),
		Content:             "# Profile\n\nWrites Go.\n\nGoes by Miguel.\n",
	})
	if !errors.Is(err, ErrSessionDeleting) {
		t.Fatalf("apply out of a deleting session = %v, want ErrSessionDeleting", err)
	}
	if profile := vault.EditableProfile(ctx()); profile.Content != original {
		t.Fatalf("an apply out of a deleting session rewrote the profile: %q", profile.Content)
	}
	if !candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("a refused apply removed the proposal")
	}
}

// A rejection is the least harmful of the three and still may not run. The
// candidate row and the reservation that tracks its file are the session's, and
// the withdrawal is what is entitled to retire them; a rejection landing beside
// it would untrack the file out from under the cleaner that is about to look
// for it.
func TestRejectRefusesACandidateWhoseSessionIsBeingDeleted(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedDecidableCandidate(t, repo, MemoryCandidateKindBelief)
	requireSessionDeletionBegan(t, repo, sessionID)

	err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID: candidate.CandidateID,
	})
	if !errors.Is(err, ErrSessionDeleting) {
		t.Fatalf("reject out of a deleting session = %v, want ErrSessionDeleting", err)
	}
	if !candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("a rejection out of a deleting session deleted the proposal")
	}
	tracked, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracked) != 1 || tracked[0].VaultPath != candidate.InboxPath {
		t.Fatalf("a refused rejection untracked the file the cleaner still has to remove: %+v", tracked)
	}
}

// The other ordering, and the one that must not deadlock or be refused. A
// decision that got there first owns the session until it commits; the
// withdrawal waits, and then does what a withdrawal does — the belief the
// promotion just wrote loses its last evidence and is marked withdrawn.
func TestSessionDeletionWaitsForADecisionAlreadyUnderway(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedDecidableCandidate(t, repo, MemoryCandidateKindBelief)

	began := make(chan error, 1)
	parked := make(chan struct{})
	repo.memoryDecisionFileBarrier = func() {
		repo.memoryDecisionFileBarrier = nil
		go func() {
			close(parked)
			_, err := repo.BeginSessionDeletion(ctx(), sessionID)
			began <- err
		}()
		<-parked
		// The withdrawal is now trying to take a lock this decision holds. If
		// it could pass, it would flip the session under a promotion that has
		// already read it — so the assertion is that it has *not* answered by
		// the time this decision is ready to commit.
		select {
		case err := <-began:
			t.Errorf("the withdrawal ran straight through a decision in flight: %v", err)
		case <-time.After(150 * time.Millisecond):
		}
	}

	note, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID: candidate.CandidateID,
	})
	if err != nil {
		t.Fatalf("a promotion that got there first was refused: %v", err)
	}
	select {
	case err := <-began:
		if err != nil {
			t.Fatalf("the withdrawal failed once the decision let go: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the withdrawal never completed; the two locks deadlocked")
	}
	if candidateFileExists(t, vault, candidate.InboxPath) {
		t.Fatal("the promoted proposal is still in the inbox")
	}
	if _, err := repo.AdvanceSessionDeletion(ctx(), sessionID, nil); err != nil {
		t.Fatalf("advance the withdrawal: %v", err)
	}
	// Normal withdrawal semantics: the note is kept and marked withdrawn,
	// because the only conversation supporting it is gone.
	stored, err := repo.MemoryNoteByID(ctx(), note.NoteID)
	if err != nil {
		t.Fatalf("the promoted note vanished: %v", err)
	}
	if stored.Status != MemoryNoteStatusWithdrawn {
		t.Fatalf("the note the deleted conversation supported is %q, want %q", stored.Status, MemoryNoteStatusWithdrawn)
	}
}

// Both orderings, run for real and repeatedly, with nothing staged. Whichever
// wins, the invariants are the same: no belief and no profile may exist on the
// authority of a session whose withdrawal already began, and neither call may
// hang waiting for the other.
func TestDecisionsAndSessionDeletionRacingLeaveNoHalfDecidedMemory(t *testing.T) {
	for attempt := range 12 {
		repo, vault, _ := newMemoryTestRepo(t)
		original := "# Profile\n\nWrites Go.\n"
		writeVaultNote(t, vault, memoryfiles.ProfileFileName, original)
		sessionID := newMemoryTestSession(t, repo)
		belief, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
			SessionID: sessionID, Kind: MemoryCandidateKindBelief,
			Title: "Dark mode", Body: "The user prefers dark mode.",
		})
		if err != nil {
			t.Fatal(err)
		}
		edit, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
			SessionID: sessionID, Kind: MemoryCandidateKindProfileEdit,
			Title: "Call me Miguel", Body: "The user goes by Miguel.",
		})
		if err != nil {
			t.Fatal(err)
		}
		reject, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
			SessionID: sessionID, Kind: MemoryCandidateKindBelief,
			Title: "Light mode", Body: "The user tolerates light mode.",
		})
		if err != nil {
			t.Fatal(err)
		}

		var waiting sync.WaitGroup
		var promoteErr, applyErr, rejectErr, deleteErr error
		start := make(chan struct{})
		waiting.Add(4)
		go func() {
			defer waiting.Done()
			<-start
			_, promoteErr = repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: belief.CandidateID})
		}()
		go func() {
			defer waiting.Done()
			<-start
			_, applyErr = repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
				CandidateID:         edit.CandidateID,
				ExpectedContentHash: memoryfiles.ContentHash(original),
				Content:             "# Profile\n\nWrites Go.\n\nGoes by Miguel.\n",
			})
		}()
		go func() {
			defer waiting.Done()
			<-start
			rejectErr = repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: reject.CandidateID})
		}()
		go func() {
			defer waiting.Done()
			<-start
			_, deleteErr = repo.BeginSessionDeletion(ctx(), sessionID)
		}()
		close(start)
		finished := make(chan struct{})
		go func() {
			waiting.Wait()
			close(finished)
		}()
		select {
		case <-finished:
		case <-time.After(20 * time.Second):
			t.Fatalf("attempt %d: a decision and a withdrawal deadlocked", attempt)
		}

		if deleteErr != nil {
			t.Fatalf("attempt %d: the withdrawal was refused: %v", attempt, deleteErr)
		}
		state, err := sessionDeletionStateFor(ctx(), repo, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if state != "deleting" {
			t.Fatalf("attempt %d: the session is %q after a withdrawal began", attempt, state)
		}
		// The apply is the one that can never be half-true: either it was
		// refused and profile.md still reads as it did, or it won the lock
		// outright and the whole document is the reviewed one.
		profile := vault.EditableProfile(ctx())
		switch {
		case applyErr != nil:
			if profile.Content != original {
				t.Fatalf("attempt %d: a refused apply rewrote the profile: %q", attempt, profile.Content)
			}
		default:
			if profile.Content == original {
				t.Fatalf("attempt %d: an apply reported success without writing the profile", attempt)
			}
		}
		// A promotion that was refused leaves no belief behind, and one that
		// won leaves exactly the note it says it wrote.
		hits, err := repo.SearchMemoryNotes(ctx(), "dark", 10)
		if err != nil {
			t.Fatal(err)
		}
		if promoteErr != nil && len(hits) != 0 {
			t.Fatalf("attempt %d: a refused promotion wrote a belief: %+v", attempt, hits)
		}
		if promoteErr == nil && len(hits) != 1 {
			t.Fatalf("attempt %d: a promotion that reported success wrote %d beliefs", attempt, len(hits))
		}
		// A refused rejection leaves the proposal exactly where the withdrawal
		// expects to find it.
		if rejectErr != nil && !candidateFileExists(t, vault, reject.InboxPath) {
			t.Fatalf("attempt %d: a refused rejection deleted the proposal", attempt)
		}
	}
}

// The last line of the gate, tested on its own because it is the one that
// should never fire.
//
// Every decision holds the source session's lock and has already read it as
// active, so this predicate is unreachable through the ordinary door — which is
// exactly why it has to be exercised deliberately. A guard nobody can make fail
// is indistinguishable from one that does nothing, and the next reader deletes
// it. Here the lock is bypassed outright: the session is flipped underneath a
// candidate that has already been read, and the transition SQL is asked to
// record the decision anyway.
func TestTheTransitionSQLRefusesADecisionOverADeletingSession(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedDecidableCandidate(t, repo, MemoryCandidateKindBelief)
	if _, err := repo.db.ExecContext(ctx(), `
		UPDATE sessions SET deletion_state = 'deleting' WHERE id = ?
	`, sessionID); err != nil {
		t.Fatal(err)
	}

	tx, err := repo.db.BeginTx(ctx(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	err = consumeMemoryCandidateTx(ctx(), tx, candidate, MemoryCandidateStateRejected, true, true)
	if !errors.Is(err, ErrMemoryCandidateInvalidTransition) {
		t.Fatalf("consuming a candidate of a deleting session = %v, want ErrMemoryCandidateInvalidTransition", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("the refused decision retired the row anyway: %v", err)
	}

	// Crash recovery is the one caller that may finish without the gate: the
	// words it is recording are already in the user's own profile.
	recoveryTx, err := repo.db.BeginTx(ctx(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = recoveryTx.Rollback() }()
	if err := consumeMemoryCandidateTx(ctx(), recoveryTx, candidate, MemoryCandidateStatePromoted, true, false); err != nil {
		t.Fatalf("crash recovery could not finish an apply that already landed: %v", err)
	}
}

// The same, for the mutation an apply turns on. A claim is the row saying the
// user's profile may already carry these words, and it may not be taken on the
// authority of a conversation that is being withdrawn.
func TestTheProfileClaimSQLRefusesACandidateOfADeletingSession(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedDecidableCandidate(t, repo, MemoryCandidateKindProfileEdit)
	if _, err := repo.db.ExecContext(ctx(), `
		UPDATE sessions SET deletion_state = 'deleting' WHERE id = ?
	`, sessionID); err != nil {
		t.Fatal(err)
	}

	err := repo.claimProfileApply(ctx(), candidate, "", memoryfiles.ContentHash("# Profile\n"))
	if !errors.Is(err, ErrMemoryCandidateInvalidTransition) {
		t.Fatalf("claiming an apply over a deleting session = %v, want ErrMemoryCandidateInvalidTransition", err)
	}
	row, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID)
	if err != nil {
		t.Fatalf("the refused claim retired the row: %v", err)
	}
	if row.State != MemoryCandidateStatePending {
		t.Fatalf("the refused claim left the proposal in %q", row.State)
	}
}

func sessionDeletionStateFor(ctx context.Context, repo *Repository, sessionID string) (string, error) {
	var state string
	err := repo.db.QueryRowContext(ctx, `SELECT deletion_state FROM sessions WHERE id = ?`, sessionID).Scan(&state)
	return state, err
}

func beliefEntries(t *testing.T, vault *memoryfiles.Vault) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(vault.Root(), memoryfiles.BeliefsDirName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read beliefs: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
