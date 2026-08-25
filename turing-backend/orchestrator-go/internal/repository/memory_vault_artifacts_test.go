package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
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

	if _, err := repo.FinalizeVaultArtifact(ctx, artifact.ArtifactID, stranger.SessionID); !errors.Is(err, ErrVaultArtifactNotFound) {
		t.Fatalf("stranger finalize error = %v, want ErrVaultArtifactNotFound", err)
	}
	if released, err := repo.ReleaseVaultArtifactReservation(ctx, artifact.ArtifactID, stranger.SessionID); err != nil || released {
		t.Fatalf("stranger release = (%v, %v), want (false, nil)", released, err)
	}

	finalized, err := repo.FinalizeVaultArtifact(ctx, artifact.ArtifactID, owner.SessionID)
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
	if _, err := repo.FinalizeVaultArtifact(ctx, artifact.ArtifactID, owner.SessionID); !errors.Is(err, ErrVaultArtifactInvalidTransition) {
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
	if err := repo.MarkSessionVaultArtifactsDeleteFailed(ctx, owner.SessionID, "vault_unavailable"); err != nil {
		t.Fatalf("MarkSessionVaultArtifactsDeleteFailed: %v", err)
	}
	pending, err = repo.PendingSessionVaultArtifacts(ctx, owner.SessionID)
	if err != nil {
		t.Fatalf("PendingSessionVaultArtifacts after failure: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after delete_failed = %d, want 0", len(pending))
	}
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

	if err := repo.MarkSessionVaultArtifactsDeleteFailed(ctx, session.SessionID, "vault_unavailable"); err != nil {
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
	if err := repo.DeleteSession(ctx, session.SessionID); err != nil {
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
