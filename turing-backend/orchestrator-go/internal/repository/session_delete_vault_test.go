package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// reconcileCompletion is the production shape of the completion hook: the
// file-writing pass, run once the rows are gone.
func reconcileCompletion(repo *Repository) SessionDeletionCompletion {
	return func(ctx context.Context) error {
		_, err := repo.ReconcileMemoryVault(ctx)
		return err
	}
}

func seedVaultDeletableSession(t *testing.T, repo *Repository, title string, body string) (string, MemoryCandidate) {
	t.Helper()
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     title,
		Body:      body,
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	return sessionID, candidate
}

// A session that never touched the sandbox but still owns a file in the user's
// vault is not finished withdrawing. The gate has to say so in the one literal
// the dispatch gate recognises — a second vocabulary for "there are still files"
// is a withdrawal that reports completion with the note still on disk.
func TestAdvanceSessionDeletionHoldsWithdrawalForVaultOnlyPendingArtifacts(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID, _ := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")

	if got := countRows(t, repo, `SELECT COUNT(*) FROM sandbox_artifacts WHERE session_id = ?`, sessionID); got != 0 {
		t.Fatalf("sandbox rows = %d, want a vault-only session", got)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM vault_artifacts WHERE session_id = ?`, sessionID); got != 1 {
		t.Fatalf("vault rows = %d, want the candidate's file tracked", got)
	}
	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	receipt, err := repo.AdvanceSessionDeletion(ctx(), sessionID, nil)
	if err != nil {
		t.Fatalf("AdvanceSessionDeletion: %v", err)
	}
	if receipt.State != "failed_external" || !receipt.Retryable || receipt.ErrorCode != "artifact_cleanup_pending" {
		t.Fatalf("vault-only pending receipt = %+v, want retryable artifact_cleanup_pending", receipt)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID); got != 1 {
		t.Fatalf("session rows after a pending vault gate = %d, want 1", got)
	}
}

// Both manifests are counted, independently. A sandbox row alone still holds
// the withdrawal, and a session owning neither goes straight through.
func TestAdvanceSessionDeletionCountsBothArtifactManifestsIndependently(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	if _, err := repo.AdvanceSessionDeletion(ctx(), sessionID, nil); err != nil {
		t.Fatalf("AdvanceSessionDeletion: %v", err)
	}

	// Drain the vault manifest the way the cleaner does, and the gate opens.
	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.memoryVault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the candidate file survived the purge: %v", err)
	}
	receipt, err := repo.AdvanceSessionDeletion(ctx(), sessionID, nil)
	if err != nil {
		t.Fatalf("AdvanceSessionDeletion after the purge: %v", err)
	}
	if receipt.State != "completed" || receipt.ErrorCode != "" {
		t.Fatalf("drained receipt = %+v, want completed", receipt)
	}
}

// A crash between the reservation and the write leaves a row naming a file that
// never existed. Cleanup has to treat it as done rather than as a failure it
// can never retry its way out of.
func TestPurgeSessionVaultArtifactsClearsAReservationWhoseWriteNeverLanded(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	failure := errors.New("crash before the bytes land")
	repo.memoryCandidateWriteBarrier = func() error { return failure }
	if _, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "bees",
		Body:      "The user keeps bees.",
	}); !errors.Is(err, failure) {
		t.Fatalf("CreateMemoryCandidate error = %v, want the barrier failure", err)
	}
	repo.memoryCandidateWriteBarrier = nil

	reserved, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(reserved) != 1 || reserved[0].State != VaultArtifactStateWriting {
		t.Fatalf("artifacts after the crash = %+v, want one writing reservation", reserved)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(reserved[0].VaultPath))); !os.IsNotExist(err) {
		t.Fatalf("the crashed reservation left a file behind: %v", err)
	}

	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts over a row with no file: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want the reservation drained", removed)
	}
	if remaining, err := repo.SessionVaultArtifacts(ctx(), sessionID); err != nil || len(remaining) != 0 {
		t.Fatalf("artifacts after the purge = (%+v, %v), want none", remaining, err)
	}
	if got := vaultArtifactAuditCount(t, database, reserved[0].ArtifactID); got != 0 {
		t.Fatalf("cleanup failure audits = %d, want none for a file that was never there", got)
	}
}

// The manifest is not a capability. A row rewritten to name a belief, the
// persona document or anything else outside the inbox is refused, and every one
// of those files is still there afterwards.
func TestPurgeSessionVaultArtifactsRefusesEveryTamperedPathOutsideTheInbox(t *testing.T) {
	protected := []string{"beliefs/precious.md", "persona.md", "profile.md"}
	for _, target := range protected {
		t.Run(target, func(t *testing.T) {
			repo, vault, _ := newMemoryTestRepo(t)
			sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
			writeVaultNote(t, vault, target, "# A file the user owns\n")
			if _, err := repo.db.ExecContext(ctx(), `
				UPDATE vault_artifacts SET vault_path = ? WHERE session_id = ? AND vault_path = ?
			`, target, sessionID, candidate.InboxPath); err != nil {
				t.Fatalf("tamper with the manifest row: %v", err)
			}

			if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); !errors.Is(err, ErrVaultArtifactPathScope) {
				t.Fatalf("purge error = %v, want ErrVaultArtifactPathScope", err)
			}
			if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(target))); err != nil {
				t.Fatalf("a tampered manifest row deleted %q: %v", target, err)
			}
		})
	}
}

// The candidate file leaves through the inbox-only primitive and nothing else,
// so the confinement check is on the path every cleanup takes rather than on a
// path a caller could route around.
func TestPurgeSessionVaultArtifactsRemovesCandidateFilesThroughTheInboxPrimitive(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	writeVaultNote(t, vault, "beliefs/kept.md", "# A belief the user accepted\n")

	// The inbox is unreachable; the belief directory is not. A cleanup that
	// reached the vault any other way would still succeed here.
	repair := breakVaultInbox(t, vault)
	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err == nil {
		repair()
		t.Fatal("PurgeSessionVaultArtifacts succeeded without the inbox")
	}
	repair()
	if _, err := os.Stat(filepath.Join(vault.Root(), "beliefs", "kept.md")); err != nil {
		t.Fatalf("the belief did not survive a failed inbox cleanup: %v", err)
	}

	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the candidate file survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), "beliefs", "kept.md")); err != nil {
		t.Fatalf("cleanup reached outside the inbox: %v", err)
	}
}

func seedSandboxArtifact(t *testing.T, repo *Repository, sessionID string, runID string, artifactID string) {
	t.Helper()
	if _, err := repo.db.ExecContext(ctx(), `
		INSERT INTO sandbox_artifacts (
			id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at
		) VALUES (?, ?, ?, ?, ?, 'ready', 'delete_on_session_delete', 1, ?)
	`, artifactID, sessionID, runID, "sha256:"+artifactID, "sessions/"+sessionID+"/files/"+artifactID+".txt", now()); err != nil {
		t.Fatalf("seed sandbox artifact: %v", err)
	}
}

func sandboxArtifactAuditCount(t *testing.T, repo *Repository, artifactID string) int {
	t.Helper()
	return countRows(t, repo, `
		SELECT COUNT(*) FROM audit_logs WHERE action = 'session.artifact.cleanup.failed' AND target = ?
	`, artifactID)
}

// A vault failure is a vault failure. Marking the sandbox rows too would file
// an audit row saying Turing could not delete a sandbox file it never tried to
// delete, and would send the next retry looking for it.
func TestMarkSessionDeletionVaultFailureMarksOnlyTheVaultManifest(t *testing.T) {
	repo, _, database := newMemoryTestRepo(t)
	sessionID, _ := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	enqueued, err := repo.EnqueueUserMessage(ctx(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "write then withdraw", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	seedSandboxArtifact(t, repo, sessionID, enqueued.RunID, "artifact_sandbox_untouched")
	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	if err := repo.MarkSessionDeletionVaultFailure(ctx(), sessionID, "vault_artifact_cleanup_failed"); err != nil {
		t.Fatalf("MarkSessionDeletionVaultFailure: %v", err)
	}

	receipt, err := repo.SessionDeletionReceipt(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionDeletionReceipt: %v", err)
	}
	if receipt.State != "failed_external" || !receipt.Retryable || receipt.ErrorCode != "vault_artifact_cleanup_failed" {
		t.Fatalf("receipt = %+v, want retryable vault_artifact_cleanup_failed", receipt)
	}
	vaultRows, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(vaultRows) != 1 || vaultRows[0].State != VaultArtifactStateDeleteFailed {
		t.Fatalf("vault rows = %+v, want one delete_failed row", vaultRows)
	}
	if got := vaultArtifactAuditCount(t, database, vaultRows[0].ArtifactID); got != 1 {
		t.Fatalf("vault cleanup audits = %d, want 1", got)
	}
	var sandboxState string
	if err := repo.db.QueryRowContext(ctx(), `SELECT state FROM sandbox_artifacts WHERE id = ?`,
		"artifact_sandbox_untouched").Scan(&sandboxState); err != nil {
		t.Fatalf("read the sandbox row: %v", err)
	}
	if sandboxState != SandboxArtifactStateReady {
		t.Fatalf("sandbox row state = %q, want it untouched by a vault failure", sandboxState)
	}
	if got := sandboxArtifactAuditCount(t, repo, "artifact_sandbox_untouched"); got != 0 {
		t.Fatalf("sandbox cleanup audits = %d, want none for a vault failure", got)
	}
}

// And the mirror: a sandbox failure never marks a vault row or files a vault
// audit for a note it never tried to remove.
func TestMarkSessionDeletionSandboxFailureMarksOnlyTheSandboxManifest(t *testing.T) {
	repo, _, database := newMemoryTestRepo(t)
	sessionID, _ := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	enqueued, err := repo.EnqueueUserMessage(ctx(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "write then withdraw", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	seedSandboxArtifact(t, repo, sessionID, enqueued.RunID, "artifact_sandbox_failed")
	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	if err := repo.MarkSessionDeletionSandboxFailure(ctx(), sessionID, "artifact_cleanup_failed"); err != nil {
		t.Fatalf("MarkSessionDeletionSandboxFailure: %v", err)
	}

	vaultRows, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(vaultRows) != 1 || vaultRows[0].State != VaultArtifactStateReady {
		t.Fatalf("vault rows = %+v, want them untouched by a sandbox failure", vaultRows)
	}
	if got := vaultArtifactAuditCount(t, database, vaultRows[0].ArtifactID); got != 0 {
		t.Fatalf("vault cleanup audits = %d, want none for a sandbox failure", got)
	}
	if got := sandboxArtifactAuditCount(t, repo, "artifact_sandbox_failed"); got != 1 {
		t.Fatalf("sandbox cleanup audits = %d, want 1", got)
	}
}

// The rows go first, because that is the promise the user made the request
// about. But the receipt may not claim the withdrawal is finished while a
// belief in their vault is still citing the conversation that just went away.
func TestAdvanceSessionDeletionStaysRetryableWhenCompletionCannotFinish(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	failure := errors.New("the vault could not be rewritten")
	receipt, err := repo.AdvanceSessionDeletion(ctx(), sessionID, func(context.Context) error { return failure })
	if err != nil {
		t.Fatalf("AdvanceSessionDeletion: %v", err)
	}
	if receipt.State != "failed_external" || !receipt.Retryable || receipt.ErrorCode != "memory_reconcile_failed" {
		t.Fatalf("receipt = %+v, want retryable memory_reconcile_failed", receipt)
	}
	if strings.Contains(receipt.ErrorCode, failure.Error()) {
		t.Fatalf("receipt error code leaked the underlying failure: %q", receipt.ErrorCode)
	}
	persisted, err := repo.SessionDeletionReceipt(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionDeletionReceipt: %v", err)
	}
	if persisted.State != "failed_external" || persisted.ErrorCode != "memory_reconcile_failed" {
		t.Fatalf("persisted receipt = %+v, want the failure durable", persisted)
	}

	retry, err := repo.AdvanceSessionDeletion(ctx(), sessionID, func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("retry AdvanceSessionDeletion: %v", err)
	}
	if retry.State != "completed" || retry.Retryable || retry.ErrorCode != "" {
		t.Fatalf("retry receipt = %+v, want completed", retry)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM audit_logs WHERE action = 'session.deleted' AND target = ?`, sessionID); got != 1 {
		t.Fatalf("session.deleted audit rows = %d, want exactly one across a retried withdrawal", got)
	}
	// And it still says how much was removed. A withdrawal that now advances
	// across more than one transaction re-runs the audit scrub on every retry;
	// scrubbing its own evidence would trade the record of what was deleted for
	// the act of finishing the deletion.
	var deletedPayload string
	if err := repo.db.QueryRowContext(ctx(), `
		SELECT payload_json FROM audit_logs WHERE action = 'session.deleted' AND target = ?
	`, sessionID).Scan(&deletedPayload); err != nil {
		t.Fatalf("read the session.deleted payload: %v", err)
	}
	if deletedPayload == scrubbedAuditPayload {
		t.Fatalf("a retried withdrawal scrubbed its own evidence: %q", deletedPayload)
	}
	if !strings.Contains(deletedPayload, `"runs"`) || !strings.Contains(deletedPayload, `"messages"`) {
		t.Fatalf("session.deleted payload = %q, want the counts intact", deletedPayload)
	}
}

// The completion runs after the rows are gone, not before: what it has to
// rewrite is a citation of a conversation that no longer exists, and it cannot
// see that while the row is still there.
func TestAdvanceSessionDeletionRunsCompletionOnceTheRowsAreGone(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	var observed int
	receipt, err := repo.AdvanceSessionDeletion(ctx(), sessionID, func(context.Context) error {
		observed = countRows(t, repo, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID)
		return nil
	})
	if err != nil {
		t.Fatalf("AdvanceSessionDeletion: %v", err)
	}
	if receipt.State != "completed" {
		t.Fatalf("receipt = %+v, want completed", receipt)
	}
	if observed != 0 {
		t.Fatalf("completion saw %d session rows, want it to run after the cascade", observed)
	}
}

// A promoted belief outlives the conversation that produced it, but its
// citation does not. The deletion itself rewrites the frontmatter — waiting for
// the next restart leaves a file on the user's disk naming a conversation
// Turing told them was gone.
func TestSessionDeletionCompletionWithdrawsThePromotedBeliefsCitations(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	note, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatalf("PromoteMemoryCandidate: %v", err)
	}
	if !strings.Contains(readVaultNote(t, vault, note.Path), sessionID) {
		t.Fatalf("the promoted belief does not cite its conversation: %q", readVaultNote(t, vault, note.Path))
	}

	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	receipt, err := repo.AdvanceSessionDeletion(ctx(), sessionID, reconcileCompletion(repo))
	if err != nil {
		t.Fatalf("AdvanceSessionDeletion: %v", err)
	}
	if receipt.State != "completed" {
		t.Fatalf("receipt = %+v, want completed", receipt)
	}

	rewritten := readVaultNote(t, vault, note.Path)
	if strings.Contains(rewritten, sessionID) {
		t.Fatalf("the belief still cites the deleted conversation: %q", rewritten)
	}
	if !strings.Contains(rewritten, memoryfiles.WithdrawnRefsMarker) {
		t.Fatalf("the belief does not say its evidence was withdrawn: %q", rewritten)
	}
	if !strings.Contains(rewritten, "The user keeps bees.") {
		t.Fatalf("the belief itself did not survive its conversation: %q", rewritten)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM memory_evidence WHERE session_id = ?`, sessionID); got != 0 {
		t.Fatalf("evidence rows after the withdrawal = %d, want 0", got)
	}
	indexed, found := noteRowFor(t, repo, note.NoteID)
	if !found || indexed.Status != MemoryNoteStatusWithdrawn {
		t.Fatalf("indexed note = (%+v, %v), want it withdrawn", indexed, found)
	}

	// A later pass reads the file it just wrote. It must not read "withdrawn"
	// back as a citation and resurrect the link.
	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault after the withdrawal: %v", err)
	}
	if report.RefsRewritten != 0 {
		t.Fatalf("a converged vault rewrote %d notes", report.RefsRewritten)
	}
	if strings.Contains(readVaultNote(t, vault, note.Path), sessionID) {
		t.Fatalf("a later pass resurrected the withdrawn citation")
	}
}

// A promotion that crashed after moving the file leaves a belief with no row,
// a candidate row with no file, and a reservation for a path the inbox no
// longer holds. Reconcile finishes it — and the deletion that follows must not
// mistake the healed belief for a candidate it owns.
func TestCrashHealedBeliefSurvivesTheDeletionOfItsSession(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	failure := errors.New("crash after the file moved")
	repo.memoryPromotionBarrier = func() error { return failure }
	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, failure) {
		t.Fatalf("PromoteMemoryCandidate error = %v, want the barrier failure", err)
	}
	repo.memoryPromotionBarrier = nil

	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM memory_candidates WHERE id = ?`, candidate.CandidateID); got != 0 {
		t.Fatalf("candidate rows after the heal = %d, want the orphan retired", got)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM vault_artifacts WHERE session_id = ?`, sessionID); got != 0 {
		t.Fatalf("reservations after the heal = %d, want it released outside the promotion transaction", got)
	}
	healed, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil || len(healed) != 1 {
		t.Fatalf("healed notes = (%+v, %v), want the belief indexed", healed, err)
	}
	beliefPath := healed[0].Path

	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	receipt, err := repo.AdvanceSessionDeletion(ctx(), sessionID, reconcileCompletion(repo))
	if err != nil {
		t.Fatalf("AdvanceSessionDeletion: %v", err)
	}
	if receipt.State != "completed" {
		t.Fatalf("receipt = %+v, want completed", receipt)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(beliefPath))); err != nil {
		t.Fatalf("the healed belief did not survive its session's deletion: %v", err)
	}
	if !strings.Contains(readVaultNote(t, vault, beliefPath), "The user keeps bees.") {
		t.Fatalf("the healed belief lost its content")
	}
}

// The other order: the session is deleted before the heal ever runs. The
// writing pass must still finish, and the belief must visibly say its evidence
// was withdrawn rather than failing on a link it can no longer make.
func TestSessionDeletedBeforeTheHealStillYieldsWithdrawnCitations(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID, candidate := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	failure := errors.New("crash after the file moved")
	repo.memoryPromotionBarrier = func() error { return failure }
	if _, err := repo.PromoteMemoryCandidate(ctx(), MemoryCandidateDecision{CandidateID: candidate.CandidateID}); !errors.Is(err, failure) {
		t.Fatalf("PromoteMemoryCandidate error = %v, want the barrier failure", err)
	}
	repo.memoryPromotionBarrier = nil

	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	// The vault manifest still holds the promoted path's reservation, so the
	// cleaner runs first, exactly as the dispatch gate makes it.
	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}
	receipt, err := repo.AdvanceSessionDeletion(ctx(), sessionID, reconcileCompletion(repo))
	if err != nil {
		t.Fatalf("AdvanceSessionDeletion: %v", err)
	}
	if receipt.State != "completed" {
		t.Fatalf("receipt = %+v, want completed", receipt)
	}

	healed, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(healed) != 0 {
		t.Fatalf("a belief whose every conversation is gone is still searchable: %+v", healed)
	}
	beliefs, err := os.ReadDir(filepath.Join(vault.Root(), memoryfiles.BeliefsDirName))
	if err != nil || len(beliefs) != 1 {
		t.Fatalf("beliefs = (%v, %v), want the healed belief still on disk", beliefs, err)
	}
	content := readVaultNote(t, vault, memoryfiles.BeliefsDirName+"/"+beliefs[0].Name())
	if strings.Contains(content, sessionID) {
		t.Fatalf("the healed belief still cites the deleted conversation: %q", content)
	}
	if !strings.Contains(content, memoryfiles.WithdrawnRefsMarker) {
		t.Fatalf("the healed belief does not say withdrawn: %q", content)
	}
}

// The completion runs with no transaction open and no vault lock held, so a
// pass already inside the vault cannot wedge a withdrawal against it.
func TestAdvanceSessionDeletionCompletionDoesNotDeadlockAgainstAConcurrentPass(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID, _ := seedVaultDeletableSession(t, repo, "bees", "The user keeps bees.")
	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts: %v", err)
	}

	done := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := repo.AdvanceSessionDeletion(context.Background(), sessionID, reconcileCompletion(repo))
		done <- err
	}()
	go func() {
		defer wg.Done()
		_, err := repo.ReconcileMemoryVault(context.Background())
		done <- err
	}()
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		t.Fatal("a withdrawal and a vault pass deadlocked")
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent pass %d: %v", i, err)
		}
	}
}

// seedVaultCandidates gives one session several notes in the vault inbox, which
// is what a real conversation that proposed more than one belief leaves behind.
func seedVaultCandidates(t *testing.T, repo *Repository, titles ...string) (string, []MemoryCandidate) {
	t.Helper()
	sessionID := newMemoryTestSession(t, repo)
	candidates := make([]MemoryCandidate, 0, len(titles))
	for _, title := range titles {
		candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
			SessionID: sessionID,
			Kind:      MemoryCandidateKindBelief,
			Title:     title,
			Body:      "The user keeps " + title + ".",
		})
		if err != nil {
			t.Fatalf("CreateMemoryCandidate(%q): %v", title, err)
		}
		candidates = append(candidates, candidate)
	}
	return sessionID, candidates
}

func vaultArtifactStates(t *testing.T, repo *Repository, sessionID string) map[string]string {
	t.Helper()
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	states := make(map[string]string, len(artifacts))
	for _, artifact := range artifacts {
		states[artifact.ArtifactID] = artifact.State
	}
	return states
}

// A poisoned manifest row is one row. Every sibling in the same session names a
// note the user asked to have removed, and abandoning the pass at the first
// refusal leaves those notes in the vault behind a manifest that can never
// drain — one tampered entry holding the whole withdrawal hostage, forever.
//
// The poison is placed first, in the middle and last, because "keep going" and
// "stop after the failure" are indistinguishable when the only bad row is the
// last one.
func TestPurgeSessionVaultArtifactsDrainsEverySiblingOfAPoisonedRow(t *testing.T) {
	titles := []string{"bees", "chickens", "goats"}
	for poisoned := 0; poisoned < len(titles); poisoned++ {
		t.Run(titles[poisoned], func(t *testing.T) {
			repo, vault, database := newMemoryTestRepo(t)
			sessionID, candidates := seedVaultCandidates(t, repo, titles...)
			writeVaultNote(t, vault, "beliefs/precious.md", "# A file the user owns\n")
			artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
			if err != nil {
				t.Fatalf("SessionVaultArtifacts: %v", err)
			}
			if len(artifacts) != len(titles) {
				t.Fatalf("seeded artifacts = %d, want %d", len(artifacts), len(titles))
			}
			if _, err := repo.db.ExecContext(ctx(), `
				UPDATE vault_artifacts SET vault_path = ? WHERE id = ?
			`, "beliefs/precious.md", artifacts[poisoned].ArtifactID); err != nil {
				t.Fatalf("tamper with the manifest row: %v", err)
			}

			if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); !errors.Is(err, ErrVaultArtifactPathScope) {
				t.Fatalf("purge error = %v, want ErrVaultArtifactPathScope", err)
			}
			for index, candidate := range candidates {
				full := filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))
				_, statErr := os.Stat(full)
				if index == poisoned {
					if statErr != nil {
						t.Fatalf("the poisoned row's own file was removed anyway: %v", statErr)
					}
					continue
				}
				if !os.IsNotExist(statErr) {
					t.Fatalf("sibling %q survived a pass poisoned at %d: %v", candidate.InboxPath, poisoned, statErr)
				}
			}
			if _, err := os.Stat(filepath.Join(vault.Root(), "beliefs", "precious.md")); err != nil {
				t.Fatalf("a tampered manifest row deleted the belief: %v", err)
			}

			states := vaultArtifactStates(t, repo, sessionID)
			if len(states) != 1 || states[artifacts[poisoned].ArtifactID] != VaultArtifactStateDeleteFailed {
				t.Fatalf("rows after the pass = %+v, want only the poisoned row, delete_failed", states)
			}
			for index, artifact := range artifacts {
				want := 0
				if index == poisoned {
					want = 1
				}
				if got := vaultArtifactAuditCount(t, database, artifact.ArtifactID); got != want {
					t.Fatalf("cleanup audits for artifact %d = %d, want %d", index, got, want)
				}
			}

			// The ticker retries a stuck withdrawal on a schedule. Each retry
			// re-reads the same poisoned row, and a marker that re-audits a row
			// it already marked turns one broken file into an unbounded stream
			// of audit rows saying the same thing.
			for retry := 0; retry < 3; retry++ {
				if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); !errors.Is(err, ErrVaultArtifactPathScope) {
					t.Fatalf("retry %d error = %v, want ErrVaultArtifactPathScope", retry, err)
				}
				if got := vaultArtifactAuditCount(t, database, artifacts[poisoned].ArtifactID); got != 1 {
					t.Fatalf("cleanup audits after retry %d = %d, want the one failure recorded once", retry, got)
				}
				states = vaultArtifactStates(t, repo, sessionID)
				if len(states) != 1 || states[artifacts[poisoned].ArtifactID] != VaultArtifactStateDeleteFailed {
					t.Fatalf("rows after retry %d = %+v, want the poisoned row and nothing else", retry, states)
				}
			}

			// And it can still finish. Once the row names its own note again,
			// the retry drains it and the withdrawal is over.
			if _, err := repo.db.ExecContext(ctx(), `
				UPDATE vault_artifacts SET vault_path = ? WHERE id = ?
			`, candidates[poisoned].InboxPath, artifacts[poisoned].ArtifactID); err != nil {
				t.Fatalf("repair the manifest row: %v", err)
			}
			removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
			if err != nil {
				t.Fatalf("PurgeSessionVaultArtifacts after the repair: %v", err)
			}
			if removed != 1 {
				t.Fatalf("removed after the repair = %d, want the last row drained", removed)
			}
			if got := len(vaultArtifactStates(t, repo, sessionID)); got != 0 {
				t.Fatalf("rows after the repair = %d, want none", got)
			}
			if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidates[poisoned].InboxPath))); !os.IsNotExist(err) {
				t.Fatalf("the repaired row's file survived: %v", err)
			}
			if _, err := os.Stat(filepath.Join(vault.Root(), "beliefs", "precious.md")); err != nil {
				t.Fatalf("the belief did not survive the whole withdrawal: %v", err)
			}
		})
	}
}

// Every failed row is marked and audited, but what the pass reports back is
// bounded. A manifest full of unusable rows is still one withdrawal, and an
// error that grows a clause per row is a value assembled from as many vault
// paths as the manifest happens to hold.
func TestPurgeSessionVaultArtifactsReportsBoundedOpaqueFailuresForEveryFailedRow(t *testing.T) {
	repo, _, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	poisonedIDs := make([]string, 0, 12)
	for index := 0; index < 12; index++ {
		reserved, err := repo.ReserveVaultArtifact(ctx(), ReserveVaultArtifactInput{
			SessionID: sessionID,
			VaultPath: fmt.Sprintf("inbox/note-%02d.md", index),
		})
		if err != nil {
			t.Fatalf("ReserveVaultArtifact(%d): %v", index, err)
		}
		if _, err := repo.db.ExecContext(ctx(), `
			UPDATE vault_artifacts SET vault_path = ? WHERE id = ?
		`, fmt.Sprintf("beliefs/poison-%02d.md", index), reserved.ArtifactID); err != nil {
			t.Fatalf("tamper with manifest row %d: %v", index, err)
		}
		poisonedIDs = append(poisonedIDs, reserved.ArtifactID)
	}

	_, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if !errors.Is(err, ErrVaultArtifactPathScope) {
		t.Fatalf("purge error = %v, want ErrVaultArtifactPathScope", err)
	}
	// The report samples; it does not accumulate. The number of failures one
	// pass can observe is the number of rows the session owns, which is
	// unbounded, so a report that carried one entry per failed row would grow
	// with the manifest. What has to survive truncation is recognisability —
	// the class is still there for errors.Is — and the counts, which say how
	// much the sample is leaving out.
	if sampled := strings.Count(err.Error(), ErrVaultArtifactPathScope.Error()); sampled > maxVaultPurgeErrors {
		t.Fatalf("purge error carries %d entries for %d failed rows, want at most %d",
			sampled, len(poisonedIDs), maxVaultPurgeErrors)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d of %d", len(poisonedIDs), len(poisonedIDs))) {
		t.Fatalf("purge error %q does not say how many of the rows failed", err.Error())
	}
	if len(err.Error()) > maxVaultPurgeErrorBytes {
		t.Fatalf("purge error is %d bytes over %d rows, want it bounded at %d",
			len(err.Error()), len(poisonedIDs), maxVaultPurgeErrorBytes)
	}
	if strings.Contains(err.Error(), "poison-") || strings.Contains(err.Error(), sessionID) {
		t.Fatalf("purge error leaked what it touched: %q", err.Error())
	}

	// The bound is on the report, never on the work: every unusable row is
	// still marked and still audited exactly once.
	states := vaultArtifactStates(t, repo, sessionID)
	if len(states) != len(poisonedIDs) {
		t.Fatalf("rows after the pass = %d, want all %d retained", len(states), len(poisonedIDs))
	}
	for index, artifactID := range poisonedIDs {
		if states[artifactID] != VaultArtifactStateDeleteFailed {
			t.Fatalf("row %d = %q, want delete_failed", index, states[artifactID])
		}
		if got := vaultArtifactAuditCount(t, database, artifactID); got != 1 {
			t.Fatalf("cleanup audits for row %d = %d, want 1", index, got)
		}
	}
}

// refuseVaultArtifactForget makes the manifest write for exactly one row fail,
// the way storage fails between deleting a file and recording that it is gone.
// It is a trigger rather than a stubbed repository because the classification
// under test lives inside PurgeSessionVaultArtifacts, and a stand-in for the
// method it calls would prove only that the stand-in was called.
func refuseVaultArtifactForget(t *testing.T, repo *Repository, artifactID string) func() {
	t.Helper()
	// A trigger body cannot carry bind variables, and the id is one this test
	// just generated.
	if _, err := repo.db.ExecContext(ctx(), fmt.Sprintf(`
		CREATE TRIGGER refuse_vault_artifact_forget
		BEFORE DELETE ON vault_artifacts
		WHEN OLD.id = '%s'
		BEGIN SELECT RAISE(ABORT, 'vault manifest is unwritable'); END
	`, artifactID)); err != nil {
		t.Fatalf("install the manifest-write refusal: %v", err)
	}
	healed := false
	heal := func() {
		if healed {
			return
		}
		healed = true
		if _, err := repo.db.ExecContext(ctx(), `DROP TRIGGER refuse_vault_artifact_forget`); err != nil {
			t.Fatalf("heal the manifest write: %v", err)
		}
	}
	t.Cleanup(heal)
	return heal
}

// One pass can fail both ways at once: a note the vault will not release, and
// the rows for the notes it did release refusing to leave the manifest.
//
// Both facts are true, and the manifest already records the first one per row.
// What the pass reports back has to carry the second, because the caller reads
// it to decide whether this was "files are still there" — which is answered by
// marking every surviving row delete_failed — and the rows surviving here name
// notes that are gone. Reporting only the removal failure is what turns a
// deleted note into an audit row telling the user it is still on their disk.
func TestPurgeSessionVaultArtifactsReportsFinalizationBesideARowItCouldNotRemove(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID, candidates := seedVaultCandidates(t, repo, "bees", "chickens")
	writeVaultNote(t, vault, "beliefs/precious.md", "# A file the user owns\n")
	artifacts, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("seeded artifacts = %d, want 2", len(artifacts))
	}
	poisoned, removable := artifacts[0], artifacts[1]
	if _, err := repo.db.ExecContext(ctx(), `
		UPDATE vault_artifacts SET vault_path = ? WHERE id = ?
	`, "beliefs/precious.md", poisoned.ArtifactID); err != nil {
		t.Fatalf("tamper with the manifest row: %v", err)
	}
	heal := refuseVaultArtifactForget(t, repo, removable.ArtifactID)

	_, err = repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if !errors.Is(err, ErrVaultArtifactManifestFinalize) {
		t.Fatalf("purge error = %v, want it to carry ErrVaultArtifactManifestFinalize", err)
	}
	if !errors.Is(err, ErrVaultArtifactPathScope) {
		t.Fatalf("purge error = %v, want the refused row still recognisable", err)
	}

	// The note that was removed is gone, and the note the pass refused to touch
	// is exactly where the user left it.
	if _, statErr := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidates[1].InboxPath))); !os.IsNotExist(statErr) {
		t.Fatalf("the removable note survived: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(vault.Root(), "beliefs", "precious.md")); statErr != nil {
		t.Fatalf("a tampered manifest row deleted the belief: %v", statErr)
	}

	states := vaultArtifactStates(t, repo, sessionID)
	if len(states) != 2 {
		t.Fatalf("rows after the pass = %+v, want both retained for the retry", states)
	}
	if states[poisoned.ArtifactID] != VaultArtifactStateDeleteFailed {
		t.Fatalf("poisoned row = %q, want delete_failed", states[poisoned.ArtifactID])
	}
	if states[removable.ArtifactID] == VaultArtifactStateDeleteFailed {
		t.Fatalf("the removed note's row = %q, want it not relabelled as undeleted",
			states[removable.ArtifactID])
	}
	if got := vaultArtifactAuditCount(t, database, poisoned.ArtifactID); got != 1 {
		t.Fatalf("audits for the note that is still there = %d, want 1", got)
	}
	if got := vaultArtifactAuditCount(t, database, removable.ArtifactID); got != 0 {
		t.Fatalf("audits for the note that was deleted = %d, want none", got)
	}

	// Once storage answers again and the row names its own note, the retry
	// drains both rows: removal is idempotent, so the note already gone costs
	// nothing to re-attempt.
	heal()
	if _, err := repo.db.ExecContext(ctx(), `
		UPDATE vault_artifacts SET vault_path = ? WHERE id = ?
	`, candidates[0].InboxPath, poisoned.ArtifactID); err != nil {
		t.Fatalf("repair the manifest row: %v", err)
	}
	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("PurgeSessionVaultArtifacts after the repair: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed after the repair = %d, want both rows drained", removed)
	}
	if got := len(vaultArtifactStates(t, repo, sessionID)); got != 0 {
		t.Fatalf("rows after the repair = %d, want none", got)
	}
	if got := vaultArtifactAuditCount(t, database, removable.ArtifactID); got != 0 {
		t.Fatalf("audits for the deleted note after the retry = %d, want none", got)
	}
}

// seedSandboxArtifactWith seeds one sandbox row with an explicit state and
// policy, so a marking rule can be tested against rows it is supposed to leave
// alone as well as the ones it owns.
func seedSandboxArtifactWith(
	t *testing.T, repo *Repository, sessionID string, runID string,
	artifactID string, state string, policy string,
) {
	t.Helper()
	if _, err := repo.db.ExecContext(ctx(), `
		INSERT INTO sandbox_artifacts (
			id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)
	`, artifactID, sessionID, runID, "sha256:"+artifactID,
		"sessions/"+sessionID+"/files/"+artifactID+".txt", state, policy, now()); err != nil {
		t.Fatalf("seed sandbox artifact %q: %v", artifactID, err)
	}
}

func sandboxArtifactState(t *testing.T, repo *Repository, artifactID string) string {
	t.Helper()
	var state string
	if err := repo.db.QueryRowContext(ctx(),
		`SELECT state FROM sandbox_artifacts WHERE id = ?`, artifactID).Scan(&state); err != nil {
		t.Fatalf("read sandbox artifact %q: %v", artifactID, err)
	}
	return state
}

// A sandbox cleanup failure marks the rows the sandbox cleaner was actually
// asked to remove, and only those.
//
// Two kinds of row sit beside them in the same manifest and neither is this
// failure's to touch. A retain_legacy_unowned row names a file the sandbox does
// not own and never tried to delete — marking it delete_failed files an audit
// row claiming Turing failed at work it never started, and points the retry at
// a file it must not remove. A row a previous partial pass already marked
// carries its own audit row already; re-marking it inflates one failure into
// two and makes the receipt's history unreadable.
func TestMarkSessionDeletionSandboxFailureMarksOnlyTheRowsItOwns(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	enqueued, err := repo.EnqueueUserMessage(ctx(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "write a file", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	seedSandboxArtifactWith(t, repo, sessionID, enqueued.RunID,
		"artifact_owned", SandboxArtifactStateReady, SandboxArtifactPolicyDeleteOnSessionDelete)
	seedSandboxArtifactWith(t, repo, sessionID, enqueued.RunID,
		"artifact_legacy", SandboxArtifactStateReady, SandboxArtifactPolicyRetainLegacyUnowned)
	seedSandboxArtifactWith(t, repo, sessionID, enqueued.RunID,
		"artifact_already_failed", SandboxArtifactStateDeleteFailed, SandboxArtifactPolicyDeleteOnSessionDelete)
	if _, err := repo.BeginSessionDeletion(ctx(), sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	if err := repo.MarkSessionDeletionSandboxFailure(ctx(), sessionID, "sandbox_artifact_cleanup_failed"); err != nil {
		t.Fatalf("MarkSessionDeletionSandboxFailure: %v", err)
	}

	if got := sandboxArtifactState(t, repo, "artifact_owned"); got != SandboxArtifactStateDeleteFailed {
		t.Fatalf("the sandbox's own row state = %q, want %q", got, SandboxArtifactStateDeleteFailed)
	}
	if got := sandboxArtifactAuditCount(t, repo, "artifact_owned"); got != 1 {
		t.Fatalf("audit rows for the failed sandbox file = %d, want exactly one", got)
	}
	if got := sandboxArtifactState(t, repo, "artifact_legacy"); got != SandboxArtifactStateReady {
		t.Fatalf("a retained legacy row was marked %q; the sandbox never tried to delete it", got)
	}
	if got := sandboxArtifactAuditCount(t, repo, "artifact_legacy"); got != 0 {
		t.Fatalf("audit rows for a retained legacy file = %d, want none", got)
	}
	if got := sandboxArtifactState(t, repo, "artifact_already_failed"); got != SandboxArtifactStateDeleteFailed {
		t.Fatalf("an already-failed row state = %q, want it left as it was", got)
	}
	if got := sandboxArtifactAuditCount(t, repo, "artifact_already_failed"); got != 0 {
		t.Fatalf("a second audit row was filed for an already-failed file: %d", got)
	}
}
