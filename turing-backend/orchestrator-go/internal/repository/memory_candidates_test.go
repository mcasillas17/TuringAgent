package repository

import (
	"context"
	"errors"
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
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "Prefers dark mode",
		Body:      "The user asked for dark mode twice.",
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
			name: "multibyte body past the candidate limit",
			input: CreateMemoryCandidateInput{
				SessionID: sessionID,
				Kind:      MemoryCandidateKindBelief,
				Body:      strings.Repeat("a", memoryfiles.MaxCandidateBodyBytes-1) + "é",
			},
			want: ErrMemoryCandidateBody,
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
	if err := repo.DeleteSessionForTests(ctx(), sessionID); err != nil {
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

// There is deliberately no generic transition. A caller that could name the
// state a candidate moves to could mark one promoted or rejected while its file
// is still sitting in the inbox and its row still claims to be a live proposal
// — a decision the user never sees, on a claim they never reviewed. The two
// decisions that consume a candidate go through PromoteMemoryCandidate,
// ApplyMemoryProfileCandidate and RejectMemoryCandidate, each of which moves
// the file first; withdrawal is the one move that keeps the row, and it is its
// own method.
func TestWithdrawMemoryCandidateIsTheOnlyTransitionAndIsAuditedExactlyOnce(t *testing.T) {
	repo, _, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate := pendingBeliefCandidate(t, repo, sessionID)

	withdrawn, err := repo.WithdrawMemoryCandidate(ctx(), candidate.CandidateID)
	if err != nil {
		t.Fatalf("WithdrawMemoryCandidate: %v", err)
	}
	if withdrawn.State != MemoryCandidateStateWithdrawn || withdrawn.DecidedAt == "" {
		t.Fatalf("withdrawn candidate is not decided: %+v", withdrawn)
	}

	// Withdrawal is terminal. A second attempt changes no row, so it must
	// refuse — and must not leave a second audit row saying a decision was
	// taken when nothing moved.
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := repo.WithdrawMemoryCandidate(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateInvalidTransition) {
			t.Fatalf("second withdrawal error = %v, want ErrMemoryCandidateInvalidTransition", err)
		}
	}
	var audits int
	if err := database.QueryRowContext(ctx(), `
		SELECT COUNT(*) FROM audit_logs WHERE target = ?
	`, candidate.CandidateID).Scan(&audits); err != nil {
		t.Fatalf("count decision audits: %v", err)
	}
	if audits != 1 {
		t.Fatalf("decision audit rows = %d, want exactly the one decision that changed a row", audits)
	}
	if _, err := repo.WithdrawMemoryCandidate(ctx(), "memcand_missing"); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("withdrawing an unknown candidate error = %v, want ErrMemoryCandidateNotFound", err)
	}
}

// Every decision that leaves a candidate decided has also moved or removed its
// file. A row sitting in 'promoted' or 'rejected' would describe an inbox entry
// that is gone — a phantom the cleaner and the review list would both believe.
func TestNoDecisionLeavesADecidedCandidateRowBehind(t *testing.T) {
	repo, _, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	promoted := pendingBeliefCandidate(t, repo, sessionID)
	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: promoted.CandidateID}); err != nil {
		t.Fatalf("PromoteMemoryCandidate: %v", err)
	}
	rejected := pendingBeliefCandidate(t, repo, sessionID)
	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: rejected.CandidateID}); err != nil {
		t.Fatalf("RejectMemoryCandidate: %v", err)
	}

	var decided int
	if err := database.QueryRowContext(ctx(), `
		SELECT COUNT(*) FROM memory_candidates WHERE state IN (?, ?)
	`, MemoryCandidateStatePromoted, MemoryCandidateStateRejected).Scan(&decided); err != nil {
		t.Fatalf("count decided candidates: %v", err)
	}
	if decided != 0 {
		t.Fatalf("rows left in a consumed state = %d, want every decision to consume its row", decided)
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

// The bound on a claim is UTF-8 bytes and only UTF-8 bytes. A separate,
// smaller character bound would refuse a body the vault would happily hold —
// and the user would be told their claim is too long while a shorter-looking
// one in another language sails through.
func TestCreateMemoryCandidateBoundsTheBodyInBytesNotCharacters(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	for _, accepted := range []struct {
		name string
		body string
	}{
		{"exactly the byte bound", strings.Repeat("a", memoryfiles.MaxCandidateBodyBytes)},
		{"more characters than the old character bound", strings.Repeat("a", 4097)},
		{"the byte bound in two-byte runes", strings.Repeat("é", memoryfiles.MaxCandidateBodyBytes/2)},
		{"a multibyte rune ending on the bound", strings.Repeat("a", memoryfiles.MaxCandidateBodyBytes-2) + "é"},
	} {
		t.Run(accepted.name, func(t *testing.T) {
			candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
				SessionID: sessionID,
				Kind:      MemoryCandidateKindBelief,
				Title:     "bound",
				Body:      accepted.body,
			})
			if err != nil {
				t.Fatalf("a %d-byte body was refused: %v", len(accepted.body), err)
			}
			if candidate.Body != accepted.body {
				t.Fatalf("the stored body is not the claim that was made (%d bytes stored, %d written)",
					len(candidate.Body), len(accepted.body))
			}
		})
	}
	for _, refused := range []struct {
		name string
		body string
	}{
		{"one byte past the bound", strings.Repeat("a", memoryfiles.MaxCandidateBodyBytes+1)},
		{"a multibyte rune straddling the bound", strings.Repeat("a", memoryfiles.MaxCandidateBodyBytes-1) + "é"},
	} {
		t.Run(refused.name, func(t *testing.T) {
			if _, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
				SessionID: sessionID,
				Kind:      MemoryCandidateKindBelief,
				Title:     "bound",
				Body:      refused.body,
			}); !errors.Is(err, ErrMemoryCandidateBody) {
				t.Fatalf("a %d-byte body error = %v, want ErrMemoryCandidateBody", len(refused.body), err)
			}
		})
	}
}

// The window between reserving a path and recording the candidate: the session
// is deleted while the creation is in flight. The reservation cascades away,
// the insert has no session to hang off, and the bytes that reached the vault a
// moment earlier are now untracked — nothing in the manifest names them and no
// later pass can discover what it has no record of.
//
// What must be left behind is nothing at all: the file is removed again through
// the inbox-only primitive, no candidate row exists, no manifest row exists,
// and the deletion the user asked for can complete.
func TestCreateMemoryCandidateRemovesTheFileWhenTheSessionVanishesMidWrite(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	repo.memoryCandidateWriteBarrier = func() error {
		return repo.DeleteSessionForTests(ctx(), sessionID)
	}
	_, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "Prefers dark mode",
		Body:      "The user asked for dark mode twice.",
	})
	repo.memoryCandidateWriteBarrier = nil
	if err == nil {
		t.Fatalf("CreateMemoryCandidate succeeded for a session that was deleted mid-write")
	}

	if names := inboxEntries(t, vault); len(names) != 0 {
		t.Fatalf("an untracked file was left in the user's vault: %v", names)
	}
	for _, table := range []string{"memory_candidates", "vault_artifacts"} {
		var rows int
		if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM `+table).Scan(&rows); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if rows != 0 {
			t.Fatalf("%s rows = %d, want the cascade to have taken them", table, rows)
		}
	}
	var sessions int
	if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("session rows = %d, want the deletion the user asked for to have completed", sessions)
	}
}

// The same window, but the reservation is still standing: only the row-writing
// half fails. The bytes must not stay in the vault on the strength of a
// transaction that did not commit, and the reservation must stay exactly as it
// is — it is the durable record that this session may have left a file behind,
// and the cleaner that finds no file simply has nothing to do.
func TestCreateMemoryCandidateRemovesTheFileWhenTheRecordFails(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	failure := errors.New("the database went away")
	repo.memoryCandidateRecordBarrier = func() error { return failure }
	_, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "Prefers dark mode",
		Body:      "The user asked for dark mode twice.",
	})
	repo.memoryCandidateRecordBarrier = nil
	if !errors.Is(err, failure) {
		t.Fatalf("CreateMemoryCandidate error = %v, want the injected failure", err)
	}

	if names := inboxEntries(t, vault); len(names) != 0 {
		t.Fatalf("a candidate no row describes was left in the vault: %v", names)
	}
	var candidates int
	if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM memory_candidates`).Scan(&candidates); err != nil {
		t.Fatalf("count candidates: %v", err)
	}
	if candidates != 0 {
		t.Fatalf("candidate rows = %d, want the transaction rolled back", candidates)
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].State != VaultArtifactStateWriting {
		t.Fatalf("artifacts = %+v, want the reservation left standing and retryable", artifacts)
	}

	// The reservation names a path with no file, which is exactly what the
	// cleaner must tolerate: it removes nothing and reports it removed the row.
	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	if removed != 1 {
		t.Fatalf("purged = %d, want the dangling reservation cleaned up idempotently", removed)
	}
}

// Provenance is what makes a claim withdrawable when a conversation is
// deleted, so it is derived from the conversation the run belongs to and never
// accepted from whoever is asking. A model that could name the sessions its
// claim rests on could name someone else's, and a belief would then survive —
// or be withdrawn by — a conversation it has nothing to do with.
func TestCreateMemoryCandidateDerivesEvidenceFromTheSourceSession(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	stranger := newMemoryTestSession(t, repo)

	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "bees",
		Body:      "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	if len(candidate.EvidenceRefs) != 1 || candidate.EvidenceRefs[0] != sessionID {
		t.Fatalf("evidence refs = %v, want exactly the conversation that produced the claim", candidate.EvidenceRefs)
	}

	var stored string
	if err := database.QueryRowContext(ctx(), `
		SELECT evidence_refs_json FROM memory_candidates WHERE id = ?
	`, candidate.CandidateID).Scan(&stored); err != nil {
		t.Fatalf("read stored refs: %v", err)
	}
	if !strings.Contains(stored, sessionID) || strings.Contains(stored, stranger) {
		t.Fatalf("stored refs = %q, want only %q", stored, sessionID)
	}
	content := readVaultNote(t, vault, candidate.InboxPath)
	if !strings.Contains(content, sessionID) || strings.Contains(content, stranger) {
		t.Fatalf("the candidate file cites the wrong conversation:\n%s", content)
	}
}

// A row whose citations have been edited to name another conversation is a
// forged provenance: promoting it would ground a belief in a conversation that
// never produced it, and deleting that conversation would then withdraw a claim
// it had nothing to do with. Every decision refuses it before touching a file.
func TestMemoryDecisionsRefuseForgedEvidence(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	stranger := newMemoryTestSession(t, repo)
	candidate := pendingBeliefCandidate(t, repo, sessionID)

	// A stranger's session id, and an emptied list. The second is the quieter
	// forgery: a claim with no provenance at all promotes as grounded memory
	// that no conversation can ever withdraw, because nothing links it to one.
	for _, forgery := range []string{`["` + stranger + `"]`, `[]`} {
		if _, err := database.ExecContext(ctx(), `
			UPDATE memory_candidates SET evidence_refs_json = ? WHERE id = ?
		`, forgery, candidate.CandidateID); err != nil {
			t.Fatalf("forge the candidate's provenance: %v", err)
		}
		if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, ErrMemoryCandidateEvidence) {
			t.Fatalf("promotion of %s error = %v, want ErrMemoryCandidateEvidence", forgery, err)
		}
		if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, ErrMemoryCandidateEvidence) {
			t.Fatalf("rejection of %s error = %v, want ErrMemoryCandidateEvidence", forgery, err)
		}
	}
	var notes int
	if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM memory_notes`).Scan(&notes); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	if notes != 0 {
		t.Fatalf("note rows = %d, want a forged candidate to reach nothing", notes)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
		t.Fatalf("a refused decision still moved the candidate file: %v", err)
	}
	var evidence int
	if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM memory_evidence`).Scan(&evidence); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if evidence != 0 {
		t.Fatalf("evidence rows = %d, want a forged citation to reach nothing", evidence)
	}
}

// Stored provenance that cannot be read is a fact about the row, not a
// detail to route around. Dropping the entries that fail to parse would let a
// poisoned row promote with less provenance than it claims — a belief the user
// accepts as grounded in three conversations, silently grounded in one, and
// surviving the deletion of the other two.
func TestPoisonedEvidenceRefsFailLoudlyInsteadOfPromotingWithLessProvenance(t *testing.T) {
	for _, poison := range []struct {
		name  string
		value string
	}{
		{"a ref that is not a string", `[42]`},
		{"a ref carrying a control character", `["sess_a\u0001b"]`},
		{"an empty ref", `[""]`},
		{"a ref past the identifier bound", `["` + strings.Repeat("s", maxMemoryEvidenceRefBytes+1) + `"]`},
	} {
		t.Run(poison.name, func(t *testing.T) {
			repo, vault, database := newMemoryTestRepo(t)
			sessionID := newMemoryTestSession(t, repo)
			candidate := pendingBeliefCandidate(t, repo, sessionID)
			if _, err := database.ExecContext(ctx(), `
				UPDATE memory_candidates SET evidence_refs_json = ? WHERE id = ?
			`, poison.value, candidate.CandidateID); err != nil {
				t.Fatalf("poison the stored refs: %v", err)
			}

			if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateEvidence) {
				t.Fatalf("read error = %v, want ErrMemoryCandidateEvidence", err)
			}
			if _, err := repo.ListMemoryCandidates(ctx(), MemoryCandidateQuery{Limit: 10}); !errors.Is(err, ErrMemoryCandidateEvidence) {
				t.Fatalf("listing error = %v, want ErrMemoryCandidateEvidence", err)
			}
			if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, ErrMemoryCandidateEvidence) {
				t.Fatalf("promotion error = %v, want ErrMemoryCandidateEvidence", err)
			}
			if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
				t.Fatalf("a refused promotion still moved the candidate file: %v", err)
			}
			var notes int
			if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM memory_notes`).Scan(&notes); err != nil {
				t.Fatalf("count notes: %v", err)
			}
			if notes != 0 {
				t.Fatalf("note rows = %d, want a poisoned candidate to reach nothing", notes)
			}
		})
	}
}

// The reservation is gone by the time the write is confirmed — a session
// deletion cascaded it, or a sweep took it. Closing it fails, the transaction
// rolls back, and the bytes must not stay: nothing in the manifest names them,
// so no cleaner could ever find them again.
func TestCreateMemoryCandidateRemovesTheFileWhenTheReservationCannotBeClosed(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	repo.memoryCandidateRecordBarrier = func() error {
		_, err := database.ExecContext(ctx(), `DELETE FROM vault_artifacts WHERE session_id = ?`, sessionID)
		return err
	}
	_, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "bees",
		Body:      "The user keeps bees.",
	})
	repo.memoryCandidateRecordBarrier = nil
	if !errors.Is(err, ErrVaultArtifactNotFound) {
		t.Fatalf("CreateMemoryCandidate error = %v, want ErrVaultArtifactNotFound", err)
	}

	if names := inboxEntries(t, vault); len(names) != 0 {
		t.Fatalf("an unclosable reservation left %v in the vault", names)
	}
	for _, table := range []string{"memory_candidates", "vault_artifacts"} {
		var rows int
		if err := database.QueryRowContext(ctx(), `SELECT COUNT(*) FROM `+table).Scan(&rows); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if rows != 0 {
			t.Fatalf("%s rows = %d, want the transaction rolled back", table, rows)
		}
	}
}

// The row's bound and the file layer's bound are one number in two places, and
// they only stay one number if something refuses to let them drift. The
// repository would refuse an over-long body before the row ever saw it, so the
// row's own CHECK is asserted directly.
func TestTheStoredBodyBoundMatchesTheVaultsBound(t *testing.T) {
	repo, _, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	insert := func(id string, body string) error {
		_, err := database.ExecContext(ctx(), `
			INSERT INTO memory_candidates (
				id, source_session_id, kind, inbox_path, content_hash, body,
				evidence_refs_json, state, created_at, updated_at
			) VALUES (?, ?, 'belief', ?, 'hash', ?, '[]', 'pending', ?, ?)`,
			id, sessionID, "inbox/"+id+".md", body, now(), now())
		return err
	}
	if err := insert("memcand_at_bound", strings.Repeat("a", memoryfiles.MaxCandidateBodyBytes)); err != nil {
		t.Fatalf("the row refused a body the vault would accept: %v", err)
	}
	if err := insert("memcand_over_bound", strings.Repeat("a", memoryfiles.MaxCandidateBodyBytes+1)); err == nil {
		t.Fatalf("the row accepted a body the vault would refuse")
	}
	_ = repo
}
