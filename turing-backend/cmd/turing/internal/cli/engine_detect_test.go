package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/machine"
)

const bytesPerGiB = uint64(1) << 30

// macMachine is a Mac as `uname -m` and `sysctl -n` report it.
func macMachine(arch, brand string, memGiB uint64) machine.Reader {
	outputs := map[string]string{
		"uname -m":                           arch + "\n",
		"sysctl -n hw.memsize":               fmt.Sprintf("%d\n", memGiB*bytesPerGiB),
		"sysctl -n machdep.cpu.brand_string": brand + "\n",
	}
	return machine.Reader{
		GOOS:   "darwin",
		GOARCH: arch,
		Output: func(name string, args ...string) (string, error) {
			out, ok := outputs[strings.Join(append([]string{name}, args...), " ")]
			if !ok {
				return "", errors.New("exec: " + name + ": executable file not found in $PATH")
			}
			return out, nil
		},
	}
}

// linuxMachine is a Linux machine made of the files named here.
func linuxMachine(files map[string]string) machine.Reader {
	return machine.Reader{
		GOOS:   "linux",
		GOARCH: "amd64",
		ReadFile: func(path string) ([]byte, error) {
			content, ok := files[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return []byte(content), nil
		},
		Glob: func(pattern string) ([]string, error) {
			var matches []string
			for path := range files {
				if ok, err := filepath.Match(pattern, path); err == nil && ok {
					matches = append(matches, path)
				}
			}
			slices.Sort(matches)
			return matches, nil
		},
	}
}

// frameworkDesktop is the 128 GB Framework Desktop: the BIOS has moved 96 GiB
// out of the 31 GiB the kernel reports.
func frameworkDesktop() machine.Reader {
	return linuxMachine(map[string]string{
		"/proc/meminfo":                                   "MemTotal:       32505856 kB\n",
		"/proc/cpuinfo":                                   "model name\t: AMD RYZEN AI MAX+ 395 w/ Radeon 8060S\n",
		"/sys/class/drm/card1/device/vendor":              "0x1002\n",
		"/sys/class/drm/card1/device/mem_info_vram_total": "103079215104\n",
	})
}

const bridgeGateway = "172.17.0.1"

// onFramework makes h the Framework Desktop, with Docker's bridge at the usual
// address.
func (h *harness) onFramework() {
	h.GOOS = "linux"
	h.Machine = frameworkDesktop()
	h.gateway = bridgeGateway
}

// onMacMini makes h a 24 GB Mac mini.
func (h *harness) onMacMini() {
	h.GOOS = "darwin"
	h.Machine = macMachine("arm64", "Apple M4", 24)
}

func sameEngines(t *testing.T, got, want []foundEngine) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("engines =\n  %+v\nwant\n  %+v", got, want)
	}
}

var (
	ollamaOnLoopback = foundEngine{
		Engine: "ollama", Label: "Ollama",
		HostURL: "http://127.0.0.1:11434", ContainerURL: "http://host.docker.internal:11434",
		Models: []string{"qwen2.5:7b", "llama3:latest", "phi3:mini"},
	}
	ds4OnLoopback = foundEngine{
		Engine: "openai", Label: "ds4",
		HostURL: "http://127.0.0.1:8000/v1", ContainerURL: "http://host.docker.internal:8000/v1",
		Models: []string{"deepseek-chat", "deepseek-reasoner"},
	}
)

func TestDiscoverEnginesFindsOllamaOnly(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "")

	sameEngines(t, got, []foundEngine{ollamaOnLoopback})
	// The configured engine is Ollama at its usual address, so it is asked once.
	if want := []string{"http://127.0.0.1:11434/api/tags", "http://127.0.0.1:8000/v1/models"}; !slices.Equal(h.engine.urls(), want) {
		t.Fatalf("asked %q, want %q", h.engine.urls(), want)
	}
	if h.gatewayAsked != 0 {
		t.Fatalf("asked Docker for the bridge gateway %d times on darwin", h.gatewayAsked)
	}
}

func TestDiscoverEnginesFindsDS4OnlyWithItsModels(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:8000", 200, openAIModels)

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "")

	sameEngines(t, got, []foundEngine{ds4OnLoopback})
}

func TestDiscoverEnginesFindsBothInTheOrderItAsks(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)
	h.engine.serve("127.0.0.1:8000", 200, openAIModels)

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "")

	sameEngines(t, got, []foundEngine{ollamaOnLoopback, ds4OnLoopback})
}

func TestDiscoverEnginesFindsNoneWhenNothingAnswers(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "")

	sameEngines(t, got, nil)
	if len(h.engine.requests) != 2 {
		t.Fatalf("asked %q, want Ollama's and ds4's usual addresses", h.engine.urls())
	}
}

func TestDiscoverEnginesWithNoDotEnvStillAsksTheUsualAddresses(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	got := h.discoverEngines(t.Context(), nil, "")

	sameEngines(t, got, []foundEngine{ollamaOnLoopback})
}

func TestDiscoverEnginesOnLinuxAsksTheBridgeGatewayOnceAndLabelsWhatAnswersThere(t *testing.T) {
	h := newHarness(t)
	h.gateway = bridgeGateway
	h.engine.serve("172.17.0.1:8000", 200, openAIModels)

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "")

	sameEngines(t, got, []foundEngine{{
		Engine: "openai", Label: "ds4",
		HostURL: "http://172.17.0.1:8000/v1", ContainerURL: "http://host.docker.internal:8000/v1",
		Models: []string{"deepseek-chat", "deepseek-reasoner"},
	}})
	if h.gatewayAsked != 1 {
		t.Fatalf("asked Docker for the bridge gateway %d times, want once for every candidate", h.gatewayAsked)
	}
}

func TestDiscoverEnginesOnLinuxMarksAnEngineThatAnswersOnlyOnLoopback(t *testing.T) {
	h := newHarness(t)
	h.gateway = bridgeGateway
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "")

	if len(got) != 1 {
		t.Fatalf("engines = %+v, want Ollama alone", got)
	}
	engine := got[0]
	if !engine.LoopbackOnly || engine.Engine != "ollama" || engine.HostURL != "http://127.0.0.1:11434" || engine.ContainerURL != "http://host.docker.internal:11434" {
		t.Fatalf("engine = %+v, want Ollama found on loopback and marked loopback-only", engine)
	}
	if !strings.Contains(engine.Problem, "127.0.0.1") || !strings.Contains(engine.Problem, "containers cannot reach") {
		t.Fatalf("problem = %q, want it to say the containers cannot reach an engine on 127.0.0.1", engine.Problem)
	}
	want := []string{
		"http://172.17.0.1:11434/api/tags", "http://127.0.0.1:11434/api/tags",
		"http://172.17.0.1:8000/v1/models", "http://127.0.0.1:8000/v1/models",
	}
	if !slices.Equal(h.engine.urls(), want) {
		t.Fatalf("asked %q, want %q", h.engine.urls(), want)
	}
}

func TestDiscoverEnginesOnLinuxWithoutAGatewayAsksLoopbackAndSaysReachabilityIsUnknown(t *testing.T) {
	h := newHarness(t)
	h.gatewayErr = errors.New("could not run docker: exec: docker: not found")
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "")

	if len(got) != 1 || got[0].LoopbackOnly || got[0].HostURL != "http://127.0.0.1:11434" {
		t.Fatalf("engines = %+v, want Ollama on loopback, not marked loopback-only", got)
	}
	if !strings.Contains(got[0].Problem, "bridge gateway was not found") || !strings.Contains(got[0].Problem, "docker: not found") {
		t.Fatalf("problem = %q, want the gateway failure", got[0].Problem)
	}
	for _, url := range h.engine.urls() {
		if !strings.Contains(url, "127.0.0.1") {
			t.Fatalf("asked %q; with no gateway only loopback can be asked", h.engine.urls())
		}
	}
}

func TestDiscoverEnginesRewritesLocalhostInURLAndFindsAnOpenAICompatibleServer(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:9000", 200, openAIModels)

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "http://localhost:9000/v1")

	sameEngines(t, got, []foundEngine{{
		Engine: "openai", Label: "OpenAI-compatible",
		HostURL: "http://127.0.0.1:9000/v1", ContainerURL: "http://host.docker.internal:9000/v1",
		Models: []string{"deepseek-chat", "deepseek-reasoner"},
	}})
	// Nothing says which kind of engine --url names, so Ollama's list is tried
	// first and, since something answered that was not it, OpenAI's.
	want := []string{
		"http://127.0.0.1:11434/api/tags", "http://127.0.0.1:8000/v1/models",
		"http://127.0.0.1:9000/v1/api/tags", "http://127.0.0.1:9000/v1/models",
	}
	if !slices.Equal(h.engine.urls(), want) {
		t.Fatalf("asked %q, want %q", h.engine.urls(), want)
	}
}

func TestDiscoverEnginesFindsAnOllamaAtURLWithoutTryingOpenAI(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:12345", 200, ollamaTags)

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "http://127.0.0.1:12345")

	if len(got) != 1 || got[0].Engine != "ollama" || got[0].Label != "Ollama" || got[0].ContainerURL != "http://host.docker.internal:12345" {
		t.Fatalf("engines = %+v, want an Ollama at the rewritten URL", got)
	}
	if slices.Contains(h.engine.urls(), "http://127.0.0.1:12345/models") {
		t.Fatalf("asked %q; OpenAI's list is tried only when Ollama's is not it", h.engine.urls())
	}
}

func TestDiscoverEnginesDropsAURLNothingAnswersOnAfterOneTry(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "http://localhost:9000")

	sameEngines(t, got, nil)
	if want := 1; countAsked(h.engine, "127.0.0.1:9000") != want {
		t.Fatalf("asked %q; an address nothing answers on is asked once, not once per kind", h.engine.urls())
	}
}

func countAsked(f *fakeEngine, hostPort string) int {
	count := 0
	for _, req := range f.requests {
		if req.URL.Host == hostPort {
			count++
		}
	}
	return count
}

func TestDiscoverEnginesAsksEachContainerURLOnce(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:8000", 200, openAIModels)
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	// The configured engine is ds4's usual address, and --url is Ollama's.
	got := h.discoverEngines(t.Context(),
		dotEnv(t, "TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://localhost:8000/v1/\n"),
		"http://localhost:11434")

	if len(got) != 2 || got[0].Label != "ds4" || got[1].Label != "Ollama" {
		t.Fatalf("engines = %+v, want ds4 (configured) then Ollama", got)
	}
	if len(h.engine.requests) != 2 {
		t.Fatalf("asked %q, want one request per container URL", h.engine.urls())
	}
}

func TestDiscoverEnginesSendsTheKeyOnlyToTheConfiguredEngine(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:7777", 200, openAIModels)
	h.engine.serve("127.0.0.1:8000", 200, openAIModels)
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	got := h.discoverEngines(t.Context(),
		dotEnv(t, "TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://host.docker.internal:7777/v1\nTURING_LOCAL_ENGINE_API_KEY=CANARY-ENGINE-KEY\n"),
		"http://localhost:9999")

	if len(got) != 3 {
		t.Fatalf("engines = %+v, want the configured one plus the two usual ones", got)
	}
	for _, req := range h.engine.requests {
		authorization := req.Header.Get("Authorization")
		if req.URL.Host == "127.0.0.1:7777" {
			if authorization != "Bearer CANARY-ENGINE-KEY" {
				t.Errorf("the configured engine was asked with Authorization %q", authorization)
			}
		} else if authorization != "" {
			t.Errorf("%s was asked with an Authorization header; the key is for the configured engine alone", req.URL)
		}
	}
}

func TestDiscoverEnginesKeepsTheConfiguredEngineThatAnswersWithAnError(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:8000", 401, `{"error":"unauthorized"}`)

	got := h.discoverEngines(t.Context(), dotEnv(t, "TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1\n"), "")

	if len(got) != 1 || got[0].Label != "ds4" || len(got[0].Models) != 0 {
		t.Fatalf("engines = %+v, want the configured ds4 with no models", got)
	}
	if !strings.Contains(got[0].Problem, "HTTP 401") {
		t.Fatalf("problem = %q, want it to say the engine answered HTTP 401", got[0].Problem)
	}
}

func TestDiscoverEnginesDropsAGuessedAddressThatAnswersWithAnError(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	// Something that is not an engine listens on ds4's usual port.
	h.engine.serve("127.0.0.1:8000", 404, "not found")

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "")

	sameEngines(t, got, nil)
}

func TestDiscoverEnginesLeavesOutAConfiguredEngineTheRuntimeWouldRefuse(t *testing.T) {
	for name, env := range map[string]string{
		"an engine that is neither ollama nor openai": "TURING_LOCAL_ENGINE=bogus\n",
		"an openai engine with no URL":                "TURING_LOCAL_ENGINE=openai\n",
		"a URL with a password":                       "TURING_LOCAL_ENGINE_URL=http://user:CANARY-URL-PASSWORD@host.docker.internal:12345\n",
		"a URL on another machine":                    "TURING_LOCAL_ENGINE_URL=http://192.168.1.50:12345\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.GOOS = "darwin"
			h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

			got := h.discoverEngines(t.Context(), dotEnv(t, env), "")

			sameEngines(t, got, []foundEngine{ollamaOnLoopback})
			for _, url := range h.engine.urls() {
				if strings.Contains(url, "CANARY") || strings.Contains(url, "12345") || strings.Contains(url, "192.168") {
					t.Fatalf("asked %q, an address the runtime refuses", url)
				}
			}
		})
	}
}

func TestDiscoverEnginesAsksAConfiguredLocalhostURLWhereTheContainersWouldNeedIt(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:8000", 200, openAIModels)

	got := h.discoverEngines(t.Context(), dotEnv(t, "TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://localhost:8000/v1\n"), "")

	sameEngines(t, got, []foundEngine{ds4OnLoopback})
}

func TestDiscoverEnginesDropsControlCharactersFromModelNames(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:11434", 200, `{"models":[{"name":"evil\u001b[2Jmodel"},{"name":"\u0007"},{"name":"plain:7b"}]}`)

	got := h.discoverEngines(t.Context(), dotEnv(t, ""), "")

	if len(got) != 1 || !slices.Equal(got[0].Models, []string{"evil[2Jmodel", "plain:7b"}) {
		t.Fatalf("engines = %+v, want the escape and bell dropped and the all-control name gone", got)
	}
}

// detect runs `engine detect` and returns what it printed.
func (h *harness) detect(args ...string) string {
	h.t.Helper()
	if code := h.run(append([]string{"engine", "detect"}, args...)...); code != 0 {
		h.t.Fatalf("engine detect %q exit = %d, want 0\nstdout: %s\nstderr: %s", args, code, h.out, h.errOut)
	}
	return h.out.String()
}

// inOrder fails the test unless each part appears in out, one after another.
func inOrder(t *testing.T, out string, parts ...string) {
	t.Helper()
	rest := out
	for _, part := range parts {
		i := strings.Index(rest, part)
		if i < 0 {
			t.Fatalf("output has no %q after the lines before it:\n%s", part, out)
		}
		rest = rest[i+len(part):]
	}
}

func TestEngineDetectOnTheFrameworkDesktopRecommendsDS4AndExplainsTheBridge(t *testing.T) {
	h := newHarness(t)
	h.onFramework()
	h.engine.serve("127.0.0.1:8000", 200, openAIModels)

	out := h.detect()

	inOrder(t, out,
		"This machine: Linux, AMD RYZEN AI MAX+ 395 w/ Radeon 8060S, 127 GB\n",
		"Engines answering:\n  ds4: http://host.docker.internal:8000/v1 (models: deepseek-chat, deepseek-reasoner); it answers only on 127.0.0.1, which the containers cannot reach\n",
		"Recommended: ds4 (ROCm) with DeepSeek V4 Flash Q2 or GLM 5.3 Q2; not Qwen3.8 (see docs/architecture/local-engines.md)\n",
		"  Why: an AMD Strix Halo in the 96 GB+ class",
		"Docker's bridge gateway is 172.17.0.1: on Linux the containers reach this machine there, as host.docker.internal",
		"  To reach ds4 from the containers, start it with --host 172.17.0.1.\n",
		"Note: "+ds4CacheNote+"\n",
		"To use it: ./turing engine use <engine> --url <url> --model <model>, or ./turing setup\n",
	)
	if strings.Contains(out, "For example:") {
		t.Fatalf("an engine the containers cannot reach is offered as an example:\n%s", out)
	}
	if h.gatewayAsked != 1 {
		t.Fatalf("asked Docker for the bridge gateway %d times, want once", h.gatewayAsked)
	}
	h.noSecrets()
}

func TestEngineDetectGivesTheUseCommandForAReachableEngine(t *testing.T) {
	h := newHarness(t)
	h.onFramework()
	h.engine.serve("172.17.0.1:8000", 200, openAIModels)

	out := h.detect()

	inOrder(t, out,
		"Engines answering:\n  ds4: http://host.docker.internal:8000/v1 (models: deepseek-chat, deepseek-reasoner)\n",
		"Recommended: ds4 (ROCm)",
		"Docker's bridge gateway is 172.17.0.1",
		"To use it: ./turing engine use <engine> --url <url> --model <model>, or ./turing setup\n",
		"For example: ./turing engine use openai --url http://host.docker.internal:8000/v1 --model deepseek-chat\n",
	)
	if strings.Contains(out, "To reach") {
		t.Fatalf("bind advice is given for an engine that is reachable:\n%s", out)
	}
}

func TestEngineDetectOnAMacMiniRecommendsOllama(t *testing.T) {
	h := newHarness(t)
	h.onMacMini()
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	out := h.detect()

	inOrder(t, out,
		"This machine: macOS, Apple M4, 24 GB\n",
		"Engines answering:\n  Ollama: http://host.docker.internal:11434 (models: qwen2.5:7b, llama3:latest, phi3:mini)\n",
		"Recommended: Ollama with qwen2.5:7b (see docs/architecture/local-engines.md)\n",
		"  Why: Apple Silicon with less memory than the 64 GB class",
		"To use it: ./turing engine use <engine> --url <url> --model <model>, or ./turing setup\n",
		"For example: ./turing engine use ollama --url http://host.docker.internal:11434 --model qwen2.5:7b\n",
	)
	for _, absent := range []string{"bridge gateway", "--kv-disk-dir", "()"} {
		if strings.Contains(out, absent) {
			t.Fatalf("output mentions %q on a Mac that wants Ollama:\n%s", absent, out)
		}
	}
	if h.gatewayAsked != 0 {
		t.Fatalf("asked Docker for the bridge gateway %d times on darwin", h.gatewayAsked)
	}
}

func TestEngineDetectOnA64GBMacRecommendsDS4OnMetal(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.Machine = macMachine("arm64", "Apple M4 Pro", 64)

	out := h.detect()

	inOrder(t, out,
		"This machine: macOS, Apple M4 Pro, 64 GB\n",
		"Engines answering: none\n",
		"Recommended: ds4 (Metal) with Qwen3.8 Q2, about 42 GiB resident (see docs/architecture/local-engines.md)\n",
		"Note: "+ds4CacheNote+"\n",
	)
}

func TestEngineDetectWithNothingAnsweringSaysNoneAndStillRecommends(t *testing.T) {
	h := newHarness(t)
	h.onMacMini()

	out := h.detect()

	inOrder(t, out, "Engines answering: none\n", "Recommended: Ollama with qwen2.5:7b", "To use it:")
	if strings.Contains(out, "For example:") {
		t.Fatalf("an example is given with no engine to use:\n%s", out)
	}
}

func TestEngineDetectSaysWhatItCouldNotReadAndStillRecommends(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "linux"
	h.gateway = bridgeGateway
	h.Machine = machine.Reader{GOOS: "linux", GOARCH: "amd64"}

	out := h.detect()

	inOrder(t, out,
		"This machine: Linux, amd64, memory unknown\n",
		"  Note: could not read this machine's memory",
		"Recommended: Ollama with qwen2.5:7b",
		"  Why: this machine's memory could not be read",
	)
}

func TestEngineDetectNamesTheFirstFiveModelsAndCountsTheRest(t *testing.T) {
	h := newHarness(t)
	h.onMacMini()
	var entries []string
	for i := 1; i <= 7; i++ {
		entries = append(entries, fmt.Sprintf(`{"name":"m%d"}`, i))
	}
	h.engine.serve("127.0.0.1:11434", 200, `{"models":[`+strings.Join(entries, ",")+`]}`)

	out := h.detect()

	if !strings.Contains(out, "(models: m1, m2, m3, m4, m5, and 2 more)") || strings.Contains(out, "m6") {
		t.Fatalf("output does not show five models and a count:\n%s", out)
	}
}

func TestEngineDetectCapsALongModelName(t *testing.T) {
	h := newHarness(t)
	h.onMacMini()
	h.engine.serve("127.0.0.1:11434", 200, `{"models":[{"name":"`+strings.Repeat("x", 500)+`"},{"name":"short"}]}`)

	out := h.detect()

	if strings.Contains(out, strings.Repeat("x", 81)) || !strings.Contains(out, strings.Repeat("x", 80)+"...") {
		t.Fatalf("a 500-character model name is not cut to 80:\n%s", out)
	}
}

func TestEngineDetectChoosesTheModelForItsExampleWithoutGuessing(t *testing.T) {
	tests := []struct {
		name   string
		models string
		want   string
	}{
		{"the recommended model when it is listed", `{"models":[{"name":"llama3:latest"},{"name":"qwen2.5:7b"}]}`, "--model qwen2.5:7b\n"},
		{"the only model when it is not the recommended one", `{"models":[{"name":"llama3:latest"}]}`, "--model llama3:latest\n"},
		{"a placeholder when there is a choice and the recommended model is not in it", `{"models":[{"name":"llama3:latest"},{"name":"phi3:mini"}]}`, "--model <model>\n"},
		{"a placeholder when none is listed", `{"models":[]}`, "--model <model>\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.onMacMini()
			h.engine.serve("127.0.0.1:11434", 200, tt.models)

			out := h.detect()

			if !strings.Contains(out, "For example: ./turing engine use ollama --url http://host.docker.internal:11434 "+tt.want) {
				t.Fatalf("output has no example ending %q:\n%s", tt.want, out)
			}
		})
	}
}

func TestEngineDetectSaysAnEngineWithNoModelsHasNone(t *testing.T) {
	h := newHarness(t)
	h.onMacMini()
	h.engine.serve("127.0.0.1:11434", 200, `{"models":[]}`)

	out := h.detect()

	if !strings.Contains(out, "Ollama: http://host.docker.internal:11434 (no models listed)") {
		t.Fatalf("output does not say Ollama lists no models:\n%s", out)
	}
}

func TestEngineDetectGivesOllamaItsOwnBindAdviceOnLinux(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "linux"
	h.gateway = bridgeGateway
	h.Machine = linuxMachine(map[string]string{"/proc/meminfo": "MemTotal: 16777216 kB\n", "/proc/cpuinfo": "model name\t: Some CPU\n"})
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	out := h.detect()

	inOrder(t, out,
		"Engines answering:\n  Ollama: http://host.docker.internal:11434",
		"Docker's bridge gateway is 172.17.0.1",
		"  To reach Ollama from the containers, set OLLAMA_HOST=172.17.0.1 for the Ollama service (for example with sudo systemctl edit ollama) and restart it.\n",
	)
}

func TestEngineDetectSaysWhenTheBridgeGatewayWasNotFound(t *testing.T) {
	h := newHarness(t)
	h.onFramework()
	h.gatewayErr = errors.New("docker network inspect bridge exited 1: Cannot connect to the Docker daemon")

	out := h.detect()

	if !strings.Contains(out, "Docker's bridge gateway was not found (docker network inspect bridge exited 1: Cannot connect to the Docker daemon); start Docker") {
		t.Fatalf("output does not say the gateway was not found:\n%s", out)
	}
	if strings.Contains(out, "Docker's bridge gateway is") {
		t.Fatalf("output names a gateway that was not found:\n%s", out)
	}
}

func TestEngineDetectRepeatsTheCacheNoteForAnyOpenAIEngineEvenWhenOllamaIsRecommended(t *testing.T) {
	h := newHarness(t)
	h.onMacMini()
	h.engine.serve("127.0.0.1:8000", 200, openAIModels)

	out := h.detect()

	inOrder(t, out, "Recommended: Ollama", "Note: "+ds4CacheNote+"\n")
}

func TestEngineDetectWithURLAsksThatAddressToo(t *testing.T) {
	h := newHarness(t)
	h.onMacMini()
	h.engine.serve("127.0.0.1:9000", 200, openAIModels)

	out := h.detect("--url", "http://localhost:9000/v1")

	if !strings.Contains(out, "OpenAI-compatible: http://host.docker.internal:9000/v1 (models: deepseek-chat, deepseek-reasoner)") {
		t.Fatalf("output does not list the engine at --url:\n%s", out)
	}
	if !strings.Contains(out, ds4CacheNote) {
		t.Fatalf("an OpenAI-compatible engine was found and the cache note is missing:\n%s", out)
	}
}

func TestEngineDetectRefusesAURLTheRuntimeRefusesAndDoesNotEchoIt(t *testing.T) {
	for _, args := range [][]string{
		{"--url", "http://user:CANARY-URL-PASSWORD@localhost:9000/v1"},
		{"--url=http://192.168.1.50:9000/v1?key=CANARY-URL-PASSWORD"},
		{"--url", ""},
	} {
		h := newHarness(t)
		h.onMacMini()

		code := h.run(append([]string{"engine", "detect"}, args...)...)

		if code != 1 {
			t.Fatalf("%q: exit = %d, want 1", args, code)
		}
		if h.out.Len() != 0 || len(h.engine.requests) != 0 {
			t.Fatalf("%q: printed %q and asked %q before refusing", args, h.out, h.engine.urls())
		}
		if strings.Contains(h.errOut.String(), "CANARY") || !strings.Contains(h.errOut.String(), "--url") || !strings.Contains(h.errOut.String(), "http or https") {
			t.Fatalf("%q: stderr = %q, want the rule without the URL", args, h.errOut)
		}
	}
}

func TestEngineDetectWorksBeforeThereIsADotEnv(t *testing.T) {
	h := newHarness(t)
	h.onMacMini()
	h.removeEnv()
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	out := h.detect()

	if !strings.Contains(out, "Ollama: http://host.docker.internal:11434") || strings.Contains(out, "Note:") {
		t.Fatalf("output without a .env:\n%s", out)
	}
}

func TestEngineDetectReportsADotEnvItCannotReadAndGoesOn(t *testing.T) {
	h := newHarness(t)
	h.onMacMini()
	h.writeEnv("OPENAI_API_KEY=a\x00b\n")
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	out := h.detect()

	inOrder(t, out, "  Note: cannot parse .env", "the engine it configures was not asked", "Ollama: http://host.docker.internal:11434")
}

func TestEngineDetectChangesNothingAndRunsNoCommand(t *testing.T) {
	h := newHarness(t)
	h.onFramework()
	h.engine.serve("172.17.0.1:8000", 200, openAIModels)
	before, err := os.ReadFile(filepath.Join(h.BackendDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}

	h.detect()

	after, err := os.ReadFile(filepath.Join(h.BackendDir, ".env"))
	if err != nil || string(after) != string(before) {
		t.Fatalf(".env changed: %q -> %q (%v)", before, after, err)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q; detect asks the bridge through Env and runs nothing itself", h.runner.commands())
	}
	for _, req := range h.engine.requests {
		if req.Method != "GET" {
			t.Fatalf("%s %s: detect only reads", req.Method, req.URL)
		}
	}
}

func TestEngineDetectNeverPrintsASecret(t *testing.T) {
	h := newHarness(t)
	h.onFramework()
	h.writeEnv("TURING_CLIENT_API_KEY=" + testKey + "\nOPENAI_API_KEY=sk-in-dotenv\n" +
		"TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1\nTURING_LOCAL_ENGINE_API_KEY=CANARY-ENGINE-KEY\n")
	h.engine.serve("172.17.0.1:8000", 200, openAIModels)

	h.detect()

	h.noSecrets()
	if strings.Contains(h.out.String(), "CANARY-ENGINE-KEY") || strings.Contains(h.errOut.String(), "CANARY-ENGINE-KEY") {
		t.Fatalf("detect printed TURING_LOCAL_ENGINE_API_KEY:\nstdout: %s\nstderr: %s", h.out, h.errOut)
	}
	if got := h.engine.requests[0].Header.Get("Authorization"); got != "Bearer CANARY-ENGINE-KEY" {
		t.Fatalf("the configured engine was asked with Authorization %q; its key is how it is told apart from a stranger", got)
	}
}

func TestEngineWithNoSubcommandOrAnUnknownOneIsAUsageErrorThatDoesNotEchoIt(t *testing.T) {
	for _, args := range [][]string{
		{"engine"},
		{"engine", "use"},
		{"engine", "check"},
		{"engine", "sk-CANARY-ARGUMENT"},
		{"engine", "--url", "sk-CANARY-ARGUMENT"},
	} {
		h := newHarness(t)

		code := h.run(args...)

		if code != 2 {
			t.Fatalf("%q: exit = %d, want 2", args, code)
		}
		if h.out.Len() != 0 || len(h.runner.calls) != 0 || len(h.engine.requests) != 0 {
			t.Fatalf("%q: printed %q, ran %q and asked %q", args, h.out, h.runner.commands(), h.engine.urls())
		}
		if strings.Contains(h.errOut.String(), "CANARY") || !strings.Contains(h.errOut.String(), "./turing help") {
			t.Fatalf("%q: stderr = %q, want a pointer to help without the argument", args, h.errOut)
		}
	}
}

func TestEngineDetectTakesNoPositionalArgumentAndNoUnknownOption(t *testing.T) {
	for _, args := range [][]string{
		{"engine", "detect", "sk-CANARY-ARGUMENT"},
		{"engine", "detect", "--nope"},
		{"engine", "detect", "--url"},
		{"engine", "detect", "--yes"},
	} {
		h := newHarness(t)

		code := h.run(args...)

		if code != 2 {
			t.Fatalf("%q: exit = %d, want 2", args, code)
		}
		if h.out.Len() != 0 || len(h.engine.requests) != 0 || strings.Contains(h.errOut.String(), "CANARY") {
			t.Fatalf("%q: stdout %q, asked %q, stderr %q", args, h.out, h.engine.urls(), h.errOut)
		}
	}
}

func TestHelpListsEngineDetectAndDoesNotClaimUseOrCheckYet(t *testing.T) {
	h := newHarness(t)
	h.run("help")

	var engineText []string
	for _, line := range strings.Split(h.out.String(), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "engine") {
			engineText = append(engineText, line)
		}
	}
	if len(engineText) == 0 || !strings.Contains(engineText[0], "engine detect [--url URL]") {
		t.Fatalf("help does not list engine detect [--url URL]:\n%s", h.out)
	}
	for _, line := range engineText {
		for _, banned := range []string{"engine use", "engine check", "coming in a later version"} {
			if strings.Contains(line, banned) {
				t.Errorf("help line %q mentions %q", line, banned)
			}
		}
	}
}

func TestOSEnvReadsThisMachine(t *testing.T) {
	env := OSEnv(t.TempDir())

	if env.Machine.GOOS != runtime.GOOS || env.Machine.GOARCH != runtime.GOARCH || env.Machine.ReadFile == nil || env.Machine.Output == nil || env.Machine.Glob == nil {
		t.Fatalf("OSEnv's Machine = %+v, want machine.OSReader()", env.Machine)
	}
}
