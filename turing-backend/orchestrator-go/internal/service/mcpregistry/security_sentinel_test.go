package mcpregistry

import (
	"bytes"
	"context"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"google.golang.org/protobuf/encoding/protojson"
)

// A sentinel improbable enough that finding it anywhere means it leaked from
// this test's own registration/rotation calls.
const mcpTokenSentinel = "mcp-registry-sentinel-3f9a6c2e8b1d5074-do-not-leak"

// The one test to keep if every other one in this package were deleted: a
// bearer token given to RegisterMcpServer and then to RotateMcpServerToken
// must never reach a response, an audit row, an event, a returned error, or
// the log — only the sealed column may hold it.
func TestBearerTokenNeverLeaksAcrossRegisterAndRotate(t *testing.T) {
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
	auditService := audit.New(repo)
	service.SetAuditRecorder(auditService)
	ctx := context.Background()

	registered, err := service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name:        "vendor",
		Url:         "https://vendor.example/mcp",
		Tier:        turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: mcpTokenSentinel,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	assertNoSentinel(t, "register response", registered)

	rotatedSentinel := mcpTokenSentinel + "-rotated"
	rotated, err := service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
		ServerId:    registered.GetServerId(),
		BearerToken: rotatedSentinel,
	})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	assertNoSentinel(t, "rotate response", rotated)

	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount == 0 {
		t.Fatal("no audit rows were written, so this check proves nothing about them")
	}

	rows, err := database.QueryContext(ctx, `SELECT payload_json FROM audit_logs`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		assertStringSentinelFree(t, "audit payload", payload, mcpTokenSentinel, rotatedSentinel)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	var eventCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		eventRows, err := database.QueryContext(ctx, `SELECT payload_json FROM events`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = eventRows.Close() }()
		for eventRows.Next() {
			var payload string
			if err := eventRows.Scan(&payload); err != nil {
				t.Fatal(err)
			}
			assertStringSentinelFree(t, "event payload", payload, mcpTokenSentinel, rotatedSentinel)
		}
	}
}

func assertNoSentinel(t *testing.T, what string, descriptor *turingv1.McpServerDescriptor) {
	t.Helper()
	encoded, err := protojson.Marshal(descriptor)
	if err != nil {
		t.Fatalf("marshal %s: %v", what, err)
	}
	assertStringSentinelFree(t, what, string(encoded), mcpTokenSentinel, mcpTokenSentinel+"-rotated")
}

func assertStringSentinelFree(t *testing.T, what string, haystack string, sentinels ...string) {
	t.Helper()
	for _, sentinel := range sentinels {
		if strings.Contains(haystack, sentinel) {
			t.Fatalf("%s carries the sentinel token: %s", what, haystack)
		}
		// A long substantial substring is as bad as the whole thing.
		if len(sentinel) > 16 && strings.Contains(haystack, sentinel[:16]) {
			t.Fatalf("%s carries a substantial substring of the sentinel token: %s", what, haystack)
		}
	}
}
