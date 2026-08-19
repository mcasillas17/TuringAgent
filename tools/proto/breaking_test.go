package proto_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBreakingCompatibility(t *testing.T) {
	requireBuf(t)

	tests := []struct {
		name            string
		fixture         string
		wantFailure     bool
		wantDiagnostics []string
	}{
		{
			name:    "additive field",
			fixture: "additive",
		},
		{
			name:            "removed live field",
			fixture:         "removed",
			wantFailure:     true,
			wantDiagnostics: []string{`field "2"`, "was deleted"},
		},
		{
			name:            "removed and reserved live field",
			fixture:         "removed_reserved",
			wantFailure:     true,
			wantDiagnostics: []string{`field "2"`, "was deleted"},
		},
		{
			name:            "renumbered live field",
			fixture:         "renumbered",
			wantFailure:     true,
			wantDiagnostics: []string{`field "2"`, "was deleted"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newCompatibilityRepo(t, test.fixture, false)
			command := exec.Command(filepath.Join(repo, "tools", "proto", "breaking.sh"), "origin/main")
			command.Dir = repo
			output, err := command.CombinedOutput()

			if test.wantFailure && err == nil {
				t.Fatalf("breaking.sh succeeded for %s schema", test.fixture)
			}
			if !test.wantFailure && err != nil {
				t.Fatalf("breaking.sh failed for %s schema: %v\n%s", test.fixture, err, output)
			}
			for _, diagnostic := range test.wantDiagnostics {
				if !strings.Contains(string(output), diagnostic) {
					t.Fatalf("breaking.sh output = %q, want it to contain %q", output, diagnostic)
				}
			}

			runGit(t, repo, "rev-parse", "--verify", "refs/remotes/origin/main")
			if shallow := runGit(t, repo, "rev-parse", "--is-shallow-repository"); shallow != "false" {
				t.Fatalf("breaking.sh changed a full repository into a shallow one")
			}
		})
	}
}

func TestBreakingCompatibilityInShallowCheckout(t *testing.T) {
	requireBuf(t)

	repo := newCompatibilityRepo(t, "additive", true)
	command := exec.Command(filepath.Join(repo, "tools", "proto", "breaking.sh"), "origin/main")
	command.Dir = repo
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("breaking.sh failed in shallow checkout: %v\n%s", err, output)
	}
	runGit(t, repo, "rev-parse", "--verify", "refs/remotes/origin/main")
	if shallow := runGit(t, repo, "rev-parse", "--is-shallow-repository"); shallow != "true" {
		t.Fatalf("breaking.sh unexpectedly changed shallow repository state to %q", shallow)
	}
}

func TestBreakingRejectsUnsupportedBufVersion(t *testing.T) {
	binDir := t.TempDir()
	writeTool(t, binDir, "buf", "#!/bin/sh\necho '9.9.9'\n")

	command := exec.Command("./breaking.sh", "origin/main")
	command.Env = append(os.Environ(), "PATH="+binDir+":/usr/bin:/bin")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("breaking.sh succeeded with unsupported Buf version")
	}
	want := "buf 1.72.0 is required (found: 9.9.9)"
	if !strings.Contains(string(output), want) {
		t.Fatalf("breaking.sh output = %q, want it to contain %q", output, want)
	}
}

func TestBreakingRejectsInvalidBaseRef(t *testing.T) {
	binDir := t.TempDir()
	writeTool(t, binDir, "buf", "#!/bin/sh\necho '1.72.0'\n")

	for _, baseRef := range []string{"main", "origin/../main"} {
		t.Run(baseRef, func(t *testing.T) {
			command := exec.Command("./breaking.sh", baseRef)
			command.Env = append(os.Environ(), "PATH="+binDir+":/usr/bin:/bin")
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("breaking.sh succeeded with invalid base ref %q", baseRef)
			}
			want := "base ref must be a valid remote-tracking ref"
			if !strings.Contains(string(output), want) {
				t.Fatalf("breaking.sh output = %q, want it to contain %q", output, want)
			}
		})
	}
}

func TestBreakingDoesNotUseStaleBaseWhenFetchFails(t *testing.T) {
	tempDir := t.TempDir()
	repo := filepath.Join(tempDir, "repo")
	runGit(t, tempDir, "init", "--initial-branch=feature", repo)
	runGit(t, repo, "config", "user.name", "Turing Proto Test")
	runGit(t, repo, "config", "user.email", "proto-test@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README")
	runGit(t, repo, "commit", "-m", "fixture")
	runGit(t, repo, "remote", "add", "origin", filepath.Join(tempDir, "missing.git"))
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")

	copyFile(t, "breaking.sh", filepath.Join(repo, "tools", "proto", "breaking.sh"), 0o755)
	bufLog := filepath.Join(tempDir, "buf.log")
	binDir := t.TempDir()
	writeTool(t, binDir, "buf", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '1.72.0'; exit 0; fi\nprintf 'called\\n' > \"$BUF_LOG\"\n")

	command := exec.Command(filepath.Join(repo, "tools", "proto", "breaking.sh"), "origin/main")
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+binDir+":/usr/bin:/bin",
		"BUF_LOG="+bufLog,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("breaking.sh succeeded after its base fetch failed")
	}
	want := "failed to refresh base ref origin/main"
	if !strings.Contains(string(output), want) {
		t.Fatalf("breaking.sh output = %q, want it to contain %q", output, want)
	}
	if _, err := os.Stat(bufLog); !os.IsNotExist(err) {
		t.Fatalf("buf was called after fetch failure; stat error = %v", err)
	}
}

func TestBreakingRefreshesRewrittenBaseBranch(t *testing.T) {
	tempDir := t.TempDir()
	remote := filepath.Join(tempDir, "origin.git")
	runGit(t, tempDir, "init", "--bare", remote)

	seed := filepath.Join(tempDir, "seed")
	runGit(t, tempDir, "init", "--initial-branch=main", seed)
	runGit(t, seed, "config", "user.name", "Turing Proto Test")
	runGit(t, seed, "config", "user.email", "proto-test@example.invalid")
	copyFile(t,
		filepath.Join("testdata", "breaking", "base", "turing", "v1", "example.proto"),
		filepath.Join(seed, "proto", "turing", "v1", "example.proto"),
		0o644,
	)
	runGit(t, seed, "add", "proto")
	runGit(t, seed, "commit", "-m", "baseline")
	runGit(t, seed, "remote", "add", "origin", remote)
	runGit(t, seed, "push", "--set-upstream", "origin", "main")

	repo := filepath.Join(tempDir, "repo")
	runGit(t, tempDir, "clone", "--branch=main", remote, repo)
	copyFile(t, "breaking.sh", filepath.Join(repo, "tools", "proto", "breaking.sh"), 0o755)

	if err := os.WriteFile(filepath.Join(seed, "marker"), []byte("rewritten\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", "marker")
	runGit(t, seed, "commit", "--amend", "-m", "rewritten baseline")
	runGit(t, seed, "push", "--force", "origin", "main")
	rewrittenCommit := runGit(t, seed, "rev-parse", "HEAD")

	bufLog := filepath.Join(tempDir, "buf.log")
	binDir := t.TempDir()
	writeTool(t, binDir, "buf", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '1.72.0'; exit 0; fi\nprintf '%s\\n' \"$*\" > \"$BUF_LOG\"\n")

	command := exec.Command(filepath.Join(repo, "tools", "proto", "breaking.sh"), "origin/main")
	command.Dir = repo
	command.Env = append(os.Environ(),
		"PATH="+binDir+":/usr/bin:/bin",
		"BUF_LOG="+bufLog,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("breaking.sh rejected rewritten base branch: %v\n%s", err, output)
	}
	if got := runGit(t, repo, "rev-parse", "refs/remotes/origin/main"); got != rewrittenCommit {
		t.Fatalf("refreshed base commit = %s, want %s", got, rewrittenCommit)
	}
	if _, err := os.Stat(bufLog); err != nil {
		t.Fatalf("buf was not called after base refresh: %v", err)
	}
}

func requireBuf(t *testing.T) {
	t.Helper()
	path, err := exec.LookPath("buf")
	if err != nil {
		if os.Getenv("TURING_REQUIRE_BUF") == "1" {
			t.Fatal("buf is required when TURING_REQUIRE_BUF=1")
		}
		t.Skip("buf is not installed")
	}
	command := exec.Command(path, "--version")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("buf --version: %v\n%s", err, output)
	}
	if version := strings.TrimSpace(string(output)); version != "1.72.0" {
		t.Fatalf("buf version = %q, want 1.72.0", version)
	}
}

func newCompatibilityRepo(t *testing.T, fixture string, shallow bool) string {
	t.Helper()
	tempDir := t.TempDir()
	remote := filepath.Join(tempDir, "origin.git")
	runGit(t, tempDir, "init", "--bare", remote)

	seed := filepath.Join(tempDir, "seed")
	runGit(t, tempDir, "init", "--initial-branch=main", seed)
	runGit(t, seed, "config", "user.name", "Turing Proto Test")
	runGit(t, seed, "config", "user.email", "proto-test@example.invalid")
	copyFile(t,
		filepath.Join("testdata", "breaking", "base", "turing", "v1", "example.proto"),
		filepath.Join(seed, "proto", "turing", "v1", "example.proto"),
		0o644,
	)
	runGit(t, seed, "add", "proto")
	runGit(t, seed, "commit", "-m", "baseline")
	runGit(t, seed, "remote", "add", "origin", remote)
	runGit(t, seed, "push", "--set-upstream", "origin", "main")

	repo := filepath.Join(tempDir, "repo")
	if shallow {
		runGit(t, tempDir, "clone", "--depth=1", "--branch=main", "file://"+remote, repo)
	} else {
		runGit(t, tempDir, "clone", "--branch=main", remote, repo)
	}
	runGit(t, repo, "config", "user.name", "Turing Proto Test")
	runGit(t, repo, "config", "user.email", "proto-test@example.invalid")
	runGit(t, repo, "switch", "-c", "feature")
	runGit(t, repo, "update-ref", "-d", "refs/remotes/origin/main")

	copyFile(t,
		filepath.Join("testdata", "breaking", fixture, "turing", "v1", "example.proto"),
		filepath.Join(repo, "proto", "turing", "v1", "example.proto"),
		0o644,
	)
	copyFile(t, "breaking.sh", filepath.Join(repo, "tools", "proto", "breaking.sh"), 0o755)
	copyFile(t, filepath.Join("..", "..", "buf.yaml"), filepath.Join(repo, "buf.yaml"), 0o644)
	return repo
}

func copyFile(t *testing.T, source, destination string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", destination, err)
	}
	if err := os.WriteFile(destination, data, mode); err != nil {
		t.Fatalf("write %s: %v", destination, err)
	}
}

func runGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
