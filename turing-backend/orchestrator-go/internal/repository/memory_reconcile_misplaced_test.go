package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// moveVaultFile is the user dragging a file in their own editor, which is the
// only way a candidate ever ends up under beliefs/ without going through the
// promotion primitive.
func moveVaultFile(t *testing.T, vault *memoryfiles.Vault, from string, to string) {
	t.Helper()
	source := filepath.Join(vault.Root(), filepath.FromSlash(from))
	target := filepath.Join(vault.Root(), filepath.FromSlash(to))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("prepare %q: %v", to, err)
	}
	if err := os.Rename(source, target); err != nil {
		t.Fatalf("move %q to %q: %v", from, to, err)
	}
}

func seedProfileEditCandidate(t *testing.T, repo *Repository) MemoryCandidate {
	t.Helper()
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindProfileEdit,
		Title:     "Call me Miguel",
		Body:      "The user goes by Miguel.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	return candidate
}

// A profile edit is a proposal to rewrite the user's own description of
// themselves. It is not a belief, and PromoteToBeliefs refuses to make it one —
// but the user can move the file by hand, and reconcile then walks a beliefs/
// folder holding a file the promotion primitive would never have put there.
//
// Indexing it would make a proposal the user never accepted answerable as a
// remembered fact, which is the one outcome the kind gate exists to prevent.
func TestReconcileRefusesToIndexAProfileEditMovedIntoBeliefs(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	candidate := seedProfileEditCandidate(t, repo)
	moved := memoryfiles.BeliefsDirName + "/" + filepath.Base(candidate.InboxPath)
	moveVaultFile(t, vault, candidate.InboxPath, moved)

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.Index.Indexed != 0 {
		t.Fatalf("reconcile indexed %d note(s); a profile edit under beliefs/ is not a belief", report.Index.Indexed)
	}
	notes, err := repo.SearchMemoryNotes(ctx(), "Miguel", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("a misplaced profile edit is searchable memory: %+v", notes)
	}
	said := false
	for _, issue := range report.Index.Errors {
		if issue.RelPath == moved && strings.Contains(strings.ToLower(issue.Reason), "profile") {
			said = true
		}
	}
	if !said {
		t.Fatalf("reconcile said nothing the user can act on about %q: %+v", moved, report.Index.Errors)
	}
}

// The candidate row and the reservation both track a file that is still in the
// vault. Sweeping them because inbox/ no longer holds the path would read the
// move as a finished promotion — leaving the user with a file they can neither
// apply nor reject, and no row saying it was ever proposed.
func TestReconcileKeepsTheCandidateOfAProfileEditMovedIntoBeliefs(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	candidate := seedProfileEditCandidate(t, repo)
	moved := memoryfiles.BeliefsDirName + "/" + filepath.Base(candidate.InboxPath)
	moveVaultFile(t, vault, candidate.InboxPath, moved)

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.OrphanCandidatesRemoved != 0 {
		t.Fatalf("reconcile retired %d candidate(s) whose file is still in the vault", report.OrphanCandidatesRemoved)
	}
	if report.ReservationsCleared != 0 {
		t.Fatalf("reconcile released %d reservation(s) for a file that is still in the vault", report.ReservationsCleared)
	}
	stored, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID)
	if err != nil {
		t.Fatalf("the candidate for a misplaced profile edit was retired: %v", err)
	}
	if stored.State != MemoryCandidateStatePending {
		t.Fatalf("candidate state = %q, want it still pending so the user can act on it", stored.State)
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(moved))); err != nil {
		t.Fatalf("the moved file is gone: %v", err)
	}
}

// A belief candidate the user moved by hand is a different story: that is the
// promotion the primitive would have performed, and the sweep finishing it is
// the crash-heal the plan asks for. The refusal must be about profile edits
// specifically, not about moved files in general.
func TestReconcileStillSweepsABeliefCandidateMovedIntoBeliefs(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Prefers dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	moveVaultFile(t, vault, candidate.InboxPath, memoryfiles.BeliefsDirName+"/"+filepath.Base(candidate.InboxPath))

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.OrphanCandidatesRemoved != 1 {
		t.Fatalf("OrphanCandidatesRemoved = %d, want the moved belief's candidate retired", report.OrphanCandidatesRemoved)
	}
}

// The retention above is built from what the walk found under beliefs/. A walk
// that could not read all of beliefs/ has not found it — and concluding "no
// misplaced profile edit" from a folder it could not open is the same mistake
// as concluding "the note was deleted" from one, which the belief removal
// already refuses to make.
//
// Until beliefs/ can be read in full, a profile edit's candidate and
// reservation are kept: the file may be sitting in the part nobody could list,
// and sweeping them would leave the user with a proposal they cannot apply,
// reject, or find a record of.
func TestReconcileKeepsAProfileEditCandidateWhileBeliefsCannotBeReadInFull(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	candidate := seedProfileEditCandidate(t, repo)
	unreadable := filepath.Join(vault.Root(), memoryfiles.BeliefsDirName, "locked")
	if err := os.MkdirAll(unreadable, 0o700); err != nil {
		t.Fatalf("prepare %q: %v", unreadable, err)
	}
	moveVaultFile(t, vault, candidate.InboxPath,
		memoryfiles.BeliefsDirName+"/locked/"+filepath.Base(candidate.InboxPath))
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("seal %q: %v", unreadable, err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root reads an unreadable directory, so this vault cannot be made partial")
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if len(report.Index.IncompleteAreas) == 0 {
		t.Fatal("the sealed folder did not make beliefs/ incomplete; the case was not reproduced")
	}
	if report.OrphanCandidatesRemoved != 0 || report.ReservationsCleared != 0 {
		t.Fatalf(
			"reconcile retired %d candidate(s) and %d reservation(s) on a vault it could not read in full",
			report.OrphanCandidatesRemoved, report.ReservationsCleared,
		)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("the candidate for a proposal in an unreadable folder was retired: %v", err)
	}
}
