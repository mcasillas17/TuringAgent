package agent

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	runnertools "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
)

func pinPersona(job *turingv1.AgentJob, body string) {
	job.PinnedPersona = &turingv1.PinnedPersonaSnapshot{
		PersonaId: "persona.md", DisplayName: "persona.md", Body: body,
		ContentHash: "persona-hash-" + body,
	}
}

func pinProfile(job *turingv1.AgentJob, body string) {
	job.PinnedProfile = &turingv1.PinnedProfileSnapshot{
		ProfileId: "profile.md", Body: body, ContentHash: "profile-hash-" + body,
	}
}

// rebindMemory re-derives the fingerprint and the applicability flag the way the
// runtime will, so a test that changes the pinned snapshot is testing the gate
// rather than a stale binding it forgot to update.
func rebindMemory(t *testing.T, job *turingv1.AgentJob) {
	t.Helper()
	fingerprint, err := runtimeMemorySnapshotFingerprint(job)
	if err != nil {
		t.Fatal(err)
	}
	job.MemorySnapshotFingerprint = fingerprint
	if job.GetEgressDecision() == nil {
		return
	}
	job.EgressDecision.MemorySnapshotFingerprint = fingerprint
	applicable := runtimeMemoryProfileApplicable(job)
	job.EgressDecision.MemoryProfileApplicable = applicable
	categories := make([]turingv1.EgressDataCategory, 0, len(job.EgressDecision.GetDataCategories())+1)
	for _, category := range job.EgressDecision.GetDataCategories() {
		if category != turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_MEMORY_PROFILE {
			categories = append(categories, category)
		}
	}
	if applicable {
		categories = append(categories, turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_MEMORY_PROFILE)
	}
	slicesSortCategories(categories)
	job.EgressDecision.DataCategories = categories
}

func slicesSortCategories(categories []turingv1.EgressDataCategory) {
	for outer := 1; outer < len(categories); outer++ {
		for inner := outer; inner > 0 && categories[inner] < categories[inner-1]; inner-- {
			categories[inner], categories[inner-1] = categories[inner-1], categories[inner]
		}
	}
}

// The runtime does not take the orchestrator's word for it. It re-derives the
// fingerprint from the snapshot it was handed, and a decision bound to other
// bytes is refused before a single request reaches a provider.
func TestRemoteRunRefusesAMemoryFingerprintItCannotReproduce(t *testing.T) {
	remote := &scriptedProvider{endpoint: "https://api.anthropic.com/v1", events: []llm.StreamEvent{{Type: "completed"}}}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	job := routedJob()
	pinPersona(job, "Speak plainly.")
	rebindMemory(t, job)
	job.EgressDecision.MemorySnapshotFingerprint = "a fingerprint from another vault"

	updates := collectUpdates(t, assistant, job)
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "egress_decision_invalid" {
		t.Fatalf("failure = %+v, want egress_decision_invalid", failure)
	}
	if len(remote.requests) != 0 {
		t.Fatalf("remote provider requests = %d, want 0", len(remote.requests))
	}
}

// The job's own copy of the binding has to agree with the decision's. A run
// carrying one snapshot and a consent granted over another is not a run.
func TestRemoteRunRefusesWhenTheJobAndDecisionDisagreeAboutMemory(t *testing.T) {
	remote := &scriptedProvider{endpoint: "https://api.anthropic.com/v1", events: []llm.StreamEvent{{Type: "completed"}}}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	job := routedJob()
	pinPersona(job, "Speak plainly.")
	rebindMemory(t, job)
	job.MemorySnapshotFingerprint = "something else"

	failure := findRunFailed(collectUpdates(t, assistant, job))
	if failure == nil || failure.GetCode() != "egress_decision_invalid" {
		t.Fatalf("failure = %+v, want egress_decision_invalid", failure)
	}
	if len(remote.requests) != 0 {
		t.Fatalf("remote provider requests = %d, want 0", len(remote.requests))
	}
}

// The flag is mirrored, not trusted. A decision claiming the memory category on
// a run with nothing pinned and no memory tool is refused, and so is one that
// denies the category on a run that would send a persona.
func TestRemoteRunMirrorsTheMemoryApplicabilityFlag(t *testing.T) {
	t.Run("claims memory with nothing pinned", func(t *testing.T) {
		remote := &scriptedProvider{endpoint: "https://api.anthropic.com/v1", events: []llm.StreamEvent{{Type: "completed"}}}
		assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
		assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
			return remote, nil
		})
		job := routedJob()
		rebindMemory(t, job)
		job.EgressDecision.MemoryProfileApplicable = true
		job.EgressDecision.DataCategories = append(job.EgressDecision.GetDataCategories(),
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_MEMORY_PROFILE)

		failure := findRunFailed(collectUpdates(t, assistant, job))
		if failure == nil || failure.GetCode() != "egress_decision_invalid" {
			t.Fatalf("failure = %+v, want egress_decision_invalid", failure)
		}
		if len(remote.requests) != 0 {
			t.Fatalf("remote provider requests = %d, want 0", len(remote.requests))
		}
	})

	t.Run("denies memory on a run that would send a persona", func(t *testing.T) {
		remote := &scriptedProvider{endpoint: "https://api.anthropic.com/v1", events: []llm.StreamEvent{{Type: "completed"}}}
		assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
		assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
			return remote, nil
		})
		job := routedJob()
		pinPersona(job, "Speak plainly.")
		rebindMemory(t, job)
		job.EgressDecision.MemoryProfileApplicable = false

		failure := findRunFailed(collectUpdates(t, assistant, job))
		if failure == nil || failure.GetCode() != "egress_decision_invalid" {
			t.Fatalf("failure = %+v, want egress_decision_invalid", failure)
		}
		if len(remote.requests) != 0 {
			t.Fatalf("remote provider requests = %d, want 0", len(remote.requests))
		}
	})
}

// Whitespace has to mean the same thing on both sides of the wire. A persona of
// blank lines sends nothing, so a decision that claims the category over it is
// as wrong as one claiming it over an empty file.
func TestRemoteRunTrimsPinnedWhitespaceTheSameWayTheOrchestratorDoes(t *testing.T) {
	remote := &scriptedProvider{
		endpoint: "https://api.anthropic.com/v1",
		events:   []llm.StreamEvent{{Type: "text", Text: "ok"}, {Type: "completed"}},
	}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	job := routedJob()
	pinPersona(job, "  \n\t ")
	pinProfile(job, "\n\n")
	rebindMemory(t, job)
	if job.GetEgressDecision().GetMemoryProfileApplicable() {
		t.Fatal("whitespace-only pins were treated as content")
	}

	if failure := findRunFailed(collectUpdates(t, assistant, job)); failure != nil {
		t.Fatalf("whitespace-only pins failed the run: %+v", failure)
	}
	if len(remote.requests) != 1 {
		t.Fatalf("remote provider requests = %d, want 1", len(remote.requests))
	}
	for _, message := range remote.requests[0].Messages {
		if strings.Contains(message.Content, "persona.md") {
			t.Fatalf("an empty persona was injected anyway: %+v", message)
		}
	}
}

// A memory tool in the frozen set is memory leaving the machine even with an
// empty vault, so the category applies and the run proceeds.
func TestRemoteRunAcceptsMemoryCategoryFromSelectedToolsAlone(t *testing.T) {
	remote := &scriptedProvider{
		endpoint: "https://api.anthropic.com/v1",
		events:   []llm.StreamEvent{{Type: "text", Text: "ok"}, {Type: "completed"}},
	}
	memoryLister := &assistantTestToolLister{definitions: []map[string]any{{
		"name": "memory.search", "description": "Search",
		"inputSchema": map[string]any{"type": "object"},
	}}}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{
		MemoryTools: func(context.Context) (ToolLister, error) { return memoryLister, nil },
	})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	job := routedJob()
	job.SelectedTools = append(job.GetSelectedTools(), "memory/memory.search")
	job.EgressDecision.SelectedTools = append([]string(nil), job.GetSelectedTools()...)
	rebindMemory(t, job)
	if !job.GetEgressDecision().GetMemoryProfileApplicable() {
		t.Fatal("a selected memory tool did not make the category applicable")
	}

	if failure := findRunFailed(collectUpdates(t, assistant, job)); failure != nil {
		t.Fatalf("run failed: %+v", failure)
	}
	if len(remote.requests) != 1 {
		t.Fatalf("remote provider requests = %d, want 1", len(remote.requests))
	}
	offered := make([]string, 0, len(remote.requests[0].Tools))
	for _, definition := range remote.requests[0].Tools {
		offered = append(offered, definition.Name)
	}
	if !slices.Contains(offered, "memory.search") {
		t.Fatalf("offered tools = %v, want the memory tool", offered)
	}
}

// The toggle is the orchestrator answering with an empty list, and the registry
// has to reflect that on the next build without the worker restarting.
func TestMemoryToolsFollowTheToggleWithoutARestart(t *testing.T) {
	enabled := true
	lister := &assistantTestToolLister{listFunc: func(context.Context) ([]map[string]any, error) {
		if !enabled {
			return nil, nil
		}
		return []map[string]any{{
			"name": "memory.search", "description": "Search",
			"inputSchema": map[string]any{"type": "object"},
		}}, nil
	}}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{
		MemoryTools: func(context.Context) (ToolLister, error) { return lister, nil },
	})

	discovered, err := assistant.DiscoveredTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reportsMemoryTool(discovered) {
		t.Fatalf("discovered = %+v, want the memory tool", discovered)
	}

	enabled = false
	assistant.InvalidateToolRegistry()
	discovered, err = assistant.DiscoveredTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reportsMemoryTool(discovered) {
		t.Fatalf("discovered = %+v, want no memory tool after the toggle", discovered)
	}
}

func reportsMemoryTool(discovered []DiscoveredTool) bool {
	for _, tool := range discovered {
		if tool.ServerName == backendegress.MemoryServerName && tool.ToolName == "memory.search" {
			return true
		}
	}
	return false
}

// Memory tools are caller-enforced: the orchestrator consumes the approval and
// runs the tool itself. The result it hands back is already framed, and it has
// to reach the model exactly once — unwrapping it would strip the label that
// says these are the user's notes, re-wrapping it would nest two.
func TestMemoryToolResultReachesTheModelFramedExactlyOnce(t *testing.T) {
	framed := "BEGIN TURING_RETRIEVED_MEMORY_SEARCH_abc\nnote\nEND TURING_RETRIEVED_MEMORY_SEARCH_abc"
	lister := &assistantTestToolLister{
		definitions: []map[string]any{{
			"name": "memory.search", "description": "Search",
			"inputSchema": map[string]any{"type": "object"},
		}},
		result: map[string]any{"content": framed},
	}
	runner := &runnertools.Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			return &turingv1.ToolPolicyDecision{
				Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
				ToolCallId: beacon.GetToolCallId(), ReadOnly: true,
			}, nil
		},
	}
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "memory.search", Arguments: map[string]any{"query": "chickens"}},
		}}},
		{{Type: "delta", Text: "done"}},
	}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{
			Runner:      runner,
			MemoryTools: func(context.Context) (ToolLister, error) { return lister, nil },
		},
	)

	job := testJob()
	rebindMemory(t, job)
	collectUpdates(t, assistant, job)
	if len(lister.calls) != 1 || lister.calls[0].name != "memory.search" {
		t.Fatalf("memory tool calls = %+v", lister.calls)
	}
	delivered := 0
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if message.Role != "tool" || !strings.Contains(message.Content, "TURING_RETRIEVED_MEMORY_SEARCH_abc") {
				continue
			}
			delivered++
			if strings.Count(message.Content, "BEGIN TURING_RETRIEVED") != 1 {
				t.Fatalf("the framed result was wrapped again: %q", message.Content)
			}
		}
	}
	if delivered == 0 {
		t.Fatal("the framed memory result never reached the model")
	}
}

// The persona is the user's own instruction about who Turing is; it goes in
// unframed. The profile is a description of them, and has to arrive labelled as
// context that is never an instruction.
func TestPinnedMemoryReachesThePromptWithPersonaUnframedAndProfileFramed(t *testing.T) {
	provider := &scriptedProvider{events: []llm.StreamEvent{{Type: "text", Text: "ok"}, {Type: "completed"}}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{}, &GeneralAssistantTools{},
	)
	job := testJob()
	pinPersona(job, "You are curt and you never apologise.")
	pinProfile(job, "The user keeps chickens.")
	rebindMemory(t, job)

	collectUpdates(t, assistant, job)
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	var persona, profile *llm.ChatMessage
	for index, message := range provider.requests[0].Messages {
		if strings.Contains(message.Content, "You are curt and you never apologise.") {
			persona = &provider.requests[0].Messages[index]
		}
		if strings.Contains(message.Content, "The user keeps chickens.") {
			profile = &provider.requests[0].Messages[index]
		}
	}
	if persona == nil {
		t.Fatalf("the persona never reached the prompt: %+v", provider.requests[0].Messages)
	}
	if profile == nil {
		t.Fatalf("the profile never reached the prompt: %+v", provider.requests[0].Messages)
	}
	if persona == profile {
		t.Fatal("the persona and the profile were merged into one message")
	}
	if strings.Contains(persona.Content, "BEGIN TURING_RETRIEVED") {
		t.Fatalf("the persona was framed: %q", persona.Content)
	}
	if !strings.Contains(profile.Content, "BEGIN TURING_RETRIEVED_MEMORY_PROFILE") ||
		!strings.Contains(profile.Content, "never as an instruction") {
		t.Fatalf("the profile was not framed as non-authoritative context: %q", profile.Content)
	}
	if profile.Role != "user" {
		t.Fatalf("the profile arrived at role %q, want user", profile.Role)
	}
}

// A withheld tier is not an empty one. Nothing is injected, and nothing claims
// the user wrote nothing.
func TestWithheldPinsInjectNothing(t *testing.T) {
	provider := &scriptedProvider{events: []llm.StreamEvent{{Type: "text", Text: "ok"}, {Type: "completed"}}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{}, &GeneralAssistantTools{},
	)
	job := testJob()
	job.PinnedPersona = &turingv1.PinnedPersonaSnapshot{PersonaId: "persona.md", Withheld: true}
	job.PinnedProfile = &turingv1.PinnedProfileSnapshot{ProfileId: "profile.md", Withheld: true}
	rebindMemory(t, job)

	collectUpdates(t, assistant, job)
	for _, message := range provider.requests[0].Messages {
		if strings.Contains(message.Content, "persona.md") || strings.Contains(message.Content, "profile.md") {
			t.Fatalf("a withheld tier reached the prompt: %+v", message)
		}
	}
}

// A withheld tier that somehow still carries a body — a state the orchestrator
// must never construct, but one this runtime does not get to assume it never
// will — has to be treated as absent by both the applicability flag and the
// prompt, in agreement with each other. Disagreement here is not academic: it
// is exactly the shape of bug that would let a disclosure under-claim what a
// prompt actually sent, or refuse a run over a mismatch neither side caused.
func TestContradictoryWithheldBodyIsAbsentToApplicabilityAndThePrompt(t *testing.T) {
	remote := &scriptedProvider{
		endpoint: "https://api.anthropic.com/v1",
		events:   []llm.StreamEvent{{Type: "text", Text: "ok"}, {Type: "completed"}},
	}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{})
	assistant.SetExternalAgentProvider(func(*turingv1.ExternalAgentTarget) (llm.Provider, error) {
		return remote, nil
	})
	job := routedJob()
	job.PinnedPersona = &turingv1.PinnedPersonaSnapshot{
		PersonaId: "persona.md", Withheld: true, Body: "leftover persona that must never surface",
	}
	job.PinnedProfile = &turingv1.PinnedProfileSnapshot{
		ProfileId: "profile.md", Withheld: true, Body: "leftover profile that must never surface",
	}
	rebindMemory(t, job)

	if runtimeMemoryProfileApplicable(job) {
		t.Fatal("a withheld tier's leftover body made the memory category applicable")
	}
	if job.GetEgressDecision().GetMemoryProfileApplicable() {
		t.Fatal("rebindMemory disagrees with runtimeMemoryProfileApplicable about a withheld contradiction")
	}

	if failure := findRunFailed(collectUpdates(t, assistant, job)); failure != nil {
		t.Fatalf("a withheld tier's own contradiction failed the run: %+v", failure)
	}
	if len(remote.requests) != 1 {
		t.Fatalf("remote provider requests = %d, want 1", len(remote.requests))
	}
	for _, message := range remote.requests[0].Messages {
		if strings.Contains(message.Content, "must never surface") {
			t.Fatalf("a withheld tier's leftover body reached the prompt: %+v", message)
		}
	}
}

// Pinned memory is omitted as a unit when it cannot fit, and the omission is
// announced. Half a persona is worse than none: it would read as the user's
// complete instruction while being a fragment of it.
func TestPinnedMemoryIsOmittedAsAUnitWithAVisibleNotice(t *testing.T) {
	provider := &budgetCapturingProvider{window: 2400, output: 200}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{}, &GeneralAssistantTools{},
	)
	job := testJob()
	pinPersona(job, strings.Repeat("persona sentence. ", 400))
	pinProfile(job, strings.Repeat("profile sentence. ", 400))
	rebindMemory(t, job)

	updates := collectUpdates(t, assistant, job)
	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("run failed instead of omitting memory: %+v", failure)
	}
	if len(provider.requests) == 0 {
		t.Fatal("no request was made")
	}
	for _, message := range provider.requests[0].Messages {
		if strings.Contains(message.Content, "persona sentence.") ||
			strings.Contains(message.Content, "profile sentence.") {
			t.Fatalf("over-context memory was injected anyway: %+v", message)
		}
	}
	noticed := false
	for _, update := range updates {
		event := update.GetEvent()
		if event == nil || event.GetType() != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			continue
		}
		payload := event.GetPayload().AsMap()
		note, _ := payload["note"].(string)
		omitted, _ := payload["memoryOmitted"].(bool)
		if strings.Contains(note, "pinned memory") && omitted {
			noticed = true
		}
	}
	if !noticed {
		t.Fatal("memory was omitted without a visible notice")
	}
}

// Every field of contextOmissions is a thing the user was not shown. A field
// that no notice mentions, or that no event payload carries, is an omission
// that happens silently — which is the one outcome this type exists to prevent.
func TestEveryContextOmissionIsBothAnnouncedAndEmitted(t *testing.T) {
	value := reflect.ValueOf(&contextOmissions{}).Elem()
	structType := value.Type()
	if structType.NumField() == 0 {
		t.Fatal("contextOmissions has no fields")
	}
	if notice := (contextOmissions{}).Notice(); notice != "" {
		t.Fatalf("an empty omission set produced a notice: %q", notice)
	}
	if payload := (contextOmissions{}).EventPayload(); len(payload) != structType.NumField() {
		t.Fatalf("payload has %d keys for %d fields: %+v", len(payload), structType.NumField(), payload)
	}
	for index := range structType.NumField() {
		field := structType.Field(index)
		populated := contextOmissions{}
		target := reflect.ValueOf(&populated).Elem().Field(index)
		switch field.Type.Kind() {
		case reflect.Bool:
			target.SetBool(true)
		case reflect.Int:
			target.SetInt(1)
		default:
			t.Fatalf("contextOmissions.%s has unhandled kind %s", field.Name, field.Type.Kind())
		}
		if populated.Notice() == "" {
			t.Fatalf("contextOmissions.%s is omitted without a notice", field.Name)
		}
		payload := populated.EventPayload()
		if len(payload) != structType.NumField() {
			t.Fatalf("contextOmissions.%s payload has %d keys, want %d: %+v",
				field.Name, len(payload), structType.NumField(), payload)
		}
		found := false
		for _, emitted := range payload {
			if reflect.DeepEqual(emitted, target.Interface()) {
				found = true
			}
		}
		if !found {
			t.Fatalf("contextOmissions.%s never reaches the emitted payload: %+v", field.Name, payload)
		}
	}
}

// The emitted payload keys are the client's contract. Renaming one silently is
// how a UI stops showing an omission it used to show.
func TestContextOmissionPayloadKeysAreExact(t *testing.T) {
	payload := contextOmissions{
		HistoryMessages: 2, RecallOmitted: true, SkillIndexOmitted: true,
		MemoryOmitted: true, ToolDefinitions: 3, ToolResults: 4,
	}.EventPayload()
	want := map[string]any{
		"historyMessagesOmitted": 2,
		"recallOmitted":          true,
		"skillIndexOmitted":      true,
		"memoryOmitted":          true,
		"toolDefinitionsOmitted": 3,
		"toolResultsOmitted":     4,
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload = %+v, want %+v", payload, want)
	}
}

// Memory's fit decision has to be made against the same mandatory set the
// budget will enforce, and that set includes the two skill tools whenever the
// skill index is in. Counting only the live-required tools under-estimates it,
// and in the window that opens up, memory is admitted as mandatory and then
// cannot be shed: on a first turn there are no tool results to compact, so the
// run hard-fails on context budget instead of quietly dropping the persona.
func TestMemoryFitCountsTheMandatorySkillTools(t *testing.T) {
	job := testJob()
	pinPersona(job, "Speak plainly about everything, at all times, without exception.")
	skillMessages := []llm.ChatMessage{{Role: "user", Content: "the enabled skill index"}}
	liveMessages := []llm.ChatMessage{{Role: "user", Content: "hi"}}
	toolDefinitions := []llm.ToolDefinition{
		{Name: skillsListToolName, Description: strings.Repeat("list the enabled skills. ", 20),
			Parameters: map[string]any{"type": "object"}},
		{Name: skillViewToolName, Description: strings.Repeat("view one skill by id. ", 20),
			Parameters: map[string]any{"type": "object"}},
	}
	memoryMessages, err := pinnedMemoryMessages(job)
	if err != nil {
		t.Fatal(err)
	}
	sizing := &budgetCapturingProvider{window: 1 << 30}
	request := func(tools []llm.ToolDefinition) llm.ChatRequest {
		messages := make([]llm.ChatMessage, 0, len(memoryMessages)+2)
		messages = append(messages, memoryMessages...)
		messages = append(messages, skillMessages...)
		messages = append(messages, liveMessages...)
		return llm.ChatRequest{Model: job.GetModel(), Messages: messages, Tools: tools}
	}
	withoutTools, err := llm.EstimateRequestTokens(sizing, request(nil))
	if err != nil {
		t.Fatal(err)
	}
	withTools, err := llm.EstimateRequestTokens(sizing, request(toolDefinitions))
	if err != nil {
		t.Fatal(err)
	}
	if withTools <= withoutTools {
		t.Fatalf("skill tool schemas cost nothing: %d vs %d", withTools, withoutTools)
	}
	// Exactly enough room for the pinned memory alone, and not for the skill
	// tools the budget is about to make mandatory alongside it.
	provider := &budgetCapturingProvider{window: withoutTools}

	admitted, omitted, err := buildMemoryMessagesWithinContext(
		provider, job.GetModel(), job, skillMessages, liveMessages, toolDefinitions,
		requiredSkillToolNames(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !omitted || len(admitted) != 0 {
		t.Fatalf("memory was admitted over the mandatory skill tools: omitted=%v admitted=%d",
			omitted, len(admitted))
	}
}

// The runtime derives the same preimage the orchestrator hashed, so the two
// cannot drift into different ideas of what was pinned.
func TestRuntimeMemoryPreimageMatchesTheSharedShape(t *testing.T) {
	job := testJob()
	pinPersona(job, "Speak plainly.")
	pinProfile(job, "The user keeps chickens.")
	job.SelectedTools = []string{"memory/memory.search"}

	expected, err := backendegress.MemorySnapshotFingerprint(backendegress.MemorySnapshot{
		PersonaID: "persona.md", PersonaDisplayName: "persona.md",
		PersonaBody: "Speak plainly.", PersonaContentHash: "persona-hash-Speak plainly.",
		ProfileID: "profile.md", ProfileBody: "The user keeps chickens.",
		ProfileContentHash:  "profile-hash-The user keeps chickens.",
		MemoryToolsSelected: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := runtimeMemorySnapshotFingerprint(job)
	if err != nil {
		t.Fatal(err)
	}
	if got != expected {
		t.Fatalf("runtime fingerprint = %q, want %q", got, expected)
	}
}
