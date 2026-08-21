package repository

import (
	"context"
	"testing"
)

func TestMCPDispatchActiveRequiresUnchangedPolicy(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	server, err := repo.UpsertImportedMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(ctx, server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, server.ID, []MCPServerTool{{
		Name: "vendor.lookup", Policy: "safe", SchemaJSON: `{"type":"object"}`,
	}}); err != nil {
		t.Fatal(err)
	}
	session, err := repo.CreateSession(ctx, "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "lookup", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE agent_runs SET execution_active = 1 WHERE id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPToolPolicy(ctx, server.ID, "vendor.lookup", "approval_required"); err != nil {
		t.Fatal(err)
	}
	active, err := repo.MCPDispatchActive(ctx, server.ID, enqueued.RunID, "vendor.lookup", "safe")
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("dispatch remained active after policy changed from safe")
	}
}
