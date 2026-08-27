package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A request that ends while a candidate is off its name is not a verdict about
// the file, and the file goes back. When the name is free that is the whole
// story, and the primitive already reported it as the cancellation it was.
//
// When the name is *not* free it stopped saying so. The bytes went to a
// recovery name, and the refusal that came back said the file had changed since
// it was read — which is a claim about the user's vault, made on behalf of a
// caller that had already gone, and it hid the one fact the caller could act
// on: their request was cancelled. A retry loop that matches on staleness reads
// that as "read it again"; a deadline that keeps expiring in the same place
// then looks like a vault that keeps rewriting itself.
//
// So the cancellation survives every branch of the restore, and what is added
// on top of it is where the bytes are — bounded, loggable, and never a word of
// what the file says.

// contestedCancellation runs one rejection that is cancelled while the file is
// off its name and finds its own name taken when it tries to go back.
func contestedCancellation(t *testing.T, link linkHook) (*Vault, string, error) {
	t.Helper()
	const decided = "the proposal the user read"
	const contender = "a third file, under the same name"
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var vault *Vault
	vault = vaultWithRemovalSeams(t, realSyncHooks(), func(phase detachPhase, _ string) {
		switch phase {
		case detachPhaseBeforeVerify:
			cancel()
		case detachPhaseBeforeRestore:
			contestTheName(t, vault, contender)
		}
	}, link, nil)
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(ctx, RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if held, readErr := os.ReadFile(full); readErr != nil || string(held) != contender {
		t.Fatalf("the file that took the name was disturbed: %q, %v", held, readErr)
	}
	return vault, decided, err
}

// The finding. Cancelled, the name taken, the bytes rescued into a draft the
// user can see — and the answer is still that the request ended.
func TestCancelledRejectionWithAContestedNameStillReportsTheCancellation(t *testing.T) {
	vault, decided, err := contestedCancellation(t, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled rejection with a contested name = %v, want the cancellation reported", err)
	}
	draft, kept := requireOneRecoveryDraft(t, vault)
	if kept != decided {
		t.Fatalf("the recovery draft holds %q, want the detached proposal", kept)
	}
	// The cancellation is the answer; where the bytes went is the detail that
	// rides along with it, so an operator reading the log can go and look.
	if !strings.Contains(err.Error(), draft) {
		t.Fatalf("the cancellation does not say where the bytes were kept: %v", err)
	}
	if strings.Contains(err.Error(), decided) {
		t.Fatalf("the cancellation repeated what was in the file: %v", err)
	}
	requireNoStagingResidue(t, vault)
}

// The same, one step worse: the rescue cannot take a visible name either, so
// the bytes stay under the reserved one the detach put them under. The sentence
// changes; the cancellation does not.
func TestCancelledRejectionKeptUnderAReservedNameStillReportsTheCancellation(t *testing.T) {
	vault, decided, err := contestedCancellation(t, func(target string, link func() error) error {
		if IsRecoveryDraftName(target) {
			return unix.EIO
		}
		return link()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled rejection kept under a reserved name = %v, want the cancellation reported", err)
	}
	staged := stagingResidueIn(t, vault, InboxDirName)
	if len(staged) != 1 {
		t.Fatalf("expected the bytes to stay under one reserved name, found %v", staged)
	}
	held, readErr := os.ReadFile(filepath.Join(vault.Root(), InboxDirName, staged[0]))
	if readErr != nil || string(held) != decided {
		t.Fatalf("the reserved name does not hold the detached proposal: %q, %v", held, readErr)
	}
	if !strings.Contains(err.Error(), staged[0]) {
		t.Fatalf("the cancellation does not say where the bytes were kept: %v", err)
	}
	if strings.Contains(err.Error(), decided) {
		t.Fatalf("the cancellation repeated what was in the file: %v", err)
	}
	if drafts := recoveryDrafts(t, vault); len(drafts) != 0 {
		t.Fatalf("a rescue that failed still published %v", drafts)
	}
}

// A deadline is the other way a request ends, and a caller that has run out of
// time is owed the same answer as one that changed its mind. The hashless door
// is the one with no re-read to fail, so it is the one where a wrong answer
// here would be a deletion rather than a sentence.
func TestExpiredHashlessRejectionWithAContestedNameReportsTheDeadline(t *testing.T) {
	const unreadable = unparseableCandidate
	const contender = "a third file, under the same name"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var vault *Vault
	vault = vaultWithRemovalSeams(t, realSyncHooks(), func(phase detachPhase, _ string) {
		switch phase {
		case detachPhaseBeforeVerify:
			<-ctx.Done()
		case detachPhaseBeforeRestore:
			contestTheName(t, vault, contender)
		}
	}, nil, nil)
	full := writeVaultFile(t, vault, "inbox/note.md", unreadable)
	identity := unreadableIdentity(t, vault, "inbox/note.md")

	err := vault.RemoveInboxNote(ctx, RemoveInboxNoteRequest{
		RelPath:    "inbox/note.md",
		Mode:       RemoveUnreadableCandidate,
		Unreadable: identity,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired rejection with a contested name = %v, want the deadline reported", err)
	}
	if held, readErr := os.ReadFile(full); readErr != nil || string(held) != contender {
		t.Fatalf("the file that took the name was disturbed: %q, %v", held, readErr)
	}
	_, kept := requireOneRecoveryDraft(t, vault)
	if kept != unreadable {
		t.Fatalf("the recovery draft holds %q, want the detached proposal", kept)
	}
	requireNoStagingResidue(t, vault)
}
