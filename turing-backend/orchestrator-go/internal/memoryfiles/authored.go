package memoryfiles

import (
	"context"
)

// MaxAuthoredDocumentBytes bounds what the user may save into a pinned
// document through Turing. It is the read ceiling, not the pin budget: the
// user is free to write far more than reaches a prompt (past the budget the
// pin is cut and says so), but a document this package could not read back
// would be a trap — saved successfully, then reported unreadable forever.
const MaxAuthoredDocumentBytes = MaxPinnedSourceBytes

// SavePersonaRequest is the user editing persona.md. It carries no path: the
// one document this primitive writes is not a caller's choice.
type SavePersonaRequest struct {
	// ExpectedContentHash is the compare-and-set token. Empty means the caller
	// expects no persona to exist yet.
	ExpectedContentHash string
	Content             string
}

// SaveProfileRequest is the user editing profile.md by hand, with the same
// closed surface as SavePersonaRequest.
type SaveProfileRequest struct {
	ExpectedContentHash string
	Content             string
}

// AuthoredDocument is a pinned document as it stands after the user saved it.
type AuthoredDocument struct {
	RelPath     string
	Content     string
	ContentHash string
}

// SavePersona writes persona.md on the authority of the user and no one else.
//
// It is a separate primitive from ApplyProfileEdit on purpose. That one exists
// to apply text a model proposed, and persona.md is the one document no
// proposal may reach: it is the sole unframed instruction channel in memory,
// and it stays trustworthy only while a human is its only author. Routing a
// persona save through the proposal path — even "just for the file write" —
// would put the agent's write surface one confused caller away from it.
//
// The compare-and-set is verified through the descriptor that is written, and
// the update happens in place rather than by rename, because the user very
// likely has this file open in Obsidian.
func (v *Vault) SavePersona(ctx context.Context, request SavePersonaRequest) (AuthoredDocument, error) {
	return v.saveAuthoredDocument(ctx, PersonaFileName, requirePersonaRelPath, request.ExpectedContentHash, request.Content)
}

// SaveProfile writes profile.md as the user typed it, with no candidate in the
// picture.
//
// ApplyProfileEdit remains the only way a proposal reaches this file; this is
// the other direction, the user writing about themselves directly. Keeping
// them apart is what lets the proposal path keep demanding a candidate the
// user has read, instead of gaining a "no candidate needed" mode.
func (v *Vault) SaveProfile(ctx context.Context, request SaveProfileRequest) (AuthoredDocument, error) {
	return v.saveAuthoredDocument(ctx, ProfileFileName, requireProfileRelPath, request.ExpectedContentHash, request.Content)
}

// saveAuthoredDocument is the shared body of the two primitives above, and it
// is unexported and unreachable with a caller-supplied name for a reason: the
// document name always arrives as a literal from one of the two entry points,
// each of which re-checks it against its own gate. There is no generic
// "save this document" surface for a handler to aim.
func (v *Vault) saveAuthoredDocument(
	ctx context.Context,
	relPath string,
	gate func(string) (string, error),
	expectedContentHash string,
	content string,
) (AuthoredDocument, error) {
	if err := ctx.Err(); err != nil {
		return AuthoredDocument{}, err
	}
	target, err := gate(relPath)
	if err != nil {
		return AuthoredDocument{}, err
	}
	if len(content) > MaxAuthoredDocumentBytes {
		return AuthoredDocument{}, &LimitError{What: "the " + target + " document", Limit: MaxAuthoredDocumentBytes, Got: len(content)}
	}

	unlock, err := v.locks.lockContext(ctx, v.pathLockKey(target))
	if err != nil {
		return AuthoredDocument{}, err
	}
	defer unlock()

	if err := v.writePinnedDocumentWithCompareAndSet(ctx, target, expectedContentHash, content); err != nil {
		return AuthoredDocument{}, err
	}
	return AuthoredDocument{
		RelPath:     target,
		Content:     content,
		ContentHash: ContentHash(content),
	}, nil
}
