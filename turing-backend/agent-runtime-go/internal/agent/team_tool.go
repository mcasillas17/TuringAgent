package agent

import (
	"context"
	"errors"
	"log"
	"slices"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	teamServerName   = "team"
	teamDelegateName = "team.delegate"
	teamDelegateTool = teamServerName + "/" + teamDelegateName
)

// withoutTeamTool sets team/team.delegate aside from a frozen set. The team
// tool is per run and never in the registry, so resolving the set with it
// would refuse every run that may delegate; the job's own set is not changed,
// so consent, dispatch enforcement and the beacon still see all of it.
func withoutTeamTool(selected []string) []string {
	return slices.DeleteFunc(slices.Clone(selected), func(name string) bool { return name == teamDelegateTool })
}

// teamDefinition is the team tool this run was given when it was enqueued, if
// any. A frozen set that does not name team/team.delegate, which every child
// and continuation has, offers none. It is asked for per run and never cached,
// because it belongs to one run. A listing that fails offers none: the team is
// optional, and the run goes on with its other tools.
func (a *GeneralAssistant) teamDefinition(ctx context.Context, job *turingv1.AgentJob, registry *ToolRegistry) (llm.ToolDefinition, bool) {
	if a.tools == nil || a.tools.Team == nil {
		return llm.ToolDefinition{}, false
	}
	if (job.GetEgressDecision() != nil || job.GetEnforceSelectedTools()) && !slices.Contains(job.GetSelectedTools(), teamDelegateTool) {
		return llm.ToolDefinition{}, false
	}
	// Two tools with one name would leave the model, and the dispatcher, to
	// guess which one it meant.
	if _, exists := registry.Lookup(teamDelegateName); exists {
		log.Printf("run %s: a registered tool is named %s, so the team is not offered", job.GetRunId(), teamDelegateName)
		return llm.ToolDefinition{}, false
	}
	// Bounded like any other tool discovery, so a hung orchestrator costs the
	// run its team rather than the worker's slot.
	listCtx, cancel := boundedContext(ctx, a.toolTimeout())
	response, err := a.tools.Team.ListTeamTools(listCtx, job.GetRunId())
	cancel()
	if err != nil {
		log.Printf("run %s: team tools unavailable: %v", job.GetRunId(), err)
		return llm.ToolDefinition{}, false
	}
	for _, descriptor := range response.GetTools() {
		if descriptor.GetToolName() != teamDelegateName || !descriptor.GetEnabled() ||
			descriptor.GetPolicy() == turingv1.ToolPolicy_TOOL_POLICY_DISABLED {
			continue
		}
		listing := map[string]any{"name": teamDelegateName, "description": descriptor.GetDescription()}
		if descriptor.GetSchema() != nil {
			listing["inputSchema"] = descriptor.GetSchema().AsMap()
		}
		// The registry's own checks, so the team's schema reaches the model
		// in the same shape as every other tool's.
		team, err := BuildToolRegistry(ctx, map[string]ToolLister{teamServerName: teamListing{listing}})
		if err != nil {
			log.Printf("run %s: team tool is malformed: %v", job.GetRunId(), err)
			return llm.ToolDefinition{}, false
		}
		if definitions := team.Definitions(); len(definitions) == 1 {
			return definitions[0], true
		}
		return llm.ToolDefinition{}, false
	}
	return llm.ToolDefinition{}, false
}

// teamListing presents one run's team tool to BuildToolRegistry. It never
// runs a call: a team.delegate call is dispatched through mcp.TeamCallClient.
type teamListing []map[string]any

func (l teamListing) ListTools(context.Context) ([]map[string]any, error) { return l, nil }

func (teamListing) CallTool(context.Context, string, map[string]any, ...string) (map[string]any, error) {
	return nil, errors.New("team tools require orchestrator caller-side enforcement")
}

// advertisedTeamTool is the name a worker that can delegate advertises, so the
// orchestrator accepts a team.delegate beacon from it and the name can enter a
// frozen set. Its schema is generic: the per-run one comes from ListTeamTools.
func advertisedTeamTool() (*turingv1.DiscoveredTool, error) {
	schema, err := structpb.NewStruct(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent":   map[string]any{"type": "string"},
			"task":    map[string]any{"type": "string"},
			"context": map[string]any{"type": "string"},
		},
		"required": []any{"agent", "task"},
	})
	if err != nil {
		return nil, err
	}
	return &turingv1.DiscoveredTool{ServerName: teamServerName, ToolName: teamDelegateName, Schema: schema}, nil
}
