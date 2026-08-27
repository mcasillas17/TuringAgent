package memoryfiles

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// A hashless rejection is the way out for a proposal nobody can read: the user
// is saying no to whatever is sitting there, so there is no hash to hold it to
// and nothing after the detach re-reads the file.
//
// That is exactly why the cancellation check cannot live inside the re-read. A
// decided rejection that is cancelled between the detach and the verify fails
// its re-read and is put back; a hashless one had no re-read to fail, so it
// used to walk straight past a cancelled context into the unlink. The request
// that asked for the deletion had already ended, and the file it deleted was a
// claim about the user their inbox will never show them again.
//
// So the check is the primitive's own, taken the moment the file is between
// names and before anything decides whether to keep it, and the file goes back
// under its own name before the cancellation is reported.
func TestHashlessRejectionCancelledMidDetachRestoresTheFileAndSaysSo(t *testing.T) {
	const unreadable = unparseableCandidate
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	vault := vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeVerify {
			cancel()
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", unreadable)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	err := vault.RemoveInboxNote(ctx, RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancellation to be reported as one, got %v", err)
	}
	if errors.Is(err, ErrStaleContent) {
		t.Fatalf("a cancelled request claimed the file had changed: %v", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("a cancelled hashless rejection deleted the proposal: %v", readErr)
	}
	if string(survived) != unreadable {
		t.Fatalf("the restored file holds %q, want the proposal %q", survived, unreadable)
	}
	requireNoStagingResidue(t, vault)
}

// The other way a request ends without anybody deciding anything. A deadline
// that runs out mid-detach is not the user changing their mind and not another
// writer taking the name; the answer is the same either way, because what makes
// the deletion unsafe is that nothing is left to receive its outcome.
func TestHashlessRejectionPastItsDeadlineMidDetachRestoresTheFile(t *testing.T) {
	const unreadable = unparseableCandidate
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	vault := vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeVerify {
			// Waiting on the deadline rather than sleeping past it: the barrier
			// leaves when the context says so, so the test cannot be flaky in
			// either direction.
			<-ctx.Done()
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", unreadable)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	err := vault.RemoveInboxNote(ctx, RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the expired deadline to be reported as one, got %v", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("a timed-out hashless rejection deleted the proposal: %v", readErr)
	}
	if string(survived) != unreadable {
		t.Fatalf("the restored file holds %q, want the proposal %q", survived, unreadable)
	}
	requireNoStagingResidue(t, vault)
}

// Turing's own tidying is not a decision about text and keeps the plain
// unlink — but it is still a request, and a request that ended deletes nothing.
// The guard belongs to the whole primitive, not to the doors with a hash.
func TestDecidedRejectionPastItsDeadlineMidDetachRestoresTheFile(t *testing.T) {
	const decided = "the proposal the user read"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	vault := vaultWithDetachBarrier(t, func(phase detachPhase, _ string) {
		if phase == detachPhaseBeforeVerify {
			<-ctx.Done()
		}
	})
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(ctx, RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the expired deadline to be reported as one, got %v", err)
	}
	if errors.Is(err, ErrStaleContent) {
		t.Fatalf("a timed-out request claimed the file had changed: %v", err)
	}
	survived, readErr := os.ReadFile(full)
	if readErr != nil {
		t.Fatalf("a timed-out rejection deleted the proposal: %v", readErr)
	}
	if string(survived) != decided {
		t.Fatalf("the restored file holds %q, want the proposal %q", survived, decided)
	}
	requireNoStagingResidue(t, vault)
}
