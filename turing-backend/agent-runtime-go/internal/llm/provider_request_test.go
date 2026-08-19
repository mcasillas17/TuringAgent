package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const providerRequestByteBudgetForTest = maxProviderRequestBytes

type providerRequestFixture struct {
	name         string
	model        string
	responseBody string
	newProvider  func(string, *http.Client) Provider
}

func providerRequestFixtures() []providerRequestFixture {
	return []providerRequestFixture{
		{
			name:         "OpenAI-compatible",
			model:        "gpt-4o-mini",
			responseBody: "data: [DONE]\n\n",
			newProvider: func(baseURL string, client *http.Client) Provider {
				return NewOpenAICompatible(baseURL, "", client)
			},
		},
		{
			name:         "Ollama",
			model:        "llama3.2",
			responseBody: `{"done":true,"done_reason":"stop"}` + "\n",
			newProvider: func(baseURL string, client *http.Client) Provider {
				return NewOllama(baseURL, client)
			},
		},
	}
}

func TestProviderRequestByteLimitNeverSilentlyTrimsHistory(t *testing.T) {
	for _, fixture := range providerRequestFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			provider, requests := providerRejectingBudgetHarness(t, fixture)
			events, err := provider.StreamChat(context.Background(), ChatRequest{
				Model: fixture.model,
				Messages: []ChatMessage{
					{Role: "user", Content: strings.Repeat("o", 9*1024*1024)},
					{Role: "assistant", Content: strings.Repeat("a", 8*1024*1024)},
					{Role: "user", Content: "recent question"},
					{Role: "assistant", Content: "recent answer"},
					{Role: "user", Content: "current question"},
				},
			})
			if err == nil {
				collectEvents(events)
				t.Fatal("StreamChat silently trimmed oversized history")
			}
			assertProviderRequestBudgetError(t, err, fixture.name)
			if got := requests.Load(); got != 0 {
				t.Fatalf("provider HTTP requests = %d, want none", got)
			}
		})
	}
}

func TestProviderRequestEstimateMatchesExactWireBody(t *testing.T) {
	for _, fixture := range providerRequestFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			bodies := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
					return
				}
				bodies <- body
				_, _ = io.WriteString(w, fixture.responseBody)
			}))
			t.Cleanup(server.Close)
			provider := fixture.newProvider(server.URL, server.Client())
			request := ChatRequest{
				Model: fixture.model,
				Messages: []ChatMessage{
					{Role: "system", Content: "Be concise."},
					{Role: "user", Content: "Use the tool."},
				},
				Tools: []ToolDefinition{{
					Name:        "tool.with-provider-specific-encoding",
					Description: "A provider-neutral tool.",
					Parameters:  map[string]any{"type": "object"},
				}},
			}

			estimate, err := EstimateRequestTokens(provider, request)
			if err != nil {
				t.Fatalf("EstimateRequestTokens failed: %v", err)
			}
			events, err := provider.StreamChat(context.Background(), request)
			if err != nil {
				t.Fatalf("StreamChat failed: %v", err)
			}
			collectEvents(events)
			if wireBytes := len(<-bodies); estimate != wireBytes {
				t.Fatalf("estimate = %d, exact wire body = %d bytes", estimate, wireBytes)
			}
		})
	}
}

func TestProviderRequestBudgetRejectsOversizedToolSchema(t *testing.T) {
	for _, fixture := range providerRequestFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			provider, requests := providerRejectingBudgetHarness(t, fixture)
			events, err := provider.StreamChat(context.Background(), ChatRequest{
				Model:    fixture.model,
				Messages: []ChatMessage{{Role: "user", Content: "use the tool"}},
				Tools: []ToolDefinition{{
					Name:        "oversized",
					Description: strings.Repeat("s", providerRequestByteBudgetForTest),
					Parameters:  map[string]any{"type": "object"},
				}},
			})
			if err == nil {
				collectEvents(events)
				t.Fatal("StreamChat accepted an oversized serialized tool schema")
			}
			assertProviderRequestBudgetError(t, err, fixture.name)
			if got := requests.Load(); got != 0 {
				t.Fatalf("provider HTTP requests = %d, want none for oversized schema", got)
			}
		})
	}
}

func TestProviderRequestBudgetRejectsOversizedRequiredToolProtocol(t *testing.T) {
	for _, fixture := range providerRequestFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			provider, requests := providerRejectingBudgetHarness(t, fixture)
			events, err := provider.StreamChat(context.Background(), ChatRequest{
				Model: fixture.model,
				Messages: []ChatMessage{
					{Role: "user", Content: "use the tool"},
					{Role: "assistant", ToolCalls: []ToolCall{{
						ID: "call_1", Name: "large_result", Arguments: map[string]any{},
					}}},
					{
						Role:       "tool",
						Name:       "large_result",
						ToolCallID: "call_1",
						Content:    strings.Repeat("r", providerRequestByteBudgetForTest),
					},
				},
			})
			if err == nil {
				collectEvents(events)
				t.Fatal("StreamChat accepted an oversized required tool protocol pair")
			}
			assertProviderRequestBudgetError(t, err, fixture.name)
			if got := requests.Load(); got != 0 {
				t.Fatalf("provider HTTP requests = %d, want none for oversized tool payload", got)
			}
		})
	}
}

func TestProviderRequestBudgetAllowsExactSerializedBoundary(t *testing.T) {
	for _, fixture := range providerRequestFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			bodies := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
					return
				}
				bodies <- body
				_, _ = io.WriteString(w, fixture.responseBody)
			}))
			t.Cleanup(server.Close)
			provider := fixture.newProvider(server.URL, server.Client())

			requestWithContentBytes := func(contentBytes int) ChatRequest {
				return ChatRequest{
					Model: fixture.model,
					Messages: []ChatMessage{{
						Role:    "user",
						Content: strings.Repeat("x", contentBytes),
					}},
				}
			}
			bestContentBytes := 0
			bestEstimate := 0
			for low, high := 0, providerRequestByteBudgetForTest; low <= high; {
				middle := low + (high-low)/2
				estimate, err := EstimateRequestTokens(provider, requestWithContentBytes(middle))
				if err != nil {
					t.Fatal(err)
				}
				if estimate <= providerRequestByteBudgetForTest {
					bestContentBytes = middle
					bestEstimate = estimate
					low = middle + 1
				} else {
					high = middle - 1
				}
			}
			if bestEstimate != providerRequestByteBudgetForTest {
				t.Fatalf("largest accepted estimate = %d, want exact boundary %d", bestEstimate, providerRequestByteBudgetForTest)
			}

			events, err := provider.StreamChat(context.Background(), requestWithContentBytes(bestContentBytes))
			if err != nil {
				t.Fatalf("StreamChat rejected exact serialized boundary: %v", err)
			}
			collectEvents(events)
			if got := len(<-bodies); got != providerRequestByteBudgetForTest {
				t.Fatalf("serialized request bytes = %d, want exact boundary %d", got, providerRequestByteBudgetForTest)
			}
		})
	}
}

func providerRejectingBudgetHarness(t *testing.T, fixture providerRequestFixture) (Provider, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, fixture.responseBody)
	}))
	t.Cleanup(server.Close)
	return fixture.newProvider(server.URL, server.Client()), &requests
}

func assertProviderRequestBudgetError(t *testing.T, err error, providerName string) {
	t.Helper()
	want := fmt.Sprintf("%s request exceeds %d-byte limit", providerName, providerRequestByteBudgetForTest)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("StreamChat error = %q, want %q", err, want)
	}
	classified, ok := err.(interface{ Retryable() bool })
	if !ok || classified.Retryable() {
		t.Fatalf("StreamChat error = %T %v, want non-retryable request-size error", err, err)
	}
}
