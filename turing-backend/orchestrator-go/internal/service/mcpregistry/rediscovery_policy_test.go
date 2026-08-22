package mcpregistry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestRediscoveryPreservesDisabledToolPolicy(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(context.Background(), server.ID, true); err != nil {
		t.Fatal(err)
	}
	discovered := []DiscoveredTool{{
		Name: "vendor.lookup", SchemaJSON: `{"type":"object"}`,
	}}
	if err := service.RecordDiscovery(context.Background(), server.ID, discovered); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.ID, ToolName: "vendor.lookup",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordDiscovery(context.Background(), server.ID, discovered); err != nil {
		t.Fatal(err)
	}
	tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Policy != "disabled" || tools[0].Enabled {
		t.Fatalf("rediscovered disabled tool = %+v", tools)
	}
}

// TestImportedRemoteServerToolsSnapshotReplacedByLiveDiscoveryOnEnable
// covers the discriminating case a plain RecordDiscovery-only test cannot:
// a remote server *imported* from mcp.json with a "tools" snapshot (created
// disabled via ImportMCPServer, with its snapshot recorded via
// RecordDiscovery — exactly what ImportJSON itself does for a "tools"
// array — never through an RPC, matching how an import actually lands the
// row), whose operator edits a tool's policy before ever enabling it.
// Enabling for the first time must run live discovery that replaces the
// snapshot's presence and schema with what the vendor actually reports,
// while still preserving the edited policy for a tool that survives, and
// dropping (marking not-present, not deleting) a tool the vendor no longer
// reports.
func TestImportedRemoteServerToolsSnapshotReplacedByLiveDiscoveryOnEnable(t *testing.T) {
	service, repo := newRegistryTestService(t)
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": map[string]any{"tools": []any{
				map[string]any{
					"name":        "vendor.lookup",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"live": map[string]any{}}},
				},
				map[string]any{
					"name":        "vendor.new_tool",
					"inputSchema": map[string]any{"type": "object"},
				},
			}},
		})
	}))
	t.Cleanup(vendor.Close)
	service.httpClient = vendor.Client()

	// Registered through the repository directly (like the enable-audit
	// tests), because the httptest server's plain-HTTP loopback URL would
	// fail the RPC-level remote/HTTPS URL classification; that
	// classification is unrelated to what this test is proving. This is
	// the same repository call ImportJSON itself makes for a new name.
	result, err := repo.ImportMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: vendor.URL, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := result.Server
	// The imported snapshot: one tool that will still be reported live
	// (with a stale schema, to prove it gets replaced) and one that the
	// vendor will stop reporting once enabled.
	if err := service.RecordDiscovery(context.Background(), server.ID, []DiscoveredTool{
		{Name: "vendor.lookup", SchemaJSON: `{"type":"object","properties":{"stale":{}}}`},
		{Name: "vendor.removed", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}
	// The operator edits the imported tool's policy before ever enabling.
	if _, err := service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.ID, ToolName: "vendor.lookup",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	}); err != nil {
		t.Fatal(err)
	}

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UP {
		t.Fatalf("liveness = %v, want up", descriptor.GetLiveness())
	}

	tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]repository.MCPServerTool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	lookup, ok := byName["vendor.lookup"]
	if !ok {
		t.Fatal("vendor.lookup missing after live discovery")
	}
	if lookup.Policy != "safe" {
		t.Fatalf("vendor.lookup policy = %q, want the edited safe policy preserved", lookup.Policy)
	}
	if !lookup.Present {
		t.Fatal("vendor.lookup must be present: live discovery re-confirmed it")
	}
	if strings.Contains(lookup.SchemaJSON, "stale") || !strings.Contains(lookup.SchemaJSON, "live") {
		t.Fatalf("vendor.lookup schema = %q, want it replaced by the live schema", lookup.SchemaJSON)
	}

	newTool, ok := byName["vendor.new_tool"]
	if !ok {
		t.Fatal("vendor.new_tool missing after live discovery")
	}
	if newTool.Policy != "approval_required" {
		t.Fatalf("vendor.new_tool policy = %q, want the default policy for a newly discovered tool", newTool.Policy)
	}

	removed, ok := byName["vendor.removed"]
	if !ok {
		t.Fatal("vendor.removed row must still exist (dropped, not deleted)")
	}
	if removed.Present {
		t.Fatal("vendor.removed must no longer be present: live discovery stopped reporting it")
	}
}

// TestImportedRemoteServerDiscoveryFailurePreservesSnapshotAndPolicy is the
// failure-side counterpart: the same imported-with-a-snapshot server, but
// enabling it fails discovery entirely. The prior snapshot and its edited
// policy must be left exactly as they were — RecordDiscovery is never
// called on a failed discover() — while the server itself is still marked
// enabled and down.
func TestImportedRemoteServerDiscoveryFailurePreservesSnapshotAndPolicy(t *testing.T) {
	service, repo := newRegistryTestService(t)
	vendor := failingVendor(t, "tools/list unavailable")
	service.httpClient = vendor.Client()

	result, err := repo.ImportMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: vendor.URL, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := result.Server
	const snapshotSchema = `{"type":"object","properties":{"snapshot":{}}}`
	if err := service.RecordDiscovery(context.Background(), server.ID, []DiscoveredTool{
		{Name: "vendor.lookup", SchemaJSON: snapshotSchema},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.ID, ToolName: "vendor.lookup",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	}); err != nil {
		t.Fatal(err)
	}

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !descriptor.GetEnabled() {
		t.Fatal("a remote server that fails discovery must remain enabled")
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_DOWN {
		t.Fatalf("liveness = %v, want down", descriptor.GetLiveness())
	}

	tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %+v, want exactly the prior snapshot preserved", tools)
	}
	tool := tools[0]
	if tool.Name != "vendor.lookup" || tool.Policy != "safe" || tool.SchemaJSON != snapshotSchema || !tool.Present {
		t.Fatalf("tool = %+v, want the prior snapshot and edited policy fully preserved", tool)
	}
}
