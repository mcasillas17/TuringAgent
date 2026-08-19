package agent

import (
	"errors"
	"fmt"
	"net/http"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
)

// ExternalAgentProviderFunc resolves the provider for a run the user routed to
// an assistant that does not run on this machine.
//
// A function rather than another entry in the provider map, because the map is
// keyed by ModelProvider and every external agent shares one of those: what
// differs is the base URL and which credential to use, which are properties of
// the job, not of the process.
type ExternalAgentProviderFunc func(target *turingv1.ExternalAgentTarget) (llm.Provider, error)

// ErrExternalAgentRoutingUnavailable is returned when a routed job arrives at
// a runtime that was never given a way to reach external agents. Reported
// rather than silently running locally: a message the user addressed to Claude
// must not be quietly answered by the local model as though it had been.
var ErrExternalAgentRoutingUnavailable = errors.New("this runtime is not configured to reach external agents")

// NewExternalAgentProviderFunc builds the resolver the runtime uses in
// production.
//
// keys maps a credential name to an API key and comes from the process
// environment. The name travels on the job; the key never does, so a database
// copied off this machine, a job payload, and every gRPC message on the wire
// are all free of third-party secrets.
func NewExternalAgentProviderFunc(
	keys map[string]string,
	contextWindowTokens int,
	client *http.Client,
) ExternalAgentProviderFunc {
	return func(target *turingv1.ExternalAgentTarget) (llm.Provider, error) {
		if target == nil {
			return nil, ErrExternalAgentRoutingUnavailable
		}
		apiKey := keys[target.GetCredentialRef()]
		if apiKey == "" {
			// Names the credential, never a value, and says exactly where to
			// put the key. A run that fails with "unauthorized" from a vendor
			// sends the user to the wrong place.
			return nil, fmt.Errorf(
				"no API key named %q is configured: add it to TURING_AGENT_API_KEYS in turing-backend/.env and restart the backend",
				target.GetCredentialRef())
		}
		if target.GetBaseUrl() == "" {
			return nil, errors.New("this agent has no endpoint configured")
		}
		// Every vendor this section is for — Anthropic, OpenAI, Google, xAI —
		// exposes an OpenAI-compatible chat-completions endpoint, so this is
		// one client configured four ways rather than four integrations.
		return llm.NewOpenAICompatible(target.GetBaseUrl(), apiKey, client).
			WithContextWindowTokens(contextWindowTokens), nil
	}
}
