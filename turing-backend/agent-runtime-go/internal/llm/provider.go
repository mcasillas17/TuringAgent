package llm

import (
	"context"
	"net/http"
)

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolDefinition describes a provider-neutral callable tool and its JSON Schema parameters.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall represents a provider-neutral tool request with parsed JSON object arguments.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
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

func providerHTTPErrorCode(status int) string {
	switch {
	case status == http.StatusRequestTimeout, status == http.StatusTooManyRequests, status >= 500 && status < 600:
		return "model_unavailable"
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return "model_auth_failed"
	default:
		return "model_request_failed"
	}
}
