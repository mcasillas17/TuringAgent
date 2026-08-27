package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The split is the whole point of having two facets: a client holding the
// public key can never discover or run a memory tool, and the runtime holding
// the internal token can never read or decide the user's memory for them.
func TestMemoryFacetsRefuseEachOthersCalls(t *testing.T) {
	service, _, _, ctx := newMemoryService(t)
	public := NewPublicServer(service)
	internal := NewInternalServer(service)

	if _, err := public.ListMemoryTools(ctx, &turingv1.ListMemoryToolsRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public ListMemoryTools error = %v, want PermissionDenied", err)
	}
	if _, err := public.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public CallMemoryTool error = %v, want PermissionDenied", err)
	}

	internalCalls := map[string]func() error{
		"ListMemoryState": func() error {
			_, err := internal.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
			return err
		},
		"GetMemorySettings": func() error {
			_, err := internal.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{})
			return err
		},
		"SetMemoryEnabled": func() error {
			_, err := internal.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{})
			return err
		},
		"ListMemoryCandidates": func() error {
			_, err := internal.ListMemoryCandidates(ctx, &turingv1.ListMemoryCandidatesRequest{})
			return err
		},
		"GetMemoryCandidate": func() error {
			_, err := internal.GetMemoryCandidate(ctx, &turingv1.GetMemoryCandidateRequest{})
			return err
		},
		"PromoteMemoryCandidate": func() error {
			_, err := internal.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{})
			return err
		},
		"RejectMemoryCandidate": func() error {
			_, err := internal.RejectMemoryCandidate(ctx, &turingv1.RejectMemoryCandidateRequest{})
			return err
		},
		"GetMemoryProfile": func() error {
			_, err := internal.GetMemoryProfile(ctx, &turingv1.GetMemoryProfileRequest{})
			return err
		},
		"ApplyMemoryProfile": func() error {
			_, err := internal.ApplyMemoryProfile(ctx, &turingv1.ApplyMemoryProfileRequest{})
			return err
		},
	}
	for name, call := range internalCalls {
		if err := call(); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("internal %s error = %v, want PermissionDenied", name, err)
		}
	}

	// And the facets do forward what they own.
	if _, err := public.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{}); err != nil {
		t.Fatalf("public GetMemorySettings: %v", err)
	}
	if _, err := internal.ListMemoryTools(ctx, &turingv1.ListMemoryToolsRequest{}); err != nil {
		t.Fatalf("internal ListMemoryTools: %v", err)
	}
}

func TestMemoryCandidateListingShowsTheWholeProposal(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	_, sessionID := newRun(t, repo, ctx)
	const body = "They prefer being called Sam, not Samuel."
	created, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindBelief, Title: "Name", Body: body,
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	listed, err := service.ListMemoryCandidates(ctx, &turingv1.ListMemoryCandidatesRequest{})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	if len(listed.GetCandidates()) != 1 {
		t.Fatalf("candidates = %d, want the one waiting proposal", len(listed.GetCandidates()))
	}
	candidate := listed.GetCandidates()[0]
	if candidate.GetContent() != body {
		t.Fatalf("candidate content = %q, want the whole proposal", candidate.GetContent())
	}
	if !candidate.GetManaged() {
		t.Fatal("a candidate Turing wrote is not marked managed")
	}
	if candidate.GetState() != turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PENDING {
		t.Fatalf("candidate state = %v, want PENDING", candidate.GetState())
	}
	if candidate.GetKind() != turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_BELIEF {
		t.Fatalf("candidate kind = %v, want BELIEF", candidate.GetKind())
	}
	if len(candidate.GetProvenance()) == 0 || candidate.GetProvenance()[0].GetSourceSessionId() != sessionID {
		t.Fatalf("candidate provenance = %v, want the session it came from", candidate.GetProvenance())
	}

	fetched, err := service.GetMemoryCandidate(ctx, &turingv1.GetMemoryCandidateRequest{CandidateId: created.CandidateID})
	if err != nil {
		t.Fatalf("GetMemoryCandidate: %v", err)
	}
	if fetched.GetContent() != body {
		t.Fatalf("fetched content = %q, want the whole proposal", fetched.GetContent())
	}
	if _, err := service.GetMemoryCandidate(ctx, &turingv1.GetMemoryCandidateRequest{
		CandidateId: "memcand_missing",
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown candidate error = %v, want NotFound", err)
	}
}

func TestMemoryPromotionAcceptsOnlyABeliefAndOnlyTheTextTheUserRead(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	_, sessionID := newRun(t, repo, ctx)
	belief, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindBelief,
		Title: "Coffee", Body: "They take their coffee black.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	profileEdit, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindProfileEdit,
		Title: "Profile", Body: "Prefers short answers.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	// A decision composed against text that has since changed is refused. The
	// token names the candidate file, so the refusal is the file's — the same
	// FailedPrecondition every other decision about moved bytes answers with.
	if _, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId: belief.CandidateID, ExpectedContentHash: "sha256:stale",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale promotion error = %v, want FailedPrecondition", err)
	}
	// A profile edit is not a belief and is never promoted into beliefs/.
	if _, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId: profileEdit.CandidateID, ExpectedContentHash: profileEdit.ContentHash,
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("profile-edit promotion error = %v, want FailedPrecondition", err)
	}
	// Silently discarding an edit the user typed would be worse than refusing it.
	if _, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId: belief.CandidateID, ExpectedContentHash: belief.ContentHash, EditedContent: "changed",
	}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("edited promotion error = %v, want Unimplemented", err)
	}

	promoted, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId: belief.CandidateID, ExpectedContentHash: belief.ContentHash,
		TargetTier: turingv1.MemoryTier_MEMORY_TIER_BELIEF,
	})
	if err != nil {
		t.Fatalf("PromoteMemoryCandidate: %v", err)
	}
	if promoted.GetNote().GetNoteId() == "" || !strings.HasPrefix(promoted.GetNote().GetPath(), memoryfiles.BeliefsDirName+"/") {
		t.Fatalf("promoted note = %+v, want a belief in beliefs/", promoted.GetNote())
	}
	if promoted.GetNote().GetStatus() != turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_MANAGED {
		t.Fatalf("promoted status = %v, want MANAGED", promoted.GetNote().GetStatus())
	}
	if promoted.GetCandidate().GetState() != turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PROMOTED {
		t.Fatalf("candidate state = %v, want PROMOTED", promoted.GetCandidate().GetState())
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), promoted.GetNote().GetPath())); err != nil {
		t.Fatalf("promoted belief is not on disk: %v", err)
	}
	// A decided candidate is never reopened.
	if _, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId: belief.CandidateID, ExpectedContentHash: belief.ContentHash,
	}); status.Code(err) != codes.NotFound && status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("second promotion error = %v, want a refusal", err)
	}
}

func TestMemoryRejectionRemovesBothTheRowAndTheFile(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	_, sessionID := newRun(t, repo, ctx)
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindBelief,
		Title: "Wrong", Body: "They hate coffee.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	if _, err := service.RejectMemoryCandidate(ctx, &turingv1.RejectMemoryCandidateRequest{
		CandidateId: candidate.CandidateID, ExpectedContentHash: "sha256:stale",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale rejection error = %v, want FailedPrecondition", err)
	}

	rejected, err := service.RejectMemoryCandidate(ctx, &turingv1.RejectMemoryCandidateRequest{
		CandidateId: candidate.CandidateID, ExpectedContentHash: candidate.ContentHash, Reason: "not true",
	})
	if err != nil {
		t.Fatalf("RejectMemoryCandidate: %v", err)
	}
	if rejected.GetCandidate().GetState() != turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_REJECTED {
		t.Fatalf("state = %v, want REJECTED", rejected.GetCandidate().GetState())
	}
	if _, err := repo.MemoryCandidateByID(ctx, candidate.CandidateID); err == nil {
		t.Fatal("the rejected candidate row survived")
	}
	if _, err := os.Stat(filepath.Join(vault.Root(), candidate.InboxPath)); !os.IsNotExist(err) {
		t.Fatalf("the rejected inbox file survived: %v", err)
	}
}

func TestMemoryProfileEditIsAppliedUnderCompareAndSet(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	_, sessionID := newRun(t, repo, ctx)
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindProfileEdit,
		Title: "Profile", Body: "Prefers short answers.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	profile, err := service.GetMemoryProfile(ctx, &turingv1.GetMemoryProfileRequest{})
	if err != nil {
		t.Fatalf("GetMemoryProfile: %v", err)
	}
	if profile.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING {
		t.Fatalf("empty profile reason = %v, want VAULT_MISSING rather than a healthy empty document", profile.GetUnavailableReason())
	}

	// The expected hash is compare-and-set against the profile, so an edit
	// composed against a profile that has since moved cannot land.
	if _, err := service.ApplyMemoryProfile(ctx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId: candidate.CandidateID, Content: "Prefers short answers.",
		ExpectedContentHash: "sha256:stale",
	}); status.Code(err) != codes.Aborted {
		t.Fatalf("stale profile apply error = %v, want Aborted", err)
	}

	applied, err := service.ApplyMemoryProfile(ctx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId: candidate.CandidateID, Content: "Prefers short answers.",
	})
	if err != nil {
		t.Fatalf("ApplyMemoryProfile: %v", err)
	}
	if applied.GetProfile().GetContent() != "Prefers short answers." {
		t.Fatalf("profile = %q, want the accepted text", applied.GetProfile().GetContent())
	}
	written, err := os.ReadFile(filepath.Join(vault.Root(), memoryfiles.ProfileFileName))
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if string(written) != "Prefers short answers." {
		t.Fatalf("profile.md = %q, want the accepted text", written)
	}
	if _, err := repo.MemoryCandidateByID(ctx, candidate.CandidateID); err == nil {
		t.Fatal("the applied profile-edit candidate survived")
	}
	if _, err := service.ApplyMemoryProfile(ctx, &turingv1.ApplyMemoryProfileRequest{
		Content: "no authority",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("candidate-less profile apply error = %v, want InvalidArgument", err)
	}
}

// A draft the user dropped into the inbox themselves is theirs. It shows up so
// they know Turing can see it, it is marked unmanaged, and there is no RPC that
// promotes it — moving the file is the only way in.
func TestMemoryStateListsUnmanagedInboxDraftsWithNoWayToPromoteThem(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	draft := filepath.Join(vault.Root(), memoryfiles.InboxDirName, "my-own-note.md")
	if err := os.WriteFile(draft, []byte("I want Turing to know I bike to work.\n"), 0o600); err != nil {
		t.Fatalf("write draft: %v", err)
	}

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	var found *turingv1.MemoryCandidate
	for _, candidate := range state.GetCandidates() {
		if strings.HasSuffix(candidate.GetInboxPath(), "my-own-note.md") {
			found = candidate
		}
	}
	if found == nil {
		t.Fatalf("candidates = %+v, want the user's own draft listed", state.GetCandidates())
	}
	if found.GetManaged() {
		t.Fatal("a draft the user wrote is marked as Turing's to rewrite")
	}
	if found.GetCandidateId() != "" {
		t.Fatalf("unmanaged draft carries candidate id %q; it has no row", found.GetCandidateId())
	}
	if !strings.Contains(found.GetContent(), "bike to work") {
		t.Fatalf("unmanaged draft content = %q, want the user's own text", found.GetContent())
	}
	// There is no promotion path for it, by identity or by path.
	if _, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId: found.GetInboxPath(), ExpectedContentHash: found.GetContentHash(),
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("promoting an unmanaged draft error = %v, want NotFound", err)
	}
	if _, err := repo.MemoryCandidateByID(ctx, found.GetInboxPath()); err == nil {
		t.Fatal("an unmanaged draft acquired a candidate row")
	}
	// Still on disk, untouched.
	if _, err := os.Stat(draft); err != nil {
		t.Fatalf("the user's draft was moved or removed: %v", err)
	}
}

func TestMemoryStateReportsSettingsTiersAndNotes(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	_, sessionID := newRun(t, repo, ctx)
	note := mustPromoteBelief(t, repo, ctx, sessionID, "Coffee", "They take their coffee black.")

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	if !state.GetSettings().GetEnabled() {
		t.Fatal("memory does not ship enabled")
	}
	if state.GetSettings().GetVaultRoot() != vault.Root() {
		t.Fatalf("vault root = %q, want %q", state.GetSettings().GetVaultRoot(), vault.Root())
	}
	if !state.GetSettings().GetVaultWritable() {
		t.Fatal("a writable vault is reported as read-only")
	}
	if state.GetSettings().GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE {
		t.Fatalf("settings reason = %v, want NONE", state.GetSettings().GetUnavailableReason())
	}

	var beliefTier *turingv1.MemoryTierState
	for _, tier := range state.GetTiers() {
		if tier.GetTier() == turingv1.MemoryTier_MEMORY_TIER_BELIEF {
			beliefTier = tier
		}
	}
	if beliefTier == nil {
		t.Fatalf("tiers = %+v, want a belief row", state.GetTiers())
	}
	if beliefTier.GetNoteCount() != 1 || !beliefTier.GetEnabled() {
		t.Fatalf("belief tier = %+v, want one enabled note", beliefTier)
	}

	if len(state.GetNotes()) != 1 || state.GetNotes()[0].GetNoteId() != note.NoteID {
		t.Fatalf("notes = %+v, want the one promoted belief", state.GetNotes())
	}
	if !strings.Contains(state.GetNotes()[0].GetContent(), "coffee black") {
		t.Fatalf("note content = %q, want the file's own text", state.GetNotes()[0].GetContent())
	}
	if state.GetNotes()[0].GetTier() != turingv1.MemoryTier_MEMORY_TIER_BELIEF {
		t.Fatalf("note tier = %v, want BELIEF", state.GetNotes()[0].GetTier())
	}
}

// Turning memory off stops the tools. It does not erase what the user has
// already accepted, and turning it back on must find it all there.
func TestMemoryToggleOffLeavesTheVaultIntact(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	_, sessionID := newRun(t, repo, ctx)
	mustPromoteBelief(t, repo, ctx, sessionID, "Coffee", "They take their coffee black.")

	if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: false}); err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}
	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	if state.GetSettings().GetEnabled() {
		t.Fatal("settings report memory as on after it was turned off")
	}
	if len(state.GetNotes()) != 1 {
		t.Fatalf("notes = %d, want turning memory off to keep what the user accepted", len(state.GetNotes()))
	}
	if state.GetSettings().GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED {
		t.Fatalf("settings reason = %v, want DISABLED", state.GetSettings().GetUnavailableReason())
	}
}

// A per-tier toggle is not something this build can honour, so it is refused
// rather than accepted and quietly applied to memory as a whole.
func TestMemorySetEnabledRefusesAPerTierToggleItCannotHonour(t *testing.T) {
	service, _, _, ctx := newMemoryService(t)
	if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{
		Enabled: false, Tier: turingv1.MemoryTier_MEMORY_TIER_BELIEF,
	}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("per-tier toggle error = %v, want Unimplemented", err)
	}
	settings, err := service.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{})
	if err != nil || !settings.GetEnabled() {
		t.Fatalf("settings = %+v err=%v, want memory untouched", settings, err)
	}
}

// Opening the Memory page is the one read that may write. A belief the user
// wrote by hand has no identity until a pass gives it one, and only the
// reconcile pass may do that — so if the state RPC quietly downgraded to the
// read-only refresh the user's own note would sit there forever, visible but
// never adopted, and nothing would say why.
func TestMemoryStateRunsTheWritingPassThatAdoptsAHandWrittenBelief(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)
	handWritten := filepath.Join(vault.Root(), memoryfiles.BeliefsDirName, "their-own-belief.md")
	const original = "---\ntitle: Bikes\n---\n\nThey bike to work every day.\n"
	if err := os.WriteFile(handWritten, []byte(original), 0o600); err != nil {
		t.Fatalf("write hand-written belief: %v", err)
	}

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	after, err := os.ReadFile(handWritten)
	if err != nil {
		t.Fatalf("read hand-written belief: %v", err)
	}
	if string(after) == original {
		t.Fatal("ListMemoryState did not run the writing pass: the hand-written belief was never given an identity")
	}
	if !strings.Contains(string(after), "bike to work every day") {
		t.Fatalf("adoption rewrote the user's own words:\n%q", after)
	}
	var adopted *turingv1.MemoryNote
	for _, note := range state.GetNotes() {
		if strings.HasSuffix(note.GetPath(), "their-own-belief.md") {
			adopted = note
		}
	}
	if adopted == nil || adopted.GetNoteId() == "" {
		t.Fatalf("notes = %+v, want the adopted belief with a stable id", state.GetNotes())
	}
	if adopted.GetStatus() != turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_UNMANAGED {
		t.Fatalf("adopted status = %v, want UNMANAGED: the user wrote it and Turing does not own it", adopted.GetStatus())
	}
}
