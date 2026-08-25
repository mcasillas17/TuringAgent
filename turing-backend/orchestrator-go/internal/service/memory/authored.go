package memory

import (
	"context"
	"errors"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The user's own hands on the two pinned documents.
//
// This file is the whole of that surface, and it is deliberately not reachable
// from anything the runtime can call: the facet split refuses these methods on
// the internal server, and the vault primitives underneath take no path. The
// persona is the only unframed instruction channel memory has, and it stays
// trustworthy exactly as long as a human is its only author.

// GetMemoryPersona reads persona.md as it stands on disk.
func (s *Server) GetMemoryPersona(ctx context.Context, _ *turingv1.GetMemoryPersonaRequest) (*turingv1.MemoryPersona, error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	return s.persona(ctx, settings), nil
}

// SaveMemoryPersona writes persona.md on the user's explicit action.
//
// The compare-and-set is the whole safety story: the user very likely has this
// file open in Obsidian, and a save composed against text the vault has since
// moved on from is refused with an instruction, never applied over the newer
// words and never silently re-prepared against them.
func (s *Server) SaveMemoryPersona(ctx context.Context, req *turingv1.SaveMemoryPersonaRequest) (*turingv1.SaveMemoryPersonaResponse, error) {
	if err := s.requireAuthoredSave(req.GetContent()); err != nil {
		return nil, err
	}
	saved, err := s.vault.SavePersona(ctx, memoryfiles.SavePersonaRequest{
		ExpectedContentHash: req.GetExpectedContentHash(),
		Content:             req.GetContent(),
	})
	if err != nil {
		return nil, authoredSaveError(err, "save persona failed")
	}
	// The trail records that the document was saved and how much of it there
	// is. Not a word of what it says: persona.md is the user's own writing,
	// and an audit row carrying it would be a second copy nobody asked for.
	s.record(ctx, "memory.persona.saved", saved.RelPath, map[string]any{"bytes": len(saved.Content)})
	return &turingv1.SaveMemoryPersonaResponse{Persona: &turingv1.MemoryPersona{
		Content:           saved.Content,
		ContentHash:       saved.ContentHash,
		Status:            turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_UNMANAGED,
		UnavailableReason: turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE,
	}}, nil
}

// SaveMemoryProfile writes profile.md as the user typed it.
//
// ApplyMemoryProfile stays what it was — the only way a proposal reaches this
// file, and it still needs a candidate the user has read. This is the other
// authority entirely: the user writing about themselves, with no proposal in
// the picture and no pending one disturbed.
func (s *Server) SaveMemoryProfile(ctx context.Context, req *turingv1.SaveMemoryProfileRequest) (*turingv1.SaveMemoryProfileResponse, error) {
	if err := s.requireAuthoredSave(req.GetContent()); err != nil {
		return nil, err
	}
	saved, err := s.vault.SaveProfile(ctx, memoryfiles.SaveProfileRequest{
		ExpectedContentHash: req.GetExpectedContentHash(),
		Content:             req.GetContent(),
	})
	if err != nil {
		return nil, authoredSaveError(err, "save profile failed")
	}
	s.record(ctx, "memory.profile.saved", saved.RelPath, map[string]any{"bytes": len(saved.Content)})
	return &turingv1.SaveMemoryProfileResponse{Profile: &turingv1.MemoryProfile{
		Content:           saved.Content,
		ContentHash:       saved.ContentHash,
		Status:            turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_MANAGED,
		UnavailableReason: turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE,
	}}, nil
}

// requireAuthoredSave is the shared precondition for both hand saves: there is
// a vault to write into, and the user actually typed something. Blank content
// is refused rather than accepted as "delete my persona" — emptying a pinned
// document is a decision, and it does not look like a save.
func (s *Server) requireAuthoredSave(content string) error {
	if s.vault == nil {
		return status.Error(codes.FailedPrecondition, "the memory vault is not available")
	}
	if strings.TrimSpace(content) == "" {
		return status.Error(codes.InvalidArgument, "content is required; to empty a document, edit it in the vault")
	}
	return nil
}

// authoredSaveError reports a lost compare-and-set as FailedPrecondition with
// the instruction that actually resolves it. The generic mapping calls this
// Aborted for the proposal path, where the caller is a decision flow; here the
// caller is a person with an editor open, and the code and the sentence are
// both aimed at them.
func authoredSaveError(err error, fallback string) error {
	if errors.Is(err, memoryfiles.ErrStaleContent) {
		return status.Error(codes.FailedPrecondition,
			"the file changed on disk since this editor read it; finish and close the memory editor, re-read the document, and save again")
	}
	return memoryError(err, fallback)
}

// persona is persona.md as the page should render it.
//
// Turning memory off does not hide it. The vault is files the user owns, and a
// disabled memory still has to be inspectable and repairable — otherwise the
// only way to fix an unreadable persona would be to turn the thing that cannot
// read it back on. The row says DISABLED so nobody mistakes a readable document
// for one that is in use.
func (s *Server) persona(ctx context.Context, settings *turingv1.MemorySettings) *turingv1.MemoryPersona {
	document, reason, detail := s.pinnedDocument(ctx, settings, func(ctx context.Context) memoryfiles.PinnedDocument {
		return s.vault.LoadPersona(ctx)
	})
	return &turingv1.MemoryPersona{
		Content:     document.Content,
		ContentHash: document.ContentHash,
		// Never MANAGED, whatever else is true: the persona is the one document
		// Turing reads and never rewrites.
		Status:            turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_UNMANAGED,
		ParseError:        detail,
		UnavailableReason: reason,
	}
}

// pinnedDocument runs one pinned read and answers the two questions the client
// asks of it separately: what the document says, and why it is not in use.
func (s *Server) pinnedDocument(
	ctx context.Context,
	settings *turingv1.MemorySettings,
	load func(context.Context) memoryfiles.PinnedDocument,
) (memoryfiles.PinnedDocument, turingv1.MemoryUnavailableReason, string) {
	if s.vault == nil {
		return memoryfiles.PinnedDocument{}, turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING, ""
	}
	document := load(ctx)
	reason := unavailableProto(document.Reason, document.Available)
	detail := document.Detail
	// A document that could not be read says so even while memory is off: the
	// user came here to fix it, and "memory is off" is not the answer to "why
	// is my persona empty" when the real answer is that it is a symlink.
	if reason == turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE && !settings.GetEnabled() {
		reason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED
	}
	return document, reason, detail
}
