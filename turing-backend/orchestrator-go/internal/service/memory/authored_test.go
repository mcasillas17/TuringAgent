package memory

import (
	"context"
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

// The persona is the user's own instruction about who Turing is. These tests
// pin the two halves of that claim: the user can write it through the public
// surface, and nothing an agent can reach ever can.

func TestMemoryPersonaIsReadAndSavedByTheUserUnderCompareAndSet(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)

	empty, err := service.GetMemoryPersona(ctx, &turingv1.GetMemoryPersonaRequest{})
	if err != nil {
		t.Fatalf("GetMemoryPersona: %v", err)
	}
	if empty.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING {
		t.Fatalf("absent persona reason = %v, want VAULT_MISSING rather than a healthy empty document", empty.GetUnavailableReason())
	}
	if empty.GetStatus() != turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_UNMANAGED {
		t.Fatalf("persona status = %v, want UNMANAGED: Turing never rewrites it", empty.GetStatus())
	}

	saved, err := service.SaveMemoryPersona(ctx, &turingv1.SaveMemoryPersonaRequest{
		Content: "# Persona\n\nBe direct.\n",
	})
	if err != nil {
		t.Fatalf("SaveMemoryPersona: %v", err)
	}
	if saved.GetPersona().GetContent() != "# Persona\n\nBe direct.\n" {
		t.Fatalf("saved persona = %q", saved.GetPersona().GetContent())
	}
	if saved.GetPersona().GetContentHash() == "" {
		t.Fatal("the save returned no compare-and-set token for the next edit")
	}
	if saved.GetPersona().GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE {
		t.Fatalf("saved persona reason = %v, want NONE", saved.GetPersona().GetUnavailableReason())
	}
	onDisk, err := os.ReadFile(filepath.Join(vault.Root(), memoryfiles.PersonaFileName))
	if err != nil {
		t.Fatalf("read persona: %v", err)
	}
	if string(onDisk) != "# Persona\n\nBe direct.\n" {
		t.Fatalf("persona.md = %q", onDisk)
	}

	read, err := service.GetMemoryPersona(ctx, &turingv1.GetMemoryPersonaRequest{})
	if err != nil {
		t.Fatalf("GetMemoryPersona after save: %v", err)
	}
	if read.GetContent() != "# Persona\n\nBe direct.\n" || read.GetContentHash() != saved.GetPersona().GetContentHash() {
		t.Fatalf("re-read persona = %+v, want the saved document and hash", read)
	}
}

// The wording matters as much as the refusal: the user is holding this file
// open in Obsidian, and the only useful instruction is to close it, re-read,
// and save again. Nothing is overwritten and nothing is re-prepared silently.
func TestMemoryPersonaSaveRefusesAStaleHashWithTheReReadWording(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)
	if _, err := service.SaveMemoryPersona(ctx, &turingv1.SaveMemoryPersonaRequest{
		Content: "first\n",
	}); err != nil {
		t.Fatalf("seed persona: %v", err)
	}
	// The user edits the same file in their own editor.
	if err := os.WriteFile(filepath.Join(vault.Root(), memoryfiles.PersonaFileName), []byte("edited in obsidian\n"), 0o600); err != nil {
		t.Fatalf("simulate an editor write: %v", err)
	}

	_, err := service.SaveMemoryPersona(ctx, &turingv1.SaveMemoryPersonaRequest{
		Content:             "second\n",
		ExpectedContentHash: memoryfiles.ContentHash("first\n"),
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale persona save error = %v, want FailedPrecondition", err)
	}
	message := status.Convert(err).Message()
	if !strings.Contains(message, "the file changed") || !strings.Contains(message, "re-read") {
		t.Fatalf("refusal %q does not tell the user the file changed and to re-read it", message)
	}
	onDisk, readErr := os.ReadFile(filepath.Join(vault.Root(), memoryfiles.PersonaFileName))
	if readErr != nil {
		t.Fatalf("read persona: %v", readErr)
	}
	if string(onDisk) != "edited in obsidian\n" {
		t.Fatalf("a refused save overwrote the user's text: %q", onDisk)
	}
}

func TestMemoryProfileIsSavedByHandWithoutAProposal(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)

	saved, err := service.SaveMemoryProfile(ctx, &turingv1.SaveMemoryProfileRequest{
		Content: "# Profile\n\nI bike to work.\n",
	})
	if err != nil {
		t.Fatalf("SaveMemoryProfile: %v", err)
	}
	if saved.GetProfile().GetContent() != "# Profile\n\nI bike to work.\n" {
		t.Fatalf("saved profile = %q", saved.GetProfile().GetContent())
	}
	onDisk, err := os.ReadFile(filepath.Join(vault.Root(), memoryfiles.ProfileFileName))
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if string(onDisk) != "# Profile\n\nI bike to work.\n" {
		t.Fatalf("profile.md = %q", onDisk)
	}

	_, err = service.SaveMemoryProfile(ctx, &turingv1.SaveMemoryProfileRequest{
		Content:             "# Profile\n\nOverwritten.\n",
		ExpectedContentHash: "sha256:stale",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale profile save error = %v, want FailedPrecondition", err)
	}
	after, err := os.ReadFile(filepath.Join(vault.Root(), memoryfiles.ProfileFileName))
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if string(after) != "# Profile\n\nI bike to work.\n" {
		t.Fatalf("a refused save overwrote the profile: %q", after)
	}
}

// A candidate is the agent's authority to touch profile.md. The hand save is
// the user's, and it must not consume, decide, or otherwise disturb a proposal
// the user has not looked at yet.
func TestMemoryProfileHandSaveLeavesPendingProposalsAlone(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	_, sessionID := newRun(t, repo, ctx)
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindProfileEdit,
		Title: "Profile", Body: "Prefers short answers.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	if _, err := service.SaveMemoryProfile(ctx, &turingv1.SaveMemoryProfileRequest{
		Content: "# Profile\n\nWritten by hand.\n",
	}); err != nil {
		t.Fatalf("SaveMemoryProfile: %v", err)
	}

	if _, err := repo.MemoryCandidateByID(ctx, candidate.CandidateID); err != nil {
		t.Fatalf("the pending proposal was disturbed by a hand save: %v", err)
	}
}

func TestMemoryDocumentSavesRefuseEmptyContent(t *testing.T) {
	service, _, _, ctx := newMemoryService(t)

	if _, err := service.SaveMemoryPersona(ctx, &turingv1.SaveMemoryPersonaRequest{Content: "   \n"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("blank persona save error = %v, want InvalidArgument", err)
	}
	if _, err := service.SaveMemoryProfile(ctx, &turingv1.SaveMemoryProfileRequest{Content: ""}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("blank profile save error = %v, want InvalidArgument", err)
	}
}

// The trail says a document was saved and never what it said. persona.md and
// profile.md are the two most personal files in the vault; an audit row
// carrying their text would be a second, unredacted copy of them.
func TestMemoryDocumentSavesAreAuditedWithoutTheirContent(t *testing.T) {
	recorder := &recordingAudit{}
	service, _, _, ctx := newMemoryServiceAt(t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), recorder)

	if _, err := service.SaveMemoryPersona(ctx, &turingv1.SaveMemoryPersonaRequest{
		Content: "# Persona\n\nSecret instruction about my employer.\n",
	}); err != nil {
		t.Fatalf("SaveMemoryPersona: %v", err)
	}
	if _, err := service.SaveMemoryProfile(ctx, &turingv1.SaveMemoryProfileRequest{
		Content: "# Profile\n\nMy home address is on Elm Street.\n",
	}); err != nil {
		t.Fatalf("SaveMemoryProfile: %v", err)
	}

	trail := recorder.text()
	for _, action := range []string{"memory.persona.saved", "memory.profile.saved"} {
		if !strings.Contains(trail, action) {
			t.Fatalf("trail %q does not record %s", trail, action)
		}
	}
	for _, leaked := range []string{"Secret instruction", "Elm Street"} {
		if strings.Contains(trail, leaked) {
			t.Fatalf("the audit trail carries document content: %q", trail)
		}
	}
}

// Every save lands in ListMemoryState, so a page that re-reads after saving
// shows what the server now holds rather than what the editor hoped for.
func TestMemoryStateCarriesThePersonaDocument(t *testing.T) {
	service, _, _, ctx := newMemoryService(t)
	if _, err := service.SaveMemoryPersona(ctx, &turingv1.SaveMemoryPersonaRequest{
		Content: "# Persona\n\nBe direct.\n",
	}); err != nil {
		t.Fatalf("SaveMemoryPersona: %v", err)
	}

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	if state.GetPersona().GetContent() != "# Persona\n\nBe direct.\n" {
		t.Fatalf("state persona = %q, want the saved document", state.GetPersona().GetContent())
	}
	if state.GetPersona().GetContentHash() == "" {
		t.Fatal("state persona carries no compare-and-set token, so an editor cannot save safely")
	}
	if state.GetPersona().GetStatus() != turingv1.MemoryNoteStatus_MEMORY_NOTE_STATUS_UNMANAGED {
		t.Fatalf("state persona status = %v, want UNMANAGED", state.GetPersona().GetStatus())
	}
}

func TestMemoryStateReportsWhyAPinnedDocumentCouldNotBeRead(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(outside, []byte("not the vault's"), 0o600); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(vault.Root(), memoryfiles.PersonaFileName)); err != nil {
		t.Fatalf("plant symlink: %v", err)
	}

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	persona := state.GetPersona()
	if persona.GetUnavailableReason() == turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_NONE {
		t.Fatal("a symlinked persona.md was reported as a healthy document")
	}
	if persona.GetContent() != "" {
		t.Fatalf("a symlinked persona.md served content: %q", persona.GetContent())
	}
	if persona.GetParseError() == "" {
		t.Fatal("the unreadable persona row carries no detail for the user to act on")
	}
}

// Memory being off hides the tools, not the vault: the user can still read and
// repair what is on disk, which is the whole point of a memory made of files.
func TestMemoryDocumentsStayInspectableWhileMemoryIsOff(t *testing.T) {
	service, _, _, ctx := newMemoryService(t)
	if _, err := service.SaveMemoryPersona(ctx, &turingv1.SaveMemoryPersonaRequest{
		Content: "# Persona\n\nStill readable.\n",
	}); err != nil {
		t.Fatalf("SaveMemoryPersona: %v", err)
	}
	if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: false}); err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}

	persona, err := service.GetMemoryPersona(ctx, &turingv1.GetMemoryPersonaRequest{})
	if err != nil {
		t.Fatalf("GetMemoryPersona while off: %v", err)
	}
	if persona.GetContent() != "# Persona\n\nStill readable.\n" {
		t.Fatalf("persona while memory is off = %q, want the document as it stands", persona.GetContent())
	}
	if persona.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED {
		t.Fatalf("persona reason while off = %v, want DISABLED stated plainly", persona.GetUnavailableReason())
	}
}

// The headline invariant of Tier 1, asserted at the facet that guards it: the
// runtime identity reaches the internal server, and the internal server has no
// persona surface at all.
func TestMemoryInternalFacetCannotReachTheUsersDocuments(t *testing.T) {
	service, _, _, ctx := newMemoryService(t)
	internal := NewInternalServer(service)
	public := NewPublicServer(service)

	userCalls := map[string]func(context.Context) error{
		"GetMemoryPersona": func(ctx context.Context) error {
			_, err := internal.GetMemoryPersona(ctx, &turingv1.GetMemoryPersonaRequest{})
			return err
		},
		"SaveMemoryPersona": func(ctx context.Context) error {
			_, err := internal.SaveMemoryPersona(ctx, &turingv1.SaveMemoryPersonaRequest{Content: "agent authored"})
			return err
		},
		"SaveMemoryProfile": func(ctx context.Context) error {
			_, err := internal.SaveMemoryProfile(ctx, &turingv1.SaveMemoryProfileRequest{Content: "agent authored"})
			return err
		},
	}
	for name, call := range userCalls {
		if err := call(ctx); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("internal %s error = %v, want PermissionDenied", name, err)
		}
	}

	if _, err := public.GetMemoryPersona(ctx, &turingv1.GetMemoryPersonaRequest{}); err != nil {
		t.Fatalf("public GetMemoryPersona: %v", err)
	}
	if _, err := public.SaveMemoryPersona(ctx, &turingv1.SaveMemoryPersonaRequest{Content: "user authored\n"}); err != nil {
		t.Fatalf("public SaveMemoryPersona: %v", err)
	}
	if _, err := public.SaveMemoryProfile(ctx, &turingv1.SaveMemoryProfileRequest{Content: "user authored\n"}); err != nil {
		t.Fatalf("public SaveMemoryProfile: %v", err)
	}
}

// The tier row and the document row are two views of one read. A persona that
// exists but is idle has to count as present on both, or the tier row reads as
// "there is no persona" when the truth is "there is one and it is not in use".
func TestMemoryPersonaTierCountsTheDocumentEvenWhileMemoryIsOff(t *testing.T) {
	service, _, _, ctx := newMemoryService(t)
	if _, err := service.SaveMemoryPersona(ctx, &turingv1.SaveMemoryPersonaRequest{
		Content: "# Persona\n\nBe direct.\n",
	}); err != nil {
		t.Fatalf("SaveMemoryPersona: %v", err)
	}
	if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: false}); err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	var persona *turingv1.MemoryTierState
	for _, tier := range state.GetTiers() {
		if tier.GetTier() == turingv1.MemoryTier_MEMORY_TIER_PERSONA {
			persona = tier
		}
	}
	if persona == nil {
		t.Fatal("no persona tier row")
	}
	if persona.GetNoteCount() != 1 {
		t.Fatalf("persona tier note count = %d, want the document counted", persona.GetNoteCount())
	}
	if persona.GetEnabled() {
		t.Fatal("persona tier reports itself enabled while memory is off")
	}
	if persona.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED {
		t.Fatalf("persona tier reason = %v, want DISABLED", persona.GetUnavailableReason())
	}
}
