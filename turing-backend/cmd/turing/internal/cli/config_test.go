package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/registry"
)

const (
	envExampleSource = "../../../../.env.example"
	labelFormat      = `{{ index .Config.Labels "org.turing.checks" }}`
	secretRefusal    = "secrets are never taken on the command line; use --stdin or answer the prompt"
	imagesPredate    = "the images predate check-config; run ./turing start --build first"
	savedNotApplied  = "Saved. It takes effect at the next ./turing start."
	applyQuestion    = "Apply now? Restarting stops a reply in progress. [Y/n]"
)

var checkedServices = []string{"turing-orchestrator", "turing-agent-runtime-general"}

func imageOf(service string) string { return "turing-backend-" + service }

func imagesCall(service string) func(host.Command) bool {
	return composeCall("config", "--images", service)
}

func inspectCall(service string) func(host.Command) bool {
	return dockerCall("image", "inspect", "--format", labelFormat, imageOf(service))
}

func checkConfigCall(service string) func(host.Command) bool {
	return composeCall("run", "--rm", "--no-deps", service, "check-config")
}

func checkEngineCall() func(host.Command) bool {
	return composeCall("run", "--rm", "--no-deps", "turing-agent-runtime-general", "check-engine")
}

// engineUsable is what the runtime's check-engine prints when every step passes.
const engineUsable = "1/4 reachable: ok\n2/4 model present: ok\n3/4 streaming: ok\n4/4 tool calling: ok\nthe engine is usable for Turing\n"

// imageLabels are the checks each image's label lists, as PR 3 builds them.
var imageLabels = map[string]string{
	"turing-orchestrator":          "config",
	"turing-agent-runtime-general": "config,engine",
}

// configHarness is an installed server where every step of a save succeeds:
// Docker answers, both images know check-config, both checks pass, the
// stack comes up healthy, and the engine check passes. A test scripts the
// step it is about before running config, and its own step wins.
type configHarness struct {
	*harness
}

func newConfigHarness(t *testing.T) *configHarness {
	t.Helper()
	h := newHarness(t)
	h.Environ = []string{"PATH=/usr/bin:/bin"}
	h.writeEnv(fullEnv(
		"TURING_LOCAL_ENGINE=ollama",
		"TURING_LOCAL_MODEL=qwen2.5:7b",
		"OPENAI_MODEL=gpt-4o-mini",
		"TURING_QUEUE_MAX_WAIT_MS=43200000",
	))
	example, err := os.ReadFile(envExampleSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.BackendDir, ".env.example"), example, 0o644); err != nil {
		t.Fatal(err)
	}
	return &configHarness{harness: h}
}

func (c *configHarness) config(args ...string) int {
	c.t.Helper()
	c.scriptSave()
	return c.run(append([]string{"config"}, args...)...)
}

// scriptSave scripts every step of a save, and the engine check after it, to
// succeed.
func (c *configHarness) scriptSave() {
	c.dockerUp()
	for _, service := range checkedServices {
		c.runner.on(imagesCall(service), fakeReply{stdout: imageOf(service) + "\n"})
		c.runner.on(inspectCall(service), fakeReply{stdout: imageLabels[service] + "\n"})
		c.runner.on(checkConfigCall(service), fakeReply{stdout: "configuration is valid\n"})
	}
	c.serveHealthy()
	c.runner.on(checkEngineCall(), fakeReply{stdout: engineUsable})
}

func (h *harness) envBytes() []byte {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.BackendDir, ".env"))
	if err != nil {
		h.t.Fatal(err)
	}
	return data
}

func (h *harness) envGet(name string) (string, bool) {
	h.t.Helper()
	return h.readEnvFile().Get(name)
}

// checkCalls returns the check-config runs, in order.
func (h *harness) checkCalls() []host.Command {
	var calls []host.Command
	for _, call := range h.runner.calls {
		if call.Name == "./scripts/compose.sh" && slices.Contains(call.Args, "check-config") {
			calls = append(calls, call)
		}
	}
	return calls
}

// ranCommand reports whether a command that starts with prefix ran.
func (h *harness) ranCommand(prefix string) bool {
	return slices.ContainsFunc(h.runner.commands(), func(command string) bool { return strings.HasPrefix(command, prefix) })
}

func (h *harness) countCommand(command string) int {
	count := 0
	for _, ran := range h.runner.commands() {
		if ran == command {
			count++
		}
	}
	return count
}

// unchanged fails the test when .env is not byte for byte what it was.
func (h *harness) unchanged(before []byte) {
	h.t.Helper()
	if after := h.envBytes(); string(after) != string(before) {
		h.t.Fatalf(".env changed:\nbefore: %q\nafter:  %q", before, after)
	}
}

func (h *harness) noOutputContains(secrets ...string) {
	h.t.Helper()
	for _, secret := range secrets {
		if strings.Contains(h.out.String()+h.errOut.String(), secret) {
			h.t.Errorf("output shows %q:\nstdout: %s\nstderr: %s", secret, h.out, h.errOut)
		}
	}
}

func lockDotEnv(h *harness) (func(), error) {
	return envfile.Lock(filepath.Join(h.BackendDir, ".env.lock"))
}

// listenOnLoopback holds a port on 127.0.0.1 for the rest of the test.
func listenOnLoopback(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return itoa(listener.Addr().(*net.TCPAddr).Port)
}

// freeLoopbackPort returns a port nothing listens on, as far as anyone can
// tell without holding it.
func freeLoopbackPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return itoa(port)
}

func itoa(n int) string { return strconv.Itoa(n) }

// --- show ---

func TestConfigShowListsEachSectionWithItsEditableSettings(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.config("show"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}

	out := h.out.String()
	last := -1
	for _, section := range registry.Sections {
		at := strings.Index(out, section+"\n")
		if at < 0 || at < last {
			t.Fatalf("section %q missing or out of order:\n%s", section, out)
		}
		last = at
	}
	for _, key := range registry.Editable() {
		if !strings.Contains(out, "\n  "+key.Name+" = ") {
			t.Errorf("show does not list %s:\n%s", key.Name, out)
		}
	}
	for _, key := range registry.All() {
		if !key.Editable && strings.Contains(out, key.Name+" = ") {
			t.Errorf("show lists %s, which config does not change", key.Name)
		}
	}
	for _, want := range []string{"  ORCHESTRATOR_PUBLIC_PORT = 3456\n", "  TURING_LOCAL_MODEL = qwen2.5:7b\n", "  OPENAI_MODEL = gpt-4o-mini\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("show does not contain %q:\n%s", want, out)
		}
	}
	// Each key sits under its own section.
	localAt, modelAt, remoteAt := strings.Index(out, "Local engine\n"), strings.Index(out, "TURING_LOCAL_MODEL = "), strings.Index(out, "Remote model\n")
	if localAt >= modelAt || modelAt >= remoteAt {
		t.Fatalf("TURING_LOCAL_MODEL is not under Local engine:\n%s", out)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("show ran %q", h.runner.commands())
	}
}

func TestConfigShowMasksSecretsAndListsAgentNamesOnly(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv(
		"TURING_LOCAL_ENGINE_API_KEY=CANARY-ENGINE",
		`TURING_AGENT_API_KEYS={"openai":"CANARY-AGENT-OPENAI","claude":"CANARY-AGENT-CLAUDE"}`,
	))

	if code := h.config("show"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}

	out := h.out.String()
	for _, want := range []string{
		"  OPENAI_API_KEY = set\n",
		"  TURING_LOCAL_ENGINE_API_KEY = set\n",
		"  TURING_AGENT_API_KEYS = claude, openai\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show does not contain %q:\n%s", want, out)
		}
	}
	h.noOutputContains("CANARY", testKey)
}

func TestConfigShowSaysNotSetForAnEmptySecretAndAnEmptyMap(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(strings.Replace(fullEnv("TURING_AGENT_API_KEYS="), "OPENAI_API_KEY="+canaryOpenAI, "OPENAI_API_KEY=", 1))

	if code := h.config("show"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	for _, want := range []string{"  OPENAI_API_KEY = not set\n", "  TURING_LOCAL_ENGINE_API_KEY = not set\n", "  TURING_AGENT_API_KEYS = not set\n"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("show does not contain %q:\n%s", want, h.out)
		}
	}
}

func TestConfigShowNeverPrintsAnAgentMapItCannotRead(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv(`TURING_AGENT_API_KEYS=CANARY-NOT-JSON`))

	if code := h.config("show"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if !strings.Contains(h.out.String(), "  TURING_AGENT_API_KEYS = set, but not readable as names and keys\n") {
		t.Fatalf("stdout = %s", h.out)
	}
	h.noOutputContains("CANARY")
}

func TestConfigShowGivesTheShippedValueOfAnEmptySetting(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv("TURING_LOCAL_MODEL=", "TURING_LOCAL_ENGINE_LABEL="))

	if code := h.config("show"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	out := h.out.String()
	for _, want := range []string{
		// Empty in .env.
		"  TURING_LOCAL_MODEL = (empty; shipped value: qwen2.5:7b)\n",
		// Absent from .env.
		"  TURING_JOB_TIMEOUT_MS = (empty; shipped value: 300000)\n",
		// .env.example ships it empty too.
		"  TURING_LOCAL_ENGINE_LABEL = (empty)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show does not contain %q:\n%s", want, out)
		}
	}
}

func TestConfigShowNeedsADotEnvAndTakesNoArguments(t *testing.T) {
	h := newConfigHarness(t)
	h.removeEnv()
	if code := h.config("show"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(h.errOut.String(), "./scripts/init.sh") {
		t.Fatalf("stderr = %q", h.errOut)
	}

	h = newConfigHarness(t)
	if code := h.config("show", "extra"); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

// --- get ---

func TestConfigGetPrintsAnEditableSetting(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.config("get", "TURING_LOCAL_MODEL"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if h.out.String() != "qwen2.5:7b\n" {
		t.Fatalf("stdout = %q", h.out)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("get ran %q", h.runner.commands())
	}
}

func TestConfigGetRefusesSecretsAndWhatConfigDoesNotOwn(t *testing.T) {
	tests := []struct {
		key  string
		code int
		want string
	}{
		{"OPENAI_API_KEY", 1, "OPENAI_API_KEY is a secret; ./turing config show says whether it is set"},
		{"TURING_LOCAL_ENGINE_API_KEY", 1, "TURING_LOCAL_ENGINE_API_KEY is a secret; ./turing config show says whether it is set"},
		{"TURING_AGENT_API_KEYS", 1, "TURING_AGENT_API_KEYS is a secret; ./turing config show says whether it is set"},
		{"TURING_AGENT_API_KEYS.claude", 1, "TURING_AGENT_API_KEYS.claude is a secret; ./turing config show says whether it is set"},
		{"TURING_CLIENT_API_KEY", 1, "./turing connect --rotate-key"},
		{"TURING_RUNTIME_TOKEN", 1, "generated by ./scripts/init.sh"},
		{"TURING_APPROVAL_JWT_SECRET", 1, "generated by ./scripts/init.sh"},
		{"ORCHESTRATOR_INTERNAL_PORT", 1, "internal wiring that ./scripts/init.sh writes"},
		{"DATABASE_PATH", 1, "internal wiring that ./scripts/init.sh writes"},
		{"TURING_MAX_CONCURRENT_RUNS_GENERAL", 1, "AGT-000"},
		{"NOT_A_SETTING", 2, "no setting has that name; ./turing config show lists the settings"},
		{"OLLAMA_MODEL", 2, "OLLAMA_MODEL is the old name of TURING_LOCAL_MODEL"},
		{"OLLAMA_BASE_URL", 2, "OLLAMA_BASE_URL is the old name of TURING_LOCAL_ENGINE_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			h := newConfigHarness(t)
			h.writeEnv(fullEnv(`TURING_AGENT_API_KEYS={"claude":"CANARY-AGENT"}`, "TURING_LOCAL_ENGINE_API_KEY=CANARY-ENGINE", "OLLAMA_MODEL=CANARY-OLD"))

			if code := h.config("get", tt.key); code != tt.code {
				t.Fatalf("exit = %d, want %d; stderr: %s", code, tt.code, h.errOut)
			}
			if !strings.Contains(h.errOut.String(), tt.want) {
				t.Fatalf("stderr = %q, want %q", h.errOut, tt.want)
			}
			if h.out.Len() != 0 {
				t.Fatalf("stdout = %q", h.out)
			}
			h.noOutputContains("CANARY", testKey)
		})
	}
}

func TestConfigGetTakesExactlyOneSetting(t *testing.T) {
	for _, args := range [][]string{{"get"}, {"get", "TURING_LOCAL_MODEL", "OPENAI_MODEL"}, {"get", "--yes", "TURING_LOCAL_MODEL"}} {
		h := newConfigHarness(t)
		if code := h.config(args...); code != 2 {
			t.Fatalf("%q: exit = %d, want 2", args, code)
		}
	}
}

// --- set and unset: what they accept ---

func TestConfigSetRefusesASecretOnTheCommandLine(t *testing.T) {
	for _, key := range []string{"OPENAI_API_KEY", "TURING_LOCAL_ENGINE_API_KEY", "TURING_AGENT_API_KEYS", "TURING_AGENT_API_KEYS.claude", "TURING_AGENT_API_KEYS.bad name"} {
		t.Run(key, func(t *testing.T) {
			h := newConfigHarness(t)
			before := h.envBytes()

			if code := h.config("set", key, "CANARY-ON-THE-COMMAND-LINE", "--yes"); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			if !strings.Contains(h.errOut.String(), secretRefusal) {
				t.Fatalf("stderr = %q, want %q", h.errOut, secretRefusal)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
			h.unchanged(before)
			h.noOutputContains("CANARY")
		})
	}
}

func TestConfigSetReadsASecretFromStdin(t *testing.T) {
	h := newConfigHarness(t)
	h.Stdin = strings.NewReader("sk-CANARY-NEW\n")

	if code := h.config("set", "OPENAI_API_KEY", "--stdin", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if got, _ := h.envGet("OPENAI_API_KEY"); got != "sk-CANARY-NEW" {
		t.Fatalf("OPENAI_API_KEY = %q, want the line without its newline", got)
	}
	for _, call := range h.checkCalls() {
		if !slices.Contains(call.Env, "OPENAI_API_KEY=sk-CANARY-NEW") {
			t.Fatalf("check-config env = %q, want the new key", call.Env)
		}
	}
	h.noOutputContains("CANARY")
}

func TestConfigSetAtATerminalAsksForTheValue(t *testing.T) {
	t.Run("a secret, with echo off", func(t *testing.T) {
		h := newConfigHarness(t)
		h.IsTerminal = true
		var prompts []string
		h.ReadSecret = func(prompt string) (string, error) {
			prompts = append(prompts, prompt)
			return "sk-CANARY-TYPED", nil
		}

		if code := h.config("set", "OPENAI_API_KEY", "--yes"); code != 0 {
			t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
		}
		if len(prompts) != 1 || !strings.Contains(prompts[0], "OPENAI_API_KEY") {
			t.Fatalf("ReadSecret prompts = %q", prompts)
		}
		if got, _ := h.envGet("OPENAI_API_KEY"); got != "sk-CANARY-TYPED" {
			t.Fatalf("OPENAI_API_KEY = %q", got)
		}
		h.noOutputContains("CANARY")
	})
	t.Run("anything else, as a line", func(t *testing.T) {
		h := newConfigHarness(t)
		h.IsTerminal = true
		h.ReadSecret = func(string) (string, error) {
			t.Fatal("a setting that is not a secret was read with echo off")
			return "", nil
		}
		h.Stdin = strings.NewReader("llama3.2\n")

		if code := h.config("set", "TURING_LOCAL_MODEL", "--yes"); code != 0 {
			t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
		}
		if !strings.Contains(h.out.String(), "TURING_LOCAL_MODEL") {
			t.Fatalf("stdout = %q, want a prompt naming the setting", h.out)
		}
		if got, _ := h.envGet("TURING_LOCAL_MODEL"); got != "llama3.2" {
			t.Fatalf("TURING_LOCAL_MODEL = %q", got)
		}
	})
}

func TestConfigSetWithoutAValueOffATerminalIsAUsageError(t *testing.T) {
	for _, key := range []string{"TURING_LOCAL_MODEL", "OPENAI_API_KEY", "TURING_AGENT_API_KEYS.claude"} {
		t.Run(key, func(t *testing.T) {
			h := newConfigHarness(t)
			h.ReadSecret = func(string) (string, error) {
				t.Fatal("asked for a secret off a terminal")
				return "", nil
			}
			before := h.envBytes()

			if code := h.config("set", key, "--yes"); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			if !strings.Contains(h.errOut.String(), "--stdin") {
				t.Fatalf("stderr = %q, want it to name --stdin", h.errOut)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
			h.unchanged(before)
		})
	}
}

func TestConfigSetWithStdinThatHoldsNothingIsAUsageError(t *testing.T) {
	h := newConfigHarness(t)
	before := h.envBytes()

	if code := h.config("set", "OPENAI_API_KEY", "--stdin", "--yes"); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
	h.unchanged(before)
}

func TestConfigSetNeverClearsASettingFromAnEmptyAnswer(t *testing.T) {
	const unsetHint = "./turing config unset "
	tests := []struct {
		name   string
		key    string
		args   []string
		answer string // what the person types, or the standard input of --stdin
	}{
		{"Enter at the hidden prompt", "OPENAI_API_KEY", nil, ""},
		{"a newline on --stdin", "OPENAI_API_KEY", []string{"--stdin"}, "\n"},
		{"a CRLF on --stdin", "OPENAI_API_KEY", []string{"--stdin"}, "\r\n"},
		{"a newline on --stdin for a plain setting", "TURING_LOCAL_MODEL", []string{"--stdin"}, "\n"},
		{"Enter at the plain prompt", "TURING_LOCAL_MODEL", nil, "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.IsTerminal = tt.args == nil
			h.ReadSecret = func(string) (string, error) { return "", nil }
			h.Stdin = strings.NewReader(tt.answer)
			before := h.envBytes()

			if code := h.config(append([]string{"set", tt.key, "--yes"}, tt.args...)...); code != 2 {
				t.Fatalf("exit = %d, want 2; stderr: %s", code, h.errOut)
			}
			if want := unsetHint + tt.key; !strings.Contains(h.errOut.String(), want) {
				t.Fatalf("stderr = %q, want %q", h.errOut, want)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
			h.unchanged(before)
		})
	}
}

func TestConfigSetTakesAnEmptyValueGivenOnTheCommandLine(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.config("set", "TURING_LOCAL_MODEL", "", "--no-apply"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if value, present := h.envGet("TURING_LOCAL_MODEL"); value != "" || !present {
		t.Fatalf("TURING_LOCAL_MODEL = %q (present %v), want an empty line", value, present)
	}
}

func TestConfigUnknownOptionNeverEchoesTheArgument(t *testing.T) {
	for _, args := range [][]string{
		{"set", "OPENAI_API_KEY", "--sk-CANARY"},
		{"set", "OPENAI_API_KEY", "-sk-CANARY"},
		{"set", "--sk-CANARY", "OPENAI_API_KEY"},
		{"set", "OPENAI_API_KEY", "--stdin=sk-CANARY"},
		{"unset", "OPENAI_API_KEY", "--sk-CANARY"},
		{"get", "OPENAI_API_KEY", "--sk-CANARY"},
		{"show", "--sk-CANARY"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newConfigHarness(t)
			before := h.envBytes()

			if code := h.config(args...); code != 2 {
				t.Fatalf("exit = %d, want 2; stderr: %s", code, h.errOut)
			}
			if !strings.Contains(h.errOut.String(), "./turing help") {
				t.Fatalf("stderr = %q, want the usage pointer", h.errOut)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
			h.unchanged(before)
			h.noOutputContains("CANARY")
		})
	}
}

// A secret typed where a name goes, as NAME=VALUE, as an agent name, or as a
// stray argument, must not be printed back: no name or argument a config
// command does not know is ever repeated.
func TestConfigNeverRepeatsANameOrArgumentItDoesNotKnow(t *testing.T) {
	tooLong := "TURING_AGENT_API_KEYS.sk-ant-api03-CANARY" + strings.Repeat("x", 64)
	tests := []struct {
		args  []string
		stdin string
		code  int
		want  string
	}{
		{[]string{"set", "OPENAI_API_KEY=sk-CANARY"}, "", 2, "two arguments"},
		{[]string{"set", "OPENAI_API_KEY=sk-CANARY", "--stdin"}, "k\n", 2, "two arguments"},
		{[]string{"set", "TURING_AGENT_API_KEYS=sk-CANARY"}, "", 2, "two arguments"},
		{[]string{"get", "OPENAI_API_KEY=sk-CANARY"}, "", 2, "two arguments"},
		{[]string{"unset", "OPENAI_API_KEY=sk-CANARY"}, "", 2, "two arguments"},
		{[]string{"set", "TURING_AGENT_API_KEYS.a=sk-ant-CANARY"}, "", 1, "--stdin"},
		{[]string{"set", "TURING_AGENT_API_KEYS.a=sk-ant-CANARY", "--stdin"}, "k\n", 1, "--stdin"},
		{[]string{"get", "TURING_AGENT_API_KEYS.a=sk-ant-CANARY"}, "", 1, "--stdin"},
		{[]string{"unset", "TURING_AGENT_API_KEYS.a=sk-ant-CANARY"}, "", 1, "--stdin"},
		{[]string{"set", "sk-CANARY"}, "", 2, "./turing config show"},
		{[]string{"get", "sk-CANARY"}, "", 2, "./turing config show"},
		{[]string{"set", tooLong, "--stdin"}, "k\n", 1, "not a valid agent name"},
		{[]string{"get", tooLong}, "", 1, "not a valid agent name"},
		{[]string{"unset", "TURING_AGENT_API_KEYS.CANARY"}, "", 1, "./turing config show"},
		{[]string{"show", "sk-CANARY"}, "", 2, "./turing help"},
		{[]string{"sk-CANARY"}, "", 2, "./turing help"},
		{[]string{"OPENAI_API_KEY=sk-CANARY"}, "", 2, "./turing help"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			h := newConfigHarness(t)
			h.writeEnv(fullEnv(`TURING_AGENT_API_KEYS={"claude":"sk-ant-kept"}`))
			h.Stdin = strings.NewReader(tt.stdin)
			before := h.envBytes()

			if code := h.config(tt.args...); code != tt.code {
				t.Fatalf("exit = %d, want %d; stderr: %s", code, tt.code, h.errOut)
			}
			if !strings.Contains(h.errOut.String(), tt.want) {
				t.Fatalf("stderr = %q, want %q", h.errOut, tt.want)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
			h.unchanged(before)
			h.noOutputContains("CANARY")
		})
	}
}

func TestConfigSetAndUnsetRefuseWhatTheyCannotChange(t *testing.T) {
	tests := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"set"}, 2, "./turing help"},
		{[]string{"set", "TURING_LOCAL_MODEL", "a", "b"}, 2, "./turing help"},
		{[]string{"set", "TURING_LOCAL_MODEL", "a", "--nope"}, 2, "unknown option"},
		{[]string{"set", "TURING_LOCAL_MODEL", "a", "--stdin"}, 2, "not both"},
		{[]string{"set", "NOT_A_SETTING", "a"}, 2, "no setting has that name; ./turing config show lists the settings"},
		{[]string{"set", "OLLAMA_MODEL", "a"}, 2, "OLLAMA_MODEL is the old name of TURING_LOCAL_MODEL"},
		{[]string{"set", "TURING_AGENT_API_KEYS"}, 2, "./turing config set TURING_AGENT_API_KEYS.<name>"},
		{[]string{"set", "TURING_CLIENT_API_KEY", "a"}, 1, "./turing connect --rotate-key"},
		{[]string{"set", "ORCHESTRATOR_INTERNAL_PORT", "4000"}, 1, "internal wiring"},
		{[]string{"set", "TURING_MAX_CONCURRENT_RUNS_GENERAL", "2"}, 1, "AGT-000"},
		{[]string{"set", "TURING_INTEGRATION_KEY", "a"}, 1, "generated by ./scripts/init.sh"},
		{[]string{"unset"}, 2, "./turing help"},
		{[]string{"unset", "TURING_LOCAL_MODEL", "x"}, 2, "./turing help"},
		{[]string{"unset", "TURING_LOCAL_MODEL", "--stdin"}, 2, "unknown option"},
		{[]string{"unset", "NOT_A_SETTING"}, 2, "no setting has that name; ./turing config show lists the settings"},
		{[]string{"unset", "TURING_RUNTIME_TOKEN"}, 1, "generated by ./scripts/init.sh"},
		{[]string{"bogus"}, 2, "unknown config command; run ./turing help"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			h := newConfigHarness(t)
			before := h.envBytes()

			if code := h.config(tt.args...); code != tt.code {
				t.Fatalf("exit = %d, want %d; stderr: %s", code, tt.code, h.errOut)
			}
			if !strings.Contains(h.errOut.String(), tt.want) {
				t.Fatalf("stderr = %q, want %q", h.errOut, tt.want)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
			h.unchanged(before)
		})
	}
}

func TestConfigSetTakesAValueThatLooksLikeANegativeNumberOrFollowsDoubleDash(t *testing.T) {
	for _, args := range [][]string{
		{"set", "OLLAMA_KEEP_ALIVE", "-1", "--no-apply"},
		{"set", "--no-apply", "OLLAMA_KEEP_ALIVE", "--", "-1"},
	} {
		h := newConfigHarness(t)
		if code := h.config(args...); code != 0 {
			t.Fatalf("%q: exit = %d; stderr: %s", args, code, h.errOut)
		}
		if got, _ := h.envGet("OLLAMA_KEEP_ALIVE"); got != "-1" {
			t.Fatalf("%q: OLLAMA_KEEP_ALIVE = %q, want -1", args, got)
		}
	}
}

func TestConfigUnsetEmptiesTheSetting(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.config("unset", "OPENAI_API_KEY", "--no-apply"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	value, present := h.envGet("OPENAI_API_KEY")
	if value != "" || !present {
		t.Fatalf("OPENAI_API_KEY = %q (present %v), want an empty line", value, present)
	}
	if calls := h.checkCalls(); len(calls) != 2 || !slices.Contains(calls[0].Env, "OPENAI_API_KEY=") {
		t.Fatalf("check-config calls = %v", calls)
	}
}

// --- the agent key map ---

func agentKeysIn(t *testing.T, h *harness) map[string]string {
	t.Helper()
	raw, _ := h.envGet("TURING_AGENT_API_KEYS")
	if raw == "" {
		return map[string]string{}
	}
	keys := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		t.Fatalf("TURING_AGENT_API_KEYS is not valid JSON: %v", err)
	}
	return keys
}

func TestConfigSetAgentKeyAddsTheNameAndKeepsTheOthers(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv(`TURING_AGENT_API_KEYS={"openai":"sk-CANARY-OPENAI","zeta":"z-CANARY"}`))
	h.Stdin = strings.NewReader("sk-ant-CANARY-'quoted'\n")

	if code := h.config("set", "TURING_AGENT_API_KEYS.claude", "--stdin", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}

	want := map[string]string{"claude": "sk-ant-CANARY-'quoted'", "openai": "sk-CANARY-OPENAI", "zeta": "z-CANARY"}
	got := agentKeysIn(t, h.harness)
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for name, key := range want {
		if got[name] != key {
			t.Fatalf("keys = %v, want %v", got, want)
		}
	}
	raw, _ := h.envGet("TURING_AGENT_API_KEYS")
	if raw != `{"claude":"sk-ant-CANARY-'quoted'","openai":"sk-CANARY-OPENAI","zeta":"z-CANARY"}` {
		t.Fatalf("raw = %s, want json.Marshal's sorted object", raw)
	}
	for _, call := range h.checkCalls() {
		if !slices.Contains(call.Env, "TURING_AGENT_API_KEYS="+raw) {
			t.Fatalf("check-config env = %q", call.Env)
		}
	}
	h.noOutputContains("CANARY")
}

func TestConfigSetAgentKeyStartsAnEmptyMap(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv("TURING_AGENT_API_KEYS="))
	h.Stdin = strings.NewReader("sk-CANARY\n")

	if code := h.config("set", "TURING_AGENT_API_KEYS.claude.v2", "--stdin", "--no-apply"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if raw, _ := h.envGet("TURING_AGENT_API_KEYS"); raw != `{"claude.v2":"sk-CANARY"}` {
		t.Fatalf("raw = %s", raw)
	}
}

func TestConfigUnsetAgentKeyRemovesOnlyThatName(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv(`TURING_AGENT_API_KEYS={"claude":"sk-CANARY-C","openai":"sk-CANARY-O"}`))

	if code := h.config("unset", "TURING_AGENT_API_KEYS.claude", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if raw, _ := h.envGet("TURING_AGENT_API_KEYS"); raw != `{"openai":"sk-CANARY-O"}` {
		t.Fatalf("raw = %s", raw)
	}
	h.noOutputContains("CANARY")

	// Removing the last name leaves the setting empty, as it ships.
	h = newConfigHarness(t)
	h.writeEnv(fullEnv(`TURING_AGENT_API_KEYS={"claude":"sk-CANARY-C"}`))
	if code := h.config("unset", "TURING_AGENT_API_KEYS.claude", "--no-apply"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if raw, present := h.envGet("TURING_AGENT_API_KEYS"); raw != "" || !present {
		t.Fatalf("raw = %q, present %v", raw, present)
	}
}

func TestConfigAgentKeyRefusesWhatTheServicesWouldRefuse(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		dot   string
		want  string
	}{
		{"a name with a space", []string{"set", "TURING_AGENT_API_KEYS.bad name", "--stdin"}, "k\n", "", "not a valid agent name"},
		{"a name with a slash", []string{"set", "TURING_AGENT_API_KEYS.a/b", "--stdin"}, "k\n", "", "not a valid agent name"},
		{"no name", []string{"set", "TURING_AGENT_API_KEYS.", "--stdin"}, "k\n", "", "not a valid agent name"},
		{"a name of 65 characters", []string{"set", "TURING_AGENT_API_KEYS." + strings.Repeat("a", 65), "--stdin"}, "k\n", "", "not a valid agent name"},
		{"an invalid name to remove", []string{"unset", "TURING_AGENT_API_KEYS.bad name"}, "", "", "not a valid agent name"},
		{"an empty key", []string{"set", "TURING_AGENT_API_KEYS.claude", "--stdin"}, "\n", "", "./turing config unset TURING_AGENT_API_KEYS.claude"},
		{"a name that is not there", []string{"unset", "TURING_AGENT_API_KEYS.nobody"}, "", `{"claude":"sk-CANARY"}`, "has no agent by that name"},
		{"a map that is not JSON", []string{"set", "TURING_AGENT_API_KEYS.claude", "--stdin"}, "k\n", "CANARY-NOT-JSON", "./turing config unset TURING_AGENT_API_KEYS"},
		{"a map of something else", []string{"set", "TURING_AGENT_API_KEYS.claude", "--stdin"}, "k\n", `{"claude":["CANARY"]}`, "not a JSON object of names and keys"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.writeEnv(fullEnv("TURING_AGENT_API_KEYS=" + tt.dot))
			h.Stdin = strings.NewReader(tt.stdin)
			before := h.envBytes()

			if code := h.config(append(tt.args, "--yes")...); code != 1 {
				t.Fatalf("exit = %d, want 1; stderr: %s", code, h.errOut)
			}
			if !strings.Contains(h.errOut.String(), tt.want) {
				t.Fatalf("stderr = %q, want %q", h.errOut, tt.want)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
			h.unchanged(before)
			h.noOutputContains("CANARY")
		})
	}
}

func TestConfigAgentKeyAcceptsANameOfSixtyFourRunes(t *testing.T) {
	h := newConfigHarness(t)
	name := strings.Repeat("a", 64)
	h.Stdin = strings.NewReader("k\n")

	if code := h.config("set", "TURING_AGENT_API_KEYS."+name, "--stdin", "--no-apply"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if got := agentKeysIn(t, h.harness); got[name] != "k" {
		t.Fatalf("keys = %v", got)
	}
}

// --- deprecated names ---

func TestSaveMigratesDeprecatedNamesTheWayTheServicesReadThem(t *testing.T) {
	tests := []struct {
		name    string
		env     []string
		set     []string
		want    map[string]string // the new names' values afterwards
		changed []string          // the check-config extras, besides the setting itself
	}{
		{
			name:    "OLLAMA_MODEL moves to an empty TURING_LOCAL_MODEL",
			env:     []string{"TURING_LOCAL_MODEL=", "OLLAMA_MODEL=llama3"},
			set:     []string{"OPENAI_MODEL", "gpt-x"},
			want:    map[string]string{"TURING_LOCAL_MODEL": "llama3"},
			changed: []string{"OLLAMA_MODEL=", "TURING_LOCAL_MODEL=llama3"},
		},
		{
			name:    "OLLAMA_MODEL moves to an absent TURING_LOCAL_MODEL",
			env:     []string{"OLLAMA_MODEL=llama3"},
			set:     []string{"OPENAI_MODEL", "gpt-x"},
			want:    map[string]string{"TURING_LOCAL_MODEL": "llama3"},
			changed: []string{"OLLAMA_MODEL=", "TURING_LOCAL_MODEL=llama3"},
		},
		{
			name:    "OLLAMA_MODEL is dropped when TURING_LOCAL_MODEL is set",
			env:     []string{"TURING_LOCAL_MODEL=qwen2.5:7b", "OLLAMA_MODEL=llama3"},
			set:     []string{"OPENAI_MODEL", "gpt-x"},
			want:    map[string]string{"TURING_LOCAL_MODEL": "qwen2.5:7b"},
			changed: []string{"OLLAMA_MODEL="},
		},
		{
			name:    "an empty OLLAMA_MODEL is dropped",
			env:     []string{"TURING_LOCAL_MODEL=", "OLLAMA_MODEL="},
			set:     []string{"OPENAI_MODEL", "gpt-x"},
			want:    map[string]string{"TURING_LOCAL_MODEL": ""},
			changed: []string{"OLLAMA_MODEL="},
		},
		{
			name:    "OLLAMA_BASE_URL moves for the ollama engine",
			env:     []string{"TURING_LOCAL_ENGINE=ollama", "TURING_LOCAL_ENGINE_URL=", "OLLAMA_BASE_URL=http://host.docker.internal:11500"},
			set:     []string{"OPENAI_MODEL", "gpt-x"},
			want:    map[string]string{"TURING_LOCAL_ENGINE_URL": "http://host.docker.internal:11500"},
			changed: []string{"OLLAMA_BASE_URL=", "TURING_LOCAL_ENGINE_URL=http://host.docker.internal:11500"},
		},
		{
			name:    "OLLAMA_BASE_URL moves when the engine is empty, which means ollama",
			env:     []string{"OLLAMA_BASE_URL=http://host.docker.internal:11500"},
			set:     []string{"OPENAI_MODEL", "gpt-x"},
			want:    map[string]string{"TURING_LOCAL_ENGINE_URL": "http://host.docker.internal:11500"},
			changed: []string{"OLLAMA_BASE_URL=", "TURING_LOCAL_ENGINE_URL=http://host.docker.internal:11500"},
		},
		{
			name:    "OLLAMA_BASE_URL is dropped for the openai engine",
			env:     []string{"TURING_LOCAL_ENGINE=openai", "TURING_LOCAL_ENGINE_URL=", "OLLAMA_BASE_URL=http://host.docker.internal:11500"},
			set:     []string{"OPENAI_MODEL", "gpt-x"},
			want:    map[string]string{"TURING_LOCAL_ENGINE_URL": ""},
			changed: []string{"OLLAMA_BASE_URL="},
		},
		{
			name:    "OLLAMA_BASE_URL is dropped when the change itself picks openai",
			env:     []string{"TURING_LOCAL_ENGINE=ollama", "OLLAMA_BASE_URL=http://host.docker.internal:11500"},
			set:     []string{"TURING_LOCAL_ENGINE", "openai"},
			want:    map[string]string{"TURING_LOCAL_ENGINE_URL": ""},
			changed: []string{"OLLAMA_BASE_URL="},
		},
		{
			name:    "every deprecated name at once",
			env:     []string{"OLLAMA_CONTEXT_WINDOW_TOKENS=8192", "OLLAMA_MAX_OUTPUT_TOKENS=1024", "TURING_LOCAL_MAX_OUTPUT_TOKENS=512"},
			set:     []string{"OPENAI_MODEL", "gpt-x"},
			want:    map[string]string{"TURING_LOCAL_CONTEXT_WINDOW_TOKENS": "8192", "TURING_LOCAL_MAX_OUTPUT_TOKENS": "512"},
			changed: []string{"OLLAMA_CONTEXT_WINDOW_TOKENS=", "OLLAMA_MAX_OUTPUT_TOKENS=", "TURING_LOCAL_CONTEXT_WINDOW_TOKENS=8192"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.writeEnv(fullEnv(tt.env...))

			if code := h.config("set", tt.set[0], tt.set[1], "--no-apply"); code != 0 {
				t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
			}

			file := h.readEnvFile()
			for name, want := range tt.want {
				if got, _ := file.Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			for _, old := range []string{"OLLAMA_MODEL", "OLLAMA_BASE_URL", "OLLAMA_CONTEXT_WINDOW_TOKENS", "OLLAMA_MAX_OUTPUT_TOKENS"} {
				if _, present := file.Get(old); present {
					t.Errorf("%s is still in .env", old)
				}
			}
			calls := h.checkCalls()
			if len(calls) != 2 {
				t.Fatalf("check-config ran %d times", len(calls))
			}
			want := append([]string{"PATH=/usr/bin:/bin"}, append(tt.changed, tt.set[0]+"="+tt.set[1])...)
			slices.Sort(want[1:])
			if !slices.Equal(calls[0].Env, want) {
				t.Fatalf("check-config env = %q, want %q", calls[0].Env, want)
			}
		})
	}
}

// Compose expands a bare or double-quoted $ in .env, and Set writes a value
// single-quoted, so moving such a value would freeze ${MODEL_NAME} into its
// text: the old line is left alone, and its new name too.
func TestSaveLeavesADeprecatedValueComposeInterpolatesWhereItIs(t *testing.T) {
	tests := []struct {
		name, line string
		env        []string
		moved      string // TURING_LOCAL_MODEL's line afterwards, "" when there is none
	}{
		{name: "bare", line: "OLLAMA_MODEL=${MODEL_NAME}"},
		{name: "bare without braces", line: "OLLAMA_MODEL=$MODEL_NAME"},
		{name: "double quoted", line: `OLLAMA_MODEL="$MODEL_NAME"`},
		{name: "bare, with the new name set", line: "OLLAMA_MODEL=${MODEL_NAME}", env: []string{"TURING_LOCAL_MODEL=qwen2.5:7b"}, moved: "TURING_LOCAL_MODEL=qwen2.5:7b"},
		{name: "single quoted, which Compose reads as written", line: "OLLAMA_MODEL='${MODEL_NAME}'", moved: "TURING_LOCAL_MODEL='${MODEL_NAME}'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.writeEnv(fullEnv(append([]string{"MODEL_NAME=llama3", tt.line}, tt.env...)...))

			if code := h.config("set", "OPENAI_MODEL", "gpt-x", "--no-apply"); code != 0 {
				t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
			}

			lines := strings.Split(string(h.envBytes()), "\n")
			interpolated := !strings.Contains(tt.line, "'")
			if slices.Contains(lines, tt.line) != interpolated {
				t.Fatalf("%s kept = %v, want %v:\n%s", tt.line, !interpolated, interpolated, h.envBytes())
			}
			newLine := ""
			for _, line := range lines {
				if strings.HasPrefix(line, "TURING_LOCAL_MODEL=") {
					newLine = line
				}
			}
			if newLine != tt.moved {
				t.Fatalf("TURING_LOCAL_MODEL line = %q, want %q", newLine, tt.moved)
			}
			want := []string{"PATH=/usr/bin:/bin", "OPENAI_MODEL=gpt-x"}
			if !interpolated {
				want = []string{"PATH=/usr/bin:/bin", "OLLAMA_MODEL=", "OPENAI_MODEL=gpt-x", "TURING_LOCAL_MODEL=${MODEL_NAME}"}
			}
			if calls := h.checkCalls(); len(calls) != 2 || !slices.Equal(calls[0].Env, want) {
				t.Fatalf("check-config env = %q, want %q", h.checkCalls(), want)
			}
		})
	}
}

// --- host rules for the app port ---

func TestSaveRefusesAnAppPortTheHostCannotUse(t *testing.T) {
	held := listenOnLoopback(t)
	tests := []struct {
		port, want string
	}{
		{"0", "from 1 to 65535"},
		{"65536", "from 1 to 65535"},
		{"abc", "from 1 to 65535"},
		{"+80", "from 1 to 65535"},
		{"3001", "must differ from ORCHESTRATOR_INTERNAL_PORT (3001)"},
		{held, "port " + held + " is already in use on 127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.port, func(t *testing.T) {
			h := newConfigHarness(t)
			before := h.envBytes()

			if code := h.config("set", "ORCHESTRATOR_PUBLIC_PORT", tt.port, "--yes"); code != 1 {
				t.Fatalf("exit = %d, want 1; stderr: %s", code, h.errOut)
			}
			if !strings.Contains(h.errOut.String(), tt.want) {
				t.Fatalf("stderr = %q, want %q", h.errOut, tt.want)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
			h.unchanged(before)
		})
	}
}

func TestSaveComparesTheAppPortWithTheInternalPortItWillHave(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv("ORCHESTRATOR_INTERNAL_PORT=4100"))

	if code := h.config("set", "ORCHESTRATOR_PUBLIC_PORT", "4100", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(h.errOut.String(), "ORCHESTRATOR_INTERNAL_PORT (4100)") {
		t.Fatalf("stderr = %q", h.errOut)
	}
}

func TestSaveDoesNotAskForThePortTuringAlreadyHolds(t *testing.T) {
	h := newConfigHarness(t)
	held := listenOnLoopback(t)
	// The same port written another way, so the save is not a no-op.
	h.writeEnv(fullEnv("ORCHESTRATOR_PUBLIC_PORT='" + held + "'"))

	if code := h.config("set", "ORCHESTRATOR_PUBLIC_PORT", held, "--no-apply"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
}

func TestSaveAppliesAFreeAppPortAndWaitsForHealthOnIt(t *testing.T) {
	h := newConfigHarness(t)
	free := freeLoopbackPort(t)

	if code := h.config("set", "ORCHESTRATOR_PUBLIC_PORT", free, "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if got, _ := h.envGet("ORCHESTRATOR_PUBLIC_PORT"); got != free {
		t.Fatalf("ORCHESTRATOR_PUBLIC_PORT = %q", got)
	}
	if len(h.healthCalls) == 0 || h.healthCalls[0].key != testKey || itoa(h.healthCalls[0].port) != free {
		t.Fatalf("health calls = %v, want port %s", h.healthCalls, free)
	}
}

// --- images and Docker ---

func TestSaveNeedsImagesThatKnowCheckConfig(t *testing.T) {
	tests := []struct {
		name  string
		reply fakeReply
	}{
		{"no label", fakeReply{stdout: "\n"}},
		{"<no value>", fakeReply{stdout: "<no value>\n"}},
		{"a label without config", fakeReply{stdout: "engine\n"}},
		{"a missing image", fakeReply{stderr: "Error: No such image\n", code: 1}},
	}
	for _, service := range checkedServices {
		for _, tt := range tests {
			t.Run(service+" "+tt.name, func(t *testing.T) {
				h := newConfigHarness(t)
				h.runner.on(inspectCall(service), tt.reply)
				before := h.envBytes()

				if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--yes"); code != 1 {
					t.Fatalf("exit = %d, want 1", code)
				}
				if !strings.Contains(h.errOut.String(), imagesPredate) {
					t.Fatalf("stderr = %q, want %q", h.errOut, imagesPredate)
				}
				if h.ranCommand("compose run") || h.ranCommand("compose up") {
					t.Fatalf("ran %q", h.runner.commands())
				}
				h.unchanged(before)
			})
		}
	}
}

func TestSaveWithDockerDownSaysSoAndSavesNothing(t *testing.T) {
	h := newConfigHarness(t)
	h.GOOS = "darwin"
	h.dockerDown()
	before := h.envBytes()

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	got := h.errOut.String()
	for _, want := range []string{"Docker is not running", "nothing was saved", "open Docker Desktop"} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	}
	if slices.ContainsFunc(h.runner.commands(), func(c string) bool { return strings.HasPrefix(c, "compose") || strings.HasPrefix(c, "docker image") }) {
		t.Fatalf("ran %q", h.runner.commands())
	}
	h.unchanged(before)
}

func TestSaveThatCannotReadTheImageLabelIsNotChecked(t *testing.T) {
	h := newConfigHarness(t)
	h.runner.on(imagesCall("turing-orchestrator"), fakeReply{stderr: "Compose launch failed: data must be a real directory.\n", code: 1})
	before := h.envBytes()

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := h.errOut.String(); !strings.Contains(got, "not checked") || !strings.Contains(got, "Compose launch failed: data must be a real directory.") {
		t.Fatalf("stderr = %q", got)
	}
	if h.ranCommand("compose run") {
		t.Fatalf("ran %q", h.runner.commands())
	}
	h.unchanged(before)
}

// --- the validation call ---

func TestSaveChecksTheCandidateWithBothServicesBeforeWriting(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv("TURING_LOCAL_MODEL=", "OLLAMA_MODEL=llama3"))
	h.Environ = []string{
		"PATH=/usr/bin:/bin",
		"OPENAI_API_KEY=sk-exported",
		"OPENAI_MODEL=exported-model",
		"TURING_LOCAL_MODEL=exported-local",
		"OLLAMA_MODEL=exported-old",
		"TURING_CLIENT_API_KEY=exported-key",
	}
	before := h.envBytes()
	var sawFileAtCheck []string
	for _, service := range checkedServices {
		h.runner.onFunc(checkConfigCall(service), func(host.Command) fakeReply {
			sawFileAtCheck = append(sawFileAtCheck, string(h.envBytes()))
			return fakeReply{stdout: "configuration is valid\n"}
		})
	}

	if code := h.config("set", "OPENAI_MODEL", "gpt-x", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}

	wantCommands := []string{
		"docker info --format {{.ServerVersion}}",
		"compose config --images turing-orchestrator",
		"docker image inspect --format " + labelFormat + " " + imageOf("turing-orchestrator"),
		"compose config --images turing-agent-runtime-general",
		"docker image inspect --format " + labelFormat + " " + imageOf("turing-agent-runtime-general"),
		"compose run --rm --no-deps turing-orchestrator check-config",
		"compose run --rm --no-deps turing-agent-runtime-general check-config",
		"compose up -d",
		"compose ps --all --format json",
	}
	if got := h.runner.commands(); !slices.Equal(got, wantCommands) {
		t.Fatalf("commands =\n%q\nwant\n%q", got, wantCommands)
	}
	wantEnv := []string{"PATH=/usr/bin:/bin", "OLLAMA_MODEL=", "OPENAI_MODEL=gpt-x", "TURING_LOCAL_MODEL=llama3"}
	for _, call := range h.checkCalls() {
		if !slices.Equal(call.Env, wantEnv) {
			t.Errorf("%q env = %q, want %q", call.Args, call.Env, wantEnv)
		}
		if call.Dir != h.BackendDir {
			t.Errorf("%q ran in %q", call.Args, call.Dir)
		}
	}
	for _, seen := range sawFileAtCheck {
		if seen != string(before) {
			t.Fatal(".env was written before check-config passed")
		}
	}
}

func TestSaveShowsAServiceRefusalAndSavesNothing(t *testing.T) {
	for _, service := range checkedServices {
		t.Run(service, func(t *testing.T) {
			h := newConfigHarness(t)
			h.runner.on(checkConfigCall(service), fakeReply{stderr: "TURING_QUEUE_TIMEOUT_POLICY must be fail or cancel\n", code: 1})
			before := h.envBytes()

			if code := h.config("set", "TURING_QUEUE_TIMEOUT_POLICY", "pause", "--yes"); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			got := h.errOut.String()
			friendly := "orchestrator"
			if service == "turing-agent-runtime-general" {
				friendly = "runtime"
			}
			for _, want := range []string{"the " + friendly + " refused the change, so nothing was saved", "TURING_QUEUE_TIMEOUT_POLICY must be fail or cancel"} {
				if !strings.Contains(got, want) {
					t.Errorf("stderr = %q, want %q", got, want)
				}
			}
			if strings.Contains(got, "not checked") {
				t.Errorf("a refusal was reported as not checked: %q", got)
			}
			if h.ranCommand("compose up") {
				t.Fatalf("ran %q", h.runner.commands())
			}
			if service == "turing-orchestrator" && len(h.checkCalls()) != 1 {
				t.Fatalf("the runtime was checked after the orchestrator refused: %q", h.runner.commands())
			}
			h.unchanged(before)
		})
	}
}

func TestSaveReportsACheckThatCouldNotRunAsNotChecked(t *testing.T) {
	tests := []struct {
		name  string
		reply fakeReply
		shows string
	}{
		{"a launch check", fakeReply{stderr: "Compose launch failed: sandbox must have mode 0700.\n", code: 1}, "Compose launch failed: sandbox must have mode 0700."},
		{"Docker went away", fakeReply{stderr: "Cannot connect to the Docker daemon at unix:///var/run/docker.sock.\n", code: 1}, "Cannot connect to the Docker daemon"},
		{"the daemon refused", fakeReply{stdout: "Error response from daemon: no space left on device\n", code: 1}, "no space left on device"},
		{"stdout without a final newline", fakeReply{stdout: "pulling", stderr: "Error response from daemon: x\n", code: 1}, "  pulling\n  Error response from daemon: x\n"},
		{"another exit code", fakeReply{stderr: "line1\nline2\nline3\nline4\nline5\nline6\n", code: 125}, "line2\n"},
		{"an unknown argument", fakeReply{stderr: "unknown argument\n", code: 2}, "unknown argument"},
		{"compose.sh could not start", fakeReply{err: errCouldNotStart, code: -1}, "permission denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.runner.on(checkConfigCall("turing-orchestrator"), tt.reply)
			before := h.envBytes()

			if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--yes"); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			got := h.errOut.String()
			if !strings.Contains(got, "not checked") || !strings.Contains(got, "nothing was saved") || !strings.Contains(got, tt.shows) {
				t.Fatalf("stderr = %q, want not checked with %q", got, tt.shows)
			}
			if strings.Contains(got, "refused the change") {
				t.Fatalf("an infrastructure failure was reported as a refusal: %q", got)
			}
			if strings.Contains(got, "line1") {
				t.Fatalf("stderr = %q, want only the last 5 lines", got)
			}
			if h.ranCommand("compose up") {
				t.Fatalf("ran %q", h.runner.commands())
			}
			h.unchanged(before)
		})
	}
}

func TestSavePassesOnTheWarningsOfAPassingCheck(t *testing.T) {
	h := newConfigHarness(t)
	h.runner.on(checkConfigCall("turing-agent-runtime-general"), fakeReply{
		stdout: "configuration is valid\n",
		stderr: "Container x Creating\nwarning: OLLAMA_KEEP_ALIVE is shorter than the approval wait\n",
	})

	if code := h.config("set", "OLLAMA_KEEP_ALIVE", "30s", "--no-apply"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if !strings.Contains(h.out.String(), "runtime: warning: OLLAMA_KEEP_ALIVE is shorter than the approval wait") {
		t.Fatalf("stdout = %q", h.out)
	}
	if strings.Contains(h.out.String(), "Creating") {
		t.Fatalf("stdout = %q shows Compose's own progress", h.out)
	}
}

// --- writing ---

func TestSaveRefusesWhileAnotherCommandHoldsTheLock(t *testing.T) {
	h := newConfigHarness(t)
	before := h.envBytes()
	unlock, err := lockDotEnv(h.harness)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(h.errOut.String(), "another turing command is changing .env") {
		t.Fatalf("stderr = %q", h.errOut)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
	h.unchanged(before)
}

func TestSaveWithoutADotEnvSaysToRunInitAndMakesNoLock(t *testing.T) {
	h := newConfigHarness(t)
	h.removeEnv()

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(h.errOut.String(), "run ./scripts/init.sh first") {
		t.Fatalf("stderr = %q", h.errOut)
	}
	if _, err := os.Lstat(filepath.Join(h.BackendDir, ".env.lock")); err == nil {
		t.Fatal("a lock file was made without a .env")
	}
}

func TestSaveRefusesASymlinkedDotEnvAndLeavesItsTargetAlone(t *testing.T) {
	h := newConfigHarness(t)
	target := filepath.Join(h.BackendDir, "elsewhere")
	if err := os.Rename(filepath.Join(h.BackendDir, ".env"), target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(h.BackendDir, ".env")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(h.errOut.String(), "regular file") {
		t.Fatalf("stderr = %q", h.errOut)
	}
	if after, _ := os.ReadFile(target); string(after) != string(before) {
		t.Fatal("the symlink's target changed")
	}
	if info, err := os.Lstat(filepath.Join(h.BackendDir, ".env")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf(".env is no longer the symlink: %v", err)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
}

func TestSaveRefusesADeprecatedValueItCannotMove(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv(`OLLAMA_MODEL=ends-in\`))
	before := h.envBytes()

	if code := h.config("set", "OPENAI_MODEL", "gpt-x", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := h.errOut.String(); !strings.Contains(got, "cannot move OLLAMA_MODEL to TURING_LOCAL_MODEL") || !strings.Contains(got, "./turing config set") {
		t.Fatalf("stderr = %q", got)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
	h.unchanged(before)
}

func TestSaveRefusesAValueDotEnvCannotHoldAndNamesTheSetting(t *testing.T) {
	h := newConfigHarness(t)
	before := h.envBytes()

	if code := h.config("set", "TURING_LOCAL_ENGINE_LABEL", `ends in a backslash\`, "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := h.errOut.String(); !strings.Contains(got, "TURING_LOCAL_ENGINE_LABEL") || !strings.Contains(got, "backslash") {
		t.Fatalf("stderr = %q", got)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
	h.unchanged(before)
}

func TestSaveWritesOnlyTheChangedLineAtMode0600(t *testing.T) {
	h := newConfigHarness(t)
	content := "# my own notes\n" + fullEnv("# a comment Compose ignores", "MY_OWN_SETTING='kept $as is'", "TURING_LOCAL_MODEL=qwen2.5:7b", "TURING_LOCAL_ENGINE_LABEL=") + "# trailing\n"
	path := filepath.Join(h.BackendDir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	if code := h.config("set", "TURING_LOCAL_ENGINE_LABEL", "my engine", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}

	want := strings.Replace(content, "TURING_LOCAL_ENGINE_LABEL=\n", "TURING_LOCAL_ENGINE_LABEL='my engine'\n", 1)
	if got := string(h.envBytes()); got != want {
		t.Fatalf(".env =\n%s\nwant\n%s", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %04o, want 0600", info.Mode().Perm())
	}
	if !strings.Contains(h.out.String(), "Turing is running with the new settings.") {
		t.Fatalf("stdout = %q", h.out)
	}
	if len(h.healthCalls) == 0 || h.healthCalls[0] != (healthCall{testPort, testKey}) {
		t.Fatalf("health calls = %v", h.healthCalls)
	}
	h.noOutputContains("CANARY", testKey)
}

func TestSaveThatChangesNothingRunsNothing(t *testing.T) {
	h := newConfigHarness(t)
	before := h.envBytes()

	if code := h.config("set", "TURING_LOCAL_MODEL", "qwen2.5:7b", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
	if !strings.Contains(h.out.String(), "Nothing to change") {
		t.Fatalf("stdout = %q", h.out)
	}
	h.unchanged(before)
}

// --- applying ---

func TestSaveWithNoApplySavesWithoutRestarting(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--no-apply", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if got, _ := h.envGet("TURING_LOCAL_MODEL"); got != "llama3.2" {
		t.Fatalf("TURING_LOCAL_MODEL = %q", got)
	}
	if h.ranCommand("compose up") || strings.Contains(h.out.String(), "Apply now?") || strings.Contains(h.out.String(), "Saved in .env.") {
		t.Fatalf("ran %q; stdout %q", h.runner.commands(), h.out)
	}
	if !strings.Contains(h.out.String(), savedNotApplied) {
		t.Fatalf("stdout = %q", h.out)
	}
}

// interruptedAnswer is a terminal whose first read fails, as when Ctrl-C
// ends the prompt, and it keeps what stdout held at that moment.
type interruptedAnswer struct {
	out  *bytes.Buffer
	seen string
	read bool
}

func (a *interruptedAnswer) Read([]byte) (int, error) {
	if !a.read {
		a.seen, a.read = a.out.String(), true
	}
	return 0, io.ErrUnexpectedEOF
}

// Ctrl-C at the Apply prompt ends the process, so what was saved has to be
// said before the question.
func TestSaveSaysItSavedBeforeAskingToApply(t *testing.T) {
	h := newConfigHarness(t)
	h.IsTerminal = true
	answer := &interruptedAnswer{out: h.out}
	h.Stdin = answer

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if want := "Saved in .env.\n" + applyQuestion + " "; !strings.HasSuffix(answer.seen, want) {
		t.Fatalf("stdout when the answer was read = %q, want it to end %q", answer.seen, want)
	}
	if !strings.Contains(h.out.String(), savedNotApplied) {
		t.Fatalf("stdout = %q, want the declined save's own message too", h.out)
	}
}

func TestSaveSaysItSavedBeforeApplyingWithYes(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	out := h.out.String()
	saved, waiting := strings.Index(out, "Saved in .env.\n"), strings.Index(out, "Waiting for Turing")
	if saved < 0 || waiting < 0 || saved > waiting {
		t.Fatalf("stdout = %q, want Saved in .env. before the wait", out)
	}
}

func TestSaveDeclinedOffATerminalSaysWhenItTakesEffect(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	out := h.out.String()
	if !strings.Contains(out, applyQuestion+" no (not a terminal; pass --yes to confirm)") || !strings.Contains(out, savedNotApplied) {
		t.Fatalf("stdout = %q", out)
	}
	if got, _ := h.envGet("TURING_LOCAL_MODEL"); got != "llama3.2" {
		t.Fatalf("TURING_LOCAL_MODEL = %q", got)
	}
	if h.ranCommand("compose up") {
		t.Fatalf("ran %q", h.runner.commands())
	}
	if strings.Contains(out, "may refuse") {
		t.Fatalf("stdout = %q warns about a bound it did not change", out)
	}
}

func TestSaveThatTurnsAQueueBoundOffAndIsNotAppliedWarns(t *testing.T) {
	for _, key := range []string{"TURING_QUEUE_MAX_WAIT_MS", "TURING_JOB_REAPER_INTERVAL_MS"} {
		t.Run(key, func(t *testing.T) {
			h := newConfigHarness(t)

			if code := h.config("set", key, "0"); code != 0 {
				t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
			}
			out := h.out.String()
			want := "the next start may refuse " + key + "=0, because only a running orchestrator can see whether a delegated task is waiting for consent"
			if !strings.Contains(out, savedNotApplied) || !strings.Contains(out, want) {
				t.Fatalf("stdout = %q, want %q", out, want)
			}
		})
	}
}

// unhealthyWhile makes the runtime restart for as long as .env holds marker.
func unhealthyWhile(h *harness, marker string) {
	h.runner.onFunc(composeCall(psArgs...), func(host.Command) fakeReply {
		if strings.Contains(string(h.envBytes()), marker) {
			return fakeReply{stdout: psOutput(withService(allRunning(), "turing-agent-runtime-general", "restarting", "")...)}
		}
		return fakeReply{stdout: psOutput(allRunning()...)}
	})
}

func TestApplyThatDoesNotComeUpHealthyRestoresThePreviousSettingsWithYes(t *testing.T) {
	h := newConfigHarness(t)
	h.HealthTimeout = 30 * time.Millisecond
	before := h.envBytes()
	unhealthyWhile(h.harness, "TURING_LOCAL_MODEL=broken")

	if code := h.config("set", "TURING_LOCAL_MODEL", "broken", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	h.unchanged(before)
	if n := h.countCommand("compose up -d"); n != 2 {
		t.Fatalf("up -d ran %d times, want 2: %q", n, h.runner.commands())
	}
	got := h.errOut.String()
	for _, want := range []string{"runtime is not healthy", "restored the previous settings", "./turing logs runtime"} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	}
	if !strings.Contains(h.out.String(), "Restoring the previous settings") {
		t.Fatalf("stdout = %q", h.out)
	}
	// The previous settings are what the second wait asks health with.
	if last := h.healthCalls[len(h.healthCalls)-1]; last != (healthCall{testPort, testKey}) {
		t.Fatalf("last health call = %v", last)
	}
}

func TestApplyThatIsNotRestoredNamesTheLogsCommand(t *testing.T) {
	h := newConfigHarness(t)
	h.HealthTimeout = 30 * time.Millisecond
	h.IsTerminal = true
	h.Stdin = strings.NewReader("y\nn\n")
	unhealthyWhile(h.harness, "TURING_LOCAL_MODEL=broken")

	if code := h.config("set", "TURING_LOCAL_MODEL", "broken"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got, _ := h.envGet("TURING_LOCAL_MODEL"); got != "broken" {
		t.Fatalf("TURING_LOCAL_MODEL = %q, want the new value kept", got)
	}
	if n := h.countCommand("compose up -d"); n != 1 {
		t.Fatalf("up -d ran %d times, want 1", n)
	}
	got := h.errOut.String()
	if !strings.Contains(got, "the new settings are saved in .env, but the runtime did not come up healthy; run ./turing logs runtime") {
		t.Fatalf("stderr = %q", got)
	}
	if strings.Contains(got, "restored") {
		t.Fatalf("stderr = %q", got)
	}
	if !strings.Contains(h.out.String(), "The runtime did not come up healthy. Restore the previous settings and apply again? [Y/n] ") {
		t.Fatalf("stdout = %q", h.out)
	}
}

func TestApplyThatComposeCannotStartOffersTheRestoreToo(t *testing.T) {
	h := newConfigHarness(t)
	before := h.envBytes()
	h.runner.onFunc(composeCall("up", "-d"), func(host.Command) fakeReply {
		if strings.Contains(string(h.envBytes()), "TURING_LOCAL_MODEL=broken") {
			return fakeReply{stderr: "Bind for 127.0.0.1:3456 failed: port is already allocated\n", code: 1}
		}
		return fakeReply{}
	})

	if code := h.config("set", "TURING_LOCAL_MODEL", "broken", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	h.unchanged(before)
	if n := h.countCommand("compose up -d"); n != 2 {
		t.Fatalf("up -d ran %d times, want 2", n)
	}
	if !strings.Contains(h.out.String(), "Restoring the previous settings") {
		t.Fatalf("stdout = %q", h.out)
	}
	if got := h.errOut.String(); !strings.Contains(got, "compose up -d failed") || !strings.Contains(got, "restored the previous settings") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestApplyThatComposeCannotStartAsksAboutTuringAtATerminal(t *testing.T) {
	h := newConfigHarness(t)
	h.IsTerminal = true
	h.Stdin = strings.NewReader("y\nn\n")
	h.runner.on(composeCall("up", "-d"), fakeReply{code: 1})

	if code := h.config("set", "TURING_LOCAL_MODEL", "broken"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(h.out.String(), "Turing did not come up healthy. Restore the previous settings and apply again? [Y/n] ") {
		t.Fatalf("stdout = %q", h.out)
	}
	if got := h.errOut.String(); !strings.Contains(got, "the new settings are saved in .env, but Turing did not come up healthy; run ./turing logs") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestApplyWhosePreviousSettingsFailToo(t *testing.T) {
	h := newConfigHarness(t)
	h.HealthTimeout = 30 * time.Millisecond
	before := h.envBytes()
	h.runner.on(composeCall(psArgs...), fakeReply{stdout: psOutput(withService(allRunning(), "turing-mcp-files", "exited", "")...)})

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	h.unchanged(before)
	if got := h.errOut.String(); !strings.Contains(got, "restored the previous settings, but they did not come up healthy either") || !strings.Contains(got, "./turing logs files") {
		t.Fatalf("stderr = %q", got)
	}
}

// --- saveSettings, which PR 3 reuses ---

func TestSaveSettingsDeletesAKeyGivenNil(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv("TURING_LOCAL_ENGINE_LABEL=ds4"))
	h.dockerUp()
	for _, service := range checkedServices {
		h.runner.on(imagesCall(service), fakeReply{stdout: imageOf(service) + "\n"})
		h.runner.on(inspectCall(service), fakeReply{stdout: "config,engine\n"})
		h.runner.on(checkConfigCall(service), fakeReply{})
	}

	if code := h.saveSettings(t.Context(), map[string]*string{"TURING_LOCAL_ENGINE_LABEL": nil}, saveOptions{NoApply: true}); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if _, present := h.envGet("TURING_LOCAL_ENGINE_LABEL"); present {
		t.Fatal("TURING_LOCAL_ENGINE_LABEL is still in .env")
	}
	if calls := h.checkCalls(); len(calls) != 2 || !slices.Contains(calls[0].Env, "TURING_LOCAL_ENGINE_LABEL=") {
		t.Fatalf("check-config calls = %v", calls)
	}
}

func TestSaveSettingsRefusesAKeyConfigDoesNotChange(t *testing.T) {
	for _, name := range []string{"TURING_CLIENT_API_KEY", "DATABASE_PATH", "OLLAMA_MODEL", "NOT_A_SETTING"} {
		h := newConfigHarness(t)
		before := h.envBytes()
		value := "x"

		if code := h.saveSettings(t.Context(), map[string]*string{name: &value}, saveOptions{Yes: true}); code != 1 {
			t.Fatalf("%s: exit = %d, want 1", name, code)
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("%s: ran %q", name, h.runner.commands())
		}
		h.unchanged(before)
	}
}
