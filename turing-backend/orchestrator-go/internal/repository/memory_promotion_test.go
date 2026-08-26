package repository

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

func pendingBeliefCandidate(t *testing.T, repo *Repository, sessionID string) MemoryCandidate {
	t.Helper()
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "bees",
		Body:      "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	return candidate
}

func TestPromoteMemoryCandidateMovesTheFileAndConsumesTheRow(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate := pendingBeliefCandidate(t, repo, sessionID)

	note, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatalf("PromoteMemoryCandidate: %v", err)
	}
	if !strings.HasPrefix(note.Path, memoryfiles.BeliefsDirName+"/") {
		t.Fatalf("promoted note path = %q", note.Path)
	}
	if note.Status != MemoryNoteStatusManaged {
		t.Fatalf("status = %q, want managed", note.Status)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(note.Path))); err != nil {
		t.Fatalf("stat the promoted belief: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the candidate file is still in the inbox: %v", err)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("candidate row error = %v, want it consumed", err)
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("reservations after promotion = %+v, want the reservation released", artifacts)
	}
	if got := evidenceSessions(t, repo, note.NoteID); len(got) != 1 || got[0] != sessionID {
		t.Fatalf("evidence = %v, want the live citation copied", got)
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 1 || hits[0].NoteID != note.NoteID {
		t.Fatalf("promoted belief is not searchable: %+v", hits)
	}
}

// Provenance is server-derived: a candidate cites exactly the conversation
// that produced it, and a promotion copies that citation only while the
// conversation still exists.
func TestPromoteMemoryCandidateCopiesOnlyTheLiveSourceSession(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	stranger := newMemoryTestSession(t, repo)
	candidate := pendingBeliefCandidate(t, repo, sessionID)

	note, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatalf("PromoteMemoryCandidate: %v", err)
	}
	got := evidenceSessions(t, repo, note.NoteID)
	if len(got) != 1 || got[0] != sessionID {
		t.Fatalf("evidence = %v, want only the conversation that produced the claim", got)
	}
	for _, session := range got {
		if session == stranger {
			t.Fatalf("a promotion grounded a belief in an unrelated conversation")
		}
	}
	if note.Status != MemoryNoteStatusManaged {
		t.Fatalf("status = %q, want managed while the source conversation survives", note.Status)
	}
}

func TestPromoteMemoryCandidateRefusesWhatItMayNotPromote(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)

	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: "memcand_missing"}); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("unknown candidate error = %v, want ErrMemoryCandidateNotFound", err)
	}

	profileEdit, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindProfileEdit,
		Title:     "profile",
		Body:      "The user is a beekeeper.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: profileEdit.CandidateID}); !errors.Is(err, ErrMemoryCandidateKind) {
		t.Fatalf("profile edit promotion error = %v, want ErrMemoryCandidateKind", err)
	}

	withdrawn := pendingBeliefCandidate(t, repo, sessionID)
	if _, err := repo.WithdrawMemoryCandidate(ctx(), withdrawn.CandidateID); err != nil {
		t.Fatalf("WithdrawMemoryCandidate: %v", err)
	}
	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: withdrawn.CandidateID}); !errors.Is(err, ErrMemoryCandidateInvalidTransition) {
		t.Fatalf("withdrawn promotion error = %v, want ErrMemoryCandidateInvalidTransition", err)
	}
}

// The window a promotion cannot close on its own: the file has moved and the
// transaction that would record it fails. What it must leave behind is a state
// reconcile can finish — never a belief in the vault that nothing knows about.
func TestPromotionThatFailsAfterTheFileMovedIsHealable(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate := pendingBeliefCandidate(t, repo, sessionID)

	failure := errors.New("the database went away")
	repo.memoryPromotionBarrier = func() error { return failure }
	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, failure) {
		t.Fatalf("PromoteMemoryCandidate error = %v, want the injected failure", err)
	}
	repo.memoryPromotionBarrier = nil

	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the candidate file survived the move: %v", err)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("the candidate row was consumed by a failed promotion: %v", err)
	}

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if report.NotesHealed != 1 || report.OrphanCandidatesRemoved != 1 || report.ReservationsCleared != 1 {
		t.Fatalf("reconcile did not converge: %+v", report)
	}
	hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("the healed belief is not searchable: %+v", hits)
	}
	if got := evidenceSessions(t, repo, hits[0].NoteID); len(got) != 1 || got[0] != sessionID {
		t.Fatalf("healed evidence = %v", got)
	}
}

func TestApplyMemoryProfileCandidateWritesProfileAndConsumesTheRow(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindProfileEdit,
		Title:     "profile",
		Body:      "The user is a beekeeper.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	document, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID: candidate.CandidateID,
		Content:     "# Profile\n\nThe user is a beekeeper.\n",
	})
	if err != nil {
		t.Fatalf("ApplyMemoryProfileCandidate: %v", err)
	}
	if document.RelPath != memoryfiles.ProfileFileName {
		t.Fatalf("profile path = %q", document.RelPath)
	}
	if got := readVaultNote(t, vault, memoryfiles.ProfileFileName); !strings.Contains(got, "beekeeper") {
		t.Fatalf("profile content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the applied candidate is still in the inbox: %v", err)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("candidate row error = %v, want it consumed", err)
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("reservations after applying = %+v", artifacts)
	}
	// The profile is a pinned document, not a note: it never enters the index.
	hits, err := repo.SearchMemoryNotes(ctx(), "beekeeper", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("the profile reached the note index: %+v", hits)
	}
}

// The compare-and-set is the user's protection against two edits racing: a
// stale expectation is refused, and the candidate stays reviewable.
func TestApplyMemoryProfileCandidateRefusesStaleAndWrongKind(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	writeVaultNote(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWritten already.\n")
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindProfileEdit,
		Title:     "profile",
		Body:      "The user is a beekeeper.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID:         candidate.CandidateID,
		ExpectedContentHash: memoryfiles.ContentHash("something else"),
		Content:             "# Profile\n\nThe user is a beekeeper.\n",
	}); !errors.Is(err, memoryfiles.ErrStaleContent) {
		t.Fatalf("stale apply error = %v, want ErrStaleContent", err)
	}
	if got := readVaultNote(t, vault, memoryfiles.ProfileFileName); !strings.Contains(got, "Written already") {
		t.Fatalf("a refused apply changed the profile: %q", got)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); err != nil {
		t.Fatalf("a refused apply consumed the candidate: %v", err)
	}

	belief := pendingBeliefCandidate(t, repo, sessionID)
	if _, err := repo.ApplyMemoryProfileCandidate(ctx(), ApplyMemoryProfileInput{
		CandidateID: belief.CandidateID,
		Content:     "# Profile\n",
	}); !errors.Is(err, ErrMemoryCandidateKind) {
		t.Fatalf("belief apply error = %v, want ErrMemoryCandidateKind", err)
	}
}

func TestRejectMemoryCandidateRemovesRowAndFile(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate := pendingBeliefCandidate(t, repo, sessionID)

	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); err != nil {
		t.Fatalf("RejectMemoryCandidate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the rejected candidate file survived: %v", err)
	}
	if _, err := repo.MemoryCandidateByID(ctx(), candidate.CandidateID); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("candidate row error = %v, want it consumed", err)
	}
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("reservations after rejection = %+v", artifacts)
	}
	var notes int
	if hits, err := repo.SearchMemoryNotes(ctx(), "bees", 10); err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	} else {
		notes = len(hits)
	}
	if notes != 0 {
		t.Fatalf("a rejected candidate reached the index")
	}
	// Rejection is idempotent for the file but not for the row: a second
	// rejection has nothing left to decide.
	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, ErrMemoryCandidateNotFound) {
		t.Fatalf("second rejection error = %v, want ErrMemoryCandidateNotFound", err)
	}
}

// Every deletion goes through the inbox-only primitive. A row whose path has
// been tampered with cannot be used to delete a belief the user accepted.
func TestRejectMemoryCandidateCannotDeleteOutsideTheInbox(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate := pendingBeliefCandidate(t, repo, sessionID)
	noteID := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/precious.md", managedBelief(noteID, nil, "The user keeps bees."))

	if _, err := database.ExecContext(ctx(), `
		UPDATE memory_candidates SET inbox_path = ? WHERE id = ?
	`, "beliefs/precious.md", candidate.CandidateID); err != nil {
		t.Fatalf("tamper with the candidate path: %v", err)
	}

	if err := repo.RejectMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, ErrVaultArtifactPathScope) {
		t.Fatalf("tampered rejection error = %v, want ErrVaultArtifactPathScope", err)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), "beliefs", "precious.md")); err != nil {
		t.Fatalf("the belief was deleted through a tampered candidate row: %v", err)
	}
	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, ErrVaultArtifactPathScope) {
		t.Fatalf("tampered promotion error = %v, want ErrVaultArtifactPathScope", err)
	}
}

// Every decision that consumes a candidate records one audit row, and that row
// carries the shape of what happened and nothing the candidate claimed: no
// body, no title, no path.
func TestCandidateDecisionsRecordRedactedAuditRows(t *testing.T) {
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
	withdrawn := pendingBeliefCandidate(t, repo, sessionID)
	if _, err := repo.WithdrawMemoryCandidate(ctx(), withdrawn.CandidateID); err != nil {
		t.Fatalf("WithdrawMemoryCandidate: %v", err)
	}

	decisions := map[string]MemoryCandidate{
		MemoryCandidateStatePromoted:  promoted,
		MemoryCandidateStateRejected:  rejected,
		MemoryCandidateStateWithdrawn: withdrawn,
	}
	for state, candidate := range decisions {
		var action, payload string
		if err := database.QueryRowContext(ctx(), `
			SELECT action, payload_json FROM audit_logs WHERE target = ?
		`, candidate.CandidateID).Scan(&action, &payload); err != nil {
			t.Fatalf("audit row for the %s candidate: %v", state, err)
		}
		if !strings.HasPrefix(action, "memory.candidate.") {
			t.Fatalf("action = %q", action)
		}
		if !strings.Contains(payload, state) || !strings.Contains(payload, MemoryCandidateKindBelief) {
			t.Fatalf("payload = %q, want the kind and the decision", payload)
		}
		for _, secret := range []string{"bees", candidate.InboxPath, candidate.SourceSessionID} {
			if strings.Contains(payload, secret) {
				t.Fatalf("payload %q leaked %q", payload, secret)
			}
		}
	}
}

// An index refresh can heal a belief in the window between a promotion moving
// the file and the transaction that records it. Both then link the same
// citation, so linking has to be idempotent — otherwise one promotion leaves
// two evidence rows for one conversation, and every count of what a memory
// rests on is wrong from then on.
func TestPromotionRacingAnIndexRefreshLinksEachCitationOnce(t *testing.T) {
	repo, _, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate := pendingBeliefCandidate(t, repo, sessionID)

	repo.memoryPromotionBarrier = func() error {
		_, err := repo.RefreshMemoryIndex(ctx())
		return err
	}
	note, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID})
	repo.memoryPromotionBarrier = nil
	if err != nil {
		t.Fatalf("PromoteMemoryCandidate: %v", err)
	}

	var rows int
	if err := database.QueryRowContext(ctx(), `
		SELECT COUNT(*) FROM memory_evidence WHERE note_id = ? AND session_id = ?
	`, note.NoteID, sessionID).Scan(&rows); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if rows != 1 {
		t.Fatalf("evidence rows = %d, want exactly one per citation", rows)
	}
}

// The excerpt hash is meant to be a non-reversible fingerprint of what the
// citation supports. Hashing the session id instead makes it a fingerprint of
// the conversation — which is already stored in the row beside it in plain
// text, so it says nothing new, and says nothing at all about the claim it is
// supposed to stand behind.
func TestEvidenceExcerptHashDigestsTheSupportedContentNotTheSession(t *testing.T) {
	repo, _, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate := pendingBeliefCandidate(t, repo, sessionID)

	note, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatalf("PromoteMemoryCandidate: %v", err)
	}
	var excerptHash string
	if err := database.QueryRowContext(ctx(), `
		SELECT excerpt_hash FROM memory_evidence WHERE note_id = ? AND session_id = ?
	`, note.NoteID, sessionID).Scan(&excerptHash); err != nil {
		t.Fatalf("read excerpt hash: %v", err)
	}
	if excerptHash != note.ContentHash {
		t.Fatalf("excerpt hash = %q, want the digest of the content it supports (%q)", excerptHash, note.ContentHash)
	}
	if excerptHash == memoryfiles.ContentHash(sessionID) {
		t.Fatalf("excerpt hash is a digest of the conversation id, not of the claim")
	}
	if strings.Contains(excerptHash, sessionID) || strings.Contains(excerptHash, "bees") {
		t.Fatalf("excerpt hash %q is not a hash", excerptHash)
	}

	// The same must hold for a belief reconcile heals from the file: the hash
	// describes the note that exists, not the conversation that is cited.
	healed := newMemoryTestSession(t, repo)
	healedID := newTestNoteID(t)
	writeVaultNote(t, repo.memoryVault, "beliefs/healed.md", managedBelief(healedID, []string{healed}, "The user keeps chickens."))
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	healedNote, found := noteRowFor(t, repo, healedID)
	if !found {
		t.Fatalf("the hand-written belief was not healed into the index")
	}
	if err := database.QueryRowContext(ctx(), `
		SELECT excerpt_hash FROM memory_evidence WHERE note_id = ? AND session_id = ?
	`, healedID, healed).Scan(&excerptHash); err != nil {
		t.Fatalf("read healed excerpt hash: %v", err)
	}
	if excerptHash != healedNote.ContentHash {
		t.Fatalf("healed excerpt hash = %q, want %q", excerptHash, healedNote.ContentHash)
	}
}

// The status a promotion writes is a decision about support, not about the
// file: a belief whose every citation names a conversation that is already
// gone is still accepted — the user said yes — but it may not be answered with
// as if it were still grounded in something they can go and read.
//
// The rule is tested directly because the promotion path reaches it only when
// a caller supplies refs this package does not construct today. Reachable or
// not, deleting the rule must break a test.
func TestPromotedNoteStatusWithdrawsAClaimWhoseEveryCitationIsGone(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		refs     []string
		live     []string
		expected string
	}{
		{name: "one citation, nothing live", refs: []string{"session_a"}, live: nil, expected: MemoryNoteStatusWithdrawn},
		{name: "several citations, nothing live", refs: []string{"session_a", "session_b"}, live: []string{}, expected: MemoryNoteStatusWithdrawn},
		{name: "the cited conversation is still there", refs: []string{"session_a"}, live: []string{"session_a"}, expected: MemoryNoteStatusManaged},
		{name: "one of two survives", refs: []string{"session_a", "session_b"}, live: []string{"session_b"}, expected: MemoryNoteStatusManaged},
		{name: "nothing was cited, so nothing was lost", refs: nil, live: nil, expected: MemoryNoteStatusManaged},
		{name: "an empty citation list is not a withdrawal", refs: []string{}, live: []string{}, expected: MemoryNoteStatusManaged},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if status := promotedNoteStatus(testCase.refs, testCase.live); status != testCase.expected {
				t.Fatalf("a promotion citing %v with %v still live was marked %q, want %q", testCase.refs, testCase.live, status, testCase.expected)
			}
		})
	}
}
