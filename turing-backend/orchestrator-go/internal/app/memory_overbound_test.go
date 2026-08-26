package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// writeOverBoundVault fills beliefs/ with one more note than the scan bound
// allows, which is the state a user reaches by pointing Turing at a vault they
// have been keeping for years.
func writeOverBoundVault(t *testing.T, memoryRoot string) {
	t.Helper()
	beliefs := filepath.Join(memoryRoot, memoryfiles.BeliefsDirName)
	for index := 0; index <= memoryfiles.MaxVaultIndexedFiles; index++ {
		noteID, err := memoryfiles.NewNoteID()
		if err != nil {
			t.Fatal(err)
		}
		body := "---\nid: \"" + noteID + "\"\nkind: \"belief\"\nmanaged: true\n---\n\nnote\n"
		if err := os.WriteFile(filepath.Join(beliefs, fmt.Sprintf("note-%05d.md", index)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// A vault past the scan bound is a bounded feature saying no, not a broken
// install. Indexing and search are what the bound protects, and those are the
// only things it may take away: persona.md and profile.md are two files opened
// by name with no walk behind them, and a conversation that pins them has
// nothing to do with how many beliefs are on disk.
//
// Refusing to boot over it means the user cannot open the app at all until they
// prune a folder the app will not show them.
func TestStartupOverAnOverBoundVaultStillBootsAndStillPins(t *testing.T) {
	databasePath, memoryRoot := newVaultBackedPaths(t)
	writeOverBoundVault(t, memoryRoot)
	if err := os.WriteFile(filepath.Join(memoryRoot, memoryfiles.PersonaFileName), []byte("Be terse.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, memoryfiles.ProfileFileName), []byte("Writes Go.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := startAppOver(t, databasePath, memoryRoot)
	ctx := context.Background()

	snapshot, err := app.Repository.EgressMemorySnapshot(ctx)
	if err != nil {
		t.Fatalf("EgressMemorySnapshot over an over-bound vault: %v", err)
	}
	if !snapshot.Persona.Available || snapshot.Persona.Content != "Be terse.\n" {
		t.Fatalf("persona = %+v, want the file the bound has nothing to do with", snapshot.Persona)
	}
	if !snapshot.Profile.Available || snapshot.Profile.Content != "Writes Go.\n" {
		t.Fatalf("profile = %+v, want the file the bound has nothing to do with", snapshot.Profile)
	}

	session, err := app.Repository.CreateSession(ctx, "over-bound")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Repository.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	}); err != nil {
		t.Fatalf("enqueueing over an over-bound vault: %v", err)
	}
}

// The other half: the bound must not wedge a withdrawal either. A deletion
// whose completion can never succeed stays on the pending list, and the
// reconcile ticker retries it every minute for as long as the app runs — while
// the user is told their conversation is still being deleted.
//
// The bounded pass is a bounded pass: it did what it could, the reservation
// sweep and the row removal are not gated on it, and the deletion finishes.
func TestPendingDeletionFinishesOverAnOverBoundVault(t *testing.T) {
	databasePath, memoryRoot := newVaultBackedPaths(t)
	staged := openStagedRepository(t, databasePath, memoryRoot)
	ctx := context.Background()
	session, err := staged.CreateSession(ctx, "leaves a note behind")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := staged.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: session.SessionID, Kind: repository.MemoryCandidateKindBelief,
		Title: "Prefers dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staged.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}
	writeOverBoundVault(t, memoryRoot)

	app := startAppOver(t, databasePath, memoryRoot)

	pending, err := app.Repository.PendingSessionDeletionIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range pending {
		if id == session.SessionID {
			t.Fatal("the deletion is still pending; a bounded vault wedged the withdrawal")
		}
	}
	notePath := filepath.Join(memoryRoot, filepath.FromSlash(candidate.InboxPath))
	if _, err := os.Lstat(notePath); !os.IsNotExist(err) {
		t.Fatalf("the deleted session's note is still in the vault: %v", err)
	}
}

// The bound is still reported. Degrading is not the same as pretending, and a
// user whose vault is not being indexed has to be able to find that out.
func TestAnOverBoundVaultIsStillReportedByTheWritingPass(t *testing.T) {
	databasePath, memoryRoot := newVaultBackedPaths(t)
	writeOverBoundVault(t, memoryRoot)
	app := startAppOver(t, databasePath, memoryRoot)

	_, err := app.Repository.ReconcileMemoryVault(context.Background())
	if err == nil {
		t.Fatal("a vault past the scan bound was reconciled as though it were within it")
	}
	if !errorIsVaultTooLarge(err) {
		t.Fatalf("reconcile error = %v, want it to name the scan bound", err)
	}
}

func errorIsVaultTooLarge(err error) bool {
	for err != nil {
		if err == memoryfiles.ErrVaultTooLarge {
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}
