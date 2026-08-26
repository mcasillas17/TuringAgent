package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// A promotion that will not delete its source puts the original back and says
// why. When the request behind it ended while that restore was running, "why"
// is not the objection any more.
//
// The primitive asks whether the request is still there before it verifies
// anything, and answered that question once. But the restore is three
// directory operations and up to three fsyncs long, and a deadline that
// expires inside it — or a user closing the window — arrives after that check
// and before the answer is chosen. The objection was then reported as itself:
// the caller got "this proposal changed while it was being promoted", Aborted,
// and a retry loop was sent round again on a claim about the user's vault made
// on behalf of somebody who had already gone.
//
// The cancellation is what happened. It carries the same bounded, content-free
// sentence about where the bytes ended up, because a caller that has gone still
// leaves an operator who has to find the file.

// cancelledRestore runs one managed promotion whose source is rewritten in
// place — so the removal refuses — and whose request ends while the original is
// going back under its own name.
//
// The barrier is scoped to the inbox: the rollback of the installed copy runs
// through the same restore under beliefs/, on a context of its own, and a test
// that cancelled there would be testing the rollback instead.
func cancelledRestore(t *testing.T, link linkHook, atRestore func(vault *Vault, clean string)) (*Vault, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var vault *Vault
	rewritten := false
	vault = vaultWithRemovalSeams(t, realSyncHooks(), func(phase detachPhase, clean string) {
		if !inInbox(clean) {
			return
		}
		switch {
		case phase == detachPhaseBeforeDetach && !rewritten:
			rewritten = true
			rewriteInPlace(t, vault, clean, "the words the user typed after Turing read it")
		case phase == detachPhaseBeforeRestore:
			if atRestore != nil {
				atRestore(vault, clean)
			}
			cancel()
		}
	}, link, nil)
	candidate := seedBelief(t, vault)

	return vault, promoteCandidate(ctx, vault, candidate)
}

// requireEndedPromotion holds the answer to being the cancellation, in the type
// that carries where the bytes are, and never the claim that the user's own
// file moved.
func requireEndedPromotion(t *testing.T, err error) *EndedRequestError {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a promotion cancelled during its restore = %v, want the cancellation reported", err)
	}
	if errors.Is(err, ErrSourceChanged) {
		t.Fatalf("a cancelled promotion was reported as the user's proposal changing: %v", err)
	}
	var ended *EndedRequestError
	if !errors.As(err, &ended) {
		t.Fatalf("a cancelled promotion = %v, want it to carry where the bytes were left", err)
	}
	if len(ended.Detail) > maxRefusalDetailBytes+len("…") {
		t.Fatalf("the cancellation detail is %d bytes, past the %d-byte bound: %q",
			len(ended.Detail), maxRefusalDetailBytes, ended.Detail)
	}
	return ended
}

// The plain case. The name is free, the original goes back under it cleanly,
// and the only thing left to say is that the request ended.
func TestPromotionCancelledDuringACleanRestoreReportsTheCancellation(t *testing.T) {
	const rewritten = "the words the user typed after Turing read it"
	vault, err := cancelledRestore(t, nil, nil)

	ended := requireEndedPromotion(t, err)
	source := filepath.Join(vault.Root(), InboxDirName)
	restored := ""
	for _, name := range vaultDirEntries(t, vault, InboxDirName) {
		if strings.HasSuffix(name, noteFileExtension) {
			restored = name
		}
	}
	if restored == "" {
		t.Fatalf("the original is not back in the inbox: %v", vaultDirEntries(t, vault, InboxDirName))
	}
	held, readErr := os.ReadFile(filepath.Join(source, restored))
	if readErr != nil || string(held) != rewritten {
		t.Fatalf("the original was not put back: %q, %v", held, readErr)
	}
	if strings.Contains(err.Error(), rewritten) {
		t.Fatalf("the cancellation repeated what was in the file: %v", err)
	}
	if !strings.Contains(ended.Detail, "left alone") {
		t.Fatalf("the cancellation does not say the file was left alone: %q", ended.Detail)
	}
	requireNoStagingResidue(t, vault)
	if drafts := recoveryDrafts(t, vault); len(drafts) != 0 {
		t.Fatalf("a clean restore published recovery drafts: %v", drafts)
	}
}

// The contested case. Another writer took the name while the original was off
// it, so the bytes are kept under a visible draft the user will see in the next
// listing — and the answer is still that the request ended, with the draft
// named for whoever reads the log.
func TestPromotionCancelledDuringAContestedRestoreReportsTheCancellation(t *testing.T) {
	const contender = "a third file, under the same name"
	vault, err := cancelledRestore(t, nil, func(vault *Vault, clean string) {
		takeTheName(t, vault, clean, contender)
	})

	ended := requireEndedPromotion(t, err)
	draft, kept := requireOneRecoveryDraft(t, vault)
	if kept != "the words the user typed after Turing read it" {
		t.Fatalf("the recovery draft holds %q, want the detached original", kept)
	}
	if !strings.Contains(ended.Detail, draft) {
		t.Fatalf("the cancellation does not say where the bytes were kept: %q", ended.Detail)
	}
	if strings.Contains(err.Error(), kept) {
		t.Fatalf("the cancellation repeated what was in the file: %v", err)
	}
	requireNoStagingResidue(t, vault)
}

// And the last resort. Nothing can be linked at all, so the bytes stay under
// the reserved name the detach put them under. The sentence changes; the
// cancellation does not.
func TestPromotionCancelledWithBytesLeftStagedReportsTheCancellation(t *testing.T) {
	vault, err := cancelledRestore(t, func(string, func() error) error {
		return unix.EIO
	}, nil)

	ended := requireEndedPromotion(t, err)
	staged := stagingResidueIn(t, vault, InboxDirName)
	if len(staged) != 1 {
		t.Fatalf("expected the bytes to stay under one reserved name, found %v", staged)
	}
	held, readErr := os.ReadFile(filepath.Join(vault.Root(), InboxDirName, staged[0]))
	if readErr != nil || string(held) != "the words the user typed after Turing read it" {
		t.Fatalf("the reserved name does not hold the detached original: %q, %v", held, readErr)
	}
	if !strings.Contains(ended.Detail, staged[0]) {
		t.Fatalf("the cancellation does not say where the bytes were kept: %q", ended.Detail)
	}
	if strings.Contains(err.Error(), string(held)) {
		t.Fatalf("the cancellation repeated what was in the file: %v", err)
	}
	if drafts := recoveryDrafts(t, vault); len(drafts) != 0 {
		t.Fatalf("a rescue that could not link still published %v", drafts)
	}
}
