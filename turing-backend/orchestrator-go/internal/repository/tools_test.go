package repository

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
)

func TestUpsertToolsStoresSnapshotAndPreservesExistingPolicy(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	err := repo.UpsertTools(ctx, []DiscoveredTool{
		{ServerName: "system", ToolName: "system.time", SchemaJSON: `{"type":"object"}`, Policy: "safe"},
		{ServerName: "files", ToolName: "files.create", SchemaJSON: `{"type":"object"}`, Policy: "approval_required"},
	})
	if err != nil {
		t.Fatalf("UpsertTools initial snapshot: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE tools SET policy = 'disabled' WHERE server_name = 'files' AND tool_name = 'files.create'`); err != nil {
		t.Fatalf("set operator policy: %v", err)
	}

	err = repo.UpsertTools(ctx, []DiscoveredTool{
		{ServerName: "files", ToolName: "files.create", SchemaJSON: `{"type":"object","required":["path"]}`, Policy: "approval_required"},
	})
	if err != nil {
		t.Fatalf("UpsertTools refreshed snapshot: %v", err)
	}

	tools, err := repo.ListEnabledTools(ctx)
	if err != nil {
		t.Fatalf("ListEnabledTools: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("enabled tools = %+v, want disabled policy to stay unavailable", tools)
	}
	if policy, enabled, found, err := repo.GetToolPolicy(ctx, "system", "system.time"); err != nil || !found || enabled || policy != "safe" {
		t.Fatalf("omitted tool state: policy=%q enabled=%v found=%v err=%v", policy, enabled, found, err)
	}
	if policy, enabled, found, err := repo.GetToolPolicy(ctx, "files", "files.create"); err != nil || !found || enabled || policy != "disabled" {
		t.Fatalf("files.create state: policy=%q enabled=%v found=%v err=%v", policy, enabled, found, err)
	}
}

func TestGetToolPolicyScopesLookupToServer(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	registerRepositoryTestServer(t, ctx, database, "trusted")
	registerRepositoryTestServer(t, ctx, database, "untrusted")
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		{ServerName: "trusted", ToolName: "shared.name", SchemaJSON: `{}`, Policy: "safe"},
		{ServerName: "untrusted", ToolName: "shared.name", SchemaJSON: `{}`, Policy: "disabled"},
	}); err != nil {
		t.Fatalf("UpsertTools: %v", err)
	}

	policy, enabled, found, err := repo.GetToolPolicy(ctx, "untrusted", "shared.name")
	if err != nil || !found || enabled || policy != "disabled" {
		t.Fatalf("untrusted state: policy=%q enabled=%v found=%v err=%v", policy, enabled, found, err)
	}
	if _, _, found, err := repo.GetToolPolicy(ctx, "missing", "shared.name"); err != nil || found {
		t.Fatalf("missing server policy resolved: found=%v err=%v", found, err)
	}
}

func TestUpsertToolsMarksRegistryInitializedForEmptySnapshot(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	initialized, err := repo.ToolRegistryInitialized(ctx)
	if err != nil || initialized {
		t.Fatalf("initial registry state = %v err=%v, want false", initialized, err)
	}
	if err := repo.UpsertTools(ctx, nil); err != nil {
		t.Fatalf("UpsertTools empty snapshot: %v", err)
	}
	initialized, err = repo.ToolRegistryInitialized(ctx)
	if err != nil || !initialized {
		t.Fatalf("registry state after empty snapshot = %v err=%v, want true", initialized, err)
	}
}

func TestUpsertToolsUpdatesBundledServerLiveness(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	if err := repo.UpsertTools(ctx, []DiscoveredTool{{
		ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe",
	}}); err != nil {
		t.Fatal(err)
	}
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, server := range servers {
		statuses[server.Name] = server.Status
	}
	if statuses["system"] != "up" || statuses["files"] != "down" {
		t.Fatalf("bundled liveness = %v, want system up and files down", statuses)
	}
}

func TestUpsertToolsRollsBackSnapshotOnInvalidTool(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	registerRepositoryTestServer(t, ctx, database, "broken")
	if err := repo.UpsertTools(ctx, []DiscoveredTool{{ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe"}}); err != nil {
		t.Fatalf("UpsertTools initial snapshot: %v", err)
	}
	if err := repo.UpsertTools(ctx, []DiscoveredTool{{ServerName: "broken", ToolName: "broken.tool", SchemaJSON: `{}`, Policy: "not-a-policy"}}); err == nil {
		t.Fatal("UpsertTools accepted an invalid policy")
	}

	policy, enabled, found, err := repo.GetToolPolicy(ctx, "system", "system.time")
	if err != nil || !found || !enabled || policy != "safe" {
		t.Fatalf("previous snapshot was not restored: policy=%q enabled=%v found=%v err=%v", policy, enabled, found, err)
	}
}

func TestUpsertToolsDropsAnUnregisteredServerFromTheSnapshot(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	if err := repo.UpsertTools(ctx, []DiscoveredTool{{
		ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe",
	}}); err != nil {
		t.Fatal(err)
	}

	err := repo.UpsertTools(ctx, []DiscoveredTool{{
		ServerName: "stranger", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, found, lookupErr := repo.GetToolPolicy(ctx, "stranger", "system.time"); lookupErr != nil || found {
		t.Fatalf("unregistered server entered registry: found=%v err=%v", found, lookupErr)
	}
}

func TestPseudoServerPolicyAvailabilityBootstrapsAndReEnables(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	for _, serverName := range []string{"skills", "integrations"} {
		t.Run(serverName, func(t *testing.T) {
			toolName := serverName + ".probe"
			available, err := repo.PseudoServerToolAvailable(ctx, serverName, toolName)
			if err != nil || !available {
				t.Fatalf("missing tool availability = %v, err=%v; want bootstrap availability", available, err)
			}
			if err := repo.UpsertTools(ctx, []DiscoveredTool{{ServerName: serverName, ToolName: toolName, SchemaJSON: `{}`, Policy: "approval_required"}}); err != nil {
				t.Fatal(err)
			}
			if err := repo.SetToolPolicyByName(ctx, serverName, toolName, "disabled"); err != nil {
				t.Fatal(err)
			}
			available, err = repo.PseudoServerToolAvailable(ctx, serverName, toolName)
			if err != nil || available {
				t.Fatalf("disabled tool availability = %v, err=%v; want false", available, err)
			}
			if err := repo.SetToolPolicyByName(ctx, serverName, toolName, "approval_required"); err != nil {
				t.Fatal(err)
			}
			available, err = repo.PseudoServerToolAvailable(ctx, serverName, toolName)
			if err != nil || !available {
				t.Fatalf("re-enabled tool availability = %v, err=%v; want true", available, err)
			}
		})
	}
}

func TestUpsertToolsRegistersIntegrationsWithoutAnMCPServerRow(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	if err := repo.UpsertTools(ctx, []DiscoveredTool{{
		ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	policy, enabled, found, err := repo.GetToolPolicy(ctx, "integrations", "github.list_issues")
	if err != nil || !found || !enabled || policy != "approval_required" {
		t.Fatalf("integration tool = policy %q enabled %v found %v err %v", policy, enabled, found, err)
	}
}

func registerRepositoryTestServer(t *testing.T, ctx context.Context, database *db.DB, name string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO mcp_servers (id, name, transport, url, tier, enabled, created_at)
		VALUES (?, ?, 'http', 'http://vendor:9000/mcp', 'local_container', 1, datetime('now'))
	`, "mcp_"+name, name); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO mcp_server_status (mcp_server_id, status)
		VALUES (?, 'unknown')
	`, "mcp_"+name); err != nil {
		t.Fatal(err)
	}
}
