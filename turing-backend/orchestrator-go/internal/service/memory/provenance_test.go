package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// What a belief rests on is read, never assumed.
//
// The page beside a claim has to answer three different questions with three
// different answers: this rests on a conversation you still have, this rested
// on one that is gone, and nothing ever grounded this. A projection that
// stamped "1 piece of evidence" on every row would collapse the first two into
// each other and make the third unreachable.

func TestMemoryStateReportsTheEvidenceABeliefActuallyHas(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	mustPromoteBelief(t, repo, ctx, sessionID, "Bees", "The user keeps bees.")

	note := onlyBelief(t, listState(t, service, ctx))
	if len(note.GetProvenance()) != 1 {
		t.Fatalf("provenance = %+v, want one row for the conversation that produced it", note.GetProvenance())
	}
	row := note.GetProvenance()[0]
	if row.GetSourceSessionId() != sessionID {
		t.Fatalf("source session = %q, want %q", row.GetSourceSessionId(), sessionID)
	}
	if row.GetEvidenceCount() != 1 {
		t.Fatalf("evidence count = %d, want the one citation the sidecar holds", row.GetEvidenceCount())
	}
	if row.GetWithdrawn() {
		t.Fatal("a belief whose conversation still exists was reported as withdrawn")
	}
}

// The conversation is deleted, the citation cascades out, and the belief stays
// because the user accepted it. The row that survives has to say so: withdrawn,
// grounded in nothing, and naming no conversation — the one it cited is gone,
// and repeating its identifier would hand back part of what was deleted.
func TestMemoryStateReportsAWithdrawnBeliefAsGroundedInNothing(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	mustPromoteBelief(t, repo, ctx, sessionID, "Bees", "The user keeps bees.")
	if _, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{}); err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}

	if err := repo.DeleteSession(ctx, sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	note := onlyBelief(t, listState(t, service, ctx))
	if note.GetStatus() != turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_WITHDRAWN {
		t.Fatalf("status = %v, want WITHDRAWN", note.GetStatus())
	}
	if len(note.GetProvenance()) != 1 {
		t.Fatalf("provenance = %+v, want one row saying the evidence was withdrawn", note.GetProvenance())
	}
	row := note.GetProvenance()[0]
	if !row.GetWithdrawn() {
		t.Fatal("the surviving provenance row does not say the evidence was withdrawn")
	}
	if row.GetEvidenceCount() != 0 {
		t.Fatalf("evidence count = %d, want 0: the conversation behind it is gone", row.GetEvidenceCount())
	}
	if row.GetSourceSessionId() != "" {
		t.Fatalf("source session = %q, want a withdrawn row to name no conversation", row.GetSourceSessionId())
	}
}

// A belief the user wrote by hand cites nothing and was never withdrawn. It
// must not be dressed in a withdrawal it never had: an empty provenance list is
// the honest answer, and the client says "kept with no evidence behind it".
func TestMemoryStateLeavesAnUnevidencedBeliefWithNoProvenanceRow(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)
	writeBelief(t, vault, "beliefs/handwritten.md", "# By hand\n\nThe user keeps bees.\n")

	note := onlyBelief(t, listState(t, service, ctx))
	if len(note.GetProvenance()) != 0 {
		t.Fatalf("provenance = %+v, want none for a note nothing cites", note.GetProvenance())
	}
	if note.GetStatus() == turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_WITHDRAWN {
		t.Fatal("a note that was never grounded was reported as withdrawn")
	}
}

// A proposal whose conversation was deleted cannot be accepted, and it does not
// still rest on that conversation either. Saying "withdrawn" and "1 piece of
// evidence" in the same breath is the page contradicting itself.
func TestMemoryStateReportsAWithdrawnProposalAsGroundedInNothing(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      repository.MemoryCandidateKindBelief,
		Title:     "Bees",
		Body:      "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	if _, err := repo.WithdrawMemoryCandidate(ctx, candidate.CandidateID); err != nil {
		t.Fatalf("WithdrawMemoryCandidate: %v", err)
	}

	state := listState(t, service, ctx)
	if len(state.GetCandidates()) != 1 {
		t.Fatalf("candidates = %+v, want the withdrawn proposal to survive", state.GetCandidates())
	}
	proposal := state.GetCandidates()[0]
	if proposal.GetState() != turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_WITHDRAWN {
		t.Fatalf("state = %v, want WITHDRAWN", proposal.GetState())
	}
	for _, row := range proposal.GetProvenance() {
		if !row.GetWithdrawn() {
			t.Fatalf("provenance row %+v does not say the evidence was withdrawn", row)
		}
		if row.GetEvidenceCount() != 0 {
			t.Fatalf("evidence count = %d, want 0 for a withdrawn proposal", row.GetEvidenceCount())
		}
	}
	if len(proposal.GetProvenance()) == 0 {
		t.Fatal("a withdrawn proposal says nothing at all about its evidence")
	}
}

// A pending proposal rests on exactly the conversation that produced it, and
// says so with the count that conversation contributes.
func TestMemoryStateReportsAPendingProposalsSourceConversation(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	if _, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      repository.MemoryCandidateKindBelief,
		Title:     "Bees",
		Body:      "The user keeps bees.",
	}); err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	state := listState(t, service, ctx)
	if len(state.GetCandidates()) != 1 {
		t.Fatalf("candidates = %+v, want one", state.GetCandidates())
	}
	provenance := state.GetCandidates()[0].GetProvenance()
	if len(provenance) != 1 {
		t.Fatalf("provenance = %+v, want one row", provenance)
	}
	if provenance[0].GetSourceSessionId() != sessionID {
		t.Fatalf("source session = %q, want %q", provenance[0].GetSourceSessionId(), sessionID)
	}
	if provenance[0].GetEvidenceCount() != 1 {
		t.Fatalf("evidence count = %d, want 1", provenance[0].GetEvidenceCount())
	}
	if provenance[0].GetWithdrawn() {
		t.Fatal("a pending proposal was reported as withdrawn")
	}
}

func listState(t *testing.T, service *Server, ctx context.Context) *turingv1.ListMemoryStateResponse {
	t.Helper()
	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	return state
}

func onlyBelief(t *testing.T, state *turingv1.ListMemoryStateResponse) *turingv1.MemoryNote {
	t.Helper()
	if len(state.GetNotes()) != 1 {
		t.Fatalf("notes = %+v, want exactly one belief", state.GetNotes())
	}
	return state.GetNotes()[0]
}

func newMemorySession(t *testing.T, repo *repository.Repository, ctx context.Context) string {
	t.Helper()
	session, err := repo.CreateSession(ctx, "memory")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return session.SessionID
}

func writeBelief(t *testing.T, vault *memoryfiles.Vault, relPath string, content string) {
	t.Helper()
	full := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("prepare %q: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write %q: %v", relPath, err)
	}
}
