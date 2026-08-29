package repository

import (
	"testing"
)

// What a belief rests on is a count the sidecar holds, not a number the
// projection assumes. Two citations naming one conversation are two pieces of
// evidence for that conversation, and a reader that answered "1" would be
// stating a fact it never looked up.
func TestMemoryNoteEvidenceCountsCitationsPerConversation(t *testing.T) {
	repo, _, database := newMemoryTestRepo(t)
	first := newMemoryTestSession(t, repo)
	second := newMemoryTestSession(t, repo)
	note := mustPromoteTestBelief(t, repo, first, "Bees", "The user keeps bees.")

	// A second citation for the same conversation, and one for another. The
	// linking helper is idempotent per (note, session) on purpose, so this is
	// written straight to the table: the question here is what the reader does
	// with rows that exist, not how they got there.
	for _, row := range []struct {
		id      string
		session string
	}{
		{"memev-extra-first", first},
		{"memev-extra-second", second},
	} {
		if _, err := database.ExecContext(ctx(), `
			INSERT INTO memory_evidence (id, note_id, session_id, excerpt_hash, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, row.id, note.NoteID, row.session, "sha256:excerpt", now()); err != nil {
			t.Fatalf("insert evidence for %q: %v", row.session, err)
		}
	}

	evidence, err := repo.MemoryNoteEvidence(ctx(), note.NoteID)
	if err != nil {
		t.Fatalf("MemoryNoteEvidence: %v", err)
	}
	counts := map[string]int{}
	for _, group := range evidence {
		counts[group.SessionID] = group.Count
	}
	if counts[first] != 2 {
		t.Fatalf("citations for the promoting conversation = %d, want 2", counts[first])
	}
	if counts[second] != 1 {
		t.Fatalf("citations for the second conversation = %d, want 1", counts[second])
	}
	if len(evidence) != 2 {
		t.Fatalf("evidence groups = %+v, want one per conversation", evidence)
	}
}

// A note nothing cites reports nothing, rather than an empty group that would
// read downstream as "one conversation, unnamed".
func TestMemoryNoteEvidenceIsEmptyForAnUncitedNote(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	session := newMemoryTestSession(t, repo)
	note := mustPromoteTestBelief(t, repo, session, "Bees", "The user keeps bees.")

	if err := repo.DeleteSessionForTests(ctx(), session); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	evidence, err := repo.MemoryNoteEvidence(ctx(), note.NoteID)
	if err != nil {
		t.Fatalf("MemoryNoteEvidence: %v", err)
	}
	if len(evidence) != 0 {
		t.Fatalf("evidence after the conversation was deleted = %+v, want none", evidence)
	}
}

func mustPromoteTestBelief(t *testing.T, repo *Repository, sessionID string, title string, body string) MemoryNote {
	t.Helper()
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: MemoryCandidateKindBelief, Title: title, Body: body,
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	note, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatalf("PromoteMemoryCandidate: %v", err)
	}
	return note
}
