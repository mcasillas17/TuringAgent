package agent

import (
	"net/http"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

func routedJob() *turingv1.AgentJob {
	job := testJob()
	job.ModelProvider = turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
	job.Model = "claude-sonnet-4-5"
	job.ExternalAgent = &turingv1.ExternalAgentTarget{
		DisplayName:   "Claude",
		BaseUrl:       "https://api.anthropic.com/v1",
		CredentialRef: "claude",
	}
	return job
}

func TestExternalAgentProviderResolvesTheNamedKey(t *testing.T) {
	resolve := NewExternalAgentProviderFunc(map[string]string{"claude": "sk-test"}, 8192, 512, http.DefaultClient)

	provider, err := resolve(routedJob().GetExternalAgent())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// One OpenAI-compatible client configured per agent, not one integration
	// per vendor — that reuse is the whole point of the routing design.
	if provider.ID() != "openai_compatible" {
		t.Fatalf("provider = %q, want openai_compatible", provider.ID())
	}
}

// The error has to name the credential and where to put it. "401 unauthorized"
// from a vendor sends the user to the wrong place entirely.
func TestExternalAgentProviderSaysWhichKeyIsMissing(t *testing.T) {
	resolve := NewExternalAgentProviderFunc(map[string]string{"openai": "sk-other"}, 8192, 512, http.DefaultClient)

	_, err := resolve(routedJob().GetExternalAgent())
	if err == nil {
		t.Fatal("resolve succeeded with no matching key")
	}
	if !strings.Contains(err.Error(), `"claude"`) {
		t.Fatalf("error = %q, want it to name the credential", err)
	}
	if !strings.Contains(err.Error(), "TURING_AGENT_API_KEYS") {
		t.Fatalf("error = %q, want it to say where the key goes", err)
	}
	// And it must not echo a key it did find.
	if strings.Contains(err.Error(), "sk-other") {
		t.Fatalf("error leaked a configured key: %q", err)
	}
}

// An agent with no endpoint would otherwise be handed to the HTTP client as an
// empty base URL, which fails somewhere far less legible than here.
func TestExternalAgentProviderRefusesATargetWithNoEndpoint(t *testing.T) {
	resolve := NewExternalAgentProviderFunc(map[string]string{"claude": "sk-test"}, 8192, 512, http.DefaultClient)

	_, err := resolve(&turingv1.ExternalAgentTarget{
		DisplayName:   "Claude",
		CredentialRef: "claude",
	})
	if err == nil {
		t.Fatal("resolve succeeded with no base URL")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("error = %q, want it to name the missing endpoint", err)
	}
}

// nil means "not routed", so a nil target reaching the resolver at all is a
// wiring mistake, not a run that should quietly proceed locally.
func TestExternalAgentProviderRefusesANilTarget(t *testing.T) {
	resolve := NewExternalAgentProviderFunc(map[string]string{"claude": "sk-test"}, 8192, 512, http.DefaultClient)

	if _, err := resolve(nil); err == nil {
		t.Fatal("resolve succeeded with a nil target")
	}
}

// The asymmetry with recall is deliberate and worth pinning: recall is
// suppressed for a routed run because it draws on OTHER conversations the user
// never chose to send anywhere, while the tools stay because they act on this
// conversation, at this user's request, with mutations still gated by the
// approval token. If that decision is ever reversed it should be by editing
// this test, not by a change nobody noticed.
func TestRoutedRunStillOffersTheLocalToolsItWasDiscoveredWith(t *testing.T) {
	remote := &scriptedProvider{events: []llm.StreamEvent{
		{Type: "delta", Text: "ok"},
		{Type: "completed", FinishReason: "stop"},
	}}
	local := &scriptedProvider{events: []llm.StreamEvent{
		{Type: "delta", Text: "ok"},
		{Type: "completed", FinishReason: "stop"},
	}}
	newAssistant := func() *GeneralAssistant {
		assistant := NewGeneralAssistant(
			map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: local},
			fakeMessageClient{},
			&GeneralAssistantTools{Runner: &tools.Runner{PostBeacon: allowToolCall}},
		)
		assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
			return remote, nil
		})
		return assistant
	}

	collectUpdates(t, newAssistant(), routedJob())
	collectUpdates(t, newAssistant(), testJob())

	if len(remote.requests) != 1 || len(local.requests) != 1 {
		t.Fatalf("requests = %d routed / %d local, want one each", len(remote.requests), len(local.requests))
	}
	// Same tool set both ways. A routed run being handed a different registry
	// than the local one would mean the disclosure in the client ("the results
	// of any tool it runs") describes something other than what happens.
	if len(remote.requests[0].Tools) != len(local.requests[0].Tools) {
		t.Fatalf("routed run saw %d tools, local run saw %d; the two must match",
			len(remote.requests[0].Tools), len(local.requests[0].Tools))
	}
}

// A routed run must never fall back to the local provider map. Answering
// locally under another assistant's name is the one failure this feature
// cannot have, because the user was told the message went elsewhere.
func TestRoutedRunNeverUsesTheLocalProviderMap(t *testing.T) {
	local := &scriptedProvider{events: []llm.StreamEvent{
		{Type: "delta", Text: "local"},
		{Type: "completed", FinishReason: "stop"},
	}}
	remote := &scriptedProvider{events: []llm.StreamEvent{
		{Type: "delta", Text: "remote"},
		{Type: "completed", FinishReason: "stop"},
	}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA:            local,
			turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE: local,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})

	collectUpdates(t, assistant, routedJob())

	if len(remote.requests) != 1 {
		t.Fatalf("routed provider called %d times, want 1", len(remote.requests))
	}
	if len(local.requests) != 0 {
		t.Fatalf("local provider called %d times, want 0", len(local.requests))
	}
	// The model named on the job is the agent's, frozen at enqueue.
	if remote.requests[0].Model != "claude-sonnet-4-5" {
		t.Fatalf("model = %q, want the agent's", remote.requests[0].Model)
	}
}

// Left unwired, a routed job must fail loudly. Silently running it locally
// would answer as Claude with the local model.
func TestRoutedRunFailsWhenTheRuntimeCannotReachExternalAgents(t *testing.T) {
	local := &scriptedProvider{events: []llm.StreamEvent{
		{Type: "delta", Text: "local"},
		{Type: "completed", FinishReason: "stop"},
	}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE: local,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)

	updates := collectUpdates(t, assistant, routedJob())

	if len(local.requests) != 0 {
		t.Fatalf("local provider called %d times, want 0", len(local.requests))
	}
	failure := findRunFailed(updates)
	if failure == nil {
		t.Fatal("no run failure emitted")
	}
	if failure.GetCode() != "external_agent_unavailable" {
		t.Fatalf("code = %q, want external_agent_unavailable", failure.GetCode())
	}
	// Retrying does not create a config file. This one needs a person.
	if failure.GetRetryable() {
		t.Fatal("failure is retryable; a missing configuration is not fixed by waiting")
	}
}

func TestRoutedRunFailsWithTheResolverError(t *testing.T) {
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{},
		fakeMessageClient{},
		&GeneralAssistantTools{Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)
	assistant.SetExternalAgentProvider(NewExternalAgentProviderFunc(nil, 8192, 512, http.DefaultClient))

	updates := collectUpdates(t, assistant, routedJob())

	failure := findRunFailed(updates)
	if failure == nil {
		t.Fatal("no run failure emitted")
	}

	if !strings.Contains(failure.GetMessage(), "TURING_AGENT_API_KEYS") {
		t.Fatalf("message = %q, want the resolver's explanation", failure.GetMessage())
	}
}

func TestExternalAgentProviderUsesConfiguredContextWindow(t *testing.T) {
	resolve := NewExternalAgentProviderFunc(
		map[string]string{"claude": "sk-test"},
		4096,
		512,
		http.DefaultClient,
	)

	provider, err := resolve(routedJob().GetExternalAgent())
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.ContextWindowTokens(); got != 4096 {
		t.Fatalf("context window = %d, want 4096", got)
	}
}

// The user opted ONE conversation into leaving. Recall draws on conversations
// they never chose to send anywhere, so it must not widen that consent.
func TestRoutedRunDoesNotSendRecalledMaterialFromOtherConversations(t *testing.T) {
	remote := &scriptedProvider{events: []llm.StreamEvent{
		{Type: "delta", Text: "ok"},
		{Type: "completed", FinishReason: "stop"},
	}}
	recaller := &fakeRecaller{
		block: llm.ChatMessage{Role: "system", Content: "recalled material"},
		ok:    true,
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{},
		fakeMessageClient{},
		&GeneralAssistantTools{Recall: recaller, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})

	collectUpdates(t, assistant, routedJob())

	// Not called at all, rather than called and discarded: the search itself
	// is cheap to skip and there is nothing it could usefully return here.
	if recaller.callCount != 0 {
		t.Fatalf("Recall called %d times for a routed run, want 0", recaller.callCount)
	}
	if len(remote.requests) != 1 {
		t.Fatalf("provider called %d times, want 1", len(remote.requests))
	}
	for _, message := range remote.requests[0].Messages {
		if strings.Contains(message.Content, "recalled material") {
			t.Fatalf("recalled material reached an external agent: %+v", message)
		}
	}
}

// The same assistant must still recall for a run that stays here, or the guard
// above would be indistinguishable from recall being broken outright.
func TestLocalRunStillRecalls(t *testing.T) {
	local := &scriptedProvider{events: []llm.StreamEvent{
		{Type: "delta", Text: "ok"},
		{Type: "completed", FinishReason: "stop"},
	}}
	recaller := &fakeRecaller{
		block: llm.ChatMessage{Role: "system", Content: "recalled material"},
		ok:    true,
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: local},
		fakeMessageClient{},
		&GeneralAssistantTools{Recall: recaller, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		t.Fatal("an unrouted run must not consult the external resolver")
		return nil, nil
	})

	collectUpdates(t, assistant, testJob())

	if recaller.callCount != 1 {
		t.Fatalf("Recall called %d times for a local run, want 1", recaller.callCount)
	}
}

func findRunFailed(updates []*turingv1.RuntimeUpdate) *turingv1.RuntimeRunFailed {
	for _, update := range updates {
		if failed := update.GetRunFailed(); failed != nil {
			return failed
		}
	}
	return nil
}
