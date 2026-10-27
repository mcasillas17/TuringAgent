package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const runMainEnv = "TURING_TEST_RUN_MAIN"

// TestMainHelperProcess is not a test of its own. Run as a subprocess by
// runMain, it calls main() with one argument, exactly as the image's
// ENTRYPOINT would, and main exits the process.
func TestMainHelperProcess(t *testing.T) {
	argument, ok := os.LookupEnv(runMainEnv)
	if !ok {
		t.Skip("runs only as runMain's subprocess")
	}
	os.Args = []string{"turing-orchestrator-go", argument}
	main()
	t.Fatal("main returned instead of exiting")
}

// runMain runs main() with argument in a fresh process with exactly environ,
// and returns its exit code and output. A process still running at the
// deadline has started the service instead of checking, which fails the test.
func runMain(t *testing.T, argument string, environ []string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMainHelperProcess$")
	cmd.Env = append(append([]string{}, environ...), runMainEnv+"="+argument)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("main(%q) was still running after 10s: it started the orchestrator", argument)
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, stdout.String(), stderr.String()
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), stdout.String(), stderr.String()
	default:
		t.Fatal(err)
		return 0, "", ""
	}
}

// What the org.turing.checks label promises is what main() does with the
// image's argument, so this runs main() itself rather than servicecmd.Main.
func TestMainHonoursTheLabelledChecks(t *testing.T) {
	// A main that ignored its argument would try to open this database and
	// fail, rather than serve, because its directory does not exist.
	environ := validEnviron("DATABASE_PATH=" + filepath.Join(t.TempDir(), "absent", "turing.db"))

	if code, stdout, stderr := runMain(t, "check-config", environ); code != 0 || !strings.Contains(stdout, "configuration is valid") {
		t.Errorf("check-config with a valid environment: code = %d, stdout = %q, stderr = %q; want 0", code, stdout, stderr)
	}
	if code, _, stderr := runMain(t, "check-config", without(environ, "TURING_CLIENT_API_KEY")); code != 1 ||
		!strings.Contains(stderr, "TURING_CLIENT_API_KEY") {
		t.Errorf("check-config with a refused environment: code = %d, stderr = %q; want 1 naming the key", code, stderr)
	}
	if code, _, stderr := runMain(t, "check-engine", environ); code != 2 || !strings.Contains(stderr, "check-engine") {
		t.Errorf("an unknown argument: code = %d, stderr = %q; want 2 naming it", code, stderr)
	}
}
