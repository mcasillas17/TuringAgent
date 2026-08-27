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
)

// The Memory page is an editor over two files the user owns, and an editor has
// to be handed the file. What it used to receive was the runtime's pin: cut at
// the budget, with a synthetic notice appended in the document's own voice, and
// hashed after both. Every one of those is wrong for an editor — the text is
// not what the file says, the notice is words the user never typed, and the
// hash can never match the bytes a compare-and-set is verified against.
//
// The pin itself is unchanged. It is the preimage of the egress fingerprint,
// and this split exists so the editor can stop borrowing it.

const pinNoticeMarker = "Open the vault to read the rest"

func longDocument() string {
	return "# Persona\n\n" + strings.Repeat("be direct and concise. ", 400)
}

func TestGetMemoryPersonaServesTheWholeDocumentAndItsOwnHash(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)
	long := longDocument()
	if len(long) <= memoryfiles.MaxPersonaBytes {
		t.Fatalf("fixture is %d bytes; it must exceed the %d byte pin budget", len(long), memoryfiles.MaxPersonaBytes)
	}
	writeVaultDocument(t, vault, memoryfiles.PersonaFileName, long)

	read, err := service.GetMemoryPersona(ctx, &turingv1.GetMemoryPersonaRequest{})
	if err != nil {
		t.Fatalf("GetMemoryPersona: %v", err)
	}
	if read.GetContent() != long {
		t.Fatalf("persona content is %d bytes, want the whole %d byte document", len(read.GetContent()), len(long))
	}
	if strings.Contains(read.GetContent(), pinNoticeMarker) {
		t.Fatal("the editor was handed the runtime's truncation notice as if the user had written it")
	}
	if read.GetContentHash() != memoryfiles.ContentHash(long) {
		t.Fatal("the compare-and-set token is not a hash of the document, so a save can never match")
	}
	if !read.GetPinnedTruncated() {
		t.Fatal("the page has no way to tell the user a run only sees part of this")
	}
	if read.GetPinnedBytes() <= 0 || read.GetPinnedBytes() > int32(memoryfiles.MaxPersonaBytes) {
		t.Fatalf("pinned_bytes = %d, want the rune-safe cut at or below %d", read.GetPinnedBytes(), memoryfiles.MaxPersonaBytes)
	}
}

// The round trip is the bug in one test: read a long persona, edit it, save it.
// Under the shared hash the save was refused forever, and the refusal told the
// user to re-read a file that would hand back the same unusable token.
func TestALongPersonaReadThroughTheServiceCanBeSavedBack(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)
	long := longDocument()
	writeVaultDocument(t, vault, memoryfiles.PersonaFileName, long)

	read, err := service.GetMemoryPersona(ctx, &turingv1.GetMemoryPersonaRequest{})
	if err != nil {
		t.Fatalf("GetMemoryPersona: %v", err)
	}
	edited := read.GetContent() + "\nAnd never pad an answer.\n"
	saved, err := service.SaveMemoryPersona(ctx, &turingv1.SaveMemoryPersonaRequest{
		ExpectedContentHash: read.GetContentHash(),
		Content:             edited,
	})
	if err != nil {
		t.Fatalf("saving a long persona the page had just read: %v", err)
	}
	if saved.GetPersona().GetContentHash() != memoryfiles.ContentHash(edited) {
		t.Fatal("the save receipt handed back a token the next save cannot use")
	}
	if !saved.GetPersona().GetPinnedTruncated() {
		t.Fatal("the save receipt stopped saying the pin will be cut")
	}
	onDisk, readErr := os.ReadFile(filepath.Join(vault.Root(), memoryfiles.PersonaFileName))
	if readErr != nil {
		t.Fatalf("read persona: %v", readErr)
	}
	if string(onDisk) != edited {
		t.Fatalf("persona.md = %d bytes, want the %d bytes the editor saved", len(onDisk), len(edited))
	}
}

// Whitespace is a clear the user typed, and the file keeps those bytes. The pin
// is empty because nothing survives trimming, and an editor handed the pin's
// hash could never save over the file again.
func TestAWhitespaceOnlyProfileCanStillBeSavedOver(t *testing.T) {
	service, _, _, ctx := newMemoryService(t)

	seeded, err := service.SaveMemoryProfile(ctx, &turingv1.SaveMemoryProfileRequest{Content: "   \n\t\n"})
	if err != nil {
		t.Fatalf("seed a whitespace-only profile: %v", err)
	}
	if seeded.GetProfile().GetContentHash() != memoryfiles.ContentHash("   \n\t\n") {
		t.Fatal("the save receipt hashed the empty pin instead of the bytes it wrote")
	}

	read, err := service.GetMemoryProfile(ctx, &turingv1.GetMemoryProfileRequest{})
	if err != nil {
		t.Fatalf("GetMemoryProfile: %v", err)
	}
	if read.GetContent() != "   \n\t\n" {
		t.Fatalf("profile content = %q, want exactly the bytes on disk", read.GetContent())
	}
	if read.GetContentHash() != memoryfiles.ContentHash("   \n\t\n") {
		t.Fatal("a whitespace-only profile handed back a hash of nothing, so the next save is refused forever")
	}
	if read.GetPinnedTruncated() {
		t.Fatal("nothing was cut; whitespace pins nothing because it is not content")
	}
	if read.GetPinnedBytes() != 0 {
		t.Fatalf("pinned_bytes = %d, want 0", read.GetPinnedBytes())
	}

	if _, err := service.SaveMemoryProfile(ctx, &turingv1.SaveMemoryProfileRequest{
		ExpectedContentHash: read.GetContentHash(),
		Content:             "# Profile\n\nSomething real.\n",
	}); err != nil {
		t.Fatalf("saving over a whitespace-only profile: %v", err)
	}
}

// A proposal is applied against the same token the page is holding. If that
// token is a hash of the pin, a user with a long profile can read a proposal,
// accept it, and watch it refused with nothing they can do about it.
func TestAProfileProposalAppliesOverALongProfile(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	long := "# Profile\n\n" + strings.Repeat("they bike to work every day. ", 300)
	writeVaultDocument(t, vault, memoryfiles.ProfileFileName, long)
	_, sessionID := newRun(t, repo, ctx)
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindProfileEdit,
		Title: "Profile", Body: "Prefers short answers.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}

	read, err := service.GetMemoryProfile(ctx, &turingv1.GetMemoryProfileRequest{})
	if err != nil {
		t.Fatalf("GetMemoryProfile: %v", err)
	}
	applied, err := service.ApplyMemoryProfile(ctx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId:         candidate.CandidateID,
		Content:             read.GetContent() + "\nPrefers short answers.\n",
		ExpectedContentHash: read.GetContentHash(),
	})
	if err != nil {
		t.Fatalf("applying a proposal over a long profile: %v", err)
	}
	if !applied.GetProfile().GetPinnedTruncated() {
		t.Fatal("the apply receipt does not say the resulting profile is longer than a run will carry")
	}
	if applied.GetProfile().GetContentHash() != memoryfiles.ContentHash(applied.GetProfile().GetContent()) {
		t.Fatal("the apply receipt handed back a token that is not a hash of the document it wrote")
	}
}

// The whole page reads the same way as the single-document calls: a long
// persona is shown in full, with no notice the user did not write.
func TestListMemoryStateShowsTheWholeDocuments(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)
	long := longDocument()
	writeVaultDocument(t, vault, memoryfiles.PersonaFileName, long)

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	if state.GetPersona().GetContent() != long {
		t.Fatalf("persona on the page is %d bytes, want the whole %d byte document", len(state.GetPersona().GetContent()), len(long))
	}
	if strings.Contains(state.GetPersona().GetContent(), pinNoticeMarker) {
		t.Fatal("the page carries the runtime's synthetic notice in the editor's plain text")
	}
	if state.GetPersona().GetContentHash() != memoryfiles.ContentHash(long) {
		t.Fatal("the page's compare-and-set token is not a hash of the document")
	}
	if !state.GetPersona().GetPinnedTruncated() {
		t.Fatal("the page cannot tell the user a run only sees part of this")
	}
}

// A document past the safety ceiling has no editor at all: a save composed
// against the first 512 KiB would truncate the user's file to whatever the page
// happened to hold, which is exactly what a compare-and-set exists to stop.
func TestAnOverCeilingPersonaIsReportedTooLargeWithNoPartialEditor(t *testing.T) {
	service, _, vault, ctx := newMemoryService(t)
	writeVaultDocument(t, vault, memoryfiles.PersonaFileName, strings.Repeat("x", memoryfiles.MaxAuthoredDocumentBytes+1))

	read, err := service.GetMemoryPersona(ctx, &turingv1.GetMemoryPersonaRequest{})
	if err != nil {
		t.Fatalf("GetMemoryPersona: %v", err)
	}
	if read.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_CONTENT_TOO_LARGE {
		t.Fatalf("reason = %v, want CONTENT_TOO_LARGE", read.GetUnavailableReason())
	}
	if read.GetContent() != "" {
		t.Fatalf("a partial editor was served: %d bytes", len(read.GetContent()))
	}
	if read.GetContentHash() != "" {
		t.Fatal("a compare-and-set token was handed out for content that was never read in full")
	}
	if read.GetParseError() == "" {
		t.Fatal("the refusal says nothing the user could act on")
	}
}

// Nothing above may move the pin. It is what a model is shown and what the
// egress fingerprint is computed over, so it keeps its budget, its notice and
// its post-truncation hash.
func TestTheRuntimePinStaysBoundedAndFingerprintedOverWhatWasSent(t *testing.T) {
	_, _, vault, _ := newMemoryService(t)
	long := longDocument()
	writeVaultDocument(t, vault, memoryfiles.PersonaFileName, long)

	pinned := vault.LoadPersona(context.Background())
	if !pinned.Truncated {
		t.Fatal("a document past the budget must still be pinned truncated")
	}
	if len(pinned.Content) > memoryfiles.MaxPersonaBytes+len(pinNoticeMarker)+128 {
		t.Fatalf("the pin grew to %d bytes; the budget is %d", len(pinned.Content), memoryfiles.MaxPersonaBytes)
	}
	if !strings.Contains(pinned.Content, pinNoticeMarker) {
		t.Fatal("the pin lost the notice that tells a model it is holding a fragment")
	}
	if pinned.ContentHash != memoryfiles.ContentHash(pinned.Content) {
		t.Fatal("the pinned hash is no longer a hash of the bytes a model would be shown")
	}
	if pinned.ContentHash == memoryfiles.ContentHash(long) {
		t.Fatal("the pinned hash became a hash of the file; the fingerprint is a claim about what was sent")
	}
}

func writeVaultDocument(t *testing.T, vault *memoryfiles.Vault, relPath string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(vault.Root(), relPath), []byte(content), 0o600); err != nil {
		t.Fatalf("seed %q: %v", relPath, err)
	}
}
