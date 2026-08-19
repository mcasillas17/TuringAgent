package agent

import (
	"context"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

type budgetCapturingProvider struct {
	window    int
	responses [][]llm.StreamEvent
	requests  []llm.ChatRequest
	estimates []int
}

func (p *budgetCapturingProvider) ID() string { return "budget-capturing" }

func (p *budgetCapturingProvider) ContextWindowTokens() int { return p.window }

func (p *budgetCapturingProvider) StreamChat(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.requests = append(p.requests, req)
	estimate, err := llm.EstimateRequestTokens(p, req)
	if err != nil {
		return nil, err
	}
	p.estimates = append(p.estimates, estimate)
	index := len(p.requests) - 1
	events := []llm.StreamEvent{{Type: "completed", FinishReason: "stop"}}
	if index < len(p.responses) {
		events = p.responses[index]
	}
	out := make(chan llm.StreamEvent, len(events))
	for _, event := range events {
		out <- event
	}
	close(out)
	return out, nil
}

func TestExecuteLongSessionStaysWithinBudgetAndPreservesRecall(t *testing.T) {
	provider := &budgetCapturingProvider{window: 900}
	recaller := &fakeRecaller{
		block: llm.ChatMessage{Role: "system", Content: "recalled material that must survive before old history"},
		ok:    true,
	}
	var history []llm.ChatMessage
	for index := 0; index < 8; index++ {
		history = append(history,
			llm.ChatMessage{Role: "user", Content: strings.Repeat("old question ", 20)},
			llm.ChatMessage{Role: "assistant", Content: strings.Repeat("old answer ", 20)},
		)
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{messages: history},
		&GeneralAssistantTools{Recall: recaller},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	if provider.estimates[0] > provider.window {
		t.Fatalf("request estimate = %d, configured window = %d", provider.estimates[0], provider.window)
	}
	if !containsMessageContent(provider.requests[0].Messages, "recalled material") {
		t.Fatalf("recall did not reach provider: %#v", provider.requests[0].Messages)
	}
	if notes := runStepNotes(updates); !containsString(notes, recallNotice) {
		t.Fatalf("run notes = %q, want recall attribution", notes)
	}
}

func TestExecuteNoticesWhenRecallCannotFitContextWindow(t *testing.T) {
	job := testJob()
	provider := &budgetCapturingProvider{}
	live := []llm.ChatMessage{{Role: "user", Content: job.GetUserText()}}
	provider.window = estimateRequest(t, provider, job.GetModel(), live, nil)
	recaller := &fakeRecaller{
		block: llm.ChatMessage{Role: "system", Content: strings.Repeat("recalled material ", 30)},
		ok:    true,
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{Recall: recaller},
	)

	updates := collectUpdates(t, assistant, job)

	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	if containsMessageContent(provider.requests[0].Messages, "recalled material") {
		t.Fatalf("recall reached constrained provider: %#v", provider.requests[0].Messages)
	}
	notes := runStepNotes(updates)
	if containsString(notes, recallNotice) {
		t.Fatalf("run notes = %q, claimed omitted recall was used", notes)
	}
	if !containsStringFragment(notes, "recalled material") {
		t.Fatalf("run notes = %q, want durable recall-omission notice", notes)
	}
}

func TestExecuteRebudgetsAfterToolResultWithoutSplittingProtocol(t *testing.T) {
	provider := &budgetCapturingProvider{
		window: 1200,
		responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_1", Name: "system.read", Arguments: map[string]any{},
			}}}},
			{{Type: "delta", Text: "done"}, {Type: "completed", FinishReason: "stop"}},
		},
	}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{
			"name":        "system.read",
			"inputSchema": map[string]any{"type": "object"},
		}},
		result: map[string]any{"content": strings.Repeat("result ", 70)},
	}
	history := []llm.ChatMessage{
		{Role: "user", Content: strings.Repeat("old question ", 25)},
		{Role: "assistant", Content: strings.Repeat("old answer ", 25)},
		{Role: "user", Content: "recent question"},
		{Role: "assistant", Content: "recent answer"},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{messages: history},
		&GeneralAssistantTools{
			SystemMCP: client,
			Runner:    &tools.Runner{PostBeacon: allowToolCall},
		},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(provider.requests))
	}
	for index, estimate := range provider.estimates {
		if estimate > provider.window {
			t.Fatalf("request %d estimate = %d, window = %d", index, estimate, provider.window)
		}
	}
	second := provider.requests[1].Messages
	if len(second) < 3 {
		t.Fatalf("second request messages = %#v, want user/call/result protocol", second)
	}
	call := second[len(second)-2]
	result := second[len(second)-1]
	if call.Role != "assistant" || len(call.ToolCalls) != 1 || call.ToolCalls[0].ID != "call_1" {
		t.Fatalf("assistant tool call was split or changed: %#v", call)
	}
	if result.Role != "tool" || result.ToolCallID != "call_1" {
		t.Fatalf("tool result was split or changed: %#v", result)
	}
	if !containsStringFragment(runStepNotes(updates), "older conversation") {
		t.Fatalf("run notes = %q, want history omission notice", runStepNotes(updates))
	}
}

func TestExecuteFailsBeforeDispatchWhenLiveToolProtocolCannotFit(t *testing.T) {
	provider := &budgetCapturingProvider{
		window: 500,
		responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_1", Name: "system.read", Arguments: map[string]any{},
			}}}},
		},
	}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.read"}},
		result:      map[string]any{"content": strings.Repeat("oversized result ", 100)},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{
			SystemMCP: client,
			Runner:    &tools.Runner{PostBeacon: allowToolCall},
		},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want no second dispatch", len(provider.requests))
	}
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "context_budget_exceeded" {
		t.Fatalf("failure = %#v, want context_budget_exceeded", failure)
	}
	if failure.GetRetryable() {
		t.Fatal("context budget failure was retryable")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsStringFragment(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
