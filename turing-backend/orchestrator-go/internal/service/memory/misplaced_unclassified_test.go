package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// A profile edit the user moved into beliefs/ and then broke is a file the
// walk can classify as nothing at all. It is still a claim about them, sitting
// in their vault, and the page has to say so on both sides of it: the file is
// listed under beliefs as an error rather than as a remembered fact, and the
// proposal it came from is still on the inbox with its claim withheld — no
// text nobody can confirm, and no compare-and-set token for a decision that
// would be applied against a path the file is no longer at.
func TestMemoryStateSurfacesAMalformedProfileEditMovedIntoBeliefs(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	session, err := repo.CreateSession(ctx, "memory")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: session.SessionID, Kind: repository.MemoryCandidateKindProfileEdit,
		Title: "Miguel", Body: "The user goes by Miguel.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	moved := memoryfiles.BeliefsDirName + "/" + filepath.Base(candidate.InboxPath)
	source := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	target := filepath.Join(vault.Root(), filepath.FromSlash(moved))
	if err := os.Rename(source, target); err != nil {
		t.Fatalf("move the proposal into beliefs/: %v", err)
	}
	if err := os.WriteFile(target, []byte("---\nid: \"unterminated\nkind: profile_edit\n---\n\nMiguel.\n"), 0o600); err != nil {
		t.Fatalf("break the moved file: %v", err)
	}

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}

	var listed *turingv1.MemoryNote
	for _, note := range state.GetNotes() {
		if note.GetPath() == moved {
			listed = note
		}
	}
	if listed == nil {
		t.Fatalf("the file in the user's vault is not on the page: %+v", state.GetNotes())
	}
	if listed.GetStatus() != turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_UNSPECIFIED {
		t.Fatalf("note status = %v, want it shown as an error rather than as memory", listed.GetStatus())
	}
	if listed.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED {
		t.Fatalf("note unavailable reason = %v", listed.GetUnavailableReason())
	}
	if !strings.Contains(listed.GetParseError(), memoryfiles.InboxDirName) {
		t.Fatalf(
			"the page does not tell the user where to put this file back: %q",
			listed.GetParseError(),
		)
	}

	var proposal *turingv1.MemoryCandidate
	for _, entry := range state.GetCandidates() {
		if entry.GetCandidateId() == candidate.CandidateID {
			proposal = entry
		}
	}
	if proposal == nil {
		t.Fatalf("the proposal was swept off the page: %+v", state.GetCandidates())
	}
	if proposal.GetContentHash() != "" || proposal.GetContent() != "" {
		t.Fatalf(
			"the proposal offers a decision against a file that is not at its path: content=%q hash=%q",
			proposal.GetContent(), proposal.GetContentHash(),
		)
	}
	if proposal.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING {
		t.Fatalf("proposal unavailable reason = %v", proposal.GetUnavailableReason())
	}
}
