package mcpregistry

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
)

// insertRawMCPServerRow inserts one synthetic, minimal mcp_servers row
// directly via SQL, bypassing RegisterMcpServer/ImportJSON (and therefore
// their own MaxNonBundledMCPServers enforcement) entirely — the same
// "somehow already exists" simulation
// TestListMcpServersDegradesGracefullyWhenAggregateToolBudgetIsPreexistingOversized
// already uses for an oversized tools row, here for a preexisting
// oversized server *row count* instead.
func insertRawMCPServerRow(t *testing.T, database *db.DB, ctx context.Context, name string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO mcp_servers (id, name, transport, url, tier, enabled, created_at)
		VALUES (?, ?, 'http', ?, 'local_container', 0, datetime('now'))
	`, "mcp_raw_"+name, name, "http://"+name+":9000/mcp"); err != nil {
		t.Fatalf("insert raw mcp_servers row %q: %v", name, err)
	}
}

// TestListMcpServersDegradesWhenServerCountExceedsCap is the
// row-count-over-repository.MaxMCPRegistryServers counterpart to
// TestListMcpServersDegradesGracefullyWhenAggregateToolBudgetIsPreexistingOversized:
// every live write path already refuses to grow mcp_servers past that
// cap, so this should be unreachable in practice, but a database that
// somehow already exceeds it must still respond usably — a bounded
// (never unbounded) set of server descriptors, each with its own Tools
// empty, plus RegistryDegraded/RegistryDegradationReason explaining why —
// rather than either an unbounded response or an outright failure.
func TestListMcpServersDegradesWhenServerCountExceedsCap(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	service := New(repo, sealer, nil)
	ctx := context.Background()

	for i := 0; i < repository.MaxMCPRegistryServers+5; i++ {
		insertRawMCPServerRow(t, database, ctx, fmt.Sprintf("vendor-over-cap-%d", i))
	}

	response, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatalf("ListMcpServers must remain usable for a preexisting oversized server count, not fail outright: %v", err)
	}
	if !response.GetRegistryDegraded() {
		t.Fatal("RegistryDegraded = false, want true when the server count exceeds MaxMCPRegistryServers")
	}
	if response.GetRegistryDegradationReason() != mcpRegistryServerCountOverCapMessage {
		t.Fatalf("RegistryDegradationReason = %q, want the fixed notice %q", response.GetRegistryDegradationReason(), mcpRegistryServerCountOverCapMessage)
	}
	if len(response.GetServers()) != repository.MaxMCPRegistryServers {
		t.Fatalf("len(Servers) = %d, want exactly MaxMCPRegistryServers (%d), never unbounded", len(response.GetServers()), repository.MaxMCPRegistryServers)
	}
	for _, server := range response.GetServers() {
		if len(server.GetTools()) != 0 {
			t.Fatalf("server %q Tools = %+v, want empty while ServersOverCap", server.GetName(), server.GetTools())
		}
	}
}

// TestListMcpServersDegradesWhenImportIssueCountExceedsCap is the
// mcp_import_issues counterpart: a database that already holds more than
// repository.MaxMCPImportIssues rows (only reachable by a direct write
// that bypasses ReplaceMCPImportIssues, since a single ImportJSON call
// can never itself persist more — see recordUnsupported's own defensive
// bound) must still respond usably: a bounded Unsupported list, plus
// RegistryDegraded/RegistryDegradationReason, and — the same one shared
// rule every degraded condition follows — every server's own Tools left
// empty too, even though the servers table itself is not what is
// oversized here.
func TestListMcpServersDegradesWhenImportIssueCountExceedsCap(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	service := New(repo, sealer, nil)
	ctx := context.Background()

	healthy, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor-healthy", URL: "http://vendor-healthy:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, healthy.Server.ID, []repository.MCPServerTool{
		{Name: "vendor-healthy.tool", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < repository.MaxMCPImportIssues+5; i++ {
		name := fmt.Sprintf("bad-entry-%d", i)
		if _, err := database.ExecContext(ctx, `
			INSERT INTO mcp_import_issues (name, reason, reported_at) VALUES (?, 'refused for testing', datetime('now'))
		`, name); err != nil {
			t.Fatalf("insert raw mcp_import_issues row %d: %v", i, err)
		}
	}

	response, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatalf("ListMcpServers must remain usable for a preexisting oversized issue count, not fail outright: %v", err)
	}
	if !response.GetRegistryDegraded() {
		t.Fatal("RegistryDegraded = false, want true when the import issue count exceeds MaxMCPImportIssues")
	}
	if response.GetRegistryDegradationReason() != mcpRegistryIssueCountOverCapMessage {
		t.Fatalf("RegistryDegradationReason = %q, want the fixed notice %q", response.GetRegistryDegradationReason(), mcpRegistryIssueCountOverCapMessage)
	}
	if len(response.GetUnsupported()) != repository.MaxMCPImportIssues {
		t.Fatalf("len(Unsupported) = %d, want exactly MaxMCPImportIssues (%d), never unbounded", len(response.GetUnsupported()), repository.MaxMCPImportIssues)
	}
	found := false
	for _, server := range response.GetServers() {
		if server.GetName() != "vendor-healthy" {
			continue
		}
		found = true
		if len(server.GetTools()) != 0 {
			t.Fatalf("vendor-healthy Tools = %+v, want empty while IssuesOverCap", server.GetTools())
		}
	}
	if !found {
		t.Fatal("vendor-healthy is missing from the response; server list must be unaffected by IssuesOverCap")
	}
}

// TestReimportRealInvalidRegistryNamedEntryNeverCollidesWithDegradedNotice
// proves finding #1 (invalid mcp.json entry keys are never used as the
// Unsupported map key) and finding #3 (the degraded registry state is an
// explicit field, never a synthetic "_registry"-named Unsupported entry)
// close the exact collision the old implementation's own doc comment
// used to accept: an mcp.json entry literally named "_registry" is
// pattern-invalid (its leading "_" fails mcpServerNamePattern) and so is
// refused through the ordinary synthetic-invalid-entry path, under an
// "_invalid_server_N" label — never literally "_registry" — so it can
// never be confused with (or silently merged into) a genuine
// RegistryDegraded notice, even when both are present in the same
// response at once.
func TestReimportRealInvalidRegistryNamedEntryNeverCollidesWithDegradedNotice(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	service := New(repo, sealer, nil)
	ctx := context.Background()

	// A genuinely degraded registry (server count over cap)...
	for i := 0; i < repository.MaxMCPRegistryServers+5; i++ {
		insertRawMCPServerRow(t, database, ctx, fmt.Sprintf("vendor-over-cap-%d", i))
	}

	// ...alongside a real mcp.json import naming an entry "_registry".
	root := t.TempDir()
	document := []byte(`{"mcpServers":{"_registry":{"url":"https://vendor.example/mcp"}}}`)
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), document, 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	reimported, err := service.ReimportMcpJson(ctx, &turingv1.ReimportMcpJsonRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reimported.GetUnsupported()) != 1 || reimported.GetUnsupported()[0].GetName() != "_invalid_server_1" {
		t.Fatalf("Unsupported = %+v, want exactly one entry named _invalid_server_1, never _registry", reimported.GetUnsupported())
	}

	response, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !response.GetRegistryDegraded() {
		t.Fatal("RegistryDegraded = false, want true: the server count is genuinely over cap")
	}
	if response.GetRegistryDegradationReason() != mcpRegistryServerCountOverCapMessage {
		t.Fatalf("RegistryDegradationReason = %q, want %q", response.GetRegistryDegradationReason(), mcpRegistryServerCountOverCapMessage)
	}
	foundInvalid := false
	for _, unsupported := range response.GetUnsupported() {
		if unsupported.GetName() == "_registry" {
			t.Fatalf("Unsupported = %+v, want no entry literally named _registry", response.GetUnsupported())
		}
		if unsupported.GetName() == "_invalid_server_1" {
			foundInvalid = true
		}
	}
	if !foundInvalid {
		t.Fatalf("Unsupported = %+v, want the persisted _invalid_server_1 issue still listed", response.GetUnsupported())
	}
}

// TestListMcpServersDegradationReasonPrioritizesOverBudgetThenServersThenIssues
// locks the documented priority order in ListMcpServers when more than one
// degraded condition trips at once: OverBudget, then ServersOverCap, then
// IssuesOverCap (see ListMcpServers' own doc comment). This test trips
// ServersOverCap and IssuesOverCap simultaneously and asserts
// RegistryDegradationReason names the server-count cause, never the
// issue-count one — proving the switch's ordering, not merely that each
// condition works in isolation.
func TestListMcpServersDegradationReasonPrioritizesOverBudgetThenServersThenIssues(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	service := New(repo, sealer, nil)
	ctx := context.Background()

	for i := 0; i < repository.MaxMCPRegistryServers+5; i++ {
		insertRawMCPServerRow(t, database, ctx, fmt.Sprintf("vendor-both-%d", i))
	}
	for i := 0; i < repository.MaxMCPImportIssues+5; i++ {
		name := fmt.Sprintf("bad-entry-both-%d", i)
		if _, err := database.ExecContext(ctx, `
			INSERT INTO mcp_import_issues (name, reason, reported_at) VALUES (?, 'refused for testing', datetime('now'))
		`, name); err != nil {
			t.Fatalf("insert raw mcp_import_issues row %d: %v", i, err)
		}
	}

	response, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatalf("ListMcpServers must remain usable with both caps exceeded at once: %v", err)
	}
	if !response.GetRegistryDegraded() {
		t.Fatal("RegistryDegraded = false, want true when both caps are exceeded")
	}
	if response.GetRegistryDegradationReason() != mcpRegistryServerCountOverCapMessage {
		t.Fatalf("RegistryDegradationReason = %q, want the server-count reason %q to win over the issue-count one when both trip",
			response.GetRegistryDegradationReason(), mcpRegistryServerCountOverCapMessage)
	}
	if len(response.GetServers()) != repository.MaxMCPRegistryServers {
		t.Fatalf("len(Servers) = %d, want exactly MaxMCPRegistryServers (%d)", len(response.GetServers()), repository.MaxMCPRegistryServers)
	}
	if len(response.GetUnsupported()) != repository.MaxMCPImportIssues {
		t.Fatalf("len(Unsupported) = %d, want exactly MaxMCPImportIssues (%d): both bounds still apply regardless of which reason is reported", len(response.GetUnsupported()), repository.MaxMCPImportIssues)
	}
}
