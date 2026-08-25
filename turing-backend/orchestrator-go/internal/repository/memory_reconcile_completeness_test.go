package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// A pass that could not read an area of the vault has learned nothing about
// what is in it. Every test here is the same sentence from a different angle:
// absence is only evidence of deletion when the walk was able to look.

// blindArea makes one area of the vault unreadable the way a half-synced
// folder, a permissions change or an editor's own bookkeeping can: the
// directory is replaced by a file, after the Vault was opened. It is chosen
// over chmod because it behaves identically for every user the tests run as.
func blindArea(t *testing.T, vault *memoryfiles.Vault, area string) {
	t.Helper()
	full := filepath.Join(vault.Root(), area)
	if err := os.RemoveAll(full); err != nil {
		t.Fatalf("remove %q: %v", area, err)
	}
	if err := os.WriteFile(full, []byte("not a folder"), 0o600); err != nil {
		t.Fatalf("replace %q with a file: %v", area, err)
	}
}

func incompleteAreaReason(t *testing.T, report MemoryIndexReport, area string) string {
	t.Helper()
	for _, issue := range report.IncompleteAreas {
		if issue.Area == area {
			if issue.Reason == "" {
				t.Fatalf("area %q was reported incomplete with no reason", area)
			}
			return issue.Reason
		}
	}
	t.Fatalf("incomplete areas = %+v, want %q named", report.IncompleteAreas, area)
	return ""
}

func requireAreaReportedComplete(t *testing.T, report MemoryIndexReport, area string) {
	t.Helper()
	for _, issue := range report.IncompleteAreas {
		if issue.Area == area {
			t.Fatalf("area %q reported incomplete (%q), want it read whole", area, issue.Reason)
		}
	}
}

func candidateCount(t *testing.T, database *db.DB) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM memory_candidates`).Scan(&count); err != nil {
		t.Fatalf("count candidates: %v", err)
	}
	return count
}

func artifactCount(t *testing.T, database *db.DB) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM vault_artifacts`).Scan(&count); err != nil {
		t.Fatalf("count vault artifacts: %v", err)
	}
	return count
}

// The core of the bug: beliefs/ becomes unreadable, the walk returns no
// beliefs, and the index reads that as "the user deleted every memory they
// have". The row, its evidence and its place in search all have to survive,
// and no audit row may claim a removal that never happened.
func TestUnreadableBeliefsNeverRetireNotes(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, []string{sessionID}, "The user keeps bees."))
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if got := evidenceSessions(t, repo, noteID); len(got) != 1 {
		t.Fatalf("evidence before the failure = %v, want one row", got)
	}
	before := auditActions(t, database)[memoryNoteRemovedAction]

	blindArea(t, vault, memoryfiles.BeliefsDirName)

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("RefreshMemoryIndex over an unreadable beliefs folder: %v", err)
	}
	if report.Removed != 0 {
		t.Fatalf("removed = %d, want nothing retired on the strength of a walk that could not look", report.Removed)
	}
	if reason := incompleteAreaReason(t, report, string(memoryfiles.AreaBeliefs)); !strings.Contains(reason, memoryfiles.BeliefsDirName) {
		t.Fatalf("incomplete reason = %q, want it to name the folder", reason)
	}
	if _, found := noteRowFor(t, repo, noteID); !found {
		t.Fatalf("an unreadable beliefs folder deleted the note row")
	}
	if got := evidenceSessions(t, repo, noteID); len(got) != 1 || got[0] != sessionID {
		t.Fatalf("evidence = %v, want it untouched", got)
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("search after the failed walk = %+v, want the note still there", hits)
	}
	if after := auditActions(t, database)[memoryNoteRemovedAction]; after != before {
		t.Fatalf("removal audits went from %d to %d; a removal was recorded that never happened", before, after)
	}
}

// The same refusal through the errno an unreadable folder actually produces in
// the field, so the guard cannot be an artefact of one failure mode.
func TestBeliefsThatCannotBeOpenedNeverRetireNotes(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which is never refused by directory permissions")
	}
	repo, vault, database := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	beliefs := filepath.Join(vault.Root(), memoryfiles.BeliefsDirName)
	if err := os.Chmod(beliefs, 0o000); err != nil {
		t.Fatalf("close beliefs: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(beliefs, 0o700) })

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	if report.Removed != 0 {
		t.Fatalf("removed = %d, want 0", report.Removed)
	}
	incompleteAreaReason(t, report, string(memoryfiles.AreaBeliefs))
	if _, found := noteRowFor(t, repo, noteID); !found {
		t.Fatalf("a beliefs folder that could not be opened deleted the note row")
	}
	if got := auditActions(t, database)[memoryNoteRemovedAction]; got != 0 {
		t.Fatalf("removal audits = %d, want none", got)
	}
}

// The inbox half of the same bug: a candidate row and the reservation that
// tracks its file are retired when the file is gone, and an inbox the walk
// could not read is not a file that is gone.
func TestUnreadableInboxNeverSweepsCandidatesOrReservations(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "bees",
		Body:      "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	blindArea(t, vault, memoryfiles.InboxDirName)

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault over an unreadable inbox: %v", err)
	}
	if report.OrphanCandidatesRemoved != 0 || report.ReservationsCleared != 0 {
		t.Fatalf(
			"orphans=%d reservations=%d, want nothing swept on the strength of a walk that could not look",
			report.OrphanCandidatesRemoved, report.ReservationsCleared,
		)
	}
	if reason := incompleteAreaReason(t, report.Index, string(memoryfiles.AreaInbox)); !strings.Contains(reason, memoryfiles.InboxDirName) {
		t.Fatalf("incomplete reason = %q, want it to name the folder", reason)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("the candidate row was retired by a walk that could not read the inbox: %v", err)
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("reservations = %+v, want the one that tracks the candidate file", artifacts)
	}
	audits := auditActions(t, database)
	if audits[memoryCandidateOrphanedAction] != 0 || audits[memoryReservationReleasedAction] != 0 {
		t.Fatalf("sweep audits = %+v, want none recorded for a sweep that did not happen", audits)
	}
}

// The vault root going away takes both areas with it, so a pass over it may
// retire nothing at all.
func TestVaultRootGoneAfterOpenRetiresNothing(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "bees",
		Body:      "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	if err := os.RemoveAll(vault.Root()); err != nil {
		t.Fatalf("remove the vault root: %v", err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault over a vanished root: %v", err)
	}
	if report.Index.Removed != 0 || report.OrphanCandidatesRemoved != 0 || report.ReservationsCleared != 0 {
		t.Fatalf("a vanished vault root retired something: %+v", report)
	}
	for _, area := range []string{
		memoryVaultRootArea,
		string(memoryfiles.AreaBeliefs),
		string(memoryfiles.AreaInbox),
	} {
		incompleteAreaReason(t, report.Index, area)
	}
	if _, found := noteRowFor(t, repo, noteID); !found {
		t.Fatalf("a vanished vault root deleted the note row")
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("a vanished vault root retired the candidate: %v", err)
	}
	if got := candidateCount(t, database); got != 1 {
		t.Fatalf("candidate rows = %d, want 1", got)
	}
	if got := artifactCount(t, database); got != 1 {
		t.Fatalf("vault artifact rows = %d, want 1", got)
	}
	audits := auditActions(t, database)
	if audits[memoryNoteRemovedAction] != 0 ||
		audits[memoryCandidateOrphanedAction] != 0 ||
		audits[memoryReservationReleasedAction] != 0 {
		t.Fatalf("audits = %+v, want no removal recorded", audits)
	}
}

// The guard is per area, not per pass. An unreadable beliefs folder must not
// stop the inbox — which the walk did read, whole — from being reconciled,
// and it must not stop the notes that were readable from being indexed either.
func TestAnUnreadableBeliefsFolderStillLetsTheInboxBeSwept(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "chickens",
		Body:      "The user keeps chickens.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	// The candidate's file really is gone, and the inbox really was read.
	if err := os.Remove(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
		t.Fatalf("remove the candidate file: %v", err)
	}
	blindArea(t, vault, memoryfiles.BeliefsDirName)

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.OrphanCandidatesRemoved != 1 || report.ReservationsCleared != 1 {
		t.Fatalf(
			"orphans=%d reservations=%d, want the readable inbox reconciled anyway",
			report.OrphanCandidatesRemoved, report.ReservationsCleared,
		)
	}
	requireAreaReportedComplete(t, report.Index, string(memoryfiles.AreaInbox))
	incompleteAreaReason(t, report.Index, string(memoryfiles.AreaBeliefs))
	if report.Index.Removed != 0 {
		t.Fatalf("removed = %d, want the unreadable area to retire nothing", report.Index.Removed)
	}
	if _, found := noteRowFor(t, repo, noteID); !found {
		t.Fatalf("the note row was retired while its area was unreadable")
	}
}

// The mirror: an unreadable inbox must not stop a belief the user really did
// delete from leaving the index.
func TestAnUnreadableInboxStillLetsDeletedBeliefsLeaveTheIndex(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "chickens",
		Body:      "The user keeps chickens.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	if err := os.Remove(filepath.Join(vault.Root(), "beliefs", "note.md")); err != nil {
		t.Fatalf("remove the belief: %v", err)
	}
	blindArea(t, vault, memoryfiles.InboxDirName)

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.Index.Removed != 1 {
		t.Fatalf("removed = %d, want the deleted belief retired from the readable area", report.Index.Removed)
	}
	if _, found := noteRowFor(t, repo, noteID); found {
		t.Fatalf("the deleted belief kept its row")
	}
	requireAreaReportedComplete(t, report.Index, string(memoryfiles.AreaBeliefs))
	incompleteAreaReason(t, report.Index, string(memoryfiles.AreaInbox))
	if report.OrphanCandidatesRemoved != 0 || report.ReservationsCleared != 0 {
		t.Fatalf("the unreadable inbox was swept anyway: %+v", report)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("the candidate row was retired by a walk that could not read the inbox: %v", err)
	}
}

// A vault that was read whole still reports nothing as incomplete, so the
// guard cannot be satisfied by reporting everything unreadable all the time.
func TestAHealthyVaultReportsNoIncompleteAreas(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	if len(report.IncompleteAreas) != 0 {
		t.Fatalf("incomplete areas = %+v, want none for a vault that was read whole", report.IncompleteAreas)
	}
	if report.Indexed != 1 {
		t.Fatalf("indexed = %d, want 1", report.Indexed)
	}
}
