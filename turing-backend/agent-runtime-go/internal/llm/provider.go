package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
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
	// Origin is where an error event came from, as the provider knows it from
	// protocol facts. It travels with the event because the provider is the
	// only layer that can see those facts, and because the alternative —
	// deciding downstream from Message — is the untrusted-text channel this
	// whole design closes. It is meaningful only on an "error" event.
	Origin    turingv1.FailureOrigin
	ToolCalls []ToolCall
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

// providerError builds an error event from a code this package chose.
//
// The origin comes from the code rather than from the message, and the mapping
// lives in one place so two providers reporting the same protocol fact cannot
// disagree about where it came from. Message stays for local logging and
// provider-level tests; nothing downstream may classify on it.
func providerError(code string, message string) StreamEvent {
	return StreamEvent{Type: "error", Code: code, Message: message, Origin: providerFailureOrigin(code)}
}

// providerFailureOrigin maps a provider's own typed error code onto the origin
// that code always describes.
//
// Protocol codes — a status the server returned, a chunk that did not parse,
// an error object the API sent — are provider-protocol. A stream that stopped
// early or timed out is provider-transport: nothing was wrong with the
// exchange, it simply did not finish. Anything else this package has not
// classified is left to the orchestrator's fail-closed default rather than
// guessed at here.
func providerFailureOrigin(code string) turingv1.FailureOrigin {
	if origin, ok := providerFailureOrigins[code]; ok {
		return origin
	}
	return turingv1.FailureOrigin_FAILURE_ORIGIN_UNKNOWN
}

// providerFailureOrigins is that mapping, as a table rather than a switch.
//
// The orchestrator pairs each of these codes with its origin to pick the public
// outcome, and it cannot import this package — agent-runtime-go's internal
// packages are not reachable from orchestrator-go — so its inventory is a copy.
// A copy needs something to be checked against, and a table is readable from
// source in one place; a switch spread across arms is not. Adding an entry here
// is therefore the single edit that widens the forwarded provider vocabulary,
// and runoutcome's producer-pair scan requires the two to agree.
var providerFailureOrigins = map[string]turingv1.FailureOrigin{
	"model_unavailable":    turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
	"model_auth_failed":    turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
	"model_request_failed": turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
	"model_quota_exceeded": turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
	"model_bad_chunk":      turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
	"model_stream_error":   turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
	"model_timeout":        turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
	"model_error":          turingv1.FailureOrigin_FAILURE_ORIGIN_EXTERNAL_PROVIDER,
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
