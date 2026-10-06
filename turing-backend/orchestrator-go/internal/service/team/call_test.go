package team

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type approvalCall struct {
	approvalID, runID, serverName, serverID, toolName string
	args                                              map[string]any
}

// fakeApprovals consumes approvals. during runs inside the consumption, as if
// the approval wait had outlasted a change.
type fakeApprovals struct {
	calls  []approvalCall
	err    error
	during func()
}

func (f *fakeApprovals) ConsumeApprovalForThirdParty(_ context.Context, approvalID, runID, serverName, serverID, toolName string, args map[string]any) error {
	f.calls = append(f.calls, approvalCall{approvalID, runID, serverName, serverID, toolName, args})
	if f.during != nil {
		f.during()
	}
	return f.err
}

type fakePublisher struct{ published []events.Event }

func (f *fakePublisher) Publish(event events.Event) { f.published = append(f.published, event) }

// delegating is a running Turing run that was offered Research, claimed by a
// team-protocol worker, with team.delegate registered under policy.
type delegating struct {
	runID, attempt string
	approvals      *fakeApprovals
	published      *fakePublisher
}

func (h *harness) delegatingRun(t *testing.T, policy string) delegating {
	t.Helper()
	ctx := context.Background()
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")
	h.addTool(t, repository.DiscoveredTool{ServerName: "team", ToolName: "team.delegate", SchemaJSON: `{}`, Policy: policy})
	roster, err := h.server.Roster(ctx)
	if err != nil || len(roster) != 1 {
		t.Fatalf("roster = %+v, %v", roster, err)
	}
	runID := h.enqueueWithRoster(t, roster)
	return h.claim(t, runID)
}

func (h *harness) claim(t *testing.T, runID string) delegating {
	t.Helper()
	claimed, err := h.repo.ClaimNextCompatibleJobWithLimit(context.Background(), "general_assistant", "team-worker-"+runID, 0, time.Hour,
		&repository.WorkerRoutingCapabilities{
			Models:            []repository.RoutingModelCapability{{Provider: "ollama", Model: defaultModel, MaxContextTokens: 8192}},
			MaxConcurrentRuns: 1, TeamProtocolVersion: 1,
		}, func(repository.RoutingRequirements) bool { return true })
	if err != nil || claimed.RunID != runID {
		t.Fatalf("claim = %+v, %v; want %s", claimed, err, runID)
	}
	approvals, published := &fakeApprovals{}, &fakePublisher{}
	h.server.SetApprovalEnforcer(approvals)
	h.server.SetEventPublisher(published)
	return delegating{runID: runID, attempt: claimed.AssignmentAttemptID, approvals: approvals, published: published}
}

var researchTask = map[string]any{"agent": "research", "task": "Gather the design review notes", "context": "The review is on Friday."}

func (h *harness) delegate(d delegating, toolCallID, approvalID string, args map[string]any) (*turingv1.CallTeamToolResponse, error) {
	structArgs, err := structpb.NewStruct(args)
	if err != nil {
		return nil, err
	}
	return h.server.CallTeamTool(context.Background(), &turingv1.CallTeamToolRequest{
		RunId: d.runID, AssignmentAttemptId: d.attempt, ApprovalId: approvalID,
		ToolCallId: toolCallID, ToolName: "team.delegate", Args: structArgs,
	})
}

func (h *harness) wantDelegations(t *testing.T, runID string, want int) {
	t.Helper()
	if got, err := h.repo.DelegationCount(context.Background(), runID); err != nil || got != want {
		t.Fatalf("delegations = %d, %v; want %d", got, err, want)
	}
}

// A running Turing run delegates to a specialist on its roster: the child is
// queued at once, and the parent's stream says so.
func TestCallTeamToolDelegatesToARosterSpecialist(t *testing.T) {
	h := newHarness(t)
	d := h.delegatingRun(t, "safe")
	h.routes.validated = nil

	response, err := h.delegate(d, "call_1", "", researchTask)
	if err != nil {
		t.Fatal(err)
	}
	result := response.GetResult().AsMap()
	if !strings.HasPrefix(result["delegation_id"].(string), "dlg_") || result["agent"] != "research" || result["state"] != "queued" || len(result) != 3 {
		t.Fatalf("result = %v", result)
	}
	delegation, found, err := h.repo.DelegationForToolCall(context.Background(), d.runID, "call_1")
	if err != nil || !found || delegation.ID != result["delegation_id"] {
		t.Fatalf("delegation = %+v, %v, %v", delegation, found, err)
	}
	var types []string
	for _, event := range d.published.published {
		types = append(types, event.Type)
	}
	if !slices.Equal(types, []string{"agent.run.queued", "delegation.started"}) {
		t.Fatalf("published = %v, want the child's queued event then delegation.started", types)
	}
	if len(d.approvals.calls) != 0 {
		t.Fatalf("a safe delegation consumed approvals: %+v", d.approvals.calls)
	}
	// The child is claimable at once: the runtime is told there is work.
	if h.routes.dispatched != 1 || h.routes.refreshed != 1 {
		t.Fatalf("dispatched %d, refreshed %d; want one of each", h.routes.dispatched, h.routes.refreshed)
	}
	if _, err := h.delegate(d, "call_1", "", researchTask); err != nil {
		t.Fatal(err)
	}
	if h.routes.dispatched != 1 || h.routes.refreshed != 1 {
		t.Fatalf("a replay dispatched %d, refreshed %d; want nothing more", h.routes.dispatched, h.routes.refreshed)
	}

	// The child's route is the one checked: the profile's model and tools, on
	// a team-protocol worker.
	child := h.routes.validated[len(h.routes.validated)-1]
	wantTools := []string{"files/files.read", "files/files.write", "memory/memory.read", "memory/memory.search",
		"skills/skill_view", "skills/skills_list", "system/system.time"}
	if child.AgentID != "general_assistant" || child.ModelProvider != "ollama" || child.Model != defaultModel ||
		child.MinimumTeamProtocolVersion != 1 || !slices.Equal(child.SelectedTools, wantTools) || !slices.Equal(child.RequestedTools, wantTools) {
		t.Fatalf("child route = %+v", child)
	}

	// The job runs as the profile, on its tools, with the brief as its turn.
	claimed, err := h.repo.ClaimNextCompatibleJobWithLimit(context.Background(), "general_assistant", "child-worker", 0, time.Hour,
		&repository.WorkerRoutingCapabilities{
			Models: []repository.RoutingModelCapability{{Provider: "ollama", Model: defaultModel, MaxContextTokens: 8192}},
			Tools:  wantTools, MaxConcurrentRuns: 2, TeamProtocolVersion: 1,
		}, func(repository.RoutingRequirements) bool { return true })
	if err != nil || claimed.RunID != delegation.ChildRunID {
		t.Fatalf("child claim = %+v, %v", claimed, err)
	}
	profile := claimed.AgentProfile
	if profile == nil || profile.ProfileID != "research" || profile.DisplayName != "Research" || profile.Emoji != "🔬" ||
		profile.Instructions != "You are a specialist." || profile.MaxToolCalls != 12 || profile.Revision != h.profile(t, "research").GetRevision() {
		t.Fatalf("child profile = %+v", profile)
	}
	if !slices.Equal(claimed.SelectedTools, wantTools) {
		t.Fatalf("child tools = %v, want %v", claimed.SelectedTools, wantTools)
	}
	for _, want := range []string{"BEGIN TURING_RETRIEVED_DELEGATION_BRIEF_", "Task:\nGather the design review notes", "Context:\nThe review is on Friday."} {
		if !strings.Contains(claimed.UserText, want) {
			t.Fatalf("brief %q does not contain %q", claimed.UserText, want)
		}
	}
}

// The arguments are exactly agent, task and an optional context, each within
// its bytes; anything else is refused and creates nothing.
func TestCallTeamToolChecksItsArguments(t *testing.T) {
	h := newHarness(t)
	d := h.delegatingRun(t, "safe")
	for name, args := range map[string]map[string]any{
		"an unknown argument":  {"agent": "research", "task": "x", "path": "/etc"},
		"no task":              {"agent": "research"},
		"an empty task":        {"agent": "research", "task": " "},
		"no agent":             {"task": "x"},
		"a numeric agent":      {"agent": 7.0, "task": "x"},
		"a task over 4 KiB":    {"agent": "research", "task": strings.Repeat("a", 4097)},
		"runes over 4 KiB":     {"agent": "research", "task": strings.Repeat("🔬", 1025)},
		"a context over 8 KiB": {"agent": "research", "task": "x", "context": strings.Repeat("a", 8193)},
		"a numeric context":    {"agent": "research", "task": "x", "context": 3.0},
	} {
		if _, err := h.delegate(d, "call_"+strings.ReplaceAll(name, " ", "_"), "", args); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s: error = %v, want InvalidArgument", name, err)
		}
	}
	h.wantDelegations(t, d.runID, 0)
	if _, err := h.delegate(d, "call_at_caps", "", map[string]any{
		"agent": "research", "task": strings.Repeat("a", 4096), "context": strings.Repeat("b", 8192),
	}); err != nil {
		t.Fatalf("a brief at its caps was refused: %v", err)
	}
	h.wantDelegations(t, d.runID, 1)

	for _, request := range []*turingv1.CallTeamToolRequest{
		{AssignmentAttemptId: d.attempt, ToolCallId: "c", ToolName: "team.delegate", Args: &structpb.Struct{}},
		{RunId: d.runID, ToolCallId: "c", ToolName: "team.delegate", Args: &structpb.Struct{}},
		{RunId: d.runID, AssignmentAttemptId: d.attempt, ToolName: "team.delegate", Args: &structpb.Struct{}},
		{RunId: d.runID, AssignmentAttemptId: d.attempt, ToolCallId: "c", ToolName: "team.delegate"},
	} {
		if _, err := h.server.CallTeamTool(context.Background(), request); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("request %+v error = %v, want InvalidArgument", request, err)
		}
	}
	if _, err := h.server.CallTeamTool(context.Background(), &turingv1.CallTeamToolRequest{
		RunId: d.runID, AssignmentAttemptId: d.attempt, ToolCallId: "c", ToolName: "team.other", Args: &structpb.Struct{},
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown tool error = %v, want NotFound", err)
	}
}

// Every gate refuses before anything is created or any approval is spent.
func TestCallTeamToolRefusesWhatTheRunCannotDelegate(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, h *harness, d *delegating) map[string]any
	}{
		{"a stale assignment attempt", func(_ *testing.T, _ *harness, d *delegating) map[string]any {
			d.attempt = "attempt_stale"
			return researchTask
		}},
		{"the team turned off", func(_ *testing.T, h *harness, _ *delegating) map[string]any {
			h.server.enabled = false
			return researchTask
		}},
		{"a server named team", func(t *testing.T, h *harness, _ *delegating) map[string]any {
			h.importServer(t, repository.ImportedMCPServer{
				Name: "team", URL: "http://team:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
			}, true, "lookup")
			return researchTask
		}},
		{"an agent not on the roster", func(_ *testing.T, _ *harness, _ *delegating) map[string]any {
			return map[string]any{"agent": "dev", "task": "x"}
		}},
		{"a disabled profile", func(t *testing.T, h *harness, _ *delegating) map[string]any {
			if _, err := h.repo.SetAgentProfileEnabled(context.Background(), "research", false); err != nil {
				t.Fatal(err)
			}
			return researchTask
		}},
		{"a profile regranted at another revision", func(t *testing.T, h *harness, _ *delegating) map[string]any {
			h.write(t, "research", strings.Replace(researchFrontmatter, "max_tool_calls: 12", "max_tool_calls: 8", 1))
			h.activate(t, "research")
			return researchTask
		}},
		{"a model no team-protocol worker serves", func(_ *testing.T, h *harness, _ *delegating) map[string]any {
			h.routes.models = nil
			return researchTask
		}},
		{"an unroutable child", func(_ *testing.T, h *harness, _ *delegating) map[string]any {
			h.routes.refuseTools = true
			return researchTask
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			d := h.delegatingRun(t, "approval_required")
			args := test.setup(t, h, &d)
			if _, err := h.delegate(d, "call_1", "approval_1", args); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("error = %v, want FailedPrecondition", err)
			}
			h.wantDelegations(t, d.runID, 0)
			if len(d.approvals.calls) != 0 {
				t.Fatalf("a refused call consumed an approval: %+v", d.approvals.calls)
			}
		})
	}
}

// A run that was never offered the team has no one to delegate to. The
// repository freezes no roster onto a remote, external-agent or unattended
// run, nor one whose frozen tools leave team.delegate out (its own tests pin
// that), so each of those reaches this refusal.
func TestCallTeamToolRefusesARunWithoutTheTeam(t *testing.T) {
	h := newHarness(t)
	d := h.delegatingRun(t, "safe")
	plain := h.claim(t, h.enqueueWithRoster(t, nil))
	if _, err := h.delegate(plain, "call_1", "", researchTask); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a run without a roster error = %v, want FailedPrecondition", err)
	}
	h.wantDelegations(t, plain.runID, 0)
	h.wantDelegations(t, d.runID, 0)
}

// With the team off, a call is refused as such, whatever its arguments.
func TestCallTeamToolReportsTheTeamOffFirst(t *testing.T) {
	h := newHarness(t)
	d := h.delegatingRun(t, "safe")
	h.server.enabled = false
	if _, err := h.delegate(d, "call_1", "", map[string]any{"agent": "research", "path": "/etc"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("error = %v, want FailedPrecondition for the team being off", err)
	}
}

// The cap counts this run's delegations; the next is refused before an
// approval is spent.
func TestCallTeamToolStopsAtTheCap(t *testing.T) {
	h := newHarness(t)
	h.server.SetMaxDelegationsPerRun(1)
	d := h.delegatingRun(t, "safe")
	if _, err := h.delegate(d, "call_1", "", researchTask); err != nil {
		t.Fatal(err)
	}
	if _, err := h.delegate(d, "call_2", "", researchTask); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("over the cap error = %v, want FailedPrecondition", err)
	}
	h.wantDelegations(t, d.runID, 1)
}

// A disabled or unregistered tool refuses; an approval-gated one consumes the
// approval bound to this run, tool and arguments; a safe one consumes none.
func TestCallTeamToolEnforcesItsPolicy(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		h := newHarness(t)
		d := h.delegatingRun(t, "disabled")
		if _, err := h.delegate(d, "call_1", "", researchTask); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("error = %v, want FailedPrecondition", err)
		}
		h.wantDelegations(t, d.runID, 0)
	})
	t.Run("unregistered", func(t *testing.T) {
		h := newHarness(t)
		h.write(t, "research", researchFrontmatter)
		h.activate(t, "research")
		roster, err := h.server.Roster(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		d := h.claim(t, h.enqueueWithRoster(t, roster))
		if _, err := h.delegate(d, "call_1", "", researchTask); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("error = %v, want FailedPrecondition", err)
		}
		h.wantDelegations(t, d.runID, 0)
	})
	t.Run("approval refused", func(t *testing.T) {
		h := newHarness(t)
		d := h.delegatingRun(t, "approval_required")
		d.approvals.err = status.Error(codes.PermissionDenied, "approval was denied")
		if _, err := h.delegate(d, "call_1", "approval_1", researchTask); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("error = %v, want the enforcer's PermissionDenied", err)
		}
		h.wantDelegations(t, d.runID, 0)
	})
	t.Run("no enforcer", func(t *testing.T) {
		h := newHarness(t)
		d := h.delegatingRun(t, "approval_required")
		h.server.SetApprovalEnforcer(nil)
		if _, err := h.delegate(d, "call_1", "approval_1", researchTask); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("error = %v, want FailedPrecondition", err)
		}
		h.wantDelegations(t, d.runID, 0)
	})
	t.Run("approval consumed", func(t *testing.T) {
		h := newHarness(t)
		d := h.delegatingRun(t, "approval_required")
		if _, err := h.delegate(d, "call_1", "approval_1", researchTask); err != nil {
			t.Fatal(err)
		}
		if len(d.approvals.calls) != 1 {
			t.Fatalf("approvals consumed = %+v, want one", d.approvals.calls)
		}
		call := d.approvals.calls[0]
		if call.approvalID != "approval_1" || call.runID != d.runID || call.serverName != "team" || call.serverID != "" ||
			call.toolName != "team.delegate" || call.args["task"] != researchTask["task"] {
			t.Fatalf("approval consumed for %+v", call)
		}
		h.wantDelegations(t, d.runID, 1)
	})
}

// An approval wait can outlast the policy, the profile and its grant, so
// they are all read again before anything is created.
func TestCallTeamToolRechecksAfterTheApprovalWait(t *testing.T) {
	for name, change := range map[string]func(t *testing.T, h *harness){
		"the policy changed": func(t *testing.T, h *harness) {
			if err := h.repo.SetToolPolicyByName(context.Background(), "team", "team.delegate", "safe"); err != nil {
				t.Fatal(err)
			}
		},
		"the profile was disabled": func(t *testing.T, h *harness) {
			if _, err := h.repo.SetAgentProfileEnabled(context.Background(), "research", false); err != nil {
				t.Fatal(err)
			}
		},
		"the profile was revised and regranted": func(t *testing.T, h *harness) {
			h.write(t, "research", strings.Replace(researchFrontmatter, "max_tool_calls: 12", "max_tool_calls: 8", 1))
			h.activate(t, "research")
		},
		// The grant in the database still names the roster's revision, so only
		// resolving the file again notices.
		"the profile's authority was edited": func(t *testing.T, h *harness) {
			h.write(t, "research", strings.Replace(researchFrontmatter, "max_tool_calls: 12", "max_tool_calls: 8", 1))
		},
		"the model stopped being served": func(_ *testing.T, h *harness) {
			h.routes.models = nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			d := h.delegatingRun(t, "approval_required")
			d.approvals.during = func() { change(t, h) }
			if _, err := h.delegate(d, "call_1", "approval_1", researchTask); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("error = %v, want FailedPrecondition", err)
			}
			h.wantDelegations(t, d.runID, 0)
		})
	}
}

// A retried tool call returns the delegation it created and spends no second
// approval, whatever has changed since; other arguments are refused.
func TestCallTeamToolReplaysARetriedCall(t *testing.T) {
	h := newHarness(t)
	h.server.SetMaxDelegationsPerRun(1)
	d := h.delegatingRun(t, "approval_required")
	first, err := h.delegate(d, "call_1", "approval_1", researchTask)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.SetAgentProfileEnabled(context.Background(), "research", false); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetToolPolicyByName(context.Background(), "team", "team.delegate", "disabled"); err != nil {
		t.Fatal(err)
	}
	h.server.enabled = false
	again, err := h.delegate(d, "call_1", "", researchTask)
	if err != nil {
		t.Fatalf("replay error = %v", err)
	}
	if again.GetResult().AsMap()["delegation_id"] != first.GetResult().AsMap()["delegation_id"] {
		t.Fatalf("replay = %v, want %v", again.GetResult().AsMap(), first.GetResult().AsMap())
	}
	if len(d.approvals.calls) != 1 {
		t.Fatalf("approvals consumed = %d, want 1", len(d.approvals.calls))
	}
	other := map[string]any{"agent": "research", "task": "Something else"}
	if _, err := h.delegate(d, "call_1", "approval_2", other); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("replay with other arguments error = %v, want FailedPrecondition", err)
	}
	h.wantDelegations(t, d.runID, 1)
}
