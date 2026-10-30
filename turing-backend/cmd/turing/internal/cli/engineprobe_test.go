package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
)

const ollamaTags = `{"models":[{"name":"qwen2.5:7b","model":"qwen2.5:7b"},{"name":"llama3:latest"},{"name":"phi3:mini"}]}`

const openAIModels = `{"object":"list","data":[{"id":"deepseek-chat"},{"id":"deepseek-reasoner"}]}`

func dotEnv(t *testing.T, content string) *envfile.File {
	t.Helper()
	file, err := envfile.Parse([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestEngineProbeDefaultsToOllamaOnTheBridgeGatewayOnLinux(t *testing.T) {
	h := newHarness(t)
	h.gateway = "172.17.0.1"
	h.engine.serve("172.17.0.1:11434", 200, ollamaTags)

	got := h.engineProbe(context.Background(), dotEnv(t, ""))

	if !got.ok() || got.Problem != "" || got.Warning != "" {
		t.Fatalf("result = %+v, want a clean one", got)
	}
	if got.Engine != "ollama" || got.URL != "http://host.docker.internal:11434" || got.Model != "qwen2.5:7b" {
		t.Fatalf("engine = %q, url = %q, model = %q", got.Engine, got.URL, got.Model)
	}
	if len(got.Models) != 3 || !got.Listed {
		t.Fatalf("models = %q, listed = %v, want three with qwen2.5:7b listed", got.Models, got.Listed)
	}
	if want := []string{"http://172.17.0.1:11434/api/tags"}; !slices.Equal(h.engine.urls(), want) {
		t.Fatalf("asked %q, want %q", h.engine.urls(), want)
	}
}

func TestEngineProbeMapsHostDockerInternalToLoopbackOnDarwin(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	got := h.engineProbe(context.Background(), dotEnv(t, ""))

	if !got.ok() {
		t.Fatalf("result = %+v", got)
	}
	if want := []string{"http://127.0.0.1:11434/api/tags"}; !slices.Equal(h.engine.urls(), want) {
		t.Fatalf("asked %q, want %q", h.engine.urls(), want)
	}
	if h.gatewayAsked != 0 {
		t.Fatalf("asked Docker for the bridge gateway %d times on darwin", h.gatewayAsked)
	}
}

func TestEngineProbeResolvesSettingsWithTheServicesFallback(t *testing.T) {
	tests := []struct {
		name                   string
		env                    string
		engine, url, model, at string
	}{
		{
			name:   "ollama with every name empty",
			env:    "TURING_LOCAL_ENGINE=\nTURING_LOCAL_ENGINE_URL=\nTURING_LOCAL_MODEL=\n",
			engine: "ollama", url: "http://host.docker.internal:11434", model: "qwen2.5:7b",
			at: "http://127.0.0.1:11434/api/tags",
		},
		{
			name:   "the deprecated names are read when the new ones are empty",
			env:    "OLLAMA_BASE_URL=http://host.docker.internal:9999\nOLLAMA_MODEL=llama3\n",
			engine: "ollama", url: "http://host.docker.internal:9999", model: "llama3",
			at: "http://127.0.0.1:9999/api/tags",
		},
		{
			name:   "the new names win over the deprecated ones",
			env:    "TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8888\nOLLAMA_BASE_URL=http://host.docker.internal:9999\nTURING_LOCAL_MODEL=phi3:mini\nOLLAMA_MODEL=llama3\n",
			engine: "ollama", url: "http://host.docker.internal:8888", model: "phi3:mini",
			at: "http://127.0.0.1:8888/api/tags",
		},
		{
			name:   "an openai engine reads only the new URL, and its model path gets no api prefix",
			env:    "TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1\nOLLAMA_BASE_URL=http://host.docker.internal:9999\nOLLAMA_MODEL=llama3\n",
			engine: "openai", url: "http://host.docker.internal:8000/v1", model: "llama3",
			at: "http://127.0.0.1:8000/v1/models",
		},
		{
			name:   "a trailing slash is not doubled",
			env:    "TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1/\n",
			engine: "openai", url: "http://host.docker.internal:8000/v1/", model: "qwen2.5:7b",
			at: "http://127.0.0.1:8000/v1/models",
		},
		{
			name:   "a URL with no port keeps none",
			env:    "TURING_LOCAL_ENGINE_URL=http://host.docker.internal\n",
			engine: "ollama", url: "http://host.docker.internal", model: "qwen2.5:7b",
			at: "http://127.0.0.1/api/tags",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.GOOS = "darwin"

			got := h.engineProbe(context.Background(), dotEnv(t, tt.env))

			if got.Engine != tt.engine || got.URL != tt.url || got.Model != tt.model {
				t.Fatalf("engine = %q, url = %q, model = %q; want %q, %q, %q", got.Engine, got.URL, got.Model, tt.engine, tt.url, tt.model)
			}
			if urls := h.engine.urls(); !slices.Equal(urls, []string{tt.at}) {
				t.Fatalf("asked %q, want %q", urls, []string{tt.at})
			}
		})
	}
}

func TestEngineProbeUsesAnOpenAIEngineModelListAndItsKey(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serve("127.0.0.1:8000", 200, openAIModels)

	got := h.engineProbe(context.Background(), dotEnv(t,
		"TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1\nTURING_LOCAL_MODEL=deepseek-chat\nTURING_LOCAL_ENGINE_API_KEY=CANARY-ENGINE\n"))

	if !got.ok() || len(got.Models) != 2 || !got.Listed {
		t.Fatalf("result = %+v, want two models with deepseek-chat listed", got)
	}
	if len(h.engine.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(h.engine.requests))
	}
	request := h.engine.requests[0]
	if request.Method != http.MethodGet {
		t.Fatalf("method = %s, want GET", request.Method)
	}
	if want := "Bearer CANARY-ENGINE"; request.Header.Get("Authorization") != want {
		t.Fatalf("Authorization = %q, want %q", request.Header.Get("Authorization"), want)
	}
	if strings.Contains(got.URL+got.Problem+got.Warning, "CANARY-ENGINE") {
		t.Fatalf("the result carries the key: %+v", got)
	}
}

func TestEngineProbeSendsNoAuthorizationWithoutAKeyAndNeverToOllama(t *testing.T) {
	tests := []struct{ name, env string }{
		{"openai with no key", "TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1\n"},
		{"ollama, which takes no key", "TURING_LOCAL_ENGINE_API_KEY=CANARY-ENGINE\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.GOOS = "darwin"
			h.engine.serve("127.0.0.1:8000", 200, openAIModels)
			h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

			h.engineProbe(context.Background(), dotEnv(t, tt.env))

			if len(h.engine.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(h.engine.requests))
			}
			if got := h.engine.requests[0].Header.Get("Authorization"); got != "" {
				t.Fatalf("Authorization = %q, want none", got)
			}
		})
	}
}

func TestEngineProbeReportsWhetherTheModelIsListed(t *testing.T) {
	tests := []struct {
		name, env, reply string
		listed           bool
	}{
		{"the default model is listed", "", ollamaTags, true},
		{"the model is not listed", "TURING_LOCAL_MODEL=mistral\n", ollamaTags, false},
		{"an untagged Ollama name means latest", "TURING_LOCAL_MODEL=llama3\n", ollamaTags, true},
		{"a tagged name is not its untagged sibling", "TURING_LOCAL_MODEL=phi3\n", ollamaTags, false},
		{"an empty list lists nothing", "", `{"models":[]}`, false},
		{"openai ids are exact", "TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1\nTURING_LOCAL_MODEL=deepseek\n", openAIModels, false},
		{"openai: an untagged name does not match latest", "TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1\nTURING_LOCAL_MODEL=deepseek-chat\n", `{"data":[{"id":"deepseek-chat:latest"}]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.GOOS = "darwin"
			h.engine.serve("127.0.0.1:11434", 200, tt.reply)
			h.engine.serve("127.0.0.1:8000", 200, tt.reply)

			got := h.engineProbe(context.Background(), dotEnv(t, tt.env))

			if !got.ok() {
				t.Fatalf("result = %+v, want a reachable engine", got)
			}
			if got.Listed != tt.listed {
				t.Fatalf("listed = %v, want %v (models %q)", got.Listed, tt.listed, got.Models)
			}
		})
	}
}

func TestEngineProbeReportsAnEngineThatAnswersBadly(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"not found", 404, `{"error":"no"}`, "HTTP 404"},
		{"server error", 500, "boom", "HTTP 500"},
		{"a redirect is not followed", 302, "", "HTTP 302"},
		{"not JSON", 200, "<html>hello</html>", "model list"},
		{"JSON of another shape", 200, `[1,2,3]`, "model list"},
		{"an empty body", 200, "", "model list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.GOOS = "darwin"
			h.engine.serve("127.0.0.1:11434", tt.status, tt.body)

			got := h.engineProbe(context.Background(), dotEnv(t, ""))

			if got.ok() || !strings.Contains(got.Problem, tt.want) {
				t.Fatalf("result = %+v, want a problem naming %q", got, tt.want)
			}
			if !strings.Contains(got.Problem, "http://127.0.0.1:11434/api/tags") {
				t.Fatalf("problem = %q, want it to name what was asked", got.Problem)
			}
		})
	}
}

func TestEngineProbeReportsAnEngineThatDoesNotAnswer(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"

	got := h.engineProbe(context.Background(), dotEnv(t, ""))

	if got.ok() || !strings.Contains(got.Problem, "did not answer") || !strings.Contains(got.Problem, "connection refused") {
		t.Fatalf("result = %+v, want a refused connection reported", got)
	}
	if strings.Contains(got.Problem, "\n") {
		t.Fatalf("problem = %q, want one line", got.Problem)
	}
	if strings.Contains(got.Problem, "Get ") {
		t.Fatalf("problem = %q, want the cause without the client's URL wrapper", got.Problem)
	}
}

// blockingTransport answers nothing until the request is cancelled.
type blockingTransport struct{}

func (blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestEngineProbeGivesUpWhenTheClientTimesOut(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.EngineClient = &http.Client{Transport: blockingTransport{}, Timeout: 20 * time.Millisecond}

	got := h.engineProbe(context.Background(), dotEnv(t, ""))

	if got.ok() || !strings.Contains(got.Problem, "did not answer in time") {
		t.Fatalf("result = %+v, want a timeout reported", got)
	}
}

func TestEngineProbeStopsWhenItsContextEnds(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := h.engineProbe(ctx, dotEnv(t, ""))

	if got.ok() {
		t.Fatalf("result = %+v, want a problem", got)
	}
}

func TestEngineProbeOnLinuxWarnsWhenOnlyLoopbackAnswers(t *testing.T) {
	h := newHarness(t)
	h.gateway = "172.17.0.1"
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	got := h.engineProbe(context.Background(), dotEnv(t, ""))

	if got.ok() {
		t.Fatalf("result = %+v, want the engine reported unreachable: the containers cannot reach 127.0.0.1", got)
	}
	for _, want := range []string{"127.0.0.1", "OLLAMA_HOST=172.17.0.1", "sudo systemctl edit ollama", "restart"} {
		if !strings.Contains(got.Warning, want) {
			t.Fatalf("warning = %q, want it to mention %q", got.Warning, want)
		}
	}
	if strings.Contains(got.Warning, "--host") {
		t.Fatalf("warning = %q: ollama serve has no --host flag", got.Warning)
	}
	if !strings.Contains(got.Problem, "172.17.0.1:11434") {
		t.Fatalf("problem = %q, want it to name the gateway address", got.Problem)
	}
	// The diagnosis is said once, in the warning: the failure line only points to it.
	if strings.Contains(got.Problem, "OLLAMA_HOST") || strings.Contains(got.Problem, "--host") || strings.Contains(got.Problem, "listens only") {
		t.Fatalf("problem = %q, want it not to repeat the warning's diagnosis and advice", got.Problem)
	}
	want := []string{"http://172.17.0.1:11434/api/tags", "http://127.0.0.1:11434/api/tags"}
	if !slices.Equal(h.engine.urls(), want) {
		t.Fatalf("asked %q, want the gateway then loopback %q", h.engine.urls(), want)
	}
}

func TestEngineProbeOnLinuxAdvisesTheHostFlagForAnOpenAIEngine(t *testing.T) {
	h := newHarness(t)
	h.gateway = "172.17.0.1"
	h.engine.serve("127.0.0.1:8000", 200, openAIModels)

	got := h.engineProbe(context.Background(), dotEnv(t,
		"TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1\n"))

	if got.ok() {
		t.Fatalf("result = %+v, want the engine reported unreachable", got)
	}
	if !strings.Contains(got.Warning, "--host 172.17.0.1") || strings.Contains(got.Warning, "OLLAMA_HOST") {
		t.Fatalf("warning = %q, want the --host flag and no Ollama setting", got.Warning)
	}
	if strings.Contains(got.Problem, "--host") || strings.Contains(got.Problem, "OLLAMA_HOST") {
		t.Fatalf("problem = %q, want it not to repeat the warning's advice", got.Problem)
	}
}

func TestEngineProbeOnLinuxWithNothingAnsweringHasNoLoopbackWarning(t *testing.T) {
	h := newHarness(t)
	h.gateway = "172.17.0.1"

	got := h.engineProbe(context.Background(), dotEnv(t, ""))

	if got.ok() || got.Warning != "" {
		t.Fatalf("result = %+v, want a problem and no warning", got)
	}
	if len(h.engine.requests) != 2 {
		t.Fatalf("asked %q, want the gateway and then loopback", h.engine.urls())
	}
}

func TestEngineProbeOnLinuxNeedsTheBridgeGateway(t *testing.T) {
	h := newHarness(t)
	h.gatewayErr = errors.New("Cannot connect to the Docker daemon")
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	got := h.engineProbe(context.Background(), dotEnv(t, ""))

	if got.ok() || !strings.Contains(got.Problem, "bridge gateway") || !strings.Contains(got.Problem, "Cannot connect to the Docker daemon") {
		t.Fatalf("result = %+v, want the lookup failure reported", got)
	}
	if len(h.engine.requests) != 0 {
		t.Fatalf("asked %q after the lookup failed", h.engine.urls())
	}
}

func TestEngineProbeOnLinuxWithNoIPv4GatewayDoesNotBlameDocker(t *testing.T) {
	h := newHarness(t)
	runner := &fakeRunner{t: t}
	runner.on(dockerCall(bridgeInspect...), fakeReply{stdout: "fd00::1 \n"})
	h.BridgeGateway = dockerBridgeGateway(runner, "/backend", nil)
	h.engine.serve("127.0.0.1:11434", 200, ollamaTags)

	got := h.engineProbe(context.Background(), dotEnv(t, ""))

	if got.ok() || !strings.Contains(got.Problem, "IPv4") || !strings.Contains(got.Problem, "fd00::1") {
		t.Fatalf("result = %+v, want the missing IPv4 gateway reported with what Docker said", got)
	}
	if strings.Contains(got.Problem, "is running") || strings.Contains(got.Problem, "daemon") {
		t.Fatalf("problem = %q: Docker answered, so it must not be blamed for being down", got.Problem)
	}
	if len(h.engine.requests) != 0 {
		t.Fatalf("asked %q without a gateway", h.engine.urls())
	}
}

// unusableEngineURLs are engine addresses the probe must not ask: ones the
// runtime refuses, and ones that inside its container name the container.
var unusableEngineURLs = []struct {
	name, env string
	key       string // the setting the problem names
	advice    string // the address to use instead, "" when none can be built
}{
	{name: "a LAN address", env: "TURING_LOCAL_ENGINE_URL=http://192.168.1.5:8000/v1\n", key: "TURING_LOCAL_ENGINE_URL"},
	{name: "credentials", env: "TURING_LOCAL_ENGINE_URL=http://user:CANARY-URLPASS@host.docker.internal:8000/v1\n", key: "TURING_LOCAL_ENGINE_URL"},
	{name: "a query", env: "TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1?token=CANARY-QUERY\n", key: "TURING_LOCAL_ENGINE_URL"},
	{name: "a fragment", env: "TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1#CANARY-FRAGMENT\n", key: "TURING_LOCAL_ENGINE_URL"},
	{name: "localhost", env: "TURING_LOCAL_ENGINE_URL=http://localhost:8000/v1\n", key: "TURING_LOCAL_ENGINE_URL", advice: "http://host.docker.internal:8000/v1"},
	{name: "localhost in capitals", env: "TURING_LOCAL_ENGINE_URL=http://LOCALHOST:8000/v1\n", key: "TURING_LOCAL_ENGINE_URL", advice: "http://host.docker.internal:8000/v1"},
	{name: "127.0.0.1", env: "TURING_LOCAL_ENGINE_URL=http://127.0.0.1:8000/v1\n", key: "TURING_LOCAL_ENGINE_URL", advice: "http://host.docker.internal:8000/v1"},
	{name: "another loopback address", env: "TURING_LOCAL_ENGINE_URL=http://127.0.0.2:8000/v1\n", key: "TURING_LOCAL_ENGINE_URL", advice: "http://host.docker.internal:8000/v1"},
	{name: "IPv6 loopback", env: "TURING_LOCAL_ENGINE_URL=http://[::1]:8000/v1\n", key: "TURING_LOCAL_ENGINE_URL", advice: "http://host.docker.internal:8000/v1"},
}

// serveEverywhere has an engine answer at every address an unusable URL
// could lead to, so a probe that asked would succeed.
func (f *fakeEngine) serveEverywhere() {
	for _, address := range []string{"192.168.1.5", "host.docker.internal", "localhost", "LOCALHOST", "127.0.0.1", "127.0.0.2", "[::1]", "172.17.0.1"} {
		f.serve(address+":8000", 200, openAIModels)
		f.serve(address+":11434", 200, ollamaTags)
	}
}

func TestEngineProbeNeverAsksAURLTheRuntimeRefusesOrCannotReach(t *testing.T) {
	for _, tt := range unusableEngineURLs {
		for _, goos := range []string{"linux", "darwin"} {
			t.Run(tt.name+" on "+goos, func(t *testing.T) {
				h := newHarness(t)
				h.GOOS = goos
				h.gateway = "172.17.0.1"
				h.engine.serveEverywhere()

				got := h.engineProbe(context.Background(), dotEnv(t,
					"TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_API_KEY=CANARY-ENGINE\n"+tt.env))

				if got.ok() {
					t.Fatalf("result = %+v, want a problem", got)
				}
				if len(h.engine.requests) != 0 {
					t.Fatalf("asked %q", h.engine.urls())
				}
				if !strings.Contains(got.Problem, tt.key) || !strings.Contains(got.Problem, "./turing config set TURING_LOCAL_ENGINE_URL") {
					t.Fatalf("problem = %q, want it to name %s and how to set the URL", got.Problem, tt.key)
				}
				if tt.advice != "" && !strings.Contains(got.Problem, "inside the runtime's container") {
					t.Fatalf("problem = %q, want it to say why the containers cannot reach that address", got.Problem)
				}
				if tt.advice != "" && !strings.Contains(got.Problem, "./turing config set TURING_LOCAL_ENGINE_URL "+tt.advice) {
					t.Fatalf("problem = %q, want the address to use, %s", got.Problem, tt.advice)
				}
				if shown := got.URL + got.Problem + got.Warning; strings.Contains(shown, "CANARY") {
					t.Fatalf("the result shows part of the URL or the key: %+v", got)
				}
			})
		}
	}
}

func TestEngineProbeNeverAsksALoopbackOllamaURLUnderItsOldName(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.engine.serveEverywhere()

	got := h.engineProbe(context.Background(), dotEnv(t, "OLLAMA_BASE_URL=http://localhost:11434\n"))

	if got.ok() || len(h.engine.requests) != 0 {
		t.Fatalf("result = %+v, asked %q; want a problem and no request", got, h.engine.urls())
	}
	if !strings.Contains(got.Problem, "OLLAMA_BASE_URL") || !strings.Contains(got.Problem, "./turing config set TURING_LOCAL_ENGINE_URL http://host.docker.internal:11434") {
		t.Fatalf("problem = %q, want the old name and the new setting with host.docker.internal", got.Problem)
	}
}

func TestEngineProbeRefusesSettingsTheServicesRefuse(t *testing.T) {
	tests := []struct{ name, env, want string }{
		{"an unknown engine", "TURING_LOCAL_ENGINE=lmstudio\n", "TURING_LOCAL_ENGINE must be ollama or openai"},
		{"openai with no URL", "TURING_LOCAL_ENGINE=openai\n", "TURING_LOCAL_ENGINE_URL is required when TURING_LOCAL_ENGINE is openai"},
		{"openai with only the old Ollama URL", "TURING_LOCAL_ENGINE=openai\nOLLAMA_BASE_URL=http://host.docker.internal:11434\n", "TURING_LOCAL_ENGINE_URL is required"},
		{"a URL that is not one", "TURING_LOCAL_ENGINE_URL=host.docker.internal:11434\n", "TURING_LOCAL_ENGINE_URL"},
		{"a URL with no host", "TURING_LOCAL_ENGINE_URL=http://:11434\n", "TURING_LOCAL_ENGINE_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.GOOS = "darwin"

			got := h.engineProbe(context.Background(), dotEnv(t, tt.env))

			if got.ok() || !strings.Contains(got.Problem, tt.want) {
				t.Fatalf("result = %+v, want a problem containing %q", got, tt.want)
			}
			if len(h.engine.requests) != 0 {
				t.Fatalf("asked %q for settings that cannot be used", h.engine.urls())
			}
		})
	}
}

func TestEngineProbeNeverShowsCredentialsInTheURL(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"

	got := h.engineProbe(context.Background(), dotEnv(t, "TURING_LOCAL_ENGINE_URL=http://user:CANARY-URLPASS@host.docker.internal:11434\n"))

	if got.ok() || len(h.engine.requests) != 0 {
		t.Fatalf("result = %+v, asked %q; want a problem and no request", got, h.engine.urls())
	}
	if shown := got.URL + got.Problem + got.Warning; strings.Contains(shown, "CANARY-URLPASS") {
		t.Fatalf("the result shows the URL's password: %+v", got)
	}
}

func TestEngineProbeWorksThroughTheRealClientOSEnvBuilds(t *testing.T) {
	var asked string
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(ollamaTags))
		case "/v1/models":
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}
	}))
	t.Cleanup(engine.Close)
	h := newHarness(t)
	h.GOOS = "darwin"
	h.EngineClient = OSEnv(h.BackendDir).EngineClient
	// On macOS host.docker.internal is asked at 127.0.0.1, where the server listens.
	at := "http://host.docker.internal:" + strings.TrimPrefix(engine.URL, "http://127.0.0.1:")

	got := h.engineProbe(context.Background(), dotEnv(t, "TURING_LOCAL_ENGINE_URL="+at+"\n"))
	if !got.ok() || len(got.Models) != 3 || asked != "/api/tags" {
		t.Fatalf("result = %+v, asked %q", got, asked)
	}

	got = h.engineProbe(context.Background(), dotEnv(t, "TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL="+at+"/v1\n"))
	if got.ok() || !strings.Contains(got.Problem, "HTTP 302") {
		t.Fatalf("result = %+v, want a redirect reported rather than followed", got)
	}
}

func TestOSEnvAsksAnEngineForThreeSeconds(t *testing.T) {
	env := OSEnv(t.TempDir())
	if env.EngineClient == nil || env.EngineClient.Timeout != 3*time.Second {
		t.Fatalf("EngineClient = %+v, want a 3 s timeout", env.EngineClient)
	}
	if env.BridgeGateway == nil {
		t.Fatal("OSEnv has no bridge gateway lookup")
	}
}

func TestOSEnvAsksAnEngineDirectlyAndNeverThroughAProxy(t *testing.T) {
	client := OSEnv(t.TempDir()).EngineClient
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("EngineClient.Transport = %T, want its own *http.Transport: the default one sends a request through HTTP_PROXY", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("EngineClient's transport has a Proxy function; it must be nil, or the engine's key can reach a proxy")
	}
	if client.CheckRedirect == nil {
		t.Fatal("EngineClient follows redirects")
	}
}

// proxyChildEnv marks the child process TestOSEnvEngineProbeNeverSendsTheKeyToAnInheritedProxy starts.
const proxyChildEnv = "TURING_TEST_ENGINE_PROXY_CHILD"

// The probe runs in a child process because net/http reads the proxy
// variables once per process: setting HTTP_PROXY in this one proves nothing.
func TestOSEnvEngineProbeNeverSendsTheKeyToAnInheritedProxy(t *testing.T) {
	if os.Getenv(proxyChildEnv) == "1" {
		probeWithTheOSEnvClient(t)
		return
	}
	var mu sync.Mutex
	var asked []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.String()+" Authorization="+r.Header.Get("Authorization"))
		mu.Unlock()
		_, _ = w.Write([]byte(openAIModels))
	}))
	t.Cleanup(proxy.Close)

	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1")
	child.Env = append(withoutProxySettings(os.Environ()),
		proxyChildEnv+"=1", "HTTP_PROXY="+proxy.URL, "http_proxy="+proxy.URL, "HTTPS_PROXY="+proxy.URL)
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe in the child process failed: %v\n%s", err, output)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) > 0 {
		t.Fatalf("the proxy in HTTP_PROXY was asked %q", asked)
	}
	if strings.Contains(string(output), "CANARY-PROXY") {
		t.Fatalf("the child printed the engine's key:\n%s", output)
	}
}

// probeWithTheOSEnvClient asks an engine at a gateway that is not loopback,
// which Go would send through a proxy, with OSEnv's own client.
func probeWithTheOSEnvClient(t *testing.T) {
	h := newHarness(t)
	// TEST-NET-1: never answers, and is not exempt from a proxy as loopback is.
	h.gateway = "192.0.2.1"
	client := *OSEnv(h.BackendDir).EngineClient
	client.Timeout = 300 * time.Millisecond
	h.EngineClient = &client

	h.engineProbe(context.Background(), dotEnv(t,
		"TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1\nTURING_LOCAL_ENGINE_API_KEY=CANARY-PROXY\n"))
}

// withoutProxySettings drops what would change how net/http picks a proxy,
// REQUEST_METHOD included: under CGI it ignores HTTP_PROXY.
func withoutProxySettings(environ []string) []string {
	var kept []string
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "REQUEST_METHOD", proxyChildEnv:
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

// bridgeInspect is the lookup's command; the trailing space in the template
// keeps the gateways of a dual-stack bridge apart.
var bridgeInspect = []string{"network", "inspect", "bridge", "--format", "{{range .IPAM.Config}}{{.Gateway}} {{end}}"}

func TestDockerBridgeGatewayReadsTheBridgeNetwork(t *testing.T) {
	tests := []struct {
		name  string
		reply fakeReply
		want  string
		fail  string
	}{
		{name: "an address", reply: fakeReply{stdout: "172.17.0.1 \n"}, want: "172.17.0.1"},
		{name: "a dual-stack bridge takes the IPv4 gateway", reply: fakeReply{stdout: "172.17.0.1 fd00::1 \n"}, want: "172.17.0.1"},
		{name: "an IPv4 gateway listed after the IPv6 one", reply: fakeReply{stdout: "fd00::1 172.17.0.1 \n"}, want: "172.17.0.1"},
		{name: "a subnet with no gateway between two", reply: fakeReply{stdout: " 172.18.0.1 \n"}, want: "172.18.0.1"},
		{name: "only an IPv6 gateway", reply: fakeReply{stdout: "fd00::1 \n"}, fail: "no IPv4 gateway"},
		{name: "docker refuses", reply: fakeReply{stderr: "Cannot connect to the Docker daemon\n", code: 1}, fail: "Cannot connect to the Docker daemon"},
		{name: "docker cannot run", reply: fakeReply{err: errors.New("exec: docker: not found")}, fail: "docker: not found"},
		{name: "no gateway", reply: fakeReply{stdout: " \n"}, fail: "no gateway"},
		{name: "not an address", reply: fakeReply{stdout: "172.17.0.1fd00::1 \n"}, fail: "172.17.0.1fd00::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{t: t}
			runner.on(dockerCall(bridgeInspect...), tt.reply)
			lookup := dockerBridgeGateway(runner, "/backend", []string{"PATH=/bin"})

			got, err := lookup(context.Background())

			if tt.fail != "" {
				if err == nil || !strings.Contains(err.Error(), tt.fail) {
					t.Fatalf("lookup = %q, %v; want an error containing %q", got, err, tt.fail)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("lookup = %q, %v; want %q", got, err, tt.want)
			}
			if call := runner.calls[0]; call.Dir != "/backend" || !slices.Equal(call.Env, []string{"PATH=/bin"}) {
				t.Fatalf("ran in %q with %q", call.Dir, call.Env)
			}
		})
	}
}
