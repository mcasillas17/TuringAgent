package llm

import (
	"encoding/json"
	"reflect"
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

func TestChatMessageJSONIncludesToolThreadingFields(t *testing.T) {
	message := ChatMessage{
		Role:       "tool",
		Content:    `{"temperature":72}`,
		Name:       "weather",
		ToolCallID: "call_1",
		ToolCalls: []ToolCall{{
			ID:        "call_2",
			Name:      "forecast",
			Arguments: `{"days":3}`,
		}},
	}

	got, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}

	const want = `{"role":"tool","content":"{\"temperature\":72}","name":"weather","tool_call_id":"call_1","tool_calls":[{"id":"call_2","name":"forecast","arguments":"{\"days\":3}"}]}`
	if string(got) != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
}

func TestToolDefinitionJSONContract(t *testing.T) {
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

	request := ChatRequest{Tools: []ToolDefinition{definition}}
	if !reflect.DeepEqual(request.Tools, []ToolDefinition{definition}) {
		t.Fatalf("tools = %#v, want %#v", request.Tools, []ToolDefinition{definition})
	}
}

func TestToolCallJSONContractAndStreamEventSupport(t *testing.T) {
	call := ToolCall{
		ID:        "call_1",
		Name:      "weather",
		Arguments: `{"city":"Seattle"}`,
	}

	got, err := json.Marshal(call)
	if err != nil {
		t.Fatal(err)
	}

	const want = `{"id":"call_1","name":"weather","arguments":"{\"city\":\"Seattle\"}"}`
	if string(got) != want {
		t.Fatalf("json = %s, want %s", got, want)
	}

	event := StreamEvent{Type: "tool_call", ToolCalls: []ToolCall{call}}
	if event.Type != "tool_call" || !reflect.DeepEqual(event.ToolCalls, []ToolCall{call}) {
		t.Fatalf("event = %#v", event)
	}
}
