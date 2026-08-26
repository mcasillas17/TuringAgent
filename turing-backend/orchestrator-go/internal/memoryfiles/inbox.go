package memoryfiles

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
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
	if strings.Contains(name, "/") {
		return ""
	}
	return NoteIDFromFileName(name)
}

// NoteIDFromFileName reads the identity out of a name this server minted,
// wherever in the vault that name has since ended up, and answers "" for any
// name it did not mint.
//
// It is the one correlation available when a file cannot be read at all. A
// note's frontmatter identity is the link that survives a move, but a file
// whose frontmatter will not parse — or that is too long to read — has no
// frontmatter anyone can consult, and the caller still has to decide whether a
// row naming a missing inbox entry is stale. The minted prefix of the name is
// the only thing left that this server, rather than the file's contents, is the
// author of.
//
// It answers about the name and nothing else. A name is not proof of what is
// inside the file, which is why the one caller uses it to *retain* lifecycle
// state and never to consume any.
func NoteIDFromFileName(name string) string {
	if !strings.HasSuffix(name, ".md") {
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
	if mode == RemoveRetiredCandidate {
		// Turing's own tidying, and deliberately the plain unlink. The outcome
		// it follows is already recorded elsewhere, there is no decision to
		// bind it to, and leaving the file behind is the failure it exists to
		// prevent. It is unreachable as a user's rejection, and it is still
		// confined: every path above this refuses anything outside inbox/.
		if err := unix.Unlinkat(int(parent.Fd()), leaf, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("remove %q: %w", clean, err)
		}
		if err := v.syncDirectory(parent); err != nil {
			return fmt.Errorf("sync vault directory after removing %q: %w", clean, err)
		}
		return ctx.Err()
	}
	return v.removeRejectedInboxEntry(ctx, parent, leaf, clean, stat, request.ExpectedContentHash)
}

// removeRejectedInboxEntry is the user's own rejection, and it deletes the file
// they were looking at or it deletes nothing.
//
// An unlink names a name. Between the read that checked the candidate and the
// unlink that removes it, the entry under that name can become a different
// file: Obsidian, a sync client and Turing's own writer all replace a file by
// writing a new one beside it and renaming over the top, so the window is the
// ordinary way this vault gets written to, not an exotic race. Removing what is
// there at unlink time means a rejection can delete a claim about the user that
// nobody has ever read — the exact opposite of what the decision said.
//
// So the deletion is two steps rather than one. The candidate is opened first,
// and that descriptor is the identity everything afterwards is checked against.
// Then the entry is detached from its name in one atomic rename into a private
// staging name inside the same confined directory — the same link-and-stage
// discipline every create here uses, run backwards. What was detached is then
// compared against the opened descriptor, and against the decision's bytes
// where there are any, before anything is unlinked.
//
// If it is not the same file, nothing is deleted: the detached entry is put
// back under its own name with link semantics that refuse to overwrite, and the
// refusal tells the caller to look again. If the name has been taken in the
// meantime the file stays under the staging name and the refusal says where it
// is, because an unreferenced file somebody can still find is recoverable and a
// deleted one is not.
func (v *Vault) removeRejectedInboxEntry(
	ctx context.Context,
	parent *os.File,
	leaf string,
	clean string,
	named unix.Stat_t,
	expectedHash string,
) error {
	opened, openedStat, err := openConfinedEntry(parent, leaf, clean)
	switch {
	case err == nil:
		defer func() { _ = opened.Close() }()
	case errors.Is(err, unix.ENOENT), errors.Is(err, os.ErrNotExist):
		// The user asked for this file not to be there, and it is not there.
		return nil
	case expectedHash == "" && (errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM)):
		// A file the user cannot even open is exactly the file this mode
		// exists for, and refusing here would leave them with a claim about
		// themselves they can neither read, accept nor be rid of. The identity
		// falls back to the entry that was just inspected under the same lock:
		// weaker than a descriptor, and still enough for the one question the
		// detach below asks — is what I am deleting what I looked at?
		openedStat = named
	default:
		// Anything else — a symlink that appeared under the name, an entry
		// that is no longer a regular file — is the barrier doing its job.
		return err
	}

	if expectedHash != "" {
		content, readErr := readEntryContent(ctx, opened, clean)
		if readErr != nil {
			return readErr
		}
		if err := requireDecidedBytes(clean, true, expectedHash, content); err != nil {
			return err
		}
	}
	v.detachBarrier(detachPhaseBeforeDetach, clean)
	if err := ctx.Err(); err != nil {
		return err
	}

	staging, err := reserveStagingName(parent)
	if err != nil {
		return fmt.Errorf("stage the removal of %q: %w", clean, err)
	}
	if err := unix.Renameat(int(parent.Fd()), leaf, int(parent.Fd()), staging); err != nil {
		_ = unix.Unlinkat(int(parent.Fd()), staging, 0)
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("detach %q before removing it: %w", clean, err)
	}

	v.detachBarrier(detachPhaseBeforeVerify, clean)
	detached := ""
	var detachedStat unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), staging, &detachedStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		detached = fmt.Sprintf("the detached entry could not be inspected (%v)", err)
	} else if detachedStat.Dev != openedStat.Dev || detachedStat.Ino != openedStat.Ino {
		detached = "another file had taken its name"
	} else if expectedHash != "" && opened != nil {
		// Same inode, and that is not the same thing as the same bytes: an
		// editor saving in place keeps the inode, and those are the user's own
		// newer words. They are held to the decision like everything else.
		content, readErr := rereadEntryContent(ctx, opened, clean)
		if readErr != nil {
			detached = fmt.Sprintf("it could not be read again before removal (%v)", readErr)
		} else if ContentHash(content) != expectedHash {
			detached = "it was rewritten in place"
		}
	}
	if detached != "" {
		return v.restoreDetachedEntry(ctx, parent, leaf, clean, staging, detached)
	}

	if err := unix.Unlinkat(int(parent.Fd()), staging, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		// The identity check passed, so these are the bytes the user asked to
		// be rid of — and the unlink that would have done it failed. The
		// rejection did not happen, and the sentence says where the file
		// actually is rather than leaving a detached entry unaccounted for.
		_ = v.syncDirectory(parent)
		return fmt.Errorf(
			"remove the detached %q: %w; it is at %s/%s",
			clean, err, path.Dir(clean), staging,
		)
	}
	if err := v.syncDirectory(parent); err != nil {
		return fmt.Errorf("sync vault directory after removing %q: %w", clean, err)
	}
	return ctx.Err()
}

// restoreDetachedEntry puts back a file this deletion detached and then found
// it had no standing to remove, and reports the refusal either way.
//
// The link refuses to overwrite, which is the whole point: whatever now holds
// the name is another writer's file and this one may not clobber it. Nothing
// here deletes anything.
//
// Where the name cannot be taken back the bytes still have to go somewhere a
// person will find them, and the private staging name is not that place: it
// begins with a dot, the vault walk skips dot entries, and a claim about the
// user that is on disk and on no page is indistinguishable from a deleted one
// to everybody except a forensic reader. So the file is linked under a name
// this server mints and the walk indexes — visible in the next scan, on the
// next ListMemoryState, and deletable by the user like any other file in their
// inbox. Only when that fails too does it stay staged, and then the refusal
// says exactly where it is.
func (v *Vault) restoreDetachedEntry(
	ctx context.Context,
	parent *os.File,
	leaf string,
	clean string,
	staging string,
	reason string,
) error {
	v.detachBarrier(detachPhaseBeforeRestore, clean)
	linkErr := v.linkDetached(parent, staging, leaf)
	if linkErr != nil {
		return v.refuseAndPreserve(parent, clean, staging, reason+" and could not be put back under its own name", linkErr)
	}
	if err := unix.Unlinkat(int(parent.Fd()), staging, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		// The file is back where it belongs; a second link to it under a name
		// nothing shows is not. Making that copy visible is the same rule as
		// above — a duplicate the user can see and delete beats one they
		// cannot.
		return v.refuseAndPreserve(parent, clean, staging, reason+", so it was left alone, but a second link to it could not be dropped", err)
	}
	_ = v.syncDirectory(parent)
	// The vault is exactly as it was, so a request that ran out of time or
	// was cancelled says that rather than making a claim about the file.
	// "It changed since you read it" is the wrong sentence when nothing
	// changed and nobody finished looking.
	if err := ctx.Err(); err != nil {
		return err
	}
	return &StaleContentError{RelPath: clean, Detail: boundRefusalDetail(reason + ", so it was left alone")}
}

// refuseAndPreserve is the one exit for a detached file that could not go back
// under its own name. It never unlinks: it moves the bytes somewhere visible if
// it can, and says where they are if it cannot.
func (v *Vault) refuseAndPreserve(
	parent *os.File,
	clean string,
	staging string,
	reason string,
	cause error,
) error {
	directory := path.Dir(clean)
	recovery, rescueErr := v.rescueDetachedEntry(parent, staging)
	if recovery == "" {
		return &StaleContentError{
			RelPath: clean,
			Cause:   cause,
			Detail: boundRefusalDetail(fmt.Sprintf(
				"%s (%v), and no recovery name could be taken for it (%v); it is not lost — it is at %s/%s",
				reason, cause, rescueErr, directory, staging,
			)),
		}
	}
	if rescueErr != nil {
		// The visible name exists and holds the bytes; only the tidying after
		// it failed. Both names are reported, because both are on disk.
		return &StaleContentError{
			RelPath: clean,
			Cause:   cause,
			Detail: boundRefusalDetail(fmt.Sprintf(
				"%s (%v); it is not lost — it was kept for recovery at %s/%s, and a copy remains at %s/%s (%v)",
				reason, cause, directory, recovery, directory, staging, rescueErr,
			)),
		}
	}
	return &StaleContentError{
		RelPath: clean,
		Cause:   cause,
		Detail: boundRefusalDetail(fmt.Sprintf(
			"%s (%v); it is not lost — it was kept for recovery at %s/%s",
			reason, cause, directory, recovery,
		)),
	}
}

// rescueDetachedEntry moves bytes this deletion is holding out of the private
// staging name and under a visible one in the same confined directory.
//
// The name is minted rather than derived from anything in the file: a rescued
// entry may be a claim about the user nobody has read, and a name built from
// its contents would publish some of it into a directory listing. The link
// refuses to overwrite and a taken name is retried, so a rescue can never
// clobber another rescue or anything else the user has.
//
// The order is what makes it durable: link, flush, then drop the staging name,
// then flush again. A crash anywhere in it leaves the bytes reachable under at
// least one name, which is the property the whole path exists for.
func (v *Vault) rescueDetachedEntry(parent *os.File, staging string) (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		name, err := RecoveryDraftFileName()
		if err != nil {
			return "", err
		}
		linkErr := v.linkDetached(parent, staging, name)
		if errors.Is(linkErr, unix.EEXIST) {
			continue
		}
		if linkErr != nil {
			return "", linkErr
		}
		if err := v.syncDirectory(parent); err != nil {
			return name, err
		}
		if err := unix.Unlinkat(int(parent.Fd()), staging, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return name, err
		}
		if err := v.syncDirectory(parent); err != nil {
			return name, err
		}
		return name, nil
	}
	return "", errors.New("could not allocate a recovery name")
}

// RecoveryDraftFileName mints the visible name a rescued file is kept under.
//
// It is a ULID, so two rescues in one inbox cannot collide, and it carries the
// note extension so the vault walk indexes it rather than stepping over it. The
// word in front is what stops it reading back as a proposal this server wrote:
// the identity a name carries is the segment before the first hyphen, and
// "recovered" is not a ULID.
func RecoveryDraftFileName() (string, error) {
	identity, err := NewNoteID()
	if err != nil {
		return "", err
	}
	return recoveryDraftPrefix + identity + noteFileExtension, nil
}

// IsRecoveryDraftName reports whether a name is one a rescue minted. It answers
// about the name only: what is inside such a file is whatever was detached, and
// this says nothing about it.
func IsRecoveryDraftName(name string) bool {
	if !strings.HasPrefix(name, recoveryDraftPrefix) || !strings.HasSuffix(name, noteFileExtension) {
		return false
	}
	identity := strings.TrimSuffix(strings.TrimPrefix(name, recoveryDraftPrefix), noteFileExtension)
	return validateNoteID(identity) == nil
}

// maxRefusalDetailBytes bounds the sentence a refused rejection carries back.
//
// The detail is assembled from constants, path components and the text of
// whatever the filesystem said, and only the first two of those have a length
// this package controls. A refusal is logged and handed to a caller, so it is
// clipped to something a log line can hold rather than trusted to stay short.
const maxRefusalDetailBytes = 512

// boundRefusalDetail clips a refusal to that bound on a rune boundary, so what
// is written down is still text.
func boundRefusalDetail(detail string) string {
	if len(detail) <= maxRefusalDetailBytes {
		return detail
	}
	cut := maxRefusalDetailBytes
	for cut > 0 && !utf8.RuneStart(detail[cut]) {
		cut--
	}
	return detail[:cut] + "…"
}

// openConfinedEntry opens one entry through a parent descriptor without
// following any link, and answers with the identity of the inode that was
// opened rather than of the name that opened it. That descriptor is what a
// deletion checks itself against afterwards.
func openConfinedEntry(parent *os.File, leaf string, clean string) (*os.File, unix.Stat_t, error) {
	var opened unix.Stat_t
	fd, err := unix.Openat(
		int(parent.Fd()),
		leaf,
		unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, opened, fmt.Errorf("open %q: %w", clean, err)
	}
	file := os.NewFile(uintptr(fd), clean)
	if file == nil {
		_ = unix.Close(fd)
		return nil, opened, fmt.Errorf("open %q: invalid descriptor", clean)
	}
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = file.Close()
		return nil, opened, fmt.Errorf("inspect open %q: %w", clean, err)
	}
	if opened.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = file.Close()
		return nil, opened, confinementError(clean, "candidate is not a regular file")
	}
	return file, opened, nil
}

// readEntryContent reads an already-open candidate under the same bound every
// other reader here uses.
func readEntryContent(ctx context.Context, file *os.File, clean string) (string, error) {
	content, err := readBounded(ctx, file, MaxNoteBytes)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", clean, err)
	}
	if len(content) > MaxNoteBytes {
		return "", &LimitError{What: fmt.Sprintf("note %q", clean), Limit: MaxNoteBytes, Got: len(content)}
	}
	return string(content), nil
}

// rereadEntryContent reads the same descriptor again from the start. It is the
// same inode by construction — a descriptor cannot be re-pointed — which is why
// this can answer whether the bytes moved without asking the directory anything.
func rereadEntryContent(ctx context.Context, file *os.File, clean string) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("re-read %q: %w", clean, err)
	}
	return readEntryContent(ctx, file, clean)
}

// reserveStagingName takes a random private name inside the candidate's own
// directory, exclusively, so the detach below has somewhere to move the file
// that nothing else can be holding. The name is created rather than merely
// generated: O_EXCL is what makes "unused" a fact instead of a probability.
func reserveStagingName(parent *os.File) (string, error) {
	staging, name, err := createStagingFile(parent, 0o600)
	if err != nil {
		return "", err
	}
	if err := staging.Close(); err != nil {
		_ = unix.Unlinkat(int(parent.Fd()), name, 0)
		return "", err
	}
	return name, nil
}
