package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// newMemoryTestRepo wires a repository to a real vault on disk. The vault is a
// real directory tree rather than a fake, because every ordering property this
// package promises is about a file existing or not existing.
func newMemoryTestRepo(t *testing.T) (*Repository, *memoryfiles.Vault, *db.DB) {
	t.Helper()
	database := openTestDB(t)
	repo := New(database)
	root := t.TempDir()
	for _, dir := range []string{memoryfiles.InboxDirName, memoryfiles.BeliefsDirName} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatalf("prepare vault dir %q: %v", dir, err)
		}
	}
	vault, err := memoryfiles.Open(root)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	repo.SetMemoryVault(vault)
	return repo, vault, database
}

func newMemoryTestSession(t *testing.T, repo *Repository) string {
	t.Helper()
	session, err := repo.CreateSession(ctx(), "memory")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return session.SessionID
}

func ctx() context.Context { return context.Background() }

func inboxEntries(t *testing.T, vault *memoryfiles.Vault) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(vault.Root(), memoryfiles.InboxDirName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read inbox: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestCreateMemoryCandidateWritesFileRowAndManifest(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID:    sessionID,
		Kind:         MemoryCandidateKindBelief,
		Title:        "Prefers dark mode",
		Body:         "The user asked for dark mode twice.",
		EvidenceRefs: []string{sessionID},
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	if candidate.State != MemoryCandidateStatePending || candidate.DecidedAt != "" {
		t.Fatalf("new candidate is not pending: %+v", candidate)
	}
	if candidate.SourceSessionID != sessionID {
		t.Fatalf("source session = %q, want %q", candidate.SourceSessionID, sessionID)
	}
	if !strings.HasPrefix(candidate.InboxPath, memoryfiles.InboxDirName+"/") {
		t.Fatalf("candidate path %q is not in the inbox", candidate.InboxPath)
	}
	if candidate.Body != "The user asked for dark mode twice." {
		t.Fatalf("stored body = %q", candidate.Body)
	}
	if len(candidate.EvidenceRefs) != 1 || candidate.EvidenceRefs[0] != sessionID {
		t.Fatalf("stored evidence refs = %v", candidate.EvidenceRefs)
	}

	content, err := os.ReadFile(filepath.Join(vault.Root(), candidate.InboxPath))
	if err != nil {
		t.Fatalf("read candidate file: %v", err)
	}
	if memoryfiles.ContentHash(string(content)) != candidate.ContentHash {
		t.Fatalf("stored hash does not describe the file on disk")
	}

	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("manifest rows = %d, want 1", len(artifacts))
	}
	if artifacts[0].VaultPath != candidate.InboxPath || artifacts[0].State != VaultArtifactStateReady {
		t.Fatalf("manifest row does not describe the written candidate: %+v", artifacts[0])
	}

	// A candidate is not memory yet: nothing about it may reach the note index.
	var notes int
	if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM memory_notes`).Scan(&notes); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	if notes != 0 {
		t.Fatalf("creating a candidate projected %d notes, want 0", notes)
	}
}

// The manifest row has to exist before the bytes do. Making the write fail
// leaves the reservation visible, which is the only durable evidence that a
// file may have been created — and the reservation is kept, not deleted,
// because deleting it is how an untracked file in the user's vault happens.
func TestCreateMemoryCandidateReservesBeforeItWrites(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	// Replace the inbox directory with a regular file. Every write into it now
	// fails, deterministically and regardless of who is running the test.
	inbox := filepath.Join(vault.Root(), memoryfiles.InboxDirName)
	if err := os.RemoveAll(inbox); err != nil {
		t.Fatalf("remove inbox: %v", err)
	}
	if err := os.WriteFile(inbox, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block the inbox: %v", err)
	}

	if _, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "Prefers dark mode",
		Body:      "The user asked for dark mode twice.",
	}); err == nil {
		t.Fatalf("CreateMemoryCandidate succeeded with a blocked inbox")
	}

	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("manifest rows = %d, want the reservation taken before the write to survive", len(artifacts))
	}
	if artifacts[0].State != VaultArtifactStateWriting {
		t.Fatalf("reservation state = %q, want it left retryable as %q", artifacts[0].State, VaultArtifactStateWriting)
	}
	var candidates int
	if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM memory_candidates`).Scan(&candidates); err != nil {
		t.Fatalf("count candidates: %v", err)
	}
	if candidates != 0 {
		t.Fatalf("a failed write left %d candidate rows, want 0", candidates)
	}
}

// The reverse order is impossible: when the reservation cannot be taken, no
// byte is written. A session that does not exist cannot hold a manifest row,
// so it is the reservation itself that fails.
func TestCreateMemoryCandidateWritesNothingWhenTheReservationFails(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)

	if _, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: "sess_missing",
		Kind:      MemoryCandidateKindBelief,
		Title:     "Prefers dark mode",
		Body:      "The user asked for dark mode twice.",
	}); err == nil {
		t.Fatalf("CreateMemoryCandidate succeeded for an unknown session")
	}

	if names := inboxEntries(t, vault); len(names) != 0 {
		t.Fatalf("a refused reservation still wrote %v", names)
	}
	var artifacts int
	if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM vault_artifacts`).Scan(&artifacts); err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	if artifacts != 0 {
		t.Fatalf("manifest rows = %d, want 0", artifacts)
	}
}

func TestCreateMemoryCandidateRefusesBadInput(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	tests := []struct {
		name  string
		input CreateMemoryCandidateInput
		want  error
	}{
		{
			name:  "unknown kind",
			input: CreateMemoryCandidateInput{SessionID: sessionID, Kind: "rumour", Body: "body"},
			want:  ErrMemoryCandidateKind,
		},
		{
			name:  "empty kind",
			input: CreateMemoryCandidateInput{SessionID: sessionID, Body: "body"},
			want:  ErrMemoryCandidateKind,
		},
		{
			name:  "empty body",
			input: CreateMemoryCandidateInput{SessionID: sessionID, Kind: MemoryCandidateKindBelief, Body: "   "},
			want:  ErrMemoryCandidateBody,
		},
		{
			name: "body past the candidate limit",
			input: CreateMemoryCandidateInput{
				SessionID: sessionID,
				Kind:      MemoryCandidateKindBelief,
				Body:      strings.Repeat("a", memoryfiles.MaxCandidateBodyBytes+1),
			},
			want: ErrMemoryCandidateBody,
		},
		{
			name: "body past what the row will hold",
			input: CreateMemoryCandidateInput{
				SessionID: sessionID,
				Kind:      MemoryCandidateKindBelief,
				Body:      strings.Repeat("a", maxMemoryCandidateBodyRunes+1),
			},
			want: ErrMemoryCandidateBody,
		},
		{
			name: "too many evidence refs",
			input: CreateMemoryCandidateInput{
				SessionID:    sessionID,
				Kind:         MemoryCandidateKindBelief,
				Body:         "body",
				EvidenceRefs: make([]string, maxMemoryEvidenceRefs+1),
			},
			want: ErrMemoryCandidateEvidence,
		},
		{
			name: "evidence ref with a control character",
			input: CreateMemoryCandidateInput{
				SessionID:    sessionID,
				Kind:         MemoryCandidateKindBelief,
				Body:         "body",
				EvidenceRefs: []string{"sess_a\nsess_b"},
			},
			want: ErrMemoryCandidateEvidence,
		},
		{
			name: "empty evidence ref",
			input: CreateMemoryCandidateInput{
				SessionID:    sessionID,
				Kind:         MemoryCandidateKindBelief,
				Body:         "body",
				EvidenceRefs: []string{""},
			},
			want: ErrMemoryCandidateEvidence,
		},
	}
	for _, test := range tests {
		_, err := repo.CreateMemoryCandidate(ctx(), test.input)
		if err == nil || !strings.Contains(err.Error(), test.want.Error()) {
			t.Fatalf("%s: error = %v, want %v", test.name, err, test.want)
		}
	}
	if names := inboxEntries(t, vault); len(names) != 0 {
		t.Fatalf("refused inputs still wrote %v", names)
	}
}

// A candidate is session state. Deleting the conversation that proposed it
// takes the row with it, and the evidence refs stored beside it.
func TestMemoryCandidatesCascadeWithTheirSession(t *testing.T) {
	repo, _, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	if _, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "Prefers dark mode",
		Body:      "The user asked for dark mode twice.",
	}); err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	if err := repo.DeleteSession(ctx(), sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	var candidates int
	if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM memory_candidates`).Scan(&candidates); err != nil {
		t.Fatalf("count candidates: %v", err)
	}
	if candidates != 0 {
		t.Fatalf("candidate rows after session deletion = %d, want 0", candidates)
	}
}

// A candidate is unreviewed model output about the user. It must never turn up
// in a search over their memory, before or after the index is refreshed.
func TestMemoryCandidatesAreNeverSearchable(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	if _, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "Marmalade",
		Body:      "The user despises marmalade.",
	}); err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	for _, refresh := range []bool{false, true} {
		if refresh {
			if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
				t.Fatalf("RefreshMemoryIndex: %v", err)
			}
		}
		hits, err := repo.SearchMemoryNotes(ctx(), "marmalade", 10)
		if err != nil {
			t.Fatalf("SearchMemoryNotes: %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("candidate content is searchable (refreshed=%v): %+v", refresh, hits)
		}
	}
}

func TestMemoryCandidateTransitionsRefuseIllegalMoves(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "Prefers dark mode",
		Body:      "The user asked for dark mode twice.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	for _, state := range []string{"pending", "archived", "", MemoryCandidateStatePromoted} {
		if _, err := repo.TransitionMemoryCandidate(ctx(), candidate.CandidateID, state); err == nil {
			t.Fatalf("transition to %q was accepted", state)
		}
	}

	withdrawn, err := repo.TransitionMemoryCandidate(ctx(), candidate.CandidateID, MemoryCandidateStateWithdrawn)
	if err != nil {
		t.Fatalf("TransitionMemoryCandidate(withdrawn): %v", err)
	}
	if withdrawn.State != MemoryCandidateStateWithdrawn || withdrawn.DecidedAt == "" {
		t.Fatalf("withdrawn candidate is not decided: %+v", withdrawn)
	}
	// Withdrawal is terminal: a decided candidate cannot be reopened or
	// re-decided into something else.
	for _, state := range []string{MemoryCandidateStateWithdrawn, MemoryCandidateStateRejected, MemoryCandidateStatePending} {
		if _, err := repo.TransitionMemoryCandidate(ctx(), candidate.CandidateID, state); err == nil {
			t.Fatalf("transition from withdrawn to %q was accepted", state)
		}
	}
	if _, err := repo.TransitionMemoryCandidate(ctx(), "memcand_missing", MemoryCandidateStateWithdrawn); err == nil {
		t.Fatalf("transitioning an unknown candidate was accepted")
	}
}

func TestListMemoryCandidatesIsScopedAndBounded(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	first := newMemoryTestSession(t, repo)
	second := newMemoryTestSession(t, repo)
	for _, sessionID := range []string{first, first, second} {
		if _, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
			SessionID: sessionID,
			Kind:      MemoryCandidateKindBelief,
			Title:     "title",
			Body:      "body",
		}); err != nil {
			t.Fatalf("CreateMemoryCandidate: %v", err)
		}
	}

	pending, err := repo.ListMemoryCandidates(ctx(), MemoryCandidateQuery{State: MemoryCandidateStatePending, Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("pending candidates = %d, want 3", len(pending))
	}
	scoped, err := repo.ListMemoryCandidates(ctx(), MemoryCandidateQuery{SessionID: second, Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryCandidates scoped: %v", err)
	}
	if len(scoped) != 1 || scoped[0].SourceSessionID != second {
		t.Fatalf("scoped listing = %+v", scoped)
	}
	for _, limit := range []int{0, -1, maxMemoryCandidateListLimit + 1} {
		if _, err := repo.ListMemoryCandidates(ctx(), MemoryCandidateQuery{Limit: limit}); err == nil {
			t.Fatalf("limit %d was accepted", limit)
		}
	}
	if _, err := repo.ListMemoryCandidates(ctx(), MemoryCandidateQuery{State: "archived", Limit: 5}); err == nil {
		t.Fatalf("unknown state filter was accepted")
	}
}
