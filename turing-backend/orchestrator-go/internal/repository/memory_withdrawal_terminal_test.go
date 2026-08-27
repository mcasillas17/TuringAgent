package repository

import (
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// Withdrawal is terminal, and the sidecar is what remembers it.
//
// Once a pass has marked a belief withdrawn, no later pass may hand it back —
// not the everyday second pass over the converged vault, and not a pass that
// reads a file some sync or backup restored from before the withdrawal ever
// happened. The restored copy is the dangerous one: it says `managed: true`
// and carries no citations at all, so nothing in the file itself admits that
// anything was ever withdrawn. Read on its own it is an ordinary, healthy
// belief. The only thing standing between it and search is the row that
// already says withdrawn, and the rule that a withdrawn row stays withdrawn.
//
// This is asserted from the two places a resurrection would actually be felt:
// the status stored in `memory_notes`, and whether a search over the user's
// memory answers with the note. `TestWithdrawnEvidenceIsWrittenAsAWithdrawalAndCannotBeReinserted`
// runs a second pass too, but only asks whether that pass rewrote a file and
// whether the marker was re-read as a citation — it never looks at the status
// again afterwards, so deleting the terminal rule leaves it green.
func TestWithdrawalIsTerminalAcrossLaterReconcilePasses(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, []string{sessionID}, "The user keeps bees."))

	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault to adopt the note: %v", err)
	}
	// The note is grounded and answerable here, so the assertions below are
	// about the withdrawal and not about a note search never matched at all.
	assertMemoryNoteSearchable(t, repo, noteID, "grounded, before the conversation was deleted")

	if err := repo.DeleteSession(ctx(), sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault after the deletion: %v", err)
	}
	assertMemoryNoteWithdrawnAndUnsearchable(t, repo, noteID, "the pass that saw the conversation was gone")

	// The everyday next pass, over the vault the withdrawal itself left behind.
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("second ReconcileMemoryVault: %v", err)
	}
	assertMemoryNoteWithdrawnAndUnsearchable(t, repo, noteID, "a later pass over the converged vault")

	// Stale frontmatter: a copy from before the withdrawal is restored under
	// the pass. Managed, no refs, no marker — a file that reads as grounded.
	restored := managedBelief(noteID, nil, "The user keeps bees.")
	writeVaultNote(t, vault, "beliefs/note.md", restored)
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault over the restored file: %v", err)
	}
	assertMemoryNoteWithdrawnAndUnsearchable(t, repo, noteID, "a pass over stale frontmatter restored by a sync")

	// The file is the user's; refusing to resurrect the note is a decision the
	// sidecar makes, not a licence to rewrite what they have on disk.
	if got := readVaultNote(t, vault, "beliefs/note.md"); got != restored {
		t.Fatalf("the pass rewrote the restored file:\nwant %q\ngot  %q", restored, got)
	}
	if got := evidenceSessions(t, repo, noteID); len(got) != 0 {
		t.Fatalf("stale frontmatter regrew evidence: %v", got)
	}
}

// The same terminal rule seen from the other side: a withdrawn note whose file
// still says `refs: "withdrawn"` in so many words. Both legs matter, because a
// rule that only holds while the marker is present is not terminal — it is the
// marker being re-read, and a file is not where this fact lives.
func TestWithdrawalIsTerminalEvenWhenTheFileNoLongerSaysSo(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, []string{sessionID}, "The user keeps bees."))

	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault to adopt the note: %v", err)
	}
	if err := repo.DeleteSession(ctx(), sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault after the deletion: %v", err)
	}
	if got := readVaultNote(t, vault, "beliefs/note.md"); !strings.Contains(got, memoryfiles.WithdrawnRefsMarker) {
		t.Fatalf("the withdrawal was never written to the file: %q", got)
	}

	// The user edits the note in Obsidian and drops the refs line entirely.
	// Nothing on disk says withdrawn any more.
	edited := managedBelief(noteID, nil, "The user keeps bees. And now also chickens.")
	writeVaultNote(t, vault, "beliefs/note.md", edited)
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault over the edited file: %v", err)
	}
	assertMemoryNoteWithdrawnAndUnsearchable(t, repo, noteID, "an edit that removed the withdrawal marker")

	// The edit still reaches the index — the note is not frozen, only withheld.
	note, found := noteRowFor(t, repo, noteID)
	if !found {
		t.Fatalf("the edited note left the index entirely")
	}
	if want := "chickens"; !strings.Contains(note.Content, want) {
		t.Fatalf("the indexed content = %q, want it to carry the user's edit %q", note.Content, want)
	}
}

func assertMemoryNoteWithdrawnAndUnsearchable(t *testing.T, repo *Repository, noteID string, after string) {
	t.Helper()
	note, found := noteRowFor(t, repo, noteID)
	if !found {
		t.Fatalf("after %s the note row is gone", after)
	}
	if note.Status != MemoryNoteStatusWithdrawn {
		t.Fatalf("after %s the status = %q, want %q", after, note.Status, MemoryNoteStatusWithdrawn)
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes after %s: %v", after, err)
	}
	for _, hit := range hits {
		if hit.NoteID == noteID {
			t.Fatalf("after %s a withdrawn note answered a search: %+v", after, hit)
		}
	}
}

func assertMemoryNoteSearchable(t *testing.T, repo *Repository, noteID string, when string) {
	t.Helper()
	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes while %s: %v", when, err)
	}
	for _, hit := range hits {
		if hit.NoteID == noteID {
			return
		}
	}
	t.Fatalf("while %s the note did not answer a search: %+v", when, hits)
}
