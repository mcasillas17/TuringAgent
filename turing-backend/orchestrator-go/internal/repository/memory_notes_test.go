package repository

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A read of a belief goes to the file, every time. The projection is a copy of
// what the file said last time anyone looked, and the user may have edited it
// in Obsidian since — serving the row would answer with a memory they have
// already changed.
func TestReadMemoryBeliefServesFreshBytesWithoutReconciling(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}

	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps chickens."))

	document, err := repo.ReadMemoryBelief(ctx(), noteID)
	if err != nil {
		t.Fatalf("ReadMemoryBelief: %v", err)
	}
	if !strings.Contains(document.Content, "chickens") {
		t.Fatalf("read served stale bytes: %q", document.Content)
	}
	if document.NoteID != noteID || document.RelPath != "beliefs/note.md" {
		t.Fatalf("unexpected document identity: %+v", document)
	}
	// The projection is deliberately still stale: nothing about a read updates
	// the index, so a read cannot be a hidden write.
	note, found := noteRowFor(t, repo, noteID)
	if !found || strings.Contains(note.Content, "chickens") {
		t.Fatalf("reading a belief rewrote the index: %+v", note)
	}
}

// There is no path argument and no scope argument. A read is a stable
// identity, resolved through the index the repository owns, and re-checked by
// the vault before a byte is opened.
func TestReadMemoryBeliefRefusesUnknownAndNonBeliefIdentities(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)

	if _, err := repo.ReadMemoryBelief(ctx(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); !errors.Is(err, ErrMemoryNoteNotFound) {
		t.Fatalf("unknown identity error = %v, want ErrMemoryNoteNotFound", err)
	}
	if _, err := repo.ReadMemoryBelief(ctx(), ""); err == nil {
		t.Fatalf("an empty identity was accepted")
	}

	// A row pointing outside beliefs/ is a corrupt index, and the read refuses
	// it rather than following it.
	writeVaultNote(t, vault, "inbox/candidate.md", managedBelief("01ARZ3NDEKTSV4RRFFQ69G5FAV", nil, "unreviewed"))
	if _, err := database.ExecContext(ctx(), `
		INSERT INTO memory_notes (id, path, content, content_hash, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "inbox/candidate.md", "unreviewed", "sha256:x", MemoryNoteStatusManaged, now(), now()); err != nil {
		t.Fatalf("seed a corrupt index row: %v", err)
	}
	if _, err := repo.ReadMemoryBelief(ctx(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err == nil {
		t.Fatalf("a read followed an index row pointing into the inbox")
	}
}

func TestSearchMemoryNotesValidatesItsBounds(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}

	for _, limit := range []int{0, -1, maxMemorySearchLimit + 1} {
		if _, err := repo.SearchMemoryNotes(ctx(), "bees", limit); !errors.Is(err, ErrMemorySearchQuery) {
			t.Fatalf("limit %d error = %v, want ErrMemorySearchQuery", limit, err)
		}
	}
	if _, err := repo.SearchMemoryNotes(ctx(), strings.Repeat("a", maxMemorySearchQueryBytes+1), 10); !errors.Is(err, ErrMemorySearchQuery) {
		t.Fatalf("an over-long query was accepted")
	}
	// A query with nothing to match on is an empty result, not an error: the
	// user typing punctuation has not asked a bad question.
	for _, query := range []string{"", "   ", "!!!"} {
		hits, err := repo.SearchMemoryNotes(ctx(), query, 10)
		if err != nil {
			t.Fatalf("SearchMemoryNotes(%q): %v", query, err)
		}
		if len(hits) != 0 {
			t.Fatalf("SearchMemoryNotes(%q) = %+v, want no hits", query, hits)
		}
	}
	// FTS5 operators in user text are matched as text, not executed as syntax.
	if _, err := repo.SearchMemoryNotes(ctx(), `bees" OR beliefs:`, 10); err != nil {
		t.Fatalf("SearchMemoryNotes with operator-shaped text: %v", err)
	}
}

// Search answers over accepted memory only. A note the vault holds outside
// beliefs/ — a candidate, an inbox draft — never enters the index at all, and
// a withdrawn note leaves it.
func TestSearchMemoryNotesCoversOnlyActiveBeliefs(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	active := newTestNoteID(t)
	withdrawn := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/active.md", managedBelief(active, nil, "The user keeps bees."))
	writeVaultNote(t, vault, "beliefs/withdrawn.md", managedBelief(withdrawn, nil, "The user keeps bees too."))
	writeVaultNote(t, vault, "inbox/draft.md", managedBelief(newTestNoteID(t), nil, "The user keeps bees as well."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	if _, err := database.ExecContext(ctx(), `
		UPDATE memory_notes SET status = ? WHERE id = ?
	`, MemoryNoteStatusWithdrawn, withdrawn); err != nil {
		t.Fatalf("withdraw a note: %v", err)
	}

	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 1 || hits[0].NoteID != active {
		t.Fatalf("hits = %+v, want only the active belief", hits)
	}
}

func TestSearchMemoryNotesHonoursItsLimit(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	for index := range 5 {
		noteID := newTestNoteID(t)
		writeVaultNote(t, vault, filepath.ToSlash(filepath.Join("beliefs", noteID+".md")),
			managedBelief(noteID, nil, "The user keeps bees, note "+string(rune('a'+index))+"."))
	}
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 2)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want the limit honoured", len(hits))
	}
}

// The index is a projection of files, and the vault is where memory lives. A
// projection that is thrown away and rebuilt must come back identical, because
// that is what makes it safe to rebuild.
func TestMemoryProjectionIsDisposable(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, []string{sessionID}, "The user keeps bees."))
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	before, found := noteRowFor(t, repo, noteID)
	if !found {
		t.Fatalf("note was not indexed")
	}

	if _, err := database.ExecContext(ctx(), `DELETE FROM memory_notes`); err != nil {
		t.Fatalf("drop the projection: %v", err)
	}
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault after dropping the projection: %v", err)
	}
	after, found := noteRowFor(t, repo, noteID)
	if !found {
		t.Fatalf("the projection did not come back")
	}
	if after.Path != before.Path || after.Content != before.Content || after.ContentHash != before.ContentHash {
		t.Fatalf("rebuilt projection differs:\nbefore %+v\nafter  %+v", before, after)
	}
	if got := evidenceSessions(t, repo, noteID); len(got) != 1 || got[0] != sessionID {
		t.Fatalf("evidence after the rebuild = %v", got)
	}
	if content := readVaultNote(t, vault, "beliefs/note.md"); !strings.Contains(content, "The user keeps bees.") {
		t.Fatalf("the file changed while the projection was rebuilt")
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), "beliefs", "note.md")); err != nil {
		t.Fatalf("stat the belief: %v", err)
	}
}
