// Package cli is the turing command: the commands an owner runs in a terminal
// on the server, and the environment they run against. Everything outside this
// process — files, Docker, the orchestrator, the terminal — reaches a command
// through Env, so tests run the real commands against fakes.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
	"golang.org/x/term"
)

// Usage is the text `help` prints.
const Usage = `Usage: ./turing <command> [options]

Commands:
  start [--build]            start Turing and wait until it is healthy
                             (--build rebuilds the images first; run it after pulling a new version)
  stop                       stop Turing; it stays stopped after a reboot
  restart [--build] [--yes]  stop, then start; this ends a reply in progress, so it asks first
  logs [service] [-f]        show a service's recent log; -f follows it
                             (service: orchestrator, runtime, files or system; default: all)
  status                     one-screen health report; exits 1 when a check fails
  connect [--rotate-key] [--yes]
                             show how the app connects; --rotate-key makes a new app key
                             and, when you agree, applies it by restarting the orchestrator
  config                     change settings in a menu (at a terminal)
  config show                list the settings you can change; a secret shows only as set or not set
  config get KEY             print one setting; a secret is never printed
  config set KEY [VALUE] [--stdin] [--yes] [--no-apply]
                             check a new value with the services, save it in .env, then apply it
                             (a secret is never taken on the command line: it is asked for with
                             typing hidden, or read from standard input with --stdin;
                             --no-apply saves without restarting; KEY may be TURING_AGENT_API_KEYS.<name>)
  config unset KEY [--yes] [--no-apply]
                             clear a setting the same way; TURING_AGENT_API_KEYS.<name> removes one agent
  help                       show this text
  setup                      guided first-time setup (coming in a later version)
  engine                     choose and check the local model engine (coming in a later version)
`

// serviceNames pairs each service's friendly name with its Compose name, in
// the order a problem is reported.
var serviceNames = []struct{ Friendly, Compose string }{
	{"orchestrator", "turing-orchestrator"},
	{"runtime", "turing-agent-runtime-general"},
	{"files", "turing-mcp-files"},
	{"system", "turing-mcp-system"},
}

const (
	portKey      = "ORCHESTRATOR_PUBLIC_PORT"
	clientKey    = "TURING_CLIENT_API_KEY"
	defaultPort  = 3000
	logTailLines = "200"
)

// Env is everything a command touches outside its own variables.
type Env struct {
	BackendDir string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	// Environ is the process environment, as os.Environ returns it.
	Environ []string
	Getenv  func(string) string
	Geteuid func() int
	GOOS    string
	Runner  host.Runner
	// Health asks the orchestrator on 127.0.0.1:port whether it is healthy.
	Health func(ctx context.Context, port int, key string) error
	// IsTerminal is true when a person can be asked: stdin and stdout are both
	// terminals.
	IsTerminal bool
	// ReadSecret prompts and reads a line with echo off.
	ReadSecret func(prompt string) (string, error)
	// HealthTimeout and PollInterval bound waitHealthy.
	HealthTimeout, PollInterval time.Duration
	// EngineClient fetches the local engine's model list from this machine. Its
	// Timeout bounds one request, and it must not follow redirects.
	EngineClient *http.Client
	// BridgeGateway returns the address of Docker's default bridge, which is
	// where host.docker.internal leads on Linux.
	BridgeGateway func(ctx context.Context) (string, error)
}

// OSEnv is the Env for this process.
func OSEnv(backendDir string) Env {
	terminal := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	return Env{
		BackendDir: backendDir,
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Environ:    os.Environ(),
		Getenv:     os.Getenv,
		Geteuid:    os.Geteuid,
		GOOS:       runtime.GOOS,
		Runner:     host.ExecRunner{},
		Health:     host.CheckHealth,
		IsTerminal: terminal,
		ReadSecret: func(prompt string) (string, error) {
			if !terminal {
				return "", errors.New("a secret can be typed only at a terminal")
			}
			_, _ = fmt.Fprint(os.Stderr, prompt)
			secret, err := term.ReadPassword(int(os.Stdin.Fd()))
			_, _ = fmt.Fprintln(os.Stderr)
			return string(secret), err
		},
		HealthTimeout: 120 * time.Second,
		PollInterval:  2 * time.Second,
		EngineClient: &http.Client{
			Transport: directTransport(),
			Timeout:   engineTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		BridgeGateway: dockerBridgeGateway(host.ExecRunner{}, backendDir, os.Environ()),
	}
}

// directTransport never uses a proxy. The default transport honours
// HTTP_PROXY for any address but loopback, Docker's bridge gateway included,
// and the engine's request can carry its key.
func directTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return transport
}

// Main runs one command and returns the process exit code: 0 for success, 1
// for a failure or a refusal, 2 for a usage error.
func Main(args []string, env Env) int {
	// Before anything is read, written or run: init.sh and Compose take the
	// owner of every file from the invoking user, and root is not one.
	if env.Geteuid() == 0 {
		return env.fail("turing refuses to run as root, because the files it manages belong to the user who ran ./scripts/init.sh; run it again as that user")
	}
	if len(args) == 0 {
		_, _ = io.WriteString(env.Stderr, Usage)
		return 2
	}
	ctx := context.Background()
	command, rest := args[0], args[1:]
	switch command {
	case "start":
		return env.start(ctx, rest)
	case "stop":
		return env.stop(ctx, rest)
	case "restart":
		return env.restart(ctx, rest)
	case "logs":
		return env.logs(ctx, rest)
	case "status":
		return env.status(ctx, rest)
	case "connect":
		return env.connect(ctx, rest)
	case "config":
		return env.config(ctx, rest)
	case "help", "-h", "--help":
		_, _ = io.WriteString(env.Stdout, Usage)
		return 0
	case "setup", "engine":
		return env.fail("./turing %s is coming in a later version; until then, run ./scripts/init.sh to set up and ./turing config to choose an engine", command)
	default:
		// Not repeated, in case it is a secret typed in the wrong place.
		return env.usageError("unknown command; run ./turing help")
	}
}

// fail prints one failure sentence and returns exit code 1.
func (e Env) fail(format string, args ...any) int {
	_, _ = fmt.Fprintf(e.Stderr, format+"\n", args...)
	return 1
}

// usageError prints one sentence about how the command was used and returns
// exit code 2.
func (e Env) usageError(format string, args ...any) int {
	_, _ = fmt.Fprintf(e.Stderr, format+"\n", args...)
	return 2
}

func (e Env) compose() host.Compose {
	return host.Compose{Runner: e.Runner, BackendDir: e.BackendDir, Environ: e.Environ}
}

var errNoDotEnv = errors.New("there is no .env file; run ./scripts/init.sh first")

// readDotEnv reads and parses .env, which must be a regular file.
func (e Env) readDotEnv() (*envfile.File, error) {
	_, file, err := e.readDotEnvBytes()
	return file, err
}

// readDotEnvBytes reads .env, which must be a regular file, and returns its
// bytes with their parse.
func (e Env) readDotEnvBytes() ([]byte, *envfile.File, error) {
	data, err := envfile.ReadRegular(filepath.Join(e.BackendDir, ".env"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, errNoDotEnv
	}
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read .env: %w; make it a regular file (./scripts/init.sh creates one) and try again", err)
	}
	file, err := envfile.Parse(data)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot parse .env: %w; fix it and try again", err)
	}
	return data, file, nil
}

// publicPort is the port the orchestrator is published on, which is 3000 unless
// .env says otherwise.
func publicPort(file *envfile.File) (int, error) {
	value, _ := file.Get(portKey)
	if value == "" {
		return defaultPort, nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s in .env is not a port number from 1 to 65535; correct it in .env and try again", portKey)
	}
	return port, nil
}

// appEndpoint returns where the orchestrator listens on this machine and the
// key the app presents, as .env names them.
func appEndpoint(file *envfile.File) (port int, key string, err error) {
	port, err = publicPort(file)
	if err != nil {
		return 0, "", err
	}
	key, _ = file.Get(clientKey)
	if key == "" {
		return 0, "", fmt.Errorf("%s is missing from .env; run ./scripts/init.sh first", clientKey)
	}
	return port, key, nil
}

// readLine reads one line from Stdin a byte at a time, so a prompt never
// consumes input that belongs to the next one.
func (e Env) readLine() (string, error) {
	var line []byte
	one := make([]byte, 1)
	for {
		n, err := e.Stdin.Read(one)
		if n == 1 {
			if one[0] == '\n' {
				return strings.TrimRight(string(line), "\r"), nil
			}
			line = append(line, one[0])
		}
		if err != nil {
			return string(line), err
		}
	}
}

// confirm asks a yes or no question. yes answers it without asking. Off a
// terminal nobody can answer, so it declines and says how to say yes.
func (e Env) confirm(question string, defaultYes, yes bool) bool {
	if yes {
		return true
	}
	choices := "[y/N]"
	if defaultYes {
		choices = "[Y/n]"
	}
	if !e.IsTerminal {
		_, _ = fmt.Fprintf(e.Stdout, "%s %s no (not a terminal; pass --yes to confirm)\n", question, choices)
		return false
	}
	_, _ = fmt.Fprintf(e.Stdout, "%s %s ", question, choices)
	line, err := e.readLine()
	if err != nil && line == "" {
		_, _ = fmt.Fprintln(e.Stdout)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return defaultYes
	case "y", "yes":
		return true
	default:
		return false
	}
}

// dockerRunning reports whether the Docker daemon answers.
func (e Env) dockerRunning(ctx context.Context) bool {
	_, ok := e.dockerVersion(ctx)
	return ok
}

// dockerVersion asks the Docker daemon for its version, and reports whether it
// answered.
func (e Env) dockerVersion(ctx context.Context) (version string, ok bool) {
	var out strings.Builder
	code, err := e.Runner.Run(ctx, host.Command{
		Dir:    e.BackendDir,
		Name:   "docker",
		Args:   []string{"info", "--format", "{{.ServerVersion}}"},
		Env:    e.Environ,
		Stdout: &out,
		Stderr: io.Discard,
	})
	if err != nil || code != 0 {
		return "", false
	}
	return strings.TrimSpace(out.String()), true
}

// composeFailed explains a compose.sh call that did not succeed, after its own
// output has already reached the terminal, and returns exit code 1. retry is
// the command that tries again. Compose is given the terminal itself, so its
// progress display works; Docker is asked afterwards why it failed.
func (e Env) composeFailed(ctx context.Context, code int, err error, retry string) int {
	switch {
	case err != nil:
		return e.fail("could not run ./scripts/compose.sh: %v; check that Docker is installed and this checkout is intact, then run %s", err, retry)
	case !e.dockerRunning(ctx):
		return e.fail("Docker is not running; %s, then run %s", e.startDockerHint(), retry)
	default:
		return e.fail("Compose failed (exit status %d); its output is above, and ./turing logs %s shows what a service itself printed",
			code, strings.Join(friendlyNames(), "|"))
	}
}

func (e Env) startDockerHint() string {
	if e.GOOS == "darwin" {
		return "open Docker Desktop and wait for it to finish starting"
	}
	return "start it with sudo systemctl start docker"
}

func friendlyNames() []string {
	names := make([]string, len(serviceNames))
	for i, service := range serviceNames {
		names[i] = service.Friendly
	}
	return names
}
