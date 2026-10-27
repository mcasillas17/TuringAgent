package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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
	for _, want := range []string{"127.0.0.1", "--host 172.17.0.1"} {
		if !strings.Contains(got.Warning, want) {
			t.Fatalf("warning = %q, want it to mention %q", got.Warning, want)
		}
	}
	if !strings.Contains(got.Problem, "172.17.0.1:11434") {
		t.Fatalf("problem = %q, want it to name the gateway address", got.Problem)
	}
	want := []string{"http://172.17.0.1:11434/api/tags", "http://127.0.0.1:11434/api/tags"}
	if !slices.Equal(h.engine.urls(), want) {
		t.Fatalf("asked %q, want the gateway then loopback %q", h.engine.urls(), want)
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

func TestEngineProbeAsksAnotherHostAsConfigured(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			h := newHarness(t)
			h.GOOS = goos
			h.engine.serve("192.168.1.5:8000", 200, openAIModels)

			got := h.engineProbe(context.Background(), dotEnv(t,
				"TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL=http://192.168.1.5:8000/v1\n"))

			if !got.ok() {
				t.Fatalf("result = %+v", got)
			}
			if want := []string{"http://192.168.1.5:8000/v1/models"}; !slices.Equal(h.engine.urls(), want) {
				t.Fatalf("asked %q, want %q", h.engine.urls(), want)
			}
			if h.gatewayAsked != 0 {
				t.Fatal("asked Docker for the bridge gateway for a host that is not host.docker.internal")
			}
		})
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

	if got.ok() {
		t.Fatalf("result = %+v, want a refused connection", got)
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
	h.EngineClient = OSEnv(h.BackendDir).EngineClient

	got := h.engineProbe(context.Background(), dotEnv(t, "TURING_LOCAL_ENGINE_URL="+engine.URL+"\n"))
	if !got.ok() || len(got.Models) != 3 || asked != "/api/tags" {
		t.Fatalf("result = %+v, asked %q", got, asked)
	}

	got = h.engineProbe(context.Background(), dotEnv(t, "TURING_LOCAL_ENGINE=openai\nTURING_LOCAL_ENGINE_URL="+engine.URL+"/v1\n"))
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

func TestDockerBridgeGatewayReadsTheBridgeNetwork(t *testing.T) {
	inspect := []string{"network", "inspect", "bridge", "--format", "{{range .IPAM.Config}}{{.Gateway}}{{end}}"}
	tests := []struct {
		name  string
		reply fakeReply
		want  string
		fail  string
	}{
		{name: "an address", reply: fakeReply{stdout: "172.17.0.1\n"}, want: "172.17.0.1"},
		{name: "an IPv6 address", reply: fakeReply{stdout: "fd00::1\n"}, want: "fd00::1"},
		{name: "docker refuses", reply: fakeReply{stderr: "Cannot connect to the Docker daemon\n", code: 1}, fail: "Cannot connect to the Docker daemon"},
		{name: "docker cannot run", reply: fakeReply{err: errors.New("exec: docker: not found")}, fail: "docker: not found"},
		{name: "no gateway", reply: fakeReply{stdout: "\n"}, fail: "no gateway"},
		{name: "not an address", reply: fakeReply{stdout: "172.17.0.1fd00::1\n"}, fail: "172.17.0.1fd00::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{t: t}
			runner.on(dockerCall(inspect...), tt.reply)
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
