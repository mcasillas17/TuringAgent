package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// ListMemoryState walks the vault twice: once inside the writing reconcile that
// heals it, and once to render it. Two cold walks of the same folder is the
// user's whole vault read twice for one page.
//
// The second pass is served from the metadata cache the first one filled. The
// seam is the filesystem: a note nobody can read is a note whose bytes the
// second pass cannot have looked at, so if the page still shows it, it came
// from the cache.
func TestListMemoryStateReadsTheVaultOnceForOnePage(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode says, so there is no seam here")
	}
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	note := mustPromoteBelief(t, repo, ctx, sessionID, "Bees", "The user keeps bees.")
	if _, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{}); err != nil {
		t.Fatalf("first page: %v", err)
	}

	full := filepath.Join(vault.Root(), filepath.FromSlash(note.Path))
	if err := os.Chmod(full, 0o000); err != nil {
		t.Fatalf("seal the belief: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(full, 0o600) })

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	listed := beliefByID(t, state, note.NoteID)
	if listed.GetParseError() != "" {
		t.Fatalf("the render pass opened the note again: %q", listed.GetParseError())
	}
	if !strings.Contains(listed.GetContent(), "bees") {
		t.Fatalf("listed content = %q, want the belief the first pass already read", listed.GetContent())
	}
}

func beliefByID(t *testing.T, state *turingv1.ListMemoryStateResponse, noteID string) *turingv1.MemoryNote {
	t.Helper()
	for _, note := range state.GetNotes() {
		if note.GetNoteId() == noteID {
			return note
		}
	}
	t.Fatalf("belief %q is not on the page; notes = %v", noteID, state.GetNotes())
	return nil
}
