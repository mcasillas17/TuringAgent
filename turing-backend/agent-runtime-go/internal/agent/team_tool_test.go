package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
	"google.golang.org/protobuf/types/known/structpb"
)

// fakeTeam is the orchestrator's internal team facet as one test scripts it.
type fakeTeam struct {
	mu          sync.Mutex
	descriptors []*turingv1.TeamToolDescriptor
	listErr     error
	// stall makes a listing wait for its context to end, as a hung
	// orchestrator would; unbounded records that nothing ever ended it.
	stall     bool
	unbounded bool
	listed    []string
	calls     []*turingv1.CallTeamToolRequest
}

func (f *fakeTeam) ListTeamTools(ctx context.Context, runID string) (*turingv1.ListTeamToolsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listed = append(f.listed, runID)
	if f.stall {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
			f.unbounded = true
			return nil, errors.New("listing was never bounded")
		}
	}
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &turingv1.ListTeamToolsResponse{Tools: f.descriptors}, nil
}

func (f *fakeTeam) CallTeamTool(_ context.Context, request *turingv1.CallTeamToolRequest) (*turingv1.CallTeamToolResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, request)
	result, err := structpb.NewStruct(map[string]any{"delegation_id": "dlg_1", "agent": "research", "state": "queued"})
	if err != nil {
		return nil, err
	}
	return &turingv1.CallTeamToolResponse{Result: result}, nil
}

func teamDescriptor(t *testing.T) *turingv1.TeamToolDescriptor {
	t.Helper()
	schema, err := structpb.NewStruct(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent": map[string]any{"type": "string", "enum": []any{"research"}},
			"task":  map[string]any{"type": "string"},
		},
		"required": []any{"agent", "task"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &turingv1.TeamToolDescriptor{
		ToolName: "team.delegate", Description: "Delegate a task to a specialist. The result arrives later.",
		Schema: schema, Enabled: true, Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	}
}

func teamToolset(runner *tools.Runner, team *fakeTeam) (*GeneralAssistantTools, *assistantTestToolLister) {
	toolset, system, _ := enforcementToolset(runner)
	toolset.Team = team
	return toolset, system
}

func offeredTool(request llm.ChatRequest, name string) (llm.ToolDefinition, bool) {
	for _, tool := range request.Tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return llm.ToolDefinition{}, false
}

func delegateCall() []llm.StreamEvent {
	return []llm.StreamEvent{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
		ID: "provider_1", Name: "team.delegate", Arguments: map[string]any{"agent": "research", "task": "Gather the notes"},
	}}}}
}

func finalAnswer() []llm.StreamEvent {
	return []llm.StreamEvent{{Type: "delta", Text: "Asked Research."}, {Type: "completed", FinishReason: "stop"}}
}

// A parent with no frozen set is offered the team tool its run was given, as
// the orchestrator renders it for that run.
func TestAParentIsOfferedTheTeamToolItsRunWasGiven(t *testing.T) {
	provider := completedProvider()
	team := &fakeTeam{descriptors: []*turingv1.TeamToolDescriptor{teamDescriptor(t)}}
	toolset, _ := teamToolset(&tools.Runner{PostBeacon: allowToolCall}, team)
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, toolset)

	collectUpdates(t, assistant, testJob())
	if !slices.Equal(team.listed, []string{"run_1"}) {
		t.Fatalf("listed = %v, want the run asked once", team.listed)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	tool, offered := offeredTool(provider.requests[0], "team.delegate")
	if !offered {
		t.Fatalf("offered tools = %v, want team.delegate", requestToolNames(provider.requests[0]))
	}
	if !strings.Contains(tool.Description, "result arrives later") {
		t.Fatalf("description = %q, want the orchestrator's", tool.Description)
	}
	agent, _ := tool.Parameters["properties"].(map[string]any)["agent"].(map[string]any)
	if enum, _ := agent["enum"].([]any); !slices.Equal(enum, []any{"research"}) {
		t.Fatalf("schema = %+v, want the run's roster", tool.Parameters)
	}
}

// A frozen set offers the team only when it names team/team.delegate, and
// naming it never makes the set unavailable: the team is not in the registry,
// so it is set aside before the rest of the set is resolved.
func TestAFrozenSetOffersTheTeamOnlyWhenItNamesIt(t *testing.T) {
	for _, test := range []struct {
		name       string
		selected   []string
		wantTeam   bool
		wantListed int
	}{
		{"names it", []string{"system/system.first", "team/team.delegate"}, true, 1},
		{"does not name it", []string{"system/system.first"}, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := completedProvider()
			team := &fakeTeam{descriptors: []*turingv1.TeamToolDescriptor{teamDescriptor(t)}}
			toolset, _ := teamToolset(&tools.Runner{PostBeacon: allowToolCall}, team)
			assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, toolset)
			job := testJob()
			job.EnforceSelectedTools = true
			job.SelectedTools = test.selected

			updates := collectUpdates(t, assistant, job)
			if updates[len(updates)-1].GetRunCompleted() == nil {
				t.Fatalf("terminal = %+v, want completed", updates[len(updates)-1])
			}
			got := requestToolNames(provider.requests[0])
			slices.Sort(got)
			want := []string{"system.first"}
			if test.wantTeam {
				want = []string{"system.first", "team.delegate"}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("offered = %v, want %v", got, want)
			}
			if len(team.listed) != test.wantListed {
				t.Fatalf("ListTeamTools calls = %d, want %d", len(team.listed), test.wantListed)
			}
			if !slices.Equal(job.GetSelectedTools(), test.selected) {
				t.Fatalf("selected tools = %v, want the job's set unchanged", job.GetSelectedTools())
			}
		})
	}
}

// An egress decision's frozen set is held to the same rule as an enforced one:
// the team is asked for, and offered, only when the set names it.
func TestAnEgressSetOffersTheTeamOnlyWhenItNamesIt(t *testing.T) {
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"system": &registryTestClient{tools: []map[string]any{{"name": "system.time", "inputSchema": map[string]any{"type": "object"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		selected []string
		want     bool
	}{
		{"names it", []string{"system/system.time", "team/team.delegate"}, true},
		{"does not name it", []string{"system/system.time"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			team := &fakeTeam{descriptors: []*turingv1.TeamToolDescriptor{teamDescriptor(t)}}
			assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{Team: team})
			job := testJob()
			job.SelectedTools = test.selected
			job.EgressDecision = &turingv1.RunEgressDecision{DecisionId: "egress_1"}

			_, offered := assistant.teamDefinition(context.Background(), job, registry)
			if offered != test.want || (len(team.listed) == 1) != test.want {
				t.Fatalf("offered = %v after %d listings, want %v", offered, len(team.listed), test.want)
			}
		})
	}
}

// The same partition applies to an egress decision's frozen set.
func TestAnEgressSetNamingTheTeamResolvesTheRestOfItsTools(t *testing.T) {
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"system": &registryTestClient{tools: []map[string]any{{"name": "system.time", "inputSchema": map[string]any{"type": "object"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	job := &turingv1.AgentJob{
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		SelectedTools:  []string{"system/system.time", "team/team.delegate"},
		EgressDecision: &turingv1.RunEgressDecision{DecisionId: "egress_local"},
	}
	definitions, err := toolDefinitionsForJob(registry, job)
	if err != nil {
		t.Fatalf("a frozen set naming the team was refused: %v", err)
	}
	if len(definitions) != 1 || definitions[0].Name != "system.time" {
		t.Fatalf("definitions = %+v, want the registry's share of the set", definitions)
	}
}

// No descriptor, a disabled one, or a failed listing offers no team, and the
// run goes on with its other tools: the team is optional.
func TestARunWithoutAUsableTeamToolGoesOnWithoutIt(t *testing.T) {
	disabled := teamDescriptor(t)
	disabled.Policy = turingv1.ToolPolicy_TOOL_POLICY_DISABLED
	notEnabled := teamDescriptor(t)
	notEnabled.Enabled = false
	malformed := teamDescriptor(t)
	stringRoot, err := structpb.NewStruct(map[string]any{"type": "string"})
	if err != nil {
		t.Fatal(err)
	}
	malformed.Schema = stringRoot
	for _, test := range []struct {
		name string
		team *fakeTeam
	}{
		{"none offered", &fakeTeam{}},
		{"disabled", &fakeTeam{descriptors: []*turingv1.TeamToolDescriptor{disabled}}},
		{"not enabled", &fakeTeam{descriptors: []*turingv1.TeamToolDescriptor{notEnabled}}},
		{"malformed schema", &fakeTeam{descriptors: []*turingv1.TeamToolDescriptor{malformed}}},
		{"listing failed", &fakeTeam{listErr: errors.New("orchestrator unavailable")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := completedProvider()
			toolset, _ := teamToolset(&tools.Runner{PostBeacon: allowToolCall}, test.team)
			assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, toolset)

			updates := collectUpdates(t, assistant, testJob())
			if updates[len(updates)-1].GetRunCompleted() == nil {
				t.Fatalf("terminal = %+v, want completed", updates[len(updates)-1])
			}
			if _, offered := offeredTool(provider.requests[0], "team.delegate"); offered {
				t.Fatalf("offered = %v, want no team", requestToolNames(provider.requests[0]))
			}
		})
	}
}

// A listing that hangs is cut off at the tool timeout like any other tool
// discovery, and the run goes on without the team rather than holding the
// worker's slot until someone cancels it.
func TestAHungTeamListingIsBoundedByTheToolTimeout(t *testing.T) {
	provider := completedProvider()
	team := &fakeTeam{descriptors: []*turingv1.TeamToolDescriptor{teamDescriptor(t)}, stall: true}
	toolset, _ := teamToolset(&tools.Runner{PostBeacon: allowToolCall}, team)
	toolset.ToolTimeout = 20 * time.Millisecond
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, toolset)

	updates := collectUpdates(t, assistant, testJob())
	if team.unbounded {
		t.Fatal("the team listing was given no deadline")
	}
	if updates[len(updates)-1].GetRunCompleted() == nil {
		t.Fatalf("terminal = %+v, want completed", updates[len(updates)-1])
	}
	if _, offered := offeredTool(provider.requests[0], "team.delegate"); offered {
		t.Fatalf("offered = %v, want no team", requestToolNames(provider.requests[0]))
	}
}

// Every run asks for its own team; nothing is cached across runs, so two
// parents each see only their own roster.
func TestEachRunAsksForItsOwnTeam(t *testing.T) {
	team := &fakeTeam{descriptors: []*turingv1.TeamToolDescriptor{teamDescriptor(t)}}
	toolset, _ := teamToolset(&tools.Runner{PostBeacon: allowToolCall}, team)
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: completedProvider()}, fakeMessageClient{}, toolset)
	first, second := testJob(), testJob()
	second.RunId = "run_2"

	collectUpdates(t, assistant, first)
	collectUpdates(t, assistant, second)
	if !slices.Equal(team.listed, []string{"run_1", "run_2"}) {
		t.Fatalf("listed = %v, want each run asked for its own", team.listed)
	}
}

// A team.delegate call goes through the tool runner, with its beacon, to the
// orchestrator, carrying the run, the assignment it holds, the decided
// approval and the model's tool call; the delegation comes back as the result.
func TestATeamDelegateCallIsDispatchedToTheOrchestrator(t *testing.T) {
	for _, test := range []struct {
		name         string
		policy       func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)
		wantApproval string
	}{
		{"allowed", allowToolCall, ""},
		{"approved", func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			return approvalToolCall(beacon), nil
		}, "approval_1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &queuedProvider{responses: [][]llm.StreamEvent{delegateCall(), finalAnswer()}}
			var beacons []*turingv1.ToolCallBeacon
			runner := &tools.Runner{
				PostBeacon: func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
					beacons = append(beacons, beacon)
					return test.policy(ctx, beacon)
				},
				WaitApproval:   func(context.Context, string) (string, error) { return "token", nil },
				ResumeApproved: func(context.Context, tools.ApprovalResume) error { return nil },
			}
			team := &fakeTeam{descriptors: []*turingv1.TeamToolDescriptor{teamDescriptor(t)}}
			toolset, _ := teamToolset(runner, team)
			assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, toolset)
			job := testJob()
			job.AssignmentAttemptId = "attempt_1"

			updates := collectUpdates(t, assistant, job)
			if updates[len(updates)-1].GetRunCompleted() == nil {
				t.Fatalf("terminal = %+v, want completed", updates[len(updates)-1])
			}
			if len(beacons) == 0 || beacons[0].GetServerName() != "team" || beacons[0].GetToolName() != "team.delegate" ||
				beacons[0].GetModelToolCallId() != "provider_1" {
				t.Fatalf("beacons = %+v, want a team/team.delegate beacon for the model's call", beacons)
			}
			if len(team.calls) != 1 {
				t.Fatalf("CallTeamTool calls = %d, want 1", len(team.calls))
			}
			call := team.calls[0]
			if call.GetRunId() != "run_1" || call.GetAssignmentAttemptId() != "attempt_1" || call.GetApprovalId() != test.wantApproval ||
				call.GetToolCallId() != "provider_1" || call.GetToolName() != "team.delegate" || call.GetArgs().AsMap()["task"] != "Gather the notes" {
				t.Fatalf("CallTeamTool request = %+v", call)
			}
			second := provider.requests[1].Messages
			result := second[len(second)-1]
			if result.Role != "tool" || !strings.Contains(result.Content, `"delegation_id":"dlg_1"`) {
				t.Fatalf("tool result = %+v, want the delegation", result)
			}
		})
	}
}

// A team.delegate call from a run that was not offered the team is an unknown
// tool: no beacon is posted and nothing reaches the orchestrator.
func TestAnUnofferedTeamDelegateCallIsAnUnknownTool(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{delegateCall(), finalAnswer()}}
	beacons := 0
	runner := &tools.Runner{PostBeacon: func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		beacons++
		return allowToolCall(ctx, beacon)
	}}
	team := &fakeTeam{}
	toolset, _ := teamToolset(runner, team)
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, toolset)

	collectUpdates(t, assistant, testJob())
	if beacons != 0 || len(team.calls) != 0 {
		t.Fatalf("beacons = %d, CallTeamTool calls = %d; want none for an unoffered team.delegate", beacons, len(team.calls))
	}
}

// A registry tool already named team.delegate keeps the team out: the model
// would otherwise see two tools with one name. Its calls go to that tool.
func TestARegistryToolNamedTeamDelegateKeepsTheTeamOut(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{delegateCall(), finalAnswer()}}
	team := &fakeTeam{descriptors: []*turingv1.TeamToolDescriptor{teamDescriptor(t)}}
	toolset, system := teamToolset(&tools.Runner{PostBeacon: allowToolCall}, team)
	system.definitions = append(system.definitions, map[string]any{"name": "team.delegate"})
	system.result = map[string]any{"ok": true}
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{}, toolset)

	collectUpdates(t, assistant, testJob())
	if len(team.listed) != 0 || len(team.calls) != 0 {
		t.Fatalf("listed = %v, calls = %d; want the team untouched", team.listed, len(team.calls))
	}
	count := 0
	for _, name := range requestToolNames(provider.requests[0]) {
		if name == "team.delegate" {
			count++
		}
	}
	if count != 1 || len(system.calls) != 1 {
		t.Fatalf("team.delegate offered %d times, registry calls = %d; want the registry's one tool", count, len(system.calls))
	}
}

// A worker that can delegate advertises team/team.delegate, so the
// orchestrator accepts its beacon and the name can enter a frozen set; one
// without the team client does not.
func TestTheWorkerAdvertisesTheTeamToolOnlyWhenItCanDelegate(t *testing.T) {
	for _, withTeam := range []bool{true, false} {
		toolset, _, _ := enforcementToolset(&tools.Runner{PostBeacon: allowToolCall})
		if withTeam {
			toolset.Team = &fakeTeam{}
		}
		assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{}, fakeMessageClient{}, toolset)
		advertised, err := assistant.AdvertisedTools(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		found := slices.ContainsFunc(advertised, func(tool *turingv1.DiscoveredTool) bool {
			return tool.GetServerName() == "team" && tool.GetToolName() == "team.delegate"
		})
		if found != withTeam {
			t.Fatalf("with team %v: advertised team/team.delegate = %v", withTeam, found)
		}
	}
}
