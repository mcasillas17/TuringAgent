package cli

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/registry"
)

const (
	canaryOpenAI = "CANARY-OPENAI"
	canaryGen    = "CANARY-GEN"
)

// statusHarness is a healthy Linux server: Docker answers, every service
// runs, the app answers, and Ollama answers on the bridge gateway. A test
// changes the one thing it is about before running status.
type statusHarness struct {
	*harness
	docker    fakeReply
	ps        fakeReply
	systemctl fakeReply
}

var systemctlArgs = []string{"is-enabled", "docker"}

func systemctlCall(args ...string) func(host.Command) bool {
	return func(cmd host.Command) bool {
		return cmd.Name == "systemctl" && strings.Join(cmd.Args, " ") == strings.Join(args, " ")
	}
}

// fullEnv is a .env that holds every generated key, as init.sh leaves it, with
// the app's key and port the harness uses, then the extra lines.
func fullEnv(extra ...string) string {
	var lines []string
	for _, key := range registry.All() {
		if !key.Generated {
			continue
		}
		value := canaryGen + "-" + key.Name
		if key.Name == "TURING_CLIENT_API_KEY" {
			value = testKey
		}
		lines = append(lines, key.Name+"="+value)
	}
	lines = append(lines, "ORCHESTRATOR_PUBLIC_PORT=3456", "OPENAI_API_KEY="+canaryOpenAI)
	return strings.Join(append(lines, extra...), "\n") + "\n"
}

func newStatusHarness(t *testing.T) *statusHarness {
	t.Helper()
	h := newHarness(t)
	h.Environ = []string{"PATH=/usr/bin:/bin"}
	h.writeEnv(fullEnv())
	h.gateway = "172.17.0.1"
	h.engine.serve("172.17.0.1:11434", 200, ollamaTags)
	return &statusHarness{
		harness:   h,
		docker:    fakeReply{stdout: "27.0.0\n"},
		ps:        fakeReply{stdout: psOutput(allRunning()...)},
		systemctl: fakeReply{stdout: "enabled\n"},
	}
}

func (s *statusHarness) status(args ...string) int {
	s.t.Helper()
	s.runner.on(dockerCall(dockerInfoArgs...), s.docker)
	s.runner.on(composeCall(psArgs...), s.ps)
	s.runner.on(systemctlCall(systemctlArgs...), s.systemctl)
	return s.run(append([]string{"status"}, args...)...)
}

// line returns the status line that starts with label.
func (s *statusHarness) line(label string) string {
	s.t.Helper()
	for _, line := range strings.Split(s.out.String(), "\n") {
		if strings.HasPrefix(line, label+" ") {
			return line
		}
	}
	s.t.Fatalf("no %q line in:\n%s", label, s.out)
	return ""
}

func (s *statusHarness) wantFailed(label, text string) {
	s.t.Helper()
	line := s.line(label)
	if !strings.Contains(line, "FAILED") || !strings.Contains(line, text) {
		s.t.Errorf("%s line = %q, want FAILED naming %q", label, line, text)
	}
}

func (s *statusHarness) wantOK(label string) {
	s.t.Helper()
	line := s.line(label)
	if strings.Contains(line, "FAILED") || !strings.Contains(line, " ok") {
		s.t.Errorf("%s line = %q, want ok", label, line)
	}
}

// warnings returns the text of the warnings section, or "" when there is none.
func (s *statusHarness) warnings() string {
	_, after, found := strings.Cut(s.out.String(), "Warnings:\n")
	if !found {
		return ""
	}
	return after
}

func TestStatusHealthyServerPrintsEveryLineAndExitsZero(t *testing.T) {
	s := newStatusHarness(t)

	if code := s.status(); code != 0 {
		t.Fatalf("exit = %d, want 0; out:\n%s\nerr:\n%s", code, s.out, s.errOut)
	}

	for _, label := range []string{"Setup", "Docker", "Services", "App port", "Engine"} {
		s.wantOK(label)
	}
	if line := s.line("Docker"); !strings.Contains(line, "27.0.0") {
		t.Errorf("Docker line = %q, want the server version", line)
	}
	if line := s.line("Services"); !strings.Contains(line, "orchestrator running (healthy)") || !strings.Contains(line, "runtime running") ||
		!strings.Contains(line, "files running") || !strings.Contains(line, "system running") {
		t.Errorf("Services line = %q, want every service with its state", line)
	}
	if line := s.line("App port"); !strings.Contains(line, "127.0.0.1:3456") {
		t.Errorf("App port line = %q, want the address", line)
	}
	engine := s.line("Engine")
	for _, want := range []string{"ollama", "http://host.docker.internal:11434", "3 models", "qwen2.5:7b", "listed",
		"(turing engine check, coming in a later version, proves it from inside the runtime)"} {
		if !strings.Contains(engine, want) {
			t.Errorf("Engine line = %q, want %q", engine, want)
		}
	}
	if line := s.line("Agent team"); !strings.Contains(line, "off") {
		t.Errorf("Agent team line = %q, want off", line)
	}
	if strings.Contains(s.out.String(), "Warnings") {
		t.Errorf("a clean server prints warnings:\n%s", s.out)
	}
	if got := s.healthCalls; len(got) != 1 || got[0].port != 3456 || got[0].key != testKey {
		t.Errorf("health asked %+v, want the .env port and key", got)
	}
	if s.errOut.Len() != 0 {
		t.Errorf("stderr = %q, want none", s.errOut)
	}
}

func TestStatusOnDarwinNeedsNeitherSystemctlNorTheGateway(t *testing.T) {
	s := newStatusHarness(t)
	s.GOOS = "darwin"
	s.engine.serve("127.0.0.1:11434", 200, ollamaTags)
	s.runner.on(dockerCall(dockerInfoArgs...), s.docker)
	s.runner.on(composeCall(psArgs...), s.ps)

	if code := s.run("status"); code != 0 {
		t.Fatalf("exit = %d, want 0; out:\n%s\nerr:\n%s", code, s.out, s.errOut)
	}
	for _, command := range s.runner.commands() {
		if strings.HasPrefix(command, "systemctl") {
			t.Fatalf("ran %q on darwin", command)
		}
	}
	if s.gatewayAsked != 0 {
		t.Fatal("asked for the bridge gateway on darwin")
	}
}

func TestStatusAgentTeamIsOnWhenEnvSaysTrue(t *testing.T) {
	for value, want := range map[string]string{"true": "on (experimental)", "false": "off", "": "off"} {
		s := newStatusHarness(t)
		s.writeEnv(fullEnv("TURING_AGENT_TEAM_ENABLED=" + value))

		if code := s.status(); code != 0 {
			t.Fatalf("TURING_AGENT_TEAM_ENABLED=%q: exit = %d", value, code)
		}
		if line := s.line("Agent team"); !strings.Contains(line, want) {
			t.Errorf("TURING_AGENT_TEAM_ENABLED=%q: line = %q, want %q", value, line, want)
		}
	}
}

func TestStatusSetupFailsWithoutAUsableDotEnv(t *testing.T) {
	tests := []struct {
		name   string
		break_ func(s *statusHarness)
		want   string
	}{
		{"missing", func(s *statusHarness) { s.removeEnv() }, "run ./scripts/init.sh first"},
		{"a symlink", func(s *statusHarness) {
			s.removeEnv()
			target := filepath.Join(s.BackendDir, "elsewhere")
			if err := os.WriteFile(target, []byte(fullEnv()), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(s.BackendDir, ".env")); err != nil {
				t.Fatal(err)
			}
		}, "regular file"},
		{"a directory", func(s *statusHarness) {
			s.removeEnv()
			if err := os.Mkdir(filepath.Join(s.BackendDir, ".env"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "regular file"},
		{"a NUL byte", func(s *statusHarness) { s.writeEnv("OPENAI_API_KEY=a\x00b\n") }, "cannot parse .env"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStatusHarness(t)
			tt.break_(s)

			if code := s.status(); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			s.wantFailed("Setup", tt.want)
			if line := s.line("App port"); !strings.Contains(line, "FAILED") || !strings.Contains(line, "not checked") {
				t.Errorf("App port line = %q, want it not checked, since there is no key to ask with", line)
			}
			if len(s.healthCalls) != 0 {
				t.Errorf("asked health %+v without a key", s.healthCalls)
			}
			if line := s.line("Engine"); !strings.Contains(line, "not checked") {
				t.Errorf("Engine line = %q, want it not checked", line)
			}
		})
	}
}

func TestStatusSetupFailsWhenAGeneratedKeyIsEmptyOrMissing(t *testing.T) {
	tests := []struct {
		name, content string
		missing       []string
	}{
		{"an empty key", strings.Replace(fullEnv(), "TURING_RUNTIME_TOKEN="+canaryGen+"-TURING_RUNTIME_TOKEN", "TURING_RUNTIME_TOKEN=", 1), []string{"TURING_RUNTIME_TOKEN"}},
		{"an absent key", strings.Replace(fullEnv(), "TURING_INTEGRATION_KEY="+canaryGen+"-TURING_INTEGRATION_KEY\n", "", 1), []string{"TURING_INTEGRATION_KEY"}},
		{"several", "TURING_CLIENT_API_KEY=" + testKey + "\n", []string{"TURING_RUNTIME_TOKEN", "TURING_INTEGRATION_KEY", "MCP_SYSTEM_TOKEN_GENERAL"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStatusHarness(t)
			s.writeEnv(tt.content)

			if code := s.status(); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			line := s.line("Setup")
			if !strings.Contains(line, "FAILED") || !strings.Contains(line, "./scripts/init.sh") {
				t.Errorf("Setup line = %q, want FAILED and the next command", line)
			}
			for _, name := range tt.missing {
				if !strings.Contains(line, name) {
					t.Errorf("Setup line = %q, want it to name %s", line, name)
				}
			}
			s.noSecrets()
		})
	}
}

func TestStatusWarnsWhenDotEnvIsNotMode0600ButStillPasses(t *testing.T) {
	s := newStatusHarness(t)
	if err := os.Chmod(filepath.Join(s.BackendDir, ".env"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := s.status(); code != 0 {
		t.Fatalf("exit = %d, want 0: a mode is only a warning", code)
	}
	s.wantOK("Setup")
	warnings := s.warnings()
	if !strings.Contains(warnings, "0644") || !strings.Contains(warnings, "chmod 600 "+filepath.Join(s.BackendDir, ".env")) {
		t.Errorf("warnings = %q, want the mode and the command that fixes it", warnings)
	}
}

func TestStatusWarnsAboutTheModeEvenWhenSetupFails(t *testing.T) {
	s := newStatusHarness(t)
	s.writeEnv("TURING_CLIENT_API_KEY=" + testKey + "\n")
	if err := os.Chmod(filepath.Join(s.BackendDir, ".env"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := s.status(); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if warnings := s.warnings(); !strings.Contains(warnings, "0644") {
		t.Errorf("warnings = %q, want the mode", warnings)
	}
}

func TestStatusDockerFailsWhenItDoesNotAnswer(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			s := newStatusHarness(t)
			s.GOOS = goos
			s.docker = fakeReply{stderr: "Cannot connect to the Docker daemon\n", code: 1}
			s.engine.serve("127.0.0.1:11434", 200, ollamaTags)

			if code := s.status(); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			s.wantFailed("Docker", "Docker is not running")
			hint := "sudo systemctl start docker"
			if goos == "darwin" {
				hint = "open Docker Desktop"
			}
			if line := s.line("Docker"); !strings.Contains(line, hint) || !strings.Contains(line, "./turing start") {
				t.Errorf("Docker line = %q, want %q and the next command", line, hint)
			}
			if line := s.line("Services"); !strings.Contains(line, "FAILED") || !strings.Contains(line, "not checked") {
				t.Errorf("Services line = %q, want it not checked while Docker is down", line)
			}
			for _, command := range s.runner.commands() {
				if strings.HasPrefix(command, "compose") {
					t.Errorf("asked Compose with Docker down: %q", command)
				}
			}
		})
	}
}

func TestStatusDockerNotEnabledAtBootIsAWarningOnLinuxOnly(t *testing.T) {
	for _, word := range []string{"disabled", "masked", ""} {
		s := newStatusHarness(t)
		s.systemctl = fakeReply{stdout: word, code: 1}

		if code := s.status(); code != 0 {
			t.Fatalf("systemctl says %q: exit = %d, want 0", word, code)
		}
		warnings := s.warnings()
		if !strings.Contains(warnings, "will not come back after a reboot") || !strings.Contains(warnings, "sudo systemctl enable docker") {
			t.Errorf("systemctl says %q: warnings = %q", word, warnings)
		}
	}
}

func TestStatusIgnoresAMissingSystemctl(t *testing.T) {
	s := newStatusHarness(t)
	s.systemctl = fakeReply{err: errors.New(`exec: "systemctl": executable file not found in $PATH`)}

	if code := s.status(); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.Contains(s.out.String(), "Warnings") {
		t.Errorf("a host without systemctl got a warning:\n%s", s.out)
	}
}

func TestStatusServicesFailWhenOneIsNotRunningOrNotHealthy(t *testing.T) {
	tests := []struct {
		name   string
		states []host.ServiceState
		want   []string
	}{
		{"exited", withService(allRunning(), "turing-agent-runtime-general", "exited", ""),
			[]string{"runtime exited", "./turing logs runtime"}},
		{"unhealthy", withService(allRunning(), "turing-mcp-files", "running", "unhealthy"),
			[]string{"files running (unhealthy)", "./turing logs files"}},
		{"still starting", withService(allRunning(), "turing-orchestrator", "running", "starting"),
			[]string{"orchestrator running (starting)", "./turing logs orchestrator"}},
		{"not created", allRunning()[:3],
			[]string{"system not created", "./turing start"}},
		{"nothing", nil,
			[]string{"orchestrator not created", "./turing start"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStatusHarness(t)
			s.ps = fakeReply{stdout: psOutput(tt.states...)}

			if code := s.status(); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			s.wantFailed("Services", tt.want[0])
			if line := s.line("Services"); !strings.Contains(line, tt.want[1]) {
				t.Errorf("Services line = %q, want %q", line, tt.want[1])
			}
			s.wantOK("Docker")
		})
	}
}

func TestStatusServicesFailWhenComposeCannotListThem(t *testing.T) {
	for name, reply := range map[string]fakeReply{
		"compose fails": {stderr: "first line\nCompose launch failed: sandbox must be a real directory.\n", code: 1},
		"unreadable":    {stdout: "not json\n"},
		"cannot run":    {err: errors.New("fork/exec ./scripts/compose.sh: permission denied")},
	} {
		t.Run(name, func(t *testing.T) {
			s := newStatusHarness(t)
			s.ps = reply

			if code := s.status(); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			line := s.line("Services")
			if !strings.Contains(line, "FAILED") || !strings.Contains(line, "could not list the services") || !strings.Contains(line, "./turing start") {
				t.Errorf("Services line = %q", line)
			}
			for _, l := range strings.Split(s.out.String(), "\n") {
				if strings.HasPrefix(l, "Compose launch failed") {
					t.Errorf("a failure spilled onto a second line: %q", s.out)
				}
			}
		})
	}
}

func TestStatusAppPortFailsWhenHealthDoesNotAnswer(t *testing.T) {
	s := newStatusHarness(t)
	s.healthErr = func(int) error { return errors.New("rpc error: code = Unavailable desc = connection refused") }

	if code := s.status(); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	line := s.line("App port")
	for _, want := range []string{"FAILED", "127.0.0.1:3456", "connection refused", "./turing logs orchestrator"} {
		if !strings.Contains(line, want) {
			t.Errorf("App port line = %q, want %q", line, want)
		}
	}
}

func TestStatusAppPortSaysStartWhenTheStackIsDown(t *testing.T) {
	s := newStatusHarness(t)
	s.ps = fakeReply{stdout: psOutput(withService(allRunning(), "turing-orchestrator", "exited", "")...)}
	s.healthErr = func(int) error { return errors.New("connection refused") }

	if code := s.status(); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if line := s.line("App port"); !strings.Contains(line, "./turing start") {
		t.Errorf("App port line = %q, want ./turing start", line)
	}
}

func TestStatusAppPortExplainsAKeyTheOrchestratorRefuses(t *testing.T) {
	s := newStatusHarness(t)
	s.healthErr = func(int) error { return host.ErrUnauthenticated }

	if code := s.status(); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	want := "the key in .env is not the one the running orchestrator started with; run ./turing restart"
	if line := s.line("App port"); !strings.HasSuffix(line, "FAILED: "+want) {
		t.Errorf("App port line = %q, want it to end with the exact sentence %q", line, want)
	}
}

func TestStatusAppPortFailsOnAnUnusablePortOrKey(t *testing.T) {
	tests := []struct{ name, content, want string }{
		{"a port that is not a number", strings.Replace(fullEnv(), "ORCHESTRATOR_PUBLIC_PORT=3456", "ORCHESTRATOR_PUBLIC_PORT=abc", 1), "ORCHESTRATOR_PUBLIC_PORT"},
		{"no client key", strings.Replace(fullEnv(), "TURING_CLIENT_API_KEY="+testKey+"\n", "", 1), "TURING_CLIENT_API_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStatusHarness(t)
			s.writeEnv(tt.content)

			if code := s.status(); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			s.wantFailed("App port", tt.want)
			if len(s.healthCalls) != 0 {
				t.Errorf("asked health %+v", s.healthCalls)
			}
		})
	}
}

func TestStatusEngineFailsWhenItIsUnreachable(t *testing.T) {
	s := newStatusHarness(t)
	s.GOOS = "darwin"
	s.engine.routes = nil

	if code := s.status(); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	s.wantFailed("Engine", "did not answer")
	if line := s.line("Engine"); !strings.Contains(line, "http://127.0.0.1:11434/api/tags") {
		t.Errorf("Engine line = %q, want what was asked", line)
	}
	for _, label := range []string{"Setup", "Docker", "Services", "App port"} {
		s.wantOK(label)
	}
}

func TestStatusEngineLineReportsAModelThatIsNotListedAndWarns(t *testing.T) {
	s := newStatusHarness(t)
	s.writeEnv(fullEnv("TURING_LOCAL_MODEL=mistral"))

	if code := s.status(); code != 0 {
		t.Fatalf("exit = %d, want 0: a missing model is a warning", code)
	}
	if line := s.line("Engine"); !strings.Contains(line, "mistral is not listed") {
		t.Errorf("Engine line = %q, want the model reported as not listed", line)
	}
	if warnings := s.warnings(); !strings.Contains(warnings, "ollama pull mistral") {
		t.Errorf("warnings = %q, want how to fetch the model", warnings)
	}
}

func TestStatusEngineOnLinuxBoundToLoopbackFailsAndNamesTheHostFlag(t *testing.T) {
	s := newStatusHarness(t)
	s.engine.routes = nil
	s.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	if code := s.status(); code != 1 {
		t.Fatalf("exit = %d, want 1: the containers cannot reach an engine bound to 127.0.0.1", code)
	}
	s.wantFailed("Engine", "172.17.0.1:11434")
	warnings := s.warnings()
	if !strings.Contains(warnings, "listens only on 127.0.0.1") || !strings.Contains(warnings, "--host 172.17.0.1") {
		t.Errorf("warnings = %q, want the loopback-only explanation and --host <gateway>", warnings)
	}
}

func TestStatusEngineOnLinuxFailsWhenTheGatewayCannotBeFound(t *testing.T) {
	s := newStatusHarness(t)
	s.gatewayErr = errors.New("Cannot connect to the Docker daemon")

	if code := s.status(); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	s.wantFailed("Engine", "bridge gateway")
}

func TestStatusWarnsAboutDeprecatedNamesInDotEnv(t *testing.T) {
	s := newStatusHarness(t)
	s.writeEnv(fullEnv("OLLAMA_BASE_URL=http://host.docker.internal:11434", "OLLAMA_MODEL=qwen2.5:7b", "OLLAMA_MAX_OUTPUT_TOKENS="))

	if code := s.status(); code != 0 {
		t.Fatalf("exit = %d, want 0: a deprecated name is a warning", code)
	}
	warnings := s.warnings()
	for _, want := range []string{"OLLAMA_BASE_URL", "TURING_LOCAL_ENGINE_URL", "OLLAMA_MODEL", "TURING_LOCAL_MODEL", "deprecated"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings = %q, want %q", warnings, want)
		}
	}
	if strings.Contains(warnings, "OLLAMA_MAX_OUTPUT_TOKENS") {
		t.Errorf("warnings = %q: an empty deprecated name is not in use", warnings)
	}
}

func TestStatusWarnsAboutRegistryNamesTheShellExports(t *testing.T) {
	s := newStatusHarness(t)
	s.Environ = []string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=CANARY-EXPORTED",
		"OLLAMA_MODEL=llama3",
		"TURING_AGENT_TEAM_ENABLED=true",
		"HOME=/home/owner",
	}

	if code := s.status(); code != 0 {
		t.Fatalf("exit = %d, want 0: an export is a warning", code)
	}
	warnings := s.warnings()
	for _, name := range []string{"OPENAI_API_KEY", "OLLAMA_MODEL", "TURING_AGENT_TEAM_ENABLED"} {
		if !strings.Contains(warnings, name) {
			t.Errorf("warnings = %q, want %s named", warnings, name)
		}
	}
	for _, name := range []string{"PATH", "HOME"} {
		if strings.Contains(warnings, name) {
			t.Errorf("warnings = %q names %s, which is not a Turing setting", warnings, name)
		}
	}
	if !strings.Contains(warnings, "your shell exports it, and your own docker compose calls would prefer it to .env") {
		t.Errorf("warnings = %q, want the explanation", warnings)
	}
	if strings.Contains(s.out.String(), "CANARY-EXPORTED") {
		t.Errorf("the output shows an exported value:\n%s", s.out)
	}
}

func TestStatusNeverPrintsASecret(t *testing.T) {
	s := newStatusHarness(t)
	s.writeEnv(fullEnv("TURING_LOCAL_ENGINE=openai", "TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1",
		"TURING_LOCAL_ENGINE_API_KEY=CANARY-ENGINE", "OLLAMA_MODEL=llama3"))
	s.Environ = []string{"PATH=/usr/bin", "OPENAI_API_KEY=CANARY-EXPORTED", "TURING_CLIENT_API_KEY=CANARY-EXPORTED"}
	s.healthErr = func(int) error { return host.ErrUnauthenticated }
	s.engine.serve("172.17.0.1:8000", 200, openAIModels)
	s.ps = fakeReply{stdout: psOutput(withService(allRunning(), "turing-mcp-files", "exited", "")...)}

	if code := s.status(); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	s.noSecrets()
	for _, secret := range []string{canaryOpenAI, canaryGen, "CANARY-ENGINE", "CANARY-EXPORTED"} {
		if strings.Contains(s.out.String(), secret) || strings.Contains(s.errOut.String(), secret) {
			t.Errorf("the output shows %q:\n%s", secret, s.out)
		}
	}
}

func TestStatusExitsOneForEachFailingLineAndZeroForWarningsAlone(t *testing.T) {
	tests := []struct {
		name  string
		setup func(s *statusHarness)
		code  int
	}{
		{"nothing wrong", func(*statusHarness) {}, 0},
		{"setup", func(s *statusHarness) { s.removeEnv() }, 1},
		{"docker", func(s *statusHarness) { s.docker = fakeReply{code: 1} }, 1},
		{"services", func(s *statusHarness) { s.ps = fakeReply{stdout: psOutput()} }, 1},
		{"app port", func(s *statusHarness) { s.healthErr = func(int) error { return errors.New("no") } }, 1},
		{"engine", func(s *statusHarness) { s.engine.routes = nil }, 1},
		{"every warning at once", func(s *statusHarness) {
			s.systemctl = fakeReply{stdout: "disabled\n", code: 1}
			s.writeEnv(fullEnv("OLLAMA_MODEL=qwen2.5:7b", "TURING_LOCAL_MODEL=absent"))
			s.Environ = []string{"OPENAI_API_KEY=x"}
			if err := os.Chmod(filepath.Join(s.BackendDir, ".env"), 0o640); err != nil {
				t.Fatal(err)
			}
		}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStatusHarness(t)
			tt.setup(s)

			if code := s.status(); code != tt.code {
				t.Fatalf("exit = %d, want %d; out:\n%s", code, tt.code, s.out)
			}
		})
	}
}

func TestStatusTakesNoArguments(t *testing.T) {
	for _, args := range [][]string{{"now"}, {"--verbose"}} {
		s := newStatusHarness(t)

		if code := s.run(append([]string{"status"}, args...)...); code != 2 {
			t.Fatalf("status %q: exit = %d, want 2", args, code)
		}
		if len(s.runner.calls) != 0 {
			t.Fatalf("status %q ran %q", args, s.runner.commands())
		}
	}
}

func TestStatusAsksComposeWithoutTheShellsExports(t *testing.T) {
	s := newStatusHarness(t)
	s.Environ = []string{"PATH=/usr/bin", "OPENAI_API_KEY=CANARY-EXPORTED"}

	s.status()

	for _, call := range s.runner.calls {
		if call.Name != "./scripts/compose.sh" {
			continue
		}
		for _, entry := range call.Env {
			if strings.HasPrefix(entry, "OPENAI_API_KEY=") {
				t.Errorf("compose was given %q", entry)
			}
		}
	}
}

var statusLineShape = regexp.MustCompile(`^(Setup|Docker|Services|App port|Engine|Agent team) +\S`)

func TestStatusPrintsOneLinePerCheckThenWarnings(t *testing.T) {
	s := newStatusHarness(t)
	s.systemctl = fakeReply{stdout: "disabled\n", code: 1}
	s.Environ = []string{"OPENAI_API_KEY=x"}

	s.status()

	lines := strings.Split(strings.TrimRight(s.out.String(), "\n"), "\n")
	if len(lines) < 8 {
		t.Fatalf("output has %d lines:\n%s", len(lines), s.out)
	}
	for i, want := range []string{"Setup", "Docker", "Services", "App port", "Engine", "Agent team"} {
		if !strings.HasPrefix(lines[i], want+" ") || !statusLineShape.MatchString(lines[i]) {
			t.Errorf("line %d = %q, want the %s line", i, lines[i], want)
		}
	}
	if lines[6] != "" || lines[7] != "Warnings:" {
		t.Errorf("lines 6 and 7 = %q and %q, want a blank line and the Warnings heading", lines[6], lines[7])
	}
	for _, line := range lines[8:] {
		if !strings.HasPrefix(line, "  - ") {
			t.Errorf("warning line %q does not start with a bullet", line)
		}
	}
}
