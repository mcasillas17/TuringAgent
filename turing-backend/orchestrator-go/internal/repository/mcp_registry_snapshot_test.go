package repository

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
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

// TestMCPRegistrySnapshotRefusesPreexistingOverBudgetAggregate proves the
// named-error guard: a database that already carries an aggregate over
// MaxMCPRegistryToolBytes (simulated here via a direct row insert,
// bypassing every repository write path that would itself refuse such a
// state) must be refused with ErrMCPRegistryToolBudgetExceeded rather than
// silently returning an oversized snapshot.
func TestMCPRegistrySnapshotRefusesPreexistingOverBudgetAggregate(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	oversizedSchema := `{"type":"object","d":"` + strings.Repeat("x", MaxMCPRegistryToolBytes) + `"}`
	if _, err := database.ExecContext(ctx, `
		INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at, mcp_server_id, present)
		VALUES ('tool_test_oversized', 'skills', 'skills.oversized', 'safe', ?, 1, datetime('now'), NULL, 1)
	`, oversizedSchema); err != nil {
		t.Fatal(err)
	}

	_, err := repo.MCPRegistrySnapshot(ctx)
	if !errors.Is(err, ErrMCPRegistryToolBudgetExceeded) {
		t.Fatalf("err = %v, want ErrMCPRegistryToolBudgetExceeded for a preexisting oversized aggregate", err)
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
		toolOfExactRawSize(t, "a", MaxMCPRegistryToolBytes-100),
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
