package tests

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Runtime SQLite backup directories (data.backup-*, data.worktree-backup-*) are
// created ad hoc beside turing-backend/data during maintenance and worktree
// operations. They hold live database state and this repository is public, so
// those paths must stay untracked and must never enter a Docker build context.
// Every assertion below reads Git metadata or ignore rules only; no database file
// is ever opened.

// repoRootFromTests is the repository root relative to this package's directory.
const repoRootFromTests = "../.."

// runtimeBackupPrefixes are the leading path-component names these backups use. A
// timestamp suffix follows, so matching is by prefix rather than by exact name.
var runtimeBackupPrefixes = []string{
	"data.backup-",
	"data.worktree-backup-",
}

func TestGitignoreCoversRuntimeDatabaseBackupDirectories(t *testing.T) {
	lines := ignoreFileLines(t, ".gitignore")
	for _, pattern := range []string{"data.backup-*", "data.worktree-backup-*"} {
		if !lines[pattern] {
			t.Errorf(".gitignore missing %q", pattern)
		}
	}
}

// A later negation would silently re-admit exactly what this containment removed,
// while the pattern-presence assertions above still passed.
func TestRuntimeBackupIgnoreRulesAreNotNegated(t *testing.T) {
	for _, ignoreFile := range []string{".gitignore", ".dockerignore"} {
		for line := range ignoreFileLines(t, ignoreFile) {
			if !strings.HasPrefix(line, "!") {
				continue
			}
			if runtimeBackupComponent(strings.TrimPrefix(line, "!")) != "" {
				t.Errorf("%s re-includes a runtime database backup path: %q", ignoreFile, line)
			}
		}
	}
}

func TestRuntimeDatabaseBackupPathsAreNotTracked(t *testing.T) {
	var tracked []string
	for _, path := range gitTrackedPaths(t) {
		if runtimeBackupComponent(path) != "" {
			tracked = append(tracked, path)
		}
	}
	if len(tracked) > 0 {
		t.Errorf("runtime database backup artifacts are tracked in this public repository: %v", tracked)
	}
}

func TestRuntimeDatabaseBackupPathsAreGitIgnored(t *testing.T) {
	for _, path := range []string{
		// The paths this containment work removed from the repository tip.
		"turing-backend/data.backup-20260815-222941",
		"turing-backend/data.backup-20260815-222941/turing.db",
		"turing-backend/data.backup-20260815-222941/.gitkeep",
		"turing-backend/data.worktree-backup-20260816-002058",
		"turing-backend/data.worktree-backup-20260816-002058/turing.db",
		"turing-backend/data.worktree-backup-20260816-002058/.gitkeep",
		// Backups a future run could create, at the backend root and the repo root.
		"turing-backend/data.backup-29991231-235959/turing.db",
		"turing-backend/data.worktree-backup-29991231-235959/turing.db",
		"data.backup-29991231-235959/turing.db",
	} {
		if !gitIgnores(t, path) {
			t.Errorf("%q is not ignored; a runtime database backup could be committed", path)
		}
	}
}

func TestRuntimeBackupComponentMatchesOnlyBackupPaths(t *testing.T) {
	for _, testCase := range []struct {
		path string
		want string
	}{
		{"turing-backend/data.backup-20260815-222941/turing.db", "data.backup-20260815-222941"},
		{"data.worktree-backup-20260816-002058/turing.db", "data.worktree-backup-20260816-002058"},
		{"turing-backend/data.backup-", "data.backup-"},
		// Near misses that must stay trackable.
		{"turing-backend/data/turing.db", ""},
		{"turing-backend/notdata.backup-1/x", ""},
		{"turing-backend/data.backupX/x", ""},
		{"turing-backend/database/schema.sql", ""},
		{"docs/data-backup-policy.md", ""},
	} {
		if got := runtimeBackupComponent(testCase.path); got != testCase.want {
			t.Errorf("runtimeBackupComponent(%q) = %q, want %q", testCase.path, got, testCase.want)
		}
	}
}

// git quotes non-ASCII paths unless -z is used. A quoted repository-root backup
// directory arrives as `"data.backup-\303\251/...`, whose first component starts
// with a quote and would slip past the scan, so tracked paths are read NUL
// separated instead.
func TestTrackedPathsAreReadUnquoted(t *testing.T) {
	paths := splitNUL("go.mod\x00data.backup-\u00e9/turing.db\x00")
	want := []string{"go.mod", "data.backup-\u00e9/turing.db"}
	if len(paths) != len(want) {
		t.Fatalf("splitNUL returned %d paths (%v), want %d", len(paths), paths, len(want))
	}
	for index, path := range paths {
		if path != want[index] {
			t.Errorf("path %d = %q, want %q", index, path, want[index])
		}
	}
	if component := runtimeBackupComponent(paths[1]); component != "data.backup-\u00e9" {
		t.Errorf("unquoted non-ASCII backup path was not detected, got %q", component)
	}
}

// runtimeBackupComponent returns the offending path component, or "" when the
// path is not inside a runtime backup directory.
func runtimeBackupComponent(path string) string {
	for _, component := range strings.Split(path, "/") {
		for _, prefix := range runtimeBackupPrefixes {
			if strings.HasPrefix(component, prefix) {
				return component
			}
		}
	}
	return ""
}

// ignoreFileLines returns the meaningful (non-empty, non-comment) lines of an
// ignore file at the repository root, as a set.
func ignoreFileLines(t *testing.T, name string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRootFromTests, name))
	if err != nil {
		t.Fatal(err)
	}
	lines := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			lines[line] = true
		}
	}
	return lines
}

func gitTrackedPaths(t *testing.T) []string {
	t.Helper()
	return splitNUL(gitOutput(t, "ls-files", "-z"))
}

func splitNUL(output string) []string {
	var paths []string
	for _, path := range strings.Split(output, "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func gitIgnores(t *testing.T, path string) bool {
	t.Helper()
	command := exec.Command(gitBinary(t), "-C", repoRootFromTests, "check-ignore", "-q", "--", path)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return true
	}
	// git check-ignore exits 1 when no ignore rule matches, and >1 on real errors.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false
	}
	t.Fatalf("git check-ignore %q: %v: %s", path, err, strings.TrimSpace(stderr.String()))
	return false
}

func gitOutput(t *testing.T, args ...string) string {
	t.Helper()
	command := exec.Command(gitBinary(t), append([]string{"-C", repoRootFromTests}, args...)...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(stderr.String()))
	}
	return string(output)
}

func gitBinary(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("git")
	if err != nil {
		// Fail closed. This guard keeps live database state out of a public
		// repository, so an environment that cannot prove containment is a
		// failure, not a reason to skip quietly.
		t.Fatalf("git is required to verify repository containment: %v", err)
	}
	return binary
}
