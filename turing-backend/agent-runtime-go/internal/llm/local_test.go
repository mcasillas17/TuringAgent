package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
)

// An engine label is shown to the user and nothing else, so it must be short,
// printable text.
func TestValidateEngineLabel(t *testing.T) {
	if err := ValidateEngineLabel("ds4"); err != nil {
		t.Fatalf("ds4: %v", err)
	}
	if err := ValidateEngineLabel(strings.Repeat("a", 64)); err != nil {
		t.Fatalf("64 bytes: %v", err)
	}
	for name, label := range map[string]string{
		"empty": "", "newline": "a\nb", "65 bytes": strings.Repeat("a", 65), "invalid UTF-8": "\xff", "escape": "a\x1b[31m",
	} {
		if err := ValidateEngineLabel(label); err == nil {
			t.Fatalf("%s: accepted %q", name, label)
		}
	}
}

// fakeLocalEngine is an OpenAI-compatible server on this machine that streams
// the given SSE data lines and records the one request it was sent.
type fakeLocalEngine struct {
	mu            sync.Mutex
	path          string
	authorization string
	body          map[string]any
}

func (f *fakeLocalEngine) serve(t *testing.T, chunks ...string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.path, f.authorization = r.URL.Path, r.Header.Get("authorization")
		_ = json.Unmarshal(raw, &f.body)
		f.mu.Unlock()
		w.Header().Set("content-type", "text/event-stream")
		for _, chunk := range chunks {
			fmt.Fprint(w, "data: "+chunk+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server.URL + "/v1"
}

func newLocalOpenAI(t *testing.T, baseURL, apiKey string) Provider {
	t.Helper()
	provider, err := NewLocal(LocalOptions{
		Engine: LocalEngineOpenAI, BaseURL: baseURL, APIKey: apiKey,
		ContextWindowTokens: 8192, MaxOutputTokens: 512,
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestNewLocalBuildsOllamaForTheOllamaEngine(t *testing.T) {
	provider, err := NewLocal(LocalOptions{
		Engine: LocalEngineOllama, BaseURL: "http://host.docker.internal:11434", KeepAlive: "2m",
		ContextWindowTokens: 8192, MaxOutputTokens: 512,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := provider.(*Ollama); !ok || provider.ID() != egress.LocalProvider {
		t.Fatalf("provider = %T %q, want Ollama as the local provider", provider, provider.ID())
	}
}

// The local route needs no consent because its engine is on this machine, so
// the OpenAI-compatible engine must never present a remote endpoint: the
// runtime's egress check reads exactly that.
func TestTheLocalOpenAIEngineIsNotAnEgressProvider(t *testing.T) {
	provider := newLocalOpenAI(t, "http://localhost:8000/v1", "")
	if _, ok := provider.(EgressProvider); ok {
		t.Fatalf("%T exposes an egress endpoint", provider)
	}
	if provider.ID() != egress.LocalProvider || provider.ContextWindowTokens() != 8192 || provider.MaxOutputTokens() != 512 {
		t.Fatalf("provider = %q %d %d", provider.ID(), provider.ContextWindowTokens(), provider.MaxOutputTokens())
	}
}

func TestNewLocalRefusesANonLocalURL(t *testing.T) {
	for _, engine := range []string{LocalEngineOllama, LocalEngineOpenAI} {
		if _, err := NewLocal(LocalOptions{
			Engine: engine, BaseURL: "http://192.168.1.20:8000/v1", ContextWindowTokens: 8192, MaxOutputTokens: 512,
		}); err == nil {
			t.Fatalf("%s accepted a LAN address", engine)
		}
	}
}

func TestNewLocalRefusesAnUnknownEngineOrAKeyForOllama(t *testing.T) {
	if _, err := NewLocal(LocalOptions{Engine: "vllm", BaseURL: "http://localhost:8000/v1", ContextWindowTokens: 8192, MaxOutputTokens: 512}); err == nil {
		t.Fatal("an unknown engine was accepted")
	}
	if _, err := NewLocal(LocalOptions{
		Engine: LocalEngineOllama, BaseURL: "http://localhost:11434", APIKey: "k", ContextWindowTokens: 8192, MaxOutputTokens: 512,
	}); err == nil {
		t.Fatal("an API key was accepted for Ollama")
	}
}

func TestTheLocalOpenAIEngineStreamsAToolCall(t *testing.T) {
	for _, apiKey := range []string{"dsv4-local", ""} {
		engine := &fakeLocalEngine{}
		url := engine.serve(t,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"system.time","arguments":"{}"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		)
		events, err := newLocalOpenAI(t, url, apiKey).StreamChat(context.Background(), ChatRequest{
			Model: "deepseek-chat", Messages: []ChatMessage{{Role: "user", Content: "time?"}},
			Tools: []ToolDefinition{{Name: "system.time", Parameters: map[string]any{"type": "object"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		var calls []ToolCall
		for _, event := range collectEvents(events) {
			calls = append(calls, event.ToolCalls...)
		}
		if len(calls) != 1 || calls[0].Name != "system.time" {
			t.Fatalf("tool calls = %+v, want system.time", calls)
		}
		engine.mu.Lock()
		_, maxTokens := engine.body["max_tokens"]
		_, maxCompletion := engine.body["max_completion_tokens"]
		wantAuth := ""
		if apiKey != "" {
			wantAuth = "Bearer " + apiKey
		}
		if engine.path != "/v1/chat/completions" || !maxTokens || maxCompletion || engine.authorization != wantAuth {
			t.Fatalf("request = %s auth %q body %v", engine.path, engine.authorization, engine.body)
		}
		engine.mu.Unlock()
	}
}

func deltaText(events []StreamEvent) string {
	var text strings.Builder
	for _, event := range events {
		if event.Type == "delta" {
			text.WriteString(event.Text)
		}
	}
	return text.String()
}

// A thinking model's reasoning arrives apart from its answer; Turing neither
// shows nor stores it.
func TestTheLocalOpenAIEngineDropsReasoning(t *testing.T) {
	engine := &fakeLocalEngine{}
	url := engine.serve(t,
		`{"choices":[{"index":0,"delta":{"reasoning_content":"hmm"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":"stop"}]}`,
	)
	events, err := newLocalOpenAI(t, url, "").StreamChat(context.Background(), ChatRequest{Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if text := deltaText(collectEvents(events)); text != "Hi" {
		t.Fatalf("text = %q, want only the answer", text)
	}
}

// A thinking model that spends its whole budget reasoning stops on length
// with nothing to show: that is the ordinary length stop, not an error.
func TestTheLocalOpenAIEngineReportsALengthStopWithNoText(t *testing.T) {
	engine := &fakeLocalEngine{}
	url := engine.serve(t,
		`{"choices":[{"index":0,"delta":{"reasoning_content":"still thinking"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`,
	)
	events, err := newLocalOpenAI(t, url, "").StreamChat(context.Background(), ChatRequest{Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	completed := 0
	for _, event := range got {
		if event.Type == "error" {
			t.Fatalf("events = %+v, want no error", got)
		}
		if event.Type == "completed" && event.FinishReason == "length" {
			completed++
		}
	}
	if text := deltaText(got); text != "" || completed != 1 {
		t.Fatalf("events = %+v, want one length completion and no text", got)
	}
}
