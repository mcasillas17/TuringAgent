package memory

import (
	"context"
	"errors"
	"sort"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// maxCandidateListing bounds one listing of proposals. It is the repository's
// own ceiling; a page beyond it is a bug in the caller, not something to widen.
const maxCandidateListing = 200

// ListMemoryState is the whole page: what memory is doing, what it holds, and
// everything it could not read.
//
// It runs the writing reconcile pass first. This is the one user-facing read
// where that is right: the user is looking at their vault, so a note they
// hand-wrote should be adopted, a candidate whose file they deleted should be
// retired, and citations should catch up — all of which is invisible unless a
// pass that may write runs. A tool call never does this; search uses the
// read-only pass instead.
func (s *Server) ListMemoryState(ctx context.Context, _ *turingv1.ListMemoryStateRequest) (*turingv1.ListMemoryStateResponse, error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	response := &turingv1.ListMemoryStateResponse{
		Settings:   settings,
		Tiers:      []*turingv1.MemoryTierState{},
		Notes:      []*turingv1.MemoryNote{},
		Candidates: []*turingv1.MemoryCandidate{},
	}
	if s.vault == nil {
		response.Profile = &turingv1.MemoryProfile{
			UnavailableReason: turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING,
		}
		response.Persona = &turingv1.MemoryPersona{
			Status:            turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_UNMANAGED,
			UnavailableReason: turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING,
		}
		return response, nil
	}

	view, err := s.readVault(ctx)
	if err != nil {
		return nil, err
	}
	if view.reason != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE &&
		settings.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED {
		// A vault the pass could not read is stated on the settings row rather
		// than rendered as an empty, healthy vault — unless the row is already
		// saying the user turned memory off, which outranks it. The folder
		// problem is not lost: it stays on the tier it actually stops.
		settings.UnavailableReason = view.reason
		settings.ParseError = view.detail
	}
	response.Notes = view.notes
	response.Candidates = view.candidates
	response.Profile = s.profile(ctx, settings)
	response.Persona = s.persona(ctx, settings)
	response.Tiers = s.tiers(settings, view, response.Profile, response.Persona)
	return response, nil
}

func (s *Server) GetMemorySettings(ctx context.Context, _ *turingv1.GetMemorySettingsRequest) (*turingv1.MemorySettings, error) {
	return s.settings(ctx)
}

// SetMemoryEnabled writes the toggle and republishes the tool list.
//
// The republish happens only when the value actually moved: telling every
// connected worker to rebuild its tools because a setting was written to the
// value it already had is noise, and noise is how a real change gets missed.
func (s *Server) SetMemoryEnabled(ctx context.Context, req *turingv1.SetMemoryEnabledRequest) (*turingv1.MemorySettings, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "a memory toggle needs a value")
	}
	if req.GetTier() != turingv1.MemoryTier_MEMORY_TIER_UNSPECIFIED {
		// Refused rather than quietly widened to the whole of memory: a user
		// who asked to turn off beliefs and had persona turned off too would
		// have been answered with something they did not ask for.
		return nil, status.Error(codes.Unimplemented, "memory is turned on or off as a whole; per-tier toggles are not available")
	}
	changed, err := s.repo.SetMemoryEnabled(ctx, req.GetEnabled())
	if err != nil {
		return nil, memoryError(err, "write memory settings failed")
	}
	if changed {
		s.record(ctx, "memory.enabled_changed", ServerName, map[string]any{"enabled": req.GetEnabled()})
		s.notifyRegistryChanged()
	}
	return s.settings(ctx)
}

// settings describes what memory is doing, in the order a person can act on.
//
// A toggle that is off outranks every other reason. "Memory is off" is a
// decision the user made and the one thing on this row they can change; a vault
// that has since gone missing is a fact about a folder. Reporting the folder
// instead would invite them to go and fix it and expect memory back — so the
// row keeps saying DISABLED, and the vault's own trouble is reported beside the
// tier it actually stops from being read.
func (s *Server) settings(ctx context.Context) (*turingv1.MemorySettings, error) {
	enabled, err := s.repo.MemoryEnabled(ctx)
	if err != nil {
		return nil, memoryError(err, "read memory settings failed")
	}
	settings := &turingv1.MemorySettings{
		Enabled:           enabled,
		UnavailableReason: turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE,
	}
	if s.vault != nil {
		settings.VaultRoot = s.vault.Root()
		settings.VaultWritable = unix.Access(s.vault.Root(), unix.W_OK) == nil
	}
	switch {
	case !enabled:
		settings.UnavailableReason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED
	case s.vault == nil:
		settings.UnavailableReason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING
	}
	return settings, nil
}

// vaultView is one pass over the vault, in the shapes the public surface needs.
type vaultView struct {
	notes      []*turingv1.MemoryNote
	candidates []*turingv1.MemoryCandidate
	beliefs    int
	reason     turingv1.MemoryUnavailableReason
	detail     string
}

func (s *Server) readVault(ctx context.Context) (vaultView, error) {
	view := vaultView{
		notes:      []*turingv1.MemoryNote{},
		candidates: []*turingv1.MemoryCandidate{},
		reason:     turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE,
	}
	report, reconcileErr := s.repo.ReconcileMemoryVault(ctx)
	if reconcileErr != nil {
		if errors.Is(reconcileErr, context.Canceled) || errors.Is(reconcileErr, context.DeadlineExceeded) {
			return view, memoryError(reconcileErr, "reconcile memory vault failed")
		}
		view.reason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_UNREADABLE
		view.detail = "the vault could not be reconciled; open it and check the folder is readable"
	}
	// The render pass goes through the repository, not straight at the vault,
	// so it shares the lock and the cache the reconcile above just filled.
	// Walking the user's whole vault a second time to draw one page is a walk
	// too many, and two independent walks can disagree about what is in it.
	scan, scanErr := s.repo.ScanMemoryVault(ctx)
	if scanErr != nil {
		if errors.Is(scanErr, context.Canceled) || errors.Is(scanErr, context.DeadlineExceeded) {
			return view, memoryError(scanErr, "scan memory vault failed")
		}
		view.reason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_UNREADABLE
		view.detail = "the vault could not be read; open it and check the folder is readable"
		return view, nil
	}
	if !scan.Completeness.Beliefs.Complete || !scan.Completeness.Inbox.Complete {
		view.reason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_UNREADABLE
		view.detail = "part of the vault could not be listed, so this is not the whole of it"
	}

	drafts := make(map[string]struct{}, len(report.Index.UnmanagedInboxDrafts))
	for _, path := range report.Index.UnmanagedInboxDrafts {
		drafts[path] = struct{}{}
	}
	// The inbox as the walk just read it, keyed by path. A managed proposal's
	// row records what Turing wrote; this is what the file says now, and the
	// two are not the same thing the moment the user opens the vault.
	inbox := make(map[string]memoryfiles.NoteRow, len(scan.Notes))
	for _, row := range scan.Notes {
		switch row.Area {
		case memoryfiles.AreaBeliefs:
			view.notes = append(view.notes, s.beliefProto(ctx, row))
			view.beliefs++
		case memoryfiles.AreaInbox:
			inbox[row.RelPath] = row
			if _, unmanaged := drafts[row.RelPath]; unmanaged {
				view.candidates = append(view.candidates, unmanagedDraftProto(row))
			}
		}
	}
	// Every candidate row, not just the pending ones: a decided candidate is
	// deleted outright, so what is left is exactly what is still sitting in the
	// inbox — including one withdrawn because its conversation was deleted,
	// whose file is still there and would otherwise vanish from the page while
	// staying on disk.
	managed, err := s.listCandidates(ctx, repository.MemoryCandidateQuery{Limit: maxCandidateListing})
	if err != nil {
		return view, err
	}
	for _, candidate := range managed {
		overlayCandidateFile(candidate, inbox)
	}
	view.candidates = append(view.candidates, managed...)
	sort.SliceStable(view.candidates, func(i int, j int) bool {
		return view.candidates[i].GetInboxPath() < view.candidates[j].GetInboxPath()
	})
	return view, nil
}

// overlayCandidateFile makes a listed proposal say what its file says.
//
// The database row is Turing's record of what it proposed; the file is what the
// user is looking at, and the vault is a vault precisely so they can open it
// and rewrite the claim before deciding. Serving the row's copy would show one
// text and carry the decision against another — and the hash the client sends
// back is compared against the file, so a stale hash would refuse every
// decision the page offered.
//
// Only content, hash and kind come from the file. Identity, provenance, source
// and lifecycle are the row's, because the file cannot know them.
func overlayCandidateFile(candidate *turingv1.MemoryCandidate, inbox map[string]memoryfiles.NoteRow) {
	row, found := inbox[candidate.GetInboxPath()]
	if !found {
		// The row names a file the walk did not find. Reconcile retires such a
		// row when it can see the whole inbox, so this is the narrow window
		// where it cannot yet — and a proposal whose text nobody can read is
		// not one to offer a decision on.
		candidate.UnavailableReason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING
		return
	}
	if row.ParseError != "" {
		candidate.Content = row.Content
		candidate.ContentHash = row.ContentHash
		candidate.ParseError = row.ParseError
		candidate.UnavailableReason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED
		return
	}
	candidate.Content = candidateBodyText(row.Body)
	candidate.ContentHash = row.ContentHash
	candidate.Kind = candidateKindProto(string(row.Kind))
}

// candidateBodyText is the claim as the user reads it, without the blank line
// the note renderer puts between the frontmatter and the body or the newline it
// ends the file with. Those two are the file's shape, not the proposal's words,
// and a client that showed them would render every proposal with a leading
// blank line the model never wrote. Nothing inside the claim is touched.
func candidateBodyText(body string) string {
	return strings.Trim(body, "\r\n")
}

// overlayCandidatesFromVault is the same rule as overlayCandidateFile for the
// reads that have no whole-vault walk to draw on: one confined read per
// proposal, bounded by the listing ceiling above it.
//
// It exists so every path that serves a managed proposal serves the same text
// and the same token. A read that skipped it would hand the client the row's
// hash, which the decision then compares against the file and refuses — a page
// whose buttons never work.
func (s *Server) overlayCandidatesFromVault(ctx context.Context, candidates []*turingv1.MemoryCandidate) {
	if s.vault == nil {
		return
	}
	for _, candidate := range candidates {
		if !candidate.GetManaged() {
			continue
		}
		note, err := s.vault.ReadInboxNote(ctx, candidate.GetInboxPath())
		if err != nil {
			candidate.UnavailableReason = unavailableProto(memoryfiles.UnavailableReasonFor(err), false)
			candidate.ParseError = err.Error()
			continue
		}
		candidate.Content = candidateBodyText(note.Body)
		candidate.ContentHash = note.ContentHash
		candidate.Kind = candidateKindProto(string(note.Kind))
	}
}

func (s *Server) beliefProto(ctx context.Context, row memoryfiles.NoteRow) *turingv1.MemoryNote {
	note := &turingv1.MemoryNote{
		NoteId:            row.NoteID,
		Path:              row.RelPath,
		Title:             row.Title,
		Content:           row.Content,
		ContentHash:       row.ContentHash,
		Status:            noteStatusProto(string(row.Status)),
		Tier:              turingv1.MemoryTier_MEMORY_TIER_BELIEF,
		ParseError:        row.ParseError,
		UnavailableReason: turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE,
		Provenance:        []*turingv1.MemoryProvenance{},
	}
	if row.ParseError != "" {
		note.UnavailableReason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED
	}
	if row.NoteID == "" {
		return note
	}
	indexed, err := s.repo.MemoryNoteByID(ctx, row.NoteID)
	if err == nil {
		note.Status = noteStatusProto(indexed.Status)
		note.CreatedAt = timestampProto(indexed.CreatedAt)
		note.UpdatedAt = timestampProto(indexed.UpdatedAt)
	}
	evidence, err := s.repo.MemoryNoteEvidence(ctx, row.NoteID)
	if err != nil {
		return note
	}
	withdrawn := row.EvidenceWithdrawn || indexed.Status == repository.MemoryNoteStatusWithdrawn
	// One provenance row per conversation the claim still rests on, carrying how
	// many times that conversation cited it. Excerpts are never returned; the
	// count is the whole of what is said about them.
	for _, cited := range evidence {
		note.Provenance = append(note.Provenance, &turingv1.MemoryProvenance{
			Kind:            turingv1.MemoryProvenanceKind_MEMORY_PROVENANCE_KIND_PROMOTED_FROM_CANDIDATE,
			SourceSessionId: cited.SessionID,
			Withdrawn:       withdrawn,
			EvidenceCount:   int32(cited.Count),
		})
	}
	// A withdrawn note whose conversations are all gone would otherwise arrive
	// with an empty list, which reads the same as a belief that never had any
	// evidence. They are different histories, so the withdrawal is said out
	// loud: no session to open, and nothing counted.
	if withdrawn && len(note.Provenance) == 0 {
		note.Provenance = append(note.Provenance, &turingv1.MemoryProvenance{
			Kind:          turingv1.MemoryProvenanceKind_MEMORY_PROVENANCE_KIND_PROMOTED_FROM_CANDIDATE,
			Withdrawn:     true,
			EvidenceCount: 0,
		})
	}
	return note
}

// unmanagedDraftProto describes a file the user put in the inbox themselves.
//
// It carries no candidate id because there is no row: nothing in the database
// claims this file, nothing will rewrite it, and every decision RPC will refuse
// it. Moving it into beliefs/ is the user's own action, in their own editor.
func unmanagedDraftProto(row memoryfiles.NoteRow) *turingv1.MemoryCandidate {
	candidate := &turingv1.MemoryCandidate{
		Kind:              turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_UNSPECIFIED,
		InboxPath:         row.RelPath,
		Content:           row.Content,
		ContentHash:       row.ContentHash,
		State:             turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_UNSPECIFIED,
		Managed:           false,
		ParseError:        row.ParseError,
		UnavailableReason: turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE,
		Provenance:        []*turingv1.MemoryProvenance{},
	}
	if row.ParseError != "" {
		candidate.UnavailableReason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED
	}
	return candidate
}

// ListMemoryCandidates lists the inbox.
//
// An unfiltered listing is the whole inbox, which includes the drafts the user
// dropped in themselves — those have no state and no kind, so they appear only
// here and drop out the moment either filter is applied, which is the honest
// answer rather than pretending they carry a lifecycle they do not have.
func (s *Server) ListMemoryCandidates(ctx context.Context, req *turingv1.ListMemoryCandidatesRequest) (*turingv1.ListMemoryCandidatesResponse, error) {
	query := repository.MemoryCandidateQuery{Limit: maxCandidateListing}
	kind := turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_UNSPECIFIED
	if req != nil {
		state, err := candidateStateName(req.GetState())
		if err != nil {
			return nil, err
		}
		query.State = state
		kind = req.GetKind()
	}
	var candidates []*turingv1.MemoryCandidate
	var err error
	if query.State == "" && kind == turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_UNSPECIFIED && s.vault != nil {
		view, viewErr := s.readVault(ctx)
		if viewErr != nil {
			return nil, viewErr
		}
		candidates = view.candidates
	} else {
		if candidates, err = s.listCandidates(ctx, query); err != nil {
			return nil, err
		}
		// A filtered listing has no whole-vault walk behind it, so the same
		// "the file is what the user is looking at" rule is applied one
		// proposal at a time. The kind filter below then reads the kind the
		// file declares rather than the one the row remembers.
		s.overlayCandidatesFromVault(ctx, candidates)
	}
	if kind != turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_UNSPECIFIED {
		filtered := make([]*turingv1.MemoryCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate.GetKind() == kind {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
	}
	response := &turingv1.ListMemoryCandidatesResponse{
		Candidates:        candidates,
		UnavailableReason: turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE,
	}
	enabled, err := s.repo.MemoryEnabled(ctx)
	if err != nil {
		return nil, memoryError(err, "read memory settings failed")
	}
	if !enabled {
		response.UnavailableReason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED
	}
	return response, nil
}

func (s *Server) listCandidates(ctx context.Context, query repository.MemoryCandidateQuery) ([]*turingv1.MemoryCandidate, error) {
	rows, err := s.repo.ListMemoryCandidates(ctx, query)
	if err != nil {
		return nil, memoryError(err, "list memory candidates failed")
	}
	candidates := make([]*turingv1.MemoryCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, candidateProto(row))
	}
	return candidates, nil
}

func (s *Server) GetMemoryCandidate(ctx context.Context, req *turingv1.GetMemoryCandidateRequest) (*turingv1.MemoryCandidate, error) {
	if req == nil || req.GetCandidateId() == "" {
		return nil, status.Error(codes.InvalidArgument, "candidate_id is required")
	}
	candidate, err := s.repo.MemoryCandidateByID(ctx, req.GetCandidateId())
	if err != nil {
		return nil, memoryError(err, "read memory candidate failed")
	}
	proposal := candidateProto(candidate)
	s.overlayCandidatesFromVault(ctx, []*turingv1.MemoryCandidate{proposal})
	return proposal, nil
}

// PromoteMemoryCandidate accepts a belief.
//
// Only a belief: a profile edit rewrites the user's profile document and goes
// through ApplyMemoryProfile, which has the compare-and-set that protects it.
// The expected hash is checked here before anything moves, so a decision
// composed against text that has since changed is refused rather than applied
// to text the user never read.
func (s *Server) PromoteMemoryCandidate(ctx context.Context, req *turingv1.PromoteMemoryCandidateRequest) (*turingv1.PromoteMemoryCandidateResponse, error) {
	if req == nil || req.GetCandidateId() == "" {
		return nil, status.Error(codes.InvalidArgument, "candidate_id is required")
	}
	if strings.TrimSpace(req.GetEditedContent()) != "" {
		// Accepting this and ignoring it would publish a claim the user thought
		// they had rewritten. Refusing says so.
		return nil, status.Error(codes.Unimplemented, "editing a proposal before accepting it is not available yet; edit the file in the vault instead")
	}
	switch req.GetTargetTier() {
	case turingv1.MemoryTier_MEMORY_TIER_UNSPECIFIED, turingv1.MemoryTier_MEMORY_TIER_BELIEF:
	default:
		return nil, status.Error(codes.InvalidArgument, "a promoted candidate becomes a belief; no other tier is written this way")
	}
	candidate, err := s.repo.MemoryCandidateByID(ctx, req.GetCandidateId())
	if err != nil {
		return nil, memoryError(err, "read memory candidate failed")
	}
	note, err := s.repo.PromoteMemoryCandidate(ctx, repository.MemoryCandidateDecision{
		CandidateID:           candidate.CandidateID,
		ExpectedCandidateHash: candidateCompareAndSet(req.GetExpectedCandidateHash(), deprecatedPromoteHash(req)),
	})
	if err != nil {
		if errors.Is(err, repository.ErrMemoryCandidateKind) {
			return nil, status.Error(codes.FailedPrecondition, "this is a profile edit; apply it to the profile instead")
		}
		return nil, memoryError(err, "promote memory candidate failed")
	}
	decided := candidateProto(candidate)
	decided.State = turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PROMOTED
	decided.PromotedNoteId = note.NoteID
	return &turingv1.PromoteMemoryCandidateResponse{
		Candidate: decided,
		Note: &turingv1.MemoryNote{
			NoteId:            note.NoteID,
			Path:              note.Path,
			Content:           note.Content,
			ContentHash:       note.ContentHash,
			Status:            noteStatusProto(note.Status),
			Tier:              turingv1.MemoryTier_MEMORY_TIER_BELIEF,
			CreatedAt:         timestampProto(note.CreatedAt),
			UpdatedAt:         timestampProto(note.UpdatedAt),
			UnavailableReason: turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE,
			Provenance:        []*turingv1.MemoryProvenance{},
		},
	}, nil
}

// RejectMemoryCandidate refuses a proposal: the row and the file both go.
func (s *Server) RejectMemoryCandidate(ctx context.Context, req *turingv1.RejectMemoryCandidateRequest) (*turingv1.RejectMemoryCandidateResponse, error) {
	if req == nil || req.GetCandidateId() == "" {
		return nil, status.Error(codes.InvalidArgument, "candidate_id is required")
	}
	candidate, err := s.repo.MemoryCandidateByID(ctx, req.GetCandidateId())
	if err != nil {
		return nil, memoryError(err, "read memory candidate failed")
	}
	if err := s.repo.RejectMemoryCandidate(ctx, repository.MemoryCandidateDecision{
		CandidateID:           candidate.CandidateID,
		ExpectedCandidateHash: candidateCompareAndSet(req.GetExpectedCandidateHash(), deprecatedRejectHash(req)),
	}); err != nil {
		return nil, memoryError(err, "reject memory candidate failed")
	}
	decided := candidateProto(candidate)
	decided.State = turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_REJECTED
	return &turingv1.RejectMemoryCandidateResponse{Candidate: decided}, nil
}

func (s *Server) GetMemoryProfile(ctx context.Context, _ *turingv1.GetMemoryProfileRequest) (*turingv1.MemoryProfile, error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	return s.profile(ctx, settings), nil
}

// ApplyMemoryProfile writes profile.md on the authority of a profile_edit the
// user is looking at, under compare-and-set against the profile itself.
func (s *Server) ApplyMemoryProfile(ctx context.Context, req *turingv1.ApplyMemoryProfileRequest) (*turingv1.ApplyMemoryProfileResponse, error) {
	if req == nil || req.GetCandidateId() == "" {
		return nil, status.Error(codes.InvalidArgument, "candidate_id is required: Turing writes the profile only on the authority of a proposal")
	}
	if strings.TrimSpace(req.GetContent()) == "" {
		return nil, status.Error(codes.InvalidArgument, "content is required")
	}
	candidate, err := s.repo.MemoryCandidateByID(ctx, req.GetCandidateId())
	if err != nil {
		return nil, memoryError(err, "read memory candidate failed")
	}
	document, err := s.repo.ApplyMemoryProfileCandidate(ctx, repository.ApplyMemoryProfileInput{
		CandidateID: candidate.CandidateID,
		// The profile's own compare-and-set, and the only place
		// expected_content_hash still means the profile document.
		ExpectedContentHash:   req.GetExpectedContentHash(),
		Content:               req.GetContent(),
		ExpectedCandidateHash: req.GetExpectedCandidateHash(),
	})
	if err != nil {
		if errors.Is(err, repository.ErrMemoryCandidateKind) || errors.Is(err, memoryfiles.ErrKind) {
			return nil, status.Error(codes.FailedPrecondition, "this is a belief; promote it instead")
		}
		return nil, memoryError(err, "apply memory profile failed")
	}
	s.record(ctx, "memory.profile.applied", candidate.CandidateID, map[string]any{"kind": candidate.Kind})
	return &turingv1.ApplyMemoryProfileResponse{Profile: &turingv1.MemoryProfile{
		Content:           document.Content,
		ContentHash:       document.ContentHash,
		Status:            turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_MANAGED,
		UnavailableReason: turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE,
		PinnedTruncated:   document.PinnedTruncated,
		PinnedBytes:       int32(document.PinnedBytes),
	}}, nil
}

// deprecatedPromoteHash and deprecatedRejectHash read the field this server
// asks clients to stop sending.
//
// Reading it is the point. A server is the one component that has to go on
// honouring a name it has deprecated, because the alternative is a client built
// before the split arriving with no compare-and-set at all and having its user's
// decision applied to text they never read. The linter is told so here, in one
// place, rather than at every call site.
func deprecatedPromoteHash(req *turingv1.PromoteMemoryCandidateRequest) string {
	return req.GetExpectedContentHash() //nolint:staticcheck // deliberately still honoured; see above
}

func deprecatedRejectHash(req *turingv1.RejectMemoryCandidateRequest) string {
	return req.GetExpectedContentHash() //nolint:staticcheck // deliberately still honoured; see above
}

// candidateCompareAndSet picks the token a decision is checked against.
//
// There is one candidate compare-and-set, and it names the candidate *file* as
// it reads now — the bytes every listing serves and the bytes the user was
// shown. expected_content_hash on a decision is the older spelling of the same
// question and is accepted as an alias, so a client built before the field was
// split is not left with no compare-and-set at all. It used to be checked
// against the database row instead, which is what made every proposal the user
// edited in their vault permanently undecidable: the page could only ever send
// the file's hash, and the row's was the only one that would be accepted.
func candidateCompareAndSet(candidateHash string, deprecatedRowHash string) string {
	if candidateHash != "" {
		return candidateHash
	}
	return deprecatedRowHash
}

func (s *Server) profile(ctx context.Context, settings *turingv1.MemorySettings) *turingv1.MemoryProfile {
	document, reason, detail := s.editableDocument(ctx, settings, func(ctx context.Context) memoryfiles.EditableDocument {
		return s.vault.EditableProfile(ctx)
	})
	return &turingv1.MemoryProfile{
		Content:           document.Content,
		ContentHash:       document.ContentHash,
		Status:            turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_MANAGED,
		ParseError:        detail,
		UnavailableReason: reason,
		PinnedTruncated:   document.PinnedTruncated,
		PinnedBytes:       int32(document.PinnedBytes),
	}
}

// tiers reports the three tiers memory has, each with what it holds and why it
// could not be read when that is the answer.
//
// The pinned rows are derived from the documents the response already carries
// rather than read a second time: two reads of the same file can disagree, and
// a tier row saying "no persona" beside a persona row holding one is exactly
// the kind of disagreement a user cannot resolve.
func (s *Server) tiers(
	settings *turingv1.MemorySettings,
	view vaultView,
	profile *turingv1.MemoryProfile,
	persona *turingv1.MemoryPersona,
) []*turingv1.MemoryTierState {
	beliefCandidates, profileCandidates := 0, 0
	for _, candidate := range view.candidates {
		switch candidate.GetKind() {
		case turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_BELIEF:
			beliefCandidates++
		case turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_PROFILE_EDIT:
			profileCandidates++
		}
	}
	personaTier := &turingv1.MemoryTierState{
		Tier:              turingv1.MemoryTier_MEMORY_TIER_PERSONA,
		Enabled:           settings.GetEnabled(),
		UnavailableReason: persona.GetUnavailableReason(),
		ParseError:        persona.GetParseError(),
	}
	if strings.TrimSpace(persona.GetContent()) != "" {
		personaTier.NoteCount = 1
	}
	profileTier := &turingv1.MemoryTierState{
		Tier:                  turingv1.MemoryTier_MEMORY_TIER_PROFILE,
		Enabled:               settings.GetEnabled(),
		PendingCandidateCount: int32(profileCandidates),
		UnavailableReason:     profile.GetUnavailableReason(),
		ParseError:            profile.GetParseError(),
	}
	// Both rows count what memory actually holds, which is what survives
	// trimming. The documents now arrive whole rather than pre-trimmed by the
	// pinned projection, so the trim has to happen here or a profile holding
	// nothing but whitespace would be counted as a profile.
	if strings.TrimSpace(profile.GetContent()) != "" {
		profileTier.NoteCount = 1
	}
	beliefTier := &turingv1.MemoryTierState{
		Tier:                  turingv1.MemoryTier_MEMORY_TIER_BELIEF,
		Enabled:               settings.GetEnabled(),
		NoteCount:             int32(view.beliefs),
		PendingCandidateCount: int32(beliefCandidates),
		UnavailableReason:     view.reason,
		ParseError:            view.detail,
	}
	if !settings.GetEnabled() && view.reason == turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE {
		// Memory being off is why this tier is idle, unless the vault has a
		// problem of its own — in which case the tier is where that problem is
		// visible, since the settings row is busy saying DISABLED.
		beliefTier.UnavailableReason = turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED
	}
	return []*turingv1.MemoryTierState{personaTier, profileTier, beliefTier}
}

func candidateProto(candidate repository.MemoryCandidate) *turingv1.MemoryCandidate {
	proposal := &turingv1.MemoryCandidate{
		CandidateId:       candidate.CandidateID,
		Kind:              candidateKindProto(candidate.Kind),
		InboxPath:         candidate.InboxPath,
		Content:           candidate.Body,
		ContentHash:       candidate.ContentHash,
		State:             candidateStateProto(candidate.State),
		PromotedNoteId:    candidate.PromotedNoteID,
		CreatedAt:         timestampProto(candidate.CreatedAt),
		UpdatedAt:         timestampProto(candidate.UpdatedAt),
		DecidedAt:         timestampProto(candidate.DecidedAt),
		Managed:           true,
		UnavailableReason: turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE,
		Provenance:        make([]*turingv1.MemoryProvenance, 0, len(candidate.EvidenceRefs)),
	}
	withdrawn := candidate.State == repository.MemoryCandidateStateWithdrawn
	// The refs are server-derived: one per conversation the proposal was
	// written in. A withdrawn proposal is one whose conversation was deleted,
	// so what it rests on now is nothing, and the count says nothing rather
	// than repeating the one it used to have.
	evidenceCount := int32(1)
	if withdrawn {
		evidenceCount = 0
	}
	for _, ref := range candidate.EvidenceRefs {
		proposal.Provenance = append(proposal.Provenance, &turingv1.MemoryProvenance{
			Kind:            turingv1.MemoryProvenanceKind_MEMORY_PROVENANCE_KIND_PROMOTED_FROM_CANDIDATE,
			SourceSessionId: ref,
			Withdrawn:       withdrawn,
			WithdrawnAt:     timestampProto(candidate.DecidedAt),
			EvidenceCount:   evidenceCount,
		})
	}
	// Withdrawn with no refs left would arrive looking like a proposal that
	// never had a source at all. Say the withdrawal instead.
	if withdrawn && len(proposal.Provenance) == 0 {
		proposal.Provenance = append(proposal.Provenance, &turingv1.MemoryProvenance{
			Kind:          turingv1.MemoryProvenanceKind_MEMORY_PROVENANCE_KIND_PROMOTED_FROM_CANDIDATE,
			Withdrawn:     true,
			WithdrawnAt:   timestampProto(candidate.DecidedAt),
			EvidenceCount: 0,
		})
	}
	return proposal
}

func candidateKindProto(kind string) turingv1.MemoryCandidateKind {
	switch kind {
	case repository.MemoryCandidateKindBelief:
		return turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_BELIEF
	case repository.MemoryCandidateKindProfileEdit:
		return turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_PROFILE_EDIT
	default:
		return turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_UNSPECIFIED
	}
}

func candidateStateProto(state string) turingv1.MemoryCandidateState {
	switch state {
	case repository.MemoryCandidateStatePending:
		return turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PENDING
	case repository.MemoryCandidateStatePromoted:
		return turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PROMOTED
	case repository.MemoryCandidateStateRejected:
		return turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_REJECTED
	case repository.MemoryCandidateStateWithdrawn:
		return turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_WITHDRAWN
	default:
		return turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_UNSPECIFIED
	}
}

func candidateStateName(state turingv1.MemoryCandidateState) (string, error) {
	switch state {
	case turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_UNSPECIFIED:
		return "", nil
	case turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PENDING:
		return repository.MemoryCandidateStatePending, nil
	case turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PROMOTED:
		return repository.MemoryCandidateStatePromoted, nil
	case turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_REJECTED:
		return repository.MemoryCandidateStateRejected, nil
	case turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_WITHDRAWN:
		return repository.MemoryCandidateStateWithdrawn, nil
	default:
		return "", status.Error(codes.InvalidArgument, "that is not a memory candidate state")
	}
}

func noteStatusProto(noteStatus string) turingv1.MemoryNoteStatus {
	switch noteStatus {
	case repository.MemoryNoteStatusManaged:
		return turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_MANAGED
	case repository.MemoryNoteStatusUnmanaged:
		return turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_UNMANAGED
	case repository.MemoryNoteStatusWithdrawn:
		return turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_WITHDRAWN
	default:
		return turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_UNSPECIFIED
	}
}

// unavailableProto never answers NONE for a document that was not read. NONE is
// the server saying "nothing is wrong", and saying that about an empty vault is
// how a client renders a failure as a healthy, empty page.
func unavailableProto(reason memoryfiles.UnavailableReason, available bool) turingv1.MemoryUnavailableReason {
	switch reason {
	case memoryfiles.UnavailableVaultMissing:
		return turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING
	case memoryfiles.UnavailableVaultUnreadable:
		return turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_UNREADABLE
	case memoryfiles.UnavailableContentParse:
		return turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED
	case memoryfiles.UnavailableContentTooLarge:
		return turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_CONTENT_TOO_LARGE
	default:
		if !available {
			return turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_UNREADABLE
		}
		return turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE
	}
}

func timestampProto(value string) *timestamppb.Timestamp {
	if value == "" {
		return nil
	}
	// A row this server itself wrote parses canonically; a row written by an
	// older build is one of the legacy shapes. A timestamp that parses as
	// neither is left unset rather than guessed at.
	parsed, err := persisttime.ParseCanonical(value)
	if err != nil {
		parsed, err = persisttime.ParseLegacy(value)
		if err != nil {
			return nil
		}
	}
	return timestamppb.New(parsed)
}
