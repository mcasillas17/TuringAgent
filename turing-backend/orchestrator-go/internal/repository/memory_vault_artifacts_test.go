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

func TestReserveVaultArtifactValidatesInboxPath(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "memory")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	for _, vaultPath := range []string{
		"",
		"inbox",
		"inbox/",
		"beliefs/note.md",
		"profile.md",
		"persona.md",
		"/inbox/note.md",
		"inbox/../beliefs/note.md",
		"../inbox/note.md",
		"inbox//note.md",
		"inbox/./note.md",
		"inbox/note.txt",
		"inbox/no\x00te.md",
		"inbox/note\n.md",
		"inbox/" + strings.Repeat("a", 300) + ".md",
	} {
		_, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{
			SessionID: session.SessionID,
			VaultPath: vaultPath,
		})
		if !errors.Is(err, ErrVaultArtifactPathScope) {
			t.Fatalf("ReserveVaultArtifact(%q) error = %v, want ErrVaultArtifactPathScope", vaultPath, err)
		}
	}

	artifact, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{
		SessionID: session.SessionID,
		VaultPath: "inbox/note.md",
	})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}
	if artifact.State != VaultArtifactStateWriting {
		t.Fatalf("state = %q, want %q", artifact.State, VaultArtifactStateWriting)
	}
	if artifact.SessionID != session.SessionID || artifact.VaultPath != "inbox/note.md" {
		t.Fatalf("unexpected artifact: %+v", artifact)
	}
	if artifact.ArtifactID == "" || artifact.CreatedAt == "" || artifact.FinalizedAt != "" {
		t.Fatalf("unexpected artifact identity/timestamps: %+v", artifact)
	}
}

func TestReserveVaultArtifactRefusesUnknownSession(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	if _, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{
		SessionID: "sess_missing",
		VaultPath: "inbox/note.md",
	}); !errors.Is(err, ErrVaultArtifactSessionUnavailable) {
		t.Fatalf("error = %v, want ErrVaultArtifactSessionUnavailable", err)
	}

	session, err := repo.CreateSession(ctx, "memory")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	if _, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{
		SessionID: session.SessionID,
		VaultPath: "inbox/note.md",
	}); !errors.Is(err, ErrVaultArtifactSessionUnavailable) {
		t.Fatalf("reserving into a deleting session error = %v, want ErrVaultArtifactSessionUnavailable", err)
	}
}

func TestVaultArtifactStateTransitionsAreScopedToTheSession(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	owner, err := repo.CreateSession(ctx, "owner")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	stranger, err := repo.CreateSession(ctx, "stranger")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	artifact, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{
		SessionID: owner.SessionID,
		VaultPath: "inbox/note.md",
	})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}

	if _, err := repo.FinalizeVaultArtifact(ctx, artifact.ArtifactID, stranger.SessionID, "sha256:written"); !errors.Is(err, ErrVaultArtifactNotFound) {
		t.Fatalf("stranger finalize error = %v, want ErrVaultArtifactNotFound", err)
	}
	if released, err := repo.ReleaseVaultArtifactReservation(ctx, artifact.ArtifactID, stranger.SessionID); err != nil || released {
		t.Fatalf("stranger release = (%v, %v), want (false, nil)", released, err)
	}

	finalized, err := repo.FinalizeVaultArtifact(ctx, artifact.ArtifactID, owner.SessionID, "sha256:written")
	if err != nil {
		t.Fatalf("FinalizeVaultArtifact: %v", err)
	}
	if finalized.State != VaultArtifactStateReady || finalized.FinalizedAt == "" {
		t.Fatalf("unexpected finalized artifact: %+v", finalized)
	}
	// A finalized artifact names a file that exists, so its reservation can no
	// longer be dropped as if the write never happened.
	if released, err := repo.ReleaseVaultArtifactReservation(ctx, artifact.ArtifactID, owner.SessionID); err != nil || released {
		t.Fatalf("release after finalize = (%v, %v), want (false, nil)", released, err)
	}
	if _, err := repo.FinalizeVaultArtifact(ctx, artifact.ArtifactID, owner.SessionID, "sha256:written"); !errors.Is(err, ErrVaultArtifactInvalidTransition) {
		t.Fatalf("second finalize error = %v, want ErrVaultArtifactInvalidTransition", err)
	}
}

func TestSessionVaultArtifactListingsAreScopedAndCounted(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	owner, err := repo.CreateSession(ctx, "owner")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	stranger, err := repo.CreateSession(ctx, "stranger")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for _, path := range []string{"inbox/a.md", "inbox/b.md"} {
		if _, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{SessionID: owner.SessionID, VaultPath: path}); err != nil {
			t.Fatalf("ReserveVaultArtifact(%q): %v", path, err)
		}
	}
	if _, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{SessionID: stranger.SessionID, VaultPath: "inbox/c.md"}); err != nil {
		t.Fatalf("ReserveVaultArtifact for the stranger: %v", err)
	}

	artifacts, err := repo.SessionVaultArtifacts(ctx, owner.SessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("artifact count = %d, want 2", len(artifacts))
	}
	for _, artifact := range artifacts {
		if artifact.SessionID != owner.SessionID {
			t.Fatalf("listing leaked another session's artifact: %+v", artifact)
		}
	}
	count, err := repo.CountSessionVaultArtifacts(ctx, owner.SessionID)
	if err != nil {
		t.Fatalf("CountSessionVaultArtifacts: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	pending, err := repo.PendingSessionVaultArtifacts(ctx, owner.SessionID)
	if err != nil {
		t.Fatalf("PendingSessionVaultArtifacts: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending count = %d, want 2", len(pending))
	}
	if err := repo.MarkSessionVaultArtifactsDeleteFailed(ctx, owner.SessionID, artifactIDsOf(artifacts), "vault_unavailable"); err != nil {
		t.Fatalf("MarkSessionVaultArtifactsDeleteFailed: %v", err)
	}
	// A delete_failed row is a file still sitting in the user's vault. Leaving
	// it out of the worklist is how a retry reports a completed withdrawal
	// while the note it was supposed to remove is still there.
	pending, err = repo.PendingSessionVaultArtifacts(ctx, owner.SessionID)
	if err != nil {
		t.Fatalf("PendingSessionVaultArtifacts after failure: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending after delete_failed = %d, want both rows retried", len(pending))
	}
	for _, artifact := range pending {
		if artifact.State != VaultArtifactStateDeleteFailed {
			t.Fatalf("worklist row %+v, want the failed rows back in the worklist", artifact)
		}
	}
}

func artifactIDsOf(artifacts []VaultArtifact) []string {
	ids := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		ids = append(ids, artifact.ArtifactID)
	}
	return ids
}

// The failure marker is the sandbox pattern applied to a different manifest:
// one redacted audit row per artifact, naming the artifact and the error code
// and nothing about what the file said. It must never reach across into
// sandbox_artifacts, which has its own cleaner and its own retention answer.
func TestMarkSessionVaultArtifactsDeleteFailedAuditsEachArtifactAndSpareSandboxRows(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "owner")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	run, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	sandbox, _, err := repo.ReserveSandboxArtifact(ctx, ReserveSandboxArtifactInput{
		SessionID:    session.SessionID,
		RunID:        run.RunID,
		LogicalPath:  "notes.txt",
		PhysicalPath: OwnedSandboxPath(session.SessionID, run.RunID, "notes.txt"),
	})
	if err != nil {
		t.Fatalf("ReserveSandboxArtifact: %v", err)
	}
	first, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{SessionID: session.SessionID, VaultPath: "inbox/a.md"})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}
	second, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{SessionID: session.SessionID, VaultPath: "inbox/b.md"})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}

	// The sandbox id is passed in deliberately: the statement names
	// vault_artifacts and only vault_artifacts, so an id from another manifest
	// matches nothing and changes nothing.
	if err := repo.MarkSessionVaultArtifactsDeleteFailed(ctx, session.SessionID,
		[]string{first.ArtifactID, second.ArtifactID, sandbox.ArtifactID}, "vault_unavailable"); err != nil {
		t.Fatalf("MarkSessionVaultArtifactsDeleteFailed: %v", err)
	}

	var sandboxState string
	if err := database.QueryRowContext(ctx, `SELECT state FROM sandbox_artifacts WHERE id = ?`, sandbox.ArtifactID).Scan(&sandboxState); err != nil {
		t.Fatalf("read sandbox artifact: %v", err)
	}
	if sandboxState != SandboxArtifactStateWriting {
		t.Fatalf("sandbox artifact state = %q, want it untouched (%q)", sandboxState, SandboxArtifactStateWriting)
	}

	for _, artifactID := range []string{first.ArtifactID, second.ArtifactID} {
		var payload string
		if err := database.QueryRowContext(ctx, `
			SELECT payload_json FROM audit_logs WHERE action = ? AND target = ?
		`, "session.vault_artifact.cleanup.failed", artifactID).Scan(&payload); err != nil {
			t.Fatalf("audit row for %q: %v", artifactID, err)
		}
		if !strings.Contains(payload, "vault_unavailable") || strings.Contains(payload, "inbox/") {
			t.Fatalf("audit payload = %q, want the error code and no vault path", payload)
		}
	}
	var sandboxAudits int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM audit_logs WHERE action = ? AND target = ?
	`, "session.vault_artifact.cleanup.failed", sandbox.ArtifactID).Scan(&sandboxAudits); err != nil {
		t.Fatalf("count sandbox audits: %v", err)
	}
	if sandboxAudits != 0 {
		t.Fatalf("sandbox artifact got %d vault audit rows, want 0", sandboxAudits)
	}
}

// The cleaner removes manifest rows only for the files it actually deleted. A
// row it never reached has to stay, or the orchestrator forgets a file that is
// still sitting in the user's vault.
func TestDeleteVaultArtifactsRemovesOnlyTheNamedRows(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "owner")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	run, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	sandbox, _, err := repo.ReserveSandboxArtifact(ctx, ReserveSandboxArtifactInput{
		SessionID:    session.SessionID,
		RunID:        run.RunID,
		LogicalPath:  "notes.txt",
		PhysicalPath: OwnedSandboxPath(session.SessionID, run.RunID, "notes.txt"),
	})
	if err != nil {
		t.Fatalf("ReserveSandboxArtifact: %v", err)
	}
	deleted, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{SessionID: session.SessionID, VaultPath: "inbox/gone.md"})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}
	kept, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{SessionID: session.SessionID, VaultPath: "inbox/kept.md"})
	if err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}

	if err := repo.DeleteVaultArtifacts(ctx, []string{deleted.ArtifactID, sandbox.ArtifactID}); err != nil {
		t.Fatalf("DeleteVaultArtifacts: %v", err)
	}

	remaining, err := repo.SessionVaultArtifacts(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ArtifactID != kept.ArtifactID {
		t.Fatalf("remaining vault artifacts = %+v, want only %q", remaining, kept.ArtifactID)
	}
	var sandboxRows int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sandbox_artifacts WHERE id = ?`, sandbox.ArtifactID).Scan(&sandboxRows); err != nil {
		t.Fatalf("count sandbox rows: %v", err)
	}
	if sandboxRows != 1 {
		t.Fatalf("sandbox artifact rows = %d, want the sandbox manifest untouched", sandboxRows)
	}
}

// Deleting the session deletes what it wrote into the vault manifest with it.
func TestVaultArtifactsCascadeWithTheSession(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "owner")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{SessionID: session.SessionID, VaultPath: "inbox/a.md"}); err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}
	if err := repo.DeleteSessionForTests(ctx, session.SessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	var rows int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM vault_artifacts WHERE session_id = ?`, session.SessionID).Scan(&rows); err != nil {
		t.Fatalf("count vault artifacts: %v", err)
	}
	if rows != 0 {
		t.Fatalf("vault artifact rows after session deletion = %d, want 0", rows)
	}
}

// A vault path belongs to one session. If a second session could reserve a
// path the first already owns, deleting the second would delete a file the
// first is still responsible for — one session erasing another's note.
func TestReserveVaultArtifactRefusesAPathAnotherSessionOwns(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	owner, err := repo.CreateSession(ctx, "owner")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	stranger, err := repo.CreateSession(ctx, "stranger")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{
		SessionID: owner.SessionID,
		VaultPath: "inbox/contested.md",
	}); err != nil {
		t.Fatalf("ReserveVaultArtifact: %v", err)
	}

	if _, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{
		SessionID: stranger.SessionID,
		VaultPath: "inbox/contested.md",
	}); !errors.Is(err, ErrVaultArtifactExists) {
		t.Fatalf("cross-session reservation error = %v, want ErrVaultArtifactExists", err)
	}
	strangerArtifacts, err := repo.SessionVaultArtifacts(ctx, stranger.SessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(strangerArtifacts) != 0 {
		t.Fatalf("a refused reservation left %+v behind", strangerArtifacts)
	}

	// The owner keeps its claim, and the path becomes reservable again only
	// once the owner has let it go.
	ownerArtifacts, err := repo.SessionVaultArtifacts(ctx, owner.SessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(ownerArtifacts) != 1 {
		t.Fatalf("owner artifacts = %+v, want its reservation intact", ownerArtifacts)
	}
	if released, err := repo.ReleaseVaultArtifactReservation(ctx, ownerArtifacts[0].ArtifactID, owner.SessionID); err != nil || !released {
		t.Fatalf("ReleaseVaultArtifactReservation = (%v, %v), want (true, nil)", released, err)
	}
	if _, err := repo.ReserveVaultArtifact(ctx, ReserveVaultArtifactInput{
		SessionID: stranger.SessionID,
		VaultPath: "inbox/contested.md",
	}); err != nil {
		t.Fatalf("reservation after the owner released the path: %v", err)
	}
}

// breakVaultInbox replaces the inbox directory with a regular file, so every
// operation that has to open it fails the same way for everyone running the
// test. It returns the repair.
func breakVaultInbox(t *testing.T, vault *memoryfiles.Vault) func() {
	t.Helper()
	inbox := filepath.Join(vault.Root(), memoryfiles.InboxDirName)
	hidden := filepath.Join(vault.Root(), "inbox-hidden")
	if err := os.Rename(inbox, hidden); err != nil {
		t.Fatalf("hide the inbox: %v", err)
	}
	if err := os.WriteFile(inbox, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block the inbox: %v", err)
	}
	return func() {
		t.Helper()
		if err := os.Remove(inbox); err != nil {
			t.Fatalf("unblock the inbox: %v", err)
		}
		if err := os.Rename(hidden, inbox); err != nil {
			t.Fatalf("restore the inbox: %v", err)
		}
	}
}

func vaultArtifactAuditCount(t *testing.T, database *db.DB, artifactID string) int {
	t.Helper()
	var rows int
	if err := database.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM audit_logs WHERE action = ? AND target = ?
	`, "session.vault_artifact.cleanup.failed", artifactID).Scan(&rows); err != nil {
		t.Fatalf("count cleanup audits for %q: %v", artifactID, err)
	}
	return rows
}

// A withdrawal that could not reach the vault has to stay retryable and has to
// be able to finish. A worklist that skips the rows a previous pass marked
// failed can never drain: the file stays in the user's vault forever and every
// retry reports there was nothing to do.
func TestPurgeSessionVaultArtifactsRetriesRowsAPreviousPassCouldNotDelete(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	candidate, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "bees",
		Body:      "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	repair := breakVaultInbox(t, vault)
	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); err == nil {
		t.Fatalf("PurgeSessionVaultArtifacts succeeded with an unreachable inbox")
	}
	failed, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(failed) != 1 || failed[0].State != VaultArtifactStateDeleteFailed {
		t.Fatalf("artifacts after the failure = %+v, want one delete_failed row", failed)
	}
	if got := vaultArtifactAuditCount(t, database, failed[0].ArtifactID); got != 1 {
		t.Fatalf("cleanup audit rows = %d, want one honest failure recorded", got)
	}

	repair()
	removed, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("retry PurgeSessionVaultArtifacts: %v", err)
	}
	if removed != 1 {
		t.Fatalf("retry removed = %d, want the failed row reattempted and drained", removed)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the retry left the candidate file behind: %v", err)
	}
	remaining, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts after the retry: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("manifest rows after a drained retry = %+v, want none", remaining)
	}
}

// A pass that fails partway through has already deleted files. Those rows must
// leave the manifest, and must not be filed as failures: an audit row saying
// Turing could not delete a file it just deleted is a false record of the
// user's withdrawal, and the next retry would go looking for a file that is
// already gone.
func TestPurgeSessionVaultArtifactsFilesOnlyTheRowsItCouldNotRemove(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	removable, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "bees",
		Body:      "The user keeps bees.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	tampered, err := repo.CreateMemoryCandidate(ctx(), CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      MemoryCandidateKindBelief,
		Title:     "chickens",
		Body:      "The user keeps chickens.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	writeVaultNote(t, vault, "beliefs/precious.md", "# A belief the user accepted\n")
	if _, err := database.ExecContext(ctx(), `
		UPDATE vault_artifacts SET vault_path = ? WHERE vault_path = ?
	`, "beliefs/precious.md", tampered.InboxPath); err != nil {
		t.Fatalf("tamper with the manifest row: %v", err)
	}

	if _, err := repo.PurgeSessionVaultArtifacts(ctx(), sessionID); !errors.Is(err, ErrVaultArtifactPathScope) {
		t.Fatalf("purge error = %v, want ErrVaultArtifactPathScope", err)
	}

	if _, err := os.Stat(filepath.Join(vault.Root(), "beliefs", "precious.md")); err != nil {
		t.Fatalf("a tampered manifest row deleted a belief: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(removable.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the removable candidate file survived: %v", err)
	}
	remaining, err := repo.SessionVaultArtifacts(ctx(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("manifest rows = %+v, want only the row that could not be removed", remaining)
	}
	if remaining[0].State != VaultArtifactStateDeleteFailed || remaining[0].VaultPath != "beliefs/precious.md" {
		t.Fatalf("surviving row = %+v, want the tampered row marked failed", remaining[0])
	}
	if got := vaultArtifactAuditCount(t, database, remaining[0].ArtifactID); got != 1 {
		t.Fatalf("cleanup audit rows for the failing row = %d, want 1", got)
	}

	var falseFailures int
	if err := database.QueryRowContext(ctx(), `
		SELECT COUNT(*) FROM audit_logs WHERE action = ?
	`, "session.vault_artifact.cleanup.failed").Scan(&falseFailures); err != nil {
		t.Fatalf("count cleanup audits: %v", err)
	}
	if falseFailures != 1 {
		t.Fatalf("cleanup failure audit rows = %d, want only the row that actually failed", falseFailures)
	}
}
