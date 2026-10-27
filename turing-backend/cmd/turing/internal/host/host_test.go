package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type reply struct {
	stdout, stderr string
	code           int
	err            error
}

// scriptedRunner records every command and answers each from reply.
type scriptedRunner struct {
	calls []Command
	reply func(Command) reply
}

func (r *scriptedRunner) Run(_ context.Context, cmd Command) (int, error) {
	r.calls = append(r.calls, cmd)
	answer := r.reply(cmd)
	if cmd.Stdout != nil {
		_, _ = io.WriteString(cmd.Stdout, answer.stdout)
	}
	if cmd.Stderr != nil {
		_, _ = io.WriteString(cmd.Stderr, answer.stderr)
	}
	return answer.code, answer.err
}

func environHas(environ []string, name string) bool {
	return slices.ContainsFunc(environ, func(entry string) bool {
		return strings.HasPrefix(entry, name+"=")
	})
}

func TestComposeRunStripsRegistryNamesAndAddsTheExtras(t *testing.T) {
	runner := &scriptedRunner{reply: func(Command) reply { return reply{} }}
	compose := Compose{
		Runner:     runner,
		BackendDir: "/srv/turing-backend",
		Environ: []string{
			"PATH=/usr/bin:/bin",
			"HOME=/home/someone",
			"OPENAI_API_KEY=sk-exported",
			"OLLAMA_MODEL=exported-model",
			"TURING_CLIENT_API_KEY=exported-key",
			"ORCHESTRATOR_PUBLIC_PORT=1",
			"NOT_A_SETTING=kept",
		},
	}
	stdin := strings.NewReader("input")
	var stdout, stderr bytes.Buffer

	code, err := compose.Run(context.Background(), map[string]string{
		"ORCHESTRATOR_PUBLIC_PORT": "3456",
		"OPENAI_MODEL":             "extra-model",
	}, stdin, &stdout, &stderr, "up", "-d")
	if err != nil || code != 0 {
		t.Fatalf("Run = %d, %v", code, err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(runner.calls))
	}
	call := runner.calls[0]
	if call.Name != "./scripts/compose.sh" || call.Dir != "/srv/turing-backend" {
		t.Fatalf("command = %q in %q", call.Name, call.Dir)
	}
	if !slices.Equal(call.Args, []string{"up", "-d"}) {
		t.Fatalf("args = %q", call.Args)
	}
	if call.Stdin != io.Reader(stdin) || call.Stdout != io.Writer(&stdout) || call.Stderr != io.Writer(&stderr) {
		t.Fatalf("streams were not passed through")
	}
	for _, name := range []string{"OPENAI_API_KEY", "OLLAMA_MODEL", "TURING_CLIENT_API_KEY"} {
		if environHas(call.Env, name) {
			t.Errorf("%s was exported into the compose environment: %q", name, call.Env)
		}
	}
	for _, want := range []string{"PATH=/usr/bin:/bin", "HOME=/home/someone", "NOT_A_SETTING=kept", "ORCHESTRATOR_PUBLIC_PORT=3456", "OPENAI_MODEL=extra-model"} {
		if !slices.Contains(call.Env, want) {
			t.Errorf("environment is missing %q: %q", want, call.Env)
		}
	}
	if slices.Contains(call.Env, "ORCHESTRATOR_PUBLIC_PORT=1") {
		t.Errorf("the exported port survived beside the extra: %q", call.Env)
	}
}

func TestComposeEnvironmentIsTheSameForEveryCallAndNeverChangesTheCallersSlice(t *testing.T) {
	runner := &scriptedRunner{reply: func(Command) reply { return reply{} }}
	environ := []string{"PATH=/bin", "OPENAI_API_KEY=x"}
	compose := Compose{Runner: runner, BackendDir: "/b", Environ: environ}
	for range 2 {
		if _, err := compose.Run(context.Background(), nil, nil, io.Discard, io.Discard, "ps"); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(environ, []string{"PATH=/bin", "OPENAI_API_KEY=x"}) {
		t.Fatalf("the caller's environment was changed: %q", environ)
	}
	if !slices.Equal(runner.calls[0].Env, runner.calls[1].Env) {
		t.Fatalf("environments differ: %q and %q", runner.calls[0].Env, runner.calls[1].Env)
	}
}

func TestComposeOutputReturnsWhatTheCommandPrinted(t *testing.T) {
	runner := &scriptedRunner{reply: func(Command) reply {
		return reply{stdout: "out\n", stderr: "warn\n", code: 3}
	}}
	compose := Compose{Runner: runner, BackendDir: "/b"}

	stdout, stderr, code, err := compose.Output(context.Background(), map[string]string{"A": "1"}, "config", "--images", "svc")
	if err != nil || code != 3 || stdout != "out\n" || stderr != "warn\n" {
		t.Fatalf("Output = %q, %q, %d, %v", stdout, stderr, code, err)
	}
	if !slices.Equal(runner.calls[0].Args, []string{"config", "--images", "svc"}) || !slices.Contains(runner.calls[0].Env, "A=1") {
		t.Fatalf("command = %+v", runner.calls[0])
	}
}

func TestComposeOutputReportsACommandThatCouldNotStart(t *testing.T) {
	boom := errors.New("fork failed")
	runner := &scriptedRunner{reply: func(Command) reply { return reply{err: boom, code: -1} }}
	_, _, _, err := Compose{Runner: runner, BackendDir: "/b"}.Output(context.Background(), nil, "ps")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestExecRunnerRunsACommandInItsDirectoryWithItsEnvironment(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s|%s|%s' \"$PWD\" \"$MARKER\" \"$(cat)\"\nprintf 'err' >&2\nexit 7\n"
	if err := os.WriteFile(filepath.Join(dir, "probe.sh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code, err := ExecRunner{}.Run(context.Background(), Command{
		Dir:    dir,
		Name:   "./probe.sh",
		Env:    []string{"MARKER=present"},
		Stdin:  strings.NewReader("typed"),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil || code != 7 {
		t.Fatalf("Run = %d, %v", code, err)
	}
	physical, evalErr := filepath.EvalSymlinks(dir)
	if evalErr != nil {
		t.Fatal(evalErr)
	}
	if want := physical + "|present|typed"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if stderr.String() != "err" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestExecRunnerReportsACommandThatCannotStartAsAnError(t *testing.T) {
	code, err := ExecRunner{}.Run(context.Background(), Command{Dir: t.TempDir(), Name: "./does-not-exist.sh"})
	if err == nil {
		t.Fatalf("Run = %d, nil; want an error", code)
	}
}

func TestExecRunnerReportsASignalledChildAsTheShellWould(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "die.sh"), []byte("#!/bin/sh\nkill -TERM $$\nsleep 5\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, err := ExecRunner{}.Run(context.Background(), Command{Dir: dir, Name: "./die.sh"})
	if err != nil || code != 128+15 {
		t.Fatalf("Run = %d, %v; want 143, nil", code, err)
	}
}
