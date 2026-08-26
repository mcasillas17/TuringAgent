package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// MaxNoteBytes is the safety ceiling on a single note this package will read
// whole — a candidate under inbox/ or a belief under beliefs/.
//
// It is not a budget and not a promise about note size. It is the point past
// which a file stops looking like a note somebody wrote and starts looking like
// something that would be unwise to hold in memory: a log tailed into the
// vault, a database dumped into it, a sync client's accident. Real Obsidian
// notes are two or three orders of magnitude below it, so it blocks nothing the
// user is likely to write, and a file past it is reported rather than partially
// loaded — half a belief is a different belief.
const MaxNoteBytes = 512 * 1024

// PromotionMode names which of the two doors into beliefs/ the caller is
// standing at: a candidate Turing wrote, or a draft the user dropped into the
// inbox themselves. The two carry different obligations, so the permissive one
// is never the default — an unstated mode is the strict door, and the loose
// door has to be asked for by name.
type PromotionMode string

const (
	// PromoteManagedCandidate promotes a candidate that declares kind: belief
	// in its own frontmatter. Anything else — a profile edit, a kind that does
	// not parse, no kind at all — is refused.
	PromoteManagedCandidate PromotionMode = "managed_candidate"

	// PromoteUnmanagedDraft promotes a file the user hand-dropped into inbox/.
	// It has no candidate row and no kind, which is exactly what this door
	// requires: a file that declares a kind belongs to the managed door.
	PromoteUnmanagedDraft PromotionMode = "unmanaged_draft"
)

// PromoteToBeliefsRequest moves one reviewed candidate into the beliefs folder.
// Both paths are checked independently by the primitive itself.
type PromoteToBeliefsRequest struct {
	SourceRelPath string
	// DestinationRelPath is optional. When unset the vault names the file from
	// the note's own identity, exactly as createInboxNote does.
	DestinationRelPath string
	// Mode says which door this promotion comes through. It defaults to
	// PromoteManagedCandidate, so the strict path is the one a caller gets by
	// saying nothing and the permissive one has to be asked for.
	Mode PromotionMode
	// Kind is the kind the caller believes the candidate has. The primitive
	// checks the file's own frontmatter too and refuses on either.
	Kind NoteKind
	// ExpectedContentHash binds the move to the exact bytes the decision was
	// made about, checked against the read this primitive does under its own
	// path lock and immediately before the file moves.
	//
	// It is required at the managed door and optional at the draft one, and the
	// asymmetry is the difference between the two. A managed candidate is
	// always decided from a listing that carried its hash, and the caller's own
	// read of it released the path lock before this call took it — so the only
	// check that can speak for the bytes being moved is this one. A file the
	// user dropped into inbox/ themselves was never listed as a proposal, so
	// there is no hash anybody could have shown them.
	ExpectedContentHash string
}

// BeliefNote is an accepted note as it exists under beliefs/.
type BeliefNote struct {
	NoteID      string
	RelPath     string
	Title       string
	Content     string
	ContentHash string
}

// PromoteToBeliefs physically installs a candidate under beliefs/ and removes
// it from inbox/. It refuses a source outside inbox/, a destination outside
// beliefs/, and a profile_edit candidate — the profile is a document, not a
// belief, and promoting one into beliefs/ would turn an edit into a claim.
//
// Which candidates it will accept at all depends on the Mode the caller states.
// See PromotionMode: managed candidates must declare kind: belief themselves,
// and only a caller explicitly promoting a hand-dropped draft gets the kindless
// path the plan reserves for files the user made in Obsidian.
func (v *Vault) PromoteToBeliefs(ctx context.Context, request PromoteToBeliefsRequest) (BeliefNote, error) {
	if err := ctx.Err(); err != nil {
		return BeliefNote{}, err
	}
	source, err := requireInboxRelPath(request.SourceRelPath)
	if err != nil {
		return BeliefNote{}, err
	}
	destination := ""
	if request.DestinationRelPath != "" {
		if destination, err = requireBeliefsRelPath(request.DestinationRelPath); err != nil {
			return BeliefNote{}, err
		}
	}
	mode := request.Mode
	if mode == "" {
		mode = PromoteManagedCandidate
	}
	if mode != PromoteManagedCandidate && mode != PromoteUnmanagedDraft {
		return BeliefNote{}, fmt.Errorf("promotion mode %q is not recognised: %w", request.Mode, ErrKind)
	}
	if request.Kind != "" && request.Kind != KindBelief {
		return BeliefNote{}, fmt.Errorf("candidate kind %q cannot be promoted to %s/: %w", request.Kind, BeliefsDirName, ErrKind)
	}

	unlockSource, err := v.locks.lockContext(ctx, v.pathLockKey(source))
	if err != nil {
		return BeliefNote{}, err
	}
	defer unlockSource()

	content, sourceStat, err := v.readConfinedFile(ctx, source, MaxNoteBytes)
	if err != nil {
		return BeliefNote{}, err
	}
	// The compare-and-set, against the bytes this primitive just read under the
	// lock it is holding through the move. A caller's earlier read let go of
	// that lock, and the user has the vault open in their editor.
	if err := requireDecidedBytes(source, mode == PromoteManagedCandidate, request.ExpectedContentHash, content); err != nil {
		return BeliefNote{}, err
	}
	parsed, err := ParseNote(source, content)
	if err != nil {
		return BeliefNote{}, err
	}
	// The file's own frontmatter is authoritative: a caller that mislabels a
	// profile edit as a belief still cannot move it here, and a caller that
	// says "unmanaged draft" about a candidate Turing wrote is corrected by
	// the file rather than believed.
	if err := checkPromotable(source, mode, parsed); err != nil {
		return BeliefNote{}, err
	}

	noteID := parsed.ID
	if noteID == "" {
		if noteID, err = NewNoteID(); err != nil {
			return BeliefNote{}, err
		}
	}
	if destination == "" {
		if destination, err = requireBeliefsRelPath(BeliefsDirName + "/" + noteFileName(noteID, parsed.Title)); err != nil {
			return BeliefNote{}, err
		}
	}

	// Always inbox first, then beliefs. Every two-lock operation in this
	// package takes them in that order, so the pair cannot deadlock.
	unlockDestination, err := v.locks.lockContext(ctx, v.pathLockKey(destination))
	if err != nil {
		return BeliefNote{}, err
	}
	defer unlockDestination()

	destinationParent, destinationLeaf, err := v.openParent(ctx, destination, true)
	if err != nil {
		return BeliefNote{}, err
	}
	defer func() { _ = destinationParent.Close() }()
	if err := v.installStagedFile(ctx, destinationParent, destinationLeaf, content); err != nil {
		return BeliefNote{}, err
	}
	// Remembered before anything else can touch the name, so a rollback removes
	// exactly the file this promotion installed and never one someone else put
	// there afterwards.
	var installed unix.Stat_t
	if err := unix.Fstatat(int(destinationParent.Fd()), destinationLeaf, &installed, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return BeliefNote{}, fmt.Errorf("inspect the promoted copy at %q: %w", destination, err)
	}
	if err := v.syncAncestors(ctx, destination); err != nil {
		// Nothing has been removed yet, so the promotion can still be undone
		// whole: a failure before the source is unlinked leaves the vault the
		// way the user left it.
		rollbackErr := v.removeInstalledCopy(destinationParent, destinationLeaf, destination, installed)
		if rollbackErr != nil {
			return BeliefNote{}, fmt.Errorf(
				"sync vault hierarchy after promoting to %q: %w (the copy at %q could not be removed either: %v)",
				destination, err, destination, rollbackErr,
			)
		}
		return BeliefNote{}, fmt.Errorf("sync vault hierarchy after promoting to %q: %w", destination, err)
	}
	removed, err := v.unlinkPromotedSource(ctx, source, sourceStat, content)
	if err != nil {
		if removed {
			// The move happened. Undoing the destination now would delete the
			// only copy of the note, so the failure is reported and the vault
			// is left in the state the user can actually see.
			return BeliefNote{}, fmt.Errorf("promoted %q to %q, but removing the original did not finish cleanly: %w", source, destination, err)
		}
		rollbackErr := v.removeInstalledCopy(destinationParent, destinationLeaf, destination, installed)
		return BeliefNote{}, promotionAbandoned(source, destination, err, rollbackErr)
	}
	return BeliefNote{
		NoteID:      noteID,
		RelPath:     destination,
		Title:       parsed.Title,
		Content:     content,
		ContentHash: ContentHash(content),
	}, nil
}

// ErrSourceChanged marks a promotion abandoned because the candidate stopped
// being the file that was read.
var ErrSourceChanged = errors.New("the candidate changed while it was being promoted")

// checkPromotable is the kind gate, run against the file's own frontmatter
// rather than against what the caller claimed.
//
// It answers two different questions in a fixed order, and they are kept apart
// on purpose. Shape first: is this the kind of file the door the caller named
// is for — one Turing wrote and labelled, or one the user dropped in with no
// label at all. Then the one prohibition: profile_edit is a kind this package
// recognises, accepts from a model, and files under inbox/ like any other
// candidate, and it still may never become a belief.
//
// Neither shape rule mentions profile_edit, so refusing one is the job of a
// single line rather than a side effect of two others. That is what makes the
// prohibition testable: delete it and a profile edit is promoted, through
// either door, instead of being refused by a rule that meant something else.
func checkPromotable(source string, mode PromotionMode, parsed ParsedNote) error {
	if err := checkPromotableShape(source, mode, parsed); err != nil {
		return err
	}
	if parsed.Kind == KindProfileEdit {
		return fmt.Errorf("%q declares kind %q and cannot be promoted to %s/: %w", source, parsed.Kind, BeliefsDirName, ErrKind)
	}
	return nil
}

// checkPromotableShape decides only whether the file came through the right
// door. Which of the recognised kinds may become a belief is not its question.
func checkPromotableShape(source string, mode PromotionMode, parsed ParsedNote) error {
	switch mode {
	case PromoteManagedCandidate:
		// What makes a file a managed candidate is that it declares a kind
		// this package can read. A file with no kind is a hand-written draft
		// and belongs to the other door; a kind that is not one of the two is
		// a file this package will not act on at all.
		if !parsed.Kind.Valid() {
			return fmt.Errorf(
				"%q does not declare a kind this vault recognises (%s), so it cannot be promoted as a managed candidate; a file the user dropped in %s/ themselves is promoted with mode %q: %w",
				source, knownNoteKinds(), InboxDirName, PromoteUnmanagedDraft, ErrKind,
			)
		}
	case PromoteUnmanagedDraft:
		// An unmanaged draft is a file with no candidate row behind it, so a
		// file that declares kind: belief is a managed candidate at the wrong
		// door — and one that declares a kind nothing here reads is not a
		// promotable draft either.
		if parsed.Kind == KindBelief {
			return fmt.Errorf(
				"%q declares kind %q, so it is a managed candidate and is promoted with mode %q, not as an unmanaged draft: %w",
				source, parsed.Kind, PromoteManagedCandidate, ErrKind,
			)
		}
		if parsed.Kind != "" && !parsed.Kind.Valid() {
			return fmt.Errorf(
				"%q declares kind %q, which is not one of %s, so it cannot be promoted at all: %w",
				source, parsed.Kind, knownNoteKinds(), ErrKind,
			)
		}
		if parsed.Managed {
			return fmt.Errorf(
				"%q is marked managed, so it is not a draft the user wrote by hand; promote it with mode %q: %w",
				source, PromoteManagedCandidate, ErrKind,
			)
		}
	}
	return nil
}

// promotionAbandoned says what happened to both halves of the move. The user
// has to be able to act on this: their edit is still in the inbox, and the
// half-promoted copy either went away or is named here so they can delete it.
func promotionAbandoned(source string, destination string, cause error, rollbackErr error) error {
	if rollbackErr != nil {
		return fmt.Errorf(
			"%q changed while it was being promoted (%v); the original was left alone, but the promoted copy of the older text at %q could not be removed (%v) — delete it or re-read and promote again: %w",
			source, cause, destination, rollbackErr, ErrSourceChanged,
		)
	}
	return fmt.Errorf(
		"%q changed while it was being promoted (%v); the original was left alone and the promoted copy of the older text was removed — re-read it and promote again: %w",
		source, cause, ErrSourceChanged,
	)
}

// unlinkPromotedSource removes the source of a move only if it is still, byte
// for byte, the file that was promoted, and reports whether it actually went
// away.
//
// Inode identity alone is not enough. Obsidian saves in place, so a sentence the
// user typed between the read and the unlink lands under the same inode, and an
// inode-only check would call that unchanged and delete their words. Anything
// this cannot confirm — a read error, a new inode, different bytes — is treated
// as changed, because the cost of being wrong is the user's own text.
//
// The removed flag is what keeps the caller's rollback honest: once the source
// is gone the move has happened, and a later failure must not be answered by
// deleting the copy that is now the only one.
func (v *Vault) unlinkPromotedSource(ctx context.Context, clean string, expected unix.Stat_t, promoted string) (bool, error) {
	parent, leaf, err := v.openParent(ctx, clean, false)
	if err != nil {
		return false, err
	}
	defer func() { _ = parent.Close() }()

	current, currentStat, err := v.readConfinedEntry(ctx, parent, leaf, clean, MaxNoteBytes)
	if err != nil {
		return false, fmt.Errorf("re-read %q before removing it: %w", clean, err)
	}
	if currentStat.Dev != expected.Dev || currentStat.Ino != expected.Ino {
		return false, fmt.Errorf("%q is no longer the same file", clean)
	}
	if ContentHash(current) != ContentHash(promoted) {
		return false, fmt.Errorf("%q holds different bytes than the ones that were promoted", clean)
	}
	// The name is re-checked against the fd that was just read, so the unlink
	// below removes the inode whose bytes were compared rather than whatever
	// arrived under that name in between.
	var named unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), leaf, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return false, fmt.Errorf("inspect %q before removing it: %w", clean, err)
	}
	if named.Dev != currentStat.Dev || named.Ino != currentStat.Ino {
		return false, fmt.Errorf("%q was replaced under its own name", clean)
	}
	if err := unix.Unlinkat(int(parent.Fd()), leaf, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return false, fmt.Errorf("remove %q after promoting it: %w", clean, err)
	}
	if err := v.syncDirectory(parent); err != nil {
		return true, fmt.Errorf("sync vault directory after removing %q: %w", clean, err)
	}
	return true, ctx.Err()
}

// removeInstalledCopy undoes the destination half of a promotion that could not
// finish. A promoted belief made from bytes the user has already replaced is
// worse than no belief at all: it is a memory of something they retracted, and
// nothing else would ever tell them it is there.
//
// It takes no context on purpose. This is the compensating half of a mutation
// that already happened, and abandoning it because a caller's deadline expired
// would leave behind exactly the file it exists to remove.
func (v *Vault) removeInstalledCopy(parent *os.File, leaf string, clean string, installed unix.Stat_t) error {
	var current unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), leaf, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("inspect the promoted copy at %q: %w", clean, err)
	}
	if current.Dev != installed.Dev || current.Ino != installed.Ino {
		return fmt.Errorf("%q is no longer the copy this promotion installed", clean)
	}
	if err := unix.Unlinkat(int(parent.Fd()), leaf, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("remove the promoted copy at %q: %w", clean, err)
	}
	if err := v.syncDirectory(parent); err != nil {
		return fmt.Errorf("sync vault directory after removing the promoted copy at %q: %w", clean, err)
	}
	return nil
}

// unopenableEntryError marks a read that failed because the entry could not be
// opened at all, as opposed to one that failed while its bytes were being read.
// Nothing outside this package sees the difference — the message and the errno
// underneath are unchanged, and errors.Is answers exactly as before — but a
// hashless rejection is bound to the broad way its candidate refused to be
// read, and "nothing can open it" and "it is too big to read" are not the same
// refusal by a file in the same state.
type unopenableEntryError struct{ err error }

func (e *unopenableEntryError) Error() string { return e.err.Error() }

func (e *unopenableEntryError) Unwrap() error { return e.err }

// readConfinedFile reads one vault entry through the descriptor walk. It
// refuses a symlink and anything that is not a regular file, and it refuses an
// over-limit file outright rather than returning a prefix of it.
func (v *Vault) readConfinedFile(ctx context.Context, clean string, limit int) (string, unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := ctx.Err(); err != nil {
		return "", stat, err
	}
	parent, leaf, err := v.openParent(ctx, clean, false)
	if err != nil {
		return "", stat, err
	}
	defer func() { _ = parent.Close() }()
	return v.readConfinedEntry(ctx, parent, leaf, clean, limit)
}

// readConfinedEntry is the same read against a parent descriptor the caller
// already holds, so a read and the mutation that follows it can share one
// walk instead of resolving the path twice.
func (v *Vault) readConfinedEntry(ctx context.Context, parent *os.File, leaf string, clean string, limit int) (string, unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := ctx.Err(); err != nil {
		return "", stat, err
	}
	if err := unix.Fstatat(int(parent.Fd()), leaf, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return "", stat, fmt.Errorf("inspect %q: %w", clean, err)
	}
	if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
		return "", stat, confinementError(clean, "entry is a symlink; Turing never reads the vault through a link")
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return "", stat, confinementError(clean, "entry is not a regular file")
	}
	fd, err := unix.Openat(int(parent.Fd()), leaf, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", stat, &unopenableEntryError{err: fmt.Errorf("open %q: %w", clean, err)}
	}
	file := os.NewFile(uintptr(fd), clean)
	if file == nil {
		_ = unix.Close(fd)
		return "", stat, fmt.Errorf("open %q: invalid descriptor", clean)
	}
	defer func() { _ = file.Close() }()

	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return "", stat, fmt.Errorf("inspect open %q: %w", clean, err)
	}
	if opened.Mode&unix.S_IFMT != unix.S_IFREG {
		return "", stat, confinementError(clean, "entry is not a regular file")
	}
	content, err := readBounded(ctx, file, limit)
	if err != nil {
		return "", opened, fmt.Errorf("read %q: %w", clean, err)
	}
	if len(content) > limit {
		return "", opened, &LimitError{What: fmt.Sprintf("note %q", clean), Limit: limit, Got: len(content)}
	}
	return string(content), opened, nil
}

// readBounded reads at most limit+1 bytes, so the caller can tell "exactly at
// the limit" from "over it" without ever holding an unbounded file in memory.
func readBounded(ctx context.Context, reader io.Reader, limit int) ([]byte, error) {
	if limit < 0 {
		return nil, errors.New("read limit must not be negative")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	content, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	return content, ctx.Err()
}
