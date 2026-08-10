package llm

import (
	"context"
	"encoding/json"
	"errors"
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
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"Hi"}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	provider := NewOpenAICompatible(server.URL, "test-key", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 2 || got[0].Text != "Hi" || got[1].Type != "completed" {
		t.Fatalf("events = %+v", got)
	}
}

func TestOpenAIChunkEmitsDeltaAndFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":"length"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 2 || got[0].Type != "delta" || got[0].Text != "Hi" ||
		got[1].Type != "completed" || got[1].FinishReason != "length" {
		t.Fatalf("events = %+v, want delta then length completion", got)
	}
}

func TestOpenAIReportsEOFBeforeTerminalEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"partial"}}]}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 2 || got[0].Type != "delta" || got[1].Type != "error" ||
		got[1].Code != "model_stream_error" || !strings.Contains(got[1].Message, "terminal event") {
		t.Fatalf("events = %+v, want delta then premature EOF stream error", got)
	}
}

func TestOpenAIReportsStreamErrorForNonterminalBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty"},
		{name: "comment only", body: ": keepalive\n\n"},
		{name: "non SSE", body: `{"choices":[]}`},
		{
			name: "unterminated data event",
			body: `data: {"choices":[{"index":0,"delta":{"content":"must not dispatch"}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "text/event-stream")
				fmt.Fprint(w, tt.body)
			}))
			t.Cleanup(server.Close)

			provider := NewOpenAICompatible(server.URL, "", server.Client())
			events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
			if err != nil {
				t.Fatal(err)
			}
			got := collectEvents(events)
			if len(got) != 1 || got[0].Type != "error" || got[0].Code != "model_stream_error" ||
				!strings.Contains(got[0].Message, "terminal event") {
				t.Fatalf("events = %+v, want terminal model_stream_error", got)
			}
		})
	}
}

func TestOpenAIReportsEOFWithPendingToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"pending","arguments":"{}"}}]}}]}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 1 || got[0].Type != "error" || got[0].Code != "model_stream_error" ||
		!strings.Contains(got[0].Message, "unfinished tool call") {
		t.Fatalf("events = %+v, want unfinished tool call stream error", got)
	}
}

func TestOpenAIRejectsDoneWithPendingToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"pending","arguments":"{}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 1 || got[0].Type != "error" || got[0].Code != "model_bad_chunk" ||
		!strings.Contains(got[0].Message, "[DONE]") || !strings.Contains(got[0].Message, "unfinished tool call") {
		t.Fatalf("events = %+v, want malformed premature DONE error", got)
	}
}

func TestOpenAIParsesMultilineSSEDataEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, ": keepalive\n")
		fmt.Fprint(w, "event: message\n")
		fmt.Fprint(w, "data: {\"choices\":[\n")
		fmt.Fprint(w, "id: ignored\n")
		fmt.Fprint(w, "data: {\"index\":0,\"delta\":{\"content\":\"multiline\"}}\n")
		fmt.Fprint(w, "data: ]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 2 || got[0].Type != "delta" || got[0].Text != "multiline" ||
		got[1].Type != "completed" {
		t.Fatalf("events = %+v, want multiline delta then completion", got)
	}
}

func TestOpenAIRejectsOversizedAggregateSSEEventData(t *testing.T) {
	halfLimit := strings.Repeat("x", maxStreamTokenBytes/2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\ndata: %s\ndata: x\n\n", halfLimit, halfLimit)
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 1 || got[0].Type != "error" || got[0].Code != "model_bad_chunk" ||
		!strings.Contains(got[0].Message, "event data exceeds") {
		t.Fatalf("events = %+v, want oversized event model_bad_chunk", got)
	}
}

func TestOpenAIParsesSSEStreamPreambleAndLineEndings(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "UTF-8 BOM",
			body: "\uFEFF" + `data: {"choices":[{"index":0,"delta":{"content":"bom"}}]}` + "\n\n" +
				"data: [DONE]\n\n",
		},
		{
			name: "CR-only separators",
			body: `data: {"choices":[{"index":0,"delta":{"content":"cr"}}]}` + "\r\r" +
				"data: [DONE]\r\r",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "text/event-stream")
				fmt.Fprint(w, tt.body)
			}))
			t.Cleanup(server.Close)

			provider := NewOpenAICompatible(server.URL, "", server.Client())
			events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
			if err != nil {
				t.Fatal(err)
			}
			got := collectEvents(events)
			if len(got) != 2 || got[0].Type != "delta" || got[1].Type != "completed" {
				t.Fatalf("events = %+v, want delta then completion", got)
			}
		})
	}
}

func TestOpenAIEmitsProviderErrorEnvelopeWithoutCompletion(t *testing.T) {
	got := streamOpenAIEvents(t,
		`data: {"error":{"message":"quota exceeded","code":"rate_limit_exceeded"}}`+"\n\n"+
			"data: [DONE]\n\n")

	assertOpenAIEventTypes(t, got, "error")
	if got[0].Code != "model_unavailable" ||
		got[0].Message != "OpenAI-compatible provider error (rate_limit_exceeded): quota exceeded" {
		t.Fatalf("error event = %+v", got[0])
	}
}

func TestOpenAIEmitsProviderErrorEnvelopeWithoutCodeOrCompletion(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "absent", data: `{"error":{"message":"quota exceeded"}}`},
		{name: "null", data: `{"error":{"message":"quota exceeded","code":null}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamOpenAIEvents(t, "data: "+tt.data+"\n\ndata: [DONE]\n\n")

			assertOpenAIEventTypes(t, got, "error")
			if got[0].Code != "model_unavailable" ||
				got[0].Message != "OpenAI-compatible provider error: quota exceeded" {
				t.Fatalf("error event = %+v", got[0])
			}
		})
	}
}

func TestOpenAIRejectsMalformedProviderErrorEnvelopes(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "null", data: `{"error":null}`},
		{name: "not object", data: `{"error":"unavailable"}`},
		{name: "missing message", data: `{"error":{"code":"rate_limit_exceeded"}}`},
		{name: "empty message", data: `{"error":{"message":"","code":"rate_limit_exceeded"}}`},
		{name: "non-string code", data: `{"error":{"message":"unavailable","code":429}}`},
		{
			name: "error and choices",
			data: `{"error":{"message":"unavailable","code":"provider_error"},"choices":[{"index":0,"delta":{}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamOpenAIEvents(t, "data: "+tt.data+"\n\ndata: [DONE]\n\n")
			assertOpenAIEventTypes(t, got, "error")
			if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, "error envelope") {
				t.Fatalf("events = %+v, want malformed error envelope model_bad_chunk", got)
			}
		})
	}
}

func TestOpenAIValidatesSingleChoiceEnvelope(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		wantMessage string
	}{
		{name: "missing choices", data: `{}`, wantMessage: "exactly one choice"},
		{name: "null choices", data: `{"choices":null}`, wantMessage: "exactly one choice"},
		{name: "empty choices", data: `{"choices":[]}`, wantMessage: "exactly one choice"},
		{
			name:        "multiple choices",
			data:        `{"choices":[{"index":0,"delta":{}},{"index":1,"delta":{}}]}`,
			wantMessage: "exactly one choice",
		},
		{
			name:        "missing choice index",
			data:        `{"choices":[{"delta":{"content":"invalid"}}]}`,
			wantMessage: "missing index",
		},
		{
			name:        "nonzero choice index",
			data:        `{"choices":[{"index":1,"delta":{"content":"invalid"}}]}`,
			wantMessage: "index 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamOpenAIEvents(t, "data: "+tt.data+"\n\ndata: [DONE]\n\n")
			assertOpenAIEventTypes(t, got, "error")
			if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, tt.wantMessage) {
				t.Fatalf("events = %+v, want model_bad_chunk containing %q", got, tt.wantMessage)
			}
		})
	}
}

func TestOpenAIStreamsToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_","type":"function","function":{"name":"get_","arguments":"{\"city\":"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"1","function":{"name":"weather","arguments":"\"Paris\"}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}

	got := collectEvents(events)
	assertOpenAIEventTypes(t, got, "tool_call", "completed")
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
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tool = %#v, want object", tools[0])
	}
	function, ok := tool["function"].(map[string]any)
	if !ok {
		t.Fatalf("function = %#v, want object", tool["function"])
	}
	parameters, ok := function["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters = %#v, want object", function["parameters"])
	}
	if tool["type"] != "function" || function["name"] != "get_weather" ||
		function["description"] != "Get the weather" || parameters["type"] != "object" {
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
	tool := requireSingleObject(t, got["tools"], "tools")
	function, ok := tool["function"].(map[string]any)
	if !ok {
		t.Fatalf("function = %#v, want object", tool["function"])
	}
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
	message := requireSingleObject(t, got["messages"], "messages")
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

func TestOpenAIRequestSerializesNilAssistantToolArgumentsAsObject(t *testing.T) {
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
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   "call_1",
				Name: "no_arguments",
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
	message := requireSingleObject(t, got["messages"], "messages")
	toolCall := requireSingleObject(t, message["tool_calls"], "tool_calls")
	function, ok := toolCall["function"].(map[string]any)
	if !ok {
		t.Fatalf("function = %#v, want object", toolCall["function"])
	}
	if function["arguments"] != "{}" {
		t.Fatalf("function arguments = %#v, want %q", function["arguments"], "{}")
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
	message := requireSingleObject(t, got["messages"], "messages")
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
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_1","function":{"name":"second","arguments":"{}"}},{"index":0,"id":"call_0","function":{"name":"first","arguments":"{}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
	}))
	t.Cleanup(server.Close)
	provider := NewOpenAICompatible(server.URL, "", server.Client())

	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	assertOpenAIEventTypes(t, got, "tool_call", "completed")
	calls := got[0].ToolCalls
	if len(calls) != 2 || calls[0].ID != "call_0" || calls[1].ID != "call_1" {
		t.Fatalf("tool calls = %+v, want index order", calls)
	}
}

func TestOpenAIRejectsNullToolArguments(t *testing.T) {
	body := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","function":{"name":"bad","arguments":null}}]}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"

	got := streamOpenAIEvents(t, body)
	assertOpenAIEventTypes(t, got, "error")
	if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, "arguments must be a string") {
		t.Fatalf("events = %+v, want null arguments model_bad_chunk and no completion", got)
	}
}

func TestOpenAIRejectsNonObjectToolArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","function":{"name":"bad","arguments":"null"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
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
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","function":{"name":"bad","arguments":"{}{}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
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

func TestOpenAIValidatesToolCallStreamIntegrity(t *testing.T) {
	tests := []struct {
		name        string
		stream      string
		wantMessage string
	}{
		{
			name:        "negative index",
			stream:      `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":-1,"id":"call_0","type":"function","function":{"name":"bad","arguments":"{}"}}]}}]}` + "\n\n",
			wantMessage: "negative index -1",
		},
		{
			name: "missing index",
			stream: `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_0","type":"function","function":{"name":"bad","arguments":"{}"}}]}}]}` + "\n\n" +
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			wantMessage: "missing index",
		},
		{
			name: "index set starts above zero",
			stream: `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_1","type":"function","function":{"name":"sparse","arguments":"{}"}}]}}]}` + "\n\n" +
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			wantMessage: "non-contiguous",
		},
		{
			name: "index set contains gap",
			stream: `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"first","arguments":"{}"}},{"index":2,"id":"call_2","type":"function","function":{"name":"third","arguments":"{}"}}]}}]}` + "\n\n" +
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			wantMessage: "non-contiguous",
		},
		{
			name: "duplicate IDs",
			stream: `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"duplicate","type":"function","function":{"name":"first","arguments":"{}"}},{"index":1,"id":"duplicate","type":"function","function":{"name":"second","arguments":"{}"}}]}}]}` + "\n\n" +
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			wantMessage: `duplicate ID "duplicate"`,
		},
		{
			name:        "unsupported type",
			stream:      `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"computer","function":{"name":"bad","arguments":"{}"}}]}}]}` + "\n\n",
			wantMessage: `unsupported type "computer"`,
		},
		{
			name:        "finish without calls",
			stream:      `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			wantMessage: "without any accumulated tool calls",
		},
		{
			name: "non-tool finish with pending call",
			stream: `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"pending","arguments":"{}"}}]}}]}` + "\n\n" +
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n",
			wantMessage: `finish_reason "stop" received with unfinished tool calls`,
		},
		{
			name: "missing ID",
			stream: `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"missing_id","arguments":"{}"}}]}}]}` + "\n\n" +
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			wantMessage: "tool call 0 is missing an ID",
		},
		{
			name: "missing name",
			stream: `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"arguments":"{}"}}]}}]}` + "\n\n" +
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			wantMessage: "tool call 0 is missing a function name",
		},
		{
			name: "malformed arguments",
			stream: `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"bad","arguments":"{}{}"}}]}}]}` + "\n\n" +
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			wantMessage: "tool call 0 arguments are malformed",
		},
		{
			name: "non-object arguments",
			stream: `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"bad","arguments":"[]"}}]}}]}` + "\n\n" +
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			wantMessage: "tool call 0 arguments must be a JSON object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "text/event-stream")
				fmt.Fprint(w, tt.stream)
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(server.Close)

			provider := NewOpenAICompatible(server.URL, "", server.Client())
			events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
			if err != nil {
				t.Fatal(err)
			}
			got := collectEvents(events)
			assertOpenAIEventTypes(t, got, "error")
			if got[0].Code != "model_bad_chunk" ||
				!strings.Contains(got[0].Message, tt.wantMessage) {
				t.Fatalf("events = %+v, want model_bad_chunk containing %q", got, tt.wantMessage)
			}
		})
	}
}

func TestOpenAIBoundsResponseWideToolCallState(t *testing.T) {
	t.Run("call count", func(t *testing.T) {
		state := newOpenAIStreamState()
		for index := 0; index < maxOpenAIToolCalls-1; index++ {
			state.toolCalls[index] = &openAIToolCall{}
		}
		if _, _, err := parseOpenAIData(openAIToolFragment(maxOpenAIToolCalls-1, "i", "n", ""), state); err != nil {
			t.Fatalf("fragment reaching call count boundary: %v", err)
		}
		if _, _, err := parseOpenAIData(openAIToolFragment(maxOpenAIToolCalls, "i", "n", ""), state); err == nil ||
			!strings.Contains(err.Error(), "tool call count exceeds") {
			t.Fatalf("overflow error = %v, want tool call count bound", err)
		}
	})

	t.Run("aggregate arguments", func(t *testing.T) {
		state := newOpenAIStreamState()
		state.argumentBytes = maxOpenAIToolCallAggregateArgumentBytes - 1
		if _, _, err := parseOpenAIData(openAIToolFragment(0, "", "", "x"), state); err != nil {
			t.Fatalf("fragment reaching argument boundary: %v", err)
		}
		if _, _, err := parseOpenAIData(openAIToolFragment(0, "", "", "x"), state); err == nil ||
			!strings.Contains(err.Error(), "aggregate arguments exceed") {
			t.Fatalf("overflow error = %v, want aggregate argument bound", err)
		}
	})

	t.Run("ID and name bytes", func(t *testing.T) {
		state := newOpenAIStreamState()
		state.identifierBytes = maxOpenAIToolCallAggregateIdentifierBytes - 2
		if _, _, err := parseOpenAIData(openAIToolFragment(0, "i", "n", ""), state); err != nil {
			t.Fatalf("fragment reaching identifier boundary: %v", err)
		}
		if _, _, err := parseOpenAIData(openAIToolFragment(0, "i", "", ""), state); err == nil ||
			!strings.Contains(err.Error(), "ID and name bytes exceed") {
			t.Fatalf("overflow error = %v, want identifier byte bound", err)
		}
	})
}

func TestOpenAIRejectsPerCallToolArgumentOverflow(t *testing.T) {
	halfLimit := strings.Repeat("x", maxOpenAIToolCallArgumentBytes/2)
	body := "data: " + string(openAIToolFragment(0, "call_0", "oversized", halfLimit)) + "\n\n" +
		"data: " + string(openAIToolFragment(0, "", "", halfLimit)) + "\n\n" +
		"data: " + string(openAIToolFragment(0, "", "", "x")) + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	got := streamOpenAIEvents(t, body)
	assertOpenAIEventTypes(t, got, "error")
	wantMessage := fmt.Sprintf("tool call 0 arguments exceed %d bytes", maxOpenAIToolCallArgumentBytes)
	if got[0].Code != "model_bad_chunk" || got[0].Message != wantMessage {
		t.Fatalf("events = %+v, want only per-call overflow error %q", got, wantMessage)
	}
}

func TestOpenAIStreamsEmptyToolArgumentsAsEmptyMap(t *testing.T) {
	tests := []struct {
		name     string
		function string
	}{
		{name: "omitted initial fragment", function: `{"name":"no_arguments"}`},
		{name: "empty string", function: `{"name":"no_arguments","arguments":""}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","function":` + tt.function + `}]}}]}` + "\n\n" +
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"
			got := streamOpenAIEvents(t, body)
			assertOpenAIEventTypes(t, got, "tool_call", "completed")
			if len(got[0].ToolCalls) != 1 {
				t.Fatalf("events = %+v, want one tool call", got)
			}
			arguments := got[0].ToolCalls[0].Arguments
			if arguments == nil || len(arguments) != 0 {
				t.Fatalf("arguments = %#v, want non-nil empty map", arguments)
			}
		})
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
	message := requireSingleObject(t, got["messages"], "messages")
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

func TestOpenAIOversizedPhysicalSSELineReturnsBadChunk(t *testing.T) {
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
	if len(got) != 1 || got[0].Type != "error" || got[0].Code != "model_bad_chunk" {
		t.Fatalf("events = %+v, want model_bad_chunk", got)
	}
}

func TestOpenAIReaderErrorReturnsStreamErrorEvent(t *testing.T) {
	readErr := errors.New("provider connection reset")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &failingReadCloser{err: readErr},
			Header:     make(http.Header),
		}, nil
	})}
	provider := NewOpenAICompatible("http://provider.example", "", client)

	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	assertOpenAIEventTypes(t, got, "error")
	if got[0].Code != "model_stream_error" || !strings.Contains(got[0].Message, readErr.Error()) {
		t.Fatalf("events = %+v, want reader model_stream_error", got)
	}
}

func TestOpenAIStreamStopsOnCancellation(t *testing.T) {
	requestCanceled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"first"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			requestCanceled <- struct{}{}
		case <-time.After(time.Second):
		}
	}))
	t.Cleanup(func() {
		server.CloseClientConnections()
		server.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(ctx, ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Type != "delta" || event.Text != "first" {
			t.Fatalf("first event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not emit first event")
	}
	cancel()

	select {
	case event, ok := <-events:
		if ok {
			t.Fatalf("event after cancellation = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not close after cancellation")
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("request context was not canceled")
	}
}

func requireSingleObject(t *testing.T, value any, label string) map[string]any {
	t.Helper()
	values, ok := value.([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("%s = %#v, want one object", label, value)
	}
	object, ok := values[0].(map[string]any)
	if !ok {
		t.Fatalf("%s[0] = %#v, want object", label, values[0])
	}
	return object
}

func streamOpenAIEvents(t *testing.T, body string) []StreamEvent {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	return collectEvents(events)
}

func assertOpenAIEventTypes(t *testing.T, events []StreamEvent, want ...string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("event types = %+v, want %v", events, want)
	}
	for index, event := range events {
		if event.Type != want[index] {
			t.Fatalf("event %d type = %q, want %q; events = %+v", index, event.Type, want[index], events)
		}
	}
}

func openAIToolFragment(index int, id, name, arguments string) []byte {
	return fmt.Appendf(nil,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":%d,"id":%q,"function":{"name":%q,"arguments":%q}}]}}]}`,
		index, id, name, arguments)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type failingReadCloser struct {
	err error
}

func (r *failingReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r *failingReadCloser) Close() error {
	return nil
}
