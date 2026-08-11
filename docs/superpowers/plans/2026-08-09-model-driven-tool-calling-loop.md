# Model-Driven Tool-Calling Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the model itself decide which MCP tool to call, execute it through the existing runner/approval plumbing, feed the result back, and continue until the model produces a final answer — replacing the hardcoded `/tool <name>` debug path.

**Architecture:** Extend the `llm` provider types to carry tool schemas (request) and tool-call deltas (response). Teach both providers (Ollama `/api/chat`, OpenAI `/chat/completions`) to serialize tool schemas and parse tool-call output. Replace `GeneralAssistant.Execute()`'s single-shot stream with a bounded loop that, on a tool-call, invokes the existing `tools.Runner.Run(...)` (which already does beacon → policy → approval → MCP call), threads the result back as a tool-role message, and re-invokes the model. All beacon/approval/event plumbing already exists and is reused unchanged.

**Tech Stack:** Go 1.23, `agent-runtime-go` module (`github.com/mcasillas17/TuringAgent`), gRPC/proto types under `turingv1`, existing `internal/tools/runner.go` + `internal/mcp/client.go`.

---

## Design decisions (locked for this plan)

1. **Tool schemas are sourced at runtime from the MCP servers**, not hardcoded. On first use per run, the agent calls `ListTools()` on each MCP client (`SystemMCP`, `FilesMCP`), builds a registry mapping `toolName → {serverName, client, jsonSchema}`, and hands the schemas to the model. This keeps Plan #1 independent of Plan #2 (dynamic discovery/persistence) — the registry is in-memory and rebuilt per process, cached across runs.
2. **Tool results thread back in-memory only.** Within one `Execute()` call, tool-role messages are appended to the in-memory `requestMessages` slice. We do NOT persist tool messages to the DB in this plan (that needs a `CreateMessage` path + `chatRole` change — deferred). Consequence: tool exchanges are not visible in *next* turn's history. Acceptable for MVP; noted as a follow-up.
3. **Loop is bounded** by `maxToolIterations = 5`. On exceeding it, emit `AGENT_RUN_STEP` note and force a final completion with whatever content exists, to prevent runaway loops. (Matches the plan's "concurrency capped" security posture.)
4. **Unified tool-call shape** across providers: `llm.ToolCall{ID, Name string; Arguments map[string]any}`. OpenAI arguments arrive as a JSON *string* (parsed to map); Ollama arguments arrive as a JSON *object* (used directly).
5. **The debug `/tool` path stays** for now (smoke tests depend on it) but is superseded — removed in a later cleanup once the loop is proven.

## Open questions (flag to user; do not block MVP)

- **Ollama tool-calling reliability with `llama3.2`.** Small local models call tools inconsistently. MVP targets the OpenAI-compatible provider as the primary tool-calling path and treats Ollama tool-calling as best-effort. Confirm the default demo model.
- **Persisting tool messages** (decision 2) — do we want tool exchanges in durable history? If yes, that's a fast-follow (add `CreateMessage`, extend `chatRole` to keep `MESSAGE_ROLE_TOOL`).
- **Parallel tool calls** (a model emitting >1 tool call in one turn). MVP executes them sequentially. Fine unless a provider insists on parallel.

## File structure

- Modify: `turing-backend/agent-runtime-go/internal/llm/provider.go` — add `ToolDefinition`, `ToolCall`; extend `ChatRequest`, `ChatMessage`, `StreamEvent`.
- Modify: `turing-backend/agent-runtime-go/internal/llm/openai_compatible.go` — serialize `tools`, accumulate `delta.tool_calls`, emit `tool_call` event.
- Modify: `turing-backend/agent-runtime-go/internal/llm/ollama.go` — serialize `tools`, parse `message.tool_calls`.
- Create: `turing-backend/agent-runtime-go/internal/agent/toolregistry.go` — build `toolName → {serverName, client, schema}` from MCP `ListTools`.
- Modify: `turing-backend/agent-runtime-go/internal/agent/general_assistant.go` — the `Execute()` loop.
- Test: `turing-backend/agent-runtime-go/internal/llm/*_test.go`, `internal/agent/general_assistant_test.go`, `internal/agent/toolregistry_test.go`.
- Test (integration): `turing-backend/tests/grpc_harness_test.go` — a model-driven tool-call end-to-end.

Run all runtime tests with: `cd turing-backend/agent-runtime-go && go test ./... -count=1`

---

## Phase 0 — Extend the `llm` types

### Task 0: Tool-aware provider types

**Files:**
- Modify: `turing-backend/agent-runtime-go/internal/llm/provider.go`
- Test: `turing-backend/agent-runtime-go/internal/llm/provider_test.go` (create)

- [ ] **Step 1: Write the failing test** (`provider_test.go`)

```go
package llm

import (
	"encoding/json"
	"testing"
)

func TestChatMessageSerializesRoleContentLowercaseJSON(t *testing.T) {
	b, err := json.Marshal(ChatMessage{Role: "user", Content: "hi"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"role":"user","content":"hi"}` {
		t.Fatalf("unexpected json: %s", b)
	}
}

func TestChatMessageOmitsEmptyToolFields(t *testing.T) {
	b, _ := json.Marshal(ChatMessage{Role: "assistant", Content: "x"})
	if got := string(b); got != `{"role":"assistant","content":"x"}` {
		t.Fatalf("tool fields should be omitted when empty, got: %s", got)
	}
}

func TestToolCallCarriesParsedArguments(t *testing.T) {
	tc := ToolCall{ID: "call_1", Name: "system.time", Arguments: map[string]any{"tz": "UTC"}}
	if tc.Arguments["tz"] != "UTC" {
		t.Fatalf("arguments not carried")
	}
}
```

- [ ] **Step 2: Run and confirm failure**

Run: `cd turing-backend/agent-runtime-go && go test ./internal/llm/ -run 'ChatMessage|ToolCall' -v`
Expected: FAIL — `ToolCall` undefined, and current `ChatMessage` marshals as `{"Role":...,"Content":...}` (no json tags).

- [ ] **Step 3: Implement the types** (replace the whole `provider.go` body)

```go
package llm

import "context"

// ToolDefinition is a tool schema advertised to the model (OpenAI/Ollama "function" shape).
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema object
}

// ToolCall is a provider-agnostic tool invocation emitted by the model.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Tool threading (all optional / omitted when empty):
	Name       string     `json:"name,omitempty"`         // tool name, for role=="tool"
	ToolCallID string     `json:"tool_call_id,omitempty"` // links a tool result to its call
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // for an assistant turn that called tools
}

type ChatRequest struct {
	Model       string
	Messages    []ChatMessage
	Tools       []ToolDefinition
	Temperature float64
	MaxTokens   int
}

type StreamEvent struct {
	Type         string     // "delta" | "completed" | "error" | "tool_call"
	Text         string     // for "delta"
	ToolCalls    []ToolCall // for "tool_call"
	FinishReason string
	Code         string
	Message      string
}

type Provider interface {
	ID() string
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `cd turing-backend/agent-runtime-go && go test ./internal/llm/ -run 'ChatMessage|ToolCall' -v`
Expected: PASS. Then `go build ./...` in the module to confirm no callers broke (the `ChatMessage` fields are additive; `ollamaChatRequest`/`openAIChatRequest` still compile).

- [ ] **Step 5: Commit**

```bash
git add turing-backend/agent-runtime-go/internal/llm/provider.go turing-backend/agent-runtime-go/internal/llm/provider_test.go
git commit -m "feat(runtime): add tool-aware llm types (ToolDefinition, ToolCall, tool fields)"
```

---

## Phase 1 — OpenAI-compatible provider tool support

The OpenAI streaming format delivers tool calls incrementally: each `choices[].delta.tool_calls[]` has an `index`, and the first chunk for an index carries `id` + `function.name`, subsequent chunks append `function.arguments` (a JSON string built up across chunks). Terminal chunk has `finish_reason == "tool_calls"`.

### Task 1: Serialize tools + accumulate tool-call deltas

**Files:**
- Modify: `turing-backend/agent-runtime-go/internal/llm/openai_compatible.go`
- Test: `turing-backend/agent-runtime-go/internal/llm/openai_compatible_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestOpenAIStreamsToolCall(t *testing.T) {
	// Fake SSE server that returns an incremental tool_call then finishes.
	chunks := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"system.time","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"tz\":"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"UTC\"}"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// assert the request carried the tools array
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["tools"]; !ok {
			t.Errorf("request missing tools field")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			_, _ = w.Write([]byte(c + "\n\n"))
		}
	}))
	defer srv.Close()

	p := NewOpenAICompatible(srv.URL, "k", srv.Client())
	events, err := p.StreamChat(context.Background(), ChatRequest{
		Model:    "gpt",
		Messages: []ChatMessage{{Role: "user", Content: "time?"}},
		Tools:    []ToolDefinition{{Name: "system.time", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got *ToolCall
	for e := range events {
		if e.Type == "tool_call" && len(e.ToolCalls) == 1 {
			got = &e.ToolCalls[0]
		}
	}
	if got == nil {
		t.Fatal("no tool_call event emitted")
	}
	if got.ID != "call_a" || got.Name != "system.time" || got.Arguments["tz"] != "UTC" {
		t.Fatalf("bad tool call: %+v", got)
	}
}
```

- [ ] **Step 2: Run, confirm failure**

Run: `cd turing-backend/agent-runtime-go && go test ./internal/llm/ -run OpenAIStreamsToolCall -v`
Expected: FAIL — request has no `tools`, no `tool_call` event emitted.

- [ ] **Step 3: Implement**

Add `Tools` to the request struct and map `ToolDefinition` into OpenAI's `{"type":"function","function":{...}}` shape:

```go
type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []ChatMessage   `json:"messages"`
	Tools       []openAITool    `json:"tools,omitempty"`
	Stream      bool            `json:"stream"`
	Temperature float64         `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"` // always "function"
	Function openAIToolFunc `json:"function"`
}
type openAIToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

func toOpenAITools(defs []ToolDefinition) []openAITool {
	if len(defs) == 0 {
		return nil
	}
	out := make([]openAITool, 0, len(defs))
	for _, d := range defs {
		params := d.Parameters
		if params == nil {
			params = map[string]any{"type": "object"}
		}
		out = append(out, openAITool{Type: "function", Function: openAIToolFunc{
			Name: d.Name, Description: d.Description, Parameters: params,
		}})
	}
	return out
}
```

In `StreamChat`, set `Tools: toOpenAITools(req.Tools)` on the request body. Extend the chunk parser to accumulate tool calls across chunks keyed by `index`, and emit a `tool_call` StreamEvent when `finish_reason == "tool_calls"`:

```go
// Add to openAIChunk.choices[].delta parsing:
type openAIToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type openAIDelta struct {
	Content   *string               `json:"content"`
	ToolCalls []openAIToolCallDelta `json:"tool_calls"`
}

// accumulator held across the stream loop in StreamChat:
type toolAccum struct {
	id   string
	name string
	args strings.Builder
}
// acc := map[int]*toolAccum{}   (declare before the read loop)

// in parseOpenAIData, when delta.ToolCalls present:
//   for each tcd: a := acc[tcd.Index]; if a==nil { a=&toolAccum{}; acc[tcd.Index]=a }
//   if tcd.ID != "" { a.id = tcd.ID }
//   if tcd.Function.Name != "" { a.name = tcd.Function.Name }
//   a.args.WriteString(tcd.Function.Arguments)
//
// when finish_reason == "tool_calls": build []ToolCall from acc (sorted by index),
//   json.Unmarshal each a.args into map[string]any (empty string -> empty map),
//   send StreamEvent{Type:"tool_call", ToolCalls: calls}; then send completed.
```

Because the accumulator must persist across `parseOpenAIData` calls, thread it in: change `parseOpenAIData` to take `acc map[int]*toolAccum` (or inline the tool-call handling into the `StreamChat` read loop where `acc` lives). Keep the existing `delta.Content` → `{Type:"delta"}` behavior unchanged.

- [ ] **Step 4: Run, confirm pass**

Run: `cd turing-backend/agent-runtime-go && go test ./internal/llm/ -run OpenAI -v`
Expected: PASS (both the new tool-call test and the existing content-streaming test).

- [ ] **Step 5: Commit**

```bash
git add turing-backend/agent-runtime-go/internal/llm/openai_compatible.go turing-backend/agent-runtime-go/internal/llm/openai_compatible_test.go
git commit -m "feat(runtime): openai provider serializes tools and emits tool_call events"
```

---

## Phase 2 — Ollama provider tool support

Ollama `/api/chat` accepts a `tools` array (same `{"type":"function","function":{...}}` shape) and, when the model calls a tool, returns a single message with `message.tool_calls` where `function.arguments` is a JSON **object** (map), not a string. Tool calls are not streamed token-by-token.

### Task 2: Serialize tools + parse `message.tool_calls`

**Files:**
- Modify: `turing-backend/agent-runtime-go/internal/llm/ollama.go`
- Test: `turing-backend/agent-runtime-go/internal/llm/ollama_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestOllamaParsesToolCall(t *testing.T) {
	line := `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"system.time","arguments":{"tz":"UTC"}}}]},"done":true,"done_reason":"stop"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["tools"]; !ok {
			t.Errorf("request missing tools")
		}
		_, _ = w.Write([]byte(line + "\n"))
	}))
	defer srv.Close()

	p := NewOllama(srv.URL, srv.Client())
	events, err := p.StreamChat(context.Background(), ChatRequest{
		Model: "llama3.2", Messages: []ChatMessage{{Role: "user", Content: "time?"}},
		Tools: []ToolDefinition{{Name: "system.time", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got *ToolCall
	for e := range events {
		if e.Type == "tool_call" && len(e.ToolCalls) == 1 {
			got = &e.ToolCalls[0]
		}
	}
	if got == nil || got.Name != "system.time" || got.Arguments["tz"] != "UTC" {
		t.Fatalf("bad tool call: %+v", got)
	}
}
```

- [ ] **Step 2: Run, confirm failure**

Run: `cd turing-backend/agent-runtime-go && go test ./internal/llm/ -run OllamaParsesToolCall -v`
Expected: FAIL — no `tools` in request, no `tool_call` event.

- [ ] **Step 3: Implement**

Add `Tools []openaiShapedTool` to `ollamaChatRequest` (reuse the OpenAI `{"type":"function",...}` shape — Ollama accepts it; if you prefer not to cross packages, define a local `ollamaTool` identical struct). Then in the NDJSON parse loop, after extracting `message`, check for `tool_calls`:

```go
// message map already decoded as obj["message"].(map[string]any)
if raw, ok := msg["tool_calls"].([]any); ok && len(raw) > 0 {
	calls := make([]ToolCall, 0, len(raw))
	for _, item := range raw {
		m, _ := item.(map[string]any)
		fn, _ := m["function"].(map[string]any)
		name, _ := fn["name"].(string)
		args, _ := fn["arguments"].(map[string]any) // Ollama gives an object
		if args == nil {
			args = map[string]any{}
		}
		calls = append(calls, ToolCall{Name: name, Arguments: args})
	}
	if !sendStreamEvent(ctx, out, StreamEvent{Type: "tool_call", ToolCalls: calls}) {
		return
	}
	continue // don't also treat as a content delta
}
```

Note: Ollama tool calls carry no `id`; leave `ToolCall.ID` empty. The agent loop (Phase 4) must tolerate an empty ID (generate one if needed for the tool-result linkage).

- [ ] **Step 4: Run, confirm pass**

Run: `cd turing-backend/agent-runtime-go && go test ./internal/llm/ -run Ollama -v`
Expected: PASS (tool-call test + existing content-streaming test).

- [ ] **Step 5: Commit**

```bash
git add turing-backend/agent-runtime-go/internal/llm/ollama.go turing-backend/agent-runtime-go/internal/llm/ollama_test.go
git commit -m "feat(runtime): ollama provider serializes tools and parses tool_calls"
```

---

## Phase 3 — Tool registry (name → server + client + schema)

The agent has two MCP clients (`SystemMCP`, `FilesMCP`). To execute a model-chosen tool it must know which client owns it, and to advertise schemas it must list them. `mcp.Client.ListTools` returns `[]map[string]any` (untyped) whose entries look like `{"name":"system.time","description":"...","inputSchema":{...}}`.

### Task 3: Build a tool registry from MCP `ListTools`

**Files:**
- Create: `turing-backend/agent-runtime-go/internal/agent/toolregistry.go`
- Test: `turing-backend/agent-runtime-go/internal/agent/toolregistry_test.go`

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

type fakeLister struct {
	list []map[string]any
	tools.MCPClient
}

func (f fakeLister) ListTools(ctx context.Context) ([]map[string]any, error) { return f.list, nil }

func TestBuildRegistryMapsNamesToClientsAndSchemas(t *testing.T) {
	sys := fakeLister{list: []map[string]any{
		{"name": "system.time", "description": "now", "inputSchema": map[string]any{"type": "object"}},
	}}
	files := fakeLister{list: []map[string]any{
		{"name": "files.create", "description": "write", "inputSchema": map[string]any{"type": "object"}},
	}}
	reg, err := BuildToolRegistry(context.Background(), map[string]toolLister{"system": sys, "files": files})
	if err != nil {
		t.Fatal(err)
	}
	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Fatalf("want 2 tool defs, got %d", len(defs))
	}
	entry, ok := reg.Lookup("files.create")
	if !ok || entry.ServerName != "files" {
		t.Fatalf("files.create not mapped to files server: %+v", entry)
	}
	if _, ok := reg.Lookup("nope"); ok {
		t.Fatal("unknown tool should not resolve")
	}
}
```

- [ ] **Step 2: Run, confirm failure**

Run: `cd turing-backend/agent-runtime-go && go test ./internal/agent/ -run BuildRegistry -v`
Expected: FAIL — `BuildToolRegistry`, `toolLister`, `ToolRegistry` undefined.

- [ ] **Step 3: Implement**

```go
package agent

import (
	"context"
	"fmt"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

type toolLister interface {
	ListTools(ctx context.Context) ([]map[string]any, error)
	tools.MCPClient // CallTool(...)
}

type ToolEntry struct {
	ServerName string
	Client     tools.MCPClient
	Definition llm.ToolDefinition
}

type ToolRegistry struct {
	byName map[string]ToolEntry
	order  []string // stable advertise order
}

func BuildToolRegistry(ctx context.Context, servers map[string]toolLister) (*ToolRegistry, error) {
	reg := &ToolRegistry{byName: map[string]ToolEntry{}}
	for serverName, client := range servers {
		list, err := client.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("list tools for %s: %w", serverName, err)
		}
		for _, item := range list {
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			desc, _ := item["description"].(string)
			params, _ := item["inputSchema"].(map[string]any)
			if params == nil {
				params = map[string]any{"type": "object"}
			}
			reg.byName[name] = ToolEntry{
				ServerName: serverName,
				Client:     client,
				Definition: llm.ToolDefinition{Name: name, Description: desc, Parameters: params},
			}
			reg.order = append(reg.order, name)
		}
	}
	return reg, nil
}

func (r *ToolRegistry) Definitions() []llm.ToolDefinition {
	out := make([]llm.ToolDefinition, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.byName[n].Definition)
	}
	return out
}

func (r *ToolRegistry) Lookup(name string) (ToolEntry, bool) {
	e, ok := r.byName[name]
	return e, ok
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `cd turing-backend/agent-runtime-go && go test ./internal/agent/ -run BuildRegistry -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add turing-backend/agent-runtime-go/internal/agent/toolregistry.go turing-backend/agent-runtime-go/internal/agent/toolregistry_test.go
git commit -m "feat(runtime): tool registry maps tool names to MCP client + schema"
```

---

## Phase 4 — The Execute loop

Replace the single-shot stream in `Execute()` with a bounded loop. Reuse the existing helpers (`messageEvent`, `emitRunFailed`) and `a.tools.Runner.Run(...)`.

### Task 4: Model-driven loop in `GeneralAssistant.Execute`

**Files:**
- Modify: `turing-backend/agent-runtime-go/internal/agent/general_assistant.go`
- Test: `turing-backend/agent-runtime-go/internal/agent/general_assistant_test.go`

- [ ] **Step 1: Write the failing test** (fake provider that calls a tool once, then answers)

```go
// fakeProvider returns a tool_call on the first StreamChat, plain text on the second.
type fakeProvider struct{ calls int }

func (f *fakeProvider) ID() string { return "fake" }
func (f *fakeProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent, 4)
	f.calls++
	if f.calls == 1 {
		ch <- llm.StreamEvent{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "system.time", Arguments: map[string]any{}}}}
		ch <- llm.StreamEvent{Type: "completed", FinishReason: "tool_calls"}
	} else {
		// assert the tool result was threaded back
		last := req.Messages[len(req.Messages)-1]
		if last.Role != "tool" {
			// signal failure via a sentinel token
			ch <- llm.StreamEvent{Type: "delta", Text: "NO_TOOL_RESULT"}
		} else {
			ch <- llm.StreamEvent{Type: "delta", Text: "It is noon."}
		}
		ch <- llm.StreamEvent{Type: "completed", FinishReason: "stop"}
	}
	close(ch)
	return ch, nil
}

func TestExecuteRunsModelChosenTool(t *testing.T) {
	// fake MCP client returns a fixed result; runner allows (PostBeacon -> ALLOW).
	// Wire GeneralAssistant with fakeProvider + a registry containing system.time.
	// Collect emitted RuntimeUpdates; assert:
	//  - a TOOL_CALL_STARTED and TOOL_CALL_COMPLETED event were emitted
	//  - final RunCompleted.Content == "It is noon."
	//  - provider was called exactly twice
}
```

(Flesh out the wiring using the existing test doubles pattern in the package; the runner needs `PostBeacon` returning `ToolPolicyDecision{Decision: ALLOW}` and a fake `MCPClient.CallTool` returning `map[string]any{"time":"12:00"}`.)

- [ ] **Step 2: Run, confirm failure**

Run: `cd turing-backend/agent-runtime-go && go test ./internal/agent/ -run ExecuteRunsModelChosenTool -v`
Expected: FAIL — current `Execute` ignores tool_call events and calls the provider once.

- [ ] **Step 3: Implement the loop**

Rewrite `Execute` (keep the signature `func (a *GeneralAssistant) Execute(ctx context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error`). Pseudocode with the real emit shapes:

```go
history, err := a.messages.FetchMessages(ctx, job.GetSessionId())
if err != nil { return emitRunFailed(emit, job, "message_fetch_failed", err.Error(), true) }

if err := emit(messageEvent(job, MESSAGE_STARTED, map[string]any{"messageId": job.GetAssistantMessageId(), "role": "assistant"})); err != nil { return err }

// keep the debug path for smoke tests (superseded, remove later)
if handled, err := a.tryDebugTool(ctx, job, emit); handled || err != nil { return err }

provider := a.providers[job.GetModelProvider()]
if provider == nil { return emitRunFailed(emit, job, "model_provider_unavailable", "...", false) }

registry, err := a.toolRegistry(ctx) // builds once, caches on the agent
if err != nil { return emitRunFailed(emit, job, "tool_discovery_failed", err.Error(), true) }

convo := append([]llm.ChatMessage{}, history...)
convo = append(convo, llm.ChatMessage{Role: "user", Content: job.GetUserText()})

var finalContent string
for iter := 0; iter < maxToolIterations; iter++ {
	events, err := provider.StreamChat(ctx, llm.ChatRequest{
		Model: job.GetModel(), Messages: convo, Tools: registry.Definitions(),
	})
	if err != nil { return emitRunFailed(emit, job, "model_stream_failed", err.Error(), true) }

	var turnText string
	var toolCalls []llm.ToolCall
	for ev := range events {
		switch ev.Type {
		case "delta":
			turnText += ev.Text
			if err := emit(messageEvent(job, MESSAGE_DELTA, map[string]any{"messageId": job.GetAssistantMessageId(), "delta": ev.Text})); err != nil { return err }
		case "tool_call":
			toolCalls = append(toolCalls, ev.ToolCalls...)
		case "error":
			return emitRunFailed(emit, job, ev.Code, ev.Message, false)
		}
	}

	if len(toolCalls) == 0 {
		finalContent = turnText
		break
	}

	// record the assistant's tool-calling turn, then execute each call.
	convo = append(convo, llm.ChatMessage{Role: "assistant", Content: turnText, ToolCalls: toolCalls})
	for _, call := range toolCalls {
		entry, ok := registry.Lookup(call.Name)
		if !ok {
			// tell the model the tool is unknown, let it recover
			convo = append(convo, llm.ChatMessage{Role: "tool", Name: call.Name, ToolCallID: call.ID, Content: `{"error":"unknown_tool"}`})
			continue
		}
		if err := emit(messageEvent(job, TOOL_CALL_STARTED, map[string]any{"toolName": call.Name, "toolCallId": call.ID})); err != nil { return err }
		result, err := a.tools.Runner.Run(ctx, tools.RunInput{
			AgentID: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
			RunID: job.GetRunId(), TraceID: job.GetTraceId(),
			ServerName: entry.ServerName, ToolName: call.Name, Args: call.Arguments, MCPClient: entry.Client,
		})
		if err != nil {
			_ = emit(messageEvent(job, TOOL_CALL_FAILED, map[string]any{"toolName": call.Name, "toolCallId": call.ID, "error": err.Error()}))
			convo = append(convo, llm.ChatMessage{Role: "tool", Name: call.Name, ToolCallID: call.ID, Content: `{"error":` + safejson.QuoteString(err.Error()) + `}`})
			continue
		}
		if err := emit(messageEvent(job, TOOL_CALL_COMPLETED, map[string]any{"toolName": call.Name, "toolCallId": call.ID})); err != nil { return err }
		resultJSON, _ := json.Marshal(result)
		convo = append(convo, llm.ChatMessage{Role: "tool", Name: call.Name, ToolCallID: call.ID, Content: string(resultJSON)})
	}
	// loop continues: model sees tool results and produces the next turn
}

if err := emit(messageEvent(job, MESSAGE_COMPLETED, map[string]any{"messageId": job.GetAssistantMessageId(), "content": finalContent})); err != nil { return err }
return emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{
	RunCompleted: &turingv1.RuntimeRunCompleted{RunId: job.GetRunId(), AssistantMessageId: job.GetAssistantMessageId(), Content: finalContent}}})
```

Add the constant and a cached registry accessor:

```go
const maxToolIterations = 5

func (a *GeneralAssistant) toolRegistry(ctx context.Context) (*ToolRegistry, error) {
	a.regOnce.Do(func() {
		a.reg, a.regErr = BuildToolRegistry(ctx, map[string]toolLister{
			"system": a.tools.SystemMCP.(toolLister),
			"files":  a.tools.FilesMCP.(toolLister),
		})
	})
	return a.reg, a.regErr
}
```

Add fields `reg *ToolRegistry`, `regErr error`, `regOnce sync.Once` to `GeneralAssistant`. Note `tools.MCPClient` is only `CallTool`; the registry needs `ListTools`, so `SystemMCP`/`FilesMCP` must be typed to include it. Change `GeneralAssistantTools.SystemMCP`/`FilesMCP` from `tools.MCPClient` to the local `toolLister` interface (the real `*mcp.Client` satisfies both `ListTools` and `CallTool`, so `cmd/runtime/main.go` needs no change beyond the type). Use `MESSAGE_STARTED` etc. as aliases for the long `turingv1.TuringEventType_TURING_EVENT_TYPE_*` constants (declare them at the top of the file).

Provide a small `safejson.QuoteString` helper or use `strconv.Quote` for error embedding.

- [ ] **Step 4: Run, confirm pass**

Run: `cd turing-backend/agent-runtime-go && go test ./internal/agent/ -run ExecuteRunsModelChosenTool -v`
Expected: PASS — provider called twice, tool events emitted, final content threaded from the second turn.

- [ ] **Step 5: Full runtime module test + build**

Run: `cd turing-backend/agent-runtime-go && go test ./... -count=1 && go build ./...`
Expected: PASS (the `GeneralAssistantTools` field-type change compiles against `mcp.Client`).

- [ ] **Step 6: Commit**

```bash
git add turing-backend/agent-runtime-go/internal/agent/general_assistant.go turing-backend/agent-runtime-go/internal/agent/general_assistant_test.go
git commit -m "feat(runtime): model-driven tool-calling loop in GeneralAssistant.Execute"
```

---

## Phase 5 — End-to-end integration test

Extend the existing harness (`turing-backend/tests/grpc_harness_test.go`), which already stands up the real orchestrator over bufconn, a real runtime worker, and fake OpenAI SSE + fake MCP HTTP servers.

### Task 5: Model-driven tool call, full stack

**Files:**
- Modify: `turing-backend/tests/grpc_harness_test.go`

- [ ] **Step 1: Write the failing test**

Add `TestModelDrivenToolCallCompletesRun`: configure the fake OpenAI server to return a `tool_call` for `system.time` on its first response and a final text answer on its second; configure the fake system MCP to answer `tools/list` (advertising `system.time`) and `tools/call`. Send a user message via `ChatService.SendMessage`, collect the stream, and assert:
- a `TOOL_CALL_STARTED` and `TOOL_CALL_COMPLETED` event appear in the event stream,
- the final `message.completed` / `run_completed` content equals the model's second-turn text,
- the fake MCP received exactly one `tools/call` for `system.time`.

(Mirror the structure of the existing `TestApprovalRequiredToolFlow` for wiring the fakes; the key new requirement is the fake OpenAI server returning tool-call SSE and the fake MCP answering `tools/list`.)

- [ ] **Step 2: Run, confirm failure**

Run: `cd turing-backend && go test ./tests/ -run ModelDrivenToolCall -v`
Expected: FAIL initially (until fakes + assertions are wired), proving the test exercises the new path.

- [ ] **Step 3: Make it pass** — no product code should be needed if Phases 0-4 are correct; only test wiring. If product gaps surface (e.g. `system.time` policy is `safe` so no approval — good), fix per systematic-debugging.

- [ ] **Step 4: Run the full verification matrix** via the `/verify` skill.

- [ ] **Step 5: Commit**

```bash
git add turing-backend/tests/grpc_harness_test.go
git commit -m "test(e2e): model-driven tool call completes a run end-to-end"
```

---

## Self-review checklist (run before handoff)

- **Spec coverage:** provider request carries tools (Tasks 1,2) ✓; providers parse tool calls (1,2) ✓; registry maps name→client+schema (3) ✓; loop executes + threads results (4) ✓; e2e proof (5) ✓.
- **Type consistency:** `ToolCall{ID,Name,Arguments}` used identically in provider.go, both providers, registry, and loop ✓. `ToolDefinition{Name,Description,Parameters}` consistent ✓. `toolLister` interface used by both the registry and the `GeneralAssistantTools` field type ✓.
- **Reused, not rebuilt:** beacon/policy/approval via `tools.Runner.Run` unchanged ✓; event emission via `messageEvent`/`emit` unchanged ✓; the worker's beacon round-trip and `SetToolBeaconPoster` wiring untouched ✓.
- **Deferred (documented):** persisting tool-role messages to DB + `chatRole` extension; removing the `/tool` debug path; parallel tool-call execution.
```
