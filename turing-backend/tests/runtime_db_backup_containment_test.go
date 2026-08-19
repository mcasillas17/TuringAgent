package tests

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Runtime SQLite backup directories (data.backup-*, data.worktree-backup-*) are
// created ad hoc beside turing-backend/data and hold live database state. This
// repository is public, so those paths must stay untracked and must never enter a
// Docker build context. Every assertion below reads Git metadata or ignore rules
// only; no database file is ever opened.

// repoRootFromTests is the repository root relative to this package's directory.
const repoRootFromTests = "../.."

// runtimeBackupPrefixes are the leading path-component names the backup helpers
// produce. A timestamp suffix follows, so matching is by prefix rather than by
// exact name.
var runtimeBackupPrefixes = []string{
	"data.backup-",
	"data.worktree-backup-",
}

func TestGitignoreCoversRuntimeDatabaseBackupDirectories(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRootFromTests, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	lines := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines[line] = true
		}
	}
	for _, pattern := range []string{"data.backup-*", "data.worktree-backup-*"} {
		if !lines[pattern] {
			t.Errorf(".gitignore missing %q", pattern)
		}
	}
}

func TestRuntimeDatabaseBackupPathsAreNotTracked(t *testing.T) {
	var tracked []string
	for _, path := range gitLines(t, "ls-files") {
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

func gitIgnores(t *testing.T, path string) bool {
	t.Helper()
	err := exec.Command(gitBinary(t), "-C", repoRootFromTests, "check-ignore", "-q", "--", path).Run()
	if err == nil {
		return true
	}
	// git check-ignore exits 1 when no ignore rule matches, and >1 on real errors.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false
	}
	t.Fatalf("git check-ignore %q: %v", path, err)
	return false
}

func gitLines(t *testing.T, args ...string) []string {
	t.Helper()
	output, err := exec.Command(gitBinary(t), append([]string{"-C", repoRootFromTests}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	var lines []string
	for _, line := range strings.Split(string(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func gitBinary(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is required to verify repository containment: %v", err)
	}
	return binary
}
