package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
)

type budgetTestProvider struct {
	window int
}

func (p *budgetTestProvider) ID() string { return "budget-test" }

func (p *budgetTestProvider) ContextWindowTokens() int { return p.window }

func (p *budgetTestProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	panic("StreamChat must not be called by the context builder")
}

func TestBuildBudgetedContextKeepsMandatoryMessagesAndStableOrder(t *testing.T) {
	provider := &budgetTestProvider{window: 64 * 1024}
	skills := llm.ChatMessage{Role: "system", Content: "attached skill"}
	recall := llm.ChatMessage{Role: "system", Content: "recalled fact"}
	input := contextInput{
		skills: &skills,
		history: []llm.ChatMessage{
			{Role: "user", Content: "old question"},
			{Role: "assistant", Content: "old answer"},
		},
		recall: &recall,
		live:   []llm.ChatMessage{{Role: "user", Content: "current question"}},
	}

	got, err := buildBudgetedContext(provider, "model", input, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []llm.ChatMessage{
		recall,
		skills,
		{Role: "user", Content: "old question"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "current question"},
	}
	if !reflect.DeepEqual(got.Request.Messages, want) {
		t.Fatalf("messages = %#v, want %#v", got.Request.Messages, want)
	}
	if got.Estimate > provider.window {
		t.Fatalf("estimate = %d, context window = %d", got.Estimate, provider.window)
	}
	if !got.RecallUsed || got.Omissions != (contextOmissions{}) {
		t.Fatalf("budget result = %#v, want recall with no omissions", got)
	}
}

func TestBuildBudgetedContextOmitsOldestCompleteHistoryFirst(t *testing.T) {
	provider := &budgetTestProvider{}
	recall := llm.ChatMessage{Role: "system", Content: "recalled"}
	skills := llm.ChatMessage{Role: "system", Content: "skill"}
	newest := []llm.ChatMessage{
		{Role: "user", Content: "new question"},
		{Role: "assistant", Content: "new answer"},
	}
	live := []llm.ChatMessage{{Role: "user", Content: "current"}}
	provider.window = estimateRequest(t, provider, "model", appendMessages(
		[]llm.ChatMessage{recall, skills},
		newest,
		live,
	), nil)

	got, err := buildBudgetedContext(provider, "model", contextInput{
		skills: &skills,
		recall: &recall,
		history: []llm.ChatMessage{
			{Role: "user", Content: strings.Repeat("old question", 20)},
			{Role: "assistant", Content: strings.Repeat("old answer", 20)},
			newest[0],
			newest[1],
		},
		live: live,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if containsMessageContent(got.Request.Messages, "old question") ||
		containsMessageContent(got.Request.Messages, "old answer") {
		t.Fatalf("old history survived: %#v", got.Request.Messages)
	}
	if !containsMessageContent(got.Request.Messages, "new question") ||
		!containsMessageContent(got.Request.Messages, "new answer") {
		t.Fatalf("newest complete turn was not preserved: %#v", got.Request.Messages)
	}
	if got.Omissions.HistoryMessages != 2 {
		t.Fatalf("HistoryMessages = %d, want 2", got.Omissions.HistoryMessages)
	}
}

func TestBuildBudgetedContextNeverPartiallyIncludesRecall(t *testing.T) {
	provider := &budgetTestProvider{}
	recall := llm.ChatMessage{Role: "system", Content: strings.Repeat("recalled fact ", 30)}
	live := []llm.ChatMessage{{Role: "user", Content: "current"}}
	provider.window = estimateRequest(t, provider, "model", live, nil)

	got, err := buildBudgetedContext(provider, "model", contextInput{
		recall: &recall,
		live:   live,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if containsMessageContent(got.Request.Messages, "recalled fact") {
		t.Fatalf("partial recall reached the request: %#v", got.Request.Messages)
	}
	if got.RecallUsed || !got.Omissions.RecallOmitted {
		t.Fatalf("budget result = %#v, want wholly omitted recall", got)
	}
}

func TestBuildBudgetedContextOmitsWholeToolDefinitionsInStableOrder(t *testing.T) {
	provider := &budgetTestProvider{}
	live := []llm.ChatMessage{{Role: "user", Content: "use a tool"}}
	tools := []llm.ToolDefinition{
		{
			Name:        "first",
			Description: "first tool",
			Parameters:  map[string]any{"type": "object"},
		},
		{
			Name:        "second",
			Description: strings.Repeat("large schema ", 30),
			Parameters:  map[string]any{"type": "object"},
		},
	}
	provider.window = estimateRequest(t, provider, "model", live, tools[:1])

	got, err := buildBudgetedContext(provider, "model", contextInput{live: live}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Request.Tools, tools[:1]) {
		t.Fatalf("tools = %#v, want first whole definition only", got.Request.Tools)
	}
	if got.Omissions.ToolDefinitions != 1 {
		t.Fatalf("ToolDefinitions = %d, want 1", got.Omissions.ToolDefinitions)
	}
}

func TestBuildBudgetedContextKeepsLiveToolCallAndResultTogether(t *testing.T) {
	provider := &budgetTestProvider{window: 64 * 1024}
	live := []llm.ChatMessage{
		{Role: "user", Content: "inspect"},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID: "call_1", Name: "files.read", Arguments: map[string]any{"path": "notes.txt"},
			}},
		},
		{Role: "tool", ToolCallID: "call_1", Name: "files.read", Content: `{"content":"value"}`},
	}
	tools := []llm.ToolDefinition{{
		Name:       "files.read",
		Parameters: map[string]any{"type": "object"},
	}}

	got, err := buildBudgetedContext(provider, "model", contextInput{live: live}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Request.Messages, live) {
		t.Fatalf("live protocol changed: %#v", got.Request.Messages)
	}
	if !reflect.DeepEqual(got.Request.Tools, tools) {
		t.Fatalf("referenced tool definition changed: %#v", got.Request.Tools)
	}
}

func TestBuildBudgetedContextFailsWhenLiveProtocolCannotFit(t *testing.T) {
	provider := &budgetTestProvider{window: 1}
	live := []llm.ChatMessage{
		{Role: "user", Content: "inspect"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "files.read"}}},
		{Role: "tool", ToolCallID: "call_1", Name: "files.read", Content: strings.Repeat("result", 100)},
	}

	_, err := buildBudgetedContext(provider, "model", contextInput{live: live}, nil)
	if !errors.Is(err, errContextBudgetExceeded) {
		t.Fatalf("error = %v, want errContextBudgetExceeded", err)
	}
}

func TestBuildBudgetedContextRequiresDefinitionsReferencedByLiveProtocol(t *testing.T) {
	provider := &budgetTestProvider{}
	live := []llm.ChatMessage{
		{Role: "user", Content: "inspect"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "files.read"}}},
		{Role: "tool", ToolCallID: "call_1", Name: "files.read", Content: "result"},
	}
	tools := []llm.ToolDefinition{{
		Name:        "files.read",
		Description: strings.Repeat("required schema ", 20),
		Parameters:  map[string]any{"type": "object"},
	}}
	provider.window = estimateRequest(t, provider, "model", live, nil)

	_, err := buildBudgetedContext(provider, "model", contextInput{live: live}, tools)
	if !errors.Is(err, errContextBudgetExceeded) {
		t.Fatalf("error = %v, want required schema to fail instead of being omitted", err)
	}
}

func TestBuildBudgetedContextIsDeterministic(t *testing.T) {
	provider := &budgetTestProvider{window: 512}
	recall := llm.ChatMessage{Role: "system", Content: strings.Repeat("recall ", 20)}
	input := contextInput{
		recall: &recall,
		history: []llm.ChatMessage{
			{Role: "user", Content: strings.Repeat("old ", 20)},
			{Role: "assistant", Content: strings.Repeat("answer ", 20)},
		},
		live: []llm.ChatMessage{{Role: "user", Content: "current"}},
	}
	tools := []llm.ToolDefinition{
		{Name: "first", Parameters: map[string]any{"type": "object"}},
		{Name: "second", Parameters: map[string]any{"type": "object"}},
	}

	first, firstErr := buildBudgetedContext(provider, "model", input, tools)
	second, secondErr := buildBudgetedContext(provider, "model", input, tools)
	if !reflect.DeepEqual(firstErr, secondErr) || !reflect.DeepEqual(first, second) {
		t.Fatalf("identical inputs differed:\nfirst=%#v / %v\nsecond=%#v / %v", first, firstErr, second, secondErr)
	}
}

func TestContextOmissionsNoticeNamesEveryOmittedCategory(t *testing.T) {
	got := (contextOmissions{
		HistoryMessages: 4,
		RecallOmitted:   true,
		ToolDefinitions: 2,
	}).Notice()
	want := "Context window limit: omitted 4 older conversation messages, recalled material, and 2 tool definitions from this model request."
	if got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
}

func TestContextOmissionsNoticeHandlesSingularAndEmpty(t *testing.T) {
	if got := (contextOmissions{}).Notice(); got != "" {
		t.Fatalf("empty omissions notice = %q, want empty", got)
	}
	got := (contextOmissions{HistoryMessages: 1, ToolDefinitions: 1}).Notice()
	want := "Context window limit: omitted 1 older conversation message and 1 tool definition from this model request."
	if got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
}

func estimateRequest(t *testing.T, provider llm.Provider, model string, messages []llm.ChatMessage, tools []llm.ToolDefinition) int {
	t.Helper()
	estimate, err := llm.EstimateRequestTokens(provider, llm.ChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	return estimate
}

func appendMessages(groups ...[]llm.ChatMessage) []llm.ChatMessage {
	var messages []llm.ChatMessage
	for _, group := range groups {
		messages = append(messages, group...)
	}
	return messages
}

func containsMessageContent(messages []llm.ChatMessage, content string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, content) {
			return true
		}
	}
	return false
}
