package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// managedBeliefFile is a belief the way the vault writes one, with an identity
// the caller chooses so a test can duplicate or contest it.
func managedBeliefFile(noteID string, body string) string {
	return "---\nid: \"" + noteID + "\"\nkind: \"belief\"\ntitle: \"a belief\"\nmanaged: true\nrefs: []\n---\n\n" + body + "\n"
}

func newNoteID(t *testing.T) string {
	t.Helper()
	noteID, err := memoryfiles.NewNoteID()
	if err != nil {
		t.Fatalf("mint note id: %v", err)
	}
	return noteID
}

// A note whose rename the pass had to hold back is in the vault and readable,
// and its row still names the path it no longer has. Search answers from the
// row, so this note either does not come back at all or comes back under a
// name the user cannot open — and the page, drawing from the walk, shows it
// looking exactly like every other note.
//
// The reconcile pass already knows: it parks the rename and reports the note in
// ContestedPaths. Nothing consumed that, so the one component that could tell
// the user said nothing.
func TestListMemoryStateSaysANoteWhoseRenameIsHeldBackIsNotCurrentInTheIndex(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	contested, mover := newNoteID(t), newNoteID(t)
	writeVaultDocument(t, vault, filepath.Join(memoryfiles.BeliefsDirName, "contested.md"),
		managedBeliefFile(contested, "The user keeps bees."))
	writeVaultDocument(t, vault, filepath.Join(memoryfiles.BeliefsDirName, "mover.md"),
		managedBeliefFile(mover, "The user rides a bicycle."))
	if _, err := repo.RefreshMemoryIndex(callCtx); err != nil {
		t.Fatalf("first index pass: %v", err)
	}

	// One sitting in Obsidian: the incumbent is duplicated, so its identity is
	// contested and no pass may move its row; and it is renamed away from
	// contested.md, so the mover can take that name on disk while the row
	// still holds it.
	writeVaultDocument(t, vault, filepath.Join(memoryfiles.BeliefsDirName, "copy-a.md"),
		managedBeliefFile(contested, "The user keeps bees."))
	renameVaultDocument(t, vault,
		filepath.Join(memoryfiles.BeliefsDirName, "contested.md"),
		filepath.Join(memoryfiles.BeliefsDirName, "copy-b.md"))
	renameVaultDocument(t, vault,
		filepath.Join(memoryfiles.BeliefsDirName, "mover.md"),
		filepath.Join(memoryfiles.BeliefsDirName, "contested.md"))

	state, err := service.ListMemoryState(callCtx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	held := noteByPath(t, state, memoryfiles.BeliefsDirName+"/contested.md")
	if held.GetParseError() == "" {
		t.Fatal("a note whose place in the index is stale is on the page with nothing saying so")
	}
	if strings.Contains(held.GetParseError(), "bicycle") {
		t.Fatalf("the reason carries the user's own prose: %q", held.GetParseError())
	}
}

// A candidate file with no candidate row is a creation that crashed between the
// write and its transaction. Turing wrote it, nothing tracks it, and every
// decision RPC refuses it — so it was neither listed as a proposal nor listed
// at all, and a claim about the user sat in their vault with the one component
// that could mention it saying nothing.
//
// It must be visible, marked as something Turing does not own, and never
// dressed up as a draft the user wrote themselves.
func TestListMemoryStateListsAnInboxNoteWithNoRowAsUntracked(t *testing.T) {
	service, repo, vault, database, callCtx := newMemoryServiceStack(
		t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), nil)
	_, sessionID := newRun(t, repo, callCtx)
	candidate, err := repo.CreateMemoryCandidate(callCtx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      repository.MemoryCandidateKindBelief,
		Title:     "Bees",
		Body:      "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	before, err := os.ReadFile(full) //nolint:gosec // a path this test just created
	if err != nil {
		t.Fatalf("read the proposal: %v", err)
	}
	dropCandidateRow(t, callCtx, database, candidate.CandidateID)

	state, err := service.ListMemoryState(callCtx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	var listed *turingv1.MemoryCandidate
	for _, row := range state.GetCandidates() {
		if row.GetInboxPath() == candidate.InboxPath {
			listed = row
		}
	}
	if listed == nil {
		t.Fatalf("an inbox file nothing tracks was left off the page entirely; candidates = %v", state.GetCandidates())
	}
	if listed.GetManaged() {
		t.Fatal("a file with no row was offered as a proposal Turing can decide")
	}
	if !listed.GetUntracked() {
		t.Fatal("a file Turing wrote and lost the record of was not marked untracked")
	}
	if listed.GetCandidateId() != "" {
		t.Fatalf("candidate id = %q, want nothing to hand a decision RPC", listed.GetCandidateId())
	}
	after, err := os.ReadFile(full) //nolint:gosec // a path this test just created
	if err != nil {
		t.Fatalf("the page deleted an inbox file it only meant to list: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("the page rewrote bytes in the user's vault")
	}
}

func renameVaultDocument(t *testing.T, vault *memoryfiles.Vault, from string, to string) {
	t.Helper()
	if err := os.Rename(filepath.Join(vault.Root(), from), filepath.Join(vault.Root(), to)); err != nil {
		t.Fatalf("rename %q to %q: %v", from, to, err)
	}
}

// dropCandidateRow is the crash this test cannot stage any other way: the
// candidate file is written before the row that describes it, so a process
// that dies in between leaves exactly this — bytes in the vault that nothing
// in the database claims.
func dropCandidateRow(t *testing.T, ctx context.Context, database *db.DB, candidateID string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `DELETE FROM memory_candidates WHERE id = ?`, candidateID); err != nil {
		t.Fatalf("drop the candidate row: %v", err)
	}
}
