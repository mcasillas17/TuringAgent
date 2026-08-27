package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// newVaultBackedPaths lays out a database and a vault the way init.sh does, so
// a test can put state in both and then start the app over them — which is what
// a restart after a crash actually is.
func newVaultBackedPaths(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{memoryfiles.InboxDirName, memoryfiles.BeliefsDirName} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatalf("prepare vault dir %q: %v", dir, err)
		}
	}
	return filepath.Join(t.TempDir(), "turing.db"), root
}

func startAppOver(t *testing.T, databasePath string, memoryRoot string) *App {
	t.Helper()
	// The sandbox scope is the other half of a deletion, and it is not what
	// these tests are about: a stand-in that answers "the namespace is gone"
	// keeps an unreachable file server from being mistaken for the vault
	// failing.
	sandbox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			LifecycleVersion int64 `json:"lifecycleVersion"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("content-type", "application/json")
		_, _ = fmt.Fprintf(w, `{"namespaceRemoved":true,"removedFiles":0,"removedDirectories":0,"lifecycleVersion":%d}`, body.LifecycleVersion)
	}))
	t.Cleanup(sandbox.Close)
	app, err := New(config.Config{
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret:    "approval-secret",
		DatabasePath:         databasePath,
		MemoryRoot:           memoryRoot,
		MCPFilesBaseURL:      sandbox.URL + "/mcp",
		MCPFilesCleanupToken: "cleanup-token",
		OllamaModel:          "llama3.2",
		OpenAIModel:          "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("start app: %v", err)
	}
	t.Cleanup(app.Stop)
	return app
}

// openStagedRepository is the pre-restart half: the same database and vault the
// app is about to be started over, opened directly so a test can leave the
// exact state a crash would have left.
func openStagedRepository(t *testing.T, databasePath string, memoryRoot string) *repository.Repository {
	t.Helper()
	repo, _ := openStagedRepositoryAndDB(t, databasePath, memoryRoot)
	return repo
}

// openStagedRepositoryAndDB is the same staging with the handle exposed, for a
// test that has to damage the database itself rather than the vault.
func openStagedRepositoryAndDB(t *testing.T, databasePath string, memoryRoot string) (*repository.Repository, *db.DB) {
	t.Helper()
	database, err := db.Open(databasePath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repo := repository.New(database)
	vault, err := memoryfiles.Open(memoryRoot)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	repo.SetMemoryVault(vault)
	return repo, database
}

func auditActionCount(t *testing.T, repo *repository.Repository, action string) int {
	t.Helper()
	records, err := repo.ListAuditRecords(context.Background(), repository.AuditQuery{
		Action: sql.NullString{String: action, Valid: true},
		Limit:  50,
	})
	if err != nil {
		t.Fatalf("ListAuditRecords(%q): %v", action, err)
	}
	return len(records)
}

// A deletion interrupted mid-flight is resumed at startup, and resuming it
// means removing the notes that session left in the user's vault. The vault has
// to be attached before that runs: a cleaner asked to purge with no vault
// reports the withdrawal as failed and files one audit row per file — telling
// the user their notes are still on disk when nothing had gone wrong at all.
func TestStartupResumesAPendingVaultDeletionWithoutFilingAFalseFailure(t *testing.T) {
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

	app := startAppOver(t, databasePath, memoryRoot)

	if count := auditActionCount(t, app.Repository, "session.vault_artifact.cleanup.failed"); count != 0 {
		t.Fatalf("startup filed %d vault cleanup failure(s) for a deletion it could have finished", count)
	}
	notePath := filepath.Join(memoryRoot, filepath.FromSlash(candidate.InboxPath))
	if _, err := os.Lstat(notePath); !os.IsNotExist(err) {
		t.Fatalf("the deleted session's note is still in the vault: %v", err)
	}
	pending, err := app.Repository.PendingSessionDeletionIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range pending {
		if id == session.SessionID {
			t.Fatal("the pending deletion was not finished at startup")
		}
	}
}

// A promotion that moved the file and then died leaves a belief in the vault
// with no row behind it. Healing that is the writing reconcile's job, and it
// has to run at startup: otherwise the note is invisible to search until
// somebody happens to open the Memory page.
func TestStartupHealsABeliefRowLostToACrashWithoutAnyMemoryCall(t *testing.T) {
	databasePath, memoryRoot := newVaultBackedPaths(t)
	staged := openStagedRepository(t, databasePath, memoryRoot)
	noteID, err := memoryfiles.NewNoteID()
	if err != nil {
		t.Fatal(err)
	}
	belief := "---\nid: \"" + noteID + "\"\nkind: \"belief\"\ntitle: \"dark mode\"\nmanaged: true\nrefs: []\n---\n\nThe user prefers dark mode.\n"
	if err := os.WriteFile(
		filepath.Join(memoryRoot, memoryfiles.BeliefsDirName, "dark-mode.md"),
		[]byte(belief), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := staged.MemoryNoteByID(context.Background(), noteID); err == nil {
		t.Fatal("the staged belief already had a row; the crash was not reproduced")
	}

	app := startAppOver(t, databasePath, memoryRoot)

	note, err := app.Repository.MemoryNoteByID(context.Background(), noteID)
	if err != nil {
		t.Fatalf("startup did not heal the orphaned belief: %v", err)
	}
	if note.Path != memoryfiles.BeliefsDirName+"/dark-mode.md" {
		t.Fatalf("healed note path = %q", note.Path)
	}
}

// An install with no vault owes the startup pass nothing. Memory already
// reports itself unavailable across the whole surface, and refusing to start
// over a folder the user has not created would take the app down for a feature
// they are not using.
func TestStartupWithNoVaultStartsAnyway(t *testing.T) {
	app, err := New(config.Config{
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret: "approval-secret",
		DatabasePath:      filepath.Join(t.TempDir(), "turing.db"),
		MemoryRoot:        filepath.Join(t.TempDir(), "not-created-yet"),
		OllamaModel:       "llama3.2",
		OpenAIModel:       "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("a missing vault folder stopped the app from starting: %v", err)
	}
	t.Cleanup(app.Stop)
}

// A vault that is there and a database that cannot answer for it is the other
// case, and it is fatal. Starting on top of it would serve an index the pass
// had already decided was wrong, and every later read would inherit that answer
// without anything saying where it came from.
//
// The inconsistency here is a real one rather than a bounded refusal: a
// database restored without its note index, which is what a partial backup or
// an interrupted copy leaves behind. A vault merely too large to scan is
// deliberately *not* this case — see the over-bound tests, which require the
// app to boot.
func TestStartupFailsClosedOnADatabaseThatCannotAnswerForTheVault(t *testing.T) {
	databasePath, memoryRoot := newVaultBackedPaths(t)
	staged, database := openStagedRepositoryAndDB(t, databasePath, memoryRoot)
	noteID, err := memoryfiles.NewNoteID()
	if err != nil {
		t.Fatal(err)
	}
	belief := "---\nid: \"" + noteID + "\"\nkind: \"belief\"\nmanaged: true\n---\n\nnote\n"
	if err := os.WriteFile(
		filepath.Join(memoryRoot, memoryfiles.BeliefsDirName, "note.md"),
		[]byte(belief), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := staged.ReconcileMemoryVault(context.Background()); err != nil {
		t.Fatalf("the staged vault was not reconcilable before the damage: %v", err)
	}
	dropMemoryNoteIndex(t, database)

	app, err := New(config.Config{
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret: "approval-secret",
		DatabasePath:      databasePath,
		MemoryRoot:        memoryRoot,
		OllamaModel:       "llama3.2",
		OpenAIModel:       "gpt-4o-mini",
	})
	if err == nil {
		app.Stop()
		t.Fatal("the app started over a vault the startup pass could not reconcile")
	}
	if !strings.Contains(err.Error(), "reconcile memory vault") {
		t.Fatalf("startup failure = %v, want it to name the reconcile it could not finish", err)
	}
}

// dropMemoryNoteIndex removes the note index and the FTS structures that hang
// off it, leaving the migration ledger claiming they are there — the shape a
// database restored from a partial backup actually has.
func dropMemoryNoteIndex(t *testing.T, database *db.DB) {
	t.Helper()
	connection, err := database.Conn(context.Background())
	if err != nil {
		t.Fatalf("pin a connection: %v", err)
	}
	defer func() { _ = connection.Close() }()
	for _, statement := range []string{
		`PRAGMA foreign_keys = OFF`,
		`DROP TRIGGER memory_notes_fts_ai`,
		`DROP TRIGGER memory_notes_fts_ad`,
		`DROP TRIGGER memory_notes_fts_au`,
		`DROP TABLE memory_notes_fts`,
		`DROP TABLE memory_notes`,
	} {
		if _, err := connection.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}
