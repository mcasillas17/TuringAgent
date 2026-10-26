package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"unicode"
	"unicode/utf8"

	"github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
)

// The local route's engines, by the protocol the runtime speaks to them. Which
// product serves the protocol (ds4, llama.cpp, LM Studio, …) is the operator's
// business and shows only in the engine label.
const (
	LocalEngineOllama = "ollama"
	LocalEngineOpenAI = "openai"
)

// maxEngineLabelBytes bounds a label a client renders verbatim.
const maxEngineLabelBytes = 64

// ValidateEngineLabel accepts a display label for the local engine: 1–64
// bytes of valid UTF-8 with no control characters. It is shown to the user and
// never routed on.
func ValidateEngineLabel(label string) error {
	if label == "" || len(label) > maxEngineLabelBytes || !utf8.ValidString(label) {
		return errors.New("must be 1 to 64 bytes of valid UTF-8")
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return errors.New("must not contain control characters")
		}
	}
	return nil
}

// LocalOptions configures the local route's engine. BaseURL must name this
// machine; APIKey is for the OpenAI engine only; KeepAlive is Ollama's only.
type LocalOptions struct {
	Engine              string
	BaseURL             string
	APIKey              string
	KeepAlive           string
	Client              *http.Client
	ContextWindowTokens int
	MaxOutputTokens     int
}

// NewLocal builds the provider for the local route. The address is checked
// again here, not only in configuration: a local provider is consent-free
// only because its engine is on this machine.
func NewLocal(opts LocalOptions) (Provider, error) {
	endpoint, err := egress.ParseLocalEndpoint(opts.BaseURL)
	if err != nil {
		return nil, err
	}
	switch opts.Engine {
	case LocalEngineOllama:
		if opts.APIKey != "" {
			return nil, errors.New("an API key applies only to the OpenAI-compatible local engine")
		}
		provider, err := NewOllamaWithLimits(endpoint.Canonical, opts.Client, opts.ContextWindowTokens, opts.MaxOutputTokens)
		if err != nil {
			return nil, err
		}
		return provider.WithKeepAlive(opts.KeepAlive), nil
	case LocalEngineOpenAI:
		inner, err := NewOpenAICompatibleWithLimits(endpoint.Canonical, opts.APIKey, opts.Client, opts.ContextWindowTokens, opts.MaxOutputTokens)
		if err != nil {
			return nil, err
		}
		inner.plainMaxTokens = true
		return &LocalOpenAI{inner: inner}, nil
	}
	return nil, fmt.Errorf("local engine %q is not %q or %q", opts.Engine, LocalEngineOllama, LocalEngineOpenAI)
}

// LocalOpenAI is an OpenAI-compatible server on this machine serving the local
// route. It wraps the OpenAI-compatible transport rather than embedding it:
// embedding would promote EgressEndpoint and make it an EgressProvider, which
// the runtime reads as "data leaves the machine".
type LocalOpenAI struct {
	inner *OpenAICompatible
}

var _ Provider = (*LocalOpenAI)(nil)

func (p *LocalOpenAI) ID() string { return egress.LocalProvider }

func (p *LocalOpenAI) ContextWindowTokens() int { return p.inner.ContextWindowTokens() }

func (p *LocalOpenAI) MaxOutputTokens() int { return p.inner.MaxOutputTokens() }

func (p *LocalOpenAI) EstimateRequestTokens(req ChatRequest) (int, error) {
	return p.inner.EstimateRequestTokens(req)
}

func (p *LocalOpenAI) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	return p.inner.StreamChat(ctx, req)
}
