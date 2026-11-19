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
// listed, a reconciler pass that changes nothing hints nothing, and the
// completion hints its row away.
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
	cleaner.fail(nil)
	if err := server.ResumePendingDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	if got := attentionHints(updates); got < 1 {
		t.Fatal("the completed deletion did not hint its row away")
	}
}
