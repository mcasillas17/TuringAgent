package sessions

import (
	"context"
	"errors"
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
	c.mu.Unlock()
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
// user's files.
func TestDeleteSessionDispatchesCleanersOnlyForTheArtifactCleanupPendingGate(t *testing.T) {
	server, repo, _, _ := newVaultBackedServer(t)
	sessionID, _ := seedVaultCandidate(t, repo, "bees")
	vaultCleaner := &scopedFakeCleaner{scope: ArtifactScopeVault}
	server.RegisterArtifactCleaners(vaultCleaner)
	ctx := context.Background()

	// A withdrawal held open by an unreconciled execution fails for a different
	// reason and must leave the vault alone.
	if _, err := repo.BeginSessionDeletion(ctx, sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	if err := repo.MarkSessionDeletionSandboxFailure(ctx, sessionID, "execution_unreconciled"); err != nil {
		t.Fatalf("MarkSessionDeletionSandboxFailure: %v", err)
	}
	if _, err := repo.SessionDeletionReceipt(ctx, sessionID); err != nil {
		t.Fatalf("SessionDeletionReceipt: %v", err)
	}
	if vaultCleaner.attempts() != 0 {
		t.Fatalf("cleaner attempts before any dispatch = %d, want 0", vaultCleaner.attempts())
	}

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetErrorCode() != "" &&
		response.GetDeletion().GetErrorCode() != "artifact_cleanup_pending" &&
		vaultCleaner.attempts() != 0 {
		t.Fatalf("cleaners ran for receipt %+v", response.GetDeletion())
	}
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

			sandboxCleaner := &scopedFakeCleaner{scope: ArtifactScopeSandbox}
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
				if got := auditRowCount(t, database, "session.vault_artifact.cleanup.failed", ""); got != 0 {
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
		sandboxCleaner := &scopedFakeCleaner{scope: ArtifactScopeSandbox}
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
	note, err := repo.PromoteMemoryCandidate(ctx, candidate.CandidateID)
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
