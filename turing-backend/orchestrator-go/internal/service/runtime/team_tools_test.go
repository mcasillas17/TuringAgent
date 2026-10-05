package runtime

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func advertiseTools(capabilities *turingv1.WorkerCapabilities, qualified ...[2]string) *turingv1.WorkerCapabilities {
	for _, tool := range qualified {
		capabilities.Tools = append(capabilities.Tools, &turingv1.DiscoveredTool{
			ServerName: tool[0], ToolName: tool[1], Schema: &structpb.Struct{},
		})
	}
	return capabilities
}

// A worker that advertises team.delegate registers it the way it registers
// memory tools: a pseudo-server row with no MCP server behind it, seeded safe.
func TestAdvertisedTeamDelegateRegistersAsAPseudoTool(t *testing.T) {
	h := newHarness(t)
	worker := connectWorkerCapabilities(t, h, "worker-team-tool", "registration-team-tool",
		advertiseTools(teamCapabilities(1), [2]string{"team", "team.delegate"}))
	defer func() { _ = worker.CloseSend() }()

	var policy string
	var serverID sql.NullString
	var present int
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT policy, mcp_server_id, present FROM tools WHERE server_name = 'team' AND tool_name = 'team.delegate'`,
	).Scan(&policy, &serverID, &present); err != nil {
		t.Fatal(err)
	}
	if policy != "safe" || serverID.Valid || present != 1 {
		t.Fatalf("team.delegate row = policy %q server %v present %d, want safe, NULL, 1", policy, serverID, present)
	}
	if !slices.Contains(h.service.EgressToolNames(teamRoute(1)), "team/team.delegate") {
		t.Fatal("team/team.delegate is missing from the worker's route tools")
	}
}

// While a user server is named Team, neither its tools nor the team
// pseudo-tool reach any worker's capabilities, so no run can select or claim
// them, and the user's rows keep their server. Removing the server restores
// delegation at the next registration.
func TestCollidingTeamNamesAreWithdrawnFromWorkerCapabilities(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.database.ExecContext(ctx, `
		INSERT INTO mcp_servers (id, name, transport, url, tier, enabled, created_at)
		VALUES ('mcp_user_team', 'Team', 'http', 'http://team:9000/mcp', 'local_container', 1, datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(ctx, `
		INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at, mcp_server_id, present)
		VALUES ('tool_user_team', 'Team', 'lookup', 'safe', '{}', 1, '2026-10-04T00:00:00Z', 'mcp_user_team', 1)`); err != nil {
		t.Fatal(err)
	}
	advertised := func() *turingv1.WorkerCapabilities {
		return advertiseTools(teamCapabilities(1, "common"), [2]string{"team", "team.delegate"}, [2]string{"Team", "lookup"})
	}
	first := connectWorkerCapabilities(t, h, "worker-collision", "registration-collision", advertised())
	if got := h.service.EgressToolNames(teamRoute(1)); !slices.Equal(got, []string{"system/common"}) {
		t.Fatalf("route tools during the collision = %v, want only system/common", got)
	}
	var pseudoRows int
	var userServer string
	if err := h.database.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM tools WHERE server_name = 'team'),
			(SELECT mcp_server_id FROM tools WHERE id = 'tool_user_team')`).Scan(&pseudoRows, &userServer); err != nil {
		t.Fatal(err)
	}
	if pseudoRows != 0 || userServer != "mcp_user_team" {
		t.Fatalf("pseudo rows = %d and the user's tool is on %q, want none and mcp_user_team", pseudoRows, userServer)
	}
	_ = first.CloseSend()

	if _, err := h.database.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id = 'mcp_user_team'`); err != nil {
		t.Fatal(err)
	}
	second := connectWorkerCapabilities(t, h, "worker-restored", "registration-restored",
		advertiseTools(teamCapabilities(1, "common"), [2]string{"team", "team.delegate"}))
	defer func() { _ = second.CloseSend() }()
	if got := h.service.EgressToolNames(teamRoute(1)); !slices.Contains(got, "team/team.delegate") {
		t.Fatalf("route tools after removal = %v, want team/team.delegate restored", got)
	}
}

// The collision withdraws team-named tools only: another pseudo-server's tool
// that has no row yet still bootstraps it at registration and reaches the
// worker's route, exactly as it does without a collision.
func TestATeamNameCollisionStillBootstrapsOtherPseudoTools(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.database.ExecContext(ctx, `
		INSERT INTO mcp_servers (id, name, transport, url, tier, enabled, created_at)
		VALUES ('mcp_user_team', 'team', 'http', 'http://team:9000/mcp', 'local_container', 1, datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(ctx, `DELETE FROM tools WHERE server_name = 'memory'`); err != nil {
		t.Fatal(err)
	}
	worker := connectWorkerCapabilities(t, h, "worker-collision-memory", "registration-collision-memory",
		advertiseTools(teamCapabilities(1), [2]string{"team", "team.delegate"}, [2]string{"memory", "memory.search"}))
	defer func() { _ = worker.CloseSend() }()

	if got := h.service.EgressToolNames(teamRoute(1)); !slices.Equal(got, []string{"memory/memory.search"}) {
		t.Fatalf("route tools during the collision = %v, want only memory/memory.search", got)
	}
	var serverID sql.NullString
	var present int
	if err := h.database.QueryRowContext(ctx, `
		SELECT mcp_server_id, present FROM tools WHERE server_name = 'memory' AND tool_name = 'memory.search'`,
	).Scan(&serverID, &present); err != nil {
		t.Fatalf("memory.search was not registered during the collision: %v", err)
	}
	if serverID.Valid || present != 1 {
		t.Fatalf("memory.search row = server %v present %d, want NULL and 1", serverID, present)
	}
}

// The orchestrator's own team/team.delegate row is not a collision: after it
// is registered, delegation stays on, and the next registration keeps the
// tool registered, present and on the worker's route.
func TestRegisteringTeamDelegateAgainDoesNotCollideWithItself(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	advertised := func() *turingv1.WorkerCapabilities {
		return advertiseTools(teamCapabilities(1), [2]string{"team", "team.delegate"})
	}
	first := connectWorkerCapabilities(t, h, "worker-team-first", "registration-team-first", advertised())
	if reason, err := h.repo.TeamNameCollision(ctx); err != nil || reason != "" {
		t.Fatalf("collision after registering team.delegate = %q, %v; want none", reason, err)
	}
	_ = first.CloseSend()

	second := connectWorkerCapabilities(t, h, "worker-team-second", "registration-team-second", advertised())
	defer func() { _ = second.CloseSend() }()
	if reason, err := h.repo.TeamNameCollision(ctx); err != nil || reason != "" {
		t.Fatalf("collision after registering team.delegate again = %q, %v; want none", reason, err)
	}
	if !slices.Contains(h.service.EgressToolNames(teamRoute(1)), "team/team.delegate") {
		t.Fatal("team/team.delegate left the route on the second registration")
	}
	var serverID sql.NullString
	var present int
	if err := h.database.QueryRowContext(ctx, `
		SELECT mcp_server_id, present FROM tools WHERE server_name = 'team' AND tool_name = 'team.delegate'`,
	).Scan(&serverID, &present); err != nil {
		t.Fatal(err)
	}
	if serverID.Valid || present != 1 {
		t.Fatalf("team.delegate row after re-registration = server %v present %d, want NULL and 1", serverID, present)
	}
}

// A third-party team.* tool is a collision only for the team namespace: it
// keeps working as its own server's tool while the team pseudo-tool is
// withdrawn.
func TestAVendorTeamToolKeepsWorkingWhileItCollides(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.database.ExecContext(ctx, `
		INSERT INTO mcp_servers (id, name, transport, url, tier, enabled, created_at)
		VALUES ('mcp_vendor', 'vendor', 'http', 'http://vendor:9000/mcp', 'local_container', 1, datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(ctx, `
		INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at, mcp_server_id, present)
		VALUES ('tool_vendor_team', 'vendor', 'team.delegate', 'safe', '{}', 1, '2026-10-04T00:00:00Z', 'mcp_vendor', 1)`); err != nil {
		t.Fatal(err)
	}
	worker := connectWorkerCapabilities(t, h, "worker-vendor-team", "registration-vendor-team",
		advertiseTools(teamCapabilities(1), [2]string{"team", "team.delegate"}, [2]string{"vendor", "team.delegate"}))
	defer func() { _ = worker.CloseSend() }()

	if got := h.service.EgressToolNames(teamRoute(1)); !slices.Equal(got, []string{"vendor/team.delegate"}) {
		t.Fatalf("route tools during the collision = %v, want only vendor/team.delegate", got)
	}
	var serverID string
	var present int
	if err := h.database.QueryRowContext(ctx, `
		SELECT mcp_server_id, present FROM tools WHERE id = 'tool_vendor_team'`).Scan(&serverID, &present); err != nil {
		t.Fatal(err)
	}
	if serverID != "mcp_vendor" || present != 1 {
		t.Fatalf("vendor tool row = server %q present %d, want mcp_vendor and 1", serverID, present)
	}
}
