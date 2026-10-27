package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// wrapperTree is a copy of turing-backend/turing in a throwaway checkout,
// with a fake go that builds a stub binary and a fake id that names the user.
type wrapperTree struct {
	root, backend, wrapper, bin, goLog string
}

func newWrapperTree(t *testing.T) wrapperTree {
	t.Helper()
	// The tree is resolved first: the wrapper reports physical paths, and on
	// macOS a temporary directory is reached through a symlink.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tree := wrapperTree{
		root:    root,
		backend: filepath.Join(root, "turing-backend"),
		bin:     filepath.Join(root, "fakebin"),
		goLog:   filepath.Join(root, "go.log"),
	}
	tree.wrapper = filepath.Join(tree.backend, "turing")
	for _, dir := range []string{tree.backend, tree.bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	copyScript(t, "../turing", tree.wrapper)

	// The stub echoes what it was started with and exits with STUB_EXIT, so a
	// test can tell the wrapper ran it with exec rather than as a child.
	fakeGo := "#!/bin/sh\n" +
		"printf '%s\\n' \"$PWD\" >> \"$GO_LOG\"\n" +
		"printf '%s\\n' \"$*\" >> \"$GO_LOG\"\n" +
		"if [ -n \"$GO_FAIL\" ]; then echo 'go: build failed' >&2; exit 1; fi\n" +
		"[ \"$1\" = build ] && [ \"$2\" = -o ] || exit 3\n" +
		"mkdir -p \"$(dirname \"$3\")\"\n" +
		"printf '#!/bin/sh\\necho \"STUB $*\"\\nexit \"${STUB_EXIT:-0}\"\\n' > \"$3\"\n" +
		"chmod +x \"$3\"\n"
	fakeID := "#!/bin/sh\n[ \"$1\" = -u ] && printf '%s\\n' \"${FAKE_UID:-1000}\"\n"
	for name, content := range map[string]string{"go": fakeGo, "id": fakeID} {
		if err := os.WriteFile(filepath.Join(tree.bin, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return tree
}

func (tree wrapperTree) run(t *testing.T, script string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(script, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = append([]string{"PATH=" + tree.bin + ":/usr/bin:/bin", "GO_LOG=" + tree.goLog}, env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return out.String(), errOut.String(), exit.ExitCode()
	}
	if err != nil {
		t.Fatalf("running %s: %v", script, err)
	}
	return out.String(), errOut.String(), 0
}

func TestTuringWrapperBuildsTheCommandAndRunsItWithTheBackendDirectory(t *testing.T) {
	tree := newWrapperTree(t)

	stdout, stderr, code := tree.run(t, tree.wrapper, nil, "logs", "runtime", "-f")
	if code != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if want := "STUB --backend-dir " + tree.backend + " logs runtime -f\n"; stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}

	goLog, err := os.ReadFile(tree.goLog)
	if err != nil {
		t.Fatal(err)
	}
	built := filepath.Join(tree.backend, ".turing", "bin", "turing")
	wantLog := tree.root + "\nbuild -o " + built + " ./turing-backend/cmd/turing\n"
	if string(goLog) != wantLog {
		t.Fatalf("go was run as %q, want %q", goLog, wantLog)
	}
}

func TestTuringWrapperRunsTheBinaryItselfSoItsExitCodeSurvives(t *testing.T) {
	tree := newWrapperTree(t)
	for _, want := range []int{2, 130} {
		_, _, code := tree.run(t, tree.wrapper, []string{"STUB_EXIT=" + strconv.Itoa(want)}, "logs")
		if code != want {
			t.Errorf("exit = %d, want %d", code, want)
		}
	}
}

func TestTuringWrapperFollowsSymlinksToTheSameBackend(t *testing.T) {
	tree := newWrapperTree(t)
	linkDir := filepath.Join(tree.root, "home", "bin")
	if err := os.MkdirAll(linkDir, 0o700); err != nil {
		t.Fatal(err)
	}
	absolute := filepath.Join(linkDir, "turing")
	if err := os.Symlink(tree.wrapper, absolute); err != nil {
		t.Fatal(err)
	}
	// A link to a link, the second relative, as ~/.local/bin setups produce.
	relative := filepath.Join(tree.root, "home", "turing-link")
	if err := os.Symlink(filepath.Join("bin", "turing"), relative); err != nil {
		t.Fatal(err)
	}

	for name, link := range map[string]string{"absolute link": absolute, "relative link to a link": relative} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, code := tree.run(t, link, nil, "help")
			if code != 0 {
				t.Fatalf("exit = %d\nstderr: %s", code, stderr)
			}
			if want := "STUB --backend-dir " + tree.backend + " help\n"; stdout != want {
				t.Fatalf("stdout = %q, want %q", stdout, want)
			}
		})
	}
}

func TestTuringWrapperRefusesRootBeforeBuildingAnything(t *testing.T) {
	tree := newWrapperTree(t)

	stdout, stderr, code := tree.run(t, tree.wrapper, []string{"FAKE_UID=0"}, "start")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "root") || stdout != "" {
		t.Fatalf("stdout = %q, stderr = %q", stdout, stderr)
	}
	if _, err := os.Stat(tree.goLog); !os.IsNotExist(err) {
		t.Fatalf("go ran for root: stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(tree.backend, ".turing")); !os.IsNotExist(err) {
		t.Fatalf("root created the build directory: stat err = %v", err)
	}
}

func TestTuringWrapperDoesNotRunAStaleBinaryWhenTheBuildFails(t *testing.T) {
	tree := newWrapperTree(t)
	if _, stderr, code := tree.run(t, tree.wrapper, nil, "help"); code != 0 {
		t.Fatalf("first run: exit = %d\nstderr: %s", code, stderr)
	}

	stdout, stderr, code := tree.run(t, tree.wrapper, []string{"GO_FAIL=1"}, "help")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if stdout != "" {
		t.Fatalf("the previous build ran: %q", stdout)
	}
	if !strings.Contains(stderr, "go: build failed") || !strings.Contains(stderr, "Go 1.26") {
		t.Fatalf("stderr = %q, want go's message and the Go version needed", stderr)
	}
}
