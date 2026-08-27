package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"google.golang.org/grpc/codes"
)

// Two refusals the vault now makes that the caller has to be able to tell apart
// from "the file changed since it was read".
//
// A rejection of a proposal nothing can open is not a claim about the file
// having moved: nothing moved, and nothing can prove what an unlink would take
// with it. Answering Aborted with "re-read it and decide again" sends the user
// round a loop that cannot end — they re-read, it is still unopenable, they
// press reject, and the same sentence comes back. What they need is the one
// thing that gets them out of it, which is making the file readable.
//
// A promotion whose request ended while the original was going back under its
// own name is not a claim about the file either. The caller has gone; telling
// whoever asks next that the user's vault moved is an invention, and a retry
// loop that matches on staleness would go round again on the strength of it.

// wantUnprovableMessage is spelled out here rather than read from the package
// under test, for the same reason the source-changed sentence is: it is what a
// person and a model both see, and a test that quotes the constant it checks
// would follow any rewrite of it — including one that starts naming the file.
const wantUnprovableMessage = "Turing could not open this proposal to check it is still the same file, so it left it alone; make the file readable in your vault and reject it again"

func TestMemoryErrorMapsAnUnprovableRejectionToAnActionableRefusal(t *testing.T) {
	// Shaped like the real thing: the vault's own sentence names the path and
	// carries both sentinels, because to every existing caller this is still
	// one more refusal that left the proposal where it was.
	refused := &memoryfiles.StaleContentError{
		RelPath: "inbox/01ARZ3NDEKTSV4RRFFQ69G5FAV-dark-mode.md",
		Detail:  "nothing here can open it to prove it is still the entry that was read, so it was left alone",
		Cause:   memoryfiles.ErrUnprovableEntry,
	}
	err := memoryError(refused, "reject memory candidate failed")
	message := requireContentFreeRefusal(t, err, codes.FailedPrecondition, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if message != wantUnprovableMessage {
		t.Fatalf("the refusal says %q, want the fixed sentence %q", message, wantUnprovableMessage)
	}
}

// The ordering that makes it worth having. The unprovable refusal carries
// ErrStaleContent too, so a case placed after the stale one would never be
// reached and the user would get "the file changed since it was read" for a
// file that did not change.
func TestAnUnprovableRefusalOutranksTheStaleContentItAlsoCarries(t *testing.T) {
	refused := &memoryfiles.StaleContentError{
		RelPath: "inbox/note.md",
		Cause:   memoryfiles.ErrUnprovableEntry,
	}
	if message := requireContentFreeRefusal(t, memoryError(refused, "reject memory candidate failed"), codes.FailedPrecondition); message != wantUnprovableMessage {
		t.Fatalf("the refusal says %q, want the unprovable sentence", message)
	}
}

// The promotion half, in the shape the vault hands over: the cancellation
// carries where the bytes were left, and the caller sees the cancellation.
func TestMemoryErrorReportsAPromotionEndedDuringItsRestoreAsCancelled(t *testing.T) {
	ended := &memoryfiles.EndedRequestError{
		RelPath: "inbox/01ARZ3NDEKTSV4RRFFQ69G5FAV-dark-mode.md",
		Detail:  "it was rewritten in place and could not be put back under its own name (file exists); it is not lost — it was kept for recovery at inbox/recovered-inbox-draft-01ARZ3NDEKTSV4RRFFQ69G5FAW.md",
		Cause:   context.Canceled,
	}
	wrapped := fmt.Errorf("%w (%v)", ended, "the promotion was abandoned")
	requireContentFreeRefusal(t, memoryError(wrapped, "promote memory candidate failed"), codes.Canceled, "01ARZ3NDEKTSV4RRFFQ69G5FAV")

	expired := &memoryfiles.EndedRequestError{RelPath: "inbox/note.md", Cause: context.DeadlineExceeded}
	requireContentFreeRefusal(t, memoryError(expired, "promote memory candidate failed"), codes.DeadlineExceeded)
}
