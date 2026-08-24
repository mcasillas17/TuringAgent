package mcpregistry

import (
	"context"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListPseudoServerTools' final step — mapping each stored tool row through
// toolDescriptor — can fail on a poisoned stored schema_json (e.g.
// corrupted by something other than this package, or left over from a bug
// elsewhere): that failure must never reach the caller as the raw
// json.Unmarshal error, the same way every other toolDescriptor call site
// in this file (SetMcpServerEnabled, UpdateMcpToolPolicy,
// UpdateToolPolicyByName, GetMcpServer/ListMcpServers) already maps it to
// a fixed, generic Internal status rather than returning it as-is (which
// would surface as the unhelpful default codes.Unknown and could repeat
// part of the stored value).
func TestListPseudoServerToolsMapsPoisonedSchemaDescriptorFailureToFixedInternalStatus(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	const poisonSentinel = "poison-schema-sentinel-9f3c2e11-must-not-leak"
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{
		{
			ServerName: "skills",
			ToolName:   "skills.broken",
			Policy:     "approval_required",
			SchemaJSON: `{"broken": ` + poisonSentinel, // deliberately invalid JSON
		},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := service.ListPseudoServerTools(ctx, &turingv1.ListPseudoServerToolsRequest{ServerName: "skills"})
	if err == nil {
		t.Fatal("want an error from the poisoned tool schema breaking descriptor construction")
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
	if err.Error() != status.Error(codes.Internal, "list pseudo-server tools failed").Error() {
		t.Fatalf("err = %q, want the fixed \"list pseudo-server tools failed\" Internal status", err.Error())
	}
	if strings.Contains(err.Error(), poisonSentinel) {
		t.Fatalf("err = %q, must not leak the poisoned schema content", err.Error())
	}
	if strings.Contains(err.Error(), "broken") || strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("err = %q, must not leak the raw json.Unmarshal error text or stored content", err.Error())
	}
}

// A well-formed schema alongside the poisoned one must still list
// successfully once the poisoned row is removed — proving the fixed
// Internal status above is specific to the poisoned schema, not a general
// regression in ListPseudoServerTools.
func TestListPseudoServerToolsSucceedsWithWellFormedSchema(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{
		{ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{"type":"object"}`, Policy: "approval_required"},
	}); err != nil {
		t.Fatal(err)
	}

	response, err := service.ListPseudoServerTools(ctx, &turingv1.ListPseudoServerToolsRequest{ServerName: "integrations"})
	if err != nil {
		t.Fatalf("well-formed schema should not fail: %v", err)
	}
	if len(response.GetTools()) != 1 || response.GetTools()[0].GetToolName() != "github.list_issues" {
		t.Fatalf("Tools = %+v, want exactly the one well-formed tool", response.GetTools())
	}
}
