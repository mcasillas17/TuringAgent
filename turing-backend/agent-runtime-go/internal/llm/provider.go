package llm

import (
	"context"
	"fmt"
	"net/http"
)

const maxProviderRequestBytes = 16 * 1024 * 1024

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

// TokenUsage carries token counts a provider REPORTED. It is never computed,
// estimated, or inferred from message lengths here or anywhere downstream.
//
// Each field is a pointer because "the provider did not say" and "the provider
// said zero" are different facts, and only one of them is a measurement. A
// tokeniser-based estimate would be a plausible-looking number that no
// provider ever produced, and somebody would eventually make a decision with
// it — so this type has no way to hold one.
type TokenUsage struct {
	InputTokens  *int64
	OutputTokens *int64
}

// Reported says whether the provider gave any number at all. A TokenUsage with
// both fields nil is indistinguishable from silence and callers should treat
// it as such.
func (u *TokenUsage) Reported() bool {
	return u != nil && (u.InputTokens != nil || u.OutputTokens != nil)
}

// TokenCount is the constructor for a count that was actually observed.
func TokenCount(value int64) *int64 {
	return &value
}

type StreamEvent struct {
	// Type is one of "delta", "tool_call", "completed", or "error".
	Type         string
	Text         string
	FinishReason string
	Code         string
	Message      string
	ToolCalls    []ToolCall
	// Usage is set only on a "completed" event, and only when the provider
	// reported counts. nil means unknown.
	Usage *TokenUsage
}

type Provider interface {
	ID() string
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
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
	messages []ChatMessage,
	marshal func([]ChatMessage) ([]byte, error),
) ([]byte, error) {
	body, err := marshal(messages)
	if err != nil {
		return nil, err
	}
	if len(body) <= maxProviderRequestBytes {
		return body, nil
	}

	preservedPrefix, dropEnds := historyDropBoundaries(messages)
	if len(dropEnds) == 0 {
		return nil, providerRequestSizeError{provider: provider, encodedBytes: len(body)}
	}

	var bounded []byte
	oversizedBytes := len(body)
	for low, high := 0, len(dropEnds)-1; low <= high; {
		middle := low + (high-low)/2
		candidate := trimHistory(messages, preservedPrefix, dropEnds[middle])
		candidateBody, err := marshal(candidate)
		if err != nil {
			return nil, err
		}
		if len(candidateBody) <= maxProviderRequestBytes {
			bounded = candidateBody
			high = middle - 1
			continue
		}
		oversizedBytes = len(candidateBody)
		low = middle + 1
	}
	if bounded != nil {
		return bounded, nil
	}
	return nil, providerRequestSizeError{provider: provider, encodedBytes: oversizedBytes}
}

func historyDropBoundaries(messages []ChatMessage) (int, []int) {
	currentUser := -1
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" {
			currentUser = index
			break
		}
	}
	if currentUser <= 0 {
		return 0, nil
	}

	preservedPrefix := 0
	for preservedPrefix < currentUser && messages[preservedPrefix].Role == "system" {
		preservedPrefix++
	}
	if preservedPrefix == currentUser {
		return 0, nil
	}

	var dropEnds []int
	for index := preservedPrefix + 1; index < currentUser; index++ {
		if messages[index].Role == "user" {
			dropEnds = append(dropEnds, index)
		}
	}
	dropEnds = append(dropEnds, currentUser)
	return preservedPrefix, dropEnds
}

func trimHistory(messages []ChatMessage, preservedPrefix int, dropEnd int) []ChatMessage {
	trimmed := make([]ChatMessage, 0, len(messages)-(dropEnd-preservedPrefix))
	trimmed = append(trimmed, messages[:preservedPrefix]...)
	trimmed = append(trimmed, messages[dropEnd:]...)
	return trimmed
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
