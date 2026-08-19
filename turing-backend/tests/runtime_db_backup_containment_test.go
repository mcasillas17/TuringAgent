package tests

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// git applies the last matching rule, so only a negation placed after the runtime
// backup patterns can re-admit them. Reject those by position instead of guessing
// which globs would match: .gitignore's existing negations (.env.example, .yarn/*)
// all precede the block and are unaffected. This closes the same-file gap that
// finitely-sampled check-ignore assertions cannot.
func TestGitignoreAddsNoNegationAfterRuntimeBackupRules(t *testing.T) {
	reached := false
	for _, line := range ignoreFileOrderedLines(t, ".gitignore") {
		if !reached {
			reached = runtimeBackupComponent(line) != ""
			continue
		}
		if strings.HasPrefix(line, "!") {
			t.Errorf(".gitignore negation %q follows the runtime database backup rules and can re-admit them", line)
		}
	}
	if !reached {
		t.Fatal(".gitignore has no runtime database backup rules to order against")
	}
}

// Docker's matcher is not a dependency of this module, so there is no way here to
// evaluate whether a given negation would re-admit a backup directory. .dockerignore
// carries no negations today, so reject them outright: a future one has to be
// weighed against this containment rather than silently re-including a build-context
// path that an earlier pattern excluded.
func TestDockerignoreHasNoNegationRules(t *testing.T) {
	for line := range ignoreFileLines(t, ".dockerignore") {
		if strings.HasPrefix(line, "!") {
			t.Errorf(".dockerignore adds negation %q; check it against the runtime database backup exclusions before allowing it", line)
		}
	}
}

func TestRuntimeDatabaseBackupPathsAreNotTracked(t *testing.T) {
	paths := gitTrackedPaths(t, repoRootFromTests)
	// This test proves an absence, which is only meaningful if the scan actually
	// saw the repository. A sentinel turns an empty scan into a failure.
	if !slices.Contains(paths, "go.mod") {
		t.Fatalf("tracked-path scan returned %d paths and no go.mod; it did not see this repository", len(paths))
	}
	var tracked []string
	for _, path := range paths {
		if runtimeBackupComponent(path) != "" {
			tracked = append(tracked, path)
		}
	}
	if len(tracked) > 0 {
		t.Errorf("runtime database backup artifacts are tracked in this public repository: %v", tracked)
	}
}

// Negations are checked by git itself rather than by scanning .gitignore for "!"
// lines, so real ordered rule precedence and glob semantics apply, including to
// nested .gitignore files along each path. This covers the paths it samples; the
// exact, unsampled backstop against anything slipping through is
// TestRuntimeDatabaseBackupPathsAreNotTracked.
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
// with a quote and would slip past the scan. Prove on a throwaway repository that
// the scan reads tracked paths unquoted; this fails if -z is ever dropped.
func TestTrackedPathScanDetectsNonASCIIBackupDirectory(t *testing.T) {
	repo := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		command := exec.Command(gitBinary(t), append([]string{"-C", repo}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	runGit("init", "-q")
	// Pin the quoting behaviour this test exists to defeat, so the fixture does
	// not depend on the developer's or runner's global core.quotePath setting.
	runGit("config", "core.quotePath", "true")

	backup := filepath.Join(repo, "data.backup-\u00e9")
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "probe"), []byte("probe"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "--force", ".")

	var found []string
	for _, path := range gitTrackedPaths(t, repo) {
		if component := runtimeBackupComponent(path); component != "" {
			found = append(found, component)
		}
	}
	if len(found) != 1 {
		t.Fatalf("scan found %v, want exactly one runtime backup component", found)
	}
	// Compared by prefix, not by exact name: macOS may store the directory name
	// in a different unicode normalization than it was written in.
	if !strings.HasPrefix(found[0], "data.backup-") || strings.Contains(found[0], `"`) {
		t.Errorf("backup component %q was not read unquoted", found[0])
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
	lines := make(map[string]bool)
	for _, line := range ignoreFileOrderedLines(t, name) {
		lines[line] = true
	}
	return lines
}

// ignoreFileOrderedLines returns the same lines in file order, which is what rule
// precedence depends on.
func ignoreFileOrderedLines(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRootFromTests, name))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}

func gitTrackedPaths(t *testing.T, repoDir string) []string {
	t.Helper()
	return splitNUL(gitOutput(t, repoDir, "ls-files", "-z"))
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

func gitOutput(t *testing.T, repoDir string, args ...string) string {
	t.Helper()
	command := exec.Command(gitBinary(t), append([]string{"-C", repoDir}, args...)...)
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
