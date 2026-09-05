package integrations

import (
	"context"
	"fmt"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	toolpolicy "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type integrationTool struct {
	name        string
	description string
	readOnly    bool
	schema      map[string]any
}

var githubTools = []integrationTool{
	{name: "github.list_issues", description: "List issues in a GitHub repository", readOnly: true, schema: objectSchema([]string{"connection_id", "owner", "repo"}, map[string]any{
		"connection_id": stringSchema("Connected GitHub account id"), "owner": stringSchema("Repository owner"), "repo": stringSchema("Repository name"),
		"state": map[string]any{"type": "string", "enum": []any{"open", "closed", "all"}}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
	})},
	{name: "github.get_issue", description: "Get one GitHub issue", readOnly: true, schema: objectSchema([]string{"connection_id", "owner", "repo", "issue_number"}, map[string]any{
		"connection_id": stringSchema("Connected GitHub account id"), "owner": stringSchema("Repository owner"), "repo": stringSchema("Repository name"), "issue_number": map[string]any{"type": "integer", "minimum": 1},
	})},
	{name: "github.get_file", description: "Get a file from a GitHub repository", readOnly: true, schema: objectSchema([]string{"connection_id", "owner", "repo", "path"}, map[string]any{
		"connection_id": stringSchema("Connected GitHub account id"), "owner": stringSchema("Repository owner"), "repo": stringSchema("Repository name"), "path": stringSchema("Repository-relative path"), "ref": stringSchema("Optional branch, tag, or commit"),
	})},
	{name: "github.create_comment", description: "Create a comment on a GitHub issue", schema: objectSchema([]string{"connection_id", "owner", "repo", "issue_number", "body"}, map[string]any{
		"connection_id": stringSchema("Connected GitHub account id"), "owner": stringSchema("Repository owner"), "repo": stringSchema("Repository name"), "issue_number": map[string]any{"type": "integer", "minimum": 1}, "body": stringSchema("Complete comment body"),
	})},
}

func objectSchema(required []string, properties map[string]any) map[string]any {
	requiredValues := make([]any, len(required))
	for index, value := range required {
		requiredValues[index] = value
	}
	return map[string]any{"type": "object", "additionalProperties": false, "required": requiredValues, "properties": properties}
}
func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func lookupIntegrationTool(name string) (integrationTool, bool) {
	for _, tool := range githubTools {
		if tool.name == name {
			return tool, true
		}
	}
	return integrationTool{}, false
}

func (s *Server) ListIntegrationTools(ctx context.Context, _ *turingv1.ListIntegrationToolsRequest) (*turingv1.ListIntegrationToolsResponse, error) {
	// Keyless installs are the modal case. Discovery must remain available.
	if s.sealer == nil {
		return &turingv1.ListIntegrationToolsResponse{Tools: []*turingv1.IntegrationToolDescriptor{}}, nil
	}
	connections, err := s.repo.ListConnections(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list integration tools failed")
	}
	live := make([]repository.Connection, 0)
	for _, connection := range connections {
		if connection.Provider == "github" && connection.Status == repository.ConnectionStatusConnected && s.sealer.SealedWithThisKey(connection.CredentialHeader) {
			live = append(live, connection)
		}
	}
	if len(live) == 0 {
		return &turingv1.ListIntegrationToolsResponse{Tools: []*turingv1.IntegrationToolDescriptor{}}, nil
	}
	pairs := make([]string, len(live))
	for index, connection := range live {
		pairs[index] = fmt.Sprintf("(%s, %s)", connection.ConnectionID, connection.DisplayName)
	}
	response := &turingv1.ListIntegrationToolsResponse{Tools: make([]*turingv1.IntegrationToolDescriptor, 0, len(githubTools))}
	for _, tool := range githubTools {
		available, err := s.repo.PseudoServerToolAvailable(ctx, "integrations", tool.name)
		if err != nil {
			return nil, status.Error(codes.Internal, "list integration tools failed")
		}
		if !available {
			continue
		}
		schema, err := structpb.NewStruct(tool.schema)
		if err != nil {
			return nil, status.Error(codes.Internal, "build integration tool schema failed")
		}
		policy, found, err := s.repo.PseudoServerToolPolicy(ctx, "integrations", tool.name)
		if err != nil {
			return nil, status.Error(codes.Internal, "list integration tools failed")
		}
		if !found {
			policy = "approval_required"
		}
		response.Tools = append(response.Tools, &turingv1.IntegrationToolDescriptor{
			ToolName: tool.name, Description: tool.description + ". Available connections: " + strings.Join(pairs, ", "), Schema: schema,
			ReadOnly: toolpolicy.ToolReadOnly("integrations", tool.name), Policy: integrationPolicyProto(policy),
		})
	}
	return response, nil
}

func integrationPolicyProto(policy string) turingv1.ToolPolicy {
	switch policy {
	case "safe":
		return turingv1.ToolPolicy_TOOL_POLICY_SAFE
	case "disabled":
		return turingv1.ToolPolicy_TOOL_POLICY_DISABLED
	default:
		return turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED
	}
}
