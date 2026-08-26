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

// seedCandidateRow writes one managed proposal into the vault and the database,
// which is what a tool call leaves behind.
func seedCandidateRow(t *testing.T, repo *repository.Repository, ctx context.Context, sessionID string, kind string, title string, body string) repository.MemoryCandidate {
	t.Helper()
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: kind, Title: title, Body: body,
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	return candidate
}

// editInboxFile is the user opening the proposal in Obsidian and changing it,
// which is the whole reason the vault is a vault.
func editInboxFile(t *testing.T, vault *memoryfiles.Vault, relPath string, replace string, with string) string {
	t.Helper()
	full := filepath.Join(vault.Root(), filepath.FromSlash(relPath))
	before, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read %q: %v", relPath, err)
	}
	after := strings.Replace(string(before), replace, with, 1)
	if after == string(before) {
		t.Fatalf("the edit changed nothing; %q does not contain %q", relPath, replace)
	}
	if err := os.WriteFile(full, []byte(after), 0o600); err != nil {
		t.Fatalf("write %q: %v", relPath, err)
	}
	return after
}

func candidateByID(t *testing.T, response *turingv1.ListMemoryStateResponse, candidateID string) *turingv1.MemoryCandidate {
	t.Helper()
	for _, candidate := range response.GetCandidates() {
		if candidate.GetCandidateId() == candidateID {
			return candidate
		}
	}
	t.Fatalf("candidate %q is not in the listing", candidateID)
	return nil
}

// The file is what the user is looking at. The row is what Turing wrote. When
// they disagree — because the user edited the proposal in their editor — the
// listing has to show the file, or the page displays one claim and the decision
// carries another.
func TestListMemoryStateShowsTheProposalAsTheFileNowReadsIt(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", "The user prefers dark mode.")
	edited := editInboxFile(t, vault, candidate.InboxPath, "The user prefers dark mode.", "The user prefers light mode.")

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := candidateByID(t, state, candidate.CandidateID)
	if !strings.Contains(listed.GetContent(), "light mode") {
		t.Fatalf("listed content = %q, want the file's own words", listed.GetContent())
	}
	if listed.GetContentHash() != memoryfiles.ContentHash(edited) {
		t.Fatalf("listed hash = %q, want a hash of the file as it stands", listed.GetContentHash())
	}
	if listed.GetInboxPath() != candidate.InboxPath || listed.GetState() != turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PENDING {
		t.Fatalf("the row's own identity and lifecycle were lost: %+v", listed)
	}
	if len(listed.GetProvenance()) == 0 {
		t.Fatal("the row's provenance was lost to the overlay")
	}
}

// The kind decides which decision the page may offer. If the user rewrites the
// proposal into a profile edit, an Apply button is the honest answer and a
// Promote button is one the server would refuse.
func TestListMemoryStateShowsTheKindTheFileNowDeclares(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", "The user prefers dark mode.")
	editInboxFile(t, vault, candidate.InboxPath, `kind: "belief"`, `kind: "profile_edit"`)

	state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	listed := candidateByID(t, state, candidate.CandidateID)
	if listed.GetKind() != turingv1.MemoryCandidateKind_MEMORY_CANDIDATE_KIND_PROFILE_EDIT {
		t.Fatalf("listed kind = %v, want the kind the file declares", listed.GetKind())
	}
}

// A decision names the exact text it was composed against. A promotion carrying
// a hash the file no longer matches is refused, and the file stays where it is
// so the user can read what it says now and decide again.
func TestPromoteRefusesAHashTheInboxFileNoLongerMatches(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", "The user prefers dark mode.")
	stale := candidate.ContentHash
	editInboxFile(t, vault, candidate.InboxPath, "The user prefers dark mode.", "The user prefers light mode.")

	_, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId:           candidate.CandidateID,
		ExpectedCandidateHash: stale,
	})
	assertFailedPrecondition(t, err, "promote against a stale candidate hash")
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); statErr != nil {
		t.Fatalf("the refused proposal left the inbox: %v", statErr)
	}
	if _, rowErr := repo.MemoryCandidateByID(ctx, candidate.CandidateID); rowErr != nil {
		t.Fatalf("the refused proposal lost its row: %v", rowErr)
	}
}

func TestPromoteAcceptsTheHashTheInboxFileActuallyHas(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", "The user prefers dark mode.")

	if _, err := service.PromoteMemoryCandidate(ctx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId:           candidate.CandidateID,
		ExpectedCandidateHash: candidate.ContentHash,
	}); err != nil {
		t.Fatalf("PromoteMemoryCandidate with the file's own hash: %v", err)
	}
}

func TestRejectRefusesAHashTheInboxFileNoLongerMatches(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindBelief, "Dark mode", "The user prefers dark mode.")
	stale := candidate.ContentHash
	editInboxFile(t, vault, candidate.InboxPath, "The user prefers dark mode.", "The user prefers light mode.")

	_, err := service.RejectMemoryCandidate(ctx, &turingv1.RejectMemoryCandidateRequest{
		CandidateId:           candidate.CandidateID,
		ExpectedCandidateHash: stale,
	})
	assertFailedPrecondition(t, err, "reject against a stale candidate hash")
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); statErr != nil {
		t.Fatalf("a refused rejection removed the file anyway: %v", statErr)
	}
}

// An apply binds two documents at once: the profile it is replacing and the
// proposal it was composed from. Both have to still be what the user read.
func TestApplyRefusesAHashTheProposalNoLongerMatchesAndLeavesTheProfileAlone(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindProfileEdit, "Call me Miguel", "The user goes by Miguel.")
	writeVaultDocument(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWrites Go.\n")
	stale := candidate.ContentHash
	editInboxFile(t, vault, candidate.InboxPath, "The user goes by Miguel.", "The user goes by someone else entirely.")

	_, err := service.ApplyMemoryProfile(ctx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId:           candidate.CandidateID,
		Content:               "# Profile\n\nWrites Go.\n\nGoes by Miguel.\n",
		ExpectedContentHash:   memoryfiles.ContentHash("# Profile\n\nWrites Go.\n"),
		ExpectedCandidateHash: stale,
	})
	assertFailedPrecondition(t, err, "apply against a stale candidate hash")
	profile := vault.EditableProfile(ctx)
	if profile.Content != "# Profile\n\nWrites Go.\n" {
		t.Fatalf("a refused apply rewrote the profile: %q", profile.Content)
	}
	if _, statErr := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(candidate.InboxPath))); statErr != nil {
		t.Fatalf("a refused apply removed the proposal: %v", statErr)
	}
}

func TestApplyAcceptsTheHashTheProposalActuallyHas(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	sessionID := newMemorySession(t, repo, ctx)
	candidate := seedCandidateRow(t, repo, ctx, sessionID, repository.MemoryCandidateKindProfileEdit, "Call me Miguel", "The user goes by Miguel.")
	writeVaultDocument(t, vault, memoryfiles.ProfileFileName, "# Profile\n\nWrites Go.\n")
	result := "# Profile\n\nWrites Go.\n\nGoes by Miguel.\n"

	applied, err := service.ApplyMemoryProfile(ctx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId:           candidate.CandidateID,
		Content:               result,
		ExpectedContentHash:   memoryfiles.ContentHash("# Profile\n\nWrites Go.\n"),
		ExpectedCandidateHash: candidate.ContentHash,
	})
	if err != nil {
		t.Fatalf("ApplyMemoryProfile with the proposal's own hash: %v", err)
	}
	if applied.GetProfile().GetContent() != result {
		t.Fatalf("applied profile = %q, want the reviewed result", applied.GetProfile().GetContent())
	}
}

// assertFailedPrecondition is what a decision about text the vault has moved on
// from must answer: not an internal error, and not silence.
func assertFailedPrecondition(t *testing.T, err error, what string) {
	t.Helper()
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("%s = %v (code %v), want FailedPrecondition", what, err, status.Code(err))
	}
	if !strings.Contains(strings.ToLower(status.Convert(err).Message()), "read it again") {
		t.Fatalf("%s said %q, which does not tell the user what to do", what, status.Convert(err).Message())
	}
}
