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

// Token capture, per provider, including the case that matters most: a
// provider that reports nothing must leave the counts UNKNOWN rather than
// zero. Everything downstream — storage, aggregation, the client — depends on
// those being distinguishable, and a zero is what an estimate would look like.

func TestOllamaReportsTokenUsageFromTerminalChunk(t *testing.T) {
	got := streamOllamaEvents(t,
		`{"done":false,"message":{"role":"assistant","content":"hi"}}`+"\n"+
			`{"done":true,"done_reason":"stop","prompt_eval_count":41,"eval_count":7}`+"\n")

	assertEventTypes(t, got, "delta", "completed")
	assertTokenUsage(t, got[1].Usage, 41, 7)
}

func TestOllamaLeavesTokenUsageUnreportedWhenAbsent(t *testing.T) {
	got := streamOllamaEvents(t, `{"done":true,"done_reason":"stop"}`+"\n")

	assertEventTypes(t, got, "completed")
	if got[0].Usage != nil {
		t.Fatalf("usage = %+v, want nil for a provider that reported none", got[0].Usage)
	}
}

// A cached prompt makes Ollama omit prompt_eval_count while still reporting
// eval_count. Half a measurement is still a measurement, and the missing half
// stays missing rather than becoming a zero.
func TestOllamaReportsPartialTokenUsage(t *testing.T) {
	got := streamOllamaEvents(t, `{"done":true,"done_reason":"stop","eval_count":12}`+"\n")

	assertEventTypes(t, got, "completed")
	if got[0].Usage == nil || got[0].Usage.InputTokens != nil {
		t.Fatalf("usage = %+v, want output-only usage", got[0].Usage)
	}
	if got[0].Usage.OutputTokens == nil || *got[0].Usage.OutputTokens != 12 {
		t.Fatalf("output tokens = %+v, want 12", got[0].Usage.OutputTokens)
	}
}

// A count that cannot be believed is dropped rather than recorded, and never
// costs the user the answer: the completion still arrives either way.
func TestOllamaIgnoresUnusableTokenCountsWithoutFailingTheRun(t *testing.T) {
	for _, tt := range []struct{ name, chunk string }{
		{name: "negative", chunk: `{"done":true,"done_reason":"stop","prompt_eval_count":-4,"eval_count":-1}`},
		{name: "string", chunk: `{"done":true,"done_reason":"stop","prompt_eval_count":"many","eval_count":"lots"}`},
		{name: "fractional", chunk: `{"done":true,"done_reason":"stop","prompt_eval_count":1.5,"eval_count":2.5}`},
		{name: "null", chunk: `{"done":true,"done_reason":"stop","prompt_eval_count":null,"eval_count":null}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := streamOllamaEvents(t, tt.chunk+"\n")
			assertEventTypes(t, got, "completed")
			if got[0].Usage != nil {
				t.Fatalf("usage = %+v, want nil for an unusable count", got[0].Usage)
			}
		})
	}
}

// The OpenAI usage chunk arrives AFTER finish_reason. Before this, the stream
// closed on finish_reason and the chunk was never read, which is why nothing
// here could report tokens for an external agent.
func TestOpenAIReportsTokenUsageFromTheChunkAfterFinishReason(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[{"index":0,"delta":{"content":"Hi"}}]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
			"data: "+`{"choices":[],"usage":{"prompt_tokens":128,"completion_tokens":19}}`+"\n\n"+
			"data: [DONE]\n\n")

	assertEventTypes(t, got, "delta", "completed")
	if got[1].FinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", got[1].FinishReason)
	}
	assertTokenUsage(t, got[1].Usage, 128, 19)
}

// Some providers drop `choices` entirely on the usage chunk rather than
// sending an empty array. That is a usage report, not a malformed chunk.
func TestOpenAIAcceptsUsageOnlyChunkWithoutChoices(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[{"index":0,"delta":{"content":"Hi"}}]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
			"data: "+`{"usage":{"prompt_tokens":5,"completion_tokens":6}}`+"\n\n"+
			"data: [DONE]\n\n")

	assertEventTypes(t, got, "delta", "completed")
	assertTokenUsage(t, got[1].Usage, 5, 6)
}

// A stream that ends after finish_reason without [DONE] still completed, and
// the usage it reported still counts.
func TestOpenAIReportsTokenUsageWhenStreamEndsWithoutDone(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
			"data: "+`{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3}}`+"\n\n")

	assertEventTypes(t, got, "completed")
	assertTokenUsage(t, got[0].Usage, 2, 3)
}

func TestOpenAILeavesTokenUsageUnreportedWhenProviderSendsNone(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[{"index":0,"delta":{"content":"Hi"}}]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
			"data: [DONE]\n\n")

	assertEventTypes(t, got, "delta", "completed")
	if got[1].Usage != nil {
		t.Fatalf("usage = %+v, want nil for a provider that reported none", got[1].Usage)
	}
}

// Trailing bytes past the terminal event must not turn a finished answer into
// a failure. Telemetry is the cheapest thing in the system and gets sacrificed
// first.
func TestOpenAIIgnoresGarbageAfterTheTerminalEvent(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[{"index":0,"delta":{"content":"Hi"}}]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
			"data: not json at all\n\n"+
			"data: [DONE]\n\n")

	assertEventTypes(t, got, "delta", "completed")
	if got[1].Usage != nil {
		t.Fatalf("usage = %+v, want nil", got[1].Usage)
	}
}

func TestOpenAIIgnoresUnusableUsageValues(t *testing.T) {
	for _, tt := range []struct{ name, usage string }{
		{name: "negative", usage: `{"prompt_tokens":-1,"completion_tokens":-2}`},
		{name: "strings", usage: `{"prompt_tokens":"5","completion_tokens":"6"}`},
		{name: "null", usage: `{"prompt_tokens":null,"completion_tokens":null}`},
		{name: "empty object", usage: `{}`},
		{name: "not an object", usage: `7`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := streamOpenAIEvents(t,
				"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
					"data: "+`{"choices":[],"usage":`+tt.usage+`}`+"\n\n"+
					"data: [DONE]\n\n")

			assertEventTypes(t, got, "completed")
			if got[0].Usage != nil {
				t.Fatalf("usage = %+v, want nil for an unusable report", got[0].Usage)
			}
		})
	}
}

// A stream carrying tool calls still reports its tokens: a run that spends its
// budget calling tools is exactly the run somebody wants the number for.
func TestOpenAIReportsTokenUsageAlongsideToolCalls(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+string(openAIToolFragment(0, "call_1", "system.time", "{}"))+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n"+
			"data: "+`{"choices":[],"usage":{"prompt_tokens":90,"completion_tokens":11}}`+"\n\n"+
			"data: [DONE]\n\n")

	assertEventTypes(t, got, "tool_call", "completed")
	assertTokenUsage(t, got[1].Usage, 90, 11)
}

// The regression this change could most easily have caused: waiting for a
// usage chunk that never comes must not cost the user a finished answer. A
// provider that sends finish_reason, no [DONE], and then holds the connection
// open still completes — without a token count, which is the cheap half.
func TestOpenAICompletesWhenTheTailNeverArrives(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"Hi"}}]}`+"\n\n")
		fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		w.(http.Flusher).Flush()
		// Neither [DONE] nor EOF, which is what a proxy holding the connection
		// open looks like.
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	provider.usageDrainTimeout = 50 * time.Millisecond
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}

	got := collectEvents(events)
	assertEventTypes(t, got, "delta", "completed")
	if got[1].FinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", got[1].FinishReason)
	}
	if got[1].Usage != nil {
		t.Fatalf("usage = %+v, want nil when the tail never arrived", got[1].Usage)
	}
}

func TestOpenAICompletesBeforeNearModelDeadlineWhenUsageTailNeverArrives(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"Hi"}}]}`+"\n\n")
		fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	provider.usageDrainTimeout = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	events, err := provider.StreamChat(ctx, ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}

	got := collectEvents(events)
	assertEventTypes(t, got, "delta", "completed")
	if got[1].FinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", got[1].FinishReason)
	}
}

// A server that rejects the unknown stream_options field must still answer.
// Those installations work today, and losing every run on them to collect a
// number nobody asked for is the wrong trade.
func TestOpenAIRetriesWithoutStreamOptionsWhenTheRequestIsRejected(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Errorf("decode request: %v", err)
		}
		bodies = append(bodies, decoded)
		if _, asked := decoded["stream_options"]; asked {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"unrecognized request argument: stream_options"}}`)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"Hi"}}]}`+"\n\n")
		fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}

	got := collectEvents(events)
	assertEventTypes(t, got, "delta", "completed")
	if got[1].Usage != nil {
		t.Fatalf("usage = %+v, want nil after dropping the request for it", got[1].Usage)
	}
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want the original and one retry", len(bodies))
	}
	if _, asked := bodies[1]["stream_options"]; asked {
		t.Fatalf("retry body = %v, still asks for usage", bodies[1])
	}
}

// The retry is for a rejected body, not for a rejected caller. Retrying an
// auth failure or a rate limit spends a second request to fail the same way.
func TestOpenAIDoesNotRetryStatusesThatAreNotAboutTheBody(t *testing.T) {
	for _, status := range []int{
		http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests,
		http.StatusNotFound, http.StatusInternalServerError,
	} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(status)
			}))
			t.Cleanup(server.Close)

			provider := NewOpenAICompatible(server.URL, "", server.Client())
			events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "gpt-4o-mini"})
			if err != nil {
				t.Fatal(err)
			}
			collectEvents(events)

			if requests != 1 {
				t.Fatalf("requests = %d, want exactly one for status %d", requests, status)
			}
		})
	}
}

// A trailing chunk too large to buffer is still a trailing chunk. The answer
// already finished, so it costs the token count and nothing else.
func TestOpenAICompletesWhenTheTrailingChunkIsOversized(t *testing.T) {
	oversized := strings.Repeat("x", maxOpenAIEventDataBytes+1)
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[{"index":0,"delta":{"content":"Hi"}}]}`+"\n\n"+
			"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
			"data: "+oversized+"\n\n"+
			"data: [DONE]\n\n")

	assertEventTypes(t, got, "delta", "completed")
	if got[1].Usage != nil {
		t.Fatalf("usage = %+v, want nil", got[1].Usage)
	}
}

// A provider that re-sends a usage object carrying only one of the two counts
// must not erase the other.
func TestOpenAIMergesSuccessiveUsageReports(t *testing.T) {
	got := streamOpenAIEvents(t,
		"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
			"data: "+`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50}}`+"\n\n"+
			"data: "+`{"choices":[],"usage":{"prompt_tokens":120}}`+"\n\n"+
			"data: [DONE]\n\n")

	assertEventTypes(t, got, "completed")
	assertTokenUsage(t, got[0].Usage, 120, 50)
}

// Asking is the only way to be told. Without stream_options the response
// carries no usage at all, and the alternative to asking is estimating.
func TestOpenAIRequestAsksForStreamedUsage(t *testing.T) {
	body := captureOpenAIRequestBody(t, ChatRequest{Model: "gpt-4o-mini"})
	options, ok := body["stream_options"].(map[string]any)
	if !ok {
		t.Fatalf("request = %v, want stream_options", body)
	}
	if include, ok := options["include_usage"].(bool); !ok || !include {
		t.Fatalf("stream_options = %v, want include_usage true", options)
	}
}

func assertEventTypes(t *testing.T, events []StreamEvent, want ...string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("events = %+v, want types %v", events, want)
	}
	for index, event := range events {
		if event.Type != want[index] {
			t.Fatalf("event %d type = %q, want %q; events = %+v", index, event.Type, want[index], events)
		}
	}
}

func assertTokenUsage(t *testing.T, usage *TokenUsage, wantInput int64, wantOutput int64) {
	t.Helper()
	if usage == nil {
		t.Fatalf("usage = nil, want input %d output %d", wantInput, wantOutput)
	}
	if usage.InputTokens == nil || *usage.InputTokens != wantInput {
		t.Fatalf("input tokens = %v, want %d", usage.InputTokens, wantInput)
	}
	if usage.OutputTokens == nil || *usage.OutputTokens != wantOutput {
		t.Fatalf("output tokens = %v, want %d", usage.OutputTokens, wantOutput)
	}
}

func captureOpenAIRequestBody(t *testing.T, request ChatRequest) map[string]any {
	t.Helper()
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		requestBody <- body
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	provider := NewOpenAICompatible(server.URL, "", server.Client())
	events, err := provider.StreamChat(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(events)

	var decoded map[string]any
	if err := json.Unmarshal(<-requestBody, &decoded); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return decoded
}
