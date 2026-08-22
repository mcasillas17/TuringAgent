package repository

import (
	"bytes"
	"context"
	"testing"
)

// A successful token replace/clear must reset liveness to unknown with an
// empty status message: a prior Up/Down observation was made using the old
// credential and says nothing about whether the new (or absent) one works.
func TestReplaceMCPServerTokenResetsLivenessToUnknown(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	created, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerStatus(ctx, created.Server.ID, "up", ""); err != nil {
		t.Fatal(err)
	}

	updated, err := repo.ReplaceMCPServerToken(ctx, created.Server.ID, []byte("sealed-rotated"))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "unknown" || updated.StatusError != "" {
		t.Fatalf("status = %q error = %q, want unknown/empty after a token rotation", updated.Status, updated.StatusError)
	}
}

// Clearing the token (an empty sealedToken) must reset liveness the same
// way a rotation to a new token does.
func TestReplaceMCPServerTokenClearAlsoResetsLivenessToUnknown(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	created, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReplaceMCPServerToken(ctx, created.Server.ID, []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerStatus(ctx, created.Server.ID, "down", "connection refused"); err != nil {
		t.Fatal(err)
	}

	cleared, err := repo.ReplaceMCPServerToken(ctx, created.Server.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Status != "unknown" || cleared.StatusError != "" {
		t.Fatalf("status = %q error = %q, want unknown/empty after clearing the token", cleared.Status, cleared.StatusError)
	}
}

// Repeated rotations and clears must each keep working and keep resetting
// liveness, regardless of how many times it has already happened.
func TestReplaceMCPServerTokenRepeatedRotationsAndClearsKeepWorking(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	created, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, sealed := range [][]byte{[]byte("first"), nil, []byte("second"), nil, []byte("third")} {
		if err := repo.SetMCPServerStatus(ctx, created.Server.ID, "up", ""); err != nil {
			t.Fatalf("round %d: seed status: %v", i, err)
		}
		updated, err := repo.ReplaceMCPServerToken(ctx, created.Server.ID, sealed)
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if updated.Status != "unknown" || updated.StatusError != "" {
			t.Fatalf("round %d: status = %q error = %q, want unknown/empty", i, updated.Status, updated.StatusError)
		}
		wantEmpty := len(sealed) == 0
		gotEmpty := len(updated.SealedToken) == 0
		if wantEmpty != gotEmpty || (!wantEmpty && !bytes.Equal(updated.SealedToken, sealed)) {
			t.Fatalf("round %d: SealedToken = %q, want %q", i, updated.SealedToken, sealed)
		}
	}
}

// A failure updating the status half of the transaction (simulated here by
// deleting the status row out from under it, so the UPDATE matches zero
// rows) must roll back the token change too, rather than leaving a rotated
// token paired with a stale liveness reading from the credential it just
// replaced.
func TestReplaceMCPServerTokenFailureRollsBackTokenAndStatusTogether(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	created, err := repo.ImportMCPServer(ctx, ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerStatus(ctx, created.Server.ID, "up", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `DELETE FROM mcp_server_status WHERE mcp_server_id = ?`, created.Server.ID); err != nil {
		t.Fatal(err)
	}

	_, err = repo.ReplaceMCPServerToken(ctx, created.Server.ID, []byte("sealed-should-not-persist"))
	if err == nil {
		t.Fatal("ReplaceMCPServerToken succeeded despite the missing status row; want the whole transaction to fail")
	}

	fetched, err := repo.GetMCPServer(ctx, created.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.SealedToken) != 0 {
		t.Fatalf("SealedToken = %q, want the token update rolled back alongside the failed status update", fetched.SealedToken)
	}
}
