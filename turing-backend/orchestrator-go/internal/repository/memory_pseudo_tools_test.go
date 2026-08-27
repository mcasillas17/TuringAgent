package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// TestUpsertToolsAcceptsMemoryPseudoServerRows pins the schema half of the
// memory pseudo-server: memory tools have no mcp_servers row to point at, so
// the trigger that demands one has to know the name, and UpsertTools has to
// take the pseudo branch that leaves mcp_server_id NULL.
func TestUpsertToolsAcceptsMemoryPseudoServerRows(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		{ServerName: "memory", ToolName: "memory.search", SchemaJSON: `{"type":"object"}`, Policy: "safe"},
		{ServerName: "memory", ToolName: "memory.read", SchemaJSON: `{"type":"object"}`, Policy: "safe"},
		{ServerName: "memory", ToolName: "memory.remember", SchemaJSON: `{"type":"object"}`, Policy: "approval_required"},
	}); err != nil {
		t.Fatalf("UpsertTools: %v", err)
	}

	for tool, want := range map[string]string{
		"memory.search":   "safe",
		"memory.read":     "safe",
		"memory.remember": "approval_required",
	} {
		var policy string
		var serverID sql.NullString
		var enabled, present int
		err := database.QueryRowContext(ctx, `
			SELECT policy, mcp_server_id, enabled, present FROM tools
			WHERE server_name = 'memory' AND tool_name = ?
		`, tool).Scan(&policy, &serverID, &enabled, &present)
		if err != nil {
			t.Fatalf("read %s: %v", tool, err)
		}
		if policy != want {
			t.Fatalf("%s policy = %q, want %q", tool, policy, want)
		}
		if serverID.Valid {
			t.Fatalf("%s bound itself to mcp server %q; a pseudo-server has no row", tool, serverID.String)
		}
		if enabled != 1 || present != 1 {
			t.Fatalf("%s enabled=%d present=%d, want both 1", tool, enabled, present)
		}
	}
}

// TestMemoryPseudoServerToolAvailabilityFollowsPolicy is the predicate the
// runtime's capability filter consults. A disabled row is the one thing that
// takes a memory tool off the worker's list.
func TestMemoryPseudoServerToolAvailabilityFollowsPolicy(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	available, err := repo.PseudoServerToolAvailable(ctx, "memory", "memory.search")
	if err != nil || !available {
		t.Fatalf("available=%v err=%v, want an unseen memory tool to bootstrap", available, err)
	}
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		{ServerName: "memory", ToolName: "memory.search", SchemaJSON: `{"type":"object"}`, Policy: "safe"},
	}); err != nil {
		t.Fatalf("UpsertTools: %v", err)
	}
	if err := repo.SetToolPolicyByName(ctx, "memory", "memory.search", "disabled"); err != nil {
		t.Fatalf("SetToolPolicyByName: %v", err)
	}
	available, err = repo.PseudoServerToolAvailable(ctx, "memory", "memory.search")
	if err != nil || available {
		t.Fatalf("available=%v err=%v, want a disabled memory tool to be unavailable", available, err)
	}
	policy, found, err := repo.PseudoServerToolPolicy(ctx, "memory", "memory.search")
	if err != nil || !found || policy != "disabled" {
		t.Fatalf("policy=%q found=%v err=%v, want the stored disabled policy", policy, found, err)
	}
}

// TestAutomationsRefuseMemoryToolsAtSaveTime is the first of the two automation
// layers: an unattended run must not be able to be granted a memory tool in the
// first place, so the allowlist refuses one at save time with its own sentinel.
func TestAutomationsRefuseMemoryToolsAtSaveTime(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	for _, tool := range []string{"memory.search", "memory.read", "memory.remember"} {
		_, err := repo.CreateAutomation(ctx, AutomationInput{
			Name:         "nightly " + tool,
			Prompt:       "summarise",
			Schedule:     Schedule{Kind: ScheduleInterval, Interval: time.Hour},
			AllowedTools: []AutomationTool{{ServerName: "memory", ToolName: tool}},
		})
		if !errors.Is(err, ErrAutomationMemoryToolUnsupported) {
			t.Fatalf("CreateAutomation with %s error = %v, want ErrAutomationMemoryToolUnsupported", tool, err)
		}
	}
}
