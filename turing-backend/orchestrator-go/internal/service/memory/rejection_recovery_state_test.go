package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// A rejection that detaches a file it may not delete, and cannot put back under
// its own name because somebody else took it, keeps the bytes under a name this
// server mints. The point of that name is that it is *visible*: the vault walk
// skips dot entries, so the private staging name it used to keep them under put
// an unread claim about the user on disk and on no page anywhere.
//
// This is the page. A recovery draft has no row, no candidate id and no
// lifecycle — nothing in the database claims it — so it arrives the way every
// other file the user has in their inbox does: listed, untracked, and there to
// be read or deleted.
func TestMemoryStateListsARecoveryDraftLeftByARefusedRejection(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)
	name, err := memoryfiles.RecoveryDraftFileName()
	if err != nil {
		t.Fatalf("mint a recovery name: %v", err)
	}
	const rescued = "a newer claim nobody has read yet"
	relPath := memoryfiles.InboxDirName + "/" + name
	if err := os.WriteFile(
		filepath.Join(vault.Root(), memoryfiles.InboxDirName, name),
		[]byte(rescued),
		0o600,
	); err != nil {
		t.Fatalf("place the recovery draft: %v", err)
	}

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	var listed *turingv1.MemoryCandidate
	for _, candidate := range state.GetCandidates() {
		if candidate.GetInboxPath() == relPath {
			listed = candidate
		}
	}
	if listed == nil {
		t.Fatalf("the rescued file is on disk and on no page: %+v", state.GetCandidates())
	}
	if listed.GetCandidateId() != "" {
		t.Fatalf("the recovery draft was given a candidate id no row backs: %q", listed.GetCandidateId())
	}
	if listed.GetManaged() {
		t.Fatal("a file rescued off a contested name is not a proposal Turing is managing")
	}
	if listed.GetContent() != rescued {
		t.Fatalf("the page shows %q for the recovery draft, want the rescued bytes", listed.GetContent())
	}
}

// The other half of the same promise: the staging name stays invisible, because
// nothing is left under it any more. A dot entry on this page would be Turing
// showing the user its own scratch space.
func TestMemoryStateDoesNotListTheReservedStagingName(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)
	reserved := ".turing-memory-0123456789abcdef"
	if err := os.WriteFile(
		filepath.Join(vault.Root(), memoryfiles.InboxDirName, reserved),
		[]byte("half a write nobody committed"),
		0o600,
	); err != nil {
		t.Fatalf("place the staging entry: %v", err)
	}

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	for _, candidate := range state.GetCandidates() {
		if strings.Contains(candidate.GetInboxPath(), reserved) {
			t.Fatalf("a staging entry was listed as a candidate: %+v", candidate)
		}
	}
	for _, note := range state.GetNotes() {
		if strings.Contains(note.GetPath(), reserved) {
			t.Fatalf("a staging entry was listed as a note: %+v", note)
		}
	}
}
