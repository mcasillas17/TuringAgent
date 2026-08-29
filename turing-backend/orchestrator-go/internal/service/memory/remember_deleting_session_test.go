package memory

import (
	"path/filepath"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The dispatch gate re-reads liveness immediately before any tool runs, but a
// session can still flip to deleting between that read and the reservation —
// ReserveVaultArtifact holds the line inside its own transaction and refuses
// with ErrVaultArtifactSessionUnavailable. That refusal is a precondition the
// model can act on, not a fault: without its own arm in memoryError it falls
// through to the Internal fallback, which tells the user Turing broke when
// what happened is that their conversation is going away — and, because
// memory.remember is not read-only, an opaque Internal is also classified as
// side-effect-unknown instead of a clean refusal. This calls the handler
// directly, past the dispatch gate, exactly where the race leaves a real call.
func TestRememberOnASessionMidDeletionIsARefusalNotAnInternal(t *testing.T) {
	service, repo, _, database, ctx := newMemoryServiceStack(t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), nil)
	runID, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")

	run, err := service.authorizeRun(ctx, runID)
	if err != nil {
		t.Fatalf("authorizeRun before the flip: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE sessions SET deletion_state='deleting' WHERE id=?`, sessionID); err != nil {
		t.Fatal(err)
	}

	response, err := service.callRemember(ctx, run, map[string]any{
		"title": "Coffee", "body": "They drink it black.",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("remember on a session mid-deletion = %v, want FailedPrecondition", err)
	}
	if response != nil {
		t.Fatalf("remember on a session mid-deletion returned %v, want nothing at all", response)
	}
}

// The mapping itself, pinned at the memoryError level like the other named
// refusals: the sentinel is standalone (it wraps neither ErrSessionDeleting
// nor ErrSessionNotFound), so no other arm can catch it by accident, and an
// implementation without the arm answers Internal here.
func TestVaultArtifactSessionUnavailableMapsToAPrecondition(t *testing.T) {
	err := memoryError(repository.ErrVaultArtifactSessionUnavailable, "file memory proposal failed")
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("memoryError(ErrVaultArtifactSessionUnavailable) = %v, want FailedPrecondition", err)
	}
}
