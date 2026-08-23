package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
)

// TestMCPRegistrySnapshotReturnsEveryServerWithItsOwnTools proves the
// basic shape: every server row (bundled and non-bundled), each paired
// with exactly the tools ListMCPServerTools would return for it — present
// and withdrawn alike — plus every recorded import issue, all read
// together.
func TestMCPRegistrySnapshotReturnsEveryServerWithItsOwnTools(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	server, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, server.Server.ID, []MCPServerTool{
		{Name: "vendor.a", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}
	// Withdraw it by reconfirming a disjoint tool, so the snapshot must
	// surface both the present and the withdrawn row.
	if err := repo.ReplaceMCPServerTools(ctx, server.Server.ID, []MCPServerTool{
		{Name: "vendor.b", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPImportIssues(ctx, map[string]string{"bad-entry": "refused for testing"}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := repo.MCPRegistrySnapshot(ctx)
	if err != nil {
		t.Fatalf("MCPRegistrySnapshot: %v", err)
	}

	expectedServers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Servers) != len(expectedServers) {
		t.Fatalf("snapshot has %d servers, want %d (matching ListMCPServers)", len(snapshot.Servers), len(expectedServers))
	}

	var vendorEntry *MCPServerWithTools
	for i := range snapshot.Servers {
		if snapshot.Servers[i].Server.Name == "vendor" {
			vendorEntry = &snapshot.Servers[i]
		}
	}
	if vendorEntry == nil {
		t.Fatal("snapshot did not include the vendor server")
	}
	if len(vendorEntry.Tools) != 2 {
		t.Fatalf("vendor tools = %+v, want 2 (one present, one withdrawn)", vendorEntry.Tools)
	}
	var sawPresent, sawWithdrawn bool
	for _, tool := range vendorEntry.Tools {
		switch tool.Name {
		case "vendor.b":
			sawPresent = tool.Present
		case "vendor.a":
			sawWithdrawn = !tool.Present
		}
	}
	if !sawPresent {
		t.Fatal("snapshot must include vendor.b, present")
	}
	if !sawWithdrawn {
		t.Fatal("snapshot must include vendor.a, withdrawn (present=false) — never deleted")
	}

	if len(snapshot.Issues) != 1 || snapshot.Issues[0].Name != "bad-entry" {
		t.Fatalf("snapshot issues = %+v, want exactly the one recorded issue", snapshot.Issues)
	}
}

// TestMCPRegistrySnapshotSetsOverBudgetAndOmitsToolsForPreexistingOverBudgetAggregate
// proves the degraded-but-usable guard: a database that already carries
// an aggregate over MaxMCPRegistryToolBytes (simulated here via a direct
// row insert, bypassing every repository write path that would itself
// refuse such a state) must still return every server row — so a caller
// like ListMcpServers can keep the registry manageable (an operator can
// still see server IDs/endpoints and delete one) — but with OverBudget
// set and every server's own Tools completely omitted, never attempting
// to read (let alone marshal and send) a schema-heavy result sized
// against an unbounded aggregate. Import issues are still returned
// normally: they carry no tool schemas and are already independently
// bounded (maxMCPImportEntries, maxMCPStatusMessageBytes), so nothing
// about the over-budget tools table affects them.
func TestMCPRegistrySnapshotSetsOverBudgetAndOmitsToolsForPreexistingOverBudgetAggregate(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	server, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-oversized", URL: "http://vendor-oversized:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPImportIssues(ctx, map[string]string{"bad-entry": "refused for testing"}); err != nil {
		t.Fatal(err)
	}

	oversizedSchema := `{"type":"object","d":"` + strings.Repeat("x", MaxMCPRegistryToolBytes) + `"}`
	if _, err := database.ExecContext(ctx, `
		INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at, mcp_server_id, present)
		VALUES ('tool_test_oversized', ?, 'vendor.oversized', 'safe', ?, 1, datetime('now'), ?, 1)
	`, server.Server.Name, oversizedSchema, server.Server.ID); err != nil {
		t.Fatal(err)
	}

	snapshot, err := repo.MCPRegistrySnapshot(ctx)
	if err != nil {
		t.Fatalf("MCPRegistrySnapshot must remain usable for a preexisting oversized aggregate, not fail outright: %v", err)
	}
	if !snapshot.OverBudget {
		t.Fatal("OverBudget = false, want true for a preexisting oversized aggregate")
	}
	if len(snapshot.Servers) == 0 {
		t.Fatal("Servers is empty, want every server row still returned (bundled and non-bundled alike) so the registry stays manageable")
	}
	for _, entry := range snapshot.Servers {
		if len(entry.Tools) != 0 {
			t.Fatalf("server %q Tools = %+v, want empty: tool schemas must never be read while OverBudget", entry.Server.Name, entry.Tools)
		}
	}
	if len(snapshot.Issues) != 1 || snapshot.Issues[0].Name != "bad-entry" {
		t.Fatalf("snapshot issues = %+v, want the one recorded issue, unaffected by OverBudget", snapshot.Issues)
	}
}

// TestMCPRegistrySnapshotSerializesAgainstConcurrentWrite is the
// deterministic, barrier-based coherence proof: MCPRegistrySnapshot pauses
// (via the test-only mcpRegistrySnapshotBarrier hook) after its own
// aggregate-budget guard has already run inside its one read transaction,
// but before it reads any server or tool row. While paused, a concurrent
// write that would grow the registry is attempted; because the database
// has exactly one connection (db.Open's SetMaxOpenConns(1)), that write
// cannot even begin its own transaction until the snapshot's transaction
// ends, so it must still be blocked a short but generous interval later.
// Releasing the barrier lets the snapshot finish first, and its own
// returned rows must reflect *that* pre-write state completely — never a
// server row without its tools, or tools without their owning server, and
// never a total that disagrees with the aggregate guard's own decision.
// Only once the snapshot has returned does the concurrent write proceed.
func TestMCPRegistrySnapshotSerializesAgainstConcurrentWrite(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	server, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-a", URL: "http://vendor-a:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, server.Server.ID, []MCPServerTool{
		toolOfExactRawSize(t, "a", MaxThirdPartyMCPRegistryToolBytes-100),
	}); err != nil {
		t.Fatal(err)
	}

	reached := make(chan struct{})
	proceed := make(chan struct{})
	var once sync.Once
	repo.mcpRegistrySnapshotBarrier = func() {
		once.Do(func() { close(reached) })
		<-proceed
	}

	type snapshotResult struct {
		snapshot MCPRegistrySnapshot
		err      error
	}
	snapshotDone := make(chan snapshotResult, 1)
	go func() {
		snap, err := repo.MCPRegistrySnapshot(ctx)
		snapshotDone <- snapshotResult{snap, err}
	}()

	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot never reached its barrier")
	}

	// A concurrent registration (well within the remaining ~100-byte
	// budget) must be unable to complete while the snapshot's own
	// transaction is still open: it needs the same single connection.
	writeDone := make(chan error, 1)
	go func() {
		_, ierr := repo.RegisterMCPServer(ctx, ImportedMCPServer{
			Name: "vendor-b", URL: "http://vendor-b:9000/mcp", Tier: MCPServerTierLocalContainer,
		})
		writeDone <- ierr
	}()

	select {
	case <-writeDone:
		t.Fatal("concurrent write completed while the snapshot's own read transaction was still open: the two are not serialized")
	case <-time.After(200 * time.Millisecond):
		// Expected: the write is still blocked waiting for the
		// database's one connection.
	}

	close(proceed)
	result := <-snapshotDone
	if result.err != nil {
		t.Fatalf("snapshot failed: %v", result.err)
	}
	if writeErr := <-writeDone; writeErr != nil {
		t.Fatalf("concurrent write failed once the snapshot released the connection: %v", writeErr)
	}

	// The snapshot must reflect the world exactly as it was before the
	// concurrent write: vendor-a (plus whatever bundled servers migrations
	// seed) only, never a torn view that already includes vendor-b's row
	// (which did not exist yet when the snapshot's transaction captured
	// its view).
	if got := nonBundledCount(snapshotServerRecords(result.snapshot)); got != 1 {
		t.Fatalf("snapshot non-bundled server count = %d, want exactly 1 (vendor-a): a coherent pre-write view", got)
	}
	var vendorAEntry *MCPServerWithTools
	for i := range result.snapshot.Servers {
		if result.snapshot.Servers[i].Server.Name == "vendor-a" {
			vendorAEntry = &result.snapshot.Servers[i]
		}
		if result.snapshot.Servers[i].Server.Name == "vendor-b" {
			t.Fatal("snapshot must not include vendor-b: it did not exist yet when the snapshot's transaction captured its view")
		}
	}
	if vendorAEntry == nil {
		t.Fatal("snapshot did not include vendor-a")
	}
	if len(vendorAEntry.Tools) != 1 || vendorAEntry.Tools[0].Name != "a" {
		t.Fatalf("vendor-a tool count = %d, name = %q, want exactly the one tool named 'a'", len(vendorAEntry.Tools), toolNameOrEmpty(vendorAEntry.Tools))
	}

	// vendor-b's own registration, once unblocked, must have committed
	// for real (proving the barrier delayed rather than corrupted it).
	after, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledCount(after) != 2 {
		t.Fatalf("non-bundled server count after both operations = %d, want 2", nonBundledCount(after))
	}
}

// snapshotServerRecords extracts the bare MCPServerRecord list from a
// snapshot's per-server entries, so nonBundledCount (defined in
// mcp_registry_cap_test.go) can be reused here directly.
func snapshotServerRecords(snapshot MCPRegistrySnapshot) []MCPServerRecord {
	records := make([]MCPServerRecord, 0, len(snapshot.Servers))
	for _, entry := range snapshot.Servers {
		records = append(records, entry.Server)
	}
	return records
}

// toolNameOrEmpty returns the first tool's name for a failure message,
// without ever risking a %+v dump of a tool whose schema may be
// arbitrarily large (as toolOfExactRawSize's own tools deliberately are).
func toolNameOrEmpty(tools []MCPServerTool) string {
	if len(tools) == 0 {
		return ""
	}
	return tools[0].Name
}

// insertRawMCPServerRows inserts count synthetic, minimal mcp_servers rows
// directly via SQL, bypassing RegisterMCPServer/ImportMCPServer (and
// therefore nonBundledMCPServerRegistryFullTx's own MaxNonBundledMCPServers
// enforcement) entirely — the same "somehow already exists" simulation
// TestMCPRegistrySnapshotSetsOverBudgetAndOmitsToolsForPreexistingOverBudgetAggregate
// already uses for an oversized tools row, here for a preexisting
// oversized *row count* instead. No mcp_server_status row is inserted for
// any of them: listMCPServersRowsBounded's LEFT JOIN already defaults a
// missing one to 'unknown', so this stays minimal.
func insertRawMCPServerRows(t *testing.T, database *db.DB, ctx context.Context, prefix string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("mcp_raw_%s_%d", prefix, i)
		name := fmt.Sprintf("%s-%d", prefix, i)
		if _, err := database.ExecContext(ctx, `
			INSERT INTO mcp_servers (id, name, transport, url, tier, enabled, created_at)
			VALUES (?, ?, 'http', ?, 'local_container', 0, datetime('now'))
		`, id, name, "http://"+name+":9000/mcp"); err != nil {
			t.Fatalf("insert raw mcp_servers row %d: %v", i, err)
		}
	}
}

// TestMCPRegistrySnapshotSetsServersOverCapAndBoundsServersForPreexistingOversizedRowCount
// proves the row-count counterpart to the tool-byte-budget guard above: a
// database that already holds more than MaxMCPRegistryServers rows (every
// live write path — RegisterMCPServer, ImportMCPServer — already refuses
// to create such a state via nonBundledMCPServerRegistryFullTx, so this
// should be unreachable in practice, simulated here the same
// bypass-every-write-path way) must not make MCPRegistrySnapshot read an
// unbounded number of rows: it returns exactly MaxMCPRegistryServers of
// them (never zero, never the full oversized count), with ServersOverCap
// set and every one of those returned servers' own Tools left empty —
// the same "still usable, never both unbounded and silently truncated
// without saying so" guarantee OverBudget already gives the tool-byte
// case.
func TestMCPRegistrySnapshotSetsServersOverCapAndBoundsServersForPreexistingOversizedRowCount(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	insertRawMCPServerRows(t, database, ctx, "over-cap", MaxMCPRegistryServers+5)
	if err := repo.ReplaceMCPImportIssues(ctx, map[string]string{"bad-entry": "refused for testing"}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := repo.MCPRegistrySnapshot(ctx)
	if err != nil {
		t.Fatalf("MCPRegistrySnapshot must remain usable for a preexisting oversized row count, not fail outright: %v", err)
	}
	if !snapshot.ServersOverCap {
		t.Fatal("ServersOverCap = false, want true for a preexisting server count over MaxMCPRegistryServers")
	}
	if snapshot.OverBudget {
		t.Fatal("OverBudget = true, want false: only the row count, not the tool-byte aggregate, is oversized here")
	}
	if len(snapshot.Servers) != MaxMCPRegistryServers {
		t.Fatalf("len(Servers) = %d, want exactly MaxMCPRegistryServers (%d)", len(snapshot.Servers), MaxMCPRegistryServers)
	}
	for _, entry := range snapshot.Servers {
		if len(entry.Tools) != 0 {
			t.Fatalf("server %q Tools = %+v, want empty while ServersOverCap", entry.Server.Name, entry.Tools)
		}
	}
	if len(snapshot.Issues) != 1 || snapshot.Issues[0].Name != "bad-entry" {
		t.Fatalf("snapshot issues = %+v, want the one recorded issue, unaffected by ServersOverCap", snapshot.Issues)
	}
}

// TestMCPRegistrySnapshotServersAtExactCapIsNotDegraded proves the
// ServersOverCap guard is not off-by-one on its in-budget side: a
// database holding *exactly* MaxMCPRegistryServers rows (bundled rows
// included, whatever their current count happens to be) must list
// successfully, with ServersOverCap false, every row returned (none
// truncated), and a healthy server's own tools intact — mirroring
// TestListMcpServersSucceedsAtExactAggregateBudgetBoundary's identical
// proof for the byte-budget guard.
func TestMCPRegistrySnapshotServersAtExactCapIsNotDegraded(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	var existing int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_servers`).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	healthy, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-healthy", URL: "http://vendor-healthy:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, healthy.Server.ID, []MCPServerTool{
		{Name: "vendor-healthy.tool", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}
	// One more raw row is already registered (vendor-healthy) than
	// `existing`, so top up to exactly MaxMCPRegistryServers total.
	insertRawMCPServerRows(t, database, ctx, "at-cap", MaxMCPRegistryServers-existing-1)

	var total int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_servers`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != MaxMCPRegistryServers {
		t.Fatalf("test setup is broken: mcp_servers has %d rows, want exactly MaxMCPRegistryServers (%d)", total, MaxMCPRegistryServers)
	}

	snapshot, err := repo.MCPRegistrySnapshot(ctx)
	if err != nil {
		t.Fatalf("MCPRegistrySnapshot at exactly the server-count boundary must succeed: %v", err)
	}
	if snapshot.ServersOverCap {
		t.Fatal("ServersOverCap = true, want false at the exact (in-cap) boundary")
	}
	if len(snapshot.Servers) != MaxMCPRegistryServers {
		t.Fatalf("len(Servers) = %d, want exactly MaxMCPRegistryServers (%d), none truncated at the boundary", len(snapshot.Servers), MaxMCPRegistryServers)
	}
	found := false
	for _, entry := range snapshot.Servers {
		if entry.Server.Name != "vendor-healthy" {
			continue
		}
		found = true
		if len(entry.Tools) != 1 || entry.Tools[0].Name != "vendor-healthy.tool" {
			t.Fatalf("vendor-healthy Tools = %+v, want its one tool intact at the exact (in-cap) boundary", entry.Tools)
		}
	}
	if !found {
		t.Fatal("vendor-healthy is missing from the snapshot at the exact boundary")
	}
}

// TestMCPRegistrySnapshotServersOneOverCapIsDegradedAndBoundedToExactlyCap
// is the complementary off-by-one proof on the over-cap side: exactly
// MaxMCPRegistryServers+1 rows (one past the boundary
// TestMCPRegistrySnapshotServersAtExactCapIsNotDegraded proves is still
// healthy) must already trip ServersOverCap and bound the returned list
// to exactly MaxMCPRegistryServers — neither MaxMCPRegistryServers+1 nor
// some other off-by-one count.
func TestMCPRegistrySnapshotServersOneOverCapIsDegradedAndBoundedToExactlyCap(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	var existing int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_servers`).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	insertRawMCPServerRows(t, database, ctx, "one-over-cap", MaxMCPRegistryServers-existing+1)

	var total int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_servers`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != MaxMCPRegistryServers+1 {
		t.Fatalf("test setup is broken: mcp_servers has %d rows, want exactly MaxMCPRegistryServers+1 (%d)", total, MaxMCPRegistryServers+1)
	}

	snapshot, err := repo.MCPRegistrySnapshot(ctx)
	if err != nil {
		t.Fatalf("MCPRegistrySnapshot one row over the server-count boundary must still succeed: %v", err)
	}
	if !snapshot.ServersOverCap {
		t.Fatal("ServersOverCap = false, want true exactly one row past the boundary")
	}
	if len(snapshot.Servers) != MaxMCPRegistryServers {
		t.Fatalf("len(Servers) = %d, want exactly MaxMCPRegistryServers (%d), not MaxMCPRegistryServers+1 or any other count", len(snapshot.Servers), MaxMCPRegistryServers)
	}
}

// TestMCPRegistrySnapshotSetsIssuesOverCapAndBoundsIssuesForPreexistingOversizedIssueCount
// is the mcp_import_issues counterpart: a database that already holds
// more than MaxMCPImportIssues rows (a single ImportJSON call can never
// itself write more than that — see mcpregistry.recordUnsupported's own
// defensive bound — so, again, only reachable through a direct write that
// bypasses ReplaceMCPImportIssues entirely) must not make
// MCPRegistrySnapshot read an unbounded number of issue rows either: it
// returns exactly MaxMCPImportIssues of them, with IssuesOverCap set.
// Every server's own Tools is left empty here too, the same as the other
// two degraded conditions — a caller (ListMcpServers) has one shared,
// simple rule ("degraded means no Tools anywhere") rather than a
// per-cause exception to remember. The server list itself is unaffected
// in count: only the issues table is oversized in this scenario.
func TestMCPRegistrySnapshotSetsIssuesOverCapAndBoundsIssuesForPreexistingOversizedIssueCount(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	server, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor-healthy", URL: "http://vendor-healthy:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMCPServerTools(ctx, server.Server.ID, []MCPServerTool{
		{Name: "vendor-healthy.tool", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true},
	}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < MaxMCPImportIssues+5; i++ {
		name := fmt.Sprintf("bad-entry-%d", i)
		if _, err := database.ExecContext(ctx, `
			INSERT INTO mcp_import_issues (name, reason, reported_at) VALUES (?, 'refused for testing', datetime('now'))
		`, name); err != nil {
			t.Fatalf("insert raw mcp_import_issues row %d: %v", i, err)
		}
	}

	snapshot, err := repo.MCPRegistrySnapshot(ctx)
	if err != nil {
		t.Fatalf("MCPRegistrySnapshot must remain usable for a preexisting oversized issue count, not fail outright: %v", err)
	}
	if !snapshot.IssuesOverCap {
		t.Fatal("IssuesOverCap = false, want true for a preexisting issue count over MaxMCPImportIssues")
	}
	if snapshot.OverBudget || snapshot.ServersOverCap {
		t.Fatalf("OverBudget = %v, ServersOverCap = %v, want both false: only the issue count is oversized here", snapshot.OverBudget, snapshot.ServersOverCap)
	}
	if len(snapshot.Issues) != MaxMCPImportIssues {
		t.Fatalf("len(Issues) = %d, want exactly MaxMCPImportIssues (%d)", len(snapshot.Issues), MaxMCPImportIssues)
	}
	found := false
	for _, entry := range snapshot.Servers {
		if entry.Server.Name != "vendor-healthy" {
			continue
		}
		found = true
		if len(entry.Tools) != 0 {
			t.Fatalf("vendor-healthy Tools = %+v, want empty while IssuesOverCap", entry.Tools)
		}
	}
	if !found {
		t.Fatal("vendor-healthy is missing from the snapshot; server count must be unaffected by IssuesOverCap")
	}
}

// insertRawMCPImportIssueRows inserts count synthetic mcp_import_issues
// rows directly via SQL, bypassing ReplaceMCPImportIssues, to simulate a
// preexisting issue count at or around the MaxMCPImportIssues boundary.
func insertRawMCPImportIssueRows(t *testing.T, database *db.DB, ctx context.Context, prefix string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("%s-%d", prefix, i)
		if _, err := database.ExecContext(ctx, `
			INSERT INTO mcp_import_issues (name, reason, reported_at) VALUES (?, 'refused for testing', datetime('now'))
		`, name); err != nil {
			t.Fatalf("insert raw mcp_import_issues row %d: %v", i, err)
		}
	}
}

// TestMCPRegistrySnapshotIssuesAtExactCapIsNotDegraded is the
// issue-count counterpart to TestMCPRegistrySnapshotServersAtExactCapIsNotDegraded:
// exactly MaxMCPImportIssues rows must list successfully with
// IssuesOverCap false and every row returned, none truncated.
func TestMCPRegistrySnapshotIssuesAtExactCapIsNotDegraded(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	insertRawMCPImportIssueRows(t, database, ctx, "at-cap", MaxMCPImportIssues)

	snapshot, err := repo.MCPRegistrySnapshot(ctx)
	if err != nil {
		t.Fatalf("MCPRegistrySnapshot at exactly the issue-count boundary must succeed: %v", err)
	}
	if snapshot.IssuesOverCap {
		t.Fatal("IssuesOverCap = true, want false at the exact (in-cap) boundary")
	}
	if len(snapshot.Issues) != MaxMCPImportIssues {
		t.Fatalf("len(Issues) = %d, want exactly MaxMCPImportIssues (%d), none truncated at the boundary", len(snapshot.Issues), MaxMCPImportIssues)
	}
}

// TestMCPRegistrySnapshotIssuesOneOverCapIsDegradedAndBoundedToExactlyCap
// is the issue-count counterpart to
// TestMCPRegistrySnapshotServersOneOverCapIsDegradedAndBoundedToExactlyCap:
// exactly MaxMCPImportIssues+1 rows must already trip IssuesOverCap and
// bound the returned list to exactly MaxMCPImportIssues.
func TestMCPRegistrySnapshotIssuesOneOverCapIsDegradedAndBoundedToExactlyCap(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	insertRawMCPImportIssueRows(t, database, ctx, "one-over-cap", MaxMCPImportIssues+1)

	snapshot, err := repo.MCPRegistrySnapshot(ctx)
	if err != nil {
		t.Fatalf("MCPRegistrySnapshot one row over the issue-count boundary must still succeed: %v", err)
	}
	if !snapshot.IssuesOverCap {
		t.Fatal("IssuesOverCap = false, want true exactly one row past the boundary")
	}
	if len(snapshot.Issues) != MaxMCPImportIssues {
		t.Fatalf("len(Issues) = %d, want exactly MaxMCPImportIssues (%d), not MaxMCPImportIssues+1 or any other count", len(snapshot.Issues), MaxMCPImportIssues)
	}
}

// TestMCPRegistrySnapshotAccommodatesMaxNamedEntriesPlusOneDocumentIssueWithoutDegrading
// proves the exact real-world shape a single interrupted reimport can
// legitimately persist through ReplaceMCPImportIssues — MaxNonBundledMCPServers
// (256) ordinary per-entry refusals, the most an mcp.json document can
// ever name at once (see the mcpregistry package's own maxMCPImportEntries),
// plus exactly one additional "_document" entry recordDocumentRefusal
// folds in on top of them when a later, whole-run failure interrupts an
// otherwise fully-processed document — never trips IssuesOverCap.
// MaxMCPImportIssues must therefore reserve headroom for that one
// document-level entry above MaxNonBundledMCPServers, not merely equal
// it: before that reservation, this exact 257-row write (256 legitimate
// per-entry issues the registry itself would have produced, plus the one
// "_document" entry) would have been misread as an over-cap, "somehow
// already exceeds it" condition indistinguishable from actual corruption
// — degrading the entire registry (blanking every server's own Tools,
// per MCPRegistrySnapshot's own doc comment) for an entirely ordinary,
// bounded outcome.
func TestMCPRegistrySnapshotAccommodatesMaxNamedEntriesPlusOneDocumentIssueWithoutDegrading(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	issues := make(map[string]string, MaxNonBundledMCPServers+1)
	for i := 0; i < MaxNonBundledMCPServers; i++ {
		issues[fmt.Sprintf("vendor-%03d", i)] = "stdio/command MCP servers are unsupported; run the server in a container or use an HTTPS URL"
	}
	issues["_document"] = "reimport mcp.json failed"
	if len(issues) != MaxNonBundledMCPServers+1 {
		t.Fatalf("test setup built %d issues, want exactly %d (%d named entries + one _document entry)", len(issues), MaxNonBundledMCPServers+1, MaxNonBundledMCPServers)
	}

	if err := repo.ReplaceMCPImportIssues(ctx, issues); err != nil {
		t.Fatalf("persisting the maximum legitimate issue set must succeed: %v", err)
	}

	snapshot, err := repo.MCPRegistrySnapshot(ctx)
	if err != nil {
		t.Fatalf("MCPRegistrySnapshot must succeed for the maximum legitimate issue set: %v", err)
	}
	if snapshot.IssuesOverCap {
		t.Fatal("IssuesOverCap = true, want false: 256 named entries plus one _document entry is a normal, bounded outcome, not a degraded one")
	}
	if snapshot.OverBudget || snapshot.ServersOverCap {
		t.Fatalf("OverBudget = %v, ServersOverCap = %v, want both false: only the issue count is exercised here", snapshot.OverBudget, snapshot.ServersOverCap)
	}
	if len(snapshot.Issues) != MaxNonBundledMCPServers+1 {
		t.Fatalf("len(Issues) = %d, want all %d persisted issues returned, none truncated", len(snapshot.Issues), MaxNonBundledMCPServers+1)
	}
	found := false
	for _, issue := range snapshot.Issues {
		if issue.Name == "_document" {
			found = true
		}
	}
	if !found {
		t.Fatal("the _document issue is missing from the snapshot")
	}
}
