package repository

import (
	"context"
	"database/sql"
	"testing"
)

const (
	teamServerCollision = "Delegation is off: an MCP server named `team` exists. Remove it and register it under another name."
	vendorToolCollision = "Delegation is off: `vendor` provides a tool named `team.delegate`."
)

// insertUserServerWithTool stands in for an install that registered the
// server before the team name was reserved.
func insertUserServerWithTool(t *testing.T, ctx context.Context, repo *Repository, serverID, serverName, toolName string) {
	t.Helper()
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO mcp_servers (id, name, transport, url, tier, enabled, created_at)
		VALUES (?, ?, 'http', 'http://user-server:9000/mcp', 'local_container', 1, datetime('now'))`,
		serverID, serverName); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at, mcp_server_id, present)
		VALUES (?, ?, ?, 'safe', '{}', 1, '2026-10-04T00:00:00Z', ?, 1)`,
		"tool_"+serverID, serverName, toolName, serverID); err != nil {
		t.Fatal(err)
	}
}

func collisionReason(t *testing.T, ctx context.Context, repo *Repository) string {
	t.Helper()
	reason, err := repo.TeamNameCollision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return reason
}

func TestTeamNameCollisionNamesAServerCalledTeamInAnyLetterCase(t *testing.T) {
	for _, name := range []string{"team", "Team", "TEAM"} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			if got := collisionReason(t, ctx, repo); got != "" {
				t.Fatalf("fresh install collision = %q, want none", got)
			}
			insertUserServerWithTool(t, ctx, repo, "mcp_user_team", name, "lookup")
			if got := collisionReason(t, ctx, repo); got != teamServerCollision {
				t.Fatalf("collision = %q, want %q", got, teamServerCollision)
			}
			if _, err := repo.db.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id = 'mcp_user_team'`); err != nil {
				t.Fatal(err)
			}
			if got := collisionReason(t, ctx, repo); got != "" {
				t.Fatalf("collision after removal = %q, want none", got)
			}
		})
	}
}

// A present third-party tool already named team.* would sit beside the team
// definition, so the model would see two tools with one name.
func TestTeamNameCollisionNamesAThirdPartyTeamTool(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	insertUserServerWithTool(t, ctx, repo, "mcp_vendor", "vendor", "team.delegate")
	if got := collisionReason(t, ctx, repo); got != vendorToolCollision {
		t.Fatalf("collision = %q, want %q", got, vendorToolCollision)
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE tools SET tool_name = 'Team.Delegate' WHERE id = 'tool_mcp_vendor'`); err != nil {
		t.Fatal(err)
	}
	if got := collisionReason(t, ctx, repo); got == "" {
		t.Fatal("a differently cased team tool was not a collision")
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE tools SET present = 0 WHERE id = 'tool_mcp_vendor'`); err != nil {
		t.Fatal(err)
	}
	if got := collisionReason(t, ctx, repo); got != "" {
		t.Fatalf("a withdrawn tool still collides: %q", got)
	}
}

// A pseudo upsert's ON CONFLICT would set the user server's row to a NULL
// server, capturing it. While the name collides, team-named tools are not
// written at all, so the user's rows keep their server byte for byte.
func TestUpsertToolsLeavesTeamNamedServersAloneWhileTheNameCollides(t *testing.T) {
	for _, name := range []string{"team", "Team"} {
		t.Run(name, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			insertUserServerWithTool(t, ctx, repo, "mcp_user_team", name, "team.delegate")
			if err := repo.UpsertTools(ctx, []DiscoveredTool{
				{ServerName: "team", ToolName: "team.delegate", SchemaJSON: `{"type":"object"}`, Policy: "safe"},
				{ServerName: name, ToolName: "team.delegate", SchemaJSON: `{"type":"object"}`, Policy: "safe"},
				{ServerName: "memory", ToolName: "memory.search", SchemaJSON: `{}`, Policy: "safe"},
			}); err != nil {
				t.Fatal(err)
			}
			var rows int
			var serverID sql.NullString
			if err := repo.db.QueryRowContext(ctx, `
				SELECT COUNT(*), MAX(mcp_server_id) FROM tools WHERE lower(server_name) = 'team'`).Scan(&rows, &serverID); err != nil {
				t.Fatal(err)
			}
			if rows != 1 || serverID.String != "mcp_user_team" {
				t.Fatalf("team-named rows = %d on server %v, want only the user's row on mcp_user_team", rows, serverID)
			}
			if tools, err := repo.ListPseudoServerTools(ctx, "memory"); err != nil || len(tools) != 1 {
				t.Fatalf("an unrelated pseudo-tool was dropped: %v, %v", tools, err)
			}
		})
	}
}

func TestUpsertToolsRegistersTeamDelegateWithoutACollision(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		{ServerName: "team", ToolName: "team.delegate", SchemaJSON: `{"type":"object"}`, Policy: "safe"},
	}); err != nil {
		t.Fatal(err)
	}
	tools, err := repo.ListPseudoServerTools(ctx, "team")
	if err != nil || len(tools) != 1 || tools[0].Policy != "safe" {
		t.Fatalf("team pseudo-tools = %+v, %v", tools, err)
	}
}
