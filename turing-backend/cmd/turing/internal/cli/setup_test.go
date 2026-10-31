package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/registry"
)

const (
	setupNeedsTerminal = "setup asks questions; run it in a terminal, or pass --yes with the choices as flags"
	remoteKeyCanary    = "sk-REMOTE-KEY-CANARY"
	connectLines       = "Backend URL: http://localhost:3456\nAPI key: " + testKey + "\nAnyone holding this key can use Turing.\n"
	ds4Use             = "./turing engine use openai --url http://host.docker.internal:8000/v1 --model deepseek-chat"
	noChanges          = "No changes.\n"
	initPrinted        = "TuringAgent backend initialized.\n"
	vllmModels         = `{"object":"list","data":[{"id":"qwen3-coder"}]}`

	setupKeyRemovedNotice = engineKeyRemovedNotice
	nextStartLine         = "takes effect at the next ./turing start"
)

var (
	// preflightToBuild is what setup runs before it asks anything.
	preflightToBuild = []string{"docker info --format {{.ServerVersion}}", "./scripts/init.sh", "compose build"}
	// startAndCheck is what setup runs after a save, or when there is nothing
	// to save: start, wait for health, then the engine check.
	startAndCheck = saveAndCheckEngine[7:]
)

// setupHarness is the Framework Desktop with a TuringAgent checkout and no
// .env yet, where every step of setup succeeds: Docker answers, init.sh
// provisions, the images build and know both checks, the checks pass, the
// stack comes up healthy and the engine check passes. No engine answers until
// a test serves one. A test scripts the step it is about before running setup,
// and its own step wins.
type setupHarness struct {
	*configHarness
	// fresh is .env as a first run of init.sh leaves it.
	fresh []byte
}

func newSetupHarness(t *testing.T) *setupHarness {
	t.Helper()
	c := newConfigHarness(t)
	c.onFramework()
	for _, name := range []string{"scripts/init.sh", "infra/docker-compose.yml"} {
		path := filepath.Join(c.BackendDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# the checkout's own\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	c.removeEnv()
	return &setupHarness{configHarness: c, fresh: freshDotEnv(t)}
}

// freshDotEnv is .env as a first run of init.sh leaves it: .env.example with
// every generated key filled in, and the harness's app port.
func freshDotEnv(t *testing.T) []byte {
	t.Helper()
	example, err := os.ReadFile(envExampleSource)
	if err != nil {
		t.Fatal(err)
	}
	file, err := envfile.Parse(example)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range registry.All() {
		if !key.Generated {
			continue
		}
		value := canaryGen + "-" + key.Name
		if key.Name == clientKey {
			value = testKey
		}
		if err := file.Set(key.Name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Set(portKey, itoa(testPort)); err != nil {
		t.Fatal(err)
	}
	return file.Bytes()
}

// rerun makes this a second run: .env is the fresh one with these settings
// changed since.
func (s *setupHarness) rerun(settings map[string]string) {
	s.t.Helper()
	file, err := envfile.Parse(s.fresh)
	if err != nil {
		s.t.Fatal(err)
	}
	for name, value := range settings {
		if err := file.Set(name, value); err != nil {
			s.t.Fatal(err)
		}
	}
	s.writeEnv(string(file.Bytes()))
}

// setup runs `turing setup` with init.sh writing the fresh .env when there is
// none, and leaving one that exists alone, as the real one does with its
// settings.
func (s *setupHarness) setup(args ...string) int {
	s.t.Helper()
	s.runner.onFunc(func(cmd host.Command) bool { return cmd.Name == "./scripts/init.sh" }, func(host.Command) fakeReply {
		path := filepath.Join(s.BackendDir, ".env")
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			if err := os.WriteFile(path, s.fresh, 0o600); err != nil {
				s.t.Error(err)
			}
		}
		return fakeReply{stdout: initPrinted}
	})
	s.runner.on(composeCall("build"), fakeReply{stdout: "Building turing-orchestrator\n"})
	s.scriptSave()
	return s.run(append([]string{"setup"}, args...)...)
}

// noDotEnv fails the test when .env exists.
func (s *setupHarness) noDotEnv() {
	s.t.Helper()
	if _, err := os.Lstat(filepath.Join(s.BackendDir, ".env")); !errors.Is(err, fs.ErrNotExist) {
		s.t.Fatalf(".env exists: %v", err)
	}
}

// generatedUnchanged fails the test when a generated key in .env is not what
// init.sh wrote.
func (s *setupHarness) generatedUnchanged() {
	s.t.Helper()
	fresh, err := envfile.Parse(s.fresh)
	if err != nil {
		s.t.Fatal(err)
	}
	for _, key := range registry.All() {
		if !key.Generated {
			continue
		}
		want, _ := fresh.Get(key.Name)
		if got, _ := s.envGet(key.Name); got != want {
			s.t.Errorf("%s = %q, want %q as init.sh wrote it", key.Name, got, want)
		}
	}
}

func (s *setupHarness) wantCommands(want ...[]string) {
	s.t.Helper()
	if got, all := s.runner.commands(), slices.Concat(want...); !slices.Equal(got, all) {
		s.t.Fatalf("commands =\n%q\nwant\n%q", got, all)
	}
}

func (s *setupHarness) noSecretAskedFor() {
	s.ReadSecret = func(prompt string) (string, error) {
		s.t.Errorf("asked for a secret: %q", prompt)
		return "", os.ErrInvalid
	}
}

// checkEnvHas fails the test unless every check-config run was given these
// values.
func (s *setupHarness) checkEnvHas(entries ...string) {
	s.t.Helper()
	calls := s.checkCalls()
	if len(calls) != 2 {
		s.t.Fatalf("check-config ran %d times, want 2", len(calls))
	}
	for _, call := range calls {
		for _, entry := range entries {
			if !slices.Contains(call.Env, entry) {
				s.t.Errorf("check-config env %q has no %q", call.Env, entry)
			}
		}
	}
}

// --- a first run ---

func TestSetupFirstRunAtATerminalSavesTheChosenEngineStartsTuringAndShowsHowToConnect(t *testing.T) {
	s := newSetupHarness(t)
	s.engine.serve(bridgeGateway+":8000", 200, openAIModels)
	s.IsTerminal = true
	s.Stdin = answers("1", "1", "n", "n", "y")
	s.noSecretAskedFor()
	var modelWhenStarted string
	s.runner.onFunc(composeCall("up", "-d"), func(host.Command) fakeReply {
		modelWhenStarted, _ = s.envGet("TURING_LOCAL_MODEL")
		return fakeReply{}
	})

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantCommands(preflightToBuild, saveAndCheckEngine)
	if modelWhenStarted != "deepseek-chat" {
		t.Fatalf("TURING_LOCAL_MODEL was %q when Turing started; want the save written first", modelWhenStarted)
	}
	s.wantEnv(t, map[string]string{
		"TURING_LOCAL_ENGINE":         "openai",
		"TURING_LOCAL_ENGINE_URL":     "http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL":          "deepseek-chat",
		"TURING_LOCAL_ENGINE_LABEL":   "ds4",
		"OPENAI_API_KEY":              "",
		"TURING_AGENT_TEAM_ENABLED":   "false",
		"TURING_LOCAL_ENGINE_API_KEY": "",
	})
	s.checkEnvHas("TURING_LOCAL_ENGINE=openai", "TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL=deepseek-chat", "TURING_LOCAL_ENGINE_LABEL=ds4")
	s.generatedUnchanged()

	out := s.out.String()
	inOrder(t, out,
		"Provisioning", initPrinted,
		"Building the images",
		"This machine: Linux, AMD RYZEN AI MAX+ 395 w/ Radeon 8060S, 127 GB",
		"Recommended: ds4 (ROCm)",
		"Local engine:\n",
		"  1) ds4 at http://host.docker.internal:8000/v1  (recommended)\n",
		"  2) Ollama with qwen2.5:7b\n",
		"  3) Another address\n",
		"Choose [1]: ",
		"Model:\n",
		"  1) deepseek-chat  (recommended)\n",
		"  2) deepseek-reasoner\n",
		"  3) Another model name\n",
		"Choose [1]: ",
		"Add a remote model? Each send to it asks you first. [y/N] ",
		"Turn on the agent team (experimental)? [y/N] ",
		"Settings to save:\n",
		"  TURING_LOCAL_ENGINE=openai\n",
		"  TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1\n",
		"  TURING_LOCAL_MODEL=deepseek-chat\n",
		"  TURING_LOCAL_ENGINE_LABEL=ds4\n",
		"Save these settings? [Y/n] ",
		"Saved in .env.\n",
		"Starting Turing",
		engineUsable,
		connectLines,
	)
	if strings.Contains(out, engineCheckHint) || strings.Contains(out, "Apply now?") || strings.Contains(out, nextStartLine) {
		t.Fatalf("stdout = %q; want no engine check left for later, no question about applying and no start left for later, since setup starts Turing and checks the engine itself", out)
	}
	if n := len(s.engineCheckCalls()); n != 1 {
		t.Fatalf("the engine was checked %d times, want once", n)
	}
	if s.errOut.Len() != 0 {
		t.Fatalf("stderr = %q", s.errOut)
	}
	s.noOutputContains(canaryGen)
}

func TestSetupFirstRunWithYesTakesTheRecommendationWhenItIsTheOneEngineAnswering(t *testing.T) {
	s := newSetupHarness(t)
	s.engine.serve(bridgeGateway+":8000", 200, openAIModels)

	if code := s.setup("--yes"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantCommands(preflightToBuild, saveAndCheckEngine)
	s.wantEnv(t, map[string]string{
		"TURING_LOCAL_ENGINE":       "openai",
		"TURING_LOCAL_ENGINE_URL":   "http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL":        "deepseek-chat",
		"TURING_LOCAL_ENGINE_LABEL": "ds4",
	})
	s.generatedUnchanged()
	out := s.out.String()
	inOrder(t, out, "Settings to save:\n", "  TURING_LOCAL_MODEL=deepseek-chat\n", engineUsable, connectLines)
	for _, unwanted := range []string{"Choose", "[y/N]", "Save these settings?", ds4Use, nextStartLine} {
		if strings.Contains(out, unwanted) {
			t.Errorf("stdout has %q:\n%s", unwanted, out)
		}
	}
}

func TestSetupFirstRunWithYesKeepsTheOllamaDefaultsUnlessTheRecommendationIsTheOneEngineAnswering(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(s *setupHarness)
		hint    string
	}{
		{"two engines answer", func(s *setupHarness) {
			s.engine.serve(bridgeGateway+":11434", 200, ollamaTags)
			s.engine.serve(bridgeGateway+":8000", 200, openAIModels)
		}, ds4Use},
		{"no engine answers", func(*setupHarness) {}, ds4Use},
		{"the one engine is not the recommended one", func(s *setupHarness) {
			s.engine.serve(bridgeGateway+":11434", 200, ollamaTags)
		}, ds4Use},
		{"the recommended engine answers only where the containers cannot reach it", func(s *setupHarness) {
			s.engine.serve(loopback+":8000", 200, openAIModels)
		}, "./turing engine use openai"},
		{"the recommendation is Ollama", func(s *setupHarness) {
			s.onMacMini()
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSetupHarness(t)
			tt.arrange(s)

			if code := s.setup("--yes"); code != 0 {
				t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
			}
			s.wantCommands(preflightToBuild, startAndCheck)
			if got := s.envBytes(); string(got) != string(s.fresh) {
				t.Fatalf(".env changed:\n%s", got)
			}
			out := s.out.String()
			inOrder(t, out, noChanges, engineUsable, connectLines)
			if tt.hint == "" {
				if strings.Contains(out, "./turing engine use") {
					t.Fatalf("stdout = %q, want no hint when Ollama is the recommendation", out)
				}
				return
			}
			inOrder(t, out, "Keeping Ollama with qwen2.5:7b", tt.hint, noChanges)
		})
	}
}

// --yes never guesses a model: an engine that lists several, none of them the
// one Turing prefers, leaves the Ollama defaults in place, with the command
// that switches.
func TestSetupFirstRunWithYesKeepsTheOllamaDefaultsWhenTheRecommendedEngineListsSeveralModelsButNotThePreferredOne(t *testing.T) {
	s := newSetupHarness(t)
	s.engine.serve(bridgeGateway+":8000", 200, `{"object":"list","data":[{"id":"glm-5.3-q2"},{"id":"qwen3-coder"}]}`)

	if code := s.setup("--yes"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	if got := s.envBytes(); string(got) != string(s.fresh) {
		t.Fatalf(".env changed:\n%s", got)
	}
	inOrder(t, s.out.String(),
		"Keeping Ollama with qwen2.5:7b",
		"To use ds4 instead, run: ./turing engine use openai --url http://host.docker.internal:8000/v1 --model <model>\n",
		noChanges, engineUsable, connectLines)
}

// An engine that lists one model leaves nothing to guess: --yes takes the
// recommended engine with that model, even when it is not the one Turing
// prefers.
func TestSetupFirstRunWithYesTakesTheRecommendedEngineWithTheOnlyModelItLists(t *testing.T) {
	s := newSetupHarness(t)
	s.engine.serve(bridgeGateway+":8000", 200, `{"object":"list","data":[{"id":"glm-5.3-q2"}]}`)

	if code := s.setup("--yes"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{
		"TURING_LOCAL_ENGINE":       "openai",
		"TURING_LOCAL_ENGINE_URL":   "http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL":        "glm-5.3-q2",
		"TURING_LOCAL_ENGINE_LABEL": "ds4",
	})
	out := s.out.String()
	inOrder(t, out, "Settings to save:\n", "  TURING_LOCAL_MODEL=glm-5.3-q2\n", engineUsable, connectLines)
	if strings.Contains(out, "Keeping Ollama") {
		t.Fatalf("stdout = %q", out)
	}
}

// --- a rerun ---

func TestSetupRerunWithYesKeepsEveryCurrentChoiceAndWritesNothing(t *testing.T) {
	s := newSetupHarness(t)
	s.onMacMini()
	s.engine.serve(loopback+":11434", 200, ollamaTags)
	s.engine.serve(loopback+":8000", 200, openAIModels)
	s.rerun(map[string]string{
		"TURING_LOCAL_ENGINE":         "openai",
		"TURING_LOCAL_ENGINE_URL":     "http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL":          "deepseek-reasoner",
		"TURING_LOCAL_ENGINE_LABEL":   "My ds4",
		"TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary,
		"OPENAI_API_KEY":              remoteKeyCanary,
		"TURING_AGENT_TEAM_ENABLED":   "true",
	})
	before := s.envBytes()

	if code := s.setup("--yes"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantCommands(preflightToBuild, startAndCheck)
	s.unchanged(before)
	out := s.out.String()
	inOrder(t, out, "Recommended: Ollama", noChanges, engineUsable, connectLines)
	if strings.Contains(out, "Keeping") || strings.Contains(out, "./turing engine use") {
		t.Fatalf("stdout = %q; a rerun keeps the current choice without a hint", out)
	}
	s.noOutputContains(engineKeyCanary, remoteKeyCanary, canaryGen)
}

func TestSetupWithEngineFlagsReplacesTheEngineAndRemovesTheKeyOfAnotherServer(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{
		"TURING_LOCAL_ENGINE":         "openai",
		"TURING_LOCAL_ENGINE_URL":     "http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL":          "deepseek-chat",
		"TURING_LOCAL_ENGINE_LABEL":   "ds4",
		"TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary,
	})

	if code := s.setup("--yes", "--engine", "ollama"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{
		"TURING_LOCAL_ENGINE":         "ollama",
		"TURING_LOCAL_ENGINE_URL":     "http://host.docker.internal:11434",
		"TURING_LOCAL_MODEL":          "qwen2.5:7b",
		"TURING_LOCAL_ENGINE_LABEL":   "",
		"TURING_LOCAL_ENGINE_API_KEY": "",
	})
	out := s.out.String()
	inOrder(t, out, "  TURING_LOCAL_ENGINE_LABEL: cleared\n", "  TURING_LOCAL_ENGINE_API_KEY: removed\n", engineUsable)
	// Ollama takes no key, so there is no key to give it.
	if strings.Contains(out, keyRemovedWords) {
		t.Fatalf("stdout = %q; want no notice about giving Ollama a key", out)
	}
	if strings.Contains(out+s.errOut.String(), "--api-key-stdin") {
		t.Fatalf("stdout = %q; setup has no --api-key-stdin, so it must not name one", out)
	}
	s.generatedUnchanged()
	s.noOutputContains(engineKeyCanary, canaryGen)
}

func TestSetupWithEngineFlagsForTheSameServerKeepsItsKey(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{
		"TURING_LOCAL_ENGINE":         "openai",
		"TURING_LOCAL_ENGINE_URL":     "http://localhost:8000/v1",
		"TURING_LOCAL_MODEL":          "deepseek-reasoner",
		"TURING_LOCAL_ENGINE_LABEL":   "ds4",
		"TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary,
	})

	if code := s.setup("--yes", "--engine", "openai", "--url", "http://127.0.0.1:8000/v1", "--model", "deepseek-chat"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{
		"TURING_LOCAL_ENGINE_URL":     "http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL":          "deepseek-chat",
		"TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary,
	})
	for _, call := range s.checkCalls() {
		for _, entry := range call.Env {
			if strings.HasPrefix(entry, engineAPIKeyKey+"=") {
				t.Fatalf("check-config was given %s as a change", engineAPIKeyKey)
			}
		}
	}
	if strings.Contains(s.out.String(), "API key was removed") {
		t.Fatalf("stdout = %q", s.out)
	}
	s.noOutputContains(engineKeyCanary)
}

func TestSetupChoosingOllamaClearsAnEngineKeyEvenWhenOllamaIsCurrent(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{"TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary})

	if code := s.setup("--yes", "--engine", "ollama"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE": "ollama", "TURING_LOCAL_ENGINE_API_KEY": ""})
	inOrder(t, s.out.String(), "Settings to save:\n", "  TURING_LOCAL_ENGINE_API_KEY: removed\n", engineUsable)
	if strings.Contains(s.out.String(), keyRemovedWords) {
		t.Fatalf("stdout = %q; want no notice about giving Ollama a key", s.out)
	}
	s.noOutputContains(engineKeyCanary)
}

func TestSetupChoosingAnotherOpenAIAddressClearsTheEngineKey(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{
		"TURING_LOCAL_ENGINE":         "openai",
		"TURING_LOCAL_ENGINE_URL":     "http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL":          "deepseek-chat",
		"TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary,
	})

	if code := s.setup("--yes", "--engine", "openai", "--url", "http://localhost:8001/v1", "--model", "deepseek-chat"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE_URL": "http://host.docker.internal:8001/v1", "TURING_LOCAL_ENGINE_API_KEY": ""})
	inOrder(t, s.out.String(), "  TURING_LOCAL_ENGINE_API_KEY: removed\n", setupKeyRemovedNotice)
	s.noOutputContains(engineKeyCanary)
}

// Keeping the current engine keeps its address, unless .env names it as
// localhost, which inside the runtime's container is the container itself: the
// address shown is the one saved. The key stays, since it is the same server.
func TestSetupAtATerminalKeepingTheCurrentOpenAIEngineSavesTheAddressTheContainersReachAndKeepsItsKey(t *testing.T) {
	s := newSetupHarness(t)
	s.engine.serve(bridgeGateway+":8000", 200, openAIModels)
	s.rerun(map[string]string{
		"TURING_LOCAL_ENGINE":         "openai",
		"TURING_LOCAL_ENGINE_URL":     "http://localhost:8000/v1",
		"TURING_LOCAL_MODEL":          "deepseek-chat",
		"TURING_LOCAL_ENGINE_LABEL":   "ds4",
		"TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary,
	})
	s.IsTerminal = true
	s.Stdin = answers("", "", "", "", "")

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{
		"TURING_LOCAL_ENGINE_URL":     "http://host.docker.internal:8000/v1",
		"TURING_LOCAL_MODEL":          "deepseek-chat",
		"TURING_LOCAL_ENGINE_LABEL":   "ds4",
		"TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary,
	})
	out := s.out.String()
	inOrder(t, out, "  1) ds4 at http://host.docker.internal:8000/v1  (recommended, current)\n",
		"Settings to save:\n", "  TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1\n", "Save these settings? [Y/n] ")
	if strings.Contains(out, "TURING_LOCAL_ENGINE_API_KEY") || strings.Contains(out, keyRemovedWords) {
		t.Fatalf("stdout = %q; the key is for this same server", out)
	}
	s.noCheckGets(engineAPIKeyKey)
	s.noOutputContains(engineKeyCanary)
}

func TestSetupAtATerminalKeepingTheCurrentEngineKeepsAnAddressTheContainersReachAsWritten(t *testing.T) {
	s := newSetupHarness(t)
	s.engine.serve(bridgeGateway+":8000", 200, openAIModels)
	s.rerun(map[string]string{
		"TURING_LOCAL_ENGINE":       "openai",
		"TURING_LOCAL_ENGINE_URL":   "http://host.docker.internal:8000/v1/",
		"TURING_LOCAL_MODEL":        "deepseek-chat",
		"TURING_LOCAL_ENGINE_LABEL": "ds4",
	})
	before := s.envBytes()
	s.IsTerminal = true
	s.Stdin = answers("", "", "", "")

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.unchanged(before)
	inOrder(t, s.out.String(), "  1) ds4 at http://host.docker.internal:8000/v1  (recommended, current)\n", noChanges)
}

func TestSetupAtATerminalKeepingACurrentEngineThatDidNotAnswerSavesTheAddressTheContainersReach(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{
		"TURING_LOCAL_ENGINE":     "openai",
		"TURING_LOCAL_ENGINE_URL": "http://127.0.0.1:8001/v1",
		"TURING_LOCAL_MODEL":      "qwen3-coder",
	})
	s.IsTerminal = true
	s.Stdin = answers("", "", "", "")

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE_URL": "http://host.docker.internal:8001/v1", "TURING_LOCAL_MODEL": "qwen3-coder"})
	inOrder(t, s.out.String(),
		"  2) OpenAI-compatible at http://host.docker.internal:8001/v1 with qwen3-coder  (current; it did not answer)\n",
		"Choose [2]: ",
		"  TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8001/v1\n",
	)
}

// --yes on a rerun keeps every current choice, so an address naming the
// container is not rewritten; the command that does it is named instead.
func TestSetupRerunWithYesWarnsAboutAnEngineAddressThatNamesTheContainerAndChangesNothing(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]string
		command  string
	}{
		{"openai", map[string]string{
			"TURING_LOCAL_ENGINE": "openai", "TURING_LOCAL_ENGINE_URL": "http://localhost:8000/v1", "TURING_LOCAL_MODEL": "deepseek-chat",
			"TURING_LOCAL_ENGINE_LABEL": "ds4", "TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary,
		}, "./turing engine use openai --url http://host.docker.internal:8000/v1 --model deepseek-chat"},
		{"ollama", map[string]string{"TURING_LOCAL_ENGINE": "ollama", "TURING_LOCAL_ENGINE_URL": "http://127.0.0.1:11434", "TURING_LOCAL_MODEL": "llama3:latest"},
			"./turing engine use ollama --url http://host.docker.internal:11434 --model llama3:latest"},
		{"a label of its own", map[string]string{
			"TURING_LOCAL_ENGINE": "openai", "TURING_LOCAL_ENGINE_URL": "http://localhost:1234/v1", "TURING_LOCAL_MODEL": "qwen3-coder",
			"TURING_LOCAL_ENGINE_LABEL": "LM Studio's",
		}, `./turing engine use openai --url http://host.docker.internal:1234/v1 --model qwen3-coder --label 'LM Studio'\''s'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSetupHarness(t)
			s.rerun(tt.settings)
			before := s.envBytes()

			if code := s.setup("--yes"); code != 0 {
				t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
			}
			s.unchanged(before)
			out := s.out.String()
			var warning string
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "Warning:") {
					warning = line
				}
			}
			if !strings.Contains(warning, "the container itself") || !strings.HasSuffix(warning, "run "+tt.command+".") || strings.Count(warning, "./turing ") != 1 {
				t.Fatalf("warning = %q, want one line naming only %q", warning, tt.command)
			}
			inOrder(t, out, warning, noChanges)
			s.noOutputContains(engineKeyCanary)
		})
	}
}

func TestSetupDecidesTheEngineKeyAgainstDotEnvAsItIsWhenSaving(t *testing.T) {
	s := newSetupHarness(t)
	s.engine.serve(bridgeGateway+":8000", 200, openAIModels)
	s.rerun(nil)
	s.IsTerminal = true
	// While the save question waits, another command gives the current
	// engine, Ollama, a key; ds4 is another server, so the save removes it.
	answered := strings.NewReader("1\n1\nn\nn\n")
	last := strings.NewReader("y\n")
	keyAdded := false
	s.Stdin = readerFunc(func(b []byte) (int, error) {
		if answered.Len() > 0 {
			return answered.Read(b)
		}
		if !keyAdded {
			keyAdded = true
			s.rerun(map[string]string{"TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary})
		}
		return last.Read(b)
	})

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE": "openai", "TURING_LOCAL_ENGINE_API_KEY": ""})
	inOrder(t, s.out.String(), "Save these settings? [Y/n] ", setupKeyRemovedNotice)
	s.noOutputContains(engineKeyCanary)
}

func TestSetupAtATerminalOnARerunOffersTheCurrentChoicesAndEnterKeepsThem(t *testing.T) {
	s := newSetupHarness(t)
	s.onMacMini()
	s.engine.serve(loopback+":11434", 200, ollamaTags)
	s.engine.serve(loopback+":8000", 200, openAIModels)
	s.rerun(map[string]string{"TURING_LOCAL_MODEL": "llama3:latest", "TURING_AGENT_TEAM_ENABLED": "true"})
	before := s.envBytes()
	s.IsTerminal = true
	s.Stdin = answers("", "", "", "")
	s.noSecretAskedFor()

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.unchanged(before)
	s.wantCommands(preflightToBuild, startAndCheck)
	inOrder(t, s.out.String(),
		"  1) Ollama at http://host.docker.internal:11434  (recommended, current)\n",
		"  2) ds4 at http://host.docker.internal:8000/v1\n",
		"  3) Ollama with qwen2.5:7b\n",
		"  4) Another address\n",
		"Choose [1]: ",
		"  1) qwen2.5:7b  (recommended)\n",
		"  2) llama3:latest  (current)\n",
		"  3) phi3:mini\n",
		"  4) Another model name\n",
		"Choose [2]: ",
		"Add a remote model? Each send to it asks you first. [y/N] ",
		"Turn on the agent team (experimental)? [Y/n] ",
		noChanges,
		connectLines,
	)
}

func TestSetupAtATerminalKeepsAnEngineThatIsNotAnsweringAsTheCurrentChoice(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{
		"TURING_LOCAL_ENGINE":     "openai",
		"TURING_LOCAL_ENGINE_URL": "http://host.docker.internal:8001/v1",
		"TURING_LOCAL_MODEL":      "qwen3-coder",
	})
	before := s.envBytes()
	s.IsTerminal = true
	s.Stdin = answers("", "", "")

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.unchanged(before)
	inOrder(t, s.out.String(),
		"  1) Ollama with qwen2.5:7b\n",
		"  2) OpenAI-compatible at http://host.docker.internal:8001/v1 with qwen3-coder  (current; it did not answer)\n",
		"  3) Another address\n",
		"Choose [2]: ",
		noChanges,
	)
	if strings.Contains(s.out.String(), "Model:") {
		t.Fatalf("stdout = %q; keeping the current engine keeps its model without asking", s.out)
	}
}

func TestSetupAtATerminalOffersNoCurrentChoiceWhenTheRuntimeWouldRefuseIt(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{"TURING_LOCAL_ENGINE": "llama"})
	s.IsTerminal = true
	s.Stdin = answers("", "", "", "y")

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE": "ollama"})
	out := s.out.String()
	inOrder(t, out, "  1) Ollama with qwen2.5:7b\n", "  2) Another address\n", "Choose [1]: ", "  TURING_LOCAL_ENGINE=ollama\n")
	if strings.Contains(out, "current") {
		t.Fatalf("stdout = %q; an engine the runtime refuses is not offered as the current choice", out)
	}
}

func TestSetupAtATerminalAsksForAnotherAddressAndItsModels(t *testing.T) {
	s := newSetupHarness(t)
	s.engine.serve(bridgeGateway+":8000", 200, openAIModels)
	s.engine.serve(bridgeGateway+":8001", 200, vllmModels)
	s.IsTerminal = true
	s.Stdin = answers("3", "http://192.168.1.20:8001/v1", "http://localhost:8001/v1", "", "n", "n", "y")

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{
		"TURING_LOCAL_ENGINE":       "openai",
		"TURING_LOCAL_ENGINE_URL":   "http://host.docker.internal:8001/v1",
		"TURING_LOCAL_MODEL":        "qwen3-coder",
		"TURING_LOCAL_ENGINE_LABEL": "",
	})
	inOrder(t, s.out.String(),
		"  3) Another address\n",
		"Engine address (such as http://localhost:8000/v1): ",
		"the runtime takes only an http or https URL on this machine",
		"Engine address (such as http://localhost:8000/v1): ",
		"  1) qwen3-coder\n",
		"  2) Another model name\n",
		"Choose [1]: ",
	)
	s.noOutputContains("192.168.1.20")
}

func TestSetupAtATerminalOffersTheMenuAgainWhenNothingAnswersAtAnotherAddress(t *testing.T) {
	s := newSetupHarness(t)
	s.IsTerminal = true
	s.Stdin = answers("2", "http://localhost:8009/v1", "1", "n", "n")

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	if got := s.envBytes(); string(got) != string(s.fresh) {
		t.Fatalf(".env changed:\n%s", got)
	}
	inOrder(t, s.out.String(),
		"  2) Another address\n",
		"Nothing answered at http://host.docker.internal:8009/v1; start the engine, then choose it again.\n",
		"Local engine:\n",
		"  1) Ollama with qwen2.5:7b\n",
		noChanges,
	)
}

func TestSetupWithEngineFlagsTakesTheLabelGiven(t *testing.T) {
	s := newSetupHarness(t)

	if code := s.setup("--yes", "--engine", "openai", "--url", "http://127.0.0.1:8001/v1", "--model", "qwen3-coder", "--label", "My server"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{
		"TURING_LOCAL_ENGINE":       "openai",
		"TURING_LOCAL_ENGINE_URL":   "http://host.docker.internal:8001/v1",
		"TURING_LOCAL_MODEL":        "qwen3-coder",
		"TURING_LOCAL_ENGINE_LABEL": "My server",
	})
	inOrder(t, s.out.String(), "  TURING_LOCAL_ENGINE_LABEL=My server\n")
}

func TestSetupNeverShowsAnAddressThatCouldCarryACredential(t *testing.T) {
	s := newSetupHarness(t)

	if code := s.setup("--yes", "--remote-url", "https://user:sk-CANARY-URL@api.example.com/v1"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	inOrder(t, s.out.String(), "  OPENAI_BASE_URL: set (not shown, because it has a user, password, query or fragment)\n")
	s.noOutputContains("sk-CANARY-URL")
}

func TestSetupAtATerminalTakesAModelNameTheEngineDoesNotList(t *testing.T) {
	s := newSetupHarness(t)
	s.engine.serve(bridgeGateway+":11434", 200, ollamaTags)
	s.IsTerminal = true
	s.Stdin = answers("1", "4", "", "llama3.2:3b", "n", "n", "y")

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE": "ollama", "TURING_LOCAL_MODEL": "llama3.2:3b"})
	inOrder(t, s.out.String(), "  4) Another model name\n", "Choose [1]: ", "Model name: ", "Model name: ", "  TURING_LOCAL_MODEL=llama3.2:3b\n")
}

func TestSetupAtATerminalWhoseAnswersEndSavesNothing(t *testing.T) {
	for _, input := range []string{"", "1\n", "1\n1\n", "1\n1\ny\n"} {
		s := newSetupHarness(t)
		s.engine.serve(bridgeGateway+":8000", 200, openAIModels)
		s.IsTerminal = true
		s.Stdin = strings.NewReader(input)
		s.ReadSecret = func(string) (string, error) { return "", errors.New("EOF") }

		if code := s.setup(); code != 1 {
			t.Fatalf("input %q: exit = %d, want 1", input, code)
		}
		if got := s.envBytes(); string(got) != string(s.fresh) {
			t.Fatalf("input %q: .env changed", input)
		}
		if s.ranCommand("compose run") || s.ranCommand("compose up") {
			t.Fatalf("input %q: ran %q", input, s.runner.commands())
		}
		if !strings.Contains(s.errOut.String(), "./turing setup") {
			t.Fatalf("input %q: stderr = %q", input, s.errOut)
		}
	}
}

func TestSetupThatIsNotConfirmedSavesNothingAndDoesNotStart(t *testing.T) {
	s := newSetupHarness(t)
	s.engine.serve(bridgeGateway+":8000", 200, openAIModels)
	s.IsTerminal = true
	s.Stdin = answers("1", "1", "n", "n", "n")

	if code := s.setup(); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := s.envBytes(); string(got) != string(s.fresh) {
		t.Fatalf(".env changed")
	}
	s.wantCommands(preflightToBuild)
	if got := s.errOut.String(); !strings.Contains(got, "nothing was saved") || !strings.Contains(got, "./turing setup") {
		t.Fatalf("stderr = %q", got)
	}
}

// --- the remote model ---

func TestSetupReadsTheRemoteKeyFromStdinAndNeverPrintsIt(t *testing.T) {
	s := newSetupHarness(t)
	s.Stdin = strings.NewReader(remoteKeyCanary + "\n")

	if code := s.setup("--yes", "--remote-key-stdin", "--remote-model", "gpt-5-mini"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"OPENAI_API_KEY": remoteKeyCanary, "OPENAI_MODEL": "gpt-5-mini", "OPENAI_BASE_URL": "https://api.openai.com/v1"})
	s.checkEnvHas("OPENAI_API_KEY="+remoteKeyCanary, "OPENAI_MODEL=gpt-5-mini")
	inOrder(t, s.out.String(), "Settings to save:\n", "  OPENAI_API_KEY: set\n", "  OPENAI_MODEL=gpt-5-mini\n", connectLines)
	s.noOutputContains(remoteKeyCanary)
}

func TestSetupAtATerminalReadsTheRemoteKeyWithTypingHidden(t *testing.T) {
	s := newSetupHarness(t)
	s.IsTerminal = true
	s.Stdin = answers("", "y", "https://api.example.com/v1", "", "", "y")
	var prompts []string
	s.ReadSecret = func(prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return remoteKeyCanary, nil
	}

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"OPENAI_API_KEY": remoteKeyCanary, "OPENAI_BASE_URL": "https://api.example.com/v1", "OPENAI_MODEL": "gpt-4o-mini"})
	if len(prompts) != 1 || !strings.Contains(prompts[0], "hidden") {
		t.Fatalf("prompts = %q, want one hidden prompt for the key", prompts)
	}
	inOrder(t, s.out.String(), "Remote base URL [https://api.openai.com/v1]: ", "Remote model [gpt-4o-mini]: ", "  OPENAI_API_KEY: set\n")
	s.noOutputContains(remoteKeyCanary)
}

func TestSetupAtATerminalKeepsTheSavedRemoteKeyOnEnter(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{"OPENAI_API_KEY": remoteKeyCanary})
	before := s.envBytes()
	s.IsTerminal = true
	s.Stdin = answers("", "", "", "", "")
	var prompts []string
	s.ReadSecret = func(prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "", nil
	}

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.unchanged(before)
	if len(prompts) != 1 || !strings.Contains(prompts[0], "Enter keeps") {
		t.Fatalf("prompts = %q, want one that says Enter keeps the saved key", prompts)
	}
	inOrder(t, s.out.String(), "Add a remote model? Each send to it asks you first. [Y/n] ", noChanges)
	s.noOutputContains(remoteKeyCanary)
}

func TestSetupAtATerminalSayingNoToTheRemoteModelOnARerunRemovesItsKey(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{"OPENAI_API_KEY": remoteKeyCanary})
	s.IsTerminal = true
	s.Stdin = answers("", "n", "", "y")
	s.noSecretAskedFor()

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"OPENAI_API_KEY": ""})
	inOrder(t, s.out.String(), "Settings to save:\n", "  OPENAI_API_KEY: removed\n", "Save these settings? [Y/n] ")
	s.noOutputContains(remoteKeyCanary)
}

func TestSetupWithAnotherRemoteAddressRemovesTheSavedRemoteKey(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{"OPENAI_API_KEY": remoteKeyCanary})

	if code := s.setup("--yes", "--remote-url", "https://api.example.com/v1"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"OPENAI_BASE_URL": "https://api.example.com/v1", "OPENAI_API_KEY": ""})
	s.checkEnvHas("OPENAI_BASE_URL=https://api.example.com/v1", "OPENAI_API_KEY=")
	out := s.out.String()
	inOrder(t, out, "Settings to save:\n", "  OPENAI_BASE_URL=https://api.example.com/v1\n", "  OPENAI_API_KEY: removed\n", remoteKeyRemovedNotice, connectLines)
	if n := strings.Count(out, remoteKeyRemovedNotice); n != 1 {
		t.Fatalf("the key notice was printed %d times, want once:\n%s", n, out)
	}
	s.noOutputContains(remoteKeyCanary)
}

func TestSetupWithTheSameRemoteAddressWrittenAnotherWayKeepsTheRemoteKey(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{"OPENAI_API_KEY": remoteKeyCanary})

	if code := s.setup("--yes", "--remote-url", "https://API.openai.com:443/v1/"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"OPENAI_BASE_URL": "https://API.openai.com:443/v1/", "OPENAI_API_KEY": remoteKeyCanary})
	s.noCheckGets("OPENAI_API_KEY")
	if out := s.out.String(); strings.Contains(out, "OPENAI_API_KEY") || strings.Contains(out, keyRemovedWords) {
		t.Fatalf("stdout = %q", out)
	}
	s.noOutputContains(remoteKeyCanary)
}

func TestSetupWithAnotherRemoteAddressAndAKeyFromStdinReplacesTheKey(t *testing.T) {
	const newKey = "sk-NEW-REMOTE-CANARY"
	s := newSetupHarness(t)
	s.rerun(map[string]string{"OPENAI_API_KEY": remoteKeyCanary})
	s.Stdin = strings.NewReader(newKey + "\n")

	if code := s.setup("--yes", "--remote-url", "https://api.example.com/v1", "--remote-key-stdin"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"OPENAI_BASE_URL": "https://api.example.com/v1", "OPENAI_API_KEY": newKey})
	inOrder(t, s.out.String(), "  OPENAI_API_KEY: set\n")
	if strings.Contains(s.out.String(), keyRemovedWords) {
		t.Fatalf("stdout = %q", s.out)
	}
	s.noOutputContains(remoteKeyCanary, newKey)
}

func TestSetupAtATerminalWithAnotherRemoteAddressDoesNotKeepTheSavedKeyOnEnter(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{"OPENAI_API_KEY": remoteKeyCanary})
	s.IsTerminal = true
	s.Stdin = answers("", "", "https://api.example.com/v1", "", "", "y")
	var prompts []string
	s.ReadSecret = func(prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "", nil
	}

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"OPENAI_BASE_URL": "https://api.example.com/v1", "OPENAI_API_KEY": ""})
	if len(prompts) != 1 || strings.Contains(prompts[0], "Enter keeps") || !strings.Contains(prompts[0], "new address") {
		t.Fatalf("prompts = %q, want one that asks for a key for the new address and does not offer to keep the saved one", prompts)
	}
	out := s.out.String()
	inOrder(t, out, "Remote base URL [https://api.openai.com/v1]: ",
		"No key was given for the new address, so the remote model is off; to turn it on, run ./turing config set OPENAI_API_KEY.\n",
		"  OPENAI_API_KEY: removed\n", "Save these settings? [Y/n] ")
	if strings.Contains(out, remoteKeyRemovedNotice) {
		t.Fatalf("stdout = %q; the removal was already said", out)
	}
	s.noOutputContains(remoteKeyCanary)
}

func TestSetupAtATerminalWithTheSameRemoteAddressKeepsTheSavedKeyOnEnter(t *testing.T) {
	s := newSetupHarness(t)
	s.rerun(map[string]string{"OPENAI_API_KEY": remoteKeyCanary})
	s.IsTerminal = true
	s.Stdin = answers("", "", "https://API.openai.com/v1/", "", "", "y")
	var prompts []string
	s.ReadSecret = func(prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "", nil
	}

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"OPENAI_BASE_URL": "https://API.openai.com/v1/", "OPENAI_API_KEY": remoteKeyCanary})
	if len(prompts) != 1 || !strings.Contains(prompts[0], "Enter keeps") {
		t.Fatalf("prompts = %q, want one that says Enter keeps the saved key", prompts)
	}
	s.noOutputContains(remoteKeyCanary)
}

func TestSetupRefusesARemoteKeyItCannotRead(t *testing.T) {
	for name, stdin := range map[string]string{"nothing piped in": "", "an empty line": "\n"} {
		t.Run(name, func(t *testing.T) {
			s := newSetupHarness(t)
			s.Stdin = strings.NewReader(stdin)

			if code := s.setup("--yes", "--remote-key-stdin"); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			if !strings.Contains(s.errOut.String(), "OPENAI_API_KEY") {
				t.Fatalf("stderr = %q", s.errOut)
			}
			if len(s.runner.calls) != 0 {
				t.Fatalf("ran %q", s.runner.commands())
			}
			s.noDotEnv()
		})
	}
}

// --- the agent team ---

func TestSetupTeamFlagTurnsTheAgentTeamOnOrOff(t *testing.T) {
	tests := []struct {
		name, current, flag, want string
		saves                     bool
	}{
		{"on", "false", "on", "true", true},
		{"off", "true", "off", "false", true},
		{"on when it is on", "true", "on", "true", false},
		{"absent keeps it on", "true", "", "true", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSetupHarness(t)
			s.rerun(map[string]string{"TURING_AGENT_TEAM_ENABLED": tt.current})
			args := []string{"--yes"}
			if tt.flag != "" {
				args = append(args, "--team", tt.flag)
			}

			if code := s.setup(args...); code != 0 {
				t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
			}
			s.wantEnv(t, map[string]string{"TURING_AGENT_TEAM_ENABLED": tt.want})
			if saved := len(s.checkCalls()) > 0; saved != tt.saves {
				t.Fatalf("saved = %v, want %v; stdout: %s", saved, tt.saves, s.out)
			}
		})
	}
}

func TestSetupAtATerminalAsksAboutTheAgentTeam(t *testing.T) {
	s := newSetupHarness(t)
	s.IsTerminal = true
	s.Stdin = answers("", "", "y", "y")

	if code := s.setup(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, s.out, s.errOut)
	}
	s.wantEnv(t, map[string]string{"TURING_AGENT_TEAM_ENABLED": "true"})
	inOrder(t, s.out.String(), "Turn on the agent team (experimental)? [y/N] ", "  TURING_AGENT_TEAM_ENABLED=true\n")
}

// --- failures ---

func TestSetupOffATerminalWithoutYesIsAUsageErrorAndRunsNothing(t *testing.T) {
	for _, args := range [][]string{{}, {"--engine", "ollama", "--team", "off"}} {
		s := newSetupHarness(t)
		s.Stdin = strings.NewReader(remoteKeyCanary + "\n")

		if code := s.setup(args...); code != 2 {
			t.Fatalf("%q: exit = %d, want 2", args, code)
		}
		if got := s.errOut.String(); got != setupNeedsTerminal+"\n" {
			t.Fatalf("%q: stderr = %q", args, got)
		}
		if len(s.runner.calls) != 0 {
			t.Fatalf("%q: ran %q", args, s.runner.commands())
		}
		s.noDotEnv()
	}
}

func TestSetupWithDockerDownSaysHowToStartItAndRunsNothingElse(t *testing.T) {
	for goos, how := range map[string]string{"darwin": "open Docker Desktop", "linux": "sudo systemctl start docker"} {
		s := newSetupHarness(t)
		s.GOOS = goos
		s.dockerDown()

		if code := s.setup("--yes"); code != 1 {
			t.Fatalf("%s: exit = %d, want 1", goos, code)
		}
		s.wantCommands([]string{"docker info --format {{.ServerVersion}}"})
		got := s.errOut.String()
		for _, want := range []string{"Docker is not running", how, "./turing setup"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: stderr = %q, want %q", goos, got, want)
			}
		}
		s.noDotEnv()
	}
}

func TestSetupOutsideATuringAgentBackendStops(t *testing.T) {
	for _, missing := range []string{"scripts/init.sh", "infra/docker-compose.yml"} {
		s := newSetupHarness(t)
		if err := os.Remove(filepath.Join(s.BackendDir, missing)); err != nil {
			t.Fatal(err)
		}

		if code := s.setup("--yes"); code != 1 {
			t.Fatalf("without %s: exit = %d, want 1", missing, code)
		}
		if got := s.errOut.String(); !strings.Contains(got, "not a TuringAgent backend") {
			t.Fatalf("without %s: stderr = %q", missing, got)
		}
		s.wantCommands([]string{"docker info --format {{.ServerVersion}}"})
	}
}

func TestSetupStopsWhenInitFails(t *testing.T) {
	s := newSetupHarness(t)
	s.runner.on(func(cmd host.Command) bool { return cmd.Name == "./scripts/init.sh" },
		fakeReply{stderr: "Initialization failed: .env must be a regular file, not a symlink.\n", code: 1})

	if code := s.setup("--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	s.wantCommands([]string{"docker info --format {{.ServerVersion}}", "./scripts/init.sh"})
	inOrder(t, s.errOut.String(), "Initialization failed", "./scripts/init.sh failed (exit status 1)")
	oneNextStep(t, s.errOut.String())
	call := s.runner.calls[1]
	if call.Dir != s.BackendDir || !slices.Equal(call.Env, append(slices.Clone(s.Environ), "TURING_SETUP=1")) || call.Stdin != nil {
		t.Fatalf("init.sh ran in %q with env %q and stdin %v; want the backend, setup's environment with TURING_SETUP=1, and no input", call.Dir, call.Env, call.Stdin)
	}
}

func TestSetupStopsWhenTheBuildFails(t *testing.T) {
	s := newSetupHarness(t)
	s.runner.on(composeCall("build"), fakeReply{stderr: "failed to solve\n", code: 17})

	if code := s.setup("--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	s.wantCommands(preflightToBuild, []string{"docker info --format {{.ServerVersion}}"})
	if got := s.errOut.String(); !strings.Contains(got, "Compose failed (exit status 17)") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestSetupStopsBeforeStartingWhenTheSaveIsRefused(t *testing.T) {
	s := newSetupHarness(t)
	s.engine.serve(bridgeGateway+":8000", 200, openAIModels)
	s.runner.on(checkConfigCall("turing-agent-runtime-general"), fakeReply{stderr: "TURING_LOCAL_MODEL is refused\n", code: 1})

	if code := s.setup("--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := s.envBytes(); string(got) != string(s.fresh) {
		t.Fatalf(".env changed")
	}
	if s.ranCommand("compose up") || len(s.engineCheckCalls()) != 0 {
		t.Fatalf("ran %q", s.runner.commands())
	}
	if !strings.Contains(s.errOut.String(), "TURING_LOCAL_MODEL is refused") {
		t.Fatalf("stderr = %q", s.errOut)
	}
	if strings.Contains(s.out.String(), "API key:") {
		t.Fatalf("stdout = %q", s.out)
	}
}

func TestSetupThatDoesNotComeUpHealthyStopsWithoutCheckingTheEngine(t *testing.T) {
	s := newSetupHarness(t)
	s.runner.on(composeCall(psArgs...), fakeReply{stdout: psOutput(withService(allRunning(), "turing-mcp-files", "exited", "")...)})

	if code := s.setup("--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if len(s.engineCheckCalls()) != 0 {
		t.Fatalf("ran %q", s.runner.commands())
	}
	if got := s.errOut.String(); !strings.Contains(got, "./turing logs files") {
		t.Fatalf("stderr = %q", got)
	}
	oneNextStep(t, s.errOut.String())
}

func TestSetupWhoseEngineCheckFailsStillShowsHowToConnectAndExits1(t *testing.T) {
	s := newSetupHarness(t)
	s.runner.on(checkEngineCall(), fakeReply{stdout: "1/4 reachable: ok\n", stderr: engineRefusal, code: 1})

	if code := s.setup("--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := s.errOut.String(); got != engineRefusal+engineFailedAfterRun+"\n" {
		t.Fatalf("stderr = %q", got)
	}
	oneNextStep(t, s.errOut.String())
	if !strings.HasSuffix(s.out.String(), connectLines) {
		t.Fatalf("stdout = %q, want it to end with how to connect", s.out)
	}
}

func TestSetupThatCouldNotCheckTheEngineNamesOneNextStep(t *testing.T) {
	s := newSetupHarness(t)
	s.runner.on(inspectCall("turing-agent-runtime-general"), fakeReply{stdout: "config\n"})

	if code := s.setup("--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	got := s.errOut.String()
	if want := "The settings are saved and Turing is running, but the engine was not checked; run ./turing start --build first"; lastLine(got) != want {
		t.Fatalf("stderr = %q, want it to end with %q", got, want)
	}
	oneNextStep(t, got)
	if !strings.HasSuffix(s.out.String(), connectLines) {
		t.Fatalf("stdout = %q", s.out)
	}
}

func TestSetupRefusesFlagsItCannotUseBeforeRunningAnything(t *testing.T) {
	tests := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--yes", "--engine", "llama"}, 2, "--engine takes ollama or openai"},
		{[]string{"--yes", "--engine", "openai", "--url", "http://localhost:8000/v1"}, 2, "openai needs --url and --model"},
		{[]string{"--yes", "--engine", "openai", "--model", "m"}, 2, "openai needs --url and --model"},
		{[]string{"--yes", "--model", "m"}, 2, "--url, --model and --label go with --engine"},
		{[]string{"--yes", "--team", "maybe"}, 2, "--team takes on or off"},
		{[]string{"--yes", "--engine", "openai", "--url", "http://192.168.1.20:8000/v1", "--model", "m"}, 1, "--url: the runtime takes only"},
		{[]string{"--yes", "--engine", "openai", "--url", "http://user:sk-CANARY-ARGUMENT@localhost:8000/v1", "--model", "m"}, 1, "--url: the runtime takes only"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			s := newSetupHarness(t)

			if code := s.setup(tt.args...); code != tt.code {
				t.Fatalf("exit = %d, want %d", code, tt.code)
			}
			if got := s.errOut.String(); !strings.Contains(got, tt.want) {
				t.Fatalf("stderr = %q, want %q", got, tt.want)
			}
			if len(s.runner.calls) != 0 {
				t.Fatalf("ran %q", s.runner.commands())
			}
			s.noOutputContains("192.168.1.20", "sk-CANARY-ARGUMENT")
			s.noDotEnv()
		})
	}
}

func TestSetupNeverTakesAKeyOrAnythingElseUnknownOnTheCommandLine(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--yes", "--remote-key", "sk-CANARY-ARGUMENT"}, "--remote-key-stdin"},
		{[]string{"--yes", "--remote-key=sk-CANARY-ARGUMENT"}, "--remote-key-stdin"},
		{[]string{"--yes", "--api-key", "sk-CANARY-ARGUMENT"}, "--remote-key-stdin"},
		{[]string{"--yes", "-api-key=sk-CANARY-ARGUMENT"}, "--remote-key-stdin"},
		{[]string{"--yes", "sk-CANARY-ARGUMENT"}, "takes no arguments"},
		{[]string{"--yes", "--key=sk-CANARY-ARGUMENT"}, "unknown option"},
		{[]string{"--yes=sk-CANARY-ARGUMENT"}, "unknown option"},
		{[]string{"--yes", "--team", "sk-CANARY-ARGUMENT"}, "--team takes on or off"},
		{[]string{"--yes", "--engine", "sk-CANARY-ARGUMENT"}, "--engine takes ollama or openai"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			s := newSetupHarness(t)

			if code := s.setup(tt.args...); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			if got := s.errOut.String(); !strings.Contains(got, tt.want) {
				t.Fatalf("stderr = %q, want %q", got, tt.want)
			}
			if len(s.runner.calls) != 0 {
				t.Fatalf("ran %q", s.runner.commands())
			}
			s.noOutputContains("sk-CANARY-ARGUMENT")
		})
	}
}

func TestHelpListsSetupAndNoCommandIsStillComing(t *testing.T) {
	h := newHarness(t)
	if code := h.run("help"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	out := h.out.String()
	for _, want := range []string{"setup [--yes]", "--engine ollama|openai", "--remote-url URL", "--remote-model M", "--remote-key-stdin", "--team on|off"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage does not mention %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "coming in a later version") {
		t.Errorf("usage still says a command is coming:\n%s", out)
	}
}
