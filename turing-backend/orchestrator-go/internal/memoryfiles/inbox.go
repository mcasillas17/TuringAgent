package memoryfiles

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
	"golang.org/x/sys/unix"
)

// MaxCandidateBodyBytes bounds a candidate body at exactly 16 KiB. A larger
// body is refused legibly and never truncated: a claim about the user that was
// silently cut in half is a different claim.
const MaxCandidateBodyBytes = 16 * 1024

// maxTitleSlugBytes bounds the filename-visible part of a model-supplied title.
// The title is decoration on a server-generated name, never a path.
const maxTitleSlugBytes = 48

// maxTitleRunes bounds the title recorded in frontmatter.
const maxTitleRunes = 200

// NoteKind mirrors the candidate kinds the schema allows. It is not a path and
// never becomes one.
type NoteKind string

const (
	KindBelief      NoteKind = "belief"
	KindProfileEdit NoteKind = "profile_edit"
)

// Valid reports whether the kind is one this package will act on at all.
func (k NoteKind) Valid() bool {
	return k == KindBelief || k == KindProfileEdit
}

// CreateInboxNoteRequest is everything a model-produced candidate may supply.
// There is deliberately no path field: the vault names the file.
type CreateInboxNoteRequest struct {
	// NoteID is optional and is never model input. A server that must record
	// the file in a durable manifest before any byte is written mints the
	// identity itself and hands it in, so it can reserve the exact name this
	// write will use. Left empty, the vault mints one. Either way the vault
	// still names the file, from InboxNoteRelPath's single rule.
	NoteID       string
	Kind         NoteKind
	Title        string
	Body         string
	EvidenceRefs []string
}

// InboxNote is the created candidate as it exists on disk.
type InboxNote struct {
	NoteID      string
	RelPath     string
	Kind        NoteKind
	Title       string
	Content     string
	ContentHash string
}

// CreateInboxNote writes a candidate under inbox/ and nowhere else. The
// filename is a server-generated ULID plus a sanitized slug of the model's
// title, so a hostile title decorates a name instead of steering a write.
func (v *Vault) CreateInboxNote(ctx context.Context, request CreateInboxNoteRequest) (InboxNote, error) {
	if err := ctx.Err(); err != nil {
		return InboxNote{}, err
	}
	if !request.Kind.Valid() {
		return InboxNote{}, fmt.Errorf("candidate kind %q is not recognised: %w", request.Kind, ErrKind)
	}
	if strings.TrimSpace(request.Body) == "" {
		return InboxNote{}, fmt.Errorf("candidate body is empty: a candidate must state a claim")
	}
	if len(request.Body) > MaxCandidateBodyBytes {
		return InboxNote{}, &LimitError{What: "candidate body", Limit: MaxCandidateBodyBytes, Got: len(request.Body)}
	}
	noteID := request.NoteID
	if noteID == "" {
		minted, err := NewNoteID()
		if err != nil {
			return InboxNote{}, err
		}
		noteID = minted
	} else if err := validateNoteID(noteID); err != nil {
		return InboxNote{}, err
	}
	title := sanitizeTitle(request.Title)
	relPath := InboxNoteRelPath(noteID, request.Title)
	content := renderNote(noteFrontmatter{
		ID:        noteID,
		Kind:      request.Kind,
		Title:     title,
		CreatedAt: time.Now().UTC(),
		Managed:   true,
		Refs:      request.EvidenceRefs,
	}, request.Body)

	clean, err := v.createInboxNoteAt(ctx, relPath, content)
	if err != nil {
		return InboxNote{}, err
	}
	return InboxNote{
		NoteID:      noteID,
		RelPath:     clean,
		Kind:        request.Kind,
		Title:       title,
		Content:     content,
		ContentHash: ContentHash(content),
	}, nil
}

// createInboxNoteAt is the primitive's own gate and write. It re-checks inbox
// confinement for itself rather than trusting whatever computed the path, so
// this refusal stands even if a future caller reaches it by another route.
func (v *Vault) createInboxNoteAt(ctx context.Context, relPath string, content string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	clean, err := requireInboxRelPath(relPath)
	if err != nil {
		return "", err
	}
	unlock, err := v.locks.lockContext(ctx, v.pathLockKey(clean))
	if err != nil {
		return "", err
	}
	defer unlock()

	parent, leaf, err := v.openParent(ctx, clean, true)
	if err != nil {
		return "", err
	}
	defer func() { _ = parent.Close() }()

	if err := v.installStagedFile(ctx, parent, leaf, content); err != nil {
		return "", err
	}
	if err := v.syncAncestors(ctx, clean); err != nil {
		return "", fmt.Errorf("sync vault hierarchy after creating %q: %w", clean, err)
	}
	return clean, nil
}

// NewNoteID mints the stable identity a note carries in its frontmatter. It is
// a bare ULID so it is safe to embed in a filename without escaping.
func NewNoteID() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("mint note identity: %w", err)
	}
	return id.String(), nil
}

// ErrNoteIdentity refuses an identity that is not a ULID this package minted
// the shape of. It is separate from ErrConfinement because a rejected identity
// never became a path in the first place.
var ErrNoteIdentity = errors.New("note identity is not a ULID")

// validateNoteID is the gate on a caller-supplied identity. Only a
// canonical 26-character Crockford base32 ULID passes, so nothing
// path-shaped, empty or merely decorative can reach a filename. It is the
// same shape NewNoteID emits, checked rather than assumed.
func validateNoteID(noteID string) error {
	parsed, err := ulid.ParseStrict(noteID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoteIdentity, err)
	}
	if parsed.String() != noteID {
		return fmt.Errorf("%w: identity is not in its canonical form", ErrNoteIdentity)
	}
	return nil
}

// InboxNoteRelPath is the one rule that names a candidate file. CreateInboxNote
// writes exactly here, so a caller holding the identity can compute the path a
// write will use — and reserve it — before the write happens. Keeping planning
// and writing on the same function is what makes the two provably agree.
func InboxNoteRelPath(noteID string, title string) string {
	return InboxDirName + "/" + noteFileName(noteID, sanitizeTitle(title))
}

// NoteIDFromInboxRelPath reads back the identity InboxNoteRelPath put into a
// name, and answers "" for anything it did not produce.
//
// It exists so a caller holding a stored inbox path can ask whether a file it
// found elsewhere in the vault is the same note — the frontmatter identity
// travels with the file when the user moves it, the path does not. It is a
// question about identity only: nothing here turns the answer back into a path.
func NoteIDFromInboxRelPath(relPath string) string {
	if !strings.HasPrefix(relPath, InboxDirName+"/") {
		return ""
	}
	name := strings.TrimPrefix(relPath, InboxDirName+"/")
	if strings.Contains(name, "/") || !strings.HasSuffix(name, ".md") {
		return ""
	}
	name = strings.TrimSuffix(name, ".md")
	if index := strings.Index(name, "-"); index >= 0 {
		name = name[:index]
	}
	if validateNoteID(name) != nil {
		return ""
	}
	return name
}

// ContentHash is the compare-and-set token used across this package.
func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// requireDecidedBytes is the compare-and-set every decision primitive makes
// against its own read, immediately before it mutates.
//
// It is one function rather than three copies so the rule reads the same at
// every door: a decision that names bytes is held to them, a door that requires
// naming them refuses a decision that does not, and nothing is ever applied to
// whatever happens to be on disk.
//
// The repository above this makes the same check earlier, and that one is for
// the user's benefit — it turns "the file moved" into a decision-shaped refusal
// before anything is claimed. It cannot be the authority: its read takes the
// path lock and gives it straight back, and the primitive takes it again. This
// check is inside the lock the mutation happens under.
func requireDecidedBytes(relPath string, required bool, expectedHash string, content string) error {
	if expectedHash == "" {
		if required {
			return fmt.Errorf("%q was not named by the decision acting on it: %w", relPath, ErrUnboundDecision)
		}
		return nil
	}
	if ContentHash(content) != expectedHash {
		return &StaleContentError{RelPath: relPath}
	}
	return nil
}

func noteFileName(noteID string, title string) string {
	slug := titleSlug(title)
	if slug == "" {
		return noteID + ".md"
	}
	return noteID + "-" + slug + ".md"
}

// titleSlug reduces a model-supplied title to lowercase ASCII words joined by
// single hyphens. Separators, traversal, NUL bytes and every other path-shaped
// character collapse to a hyphen, so no title can add a component.
func titleSlug(title string) string {
	var builder strings.Builder
	previousHyphen := false
	for _, symbol := range title {
		switch {
		case symbol >= 'a' && symbol <= 'z', symbol >= '0' && symbol <= '9':
			builder.WriteRune(symbol)
			previousHyphen = false
		case symbol >= 'A' && symbol <= 'Z':
			builder.WriteRune(unicode.ToLower(symbol))
			previousHyphen = false
		default:
			if !previousHyphen && builder.Len() > 0 {
				builder.WriteByte('-')
				previousHyphen = true
			}
		}
		if builder.Len() >= maxTitleSlugBytes {
			break
		}
	}
	return strings.Trim(builder.String(), "-")
}

// sanitizeTitle keeps the human-readable title but strips the control
// characters that would corrupt frontmatter, and bounds its length.
func sanitizeTitle(title string) string {
	var builder strings.Builder
	runes := 0
	for _, symbol := range title {
		if symbol == utf8.RuneError || symbol < 0x20 || symbol == 0x7f {
			continue
		}
		builder.WriteRune(symbol)
		runes++
		if runes >= maxTitleRunes {
			break
		}
	}
	return strings.TrimSpace(builder.String())
}

// InboxNoteContent is one candidate file exactly as it stands on disk right
// now: the whole bytes, a hash of exactly those bytes, and what the frontmatter
// says about them.
//
// It is what a decision is verified against. The database row records what
// Turing wrote; this records what the user is looking at, and when the two
// disagree the file is the one that counts — the user may have opened the
// proposal in Obsidian and rewritten the claim before deciding on it.
type InboxNoteContent struct {
	RelPath     string
	Kind        NoteKind
	Title       string
	Content     string
	ContentHash string
	Body        string
}

// ReadInboxNote reads one candidate under inbox/ and nowhere else.
//
// It takes and releases the path lock for the read alone, so a caller may take
// it again for the write that follows without deadlocking against itself. The
// serialisation a decision needs is per-candidate and belongs to its caller;
// this is only the read.
func (v *Vault) ReadInboxNote(ctx context.Context, relPath string) (InboxNoteContent, error) {
	if err := ctx.Err(); err != nil {
		return InboxNoteContent{}, err
	}
	clean, err := requireInboxRelPath(relPath)
	if err != nil {
		return InboxNoteContent{}, err
	}
	unlock, err := v.locks.lockContext(ctx, v.pathLockKey(clean))
	if err != nil {
		return InboxNoteContent{}, err
	}
	defer unlock()

	content, _, err := v.readConfinedFile(ctx, clean, MaxNoteBytes)
	if err != nil {
		return InboxNoteContent{}, err
	}
	parsed, err := ParseNote(clean, content)
	if err != nil {
		return InboxNoteContent{}, err
	}
	return InboxNoteContent{
		RelPath:     clean,
		Kind:        parsed.Kind,
		Title:       parsed.Title,
		Content:     content,
		ContentHash: ContentHash(content),
		Body:        parsed.Body,
	}, nil
}

// InboxRemovalMode names why a candidate file is being deleted, and with it
// whether the deletion has to say which bytes it is deleting.
//
// There is one door that deletes text a user decided about, and it is held to
// naming that text. The other two exist because in their cases no such name can
// exist, and both have to be asked for explicitly: an unstated mode is the
// strict door, so a caller that forgets is refused rather than quietly given
// the permissive one.
type InboxRemovalMode string

const (
	// RemoveDecidedCandidate is the user saying no to a proposal they read.
	// ExpectedContentHash is required and is checked against this primitive's
	// own read, under its own lock, immediately before the unlink.
	RemoveDecidedCandidate InboxRemovalMode = "decided_candidate"

	// RemoveUnreadableCandidate is the user throwing away a proposal whose
	// frontmatter nobody could read. There is no hash to name, because nothing
	// could parse the file to produce one, and refusing the deletion would
	// leave them with a file they can neither accept nor be rid of.
	RemoveUnreadableCandidate InboxRemovalMode = "unreadable_candidate"

	// RemoveRetiredCandidate is Turing's own tidying: bytes whose outcome is
	// already recorded elsewhere — an applied profile edit, a candidate no row
	// will ever describe, a file the session cleaner's manifest names. It is
	// idempotent and hashless because it is not a decision about text; the
	// decision already happened, and leaving the file behind would be the
	// failure. It is deliberately not reachable as a user's rejection.
	RemoveRetiredCandidate InboxRemovalMode = "retired_candidate"
)

// RemoveInboxNoteRequest is one candidate deletion. Mode defaults to
// RemoveDecidedCandidate, so the strict door is the one a caller gets by saying
// nothing and the hashless ones have to be named.
type RemoveInboxNoteRequest struct {
	RelPath             string
	Mode                InboxRemovalMode
	ExpectedContentHash string
}

// RemoveInboxNote is the only deletion primitive in this package. It removes a
// candidate under inbox/ and refuses everything else, so the rejection RPC and
// the vault cleaner that call it cannot be pointed at a belief, at persona.md,
// or at anything outside the vault.
//
// A missing target is not an error, whatever the mode: cleanup has to be
// idempotent because the caller may be retrying after a crash that already did
// the work, and a decided rejection whose file has already gone has nothing
// left to protect.
//
// A decided rejection that finds the file still there reads it under this
// primitive's own lock and compares it to the hash the decision named, then
// unlinks the inode whose bytes it just compared. The caller's earlier read
// released the lock this one holds; without the check here, a proposal the user
// rewrote between the two would be deleted as though they had read it.
func (v *Vault) RemoveInboxNote(ctx context.Context, request RemoveInboxNoteRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clean, err := requireInboxRelPath(request.RelPath)
	if err != nil {
		return err
	}
	mode := request.Mode
	if mode == "" {
		mode = RemoveDecidedCandidate
	}
	switch mode {
	case RemoveDecidedCandidate:
		if request.ExpectedContentHash == "" {
			return fmt.Errorf(
				"a rejection of %q must name the bytes it deletes; a proposal that cannot be read at all is removed with mode %q: %w",
				clean, RemoveUnreadableCandidate, ErrUnboundDecision,
			)
		}
	case RemoveUnreadableCandidate, RemoveRetiredCandidate:
	default:
		return fmt.Errorf("inbox removal mode %q is not recognised: %w", request.Mode, ErrUnboundDecision)
	}

	unlock, err := v.locks.lockContext(ctx, v.pathLockKey(clean))
	if err != nil {
		return err
	}
	defer unlock()

	parent, leaf, err := v.openParent(ctx, clean, false)
	if err != nil {
		if errors.Is(err, unix.ENOENT) || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer func() { _ = parent.Close() }()

	var stat unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), leaf, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("inspect %q: %w", clean, err)
	}
	if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
		return confinementError(clean, "candidate is a symlink; Turing never removes through a link")
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return confinementError(clean, "candidate is not a regular file")
	}
	if mode == RemoveDecidedCandidate {
		if err := v.requireDecidedInboxEntry(ctx, parent, leaf, clean, request.ExpectedContentHash); err != nil {
			return err
		}
	}
	if err := unix.Unlinkat(int(parent.Fd()), leaf, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("remove %q: %w", clean, err)
	}
	if err := v.syncDirectory(parent); err != nil {
		return fmt.Errorf("sync vault directory after removing %q: %w", clean, err)
	}
	return ctx.Err()
}

// requireDecidedInboxEntry reads the candidate through the parent descriptor
// the unlink will use and holds it to the hash the decision named.
//
// A file that has gone in the meantime is not a refusal, for the same reason a
// missing target never is: the user asked for it not to be there.
func (v *Vault) requireDecidedInboxEntry(
	ctx context.Context,
	parent *os.File,
	leaf string,
	clean string,
	expectedHash string,
) error {
	content, _, err := v.readConfinedEntry(ctx, parent, leaf, clean, MaxNoteBytes)
	if err != nil {
		if errors.Is(err, unix.ENOENT) || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return requireDecidedBytes(clean, true, expectedHash, content)
}
