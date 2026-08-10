package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleStreamChatParsesSSEDeltaAndCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Hi"}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	provider := NewOpenAICompatible(server.URL, "test-key", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if got[0].Text != "Hi" || got[1].Type != "completed" {
		t.Fatalf("events = %+v", got)
	}
}

func TestOpenAIStreamsToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_","type":"function","function":{"name":"get_","arguments":"{\"city\":"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"1","function":{"name":"weather","arguments":"\"Paris\"}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}

	got := collectEvents(events)
	if len(got) != 2 {
		t.Fatalf("events = %+v, want tool_call and completed", got)
	}
	if got[0].Type != "tool_call" || len(got[0].ToolCalls) != 1 {
		t.Fatalf("tool call event = %+v", got[0])
	}
	call := got[0].ToolCalls[0]
	if call.ID != "call_1" || call.Name != "get_weather" || call.Arguments["city"] != "Paris" {
		t.Fatalf("tool call = %+v", call)
	}
	if got[1].Type != "completed" || got[1].FinishReason != "tool_calls" {
		t.Fatalf("completion = %+v", got[1])
	}
}

func TestOpenAIRequestSerializesFunctionTools(t *testing.T) {
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBody <- body
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Tools: []ToolDefinition{{
			Name:        "get_weather",
			Description: "Get the weather",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(events)

	var got map[string]any
	if err := json.Unmarshal(<-requestBody, &got); err != nil {
		t.Fatal(err)
	}
	tools, ok := got["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", got["tools"])
	}
	tool := tools[0].(map[string]any)
	function := tool["function"].(map[string]any)
	if tool["type"] != "function" || function["name"] != "get_weather" ||
		function["description"] != "Get the weather" || function["parameters"].(map[string]any)["type"] != "object" {
		t.Fatalf("tool = %#v", tool)
	}
}

func TestOpenAIRequestDefaultsNilToolParameters(t *testing.T) {
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBody <- body
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Tools: []ToolDefinition{{Name: "no_arguments"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(events)

	var got map[string]any
	if err := json.Unmarshal(<-requestBody, &got); err != nil {
		t.Fatal(err)
	}
	function := got["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	want := map[string]any{
		"name":       "no_arguments",
		"parameters": map[string]any{"type": "object"},
	}
	if !reflect.DeepEqual(function, want) {
		t.Fatalf("function = %#v, want %#v", function, want)
	}
}

func TestOpenAIRequestSerializesAssistantToolCalls(t *testing.T) {
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBody <- body
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []ChatMessage{{
			Role:    "assistant",
			Content: "",
			Name:    "planner",
			ToolCalls: []ToolCall{{
				ID:        "call_1",
				Name:      "get_weather",
				Arguments: map[string]any{"city": "Paris"},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(events)

	var got map[string]any
	if err := json.Unmarshal(<-requestBody, &got); err != nil {
		t.Fatal(err)
	}
	message := got["messages"].([]any)[0].(map[string]any)
	want := map[string]any{
		"role":    "assistant",
		"content": "",
		"name":    "planner",
		"tool_calls": []any{map[string]any{
			"id":   "call_1",
			"type": "function",
			"function": map[string]any{
				"name":      "get_weather",
				"arguments": `{"city":"Paris"}`,
			},
		}},
	}
	if !reflect.DeepEqual(message, want) {
		t.Fatalf("message = %#v, want %#v", message, want)
	}
}

func TestOpenAIRequestSerializesToolResultMessage(t *testing.T) {
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBody <- body
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []ChatMessage{{
			Role:       "tool",
			Content:    `{"temperature":72}`,
			Name:       "get_weather",
			ToolCallID: "call_1",
			ToolCalls: []ToolCall{{
				ID:        "provider-neutral-only",
				Name:      "must_not_be_flattened",
				Arguments: map[string]any{"ignored": true},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(events)

	var got map[string]any
	if err := json.Unmarshal(<-requestBody, &got); err != nil {
		t.Fatal(err)
	}
	message := got["messages"].([]any)[0].(map[string]any)
	want := map[string]any{
		"role":         "tool",
		"content":      `{"temperature":72}`,
		"tool_call_id": "call_1",
	}
	if !reflect.DeepEqual(message, want) {
		t.Fatalf("message = %#v, want %#v", message, want)
	}
}

func TestOpenAIStreamsToolCallsSortedByIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_1","function":{"name":"second","arguments":"{}"}},{"index":0,"id":"call_0","function":{"name":"first","arguments":"{}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
	}))
	t.Cleanup(server.Close)
	provider := NewOpenAICompatible(server.URL, "", server.Client())

	for attempt := 0; attempt < 20; attempt++ {
		events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
		if err != nil {
			t.Fatal(err)
		}
		got := collectEvents(events)
		calls := got[0].ToolCalls
		if len(calls) != 2 || calls[0].ID != "call_0" || calls[1].ID != "call_1" {
			t.Fatalf("tool calls = %+v, want index order", calls)
		}
	}
}

func TestOpenAIRejectsNonObjectToolArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_0","function":{"name":"bad","arguments":"null"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 1 || got[0].Type != "error" || got[0].Code != "model_bad_chunk" {
		t.Fatalf("events = %+v, want model_bad_chunk error", got)
	}
}

func TestOpenAIRejectsMalformedToolArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_0","function":{"name":"bad","arguments":"{}{}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 1 || got[0].Type != "error" || got[0].Code != "model_bad_chunk" {
		t.Fatalf("events = %+v, want model_bad_chunk error", got)
	}
}

func TestOpenAIStreamsEmptyToolArgumentsAsEmptyMap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_0","function":{"name":"no_arguments","arguments":""}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	arguments := got[0].ToolCalls[0].Arguments
	if arguments == nil || len(arguments) != 0 {
		t.Fatalf("arguments = %#v, want non-nil empty map", arguments)
	}
}

func TestOpenAIRequestOmitsToolsWhenNone(t *testing.T) {
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBody <- body
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(events)

	var got map[string]any
	if err := json.Unmarshal(<-requestBody, &got); err != nil {
		t.Fatal(err)
	}
	if _, exists := got["tools"]; exists {
		t.Fatalf("request includes tools: %#v", got["tools"])
	}
}

func TestOpenAIRequestSerializesNormalMessage(t *testing.T) {
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBody <- body
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []ChatMessage{{
			Role:       "user",
			Content:    "hello",
			Name:       "alice",
			ToolCallID: "provider-neutral-only",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(events)

	var got map[string]any
	if err := json.Unmarshal(<-requestBody, &got); err != nil {
		t.Fatal(err)
	}
	message := got["messages"].([]any)[0].(map[string]any)
	want := map[string]any{"role": "user", "content": "hello", "name": "alice"}
	if !reflect.DeepEqual(message, want) {
		t.Fatalf("message = %#v, want %#v", message, want)
	}
}

func TestOpenAIMalformedChunkReturnsErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, "data: {not-json}\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 1 || got[0].Type != "error" || got[0].Code != "model_bad_chunk" {
		t.Fatalf("events = %+v, want model_bad_chunk error", got)
	}
}

func TestOpenAIRejectsChunkWithTrailingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[]}{"extra":true}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 1 || got[0].Type != "error" || got[0].Code != "model_bad_chunk" {
		t.Fatalf("events = %+v, want model_bad_chunk error", got)
	}
}

func TestOpenAIScannerErrorReturnsStreamErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, "data: "+strings.Repeat("x", maxStreamTokenBytes+1)+"\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 1 || got[0].Type != "error" || got[0].Code != "model_stream_error" {
		t.Fatalf("events = %+v, want model_stream_error", got)
	}
}

func TestOpenAIStreamStopsOnCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"first"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(ctx, ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	if event := <-events; event.Type != "delta" || event.Text != "first" {
		t.Fatalf("first event = %+v", event)
	}
	cancel()

	select {
	case event, ok := <-events:
		if ok {
			t.Fatalf("event after cancellation = %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not close after cancellation")
	}
}
