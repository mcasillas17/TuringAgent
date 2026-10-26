package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
)

// The local route's engines, by the protocol the runtime speaks to them. Which
// product serves the protocol (ds4, llama.cpp, LM Studio, …) is the operator's
// business and shows only in the engine label.
const (
	LocalEngineOllama = "ollama"
	LocalEngineOpenAI = "openai"
)

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
