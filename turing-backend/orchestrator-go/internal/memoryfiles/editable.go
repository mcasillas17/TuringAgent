package memoryfiles

import (
	"context"
	"strings"
)

// EditableDocument is persona.md or profile.md as the person editing it must
// see it: the whole file, and a token that names the whole file.
//
// It is deliberately not PinnedDocument. That one answers "what reaches a
// prompt" — bounded at the budget, cut on a rune boundary, with a notice
// appended in the document's own voice so a model knows it is holding a
// fragment, and hashed after all of it because the hash is the preimage an
// egress fingerprint is computed over. Every one of those is wrong here. An
// editor showing the pin would display a truncated document with words the
// user never typed, and a compare-and-set carrying the pin's hash could never
// match the file it is verified against, so a document past the budget could
// be read once and never saved again.
//
// The two pinned fields are what is left of the runtime's answer: a statement
// about the pin, kept beside the document instead of baked into it.
type EditableDocument struct {
	RelPath string
	// Content is the file, whole, up to MaxAuthoredDocumentBytes.
	Content string
	// ContentHash covers exactly the bytes in Content. It is the
	// compare-and-set token a save is verified with, so it is empty whenever
	// the document could not be read in full — a token for a partial read
	// would let a save truncate the file to whatever the editor happened to
	// hold, which is the outcome the compare-and-set exists to prevent.
	ContentHash string
	// PinnedTruncated says a run carries less than the whole of Content.
	PinnedTruncated bool
	// PinnedBytes is how many of this document's bytes reach a prompt: the
	// rune-safe cut when it is truncated, its whole length when it is not, and
	// zero when nothing survives trimming. It never counts the truncation
	// notice, which is the runtime's sentence rather than the user's document.
	PinnedBytes int
	Available   bool
	Reason      UnavailableReason
	Detail      string
	ModTimeUnix int64
	SizeBytes   int64
}

// EditablePersona reads persona.md for the user's own editor. Like the pinned
// loader it takes no path, so no caller can aim it at the inbox.
func (v *Vault) EditablePersona(ctx context.Context) EditableDocument {
	return v.loadEditable(ctx, PersonaFileName, MaxPersonaBytes, requirePersonaRelPath)
}

// EditableProfile reads profile.md for the user's own editor, with the same
// closed surface as EditablePersona.
func (v *Vault) EditableProfile(ctx context.Context) EditableDocument {
	return v.loadEditable(ctx, ProfileFileName, MaxProfileBytes, requireProfileRelPath)
}

// loadEditable never returns an error, for the same reason loadPinned does not:
// a document that could not be read is a visible row saying why, not a page
// that renders an unreadable persona as an empty healthy one.
//
// A document past MaxAuthoredDocumentBytes is refused outright rather than
// served in part. Reading the first 512 KiB into an editor would put the user
// one Save away from truncating their own file to it.
func (v *Vault) loadEditable(ctx context.Context, relPath string, budget int, gate func(string) (string, error)) EditableDocument {
	document := EditableDocument{RelPath: relPath}
	clean, err := gate(relPath)
	if err != nil {
		document.Reason = UnavailableVaultUnreadable
		document.Detail = err.Error()
		return document
	}
	content, stat, err := v.readConfinedFile(ctx, clean, MaxAuthoredDocumentBytes)
	if err != nil {
		document.Reason = unavailableReasonFor(err)
		document.Detail = err.Error()
		return document
	}
	document.ModTimeUnix = stat.Mtim.Sec
	document.SizeBytes = stat.Size
	document.Content = content
	document.ContentHash = ContentHash(content)
	document.PinnedBytes, document.PinnedTruncated = pinnedBudget(content, budget)
	document.Available = true
	document.Reason = UnavailableNone
	return document
}

// pinnedBudget reports what the runtime would pin from this document, without
// building the pin: how many of its own bytes reach a prompt, and whether the
// rest was cut.
//
// It is the one place the budget rule lives, shared with the pinned loader, so
// an editor can never say "all of this reaches a run" about a document the
// runtime would truncate.
func pinnedBudget(content string, budget int) (int, bool) {
	kept, truncated := truncateRunes(content, budget)
	// Whitespace that survives the cut is not content, and pins nothing. The
	// pinned loader makes the same call before it appends its notice, so the
	// two answers cannot drift.
	if strings.TrimSpace(kept) == "" {
		return 0, truncated
	}
	return len(kept), truncated
}
