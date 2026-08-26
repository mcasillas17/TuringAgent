package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// ErrStaleContent refuses a compare-and-set whose expected content no longer
// matches what is on disk. Its message is written for a human, because the
// person who has to act on it is the one holding the vault open.
var ErrStaleContent = errors.New("the file changed since it was read; re-read it and apply the edit again")

// MaxProfileEditBytes bounds what an accepted profile_edit may write. The
// content of an apply is the *whole resulting profile* the user reviewed, not
// the candidate's own claim — so it is bounded exactly like a hand-authored
// save: by what this package can read back. Binding it to the 16 KiB candidate
// body instead made every profile longer than one proposal permanently
// un-appliable, which is the trap MaxAuthoredDocumentBytes exists to avoid.
//
// The candidate body stays at MaxCandidateBodyBytes and is still refused there:
// a model cannot launder an over-limit claim in through this door, because the
// claim never reaches this bound — the document the user composed does.
const MaxProfileEditBytes = MaxAuthoredDocumentBytes

// StaleContentError names the file whose compare-and-set failed.
type StaleContentError struct {
	RelPath string
	Detail  string
	// Cause is the failure underneath a refusal that had one — the link that
	// would not be made, the entry that could not be inspected. It is carried
	// rather than only described so a caller can match on it: a refusal whose
	// real reason is EIO and one whose real reason is another writer are the
	// same sentence to a person and different facts to an operator.
	Cause error
}

func (e *StaleContentError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("%s: %v", e.RelPath, ErrStaleContent)
	}
	return fmt.Sprintf("%s: %s: %v", e.RelPath, e.Detail, ErrStaleContent)
}

// Unwrap answers with both, so errors.Is finds the sentinel every caller
// already matches on and the cause the operator needs.
func (e *StaleContentError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrStaleContent}
	}
	return []error{ErrStaleContent, e.Cause}
}

// ApplyProfileEditRequest applies one reviewed profile_edit candidate to
// profile.md. Content is the whole resulting document, because the user may
// have edited the proposal before accepting it.
type ApplyProfileEditRequest struct {
	CandidateRelPath string
	TargetRelPath    string
	// ExpectedContentHash is the compare-and-set token. Empty means the caller
	// expects no profile to exist yet.
	ExpectedContentHash string
	// ExpectedCandidateHash is the other compare-and-set, and it names a
	// different document: the proposal in inbox/ this apply is drawing its
	// authority from. It is required, and it is checked against the read this
	// primitive does under the candidate's own path lock, immediately before
	// the profile is written — the caller's earlier read gave that lock back,
	// and rewriting the user's profile on the authority of a proposal they have
	// since rewritten is the failure this closes.
	ExpectedCandidateHash string
	Content               string
}

// ProfileDocument is profile.md as it stands after an apply. ContentHash covers
// the whole document that was written, and the two pinned fields say what a run
// will carry of it — the same separation the editor read makes, so a page that
// adopts an apply receipt holds a token the next save can actually use.
type ProfileDocument struct {
	RelPath         string
	Content         string
	ContentHash     string
	PinnedTruncated bool
	PinnedBytes     int
}

// ApplyProfileEdit writes profile.md and nothing else, and only on the
// authority of a profile_edit candidate sitting in inbox/. persona.md is
// refused outright: it is the user's description of Turing, not Turing's
// description of the user, and nothing in this package may write it.
//
// The compare-and-set is verified through the same descriptor that is written,
// so the file that was hashed is provably the file that is updated. The update
// is in place rather than a rename-replace: the user very likely has profile.md
// open in Obsidian, and swapping the inode underneath an open editor is how a
// "safe" write turns into a lost document.
func (v *Vault) ApplyProfileEdit(ctx context.Context, request ApplyProfileEditRequest) (ProfileDocument, error) {
	if err := ctx.Err(); err != nil {
		return ProfileDocument{}, err
	}
	candidate, err := requireInboxRelPath(request.CandidateRelPath)
	if err != nil {
		return ProfileDocument{}, err
	}
	target, err := requireProfileRelPath(request.TargetRelPath)
	if err != nil {
		return ProfileDocument{}, err
	}
	if len(request.Content) > MaxProfileEditBytes {
		return ProfileDocument{}, &LimitError{What: "profile document", Limit: MaxProfileEditBytes, Got: len(request.Content)}
	}

	unlockCandidate, err := v.locks.lockContext(ctx, v.pathLockKey(candidate))
	if err != nil {
		return ProfileDocument{}, err
	}
	defer unlockCandidate()

	candidateContent, _, err := v.readConfinedFile(ctx, candidate, MaxNoteBytes)
	if err != nil {
		return ProfileDocument{}, err
	}
	// Bound to the proposal the user decided about, against the bytes read
	// under the lock this call is holding — and before the target lock, so a
	// refusal here has touched nothing at all.
	if err := requireDecidedBytes(candidate, true, request.ExpectedCandidateHash, candidateContent); err != nil {
		return ProfileDocument{}, err
	}
	parsed, err := ParseNote(candidate, candidateContent)
	if err != nil {
		return ProfileDocument{}, err
	}
	// An undeclared kind is refused as firmly as a wrong one. Rewriting the
	// user's profile needs an explicit profile_edit, not an inference.
	if parsed.Kind != KindProfileEdit {
		return ProfileDocument{}, fmt.Errorf("%q declares kind %q, so it cannot edit %s: %w", candidate, parsed.Kind, target, ErrKind)
	}

	unlockTarget, err := v.locks.lockContext(ctx, v.pathLockKey(target))
	if err != nil {
		return ProfileDocument{}, err
	}
	defer unlockTarget()

	if err := v.writePinnedDocumentWithCompareAndSet(ctx, target, request.ExpectedContentHash, request.Content); err != nil {
		return ProfileDocument{}, err
	}
	pinnedBytes, truncated := pinnedBudget(request.Content, MaxProfileBytes)
	return ProfileDocument{
		RelPath:         target,
		Content:         request.Content,
		ContentHash:     ContentHash(request.Content),
		PinnedTruncated: truncated,
		PinnedBytes:     pinnedBytes,
	}, nil
}

// writePinnedDocumentWithCompareAndSet is the one fd-verified in-place write in
// this package. Its callers each gate their own target first — an accepted
// proposal for profile.md above, the user's own hand for either pinned document
// in authored.go — so it never sees a path a caller chose freely.
func (v *Vault) writePinnedDocumentWithCompareAndSet(ctx context.Context, target string, expectedHash string, content string) error {
	parent, leaf, err := v.openParent(ctx, target, false)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()

	fd, openErr := unix.Openat(int(parent.Fd()), leaf, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if openErr != nil {
		if errors.Is(openErr, unix.ENOENT) {
			if expectedHash != "" {
				return &StaleContentError{RelPath: target, Detail: "the document no longer exists"}
			}
			if err := v.installStagedFile(ctx, parent, leaf, content); err != nil {
				return err
			}
			return v.syncAncestors(ctx, target)
		}
		return fmt.Errorf("open %q: %w", target, openErr)
	}
	file := os.NewFile(uintptr(fd), target)
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open %q: invalid descriptor", target)
	}
	defer func() { _ = file.Close() }()

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("inspect %q: %w", target, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return confinementError(target, "entry is not a regular file")
	}
	// The document already on disk is read against the pinned ceiling, not the
	// edit limit: the user may have written a profile far longer than anything
	// a candidate could propose, and the compare-and-set still has to be able
	// to tell them their expected hash no longer matches.
	current, err := readBounded(ctx, file, MaxPinnedSourceBytes)
	if err != nil {
		return fmt.Errorf("read %q: %w", target, err)
	}
	if len(current) > MaxPinnedSourceBytes {
		return &LimitError{What: fmt.Sprintf("existing %q", target), Limit: MaxPinnedSourceBytes, Got: len(current)}
	}
	if ContentHash(string(current)) != expectedHash {
		return &StaleContentError{RelPath: target}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Write first, then truncate. The reverse order would leave an empty
	// profile visible if the process died between the two, which is exactly the
	// user text this compare-and-set exists to protect.
	if _, err := file.WriteAt([]byte(content), 0); err != nil {
		return fmt.Errorf("write %q: %w", target, err)
	}
	if err := file.Truncate(int64(len(content))); err != nil {
		return fmt.Errorf("truncate %q: %w", target, err)
	}
	if err := v.syncFile(file); err != nil {
		return fmt.Errorf("sync %q: %w", target, err)
	}
	if err := v.syncDirectory(parent); err != nil {
		return fmt.Errorf("sync the vault root after writing %q: %w", target, err)
	}
	return ctx.Err()
}
