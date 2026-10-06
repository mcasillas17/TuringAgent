package sessions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	eventsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// seedDelegationChild is a hidden child of parentID with one queued run, as
// delegation leaves it.
func seedDelegationChild(t *testing.T, repo *repository.Repository, database *db.DB, parentID string) (string, string) {
	t.Helper()
	ctx := context.Background()
	child, err := repo.CreateSession(ctx, "🔬 Research")
	if err != nil {
		t.Fatal(err)
	}
	queued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: child.SessionID, Content: "the brief", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	markChildOf(t, database, child.SessionID, parentID)
	return child.SessionID, queued.RunID
}

func markChildOf(t *testing.T, database *db.DB, childID, parentID string) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(), `
		UPDATE sessions SET kind = 'delegation', parent_session_id = ? WHERE id = ?`, parentID, childID); err != nil {
		t.Fatal(err)
	}
}

func sessionRowExists(t *testing.T, database *db.DB, sessionID string) bool {
	t.Helper()
	var count int
	if err := database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

func receiptState(t *testing.T, database *db.DB, sessionID string) (state, errorCode, parent string) {
	t.Helper()
	if err := database.QueryRowContext(context.Background(), `
		SELECT state, COALESCE(error_code, ''), COALESCE(parent_session_id, '') FROM session_deletions WHERE session_id = ?`,
		sessionID).Scan(&state, &errorCode, &parent); err != nil {
		t.Fatalf("receipt for %s: %v", sessionID, err)
	}
	return state, errorCode, parent
}

// publicReceiptIDs is what the client's sidebar is given.
func publicReceiptIDs(t *testing.T, server *Server) []string {
	t.Helper()
	listed, err := server.ListSessionDeletionReceipts(context.Background(), &turingv1.ListSessionDeletionReceiptsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, receipt := range listed.GetDeletions() {
		ids = append(ids, receipt.GetSessionId())
	}
	return ids
}

// sessionCleaner records the session every cleanup was asked for and can be
// made to fail.
type sessionCleaner struct {
	SessionArtifactCleaner
	mu       sync.Mutex
	sessions []string
	err      error
}

func (c *sessionCleaner) CleanupSessionArtifacts(ctx context.Context, sessionID string, version int64) error {
	c.mu.Lock()
	c.sessions = append(c.sessions, sessionID)
	failure := c.err
	c.mu.Unlock()
	if failure != nil {
		return failure
	}
	return c.SessionArtifactCleaner.CleanupSessionArtifacts(ctx, sessionID, version)
}

func (c *sessionCleaner) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
}

func (c *sessionCleaner) cleanedSessions() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sessions...)
}

// Deleting a parent withdraws a queued and a running child, each under its own
// receipt and through its own runtime cancellation, and deletes the parent's
// row only once both are gone. The running child's stream ends with its
// session.deleted.
func TestDeletingAParentWithdrawsEachChildFirst(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	bus := eventsvc.NewBus(4)
	capabilities := &sessionCapabilitySource{}
	server := New(repo, config.Config{}, capabilities, bus)
	ctx := context.Background()
	parent, err := repo.CreateSession(ctx, "Turing")
	if err != nil {
		t.Fatal(err)
	}
	queuedChild, queuedRun := seedDelegationChild(t, repo, database, parent.SessionID)
	runningChild, runningRun := seedDelegationChild(t, repo, database, parent.SessionID)
	if _, err := database.ExecContext(ctx, `UPDATE jobs SET status = 'completed' WHERE run_id = ?`, queuedRun); err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker", 0, time.Hour, nil, nil); err != nil || claimed.RunID != runningRun {
		t.Fatalf("claim = %+v, %v; want the running child", claimed, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE jobs SET status = 'pending' WHERE run_id = ?`, queuedRun); err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := bus.Subscribe(runningChild)
	defer unsubscribe()

	first, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: parent.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if first.GetDeletion().GetErrorCode() != repository.SessionDeletionChildPending || !first.GetDeletion().GetRetryable() {
		t.Fatalf("parent receipt = %+v, want child_deletion_pending while a child is still running", first.GetDeletion())
	}
	if !sessionRowExists(t, database, parent.SessionID) {
		t.Fatal("the parent row was deleted while a child was still running")
	}
	if state, _, parentID := receiptState(t, database, queuedChild); state != "completed" || parentID != parent.SessionID {
		t.Fatalf("queued child receipt = %s naming %q, want completed naming the parent", state, parentID)
	}
	if state, _, parentID := receiptState(t, database, runningChild); state != "quiescing" || parentID != parent.SessionID {
		t.Fatalf("running child receipt = %s naming %q, want quiescing naming the parent", state, parentID)
	}
	var runningStatus string
	if err := database.QueryRowContext(ctx, `SELECT status FROM agent_runs WHERE id = ?`, runningRun).Scan(&runningStatus); err != nil {
		t.Fatal(err)
	}
	if runningStatus != "cancelled" {
		t.Fatalf("running child run = %q, want cancelled", runningStatus)
	}
	if sessionRowExists(t, database, queuedChild) {
		t.Fatal("the queued child's session survived its own completed withdrawal")
	}
	capabilities.mu.Lock()
	runtimeCancels := append([]string(nil), capabilities.cancelledSessions...)
	capabilities.mu.Unlock()
	for _, sessionID := range []string{parent.SessionID, queuedChild, runningChild} {
		if !slices.Contains(runtimeCancels, sessionID) {
			t.Fatalf("runtime cancellations = %v, want one for %s", runtimeCancels, sessionID)
		}
	}
	if got := publicReceiptIDs(t, server); !slices.Equal(got, []string{parent.SessionID}) {
		t.Fatalf("public receipts = %v, want only the parent's", got)
	}

	if err := repo.AcknowledgeExecutionExit(ctx, runningRun); err != nil {
		t.Fatal(err)
	}
	second, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: parent.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if second.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("parent receipt = %+v, want completed once its children are gone", second.GetDeletion())
	}
	for _, sessionID := range []string{parent.SessionID, queuedChild, runningChild} {
		if sessionRowExists(t, database, sessionID) {
			t.Fatalf("session %s survived its parent's deletion", sessionID)
		}
	}
	select {
	case event, ok := <-events:
		if !ok || event.Type != "session.deleted" {
			t.Fatalf("child stream = %+v, %v; want its session.deleted", event, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("the running child's stream never received session.deleted")
	}
	if _, ok := <-events; ok {
		t.Fatal("the child's stream stayed open after its session.deleted")
	}
}

// A child's sandbox and vault files are removed under the child's own receipt.
// While the child's cleanup fails, the parent waits at child_deletion_pending,
// keeps its row, runs no cleaner for its own ID, and is the only receipt the
// client sees; a retry or a restart then finishes both.
func TestAChildsArtifactsAreRemovedUnderItsOwnReceipt(t *testing.T) {
	for _, recovery := range []string{"retry", "restart"} {
		t.Run(recovery, func(t *testing.T) {
			server, repo, vault, database := newVaultBackedServer(t)
			ctx := context.Background()
			parent, err := repo.CreateSession(ctx, "Turing")
			if err != nil {
				t.Fatal(err)
			}
			child, candidate := seedVaultCandidate(t, repo, "bees")
			seedSandboxArtifactRow(t, database, child, "artifact_child_"+recovery)
			markChildOf(t, database, child, parent.SessionID)
			sandbox := &sessionCleaner{SessionArtifactCleaner: &scopedFakeCleaner{scope: ArtifactScopeSandbox, manifest: repo}}
			vaultCleaner := &sessionCleaner{SessionArtifactCleaner: NewVaultArtifactCleaner(repo)}
			sandbox.fail(errors.New("sandbox unavailable"))
			server.RegisterArtifactCleaners(sandbox, vaultCleaner)

			first, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: parent.SessionID})
			if err != nil {
				t.Fatal(err)
			}
			if first.GetDeletion().GetErrorCode() != repository.SessionDeletionChildPending {
				t.Fatalf("parent receipt = %+v, want child_deletion_pending", first.GetDeletion())
			}
			if !sessionRowExists(t, database, parent.SessionID) || !sessionRowExists(t, database, child) {
				t.Fatal("a session row was deleted while the child's files were still there")
			}
			if _, code, _ := receiptState(t, database, child); code != repository.SessionDeletionSandboxCleanupFailed {
				t.Fatalf("child receipt code = %q, want its own cleanup failure", code)
			}
			if got := publicReceiptIDs(t, server); !slices.Equal(got, []string{parent.SessionID}) {
				t.Fatalf("public receipts = %v, want only the parent's", got)
			}

			sandbox.fail(nil)
			if recovery == "retry" {
				if _, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: parent.SessionID}); err != nil {
					t.Fatal(err)
				}
			} else {
				restarted := New(repo, config.Config{}, &sessionCapabilitySource{})
				restarted.RegisterArtifactCleaners(sandbox, vaultCleaner)
				if err := restarted.ResumePendingDeletions(ctx); err != nil {
					t.Fatal(err)
				}
			}
			for _, sessionID := range []string{parent.SessionID, child} {
				if state, _, _ := receiptState(t, database, sessionID); state != "completed" {
					t.Fatalf("receipt for %s = %s, want completed", sessionID, state)
				}
			}
			if _, err := os.Stat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); !os.IsNotExist(err) {
				t.Fatalf("the child's vault file survived: %v", err)
			}
			if rows, err := repo.SessionSandboxArtifacts(ctx, child); err != nil || len(rows) != 0 {
				t.Fatalf("child sandbox rows = %+v, %v; want none", rows, err)
			}
			for _, cleaner := range []*sessionCleaner{sandbox, vaultCleaner} {
				if slices.Contains(cleaner.cleanedSessions(), parent.SessionID) {
					t.Fatalf("a cleaner ran for the parent's ID: %v", cleaner.cleanedSessions())
				}
				if !slices.Contains(cleaner.cleanedSessions(), child) {
					t.Fatalf("a cleaner never ran for the child: %v", cleaner.cleanedSessions())
				}
			}
		})
	}
}

// A crash after the parent's begin and before its children's leaves a child
// with no receipt. The reconciler starts from the parent and withdraws it.
func TestResumeWithdrawsChildrenACrashLeftUnbegun(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	ctx := context.Background()
	parent, err := repo.CreateSession(ctx, "Turing")
	if err != nil {
		t.Fatal(err)
	}
	child, _ := seedDelegationChild(t, repo, database, parent.SessionID)
	if _, err := repo.BeginSessionDeletion(ctx, parent.SessionID); err != nil {
		t.Fatal(err)
	}

	if err := server.ResumePendingDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	if state, _, parentID := receiptState(t, database, child); state != "completed" || parentID != parent.SessionID {
		t.Fatalf("child receipt = %s naming %q, want completed naming the parent", state, parentID)
	}
	if state, _, _ := receiptState(t, database, parent.SessionID); state != "completed" {
		t.Fatalf("parent receipt = %s, want completed", state)
	}
}

// A child whose completion failed after its row was deleted is found again
// through its receipt: the parent waits, the client still sees only the
// parent, and after a restart the reconciler finishes the child and then the
// parent.
func TestAChildWhoseCompletionFailedIsFinishedByItsParent(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	var mu sync.Mutex
	completionFails := true
	completion := func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if completionFails {
			return errors.New("vault unavailable")
		}
		return nil
	}
	server.SetMemoryReconcileCompletion(completion)
	ctx := context.Background()
	parent, err := repo.CreateSession(ctx, "Turing")
	if err != nil {
		t.Fatal(err)
	}
	child, _ := seedDelegationChild(t, repo, database, parent.SessionID)

	first, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: parent.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if first.GetDeletion().GetErrorCode() != repository.SessionDeletionChildPending {
		t.Fatalf("parent receipt = %+v, want child_deletion_pending", first.GetDeletion())
	}
	if sessionRowExists(t, database, child) || !sessionRowExists(t, database, parent.SessionID) {
		t.Fatal("want the child's row gone and the parent's kept")
	}
	if state, code, parentID := receiptState(t, database, child); state != "failed_external" ||
		code != repository.SessionDeletionMemoryReconcileFailed || parentID != parent.SessionID {
		t.Fatalf("child receipt = %s/%s naming %q", state, code, parentID)
	}
	if got := publicReceiptIDs(t, server); !slices.Equal(got, []string{parent.SessionID}) {
		t.Fatalf("public receipts = %v, want only the parent's", got)
	}
	if _, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: child}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("public DeleteSession on the child after its row is gone = %v, want FailedPrecondition", err)
	}

	mu.Lock()
	completionFails = false
	mu.Unlock()
	restarted := New(repo, config.Config{}, &sessionCapabilitySource{})
	restarted.SetMemoryReconcileCompletion(completion)
	if err := restarted.ResumePendingDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	for _, sessionID := range []string{child, parent.SessionID} {
		if state, _, _ := receiptState(t, database, sessionID); state != "completed" {
			t.Fatalf("receipt for %s = %s, want completed", sessionID, state)
		}
	}
	if sessionRowExists(t, database, parent.SessionID) {
		t.Fatal("the parent row survived")
	}
}

// A child receipt always has an unfinished parent. If one ever does not, the
// reconciler finishes the child on its own and leaves the parent alone.
func TestResumeFinishesAnOrphanChildReceiptDirectly(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	ctx := context.Background()
	parent, err := repo.CreateSession(ctx, "Turing")
	if err != nil {
		t.Fatal(err)
	}
	child, _ := seedDelegationChild(t, repo, database, parent.SessionID)
	if _, err := repo.BeginDelegationSessionDeletion(ctx, child); err != nil {
		t.Fatal(err)
	}

	if err := server.ResumePendingDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := receiptState(t, database, child); state != "completed" {
		t.Fatalf("orphan child receipt = %s, want completed", state)
	}
	if !sessionRowExists(t, database, parent.SessionID) {
		t.Fatal("finishing an orphan child deleted its parent")
	}
}

// A child whose withdrawal fails outright does not stop its siblings or the
// parent's own step: the next child is still withdrawn, and the parent records
// child_deletion_pending and keeps its row, through DeleteSession and through
// the reconciler, until the failing child can be withdrawn.
func TestAChildThatCannotBeWithdrawnDoesNotStopItsSiblings(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	ctx := context.Background()
	parent, err := repo.CreateSession(ctx, "Turing")
	if err != nil {
		t.Fatal(err)
	}
	failing, _ := seedDelegationChild(t, repo, database, parent.SessionID)
	sibling, _ := seedDelegationChild(t, repo, database, parent.SessionID)
	if failing > sibling {
		failing, sibling = sibling, failing
	}
	if _, err := database.ExecContext(ctx, `
		CREATE TRIGGER refuse_child_receipt BEFORE INSERT ON session_deletions
		WHEN NEW.session_id = '`+failing+`'
		BEGIN SELECT RAISE(ABORT, 'receipt store unavailable'); END`); err != nil {
		t.Fatal(err)
	}

	assertHeld := func(stage string) {
		t.Helper()
		if state, code, _ := receiptState(t, database, parent.SessionID); state != "failed_external" || code != repository.SessionDeletionChildPending {
			t.Fatalf("%s: parent receipt = %s/%s, want child_deletion_pending", stage, state, code)
		}
		if !sessionRowExists(t, database, parent.SessionID) || !sessionRowExists(t, database, failing) {
			t.Fatalf("%s: want the parent and the failing child kept", stage)
		}
	}
	if _, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: parent.SessionID}); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := receiptState(t, database, sibling); state != "completed" {
		t.Fatalf("sibling receipt = %s, want completed after the first child failed", state)
	}
	assertHeld("DeleteSession")
	if err := server.ResumePendingDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	assertHeld("ResumePendingDeletions")

	if _, err := database.ExecContext(ctx, `DROP TRIGGER refuse_child_receipt`); err != nil {
		t.Fatal(err)
	}
	if err := server.ResumePendingDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	for _, sessionID := range []string{failing, parent.SessionID} {
		if state, _, _ := receiptState(t, database, sessionID); state != "completed" {
			t.Fatalf("receipt for %s = %s, want completed", sessionID, state)
		}
	}
}

// A parent with files of its own waits for its child first and then runs its
// own cleaners for its own ID: the child's under the child's receipt, the
// parent's under the parent's, and the parent completes.
func TestAParentCleansItsOwnArtifactsAfterItsChild(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	cleaner := &sessionCleaner{SessionArtifactCleaner: &scopedFakeCleaner{scope: ArtifactScopeSandbox, manifest: repo}}
	server.RegisterArtifactCleaners(cleaner)
	ctx := context.Background()
	parent, err := repo.CreateSession(ctx, "Turing")
	if err != nil {
		t.Fatal(err)
	}
	seedSandboxArtifactRow(t, database, parent.SessionID, "artifact_parent")
	child, err := repo.CreateSession(ctx, "🔬 Research")
	if err != nil {
		t.Fatal(err)
	}
	seedSandboxArtifactRow(t, database, child.SessionID, "artifact_child")
	markChildOf(t, database, child.SessionID, parent.SessionID)
	cleaner.fail(errors.New("sandbox unavailable"))

	first, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: parent.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if first.GetDeletion().GetErrorCode() != repository.SessionDeletionChildPending {
		t.Fatalf("parent receipt = %+v, want child_deletion_pending ahead of its own files", first.GetDeletion())
	}
	if got := cleaner.cleanedSessions(); !slices.Equal(got, []string{child.SessionID}) {
		t.Fatalf("cleaned sessions = %v, want only the child while it is unfinished", got)
	}

	cleaner.fail(nil)
	second, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: parent.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if second.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("parent receipt = %+v, want completed", second.GetDeletion())
	}
	if got := cleaner.cleanedSessions(); !slices.Equal(got, []string{child.SessionID, child.SessionID, parent.SessionID}) {
		t.Fatalf("cleaned sessions = %v, want the child's retry and then the parent's own cleanup", got)
	}
	for _, sessionID := range []string{parent.SessionID, child.SessionID} {
		if rows, err := repo.SessionSandboxArtifacts(ctx, sessionID); err != nil || len(rows) != 0 {
			t.Fatalf("sandbox rows for %s = %+v, %v; want none", sessionID, rows, err)
		}
	}
}
