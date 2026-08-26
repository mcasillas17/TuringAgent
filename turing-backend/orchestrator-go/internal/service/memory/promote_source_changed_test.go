package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A promotion that cannot finish leaves the user's proposal exactly where it
// was and says so. Underneath, the vault says it with ErrSourceChanged and a
// sentence that names the file, the reserved recovery name it may have kept a
// copy under, and whatever the filesystem said.
//
// None of that may reach a caller. Until this round none of it was mapped at
// all, so the whole sentence fell through to the Internal fallback: the user
// was told Turing had broken when what happened is that their vault moved under
// a decision, and a model mid-run was handed an error it could only retry
// blindly. The status is Aborted — the same class the file-level
// compare-and-set already answers with — and its words are fixed.

// wantSourceChangedMessage is spelled out here rather than read from the
// package under test. It is the sentence a user and a model both see, and a
// test that quotes the constant it is checking would follow any rewrite of it
// — including one that starts naming the file.
const wantSourceChangedMessage = "this proposal changed while it was being promoted; read it again and decide on what it says now"

// requireContentFreeRefusal holds a status to the two things that make it safe
// to hand out: it is the code the caller can act on, and it says nothing about
// the vault. Paths, reserved names and errno text are all detail this server
// keeps to its own logs.
func requireContentFreeRefusal(t *testing.T, err error, want codes.Code, forbidden ...string) string {
	t.Helper()
	if status.Code(err) != want {
		t.Fatalf("error = %v (code %v), want %v", err, status.Code(err), want)
	}
	message := status.Convert(err).Message()
	for _, secret := range forbidden {
		if secret != "" && strings.Contains(message, secret) {
			t.Fatalf("the refusal leaked %q: %q", secret, message)
		}
	}
	for _, name := range []string{
		memoryfiles.InboxDirName + "/",
		memoryfiles.BeliefsDirName + "/",
		".turing-memory",
		"recovered-inbox-draft-",
	} {
		if strings.Contains(message, name) {
			t.Fatalf("the refusal named %q from the vault: %q", name, message)
		}
	}
	return message
}

// The whole stack: a real service over a real repository over a real vault. The
// promotion reads the candidate, installs the copy under beliefs/, and then
// cannot take the original off its name — the inbox stopped accepting writes
// underneath it, which is what a read-only sync mount, a full disk or a
// permission change during an Obsidian sync looks like from in here.
//
// The vault abandons the move: the copy it installed is rolled back and the
// user's proposal is left alone. What the caller gets is Aborted and a sentence
// that could be about any proposal in any vault.
func TestPromotionAbandonedBecauseTheSourceMovedIsReportedAsAborted(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where no directory refuses a write")
	}
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	const body = "The user prefers dark mode."
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", body)

	inbox := filepath.Join(vault.Root(), memoryfiles.InboxDirName)
	if err := os.Chmod(inbox, 0o500); err != nil {
		t.Fatalf("close the inbox to writers: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(inbox, 0o700) })

	_, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId:           candidate.CandidateID,
		ExpectedCandidateHash: candidate.ContentHash,
	})
	message := requireContentFreeRefusal(t, err, codes.Aborted, body, "Dark mode", vault.Root())
	if message != wantSourceChangedMessage {
		t.Fatalf("the refusal says %q, want the fixed sentence %q", message, wantSourceChangedMessage)
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); statErr != nil {
		t.Fatalf("the abandoned proposal left the inbox: %v", statErr)
	}
	if _, rowErr := repo.MemoryCandidateByID(ctx, candidate.CandidateID); rowErr != nil {
		t.Fatalf("the abandoned proposal lost its row: %v", rowErr)
	}
}

// The mapping itself, held to the sentinel rather than to one path that reaches
// it. Delete the case and this fails: an unmapped ErrSourceChanged is an
// Internal, and Internal is what this server says when it does not know what
// happened.
func TestMemoryErrorMapsASourceChangedPromotionToAborted(t *testing.T) {
	// Shaped like the real thing: the vault's sentence carries the path, the
	// reserved recovery name and the errno underneath.
	wrapped := fmt.Errorf(
		"%q changed while it was being promoted (another file had taken its name); it is not lost — it was kept for recovery at %s: %w",
		"inbox/01ARZ3NDEKTSV4RRFFQ69G5FAV-dark-mode.md",
		"inbox/.turing-memory-0123456789abcdef",
		memoryfiles.ErrSourceChanged,
	)
	err := memoryError(wrapped, "promote memory candidate failed")
	message := requireContentFreeRefusal(t, err, codes.Aborted, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if message != wantSourceChangedMessage {
		t.Fatalf("the refusal says %q, want the fixed sentence %q", message, wantSourceChangedMessage)
	}
}

// A promotion whose request simply ended is not a proposal that changed. The
// cancellation is carried through the vault's own refusal, and it outranks
// every sentence about the file: the caller is gone, and telling whoever asks
// next that the user's vault moved would be an invention.
func TestMemoryErrorReportsACancelledRequestRatherThanStaleness(t *testing.T) {
	cancelled := errors.Join(memoryfiles.ErrStaleContent, context.Canceled)
	if status.Code(memoryError(cancelled, "reject memory candidate failed")) != codes.Canceled {
		t.Fatalf("a cancelled refusal = %v, want Canceled", memoryError(cancelled, "reject memory candidate failed"))
	}
	expired := errors.Join(memoryfiles.ErrStaleContent, context.DeadlineExceeded)
	if status.Code(memoryError(expired, "reject memory candidate failed")) != codes.DeadlineExceeded {
		t.Fatalf("an expired refusal = %v, want DeadlineExceeded", memoryError(expired, "reject memory candidate failed"))
	}
}
