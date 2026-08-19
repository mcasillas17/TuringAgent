# TUR-020 Context Budget Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pin every built-in provider to an explicit configured context window and deterministically keep each assembled model request within that window without silently dropping recall, history, tool schemas, or live tool protocol.

**Architecture:** Each built-in provider owns its configured context-window size and can estimate a request from the exact JSON body it would send. The estimate deliberately treats every serialized UTF-8 byte as one token: it is a conservative admission bound, not a tokenizer claim. A focused general-assistant context builder preserves attached skills, the current user turn, and the complete live assistant-tool/result chain; it then admits whole tool definitions, the recall block, and newest complete history turns in deterministic priority order. Any omitted optional material produces a durable `agent.run.step` notice before dispatch, while required live protocol that cannot fit fails the run instead of being partially truncated.

**Tech Stack:** Go 1.23, gRPC runtime events, Ollama `/api/chat`, OpenAI-compatible `/chat/completions`, SQLite-backed orchestrator event persistence, Docker Compose, Flutter run-notice rendering, Go tests.

---

## File structure

- Create `turing-backend/agent-runtime-go/internal/agent/context_budget.go`: provider-aware request assembly, whole-history grouping, required live-protocol validation, omission metadata, and notice wording.
- Create `turing-backend/agent-runtime-go/internal/agent/context_budget_test.go`: deterministic priority, conservative budget, recall/history/schema omission, and live protocol integrity tests.
- Modify `turing-backend/agent-runtime-go/internal/llm/provider.go`: context-window capability, conservative estimate helpers, and strict provider byte cap with no hidden history trimming.
- Modify `turing-backend/agent-runtime-go/internal/llm/ollama.go`: configured context window, exact request estimator, and mandatory `options.num_ctx`.
- Modify `turing-backend/agent-runtime-go/internal/llm/openai_compatible.go`: configured context window and exact request estimator without sending Ollama-only fields.
- Modify `turing-backend/agent-runtime-go/internal/llm/{ollama_test.go,openai_compatible_test.go,provider_request_test.go}`: wire-contract and no-hidden-truncation tests.
- Modify `turing-backend/agent-runtime-go/internal/config/{config.go,config_test.go}`: validated provider context-window environment values.
- Modify `turing-backend/agent-runtime-go/internal/agent/{general_assistant.go,general_assistant_test.go,external_agent.go,external_agent_test.go}`: pre-dispatch budgeting, durable notices, recall attribution after admission, and configured external-provider budget.
- Modify `turing-backend/agent-runtime-go/internal/worker/worker_test.go`: provider test double capability method.
- Modify `turing-backend/agent-runtime-go/cmd/runtime/main.go`: pass validated context windows to both providers and external-agent resolution.
- Modify `turing-backend/agent-runtime-go/testkit/{testkit.go,testkit_test.go}`: configure a context window for OpenAI-compatible integration workers.
- Modify `turing-backend/orchestrator-go/internal/service/runtime/service_test.go`: prove a context-budget run notice is persisted as an event.
- Modify `turing-backend/infra/docker-compose.yml` and `turing-backend/tests/docker_compose_security_test.go`: pass only the runtime the two context-window values and pin the Compose contract.
- Modify `turing-backend/.env.example`, `README.md`, `docs/architecture/tech-stack.md`, and `docs/VISION.md`: describe configuration, priority/omission behavior, conservative estimation limitation, durable notices, and focused/full verification.
- Keep generated protobuf and Flutter code unchanged: the existing durable `agent.run.step` protocol and `RunNoticeCard` already provide the required persisted, user-visible surface.

### Task 1: Pin and validate provider context windows

**Files:**
- Modify: `turing-backend/agent-runtime-go/internal/config/config_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/config/config.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/provider.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/ollama_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/ollama.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/openai_compatible_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/openai_compatible.go`

- [ ] **Step 1: Write failing configuration tests**

Add tests that pin both defaults, honor explicit overrides, and reject blank/non-integer/zero/negative/overflow values:

```go
func TestLoadFromEnvDefaultsProviderContextWindows(t *testing.T) {
	cfg, err := LoadFromEnv(mapEnv(map[string]string{
		"TURING_INTERNAL_TOKEN": "internal",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OllamaContextWindowTokens != 8192 {
		t.Fatalf("OllamaContextWindowTokens = %d, want 8192", cfg.OllamaContextWindowTokens)
	}
	if cfg.OpenAIContextWindowTokens != 8192 {
		t.Fatalf("OpenAIContextWindowTokens = %d, want 8192", cfg.OpenAIContextWindowTokens)
	}
}

func TestLoadFromEnvValidatesProviderContextWindows(t *testing.T) {
	for _, name := range []string{"OLLAMA_CONTEXT_WINDOW_TOKENS", "OPENAI_CONTEXT_WINDOW_TOKENS"} {
		for _, value := range []string{"0", "-1", "not-a-number", "16777217"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				_, err := LoadFromEnv(mapEnv(map[string]string{
					"TURING_INTERNAL_TOKEN": name,
					name:                    value,
				}))
				if err == nil || !strings.Contains(err.Error(), name) {
					t.Fatalf("LoadFromEnv error = %v, want %s validation", err, name)
				}
			})
		}
	}
}
```

- [ ] **Step 2: Run configuration tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/config -run 'TestLoadFromEnv(Default|Validates)ProviderContextWindows' -count=1
```

Expected: FAIL because the context-window fields and environment parsing do not exist.

- [ ] **Step 3: Implement validated configuration**

Add provider-specific fields and parse them with a shared positive bounded helper:

```go
const (
	defaultContextWindowTokens = 8192
	maxContextWindowTokens     = 16 * 1024 * 1024
)

type Config struct {
	// existing fields...
	OllamaContextWindowTokens int
	OpenAIContextWindowTokens int
}

func contextWindowTokensValue(getenv func(string) string, name string) (int, error) {
	value, err := intValue(getenv, name, defaultContextWindowTokens)
	if err != nil {
		return 0, err
	}
	if value <= 0 || value > maxContextWindowTokens {
		return 0, fmt.Errorf("%s must be between 1 and %d", name, maxContextWindowTokens)
	}
	return value, nil
}
```

Parse both values before returning `Config` and assign them to the new fields.

- [ ] **Step 4: Write failing provider wire tests**

Pin the capability and transport behavior:

```go
func TestOllamaRequestAlwaysPinsConfiguredContextWindow(t *testing.T) {
	provider := NewOllama(server.URL, server.Client()).WithContextWindowTokens(6144)
	body := captureOllamaProviderRequest(t, provider, ChatRequest{Model: "qwen2.5:7b"})
	options := body["options"].(map[string]any)
	if options["num_ctx"] != float64(6144) {
		t.Fatalf("num_ctx = %#v, want 6144", options["num_ctx"])
	}
}

func TestOpenAICompatibleDoesNotSendOllamaContextOption(t *testing.T) {
	provider := NewOpenAICompatible(server.URL, "", server.Client()).WithContextWindowTokens(6144)
	body := captureOpenAIProviderRequest(t, provider, ChatRequest{Model: "compatible-model"})
	if _, present := body["num_ctx"]; present {
		t.Fatalf("OpenAI-compatible request contained num_ctx: %#v", body)
	}
	if _, present := body["options"]; present {
		t.Fatalf("OpenAI-compatible request contained Ollama options: %#v", body)
	}
}
```

- [ ] **Step 5: Run provider wire tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/llm -run 'Test(OllamaRequestAlwaysPinsConfiguredContextWindow|OpenAICompatibleDoesNotSendOllamaContextOption)' -count=1
```

Expected: FAIL because neither provider exposes a configured context window and Ollama omits `num_ctx`.

- [ ] **Step 6: Implement the provider capability and Ollama pin**

Extend the provider boundary:

```go
const DefaultContextWindowTokens = 8192

type Provider interface {
	ID() string
	ContextWindowTokens() int
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}
```

Store `contextWindowTokens` on both built-in providers, default constructors to `DefaultContextWindowTokens`, expose `WithContextWindowTokens` and `ContextWindowTokens`, and make Ollama options mandatory:

```go
type ollamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
	NumCtx      int     `json:"num_ctx"`
}

func ollamaRequestOptions(temperature float64, maxTokens, contextWindowTokens int) *ollamaOptions {
	return &ollamaOptions{
		Temperature: temperature,
		NumPredict:  maxTokens,
		NumCtx:      contextWindowTokens,
	}
}
```

OpenAI-compatible stores the same capability for admission control but does not serialize it.

- [ ] **Step 7: Update provider test doubles and run focused tests**

Add a `ContextWindowTokens() int` method returning `llm.DefaultContextWindowTokens` to provider doubles in:

- `turing-backend/agent-runtime-go/internal/agent/general_assistant_test.go`
- `turing-backend/agent-runtime-go/internal/worker/worker_test.go`

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/config ./turing-backend/agent-runtime-go/internal/llm ./turing-backend/agent-runtime-go/internal/agent ./turing-backend/agent-runtime-go/internal/worker -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add turing-backend/agent-runtime-go/internal/config turing-backend/agent-runtime-go/internal/llm turing-backend/agent-runtime-go/internal/agent/general_assistant_test.go turing-backend/agent-runtime-go/internal/worker/worker_test.go
git commit -m "feat(runtime): pin provider context windows"
```

### Task 2: Add conservative provider request estimation

**Files:**
- Modify: `turing-backend/agent-runtime-go/internal/llm/provider_request_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/provider.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/ollama.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/openai_compatible.go`

- [ ] **Step 1: Replace the hidden-truncation test with a failing strict-boundary test**

The provider layer must never mutate assembled context:

```go
func TestProviderRequestByteLimitNeverSilentlyTrimsHistory(t *testing.T) {
	for _, fixture := range providerRequestFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			provider, requests := providerRejectingBudgetHarness(t, fixture)
			events, err := provider.StreamChat(context.Background(), ChatRequest{
				Model: fixture.model,
				Messages: []ChatMessage{
					{Role: "user", Content: strings.Repeat("o", 9*1024*1024)},
					{Role: "assistant", Content: strings.Repeat("a", 8*1024*1024)},
					{Role: "user", Content: "current question"},
				},
			})
			if err == nil {
				collectEvents(events)
				t.Fatal("provider silently trimmed oversized history")
			}
			assertProviderRequestBudgetError(t, err, fixture.name)
			if got := requests.Load(); got != 0 {
				t.Fatalf("HTTP requests = %d, want none", got)
			}
		})
	}
}
```

- [ ] **Step 2: Run the strict provider test and verify it fails**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/llm -run TestProviderRequestByteLimitNeverSilentlyTrimsHistory -count=1
```

Expected: FAIL because `marshalProviderRequest` currently drops old messages and sends a request.

- [ ] **Step 3: Remove provider-side history mutation**

Replace the binary-search trimming helper with strict marshaling:

```go
func marshalProviderRequest(provider string, marshal func() ([]byte, error)) ([]byte, error) {
	body, err := marshal()
	if err != nil {
		return nil, err
	}
	if len(body) > maxProviderRequestBytes {
		return nil, providerRequestSizeError{provider: provider, encodedBytes: len(body)}
	}
	return body, nil
}
```

Delete `historyDropBoundaries` and `trimHistory`. Update both providers to marshal the messages they were given unchanged.

- [ ] **Step 4: Write failing exact-body estimator tests**

For both providers, assert that the estimator returns the exact serialized request byte length and that it observes provider-specific transformations:

```go
func TestProviderContextEstimateUsesExactWireBody(t *testing.T) {
	req := ChatRequest{
		Model: "model",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
		Tools: []ToolDefinition{{Name: "tool.with.invalid.openai.characters"}},
	}
	for _, provider := range []Provider{ollamaProvider, openAIProvider} {
		estimated, err := EstimateRequestTokens(provider, req)
		if err != nil {
			t.Fatal(err)
		}
		body := captureRequestBody(t, provider, req)
		if estimated != len(body) {
			t.Fatalf("%s estimate = %d, wire bytes = %d", provider.ID(), estimated, len(body))
		}
	}
}
```

- [ ] **Step 5: Implement exact provider request estimators**

Add an internal optional capability and fallback:

```go
type requestTokenEstimator interface {
	EstimateRequestTokens(ChatRequest) (int, error)
}

func EstimateRequestTokens(provider Provider, req ChatRequest) (int, error) {
	if estimator, ok := provider.(requestTokenEstimator); ok {
		return estimator.EstimateRequestTokens(req)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}
	return len(body), nil
}
```

Extract provider-specific `marshalRequest` helpers and implement `EstimateRequestTokens` by returning `len(body)`. Document that one serialized UTF-8 byte is treated as one estimated token; this intentionally overestimates normal tokenizer use and does not claim exact usage.

- [ ] **Step 6: Run the LLM package**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/llm -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add turing-backend/agent-runtime-go/internal/llm
git commit -m "fix(runtime): forbid hidden provider context trimming"
```

### Task 3: Build deterministic whole-unit context admission

**Files:**
- Create: `turing-backend/agent-runtime-go/internal/agent/context_budget.go`
- Create: `turing-backend/agent-runtime-go/internal/agent/context_budget_test.go`

- [ ] **Step 1: Write failing priority and determinism tests**

Create fixtures with a tiny fake provider window and exact request estimates. Cover:

```go
func TestBuildBudgetedContextKeepsMandatoryLiveContext(t *testing.T) {
	input := contextInput{
		skills: &llm.ChatMessage{Role: "system", Content: "attached skill"},
		history: []llm.ChatMessage{
			{Role: "user", Content: "old question"},
			{Role: "assistant", Content: "old answer"},
		},
		recall: &llm.ChatMessage{Role: "system", Content: "recalled fact"},
		live: []llm.ChatMessage{{Role: "user", Content: "current question"}},
	}
	got, err := buildBudgetedContext(provider, "model", input, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertContainsWholeMessage(t, got.Messages, "attached skill")
	assertContainsWholeMessage(t, got.Messages, "current question")
}

func TestBuildBudgetedContextIsDeterministic(t *testing.T) {
	first, err := buildBudgetedContext(provider, "model", input, tools)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildBudgetedContext(provider, "model", input, tools)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("budgeting changed between identical runs:\nfirst=%#v\nsecond=%#v", first, second)
	}
}
```

Expected admission order:

1. Mandatory attached-skill system message, current user turn, and all live assistant tool-call/tool-result messages.
2. Tool definitions in stable registry order; any definition named by the live protocol is mandatory.
3. The complete recall system message.
4. Complete historical turns from newest to oldest.

Messages and schemas are admitted whole. Final message order remains recall, skills, chronological admitted history, then chronological live messages.

- [ ] **Step 2: Write failing omission tests**

Cover each optional category and exact omission metadata:

```go
func TestBuildBudgetedContextOmitsOldestCompleteHistoryFirst(t *testing.T) { /* two turns; newest survives */ }
func TestBuildBudgetedContextNeverPartiallyIncludesRecall(t *testing.T) { /* recall is wholly present or absent */ }
func TestBuildBudgetedContextOmitsWholeToolDefinitions(t *testing.T) { /* no schema map is cut */ }
```

Assert `omissions` reports message and definition counts plus `RecallOmitted`.

- [ ] **Step 3: Write failing live protocol safety tests**

Pin the protocol invariant:

```go
func TestBuildBudgetedContextKeepsLiveToolCallAndResultTogether(t *testing.T) {
	live := []llm.ChatMessage{
		{Role: "user", Content: "inspect"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "files.read"}}},
		{Role: "tool", ToolCallID: "call_1", Name: "files.read", Content: `{"content":"value"}`},
	}
	got, err := buildBudgetedContext(provider, "model", contextInput{live: live}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Messages, live) {
		t.Fatalf("live protocol changed: %#v", got.Messages)
	}
}

func TestBuildBudgetedContextFailsWhenLiveProtocolCannotFit(t *testing.T) {
	_, err := buildBudgetedContext(tinyProvider, "model", contextInput{live: oversizedLiveProtocol}, tools)
	if !errors.Is(err, errContextBudgetExceeded) {
		t.Fatalf("error = %v, want errContextBudgetExceeded", err)
	}
}
```

- [ ] **Step 4: Run context-builder tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/agent -run 'TestBuildBudgetedContext' -count=1
```

Expected: FAIL because the builder does not exist.

- [ ] **Step 5: Implement the context builder**

Use focused types:

```go
var errContextBudgetExceeded = errors.New("required context exceeds the configured model window")

type contextInput struct {
	skills  *llm.ChatMessage
	history []llm.ChatMessage
	recall  *llm.ChatMessage
	live    []llm.ChatMessage
}

type contextOmissions struct {
	HistoryMessages       int
	RecallOmitted         bool
	ToolDefinitions       int
}

type budgetedContext struct {
	Request   llm.ChatRequest
	Omissions contextOmissions
	RecallUsed bool
	Estimate  int
}
```

Implement a `fits` closure that calls `llm.EstimateRequestTokens(provider, candidate)` and compares it to `provider.ContextWindowTokens()`. Build the mandatory candidate first and return `errContextBudgetExceeded` when it cannot fit. Add optional units one at a time in the specified stable priority. Group history from user message through the messages preceding the next user so an old user/assistant exchange is never split.

- [ ] **Step 6: Implement accurate notice wording tests and code**

```go
func TestContextOmissionsNoticeNamesEveryOmittedCategory(t *testing.T) {
	got := (contextOmissions{
		HistoryMessages: 4,
		RecallOmitted: true,
		ToolDefinitions: 2,
	}).Notice()
	want := "Context window limit: omitted 4 older conversation messages, recalled material, and 2 tool definitions from this model request."
	if got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
}
```

Return an empty notice for zero omissions and use correct singular/plural wording. Do not include an estimated token count in the notice.

- [ ] **Step 7: Run and format the context-builder package**

Run:

```bash
gofmt -w turing-backend/agent-runtime-go/internal/agent/context_budget.go turing-backend/agent-runtime-go/internal/agent/context_budget_test.go
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/agent -run 'Test(BuildBudgetedContext|ContextOmissions)' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add turing-backend/agent-runtime-go/internal/agent/context_budget.go turing-backend/agent-runtime-go/internal/agent/context_budget_test.go
git commit -m "feat(runtime): budget assembled model context"
```

### Task 4: Integrate budgeting and durable notices into the tool loop

**Files:**
- Modify: `turing-backend/agent-runtime-go/internal/agent/general_assistant_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/agent/general_assistant.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/service_test.go`

- [ ] **Step 1: Write a failing long-session recall-preservation test**

Use a provider with a configured window large enough for recall plus only the newest history:

```go
func TestExecuteLongSessionStaysWithinBudgetAndPreservesRecall(t *testing.T) {
	provider := &capturingBudgetProvider{window: 1200}
	recaller := &fakeRecaller{
		block: llm.ChatMessage{Role: "system", Content: "recalled material"},
		ok: true,
	}
	history := representativeLongHistory()
	assistant := NewGeneralAssistant(providerMap(provider), fakeMessageClient{messages: history}, &GeneralAssistantTools{Recall: recaller})

	updates := collectUpdates(t, assistant, testJob())

	if provider.estimate > provider.window {
		t.Fatalf("estimate = %d, configured window = %d", provider.estimate, provider.window)
	}
	assertMessageContent(t, provider.request.Messages, "recalled material")
	assertRunStepNote(t, updates, recallNotice)
}
```

- [ ] **Step 2: Write a failing recall-omission notice test**

Use a window where mandatory content and tools fit but recall does not. Assert recall is absent and one `agent.run.step` note accurately names recalled material:

```go
func TestExecuteNoticesWhenRecallCannotFitContextWindow(t *testing.T) {
	// ...
	if containsMessage(provider.request.Messages, "recalled material") {
		t.Fatal("recall reached the provider despite the constrained window")
	}
	assertRunStepNoteContains(t, updates, "recalled material")
	assertNoRunStepNote(t, updates, recallNotice)
}
```

- [ ] **Step 3: Write a failing evolving tool-loop budget test**

Script a tool call followed by a final answer. Make its result large enough to force more old history out on the second dispatch. Assert:

- Both requests remain within the provider window.
- The second request contains the complete assistant tool call and matching tool result.
- The omission notice precedes the second provider dispatch.
- No category notice repeats when the omission set is unchanged.

- [ ] **Step 4: Write a failing oversized-live-protocol test**

Script a tool result whose complete call/result chain cannot fit. Assert no second provider request occurs and the terminal failure code is `context_budget_exceeded`, not a request with a truncated tool result.

- [ ] **Step 5: Run focused assistant tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/agent -run 'TestExecute(LongSession|NoticesWhenRecall|EvolvingToolLoop|RejectsOversizedLiveProtocol)' -count=1
```

Expected: FAIL because `Execute` sends the unbudgeted combined request.

- [ ] **Step 6: Refactor `Execute` around immutable context sources**

Keep:

```go
historyMessages := append([]llm.ChatMessage{}, messages...)
liveMessages := []llm.ChatMessage{{Role: "user", Content: job.GetUserText()}}
toolDefinitions := registry.Definitions()
```

Store skill and recall messages separately. Before every provider call:

```go
budgeted, err := buildBudgetedContext(provider, job.GetModel(), contextInput{
	skills:  skillMessage,
	history: historyMessages,
	recall:  recallMessage,
	live:    liveMessages,
}, toolDefinitions)
if err != nil {
	return emitRunFailed(emit, job, "context_budget_exceeded", err.Error(), false)
}
```

Emit a durable `AGENT_RUN_STEP` event when the omission set changes and is non-empty:

```go
if notice := budgeted.Omissions.Notice(); notice != "" && budgeted.Omissions != lastOmissions {
	if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP, map[string]any{
		"note": notice,
		"reason": "context_budget",
		"historyMessagesOmitted": budgeted.Omissions.HistoryMessages,
		"recallOmitted": budgeted.Omissions.RecallOmitted,
		"toolDefinitionsOmitted": budgeted.Omissions.ToolDefinitions,
	})); err != nil {
		return err
	}
	lastOmissions = budgeted.Omissions
}
```

Emit the existing recall-attribution notice only once, immediately before the first dispatch that actually includes recall. Append assistant tool calls and tool results only to `liveMessages`.

- [ ] **Step 7: Prove the notice is durable**

Add an orchestrator runtime-service test that sends an `AGENT_RUN_STEP` with:

```go
map[string]any{
	"note": "Context window limit: omitted recalled material from this model request.",
	"reason": "context_budget",
	"recallOmitted": true,
}
```

Replay the session events from the repository and assert the persisted event type and payload match. This pins the existing runtime-event persistence path without introducing a new proto event.

- [ ] **Step 8: Run focused runtime and assistant tests**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/agent ./turing-backend/orchestrator-go/internal/service/runtime -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add turing-backend/agent-runtime-go/internal/agent turing-backend/orchestrator-go/internal/service/runtime/service_test.go
git commit -m "feat(runtime): surface context budget omissions"
```

### Task 5: Wire both provider configurations through production and tests

**Files:**
- Modify: `turing-backend/agent-runtime-go/cmd/runtime/main.go`
- Modify: `turing-backend/agent-runtime-go/internal/agent/external_agent_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/agent/external_agent.go`
- Modify: `turing-backend/agent-runtime-go/testkit/testkit_test.go`
- Modify: `turing-backend/agent-runtime-go/testkit/testkit.go`
- Modify: `turing-backend/infra/docker-compose.yml`
- Modify: `turing-backend/tests/docker_compose_security_test.go`

- [ ] **Step 1: Write failing wiring tests**

Pin external-agent and testkit configuration:

```go
func TestExternalAgentProviderUsesConfiguredContextWindow(t *testing.T) {
	resolve := NewExternalAgentProviderFunc(map[string]string{"claude": "key"}, 4096, http.DefaultClient)
	provider, err := resolve(routedJob().GetExternalAgent())
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.ContextWindowTokens(); got != 4096 {
		t.Fatalf("context window = %d, want 4096", got)
	}
}

func TestWorkerConfigDefaultsContextWindow(t *testing.T) {
	if got := (WorkerConfig{}).contextWindowTokens(); got != llm.DefaultContextWindowTokens {
		t.Fatalf("context window = %d", got)
	}
}
```

- [ ] **Step 2: Run wiring tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/agent ./turing-backend/agent-runtime-go/testkit -run 'Test(ExternalAgentProviderUsesConfiguredContextWindow|WorkerConfigDefaultsContextWindow)' -count=1
```

Expected: FAIL because the context window is not passed.

- [ ] **Step 3: Wire production and integration providers**

In `main.go`:

```go
ollama := llm.NewOllama(cfg.OllamaBaseURL, http.DefaultClient).
	WithKeepAlive(cfg.OllamaKeepAlive).
	WithContextWindowTokens(cfg.OllamaContextWindowTokens)

openAI := llm.NewOpenAICompatible(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, http.DefaultClient).
	WithContextWindowTokens(cfg.OpenAIContextWindowTokens)
```

Pass `cfg.OpenAIContextWindowTokens` to `NewExternalAgentProviderFunc`. Add `ContextWindowTokens` to `testkit.WorkerConfig`, default it to `llm.DefaultContextWindowTokens`, and apply it to the OpenAI-compatible test provider.

- [ ] **Step 4: Add Compose configuration and least-privilege assertions**

Add only to `turing-agent-runtime-general.environment`:

```yaml
OLLAMA_CONTEXT_WINDOW_TOKENS: ${OLLAMA_CONTEXT_WINDOW_TOKENS:-8192}
OPENAI_CONTEXT_WINDOW_TOKENS: ${OPENAI_CONTEXT_WINDOW_TOKENS:-8192}
```

Assert the runtime contains both and MCP services contain neither.

- [ ] **Step 5: Run wiring and Compose tests**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/cmd/runtime ./turing-backend/agent-runtime-go/internal/agent ./turing-backend/agent-runtime-go/testkit ./turing-backend/tests -run 'Test(ExternalAgentProvider|WorkerConfig|DockerCompose)' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add turing-backend/agent-runtime-go/cmd/runtime turing-backend/agent-runtime-go/internal/agent/external_agent.go turing-backend/agent-runtime-go/internal/agent/external_agent_test.go turing-backend/agent-runtime-go/testkit turing-backend/infra/docker-compose.yml turing-backend/tests/docker_compose_security_test.go
git commit -m "feat(runtime): wire provider context budgets"
```

### Task 6: Document shipped behavior and operational limits

**Files:**
- Modify: `turing-backend/.env.example`
- Modify: `README.md`
- Modify: `docs/architecture/tech-stack.md`
- Modify: `docs/VISION.md`

- [ ] **Step 1: Update the environment example**

Add:

```dotenv
# Prompt admission limit for the local model. The runtime sends this exact value
# as Ollama options.num_ctx on every request; it never relies on the Ollama host
# default. Set it to a positive value no larger than the selected model supports.
OLLAMA_CONTEXT_WINDOW_TOKENS=8192

# Conservative prompt admission limit for OpenAI-compatible and routed external
# agents. This is enforced locally but is not sent as the Ollama-only num_ctx
# option. Match it to the smallest context window exposed by the configured model.
OPENAI_CONTEXT_WINDOW_TOKENS=8192
```

- [ ] **Step 2: Update operator documentation**

In `README.md` and `docs/architecture/tech-stack.md`, document:

- Both environment variables fail startup when non-integer, non-positive, or above `16777216`.
- Ollama always receives `options.num_ctx`; OpenAI-compatible providers do not.
- Admission uses the exact serialized request byte length as a conservative token estimate and never reports it as exact provider token usage.
- Priority is mandatory skills/current turn/live tool protocol, stable whole tool schemas, whole recall, then newest complete history turns.
- Optional material is omitted only as whole messages/turns/schemas.
- Every changed omission set emits a persisted `agent.run.step` notice rendered by the chat client.
- A live tool call/result chain that cannot fit fails with `context_budget_exceeded`; it is never partially truncated.
- Focused verification command:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/config ./turing-backend/agent-runtime-go/internal/llm ./turing-backend/agent-runtime-go/internal/agent ./turing-backend/orchestrator-go/internal/service/runtime ./turing-backend/tests -count=1
```

- [ ] **Step 3: Update the living architecture state**

In `docs/VISION.md`, update the verified date/state and add context budgeting to "What is true today" and "The user is never left guessing." State the limitation plainly: there is no provider tokenizer or automatic model-capability discovery, so operators configure the window and the runtime applies a conservative request-byte admission bound.

- [ ] **Step 4: Review documentation against behavior**

Run:

```bash
rg -n 'CONTEXT_WINDOW|num_ctx|context budget|context_budget_exceeded|conservative' turing-backend/.env.example README.md docs/architecture/tech-stack.md docs/VISION.md
```

Expected: every configuration, behavior, limitation, failure mode, and verification statement appears in the directly related docs and matches code names.

- [ ] **Step 5: Commit**

```bash
git add turing-backend/.env.example README.md docs/architecture/tech-stack.md docs/VISION.md
git commit -m "docs: explain model context budgeting"
```

### Task 7: Review, verify, commit, and open the focused PR

**Files:**
- Review: full diff from `main`
- Verify: all repository modules and generated-proto guard

- [ ] **Step 1: Run focused tests and inspect the complete diff**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/config ./turing-backend/agent-runtime-go/internal/llm ./turing-backend/agent-runtime-go/internal/agent ./turing-backend/agent-runtime-go/cmd/runtime ./turing-backend/agent-runtime-go/testkit ./turing-backend/orchestrator-go/internal/service/runtime ./turing-backend/tests -count=1
git --no-pager diff --check
git --no-pager diff main...HEAD
```

Expected: tests PASS; no whitespace errors; the diff is limited to TUR-020 code, tests, configuration, and directly related docs.

- [ ] **Step 2: Run the two required independent reviewers in parallel**

Reviewer A: Claude Opus 5, `xhigh`, `long_context`, full diff plus this task and `docs/VISION.md`; request spec and architecture review.

Reviewer B: GPT-5.6 Luna, `xhigh`, `long_context`, full diff; request correctness, edge-case, regression, performance, and test-coverage review.

Include documentation in both review scopes.

- [ ] **Step 3: Resolve all valid findings and repeat both reviewers**

For each review round:

1. Inspect every finding against code and tests.
2. Add a failing regression test for each accepted behavior bug.
3. Implement the minimal correction.
4. Rerun affected focused tests.
5. Rerun both reviewers on the new full diff in parallel.

Stop only when both reviewers explicitly report no remaining feedback. Record the number of completed review rounds.

- [ ] **Step 4: Run the repository-required Opus 4.8 final review**

Dispatch Claude Opus 4.8 against the final full diff for correctness, intent gaps, simplification, naming, and unit-test coverage. Resolve every valid item and rerun affected tests.

- [ ] **Step 5: Run the full verification matrix**

Invoke `/verify`, which must run:

```bash
go test -tags sqlite_fts5 ./... -count=1
go test -tags sqlite_fts5 -race ./... -count=1
go build -tags sqlite_fts5 ./...
( cd turing-backend/mcp-files  && go test ./... -count=1 && go test -race ./... -count=1 && go build ./cmd/server )
( cd turing-backend/mcp-system && go test ./... -count=1 && go test -race ./... -count=1 && go build ./... )
( cd turing-client/turing_app  && flutter test )
tools/proto/check.sh
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files  && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```

Expected: every command PASS.

- [ ] **Step 6: Create the final commit**

If review fixes remain uncommitted:

```bash
git add README.md docs turing-backend
git commit -m "feat(runtime): enforce model context budgets

Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

- [ ] **Step 7: Push and open the PR**

Push the current branch and use the app-native `create_pull_request` tool with base `main`, a focused title, and a body containing:

- The explicit Ollama/OpenAI-compatible configuration.
- Deterministic priority and safe live-protocol behavior.
- Durable omission notices.
- Focused and full verification evidence.
- Both zero-feedback reviewer rounds and the Opus 4.8 review.
- Documentation updates.

- [ ] **Step 8: Notify the creator session**

Send the creator session the PR URL, full verification evidence, reviewer-loop count, and any genuine follow-up dependency. Do not merge the PR.

## Self-review

- **Spec coverage:** Tasks 1 and 5 pin and validate Ollama/OpenAI-compatible windows; Task 2 removes hidden provider truncation and supplies conservative exact-body estimates; Tasks 3 and 4 budget all requested context sources while protecting live protocol and persisting accurate notices; Task 6 documents behavior, configuration, limitations, and testing; Task 7 enforces all review, verification, push, PR, and creator-notification gates.
- **Scope boundary:** This plan omits summaries and curated memory. It drops only complete optional units and leaves provenance-preserving summarization to MEM-014.
- **Type consistency:** `ContextWindowTokens`, `EstimateRequestTokens`, `contextInput`, `contextOmissions`, `budgetedContext`, `errContextBudgetExceeded`, and the two environment names are used consistently throughout.
- **Placeholder scan:** No implementation step defers error handling, tests, or behavior to unspecified follow-up work.
