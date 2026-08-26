package memory

import (
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// The writing pass steps over a note it cannot write instead of taking the
// whole vault down with it. That is only half an answer: the note is still on
// the page, still readable, and still not in the index — and unless the pass's
// reason travels with it, the user sees a memory that simply never turns up in
// search, which is indistinguishable from one Turing decided to forget.
func TestListMemoryStateSaysWhyANoteTheWritingPassCouldNotWriteIsNotIndexed(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)
	// Frontmatter on one line: it parses, it is a perfectly good note, and it
	// is the one shape a frontmatter splice cannot edit — so the pass cannot
	// give it the identity it needs to be indexed.
	writeVaultDocument(t, vault, filepath.Join(memoryfiles.BeliefsDirName, "one-line.md"),
		"---\n{kind: \"belief\", title: \"one line\", managed: true, refs: []}\n---\n\nThe user writes YAML on one line.\n")
	writeVaultDocument(t, vault, filepath.Join(memoryfiles.BeliefsDirName, "ordinary.md"),
		"---\nkind: \"belief\"\ntitle: \"ordinary\"\nmanaged: true\nrefs: []\n---\n\nThe user keeps bees.\n")

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	refused := noteByPath(t, state, memoryfiles.BeliefsDirName+"/one-line.md")
	if refused.GetParseError() == "" {
		t.Fatal("a note the pass could not write is on the page with nothing saying it is not indexed")
	}
	if strings.Contains(refused.GetParseError(), "YAML on one line") {
		t.Fatalf("the reason carries the user's own prose: %q", refused.GetParseError())
	}
	adopted := noteByPath(t, state, memoryfiles.BeliefsDirName+"/ordinary.md")
	if adopted.GetNoteId() == "" {
		t.Fatal("the note beside it was not adopted")
	}
	if adopted.GetParseError() != "" {
		t.Fatalf("a note the pass wrote without trouble carries a reason: %q", adopted.GetParseError())
	}
}

func noteByPath(t *testing.T, state *turingv1.ListMemoryStateResponse, path string) *turingv1.MemoryNote {
	t.Helper()
	for _, note := range state.GetNotes() {
		if note.GetPath() == path {
			return note
		}
	}
	t.Fatalf("note %q is not on the page; notes = %v", path, state.GetNotes())
	return nil
}
