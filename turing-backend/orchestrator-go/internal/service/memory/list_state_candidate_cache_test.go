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

// rewriteInboxKeepingMetadata is the user editing a proposal in Obsidian in the
// one way the vault's metadata cache cannot see: the same file, written in the
// same second, ending up exactly the same length. Same inode, because the write
// truncates the existing entry rather than renaming a new one over it.
func rewriteInboxKeepingMetadata(t *testing.T, vault *memoryfiles.Vault, relPath string, replace string, with string) string {
	t.Helper()
	if len(replace) != len(with) {
		t.Fatalf("this edit changes the length (%d -> %d), so it is not the residual case", len(replace), len(with))
	}
	full := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
	before, err := os.Stat(full)
	if err != nil {
		t.Fatalf("stat %q: %v", relPath, err)
	}
	original, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read %q: %v", relPath, err)
	}
	rewritten := strings.Replace(string(original), replace, with, 1)
	if rewritten == string(original) {
		t.Fatalf("the edit changed nothing; %q does not contain %q", relPath, replace)
	}
	if err := os.WriteFile(full, []byte(rewritten), 0o600); err != nil {
		t.Fatalf("write %q: %v", relPath, err)
	}
	if err := os.Chtimes(full, before.ModTime(), before.ModTime()); err != nil {
		t.Fatalf("restore times on %q: %v", relPath, err)
	}
	after, err := os.Stat(full)
	if err != nil {
		t.Fatalf("stat %q after the rewrite: %v", relPath, err)
	}
	// The premise, asserted rather than assumed: if any of these moved, the
	// cache would have noticed the edit and the test would be proving nothing.
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("the rewrite was visible to a (mtime, size) cache: %v/%d -> %v/%d",
			before.ModTime(), before.Size(), after.ModTime(), after.Size())
	}
	return rewritten
}

// The page and the decision have to agree about what the file says, and only
// one of them can be served from a cache.
//
// The listing renders the candidate's bytes and hands out its hash; the
// decision is a compare-and-set against the file under the vault's own lock. So
// a pass that serves a proposal out of the metadata cache puts words on the
// page that the decision then refuses — and re-reading the page produces the
// same refused token, on every press, for as long as the cache entry lives. A
// proposal edited this way used to be undecidable.
//
// The edit below is the residual the cache is documented to accept: same file,
// same second, same length. That residual is a stale search result over
// beliefs, which the next pass corrects. It was never an argument for holding
// an unreviewed claim about the user in front of the one screen where they act
// on it.
func TestListMemoryStateReadsACandidateAfreshAfterAnEditTheCacheCannotSee(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", "The user prefers dark mode.")

	warm, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	before := candidateByID(t, warm, candidate.CandidateID)

	rewritten := rewriteInboxKeepingMetadata(t, vault, candidate.InboxPath, "dark mode.", "warm mode.")

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState after the edit: %v", err)
	}
	listed := candidateByID(t, state, candidate.CandidateID)
	if !strings.Contains(listed.GetContent(), "warm mode") {
		t.Fatalf("the page shows %q, want the words the file now holds", listed.GetContent())
	}
	if listed.GetContent() == before.GetContent() {
		t.Fatalf("the page served the words it was holding: %q", listed.GetContent())
	}
	if listed.GetContentHash() == before.GetContentHash() {
		t.Fatalf("the page handed out the hash of bytes that are no longer there: %q", listed.GetContentHash())
	}
	if listed.GetContentHash() != memoryfiles.ContentHash(rewritten) {
		t.Fatalf("hash = %q, want the hash of the file as it now reads", listed.GetContentHash())
	}

	// And the decision the page just offered is one the server accepts. This is
	// the half that made the stale entry a trap rather than a nuisance.
	if _, err := service.RejectMemoryCandidate(ctx, &turingv1.RejectMemoryCandidateRequest{
		CandidateId:           candidate.CandidateID,
		ExpectedContentHash:   listed.GetContentHash(),
		ExpectedCandidateHash: listed.GetContentHash(),
	}); err != nil {
		t.Fatalf("rejecting the proposal the listing served: %v", err)
	}
	full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
	if _, statErr := os.Lstat(full); !os.IsNotExist(statErr) {
		t.Fatalf("the rejected proposal is still in the inbox: %v", statErr)
	}
}
