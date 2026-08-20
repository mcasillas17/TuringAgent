package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type guardCall struct {
	stage        string
	physicalPath string
	artifactID   string
	committed    bool
}

type fakeGuard struct {
	mu                sync.Mutex
	provenance        Provenance
	verifyErr         error
	verifyCount       int
	authorizeErr      error
	reservationPath   string
	finalizeErr       error
	commitErrOnly     error
	calls             []guardCall
	writtenAtAuthTime bool
	sandbox           string
	targetOnDisk      string

	// sessionStates scripts the answers to the before-I/O checks in order;
	// sessionStateEnd answers every check after they run out, which is how the
	// after-I/O check is made deterministic.
	sessionStates   []error
	sessionStateEnd error
	sessionChecks   int
}

func (g *fakeGuard) CheckSession(_ context.Context, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sessionChecks++
	if len(g.sessionStates) > 0 {
		next := g.sessionStates[0]
		g.sessionStates = g.sessionStates[1:]
		return next
	}
	return g.sessionStateEnd
}

func (g *fakeGuard) Verify(_ string, _ string, _ map[string]any, _ string) (Provenance, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.verifyCount++
	if g.verifyErr != nil {
		return Provenance{}, g.verifyErr
	}
	return g.provenance, nil
}

func (g *fakeGuard) AuthorizeWrite(_ context.Context, req WriteAuthorization) (Reservation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, guardCall{stage: "authorize", physicalPath: req.PhysicalPath})
	if g.targetOnDisk != "" {
		if _, err := os.Stat(filepath.Join(g.sandbox, g.targetOnDisk)); err == nil {
			g.writtenAtAuthTime = true
		}
	}
	if g.authorizeErr != nil {
		return Reservation{}, g.authorizeErr
	}
	path := req.PhysicalPath
	if g.reservationPath != "" {
		path = g.reservationPath
	}
	return Reservation{ArtifactID: "sbxa_1", PhysicalPath: path, Policy: "delete_on_session_delete"}, nil
}

func (g *fakeGuard) FinalizeWrite(_ context.Context, artifactID string, _ string, committed bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, guardCall{stage: "finalize", artifactID: artifactID, committed: committed})
	if committed && g.commitErrOnly != nil {
		return g.commitErrOnly
	}
	return g.finalizeErr
}

func (g *fakeGuard) stages() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	stages := make([]string, 0, len(g.calls))
	for _, call := range g.calls {
		stages = append(stages, call.stage)
	}
	return stages
}

func (g *fakeGuard) lastCall(t *testing.T) guardCall {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.calls) == 0 {
		t.Fatal("guard was never called")
	}
	return g.calls[len(g.calls)-1]
}

func guardedTools(t *testing.T, guard *fakeGuard) (FilesTools, string) {
	t.Helper()
	sandbox := t.TempDir()
	guard.sandbox = sandbox
	if guard.provenance.SessionID == "" {
		guard.provenance = Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "notes/todo.txt"}
	}
	return NewFilesTools(sandbox).WithProvenanceGuard(guard), sandbox
}

func guardedCall(t *testing.T, tools FilesTools, name string, args map[string]any) (map[string]any, error) {
	t.Helper()
	return tools.CallRequestContext(context.Background(), CallRequest{
		Name: name, Args: args, ApprovalToken: "approval", ProvenanceToken: "capability", AgentID: "general_assistant",
	})
}

func TestCreateMapsSessionOwnedWriteIntoRunScopedPath(t *testing.T) {
	guard := &fakeGuard{}
	tools, sandbox := guardedTools(t, guard)

	result, err := guardedCall(t, tools, "files.create", map[string]any{"path": "notes/todo.txt", "content": "hello"})
	if err != nil {
		t.Fatalf("files.create: %v", err)
	}

	owned := filepath.Join(sandbox, "sessions", "sess_1", "runs", "run_1", "files", "notes", "todo.txt")
	content, err := os.ReadFile(owned)
	if err != nil {
		t.Fatalf("read owned artifact: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("owned artifact content = %q", content)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "notes", "todo.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("session-owned write also landed at the sandbox root")
	}
	if result["path"] != "notes/todo.txt" {
		t.Fatalf("result path = %#v, want the logical path the caller asked for", result["path"])
	}
}

func TestCreateReservesBeforeWritingAndFinalizesAfter(t *testing.T) {
	guard := &fakeGuard{targetOnDisk: "sessions/sess_1/runs/run_1/files/notes/todo.txt"}
	tools, _ := guardedTools(t, guard)

	if _, err := guardedCall(t, tools, "files.create", map[string]any{"path": "notes/todo.txt", "content": "hello"}); err != nil {
		t.Fatalf("files.create: %v", err)
	}

	if want := []string{"authorize", "finalize"}; !equalStrings(guard.stages(), want) {
		t.Fatalf("guard stages = %v, want %v", guard.stages(), want)
	}
	if guard.writtenAtAuthTime {
		t.Fatal("the file already existed when the reservation was taken")
	}
	final := guard.lastCall(t)
	if final.artifactID != "sbxa_1" || !final.committed {
		t.Fatalf("finalize call = %+v, want the reserved artifact committed", final)
	}
	if guard.verifyCount < 2 {
		t.Fatalf("provenance verified %d times, want it verified before and after the write", guard.verifyCount)
	}
}

func TestCreateRequiresProvenanceCapability(t *testing.T) {
	guard := &fakeGuard{}
	tools, _ := guardedTools(t, guard)

	_, err := tools.CallRequestContext(context.Background(), CallRequest{
		Name: "files.create", Args: map[string]any{"path": "notes/todo.txt", "content": "hello"},
		ApprovalToken: "approval", AgentID: "general_assistant",
	})

	if err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("files.create error = %v, want a refusal naming the missing capability", err)
	}
}

func TestSafeToolRequiresProvenanceCapability(t *testing.T) {
	guard := &fakeGuard{}
	tools, _ := guardedTools(t, guard)

	_, err := tools.CallRequestContext(context.Background(), CallRequest{
		Name: "files.list", Args: map[string]any{}, AgentID: "general_assistant",
	})

	if err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("files.list error = %v, want a refusal naming the missing capability", err)
	}
}

func TestCreateRejectsCapabilityScopedToAnotherPath(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "notes/other.txt"}}
	tools, sandbox := guardedTools(t, guard)

	_, err := guardedCall(t, tools, "files.create", map[string]any{"path": "notes/todo.txt", "content": "hello"})

	if err == nil {
		t.Fatal("files.create accepted a capability scoped to a different path")
	}
	if _, statErr := os.Stat(filepath.Join(sandbox, "sessions")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("refused create still created session-owned storage")
	}
	if len(guard.stages()) != 0 {
		t.Fatalf("guard stages = %v, want no reservation for a refused call", guard.stages())
	}
}

func TestCreateReleasesReservationWhenTheWriteFails(t *testing.T) {
	guard := &fakeGuard{}
	tools, _ := guardedTools(t, guard)
	tools.syncFile = func(*os.File) error { return errors.New("disk failure") }

	if _, err := guardedCall(t, tools, "files.create", map[string]any{"path": "notes/todo.txt", "content": "hello"}); err == nil {
		t.Fatal("files.create reported success despite a failed write")
	}

	final := guard.lastCall(t)
	if final.stage != "finalize" || final.committed {
		t.Fatalf("last guard call = %+v, want the reservation released", final)
	}
}

func TestCreateSurfacesFinalizationFailure(t *testing.T) {
	guard := &fakeGuard{finalizeErr: errors.New("orchestrator unavailable")}
	tools, sandbox := guardedTools(t, guard)

	_, err := guardedCall(t, tools, "files.create", map[string]any{"path": "notes/todo.txt", "content": "hello"})

	if err == nil || !strings.Contains(err.Error(), "orchestrator unavailable") {
		t.Fatalf("files.create error = %v, want the finalization failure surfaced rather than swallowed", err)
	}
	// The bytes really are on disk; the caller has to learn that the manifest
	// does not know about them yet.
	if _, statErr := os.Stat(filepath.Join(sandbox, "sessions", "sess_1", "runs", "run_1", "files", "notes", "todo.txt")); statErr != nil {
		t.Fatalf("written artifact missing after finalization failure: %v", statErr)
	}
}

func TestUpdateKeepsPreExistingRootFileInPlaceAsLegacyArtifact(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "legacy.txt"}}
	tools, sandbox := guardedTools(t, guard)
	if err := os.WriteFile(filepath.Join(sandbox, "legacy.txt"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := guardedCall(t, tools, "files.update", map[string]any{"path": "legacy.txt", "content": "new"}); err != nil {
		t.Fatalf("files.update: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(sandbox, "legacy.txt"))
	if err != nil || string(content) != "new" {
		t.Fatalf("legacy file content = %q err = %v, want the pre-existing file updated in place", content, err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "sessions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("updating a pre-existing root file created a session-owned copy")
	}
	if got := guard.calls[0].physicalPath; got != "legacy.txt" {
		t.Fatalf("reserved path = %q, want the legacy root path so the manifest can retain it", got)
	}
}

func TestUpdateTargetsTheSessionOwnedCopyWhenOneExists(t *testing.T) {
	guard := &fakeGuard{}
	tools, sandbox := guardedTools(t, guard)
	if err := os.WriteFile(filepath.Join(sandbox, "notes.txt"), []byte("root"), 0600); err != nil {
		t.Fatal(err)
	}
	guard.provenance = Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "notes.txt"}
	if _, err := guardedCall(t, tools, "files.create", map[string]any{"path": "notes.txt", "content": "owned"}); err != nil {
		t.Fatalf("files.create: %v", err)
	}

	if _, err := guardedCall(t, tools, "files.update", map[string]any{"path": "notes.txt", "content": "owned-updated"}); err != nil {
		t.Fatalf("files.update: %v", err)
	}

	owned, err := os.ReadFile(filepath.Join(sandbox, "sessions", "sess_1", "runs", "run_1", "files", "notes.txt"))
	if err != nil || string(owned) != "owned-updated" {
		t.Fatalf("owned copy = %q err = %v", owned, err)
	}
	root, err := os.ReadFile(filepath.Join(sandbox, "notes.txt"))
	if err != nil || string(root) != "root" {
		t.Fatalf("pre-existing root file = %q err = %v, want it untouched", root, err)
	}
}

func TestReadPrefersSessionOwnedCopyThenFallsBackToRoot(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "notes.txt"}}
	tools, sandbox := guardedTools(t, guard)
	if err := os.WriteFile(filepath.Join(sandbox, "notes.txt"), []byte("root"), 0600); err != nil {
		t.Fatal(err)
	}

	fallback, err := guardedCall(t, tools, "files.read", map[string]any{"path": "notes.txt"})
	if err != nil {
		t.Fatalf("files.read fallback: %v", err)
	}
	if fallback["content"] != "root" {
		t.Fatalf("content = %#v, want the pre-existing root file", fallback["content"])
	}

	if _, err := guardedCall(t, tools, "files.create", map[string]any{"path": "notes.txt", "content": "owned"}); err != nil {
		t.Fatal(err)
	}
	owned, err := guardedCall(t, tools, "files.read", map[string]any{"path": "notes.txt"})
	if err != nil {
		t.Fatalf("files.read owned: %v", err)
	}
	if owned["content"] != "owned" {
		t.Fatalf("content = %#v, want the session's own copy", owned["content"])
	}
	if owned["path"] != "notes.txt" {
		t.Fatalf("result path = %#v, want the logical path", owned["path"])
	}
}

func TestReadFindsAnArtifactWrittenByAnEarlierRunOfTheSameSession(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "notes.txt"}}
	tools, _ := guardedTools(t, guard)
	if _, err := guardedCall(t, tools, "files.create", map[string]any{"path": "notes.txt", "content": "from run 1"}); err != nil {
		t.Fatal(err)
	}
	guard.provenance = Provenance{SessionID: "sess_1", RunID: "run_2", LogicalPath: "notes.txt"}

	result, err := guardedCall(t, tools, "files.read", map[string]any{"path": "notes.txt"})
	if err != nil {
		t.Fatalf("files.read across runs: %v", err)
	}
	if result["content"] != "from run 1" {
		t.Fatalf("content = %#v, want the earlier run's artifact", result["content"])
	}
}

func TestReadCannotReachAnotherSessionsArtifacts(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "notes.txt"}}
	tools, sandbox := guardedTools(t, guard)
	other := filepath.Join(sandbox, "sessions", "sess_2", "runs", "run_9", "files")
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "notes.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := guardedCall(t, tools, "files.read", map[string]any{"path": "notes.txt"}); err == nil {
		t.Fatal("files.read reached another session's artifact")
	}
}

func TestListHidesTheServerManagedSessionSubtreeFromTheSandboxRoot(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: ""}}
	tools, sandbox := guardedTools(t, guard)
	if err := os.WriteFile(filepath.Join(sandbox, "legacy.txt"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sandbox, "sessions", "sess_2", "runs", "run_9", "files"), 0700); err != nil {
		t.Fatal(err)
	}

	result, err := guardedCall(t, tools, "files.list", map[string]any{})
	if err != nil {
		t.Fatalf("files.list: %v", err)
	}

	items, _ := result["items"].([]map[string]any)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item["name"].(string))
	}
	if !equalStrings(names, []string{"legacy.txt"}) {
		t.Fatalf("listed names = %v, want only the legacy root file", names)
	}
}

func TestListShowsTheSessionsOwnArtifactsOnceItHasSome(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "owned.txt"}}
	tools, _ := guardedTools(t, guard)
	if _, err := guardedCall(t, tools, "files.create", map[string]any{"path": "owned.txt", "content": "hello"}); err != nil {
		t.Fatal(err)
	}
	guard.provenance = Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: ""}

	result, err := guardedCall(t, tools, "files.list", map[string]any{})
	if err != nil {
		t.Fatalf("files.list: %v", err)
	}

	items, _ := result["items"].([]map[string]any)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item["name"].(string))
	}
	if !equalStrings(names, []string{"owned.txt"}) {
		t.Fatalf("listed names = %v, want the session's own artifact", names)
	}
}

func equalStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestSearchDoesNotReachOtherSessionsWhenFallingBackToTheSandboxRoot(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: ""}}
	tools, sandbox := guardedTools(t, guard)
	if err := os.WriteFile(filepath.Join(sandbox, "legacy.txt"), []byte("shared secret"), 0600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(sandbox, "sessions", "sess_2", "runs", "run_9", "files")
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "private.txt"), []byte("shared secret"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := guardedCall(t, tools, "files.search", map[string]any{"query": "shared secret"})
	if err != nil {
		t.Fatalf("files.search: %v", err)
	}

	matches, _ := result["matches"].([]map[string]any)
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		paths = append(paths, match["path"].(string))
	}
	if !equalStrings(paths, []string{"legacy.txt"}) {
		t.Fatalf("search matches = %v, want only the legacy root file", paths)
	}
}

func TestReadRefusesACapabilityThatNamesAnotherSessionsStorage(t *testing.T) {
	foreign := "sessions/sess_2/runs/run_9/files/private.txt"
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: foreign}}
	tools, sandbox := guardedTools(t, guard)
	directory := filepath.Join(sandbox, "sessions", "sess_2", "runs", "run_9", "files")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "private.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := guardedCall(t, tools, "files.read", map[string]any{"path": foreign})

	if err == nil {
		t.Fatal("files.read served another session's artifact")
	}
	if !strings.Contains(err.Error(), "server-managed") {
		t.Fatalf("files.read error = %v, want a refusal naming server-managed storage", err)
	}
}

func TestCreateKeepsTheReservationOpenWhenFinalizationFailsAfterTheWrite(t *testing.T) {
	// The release call would succeed here; only the commit fails. Releasing
	// would delete the manifest row for a file that is already on disk, and
	// session deletion would then complete over bytes nothing accounts for.
	guard := &fakeGuard{commitErrOnly: errors.New("orchestrator unavailable")}
	tools, sandbox := guardedTools(t, guard)

	if _, err := guardedCall(t, tools, "files.create", map[string]any{"path": "notes/todo.txt", "content": "hello"}); err == nil {
		t.Fatal("files.create reported success despite a failed finalization")
	}

	if _, err := os.Stat(filepath.Join(sandbox, "sessions", "sess_1", "runs", "run_1", "files", "notes", "todo.txt")); err != nil {
		t.Fatalf("written artifact missing: %v", err)
	}
	for _, call := range guard.calls {
		if call.stage == "finalize" && !call.committed {
			t.Fatal("the reservation was released for a file that is on disk")
		}
	}
}

func TestUpdateKeepsTheReservationOpenWhenFinalizationFailsAfterTheWrite(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "legacy.txt"}}
	tools, sandbox := guardedTools(t, guard)
	if err := os.WriteFile(filepath.Join(sandbox, "legacy.txt"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	guard.commitErrOnly = errors.New("orchestrator unavailable")

	if _, err := guardedCall(t, tools, "files.update", map[string]any{"path": "legacy.txt", "content": "new"}); err == nil {
		t.Fatal("files.update reported success despite a failed finalization")
	}

	content, err := os.ReadFile(filepath.Join(sandbox, "legacy.txt"))
	if err != nil || string(content) != "new" {
		t.Fatalf("updated file = %q err = %v, want the replacement already durable", content, err)
	}
	for _, call := range guard.calls {
		if call.stage == "finalize" && !call.committed {
			t.Fatal("the reservation was released for a file that is on disk")
		}
	}
}

func TestUpdateFromALaterRunTargetsTheEarlierRunsArtifact(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "notes.txt"}}
	tools, sandbox := guardedTools(t, guard)
	if _, err := guardedCall(t, tools, "files.create", map[string]any{"path": "notes.txt", "content": "from run 1"}); err != nil {
		t.Fatal(err)
	}
	guard.provenance = Provenance{SessionID: "sess_1", RunID: "run_2", LogicalPath: "notes.txt"}

	if _, err := guardedCall(t, tools, "files.update", map[string]any{"path": "notes.txt", "content": "from run 2"}); err != nil {
		t.Fatalf("cross-run files.update: %v", err)
	}

	// The reservation names the earlier run's location, which is where the file
	// actually is; the orchestrator accepts it because it is still inside this
	// session's own subtree.
	want := "sessions/sess_1/runs/run_1/files/notes.txt"
	var reserved string
	for _, call := range guard.calls {
		if call.stage == "authorize" {
			reserved = call.physicalPath
		}
	}
	if reserved != want {
		t.Fatalf("reserved path = %q, want %q", reserved, want)
	}
	content, err := os.ReadFile(filepath.Join(sandbox, "sessions", "sess_1", "runs", "run_1", "files", "notes.txt"))
	if err != nil || string(content) != "from run 2" {
		t.Fatalf("artifact = %q err = %v, want the earlier run's file updated", content, err)
	}
}

func TestSessionStorageIsNotEnumerableThroughAnyTool(t *testing.T) {
	// The bare "sessions" path is the interesting one: it names the shared root
	// directly rather than descending into it, so a prefix check that only
	// looks for "sessions/" lets it through and every session id becomes
	// readable.
	for _, requested := range []string{"sessions", "sessions/", "./sessions", "sessions/sess_2", "sessions/sess_2/runs", "SESSIONS"} {
		for _, tool := range []string{"files.list", "files.read", "files.search"} {
			t.Run(tool+" "+requested, func(t *testing.T) {
				guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: requested}}
				tools, sandbox := guardedTools(t, guard)
				other := filepath.Join(sandbox, "sessions", "sess_2", "runs", "run_9", "files")
				if err := os.MkdirAll(other, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(other, "private.txt"), []byte("another session"), 0600); err != nil {
					t.Fatal(err)
				}
				args := map[string]any{"path": requested}
				if tool == "files.search" {
					args["query"] = "another session"
				}

				result, err := guardedCall(t, tools, tool, args)

				if err == nil {
					t.Fatalf("%s served %q: %+v", tool, requested, result)
				}
				if !strings.Contains(err.Error(), "server-managed") {
					t.Fatalf("error = %v, want a refusal naming server-managed storage", err)
				}
			})
		}
	}
}

func TestSessionStorageIsNotWritableThroughAnyTool(t *testing.T) {
	for _, tool := range []string{"files.create", "files.update"} {
		t.Run(tool, func(t *testing.T) {
			guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "sessions/sess_2/runs/run_9/files/private.txt"}}
			tools, sandbox := guardedTools(t, guard)
			other := filepath.Join(sandbox, "sessions", "sess_2", "runs", "run_9", "files")
			if err := os.MkdirAll(other, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(other, "private.txt"), []byte("another session"), 0600); err != nil {
				t.Fatal(err)
			}

			_, err := guardedCall(t, tools, tool, map[string]any{
				"path": "sessions/sess_2/runs/run_9/files/private.txt", "content": "overwritten",
			})

			if err == nil {
				t.Fatalf("%s wrote into another session's storage", tool)
			}
			content, readErr := os.ReadFile(filepath.Join(other, "private.txt"))
			if readErr != nil || string(content) != "another session" {
				t.Fatalf("another session's file = %q err = %v", content, readErr)
			}
		})
	}
}

func TestReadIsRefusedWhenWithdrawalHasAlreadyStarted(t *testing.T) {
	guard := &fakeGuard{
		provenance:      Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "notes.txt"},
		sessionStates:   []error{errors.New("session deletion is in progress")},
		sessionStateEnd: errors.New("session deletion is in progress"),
	}
	tools, sandbox := guardedTools(t, guard)
	if err := os.WriteFile(filepath.Join(sandbox, "notes.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := guardedCall(t, tools, "files.read", map[string]any{"path": "notes.txt"})

	if err == nil {
		t.Fatalf("files.read served a withdrawn session: %+v", result)
	}
	if !strings.Contains(err.Error(), "deletion") {
		t.Fatalf("error = %v, want it to name the deletion in progress", err)
	}
	if guard.sessionChecks != 1 {
		t.Fatalf("session checks = %d, want the read refused on the first check", guard.sessionChecks)
	}
}

func TestReadIsRefusedWhenWithdrawalStartsDuringTheRead(t *testing.T) {
	// Deterministic race: the first check (before I/O) says active, the second
	// (after I/O) says the withdrawal began. The content was read, and must not
	// be returned.
	guard := &fakeGuard{
		provenance:      Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "notes.txt"},
		sessionStates:   []error{nil},
		sessionStateEnd: errors.New("session deletion is in progress"),
	}
	tools, sandbox := guardedTools(t, guard)
	if err := os.WriteFile(filepath.Join(sandbox, "notes.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := guardedCall(t, tools, "files.read", map[string]any{"path": "notes.txt"})

	if err == nil {
		t.Fatalf("files.read returned content read across a withdrawal: %+v", result)
	}
	if result != nil {
		t.Fatalf("result = %+v, want nothing returned", result)
	}
	if !strings.Contains(err.Error(), "deletion") {
		t.Fatalf("error = %v, want it to name the deletion in progress", err)
	}
	if guard.sessionChecks != 2 {
		t.Fatalf("session checks = %d, want a check before and after the read", guard.sessionChecks)
	}
}

func TestListAndSearchAreRefusedWhenWithdrawalStartsDuringTheCall(t *testing.T) {
	for _, tool := range []string{"files.list", "files.search"} {
		t.Run(tool, func(t *testing.T) {
			guard := &fakeGuard{
				provenance:      Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: ""},
				sessionStates:   []error{nil},
				sessionStateEnd: errors.New("session deletion is in progress"),
			}
			tools, sandbox := guardedTools(t, guard)
			if err := os.WriteFile(filepath.Join(sandbox, "notes.txt"), []byte("secret"), 0600); err != nil {
				t.Fatal(err)
			}
			args := map[string]any{}
			if tool == "files.search" {
				args["query"] = "secret"
			}

			result, err := guardedCall(t, tools, tool, args)

			if err == nil {
				t.Fatalf("%s answered across a withdrawal: %+v", tool, result)
			}
			if guard.sessionChecks != 2 {
				t.Fatalf("session checks = %d, want a check before and after", guard.sessionChecks)
			}
		})
	}
}

func TestSafeToolsAreRefusedWhenTheSessionStateCannotBeRead(t *testing.T) {
	guard := &fakeGuard{
		provenance:      Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "notes.txt"},
		sessionStates:   []error{errors.New("orchestrator unavailable")},
		sessionStateEnd: errors.New("orchestrator unavailable"),
	}
	tools, sandbox := guardedTools(t, guard)
	if err := os.WriteFile(filepath.Join(sandbox, "notes.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := guardedCall(t, tools, "files.read", map[string]any{"path": "notes.txt"}); err == nil {
		t.Fatal("files.read treated an unreachable orchestrator as an active session")
	}
}

func TestWritesStillCheckTheSessionThroughTheReservation(t *testing.T) {
	// A write's before-state is the reservation and its after-state is the
	// finalization, both server-side, so it must not also pay for a capability
	// check on every call.
	guard := &fakeGuard{}
	tools, _ := guardedTools(t, guard)

	if _, err := guardedCall(t, tools, "files.create", map[string]any{"path": "notes/todo.txt", "content": "hello"}); err != nil {
		t.Fatalf("files.create: %v", err)
	}

	if guard.sessionChecks != 0 {
		t.Fatalf("session checks = %d, want none for a write", guard.sessionChecks)
	}
}
