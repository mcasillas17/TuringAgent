package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// writeVaultFile is the user saving over a file in their own editor. It is how
// these tests turn a moved proposal into one the walk can no longer classify.
func writeVaultFile(t *testing.T, vault *memoryfiles.Vault, relPath string, content string) {
	t.Helper()
	target := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("prepare %q: %v", relPath, err)
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		t.Fatalf("write %q: %v", relPath, err)
	}
}

// seedProfileEditCandidateForSession is seedProfileEditCandidate plus the
// session id, which is what the manifest rows are counted by.
func seedProfileEditCandidateForSession(t *testing.T, repo *Repository, title string) (MemoryCandidate, string) {
	t.Helper()
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindProfileEdit,
		Title:     title,
		Body:      "The user goes by " + title + ".",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	return candidate, sessionID
}

// unparseableNote is a frontmatter block no YAML parser will read, so the walk
// learns neither the identity nor the kind of the file carrying it.
const unparseableNote = "---\nid: \"unterminated\nkind: profile_edit\n---\n\nThe user goes by Miguel.\n"

// assertMisplacedProposalRetained is the whole of what the user is owed when a
// proposal ends up under beliefs/ in a shape the walk cannot classify: the row
// that makes it decidable, the manifest entry that makes it cleanable, the file
// itself, and no place in searchable memory.
func assertMisplacedProposalRetained(
	t *testing.T,
	repo *Repository,
	vault *memoryfiles.Vault,
	report MemoryReconcileReport,
	candidate MemoryCandidate,
	sessionID string,
	moved string,
) {
	t.Helper()
	if report.OrphanCandidatesRemoved != 0 {
		t.Fatalf("reconcile retired %d candidate(s) whose file is still in the vault", report.OrphanCandidatesRemoved)
	}
	if report.ReservationsCleared != 0 {
		t.Fatalf("reconcile released %d reservation(s) for a file that is still in the vault", report.ReservationsCleared)
	}
	stored, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID)
	if err != nil {
		t.Fatalf("the candidate for a misplaced proposal was retired: %v", err)
	}
	if stored.State != MemoryCandidateStatePending {
		t.Fatalf("candidate state = %q, want it still pending so the user can act on it", stored.State)
	}
	if rows := vaultArtifactRows(t, repo, sessionID); rows != 1 {
		t.Fatalf("vault_artifacts rows = %d, want the file the user can see to still be tracked", rows)
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(moved))); err != nil {
		t.Fatalf("the moved file is gone: %v", err)
	}
	if report.Index.Indexed != 0 {
		t.Fatalf("reconcile indexed %d note(s) from a file it could not read", report.Index.Indexed)
	}
	notes, err := repo.SearchMemoryNotes(ctx(), "Miguel", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("a misplaced proposal became searchable memory: %+v", notes)
	}
	said := false
	for _, issue := range report.Index.Errors {
		if issue.RelPath == moved {
			said = true
		}
	}
	if !said {
		t.Fatalf("reconcile said nothing the user can act on about %q: %+v", moved, report.Index.Errors)
	}
}

// A profile edit the user moved into beliefs/ and then broke is the same file
// in the same place as one they only moved. The retention that keeps it
// decidable reads the kind out of the frontmatter — and a frontmatter that no
// longer parses has no kind to read, so the sweep saw a candidate row whose
// inbox path was empty, called it an orphan, deleted the row and released the
// reservation beside it.
//
// What that leaves is the worst outcome the manifest exists to prevent: a claim
// about the user, in the user's own vault, that nothing in the system names, no
// cleaner can find and no decision can retract.
func TestReconcileKeepsTheCandidateOfAMalformedProfileEditMovedIntoBeliefs(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	candidate, sessionID := seedProfileEditCandidateForSession(t, repo, "Miguel")
	moved := memoryfiles.BeliefsDirName + "/" + filepath.Base(candidate.InboxPath)
	moveVaultFile(t, vault, candidate.InboxPath, moved)
	writeVaultFile(t, vault, moved, unparseableNote)

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	assertMisplacedProposalRetained(t, repo, vault, report, candidate, sessionID, moved)
}

// The other way a moved proposal loses its kind: the file is readable, and the
// read refuses it by length before anything parses it. The walk reports a note
// with no identity and no kind for exactly the same reason, so the retention
// has to cover it for exactly the same reason.
func TestReconcileKeepsTheCandidateOfAnOverLimitProfileEditMovedIntoBeliefs(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	candidate, sessionID := seedProfileEditCandidateForSession(t, repo, "Miguel")
	moved := memoryfiles.BeliefsDirName + "/" + filepath.Base(candidate.InboxPath)
	moveVaultFile(t, vault, candidate.InboxPath, moved)
	writeVaultFile(t, vault, moved, "---\nkind: profile_edit\n---\n\nThe user goes by Miguel.\n"+
		strings.Repeat("x", memoryfiles.MaxNoteBytes+1))

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	assertMisplacedProposalRetained(t, repo, vault, report, candidate, sessionID, moved)
}

// Retention is not a licence to keep everything. A file the walk cannot
// classify is correlated back to the proposal that produced it by the identity
// this server minted into its name — so a second proposal, whose file really is
// gone, is still retired on the same pass.
//
// Without this the fix would be indistinguishable from "never sweep a profile
// edit again", which leaves every decided proposal's row behind forever.
func TestReconcileStillRetiresAnUnrelatedProfileEditBesideAMalformedOne(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	misplaced, _ := seedProfileEditCandidateForSession(t, repo, "Miguel")
	moved := memoryfiles.BeliefsDirName + "/" + filepath.Base(misplaced.InboxPath)
	moveVaultFile(t, vault, misplaced.InboxPath, moved)
	writeVaultFile(t, vault, moved, unparseableNote)

	decided, _ := seedProfileEditCandidateForSession(t, repo, "Ana")
	if err := os.Remove(filepath.Join(vault.Root(), filepath.FromSlash(decided.InboxPath))); err != nil {
		t.Fatalf("remove %q: %v", decided.InboxPath, err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.OrphanCandidatesRemoved != 1 {
		t.Fatalf(
			"OrphanCandidatesRemoved = %d, want only the proposal whose file is really gone retired",
			report.OrphanCandidatesRemoved,
		)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), misplaced.CandidateID); err != nil {
		t.Fatalf("the candidate for the misplaced proposal was retired: %v", err)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), decided.CandidateID); err == nil {
		t.Fatal("the candidate whose file is really gone was kept; the sweep stopped sweeping")
	}
}

// A file the walk cannot classify and cannot correlate is the ambiguous case,
// and ambiguity resolves towards the user keeping their proposals. A renamed
// file carries no minted identity, so nothing here can say which proposal it
// is — or that it is not one — and every profile edit still on the books is
// retained until the file can be read.
//
// Keeping a row too long costs one more pass. Consuming one costs the user a
// proposal in their vault that nothing says was ever proposed.
func TestReconcileKeepsEveryProfileEditWhenAnUnclassifiableFileCannotBeCorrelated(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	candidate, sessionID := seedProfileEditCandidateForSession(t, repo, "Miguel")
	renamed := memoryfiles.BeliefsDirName + "/about-me.md"
	moveVaultFile(t, vault, candidate.InboxPath, renamed)
	writeVaultFile(t, vault, renamed, unparseableNote)

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	assertMisplacedProposalRetained(t, repo, vault, report, candidate, sessionID, renamed)
}

// A belief candidate is not covered by any of this. Moving one into beliefs/ is
// the promotion the primitive would have performed, and the sweep finishing it
// is the crash-heal the plan asks for — even when the moved file is one the
// walk cannot read, because the row it retires describes the inbox, not the
// file it can no longer classify.
func TestReconcileStillSweepsABeliefCandidateBesideAMalformedFile(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	moved := memoryfiles.BeliefsDirName + "/" + filepath.Base(candidate.InboxPath)
	moveVaultFile(t, vault, candidate.InboxPath, moved)
	writeVaultFile(t, vault, moved, unparseableNote)

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.OrphanCandidatesRemoved != 1 {
		t.Fatalf("OrphanCandidatesRemoved = %d, want the moved belief's candidate retired", report.OrphanCandidatesRemoved)
	}
}

// The retention is a holding pattern, not a new lifecycle. Once the user does
// what the page told them to — move the file back to inbox/ and fix it — the
// proposal is readable at the path its row names again, and the ordinary
// decision goes through on the very next pass.
func TestAMisplacedProfileEditDecidesNormallyOnceItIsMovedBack(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	candidate, sessionID := seedProfileEditCandidateForSession(t, repo, "Miguel")
	moved := memoryfiles.BeliefsDirName + "/" + filepath.Base(candidate.InboxPath)
	moveVaultFile(t, vault, candidate.InboxPath, moved)
	writeVaultFile(t, vault, moved, unparseableNote)
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}

	moveVaultFile(t, vault, moved, candidate.InboxPath)
	writeVaultFile(t, vault, candidate.InboxPath,
		"---\nid: "+memoryfiles.NoteIDFromInboxRelPath(candidate.InboxPath)+
			"\nkind: profile_edit\ntitle: Miguel\n---\n\nThe user goes by Miguel.\n")

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.OrphanCandidatesRemoved != 0 {
		t.Fatalf("the returned proposal was retired: %+v", report)
	}
	restored, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID)
	if err != nil {
		t.Fatalf("MemoryCandidateByID: %v", err)
	}
	note, err := vault.ReadInboxNote(ctx(), restored.InboxPath)
	if err != nil {
		t.Fatalf("the returned proposal is not readable where its row says it is: %v", err)
	}
	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{
		CandidateID:           restored.CandidateID,
		ExpectedCandidateHash: note.ContentHash,
	}); err != nil {
		t.Fatalf("RejectMemoryCandidate: %v", err)
	}
	if rows := vaultArtifactRows(t, repo, sessionID); rows != 0 {
		t.Fatalf("vault_artifacts rows = %d after the decision, want the reservation released", rows)
	}
}
