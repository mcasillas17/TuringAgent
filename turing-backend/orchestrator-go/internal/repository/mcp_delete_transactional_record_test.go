package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestDeleteMCPServerReturnsTheExactDeletedRecord proves DeleteMCPServer
// returns the deleted row's own fields, captured from inside the same
// transaction as the tier check, tombstone insert, and delete itself —
// so a caller (the service's DeleteMcpServer) never needs its own
// separate pre-read of a row this call is about to remove.
func TestDeleteMCPServerReturnsTheExactDeletedRecord(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	created, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := repo.DeleteMCPServer(ctx, created.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ID != created.Server.ID {
		t.Fatalf("deleted.ID = %q, want %q", deleted.ID, created.Server.ID)
	}
	if deleted.Name != "vendor" {
		t.Fatalf("deleted.Name = %q, want vendor", deleted.Name)
	}
	if deleted.URL != "https://vendor.example/mcp" {
		t.Fatalf("deleted.URL = %q, want https://vendor.example/mcp", deleted.URL)
	}
	if deleted.Tier != MCPServerTierRemoteURL {
		t.Fatalf("deleted.Tier = %q, want remote_url", deleted.Tier)
	}
}

// A missing server_id must still map to ErrMCPServerNotFound and return a
// zero-value record — unchanged from before this fix.
func TestDeleteMCPServerNotFoundReturnsZeroRecord(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	deleted, err := repo.DeleteMCPServer(ctx, "mcp_missing")
	if !errors.Is(err, ErrMCPServerNotFound) {
		t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
	}
	if !isZeroMCPServerRecord(deleted) {
		t.Fatalf("deleted = %+v, want the zero value on a not-found error", deleted)
	}
}

// A bundled server must still refuse deletion with ErrMCPServerBundled and
// a zero-value record, and must remain completely untouched.
func TestDeleteMCPServerBundledReturnsZeroRecordAndLeavesRowUntouched(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	bundled, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "bundled-vendor", URL: "https://bundled.example/mcp", Tier: MCPServerTierBundled,
	})
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := repo.DeleteMCPServer(ctx, bundled.Server.ID)
	if !errors.Is(err, ErrMCPServerBundled) {
		t.Fatalf("err = %v, want ErrMCPServerBundled", err)
	}
	if !isZeroMCPServerRecord(deleted) {
		t.Fatalf("deleted = %+v, want the zero value on a bundled refusal", deleted)
	}
	if _, err := repo.GetMCPServerByName(ctx, "bundled-vendor"); err != nil {
		t.Fatalf("bundled server must still be registered: %v", err)
	}
}

// TestConcurrentDeleteMCPServerCallsExactlyOneSucceedsWithTheRealRecord
// races two DeleteMCPServer calls for the same server id against each
// other: because the tier check, tombstone insert, and delete now all
// happen inside DeleteMCPServer's own single transaction (rather than an
// outer pre-read that could observe a stale snapshot before a separate
// transaction actually removes the row), exactly one of the two must
// actually see the row and delete it — returning its real, pre-delete
// field values — and the other must see the row already gone
// (ErrMCPServerNotFound) with a zero-value record, never both succeeding
// and never a torn/zero record misreported as a success.
func TestConcurrentDeleteMCPServerCallsExactlyOneSucceedsWithTheRealRecord(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	created, err := repo.RegisterMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	const attempts = 2
	var wg sync.WaitGroup
	results := make([]MCPServerRecord, attempts)
	errs := make([]error, attempts)
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = repo.DeleteMCPServer(ctx, created.Server.ID)
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	for i := 0; i < attempts; i++ {
		switch {
		case errs[i] == nil:
			successes++
			if results[i].ID != created.Server.ID || results[i].Name != "vendor" {
				t.Fatalf("successful delete #%d returned record = %+v, want the real pre-delete row", i, results[i])
			}
		case errors.Is(errs[i], ErrMCPServerNotFound):
			if !isZeroMCPServerRecord(results[i]) {
				t.Fatalf("delete #%d failed with ErrMCPServerNotFound but returned a non-zero record: %+v", i, results[i])
			}
		default:
			t.Fatalf("delete #%d unexpected error: %v", i, errs[i])
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want exactly 1 of the %d concurrent deletes to succeed", successes, attempts)
	}
}

// isZeroMCPServerRecord reports whether record is the zero MCPServerRecord.
// MCPServerRecord embeds a []byte field (SealedToken), so Go's == operator
// cannot compare it directly; this checks the fields a refusal path could
// ever actually populate.
func isZeroMCPServerRecord(record MCPServerRecord) bool {
	return record.ID == "" && record.Name == "" && record.Transport == "" &&
		record.URL == "" && len(record.SealedToken) == 0 && record.Tier == "" &&
		!record.Enabled && record.Status == "" && record.StatusError == "" && record.CreatedAt == ""
}
