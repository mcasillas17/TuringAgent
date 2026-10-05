package db

import (
	"context"
	"testing"
)

// The NULL-server whitelist is a full restatement: adding "team" must keep
// every earlier carve-out and still refuse an unregistered name.
func TestDelegationGroundworkMigrationReservesTeamPseudoServerWithoutLosingCarveOuts(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)

	for _, pseudoServer := range []string{"skills", "integrations", "memory", "team"} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at)
			VALUES (?, ?, ?, 'safe', '{}', 1, '2026-10-04T00:00:00Z')`,
			"tool_"+pseudoServer, pseudoServer, pseudoServer+".probe",
		); err != nil {
			t.Fatalf("pseudo-server %q was rejected on insert: %v", pseudoServer, err)
		}
	}
	for _, refused := range []string{"not-a-server", "Team", "TEAM"} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at)
			VALUES (?, ?, 'team.probe', 'safe', '{}', 1, '2026-10-04T00:00:00Z')`,
			"tool_refused_"+refused, refused,
		); err == nil {
			t.Fatalf("unregistered server %q was accepted on insert", refused)
		}
	}
	if _, err := database.ExecContext(ctx, `UPDATE tools SET server_name = 'team' WHERE id = 'tool_skills'`); err != nil {
		t.Fatalf("update to the team pseudo-server was rejected: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE tools SET server_name = 'not-a-server' WHERE id = 'tool_memory'`); err == nil {
		t.Fatal("an unregistered server name was accepted on update")
	}
}

func TestDelegationGroundworkMigrationMakesEverySessionAChat(t *testing.T) {
	ctx := context.Background()
	database := databaseBeforeMigration(t, ctx, "0024_delegation_groundwork.sql")
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, created_at, updated_at)
		VALUES ('sess_before', '2026-10-04T00:00:00.000000000Z', '2026-10-04T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, created_at, updated_at)
		VALUES ('sess_after', '2026-10-04T00:00:00.000000000Z', '2026-10-04T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"sess_before", "sess_after"} {
		var kind string
		if err := database.QueryRowContext(ctx, `SELECT kind FROM sessions WHERE id = ?`, id).Scan(&kind); err != nil {
			t.Fatal(err)
		}
		if kind != "chat" {
			t.Fatalf("%s kind = %q, want chat", id, kind)
		}
	}
	if _, err := database.ExecContext(ctx, `UPDATE sessions SET kind = 'delegation' WHERE id = 'sess_after'`); err != nil {
		t.Fatalf("kind 'delegation' was refused: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE sessions SET kind = 'other' WHERE id = 'sess_after'`); err == nil {
		t.Fatal("an unknown session kind was accepted")
	}
}

// An upgraded install may already hold a user server named team in any letter
// case. The migration must neither rename it nor detach its tools: the
// orchestrator withdraws them by an explicit gate instead.
func TestDelegationGroundworkMigrationLeavesAnExistingTeamServerUntouched(t *testing.T) {
	for _, name := range []string{"team", "Team"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			database := databaseBeforeMigration(t, ctx, "0024_delegation_groundwork.sql")
			if _, err := database.ExecContext(ctx, `
				INSERT INTO mcp_servers (id, name, transport, url, tier, enabled, created_at)
				VALUES ('mcp_user_team', ?, 'http', 'http://team:9000/mcp', 'local_container', 1, datetime('now'))`, name); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(ctx, `
				INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at, mcp_server_id, present)
				VALUES ('tool_user_team', ?, 'team.delegate', 'safe', '{}', 1, '2026-10-04T00:00:00Z', 'mcp_user_team', 1)`, name); err != nil {
				t.Fatal(err)
			}
			if err := ApplyMigrations(ctx, database); err != nil {
				t.Fatal(err)
			}
			var serverName, serverID string
			if err := database.QueryRowContext(ctx, `
				SELECT s.name, t.mcp_server_id FROM tools t JOIN mcp_servers s ON s.id = t.mcp_server_id
				WHERE t.id = 'tool_user_team'`).Scan(&serverName, &serverID); err != nil {
				t.Fatal(err)
			}
			if serverName != name || serverID != "mcp_user_team" {
				t.Fatalf("server %q tool now on (%q, %q)", name, serverName, serverID)
			}
		})
	}
}
