package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
)

const (
	maxProviderRequestBytes    = 16 * 1024 * 1024
	DefaultContextWindowTokens = 32768
	DefaultMaxOutputTokens     = 2048
	MaxContextWindowTokens     = 16 * 1024 * 1024
)

type ChatMessage struct {
	MessageID  string     `json:"-"`
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
	ContextWindowTokens() int
	MaxOutputTokens() int
	EstimateRequestTokens(ChatRequest) (int, error)
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}

type EgressProvider interface {
	Provider
	EgressEndpoint() string
}

// EstimateRequestTokens applies the runtime's conservative admission rule: one
// serialized UTF-8 request byte counts as one upper-bound token estimate.
// Providers must estimate their exact wire representation.
func EstimateRequestTokens(provider Provider, req ChatRequest) (int, error) {
	if ProviderIsNil(provider) {
		return 0, errors.New("model provider is unavailable")
	}
	return provider.EstimateRequestTokens(req)
}

func ProviderIsNil(provider Provider) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func ValidateContextLimits(contextWindowTokens, maxOutputTokens int) error {
	if contextWindowTokens <= 0 || contextWindowTokens > MaxContextWindowTokens {
		return fmt.Errorf("context window tokens must be between 1 and %d", MaxContextWindowTokens)
	}
	if maxOutputTokens <= 0 {
		return errors.New("max output tokens must be greater than 0")
	}
	if maxOutputTokens >= contextWindowTokens {
		return errors.New("max output tokens must be less than context window tokens")
	}
	return nil
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
