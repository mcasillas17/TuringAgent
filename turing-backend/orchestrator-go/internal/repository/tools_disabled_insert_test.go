package repository

import (
	"context"
	"testing"
)

func TestUpsertToolsDoesNotEnableNewDisabledPolicy(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	registerRepositoryTestServer(t, ctx, database, "vendor")
	if err := repo.UpsertTools(ctx, []DiscoveredTool{{
		ServerName: "vendor", ToolName: "vendor.disabled",
		SchemaJSON: `{"type":"object"}`, Policy: "disabled",
	}}); err != nil {
		t.Fatal(err)
	}
	policy, enabled, found, err := repo.GetToolPolicy(ctx, "vendor", "vendor.disabled")
	if err != nil || !found || enabled || policy != "disabled" {
		t.Fatalf("disabled insert = policy:%q enabled:%v found:%v err:%v", policy, enabled, found, err)
	}
}
