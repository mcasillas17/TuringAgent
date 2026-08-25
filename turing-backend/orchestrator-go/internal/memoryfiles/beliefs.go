package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// MaxNoteFileBytes bounds any single note this package will read whole. It is
// larger than a candidate body because a promoted note accumulates frontmatter
// and the user may keep writing in it, but it is still a bound: a file past it
// is reported, never partially loaded.
const MaxNoteFileBytes = 512 * 1024

// PromoteToBeliefsRequest moves one reviewed candidate into the beliefs folder.
// Both paths are checked independently by the primitive itself.
type PromoteToBeliefsRequest struct {
	SourceRelPath string
	// DestinationRelPath is optional. When unset the vault names the file from
	// the note's own identity, exactly as createInboxNote does.
	DestinationRelPath string
	// Kind is the kind the caller believes the candidate has. The primitive
	// checks the file's own frontmatter too and refuses on either.
	Kind NoteKind
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
	if request.Kind != "" && request.Kind != KindBelief {
		return BeliefNote{}, fmt.Errorf("candidate kind %q cannot be promoted to %s/: %w", request.Kind, BeliefsDirName, ErrKind)
	}

	unlockSource, err := v.locks.lockContext(ctx, v.pathLockKey(source))
	if err != nil {
		return BeliefNote{}, err
	}
	defer unlockSource()

	content, sourceStat, err := v.readConfinedFile(ctx, source, MaxNoteFileBytes)
	if err != nil {
		return BeliefNote{}, err
	}
	parsed, err := ParseNote(source, content)
	if err != nil {
		return BeliefNote{}, err
	}
	// The file's own frontmatter is authoritative: a caller that mislabels a
	// profile edit as a belief still cannot move it here.
	if parsed.Kind == KindProfileEdit {
		return BeliefNote{}, fmt.Errorf("%q declares kind %q and cannot be promoted to %s/: %w", source, parsed.Kind, BeliefsDirName, ErrKind)
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
	if err := v.syncAncestors(ctx, destination); err != nil {
		return BeliefNote{}, fmt.Errorf("sync vault hierarchy after promoting to %q: %w", destination, err)
	}
	if err := v.unlinkUnchanged(ctx, source, sourceStat); err != nil {
		return BeliefNote{}, err
	}
	return BeliefNote{
		NoteID:      noteID,
		RelPath:     destination,
		Title:       parsed.Title,
		Content:     content,
		ContentHash: ContentHash(content),
	}, nil
}

// unlinkUnchanged removes the source of a move only if it is still the exact
// file that was read. The destination is already durable at this point, so a
// source the user changed underneath is reported rather than deleted: losing
// their edit would be worse than leaving a duplicate they can see.
func (v *Vault) unlinkUnchanged(ctx context.Context, clean string, expected unix.Stat_t) error {
	parent, leaf, err := v.openParent(ctx, clean, false)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	var current unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), leaf, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("inspect %q before removing it: %w", clean, err)
	}
	if current.Dev != expected.Dev || current.Ino != expected.Ino {
		return fmt.Errorf("%q changed while it was being promoted; the promoted copy is in place and the original was left alone", clean)
	}
	if err := unix.Unlinkat(int(parent.Fd()), leaf, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("remove %q after promoting it: %w", clean, err)
	}
	if err := v.syncDirectory(parent); err != nil {
		return fmt.Errorf("sync vault directory after removing %q: %w", clean, err)
	}
	return ctx.Err()
}

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
		return "", stat, fmt.Errorf("open %q: %w", clean, err)
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
