package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/internal/servicecmd"
)

// How a fake engine answers a chat request. The no-tools request is step 3's
// and the request with a tool is step 4's, so each is set apart.
const (
	modeText      = "text"
	modeReasoning = "reasoning"
	modeError     = "error"
	modeHang      = "hang"
	modeStall     = "stall"
	modeCall      = "call"
	modeWrongName = "wrong-name"
)

const (
	ollamaModel = "qwen2.5:7b"
	openAIModel = "deepseek-chat"
)

var stepLines = []string{
	"1/4 reachable: ok\n",
	"2/4 model present: ok\n",
	"3/4 streaming: ok\n",
	"4/4 tool calling: ok\n",
}

var allFourSteps = strings.Join(stepLines, "") + "the engine is usable for Turing\n"

type fakeRequest struct {
	Method        string
	Path          string
	Authorization string
	Body          []byte
}

// fakeEngine speaks the wire format of an Ollama server or an
// OpenAI-compatible one, modelled on the provider tests in internal/llm.
type fakeEngine struct {
	t      *testing.T
	engine string
	model  string

	models     []string
	listBody   string
	listStatus int
	hangList   bool
	requireKey string
	stream     string
	tool       string

	server *httptest.Server
	done   chan struct{}

	mu       sync.Mutex
	requests []fakeRequest
}

func newFakeEngine(t *testing.T, engine string, models ...string) *fakeEngine {
	t.Helper()
	f := &fakeEngine{t: t, engine: engine, model: ollamaModel, stream: modeText, tool: modeCall, done: make(chan struct{})}
	if engine == llm.LocalEngineOpenAI {
		f.model = openAIModel
	}
	f.models = models
	if len(models) == 0 {
		f.models = []string{f.model}
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	// Runs before server.Close, so a handler blocked on a hang is released.
	t.Cleanup(func() { close(f.done) })
	return f
}

func (f *fakeEngine) url() string {
	if f.engine == llm.LocalEngineOpenAI {
		return f.server.URL + "/v1"
	}
	return f.server.URL
}

func (f *fakeEngine) paths() (version, listing, chat string) {
	if f.engine == llm.LocalEngineOpenAI {
		return "", "/v1/models", "/v1/chat/completions"
	}
	return "/api/version", "/api/tags", "/api/chat"
}

func (f *fakeEngine) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, fakeRequest{
		Method: r.Method, Path: r.URL.Path, Authorization: r.Header.Get("Authorization"), Body: body,
	})
	f.mu.Unlock()
	if f.requireKey != "" && r.Header.Get("Authorization") != "Bearer "+f.requireKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	version, listing, chat := f.paths()
	switch r.URL.Path {
	case version:
		if f.hangList {
			f.block(r)
			return
		}
		_, _ = io.WriteString(w, `{"version":"0.5.7"}`)
	case listing:
		if f.hangList {
			f.block(r)
			return
		}
		f.list(w)
	case chat:
		f.chat(w, r, body)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeEngine) block(r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-f.done:
	}
}

func (f *fakeEngine) list(w http.ResponseWriter) {
	if f.listStatus != 0 {
		http.Error(w, "list failed", f.listStatus)
		return
	}
	if f.listBody != "" {
		_, _ = io.WriteString(w, f.listBody)
		return
	}
	entries := make([]map[string]string, 0, len(f.models))
	for _, name := range f.models {
		if f.engine == llm.LocalEngineOpenAI {
			entries = append(entries, map[string]string{"id": name, "object": "model"})
		} else {
			entries = append(entries, map[string]string{"name": name})
		}
	}
	key := "models"
	if f.engine == llm.LocalEngineOpenAI {
		key = "data"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{key: entries})
}

func (f *fakeEngine) chat(w http.ResponseWriter, r *http.Request, body []byte) {
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	mode := f.stream
	if request["tools"] != nil {
		mode = f.tool
	}
	if mode == modeHang {
		f.block(r)
		return
	}
	chunks := f.chunks(mode)
	if mode == modeStall {
		chunks = chunks[:1]
	}
	flusher, _ := w.(http.Flusher)
	for _, chunk := range chunks {
		_, _ = io.WriteString(w, chunk)
		if flusher != nil {
			flusher.Flush()
		}
	}
	if mode == modeStall {
		f.block(r)
	}
}

func (f *fakeEngine) chunks(mode string) []string {
	if f.engine == llm.LocalEngineOpenAI {
		return openAIChunks(mode)
	}
	return ollamaChunks(mode)
}

func ollamaChunks(mode string) []string {
	const done = `{"done":true,"done_reason":"stop"}` + "\n"
	switch mode {
	case modeReasoning:
		return []string{`{"message":{"role":"assistant","content":"","thinking":"let me think"},"done":false}` + "\n", done}
	case modeError:
		return []string{`{"error":"the model failed to load"}` + "\n"}
	case modeCall, modeWrongName:
		name := "turing_engine_check"
		if mode == modeWrongName {
			name = "get_weather"
		}
		return []string{fmt.Sprintf(`{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":%q,"arguments":{"status":"ok"}}}]},"done":true,"done_reason":"stop"}`+"\n", name)}
	default:
		return []string{
			`{"message":{"role":"assistant","content":"rea"},"done":false}` + "\n",
			`{"message":{"role":"assistant","content":"dy"},"done":false}` + "\n",
			done,
		}
	}
}

func openAIChunks(mode string) []string {
	sse := func(data string) string { return "data: " + data + "\n\n" }
	const finish = `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	const done = "data: [DONE]\n\n"
	switch mode {
	case modeReasoning:
		return []string{sse(`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"let me think"}}]}`), sse(finish), done}
	case modeError:
		return []string{sse(`{"error":{"message":"the model failed to load","type":"server_error","code":"engine_failure"}}`)}
	case modeCall, modeWrongName:
		name := "turing_engine_check"
		if mode == modeWrongName {
			name = "get_weather"
		}
		return []string{
			sse(fmt.Sprintf(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":%q,"arguments":""}}]}}]}`, name)),
			sse(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"status\":\"ok\"}"}}]}}]}`),
			sse(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`),
			done,
		}
	default:
		return []string{
			sse(`{"choices":[{"index":0,"delta":{"role":"assistant","content":"rea"}}]}`),
			sse(`{"choices":[{"index":0,"delta":{"content":"dy"}}]}`),
			sse(finish),
			done,
		}
	}
}

func (f *fakeEngine) recorded() []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeRequest(nil), f.requests...)
}

// chatBodies are the decoded bodies of the chat requests, in order.
func (f *fakeEngine) chatBodies(t *testing.T) []map[string]any {
	t.Helper()
	_, _, chat := f.paths()
	var bodies []map[string]any
	for _, request := range f.recorded() {
		if request.Path != chat {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(request.Body, &body); err != nil {
			t.Fatalf("chat request body is not JSON: %v", err)
		}
		bodies = append(bodies, body)
	}
	return bodies
}

func eachEngine(t *testing.T, test func(t *testing.T, engine string)) {
	t.Helper()
	for _, engine := range []string{llm.LocalEngineOllama, llm.LocalEngineOpenAI} {
		t.Run(engine, func(t *testing.T) { test(t, engine) })
	}
}

// newEngineHarness is the offline harness for a runtime whose configured
// engine is the fake: an orchestrator listener nobody may dial, and the
// environment that points the runtime at the fake.
func newEngineHarness(t *testing.T, f *fakeEngine, extra ...string) (*offlineHarness, []string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	h := &offlineHarness{t: t, orchestrator: listener.(*net.TCPListener), engineURL: f.url()}
	environ := append(h.environ(), "TURING_LOCAL_ENGINE="+f.engine, "TURING_LOCAL_MODEL="+f.model)
	return h, append(environ, extra...)
}

func runCheckEngine(t *testing.T, f *fakeEngine, extra ...string) (int, string, string) {
	t.Helper()
	h, environ := newEngineHarness(t, f, extra...)
	var stdout, stderr bytes.Buffer
	code := servicecmd.Main([]string{"check-engine"}, environ, checks, failServe(t), &stdout, &stderr)
	h.assertNothingDialed()
	return code, stdout.String(), stderr.String()
}

func setStepTimeouts(t *testing.T, listing, completion time.Duration) {
	t.Helper()
	savedListing, savedCompletion := listingTimeout, completionTimeout
	listingTimeout, completionTimeout = listing, completion
	t.Cleanup(func() { listingTimeout, completionTimeout = savedListing, savedCompletion })
}

// requireStepFailure checks that the check passed exactly the steps before
// failedStep, and then said one sentence on stderr in the documented shape.
func requireStepFailure(t *testing.T, code int, stdout, stderr string, failedStep int, name string, wants ...string) {
	t.Helper()
	if code != 1 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q; want 1", code, stdout, stderr)
	}
	if want := strings.Join(stepLines[:failedStep-1], ""); stdout != want {
		t.Fatalf("stdout = %q, want the passed steps %q", stdout, want)
	}
	prefix := fmt.Sprintf("step %d/4 (%s) failed: ", failedStep, name)
	if !strings.HasPrefix(stderr, prefix) || strings.Count(stderr, "\n") != 1 || !strings.HasSuffix(stderr, "\n") {
		t.Fatalf("stderr = %q, want one line starting %q", stderr, prefix)
	}
	if !strings.Contains(strings.TrimPrefix(stderr, prefix), "; ") {
		t.Fatalf("stderr = %q, want the reason and what to change, separated by %q", stderr, "; ")
	}
	for _, want := range wants {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

func TestCheckEngineProvesAFakeEngineInFourSteps(t *testing.T) {
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		code, stdout, stderr := runCheckEngine(t, f)
		if code != 0 || stdout != allFourSteps || stderr != "" {
			t.Fatalf("code = %d, stdout = %q, stderr = %q; want 0 and %q", code, stdout, stderr, allFourSteps)
		}
	})
}

func TestCheckEngineFailsAtStepOneWhenNothingIsListening(t *testing.T) {
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		f.server.Close()
		code, stdout, stderr := runCheckEngine(t, f)
		requireStepFailure(t, code, stdout, stderr, 1, "reachable", f.url(), "TURING_LOCAL_ENGINE_URL")
	})
}

func TestCheckEngineNamesTheAPIKeyWhenTheEngineAnswers401(t *testing.T) {
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		f.requireKey = "sk-the-right-key"
		code, stdout, stderr := runCheckEngine(t, f)
		requireStepFailure(t, code, stdout, stderr, 1, "reachable", "401", "TURING_LOCAL_ENGINE_API_KEY")
	})
}

func TestCheckEngineFailsAtStepOneWhenTheServerIsNotTheConfiguredEngine(t *testing.T) {
	// An OpenAI-compatible server has no /api/version.
	f := newFakeEngine(t, llm.LocalEngineOpenAI)
	code, stdout, stderr := runCheckEngine(t, f, "TURING_LOCAL_ENGINE=ollama", "TURING_LOCAL_ENGINE_URL="+f.server.URL)
	requireStepFailure(t, code, stdout, stderr, 1, "reachable", "404", "TURING_LOCAL_ENGINE")
}

func TestCheckEngineFailsAtStepTwoWhenOllamaLacksTheModel(t *testing.T) {
	f := newFakeEngine(t, llm.LocalEngineOllama, "llama3:latest")
	code, stdout, stderr := runCheckEngine(t, f)
	requireStepFailure(t, code, stdout, stderr, 2, "model present", "ollama pull qwen2.5:7b")
}

func TestCheckEngineFailsAtStepTwoWhenOllamaListsNothing(t *testing.T) {
	f := newFakeEngine(t, llm.LocalEngineOllama)
	f.listBody = `{"models":[]}`
	code, stdout, stderr := runCheckEngine(t, f)
	requireStepFailure(t, code, stdout, stderr, 2, "model present", "ollama pull qwen2.5:7b")
}

func TestCheckEngineFailsAtStepTwoWhenAnOpenAIEngineListsNoModels(t *testing.T) {
	f := newFakeEngine(t, llm.LocalEngineOpenAI)
	f.listBody = `{"object":"list","data":[]}`
	code, stdout, stderr := runCheckEngine(t, f)
	requireStepFailure(t, code, stdout, stderr, 2, "model present", "lists no models")
}

func TestCheckEngineFailsAtStepTwoWhenTheListIsNotWhatTheEngineSends(t *testing.T) {
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		f.listBody = `<html>not a model list</html>`
		code, stdout, stderr := runCheckEngine(t, f)
		requireStepFailure(t, code, stdout, stderr, 2, "model present", "model list")
	})
}

// An OpenAI-compatible server's one listing serves both steps, so a refusal of
// it is the first step's.
func TestCheckEngineFailsWhenTheModelListingIsRefused(t *testing.T) {
	for _, tc := range []struct {
		engine string
		step   int
		name   string
	}{
		{llm.LocalEngineOllama, 2, "model present"},
		{llm.LocalEngineOpenAI, 1, "reachable"},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			f := newFakeEngine(t, tc.engine)
			f.listStatus = http.StatusInternalServerError
			code, stdout, stderr := runCheckEngine(t, f)
			requireStepFailure(t, code, stdout, stderr, tc.step, tc.name, "500")
		})
	}
}

func TestCheckEngineTreatsAnUnlistedOpenAIModelAsAnAlias(t *testing.T) {
	f := newFakeEngine(t, llm.LocalEngineOpenAI, "some-other-model")
	f.model = openAIModel
	code, stdout, stderr := runCheckEngine(t, f)
	want := "1/4 reachable: ok\n" +
		"2/4 model present: ok (TURING_LOCAL_MODEL is not listed; treating it as an alias)\n" +
		"3/4 streaming: ok\n4/4 tool calling: ok\nthe engine is usable for Turing\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Fatalf("code = %d, stdout = %q, stderr = %q; want 0 and %q", code, stdout, stderr, want)
	}
}

func TestCheckEngineAcceptsAnUntaggedOllamaModelByItsLatestTag(t *testing.T) {
	f := newFakeEngine(t, llm.LocalEngineOllama, "llama3:latest")
	f.model = "llama3"
	code, stdout, stderr := runCheckEngine(t, f)
	if code != 0 || stdout != allFourSteps || stderr != "" {
		t.Fatalf("code = %d, stdout = %q, stderr = %q; want 0 and %q", code, stdout, stderr, allFourSteps)
	}
	for _, body := range f.chatBodies(t) {
		if body["model"] != "llama3" {
			t.Fatalf("chat request model = %v, want the configured llama3", body["model"])
		}
	}
}

func TestCheckEngineFailsAtStepThreeWhenTheStreamHasOnlyReasoning(t *testing.T) {
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		f.stream = modeReasoning
		code, stdout, stderr := runCheckEngine(t, f)
		requireStepFailure(t, code, stdout, stderr, 3, "streaming", "no text")
	})
}

func TestCheckEngineFailsAtStepThreeWithTheStreamErrorsCode(t *testing.T) {
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		f.stream = modeError
		code, stdout, stderr := runCheckEngine(t, f)
		requireStepFailure(t, code, stdout, stderr, 3, "streaming", "model_unavailable")
	})
}

func TestCheckEngineFailsAtStepFourWhenTheModelAnswersInText(t *testing.T) {
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		f.tool = modeText
		code, stdout, stderr := runCheckEngine(t, f)
		requireStepFailure(t, code, stdout, stderr, 4, "tool calling", "does not do tool calling")
	})
}

func TestCheckEngineFailsAtStepFourWhenTheToolHasAnotherName(t *testing.T) {
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		f.tool = modeWrongName
		code, stdout, stderr := runCheckEngine(t, f)
		requireStepFailure(t, code, stdout, stderr, 4, "tool calling", `"get_weather"`, "turing_engine_check")
	})
}

func TestCheckEngineFailsAtStepFourWithTheStreamErrorsCode(t *testing.T) {
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		f.tool = modeError
		code, stdout, stderr := runCheckEngine(t, f)
		requireStepFailure(t, code, stdout, stderr, 4, "tool calling", "model_unavailable")
	})
}

func TestCheckEngineSendsOnlyTheFixedProbeRequests(t *testing.T) {
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		if code, stdout, stderr := runCheckEngine(t, f); code != 0 {
			t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
		}
		bodies := f.chatBodies(t)
		if len(bodies) != 2 {
			t.Fatalf("the engine saw %d chat requests, want the streaming probe and the tool probe", len(bodies))
		}
		tokenLimit := func(body map[string]any) any {
			if engine == llm.LocalEngineOllama {
				options, _ := body["options"].(map[string]any)
				return options["num_predict"]
			}
			return body["max_tokens"]
		}
		for i, probe := range []struct {
			prompt    string
			maxTokens float64
			withTool  bool
		}{
			{"Reply with the single word: ready", 64, false},
			{"Call the turing_engine_check tool with status \"ok\". Do not answer with text.", 256, true},
		} {
			body := bodies[i]
			if body["model"] != f.model {
				t.Errorf("request %d model = %v, want %q", i+1, body["model"], f.model)
			}
			messages, _ := body["messages"].([]any)
			if len(messages) != 1 {
				t.Fatalf("request %d has %d messages, want only the fixed prompt: %v", i+1, len(messages), messages)
			}
			message, _ := messages[0].(map[string]any)
			if message["role"] != "user" || message["content"] != probe.prompt {
				t.Errorf("request %d message = %v, want the user prompt %q", i+1, message, probe.prompt)
			}
			if tokenLimit(body) != probe.maxTokens {
				t.Errorf("request %d token limit = %v, want %v", i+1, tokenLimit(body), probe.maxTokens)
			}
			tools, _ := body["tools"].([]any)
			if !probe.withTool {
				if len(tools) != 0 {
					t.Errorf("request %d offers tools %v, want none", i+1, tools)
				}
				continue
			}
			if len(tools) != 1 {
				t.Fatalf("request %d offers %d tools, want exactly turing_engine_check", i+1, len(tools))
			}
			function, _ := tools[0].(map[string]any)["function"].(map[string]any)
			if function["name"] != "turing_engine_check" {
				t.Errorf("tool name = %v, want turing_engine_check", function["name"])
			}
			var wantParameters any
			if err := json.Unmarshal([]byte(`{"type":"object","properties":{"status":{"type":"string","enum":["ok"]}},"required":["status"]}`), &wantParameters); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(function["parameters"], wantParameters) {
				t.Errorf("tool parameters = %v, want %v", function["parameters"], wantParameters)
			}
		}
	})
}

func TestCheckEngineSendsTheAPIKeyOnEveryOpenAIRequestAndNeverPrintsIt(t *testing.T) {
	const key = "sk-local-SECRETVALUE"
	f := newFakeEngine(t, llm.LocalEngineOpenAI)
	f.requireKey = key
	code, stdout, stderr := runCheckEngine(t, f, "TURING_LOCAL_ENGINE_API_KEY="+key)
	if code != 0 || stdout != allFourSteps || stderr != "" {
		t.Fatalf("code = %d, stdout = %q, stderr = %q; want 0 and %q", code, stdout, stderr, allFourSteps)
	}
	var listings int
	for _, request := range f.recorded() {
		if request.Authorization != "Bearer "+key {
			t.Errorf("%s %s carried Authorization %q, want the configured key as a bearer", request.Method, request.Path, request.Authorization)
		}
		if request.Method == http.MethodGet && request.Path == "/v1/models" {
			listings++
		}
	}
	if listings < 2 {
		t.Errorf("the engine saw %d model listings, want one for step 1 and one for step 2", listings)
	}

	rejected := newFakeEngine(t, llm.LocalEngineOpenAI)
	rejected.requireKey = "a-different-key"
	code, stdout, stderr = runCheckEngine(t, rejected, "TURING_LOCAL_ENGINE_API_KEY="+key)
	requireStepFailure(t, code, stdout, stderr, 1, "reachable", "401", "TURING_LOCAL_ENGINE_API_KEY")
	if strings.Contains(stdout+stderr, "SECRETVALUE") {
		t.Fatalf("output repeats the key: stdout = %q, stderr = %q", stdout, stderr)
	}
}

func TestCheckEngineSendsNoAuthorizationWithoutAKey(t *testing.T) {
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		if code, stdout, stderr := runCheckEngine(t, f); code != 0 {
			t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
		}
		for _, request := range f.recorded() {
			if request.Authorization != "" {
				t.Errorf("%s %s carried Authorization %q with no key configured", request.Method, request.Path, request.Authorization)
			}
		}
	})
}

func TestCheckEngineRefusesAConfigurationBeforeAnyRequest(t *testing.T) {
	f := newFakeEngine(t, llm.LocalEngineOllama)
	code, stdout, stderr := runCheckEngine(t, f, "TURING_LOCAL_ENGINE=vllm")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "TURING_LOCAL_ENGINE") || strings.Contains(stderr, "step ") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q; want 1 and the refused key", code, stdout, stderr)
	}
	if requests := f.recorded(); len(requests) != 0 {
		t.Fatalf("the engine saw %d requests for a refused configuration, want none", len(requests))
	}
}

func TestCheckEngineWarnsAboutDeprecatedKeys(t *testing.T) {
	f := newFakeEngine(t, llm.LocalEngineOllama)
	code, stdout, stderr := runCheckEngine(t, f, "TURING_LOCAL_MODEL=", "OLLAMA_MODEL="+f.model)
	if code != 0 || stdout != allFourSteps {
		t.Fatalf("code = %d, stdout = %q, stderr = %q; a deprecated name is a warning, not a failure", code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "warning: ") || !strings.Contains(stderr, "TURING_LOCAL_MODEL") {
		t.Fatalf("stderr = %q, want a warning naming TURING_LOCAL_MODEL", stderr)
	}
}

func TestCheckEngineGivesUpOnAListingThatNeverAnswers(t *testing.T) {
	setStepTimeouts(t, 150*time.Millisecond, 5*time.Second)
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		f.hangList = true
		started := time.Now()
		code, stdout, stderr := runCheckEngine(t, f)
		requireStepFailure(t, code, stdout, stderr, 1, "reachable", "within 150ms")
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Fatalf("the check took %s, want it to stop at the step timeout", elapsed)
		}
	})
}

func TestCheckEngineGivesUpOnAStreamThatNeverAnswers(t *testing.T) {
	setStepTimeouts(t, 5*time.Second, 150*time.Millisecond)
	for _, mode := range []string{modeHang, modeStall} {
		t.Run(mode, func(t *testing.T) {
			eachEngine(t, func(t *testing.T, engine string) {
				f := newFakeEngine(t, engine)
				f.stream = mode
				started := time.Now()
				code, stdout, stderr := runCheckEngine(t, f)
				requireStepFailure(t, code, stdout, stderr, 3, "streaming", "within 150ms", "still loading")
				if elapsed := time.Since(started); elapsed > 3*time.Second {
					t.Fatalf("the check took %s, want it to stop at the step timeout", elapsed)
				}
			})
		})
	}
}

func TestCheckEngineGivesUpOnAToolProbeThatNeverAnswers(t *testing.T) {
	setStepTimeouts(t, 5*time.Second, 150*time.Millisecond)
	eachEngine(t, func(t *testing.T, engine string) {
		f := newFakeEngine(t, engine)
		f.tool = modeHang
		code, stdout, stderr := runCheckEngine(t, f)
		requireStepFailure(t, code, stdout, stderr, 4, "tool calling", "within 150ms")
	})
}

func TestModelListed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		engine    string
		model     string
		names     []string
		wantListd bool
		wantAlias bool
	}{
		{"ollama exact", llm.LocalEngineOllama, "qwen2.5:7b", []string{"llama3:latest", "qwen2.5:7b"}, true, false},
		{"ollama untagged matches latest", llm.LocalEngineOllama, "llama3", []string{"llama3:latest"}, true, false},
		{"ollama untagged does not match another tag", llm.LocalEngineOllama, "llama3", []string{"llama3:8b"}, false, false},
		{"ollama untagged does not match a longer name", llm.LocalEngineOllama, "llama3", []string{"llama3.1:latest"}, false, false},
		{"ollama tagged does not match latest", llm.LocalEngineOllama, "qwen2.5:7b", []string{"qwen2.5:latest"}, false, false},
		{"ollama tagged by its registry path matches latest", llm.LocalEngineOllama, "localhost:5000/team/model", []string{"localhost:5000/team/model:latest"}, true, false},
		{"ollama absent", llm.LocalEngineOllama, "qwen2.5:7b", []string{"llama3:latest"}, false, false},
		{"ollama empty", llm.LocalEngineOllama, "qwen2.5:7b", nil, false, false},
		{"ollama never aliases", llm.LocalEngineOllama, "qwen2.5:7b", []string{"anything"}, false, false},
		{"openai exact", llm.LocalEngineOpenAI, "deepseek-chat", []string{"deepseek-chat"}, true, false},
		{"openai not listed is an alias", llm.LocalEngineOpenAI, "deepseek-chat", []string{"other"}, false, true},
		{"openai does not add latest", llm.LocalEngineOpenAI, "llama3", []string{"llama3:latest"}, false, true},
		{"openai empty list is neither", llm.LocalEngineOpenAI, "deepseek-chat", nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listed, alias := modelListed(tc.engine, tc.model, tc.names)
			if listed != tc.wantListd || alias != tc.wantAlias {
				t.Fatalf("modelListed(%q, %q, %q) = %v, %v; want %v, %v",
					tc.engine, tc.model, tc.names, listed, alias, tc.wantListd, tc.wantAlias)
			}
		})
	}
}

func TestTheEngineClientIgnoresProxiesAndHasNoClientWideTimeout(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.invalid:3128")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:3128")
	client := newEngineClient()
	if client.Timeout != 0 {
		t.Fatalf("client timeout = %s, want none: each step's context carries its own", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport is %T, want an *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		request, _ := http.NewRequest(http.MethodGet, "http://host.docker.internal:11434/api/version", nil)
		proxy, _ := transport.Proxy(request)
		t.Fatalf("client would use the proxy %v, want none", proxy)
	}
	if transport == http.DefaultTransport {
		t.Fatal("client shares http.DefaultTransport; it must be a clone with its own proxy setting")
	}
}

func TestOneLineMakesAServersTextSafeToPrint(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"plain", "boom", 20, "boom"},
		{"newlines and tabs", "a\nb\t\tc", 20, "a b c"},
		{"terminal escapes", "x\x1b[31mred\x1b[0m", 40, "x [31mred [0m"},
		{"cut with a mark", "abcdefghij", 4, "abcd…"},
		{"cut on a rune boundary", "ééééé", 2, "éé…"},
		{"empty", "", 5, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := oneLine(tc.in, tc.limit); got != tc.want {
				t.Fatalf("oneLine(%q, %d) = %q, want %q", tc.in, tc.limit, got, tc.want)
			}
		})
	}
}

// stubProvider replays fixed events, so the steps' handling of event shapes
// the two real providers do not currently produce is still pinned down.
type stubProvider struct {
	events []llm.StreamEvent
	err    error
}

func (stubProvider) ID() string                                         { return "stub" }
func (stubProvider) ContextWindowTokens() int                           { return llm.DefaultContextWindowTokens }
func (stubProvider) MaxOutputTokens() int                               { return llm.DefaultMaxOutputTokens }
func (stubProvider) EstimateRequestTokens(llm.ChatRequest) (int, error) { return 0, nil }

func (s stubProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(chan llm.StreamEvent, len(s.events))
	for _, event := range s.events {
		out <- event
	}
	close(out)
	return out, nil
}

func stubbedProbe(events []llm.StreamEvent, err error) *engineProbe {
	return &engineProbe{
		engine: llm.LocalEngineOpenAI, baseURL: "http://127.0.0.1:1/v1", model: "m",
		provider: stubProvider{events: events, err: err},
	}
}

func probeCall(name string, arguments map[string]any) llm.ToolCall {
	return llm.ToolCall{ID: "call_0", Name: name, Arguments: arguments}
}

func TestToolCallingAcceptsACallOnTheToolCallEventOrOnCompleted(t *testing.T) {
	ok := probeCall("turing_engine_check", map[string]any{"status": "ok"})
	for _, tc := range []struct {
		name   string
		events []llm.StreamEvent
	}{
		{"tool_call event", []llm.StreamEvent{{Type: "tool_call", ToolCalls: []llm.ToolCall{ok}}, {Type: "completed"}}},
		{"completed event", []llm.StreamEvent{{Type: "completed", ToolCalls: []llm.ToolCall{ok}}}},
		{"empty arguments object", []llm.StreamEvent{{Type: "tool_call", ToolCalls: []llm.ToolCall{probeCall("turing_engine_check", map[string]any{})}}, {Type: "completed"}}},
		{"the right call among others", []llm.StreamEvent{{Type: "tool_call", ToolCalls: []llm.ToolCall{probeCall("other", map[string]any{}), ok}}, {Type: "completed"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := stubbedProbe(tc.events, nil).toolCalling(context.Background()); err != nil {
				t.Fatalf("toolCalling = %v, want it to pass", err)
			}
		})
	}
}

func TestToolCallingRejectsWhatIsNotAUsableCall(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []llm.StreamEvent
		want   string
	}{
		{"text only", []llm.StreamEvent{{Type: "delta", Text: "Sure, ok"}, {Type: "completed"}}, "does not do tool calling"},
		{"nothing at all", []llm.StreamEvent{{Type: "completed"}}, "does not do tool calling"},
		{"nil arguments", []llm.StreamEvent{{Type: "tool_call", ToolCalls: []llm.ToolCall{probeCall("turing_engine_check", nil)}}, {Type: "completed"}}, "arguments"},
		{"another tool", []llm.StreamEvent{{Type: "tool_call", ToolCalls: []llm.ToolCall{probeCall("other", map[string]any{})}}, {Type: "completed"}}, `"other"`},
		{"an error event", []llm.StreamEvent{{Type: "error", Code: "model_bad_chunk", Message: "arguments are malformed"}}, "model_bad_chunk"},
		{"a stream that just ends", nil, "ended"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := stubbedProbe(tc.events, nil).toolCalling(context.Background())
			var got failure
			if !errors.As(err, &got) || !strings.Contains(got.Error(), tc.want) {
				t.Fatalf("toolCalling = %v, want a failure containing %q", err, tc.want)
			}
		})
	}
}

func TestToolCallingReportsAFailureToStartTheStream(t *testing.T) {
	_, err := stubbedProbe(nil, errors.New("dial tcp 127.0.0.1:1: connect: connection refused")).toolCalling(context.Background())
	var got failure
	if !errors.As(err, &got) || !strings.Contains(got.Error(), "connection refused") {
		t.Fatalf("toolCalling = %v, want a failure naming the cause", err)
	}
}

func TestStreamingNeedsATextDeltaBeforeCompleted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []llm.StreamEvent
		want   string // empty means the step passes
	}{
		{"text then completed", []llm.StreamEvent{{Type: "delta", Text: "ready"}, {Type: "completed"}}, ""},
		{"whitespace is not text", []llm.StreamEvent{{Type: "delta", Text: " \n"}, {Type: "completed"}}, "no text"},
		{"completed alone", []llm.StreamEvent{{Type: "completed"}}, "no text"},
		{"text without completed", []llm.StreamEvent{{Type: "delta", Text: "ready"}}, "ended"},
		{"error after text", []llm.StreamEvent{{Type: "delta", Text: "re"}, {Type: "error", Code: "model_stream_error", Message: "cut off"}}, "model_stream_error"},
		{"error with control characters", []llm.StreamEvent{{Type: "error", Code: "model_error", Message: "bad\x1b[31m\nthing"}}, "bad [31m thing"},
		{"authentication refused", []llm.StreamEvent{{Type: "error", Code: "model_auth_failed", Message: "no"}}, "TURING_LOCAL_ENGINE_API_KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := stubbedProbe(tc.events, nil).streaming(context.Background())
			if tc.want == "" {
				if err != nil {
					t.Fatalf("streaming = %v, want it to pass", err)
				}
				return
			}
			var got failure
			if !errors.As(err, &got) || !strings.Contains(got.Error(), tc.want) {
				t.Fatalf("streaming = %v, want a failure containing %q", err, tc.want)
			}
		})
	}
}

func TestToolCallFailureNamesWhatWentWrong(t *testing.T) {
	if err := toolCallFailure([]llm.ToolCall{probeCall("turing_engine_check", map[string]any{"status": "ok"})}); err != nil {
		t.Fatalf("toolCallFailure = %v, want nil", err)
	}
	if err := toolCallFailure(nil); err == nil || !strings.Contains(err.Error(), "does not do tool calling") {
		t.Fatalf("toolCallFailure(nil) = %v, want the no-tool-calling failure", err)
	}
	if err := toolCallFailure([]llm.ToolCall{probeCall("x\ny", map[string]any{})}); err == nil ||
		strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), "x y") {
		t.Fatalf("toolCallFailure = %v, want a one-line failure naming the tool", err)
	}
}
