package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

func writeVaultNote(t *testing.T, vault *memoryfiles.Vault, relPath string, content string) {
	t.Helper()
	full := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("prepare %q: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write %q: %v", relPath, err)
	}
}

func readVaultNote(t *testing.T, vault *memoryfiles.Vault, relPath string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(vault.Root(), filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("read %q: %v", relPath, err)
	}
	return string(content)
}

func newTestNoteID(t *testing.T) string {
	t.Helper()
	noteID, err := memoryfiles.NewNoteID()
	if err != nil {
		t.Fatalf("mint note id: %v", err)
	}
	return noteID
}

func managedBelief(noteID string, refs []string, body string) string {
	quoted := make([]string, 0, len(refs))
	for _, ref := range refs {
		quoted = append(quoted, `"`+ref+`"`)
	}
	return "---\nid: \"" + noteID + "\"\nkind: \"belief\"\ntitle: \"a belief\"\nmanaged: true\n" +
		"refs: [" + strings.Join(quoted, ", ") + "]\n---\n\n" + body + "\n"
}

func noteRowFor(t *testing.T, repo *Repository, noteID string) (MemoryNote, bool) {
	t.Helper()
	note, err := repo.MemoryNoteByID(ctx(), noteID)
	if err != nil {
		if err == ErrMemoryNoteNotFound {
			return MemoryNote{}, false
		}
		t.Fatalf("MemoryNoteByID: %v", err)
	}
	return note, true
}

func evidenceSessions(t *testing.T, repo *Repository, noteID string) []string {
	t.Helper()
	sessions, err := repo.MemoryNoteEvidenceSessions(ctx(), noteID)
	if err != nil {
		t.Fatalf("MemoryNoteEvidenceSessions: %v", err)
	}
	return sessions
}

// A read is a read. The index refresh may learn that a note carries no
// identity, but it must never mint one, because a memory system that rewrites
// the user's file the first time it looks at it is not one they can trust.
func TestRefreshMemoryIndexNeverWritesFiles(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	handwritten := "# Written by hand\n\nNo frontmatter at all.\n"
	writeVaultNote(t, vault, "beliefs/handwritten.md", handwritten)

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	if got := readVaultNote(t, vault, "beliefs/handwritten.md"); got != handwritten {
		t.Fatalf("refresh rewrote the note:\nbefore %q\nafter  %q", handwritten, got)
	}
	if len(report.AwaitingIdentity) != 1 || report.AwaitingIdentity[0] != "beliefs/handwritten.md" {
		t.Fatalf("awaiting identity = %v, want the handwritten note", report.AwaitingIdentity)
	}
	if report.Indexed != 0 {
		t.Fatalf("indexed = %d, want a note with no identity left unindexed", report.Indexed)
	}
}

// The file-writing pass is the one allowed to adopt a note the user wrote. It
// gives the file an identity, keeps the prose exactly as written, and from
// then on the note is part of the index the user can search.
func TestReconcileAdoptsAUserWrittenNote(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	writeVaultNote(t, vault, "beliefs/handwritten.md", "# Written by hand\n\nThe user keeps bees.\n")

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.IdentitiesAssigned != 1 {
		t.Fatalf("identities assigned = %d, want 1", report.IdentitiesAssigned)
	}
	content := readVaultNote(t, vault, "beliefs/handwritten.md")
	if !strings.Contains(content, "The user keeps bees.") {
		t.Fatalf("adoption lost the user's prose: %q", content)
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 1 || hits[0].Path != "beliefs/handwritten.md" {
		t.Fatalf("adopted note is not searchable: %+v", hits)
	}
	if hits[0].Status != MemoryNoteStatusUnmanaged {
		t.Fatalf("status = %q, want an adopted hand-written note to stay unmanaged", hits[0].Status)
	}
}

// A note is its identity, not its filename. Renaming it in Obsidian keeps the
// row, the evidence hanging off it, and its place in search.
func TestRenameInsideBeliefsKeepsIdentityEvidenceAndSearch(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/first.md", managedBelief(noteID, []string{sessionID}, "The user keeps bees."))

	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if got := evidenceSessions(t, repo, noteID); len(got) != 1 || got[0] != sessionID {
		t.Fatalf("evidence after adoption = %v, want %q", got, sessionID)
	}

	if err := os.Rename(
		filepath.Join(vault.Root(), "beliefs", "first.md"),
		filepath.Join(vault.Root(), "beliefs", "renamed.md"),
	); err != nil {
		t.Fatalf("rename note: %v", err)
	}
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}

	note, found := noteRowFor(t, repo, noteID)
	if !found {
		t.Fatalf("renaming the file dropped the note row")
	}
	if note.Path != "beliefs/renamed.md" {
		t.Fatalf("path = %q, want the new name", note.Path)
	}
	if got := evidenceSessions(t, repo, noteID); len(got) != 1 || got[0] != sessionID {
		t.Fatalf("evidence after rename = %v, want it preserved", got)
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 1 || hits[0].Path != "beliefs/renamed.md" {
		t.Fatalf("search after rename = %+v", hits)
	}
}

// Editing a note in Obsidian changes what it means. Search has to follow.
func TestEditedNoteBecomesSearchableAndStopsMatchingTheOldText(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}

	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps chickens."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex after the edit: %v", err)
	}

	hits, err := repo.SearchMemoryNotes(ctx(), "chickens", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("edited text is not searchable: %+v", hits)
	}
	stale, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes for the old text: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("search still matches text the vault no longer contains: %+v", stale)
	}
}

// The file is the memory. Deleting it in Obsidian retires the projection with
// it, rather than leaving search answering from a copy the user deleted.
func TestDeletedNoteLeavesTheIndex(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	if err := os.Remove(filepath.Join(vault.Root(), "beliefs", "note.md")); err != nil {
		t.Fatalf("remove note: %v", err)
	}
	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("RefreshMemoryIndex after deletion: %v", err)
	}
	if report.Removed != 1 {
		t.Fatalf("removed = %d, want 1", report.Removed)
	}
	if _, found := noteRowFor(t, repo, noteID); found {
		t.Fatalf("the row outlived the file")
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("deleted note is still searchable: %+v", hits)
	}
}

// Two files claiming one identity is a question only the user can settle. Both
// stay visible and neither is indexed, and an existing row is not quietly
// repointed at whichever file the walk happened to reach first.
func TestDuplicateIdentitiesAreVisibleAndUnindexed(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/original.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	writeVaultNote(t, vault, "beliefs/copy.md", managedBelief(noteID, nil, "The user keeps aardvarks."))

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("RefreshMemoryIndex with a duplicate: %v", err)
	}
	if len(report.DuplicateNoteIDs) != 1 || report.DuplicateNoteIDs[0] != noteID {
		t.Fatalf("duplicate ids = %v, want %q", report.DuplicateNoteIDs, noteID)
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "aardvarks", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("a duplicated identity reached the index: %+v", hits)
	}
	note, found := noteRowFor(t, repo, noteID)
	if !found || note.Path != "beliefs/original.md" {
		t.Fatalf("existing row = (%+v, %v), want the original left alone", note, found)
	}
}

// A note whose frontmatter cannot be read is named, not swallowed, and its
// contents stay out of the index until the user fixes it.
func TestParseErrorsAreVisibleAndUnindexed(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	writeVaultNote(t, vault, "beliefs/broken.md", "---\nkind: \"gossip\"\n---\n\nThe user keeps aardvarks.\n")

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	if len(report.Errors) != 1 || report.Errors[0].RelPath != "beliefs/broken.md" {
		t.Fatalf("errors = %+v, want the broken note named", report.Errors)
	}
	if report.Errors[0].Reason == "" {
		t.Fatalf("the broken note was reported with no reason")
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "aardvarks", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("an unparseable note reached the index: %+v", hits)
	}
}

// Evidence has one direction. The sidecar is what the user's deletions act on,
// so a file still listing a session that has been deleted is rewritten from
// the sidecar — never the other way round.
func TestStaleFrontmatterCannotResurrectDeletedEvidence(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, []string{sessionID}, "The user keeps bees."))
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if got := evidenceSessions(t, repo, noteID); len(got) != 1 {
		t.Fatalf("evidence after adoption = %v, want one row", got)
	}

	if err := repo.DeleteSession(ctx(), sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if got := evidenceSessions(t, repo, noteID); len(got) != 0 {
		t.Fatalf("evidence survived the session deletion: %v", got)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault after the deletion: %v", err)
	}
	if report.RefsRewritten != 1 {
		t.Fatalf("refs rewritten = %d, want the file caught up with the sidecar", report.RefsRewritten)
	}
	content := readVaultNote(t, vault, "beliefs/note.md")
	if strings.Contains(content, sessionID) {
		t.Fatalf("the file still cites a deleted conversation")
	}
	if got := evidenceSessions(t, repo, noteID); len(got) != 0 {
		t.Fatalf("stale frontmatter resurrected evidence: %v", got)
	}
	note, found := noteRowFor(t, repo, noteID)
	if !found || note.Status != MemoryNoteStatusWithdrawn {
		t.Fatalf("note = (%+v, %v), want it marked withdrawn", note, found)
	}
	var rows int
	if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM memory_evidence`).Scan(&rows); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if rows != 0 {
		t.Fatalf("evidence rows = %d, want 0", rows)
	}
	// A withdrawn note is not active memory, so it leaves search behind too.
	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("a withdrawn note is still searchable: %+v", hits)
	}
	// Running it again must not undo the withdrawal or rewrite anything more.
	second, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("second ReconcileMemoryVault: %v", err)
	}
	if second.RefsRewritten != 0 {
		t.Fatalf("a converged vault rewrote %d notes", second.RefsRewritten)
	}
}

// The window a promotion cannot close: the file has already moved into
// beliefs/, and the transaction that was going to record it never committed.
// Reconcile is what closes it — the note is created from the file, the
// candidate that no longer has an inbox entry is removed, and the reservation
// that tracked it is released.
func TestReconcileHealsAPromotionThatCrashedAfterTheFileMoved(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID:    sessionID,
		Kind:         MemoryCandidateKindBelief,
		Title:        "bees",
		Body:         "The user keeps bees.",
		EvidenceRefs: []string{sessionID},
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	// The file half of a promotion, with no database half at all.
	promoted, err := vault.PromoteToBeliefs(ctx(), memoryfiles.PromoteToBeliefsRequest{
		SourceRelPath: candidate.InboxPath,
		Mode:          memoryfiles.PromoteManagedCandidate,
		Kind:          memoryfiles.KindBelief,
	})
	if err != nil {
		t.Fatalf("PromoteToBeliefs: %v", err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.NotesHealed != 1 {
		t.Fatalf("healed = %d, want 1", report.NotesHealed)
	}
	if report.OrphanCandidatesRemoved != 1 {
		t.Fatalf("orphan candidates removed = %d, want 1", report.OrphanCandidatesRemoved)
	}
	if report.ReservationsCleared != 1 {
		t.Fatalf("reservations cleared = %d, want 1", report.ReservationsCleared)
	}

	note, found := noteRowFor(t, repo, promoted.NoteID)
	if !found {
		t.Fatalf("the promoted belief was not healed into the index")
	}
	if note.Path != promoted.RelPath || note.Status != MemoryNoteStatusManaged {
		t.Fatalf("healed note = %+v", note)
	}
	if !strings.Contains(note.Content, "The user keeps bees.") {
		t.Fatalf("healed note lost the user's content: %q", note.Content)
	}
	if got := evidenceSessions(t, repo, promoted.NoteID); len(got) != 1 || got[0] != sessionID {
		t.Fatalf("healed evidence = %v, want %q", got, sessionID)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err == nil {
		t.Fatalf("the orphan candidate row survived")
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("reservations after healing = %+v, want none", artifacts)
	}

	// Deleting the conversation afterwards withdraws the citation, not the
	// belief: the note was accepted into memory and is no longer session state.
	if err := repo.DeleteSession(ctx(), sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, found := noteRowFor(t, repo, promoted.NoteID); !found {
		t.Fatalf("deleting the source conversation deleted the promoted belief")
	}
	if got := evidenceSessions(t, repo, promoted.NoteID); len(got) != 0 {
		t.Fatalf("evidence survived its session: %v", got)
	}
}

// The same crash, but the conversation is deleted before anyone heals it. The
// missing citations cannot become evidence rows — there is no session to point
// at — so the note is healed as withdrawn instead of failing on a foreign key.
func TestHealAfterTheSourceSessionIsAlreadyGoneWithdrawsTheNote(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, []string{sessionID}, "The user keeps bees."))
	if err := repo.DeleteSession(ctx(), sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.NotesHealed != 1 {
		t.Fatalf("healed = %d, want 1", report.NotesHealed)
	}
	note, found := noteRowFor(t, repo, noteID)
	if !found {
		t.Fatalf("the note was not healed")
	}
	if note.Status != MemoryNoteStatusWithdrawn {
		t.Fatalf("status = %q, want withdrawn", note.Status)
	}
	if got := evidenceSessions(t, repo, noteID); len(got) != 0 {
		t.Fatalf("evidence = %v, want none for a session that no longer exists", got)
	}
	if content := readVaultNote(t, vault, "beliefs/note.md"); strings.Contains(content, sessionID) {
		t.Fatalf("the file still cites a conversation that is gone")
	}
}

// A candidate row describes an inbox entry. When the entry is gone and no
// decision was recorded, the row is the only thing left claiming the user has
// something to review, and it goes — with the reservation that tracked it.
func TestReconcileRemovesOrphanCandidatesAndTheirReservations(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
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
	if err := os.Remove(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
		t.Fatalf("remove the candidate file: %v", err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.OrphanCandidatesRemoved != 1 || report.ReservationsCleared != 1 {
		t.Fatalf("orphans=%d reservations=%d, want 1 and 1", report.OrphanCandidatesRemoved, report.ReservationsCleared)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err == nil {
		t.Fatalf("the orphan candidate row survived")
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("reservations = %+v, want none", artifacts)
	}
}

// A candidate created while a pass is already walking the vault must not be
// mistaken for an orphan: the walk simply happened before the file existed.
func TestReconcileLeavesCandidatesCreatedAfterTheScanAlone(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	// Anchored before anything this candidate writes, so both its reservation
	// and its row are newer than the walk that is about to run.
	repo.memoryReconcileScanAnchor = now()
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "bees",
		Body:      "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	// Hide the file from the walk without touching the row, which is what a
	// candidate written a moment after the walk looks like from here.
	hidden := filepath.Join(vault.Root(), "hidden.md")
	if err := os.Rename(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath)), hidden); err != nil {
		t.Fatalf("hide the candidate file: %v", err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.OrphanCandidatesRemoved != 0 || report.ReservationsCleared != 0 {
		t.Fatalf("a candidate newer than the scan was cleaned up: %+v", report)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("MemoryCandidateByID: %v", err)
	}
}

// A file the user dropped into the inbox themselves is theirs. It is reported
// so the client can offer to accept it, and nothing about it is assumed.
func TestReconcileReportsUnmanagedInboxDrafts(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	writeVaultNote(t, vault, "inbox/mine.md", "Just some notes I jotted down.\n")

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if len(report.Index.UnmanagedInboxDrafts) != 1 || report.Index.UnmanagedInboxDrafts[0] != "inbox/mine.md" {
		t.Fatalf("unmanaged drafts = %v", report.Index.UnmanagedInboxDrafts)
	}
	if report.IdentitiesAssigned != 0 {
		t.Fatalf("reconcile adopted an inbox draft; only beliefs are adopted")
	}
	if content := readVaultNote(t, vault, "inbox/mine.md"); content != "Just some notes I jotted down.\n" {
		t.Fatalf("reconcile rewrote a user's inbox draft: %q", content)
	}
	var notes int
	if hits, err := repo.SearchMemoryNotes(ctx(), "jotted", 10); err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	} else {
		notes = len(hits)
	}
	if notes != 0 {
		t.Fatalf("an inbox draft reached the searchable index")
	}
}

func TestMemoryMethodsRefuseWithoutAVault(t *testing.T) {
	repo := New(openTestDB(t))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != ErrMemoryVaultUnavailable {
		t.Fatalf("RefreshMemoryIndex error = %v, want ErrMemoryVaultUnavailable", err)
	}
	if _, err := repo.ReconcileMemoryVault(ctx()); err != ErrMemoryVaultUnavailable {
		t.Fatalf("ReconcileMemoryVault error = %v, want ErrMemoryVaultUnavailable", err)
	}
	if _, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{}); err != ErrMemoryVaultUnavailable {
		t.Fatalf("CreateMemoryCandidate error = %v, want ErrMemoryVaultUnavailable", err)
	}
	if _, err := repo.ReadMemoryBelief(ctx(), "id"); err != ErrMemoryVaultUnavailable {
		t.Fatalf("ReadMemoryBelief error = %v, want ErrMemoryVaultUnavailable", err)
	}
}

// A candidate file with no candidate row is a creation that crashed between
// the write and its transaction. It is named so the user is not left guessing,
// it is never indexed, and it is never deleted on a guess: the reservation
// still tracks it, so the session cleaner can remove it later.
func TestReconcileReportsManagedInboxNotesWithNoCandidateRow(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate := pendingBeliefCandidate(t, repo, sessionID, nil)
	if _, err := database.ExecContext(ctx(), `DELETE FROM memory_candidates WHERE id = ?`, candidate.CandidateID); err != nil {
		t.Fatalf("drop the candidate row: %v", err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if len(report.Index.OrphanInboxNotes) != 1 || report.Index.OrphanInboxNotes[0] != candidate.InboxPath {
		t.Fatalf("orphan inbox notes = %v, want %q", report.Index.OrphanInboxNotes, candidate.InboxPath)
	}
	if len(report.Index.UnmanagedInboxDrafts) != 0 {
		t.Fatalf("a Turing-written candidate was reported as a user draft: %v", report.Index.UnmanagedInboxDrafts)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
		t.Fatalf("reconcile deleted an inbox note it only meant to report: %v", err)
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("an inbox note reached the index: %+v", hits)
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want the reservation still tracking the file", artifacts)
	}
}

// A reservation whose write never happened names a file that does not exist.
// Reconcile has to tolerate that rather than treat it as work: the creation may
// still be in flight, and clearing the row would leave the bytes that land a
// moment later with nothing naming them.
func TestReconcileToleratesAReservationWithNoFile(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	artifact, err := repo.ReserveVaultArtifact(ctx(), ReserveVaultArtifactInput{
		SessionID: sessionID,
		VaultPath: "inbox/never-written.md",
	})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.ReservationsCleared != 0 {
		t.Fatalf("reservations cleared = %d, want an in-flight reservation left alone", report.ReservationsCleared)
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].ArtifactID != artifact.ArtifactID {
		t.Fatalf("artifacts = %+v, want the reservation preserved", artifacts)
	}
}

// A reservation is taken before the file exists, so its own age cannot decide
// whether the walk should have seen that file. Only the moment the write was
// confirmed can: a reservation finalized after the walk started describes a
// file the walk ran too early to find, and sweeping it would leave those bytes
// with nothing in the manifest naming them.
func TestReconcileKeepsAReservationFinalizedAfterTheScanStarted(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	artifact, err := repo.ReserveVaultArtifact(ctx(), ReserveVaultArtifactInput{
		SessionID: sessionID,
		VaultPath: "inbox/written-late.md",
	})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}
	// The walk starts after the reservation and before the write is confirmed.
	repo.memoryReconcileScanAnchor = now()
	if _, err := repo.FinalizeVaultArtifact(ctx(), artifact.ArtifactID, sessionID); err != nil {
		t.Fatalf("FinalizeVaultArtifact: %v", err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.ReservationsCleared != 0 {
		t.Fatalf("reservations cleared = %d, want a write confirmed after the walk left alone", report.ReservationsCleared)
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want the reservation preserved", artifacts)
	}
}
