package repository

import (
	"context"
	"errors"
	"testing"
)

func seedArtifactSession(t *testing.T, repo *Repository, title string) EnqueueUserMessageResult {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "write a file", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	return enqueued
}

func ownedReservation(enqueued EnqueueUserMessageResult, logicalPath string) ReserveSandboxArtifactInput {
	return ReserveSandboxArtifactInput{
		SessionID:          enqueued.SessionID,
		RunID:              enqueued.RunID,
		LogicalPath:        logicalPath,
		PhysicalPath:       OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, logicalPath),
		DeletionGeneration: 0,
	}
}

func TestReserveSandboxArtifactRecordsOwnedWritingRow(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact reservation")

	artifact, created, err := repo.ReserveSandboxArtifact(ctx, ownedReservation(enqueued, "notes/todo.txt"))
	if err != nil {
		t.Fatalf("ReserveSandboxArtifact: %v", err)
	}

	if artifact.ArtifactID == "" {
		t.Fatal("reservation did not return an artifact id")
	}
	if !created {
		t.Fatal("first reservation did not report that it created the row")
	}
	if artifact.State != SandboxArtifactStateWriting {
		t.Fatalf("state = %q, want %q", artifact.State, SandboxArtifactStateWriting)
	}
	if artifact.Policy != SandboxArtifactPolicyDeleteOnSessionDelete {
		t.Fatalf("policy = %q, want %q", artifact.Policy, SandboxArtifactPolicyDeleteOnSessionDelete)
	}
	want := "sessions/" + enqueued.SessionID + "/runs/" + enqueued.RunID + "/files/notes/todo.txt"
	if artifact.PhysicalPath != want {
		t.Fatalf("physical path = %q, want %q", artifact.PhysicalPath, want)
	}
	if artifact.LogicalPathHash == "" || artifact.CreatedAt == "" {
		t.Fatalf("artifact = %+v, want hashed logical path and creation timestamp", artifact)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sandbox_artifacts WHERE session_id = ? AND state = 'writing'`, enqueued.SessionID); got != 1 {
		t.Fatalf("writing rows = %d, want 1", got)
	}
}

func TestReserveSandboxArtifactIsIdempotentPerRunAndPath(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact retry")
	input := ownedReservation(enqueued, "notes/todo.txt")

	first, firstCreated, err := repo.ReserveSandboxArtifact(ctx, input)
	if err != nil {
		t.Fatalf("first ReserveSandboxArtifact: %v", err)
	}
	second, secondCreated, err := repo.ReserveSandboxArtifact(ctx, input)
	if err != nil {
		t.Fatalf("second ReserveSandboxArtifact: %v", err)
	}

	if !firstCreated {
		t.Fatal("first reservation did not report that it created the row")
	}
	if secondCreated {
		t.Fatal("retried reservation claimed to have created a second row")
	}
	if second.ArtifactID != first.ArtifactID {
		t.Fatalf("retry artifact id = %q, want %q", second.ArtifactID, first.ArtifactID)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sandbox_artifacts WHERE session_id = ?`, enqueued.SessionID); got != 1 {
		t.Fatalf("artifact rows = %d, want 1", got)
	}
}

func TestReserveSandboxArtifactKeepsFinalizedRowReady(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact rewrite")
	input := ownedReservation(enqueued, "notes/todo.txt")

	first, _, err := repo.ReserveSandboxArtifact(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FinalizeSandboxArtifact(ctx, FinalizeSandboxArtifactInput{
		ArtifactID: first.ArtifactID, SessionID: enqueued.SessionID, RunID: enqueued.RunID,
	}); err != nil {
		t.Fatal(err)
	}

	second, secondCreated, err := repo.ReserveSandboxArtifact(ctx, input)
	if err != nil {
		t.Fatalf("re-reserve finalized artifact: %v", err)
	}
	if secondCreated {
		t.Fatal("re-reservation claimed to have created a row for an existing artifact")
	}
	if second.State != SandboxArtifactStateReady {
		t.Fatalf("re-reserved state = %q, want %q; a finalized artifact must not be downgraded to writing", second.State, SandboxArtifactStateReady)
	}
}

func TestReserveSandboxArtifactRejectsRunFromAnotherSession(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	owner := seedArtifactSession(t, repo, "Artifact owner")
	other := seedArtifactSession(t, repo, "Artifact other")

	input := ownedReservation(owner, "notes/todo.txt")
	input.RunID = other.RunID

	if _, _, err := repo.ReserveSandboxArtifact(ctx, input); !errors.Is(err, ErrSandboxArtifactUnowned) {
		t.Fatalf("cross-session reservation error = %v, want %v", err, ErrSandboxArtifactUnowned)
	}
}

func TestReserveSandboxArtifactRejectsPhysicalPathOutsideRunScope(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact scope")

	input := ownedReservation(enqueued, "notes/todo.txt")
	input.PhysicalPath = "sessions/" + enqueued.SessionID + "/runs/other-run/files/notes/todo.txt"

	if _, _, err := repo.ReserveSandboxArtifact(ctx, input); !errors.Is(err, ErrSandboxArtifactPathScope) {
		t.Fatalf("out-of-scope path error = %v, want %v", err, ErrSandboxArtifactPathScope)
	}
}

func TestReserveSandboxArtifactTypesLegacyRootPathAsRetained(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact legacy")

	artifact, created, err := repo.ReserveSandboxArtifact(ctx, ReserveSandboxArtifactInput{
		SessionID:    enqueued.SessionID,
		RunID:        enqueued.RunID,
		LogicalPath:  "legacy.txt",
		PhysicalPath: "legacy.txt",
	})
	if err != nil {
		t.Fatalf("ReserveSandboxArtifact: %v", err)
	}
	if !created {
		t.Fatal("legacy reservation did not report that it created the row")
	}

	if artifact.Policy != SandboxArtifactPolicyRetainLegacyUnowned {
		t.Fatalf("policy = %q, want %q", artifact.Policy, SandboxArtifactPolicyRetainLegacyUnowned)
	}
}

func TestReserveSandboxArtifactRejectsLegacyClaimForSessionOwnedPath(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact legacy forgery")

	_, _, err := repo.ReserveSandboxArtifact(ctx, ReserveSandboxArtifactInput{
		SessionID:    enqueued.SessionID,
		RunID:        enqueued.RunID,
		LogicalPath:  "sessions/" + enqueued.SessionID + "/runs/" + enqueued.RunID + "/files/x.txt",
		PhysicalPath: "sessions/" + enqueued.SessionID + "/runs/" + enqueued.RunID + "/files/x.txt",
	})
	if !errors.Is(err, ErrSandboxArtifactPathScope) {
		t.Fatalf("forged legacy claim error = %v, want %v", err, ErrSandboxArtifactPathScope)
	}
}

func TestReserveSandboxArtifactRefusesDeletingSession(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact during deletion")
	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	_, _, err := repo.ReserveSandboxArtifact(ctx, ownedReservation(enqueued, "notes/todo.txt"))
	if !errors.Is(err, ErrSessionDeleting) {
		t.Fatalf("reservation during deletion error = %v, want %v", err, ErrSessionDeleting)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sandbox_artifacts WHERE session_id = ?`, enqueued.SessionID); got != 0 {
		t.Fatalf("refused reservation still wrote %d rows", got)
	}
}

func TestReserveSandboxArtifactRejectsStaleDeletionGeneration(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact stale generation")

	input := ownedReservation(enqueued, "notes/todo.txt")
	input.DeletionGeneration = 7

	if _, _, err := repo.ReserveSandboxArtifact(ctx, input); !errors.Is(err, ErrSandboxArtifactGenerationStale) {
		t.Fatalf("stale generation error = %v, want %v", err, ErrSandboxArtifactGenerationStale)
	}
}

func TestSessionDeletionGenerationTracksReceiptLifecycleVersion(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact generation")

	generation, err := repo.SessionDeletionGeneration(ctx, enqueued.SessionID)
	if err != nil {
		t.Fatalf("SessionDeletionGeneration: %v", err)
	}
	if generation != 0 {
		t.Fatalf("active session generation = %d, want 0", generation)
	}

	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}
	generation, err = repo.SessionDeletionGeneration(ctx, enqueued.SessionID)
	if err != nil {
		t.Fatalf("SessionDeletionGeneration after begin: %v", err)
	}
	if generation != 1 {
		t.Fatalf("deleting session generation = %d, want 1", generation)
	}
}

func TestSessionDeletionGenerationReportsMissingSession(t *testing.T) {
	repo := New(openTestDB(t))

	if _, err := repo.SessionDeletionGeneration(context.Background(), "sess_missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session error = %v, want %v", err, ErrSessionNotFound)
	}
}

func TestFinalizeSandboxArtifactMarksReadyAndRecordsHash(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact finalize")
	reserved, _, err := repo.ReserveSandboxArtifact(ctx, ownedReservation(enqueued, "notes/todo.txt"))
	if err != nil {
		t.Fatal(err)
	}

	finalized, err := repo.FinalizeSandboxArtifact(ctx, FinalizeSandboxArtifactInput{
		ArtifactID: reserved.ArtifactID, SessionID: enqueued.SessionID, RunID: enqueued.RunID,
	})
	if err != nil {
		t.Fatalf("FinalizeSandboxArtifact: %v", err)
	}
	if finalized.State != SandboxArtifactStateReady || finalized.FinalizedAt == "" {
		t.Fatalf("finalized artifact = %+v, want ready with a finalization timestamp", finalized)
	}
}

func TestFinalizeSandboxArtifactRejectsForeignRun(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact finalize foreign")
	other := seedArtifactSession(t, repo, "Artifact finalize other")
	reserved, _, err := repo.ReserveSandboxArtifact(ctx, ownedReservation(enqueued, "notes/todo.txt"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = repo.FinalizeSandboxArtifact(ctx, FinalizeSandboxArtifactInput{
		ArtifactID: reserved.ArtifactID, SessionID: other.SessionID, RunID: other.RunID,
	})
	if !errors.Is(err, ErrSandboxArtifactUnowned) {
		t.Fatalf("foreign finalization error = %v, want %v", err, ErrSandboxArtifactUnowned)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sandbox_artifacts WHERE id = ? AND state = 'writing'`, reserved.ArtifactID); got != 1 {
		t.Fatalf("refused finalization changed the reservation state")
	}
}

func TestFinalizeSandboxArtifactSucceedsWhileSessionIsDeleting(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact finalize during deletion")
	reserved, _, err := repo.ReserveSandboxArtifact(ctx, ownedReservation(enqueued, "notes/todo.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	// The file is already on disk by the time finalization runs, so the manifest
	// must record it. Refusing here would hide a real artifact from cleanup.
	finalized, err := repo.FinalizeSandboxArtifact(ctx, FinalizeSandboxArtifactInput{
		ArtifactID: reserved.ArtifactID, SessionID: enqueued.SessionID, RunID: enqueued.RunID,
	})
	if err != nil {
		t.Fatalf("FinalizeSandboxArtifact during deletion: %v", err)
	}
	if finalized.State != SandboxArtifactStateReady {
		t.Fatalf("state = %q, want %q", finalized.State, SandboxArtifactStateReady)
	}
}

func TestReleaseSandboxArtifactReservationDropsUnwrittenRow(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact release")
	reserved, _, err := repo.ReserveSandboxArtifact(ctx, ownedReservation(enqueued, "notes/todo.txt"))
	if err != nil {
		t.Fatal(err)
	}

	released, err := repo.ReleaseSandboxArtifactReservation(ctx, reserved.ArtifactID, enqueued.SessionID, enqueued.RunID)
	if err != nil {
		t.Fatalf("ReleaseSandboxArtifactReservation: %v", err)
	}
	if !released {
		t.Fatal("release reported no change for a writing reservation")
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sandbox_artifacts WHERE id = ?`, reserved.ArtifactID); got != 0 {
		t.Fatalf("released reservation rows = %d, want 0", got)
	}
}

func TestReleaseSandboxArtifactReservationKeepsFinalizedArtifact(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact release finalized")
	reserved, _, err := repo.ReserveSandboxArtifact(ctx, ownedReservation(enqueued, "notes/todo.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FinalizeSandboxArtifact(ctx, FinalizeSandboxArtifactInput{
		ArtifactID: reserved.ArtifactID, SessionID: enqueued.SessionID, RunID: enqueued.RunID,
	}); err != nil {
		t.Fatal(err)
	}

	released, err := repo.ReleaseSandboxArtifactReservation(ctx, reserved.ArtifactID, enqueued.SessionID, enqueued.RunID)
	if err != nil {
		t.Fatalf("ReleaseSandboxArtifactReservation: %v", err)
	}
	if released {
		t.Fatal("release removed an artifact that was already written to disk")
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sandbox_artifacts WHERE id = ? AND state = 'ready'`, reserved.ArtifactID); got != 1 {
		t.Fatal("finalized artifact row was not retained")
	}
}

func TestSessionSandboxArtifactsListsDeletableWorkBeforeRetainedLegacy(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact listing")
	if _, _, err := repo.ReserveSandboxArtifact(ctx, ReserveSandboxArtifactInput{
		SessionID: enqueued.SessionID, RunID: enqueued.RunID,
		LogicalPath: "legacy.txt", PhysicalPath: "legacy.txt",
	}); err != nil {
		t.Fatal(err)
	}
	owned, _, err := repo.ReserveSandboxArtifact(ctx, ownedReservation(enqueued, "notes/todo.txt"))
	if err != nil {
		t.Fatal(err)
	}

	artifacts, err := repo.SessionSandboxArtifacts(ctx, enqueued.SessionID)
	if err != nil {
		t.Fatalf("SessionSandboxArtifacts: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("artifact count = %d, want 2", len(artifacts))
	}
	if artifacts[0].ArtifactID != owned.ArtifactID {
		t.Fatalf("first artifact = %+v, want the deletable owned artifact first", artifacts[0])
	}
	if artifacts[1].Policy != SandboxArtifactPolicyRetainLegacyUnowned {
		t.Fatalf("second artifact policy = %q, want %q", artifacts[1].Policy, SandboxArtifactPolicyRetainLegacyUnowned)
	}
}

func TestDeleteSandboxArtifactRefusesRetainedLegacyArtifact(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact legacy retention")
	legacy, _, err := repo.ReserveSandboxArtifact(ctx, ReserveSandboxArtifactInput{
		SessionID: enqueued.SessionID, RunID: enqueued.RunID,
		LogicalPath: "legacy.txt", PhysicalPath: "legacy.txt",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteSandboxArtifact(ctx, legacy.ArtifactID); !errors.Is(err, ErrSandboxArtifactRetained) {
		t.Fatalf("delete retained artifact error = %v, want %v", err, ErrSandboxArtifactRetained)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sandbox_artifacts WHERE id = ?`, legacy.ArtifactID); got != 1 {
		t.Fatal("retained legacy artifact row was removed")
	}
}

func TestDeleteSandboxArtifactRemovesOwnedRowAfterCleanup(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact cleanup")
	owned, _, err := repo.ReserveSandboxArtifact(ctx, ownedReservation(enqueued, "notes/todo.txt"))
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteSandboxArtifact(ctx, owned.ArtifactID); err != nil {
		t.Fatalf("DeleteSandboxArtifact: %v", err)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sandbox_artifacts WHERE id = ?`, owned.ArtifactID); got != 0 {
		t.Fatalf("owned artifact rows after cleanup = %d, want 0", got)
	}
	if err := repo.DeleteSandboxArtifact(ctx, owned.ArtifactID); err != nil {
		t.Fatalf("repeated DeleteSandboxArtifact: %v, want idempotent success", err)
	}
}

func TestMarkSandboxArtifactDeleteFailedRecordsExternalFailure(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact delete failure")
	owned, _, err := repo.ReserveSandboxArtifact(ctx, ownedReservation(enqueued, "notes/todo.txt"))
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.MarkSandboxArtifactDeleteFailed(ctx, owned.ArtifactID); err != nil {
		t.Fatalf("MarkSandboxArtifactDeleteFailed: %v", err)
	}
	if got := countRows(t, repo, `SELECT COUNT(*) FROM sandbox_artifacts WHERE id = ? AND state = 'delete_failed'`, owned.ArtifactID); got != 1 {
		t.Fatal("artifact was not marked delete_failed")
	}
}

func TestMarkSandboxArtifactDeleteFailedReportsMissingArtifact(t *testing.T) {
	repo := New(openTestDB(t))

	err := repo.MarkSandboxArtifactDeleteFailed(context.Background(), "artifact_missing")
	if !errors.Is(err, ErrSandboxArtifactNotFound) {
		t.Fatalf("missing artifact error = %v, want %v", err, ErrSandboxArtifactNotFound)
	}
}

func TestCountRetainedLegacySandboxArtifactsCountsOnlyRetainedRows(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Artifact retention count")
	if _, _, err := repo.ReserveSandboxArtifact(ctx, ownedReservation(enqueued, "notes/todo.txt")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ReserveSandboxArtifact(ctx, ReserveSandboxArtifactInput{
		SessionID: enqueued.SessionID, RunID: enqueued.RunID,
		LogicalPath: "legacy.txt", PhysicalPath: "legacy.txt",
	}); err != nil {
		t.Fatal(err)
	}

	retained, err := repo.CountRetainedLegacySandboxArtifacts(ctx, enqueued.SessionID)
	if err != nil {
		t.Fatalf("CountRetainedLegacySandboxArtifacts: %v", err)
	}
	if retained != 1 {
		t.Fatalf("retained count = %d, want 1", retained)
	}
}

func TestCountRetainedLegacySandboxArtifactsCountsDistinctFiles(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	first := seedArtifactSession(t, repo, "Artifact retention distinct")
	second, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: first.SessionID, Content: "again", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, runID := range []string{first.RunID, second.RunID} {
		if _, _, err := repo.ReserveSandboxArtifact(ctx, ReserveSandboxArtifactInput{
			SessionID: first.SessionID, RunID: runID,
			LogicalPath: "legacy.txt", PhysicalPath: "legacy.txt",
		}); err != nil {
			t.Fatal(err)
		}
	}

	retained, err := repo.CountRetainedLegacySandboxArtifacts(ctx, first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if retained != 1 {
		t.Fatalf("retained count = %d, want 1; two runs touching one file is still one retained file", retained)
	}
}

func TestReserveSandboxArtifactAcceptsAnEarlierRunOfTheSameSession(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	first := seedArtifactSession(t, repo, "Artifact across runs")
	second, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: first.SessionID, Content: "update it", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The later run updates the file the earlier run wrote, which still lives
	// at the earlier run's location.
	artifact, created, err := repo.ReserveSandboxArtifact(ctx, ReserveSandboxArtifactInput{
		SessionID:    first.SessionID,
		RunID:        second.RunID,
		LogicalPath:  "notes/todo.txt",
		PhysicalPath: OwnedSandboxPath(first.SessionID, first.RunID, "notes/todo.txt"),
	})
	if err != nil {
		t.Fatalf("cross-run reservation: %v", err)
	}
	if !created || artifact.RunID != second.RunID {
		t.Fatalf("artifact = %+v, want a new row owned by the run doing the write", artifact)
	}
	if artifact.Policy != SandboxArtifactPolicyDeleteOnSessionDelete {
		t.Fatalf("policy = %q, want the session's own subtree to stay deletable", artifact.Policy)
	}
}

func TestReserveSandboxArtifactRejectsAnotherSessionsRunScopedPath(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	owner := seedArtifactSession(t, repo, "Artifact owner session")
	other := seedArtifactSession(t, repo, "Artifact other session")

	_, _, err := repo.ReserveSandboxArtifact(ctx, ReserveSandboxArtifactInput{
		SessionID:    owner.SessionID,
		RunID:        owner.RunID,
		LogicalPath:  "notes/todo.txt",
		PhysicalPath: OwnedSandboxPath(other.SessionID, other.RunID, "notes/todo.txt"),
	})

	if !errors.Is(err, ErrSandboxArtifactPathScope) {
		t.Fatalf("cross-session path error = %v, want %v", err, ErrSandboxArtifactPathScope)
	}
}

func TestSessionWithdrawalStateReportsActiveSession(t *testing.T) {
	repo := New(openTestDB(t))
	enqueued := seedArtifactSession(t, repo, "Withdrawal state active")

	state, err := repo.SessionWithdrawalState(context.Background(), enqueued.SessionID)
	if err != nil {
		t.Fatalf("SessionWithdrawalState: %v", err)
	}

	if !state.Active || state.DeletionGeneration != 0 {
		t.Fatalf("state = %+v, want an active session at generation 0", state)
	}
}

func TestSessionWithdrawalStateReportsWithdrawalInProgress(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued := seedArtifactSession(t, repo, "Withdrawal state deleting")
	if _, err := repo.BeginSessionDeletion(ctx, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	state, err := repo.SessionWithdrawalState(ctx, enqueued.SessionID)
	if err != nil {
		t.Fatalf("SessionWithdrawalState: %v", err)
	}

	if state.Active {
		t.Fatalf("state = %+v, want the session reported as withdrawing", state)
	}
	if state.DeletionGeneration != 1 {
		t.Fatalf("generation = %d, want 1", state.DeletionGeneration)
	}
}

func TestSessionWithdrawalStateReportsMissingSession(t *testing.T) {
	repo := New(openTestDB(t))

	if _, err := repo.SessionWithdrawalState(context.Background(), "sess_missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session error = %v, want %v", err, ErrSessionNotFound)
	}
}
