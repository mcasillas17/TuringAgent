package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// noteStatus reads the one column the withdrawal is about.
func noteStatus(t *testing.T, repo *Repository, noteID string) string {
	t.Helper()
	var status string
	if err := repo.db.QueryRowContext(ctx(), `SELECT status FROM memory_notes WHERE id = ?`, noteID).Scan(&status); err != nil {
		t.Fatalf("read status of %q: %v", noteID, err)
	}
	return status
}

// overfillVault pushes the vault past the scan bound, so every whole-vault pass
// over it refuses. It is stated as a precondition rather than assumed, so the
// test cannot silently degrade into one over an ordinary vault.
func overfillVault(t *testing.T, vault *memoryfiles.Vault) {
	t.Helper()
	beliefs := filepath.Join(vault.Root(), memoryfiles.BeliefsDirName)
	for index := range memoryfiles.MaxVaultIndexedFiles + 1 {
		name := filepath.Join(beliefs, fmt.Sprintf("filler-%05d.md", index))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatalf("seed filler note %d: %v", index, err)
		}
	}
	if _, err := vault.Scan(ctx()); !errors.Is(err, memoryfiles.ErrVaultTooLarge) {
		t.Fatalf("the fixture is not over the index bound: %v", err)
	}
}

// Withdrawal is a promise about the database, and it cannot be owed to a
// filesystem pass that may never run.
//
// A belief rests on one conversation. The user deletes that conversation while
// their vault is past the scan bound, so the completion pass refuses before it
// can rewrite a single file. The rows still go — that half of the withdrawal is
// never held hostage to a folder — and the claim they grounded must go with
// them: search must not answer with it, and a model still holding its identity
// must not be able to read it back.
func TestAdvanceSessionDeletionWithdrawsBeliefsWhoseLastEvidenceItRemoves(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	note := mustPromoteTestBelief(t, repo, sessionID, "Bees", "The user keeps bees.")
	overfillVault(t, vault)

	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	receipt, err := repo.AdvanceSessionDeletion(ctx(), sessionID, reconcileCompletion(repo))
	if err != nil {
		t.Fatalf("AdvanceSessionDeletion: %v", err)
	}
	// The rows are the half of the promise the user asked for, and they are
	// gone whatever the vault said.
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID); got != 0 {
		t.Fatalf("session rows after the cascade = %d, want 0 (receipt %+v)", got, receipt)
	}

	if status := noteStatus(t, repo, note.NoteID); status != MemoryNoteStatusWithdrawn {
		t.Fatalf("belief status after its only conversation was deleted = %q, want %q",
			status, MemoryNoteStatusWithdrawn)
	}
	found, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("search over a withdrawn belief returned %d notes, want none", len(found))
	}
	if _, err := repo.ReadMemoryBelief(ctx(), note.NoteID); !errors.Is(err, ErrMemoryNoteWithdrawn) {
		t.Fatalf("ReadMemoryBelief after the withdrawal = %v, want ErrMemoryNoteWithdrawn", err)
	}
}

// A claim two conversations support does not fall over because one of them was
// deleted. It is withdrawn when the last one is, and not before — withdrawing
// early would take a grounded belief away from the user over a conversation
// they were entitled to delete.
func TestAdvanceSessionDeletionKeepsABeliefAnotherConversationStillSupports(t *testing.T) {
	repo, _, database := newMemoryTestRepo(t)
	first := newMemoryTestSession(t, repo)
	second := newMemoryTestSession(t, repo)
	note := mustPromoteTestBelief(t, repo, first, "Bees", "The user keeps bees.")
	if _, err := database.ExecContext(ctx(), `
		INSERT INTO memory_evidence (id, note_id, session_id, excerpt_hash, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, "memev-second-source", note.NoteID, second, "sha256:excerpt", now()); err != nil {
		t.Fatalf("insert the second conversation's evidence: %v", err)
	}

	if _, err := repo.BeginSessionDeletion(ctx(), first); err != nil {
		t.Fatalf("BeginSessionDeletion(first): %v", err)
	}
	if _, err := repo.AdvanceSessionDeletion(ctx(), first, nil); err != nil {
		t.Fatalf("AdvanceSessionDeletion(first): %v", err)
	}
	if status := noteStatus(t, repo, note.NoteID); status != MemoryNoteStatusManaged {
		t.Fatalf("belief status while a second conversation still supports it = %q, want %q",
			status, MemoryNoteStatusManaged)
	}
	found, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("search over a still-supported belief returned %d notes, want 1", len(found))
	}

	if _, err := repo.BeginSessionDeletion(ctx(), second); err != nil {
		t.Fatalf("BeginSessionDeletion(second): %v", err)
	}
	if _, err := repo.AdvanceSessionDeletion(ctx(), second, nil); err != nil {
		t.Fatalf("AdvanceSessionDeletion(second): %v", err)
	}
	if status := noteStatus(t, repo, note.NoteID); status != MemoryNoteStatusWithdrawn {
		t.Fatalf("belief status after the last conversation was deleted = %q, want %q",
			status, MemoryNoteStatusWithdrawn)
	}
}

// The withdrawal and the cascade are one transaction or they are nothing. A
// failure between them must leave the belief exactly as supported as it was —
// a note marked withdrawn beside the evidence that still grounds it is a claim
// the user accepted, taken away for no reason anything can point at.
func TestAdvanceSessionDeletionRollsBackTheWithdrawalWithTheCascade(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	note := mustPromoteTestBelief(t, repo, sessionID, "Bees", "The user keeps bees.")

	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	failure := errors.New("crash between the withdrawal and the cascade")
	repo.memoryDeletionWithdrawalBarrier = func() error { return failure }
	if _, err := repo.AdvanceSessionDeletion(ctx(), sessionID, nil); !errors.Is(err, failure) {
		t.Fatalf("AdvanceSessionDeletion error = %v, want the barrier failure", err)
	}
	repo.memoryDeletionWithdrawalBarrier = nil

	if status := noteStatus(t, repo, note.NoteID); status != MemoryNoteStatusManaged {
		t.Fatalf("belief status after a rolled-back withdrawal = %q, want %q",
			status, MemoryNoteStatusManaged)
	}
	if got := countRows(t, repo,
		`SELECT COUNT(*) FROM memory_evidence WHERE note_id = ? AND session_id = ?`,
		note.NoteID, sessionID,
	); got != 1 {
		t.Fatalf("evidence rows after a rolled-back withdrawal = %d, want 1", got)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID); got != 1 {
		t.Fatalf("session rows after a rolled-back withdrawal = %d, want 1", got)
	}
}

// The withdrawal is auditable, and the audit row says which note and why —
// never a word of what the note or the conversation said.
func TestAdvanceSessionDeletionRecordsARedactedWithdrawalAudit(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	note := mustPromoteTestBelief(t, repo, sessionID, "Bees", "The user keeps bees.")

	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	if _, err := repo.AdvanceSessionDeletion(ctx(), sessionID, nil); err != nil {
		t.Fatalf("AdvanceSessionDeletion: %v", err)
	}

	var payload string
	if err := repo.db.QueryRowContext(ctx(), `
		SELECT payload_json FROM audit_logs
		WHERE action = ? AND target = ?
	`, memoryNoteWithdrawnAction, note.NoteID).Scan(&payload); err != nil {
		t.Fatalf("read the withdrawal audit row: %v", err)
	}
	if payload != `{"status":"evidence_gone"}` {
		t.Fatalf("withdrawal audit payload = %q, want the reason and nothing else", payload)
	}
	for _, secret := range []string{"Bees", "keeps bees", sessionID} {
		if strings.Contains(payload, secret) {
			t.Fatalf("withdrawal audit payload %q leaked %q", payload, secret)
		}
	}
}

// A second advance over a withdrawal whose completion failed must not write a
// second audit row. The note was withdrawn once; a log that counts retries as
// withdrawals is one nobody can read the real events out of.
func TestAdvanceSessionDeletionWithdrawsEachBeliefOnlyOnce(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	note := mustPromoteTestBelief(t, repo, sessionID, "Bees", "The user keeps bees.")
	overfillVault(t, vault)

	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	for attempt := range 2 {
		if _, err := repo.AdvanceSessionDeletion(ctx(), sessionID, reconcileCompletion(repo)); err != nil {
			t.Fatalf("AdvanceSessionDeletion attempt %d: %v", attempt, err)
		}
	}

	if got := countRows(t, repo, `
		SELECT COUNT(*) FROM audit_logs WHERE action = ? AND target = ?
	`, memoryNoteWithdrawnAction, note.NoteID); got != 1 {
		t.Fatalf("withdrawal audit rows after two advances = %d, want 1", got)
	}
}
