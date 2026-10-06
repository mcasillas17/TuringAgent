package team

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	toolpolicy "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	serverName       = "team"
	delegateToolName = "team.delegate"
	// The brief is all a specialist sees, so its two parts are bounded.
	maxTaskBytes    = 4 * 1024
	maxContextBytes = 8 * 1024
	// maxLength counts characters, so the byte budget travels in the same
	// vendor keyword memory's tools use, and each description names the unit.
	schemaMaxBytesKeyword = "x-turing-maxBytes"
)

const delegateDescription = "Delegate a task to a specialist on your team. Answer directly whenever you can; " +
	"delegate only work that belongs to a specialist. The result arrives later, " +
	"in a follow-up turn, not in this one. After calling this, tell the user briefly what you " +
	"delegated and to whom. The specialist sees only `task` and `context`, not this conversation, " +
	"so put everything it needs in them."

// ListTeamTools renders the roster frozen onto the run's job as the
// team.delegate tool. A run that was not offered the team gets nothing, and so
// does every run while the team is off, a name collides with `team`, or the
// tool's policy disables it.
func (s *Server) ListTeamTools(ctx context.Context, req *turingv1.ListTeamToolsRequest) (*turingv1.ListTeamToolsResponse, error) {
	runID, err := requireField("run_id", req.GetRunId())
	if err != nil {
		return nil, err
	}
	roster, err := s.repo.RunTeamRoster(ctx, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "run not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "list team tools failed")
	}
	response := &turingv1.ListTeamToolsResponse{}
	if !s.enabled || len(roster) == 0 {
		return response, nil
	}
	// A user's server or tool named `team` would otherwise be shadowed by, or
	// captured beside, this one: the collision turns delegation off.
	collision, err := s.repo.TeamNameCollision(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list team tools failed")
	}
	if collision != "" {
		return response, nil
	}
	policy, found, err := s.repo.PseudoServerToolPolicy(ctx, serverName, delegateToolName)
	if err != nil {
		return nil, status.Error(codes.Internal, "list team tools failed")
	}
	if !found {
		policy = string(toolpolicy.DefaultPolicyFor(serverName, delegateToolName))
	}
	toolPolicy := toolpolicy.ProtoFor(policy)
	if toolPolicy == turingv1.ToolPolicy_TOOL_POLICY_DISABLED {
		return response, nil
	}
	schema, err := structpb.NewStruct(delegateSchema(roster))
	if err != nil {
		return nil, status.Error(codes.Internal, "build team tool schema failed")
	}
	response.Tools = append(response.Tools, &turingv1.TeamToolDescriptor{
		ToolName: delegateToolName, Policy: toolPolicy, Schema: schema, Enabled: true, Description: delegateDescription,
	})
	return response, nil
}

// delegateSchema is team.delegate's argument schema for one roster. Turing
// learns who is on the team from the agent enum and its description, with no
// prompt channel of its own.
func delegateSchema(roster []repository.TeamRosterEntry) map[string]any {
	ids := make([]any, 0, len(roster))
	var team strings.Builder
	team.WriteString("The specialist to delegate to:")
	for _, entry := range roster {
		ids = append(ids, entry.ProfileID)
		fmt.Fprintf(&team, "\n- %s (%s %s): %s", entry.ProfileID, entry.Emoji, entry.Name, entry.Description)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent": map[string]any{"type": "string", "enum": ids, "description": team.String()},
			"task": map[string]any{
				"type": "string", "maxLength": maxTaskBytes, schemaMaxBytesKeyword: maxTaskBytes,
				"description": fmt.Sprintf("What the specialist should do, self-contained. At most %d bytes of UTF-8.", maxTaskBytes),
			},
			"context": map[string]any{
				"type": "string", "maxLength": maxContextBytes, schemaMaxBytesKeyword: maxContextBytes,
				"description": fmt.Sprintf("Optional background the specialist needs from this conversation. At most %d bytes of UTF-8.", maxContextBytes),
			},
		},
		"required":             []any{"agent", "task"},
		"additionalProperties": false,
	}
}
