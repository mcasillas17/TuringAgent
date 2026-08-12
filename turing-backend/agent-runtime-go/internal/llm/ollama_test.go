package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllamaStreamChatParsesDeltaAndCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"content":"Hel"},"done":false}` + "\n"))
		w.Write([]byte(`{"done":true,"done_reason":"stop"}` + "\n"))
	}))
	t.Cleanup(server.Close)
	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "llama3.2", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 2 || got[0].Text != "Hel" || got[1].Type != "completed" {
		t.Fatalf("events = %+v", got)
	}
}

func TestOllamaStreamChatMalformedJSONReturnsErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":` + "\n"))
	}))
	t.Cleanup(server.Close)
	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "llama3.2"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 1 || got[0].Code != "model_bad_chunk" {
		t.Fatalf("events = %+v, want model_bad_chunk", got)
	}
}

func TestOllamaRejectsMultipleJSONValuesOnOneLine(t *testing.T) {
	got := streamOllamaEvents(t, `{"message":{"content":"must not stream"}}{"done":true}`+"\n")
	assertOllamaEventTypes(t, got, "error")
	if got[0].Code != "model_bad_chunk" {
		t.Fatalf("event = %+v, want model_bad_chunk", got[0])
	}
}

func TestOllamaParsesToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"role":"assistant","content":"I'll check.","tool_calls":[{"id":"call_1","type":"function","function":{"index":0,"name":"get_weather","arguments":{"city":"Paris"}}}]},"done":true,"done_reason":"tool_calls"}` + "\n"))
	}))
	t.Cleanup(server.Close)

	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "llama3.2"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	assertOllamaEventTypes(t, got, "delta", "tool_call", "completed")
	if got[0].Text != "I'll check." {
		t.Fatalf("delta = %+v", got[0])
	}
	if got[1].Text != "" || len(got[1].ToolCalls) != 1 {
		t.Fatalf("tool call event = %+v", got[1])
	}
	call := got[1].ToolCalls[0]
	if call.ID != "call_1" || call.Name != "get_weather" || call.Arguments["city"] != "Paris" {
		t.Fatalf("tool call = %+v", call)
	}
	if got[2].FinishReason != "tool_calls" {
		t.Fatalf("completion = %+v, want tool_calls reason", got[2])
	}
}

func TestOllamaAcceptsLegacyToolCallWithoutIDOrIndex(t *testing.T) {
	got := streamOllamaEvents(t,
		`{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Paris"}}}]},"done":true,"done_reason":"tool_calls"}`+"\n")
	assertOllamaEventTypes(t, got, "tool_call", "completed")
	call := got[0].ToolCalls[0]
	if call.ID != "" || call.Name != "get_weather" || call.Arguments["city"] != "Paris" {
		t.Fatalf("legacy tool call = %+v", call)
	}
}

func TestOllamaMapsLegacyToolCallsByArrayPosition(t *testing.T) {
	got := streamOllamaEvents(t,
		`{"message":{"tool_calls":[{"function":{"name":"first","arguments":{"position":0}}},{"function":{"name":"second","arguments":{"position":1}}}]},"done":true,"done_reason":"tool_calls"}`+"\n")

	assertOllamaEventTypes(t, got, "tool_call", "completed")
	calls := got[0].ToolCalls
	if len(calls) != 2 ||
		calls[0].Name != "first" || calls[0].Arguments["position"] != json.Number("0") ||
		calls[1].Name != "second" || calls[1].Arguments["position"] != json.Number("1") {
		t.Fatalf("legacy tool calls = %+v", calls)
	}
}

func TestOllamaMergesLegacyToolCallFragmentsByArrayPosition(t *testing.T) {
	got := streamOllamaEvents(t,
		`{"message":{"tool_calls":[{"function":{"name":"first","arguments":{"first_part":true}}},{"function":{"name":"second","arguments":{"first_part":true}}}]},"done":false}`+"\n"+
			`{"message":{"tool_calls":[{"function":{"name":"","arguments":{"second_part":0}}},{"function":{"name":"","arguments":{"second_part":1}}}]},"done":true,"done_reason":"tool_calls"}`+"\n")

	assertOllamaEventTypes(t, got, "tool_call", "completed")
	calls := got[0].ToolCalls
	if len(calls) != 2 ||
		calls[0].Name != "first" || calls[0].Arguments["first_part"] != true || calls[0].Arguments["second_part"] != json.Number("0") ||
		calls[1].Name != "second" || calls[1].Arguments["first_part"] != true || calls[1].Arguments["second_part"] != json.Number("1") {
		t.Fatalf("fragmented legacy tool calls = %+v", calls)
	}
}

func TestOllamaCombinesExplicitAndImplicitToolCallIndices(t *testing.T) {
	got := streamOllamaEvents(t,
		`{"message":{"tool_calls":[{"id":"call_1","function":{"index":1,"name":"second","arguments":{"explicit":true}}}]},"done":false}`+"\n"+
			`{"message":{"tool_calls":[{"function":{"name":"first","arguments":{"implicit":true}}}]},"done":true,"done_reason":"tool_calls"}`+"\n")

	assertOllamaEventTypes(t, got, "tool_call", "completed")
	calls := got[0].ToolCalls
	if len(calls) != 2 || calls[0].Name != "first" || calls[1].ID != "call_1" || calls[1].Name != "second" {
		t.Fatalf("mixed-index tool calls = %+v", calls)
	}
}

func TestOllamaRejectsExplicitIndexCollisionWithImplicitPosition(t *testing.T) {
	got := streamOllamaEvents(t,
		`{"message":{"tool_calls":[{"function":{"name":"implicit","arguments":{}}},{"function":{"index":0,"name":"explicit","arguments":{}}}]},"done":true}`+"\n")

	assertOllamaEventTypes(t, got, "error")
	if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, "duplicate index 0") {
		t.Fatalf("events = %+v, want duplicate-index model_bad_chunk", got)
	}
}

func TestOllamaRequestSerializesTools(t *testing.T) {
	body := captureOllamaRequest(t, ChatRequest{
		Model: "llama3.2",
		Tools: []ToolDefinition{
			{Name: "get_weather", Description: "Get weather"},
			{Name: "search", Parameters: map[string]any{"type": "object", "required": []any{"query"}}},
		},
	})
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	first := tools[0].(map[string]any)
	if first["type"] != "function" {
		t.Fatalf("first tool = %#v", first)
	}
	firstFunction := first["function"].(map[string]any)
	if firstFunction["name"] != "get_weather" || firstFunction["description"] != "Get weather" {
		t.Fatalf("first function = %#v", firstFunction)
	}
	if parameters := firstFunction["parameters"].(map[string]any); parameters["type"] != "object" {
		t.Fatalf("default parameters = %#v", parameters)
	}
	secondFunction := tools[1].(map[string]any)["function"].(map[string]any)
	if _, present := secondFunction["description"]; present {
		t.Fatalf("empty description was serialized: %#v", secondFunction)
	}
	if required := secondFunction["parameters"].(map[string]any)["required"].([]any); len(required) != 1 || required[0] != "query" {
		t.Fatalf("parameters = %#v", secondFunction["parameters"])
	}

	withoutTools := captureOllamaRequest(t, ChatRequest{Model: "llama3.2"})
	if _, present := withoutTools["tools"]; present {
		t.Fatalf("empty tools were serialized: %#v", withoutTools)
	}
}

func TestOllamaRequestNestsOptions(t *testing.T) {
	body := captureOllamaRequest(t, ChatRequest{
		Model:       "llama3.2",
		Temperature: 0.25,
		MaxTokens:   321,
	})
	if _, present := body["temperature"]; present {
		t.Fatalf("top-level temperature was serialized: %#v", body)
	}
	if _, present := body["num_predict"]; present {
		t.Fatalf("top-level num_predict was serialized: %#v", body)
	}
	options, ok := body["options"].(map[string]any)
	if !ok || options["temperature"] != json.Number("0.25") || options["num_predict"] != json.Number("321") {
		t.Fatalf("options = %#v", body["options"])
	}

	withoutOptions := captureOllamaRequest(t, ChatRequest{Model: "llama3.2"})
	if _, present := withoutOptions["options"]; present {
		t.Fatalf("zero options were serialized: %#v", withoutOptions)
	}
}

func TestOllamaRequestSerializesProviderSpecificMessages(t *testing.T) {
	body := captureOllamaRequest(t, ChatRequest{
		Model: "llama3.2",
		Messages: []ChatMessage{
			{Role: "system", Content: "Be concise"},
			{Role: "user", Content: "Weather?", Name: "neutral-only"},
			{
				Role:    "assistant",
				Content: "",
				ToolCalls: []ToolCall{{
					ID:        "call_1",
					Name:      "get_weather",
					Arguments: map[string]any{"city": "Paris"},
				}},
			},
			{Role: "tool", Content: `{"temperature":72}`, Name: "get_weather", ToolCallID: "call_1"},
		},
	})
	messages := body["messages"].([]any)
	if len(messages) != 4 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].(map[string]any)["role"] != "system" || messages[0].(map[string]any)["content"] != "Be concise" ||
		messages[1].(map[string]any)["role"] != "user" || messages[1].(map[string]any)["content"] != "Weather?" {
		t.Fatalf("normal messages = %#v", messages[:2])
	}
	if _, present := messages[1].(map[string]any)["name"]; present {
		t.Fatalf("unsupported neutral name was serialized: %#v", messages[1])
	}
	assistant := messages[2].(map[string]any)
	toolCalls := assistant["tool_calls"].([]any)
	call := toolCalls[0].(map[string]any)
	if call["id"] != "call_1" {
		t.Fatalf("tool call ID = %#v, want call_1", call["id"])
	}
	if call["type"] != "function" {
		t.Fatalf("tool call type = %#v, want function", call["type"])
	}
	function := call["function"].(map[string]any)
	if function["name"] != "get_weather" || function["index"] != json.Number("0") {
		t.Fatalf("function = %#v", function)
	}
	if arguments, ok := function["arguments"].(map[string]any); !ok || arguments["city"] != "Paris" {
		t.Fatalf("arguments = %#v, want object", function["arguments"])
	}
	toolResult := messages[3].(map[string]any)
	if toolResult["role"] != "tool" || toolResult["content"] != `{"temperature":72}` ||
		toolResult["tool_name"] != "get_weather" || toolResult["tool_call_id"] != "call_1" {
		t.Fatalf("tool result = %#v", toolResult)
	}
}

func TestOllamaRequestPreservesEmptyToolCallCompatibility(t *testing.T) {
	body := captureOllamaRequest(t, ChatRequest{
		Model: "llama3.2",
		Messages: []ChatMessage{
			{Role: "assistant", ToolCalls: []ToolCall{{Name: "legacy"}}},
			{Role: "tool", Content: "ok"},
		},
	})
	messages := body["messages"].([]any)
	call := messages[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if _, present := call["id"]; present {
		t.Fatalf("empty ID was serialized: %#v", call)
	}
	function := call["function"].(map[string]any)
	if function["index"] != json.Number("0") {
		t.Fatalf("function index = %#v, want 0", function["index"])
	}
	toolResult := messages[1].(map[string]any)
	if _, present := toolResult["tool_call_id"]; present {
		t.Fatalf("empty tool_call_id was serialized: %#v", toolResult)
	}
	if _, present := toolResult["tool_name"]; present {
		t.Fatalf("empty tool_name was serialized: %#v", toolResult)
	}
}

func TestOllamaRequestDefaultsNilToolCallArguments(t *testing.T) {
	body := captureOllamaRequest(t, ChatRequest{
		Model: "llama3.2",
		Messages: []ChatMessage{{
			Role:      "assistant",
			ToolCalls: []ToolCall{{Name: "no_arguments"}},
		}},
	})
	function := body["messages"].([]any)[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	arguments, ok := function["arguments"].(map[string]any)
	if !ok || len(arguments) != 0 {
		t.Fatalf("arguments = %#v, want empty object", function["arguments"])
	}
}

func TestOllamaParsesMultipleToolCallsAndEmptyArguments(t *testing.T) {
	got := streamOllamaEvents(t,
		`{"message":{"role":"assistant","content":"using tools","tool_calls":[{"id":"call_1","function":{"index":1,"name":"second","arguments":{"count":2}}},{"id":"call_0","function":{"index":0,"name":"first","arguments":{}}}]},"done":false}`+"\n"+
			`{"done":true,"done_reason":"tool_calls"}`+"\n")
	assertOllamaEventTypes(t, got, "delta", "tool_call", "completed")
	if got[0].Text != "using tools" {
		t.Fatalf("delta = %+v", got[0])
	}
	if len(got[1].ToolCalls) != 2 || got[1].ToolCalls[0].Name != "first" ||
		got[1].ToolCalls[0].ID != "call_0" || got[1].ToolCalls[0].Arguments == nil ||
		got[1].ToolCalls[1].Name != "second" || got[1].ToolCalls[1].Arguments["count"] != json.Number("2") {
		t.Fatalf("tool calls = %+v", got[1].ToolCalls)
	}
	if got[2].FinishReason != "tool_calls" {
		t.Fatalf("completion = %+v", got[2])
	}
}

func TestOllamaMergesPartialToolCallArguments(t *testing.T) {
	got := streamOllamaEvents(t,
		`{"message":{"content":"first ","tool_calls":[{"id":"call_0","function":{"index":0,"name":"get_weather","arguments":{"query":"weather","details":{"days":3}}}}]},"done":false}`+"\n"+
			`{"message":{"content":"then ","tool_calls":[{"id":"call_0","function":{"index":0,"name":"get_weather","arguments":{"units":"celsius","details":{"lang":"fr"},"query":"weather"}}},{"id":"call_1","function":{"index":1,"name":"clock","arguments":{"city":"Paris"}}}]},"done":false}`+"\n"+
			`{"message":{"content":"done"},"done":true,"done_reason":"tool_calls"}`+"\n")

	assertOllamaEventTypes(t, got, "delta", "delta", "delta", "tool_call", "completed")
	if got[0].Text != "first " || got[1].Text != "then " || got[2].Text != "done" {
		t.Fatalf("content events = %+v", got[:3])
	}
	calls := got[3].ToolCalls
	if len(calls) != 2 || calls[0].ID != "call_0" || calls[0].Name != "get_weather" ||
		calls[0].Arguments["query"] != "weather" || calls[0].Arguments["units"] != "celsius" {
		t.Fatalf("tool calls = %+v", calls)
	}
	details, ok := calls[0].Arguments["details"].(map[string]any)
	if !ok || details["days"] != json.Number("3") || details["lang"] != "fr" {
		t.Fatalf("merged details = %#v", calls[0].Arguments["details"])
	}
	if calls[1].ID != "call_1" || calls[1].Name != "clock" || calls[1].Arguments["city"] != "Paris" {
		t.Fatalf("second call = %+v", calls[1])
	}
}

func TestOllamaRejectsConflictingToolCallIdentityFragments(t *testing.T) {
	tests := []struct {
		name        string
		firstID     string
		secondID    string
		firstName   string
		secondName  string
		wantMessage string
	}{
		{
			name:        "ID",
			firstID:     "call_0",
			secondID:    "call_changed",
			firstName:   "get_weather",
			secondName:  "get_weather",
			wantMessage: "conflicting ID",
		},
		{
			name:        "function name",
			firstID:     "call_0",
			secondID:    "call_0",
			firstName:   "get_weather",
			secondName:  "get_time",
			wantMessage: "conflicting function name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(
				"{\"message\":{\"tool_calls\":[{\"id\":%q,\"function\":{\"index\":0,\"name\":%q,\"arguments\":{}}}]},\"done\":false}\n"+
					"{\"message\":{\"tool_calls\":[{\"id\":%q,\"function\":{\"index\":0,\"name\":%q,\"arguments\":{}}}]},\"done\":true}\n",
				tt.firstID,
				tt.firstName,
				tt.secondID,
				tt.secondName,
			)

			got := streamOllamaEvents(t, body)

			assertOllamaEventTypes(t, got, "error")
			if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, tt.wantMessage) {
				t.Fatalf("events = %+v, want model_bad_chunk containing %q", got, tt.wantMessage)
			}
		})
	}
}

// Every tool mcp-system exposes takes no arguments (system.time, system.health
// and system.info all declare an empty schema), and Ollama's ToolCallFunction
// carries `arguments` with no omitempty, so a zero-argument call arrives as
// `"arguments":null` — or absent, from other OpenAI-compatible servers. Treating
// either as malformed fails the whole run and the tool is never dispatched,
// which is precisely what "what time is it?" produces against a live model.
func TestOllamaAcceptsZeroArgumentToolCalls(t *testing.T) {
	for name, toolCalls := range map[string]string{
		"arguments null":   `[{"function":{"name":"system.time","arguments":null}}]`,
		"arguments absent": `[{"function":{"name":"system.time"}}]`,
		"arguments empty":  `[{"function":{"name":"system.time","arguments":{}}}]`,
	} {
		t.Run(name, func(t *testing.T) {
			got := streamOllamaEvents(t, `{"message":{"tool_calls":`+toolCalls+`},"done":true,"done_reason":"stop"}`+"\n")
			var calls []ToolCall
			for _, event := range got {
				if len(event.ToolCalls) > 0 {
					calls = event.ToolCalls
				}
				if event.Type == "error" {
					t.Fatalf("zero-argument tool call rejected: %s: %s", event.Code, event.Message)
				}
			}
			if len(calls) != 1 {
				t.Fatalf("want 1 tool call, got %d (%+v)", len(calls), got)
			}
			if calls[0].Name != "system.time" {
				t.Fatalf("tool name = %q, want system.time", calls[0].Name)
			}
			// Downstream marshals these straight into the MCP request, so an
			// absent argument map must arrive as an empty object, never nil.
			if calls[0].Arguments == nil {
				t.Fatal("arguments must be an empty map, not nil")
			}
			if len(calls[0].Arguments) != 0 {
				t.Fatalf("arguments = %v, want empty", calls[0].Arguments)
			}
		})
	}
}

func TestOllamaRejectsMalformedToolCalls(t *testing.T) {
	tests := []struct {
		name      string
		toolCalls string
	}{
		{name: "null", toolCalls: `null`},
		{name: "not array", toolCalls: `{}`},
		{name: "entry not object", toolCalls: `["bad"]`},
		{name: "function missing", toolCalls: `[{}]`},
		{name: "function null", toolCalls: `[{"function":null}]`},
		{name: "name missing", toolCalls: `[{"function":{"arguments":{}}}]`},
		{name: "name empty", toolCalls: `[{"function":{"name":"","arguments":{}}}]`},
		{name: "name not string", toolCalls: `[{"function":{"name":42,"arguments":{}}}]`},
		// NOTE: absent and null `arguments` are NOT malformed — see
		// TestOllamaAcceptsZeroArgumentToolCalls. Only a present, non-object
		// value is a protocol error.
		{name: "arguments not object", toolCalls: `[{"function":{"name":"bad","arguments":[]}}]`},
		{name: "arguments string", toolCalls: `[{"function":{"name":"bad","arguments":"{}"}}]`},
		{name: "arguments number", toolCalls: `[{"function":{"name":"bad","arguments":3}}]`},
		{name: "null type", toolCalls: `[{"type":null,"function":{"name":"bad","arguments":{}}}]`},
		{name: "non-string type", toolCalls: `[{"type":42,"function":{"name":"bad","arguments":{}}}]`},
		{name: "other string type", toolCalls: `[{"type":"command","function":{"name":"bad","arguments":{}}}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamOllamaEvents(t, `{"message":{"tool_calls":`+tt.toolCalls+`},"done":true}`+"\n")
			assertOllamaEventTypes(t, got, "error")
			if got[0].Code != "model_bad_chunk" {
				t.Fatalf("event = %+v, want model_bad_chunk", got[0])
			}
		})
	}
}

func TestOllamaValidatesChunkEnvelopeFields(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantMessage string
	}{
		{name: "message null", body: `{"message":null,"done":false}`, wantMessage: "message must be an object"},
		{name: "message array", body: `{"message":[],"done":false}`, wantMessage: "message must be an object"},
		{name: "role null", body: `{"message":{"role":null},"done":false}`, wantMessage: "message role must be a string"},
		{name: "role number", body: `{"message":{"role":3},"done":false}`, wantMessage: "message role must be a string"},
		{name: "content null", body: `{"message":{"content":null},"done":false}`, wantMessage: "message content must be a string"},
		{name: "content object", body: `{"message":{"content":{}},"done":false}`, wantMessage: "message content must be a string"},
		{name: "tool calls object", body: `{"message":{"tool_calls":{}},"done":false}`, wantMessage: "tool_calls must be an array"},
		{name: "done missing", body: `{"message":{"content":"partial"}}`, wantMessage: "done must be a boolean"},
		{name: "done null", body: `{"done":null}`, wantMessage: "done must be a boolean"},
		{name: "done string", body: `{"done":"true"}`, wantMessage: "done must be a boolean"},
		{name: "done reason null", body: `{"done":true,"done_reason":null}`, wantMessage: "done_reason must be a string"},
		{name: "done reason number", body: `{"done":true,"done_reason":3}`, wantMessage: "done_reason must be a string"},
		{name: "nonterminal done reason", body: `{"done":false,"done_reason":"stop"}`, wantMessage: "done_reason is only valid on a terminal chunk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamOllamaEvents(t, tt.body+"\n")
			assertOllamaEventTypes(t, got, "error")
			if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, tt.wantMessage) {
				t.Fatalf("events = %+v, want model_bad_chunk containing %q", got, tt.wantMessage)
			}
		})
	}
}

func TestOllamaValidatesToolCallStreamIntegrity(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantMessage string
	}{
		{
			name:        "negative index",
			body:        `{"message":{"tool_calls":[{"function":{"index":-1,"name":"bad","arguments":{}}}]},"done":true}`,
			wantMessage: "negative index -1",
		},
		{
			name:        "non-integer index",
			body:        `{"message":{"tool_calls":[{"function":{"index":1.5,"name":"bad","arguments":{}}}]},"done":true}`,
			wantMessage: "index must be an integer",
		},
		{
			name:        "index out of range",
			body:        `{"message":{"tool_calls":[{"function":{"index":128,"name":"bad","arguments":{}}}]},"done":true}`,
			wantMessage: "index 128 exceeds",
		},
		{
			name:        "sparse indices",
			body:        `{"message":{"tool_calls":[{"function":{"index":1,"name":"bad","arguments":{}}}]},"done":true}`,
			wantMessage: "non-contiguous",
		},
		{
			name:        "duplicate indices in chunk",
			body:        `{"message":{"tool_calls":[{"function":{"index":0,"name":"first","arguments":{}}},{"function":{"index":0,"name":"again","arguments":{}}}]},"done":true}`,
			wantMessage: "duplicate index 0",
		},
		{
			name:        "duplicate IDs",
			body:        `{"message":{"tool_calls":[{"id":"duplicate","function":{"index":0,"name":"first","arguments":{}}},{"id":"duplicate","function":{"index":1,"name":"second","arguments":{}}}]},"done":true}`,
			wantMessage: `duplicate ID "duplicate"`,
		},
		{
			name:        "ID wrong type",
			body:        `{"message":{"tool_calls":[{"id":3,"function":{"index":0,"name":"bad","arguments":{}}}]},"done":true}`,
			wantMessage: "ID must be a string",
		},
		{
			name:        "index wrong type",
			body:        `{"message":{"tool_calls":[{"function":{"index":"0","name":"bad","arguments":{}}}]},"done":true}`,
			wantMessage: "index must be an integer",
		},
		{
			name:        "name wrong type",
			body:        `{"message":{"tool_calls":[{"function":{"index":0,"name":3,"arguments":{}}}]},"done":true}`,
			wantMessage: "name must be a string",
		},
		{
			name:        "missing final name",
			body:        `{"message":{"tool_calls":[{"function":{"index":0,"arguments":{}}}]},"done":true}`,
			wantMessage: "missing a function name",
		},
		{
			name: "conflicting scalar argument",
			body: `{"message":{"tool_calls":[{"function":{"index":0,"name":"tool","arguments":{"key":"first"}}}]},"done":false}` + "\n" +
				`{"message":{"tool_calls":[{"function":{"index":0,"name":"","arguments":{"key":"second"}}}]},"done":true}`,
			wantMessage: `conflicting argument "key"`,
		},
		{
			name: "conflicting argument type",
			body: `{"message":{"tool_calls":[{"function":{"index":0,"name":"tool","arguments":{"key":{"nested":true}}}}]},"done":false}` + "\n" +
				`{"message":{"tool_calls":[{"function":{"index":0,"name":"","arguments":{"key":"second"}}}]},"done":true}`,
			wantMessage: `conflicting argument "key"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamOllamaEvents(t, tt.body+"\n")
			assertOllamaEventTypes(t, got, "error")
			if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, tt.wantMessage) {
				t.Fatalf("events = %+v, want model_bad_chunk containing %q", got, tt.wantMessage)
			}
		})
	}
}

func TestOllamaTreatsEmptyToolCallsAsNoCalls(t *testing.T) {
	got := streamOllamaEvents(t, `{"message":{"content":"ordinary","tool_calls":[]},"done":true,"done_reason":"stop"}`+"\n")
	assertOllamaEventTypes(t, got, "delta", "completed")
	if got[0].Text != "ordinary" {
		t.Fatalf("delta = %+v", got[0])
	}
}

func TestOllamaBoundsToolCalls(t *testing.T) {
	t.Run("call count boundary accepted", func(t *testing.T) {
		got := streamOllamaEvents(t, ollamaToolCallsBody(t, maxOllamaToolCalls)+"\n")
		assertOllamaEventTypes(t, got, "tool_call", "completed")
		if len(got[0].ToolCalls) != maxOllamaToolCalls {
			t.Fatalf("tool call count = %d", len(got[0].ToolCalls))
		}
	})

	t.Run("call count overflow", func(t *testing.T) {
		got := streamOllamaEvents(t, ollamaToolCallsBody(t, maxOllamaToolCalls+1)+"\n")
		assertOllamaEventTypes(t, got, "error")
		if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, "count exceeds") {
			t.Fatalf("events = %+v", got)
		}
	})

	for _, field := range []string{"id", "name"} {
		t.Run(field+" boundary accepted", func(t *testing.T) {
			value := strings.Repeat("x", 1024)
			call := `{"id":"` + value + `","function":{"index":0,"name":"tool","arguments":{}}}`
			if field == "name" {
				call = `{"id":"call","function":{"index":0,"name":"` + value + `","arguments":{}}}`
			}
			got := streamOllamaEvents(t, `{"message":{"tool_calls":[`+call+`]},"done":true}`+"\n")
			assertOllamaEventTypes(t, got, "tool_call", "completed")
		})

		t.Run(field+" overflow", func(t *testing.T) {
			value := strings.Repeat("x", 1025)
			call := `{"id":"` + value + `","function":{"index":0,"name":"tool","arguments":{}}}`
			if field == "name" {
				call = `{"id":"call","function":{"index":0,"name":"` + value + `","arguments":{}}}`
			}
			got := streamOllamaEvents(t, `{"message":{"tool_calls":[`+call+`]},"done":true}`+"\n")
			assertOllamaEventTypes(t, got, "error")
			wantMessage := field + " exceeds"
			if field == "id" {
				wantMessage = "ID exceeds"
			}
			if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, wantMessage) {
				t.Fatalf("events = %+v", got)
			}
		})
	}

	t.Run("per-call arguments", func(t *testing.T) {
		fragment := strings.Repeat("x", maxOllamaToolCallArgumentBytes/2)
		body := `{"message":{"tool_calls":[{"function":{"index":0,"name":"tool","arguments":{"a":"` + fragment + `"}}}]},"done":false}` + "\n" +
			`{"message":{"tool_calls":[{"function":{"index":0,"name":"","arguments":{"b":"` + fragment + `"}}}]},"done":true}` + "\n"
		got := streamOllamaEvents(t, body)
		assertOllamaEventTypes(t, got, "error")
		if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, "arguments exceed") {
			t.Fatalf("events = %+v", got)
		}
	})

	t.Run("aggregate arguments", func(t *testing.T) {
		fragment := strings.Repeat("x", maxOllamaToolCallArgumentBytes/2)
		body := `{"message":{"tool_calls":[{"function":{"index":0,"name":"first","arguments":{"a":"` + fragment + `"}}}]},"done":false}` + "\n" +
			`{"message":{"tool_calls":[{"function":{"index":1,"name":"second","arguments":{"b":"` + fragment + `"}}}]},"done":true}` + "\n"
		got := streamOllamaEvents(t, body)
		assertOllamaEventTypes(t, got, "error")
		if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, "aggregate arguments exceed") {
			t.Fatalf("events = %+v", got)
		}
	})
}

func TestOllamaEmitsProviderError(t *testing.T) {
	got := streamOllamaEvents(t, `{"error":"model not found"}`+"\n"+`{"done":true}`+"\n")
	assertOllamaEventTypes(t, got, "error")
	if got[0].Code != "model_unavailable" || !strings.Contains(got[0].Message, "model not found") {
		t.Fatalf("event = %+v", got[0])
	}
}

func TestOllamaRejectsMalformedProviderErrors(t *testing.T) {
	for _, value := range []string{`null`, `""`, `" "`, `{}`, `42`} {
		t.Run(value, func(t *testing.T) {
			got := streamOllamaEvents(t, `{"error":`+value+`}`+"\n")
			assertOllamaEventTypes(t, got, "error")
			if got[0].Code != "model_bad_chunk" {
				t.Fatalf("event = %+v, want model_bad_chunk", got[0])
			}
		})
	}
}

func TestOllamaReportsEOFBeforeTerminalEvent(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantTypes []string
	}{
		{name: "empty", wantTypes: []string{"error"}},
		{name: "nonterminal", body: `{"message":{"content":"partial"},"done":false}` + "\n", wantTypes: []string{"delta", "error"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamOllamaEvents(t, tt.body)
			assertOllamaEventTypes(t, got, tt.wantTypes...)
			last := got[len(got)-1]
			if last.Code != "model_stream_error" || !strings.Contains(last.Message, "terminal event") {
				t.Fatalf("last event = %+v", last)
			}
		})
	}
}

func TestOllamaOversizedNDJSONLineReturnsBadChunk(t *testing.T) {
	got := streamOllamaEvents(t, strings.Repeat("x", maxStreamTokenBytes+1)+"\n")
	assertOllamaEventTypes(t, got, "error")
	if got[0].Code != "model_bad_chunk" {
		t.Fatalf("event = %+v, want model_bad_chunk", got[0])
	}
}

func TestOllamaEmitsOnlyOneTerminalSequence(t *testing.T) {
	got := streamOllamaEvents(t,
		`{"message":{"tool_calls":[{"id":"call_0","function":{"index":0,"name":"tool","arguments":{}}}]},"done":true,"done_reason":"tool_calls"}`+"\n"+
			`{"done":true,"done_reason":"stop"}`+"\n")
	assertOllamaEventTypes(t, got, "tool_call", "completed")
}

func TestOllamaClassifiesHTTPErrorStatus(t *testing.T) {
	tests := []struct {
		status int
		code   string
	}{
		{status: http.StatusRequestTimeout, code: "model_unavailable"},
		{status: http.StatusTooManyRequests, code: "model_unavailable"},
		{status: http.StatusServiceUnavailable, code: "model_unavailable"},
		{status: http.StatusUnauthorized, code: "model_auth_failed"},
		{status: http.StatusForbidden, code: "model_auth_failed"},
		{status: http.StatusBadRequest, code: "model_request_failed"},
		{status: 600, code: "model_request_failed"},
	}
	for _, test := range tests {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "provider error", test.status)
			}))
			t.Cleanup(server.Close)
			provider := NewOllama(server.URL, server.Client())
			events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "llama3.2"})
			if err != nil {
				t.Fatal(err)
			}
			got := collectEvents(events)
			assertOllamaEventTypes(t, got, "error")
			if got[0].Code != test.code || !strings.Contains(got[0].Message, fmt.Sprint(test.status)) {
				t.Fatalf("event = %+v, want code %q with status %d", got[0], test.code, test.status)
			}
		})
	}
}

func TestOllamaStreamStopsOnCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		fmt.Fprintln(w, `{"message":{"content":"partial"},"done":false}`)
		flusher.Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(ctx, ChatRequest{Model: "llama3.2"})
	if err != nil {
		t.Fatal(err)
	}
	if event := <-events; event.Type != "delta" {
		t.Fatalf("first event = %+v", event)
	}
	cancel()
	select {
	case _, open := <-events:
		if open {
			t.Fatal("event stream remained open after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("event stream did not terminate after cancellation")
	}
}

func captureOllamaRequest(t *testing.T, request ChatRequest) map[string]any {
	t.Helper()
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		requestBody <- body
		fmt.Fprintln(w, `{"done":true,"done_reason":"stop"}`)
	}))
	t.Cleanup(server.Close)
	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(events)

	var body map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(<-requestBody)))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return body
}

func streamOllamaEvents(t *testing.T, body string) []StreamEvent {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "llama3.2"})
	if err != nil {
		t.Fatal(err)
	}
	return collectEvents(events)
}

func assertOllamaEventTypes(t *testing.T, events []StreamEvent, types ...string) {
	t.Helper()
	if len(events) != len(types) {
		t.Fatalf("events = %+v, want types %v", events, types)
	}
	for index, eventType := range types {
		if events[index].Type != eventType {
			t.Fatalf("event %d = %+v, want type %q", index, events[index], eventType)
		}
	}
}

func ollamaToolCallsBody(t *testing.T, count int) string {
	t.Helper()
	calls := make([]any, count)
	for index := range calls {
		calls[index] = map[string]any{
			"id": fmt.Sprintf("call_%d", index),
			"function": map[string]any{
				"index":     index,
				"name":      "tool",
				"arguments": map[string]any{},
			},
		}
	}
	encoded, err := json.Marshal(map[string]any{
		"message": map[string]any{"tool_calls": calls},
		"done":    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func collectEvents(events <-chan StreamEvent) []StreamEvent {
	var got []StreamEvent
	for event := range events {
		got = append(got, event)
	}
	return got
}
