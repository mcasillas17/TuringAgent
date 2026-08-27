package memoryfiles

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// A rescue is four steps — link the visible name, flush, drop the staging name,
// flush again — and three of them can fail. Until this round all three came
// back through one answer, so the caller could not tell them apart and said the
// same sentence about all of them: "it was kept for recovery at X, but a second
// link to it could not be dropped".
//
// For the middle two that is true. For the first flush it is two false claims
// at once. The drop never ran, so nothing "could not be dropped" — and the link
// the sentence sends the user to is one no fsync has established, so after a
// crash it can be a name that never existed. Blaming a step that never
// happened, and promising a location nobody flushed, are the two ways a refusal
// about somebody's file can lie.
//
// These three tests stand at each step and hold the refusal to the state that
// step actually leaves behind.

// rescueSyncStep names which of the rescue's two directory flushes a test
// fails. They are numbered from the moment the restore is about to happen,
// because the restore's own link fails first on every one of these paths and a
// count from further back changes whenever anything else learns to fsync.
const (
	rescueFirstFlush  = 1
	rescueSecondFlush = 2
)

// rescuedBytes are the words the detached entry actually holds by the time the
// rescue is reached: the rejection refuses because the user rewrote the note in
// place after deciding, so what is being kept is text nobody has read.
const rescuedBytes = "the words the user typed after they decided"

// refuseWithARescue runs one rejection whose restore cannot take the note's own
// name back — another file is under it — so the bytes go down the rescue road.
// arm stands at the moment just before the restore, which is where a test
// decides which step of the rescue is going to fail.
func refuseWithARescue(
	t *testing.T,
	recorder *syncRecorder,
	unlink unlinkHook,
	arm func(),
) (*Vault, error) {
	t.Helper()
	const decided = "the proposal the user read"
	const contender = "a third file, under the same name"
	var vault *Vault
	vault = vaultWithRemovalSeams(t, recorder.hooks(), func(phase detachPhase, _ string) {
		switch phase {
		case detachPhaseBeforeDetach:
			rewriteInPlace(t, vault, "inbox/note.md", rescuedBytes)
		case detachPhaseBeforeRestore:
			contestTheName(t, vault, contender)
			arm()
		}
	}, nil, unlink)
	full := writeVaultFile(t, vault, "inbox/note.md", decided)

	err := vault.RemoveInboxNote(context.Background(), RemoveInboxNoteRequest{
		RelPath:             "inbox/note.md",
		Mode:                RemoveDecidedCandidate,
		ExpectedContentHash: ContentHash(decided),
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("rejection = %v, want a stale-content refusal", err)
	}
	if held, readErr := os.ReadFile(full); readErr != nil || string(held) != contender {
		t.Fatalf("the file that took the name was disturbed: %q, %v", held, readErr)
	}
	return vault, err
}

// The first flush. The visible name is linked and nothing has reached the disk,
// so the staging name is deliberately kept: it is the other name the same bytes
// are under, and dropping it now would trade a link a crash might find for one
// it might not.
//
// What the refusal owes is both names and the caveat. It must not say the drop
// failed, because the drop never ran.
func TestRescueThatCannotFlushTheRecoveryLinkKeepsBothNames(t *testing.T) {
	recorder := &syncRecorder{}
	vault, err := refuseWithARescue(t, recorder, nil, func() {
		recorder.setFailDirectorySyncNumber(rescueFirstFlush)
	})

	draft, drafted := requireOneRecoveryDraft(t, vault)
	if drafted != rescuedBytes {
		t.Fatalf("the recovery draft holds %q, want the detached bytes", drafted)
	}
	staged, held := stagingResidueContent(t, vault, InboxDirName)
	if held != rescuedBytes {
		t.Fatalf("the staging entry holds %q, want the detached bytes", held)
	}

	stale := requireBoundedRefusal(t, err, rescuedBytes)
	if !strings.Contains(stale.Detail, draft) {
		t.Fatalf("the refusal does not say where the bytes were kept: %q", stale.Detail)
	}
	if !strings.Contains(stale.Detail, staged) {
		t.Fatalf("the refusal does not name the reserved name the bytes are also under: %q", stale.Detail)
	}
	// The attribution. Nothing was dropped and nothing failed to be dropped:
	// the rescue stopped before the drop, on purpose.
	if strings.Contains(stale.Detail, "could not be dropped") {
		t.Fatalf("the refusal blames a drop that never ran: %q", stale.Detail)
	}
	if strings.Contains(stale.Detail, "come back") {
		t.Fatalf("the refusal reports a link that is on disk as one a crash would restore: %q", stale.Detail)
	}
	// The durability. The recovery link is not one a crash is guaranteed to
	// keep, and a person being sent to it has to know that.
	if !strings.Contains(stale.Detail, "survived a crash") {
		t.Fatalf("the refusal promises a recovery location it never flushed: %q", stale.Detail)
	}
	if !strings.Contains(err.Error(), "simulated directory fsync failure") {
		t.Fatalf("the refusal does not carry the fsync failure underneath it: %v", err)
	}
}

// The drop itself. The recovery link is durable, the staging name will not go
// away, and it is a file somebody can go and open right now. This is the one
// case where "could not be dropped" is the true sentence.
func TestRescueThatCannotDropTheStagingNameSaysTheDropFailed(t *testing.T) {
	recorder := &syncRecorder{}
	vault, err := refuseWithARescue(t, recorder, failStagingUnlink(), func() {})

	draft, drafted := requireOneRecoveryDraft(t, vault)
	if drafted != rescuedBytes {
		t.Fatalf("the recovery draft holds %q, want the detached bytes", drafted)
	}
	staged, held := stagingResidueContent(t, vault, InboxDirName)
	if held != rescuedBytes {
		t.Fatalf("the staging entry holds %q, want the detached bytes", held)
	}

	stale := requireBoundedRefusal(t, err, rescuedBytes)
	if !strings.Contains(stale.Detail, draft) {
		t.Fatalf("the refusal does not say where the bytes were kept: %q", stale.Detail)
	}
	if !strings.Contains(stale.Detail, staged) {
		t.Fatalf("the refusal does not name the link it could not drop: %q", stale.Detail)
	}
	if !strings.Contains(stale.Detail, "could not be dropped") {
		t.Fatalf("the refusal does not say the drop failed: %q", stale.Detail)
	}
	if strings.Contains(stale.Detail, "come back") {
		t.Fatalf("the refusal reports a link that is on disk as one a crash would restore: %q", stale.Detail)
	}
	// The recovery link itself was flushed, so there is nothing to hedge.
	if strings.Contains(stale.Detail, "survived a crash") {
		t.Fatalf("the refusal hedges a recovery link that did reach the disk: %q", stale.Detail)
	}
	if !errors.Is(err, errStagingUnlink) {
		t.Fatalf("the refusal does not carry the unlink failure underneath it: %v", err)
	}
}

// The second flush. The drop happened; only its flush did not, so the reserved
// name is gone today and can come back after a crash. Saying it "could not be
// dropped" sends a person after a file that is not there.
func TestRescueThatCannotFlushTheDropSaysTheNameCanComeBack(t *testing.T) {
	recorder := &syncRecorder{}
	vault, err := refuseWithARescue(t, recorder, nil, func() {
		recorder.setFailDirectorySyncNumber(rescueSecondFlush)
	})

	draft, drafted := requireOneRecoveryDraft(t, vault)
	if drafted != rescuedBytes {
		t.Fatalf("the recovery draft holds %q, want the detached bytes", drafted)
	}
	requireNoStagingResidue(t, vault)

	stale := requireBoundedRefusal(t, err, rescuedBytes)
	if !strings.Contains(stale.Detail, draft) {
		t.Fatalf("the refusal does not say where the bytes were kept: %q", stale.Detail)
	}
	if !strings.Contains(stale.Detail, stagingPrefix) {
		t.Fatalf("the refusal does not name the link whose removal was not flushed: %q", stale.Detail)
	}
	if strings.Contains(stale.Detail, "could not be dropped") {
		t.Fatalf("the refusal claims a link that was dropped is still there: %q", stale.Detail)
	}
	if !strings.Contains(stale.Detail, "come back") {
		t.Fatalf("the refusal does not say the name can come back after a crash: %q", stale.Detail)
	}
	// The recovery link reached the disk before the drop was attempted, so the
	// placement itself is known good.
	if strings.Contains(stale.Detail, "survived a crash") {
		t.Fatalf("the refusal hedges a recovery link that did reach the disk: %q", stale.Detail)
	}
	if !strings.Contains(err.Error(), "simulated directory fsync failure") {
		t.Fatalf("the refusal does not carry the fsync failure underneath it: %v", err)
	}
}
