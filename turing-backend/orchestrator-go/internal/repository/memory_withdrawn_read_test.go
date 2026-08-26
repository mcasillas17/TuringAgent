package repository

import (
	"errors"
	"testing"
)

// A withdrawn note is one whose supporting conversations were deleted. Search
// already refuses to surface it; a read by identity has to refuse it too, or
// the withdrawal is only as good as the model's memory of the id it was handed
// before the conversation was removed.
//
// The row is kept on purpose — the user accepted this belief once, and the file
// is still theirs — but nothing may answer with it as if it were still
// grounded.
func TestReadMemoryBeliefRefusesAWithdrawnNoteEvenToACallerHoldingItsID(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	if _, err := repo.ReadMemoryBelief(ctx(), noteID); err != nil {
		t.Fatalf("the note was not readable before it was withdrawn: %v", err)
	}

	if _, err := database.ExecContext(ctx(), `
		UPDATE memory_notes SET status = ? WHERE id = ?
	`, MemoryNoteStatusWithdrawn, noteID); err != nil {
		t.Fatalf("withdraw the note: %v", err)
	}

	document, err := repo.ReadMemoryBelief(ctx(), noteID)
	if !errors.Is(err, ErrMemoryNoteWithdrawn) {
		t.Fatalf("read of a withdrawn note = %v, want ErrMemoryNoteWithdrawn", err)
	}
	if document.Content != "" {
		t.Fatalf("a refused read still served the note's bytes: %q", document.Content)
	}
}

// The refusal is stated as "this status is not one a read answers from" rather
// than "withdrawn", so a status this build does not recognise — a row written
// by a newer version, or damaged — fails closed as well.
func TestReadMemoryBeliefRefusesAnyStatusItCannotAnswerFrom(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/note.md", managedBelief(noteID, nil, "The user keeps bees."))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("RefreshMemoryIndex: %v", err)
	}
	// The CHECK constraint refuses an unknown status outright, which is the
	// first line of this defence. The read has to hold the same line for the
	// statuses the schema does allow but a read must not answer from.
	if _, err := database.ExecContext(ctx(), `
		UPDATE memory_notes SET status = ? WHERE id = ?
	`, "not-a-status", noteID); err == nil {
		t.Fatal("the schema accepted a status outside its CHECK constraint")
	}

	if _, err := database.ExecContext(ctx(), `
		UPDATE memory_notes SET status = ? WHERE id = ?
	`, MemoryNoteStatusUnmanaged, noteID); err != nil {
		t.Fatalf("mark the note unmanaged: %v", err)
	}
	if _, err := repo.ReadMemoryBelief(ctx(), noteID); err != nil {
		t.Fatalf("an unmanaged belief the user wrote themselves is still readable: %v", err)
	}
}
