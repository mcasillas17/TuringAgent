package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// A vault that has gone away between two runs — unmounted, renamed, on a disk
// that is not plugged in — leaves an app with a database that still remembers
// every proposal the last run wrote and no way to read a single one of them.
//
// What the surface must not do is answer out of that memory. The row holds what
// Turing wrote so a decision can be audited; it is not a second copy of the
// user's inbox, and serving it would show text nobody can confirm is still
// there, above a compare-and-set token the server would then refuse against a
// file it cannot open.
func TestStartupWithAnUnreachableVaultServesNoStoredCandidateBody(t *testing.T) {
	databasePath, memoryRoot := newVaultBackedPaths(t)
	staged := openStagedRepository(t, databasePath, memoryRoot)
	ctx := context.Background()
	session, err := staged.CreateSession(ctx, "proposes something")
	if err != nil {
		t.Fatal(err)
	}
	claim := "The user prefers dark mode."
	candidate, err := staged.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: session.SessionID, Kind: repository.MemoryCandidateKindBelief,
		Title: "Prefers dark mode", Body: claim,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The vault the next run cannot open, with the row that describes it still
	// sitting in the database.
	if err := os.RemoveAll(memoryRoot); err != nil {
		t.Fatalf("take the vault away: %v", err)
	}

	app := startAppOver(t, databasePath, filepath.Join(memoryRoot, "gone"))

	listing, err := app.MemoryService.ListMemoryCandidates(ctx, &turingv1.ListMemoryCandidatesRequest{})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	var listed *turingv1.MemoryCandidate
	for _, entry := range listing.GetCandidates() {
		if entry.GetCandidateId() == candidate.CandidateID {
			listed = entry
		}
	}
	if listed == nil {
		t.Fatal("the proposal waiting in the inbox is not listed at all")
	}
	assertStartupWithheldBody(t, listed, claim)
	if listing.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING {
		t.Fatalf("listing reason = %v, want VAULT_MISSING", listing.GetUnavailableReason())
	}

	fetched, err := app.MemoryService.GetMemoryCandidate(ctx, &turingv1.GetMemoryCandidateRequest{
		CandidateId: candidate.CandidateID,
	})
	if err != nil {
		t.Fatalf("GetMemoryCandidate: %v", err)
	}
	assertStartupWithheldBody(t, fetched, claim)

	state, err := app.MemoryService.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
	if err != nil {
		t.Fatalf("ListMemoryState: %v", err)
	}
	var onPage *turingv1.MemoryCandidate
	for _, entry := range state.GetCandidates() {
		if entry.GetCandidateId() == candidate.CandidateID {
			onPage = entry
		}
	}
	if onPage == nil {
		t.Fatal("the memory page dropped a proposal that is still waiting on the user")
	}
	assertStartupWithheldBody(t, onPage, claim)
	if onPage.GetInboxPath() != candidate.InboxPath {
		t.Fatalf("listed path = %q, want the proposal still identifiable", onPage.GetInboxPath())
	}
}

func assertStartupWithheldBody(t *testing.T, candidate *turingv1.MemoryCandidate, claim string) {
	t.Helper()
	if strings.Contains(candidate.GetContent(), claim) {
		t.Fatalf("the response served the database's copy of the claim: %q", candidate.GetContent())
	}
	if candidate.GetContent() != "" || candidate.GetContentHash() != "" {
		t.Fatalf("content=%q hash=%q, want neither for a proposal nobody could read",
			candidate.GetContent(), candidate.GetContentHash())
	}
	if candidate.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING {
		t.Fatalf("unavailable reason = %v, want VAULT_MISSING", candidate.GetUnavailableReason())
	}
}
