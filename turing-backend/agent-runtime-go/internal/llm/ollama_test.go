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
		w.Write([]byte(`{"message":{"role":"assistant","content":"ignored","tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Paris"}}}]},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	t.Cleanup(server.Close)

	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "llama3.2"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 2 || got[0].Type != "tool_call" || got[1].Type != "completed" {
		t.Fatalf("events = %+v, want tool_call then completed", got)
	}
	if got[0].Text != "" || len(got[0].ToolCalls) != 1 {
		t.Fatalf("tool call event = %+v", got[0])
	}
	call := got[0].ToolCalls[0]
	if call.ID != "" || call.Name != "get_weather" || call.Arguments["city"] != "Paris" {
		t.Fatalf("tool call = %+v", call)
	}
	if got[1].FinishReason != "stop" {
		t.Fatalf("completion = %+v, want stop reason", got[1])
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
	if _, present := call["id"]; present {
		t.Fatalf("unsupported tool call ID was serialized: %#v", call)
	}
	if _, present := call["type"]; present {
		t.Fatalf("unsupported tool call type was serialized: %#v", call)
	}
	function := call["function"].(map[string]any)
	if function["name"] != "get_weather" {
		t.Fatalf("function = %#v", function)
	}
	if arguments, ok := function["arguments"].(map[string]any); !ok || arguments["city"] != "Paris" {
		t.Fatalf("arguments = %#v, want object", function["arguments"])
	}
	toolResult := messages[3].(map[string]any)
	if toolResult["role"] != "tool" || toolResult["content"] != `{"temperature":72}` || toolResult["tool_name"] != "get_weather" {
		t.Fatalf("tool result = %#v", toolResult)
	}
	if _, present := toolResult["tool_call_id"]; present {
		t.Fatalf("OpenAI-only tool_call_id was serialized: %#v", toolResult)
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
		`{"message":{"role":"assistant","content":"must not stream","tool_calls":[{"function":{"name":"first","arguments":{}}},{"function":{"name":"second","arguments":{"count":2}}}]},"done":false}`+"\n"+
			`{"done":true,"done_reason":"tool_calls"}`+"\n")
	assertOllamaEventTypes(t, got, "tool_call", "completed")
	if len(got[0].ToolCalls) != 2 || got[0].ToolCalls[0].Name != "first" ||
		got[0].ToolCalls[0].ID != "" || got[0].ToolCalls[0].Arguments == nil ||
		got[0].ToolCalls[1].Name != "second" || got[0].ToolCalls[1].Arguments["count"] != json.Number("2") {
		t.Fatalf("tool calls = %+v", got[0].ToolCalls)
	}
	if got[1].FinishReason != "tool_calls" {
		t.Fatalf("completion = %+v", got[1])
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
		{name: "arguments missing", toolCalls: `[{"function":{"name":"bad"}}]`},
		{name: "arguments null", toolCalls: `[{"function":{"name":"bad","arguments":null}}]`},
		{name: "arguments not object", toolCalls: `[{"function":{"name":"bad","arguments":[]}}]`},
		{name: "unexpected ID", toolCalls: `[{"id":"call_1","function":{"name":"bad","arguments":{}}}]`},
		{name: "unexpected type", toolCalls: `[{"type":"function","function":{"name":"bad","arguments":{}}}]`},
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

func TestOllamaTreatsEmptyToolCallsAsNoCalls(t *testing.T) {
	got := streamOllamaEvents(t, `{"message":{"content":"ordinary","tool_calls":[]},"done":true,"done_reason":"stop"}`+"\n")
	assertOllamaEventTypes(t, got, "delta", "completed")
	if got[0].Text != "ordinary" {
		t.Fatalf("delta = %+v", got[0])
	}
}

func TestOllamaBoundsToolCalls(t *testing.T) {
	t.Run("call count", func(t *testing.T) {
		calls := make([]any, 129)
		for index := range calls {
			calls[index] = map[string]any{"function": map[string]any{"name": "tool", "arguments": map[string]any{}}}
		}
		if _, err := parseOllamaToolCalls(calls); err == nil || !strings.Contains(err.Error(), "count exceeds") {
			t.Fatalf("error = %v, want tool call count bound", err)
		}
	})

	t.Run("encoded arguments", func(t *testing.T) {
		calls := []any{map[string]any{
			"function": map[string]any{
				"name":      "tool",
				"arguments": map[string]any{"value": strings.Repeat("x", maxStreamTokenBytes+1)},
			},
		}}
		if _, err := parseOllamaToolCalls(calls); err == nil || !strings.Contains(err.Error(), "arguments exceed") {
			t.Fatalf("error = %v, want encoded argument bound", err)
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

func TestOllamaOversizedNDJSONLineReturnsStreamError(t *testing.T) {
	got := streamOllamaEvents(t, strings.Repeat("x", maxStreamTokenBytes+1)+"\n")
	assertOllamaEventTypes(t, got, "error")
	if got[0].Code != "model_stream_error" {
		t.Fatalf("event = %+v, want model_stream_error", got[0])
	}
}

func TestOllamaHTTPErrorReturnsUnavailableEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "llama3.2"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	assertOllamaEventTypes(t, got, "error")
	if got[0].Code != "model_unavailable" {
		t.Fatalf("event = %+v", got[0])
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

func collectEvents(events <-chan StreamEvent) []StreamEvent {
	var got []StreamEvent
	for event := range events {
		got = append(got, event)
	}
	return got
}
