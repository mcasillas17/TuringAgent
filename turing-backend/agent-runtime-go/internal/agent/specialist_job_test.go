package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

func specialistJob(instructions string, maxToolCalls int32) *turingv1.AgentJob {
	job := testJob()
	job.UserText = "brief from Turing: find the design notes"
	job.AgentProfile = &turingv1.AgentProfileSnapshot{
		ProfileId: "research", Revision: "rev-1", DisplayName: "Research", Emoji: "🔬",
		Instructions: instructions, MaxToolCalls: maxToolCalls,
	}
	return job
}

func completedProvider() *scriptedProvider {
	return &scriptedProvider{events: []llm.StreamEvent{{Type: "delta", Text: "done"}, {Type: "completed", FinishReason: "stop"}}}
}

// A specialist speaks with its profile, not as Turing: the profile body is
// the system instruction and the persona is never sent, even when pinned.
func TestASpecialistJobSpeaksWithItsProfileInsteadOfThePersona(t *testing.T) {
	provider := completedProvider()
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, nil)
	job := specialistJob("You are Research. Cite sources.", 0)
	job.PinnedPersona = &turingv1.PinnedPersonaSnapshot{PersonaId: "turing", Body: "You are Turing, warm and brief."}

	updates := collectUpdates(t, assistant, job)
	if updates[len(updates)-1].GetRunCompleted() == nil {
		t.Fatalf("terminal update = %+v, want completed", updates[len(updates)-1])
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	messages := provider.requests[0].Messages
	if len(messages) < 2 || messages[0].Role != "system" || messages[0].Content != "You are Research. Cite sources." {
		t.Fatalf("first message = %+v, want the profile body at system role", messages)
	}
	last := messages[len(messages)-1]
	if last.Role != "user" || last.Content != job.GetUserText() {
		t.Fatalf("last message = %+v, want the brief as the live user turn", last)
	}
	for _, message := range messages {
		if strings.Contains(message.Content, "You are Turing") {
			t.Fatalf("the persona reached a specialist: %+v", messages)
		}
	}
}

// Turing owns the user's identity, so a specialist is never told who the user
// is: a pinned profile the orchestrator failed to withhold is dropped, not
// sent.
func TestASpecialistJobNeverCarriesThePinnedProfile(t *testing.T) {
	provider := completedProvider()
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, nil)
	job := specialistJob("You are Research. Cite sources.", 0)
	job.PinnedProfile = &turingv1.PinnedProfileSnapshot{ProfileId: "profile.md", Body: "The user is Mike. They like long walks."}

	updates := collectUpdates(t, assistant, job)
	if updates[len(updates)-1].GetRunCompleted() == nil {
		t.Fatalf("terminal update = %+v, want completed", updates[len(updates)-1])
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	for _, message := range provider.requests[0].Messages {
		if strings.Contains(message.Content, "The user is Mike") || strings.Contains(message.Content, "TURING_RETRIEVED_") {
			t.Fatalf("the pinned profile reached a specialist: %+v", message)
		}
	}
}

// Without a profile the persona is the system instruction, exactly as today.
func TestAnOrdinaryJobStillSpeaksWithThePersona(t *testing.T) {
	provider := completedProvider()
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, nil)
	job := testJob()
	job.PinnedPersona = &turingv1.PinnedPersonaSnapshot{PersonaId: "turing", Body: "You are Turing, warm and brief."}

	collectUpdates(t, assistant, job)
	if len(provider.requests) != 1 || provider.requests[0].Messages[0].Role != "system" ||
		provider.requests[0].Messages[0].Content != "You are Turing, warm and brief." {
		t.Fatalf("requests = %+v, want the persona at system role", provider.requests)
	}
}

// Pinned memory is dropped, and said so, when it does not fit. A specialist's
// instructions are not memory: running without them would be a different
// agent, so the run fails instead.
func TestASpecialistWhoseProfileCannotFitFailsInsteadOfRunningWithoutIt(t *testing.T) {
	provider := completedProvider()
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, nil)
	job := specialistJob(strings.Repeat("Read every source twice. ", 4*llm.DefaultContextWindowTokens), 0)

	updates := collectUpdates(t, assistant, job)
	failed := updates[len(updates)-1].GetRunFailed()
	if failed == nil || failed.GetCode() != "context_budget_exceeded" {
		t.Fatalf("terminal update = %+v, want context_budget_exceeded", updates[len(updates)-1])
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider requests = %d, want none without the profile", len(provider.requests))
	}
}

// A profile's max_tool_calls lowers the run's tool-call limit, never raises it.
func TestASpecialistsToolCallLimitIsTheLowerOfItsProfileAndTheRuntime(t *testing.T) {
	for _, test := range []struct {
		name        string
		runtimeCap  int
		profileCap  int32
		calls       int
		wantLimited bool
	}{
		{"profile lowers the cap", 10, 2, 3, true},
		{"profile at its cap", 10, 3, 3, false},
		{"profile cannot raise the cap", 2, 50, 3, true},
		{"no profile cap uses the runtime's", 10, 0, 3, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			requested := make([]llm.ToolCall, test.calls)
			definitions := make([]map[string]any, test.calls)
			for index := range requested {
				name := fmt.Sprintf("system.tool_%d", index)
				requested[index] = llm.ToolCall{ID: fmt.Sprintf("provider_%d", index), Name: name}
				definitions[index] = map[string]any{"name": name}
			}
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: requested}},
				{{Type: "delta", Text: "done"}, {Type: "completed", FinishReason: "stop"}},
			}}
			client := &assistantTestToolLister{definitions: definitions}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}, MaxToolCallsPerRun: test.runtimeCap},
			)

			updates := collectUpdates(t, assistant, specialistJob("You are Research.", test.profileCap))
			failed := updates[len(updates)-1].GetRunFailed()
			limited := failed != nil && failed.GetCode() == "tool_call_limit_exceeded"
			if limited != test.wantLimited {
				t.Fatalf("limited = %v (terminal %+v), want %v", limited, updates[len(updates)-1], test.wantLimited)
			}
			if limited && len(client.calls) != 0 {
				t.Fatalf("MCP calls = %d, want none past the limit", len(client.calls))
			}
		})
	}
}

func enforcementToolset(runner *tools.Runner) (*GeneralAssistantTools, *assistantTestToolLister, *assistantTestToolLister) {
	system := &assistantTestToolLister{definitions: []map[string]any{{"name": "system.first"}, {"name": "system.second"}}}
	files := &assistantTestToolLister{definitions: []map[string]any{{"name": "files.read"}}}
	return &GeneralAssistantTools{SystemMCP: system, FilesMCP: files, Runner: runner}, system, files
}

func requestToolNames(request llm.ChatRequest) []string {
	names := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// A frozen set is offered whole or not at all: an enforcing job whose set
// names a tool this worker cannot offer fails before any request, exactly as a
// frozen egress set does today, rather than running with the rest of it.
func TestAnEnforcedJobNamingAToolTheWorkerLacksFailsBeforeAnyRequest(t *testing.T) {
	provider := completedProvider()
	toolset, _, _ := enforcementToolset(&tools.Runner{PostBeacon: allowToolCall})
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, toolset)
	job := testJob()
	job.EnforceSelectedTools = true
	job.SelectedTools = []string{"system/system.first", "vendor/vendor.missing"}

	updates := collectUpdates(t, assistant, job)
	if failed := updates[len(updates)-1].GetRunFailed(); failed == nil || failed.GetCode() != "egress_decision_invalid" {
		t.Fatalf("terminal = %+v, want egress_decision_invalid", updates[len(updates)-1])
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider requests = %d, want none", len(provider.requests))
	}
}

// With the flag the model is offered exactly the frozen set, with no egress
// decision needed, and an empty set is no tools at all — never the registry.
// Without it the whole registry is offered, as today.
func TestAnEnforcedJobIsOfferedExactlyItsSelectedTools(t *testing.T) {
	for _, test := range []struct {
		name     string
		enforce  bool
		selected []string
		want     []string
	}{
		{"its set", true, []string{"system/system.first"}, []string{"system.first"}},
		{"an empty set is no tools", true, nil, []string{}},
		{"unenforced keeps the registry", false, []string{"system/system.first"}, []string{"files.read", "skill_view", "skills_list", "system.first", "system.second"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := completedProvider()
			toolset, _, _ := enforcementToolset(&tools.Runner{PostBeacon: allowToolCall})
			assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, toolset)
			job := testJob()
			job.EnforceSelectedTools = test.enforce
			job.SelectedTools = test.selected

			collectUpdates(t, assistant, job)
			if len(provider.requests) != 1 {
				t.Fatalf("provider requests = %d, want 1", len(provider.requests))
			}
			got := requestToolNames(provider.requests[0])
			slices.Sort(got)
			if !slices.Equal(got, test.want) {
				t.Fatalf("offered tools = %v, want %v", got, test.want)
			}
		})
	}
}

// A call outside the set is refused in the runtime before any beacon is
// posted, built-in reads included, so the orchestrator never even sees it.
func TestAnEnforcedJobDispatchesNothingOutsideItsSet(t *testing.T) {
	for _, name := range []string{"system.second", "files.read", "skills_list"} {
		t.Run(name, func(t *testing.T) {
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_1", Name: name}}}},
				{{Type: "delta", Text: "done"}, {Type: "completed", FinishReason: "stop"}},
			}}
			beacons := 0
			toolset, system, files := enforcementToolset(&tools.Runner{PostBeacon: func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
				beacons++
				return allowToolCall(ctx, beacon)
			}})
			assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, toolset)
			job := testJob()
			job.EnforceSelectedTools = true
			job.SelectedTools = []string{"system/system.first"}

			collectUpdates(t, assistant, job)
			if beacons != 0 || len(system.calls) != 0 || len(files.calls) != 0 {
				t.Fatalf("beacons = %d, system calls = %d, files calls = %d; want none for an out-of-set call", beacons, len(system.calls), len(files.calls))
			}
		})
	}
}

// Automatic recall searches every conversation the user has, so a specialist
// or continuation never gets it: material from unrelated sessions must not
// reach a model whose context only the brief or the results should shape.
func TestAJobThatSkipsRecallNeverConsultsIt(t *testing.T) {
	for _, skip := range []bool{true, false} {
		t.Run(fmt.Sprintf("skip %v", skip), func(t *testing.T) {
			provider := completedProvider()
			recaller := &fakeRecaller{block: llm.ChatMessage{Role: "system", Content: "a private message from another session"}, ok: true}
			assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, &GeneralAssistantTools{Recall: recaller})
			job := testJob()
			job.SkipAutomaticRecall = skip

			collectUpdates(t, assistant, job)
			leaked := false
			for _, message := range provider.requests[0].Messages {
				leaked = leaked || strings.Contains(message.Content, "a private message from another session")
			}
			if skip && (recaller.callCount != 0 || leaked) {
				t.Fatalf("recall calls = %d, leaked = %v; want recall never consulted", recaller.callCount, leaked)
			}
			if !skip && (recaller.callCount == 0 || !leaked) {
				t.Fatalf("recall calls = %d, leaked = %v; want recall used as today", recaller.callCount, leaked)
			}
		})
	}
}

// A specialist never recalls, whether or not its job remembered to say so:
// recall would put the user's other conversations in front of an agent that
// is not Turing, so the runtime refuses to rely on the flag being set.
func TestASpecialistNeverRecallsEvenWithoutTheFlag(t *testing.T) {
	provider := completedProvider()
	recaller := &fakeRecaller{block: llm.ChatMessage{Role: "system", Content: "a private message from another session"}, ok: true}
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, &GeneralAssistantTools{Recall: recaller})
	job := specialistJob("You are Research.", 0)
	job.SkipAutomaticRecall = false

	collectUpdates(t, assistant, job)
	if recaller.callCount != 0 {
		t.Fatalf("recall calls = %d, want a specialist never to consult recall", recaller.callCount)
	}
	for _, message := range provider.requests[0].Messages {
		if strings.Contains(message.Content, "a private message from another session") {
			t.Fatalf("recalled material reached a specialist: %+v", message)
		}
	}
}

// A specialist's instructions are mandatory, but they are judged against the
// context the budgeter can actually build: a large tool result is compacted
// first, as for any run, and the instructions stay.
func TestASpecialistsInstructionsSurviveToolResultCompaction(t *testing.T) {
	provider := &budgetCapturingProvider{
		window: 700,
		responses: [][]llm.StreamEvent{{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
			ID: "call_1", Name: "system.read", Arguments: map[string]any{},
		}}}}},
	}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.read"}},
		result:      map[string]any{"content": strings.Repeat("oversized result ", 100)},
	}
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{}, &GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}})
	job := specialistJob("You are Research.", 0)
	job.UserText = "hi"

	updates := collectUpdates(t, assistant, job)
	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d (terminal %+v), want a compacted second dispatch", len(provider.requests), updates[len(updates)-1])
	}
	second := provider.requests[1].Messages
	if second[0].Role != "system" || second[0].Content != "You are Research." {
		t.Fatalf("first message = %+v, want the profile instructions kept", second[0])
	}
	if result := second[len(second)-1]; result.Role != "tool" || !strings.Contains(result.Content, "omitted") {
		t.Fatalf("tool result = %+v, want it compacted", result)
	}
}

// An enforced set is offered whole or the run fails: the budget never quietly
// drops a tool the job was frozen with. Without the flag, legacy budgeting
// still drops an optional tool that does not fit.
func TestAnEnforcedSetIsNeverNarrowedByTheContextBudget(t *testing.T) {
	description := strings.Repeat("describes the tool at length. ", 40)
	for _, enforce := range []bool{true, false} {
		t.Run(fmt.Sprintf("enforce %v", enforce), func(t *testing.T) {
			provider := &budgetCapturingProvider{
				window:    900,
				responses: [][]llm.StreamEvent{{{Type: "delta", Text: "done"}, {Type: "completed", FinishReason: "stop"}}},
			}
			client := &assistantTestToolLister{definitions: []map[string]any{{"name": "system.first", "description": description}}}
			assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{}, &GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}})
			job := testJob()
			job.EnforceSelectedTools = enforce
			job.SelectedTools = []string{"system/system.first"}

			updates := collectUpdates(t, assistant, job)
			terminal := updates[len(updates)-1]
			if enforce {
				if failed := terminal.GetRunFailed(); failed == nil || failed.GetCode() != "context_budget_exceeded" || len(provider.requests) != 0 {
					t.Fatalf("terminal = %+v with %d requests, want context_budget_exceeded before any request", terminal, len(provider.requests))
				}
				return
			}
			if terminal.GetRunCompleted() == nil || len(provider.requests) != 1 || slices.Contains(requestToolNames(provider.requests[0]), "system.first") {
				t.Fatalf("terminal = %+v, requests = %d; want legacy budgeting to drop the tool and complete", terminal, len(provider.requests))
			}
		})
	}
}

// The /tool debug shortcut calls the runner directly. A specialist or an
// enforcing job never takes it: no beacon is posted for a tool outside the
// set, and an oversized profile still fails on the context budget.
func TestADebugCommandCannotBypassTheSpecialistContract(t *testing.T) {
	for _, text := range []string{"/tool system.time", "/tool files.create"} {
		t.Run(text, func(t *testing.T) {
			beacons := 0
			toolset, _, _ := enforcementToolset(&tools.Runner{PostBeacon: func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
				beacons++
				return allowToolCall(ctx, beacon)
			}})
			assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: completedProvider()}, fakeMessageClient{}, toolset)

			enforced := testJob()
			enforced.UserText = text
			enforced.EnforceSelectedTools = true
			collectUpdates(t, assistant, enforced)
			if beacons != 0 {
				t.Fatalf("an enforcing job's debug command posted %d beacons, want none", beacons)
			}

			oversized := specialistJob(strings.Repeat("Read every source twice. ", 4*llm.DefaultContextWindowTokens), 0)
			oversized.UserText = text
			updates := collectUpdates(t, assistant, oversized)
			if failed := updates[len(updates)-1].GetRunFailed(); failed == nil || failed.GetCode() != "context_budget_exceeded" || beacons != 0 {
				t.Fatalf("oversized specialist debug command = %+v with %d beacons, want context_budget_exceeded and none", updates[len(updates)-1], beacons)
			}
		})
	}
}

// The skill index is optional context; a specialist's instructions and its
// enforced tools are not. So the index is sized against them, on the first
// turn and when checking that a turn after tool calls will fit: a specialist
// that fits without the index runs with the index cut and disclosed, rather
// than failing on a budget the index alone used up.
func TestASpecialistsSkillIndexGivesWayToItsMandatoryContext(t *testing.T) {
	for _, toolCall := range []bool{false, true} {
		t.Run(fmt.Sprintf("tool call %v", toolCall), func(t *testing.T) {
			run := func(window int, skills []*turingv1.SkillSnapshot) (*budgetCapturingProvider, []*turingv1.RuntimeUpdate) {
				provider := &budgetCapturingProvider{window: window, output: 100}
				if toolCall {
					provider.responses = [][]llm.StreamEvent{
						{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "system.first", Arguments: map[string]any{}}}}},
						{{Type: "delta", Text: "done"}},
					}
				}
				client := &assistantTestToolLister{
					definitions: []map[string]any{{"name": "system.first", "description": strings.Repeat("describes the tool at length. ", 20)}},
					result:      map[string]any{"content": "ok"},
				}
				assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
					fakeMessageClient{}, &GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}})
				job := specialistJob("You are Research. "+strings.Repeat("Cite every source you use. ", 20), 0)
				job.EnforceSelectedTools = true
				job.SelectedTools = []string{"system/system.first"}
				job.Skills = skills
				return provider, collectUpdates(t, assistant, job)
			}
			wantRequests := 1
			if toolCall {
				wantRequests = 2
			}

			// Measure what the specialist must carry, with no index to compete.
			measured, updates := run(1<<20, nil)
			if updates[len(updates)-1].GetRunCompleted() == nil || len(measured.estimates) != wantRequests {
				t.Fatalf("baseline terminal = %+v with %d requests, want %d completed", updates[len(updates)-1], len(measured.estimates), wantRequests)
			}
			var skills []*turingv1.SkillSnapshot
			for index := 0; index < 60; index++ {
				skills = append(skills, &turingv1.SkillSnapshot{
					SkillId: fmt.Sprintf("research/skill-%02d", index), Name: fmt.Sprintf("Skill %02d", index),
					Category: "research", Description: strings.Repeat("a long skill description ", 10),
				})
			}
			// Room for the mandatory context and a disclosure, far less than the index.
			window := slices.Max(measured.estimates) + 100 + 600

			provider, updates := run(window, skills)
			if updates[len(updates)-1].GetRunCompleted() == nil || len(provider.requests) != wantRequests {
				t.Fatalf("terminal = %+v with %d requests, want the specialist to run without its full index", updates[len(updates)-1], len(provider.requests))
			}
			for index, request := range provider.requests {
				if request.Messages[0].Role != "system" || !strings.HasPrefix(request.Messages[0].Content, "You are Research.") {
					t.Fatalf("request %d first message = %+v, want the profile instructions", index, request.Messages[0])
				}
				if !slices.Equal(requestToolNames(request), []string{"system.first"}) {
					t.Fatalf("request %d tools = %v, want exactly the enforced set", index, requestToolNames(request))
				}
				if provider.estimates[index]+provider.output > provider.window {
					t.Fatalf("request %d estimate %d + output %d exceeds window %d", index, provider.estimates[index], provider.output, provider.window)
				}
			}
			disclosed := false
			for _, update := range updates {
				if event := update.GetEvent(); event != nil && event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
					payload := event.GetPayload().AsMap()
					disclosed = disclosed || (payload["reason"] == "context_budget" && payload["skillIndexOmitted"] == true)
				}
			}
			if !disclosed {
				t.Fatal("the cut skill index was not disclosed")
			}
		})
	}
}
