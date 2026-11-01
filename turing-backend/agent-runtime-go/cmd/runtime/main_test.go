package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/worker"
)

func TestAdvertisedModelsReflectConfiguredProvidersAndContextLimits(t *testing.T) {
	cfg := config.Config{
		LocalModel: "qwen", LocalContextWindowTokens: 32768, LocalEngineLabel: "ds4",
		OpenAIModel: "gpt", OpenAIContextWindowTokens: 128000,
	}
	models := advertisedModels(cfg)
	if len(models) != 1 || models[0].GetProvider() != turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA ||
		models[0].GetModel() != "qwen" || models[0].GetMaxContextTokens() != 32768 || models[0].GetEngine() != "ds4" {
		t.Fatalf("models without OpenAI key = %+v", models)
	}
	cfg.OpenAIAPIKey = "configured"
	models = advertisedModels(cfg)
	if len(models) != 2 || models[1].GetProvider() != turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE ||
		models[1].GetModel() != "gpt" || models[1].GetMaxContextTokens() != 128000 || models[1].GetEngine() != "" {
		t.Fatalf("models with OpenAI key = %+v", models)
	}
}

// The Ollama engine sends the configured keep-alive on every request.
func TestBuildProvidersPassesTheKeepAliveToOllama(t *testing.T) {
	keepAlive := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			KeepAlive json.RawMessage `json:"keep_alive"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		keepAlive <- string(body.KeepAlive)
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"ok"},"done":true,"done_reason":"stop"}`+"\n")
	}))
	t.Cleanup(server.Close)
	providers, err := buildProviders(config.Config{
		LocalEngine: llm.LocalEngineOllama, LocalEngineURL: server.URL, OllamaKeepAlive: "30s",
		LocalContextWindowTokens: 8192, LocalMaxOutputTokens: 512,
	}, server.Client(), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	events, err := providers[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA].StreamChat(context.Background(), llm.ChatRequest{Model: "llama3.2"})
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	if got := <-keepAlive; got != `"30s"` {
		t.Fatalf("keep_alive = %s, want \"30s\"", got)
	}
}

// The worker's local provider and the one check-engine probes are built by one
// function, so an option added to the worker reaches the check too: both send
// the same request for the same configuration.
func TestTheWorkersLocalProviderSendsWhatNewLocalProviderSends(t *testing.T) {
	type sent struct{ authorization, body string }
	for _, engine := range []string{llm.LocalEngineOllama, llm.LocalEngineOpenAI} {
		t.Run(engine, func(t *testing.T) {
			var seen []sent
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				seen = append(seen, sent{r.Header.Get("Authorization"), string(body)})
				if engine == llm.LocalEngineOllama {
					_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"ok"},"done":true,"done_reason":"stop"}`+"\n")
					return
				}
				_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
			}))
			t.Cleanup(server.Close)
			cfg := config.Config{
				LocalEngine: engine, LocalEngineURL: server.URL, LocalEngineAPIKey: "k",
				OllamaKeepAlive: "7m", LocalContextWindowTokens: 8192, LocalMaxOutputTokens: 512,
			}
			if engine == llm.LocalEngineOllama {
				cfg.LocalEngineAPIKey = ""
			} else {
				cfg.LocalEngineURL += "/v1"
			}
			providers, err := buildProviders(cfg, server.Client(), server.Client())
			if err != nil {
				t.Fatal(err)
			}
			local, err := newLocalProvider(cfg, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			for _, provider := range []llm.Provider{providers[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA], local} {
				events, err := provider.StreamChat(context.Background(), llm.ChatRequest{
					Model: "m", Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}}, MaxTokens: 64,
				})
				if err != nil {
					t.Fatal(err)
				}
				for range events {
				}
			}
			if len(seen) != 2 || seen[0] != seen[1] {
				t.Fatalf("buildProviders' local provider and newLocalProvider sent %+v, want the same request", seen)
			}
			if engine == llm.LocalEngineOllama && !strings.Contains(seen[0].body, `"keep_alive":"7m"`) {
				t.Fatalf("request body = %s, want the configured keep_alive", seen[0].body)
			}
			if engine == llm.LocalEngineOpenAI && seen[0].authorization != "Bearer k" {
				t.Fatalf("Authorization = %q, want the configured key", seen[0].authorization)
			}
		})
	}
}

func TestNewLocalProviderNamesTheLocalEngineWhenItCannotBuildOne(t *testing.T) {
	cfg := config.Config{LocalEngine: llm.LocalEngineOllama, LocalEngineURL: "https://api.example.com"}
	_, err := newLocalProvider(cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "configure the local engine") {
		t.Fatalf("newLocalProvider = %v, want an error naming the local engine", err)
	}
	if _, err := buildProviders(cfg, nil, nil); err == nil || !strings.Contains(err.Error(), "configure the local engine") {
		t.Fatalf("buildProviders = %v, want the same error", err)
	}
}

// Each deprecated setting the configuration fell back to is logged once at
// startup, naming its replacement.
func TestReportDeprecatedKeysLogsEachOnce(t *testing.T) {
	var logged strings.Builder
	previous := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(previous)
	reportDeprecatedKeys(config.Config{DeprecatedKeys: []string{
		"OLLAMA_MODEL is deprecated; set TURING_LOCAL_MODEL instead",
	}})
	if strings.Count(logged.String(), "OLLAMA_MODEL is deprecated; set TURING_LOCAL_MODEL instead") != 1 {
		t.Fatalf("log = %q", logged.String())
	}
}

// The local engine and the remote provider are two providers: an
// OpenAI-compatible local engine never takes the remote one's place or its
// identity as a destination data leaves for.
func TestTheLocalEngineAndTheRemoteProviderStaySeparate(t *testing.T) {
	cfg := config.Config{
		LocalEngine: llm.LocalEngineOpenAI, LocalEngineURL: "http://host.docker.internal:8000/v1",
		LocalContextWindowTokens: 8192, LocalMaxOutputTokens: 512,
		OpenAIAPIKey: "k", OpenAIBaseURL: "https://api.openai.com/v1",
		OpenAIContextWindowTokens: 8192, OpenAIMaxOutputTokens: 512,
	}
	providers, err := buildProviders(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, remote := providers[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA].(llm.EgressProvider); remote {
		t.Fatalf("the local provider %T exposes an egress endpoint", providers[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA])
	}
	remote, ok := providers[turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE].(llm.EgressProvider)
	if !ok || remote.EgressEndpoint() != "https://api.openai.com/v1" {
		t.Fatalf("remote provider = %+v", providers[turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE])
	}
}

// The worker reaches its local engine as check-engine does, never through a
// proxy, so what the check proves is the path a chat takes; the remote model
// may need the proxy the container is given.
func TestTheWorkersLocalEngineClientIgnoresProxiesAndTheRemoteOneDoesNot(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.invalid:3128")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:3128")
	local, remote := workerClients()
	requireNoProxy(t, local)
	if remote != http.DefaultClient {
		t.Fatalf("the remote model's client is %+v, want http.DefaultClient, which honours the container's proxy", remote)
	}
}

type recordingTransport struct {
	mu    sync.Mutex
	hosts []string
}

func (r *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.hosts = append(r.hosts, request.URL.Host)
	r.mu.Unlock()
	return nil, errors.New("recorded")
}

func (r *recordingTransport) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.hosts)
}

// Each provider buildProviders makes sends its requests through its own
// client: the local engine through the local one, the remote model through the
// remote one.
func TestBuildProvidersSendsEachRouteThroughItsOwnClient(t *testing.T) {
	localTransport, remoteTransport := &recordingTransport{}, &recordingTransport{}
	cfg := config.Config{
		LocalEngine: llm.LocalEngineOllama, LocalEngineURL: "http://host.docker.internal:11434",
		LocalContextWindowTokens: 8192, LocalMaxOutputTokens: 512,
		OpenAIAPIKey: "k", OpenAIBaseURL: "https://api.openai.com/v1",
		OpenAIContextWindowTokens: 8192, OpenAIMaxOutputTokens: 512,
	}
	providers, err := buildProviders(cfg, &http.Client{Transport: localTransport}, &http.Client{Transport: remoteTransport})
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range providers {
		events, err := provider.StreamChat(context.Background(), llm.ChatRequest{
			Model: "m", Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}}, MaxTokens: 64,
		})
		if err != nil {
			continue
		}
		for range events {
		}
	}
	if got := localTransport.seen(); len(got) == 0 || slices.ContainsFunc(got, func(host string) bool { return host != "host.docker.internal:11434" }) {
		t.Fatalf("the local client carried requests to %q, want only the local engine's", got)
	}
	if got := remoteTransport.seen(); len(got) == 0 || slices.ContainsFunc(got, func(host string) bool { return host != "api.openai.com" }) {
		t.Fatalf("the remote client carried requests to %q, want only the remote model's", got)
	}
}

func TestAgentCredentialRefsAdvertiseSortedNamesWithoutSecrets(t *testing.T) {
	keys := map[string]string{"openai": "secret-openai", "claude": "secret-claude"}
	if got := agentCredentialRefs(keys); !slices.Equal(got, []string{"claude", "openai"}) {
		t.Fatalf("credential refs = %v, want sorted names only", got)
	}
}

func TestShouldReconnect(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	for name, tc := range map[string]struct {
		ctx  context.Context
		err  error
		want bool
	}{
		// Transport problems are exactly what the loop exists for.
		"stream dropped":          {context.Background(), errors.New("rpc error: code = Unavailable"), true},
		"orchestrator not up yet": {context.Background(), errors.New("connection refused"), true},
		// Run returns nil when the orchestrator sent a shutdown command. Coming
		// back would fight an operator who just asked us to stop.
		"orchestrator asked us to stop": {context.Background(), nil, false},
		// The process was signalled; reconnecting would ignore SIGTERM.
		"signalled mid-stream":     {cancelled, errors.New("rpc error: code = Canceled"), false},
		"signalled with nil error": {cancelled, nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := shouldReconnect(tc.ctx, tc.err); got != tc.want {
				t.Fatalf("shouldReconnect = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	delays := []time.Duration{initialBackoff}
	for len(delays) < 12 {
		delays = append(delays, nextBackoff(delays[len(delays)-1]))
	}
	if delays[0] != initialBackoff {
		t.Fatalf("first delay = %v, want %v", delays[0], initialBackoff)
	}
	for i := 1; i < len(delays); i++ {
		if delays[i] < delays[i-1] {
			t.Fatalf("backoff decreased at %d: %v", i, delays)
		}
	}
	if last := delays[len(delays)-1]; last != maxBackoff {
		t.Fatalf("backoff did not cap at %v: %v", maxBackoff, last)
	}
}

// The delay must be interruptible. Writing time.Sleep here instead of a select
// on ctx.Done() would make SIGTERM wait out a 30s backoff before exiting, which
// Docker turns into a SIGKILL.
func TestServeReturnsPromptlyWhenSignalledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := make(chan struct{}, 4)
	run := func(context.Context) error {
		attempts <- struct{}{}
		return errors.New("stream dropped")
	}

	done := make(chan error, 1)
	// Real time.After here: the point is that cancellation interrupts a genuine
	// wait rather than the loop happening to come round again.
	go func() { done <- serveWith(ctx, run, healthyStreamDuration, time.After) }()

	<-attempts // the first attempt failed; serve is now sleeping out its backoff
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil on shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return promptly after cancellation")
	}
}

func TestServeRetriesUntilShutdownRequested(t *testing.T) {
	var calls int
	run := func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("stream dropped")
		}
		return nil // the orchestrator asked us to stop
	}
	if err := serveWith(context.Background(), run, healthyStreamDuration, instantSleep); err != nil {
		t.Fatalf("serve returned %v, want nil", err)
	}
	if calls != 3 {
		t.Fatalf("run called %d times, want 3 (two failures then a clean stop)", calls)
	}
}

// Jitter must stay within [d/2, d]: a floor keeps a tight failure loop from
// spinning, and the ceiling keeps the cap meaningful.
func TestJitterStaysWithinHalfToFullDelay(t *testing.T) {
	const d = 8 * time.Second
	var sawBelowFull bool
	for i := 0; i < 200; i++ {
		got := jitter(d)
		if got < d/2 || got > d {
			t.Fatalf("jitter(%v) = %v, want within [%v, %v]", d, got, d/2, d)
		}
		if got < d {
			sawBelowFull = true
		}
	}
	if !sawBelowFull {
		t.Fatal("jitter never varied below the full delay")
	}
	if jitter(0) != 0 {
		t.Fatal("jitter(0) must be 0")
	}
}

// instantSleep makes the loop's delay a no-op so timing behaviour can be
// asserted without waiting real seconds.
func instantSleep(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

// recordingSleep captures each requested delay and returns immediately.
func recordingSleep(into *[]time.Duration) func(time.Duration) <-chan time.Time {
	return func(d time.Duration) <-chan time.Time {
		*into = append(*into, d)
		return instantSleep(d)
	}
}

// Without this, deleting `backoff = nextBackoff(backoff)` from the loop leaves
// the suite green while the runtime retries at the initial delay forever.
func TestServeGrowsTheDelayBetweenFailedAttempts(t *testing.T) {
	var delays []time.Duration
	var calls int
	run := func(context.Context) error {
		calls++
		if calls > 4 {
			return nil
		}
		return errors.New("stream dropped")
	}
	if err := serveWith(context.Background(), run, time.Hour, recordingSleep(&delays)); err != nil {
		t.Fatal(err)
	}
	if len(delays) != 4 {
		t.Fatalf("recorded %d delays, want 4: %v", len(delays), delays)
	}
	// Jitter is [d/2, d], so consecutive samples can overlap; compare the ends.
	if delays[len(delays)-1] <= delays[0] {
		t.Fatalf("delay did not grow across attempts: %v", delays)
	}
}

// Without this, inverting or deleting the healthy-stream reset is invisible.
func TestServeResetsTheDelayAfterAHealthyStream(t *testing.T) {
	var delays []time.Duration
	var calls int
	run := func(context.Context) error {
		calls++
		if calls > 4 {
			return nil
		}
		return errors.New("stream dropped")
	}
	// healthyFor 0 means every attempt counts as healthy, so the delay must
	// never climb past the initial value.
	if err := serveWith(context.Background(), run, 0, recordingSleep(&delays)); err != nil {
		t.Fatal(err)
	}
	for i, d := range delays {
		if d > initialBackoff {
			t.Fatalf("delay %d = %v exceeded the initial backoff; the reset did not apply: %v", i, d, delays)
		}
	}
}

// A configuration Run rejects can never succeed on retry. Looping it would keep
// the process alive, never exit non-zero, and hide the fault from the operator.
func TestServeDoesNotRetryInvalidConfiguration(t *testing.T) {
	var calls int
	run := func(context.Context) error {
		calls++
		return fmt.Errorf("%w: max concurrent runs must be between 1 and 128", worker.ErrInvalidConfig)
	}
	var delays []time.Duration
	if err := serveWith(context.Background(), run, time.Hour, recordingSleep(&delays)); err != nil {
		t.Fatalf("serve returned %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("run called %d times, want 1 — an unretryable config error must not loop", calls)
	}
}
