// Package host is the command's view of the machine it runs on: the Docker
// Compose stack, the images Compose builds, and the orchestrator's health
// answer. Everything that starts a process goes through a Runner, so the
// layers above can be tested without Docker.
package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/registry"
	"github.com/mcasillas17/TuringAgent/turing-backend/internal/servicecmd"
)

// ErrImageMissing means Docker has no image for a service: it was never built,
// or was removed.
var ErrImageMissing = errors.New("the image is not built")

// Command is one process to run. A nil Env hands the process this program's
// own environment; an empty one hands it none.
type Command struct {
	Dir, Name      string
	Args, Env      []string
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// Runner runs a Command and reports its exit code. The error is non-nil only
// when the command could not start: a command that ran and failed is a
// non-zero code.
type Runner interface {
	Run(ctx context.Context, cmd Command) (exitCode int, err error)
}

// ExecRunner runs commands with os/exec.
type ExecRunner struct{}

// Run runs cmd. A child ended by a signal reports 128 plus the signal, as a
// shell does, so Ctrl-C on `docker compose logs -f` is 130 here too.
func (ExecRunner) Run(ctx context.Context, cmd Command) (int, error) {
	process := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	process.Dir = cmd.Dir
	process.Env = cmd.Env
	process.Stdin = cmd.Stdin
	process.Stdout = cmd.Stdout
	process.Stderr = cmd.Stderr

	// Ctrl-C reaches the whole foreground group, this program included. It is
	// caught rather than ignored: a caught signal is reset to its default in
	// the child, an ignored one would be inherited, and the child must still
	// stop. Catching it here lets this program outlive the child and report
	// the code it ended with.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	err := process.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if waited, ok := exit.Sys().(syscall.WaitStatus); ok && waited.Signaled() {
			return 128 + int(waited.Signal()), nil
		}
		return exit.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

// Compose runs scripts/compose.sh, the one place that knows how to launch the
// stack safely, from the backend directory.
type Compose struct {
	Runner     Runner
	BackendDir string
	Environ    []string
}

// env is the environment for a call: Environ without any setting the backend
// reads from .env, because Compose prefers a variable the shell exports to
// .env, then the explicit extras.
func (c Compose) env(extra map[string]string) []string {
	skip := make(map[string]bool)
	for _, name := range registry.StripNames() {
		skip[name] = true
	}
	for name := range extra {
		skip[name] = true
	}
	env := make([]string, 0, len(c.Environ)+len(extra))
	for _, entry := range c.Environ {
		name, _, _ := strings.Cut(entry, "=")
		if !skip[name] {
			env = append(env, entry)
		}
	}
	names := make([]string, 0, len(extra))
	for name := range extra {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		env = append(env, name+"="+extra[name])
	}
	return env
}

// Run runs compose.sh with args, and reports its exit code.
func (c Compose) Run(ctx context.Context, extra map[string]string, stdin io.Reader, stdout, stderr io.Writer, args ...string) (int, error) {
	return c.Runner.Run(ctx, Command{
		Dir:    c.BackendDir,
		Name:   "./scripts/compose.sh",
		Args:   args,
		Env:    c.env(extra),
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})
}

// Output runs compose.sh with args and returns what it printed.
func (c Compose) Output(ctx context.Context, extra map[string]string, args ...string) (stdout, stderr string, code int, err error) {
	return c.capture(ctx, extra, "./scripts/compose.sh", args...)
}

func (c Compose) capture(ctx context.Context, extra map[string]string, name string, args ...string) (stdout, stderr string, code int, err error) {
	var out, errOut bytes.Buffer
	code, err = c.Runner.Run(ctx, Command{
		Dir:    c.BackendDir,
		Name:   name,
		Args:   args,
		Env:    c.env(extra),
		Stdout: &out,
		Stderr: &errOut,
	})
	return out.String(), errOut.String(), code, err
}

// ImageChecks returns the checks a service's image says its binary knows, from
// the servicecmd.LabelKey label, which an image built before the label existed
// does not carry: that is nil, nil. The image is the one Compose would run.
func ImageChecks(ctx context.Context, c Compose, service string) ([]string, error) {
	stdout, stderr, code, err := c.Output(ctx, nil, "config", "--images", service)
	if err != nil {
		return nil, fmt.Errorf("could not ask compose for the %s image: %w", service, err)
	}
	if code != 0 {
		return nil, fmt.Errorf("compose could not name the %s image (exit %d): %s", service, code, LastLines(stderr, 5))
	}
	image, err := serviceImage(strings.Fields(stdout), service)
	if err != nil {
		return nil, err
	}

	format := fmt.Sprintf(`{{ index .Config.Labels %q }}`, servicecmd.LabelKey)
	label, _, code, err := c.capture(ctx, nil, "docker", "image", "inspect", "--format", format, image)
	if err != nil {
		return nil, fmt.Errorf("could not ask docker about the %s image: %w", image, err)
	}
	if code != 0 {
		return nil, fmt.Errorf("%w: %s", ErrImageMissing, image)
	}
	label = strings.TrimSpace(label)
	if label == "" || label == "<no value>" {
		return nil, nil
	}
	var checks []string
	for _, name := range strings.Split(label, ",") {
		if name = strings.TrimSpace(name); name != "" {
			checks = append(checks, name)
		}
	}
	return checks, nil
}

// serviceImage picks the service's own image from what `config --images`
// names. Compose lists the images of the service's dependencies too, so with
// more than one it is the one named the way Compose names a built image:
// <project>-<service>.
func serviceImage(images []string, service string) (string, error) {
	if len(images) == 1 {
		return images[0], nil
	}
	var own []string
	for _, image := range images {
		if strings.HasSuffix(image, "-"+service) {
			own = append(own, image)
		}
	}
	if len(own) != 1 {
		return "", fmt.Errorf("compose named %d images for %s, and %d of them are named for it; want one", len(images), service, len(own))
	}
	return own[0], nil
}

// LastLines returns the last n non-blank lines of text, each without a
// trailing carriage return, joined by a newline.
func LastLines(text string, n int) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimRight(line, "\r"))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
