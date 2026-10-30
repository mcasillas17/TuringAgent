package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rotateTree is rotate-client-key.sh copied beside a fake turing, laid out as
// turing-backend is: scripts/rotate-client-key.sh and turing.
type rotateTree struct {
	script, args string
}

func newRotateTree(t *testing.T) rotateTree {
	t.Helper()
	backend := t.TempDir()
	scripts := filepath.Join(backend, "scripts")
	if err := os.Mkdir(scripts, 0o700); err != nil {
		t.Fatal(err)
	}
	tree := rotateTree{script: filepath.Join(scripts, "rotate-client-key.sh"), args: filepath.Join(backend, "args.log")}
	copyScript(t, "rotate-client-key.sh", tree.script)

	// The fake records each argument on its own line, so spaces survive, and
	// exits with FAKE_EXIT.
	fake := "#!/bin/sh\n" +
		"for arg in \"$@\"; do printf '%s\\n' \"$arg\" >> \"$ARGS_LOG\"; done\n" +
		"echo \"fake turing ran\"\n" +
		"exit \"${FAKE_EXIT:-0}\"\n"
	if err := os.WriteFile(filepath.Join(backend, "turing"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	return tree
}

func (tree rotateTree) run(t *testing.T, args ...string) (stdout string, code int) {
	t.Helper()
	cmd := exec.Command(tree.script, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=/usr/bin:/bin", "ARGS_LOG=" + tree.args, "FAKE_EXIT=" + os.Getenv("FAKE_EXIT")}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return out.String(), exit.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return out.String(), 0
}

func (tree rotateTree) recorded(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(tree.args)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func TestRotateClientKeyCallsTurningConnectRotateKey(t *testing.T) {
	tree := newRotateTree(t)

	stdout, code := tree.run(t)

	if code != 0 || !strings.Contains(stdout, "fake turing ran") {
		t.Fatalf("exit = %d, stdout = %q", code, stdout)
	}
	if got, want := strings.Join(tree.recorded(t), " "), "connect --rotate-key"; got != want {
		t.Fatalf("turing was run with %q, want %q", got, want)
	}
}

func TestRotateClientKeyPassesItsArgumentsOn(t *testing.T) {
	tree := newRotateTree(t)

	tree.run(t, "--yes", "an argument with spaces")

	want := []string{"connect", "--rotate-key", "--yes", "an argument with spaces"}
	if got := tree.recorded(t); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("turing was run with %q, want %q", got, want)
	}
}

func TestRotateClientKeyExitsWithTurningsStatus(t *testing.T) {
	t.Setenv("FAKE_EXIT", "7")
	tree := newRotateTree(t)

	if _, code := tree.run(t); code != 7 {
		t.Fatalf("exit = %d, want turing's 7", code)
	}
}

func TestRotateClientKeyFindsTheWrapperWhateverTheCurrentDirectory(t *testing.T) {
	tree := newRotateTree(t)

	// From its own directory by a relative path, then from elsewhere by an
	// absolute one.
	for dir, path := range map[string]string{filepath.Dir(tree.script): "./rotate-client-key.sh", t.TempDir(): tree.script} {
		cmd := exec.Command(path, "--yes")
		cmd.Dir = dir
		cmd.Env = []string{"PATH=/usr/bin:/bin", "ARGS_LOG=" + tree.args}
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s from %s: %v", path, dir, err)
		}
	}
	if got := tree.recorded(t); len(got) != 6 {
		t.Fatalf("turing was run with %q, want two runs of three arguments", got)
	}
}

func TestRotateClientKeyIsAThinExecOfTheWrapper(t *testing.T) {
	data, err := os.ReadFile("rotate-client-key.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		"set -euo pipefail",
		`exec "$(dirname "$0")/../turing" connect --rotate-key "$@"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("rotate-client-key.sh does not contain %q:\n%s", want, script)
		}
	}
	for _, gone := range []string{"openssl", "sed ", ".env.bak"} {
		if strings.Contains(script, gone) {
			t.Errorf("rotate-client-key.sh still contains %q: the key is rotated by the command now", gone)
		}
	}
}
