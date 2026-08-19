package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	maxProviderRequestBytes    = 16 * 1024 * 1024
	DefaultContextWindowTokens = 8192
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

type contextWindowProvider interface {
	ContextWindowTokens() int
}

type requestTokenEstimator interface {
	EstimateRequestTokens(ChatRequest) (int, error)
}

func ProviderContextWindowTokens(provider Provider) int {
	if configured, ok := provider.(contextWindowProvider); ok && configured.ContextWindowTokens() > 0 {
		return configured.ContextWindowTokens()
	}
	return DefaultContextWindowTokens
}

// EstimateRequestTokens applies the runtime's conservative admission rule: one
// serialized UTF-8 request byte counts as one estimated token. Built-in
// providers estimate their exact wire representation; provider test doubles and
// future implementations fall back to the provider-neutral request shape.
func EstimateRequestTokens(provider Provider, req ChatRequest) (int, error) {
	if estimator, ok := provider.(requestTokenEstimator); ok {
		return estimator.EstimateRequestTokens(req)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}
	return len(body), nil
}

type providerRequestSizeError struct {
	provider     string
	encodedBytes int
}

func (e providerRequestSizeError) Error() string {
	return fmt.Sprintf(
		"%s request exceeds %d-byte limit: encoded size is %d bytes",
		e.provider,
		maxProviderRequestBytes,
		e.encodedBytes,
	)
}

func (providerRequestSizeError) Retryable() bool { return false }

func marshalProviderRequest(
	provider string,
	marshal func() ([]byte, error),
) ([]byte, error) {
	body, err := marshal()
	if err != nil {
		return nil, err
	}
	if len(body) > maxProviderRequestBytes {
		return nil, providerRequestSizeError{provider: provider, encodedBytes: len(body)}
	}
	return body, nil
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
