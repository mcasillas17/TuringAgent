package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const (
	// MaxPersonaBytes and MaxProfileBytes are context budgets: how much of each
	// pinned document reaches a prompt. They bound the pin, not the file. The
	// user may write as much as they like; past the budget the pin is cut on a
	// rune boundary and carries a notice saying so.
	MaxPersonaBytes = 4096
	MaxProfileBytes = 4096

	// MaxPinnedSourceBytes is a different question with a different answer: how
	// large a pinned document this package will read off disk at all. It exists
	// only as a safety ceiling for the absurd case — a persona.md a script
	// appended a gigabyte to — and it is two orders of magnitude above the
	// budget precisely so that going over the budget stays what it is, an
	// ordinary truncation, and never gets reported as an unreadable file.
	//
	// A file past this ceiling pins nothing and says why, per the plan's
	// pinned-file failure posture: no silent partial load.
	MaxPinnedSourceBytes = 512 * 1024
)

// UnavailableReason is the visible answer to "why is nothing pinned". It
// mirrors the reasons the memory protocol reports, so the client can say what
// is wrong instead of rendering an empty vault as a healthy one.
type UnavailableReason string

const (
	UnavailableNone            UnavailableReason = ""
	UnavailableVaultMissing    UnavailableReason = "vault_missing"
	UnavailableVaultUnreadable UnavailableReason = "vault_unreadable"
	UnavailableContentParse    UnavailableReason = "content_parse_failed"
	UnavailableContentTooLarge UnavailableReason = "content_too_large"
)

// PinnedDocument is persona.md or profile.md as it will be pinned.
//
// Persona is pinned unframed: it is the user's instruction about who Turing is,
// and wrapping it in scaffolding would change its voice. Profile is returned as
// plain content for the runtime to frame, because a description of the user has
// to be labelled as such before a model reads it.
type PinnedDocument struct {
	RelPath     string
	Content     string
	ContentHash string
	Truncated   bool
	Available   bool
	Reason      UnavailableReason
	Detail      string
	ModTimeUnix int64
	SizeBytes   int64
}

// LoadPersona pins persona.md. It takes no path argument at all, so no caller
// can aim it at the inbox.
func (v *Vault) LoadPersona(ctx context.Context) PinnedDocument {
	return v.loadPinned(ctx, PersonaFileName, MaxPersonaBytes, requirePersonaRelPath)
}

// LoadProfile pins profile.md, with the same closed surface as LoadPersona.
func (v *Vault) LoadProfile(ctx context.Context) PinnedDocument {
	return v.loadPinned(ctx, ProfileFileName, MaxProfileBytes, requireProfileRelPath)
}

// loadPinned never returns an error: a missing, unreadable or over-large pinned
// document pins nothing and says why, because a prompt silently missing the
// user's persona is worse than a visible row saying it could not be read.
func (v *Vault) loadPinned(ctx context.Context, relPath string, limit int, gate func(string) (string, error)) PinnedDocument {
	document := PinnedDocument{RelPath: relPath}
	clean, err := gate(relPath)
	if err != nil {
		document.Reason = UnavailableVaultUnreadable
		document.Detail = err.Error()
		return document
	}
	content, stat, err := v.readConfinedFile(ctx, clean, MaxPinnedSourceBytes)
	if err != nil {
		document.Reason = unavailableReasonFor(err)
		document.Detail = err.Error()
		return document
	}
	document.ModTimeUnix = stat.Mtim.Sec
	document.SizeBytes = stat.Size

	pinned, truncated := truncateRunes(content, limit)
	document.Truncated = truncated
	// Whitespace that survives truncation is not content. Checked before the
	// notice is appended, or the notice itself would make an empty pin look
	// populated.
	if strings.TrimSpace(pinned) == "" {
		pinned = ""
	} else if truncated {
		pinned += truncationNotice(relPath, len(pinned))
	}
	document.Content = pinned
	// The hash covers exactly what was pinned, notice included. It is the
	// preimage an enqueue re-checks to notice the vault changed underneath a
	// consented run, so hashing the file's own bytes instead would compare
	// against something no model was ever shown.
	document.ContentHash = ContentHash(pinned)
	document.Available = true
	document.Reason = UnavailableNone
	return document
}

func unavailableReasonFor(err error) UnavailableReason {
	switch {
	case errors.Is(err, ErrTooLarge):
		return UnavailableContentTooLarge
	case errors.Is(err, ErrNoteParse):
		return UnavailableContentParse
	case errors.Is(err, unix.ENOENT), errors.Is(err, unix.ENOTDIR):
		return UnavailableVaultMissing
	default:
		return UnavailableVaultUnreadable
	}
}

// truncateRunes cuts at a rune boundary at or below the byte limit, so a pinned
// document never ends in half a character.
func truncateRunes(content string, limit int) (string, bool) {
	if len(content) <= limit {
		return content, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return content[:cut], true
}

// truncationNotice is written in the document's own voice rather than as
// metadata, because the model reading it has no other channel to learn that
// what it is holding is a fragment.
//
// It reports the bytes actually kept, not the budget. The rune-safe cut lands
// at or below the budget, and a notice that always claimed the budget would be
// quietly wrong on every document that ends in a multi-byte character — which
// is the only situation where anyone would go looking at the number.
func truncationNotice(relPath string, retainedBytes int) string {
	return fmt.Sprintf("\n\n[Only the first %d bytes of %s are pinned. Open the vault to read the rest.]\n", retainedBytes, relPath)
}

// BeliefResolver maps a stable belief identity to its vault-relative path. The
// repository owns the mapping; this package still re-checks whatever comes back.
type BeliefResolver func(beliefID string) (string, bool)

// BeliefDocument is one belief served from disk.
type BeliefDocument struct {
	NoteID      string
	RelPath     string
	Title       string
	Content     string
	ContentHash string
	ModTimeUnix int64
	SizeBytes   int64
}

// ReadBeliefByID serves a belief's current bytes, read fresh through the
// confined opener rather than from a database column or a search projection: a
// row is a copy of what the file said last time anyone looked, and the user may
// have edited the file since.
//
// There is no path or scope argument. The only way in is a stable identity, and
// whatever path the resolver hands back is re-checked here against the beliefs
// gate before a single byte is opened.
func (v *Vault) ReadBeliefByID(ctx context.Context, beliefID string, resolve BeliefResolver) (BeliefDocument, error) {
	if err := ctx.Err(); err != nil {
		return BeliefDocument{}, err
	}
	if strings.TrimSpace(beliefID) == "" {
		return BeliefDocument{}, errors.New("a belief can only be read by a non-empty identity")
	}
	if resolve == nil {
		return BeliefDocument{}, errors.New("no belief resolver was supplied")
	}
	resolved, ok := resolve(beliefID)
	if !ok {
		return BeliefDocument{}, fmt.Errorf("belief %q is not in the vault index", beliefID)
	}
	clean, err := requireBeliefsRelPath(resolved)
	if err != nil {
		return BeliefDocument{}, err
	}
	content, stat, err := v.readConfinedFile(ctx, clean, MaxNoteBytes)
	if err != nil {
		return BeliefDocument{}, err
	}
	parsed, err := ParseNote(clean, content)
	if err != nil {
		return BeliefDocument{}, err
	}
	// A file that does not carry this identity is not this belief, however the
	// index got there. Serving one that names a different id would answer a
	// question nobody asked; serving one that names no id at all would attach a
	// citation the file cannot support — it is a note the user wrote by hand
	// that reconcile has not adopted yet, not a memory Turing stored.
	if parsed.ID != beliefID {
		if parsed.ID == "" {
			return BeliefDocument{}, fmt.Errorf("%q carries no stable id, so it cannot be served as belief %q; the vault index is stale", clean, beliefID)
		}
		return BeliefDocument{}, fmt.Errorf("%q holds identity %q, not %q; the vault index is stale", clean, parsed.ID, beliefID)
	}
	return BeliefDocument{
		NoteID:      beliefID,
		RelPath:     clean,
		Title:       parsed.Title,
		Content:     content,
		ContentHash: ContentHash(content),
		ModTimeUnix: stat.Mtim.Sec,
		SizeBytes:   stat.Size,
	}, nil
}
