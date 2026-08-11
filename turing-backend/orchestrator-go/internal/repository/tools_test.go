package repository

import (
	"context"
	"testing"
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
	if len(tools) != 1 {
		t.Fatalf("enabled tools = %+v, want only refreshed tool", tools)
	}
	if got := tools[0]; got.ServerName != "files" || got.ToolName != "files.create" || got.Policy != "disabled" || got.SchemaJSON != `{"type":"object","required":["path"]}` {
		t.Fatalf("refreshed tool = %+v", got)
	}
	if _, ok, err := repo.GetToolPolicy(ctx, "system", "system.time"); err != nil || ok {
		t.Fatalf("omitted tool policy resolved: ok=%v err=%v", ok, err)
	}
	if policy, ok, err := repo.GetToolPolicy(ctx, "files", "files.create"); err != nil || !ok || policy != "disabled" {
		t.Fatalf("files.create policy = %q ok=%v err=%v", policy, ok, err)
	}
}

func TestGetToolPolicyScopesLookupToServer(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		{ServerName: "trusted", ToolName: "shared.name", SchemaJSON: `{}`, Policy: "safe"},
		{ServerName: "untrusted", ToolName: "shared.name", SchemaJSON: `{}`, Policy: "disabled"},
	}); err != nil {
		t.Fatalf("UpsertTools: %v", err)
	}

	policy, ok, err := repo.GetToolPolicy(ctx, "untrusted", "shared.name")
	if err != nil || !ok || policy != "disabled" {
		t.Fatalf("untrusted policy = %q ok=%v err=%v", policy, ok, err)
	}
	if _, ok, err := repo.GetToolPolicy(ctx, "missing", "shared.name"); err != nil || ok {
		t.Fatalf("missing server policy resolved: ok=%v err=%v", ok, err)
	}
}

func TestUpsertToolsRollsBackSnapshotOnInvalidTool(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	if err := repo.UpsertTools(ctx, []DiscoveredTool{{ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe"}}); err != nil {
		t.Fatalf("UpsertTools initial snapshot: %v", err)
	}
	if err := repo.UpsertTools(ctx, []DiscoveredTool{{ServerName: "broken", ToolName: "broken.tool", SchemaJSON: `{}`, Policy: "not-a-policy"}}); err == nil {
		t.Fatal("UpsertTools accepted an invalid policy")
	}

	policy, ok, err := repo.GetToolPolicy(ctx, "system", "system.time")
	if err != nil || !ok || policy != "safe" {
		t.Fatalf("previous snapshot was not restored: policy=%q ok=%v err=%v", policy, ok, err)
	}
}
