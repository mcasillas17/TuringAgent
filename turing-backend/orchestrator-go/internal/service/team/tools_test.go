package team

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var twoSpecialists = []repository.TeamRosterEntry{
	{ProfileID: "dev", Revision: "rev-dev", Name: "Dev", Emoji: "💻", Description: "Works on code", Tools: []string{"files/files.read"}},
	{ProfileID: "research", Revision: "rev-research", Name: "Research", Emoji: "🔬", Description: "Looks things up", Tools: []string{"system/system.time"}},
}

// enqueueWithRoster queues a Turing run that was offered roster.
func (h *harness) enqueueWithRoster(t *testing.T, roster []repository.TeamRosterEntry) string {
	t.Helper()
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Turing")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "prep me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: defaultModel, TeamRoster: roster,
	})
	if err != nil {
		t.Fatal(err)
	}
	return enqueued.RunID
}

func (h *harness) teamTools(t *testing.T, server *Server, runID string) []*turingv1.TeamToolDescriptor {
	t.Helper()
	response, err := server.ListTeamTools(context.Background(), &turingv1.ListTeamToolsRequest{RunId: runID})
	if err != nil {
		t.Fatal(err)
	}
	return response.GetTools()
}

// The run is offered one tool whose schema is its own frozen team: the agent
// enum is the roster's IDs, each described, and the brief's parts are capped.
func TestListTeamToolsRendersTheRunsRoster(t *testing.T) {
	h := newHarness(t)
	tools := h.teamTools(t, h.server, h.enqueueWithRoster(t, twoSpecialists))
	if len(tools) != 1 {
		t.Fatalf("tools = %+v, want team.delegate alone", tools)
	}
	tool := tools[0]
	if tool.GetToolName() != "team.delegate" || !tool.GetEnabled() || tool.GetPolicy() != turingv1.ToolPolicy_TOOL_POLICY_SAFE {
		t.Fatalf("tool = %+v, want an enabled, safe team.delegate", tool)
	}
	for _, said := range []string{"Answer directly whenever you can", "result arrives later", "tell the user", "sees only `task` and `context`"} {
		if !strings.Contains(tool.GetDescription(), said) {
			t.Fatalf("description %q does not say %q", tool.GetDescription(), said)
		}
	}
	schema := tool.GetSchema().AsMap()
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatalf("schema = %+v, want a closed object", schema)
	}
	if required, _ := schema["required"].([]any); !slices.Equal(required, []any{"agent", "task"}) {
		t.Fatalf("required = %v, want agent and task", schema["required"])
	}
	properties := schema["properties"].(map[string]any)
	agent := properties["agent"].(map[string]any)
	if enum, _ := agent["enum"].([]any); !slices.Equal(enum, []any{"dev", "research"}) {
		t.Fatalf("agent enum = %v, want the roster's IDs", agent["enum"])
	}
	for _, said := range []string{"dev", "Works on code", "research", "Looks things up"} {
		if !strings.Contains(agent["description"].(string), said) {
			t.Fatalf("agent description %q does not mention %q", agent["description"], said)
		}
	}
	// maxLength counts characters and the brief is bounded in bytes, so the
	// byte budget travels beside it and the description names the unit.
	for name, limit := range map[string]float64{"task": 4096, "context": 8192} {
		part := properties[name].(map[string]any)
		if part["type"] != "string" || part["maxLength"] != limit || part["x-turing-maxBytes"] != limit ||
			!strings.Contains(part["description"].(string), fmt.Sprintf("At most %d bytes of UTF-8", int(limit))) {
			t.Fatalf("%s = %+v, want a string of at most %v bytes", name, part, limit)
		}
	}
}

// The schema is the team the run was enqueued with. Editing or disabling a
// profile afterwards changes the next run, never this one.
func TestListTeamToolsReadsTheFrozenRosterNotTheProfiles(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")
	roster, err := h.server.Roster(context.Background())
	if err != nil || len(roster) != 1 {
		t.Fatalf("roster = %+v, %v", roster, err)
	}
	runID := h.enqueueWithRoster(t, roster)
	h.write(t, "research", strings.Replace(researchFrontmatter, "Looks things up", "Something else entirely", 1))
	if _, err := h.repo.SetAgentProfileEnabled(context.Background(), "research", false); err != nil {
		t.Fatal(err)
	}

	tools := h.teamTools(t, h.server, runID)
	agent := tools[0].GetSchema().AsMap()["properties"].(map[string]any)["agent"].(map[string]any)
	if enum, _ := agent["enum"].([]any); !slices.Equal(enum, []any{"research"}) ||
		!strings.Contains(agent["description"].(string), "Looks things up") {
		t.Fatalf("agent = %+v, want the roster as enqueued", agent)
	}
}

// Each run sees only the team it was enqueued with: two parents with
// different rosters, asked in either order, never see each other's team.
func TestListTeamToolsIsolatesEachRunsTeam(t *testing.T) {
	h := newHarness(t)
	both := h.enqueueWithRoster(t, twoSpecialists)
	researchOnly := h.enqueueWithRoster(t, twoSpecialists[1:])
	want := map[string][]any{both: {"dev", "research"}, researchOnly: {"research"}}
	for _, order := range [][]string{{both, researchOnly}, {researchOnly, both}} {
		for _, runID := range order {
			tools := h.teamTools(t, h.server, runID)
			agent := tools[0].GetSchema().AsMap()["properties"].(map[string]any)["agent"].(map[string]any)
			if enum, _ := agent["enum"].([]any); !slices.Equal(enum, want[runID]) {
				t.Fatalf("run %s agent enum = %v, want %v", runID, agent["enum"], want[runID])
			}
		}
	}
}

// A run that was not offered the team gets no team tool, and neither does any
// run while the team is off, the tool is withdrawn, or its policy disables it.
func TestListTeamToolsOffersNothingWhereTheRunCannotDelegate(t *testing.T) {
	for _, test := range []struct {
		name   string
		roster []repository.TeamRosterEntry
		setup  func(t *testing.T, h *harness) *Server
	}{
		{"a run without a roster", nil, func(_ *testing.T, h *harness) *Server { return h.server }},
		{"the team is off", twoSpecialists, func(_ *testing.T, h *harness) *Server {
			return New(h.repo, h.routes, defaultModel, false)
		}},
		{"a server named team", twoSpecialists, func(t *testing.T, h *harness) *Server {
			h.importServer(t, repository.ImportedMCPServer{
				Name: "team", URL: "http://team:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
			}, true, "lookup")
			return h.server
		}},
		{"a disabled policy", twoSpecialists, func(t *testing.T, h *harness) *Server {
			h.addTool(t, repository.DiscoveredTool{ServerName: "team", ToolName: "team.delegate", SchemaJSON: `{}`, Policy: "safe"})
			if err := h.repo.SetToolPolicyByName(context.Background(), "team", "team.delegate", "disabled"); err != nil {
				t.Fatal(err)
			}
			return h.server
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			runID := h.enqueueWithRoster(t, test.roster)
			if tools := h.teamTools(t, test.setup(t, h), runID); len(tools) != 0 {
				t.Fatalf("tools = %+v, want none", tools)
			}
		})
	}
}

// An approval-required policy is reported as such, so the runtime asks first.
func TestListTeamToolsReportsTheCurrentPolicy(t *testing.T) {
	h := newHarness(t)
	runID := h.enqueueWithRoster(t, twoSpecialists)
	h.addTool(t, repository.DiscoveredTool{ServerName: "team", ToolName: "team.delegate", SchemaJSON: `{}`, Policy: "safe"})
	if err := h.repo.SetToolPolicyByName(context.Background(), "team", "team.delegate", "approval_required"); err != nil {
		t.Fatal(err)
	}
	if tools := h.teamTools(t, h.server, runID); len(tools) != 1 || tools[0].GetPolicy() != turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED {
		t.Fatalf("tools = %+v, want team.delegate requiring approval", tools)
	}
}

func TestListTeamToolsValidatesTheRun(t *testing.T) {
	h := newHarness(t)
	for name, server := range map[string]*Server{
		"on": h.server, "off": New(h.repo, h.routes, defaultModel, false),
	} {
		if _, err := server.ListTeamTools(context.Background(), &turingv1.ListTeamToolsRequest{}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s: empty run_id error = %v, want InvalidArgument", name, err)
		}
		if _, err := server.ListTeamTools(context.Background(), &turingv1.ListTeamToolsRequest{RunId: "run_missing"}); status.Code(err) != codes.NotFound {
			t.Fatalf("%s: unknown run error = %v, want NotFound", name, err)
		}
	}
}

// The public facet manages profiles and never serves the tool; the internal
// facet serves the tool and never manages a profile.
func TestTheTeamFacetsSplitByMethod(t *testing.T) {
	h := newHarness(t)
	runID := h.enqueueWithRoster(t, twoSpecialists)
	ctx := context.Background()
	public, internal := NewPublicServer(h.server), NewInternalServer(h.server)
	// Only a facet can be registered: the shared server serving both halves
	// would hand the internal tool to public clients.
	if _, registrable := any(h.server).(turingv1.TeamServiceServer); registrable {
		t.Fatal("the unsplit team server satisfies TeamServiceServer")
	}

	if _, err := public.ListTeamTools(ctx, &turingv1.ListTeamToolsRequest{RunId: runID}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public ListTeamTools error = %v, want PermissionDenied", err)
	}
	if _, err := public.CallTeamTool(ctx, &turingv1.CallTeamToolRequest{RunId: runID}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public CallTeamTool error = %v, want PermissionDenied", err)
	}
	if _, err := internal.CallTeamTool(ctx, &turingv1.CallTeamToolRequest{RunId: runID}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("internal CallTeamTool error = %v, want the service's InvalidArgument", err)
	}
	if _, err := public.ListAgentProfiles(ctx, &turingv1.ListAgentProfilesRequest{}); err != nil {
		t.Fatalf("public ListAgentProfiles: %v", err)
	}
	if response, err := internal.ListTeamTools(ctx, &turingv1.ListTeamToolsRequest{RunId: runID}); err != nil || len(response.GetTools()) != 1 {
		t.Fatalf("internal ListTeamTools = %+v, %v", response, err)
	}
	for name, call := range map[string]func() error{
		"ListAgentProfiles": func() error {
			_, err := internal.ListAgentProfiles(ctx, &turingv1.ListAgentProfilesRequest{})
			return err
		},
		"SetAgentProfileEnabled": func() error {
			_, err := internal.SetAgentProfileEnabled(ctx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "dev", Enabled: true})
			return err
		},
		"GrantAgentProfile": func() error {
			_, err := internal.GrantAgentProfile(ctx, &turingv1.GrantAgentProfileRequest{ProfileId: "dev", Revision: "rev-dev"})
			return err
		},
	} {
		if err := call(); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("internal %s error = %v, want PermissionDenied", name, err)
		}
	}
}
