package cli

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
)

const (
	imagesPredateEngine  = "the images predate check-engine; run ./turing start --build first"
	engineFailedAfterRun = "The settings are saved and Turing is running, but the engine check failed; fix the engine, then run ./turing engine check."
	engineCheckHint      = "After ./turing start, run ./turing engine check."
	engineKeyCanary      = "sk-ENGINE-KEY-CANARY"
	engineRefusal        = "step 4/4 (tool calling) failed: the model answered with text instead of calling turing_engine_check; the model or server does not do tool calling, so choose another model\n"
)

// runEngine runs `turing engine` with every step of a save and an engine check
// scripted to succeed, unless the test scripted its own first.
func (c *configHarness) runEngine(args ...string) int {
	c.t.Helper()
	c.scriptSave()
	return c.run(append([]string{"engine"}, args...)...)
}

// engineCheckCalls returns the check-engine runs, in order.
func (h *harness) engineCheckCalls() []host.Command {
	var calls []host.Command
	for _, call := range h.runner.calls {
		if call.Name == "./scripts/compose.sh" && slices.Contains(call.Args, "check-engine") {
			calls = append(calls, call)
		}
	}
	return calls
}

func (h *harness) wantEnv(t *testing.T, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if got, _ := h.envGet(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

var saveAndCheckEngine = []string{
	"docker info --format {{.ServerVersion}}",
	"compose config --images turing-orchestrator",
	"docker image inspect --format " + labelFormat + " " + imageOf("turing-orchestrator"),
	"compose config --images turing-agent-runtime-general",
	"docker image inspect --format " + labelFormat + " " + imageOf("turing-agent-runtime-general"),
	"compose run --rm --no-deps turing-orchestrator check-config",
	"compose run --rm --no-deps turing-agent-runtime-general check-config",
	"compose up -d",
	"compose ps --all --format json",
	"docker info --format {{.ServerVersion}}",
	"compose config --images turing-agent-runtime-general",
	"docker image inspect --format " + labelFormat + " " + imageOf("turing-agent-runtime-general"),
	"compose run --rm --no-deps turing-agent-runtime-general check-engine",
}

// --- engine check ---

func TestEngineCheckRunsTheRuntimesCheckEngineWithoutTheShellsSettings(t *testing.T) {
	h := newConfigHarness(t)
	h.Environ = []string{
		"PATH=/usr/bin:/bin",
		"TURING_LOCAL_MODEL=exported-model",
		"TURING_LOCAL_ENGINE_URL=http://host.docker.internal:9999",
		"OLLAMA_BASE_URL=http://host.docker.internal:9998",
		"OPENAI_API_KEY=sk-exported",
	}

	if code := h.runEngine("check"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	want := []string{
		"docker info --format {{.ServerVersion}}",
		"compose config --images turing-agent-runtime-general",
		"docker image inspect --format " + labelFormat + " " + imageOf("turing-agent-runtime-general"),
		"compose run --rm --no-deps turing-agent-runtime-general check-engine",
	}
	if got := h.runner.commands(); !slices.Equal(got, want) {
		t.Fatalf("commands =\n%q\nwant\n%q", got, want)
	}
	call := h.engineCheckCalls()[0]
	if !slices.Equal(call.Env, []string{"PATH=/usr/bin:/bin"}) || call.Dir != h.BackendDir || call.Stdin != nil {
		t.Fatalf("check-engine ran in %q with env %q and stdin %v; want the backend, no setting from the shell, and no input", call.Dir, call.Env, call.Stdin)
	}
	if !strings.Contains(h.out.String(), engineUsable) {
		t.Fatalf("stdout = %q, want the check's own steps", h.out)
	}
	if h.errOut.Len() != 0 {
		t.Fatalf("stderr = %q", h.errOut)
	}
}

func TestEngineCheckShowsEachStepAsItHappens(t *testing.T) {
	h := newConfigHarness(t)
	var stdoutDuring, stderrDuring string
	h.runner.onFunc(checkEngineCall(), func(cmd host.Command) fakeReply {
		_, _ = cmd.Stdout.Write([]byte("1/4 reachable: ok\n"))
		_, _ = cmd.Stderr.Write([]byte("warning: OLLAMA_MODEL is deprecated\n"))
		stdoutDuring, stderrDuring = h.out.String(), h.errOut.String()
		return fakeReply{stdout: "2/4 model present: ok\n"}
	})

	if code := h.runEngine("check"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if !strings.HasSuffix(stdoutDuring, "1/4 reachable: ok\n") || !strings.HasSuffix(stderrDuring, "warning: OLLAMA_MODEL is deprecated\n") {
		t.Fatalf("while the check ran, stdout was %q and stderr %q; want each line passed on as it was written", stdoutDuring, stderrDuring)
	}
}

func TestEngineCheckPassesTheRuntimesRefusalThroughAndExits1(t *testing.T) {
	h := newConfigHarness(t)
	h.runner.on(checkEngineCall(), fakeReply{stdout: "1/4 reachable: ok\n2/4 model present: ok\n3/4 streaming: ok\n", stderr: engineRefusal, code: 1})

	if code := h.runEngine("check"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := h.errOut.String(); got != engineRefusal {
		t.Fatalf("stderr = %q, want the runtime's sentence alone, once", got)
	}
	if !strings.Contains(h.out.String(), "3/4 streaming: ok\n") {
		t.Fatalf("stdout = %q, want the steps that passed", h.out)
	}
}

func TestEngineCheckThatDidNotRunIsNotChecked(t *testing.T) {
	tests := []struct {
		name  string
		reply fakeReply
		want  string
	}{
		{"Compose's own checks", fakeReply{stderr: "Compose launch failed: data must be a real directory.\n", code: 1}, "Compose launch failed: data must be a real directory."},
		{"the Docker daemon", fakeReply{stderr: "Error response from daemon: no such image\n", code: 125}, "Error response from daemon: no such image"},
		{"an interruption", fakeReply{stdout: "1/4 reachable: ok\n", code: 130}, "1/4 reachable: ok"},
		{"compose.sh that cannot start", fakeReply{err: errCouldNotStart, code: -1}, errCouldNotStart.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.runner.on(checkEngineCall(), tt.reply)

			if code := h.runEngine("check"); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			got := h.errOut.String()
			for _, want := range []string{"not checked", "  " + tt.want, "./turing engine check"} {
				if !strings.Contains(got, want) {
					t.Errorf("stderr = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestEngineCheckWithDockerDownSaysHowToStartIt(t *testing.T) {
	for goos, how := range map[string]string{"darwin": "open Docker Desktop", "linux": "sudo systemctl start docker"} {
		t.Run(goos, func(t *testing.T) {
			h := newConfigHarness(t)
			h.GOOS = goos
			h.dockerDown()

			if code := h.runEngine("check"); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			got := h.errOut.String()
			for _, want := range []string{"Docker is not running", how, "./turing engine check"} {
				if !strings.Contains(got, want) {
					t.Errorf("stderr = %q, want %q", got, want)
				}
			}
			if want := []string{"docker info --format {{.ServerVersion}}"}; !slices.Equal(h.runner.commands(), want) {
				t.Fatalf("ran %q", h.runner.commands())
			}
		})
	}
}

func TestEngineCheckNeedsARuntimeImageThatKnowsCheckEngine(t *testing.T) {
	tests := []struct {
		name  string
		reply fakeReply
	}{
		{"a label without engine", fakeReply{stdout: "config\n"}},
		{"<no value>", fakeReply{stdout: "<no value>\n"}},
		{"no label", fakeReply{stdout: "\n"}},
		{"a missing image", fakeReply{stderr: "Error: No such image\n", code: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.runner.on(inspectCall("turing-agent-runtime-general"), tt.reply)

			if code := h.runEngine("check"); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if got := h.errOut.String(); got != imagesPredateEngine+"\n" {
				t.Fatalf("stderr = %q, want %q", got, imagesPredateEngine)
			}
			if h.ranCommand("compose run") {
				t.Fatalf("ran %q; an image that cannot say it knows check-engine is never run with it", h.runner.commands())
			}
		})
	}
}

func TestEngineCheckThatCannotReadTheImageLabelIsNotChecked(t *testing.T) {
	h := newConfigHarness(t)
	h.runner.on(imagesCall("turing-agent-runtime-general"), fakeReply{stderr: "Compose launch failed: memory must be mode 0700.\n", code: 1})

	if code := h.runEngine("check"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := h.errOut.String(); !strings.Contains(got, "not checked") || !strings.Contains(got, "memory must be mode 0700") {
		t.Fatalf("stderr = %q", got)
	}
	if h.ranCommand("compose run") {
		t.Fatalf("ran %q", h.runner.commands())
	}
}

func TestEngineCheckNeedsADotEnv(t *testing.T) {
	h := newConfigHarness(t)
	h.removeEnv()

	if code := h.runEngine("check"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := h.errOut.String(); !strings.Contains(got, "there is no .env file") {
		t.Fatalf("stderr = %q", got)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
}

func TestEngineCheckTakesNoArgumentAndDoesNotEchoOne(t *testing.T) {
	for _, args := range [][]string{
		{"check", "sk-CANARY-ARGUMENT"},
		{"check", "--yes"},
		{"check", "--url", "http://localhost:8000/v1"},
	} {
		h := newConfigHarness(t)

		if code := h.run(append([]string{"engine"}, args...)...); code != 2 {
			t.Fatalf("%q: exit = %d, want 2", args, code)
		}
		if len(h.runner.calls) != 0 || strings.Contains(h.errOut.String(), "CANARY") || !strings.Contains(h.errOut.String(), "./turing help") {
			t.Fatalf("%q: ran %q; stderr %q", args, h.runner.commands(), h.errOut)
		}
	}
}

// --- engine use ---

func TestEngineUseOpenAIRewritesLocalhostLabelsDS4AndChecksTheEngineOnceApplied(t *testing.T) {
	h := newConfigHarness(t)
	before := h.envBytes()
	var sawFileAtCheck []string
	for _, service := range checkedServices {
		h.runner.onFunc(checkConfigCall(service), func(host.Command) fakeReply {
			sawFileAtCheck = append(sawFileAtCheck, string(h.envBytes()))
			return fakeReply{stdout: "configuration is valid\n"}
		})
	}

	if code := h.runEngine("use", "openai", "--url", "http://localhost:8000/v1", "--model", "deepseek-chat", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}

	h.wantEnv(t, map[string]string{
		"TURING_LOCAL_ENGINE":       "openai",
		"TURING_LOCAL_ENGINE_URL":   "http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL":        "deepseek-chat",
		"TURING_LOCAL_ENGINE_LABEL": "ds4",
	})
	if _, present := h.envGet("TURING_LOCAL_ENGINE_API_KEY"); present {
		t.Error("TURING_LOCAL_ENGINE_API_KEY was written, though no key was given")
	}
	if got := h.runner.commands(); !slices.Equal(got, saveAndCheckEngine) {
		t.Fatalf("commands =\n%q\nwant\n%q", got, saveAndCheckEngine)
	}
	for _, call := range h.checkCalls() {
		for _, want := range []string{
			"TURING_LOCAL_ENGINE=openai",
			"TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1",
			"TURING_LOCAL_MODEL=deepseek-chat",
			"TURING_LOCAL_ENGINE_LABEL=ds4",
		} {
			if !slices.Contains(call.Env, want) {
				t.Errorf("%q env = %q, want %q", call.Args, call.Env, want)
			}
		}
	}
	for _, seen := range sawFileAtCheck {
		if seen != string(before) {
			t.Fatal(".env was written before check-config passed")
		}
	}
	out := h.out.String()
	inOrder(t, out, "Turing is running with the new settings.\n", engineUsable)
	if strings.Contains(out, engineCheckHint) {
		t.Fatalf("stdout = %q; the engine was checked, so there is nothing to run after", out)
	}
}

func TestEngineUseOllamaWithNoOptionsReturnsToTheDefaults(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv(
		"TURING_LOCAL_ENGINE=openai",
		"TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL=deepseek-chat",
		"TURING_LOCAL_ENGINE_LABEL=ds4",
		"TURING_LOCAL_ENGINE_API_KEY="+engineKeyCanary,
		"TURING_LOCAL_CONTEXT_WINDOW_TOKENS=65536",
	))

	if code := h.runEngine("use", "ollama", "--no-apply"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}

	h.wantEnv(t, map[string]string{
		"TURING_LOCAL_ENGINE":                "ollama",
		"TURING_LOCAL_ENGINE_URL":            "",
		"TURING_LOCAL_MODEL":                 "",
		"TURING_LOCAL_ENGINE_LABEL":          "",
		"TURING_LOCAL_ENGINE_API_KEY":        "",
		"TURING_LOCAL_CONTEXT_WINDOW_TOKENS": "65536",
	})
	for _, call := range h.checkCalls() {
		for _, want := range []string{"TURING_LOCAL_ENGINE=ollama", "TURING_LOCAL_ENGINE_URL=", "TURING_LOCAL_MODEL=", "TURING_LOCAL_ENGINE_LABEL=", "TURING_LOCAL_ENGINE_API_KEY="} {
			if !slices.Contains(call.Env, want) {
				t.Errorf("%q env = %q, want %q", call.Args, call.Env, want)
			}
		}
	}
	h.noOutputContains(engineKeyCanary)
}

func TestEngineUseSetsTheURLModelLabelAndContextItIsGiven(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.runEngine("use", "ollama", "--url", "http://127.0.0.1:11500", "--model", "llama3.2", "--label", "Ollama on the GPU box", "--context", "65536", "--no-apply"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}

	h.wantEnv(t, map[string]string{
		"TURING_LOCAL_ENGINE":                "ollama",
		"TURING_LOCAL_ENGINE_URL":            "http://host.docker.internal:11500",
		"TURING_LOCAL_MODEL":                 "llama3.2",
		"TURING_LOCAL_ENGINE_LABEL":          "Ollama on the GPU box",
		"TURING_LOCAL_CONTEXT_WINDOW_TOKENS": "65536",
	})
}

func TestEngineUseOpenAIOnAnotherPortClearsTheLabelUnlessGivenOne(t *testing.T) {
	tests := []struct {
		name  string
		extra []string
		want  string
	}{
		{"no label", nil, ""},
		{"a label", []string{"--label", "llama.cpp"}, "llama.cpp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.writeEnv(fullEnv("TURING_LOCAL_ENGINE_LABEL=ds4"))
			args := append([]string{"use", "openai", "--url", "http://localhost:9000/v1", "--model", "qwen", "--no-apply"}, tt.extra...)

			if code := h.runEngine(args...); code != 0 {
				t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
			}
			h.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE_LABEL": tt.want, "TURING_LOCAL_ENGINE_URL": "http://host.docker.internal:9000/v1"})
		})
	}
}

func TestEngineUseTakesItsOptionsBeforeOrAfterTheEngine(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.runEngine("use", "--model", "deepseek-chat", "openai", "--url", "http://localhost:8000/v1", "--no-apply"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	h.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE": "openai", "TURING_LOCAL_MODEL": "deepseek-chat"})
}

func TestEngineUseOpenAINeedsAURLAndAModel(t *testing.T) {
	for _, args := range [][]string{
		{"use", "openai"},
		{"use", "openai", "--model", "deepseek-chat"},
		{"use", "openai", "--url", "http://localhost:8000/v1"},
		{"use", "openai", "--url", "", "--model", "deepseek-chat"},
	} {
		h := newConfigHarness(t)
		before := h.envBytes()

		if code := h.run(append([]string{"engine"}, args...)...); code != 2 {
			t.Fatalf("%q: exit = %d, want 2", args, code)
		}
		if got := h.errOut.String(); !strings.Contains(got, "openai needs --url and --model") {
			t.Fatalf("%q: stderr = %q", args, got)
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("%q: ran %q", args, h.runner.commands())
		}
		h.unchanged(before)
	}
}

func TestEngineUseRefusesAURLTheRuntimeRefusesWithoutSavingOrRunning(t *testing.T) {
	for _, url := range []string{
		"http://192.168.1.20:8000/v1",
		"http://example.com:8000/v1",
		"http://user:sk-CANARY-PASSWORD@localhost:8000/v1",
		"http://localhost:8000/v1?key=sk-CANARY-QUERY",
		"http://127.0.0.2:8000/v1",
	} {
		h := newConfigHarness(t)
		before := h.envBytes()

		if code := h.run("engine", "use", "openai", "--url", url, "--model", "deepseek-chat", "--yes"); code != 1 {
			t.Fatalf("%s: exit = %d, want 1", url, code)
		}
		got := h.errOut.String()
		if !strings.Contains(got, "--url") || !strings.Contains(got, "nothing was saved") || !strings.Contains(got, "host.docker.internal") {
			t.Fatalf("%s: stderr = %q, want the runtime's rule and that nothing was saved", url, got)
		}
		if strings.Contains(got, "CANARY") || strings.Contains(got, "192.168.1.20") || strings.Contains(got, "example.com") {
			t.Fatalf("%s: stderr = %q repeats the URL", url, got)
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("%s: ran %q", url, h.runner.commands())
		}
		h.unchanged(before)
	}
}

func TestEngineUseReadsTheKeyFromStdinAndNeverPrintsIt(t *testing.T) {
	h := newConfigHarness(t)
	h.Stdin = strings.NewReader(engineKeyCanary + "\n")
	// The engine check fails, so every message a failure prints is seen.
	h.runner.on(checkEngineCall(), fakeReply{stderr: "step 1/4 (reachable) failed: the engine answered HTTP 401; check TURING_LOCAL_ENGINE_API_KEY\n", code: 1})

	code := h.runEngine("use", "openai", "--url", "http://localhost:8000/v1", "--model", "deepseek-chat", "--api-key-stdin", "--yes")

	if code != 1 {
		t.Fatalf("exit = %d, want 1 for the failed engine check", code)
	}
	if got, _ := h.envGet("TURING_LOCAL_ENGINE_API_KEY"); got != engineKeyCanary {
		t.Fatalf("TURING_LOCAL_ENGINE_API_KEY = %q, want the key read from stdin", got)
	}
	for _, call := range h.checkCalls() {
		if !slices.Contains(call.Env, "TURING_LOCAL_ENGINE_API_KEY="+engineKeyCanary) {
			t.Errorf("%q was not given the new key to check", call.Args)
		}
	}
	h.noOutputContains(engineKeyCanary)
	h.noSecrets()
}

func TestEngineUseAtATerminalReadsTheKeyWithTypingHidden(t *testing.T) {
	h := newConfigHarness(t)
	h.IsTerminal = true
	h.Stdin = strings.NewReader("y\n")
	var prompts []string
	h.ReadSecret = func(prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return engineKeyCanary, nil
	}

	if code := h.runEngine("use", "openai", "--url", "http://localhost:8000/v1", "--model", "deepseek-chat", "--api-key-stdin"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "TURING_LOCAL_ENGINE_API_KEY") || !strings.Contains(prompts[0], "hidden") {
		t.Fatalf("prompts = %q, want one hidden prompt for the key", prompts)
	}
	if got, _ := h.envGet("TURING_LOCAL_ENGINE_API_KEY"); got != engineKeyCanary {
		t.Fatalf("TURING_LOCAL_ENGINE_API_KEY = %q", got)
	}
	h.noOutputContains(engineKeyCanary)
}

func TestEngineUseKeepsTheKeyOfAnOpenAIEngineWhenNoneIsGiven(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv("TURING_LOCAL_ENGINE=openai", "TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1", "TURING_LOCAL_ENGINE_API_KEY="+engineKeyCanary))

	if code := h.runEngine("use", "openai", "--url", "http://localhost:8000/v1", "--model", "glm", "--no-apply"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	h.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary, "TURING_LOCAL_MODEL": "glm"})
	h.noOutputContains(engineKeyCanary)
}

func TestEngineUseRefusesAKeyFromStdinItCannotUse(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"ollama", []string{"use", "ollama", "--api-key-stdin"}, engineKeyCanary + "\n", "Ollama takes no API key"},
		{"nothing piped in", []string{"use", "openai", "--url", "http://localhost:8000/v1", "--model", "m", "--api-key-stdin"}, "", "nothing was read from standard input"},
		{"an empty line", []string{"use", "openai", "--url", "http://localhost:8000/v1", "--model", "m", "--api-key-stdin"}, "\n", "the key read from standard input is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.Stdin = strings.NewReader(tt.stdin)
			before := h.envBytes()

			if code := h.run(append([]string{"engine"}, tt.args...)...); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			if got := h.errOut.String(); !strings.Contains(got, tt.want) {
				t.Fatalf("stderr = %q, want %q", got, tt.want)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
			h.unchanged(before)
			h.noOutputContains(engineKeyCanary)
		})
	}
}

func TestEngineUseNeverTakesAKeyOrAnythingElseUnknownOnTheCommandLine(t *testing.T) {
	const url, model = "http://localhost:8000/v1", "deepseek-chat"
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"use", "openai", "--url", url, "--model", model, "--api-key", "sk-CANARY-ARGUMENT"}, "--api-key-stdin"},
		{[]string{"use", "openai", "--url", url, "--model", model, "--api-key=sk-CANARY-ARGUMENT"}, "--api-key-stdin"},
		{[]string{"use", "openai", "--url", url, "--model", model, "-api-key", "sk-CANARY-ARGUMENT"}, "--api-key-stdin"},
		{[]string{"use", "openai", "--url", url, "--model", model, "--key=sk-CANARY-ARGUMENT"}, "unknown option"},
		{[]string{"use", "openai", "sk-CANARY-ARGUMENT", "--url", url, "--model", model}, "takes one engine"},
		{[]string{"use", "openai", "--url", url, "--model", model, "--", "sk-CANARY-ARGUMENT"}, "takes one engine"},
		{[]string{"use", "sk-CANARY-ARGUMENT"}, "ollama or openai"},
		{[]string{"use", "--yes"}, "ollama or openai"},
		{[]string{"use", "openai", "--url", url, "--model", model, "--yes=sk-CANARY-ARGUMENT"}, "unknown option"},
	}
	for _, tt := range tests {
		h := newConfigHarness(t)
		before := h.envBytes()

		if code := h.run(append([]string{"engine"}, tt.args...)...); code != 2 {
			t.Fatalf("%q: exit = %d, want 2", tt.args, code)
		}
		got := h.errOut.String()
		if strings.Contains(got+h.out.String(), "CANARY") || !strings.Contains(got, tt.want) || !strings.Contains(got, "./turing help") {
			t.Fatalf("%q: stderr = %q, want %q and a pointer to help, without the argument", tt.args, got, tt.want)
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("%q: ran %q", tt.args, h.runner.commands())
		}
		h.unchanged(before)
	}
}

// --- checking the engine after a save ---

func TestAnAppliedEngineChangeWhoseEngineCheckFailsKeepsTheSettingsAndSaysSo(t *testing.T) {
	h := newConfigHarness(t)
	h.runner.on(checkEngineCall(), fakeReply{stdout: "1/4 reachable: ok\n", stderr: engineRefusal, code: 1})

	if code := h.runEngine("use", "openai", "--url", "http://localhost:8000/v1", "--model", "deepseek-chat", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got, want := h.errOut.String(), engineRefusal+engineFailedAfterRun+"\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	h.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE": "openai", "TURING_LOCAL_MODEL": "deepseek-chat"})
	if n := h.countCommand("compose up -d"); n != 1 {
		t.Fatalf("up -d ran %d times, want once: there is nothing to restore once health passed", n)
	}
	if strings.Contains(h.out.String(), "Restor") {
		t.Fatalf("stdout = %q", h.out)
	}
}

func TestAnEngineChangeThatIsNotAppliedSaysToCheckTheEngineAfterStart(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"--no-apply", []string{"--no-apply"}},
		{"declined off a terminal", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newConfigHarness(t)

			args := append([]string{"use", "openai", "--url", "http://localhost:8000/v1", "--model", "deepseek-chat"}, tt.args...)
			if code := h.runEngine(args...); code != 0 {
				t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
			}
			inOrder(t, h.out.String(), savedNotApplied+"\n", engineCheckHint+"\n")
			if len(h.engineCheckCalls()) != 0 || h.ranCommand("compose up") {
				t.Fatalf("ran %q", h.runner.commands())
			}
		})
	}
}

func TestAnEngineChangeThatDoesNotComeUpHealthyIsRestoredWithoutAnEngineCheck(t *testing.T) {
	h := newConfigHarness(t)
	h.HealthTimeout = 30 * time.Millisecond
	before := h.envBytes()
	unhealthyWhile(h.harness, "TURING_LOCAL_MODEL=broken")

	if code := h.runEngine("use", "ollama", "--model", "broken", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	h.unchanged(before)
	if len(h.engineCheckCalls()) != 0 || strings.Contains(h.errOut.String(), engineFailedAfterRun) {
		t.Fatalf("ran %q; stderr %q", h.runner.commands(), h.errOut)
	}
}

func TestConfigSetOfALocalEngineSettingChecksTheEngineOnceApplied(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if got := h.runner.commands(); !slices.Equal(got, saveAndCheckEngine) {
		t.Fatalf("commands =\n%q\nwant\n%q", got, saveAndCheckEngine)
	}
}

// The check changes nothing and can take minutes, so another command may
// change .env meanwhile.
func TestTheEngineCheckAfterASaveRunsWithDotEnvUnlocked(t *testing.T) {
	h := newConfigHarness(t)
	var lockErr error
	h.runner.onFunc(checkEngineCall(), func(host.Command) fakeReply {
		unlock, err := lockDotEnv(h.harness)
		if err == nil {
			unlock()
		}
		lockErr = err
		return fakeReply{stdout: engineUsable}
	})

	if code := h.config("set", "TURING_LOCAL_MODEL", "llama3.2", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if len(h.engineCheckCalls()) != 1 || lockErr != nil {
		t.Fatalf("ran %q; locking .env during the engine check: %v", h.runner.commands(), lockErr)
	}
}

func TestConfigSetOfALocalEngineSettingWhoseEngineCheckFailsExits1(t *testing.T) {
	h := newConfigHarness(t)
	h.runner.on(checkEngineCall(), fakeReply{stderr: engineRefusal, code: 1})

	if code := h.config("set", "OLLAMA_KEEP_ALIVE", "10m", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.HasSuffix(h.errOut.String(), engineFailedAfterRun+"\n") {
		t.Fatalf("stderr = %q", h.errOut)
	}
	if got, _ := h.envGet("OLLAMA_KEEP_ALIVE"); got != "10m" {
		t.Fatalf("OLLAMA_KEEP_ALIVE = %q, want the change kept", got)
	}
}

func TestASaveThatLeavesTheLocalEngineAloneDoesNotCheckIt(t *testing.T) {
	for _, extra := range [][]string{{"--yes"}, {"--no-apply"}} {
		h := newConfigHarness(t)

		if code := h.config(append([]string{"set", "TURING_QUEUE_MAX_WAIT_MS", "60000"}, extra...)...); code != 0 {
			t.Fatalf("%q: exit = %d; stderr: %s", extra, code, h.errOut)
		}
		if len(h.engineCheckCalls()) != 0 || strings.Contains(h.out.String(), engineCheckHint) {
			t.Fatalf("%q: ran %q; stdout %q", extra, h.runner.commands(), h.out)
		}
	}
}

// Moving a deprecated name to its new one changes nothing the runtime reads,
// so it is not an engine change.
func TestASaveThatOnlyMigratesADeprecatedEngineNameDoesNotCheckTheEngine(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv("OLLAMA_MODEL=llama3"))

	if code := h.config("set", "OPENAI_MODEL", "gpt-x", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if got, _ := h.envGet("TURING_LOCAL_MODEL"); got != "llama3" {
		t.Fatalf("TURING_LOCAL_MODEL = %q, want the migrated value", got)
	}
	if len(h.engineCheckCalls()) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
}

func TestSaveSettingsLeavesTheEngineCheckToACallerThatRunsItItself(t *testing.T) {
	for _, opts := range []saveOptions{{Yes: true, SkipEngineCheck: true}, {NoApply: true, SkipEngineCheck: true}} {
		h := newConfigHarness(t)
		h.scriptSave()
		model := "llama3.2"

		if code := h.saveSettings(t.Context(), map[string]*string{"TURING_LOCAL_MODEL": &model}, opts); code != 0 {
			t.Fatalf("%+v: exit = %d; stderr: %s", opts, code, h.errOut)
		}
		if len(h.engineCheckCalls()) != 0 || strings.Contains(h.out.String(), engineCheckHint) {
			t.Fatalf("%+v: ran %q; stdout %q", opts, h.runner.commands(), h.out)
		}
	}
}

func TestEngineCheckIsWhatTheEngineCheckAfterASaveRuns(t *testing.T) {
	h := newConfigHarness(t)
	h.scriptSave()

	if code := h.engineCheck(t.Context()); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if len(h.engineCheckCalls()) != 1 {
		t.Fatalf("ran %q", h.runner.commands())
	}
}

// os/exec copies a command's two streams from two goroutines into what
// keeps its output; -race proves that is safe.
func TestTailBufferTakesBothStreamsAtOnceAndKeepsTheLastPart(t *testing.T) {
	buffer := &tailBuffer{}
	var wg sync.WaitGroup
	for _, line := range []string{"out\n", "err\n"} {
		wg.Go(func() {
			for range maxCheckOutput / 4 {
				_, _ = buffer.Write([]byte(line))
			}
		})
	}
	wg.Wait()
	_, _ = buffer.Write([]byte("last line\n"))

	got := buffer.String()
	if len(got) != maxCheckOutput || !strings.HasSuffix(got, "last line\n") {
		t.Fatalf("kept %d bytes ending %q, want %d ending with the last write", len(got), got[max(0, len(got)-20):], maxCheckOutput)
	}
}

// --- help ---

func TestHelpListsEveryEngineCommand(t *testing.T) {
	h := newHarness(t)
	h.run("help")

	out := h.out.String()
	for _, want := range []string{
		"engine detect [--url URL]",
		"engine use <ollama|openai> [--url URL] [--model M] [--context N] [--label L]",
		"[--api-key-stdin] [--yes] [--no-apply]",
		"engine check",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help does not mention %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "engine") && strings.Contains(line, "coming in a later version") {
			t.Errorf("help says an engine command is still coming: %q", line)
		}
	}
}
