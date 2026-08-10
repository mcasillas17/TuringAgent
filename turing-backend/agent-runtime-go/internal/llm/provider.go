package llm

import "context"

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ChatRequest struct {
	Model       string
	Messages    []ChatMessage
	Temperature float64
	MaxTokens   int
	Tools       []ToolDefinition
}

type StreamEvent struct {
	// Type is one of "delta", "tool_call", "completed", or "error".
	Type         string
	Text         string
	FinishReason string
	Code         string
	Message      string
	ToolCalls    []ToolCall
}

type Provider interface {
	ID() string
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}
