package sessions

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

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// newVaultBackedServer wires a session service to a repository with a real
// vault on disk, because every property here is about a note file existing or
// not existing after a withdrawal.
func newVaultBackedServer(t *testing.T) (*Server, *repository.Repository, *memoryfiles.Vault, *db.DB) {
	t.Helper()
	database := openSessionTestDB(t)
	repo := repository.New(database)
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
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	server.SetMemoryReconcileCompletion(func(ctx context.Context) error {
		_, err := repo.ReconcileMemoryVault(ctx)
		return err
	})
	return server, repo, vault, database
}

func seedVaultCandidate(t *testing.T, repo *repository.Repository, title string) (string, repository.MemoryCandidate) {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Vault withdrawal")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: session.SessionID,
		Kind:      repository.MemoryCandidateKindBelief,
		Title:     title,
		Body:      "The user keeps " + title + ".",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	return session.SessionID, candidate
}

func seedSandboxArtifactRow(t *testing.T, database *db.DB, sessionID string, artifactID string) string {
	t.Helper()
	ctx := context.Background()
	repo := repository.New(database)
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: sessionID, Content: "write then withdraw", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sandbox_artifacts (
			id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at
		) VALUES (?, ?, ?, ?, ?, 'ready', 'delete_on_session_delete', 0, ?)
	`,
		artifactID, sessionID, enqueued.RunID, "sha256:"+artifactID,
		"sessions/"+sessionID+"/files/"+artifactID+".txt",
		repository.FormatTimestamp(time.Now()),
	); err != nil {
		t.Fatalf("seed sandbox artifact: %v", err)
	}
	return enqueued.RunID
}

// scopedFakeCleaner is a cleaner for exactly one manifest scope that records
// whether it was attempted and can be made to fail on demand.
type scopedFakeCleaner struct {
	mu       sync.Mutex
	scope    string
	calls    int
	forgets  int
	err      error
	delegate SessionArtifactCleaner
	manifest sandboxArtifactManifest
}

func (c *scopedFakeCleaner) ArtifactScope() string { return c.scope }

func (c *scopedFakeCleaner) CleanupSessionArtifacts(ctx context.Context, sessionID string, version int64) error {
	c.mu.Lock()
	c.calls++
	failure := c.err
	delegate := c.delegate
	c.mu.Unlock()
	if failure != nil {
		return failure
	}
	if delegate != nil {
		return delegate.CleanupSessionArtifacts(ctx, sessionID, version)
	}
	return nil
}

func (c *scopedFakeCleaner) ForgetCleanedArtifacts(ctx context.Context, sessionID string) error {
	c.mu.Lock()
	c.forgets++
	delegate := c.delegate
	manifest := c.manifest
	c.mu.Unlock()
	if manifest != nil {
		return forgetSandboxArtifacts(ctx, manifest, sessionID)
	}
	if delegate != nil {
		return delegate.ForgetCleanedArtifacts(ctx, sessionID)
	}
	return nil
}

func (c *scopedFakeCleaner) attempts() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *scopedFakeCleaner) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
}

func auditRowCount(t *testing.T, database *db.DB, action string, target string) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM audit_logs WHERE action = ? AND target = ?
	`, action, target).Scan(&count); err != nil {
		t.Fatalf("count %q audits: %v", action, err)
	}
	return count
}

func auditActionCount(t *testing.T, database *db.DB, action string) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM audit_logs WHERE action = ?
	`, action).Scan(&count); err != nil {
		t.Fatalf("count %q audits: %v", action, err)
	}
	return count
}

// A session that only ever wrote into the vault reaches the same gate the
// sandbox does, and the vault cleaner finishes the withdrawal in the same call.
func TestDeleteSessionCompletesAVaultOnlyPendingCleanup(t *testing.T) {
	server, repo, vault, database := newVaultBackedServer(t)
	sessionID, candidate := seedVaultCandidate(t, repo, "bees")
	server.RegisterArtifactCleaners(NewVaultArtifactCleaner(repo))
	ctx := context.Background()

	var sandboxRows int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sandbox_artifacts WHERE session_id = ?`,
		sessionID).Scan(&sandboxRows); err != nil {
		t.Fatal(err)
	}
	if sandboxRows != 0 {
		t.Fatalf("sandbox rows = %d, want a vault-only session", sandboxRows)
	}

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("receipt = %+v, want completed", response.GetDeletion())
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the candidate file survived the withdrawal: %v", err)
	}
	remaining, err := repo.SessionVaultArtifacts(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("vault manifest rows after the withdrawal = %+v, want none", remaining)
	}
}

// Only the exact literal the pending gate writes may dispatch the cleaners. A
// receipt that failed for any other reason must not be answered by deleting the
// user's files: nothing a cleaner does would unstick it, and the notes it would
// take with it are not what the withdrawal is waiting on.
func TestDeleteSessionDispatchesCleanersOnlyForTheArtifactCleanupPendingGate(t *testing.T) {
	server, repo, vault, database := newVaultBackedServer(t)
	sessionID, candidate := seedVaultCandidate(t, repo, "bees")
	vaultCleaner := &scopedFakeCleaner{scope: ArtifactScopeVault, delegate: NewVaultArtifactCleaner(repo)}
	server.RegisterArtifactCleaners(vaultCleaner)
	ctx := context.Background()

	// An execution nobody has proven is gone. The session owns a vault file,
	// so the only thing keeping the cleaners out is the gate itself.
	if _, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: sessionID, Content: "still running", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-gate"); err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_IN_PROGRESS {
		t.Fatalf("receipt = %+v, want quiescing", response.GetDeletion())
	}
	if vaultCleaner.attempts() != 0 {
		t.Fatalf("cleaner attempts while quiescing = %d, want 0", vaultCleaner.attempts())
	}

	// The drain lease runs out. The receipt fails, but for a reason no cleaner
	// can answer, so it still must not reach the vault.
	if _, err := database.ExecContext(ctx, `
		UPDATE session_deletions SET quiesce_deadline_at = '2000-01-01T00:00:00.000000000Z' WHERE session_id = ?
	`, sessionID); err != nil {
		t.Fatalf("expire the drain lease: %v", err)
	}
	expired, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession after the lease expired: %v", err)
	}
	if expired.GetDeletion().GetErrorCode() != "execution_unreconciled" {
		t.Fatalf("receipt = %+v, want execution_unreconciled", expired.GetDeletion())
	}
	if vaultCleaner.attempts() != 0 {
		t.Fatalf("cleaner attempts for a non-artifact failure = %d, want 0", vaultCleaner.attempts())
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
		t.Fatalf("a withdrawal stuck on an execution deleted the user's note: %v", err)
	}

	// Once the runtime is accounted for, the gate opens and the same cleaner
	// runs — proving the refusal above was the gate and not a missing wiring.
	if err := repo.AcknowledgeExecutionExit(ctx, activeRunID(t, database, sessionID)); err != nil {
		t.Fatalf("AcknowledgeExecutionExit: %v", err)
	}
	drained, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession after the exit: %v", err)
	}
	if vaultCleaner.attempts() != 1 {
		t.Fatalf("cleaner attempts after the pending gate = %d, want 1", vaultCleaner.attempts())
	}
	if drained.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("receipt = %+v, want completed", drained.GetDeletion())
	}
}

func activeRunID(t *testing.T, database *db.DB, sessionID string) string {
	t.Helper()
	var runID string
	if err := database.QueryRowContext(context.Background(), `
		SELECT id FROM agent_runs WHERE session_id = ? AND execution_active = 1 ORDER BY created_at, id LIMIT 1
	`, sessionID).Scan(&runID); err != nil {
		t.Fatalf("read the active run: %v", err)
	}
	return runID
}

// One scope failing must not take the other down with it. Every cleaner is
// attempted, the scope that finished has its rows removed, and only the scope
// that failed is left behind — with no audit row claiming a failure in the
// scope that succeeded.
func TestDeleteSessionKeepsOnlyTheFailingCleanerScope(t *testing.T) {
	for _, failing := range []string{ArtifactScopeSandbox, ArtifactScopeVault} {
		t.Run(failing, func(t *testing.T) {
			server, repo, vault, database := newVaultBackedServer(t)
			sessionID, candidate := seedVaultCandidate(t, repo, "bees")
			seedSandboxArtifactRow(t, database, sessionID, "artifact_scope_"+failing)
			ctx := context.Background()

			sandboxCleaner := &scopedFakeCleaner{scope: ArtifactScopeSandbox, manifest: repo}
			vaultCleaner := &scopedFakeCleaner{scope: ArtifactScopeVault, delegate: NewVaultArtifactCleaner(repo)}
			failure := errors.New("cleanup transport unavailable")
			if failing == ArtifactScopeSandbox {
				sandboxCleaner.fail(failure)
			} else {
				vaultCleaner.fail(failure)
			}
			server.RegisterArtifactCleaners(sandboxCleaner, vaultCleaner)

			response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
			if err != nil {
				t.Fatalf("DeleteSession: %v", err)
			}
			if sandboxCleaner.attempts() != 1 || vaultCleaner.attempts() != 1 {
				t.Fatalf("attempts = (sandbox %d, vault %d), want every cleaner attempted",
					sandboxCleaner.attempts(), vaultCleaner.attempts())
			}
			if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL ||
				!response.GetDeletion().GetRetryable() {
				t.Fatalf("receipt = %+v, want retryable failed_external", response.GetDeletion())
			}
			var sessionRows int
			if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`,
				sessionID).Scan(&sessionRows); err != nil {
				t.Fatal(err)
			}
			if sessionRows != 1 {
				t.Fatalf("session rows = %d, want the withdrawal held open", sessionRows)
			}

			sandboxRows, err := repo.SessionSandboxArtifacts(ctx, sessionID)
			if err != nil {
				t.Fatal(err)
			}
			vaultRows, err := repo.SessionVaultArtifacts(ctx, sessionID)
			if err != nil {
				t.Fatal(err)
			}
			if failing == ArtifactScopeSandbox {
				if len(sandboxRows) != 1 || sandboxRows[0].State != repository.SandboxArtifactStateDeleteFailed {
					t.Fatalf("sandbox rows = %+v, want the failing scope retained", sandboxRows)
				}
				if len(vaultRows) != 0 {
					t.Fatalf("vault rows = %+v, want the finished scope drained", vaultRows)
				}
				if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
					t.Fatalf("the vault cleaner did not finish its own scope: %v", err)
				}
				if got := auditActionCount(t, database, "session.vault_artifact.cleanup.failed"); got != 0 {
					t.Fatalf("vault failure audits = %d, want none for a sandbox failure", got)
				}
				if got := auditRowCount(t, database, "session.artifact.cleanup.failed", "artifact_scope_"+failing); got != 1 {
					t.Fatalf("sandbox failure audits = %d, want 1", got)
				}
			} else {
				if len(vaultRows) != 1 || vaultRows[0].State != repository.VaultArtifactStateDeleteFailed {
					t.Fatalf("vault rows = %+v, want the failing scope retained", vaultRows)
				}
				if len(sandboxRows) != 0 {
					t.Fatalf("sandbox rows = %+v, want the finished scope drained", sandboxRows)
				}
				if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
					t.Fatalf("a failed vault cleanup removed the file anyway: %v", err)
				}
				if got := auditRowCount(t, database, "session.artifact.cleanup.failed", "artifact_scope_"+failing); got != 0 {
					t.Fatalf("sandbox failure audits = %d, want none for a vault failure", got)
				}
				if got := auditRowCount(t, database, "session.vault_artifact.cleanup.failed", vaultRows[0].ArtifactID); got != 1 {
					t.Fatalf("vault failure audits = %d, want 1", got)
				}
			}

			// The retry reruns every cleaner, not just the one that failed.
			sandboxCleaner.fail(nil)
			vaultCleaner.fail(nil)
			retry, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
			if err != nil {
				t.Fatalf("retry DeleteSession: %v", err)
			}
			if sandboxCleaner.attempts() != 2 || vaultCleaner.attempts() != 2 {
				t.Fatalf("retry attempts = (sandbox %d, vault %d), want every cleaner rerun",
					sandboxCleaner.attempts(), vaultCleaner.attempts())
			}
			if retry.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
				t.Fatalf("retry receipt = %+v, want completed", retry.GetDeletion())
			}
			if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
				t.Fatalf("the retry left the candidate file behind: %v", err)
			}
		})
	}
}

// Registration is a list, not an order of precedence. The same failure produces
// the same rows, the same audits and the same receipt whichever way round the
// cleaners were registered.
func TestDeleteSessionOutcomeIsIndependentOfCleanerRegistrationOrder(t *testing.T) {
	type outcome struct {
		state      turingv1.SessionDeletionState
		errorCode  string
		vaultRows  int
		sandboxRow string
	}
	run := func(t *testing.T, vaultFirst bool) outcome {
		t.Helper()
		server, repo, _, database := newVaultBackedServer(t)
		sessionID, _ := seedVaultCandidate(t, repo, "bees")
		seedSandboxArtifactRow(t, database, sessionID, "artifact_order")
		ctx := context.Background()
		sandboxCleaner := &scopedFakeCleaner{scope: ArtifactScopeSandbox, manifest: repo}
		vaultCleaner := &scopedFakeCleaner{scope: ArtifactScopeVault, delegate: NewVaultArtifactCleaner(repo)}
		vaultCleaner.fail(errors.New("vault unavailable"))
		if vaultFirst {
			server.RegisterArtifactCleaners(vaultCleaner, sandboxCleaner)
		} else {
			server.RegisterArtifactCleaners(sandboxCleaner, vaultCleaner)
		}

		response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
		if err != nil {
			t.Fatalf("DeleteSession: %v", err)
		}
		if sandboxCleaner.attempts() != 1 || vaultCleaner.attempts() != 1 {
			t.Fatalf("attempts = (sandbox %d, vault %d), want both attempted",
				sandboxCleaner.attempts(), vaultCleaner.attempts())
		}
		vaultRows, err := repo.SessionVaultArtifacts(ctx, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		sandboxRows, err := repo.SessionSandboxArtifacts(ctx, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		result := outcome{
			state:     response.GetDeletion().GetState(),
			errorCode: response.GetDeletion().GetErrorCode(),
			vaultRows: len(vaultRows),
		}
		if len(sandboxRows) > 0 {
			result.sandboxRow = sandboxRows[0].State
		}
		return result
	}

	var sandboxFirst, vaultFirst outcome
	t.Run("sandbox_first", func(t *testing.T) { sandboxFirst = run(t, false) })
	t.Run("vault_first", func(t *testing.T) { vaultFirst = run(t, true) })
	if sandboxFirst != vaultFirst {
		t.Fatalf("registration order changed the outcome: %+v vs %+v", sandboxFirst, vaultFirst)
	}
	if sandboxFirst.state != turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL ||
		sandboxFirst.vaultRows != 1 || sandboxFirst.sandboxRow != "" {
		t.Fatalf("outcome = %+v, want a retained vault row and a drained sandbox scope", sandboxFirst)
	}
}

// A vault row a previous pass could not delete is still work. The retry has to
// pick it up and drain it rather than reporting a completed withdrawal over a
// note that is still in the user's vault.
func TestDeleteSessionRetriesVaultRowsAPreviousPassMarkedFailed(t *testing.T) {
	server, repo, vault, _ := newVaultBackedServer(t)
	sessionID, candidate := seedVaultCandidate(t, repo, "bees")
	server.RegisterArtifactCleaners(NewVaultArtifactCleaner(repo))
	ctx := context.Background()

	if _, err := repo.BeginSessionDeletion(ctx, sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	if err := repo.MarkSessionDeletionVaultFailure(ctx, sessionID, "vault_artifact_cleanup_failed"); err != nil {
		t.Fatalf("MarkSessionDeletionVaultFailure: %v", err)
	}
	failed, err := repo.SessionVaultArtifacts(ctx, sessionID)
	if err != nil || len(failed) != 1 || failed[0].State != repository.VaultArtifactStateDeleteFailed {
		t.Fatalf("seeded rows = (%+v, %v), want one delete_failed row", failed, err)
	}

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("receipt = %+v, want completed", response.GetDeletion())
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the delete_failed row was never retried: %v", err)
	}
}

// The withdrawal rewrites the belief's citations as part of finishing, not on
// the next restart, and a completion that cannot write says so instead of
// claiming the session is gone while a belief still names it.
func TestDeleteSessionWithdrawsPromotedBeliefCitationsOnCompletion(t *testing.T) {
	server, repo, vault, _ := newVaultBackedServer(t)
	sessionID, candidate := seedVaultCandidate(t, repo, "bees")
	server.RegisterArtifactCleaners(NewVaultArtifactCleaner(repo))
	ctx := context.Background()
	note, err := repo.PromoteMemoryCandidate(ctx, repository.MemoryCandidateDecision{CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatalf("PromoteMemoryCandidate: %v", err)
	}

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("receipt = %+v, want completed", response.GetDeletion())
	}
	content, err := os.ReadFile(filepath.Join(vault.Root(), filepath.FromSlash(note.Path)))
	if err != nil {
		t.Fatalf("read the promoted belief: %v", err)
	}
	if strings.Contains(string(content), sessionID) {
		t.Fatalf("the belief still cites the deleted conversation: %q", content)
	}
	if !strings.Contains(string(content), memoryfiles.WithdrawnRefsMarker) {
		t.Fatalf("the belief does not say its evidence was withdrawn: %q", content)
	}
}

// A completion that cannot finish leaves a retryable receipt, never a
// completed one: a belief citing a conversation Turing reported deleted is the
// exact state this is here to prevent.
func TestDeleteSessionStaysRetryableWhenTheCompletionCannotWrite(t *testing.T) {
	server, repo, _, _ := newVaultBackedServer(t)
	sessionID, _ := seedVaultCandidate(t, repo, "bees")
	server.RegisterArtifactCleaners(NewVaultArtifactCleaner(repo))
	ctx := context.Background()
	server.SetMemoryReconcileCompletion(func(context.Context) error {
		return errors.New("the vault could not be rewritten")
	})

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL ||
		!response.GetDeletion().GetRetryable() {
		t.Fatalf("receipt = %+v, want retryable failed_external", response.GetDeletion())
	}
	if code := response.GetDeletion().GetErrorCode(); code != "memory_reconcile_failed" {
		t.Fatalf("error code = %q, want the opaque memory_reconcile_failed class", code)
	}
}

// Two withdrawals and a vault pass at once must all finish. The cleaner, the
// repository transactions and the vault-wide lock are taken in one order, so
// none of them can wait on another that is waiting on it.
func TestDeleteSessionDoesNotDeadlockWithAConcurrentVaultPass(t *testing.T) {
	server, repo, _, _ := newVaultBackedServer(t)
	server.RegisterArtifactCleaners(NewVaultArtifactCleaner(repo))
	first, _ := seedVaultCandidate(t, repo, "bees")
	second, _ := seedVaultCandidate(t, repo, "chickens")

	var wg sync.WaitGroup
	failures := make(chan error, 3)
	for _, sessionID := range []string{first, second} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if _, err := server.DeleteSession(context.Background(), &turingv1.DeleteSessionRequest{SessionId: id}); err != nil {
				failures <- err
			}
		}(sessionID)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := repo.ReconcileMemoryVault(context.Background()); err != nil {
			failures <- err
		}
	}()

	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent withdrawals and a vault pass deadlocked")
	}
	close(failures)
	for err := range failures {
		t.Fatalf("concurrent pass: %v", err)
	}
}

type stubVaultReconciler struct {
	calls int
	err   error
}

func (r *stubVaultReconciler) ReconcileMemoryVault(context.Context) (repository.MemoryReconcileReport, error) {
	r.calls++
	return repository.MemoryReconcileReport{}, r.err
}

// An install the user never gave a vault owes the withdrawal nothing on disk.
// Reporting that as an unfinished completion would leave every deletion on such
// an install permanently retryable over a promise it had already kept.
func TestMemoryReconcileCompletionTreatsAnAbsentVaultAsNothingOwed(t *testing.T) {
	unavailable := &stubVaultReconciler{err: repository.ErrMemoryVaultUnavailable}
	if err := NewMemoryReconcileCompletion(unavailable)(context.Background()); err != nil {
		t.Fatalf("completion with no vault = %v, want nil", err)
	}
	if unavailable.calls != 1 {
		t.Fatalf("reconcile calls = %d, want 1", unavailable.calls)
	}

	// Every other failure is still a withdrawal that has not finished.
	broken := &stubVaultReconciler{err: errors.New("the vault could not be rewritten")}
	if err := NewMemoryReconcileCompletion(broken)(context.Background()); err == nil {
		t.Fatal("completion swallowed a real reconcile failure")
	}
}

// The same, end to end: a sandbox-only withdrawal on an install with no vault
// reaches completion rather than sticking on a scope that owns nothing.
func TestDeleteSessionCompletesOnAnInstallWithNoVault(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	server.SetMemoryReconcileCompletion(NewMemoryReconcileCompletion(repo))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "No vault here")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	seedSandboxArtifactRow(t, database, session.SessionID, "artifact_no_vault")
	sandboxCleaner := &scopedFakeCleaner{scope: ArtifactScopeSandbox, manifest: repo}
	server.RegisterArtifactCleaners(sandboxCleaner, NewVaultArtifactCleaner(repo))

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("receipt = %+v, want completed", response.GetDeletion())
	}
}

// Both scopes failing at once is one withdrawal, so it reports one class. Each
// manifest still keeps its own rows and its own audit action, which is where a
// reader finds out which store could not be reached.
func TestDeleteSessionReportsOneClassWhenEveryScopeFails(t *testing.T) {
	server, repo, vault, database := newVaultBackedServer(t)
	sessionID, candidate := seedVaultCandidate(t, repo, "bees")
	seedSandboxArtifactRow(t, database, sessionID, "artifact_both_fail")
	ctx := context.Background()

	sandboxCleaner := &scopedFakeCleaner{scope: ArtifactScopeSandbox, manifest: repo}
	vaultCleaner := &scopedFakeCleaner{scope: ArtifactScopeVault, delegate: NewVaultArtifactCleaner(repo)}
	sandboxCleaner.fail(errors.New("sandbox unavailable"))
	vaultCleaner.fail(errors.New("vault unavailable"))
	server.RegisterArtifactCleaners(sandboxCleaner, vaultCleaner)

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if sandboxCleaner.attempts() != 1 || vaultCleaner.attempts() != 1 {
		t.Fatalf("attempts = (sandbox %d, vault %d), want both attempted",
			sandboxCleaner.attempts(), vaultCleaner.attempts())
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL ||
		!response.GetDeletion().GetRetryable() {
		t.Fatalf("receipt = %+v, want retryable failed_external", response.GetDeletion())
	}
	if code := response.GetDeletion().GetErrorCode(); code != "artifact_cleanup_failed" {
		t.Fatalf("error code = %q, want the one general artifact class", code)
	}

	sandboxRows, err := repo.SessionSandboxArtifacts(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	vaultRows, err := repo.SessionVaultArtifacts(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxRows) != 1 || sandboxRows[0].State != repository.SandboxArtifactStateDeleteFailed {
		t.Fatalf("sandbox rows = %+v, want the failing scope retained", sandboxRows)
	}
	if len(vaultRows) != 1 || vaultRows[0].State != repository.VaultArtifactStateDeleteFailed {
		t.Fatalf("vault rows = %+v, want the failing scope retained", vaultRows)
	}
	if got := auditRowCount(t, database, "session.artifact.cleanup.failed", "artifact_both_fail"); got != 1 {
		t.Fatalf("sandbox failure audits = %d, want 1", got)
	}
	if got := auditRowCount(t, database, "session.vault_artifact.cleanup.failed", vaultRows[0].ArtifactID); got != 1 {
		t.Fatalf("vault failure audits = %d, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
		t.Fatalf("a failed vault cleanup removed the file anyway: %v", err)
	}

	sandboxCleaner.fail(nil)
	vaultCleaner.fail(nil)
	retry, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("retry DeleteSession: %v", err)
	}
	if retry.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("retry receipt = %+v, want completed", retry.GetDeletion())
	}
}

// failingForgetCleaner finishes its external removal and then cannot record
// that it did. That is the shape of a storage failure between deleting the
// files and dropping the manifest rows that named them.
type failingForgetCleaner struct {
	mu       sync.Mutex
	scope    string
	cleanups int
	forgets  int
	manifest sandboxArtifactManifest
	forgetFn func() error
}

func (c *failingForgetCleaner) ArtifactScope() string { return c.scope }

func (c *failingForgetCleaner) CleanupSessionArtifacts(context.Context, string, int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanups++
	return nil
}

func (c *failingForgetCleaner) ForgetCleanedArtifacts(ctx context.Context, sessionID string) error {
	c.mu.Lock()
	c.forgets++
	forgetFn := c.forgetFn
	manifest := c.manifest
	c.mu.Unlock()
	if forgetFn != nil {
		if err := forgetFn(); err != nil {
			return err
		}
	}
	return forgetSandboxArtifacts(ctx, manifest, sessionID)
}

func (c *failingForgetCleaner) heal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forgetFn = nil
}

// The vault has its own failure class because it is the one a user can act on
// by closing their editor. Reporting a vault-only failure under the general
// artifact class tells them to go looking in a sandbox they cannot see, for a
// file that is sitting in the folder they have open.
func TestDeleteSessionReportsTheExactVaultClassForAVaultOnlyCleanerFailure(t *testing.T) {
	server, repo, vault, _ := newVaultBackedServer(t)
	sessionID, candidate := seedVaultCandidate(t, repo, "bees")
	vaultCleaner := &scopedFakeCleaner{scope: ArtifactScopeVault, delegate: NewVaultArtifactCleaner(repo)}
	vaultCleaner.fail(errors.New("the vault is unreachable"))
	server.RegisterArtifactCleaners(vaultCleaner)
	ctx := context.Background()

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if got := response.GetDeletion().GetErrorCode(); got != repository.SessionDeletionVaultCleanupFailed {
		t.Fatalf("error code = %q, want exactly %q", got, repository.SessionDeletionVaultCleanupFailed)
	}
	if got := response.GetDeletion().GetErrorCode(); got == repository.SessionDeletionSandboxCleanupFailed {
		t.Fatalf("a vault failure reported the general sandbox class %q", got)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL ||
		!response.GetDeletion().GetRetryable() {
		t.Fatalf("receipt = %+v, want retryable failed_external", response.GetDeletion())
	}
	// The persisted receipt is what a restart and the client's recovery list
	// read, so the class has to survive the round trip too.
	persisted, err := repo.SessionDeletionReceipt(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionDeletionReceipt: %v", err)
	}
	if persisted.ErrorCode != repository.SessionDeletionVaultCleanupFailed {
		t.Fatalf("persisted error code = %q, want exactly %q",
			persisted.ErrorCode, repository.SessionDeletionVaultCleanupFailed)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); err != nil {
		t.Fatalf("a failed vault cleanup removed the note anyway: %v", err)
	}
}

// The files are gone and the rows that named them are not. That is a manifest
// this withdrawal could not finalize — it is not a file Turing could not
// delete, and recording it as one writes a per-file audit row claiming a
// failure for every note the user's own withdrawal successfully removed.
func TestDeleteSessionSeparatesAManifestFinalizationFailureFromADeletionFailure(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Finalize the manifest")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	seedSandboxArtifactRow(t, database, session.SessionID, "artifact_finalize")
	cleaner := &failingForgetCleaner{
		scope:    ArtifactScopeSandbox,
		manifest: repo,
		forgetFn: func() error { return errors.New("database is locked") },
	}
	server.RegisterArtifactCleaners(cleaner)

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL ||
		!response.GetDeletion().GetRetryable() {
		t.Fatalf("receipt = %+v, want retryable failed_external", response.GetDeletion())
	}
	if got := response.GetDeletion().GetErrorCode(); got != repository.SessionDeletionArtifactManifestFinalizeFailed {
		t.Fatalf("error code = %q, want the distinct %q class",
			got, repository.SessionDeletionArtifactManifestFinalizeFailed)
	}
	if got := auditRowCount(t, database, "session.artifact.cleanup.failed", "artifact_finalize"); got != 0 {
		t.Fatalf("per-file deletion-failure audits = %d, want none for a file that was deleted", got)
	}
	// The rows are the retry's worklist. They must survive, and they must not
	// be relabelled as files the cleanup could not remove.
	rows, err := repo.SessionSandboxArtifacts(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("SessionSandboxArtifacts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("sandbox rows = %+v, want the manifest preserved for the retry", rows)
	}
	if rows[0].State == repository.SandboxArtifactStateDeleteFailed {
		t.Fatalf("row state = %q, want it not relabelled as an undeleted file", rows[0].State)
	}

	// Removal is idempotent, so the retry reruns it over files that are
	// already gone and finally drains the rows.
	cleaner.heal()
	retry, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("retry DeleteSession: %v", err)
	}
	if retry.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("retry receipt = %+v, want completed", retry.GetDeletion())
	}
	if got := auditRowCount(t, database, "session.artifact.cleanup.failed", "artifact_finalize"); got != 0 {
		t.Fatalf("per-file deletion-failure audits after the retry = %d, want none", got)
	}
	remaining, err := repo.SessionSandboxArtifacts(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("SessionSandboxArtifacts after the retry: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("sandbox rows after the retry = %+v, want the manifest drained", remaining)
	}
}

// A cleaner registered under a scope this withdrawal has no manifest for is a
// wiring mistake, and the only safe answer is to say so. Marking nothing and
// handing back the pending gate reports a withdrawal that is politely waiting
// for a cleaner that already failed, and it waits forever.
func TestDeleteSessionFailsClosedOnAnUnknownArtifactScope(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Unknown scope")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	seedSandboxArtifactRow(t, database, session.SessionID, "artifact_unknown_scope")
	stranger := &scopedFakeCleaner{scope: "object_store"}
	stranger.fail(errors.New("object store unavailable"))
	server.RegisterArtifactCleaners(stranger)

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL ||
		!response.GetDeletion().GetRetryable() {
		t.Fatalf("receipt = %+v, want retryable failed_external", response.GetDeletion())
	}
	if got := response.GetDeletion().GetErrorCode(); got != repository.SessionDeletionUnsupportedArtifactScope {
		t.Fatalf("error code = %q, want the explicit %q class",
			got, repository.SessionDeletionUnsupportedArtifactScope)
	}
	if got := response.GetDeletion().GetErrorCode(); got == repository.SessionDeletionArtifactCleanupPending {
		t.Fatalf("an unknown scope reported the pending gate %q rather than failing closed", got)
	}
	persisted, err := repo.SessionDeletionReceipt(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("SessionDeletionReceipt: %v", err)
	}
	if persisted.ErrorCode != repository.SessionDeletionUnsupportedArtifactScope || !persisted.Retryable {
		t.Fatalf("persisted receipt = %+v, want a retryable unsupported-scope failure", persisted)
	}
	// Nothing was marked in a manifest the failing cleaner does not own.
	rows, err := repo.SessionSandboxArtifacts(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("SessionSandboxArtifacts: %v", err)
	}
	if len(rows) != 1 || rows[0].State == repository.SandboxArtifactStateDeleteFailed {
		t.Fatalf("sandbox rows = %+v, want them untouched by a stranger's failure", rows)
	}
	if got := auditRowCount(t, database, "session.artifact.cleanup.failed", "artifact_unknown_scope"); got != 0 {
		t.Fatalf("sandbox failure audits = %d, want none for a scope with no manifest", got)
	}
}

// The count of files the sandbox does not own reaches the client through the
// receipt. A withdrawal that failed its completion and was retried has already
// cascaded away the rows the count came from, and a response that answers zero
// there tells the user nothing was left behind while the files are still on
// their disk.
func TestDeleteSessionKeepsTheRetainedLegacyCountAcrossACompletionRetry(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Retained across a retry")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "touch legacy", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sandbox_artifacts (
			id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at
		) VALUES (?, ?, ?, ?, ?, 'ready', 'retain_legacy_unowned', 0, ?)
	`,
		"artifact_legacy_across_retry", session.SessionID, enqueued.RunID,
		"sha256:legacy", "legacy.txt", repository.FormatTimestamp(time.Now()),
	); err != nil {
		t.Fatalf("seed legacy artifact: %v", err)
	}
	completionErr := errors.New("the vault could not be rewritten")
	server.SetMemoryReconcileCompletion(func(context.Context) error { return completionErr })

	failed, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if got := failed.GetDeletion().GetRetainedLegacyArtifactCount(); got != 1 {
		t.Fatalf("retained legacy count on the failing pass = %d, want 1", got)
	}

	completionErr = nil
	server.SetMemoryReconcileCompletion(func(context.Context) error { return nil })
	retry, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("retry DeleteSession: %v", err)
	}
	if retry.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("retry receipt = %+v, want completed", retry.GetDeletion())
	}
	if got := retry.GetDeletion().GetRetainedLegacyArtifactCount(); got != 1 {
		t.Fatalf("retained legacy count in the retry response = %d, want it preserved at 1", got)
	}
	persisted, err := repo.SessionDeletionReceipt(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("SessionDeletionReceipt: %v", err)
	}
	if persisted.RetainedLegacyArtifactCount != 1 {
		t.Fatalf("persisted retained legacy count = %d, want 1", persisted.RetainedLegacyArtifactCount)
	}
}

// finalizeOnlyPurger removes every note the way the real purge does and then
// refuses the manifest write. It is the vault's version of a storage failure
// landing between the deletion and the record of it.
type finalizeOnlyPurger struct {
	mu     sync.Mutex
	repo   *repository.Repository
	vault  *memoryfiles.Vault
	broken bool
}

func (p *finalizeOnlyPurger) PurgeSessionVaultArtifacts(ctx context.Context, sessionID string) (int, error) {
	p.mu.Lock()
	broken := p.broken
	p.mu.Unlock()
	if !broken {
		return p.repo.PurgeSessionVaultArtifacts(ctx, sessionID)
	}
	pending, err := p.repo.PendingSessionVaultArtifacts(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	for _, artifact := range pending {
		if err := p.vault.RemoveInboxNote(ctx, memoryfiles.RemoveInboxNoteRequest{RelPath: artifact.VaultPath, Mode: memoryfiles.RemoveRetiredCandidate}); err != nil {
			return 0, err
		}
	}
	// The notes are gone and the rows that named them are untouched.
	return 0, errors.Join(
		repository.ErrVaultArtifactManifestFinalize,
		errors.New("vault manifest is unwritable"),
	)
}

func (p *finalizeOnlyPurger) heal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.broken = false
}

// The vault reaches the same split the sandbox does. A note the user asked to
// have withdrawn, which Turing did in fact delete, must never be recorded as a
// file Turing could not delete — the audit log is what a user reads to learn
// what is still on their disk, and a false entry there is the same lie whether
// it comes from the sandbox manifest or the vault one.
func TestDeleteSessionDoesNotBlameTheVaultForAManifestItCouldNotFinalize(t *testing.T) {
	server, repo, vault, database := newVaultBackedServer(t)
	sessionID, candidate := seedVaultCandidate(t, repo, "bees")
	purger := &finalizeOnlyPurger{repo: repo, vault: vault, broken: true}
	server.RegisterArtifactCleaners(NewVaultArtifactCleaner(purger))
	ctx := context.Background()

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if got := response.GetDeletion().GetErrorCode(); got != repository.SessionDeletionArtifactManifestFinalizeFailed {
		t.Fatalf("error code = %q, want the distinct %q class",
			got, repository.SessionDeletionArtifactManifestFinalizeFailed)
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
		t.Fatalf("the note survived, so this was not a finalize-only failure: %v", err)
	}
	if got := auditActionCount(t, database, "session.vault_artifact.cleanup.failed"); got != 0 {
		t.Fatalf("per-note deletion-failure audits = %d, want none for a note that was deleted", got)
	}
	rows, err := repo.SessionVaultArtifacts(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("vault rows = %+v, want the manifest preserved for the retry", rows)
	}
	if rows[0].State == repository.VaultArtifactStateDeleteFailed {
		t.Fatalf("row state = %q, want it not relabelled as an undeleted note", rows[0].State)
	}

	// Removal is idempotent, so the retry runs over a note that is already gone
	// and finally drains the row.
	purger.heal()
	retry, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("retry DeleteSession: %v", err)
	}
	if retry.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("retry receipt = %+v, want completed", retry.GetDeletion())
	}
	if got := auditActionCount(t, database, "session.vault_artifact.cleanup.failed"); got != 0 {
		t.Fatalf("per-note deletion-failure audits after the retry = %d, want none", got)
	}
}

// A stranger scope is a wiring mistake wherever in the pass it surfaces. One
// that removes files and then cannot drop its rows has to be reported as the
// misconfiguration it is, not as an ordinary manifest failure that a retry
// would eventually clear — no retry clears a cleaner nothing here knows how to
// mark.
func TestDeleteSessionFailsClosedOnAnUnknownScopeThatOnlyFailsToFinalize(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Unknown finalize scope")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	seedSandboxArtifactRow(t, database, session.SessionID, "artifact_unknown_finalize")
	stranger := &failingForgetCleaner{
		scope:    "object_store",
		forgetFn: func() error { return errors.New("object store manifest unwritable") },
	}
	server.RegisterArtifactCleaners(stranger)

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if got := response.GetDeletion().GetErrorCode(); got != repository.SessionDeletionUnsupportedArtifactScope {
		t.Fatalf("error code = %q, want the explicit %q class",
			got, repository.SessionDeletionUnsupportedArtifactScope)
	}
	if !response.GetDeletion().GetRetryable() {
		t.Fatalf("receipt = %+v, want it retryable", response.GetDeletion())
	}
}

// A misconfigured cleaner must not cost a real failure its evidence. The
// stranger decides the receipt's class, because no retry fixes a scope nothing
// here can mark — but the sandbox files that genuinely could not be deleted are
// still on the user's disk, and the rows naming them still have to say so.
func TestDeleteSessionStillMarksAKnownScopeWhenAStrangerScopeAlsoFails(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Stranger beside a real failure")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	seedSandboxArtifactRow(t, database, session.SessionID, "artifact_real_failure")
	sandbox := &scopedFakeCleaner{scope: ArtifactScopeSandbox}
	sandbox.fail(errors.New("sandbox unreachable"))
	stranger := &scopedFakeCleaner{scope: "object_store"}
	stranger.fail(errors.New("object store unavailable"))
	server.RegisterArtifactCleaners(sandbox, stranger)

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if got := response.GetDeletion().GetErrorCode(); got != repository.SessionDeletionUnsupportedArtifactScope {
		t.Fatalf("error code = %q, want the stranger to decide the class", got)
	}
	rows, err := repo.SessionSandboxArtifacts(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("SessionSandboxArtifacts: %v", err)
	}
	if len(rows) != 1 || rows[0].State != repository.SandboxArtifactStateDeleteFailed {
		t.Fatalf("sandbox rows = %+v, want the real failure still marked delete_failed", rows)
	}
	if got := auditRowCount(t, database, "session.artifact.cleanup.failed", "artifact_real_failure"); got != 1 {
		t.Fatalf("audits for the genuinely undeleted file = %d, want 1", got)
	}
}

// refuseVaultArtifactForget makes the manifest write for exactly one row fail,
// the way storage fails between deleting a note and recording that it is gone.
// The refusal is installed in the database rather than in a stand-in for the
// repository, because what is under test is how the real purge classifies a
// pass that failed in two ways at once.
func refuseVaultArtifactForget(t *testing.T, database *db.DB, artifactID string) func() {
	t.Helper()
	// A trigger body cannot carry bind variables, and the id is one this test
	// just generated.
	if _, err := database.ExecContext(context.Background(), fmt.Sprintf(`
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
		if _, err := database.ExecContext(context.Background(), `DROP TRIGGER refuse_vault_artifact_forget`); err != nil {
			t.Fatalf("heal the manifest write: %v", err)
		}
	}
	t.Cleanup(heal)
	return heal
}

// seedSecondVaultCandidate gives a session a second note, so a pass can have
// one row it cannot remove and one it can.
func seedSecondVaultCandidate(t *testing.T, repo *repository.Repository, sessionID string, title string) repository.MemoryCandidate {
	t.Helper()
	candidate, err := repo.CreateMemoryCandidate(context.Background(), repository.CreateMemoryCandidateInput{
		SessionID: sessionID,
		Kind:      repository.MemoryCandidateKindBelief,
		Title:     title,
		Body:      "The user keeps " + title + ".",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate(%q): %v", title, err)
	}
	return candidate
}

// vaultArtifactByPath finds the manifest row naming one note.
func vaultArtifactByPath(t *testing.T, repo *repository.Repository, sessionID string, vaultPath string) repository.VaultArtifact {
	t.Helper()
	artifacts, err := repo.SessionVaultArtifacts(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	for _, artifact := range artifacts {
		if artifact.VaultPath == vaultPath {
			return artifact
		}
	}
	t.Fatalf("no manifest row for %q in %+v", vaultPath, artifacts)
	return repository.VaultArtifact{}
}

// A withdrawal can fail in both directions in the same pass: one note the vault
// will not release, and the rows for the notes it did release refusing to leave
// the manifest.
//
// The failure that touched the user's data is already written down per row, by
// the pass that observed it. What must not happen is the withdrawal answering
// the second failure with the session-wide vault marker: that marks every row
// still standing, and the rows still standing here include one naming a note
// Turing did delete — an audit entry telling the user a note they asked to have
// removed, and which is gone, is still on their disk.
func TestDeleteSessionDoesNotBlameADeletedNoteForAPoisonedRowBesideIt(t *testing.T) {
	server, repo, vault, database := newVaultBackedServer(t)
	sessionID, poisonedCandidate := seedVaultCandidate(t, repo, "bees")
	removableCandidate := seedSecondVaultCandidate(t, repo, sessionID, "chickens")
	beliefPath := filepath.Join(vault.Root(), "beliefs", "precious.md")
	if err := os.WriteFile(beliefPath, []byte("# A file the user owns\n"), 0o600); err != nil {
		t.Fatalf("write the belief: %v", err)
	}
	poisoned := vaultArtifactByPath(t, repo, sessionID, poisonedCandidate.InboxPath)
	removable := vaultArtifactByPath(t, repo, sessionID, removableCandidate.InboxPath)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `
		UPDATE vault_artifacts SET vault_path = ? WHERE id = ?
	`, "beliefs/precious.md", poisoned.ArtifactID); err != nil {
		t.Fatalf("tamper with the manifest row: %v", err)
	}
	heal := refuseVaultArtifactForget(t, database, removable.ArtifactID)
	server.RegisterArtifactCleaners(NewVaultArtifactCleaner(repo))

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL ||
		!response.GetDeletion().GetRetryable() {
		t.Fatalf("receipt = %+v, want retryable failed_external", response.GetDeletion())
	}
	if got := response.GetDeletion().GetErrorCode(); got != repository.SessionDeletionArtifactManifestFinalizeFailed {
		t.Fatalf("error code = %q, want the distinct %q class",
			got, repository.SessionDeletionArtifactManifestFinalizeFailed)
	}

	// What actually happened on disk: one note gone, one note the pass refused
	// to touch, and the belief a tampered row named untouched.
	if _, statErr := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(removableCandidate.InboxPath))); !os.IsNotExist(statErr) {
		t.Fatalf("the removable note survived, so this was not the compound failure: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(poisonedCandidate.InboxPath))); statErr != nil {
		t.Fatalf("the poisoned row's own note was removed anyway: %v", statErr)
	}
	if _, statErr := os.Stat(beliefPath); statErr != nil {
		t.Fatalf("a tampered manifest row deleted the belief: %v", statErr)
	}

	rows, err := repo.SessionVaultArtifacts(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("vault rows = %+v, want both preserved for the retry", rows)
	}
	states := make(map[string]string, len(rows))
	for _, row := range rows {
		states[row.ArtifactID] = row.State
	}
	if states[poisoned.ArtifactID] != repository.VaultArtifactStateDeleteFailed {
		t.Fatalf("the row naming a note still on disk = %q, want delete_failed", states[poisoned.ArtifactID])
	}
	if states[removable.ArtifactID] == repository.VaultArtifactStateDeleteFailed {
		t.Fatalf("the deleted note's row = %q, want it not relabelled as undeleted",
			states[removable.ArtifactID])
	}
	if got := auditRowCount(t, database, "session.vault_artifact.cleanup.failed", poisoned.ArtifactID); got != 1 {
		t.Fatalf("audits for the note that is still there = %d, want 1", got)
	}
	if got := auditRowCount(t, database, "session.vault_artifact.cleanup.failed", removable.ArtifactID); got != 0 {
		t.Fatalf("audits for the note that was deleted = %d, want none", got)
	}

	// Once storage answers again and the row names its own note, the retry
	// drains the manifest and the withdrawal finishes.
	heal()
	if _, err := database.ExecContext(ctx, `
		UPDATE vault_artifacts SET vault_path = ? WHERE id = ?
	`, poisonedCandidate.InboxPath, poisoned.ArtifactID); err != nil {
		t.Fatalf("repair the manifest row: %v", err)
	}
	retry, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("retry DeleteSession: %v", err)
	}
	if retry.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("retry receipt = %+v, want completed", retry.GetDeletion())
	}
	remaining, err := repo.SessionVaultArtifacts(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionVaultArtifacts after the retry: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("vault rows after the retry = %+v, want the manifest drained", remaining)
	}
	if got := auditRowCount(t, database, "session.vault_artifact.cleanup.failed", removable.ArtifactID); got != 0 {
		t.Fatalf("audits for the deleted note after the retry = %d, want none", got)
	}
	if got := auditRowCount(t, database, "session.vault_artifact.cleanup.failed", poisoned.ArtifactID); got != 1 {
		t.Fatalf("audits for the poisoned row after the retry = %d, want the one failure recorded once", got)
	}
	if _, statErr := os.Stat(beliefPath); statErr != nil {
		t.Fatalf("the belief did not survive the whole withdrawal: %v", statErr)
	}
}

// blockingForgetCleaner finishes its removal and then never returns from the
// manifest write, the way a storage layer that has stopped answering behaves.
// It waits on the context it was handed, so it returns exactly when — and only
// when — that context is bounded.
type blockingForgetCleaner struct {
	scope string
	// entered reports that the manifest write has begun blocking.
	entered chan struct{}
	// safety is how long the fake waits for a deadline that never comes. It
	// exists so an unbounded call fails this test instead of hanging the whole
	// package until the go test timeout.
	safety time.Duration
	// unbounded is the error a call that outlived the safety net reports.
	unbounded error
}

func (c *blockingForgetCleaner) ArtifactScope() string { return c.scope }

func (c *blockingForgetCleaner) CleanupSessionArtifacts(context.Context, string, int64) error {
	return nil
}

func (c *blockingForgetCleaner) ForgetCleanedArtifacts(ctx context.Context, _ string) error {
	select {
	case c.entered <- struct{}{}:
	default:
	}
	timer := time.NewTimer(c.safety)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return c.unbounded
	}
}

// The manifest write a finished cleaner still owes is detached from the
// caller's context on purpose — the files are already gone, and a client that
// hung up must not leave the rows naming them behind. Detached is not the same
// as unbounded: a storage layer that never answers would hold the withdrawal,
// the request, and the shutdown that is waiting on it open forever.
func TestDeleteSessionBoundsAManifestWriteThatNeverAnswers(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	server.artifactFinalizeTimeoutOverride = 150 * time.Millisecond
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "A manifest that never answers")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	seedSandboxArtifactRow(t, database, session.SessionID, "artifact_unbounded_finalize")
	unbounded := errors.New("the manifest write was never bounded")
	cleaner := &blockingForgetCleaner{
		scope:     ArtifactScopeSandbox,
		entered:   make(chan struct{}, 1),
		safety:    10 * time.Second,
		unbounded: unbounded,
	}
	server.RegisterArtifactCleaners(cleaner)

	started := time.Now()
	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	elapsed := time.Since(started)
	select {
	case <-cleaner.entered:
	default:
		t.Fatal("the manifest write never ran, so this proved nothing about its bound")
	}
	if elapsed >= cleaner.safety {
		t.Fatalf("DeleteSession took %v, want it bounded well under the %v safety net",
			elapsed, cleaner.safety)
	}
	// A bounded write that ran out of time is still outstanding work, and the
	// rows it could not drop are still the retry's worklist.
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL ||
		!response.GetDeletion().GetRetryable() {
		t.Fatalf("receipt = %+v, want retryable failed_external", response.GetDeletion())
	}
	if got := response.GetDeletion().GetErrorCode(); got != repository.SessionDeletionArtifactManifestFinalizeFailed {
		t.Fatalf("error code = %q, want %q",
			got, repository.SessionDeletionArtifactManifestFinalizeFailed)
	}
	if got := auditRowCount(t, database, "session.artifact.cleanup.failed", "artifact_unbounded_finalize"); got != 0 {
		t.Fatalf("per-file deletion-failure audits = %d, want none for a file that was deleted", got)
	}
	rows, err := repo.SessionSandboxArtifacts(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("SessionSandboxArtifacts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("sandbox rows = %+v, want the manifest preserved for the retry", rows)
	}
}

// The reconcile ticker and the shutdown behind it are the reason the bound has
// to exist. ResumePendingDeletions walks every unfinished receipt, and one
// storage layer that has stopped answering must cost that walk a deadline
// rather than the process's ability to stop.
func TestResumePendingDeletionsProgressesPastAManifestWriteThatNeverAnswers(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	server.artifactFinalizeTimeoutOverride = 150 * time.Millisecond
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "A withdrawal the ticker retries")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	seedSandboxArtifactRow(t, database, session.SessionID, "artifact_resume_finalize")
	cleaner := &blockingForgetCleaner{
		scope:     ArtifactScopeSandbox,
		entered:   make(chan struct{}, 1),
		safety:    10 * time.Second,
		unbounded: errors.New("the manifest write was never bounded"),
	}
	server.RegisterArtifactCleaners(cleaner)
	if _, err := repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}

	resumeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.ResumePendingDeletions(resumeCtx) }()
	// The shutdown path cancels this context and then waits for the loop. The
	// wait only ends if the detached manifest write is bounded.
	select {
	case <-cleaner.entered:
	case <-time.After(cleaner.safety):
		t.Fatal("the manifest write never ran")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ResumePendingDeletions: %v", err)
		}
	case <-time.After(cleaner.safety):
		t.Fatal("ResumePendingDeletions never returned, so a stalled manifest write blocks shutdown")
	}
	persisted, err := repo.SessionDeletionReceipt(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("SessionDeletionReceipt: %v", err)
	}
	if persisted.ErrorCode != repository.SessionDeletionArtifactManifestFinalizeFailed || !persisted.Retryable {
		t.Fatalf("persisted receipt = %+v, want a retryable manifest-finalize failure", persisted)
	}
}
