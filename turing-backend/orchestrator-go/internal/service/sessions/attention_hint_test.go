package sessions

import (
	"context"
	"errors"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	eventsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
)

// attentionHints counts the attention.changed hints that reach a global
// updates subscriber within a short wait.
func attentionHints(updates <-chan eventsvc.Event) int {
	hints := 0
	for {
		select {
		case event := <-updates:
			if event.Type == eventsvc.AttentionChangedType {
				hints++
			}
		case <-time.After(100 * time.Millisecond):
			return hints
		}
	}
}

// UX-002a: an owner's deletion is a Needs-you source. Starting one hints (the
// conversation's rows leave the queue), a receipt that fails retryably is
// listed, a reconciler pass that changes nothing hints nothing, one that
// changes the listed cause hints, and the completion hints its row away.
func TestDeletionHintsAttentionWhenItsRowChanges(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	bus := eventsvc.NewBus(8)
	server := New(repo, config.Config{}, &sessionCapabilitySource{}, bus)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Hinted deletion")
	if err != nil {
		t.Fatal(err)
	}
	seedSandboxArtifactRow(t, database, session.SessionID, "artifact_hint")
	cleaner := &scopedFakeCleaner{scope: ArtifactScopeSandbox, manifest: repo}
	cleaner.fail(errors.New("cleanup transport unavailable"))
	server.RegisterArtifactCleaners(cleaner)
	updates, unsubscribe := bus.SubscribeSessionUpdates()
	defer unsubscribe()

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL {
		t.Fatalf("receipt = %+v, want failed_external", response.GetDeletion())
	}
	if got := attentionHints(updates); got != 1 {
		t.Fatalf("hints after a deletion that failed = %d, want 1", got)
	}
	if err := server.ResumePendingDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	if got := attentionHints(updates); got != 0 {
		t.Fatalf("hints after a pass that changed nothing = %d, want 0", got)
	}
	// A pass that changes the listed cause changes the row.
	if _, err := database.ExecContext(ctx, `UPDATE session_deletions SET error_code = 'execution_unreconciled' WHERE session_id = ?`,
		session.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := server.ResumePendingDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	if got := attentionHints(updates); got != 1 {
		t.Fatalf("hints after a pass that changed the cause = %d, want 1", got)
	}
	cleaner.fail(nil)
	if err := server.ResumePendingDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	if got := attentionHints(updates); got < 1 {
		t.Fatal("the completed deletion did not hint its row away")
	}
}

// Whether a withdrawal pass changed what the Needs-you queue lists: a begun
// withdrawal always did, even when its finish failed (the conversation's
// rows left the queue); otherwise only a receipt that started or stopped
// being one the owner can retry, or whose listed cause changed, did.
func TestDeletionChangedAttention(t *testing.T) {
	retryable := func(code string) repository.SessionDeletionReceipt {
		return repository.SessionDeletionReceipt{State: "failed_external", Retryable: true, ErrorCode: code}
	}
	quiescing := repository.SessionDeletionReceipt{State: "quiescing", Retryable: true}
	completed := repository.SessionDeletionReceipt{State: "completed"}
	for _, test := range []struct {
		name     string
		before   repository.SessionDeletionReceipt
		began    bool
		after    repository.SessionDeletionReceipt
		finished bool
		want     bool
	}{
		{name: "a begun withdrawal", began: true, after: quiescing, finished: true, want: true},
		{name: "a begun withdrawal whose finish failed", began: true, want: true},
		{name: "a retry whose finish failed", before: retryable("artifact_cleanup_pending"), want: false},
		{name: "a receipt that became retryable", before: quiescing, after: retryable("execution_unreconciled"), finished: true, want: true},
		{name: "a retryable receipt that completed", before: retryable("artifact_cleanup_pending"), after: completed, finished: true, want: true},
		{name: "a retryable receipt whose cause changed", before: retryable("artifact_cleanup_pending"), after: retryable("vault_artifact_cleanup_failed"), finished: true, want: true},
		{name: "a retryable receipt unchanged", before: retryable("artifact_cleanup_pending"), after: retryable("artifact_cleanup_pending"), finished: true},
		{name: "a quiescing receipt still quiescing", before: quiescing, after: quiescing, finished: true},
	} {
		if got := deletionChangedAttention(test.before, test.began, test.after, test.finished); got != test.want {
			t.Fatalf("%s: deletionChangedAttention = %v, want %v", test.name, got, test.want)
		}
	}
}
