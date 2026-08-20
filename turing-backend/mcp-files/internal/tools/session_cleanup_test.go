package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func cleanupTools(t *testing.T) (FilesTools, string) {
	t.Helper()
	sandbox := t.TempDir()
	return NewFilesTools(sandbox), sandbox
}

func writeSandboxFile(t *testing.T, sandbox string, relative string, content string) {
	t.Helper()
	full := filepath.Join(sandbox, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func cleanupArgs(sessionID string) map[string]any {
	return map[string]any{"sessionId": sessionID, "lifecycleVersion": float64(1)}
}

func TestSessionCleanupRemovesOnlyTheNamedNamespace(t *testing.T) {
	tools, sandbox := cleanupTools(t)
	writeSandboxFile(t, sandbox, "sessions/sess_1/runs/run_1/files/notes/todo.txt", "owned")
	writeSandboxFile(t, sandbox, "sessions/sess_1/runs/run_2/files/other.txt", "owned too")
	writeSandboxFile(t, sandbox, "sessions/sess_2/runs/run_9/files/private.txt", "another session")
	writeSandboxFile(t, sandbox, "legacy.txt", "pre-existing")

	result, err := tools.SessionCleanup(cleanupArgs("sess_1"))
	if err != nil {
		t.Fatalf("SessionCleanup: %v", err)
	}

	if removed, _ := result["namespaceRemoved"].(bool); !removed {
		t.Fatalf("result = %+v, want the namespace reported as removed", result)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "sessions", "sess_1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session namespace still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "sessions", "sess_2", "runs", "run_9", "files", "private.txt")); err != nil {
		t.Fatalf("another session's artifact was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "legacy.txt")); err != nil {
		t.Fatalf("pre-existing root file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "sessions")); err != nil {
		t.Fatalf("shared sessions root was removed: %v", err)
	}
}

func TestSessionCleanupReportsBoundedCountsWithoutPathsOrContent(t *testing.T) {
	tools, sandbox := cleanupTools(t)
	writeSandboxFile(t, sandbox, "sessions/sess_1/runs/run_1/files/notes/todo.txt", "secret content")

	result, err := tools.SessionCleanup(cleanupArgs("sess_1"))
	if err != nil {
		t.Fatalf("SessionCleanup: %v", err)
	}

	files, filesReported := result["removedFiles"].(int)
	directories, directoriesReported := result["removedDirectories"].(int)
	if !filesReported || !directoriesReported {
		t.Fatalf("result = %+v, want removedFiles and removedDirectories counts", result)
	}
	if files != 1 {
		t.Fatalf("removedFiles = %d, want 1", files)
	}
	// runs, runs/run_1, runs/run_1/files, .../files/notes and the namespace itself.
	if directories != 5 {
		t.Fatalf("removedDirectories = %d, want 5", directories)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"todo.txt", "notes", "run_1", "secret content", "sessions/"} {
		if strings.Contains(string(encoded), leak) {
			t.Fatalf("result %s leaks %q", encoded, leak)
		}
	}
}

func TestSessionCleanupIsIdempotentForAMissingNamespace(t *testing.T) {
	tools, sandbox := cleanupTools(t)
	writeSandboxFile(t, sandbox, "sessions/sess_2/runs/run_9/files/private.txt", "another session")

	result, err := tools.SessionCleanup(cleanupArgs("sess_1"))
	if err != nil {
		t.Fatalf("SessionCleanup for a missing namespace: %v", err)
	}

	if removed, _ := result["namespaceRemoved"].(bool); removed {
		t.Fatalf("result = %+v, want namespaceRemoved false for a namespace that was already gone", result)
	}
	if result["removedFiles"] != 0 || result["removedDirectories"] != 0 {
		t.Fatalf("result = %+v, want zero counts", result)
	}
}

func TestSessionCleanupIsIdempotentWhenNoSessionStorageExistsAtAll(t *testing.T) {
	tools, _ := cleanupTools(t)

	if _, err := tools.SessionCleanup(cleanupArgs("sess_1")); err != nil {
		t.Fatalf("SessionCleanup with no sessions root: %v", err)
	}
}

func TestSessionCleanupRepeatsWithoutError(t *testing.T) {
	tools, sandbox := cleanupTools(t)
	writeSandboxFile(t, sandbox, "sessions/sess_1/runs/run_1/files/notes.txt", "owned")

	if _, err := tools.SessionCleanup(cleanupArgs("sess_1")); err != nil {
		t.Fatal(err)
	}
	second, err := tools.SessionCleanup(cleanupArgs("sess_1"))
	if err != nil {
		t.Fatalf("repeated SessionCleanup: %v", err)
	}
	if removed, _ := second["namespaceRemoved"].(bool); removed {
		t.Fatalf("second cleanup = %+v, want namespaceRemoved false", second)
	}
}

func TestSessionCleanupRejectsUnsafeSessionIdentifiers(t *testing.T) {
	tools, sandbox := cleanupTools(t)
	writeSandboxFile(t, sandbox, "legacy.txt", "pre-existing")
	writeSandboxFile(t, sandbox, "sessions/sess_1/runs/run_1/files/notes.txt", "owned")
	unsafe := map[string]any{
		"parent traversal":   "..",
		"current directory":  ".",
		"nested traversal":   "../..",
		"path separator":     "sess_1/runs",
		"absolute":           "/sess_1",
		"leading dot":        ".sess_1",
		"embedded null":      "sess\x00_1",
		"empty":              "",
		"blank":              "   ",
		"trailing separator": "sess_1/",
		"escape sequence":    "sessions/../..",
		"too long":           strings.Repeat("s", 200),
		"unexpected symbol":  "sess_1;rm",
	}
	for name, sessionID := range unsafe {
		t.Run(name, func(t *testing.T) {
			_, err := tools.SessionCleanup(map[string]any{"sessionId": sessionID, "lifecycleVersion": float64(1)})

			if err == nil {
				t.Fatal("SessionCleanup accepted an unsafe session identifier")
			}
			if !IsInvalidParams(err) {
				t.Fatalf("error = %v, want invalid params", err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(sandbox, "legacy.txt")); err != nil {
		t.Fatalf("a refused cleanup removed a root file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "sessions", "sess_1", "runs", "run_1", "files", "notes.txt")); err != nil {
		t.Fatalf("a refused cleanup removed session storage: %v", err)
	}
}

func TestSessionCleanupRejectsMalformedArguments(t *testing.T) {
	tools, _ := cleanupTools(t)
	cases := map[string]map[string]any{
		"missing session":           {"lifecycleVersion": float64(1)},
		"missing lifecycle":         {"sessionId": "sess_1"},
		"unknown argument":          {"sessionId": "sess_1", "lifecycleVersion": float64(1), "force": true},
		"non-string session":        {"sessionId": 1, "lifecycleVersion": float64(1)},
		"non-numeric lifecycle":     {"sessionId": "sess_1", "lifecycleVersion": "1"},
		"fractional lifecycle":      {"sessionId": "sess_1", "lifecycleVersion": float64(1.5)},
		"zero lifecycle":            {"sessionId": "sess_1", "lifecycleVersion": float64(0)},
		"negative lifecycle":        {"sessionId": "sess_1", "lifecycleVersion": float64(-1)},
		"out of range lifecycle":    {"sessionId": "sess_1", "lifecycleVersion": float64(1) * 1e18},
		"no arguments at all":       {},
		"path argument smuggled in": {"sessionId": "sess_1", "lifecycleVersion": float64(1), "path": "legacy.txt"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tools.SessionCleanup(args); err == nil || !IsInvalidParams(err) {
				t.Fatalf("SessionCleanup error = %v, want invalid params", err)
			}
		})
	}
}

func TestSessionCleanupDoesNotFollowSymlinksOutOfTheNamespace(t *testing.T) {
	tools, sandbox := cleanupTools(t)
	writeSandboxFile(t, sandbox, "legacy.txt", "pre-existing")
	writeSandboxFile(t, sandbox, "outside/keep.txt", "must survive")
	writeSandboxFile(t, sandbox, "sessions/sess_1/runs/run_1/files/notes.txt", "owned")
	if err := os.Symlink(filepath.Join(sandbox, "outside"), filepath.Join(sandbox, "sessions", "sess_1", "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sandbox, "legacy.txt"), filepath.Join(sandbox, "sessions", "sess_1", "runs", "run_1", "legacy-link")); err != nil {
		t.Fatal(err)
	}

	if _, err := tools.SessionCleanup(cleanupArgs("sess_1")); err != nil {
		t.Fatalf("SessionCleanup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sandbox, "outside", "keep.txt")); err != nil {
		t.Fatalf("cleanup followed a symlink out of the namespace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "legacy.txt")); err != nil {
		t.Fatalf("cleanup followed a symlink to a root file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "sessions", "sess_1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("namespace was not removed: %v", err)
	}
}

func TestSessionCleanupRefusesANamespaceThatIsNotADirectory(t *testing.T) {
	tools, sandbox := cleanupTools(t)
	writeSandboxFile(t, sandbox, "outside/keep.txt", "must survive")
	if err := os.MkdirAll(filepath.Join(sandbox, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sandbox, "outside"), filepath.Join(sandbox, "sessions", "sess_1")); err != nil {
		t.Fatal(err)
	}

	_, err := tools.SessionCleanup(cleanupArgs("sess_1"))

	if err == nil {
		t.Fatal("SessionCleanup accepted a symlinked namespace")
	}
	if _, statErr := os.Stat(filepath.Join(sandbox, "outside", "keep.txt")); statErr != nil {
		t.Fatalf("cleanup removed the symlink target's content: %v", statErr)
	}
}

func TestSessionCleanupIsNotReachableThroughAgentToolCalls(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: ""}}
	tools, sandbox := guardedTools(t, guard)
	writeSandboxFile(t, sandbox, "sessions/sess_1/runs/run_1/files/notes.txt", "owned")

	_, err := tools.CallRequestContext(context.Background(), CallRequest{
		Name:            "files.session_cleanup",
		Args:            cleanupArgs("sess_1"),
		ProvenanceToken: "capability",
		AgentID:         "general_assistant",
	})

	if err == nil {
		t.Fatal("an agent tool call reached session cleanup")
	}
	if _, statErr := os.Stat(filepath.Join(sandbox, "sessions", "sess_1", "runs", "run_1", "files", "notes.txt")); statErr != nil {
		t.Fatalf("the agent call removed session storage: %v", statErr)
	}
}

func TestSessionCleanupIsNotReachableThroughTheLegacyCallPath(t *testing.T) {
	tools, sandbox := cleanupTools(t)
	writeSandboxFile(t, sandbox, "sessions/sess_1/runs/run_1/files/notes.txt", "owned")

	_, err := tools.Call("files.session_cleanup", cleanupArgs("sess_1"), "approval", "general_assistant")

	if err == nil {
		t.Fatal("the ordinary tool dispatch reached session cleanup")
	}
	if _, statErr := os.Stat(filepath.Join(sandbox, "sessions", "sess_1", "runs", "run_1", "files", "notes.txt")); statErr != nil {
		t.Fatalf("the ordinary dispatch removed session storage: %v", statErr)
	}
}

func TestSessionCleanupStopsOnAnOversizedNamespace(t *testing.T) {
	tools, sandbox := cleanupTools(t)
	directory := filepath.Join(sandbox, "sessions", "sess_1", "files")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= MaxSessionCleanupEntries; index++ {
		if err := os.WriteFile(filepath.Join(directory, "f"+strconv.Itoa(index)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	_, err := tools.SessionCleanup(cleanupArgs("sess_1"))

	if err == nil {
		t.Fatal("SessionCleanup removed an unbounded number of entries")
	}
	if IsInvalidParams(err) {
		t.Fatalf("error = %v, want an operational failure rather than invalid params", err)
	}
}

func TestSessionCleanupAfterAnOversizedFailureCompletesOnRetry(t *testing.T) {
	tools, sandbox := cleanupTools(t)
	directory := filepath.Join(sandbox, "sessions", "sess_1", "files")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= MaxSessionCleanupEntries; index++ {
		if err := os.WriteFile(filepath.Join(directory, "f"+strconv.Itoa(index)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tools.SessionCleanup(cleanupArgs("sess_1")); err == nil {
		t.Fatal("oversized namespace was accepted")
	}

	// Each attempt leaves less behind, so a retry finishes rather than looping
	// on the same refusal forever.
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if _, lastErr = tools.SessionCleanup(cleanupArgs("sess_1")); lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		t.Fatalf("retried cleanup never completed: %v", lastErr)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "sessions", "sess_1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("namespace survived the retried cleanup: %v", err)
	}
}
