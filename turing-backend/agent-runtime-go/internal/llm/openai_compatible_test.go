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
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleStreamChatAcceptsRequiredFixtureWithoutChoiceIndex(t *testing.T) {
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
	if len(got) != 2 || got[0].Text != "Hi" || got[1].Type != "completed" {
		t.Fatalf("events = %+v", got)
	}
}

func TestOpenAICompatibleContextWindowDoesNotChangeWireFormat(t *testing.T) {
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		requestBody <- body
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider, err := NewOpenAICompatibleWithLimits(server.URL, "", server.Client(), 6144, 512)
	if err != nil {
		t.Fatal(err)
	}
	events, err := provider.StreamChat(context.Background(), ChatRequest{
		Model:    "compatible-model",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(events)

	var body map[string]any
	if err := json.Unmarshal(<-requestBody, &body); err != nil {
		t.Fatal(err)
	}
	if _, present := body["num_ctx"]; present {
		t.Fatalf("OpenAI-compatible request contained num_ctx: %#v", body)
	}
	if _, present := body["options"]; present {
		t.Fatalf("OpenAI-compatible request contained Ollama options: %#v", body)
	}
	if body["max_tokens"] != float64(512) {
		t.Fatalf("max_tokens = %#v, want 512", body["max_tokens"])
	}
	if got := provider.ContextWindowTokens(); got != 6144 {
		t.Fatalf("ContextWindowTokens = %d, want 6144", got)
	}
	if got := provider.MaxOutputTokens(); got != 512 {
		t.Fatalf("MaxOutputTokens = %d, want 512", got)
	}
}

func TestOpenAICompatibleLimitsRejectInvalidValues(t *testing.T) {
	if _, err := NewOpenAICompatibleWithLimits("http://example.test", "", nil, 1024, 1024); err == nil {
		t.Fatal("output reservation equal to the context window was accepted")
	}
}

func TestOpenAIReasoningModelUsesMaxCompletionTokens(t *testing.T) {
	for _, model := range []string{
		"o1",
		"o3-mini",
		"o4-mini",
		"openai/o3",
		"gpt-5",
		"gpt-5-mini",
		"openai/gpt-5.1",
	} {
		t.Run(model, func(t *testing.T) {
			_, body := captureOpenAIRequestForModel(t, model, nil, nil)
			if body["max_completion_tokens"] != float64(DefaultMaxOutputTokens) {
				t.Fatalf("max_completion_tokens = %#v, want %d", body["max_completion_tokens"], DefaultMaxOutputTokens)
			}
			if _, present := body["max_tokens"]; present {
				t.Fatalf("o-series request included incompatible max_tokens: %#v", body)
			}
		})
	}
}

func TestOpenAILegacyCompatibleModelsKeepMaxTokens(t *testing.T) {
	for _, model := range []string{"gpt-4o-mini", "claude-sonnet-4"} {
		t.Run(model, func(t *testing.T) {
			_, body := captureOpenAIRequestForModel(t, model, nil, nil)
			if body["max_tokens"] != float64(DefaultMaxOutputTokens) {
				t.Fatalf("max_tokens = %#v, want %d", body["max_tokens"], DefaultMaxOutputTokens)
			}
			if _, present := body["max_completion_tokens"]; present {
				t.Fatalf("legacy-compatible request included max_completion_tokens: %#v", body)
			}
		})
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

func TestOpenAIReportsLengthWithPendingToolCallAsOutputLimitCompletion(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[{"index":0,"delta":{"content":"partial","tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"files_create","arguments":"{\"path\":\""}}]}}]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`+"\n\n",
	)

	assertOpenAIEventTypes(t, got, "delta", "completed")
	if got[0].Text != "partial" || got[1].FinishReason != "length" {
		t.Fatalf("events = %+v, want partial delta then length completion", got)
	}
}

func TestOpenAIEmitsCompleteToolCallBeforeLengthCompletion(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"files_create","arguments":"{\"path\":\"note.txt\"}"}}]}}]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`+"\n\n",
	)

	assertOpenAIEventTypes(t, got, "tool_call", "completed")
	if len(got[0].ToolCalls) != 1 ||
		got[0].ToolCalls[0].ID != "call_0" ||
		got[0].ToolCalls[0].Name != "files_create" ||
		got[0].ToolCalls[0].Arguments["path"] != "note.txt" ||
		got[1].FinishReason != "length" {
		t.Fatalf("events = %+v, want complete tool call then length completion", got)
	}
}

func TestOpenAIEmitsCompleteCallsAndDropsIncompleteCallsAtLength(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_complete","type":"function","function":{"name":"files_create","arguments":"{\"path\":\"note.txt\"}"}},{"index":1,"id":"call_incomplete","type":"function","function":{"name":"files_update","arguments":"{\"path\":\""}}]}}]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`+"\n\n",
	)

	assertOpenAIEventTypes(t, got, "tool_call", "completed")
	if len(got[0].ToolCalls) != 1 ||
		got[0].ToolCalls[0].ID != "call_complete" ||
		got[0].ToolCalls[0].Name != "files_create" ||
		got[1].FinishReason != "length" {
		t.Fatalf("events = %+v, want only complete call before length completion", got)
	}
}

func TestOpenAIRejectsSparseToolCallIndicesAtLength(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"first","arguments":"{}"}},{"index":2,"id":"call_2","type":"function","function":{"name":"third","arguments":"{}"}}]}}]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`+"\n\n",
	)

	assertOpenAIEventTypes(t, got, "error")
	if got[0].Code != "model_bad_chunk" ||
		!strings.Contains(got[0].Message, "non-contiguous") {
		t.Fatalf("events = %+v, want sparse-index model_bad_chunk", got)
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

func TestOpenAIClassifiesStreamedProviderErrorEnvelopes(t *testing.T) {
	tests := []struct {
		name        string
		errorJSON   string
		wantCode    string
		wantMessage string
	}{
		{
			name:        "authentication type with null code",
			errorJSON:   `{"message":"API key rejected","type":"authentication_error","code":null}`,
			wantCode:    "model_auth_failed",
			wantMessage: "OpenAI-compatible provider error: API key rejected",
		},
		{
			name:        "permission code with absent type",
			errorJSON:   `{"message":"Permission denied","code":"permission_denied"}`,
			wantCode:    "model_auth_failed",
			wantMessage: "OpenAI-compatible provider error (permission_denied): Permission denied",
		},
		{
			name:        "invalid request type with absent code",
			errorJSON:   `{"message":"Invalid input","type":"invalid_request_error"}`,
			wantCode:    "model_request_failed",
			wantMessage: "OpenAI-compatible provider error: Invalid input",
		},
		{
			name:        "model not found code with null type",
			errorJSON:   `{"message":"Unknown model","type":null,"code":"model_not_found"}`,
			wantCode:    "model_request_failed",
			wantMessage: "OpenAI-compatible provider error (model_not_found): Unknown model",
		},
		{
			name:        "invalid prompt code",
			errorJSON:   `{"message":"Prompt rejected","code":"invalid_prompt"}`,
			wantCode:    "model_request_failed",
			wantMessage: "OpenAI-compatible provider error (invalid_prompt): Prompt rejected",
		},
		{
			name:        "context error from message with null fields",
			errorJSON:   `{"message":"maximum context length exceeded","type":null,"code":null}`,
			wantCode:    "model_request_failed",
			wantMessage: "OpenAI-compatible provider error: maximum context length exceeded",
		},
		{
			name:        "rate limit code with null type",
			errorJSON:   `{"message":"Try later","type":null,"code":"rate_limit_exceeded"}`,
			wantCode:    "model_unavailable",
			wantMessage: "OpenAI-compatible provider error (rate_limit_exceeded): Try later",
		},
		{
			name:        "insufficient quota code",
			errorJSON:   `{"message":"Add credits","code":"insufficient_quota"}`,
			wantCode:    "model_quota_exceeded",
			wantMessage: "OpenAI-compatible provider error (insufficient_quota): Add credits",
		},
		{
			name:        "billing hard limit type",
			errorJSON:   `{"message":"Billing limit reached","type":"billing_hard_limit_reached"}`,
			wantCode:    "model_quota_exceeded",
			wantMessage: "OpenAI-compatible provider error: Billing limit reached",
		},
		{
			name:        "server type with absent code",
			errorJSON:   `{"message":"Provider failed","type":"server_error"}`,
			wantCode:    "model_unavailable",
			wantMessage: "OpenAI-compatible provider error: Provider failed",
		},
		{
			name:        "overloaded from message with absent fields",
			errorJSON:   `{"message":"The service is overloaded"}`,
			wantCode:    "model_unavailable",
			wantMessage: "OpenAI-compatible provider error: The service is overloaded",
		},
		{
			name:        "unknown strings default to permanent model error",
			errorJSON:   `{"message":"provider-specific failure","type":"vendor_error","code":"vendor_code"}`,
			wantCode:    "model_error",
			wantMessage: "OpenAI-compatible provider error (vendor_code): provider-specific failure",
		},
		{
			name:        "unknown absent fields default to permanent model error",
			errorJSON:   `{"message":"provider-specific failure"}`,
			wantCode:    "model_error",
			wantMessage: "OpenAI-compatible provider error: provider-specific failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamOpenAIEvents(t, "data: {\"error\":"+tt.errorJSON+"}\n\ndata: [DONE]\n\n")

			assertOpenAIEventTypes(t, got, "error")
			if got[0].Code != tt.wantCode || got[0].Message != tt.wantMessage {
				t.Fatalf("error event = %+v, want code %q and message %q", got[0], tt.wantCode, tt.wantMessage)
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
		{name: "non-string type", data: `{"error":{"message":"unavailable","type":429}}`},
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
		{
			name:        "multiple choices",
			data:        `{"choices":[{"index":0,"delta":{}},{"index":1,"delta":{}}]}`,
			wantMessage: "exactly one choice",
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

func TestOpenAIIgnoresEmptyChoiceUsageAndKeepaliveChunks(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":0}}`+"\n\n"+
			"data: "+`{"choices":[]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{"content":"done"}}]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
			"data: [DONE]\n\n",
	)

	assertOpenAIEventTypes(t, got, "delta", "completed")
	if got[0].Text != "done" || got[1].FinishReason != "stop" {
		t.Fatalf("events = %+v, want normal completion after ignored chunks", got)
	}
	// The choiceless chunk carries no text, but it does carry the token counts,
	// and a completion_tokens of 0 reported by the provider is a measurement
	// rather than the absence of one.
	assertTokenUsage(t, got[1].Usage, 3, 0)
}

func TestOpenAIToolCallDeltaAcceptsRepeatedIDAndName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","function":{"name":"get_weather","arguments":"\"Paris\"}"}}]}}]}`+"\n\n")
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
	if call.ID != "call_0" || call.Name != "get_weather" || call.Arguments["city"] != "Paris" {
		t.Fatalf("tool call = %+v", call)
	}
	if got[1].Type != "completed" || got[1].FinishReason != "tool_calls" {
		t.Fatalf("completion = %+v", got[1])
	}
}

// A zero-argument tool (every mcp-system tool is one) may stream its arguments
// as a JSON null from servers that send the field regardless. That means the
// same as omitting it, which this parser already accepts, so it must not fail
// the run before the tool is dispatched. Mirrors the Ollama-side case.
func TestOpenAIAcceptsNullArgumentsForZeroArgumentToolCall(t *testing.T) {
	for name, fragment := range map[string]string{
		"null arguments":   `{"name":"system.time","arguments":null}`,
		"absent arguments": `{"name":"system.time"}`,
		"empty arguments":  `{"name":"system.time","arguments":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "text/event-stream")
				fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":`+fragment+`}]}}]}`+"\n\n")
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
			for _, event := range got {
				if event.Type == "error" {
					t.Fatalf("zero-argument tool call rejected: %s: %s", event.Code, event.Message)
				}
			}
			if len(got) == 0 || got[0].Type != "tool_call" || len(got[0].ToolCalls) != 1 {
				t.Fatalf("want one tool_call event, got %+v", got)
			}
			call := got[0].ToolCalls[0]
			if call.Name != "system.time" {
				t.Fatalf("tool name = %q, want system.time", call.Name)
			}
			// Marshalled straight into the MCP request downstream, so it must be
			// an empty object rather than nil.
			if call.Arguments == nil {
				t.Fatal("arguments must be an empty map, not nil")
			}
			if len(call.Arguments) != 0 {
				t.Fatalf("arguments = %v, want empty", call.Arguments)
			}
		})
	}
}

func TestOpenAIRejectsConflictingToolCallIdentityFragments(t *testing.T) {
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
			body := "data: " + string(openAIToolFragment(0, tt.firstID, tt.firstName, "")) + "\n\n" +
				"data: " + string(openAIToolFragment(0, tt.secondID, tt.secondName, "{}")) + "\n\n"

			got := streamOpenAIEvents(t, body)

			assertOpenAIEventTypes(t, got, "error")
			if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, tt.wantMessage) {
				t.Fatalf("events = %+v, want model_bad_chunk containing %q", got, tt.wantMessage)
			}
		})
	}
}

func TestDecodeOpenAIDeltaRejectsTrailingJSONValues(t *testing.T) {
	_, err := decodeOpenAIDelta([]byte(`{"content":"accepted"} {"content":"ignored"}`))

	if err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("decodeOpenAIDelta error = %v, want trailing-data error", err)
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

func TestOpenAIRequestAliasesMCPToolNamesWithoutChangingDefinitions(t *testing.T) {
	longName := strings.Repeat("long-name-", 8)
	definitions := []ToolDefinition{
		{Name: "system.time", Description: "system.time", Parameters: map[string]any{"type": "object", "required": []any{"zone"}}},
		{Name: "files.create", Description: "files.create", Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}}},
		{Name: "already_valid-1", Description: "already_valid-1", Parameters: map[string]any{"type": "object"}},
		{Name: longName, Description: longName, Parameters: map[string]any{"type": "object", "additionalProperties": false}},
		{Name: "name.with/collision", Description: "name.with/collision", Parameters: map[string]any{"type": "object"}},
		{Name: "name/with.collision", Description: "name/with.collision", Parameters: map[string]any{"type": "object"}},
	}

	functions := captureOpenAIToolFunctions(t, definitions, nil)
	aliases := make(map[string]string, len(functions))
	validName := regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	for _, function := range functions {
		original, _ := function["description"].(string)
		alias, _ := function["name"].(string)
		if !validName.MatchString(alias) {
			t.Fatalf("alias %q for %q is not OpenAI compliant", alias, original)
		}
		if prior, duplicate := aliases[alias]; duplicate {
			t.Fatalf("alias %q is shared by %q and %q", alias, prior, original)
		}
		aliases[alias] = original
		wantParameters := toolDefinitionByName(t, definitions, original).Parameters
		if !reflect.DeepEqual(function["parameters"], wantParameters) {
			t.Fatalf("parameters for %q = %#v, want %#v", original, function["parameters"], wantParameters)
		}
	}
	if aliases["already_valid-1"] != "already_valid-1" {
		t.Fatalf("valid unique name was changed: aliases = %#v", aliases)
	}
	for _, original := range []string{"system.time", "files.create", longName} {
		for alias, mapped := range aliases {
			if mapped == original && alias == original {
				t.Fatalf("invalid original %q was not aliased", original)
			}
		}
	}
}

func TestOpenAIToolAliasesAreDeterministicAcrossDefinitionOrder(t *testing.T) {
	forward := []ToolDefinition{
		{Name: "system.time", Description: "system.time"},
		{Name: "files.create", Description: "files.create"},
		{Name: "valid_name", Description: "valid_name"},
	}
	reversed := []ToolDefinition{forward[2], forward[1], forward[0]}

	first := aliasesByDescription(captureOpenAIToolFunctions(t, forward, nil))
	second := aliasesByDescription(captureOpenAIToolFunctions(t, reversed, nil))
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("aliases depend on definition order: forward=%#v reversed=%#v", first, second)
	}
}

func TestOpenAIToolAliasAvoidsValidNameCollision(t *testing.T) {
	invalid := ToolDefinition{Name: "system.time", Description: "system.time"}
	initial := aliasesByDescription(captureOpenAIToolFunctions(t, []ToolDefinition{invalid}, nil))["system.time"]
	definitions := []ToolDefinition{
		invalid,
		{Name: initial, Description: initial},
	}

	aliases := aliasesByDescription(captureOpenAIToolFunctions(t, definitions, nil))
	if aliases[initial] != initial {
		t.Fatalf("valid name %q was changed: aliases=%#v", initial, aliases)
	}
	if aliases["system.time"] == initial {
		t.Fatalf("invalid name alias collided with valid name %q", initial)
	}
}

func TestOpenAIRequestUsesAdvertisedAliasForAssistantToolCalls(t *testing.T) {
	messages := []ChatMessage{{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID: "call_1", Name: "system.time", Arguments: map[string]any{},
		}},
	}}

	functions, request := captureOpenAIRequest(t, []ToolDefinition{{Name: "system.time", Description: "clock"}}, messages)
	alias, _ := functions[0]["name"].(string)
	message := requireSingleObject(t, request["messages"], "messages")
	call := requireSingleObject(t, message["tool_calls"], "tool_calls")
	function, _ := call["function"].(map[string]any)
	if function["name"] != alias || alias == "system.time" {
		t.Fatalf("assistant function name = %#v, advertised alias = %q", function["name"], alias)
	}
}

func TestOpenAIStreamsAdvertisedAliasAsOriginalToolName(t *testing.T) {
	requestAlias := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request openAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		alias := request.Tools[0].Function.Name
		requestAlias <- alias
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":%q,\"arguments\":\"{}\"}}]}}]}\n\n", alias)
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Tools: []ToolDefinition{{Name: "files.create"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if alias := <-requestAlias; alias == "files.create" {
		t.Fatalf("advertised name = %q, want alias", alias)
	}
	if len(got) < 1 || len(got[0].ToolCalls) != 1 || got[0].ToolCalls[0].Name != "files.create" {
		t.Fatalf("events = %+v, want provider-neutral files.create tool call", got)
	}
}

func TestOpenAILeavesUnadvertisedStreamedToolNameUnchanged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"not_advertised","arguments":"{}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Tools: []ToolDefinition{{Name: "system.time"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) < 1 || got[0].ToolCalls[0].Name != "not_advertised" {
		t.Fatalf("events = %+v, want unknown model name unchanged", got)
	}
}

func TestOpenAIRejectsDuplicateOriginalToolDefinitions(t *testing.T) {
	provider := NewOpenAICompatible("http://example.test", "", nil)
	_, err := provider.StreamChat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Tools: []ToolDefinition{{Name: "system.time"}, {Name: "system.time"}},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate") || !strings.Contains(err.Error(), "system.time") {
		t.Fatalf("StreamChat error = %v, want duplicate original definition error", err)
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

	body := <-requestBody
	const wantJSON = `{"model":"gpt-4o-mini","messages":[{"role":"assistant","content":null,"name":"planner","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]}],"stream":true,"max_tokens":2048,"stream_options":{"include_usage":true}}`
	if string(body) != wantJSON {
		t.Fatalf("request JSON = %s, want %s", body, wantJSON)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	message := requireSingleObject(t, got["messages"], "messages")
	want := map[string]any{
		"role":    "assistant",
		"content": nil,
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

// The fragment-level guard: `arguments` is a JSON *string* carrying a fragment
// of the eventual object. Null now means "no fragment", but anything else
// non-string is still a protocol error and must not be coerced to empty — that
// would dispatch the call with no arguments instead of failing. This is the only
// coverage of that guard now that the null case is legal.
func TestOpenAIRejectsNonStringToolArgumentFragments(t *testing.T) {
	for name, arguments := range map[string]string{
		"object":  `{"city":"Paris"}`,
		"array":   `[]`,
		"number":  `3`,
		"boolean": `true`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","function":{"name":"bad","arguments":` + arguments + `}}]}}]}` + "\n\n" +
				`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"

			got := streamOpenAIEvents(t, body)
			assertOpenAIEventTypes(t, got, "error")
			// Assert the message, not just the code: the finalize-level guard
			// reports "must be a JSON object" for a different input class, and
			// the two must not be confusable.
			if got[0].Code != "model_bad_chunk" || !strings.Contains(got[0].Message, "arguments must be a string") {
				t.Fatalf("events = %+v, want fragment-level model_bad_chunk", got)
			}
		})
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

	t.Run("aggregate ID and name bytes", func(t *testing.T) {
		state := newOpenAIStreamState()
		boundaryID := strings.Repeat("i", maxOpenAIToolCallAggregateIdentifierBytes-1)
		if _, _, err := parseOpenAIData(openAIToolFragment(0, boundaryID, "n", ""), state); err != nil {
			t.Fatalf("fragment reaching identifier boundary: %v", err)
		}
		if _, _, err := parseOpenAIData(openAIToolFragment(1, "i", "", ""), state); err == nil ||
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

func TestOpenAIClassifiesHTTPErrorStatus(t *testing.T) {
	tests := []struct {
		status int
		code   string
	}{
		{status: http.StatusRequestTimeout, code: "model_unavailable"},
		{status: http.StatusTooManyRequests, code: "model_unavailable"},
		{status: http.StatusInternalServerError, code: "model_unavailable"},
		{status: http.StatusUnauthorized, code: "model_auth_failed"},
		{status: http.StatusForbidden, code: "model_auth_failed"},
		{status: http.StatusUnprocessableEntity, code: "model_request_failed"},
		{status: 600, code: "model_request_failed"},
	}
	for _, test := range tests {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "provider error", test.status)
			}))
			t.Cleanup(server.Close)
			provider := NewOpenAICompatible(server.URL, "", server.Client())
			events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
			if err != nil {
				t.Fatal(err)
			}
			got := collectEvents(events)
			assertOpenAIEventTypes(t, got, "error")
			if got[0].Code != test.code || !strings.Contains(got[0].Message, fmt.Sprint(test.status)) {
				t.Fatalf("event = %+v, want code %q with status %d", got[0], test.code, test.status)
			}
		})
	}
}

func TestOpenAIClassifiesHTTPQuota429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"Add credits","type":"insufficient_quota","code":"insufficient_quota"}}`)
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	assertOpenAIEventTypes(t, got, "error")
	if got[0].Code != "model_quota_exceeded" ||
		got[0].Message != "OpenAI-compatible provider returned 429" {
		t.Fatalf("event = %+v, want nonretryable quota status error", got[0])
	}
}

func TestOpenAIKeepsOrdinaryHTTPRateLimit429Unavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"Slow down","type":"rate_limit_error","code":"rate_limit_exceeded"}}`)
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	assertOpenAIEventTypes(t, got, "error")
	if got[0].Code != "model_unavailable" ||
		got[0].Message != "OpenAI-compatible provider returned 429" {
		t.Fatalf("event = %+v, want retryable rate-limit status error", got[0])
	}
}

func TestOpenAIMalformedOrEmptyHTTP429BodyRemainsUnavailable(t *testing.T) {
	for _, body := range []string{"", `{"error":`} {
		t.Run(fmt.Sprintf("body %q", body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprint(w, body)
			}))
			t.Cleanup(server.Close)

			provider := NewOpenAICompatible(server.URL, "", server.Client())
			events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
			if err != nil {
				t.Fatal(err)
			}
			got := collectEvents(events)
			assertOpenAIEventTypes(t, got, "error")
			if got[0].Code != "model_unavailable" ||
				got[0].Message != "OpenAI-compatible provider returned 429" {
				t.Fatalf("event = %+v, want unavailable status error", got[0])
			}
		})
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

func captureOpenAIToolFunctions(t *testing.T, definitions []ToolDefinition, messages []ChatMessage) []map[string]any {
	t.Helper()
	functions, _ := captureOpenAIRequest(t, definitions, messages)
	return functions
}

func captureOpenAIRequest(t *testing.T, definitions []ToolDefinition, messages []ChatMessage) ([]map[string]any, map[string]any) {
	return captureOpenAIRequestForModel(t, "gpt-4o-mini", definitions, messages)
}

func captureOpenAIRequestForModel(
	t *testing.T,
	model string,
	definitions []ToolDefinition,
	messages []ChatMessage,
) ([]map[string]any, map[string]any) {
	t.Helper()
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBody <- body
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{
		Model: model, Tools: definitions, Messages: messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(events)

	var request map[string]any
	if err := json.Unmarshal(<-requestBody, &request); err != nil {
		t.Fatal(err)
	}
	rawTools, _ := request["tools"].([]any)
	functions := make([]map[string]any, 0, len(rawTools))
	for _, rawTool := range rawTools {
		tool, _ := rawTool.(map[string]any)
		function, _ := tool["function"].(map[string]any)
		functions = append(functions, function)
	}
	return functions, request
}

func aliasesByDescription(functions []map[string]any) map[string]string {
	aliases := make(map[string]string, len(functions))
	for _, function := range functions {
		original, _ := function["description"].(string)
		alias, _ := function["name"].(string)
		aliases[original] = alias
	}
	return aliases
}

func toolDefinitionByName(t *testing.T, definitions []ToolDefinition, name string) ToolDefinition {
	t.Helper()
	for _, definition := range definitions {
		if definition.Name == name {
			return definition
		}
	}
	t.Fatalf("missing tool definition %q", name)
	return ToolDefinition{}
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
