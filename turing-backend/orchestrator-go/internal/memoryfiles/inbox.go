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

	if err := v.installStagedFile(ctx, parent, leaf, clean, content); err != nil {
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

// UnreadableCandidateEntry names the exact inbox entry a decision's pre-check
// failed to read, and it is the only thing a hashless rejection may act on.
//
// The hashless door exists because a proposal nobody can parse has no bytes to
// name, and refusing to delete it would leave the user with a claim about
// themselves they can neither accept nor be rid of. What it must not become is
// a licence to delete whatever happens to be under the name later: the
// pre-check releases the vault's path lock before the primitive takes it, and
// an editor, a sync client or Turing's own writer can put a different file
// there in between. So the pre-check says which entry it failed on, and the
// primitive deletes that entry or nothing.
//
// Every field is unexported and there is no constructor: the only way to obtain
// one is ReadInboxCandidate actually failing to read a real file, so no caller
// can forge one. It is also never anything a client sees. It carries an inode
// number and a hash of bytes nobody has read, and a hash handed out is a token
// a caller could send back — which is exactly the binding this type exists to
// keep on the server.
type UnreadableCandidateEntry struct {
	// bound distinguishes "a pre-check answered about this entry" from the
	// zero value, which authorises nothing.
	bound bool
	// present records whether there was an entry at all. A pre-check that
	// found nothing may not delete a file that arrived afterwards.
	present bool
	device  uint64
	inode   uint64
	// rawHash is the hash of the raw bytes, set only when they could be read
	// at all. A file nobody can open and a file past the size bound reach this
	// door with no bytes to hash, and are held to their identity alone.
	rawHash string
	hashed  bool
	// failure is the broad way the pre-check's read failed, and it is the
	// second half of what an unhashed binding is bound to. Identity says this
	// is the same entry; this says it is still in the state the pre-check
	// answered about. It is meaningless when hashed is set, because then the
	// binding is to bytes.
	failure unreadableFailure
}

// unreadableFailure names the broad way a candidate refused to be read.
//
// A hashless rejection with no bytes behind it is a licence to delete a file
// nobody has read, and it is granted because the alternative is leaving the
// user stuck with a claim about themselves they can neither read nor be rid
// of. What makes it safe is that the file goes on refusing to be read in the
// same broad way — that refusal, repeated under the primitive's own lock, is
// the whole of the evidence that these are still the bytes nobody saw. Coarse
// on purpose: EACCES and EPERM are one answer, and no distinction finer than
// this is one a user could act on.
type unreadableFailure uint8

const (
	// unreadableFailureNone is a read that did not fail, and authorises
	// nothing.
	unreadableFailureNone unreadableFailure = iota
	// unreadableFailureUnopenable is an entry nothing could open.
	unreadableFailureUnopenable
	// unreadableFailureOverLimit is an entry that opened and is past the bound
	// every reader here works under.
	unreadableFailureOverLimit
	// unreadableFailureUnreadable is an entry that opened and whose bytes
	// could not be read anyway.
	unreadableFailureUnreadable
)

// classifyUnreadableFailure sorts a failed read into the class an unhashed
// binding is held to.
func classifyUnreadableFailure(err error) unreadableFailure {
	var unopenable *unopenableEntryError
	switch {
	case err == nil:
		return unreadableFailureNone
	case errors.Is(err, ErrTooLarge):
		return unreadableFailureOverLimit
	case errors.As(err, &unopenable):
		return unreadableFailureUnopenable
	default:
		return unreadableFailureUnreadable
	}
}

// Bound reports whether this identity came from a pre-check that really failed
// to read a candidate. It says nothing about the file itself.
func (e UnreadableCandidateEntry) Bound() bool { return e.bound }

// InboxCandidateReading is what a decision's pre-check comes away with.
//
// Exactly one half is meaningful. Note holds the proposal when the file read
// and parsed; Unreadable names the entry when it did not, and ReadErr says why.
// A candidate that reads is decided about by its bytes; one that does not is
// decided about by its identity, and nothing else is a decision at all.
type InboxCandidateReading struct {
	Note       InboxNoteContent
	Readable   bool
	Unreadable UnreadableCandidateEntry
	ReadErr    error
}

// ReadInboxCandidate reads one candidate under inbox/ and nowhere else, and
// answers for it either way.
//
// It takes and releases the path lock for the read alone, so a caller may take
// it again for the write that follows without deadlocking against itself. The
// serialisation a decision needs is per-candidate and belongs to its caller;
// this is only the read.
//
// A file that cannot be read is not an error here — it is an answer, and the
// answer names the entry it is about. What stays an error is everything that is
// not a candidate at all: a path outside the inbox, a symlink, an entry that is
// not a regular file. None of those is a proposal, and none of them acquires a
// way out of the vault by being unreadable.
func (v *Vault) ReadInboxCandidate(ctx context.Context, relPath string) (InboxCandidateReading, error) {
	if err := ctx.Err(); err != nil {
		return InboxCandidateReading{}, err
	}
	clean, err := requireInboxRelPath(relPath)
	if err != nil {
		return InboxCandidateReading{}, err
	}
	unlock, err := v.locks.lockContext(ctx, v.pathLockKey(clean))
	if err != nil {
		return InboxCandidateReading{}, err
	}
	defer unlock()

	content, stat, readErr := v.readConfinedFile(ctx, clean, MaxNoteBytes)
	if readErr == nil {
		parsed, parseErr := ParseNote(clean, content)
		if parseErr == nil {
			return InboxCandidateReading{
				Readable: true,
				Note: InboxNoteContent{
					RelPath:     clean,
					Kind:        parsed.Kind,
					Title:       parsed.Title,
					Content:     content,
					ContentHash: ContentHash(content),
					Body:        parsed.Body,
				},
			}, nil
		}
		// The bytes are there and they are not a note. This is the case the
		// hashless door exists for, and it is the one case where it can be
		// named exactly: identity *and* a hash of what would not parse.
		return InboxCandidateReading{
			Unreadable: UnreadableCandidateEntry{
				bound:   true,
				present: true,
				device:  uint64(stat.Dev),
				inode:   uint64(stat.Ino),
				rawHash: ContentHash(content),
				hashed:  true,
			},
			ReadErr: parseErr,
		}, nil
	}
	switch {
	case errors.Is(readErr, unix.ENOENT), errors.Is(readErr, os.ErrNotExist):
		return InboxCandidateReading{
			Unreadable: UnreadableCandidateEntry{bound: true},
			ReadErr:    readErr,
		}, nil
	case ctx.Err() != nil, errors.Is(readErr, ErrConfinement), stat.Ino == 0:
		// Not a candidate, or nothing was inspected at all. There is no entry
		// to bind a removal to, so this stays the caller's failure.
		return InboxCandidateReading{}, readErr
	default:
		// Unopenable, or past the size bound: the bytes cannot be hashed, and
		// the entry that was inspected is the whole identity there is. The way
		// it refused to be read is carried with it, because that refusal
		// repeating is the only evidence the primitive will have that these
		// are still the bytes nobody has seen.
		return InboxCandidateReading{
			Unreadable: UnreadableCandidateEntry{
				bound:   true,
				present: true,
				device:  uint64(stat.Dev),
				inode:   uint64(stat.Ino),
				failure: classifyUnreadableFailure(readErr),
			},
			ReadErr: readErr,
		}, nil
	}
}

// ReadInboxNote is the same read for callers that only want a proposal they can
// show: a candidate that will not read comes back as the failure to read it.
func (v *Vault) ReadInboxNote(ctx context.Context, relPath string) (InboxNoteContent, error) {
	reading, err := v.ReadInboxCandidate(ctx, relPath)
	if err != nil {
		return InboxNoteContent{}, err
	}
	if !reading.Readable {
		return InboxNoteContent{}, reading.ReadErr
	}
	return reading.Note, nil
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
	// leave them with a file they can neither accept nor be rid of. What it is
	// bound to instead is the identity of the exact entry the pre-check failed
	// on, which Unreadable carries and which nothing else can produce.
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
	// Unreadable is the entry a RemoveUnreadableCandidate removal is bound to,
	// and that mode requires it. It comes from ReadInboxCandidate and nowhere
	// else, so a hashless deletion is still a deletion of something somebody
	// actually looked at — and it never crosses a wire, because the only thing
	// inside it is the identity and the bytes of a proposal nobody has read.
	Unreadable UnreadableCandidateEntry
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
	case RemoveUnreadableCandidate:
		if !request.Unreadable.bound {
			return fmt.Errorf(
				"a hashless rejection of %q must name the entry it deletes, and only a pre-check that failed to read one can: %w",
				clean, ErrUnboundDecision,
			)
		}
	case RemoveRetiredCandidate:
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
	return v.removeRejectedInboxEntry(ctx, parent, leaf, clean, stat, request)
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
//
// A request that ends while the file is between names goes down that same road.
// Cancellation is not a verdict about the bytes, and a caller that has gone is
// nobody to delete a claim about the user on behalf of, so the entry is put back
// — or kept under a name the user can see — before the context error is what
// this returns.
//
// A hashless rejection has no decision bytes, so what it is held to instead is
// the identity its pre-check named: same entry, and still unreadable. Both are
// checked before anything is detached, because a proposal the user repaired in
// their editor is one they can now read and decide about properly, and a file
// that merely took the name is somebody else's entirely.
func (v *Vault) removeRejectedInboxEntry(
	ctx context.Context,
	parent *os.File,
	leaf string,
	clean string,
	named unix.Stat_t,
	request RemoveInboxNoteRequest,
) error {
	expectedHash := request.ExpectedContentHash
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
		// weaker than a descriptor, and enough only where identity was the
		// whole binding to begin with. Where the pre-check managed to hash the
		// bytes, the check below refuses instead — a hash that cannot be
		// compared is not answered by an inode number.
		openedStat = named
	default:
		// Anything else — a symlink that appeared under the name, an entry
		// that is no longer a regular file — is the barrier doing its job.
		return err
	}

	// boundHash is the bytes this removal is entitled to delete, whichever door
	// it came through: the decision's own hash, or the hash of what the
	// pre-check could not parse. It is empty only when nobody could read the
	// file at all, and then identity is all there is.
	boundHash := expectedHash
	boundFailure := unreadableFailureNone
	if expectedHash != "" {
		content, readErr := readEntryContent(ctx, opened, clean, MaxNoteBytes)
		if readErr != nil {
			return readErr
		}
		if err := requireDecidedBytes(clean, true, expectedHash, content); err != nil {
			return err
		}
	} else {
		if err := requireBoundUnreadableEntry(ctx, clean, request.Unreadable, opened, openedStat); err != nil {
			return err
		}
		if request.Unreadable.hashed {
			boundHash = request.Unreadable.rawHash
		} else {
			// No bytes were ever read, so what crosses the detach is the way
			// the file refused to be read. The far side asks the same question
			// the near side did.
			boundFailure = request.Unreadable.failure
		}
	}
	detached, err := v.detachEntry(ctx, parent, leaf, clean)
	if err != nil {
		return err
	}
	if detached == nil {
		// The user asked for this file not to be there, and it is not there.
		return nil
	}
	// The file is off its name and nothing has decided yet whether it may go.
	// A request that has ended here is not a decision, so there is nothing left
	// to authorise an unlink: the bytes go back under their own name first, and
	// the cancellation is reported only once they are somewhere the user can
	// see them again.
	//
	// This has to be the primitive's own check rather than a side effect of the
	// re-read below. A hashless removal — the one door out for a proposal
	// nobody can read — never re-reads anything, so a cancelled request would
	// otherwise walk past this point straight into the unlink and delete a
	// claim about the user on behalf of a caller that had already gone.
	if err := ctx.Err(); err != nil {
		return v.refuseDetachedRejection(ctx, detached, endedBeforeVerification)
	}
	objection := detached.objectionToDetachedEntry(ctx, boundHash, boundFailure, opened, openedStat)
	if objection != "" {
		return v.refuseDetachedRejection(ctx, detached, objection)
	}

	if err := detached.discard(); err != nil {
		// The identity check passed, so these are the bytes the user asked to
		// be rid of — and the unlink that would have done it failed. The
		// rejection did not happen, and the sentence says where the file
		// actually is rather than leaving a detached entry unaccounted for.
		return err
	}
	return ctx.Err()
}

// requireBoundUnreadableEntry holds a hashless rejection to the entry its
// pre-check actually failed to read, and refuses it otherwise.
//
// Three things can have happened in the window the pre-check opened by giving
// the path lock back, and none of them is the rejection the user asked for.
// Another file can have taken the name — a replacement is a new inode, so
// identity catches it, whether the replacement parses or is broken in some new
// way of its own. The bytes can have been rewritten under the same inode, which
// is what an editor saving in place does, and the hash of what would not parse
// catches that. And the user can simply have repaired the frontmatter, in which
// case the file is no longer the thing this door exists for at all: it reads as
// a proposal now, and a proposal is decided about by reading it.
//
// Only two cases arrive here with no bytes to compare — a file nothing can open
// and a file past the size bound — and they are answered differently, because
// only one of them can still be proved. A file past the bound opens: there is a
// descriptor, its identity is the entry's own, and a read that fails the same
// way it failed before is that entry saying so. A file nothing can open has no
// descriptor and no way to get one, and a second failure to open says nothing
// about which inode is under the name — a different file, unreadable in the
// same way, gives the same answer. That one is refused, and the refusal says
// the one thing that would change it.
//
// That is narrower than the licence this door used to carry, and deliberately.
// It means a proposal nothing can open cannot be thrown away until it can be
// opened. The alternative is deleting bytes on the strength of a failure, which
// is how a rejection ends up removing a file nobody ever held a descriptor to.
//
// The licence that remains is also narrower than "it did not read". It is for
// the file the pre-check answered about, in the state it answered about it in,
// so the read has to fail again in the same broad way: a candidate past the
// bound which is now under it is a file something has written to since. And if
// the bytes can be read now the refusal is flat, whether or not they parse.
// Nobody has ever read them — the pre-check could not, so nothing was shown to
// the user and nothing was bound, and there is no hash to hold them to and no
// reader who could say they are what was rejected. A rejection is a verdict on
// words somebody saw.
func requireBoundUnreadableEntry(
	ctx context.Context,
	clean string,
	identity UnreadableCandidateEntry,
	opened *os.File,
	openedStat unix.Stat_t,
) error {
	if !identity.present {
		return &StaleContentError{RelPath: clean, Detail: boundRefusalDetail(
			"there was no proposal under that name when it was read, and something has taken it since, so it was left alone")}
	}
	if uint64(openedStat.Dev) != identity.device || uint64(openedStat.Ino) != identity.inode {
		return &StaleContentError{RelPath: clean, Detail: boundRefusalDetail(
			"another file has taken its name since it was read, so it was left alone")}
	}
	if opened == nil {
		// Nothing here can open the entry, and every removal in this package
		// is authorised by a descriptor whose own identity is checked. Without
		// one there is nothing that says which inode a name points at, and an
		// unlink names a name.
		//
		// The two ways of arriving here differ only in what is being given up.
		// A hashed binding has bytes it cannot compare, and an inode number
		// cannot tell a rewrite in place from the file that was read — which
		// is precisely the move a rejection must never delete. A binding to no
		// bytes at all has nothing but the identity, and a second failure to
		// open is not a fact about identity: it is the same answer a different
		// file of the same kind would give. Both are refused.
		if identity.hashed {
			return &StaleContentError{RelPath: clean, Detail: boundRefusalDetail(
				"its bytes were read once and cannot be read again to check they are still the same bytes, so it was left alone")}
		}
		if identity.failure != unreadableFailureUnopenable {
			return staleUnreadableFailureChange(clean)
		}
		// Refused here rather than after the detach. The answer cannot change
		// on the far side — an entry that opens there has become readable, or
		// refuses in a way it did not refuse before, and both of those are
		// refusals too — so a removal with no standing has no business taking
		// the user's bytes off their name to find that out.
		return unprovableEntryRefusal(clean)
	}
	content, readErr := readEntryContent(ctx, opened, clean, MaxNoteBytes)
	if readErr != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Still unreadable, and still the same entry. For a binding to bytes
		// that is as far as this check goes, and the far side of the detach
		// answers the rest. For a binding to no bytes at all it has to be
		// unreadable the same way it was, or something has written to the file
		// since the user was told it could not be read.
		if !identity.hashed && classifyUnreadableFailure(readErr) != identity.failure {
			return staleUnreadableFailureChange(clean)
		}
		return nil
	}
	if !identity.hashed {
		return staleUnreadableBecameReadable(clean)
	}
	if ContentHash(content) != identity.rawHash {
		return &StaleContentError{RelPath: clean, Detail: boundRefusalDetail(
			"it was rewritten in place since it was read, so it was left alone")}
	}
	if _, err := ParseNote(clean, content); err == nil {
		return &StaleContentError{RelPath: clean, Detail: boundRefusalDetail(
			"it reads as a proposal now, so it was left alone; read it again and decide about what it says")}
	}
	return nil
}

// staleUnreadableFailureChange is the refusal for an entry that refuses to be
// read in a different way than it refused before. It is the same file by
// identity, and it is not in the state the rejection was issued about.
func staleUnreadableFailureChange(clean string) error {
	return &StaleContentError{RelPath: clean, Detail: boundRefusalDetail(
		"it is unreadable in a different way than when it was read, so something has written to it since; it was left alone")}
}

// staleUnreadableBecameReadable is the refusal for the file this guard exists
// for: nothing could read it when it was rejected and something can read it
// now, so whatever it says is something nobody has seen.
func staleUnreadableBecameReadable(clean string) error {
	return &StaleContentError{RelPath: clean, Detail: boundRefusalDetail(
		"nothing could read it when it was rejected and it can be read now, so nobody has seen what it says; it was left alone")}
}

// unprovableEntryRefusal is the answer for a proposal nothing here can open.
//
// It is a refusal of its own kind rather than "the file changed since it was
// read", because nothing changed: the file is exactly where it was and says
// exactly what it said, and what is missing is any way to prove that an unlink
// would take that entry and not another. It carries ErrStaleContent too, so
// every caller that already handles a refused decision keeps handling it, and
// the sentence names the one thing the user can do about it.
func unprovableEntryRefusal(clean string) error {
	return &StaleContentError{
		RelPath: clean,
		Cause:   ErrUnprovableEntry,
		Detail: boundRefusalDetail(
			unprovableDetachedEntry + ", so it was left alone; make it readable in your vault and reject it again"),
	}
}

// objectionToDetachedEntry answers whether the entry a rejection has just taken
// off its name is the one it is entitled to delete, and says in a sentence why
// not when it is not. An empty answer is the only thing that authorises the
// unlink below it.
//
// It is a method on the detached entry because the entry is what it asks about.
// The name is gone; what is left is a reserved private name inside the same
// confined directory, and that is reachable — so a check that has nothing else
// to go on can still go and look, instead of concluding that a question it
// cannot ask has been answered.
//
// It asks whichever question the near side of the detach asked. A removal bound
// to bytes re-reads them. A removal bound to nothing but "this entry, and
// nothing could read it" asks the file again whether it still cannot be read,
// and in the same broad way — the two guards have to agree, or the detach
// window becomes the one place where bytes nobody has seen may be deleted.
func (d *detachedEntry) objectionToDetachedEntry(
	ctx context.Context,
	boundHash string,
	boundFailure unreadableFailure,
	opened *os.File,
	openedStat unix.Stat_t,
) string {
	detachedStat, statErr := d.stat()
	if statErr != nil {
		return fmt.Sprintf("the detached entry could not be inspected (%v)", statErr)
	}
	if detachedStat.Dev != openedStat.Dev || detachedStat.Ino != openedStat.Ino {
		return "another file had taken its name"
	}
	if boundHash == "" {
		return d.objectionToDetachedUnreadableEntry(ctx, boundFailure, opened, openedStat)
	}
	if opened == nil {
		// A binding to bytes is checked by reading those bytes. Without a
		// descriptor there is nothing to read them from, and the inode match
		// above is not that check: an editor writing new words in place keeps
		// the inode. The file goes back.
		return "it could not be read again before removal (nothing here can open it any more)"
	}
	// Same inode, and that is not the same thing as the same bytes: an editor
	// saving in place keeps the inode, and those are the user's own newer
	// words. They are held to the decision — or, for a hashless rejection, to
	// the bytes the pre-check could not parse — like everything else.
	content, readErr := rereadEntryContent(ctx, opened, d.clean)
	if readErr != nil {
		return fmt.Sprintf("it could not be read again before removal (%v)", readErr)
	}
	if ContentHash(content) != boundHash {
		return "it was rewritten in place"
	}
	return ""
}

// objectionToDetachedUnreadableEntry is the far side of the detach for the
// removal that has no bytes to compare: a proposal past the size bound, being
// thrown away on its identity and on its going on refusing to be read.
//
// The identity question is already answered above — for the name the stat
// found. This asks the other half, and asks it through a descriptor, because
// that is the only thing that answers both at once. Where the removal opened
// one before the detach it uses that, since a descriptor is the same inode
// whatever has happened to the name. Where it could not open one at all it goes
// and opens the detached entry through the reserved name it is now under, and
// an open that fails there is not an answer: it is the absence of one.
//
// That second path is the one this guard used to get wrong twice over. First it
// skipped the reopen entirely — "no descriptor" was read as "nothing to check",
// so a proposal whose permissions came back inside the detach window was
// deleted on an inode number with nobody having read a word of it. Then, with
// the reopen in place, a failed reopen was read as agreement: still unopenable,
// same class, unlink. But the class is a property of the file, not of the name,
// and any file of the same class under that name gives the same answer. What
// authorises a removal here is a descriptor whose own identity matches, and
// nothing else.
func (d *detachedEntry) objectionToDetachedUnreadableEntry(
	ctx context.Context,
	boundFailure unreadableFailure,
	opened *os.File,
	expected unix.Stat_t,
) string {
	if boundFailure == unreadableFailureNone {
		// Nothing recorded how this file refused to be read, so there is no
		// state to hold it to. Unreachable from the primitive above, which
		// refuses a binding like this before anything is detached, and a
		// refusal rather than a shrug because the alternative is a deletion
		// authorised by an absence.
		return "nothing recorded how it refused to be read, so there is nothing left to check it against"
	}
	if opened == nil {
		return d.objectionToReopenedUnreadableEntry(ctx, boundFailure, expected)
	}
	_, readErr := rereadEntryContent(ctx, opened, d.clean)
	return unreadableStateObjection(ctx, boundFailure, readErr)
}

// objectionToReopenedUnreadableEntry opens the detached entry through the
// reserved name it is under and asks it the near side's question again.
//
// The identity check is repeated against the reopened inode rather than assumed
// from the stat above. The two are separate syscalls, and the reserved name is
// random but not secret: anything that can list the vault directory can read it
// and rename over it. So an entry that has been swapped under it between the
// stat and the open is a file this rejection never looked at, and the only
// thing that can tell the difference is a descriptor.
//
// Which is why an open that fails is never an authorisation. It leaves this
// with no descriptor and so with no answer to either question: not what the
// entry is, and not whether the name still holds what the stat found. The bytes
// go back, and the caller says what would make a removal possible. In
// production the primitive above refuses a hashless rejection it cannot open
// before anything is detached, so this is the second lock on that door rather
// than the one the user meets — and it is the lock that has to hold if the
// reasoning above it is ever changed.
func (d *detachedEntry) objectionToReopenedUnreadableEntry(
	ctx context.Context,
	boundFailure unreadableFailure,
	expected unix.Stat_t,
) string {
	d.vault.detachBarrier(detachPhaseBeforeReopen, d.clean)
	reopened, reopenedStat, openErr := d.open()
	if openErr != nil {
		if ended := ctx.Err(); ended != nil {
			return endedBeforeVerification
		}
		if errors.Is(openErr, ErrConfinement) {
			// Not a regular file any more. Nothing about that is the state the
			// pre-check answered about, and this package does not remove it.
			return fmt.Sprintf("it is no longer an entry this vault will act on (%v)", openErr)
		}
		if boundFailure != unreadableFailureUnopenable {
			return "it is unreadable in a different way than when it was read, so something has written to it since"
		}
		return unprovableDetachedEntry
	}
	defer func() { _ = reopened.Close() }()
	if reopenedStat.Dev != expected.Dev || reopenedStat.Ino != expected.Ino {
		return "another file had taken its name"
	}
	_, readErr := readEntryContent(ctx, reopened, d.stagedRelPath(), MaxNoteBytes)
	return unreadableStateObjection(ctx, boundFailure, readErr)
}

// endedBeforeVerification is the one sentence every branch here uses for a
// request that went away mid-check. It is not a verdict about the file, and the
// caller turns it into the cancellation rather than into a claim about the
// vault.
const endedBeforeVerification = "the request ended before the removal could be verified"

// unprovableDetachedEntry is the one refusal here that is not a claim about the
// file having changed. Nothing can open the entry, so nothing can say which
// inode a name points at, and a removal that cannot prove what it would delete
// does not delete.
//
// It is a constant because two places have to produce exactly this sentence —
// the check before anything is detached and the descriptor recheck on the far
// side — and because the caller turns it into a refusal of its own kind rather
// than into "the file changed since it was read", which would send the user
// back to look at a change that never happened.
const unprovableDetachedEntry = "nothing here can open it to prove it is still the entry that was read"

// unreadableStateObjection compares how the entry refuses to be read now with
// how it refused when the user was told it could not be read.
//
// A request that ended is answered as itself: a read that stopped because the
// caller had gone says nothing about the file, and calling that "unreadable in
// a different way" would report a cancellation as a change to the user's vault.
func unreadableStateObjection(ctx context.Context, boundFailure unreadableFailure, readErr error) string {
	if ended := ctx.Err(); ended != nil {
		return endedBeforeVerification
	}
	switch {
	case readErr == nil:
		return "nothing could read it when it was rejected and it can be read now, so nobody has seen what it says"
	case classifyUnreadableFailure(readErr) != boundFailure:
		return "it is unreadable in a different way than when it was read, so something has written to it since"
	default:
		return ""
	}
}

// refuseDetachedRejection puts back a file this rejection detached and then
// found it had no standing to remove, and phrases the refusal either way.
//
// Where the name cannot be taken back the bytes still have to go somewhere a
// person will find them, and the private staging name is not that place: it
// begins with a dot, the vault walk skips dot entries, and a claim about the
// user that is on disk and on no page is indistinguishable from a deleted one
// to everybody except a forensic reader. So an inbox entry is linked under a
// name this server mints and the walk indexes — visible in the next scan, on
// the next ListMemoryState, and deletable by the user like any other file in
// their inbox. Only when that fails too does it stay staged, and then the
// refusal says exactly where it is.
//
// A request that has ended by the time the file is back is answered as the
// cancellation it is, whichever of those places the bytes ended up in. That is
// one rule rather than a special case for the tidy branch: "the file changed
// since it was read" is a claim about the user's vault, and a caller that has
// gone is nobody to make it on behalf of. What the cancellation carries is
// where the bytes are, so an operator reading the log still knows — the words
// go in the error, never into the status a caller sees.
func (v *Vault) refuseDetachedRejection(ctx context.Context, detached *detachedEntry, reason string) error {
	placement := detached.putBack()
	detail := boundRefusalDetail(reason + ", so it was left alone")
	if !placement.clean() {
		detail = boundRefusalDetail(placement.explain(reason))
	}
	if ended := ctx.Err(); ended != nil {
		return &EndedRequestError{RelPath: detached.clean, Detail: detail, Cause: ended}
	}
	// An entry nothing could open is not a claim that the file changed, and it
	// is answered as itself wherever it is discovered. The sentence is one
	// constant with one producer on each side of the detach, so matching on it
	// is matching on the branch that wrote it.
	cause := placement.failure()
	if reason == unprovableDetachedEntry {
		cause = errors.Join(cause, ErrUnprovableEntry)
	}
	if cause == nil {
		return &StaleContentError{RelPath: detached.clean, Detail: detail}
	}
	return &StaleContentError{
		RelPath: detached.clean,
		Cause:   cause,
		Detail:  detail,
	}
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

// readEntryContent reads an already-open entry under a stated bound, and
// refuses an over-bound file outright rather than answering with a prefix of it.
//
// The bound is a parameter because the two readers here are asking different
// questions. A candidate is read against what this package will hold in memory
// at all; a rollback re-reading its own write is asking whether these are still
// exactly those bytes, and anything longer already answers no.
func readEntryContent(ctx context.Context, file *os.File, clean string, limit int) (string, error) {
	content, err := readBounded(ctx, file, limit)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", clean, err)
	}
	if len(content) > limit {
		return "", &LimitError{What: fmt.Sprintf("note %q", clean), Limit: limit, Got: len(content)}
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
	return readEntryContent(ctx, file, clean, MaxNoteBytes)
}
