package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func routedJob() *turingv1.AgentJob {
	job := testJob()
	job.ModelProvider = turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
	job.Model = "claude-sonnet-4-5"
	job.ExternalAgent = &turingv1.ExternalAgentTarget{
		AgentId:       "agent_claude",
		DisplayName:   "Claude",
		BaseUrl:       "https://api.anthropic.com/v1",
		CredentialRef: "claude",
	}
	job.SelectedTools = []string{"skills/skill_view", "skills/skills_list"}
	skillFingerprint, _ := backendegress.SkillSnapshotFingerprint(nil)
	job.EgressDecision = &turingv1.RunEgressDecision{
		DecisionId: "egress_test", Version: int32(backendegress.DecisionVersion),
		Provider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:    "claude-sonnet-4-5", Endpoint: "https://api.anthropic.com/v1",
		EndpointHost:              "api.anthropic.com",
		ExternalAgentId:           "agent_claude",
		ExternalCredentialRefHash: backendegress.HashCredentialReference("claude"),
		DataCategories: []turingv1.EgressDataCategory{
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CURRENT_MESSAGE,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_SCHEMAS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS,
		},
		ChallengeFingerprint:     "fingerprint_test",
		RequestDigest:            "request_digest_test",
		ConsentGrantedAt:         timestamppb.Now(),
		SelectedTools:            append([]string(nil), job.SelectedTools...),
		SkillSnapshotFingerprint: skillFingerprint,
		RecallApplicable:         false,
	}
	bindRuntimeMemory(job)
	return job
}

func authorizeDirectRemoteJob(job *turingv1.AgentJob, endpoint string) {
	skillFingerprint, _ := runtimeSkillSnapshotFingerprint(job.GetSkills())
	job.EgressDecision = &turingv1.RunEgressDecision{
		DecisionId: "egress_direct_test", Version: int32(backendegress.DecisionVersion),
		Provider: job.GetModelProvider(), Model: job.GetModel(),
		Endpoint: endpoint, ChallengeFingerprint: "fingerprint_direct_test",
		RequestDigest: "request_digest_direct_test",
		DataCategories: []turingv1.EgressDataCategory{
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CURRENT_MESSAGE,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL,
		},
		ConsentGrantedAt:         timestamppb.Now(),
		SkillSnapshotFingerprint: skillFingerprint,
		RecallApplicable:         true,
	}
	parsed, _ := backendegress.ParseKeyedEndpoint(endpoint)
	job.EgressDecision.EndpointHost = parsed.Host
	bindRuntimeMemory(job)
}

func addDisclosedSkill(job *turingv1.AgentJob) {
	job.Skills = []*turingv1.SkillSnapshot{{
		SkillId: "writing/tone", Name: "Tone", Instructions: "Be concise.",
	}}
	fingerprint, _ := runtimeSkillSnapshotFingerprint(job.GetSkills())
	job.EgressDecision.SkillSnapshotFingerprint = fingerprint
	job.EgressDecision.DataCategories = append(
		job.EgressDecision.DataCategories,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_SKILL_CONTENT,
	)
	slices.Sort(job.EgressDecision.DataCategories)
}

func newRoutedRemoteAssistant() (*GeneralAssistant, *scriptedProvider) {
	remote := &scriptedProvider{
		endpoint: "https://api.anthropic.com/v1",
		events:   []llm.StreamEvent{{Type: "completed", FinishReason: "stop"}},
	}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	return assistant, remote
}

func TestRemoteRunWithDisclosedSkillExecutes(t *testing.T) {
	assistant, remote := newRoutedRemoteAssistant()
	job := routedJob()
	addDisclosedSkill(job)

	updates := collectUpdates(t, assistant, job)
	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("remote run failed: %+v", failure)
	}
	if len(remote.requests) != 1 {
		t.Fatalf("remote requests = %d, want 1", len(remote.requests))
	}
}

func TestRemoteRunRejectsSkillCategoryWithEmptySnapshot(t *testing.T) {
	assistant, remote := newRoutedRemoteAssistant()
	job := routedJob()
	job.EgressDecision.DataCategories = append(
		job.EgressDecision.DataCategories,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_SKILL_CONTENT,
	)
	slices.Sort(job.EgressDecision.DataCategories)

	updates := collectUpdates(t, assistant, job)
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" || failure.GetRetryable() {
		t.Fatalf("failure = %+v, want non-retryable egress_decision_invalid", failure)
	}
	if len(remote.requests) != 0 {
		t.Fatalf("remote provider requests = %d, want 0", len(remote.requests))
	}
}

func TestRemoteRunRejectsUndisclosedNonEmptySkillSnapshot(t *testing.T) {
	assistant, remote := newRoutedRemoteAssistant()
	job := routedJob()
	job.Skills = []*turingv1.SkillSnapshot{{
		SkillId: "writing/tone", Name: "Tone", Instructions: "Be concise.",
	}}
	fingerprint, err := runtimeSkillSnapshotFingerprint(job.GetSkills())
	if err != nil {
		t.Fatal(err)
	}
	job.EgressDecision.SkillSnapshotFingerprint = fingerprint

	updates := collectUpdates(t, assistant, job)
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" || failure.GetRetryable() {
		t.Fatalf("failure = %+v, want non-retryable egress_decision_invalid", failure)
	}
	if len(remote.requests) != 0 {
		t.Fatalf("remote provider requests = %d, want 0", len(remote.requests))
	}
}

func TestRoutedRunRejectsMissingEgressDecisionBeforeProviderIO(t *testing.T) {
	assistant, remote := newRoutedRemoteAssistant()
	job := routedJob()
	job.EgressDecision = nil

	updates := collectUpdates(t, assistant, job)
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" || failure.GetRetryable() {
		t.Fatalf("failure = %+v, want non-retryable egress_decision_invalid", failure)
	}
	if len(remote.requests) != 0 {
		t.Fatalf("remote provider requests = %d, want 0", len(remote.requests))
	}
}

func TestRoutedRunRejectsEndpointMismatchBeforeProviderIO(t *testing.T) {
	remote := &scriptedProvider{endpoint: "https://api.anthropic.com/v1", events: []llm.StreamEvent{{Type: "completed", FinishReason: "stop"}}}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	job := routedJob()
	job.EgressDecision.Endpoint = "https://other.example/v1"

	updates := collectUpdates(t, assistant, job)
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" {
		t.Fatalf("failure = %+v, want egress_decision_invalid", failure)
	}
	if len(remote.requests) != 0 {
		t.Fatalf("remote provider requests = %d, want 0", len(remote.requests))
	}
}

func TestExternalTargetCannotBypassRemoteClassificationWithLocalProviderEnum(t *testing.T) {
	remote := &scriptedProvider{endpoint: "https://api.anthropic.com/v1", events: []llm.StreamEvent{{Type: "completed"}}}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	job := routedJob()
	job.ModelProvider = turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA

	updates := collectUpdates(t, assistant, job)
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" {
		t.Fatalf("failure = %+v, want egress_decision_invalid", failure)
	}
	if len(remote.requests) != 0 {
		t.Fatalf("remote provider requests = %d, want 0", len(remote.requests))
	}
}

func TestRemoteRunRejectsUnsupportedDisclosureCategories(t *testing.T) {
	remote := &scriptedProvider{endpoint: "https://api.anthropic.com/v1", events: []llm.StreamEvent{{Type: "completed"}}}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	job := routedJob()
	job.EgressDecision.DataCategories = append(
		job.EgressDecision.DataCategories,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_ATTACHMENTS,
	)

	updates := collectUpdates(t, assistant, job)
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" {
		t.Fatalf("failure = %+v, want egress_decision_invalid", failure)
	}
	if len(remote.requests) != 0 {
		t.Fatalf("remote provider requests = %d, want 0", len(remote.requests))
	}
}

func TestLocalProviderMapCannotHideRemoteProvider(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()
	provider := llm.NewOpenAICompatible(server.URL, "", server.Client())
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{},
	)

	updates := collectUpdates(t, assistant, testJob())
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" {
		t.Fatalf("failure = %+v, want egress_decision_invalid", failure)
	}

	if requests != 0 {
		t.Fatalf("remote HTTP requests = %d, want 0", requests)
	}
}

func TestLocalRunRejectsRemoteEgressDecisionBeforeProviderIO(t *testing.T) {
	local := &scriptedProvider{events: []llm.StreamEvent{{Type: "completed"}}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: local,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{},
	)
	job := testJob()
	job.EgressDecision = routedJob().EgressDecision

	updates := collectUpdates(t, assistant, job)
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" ||
		failure.GetRetryable() {
		t.Fatalf("failure = %+v, want non-retryable egress_decision_invalid", failure)
	}
	if len(local.requests) != 0 {
		t.Fatalf("local provider requests = %d, want 0", len(local.requests))
	}
}

func TestRemoteRunRejectsUnselectedToolAndListsOnlySelectedTools(t *testing.T) {
	remote := &queuedProvider{
		endpoint: "https://api.anthropic.com/v1",
		responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_unselected", Name: "system.time", Arguments: map[string]any{},
			}}}, {Type: "completed", FinishReason: "tool_calls"}},
			{{Type: "delta", Text: "done"}, {Type: "completed", FinishReason: "stop"}},
		},
	}

	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.time"}},
		result:      map[string]any{"utc": "never"},
	}
	assistant := NewGeneralAssistant(
		nil,
		fakeMessageClient{},
		&GeneralAssistantTools{
			SystemMCP: client,
			Runner:    &tools.Runner{PostBeacon: allowToolCall},
		},
	)
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})

	updates := collectUpdates(t, assistant, routedJob())
	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("remote run failed: %+v", failure)
	}
	if len(client.calls) != 0 {
		t.Fatalf("unselected tool executed %d time(s)", len(client.calls))
	}
	if len(remote.requests) != 2 {
		t.Fatalf("remote requests = %d, want 2", len(remote.requests))
	}
	result := remote.requests[1].Messages[len(remote.requests[1].Messages)-1].Content
	var payload unknownToolPayload
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.AvailableTools) != 2 ||
		payload.AvailableTools[0] != "skills_list" ||
		payload.AvailableTools[1] != "skill_view" {
		t.Fatalf("unknown-tool available tools = %v", payload.AvailableTools)
	}
}

func TestRemoteRunWithoutToolResultConsentDoesNotSendUnknownToolResult(t *testing.T) {
	remote := &queuedProvider{
		endpoint: "https://api.anthropic.com/v1",
		responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_undisclosed", Name: "system.time", Arguments: map[string]any{},
			}}}, {Type: "completed", FinishReason: "tool_calls"}},
		},
	}

	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	job := routedJob()
	job.SelectedTools = nil
	job.EgressDecision.SelectedTools = nil
	job.EgressDecision.DataCategories = []turingv1.EgressDataCategory{
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CURRENT_MESSAGE,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY,
	}

	updates := collectUpdates(t, assistant, job)
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" {
		t.Fatalf("failure = %+v, want egress_decision_invalid", failure)
	}
	if len(remote.requests) != 1 {
		t.Fatalf("remote requests = %d, want exactly 1", len(remote.requests))
	}
}

func TestRemoteRunRejectsSkillSnapshotFingerprintMismatch(t *testing.T) {
	remote := &scriptedProvider{
		endpoint: "https://api.anthropic.com/v1",
		events:   []llm.StreamEvent{{Type: "completed"}},
	}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	job := routedJob()
	job.Skills = []*turingv1.SkillSnapshot{{
		SkillId: "writing/tone", Name: "Tone", Instructions: "Changed snapshot",
	}}

	updates := collectUpdates(t, assistant, job)
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" {
		t.Fatalf("failure = %+v, want egress_decision_invalid", failure)
	}
	if len(remote.requests) != 0 {
		t.Fatalf("remote requests = %d, want 0", len(remote.requests))
	}
}

func TestRemoteRunRejectsUnavailableFrozenSelectedTool(t *testing.T) {
	remote := &scriptedProvider{
		endpoint: "https://api.anthropic.com/v1",
		events:   []llm.StreamEvent{{Type: "completed"}},
	}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	job := routedJob()
	job.SelectedTools = append(job.SelectedTools, "system/missing")
	job.EgressDecision.SelectedTools = append(
		job.EgressDecision.SelectedTools,
		"system/missing",
	)

	updates := collectUpdates(t, assistant, job)
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" ||
		failure.GetRetryable() {
		t.Fatalf("failure = %+v, want non-retryable egress_decision_invalid", failure)
	}
	if len(remote.requests) != 0 {
		t.Fatalf("remote requests = %d, want 0", len(remote.requests))
	}
}

func TestRemoteRunRejectsProviderWithoutRemoteIdentity(t *testing.T) {
	tests := []struct {
		name     string
		provider llm.Provider
	}{
		{
			name: "non-remote provider",
			provider: &scriptedProvider{
				events: []llm.StreamEvent{{Type: "completed"}},
			},
		},
		{
			name: "remote provider without endpoint identity",
			provider: &nonIdentifyingRemoteProvider{
				events: []llm.StreamEvent{{Type: "completed"}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
			assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
				return test.provider, nil
			})
			updates := collectUpdates(t, assistant, routedJob())
			failure := findRunFailed(updates)
			if failure == nil || failure.GetCode() != "egress_decision_invalid" {
				t.Fatalf("failure = %+v, want egress_decision_invalid", failure)
			}
		})
	}
}

type nonIdentifyingRemoteProvider struct {
	events []llm.StreamEvent
}

func (*nonIdentifyingRemoteProvider) ID() string { return "openai_compatible" }
func (*nonIdentifyingRemoteProvider) ContextWindowTokens() int {
	return llm.DefaultContextWindowTokens
}
func (*nonIdentifyingRemoteProvider) MaxOutputTokens() int {
	return llm.DefaultMaxOutputTokens
}
func (*nonIdentifyingRemoteProvider) EstimateRequestTokens(req llm.ChatRequest) (int, error) {
	return estimateTestProviderRequest(req)
}
func (p *nonIdentifyingRemoteProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	out := make(chan llm.StreamEvent, len(p.events))
	for _, event := range p.events {
		out <- event
	}
	close(out)
	return out, nil
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
	remote := &scriptedProvider{endpoint: "https://api.anthropic.com/v1", events: []llm.StreamEvent{
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
	remote := &scriptedProvider{endpoint: "https://api.anthropic.com/v1", events: []llm.StreamEvent{
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

	// The resolver's explanation — which names the environment variable an
	// operator has to set — stays in the runtime's own error. What crosses the
	// boundary is that this run could not reach the external provider it was
	// routed to, which is what the orchestrator turns into a public outcome.
	if failure.GetCode() != "external_agent_unavailable" {
		t.Fatalf("code = %q, want external_agent_unavailable", failure.GetCode())
	}
	if failure.GetFailureOrigin() != turingv1.FailureOrigin_FAILURE_ORIGIN_EXTERNAL_PROVIDER {
		t.Fatalf("origin = %v, want external provider", failure.GetFailureOrigin())
	}
	if failure.GetMessage() != "" {
		t.Fatalf("resolver text crossed the runtime boundary as %q", failure.GetMessage())
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

func TestRoutedExternalAgentNoticesOpenAIOutputLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			"data: " + `{"choices":[{"index":0,"delta":{"content":"partial"}}]}` + "\n\n" +
				"data: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}` + "\n\n",
		))
	}))
	t.Cleanup(server.Close)
	provider, err := llm.NewOpenAICompatibleWithLimits(server.URL, "", server.Client(), 4096, 512)
	if err != nil {
		t.Fatal(err)
	}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return provider, nil
	})

	job := routedJob()
	job.ExternalAgent.BaseUrl = server.URL
	job.EgressDecision.Endpoint = server.URL
	endpoint, _ := backendegress.ParseKeyedEndpoint(server.URL)
	job.EgressDecision.EndpointHost = endpoint.Host
	updates := collectUpdates(t, assistant, job)

	for _, update := range updates {
		event := update.GetEvent()
		if event == nil || event.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			continue
		}
		payload := event.GetPayload().AsMap()
		if payload["reason"] == "model_output_limit" &&
			payload["setting"] == "OPENAI_MAX_OUTPUT_TOKENS" {
			return
		}
	}
	t.Fatalf("routed output-limit notice missing from updates: %#v", updates)
}

// The user opted ONE conversation into leaving. Recall draws on conversations
// they never chose to send anywhere, so it must not widen that consent.
func TestRoutedRunDoesNotSendRecalledMaterialFromOtherConversations(t *testing.T) {
	remote := &scriptedProvider{endpoint: "https://api.anthropic.com/v1", events: []llm.StreamEvent{
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

// bindRuntimeMemory gives a fixture the memory binding a real job carries: the
// fingerprint the runtime re-derives from the pinned snapshot on the job, on
// both the job and the decision it is checked against.
func bindRuntimeMemory(job *turingv1.AgentJob) {
	fingerprint, err := runtimeMemorySnapshotFingerprint(job)
	if err != nil {
		panic(err)
	}
	job.MemorySnapshotFingerprint = fingerprint
	if job.GetEgressDecision() != nil {
		job.EgressDecision.MemorySnapshotFingerprint = fingerprint
	}
}
