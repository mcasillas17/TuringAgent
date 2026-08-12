package llm

import (
	"encoding/json"
	"testing"
)

func TestChatMessageJSONUsesLowercaseRoleAndContentKeys(t *testing.T) {
	got, err := json.Marshal(ChatMessage{Role: "user", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}

	const want = `{"role":"user","content":"hello"}`
	if string(got) != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
}

func TestChatMessageJSONOmitsEmptyToolThreadingFields(t *testing.T) {
	tests := []struct {
		name    string
		message ChatMessage
	}{
		{name: "zero values", message: ChatMessage{Role: "assistant", Content: "done"}},
		{name: "empty tool calls", message: ChatMessage{Role: "assistant", Content: "done", ToolCalls: []ToolCall{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.message)
			if err != nil {
				t.Fatal(err)
			}

			const want = `{"role":"assistant","content":"done"}`
			if string(got) != want {
				t.Fatalf("json = %s, want %s", got, want)
			}
		})
	}
}

func TestAssistantChatMessageJSONIncludesToolCalls(t *testing.T) {
	message := ChatMessage{
		Role:    "assistant",
		Content: "",
		ToolCalls: []ToolCall{{
			ID:        "call_1",
			Name:      "weather",
			Arguments: map[string]any{"days": 3},
		}},
	}

	got, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}

	const want = `{"role":"assistant","content":"","tool_calls":[{"id":"call_1","name":"weather","arguments":{"days":3}}]}`
	if string(got) != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
}

func TestToolChatMessageJSONIncludesResultThreadingFields(t *testing.T) {
	message := ChatMessage{
		Role:       "tool",
		Content:    `{"temperature":72}`,
		Name:       "weather",
		ToolCallID: "call_1",
	}

	got, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}

	const want = `{"role":"tool","content":"{\"temperature\":72}","name":"weather","tool_call_id":"call_1"}`
	if string(got) != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
}

func TestToolDefinitionJSONIncludesDescriptionAndParameters(t *testing.T) {
	definition := ToolDefinition{
		Name:        "weather",
		Description: "Get the current weather",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{"type": "string"},
			},
			"required": []string{"city"},
		},
	}

	got, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}

	const want = `{"name":"weather","description":"Get the current weather","parameters":{"properties":{"city":{"type":"string"}},"required":["city"],"type":"object"}}`
	if string(got) != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
}

func TestToolDefinitionJSONOmitsEmptyDescription(t *testing.T) {
	got, err := json.Marshal(ToolDefinition{
		Name:       "weather",
		Parameters: map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatal(err)
	}

	const want = `{"name":"weather","parameters":{"type":"object"}}`
	if string(got) != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
}

func TestToolCallCarriesParsedArgumentsAndSerializesThemAsObject(t *testing.T) {
	var arguments map[string]any
	if err := json.Unmarshal([]byte(`{"city":"Seattle"}`), &arguments); err != nil {
		t.Fatal(err)
	}

	call := ToolCall{
		ID:        "call_1",
		Name:      "weather",
		Arguments: arguments,
	}
	event := StreamEvent{Type: "tool_call", ToolCalls: []ToolCall{call}}

	got, err := json.Marshal(event.ToolCalls)
	if err != nil {
		t.Fatal(err)
	}

	const want = `[{"id":"call_1","name":"weather","arguments":{"city":"Seattle"}}]`
	if string(got) != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
}
