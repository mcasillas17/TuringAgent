package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
)

var errCouldNotStart = errors.New("fork/exec ./scripts/compose.sh: permission denied")

const (
	testKey  = "tk_CANARY0123456789abcdef"
	testPort = 3456
)

// fakeReply is one answer a scripted command gives.
type fakeReply struct {
	stdout, stderr string
	code           int
	err            error
}

// fakeStep answers every command its match accepts. Replies are given in
// order and the last one repeats, so a poll can change its answer.
type fakeStep struct {
	match   func(host.Command) bool
	replies []fakeReply
	// answer, when set, decides the reply from the command and the moment it
	// runs, instead of replies.
	answer func(host.Command) fakeReply
	used   int
}

// fakeRunner records each command and answers it from a script. A command the
// script does not cover fails the test.
type fakeRunner struct {
	t     *testing.T
	steps []*fakeStep
	calls []host.Command
}

func (f *fakeRunner) on(match func(host.Command) bool, replies ...fakeReply) *fakeStep {
	step := &fakeStep{match: match, replies: replies}
	f.steps = append(f.steps, step)
	return step
}

// onFunc answers every command match accepts with whatever answer returns
// when the command runs.
func (f *fakeRunner) onFunc(match func(host.Command) bool, answer func(host.Command) fakeReply) *fakeStep {
	step := &fakeStep{match: match, answer: answer}
	f.steps = append(f.steps, step)
	return step
}

func (f *fakeRunner) Run(_ context.Context, cmd host.Command) (int, error) {
	f.t.Helper()
	f.calls = append(f.calls, cmd)
	for _, step := range f.steps {
		if !step.match(cmd) {
			continue
		}
		var reply fakeReply
		if step.answer != nil {
			reply = step.answer(cmd)
		} else {
			reply = step.replies[min(step.used, len(step.replies)-1)]
		}
		step.used++
		if cmd.Stdout != nil {
			_, _ = cmd.Stdout.Write([]byte(reply.stdout))
		}
		if cmd.Stderr != nil {
			_, _ = cmd.Stderr.Write([]byte(reply.stderr))
		}
		return reply.code, reply.err
	}
	f.t.Fatalf("unscripted command: %s %q", cmd.Name, cmd.Args)
	return 0, nil
}

// commands lists what ran, with compose.sh shortened to "compose".
func (f *fakeRunner) commands() []string {
	var out []string
	for _, cmd := range f.calls {
		name := cmd.Name
		if name == "./scripts/compose.sh" {
			name = "compose"
		}
		out = append(out, strings.Join(append([]string{name}, cmd.Args...), " "))
	}
	return out
}

func composeCall(args ...string) func(host.Command) bool {
	return func(cmd host.Command) bool {
		return cmd.Name == "./scripts/compose.sh" && slices.Equal(cmd.Args, args)
	}
}

func dockerCall(args ...string) func(host.Command) bool {
	return func(cmd host.Command) bool {
		return cmd.Name == "docker" && slices.Equal(cmd.Args, args)
	}
}

var dockerInfoArgs = []string{"info", "--format", "{{.ServerVersion}}"}

// dockerUp scripts a Docker daemon that answers; dockerDown one that does not.
func (h *harness) dockerUp() {
	h.runner.on(dockerCall(dockerInfoArgs...), fakeReply{stdout: "27.0.0\n"})
}

func (h *harness) dockerDown() {
	h.runner.on(dockerCall(dockerInfoArgs...), fakeReply{stderr: "Cannot connect to the Docker daemon\n", code: 1})
}

var psArgs = []string{"ps", "--all", "--format", "json"}

func serviceState(service, state, health string) host.ServiceState {
	return host.ServiceState{Service: service, State: state, Health: health}
}

// psOutput renders states the way `docker compose ps --format json` does.
func psOutput(states ...host.ServiceState) string {
	var out strings.Builder
	for _, s := range states {
		out.WriteString(`{"Service":"` + s.Service + `","State":"` + s.State + `","Health":"` + s.Health + `"}` + "\n")
	}
	return out.String()
}

func allRunning() []host.ServiceState {
	return []host.ServiceState{
		serviceState("turing-orchestrator", "running", "healthy"),
		serviceState("turing-agent-runtime-general", "running", ""),
		serviceState("turing-mcp-files", "running", ""),
		serviceState("turing-mcp-system", "running", ""),
	}
}

// withService replaces one service's state in a copy of states.
func withService(states []host.ServiceState, service, state, health string) []host.ServiceState {
	out := slices.Clone(states)
	for i := range out {
		if out[i].Service == service {
			out[i] = serviceState(service, state, health)
		}
	}
	return out
}

// fakeEngine is an HTTP transport for the engine probe: a request goes to the
// scripted reply for its host:port, and a host:port with none is refused, as
// when nothing listens there.
type fakeEngine struct {
	routes   map[string]fakeEngineReply
	requests []*http.Request
}

type fakeEngineReply struct {
	status int
	body   string
}

func (f *fakeEngine) serve(hostPort string, status int, body string) {
	if f.routes == nil {
		f.routes = make(map[string]fakeEngineReply)
	}
	f.routes[hostPort] = fakeEngineReply{status: status, body: body}
}

func (f *fakeEngine) RoundTrip(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req)
	reply, ok := f.routes[req.URL.Host]
	if !ok {
		return nil, errors.New("dial tcp " + req.URL.Host + ": connect: connection refused")
	}
	return &http.Response{
		StatusCode: reply.status,
		Status:     http.StatusText(reply.status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(reply.body)),
		Request:    req,
	}, nil
}

// urls lists what the probe asked, in order.
func (f *fakeEngine) urls() []string {
	var out []string
	for _, req := range f.requests {
		out = append(out, req.URL.String())
	}
	return out
}

type healthCall struct {
	port int
	key  string
}

// harness is an Env wired to fakes, with a .env in a temporary backend dir.
type harness struct {
	Env
	t           *testing.T
	runner      *fakeRunner
	out, errOut *bytes.Buffer
	healthCalls []healthCall
	healthErr   func(call int) error
	engine      *fakeEngine
	// gateway is what the Docker bridge lookup answers.
	gateway      string
	gatewayErr   error
	gatewayAsked int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, runner: &fakeRunner{t: t}, out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, engine: &fakeEngine{}}
	dir := t.TempDir()
	h.Env = Env{
		BackendDir: dir,
		Stdin:      strings.NewReader(""),
		Stdout:     h.out,
		Stderr:     h.errOut,
		Environ:    []string{"PATH=/usr/bin:/bin", "OPENAI_API_KEY=sk-exported"},
		Getenv:     func(string) string { return "" },
		Geteuid:    func() int { return 1000 },
		GOOS:       "linux",
		Runner:     h.runner,
		Health: func(_ context.Context, port int, key string) error {
			h.healthCalls = append(h.healthCalls, healthCall{port, key})
			if h.healthErr != nil {
				return h.healthErr(len(h.healthCalls))
			}
			return nil
		},
		ReadSecret:   func(string) (string, error) { return "", os.ErrInvalid },
		EngineClient: &http.Client{Transport: h.engine},
		BridgeGateway: func(context.Context) (string, error) {
			h.gatewayAsked++
			return h.gateway, h.gatewayErr
		},
		HealthTimeout: 200 * time.Millisecond,
		PollInterval:  time.Millisecond,
	}
	h.writeEnv("TURING_CLIENT_API_KEY=" + testKey + "\nORCHESTRATOR_PUBLIC_PORT=3456\nOPENAI_API_KEY=sk-in-dotenv\n")
	return h
}

func (h *harness) writeEnv(content string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.BackendDir, ".env"), []byte(content), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) removeEnv() {
	h.t.Helper()
	if err := os.Remove(filepath.Join(h.BackendDir, ".env")); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) run(args ...string) int {
	h.t.Helper()
	return Main(args, h.Env)
}

// serveHealthy scripts compose up and a ps that shows every service running.
func (h *harness) serveHealthy() {
	h.runner.on(composeCall("up", "-d"), fakeReply{})
	h.runner.on(composeCall(psArgs...), fakeReply{stdout: psOutput(allRunning()...)})
}

// noSecrets fails the test when anything printed shows a secret the harness's
// .env, environment or flags carry.
func (h *harness) noSecrets() {
	h.t.Helper()
	for _, secret := range []string{testKey, "sk-in-dotenv", "sk-exported"} {
		if strings.Contains(h.out.String(), secret) || strings.Contains(h.errOut.String(), secret) {
			h.t.Errorf("output shows a secret %q:\nstdout: %s\nstderr: %s", secret, h.out, h.errOut)
		}
	}
}
