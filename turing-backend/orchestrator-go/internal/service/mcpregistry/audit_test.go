package mcpregistry

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// recordingAuditRecorder captures every call so a test can inspect the
// action, target, and payload without a real audit service.
type recordingAuditRecorder struct {
	mu      sync.Mutex
	records []auditCall
	fail    error
}

type auditCall struct {
	action  string
	target  string
	payload map[string]any
}

func (r *recordingAuditRecorder) Record(_ context.Context, _ string, _ string, _ string, action string, target string, payload map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, auditCall{action: action, target: target, payload: payload})
	return r.fail
}

func TestRegisterMcpServerIsAuditedWithNameAndTierOnly(t *testing.T) {
	service, _ := newRegistryTestService(t)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	descriptor, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: "vendor-secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want exactly one", recorder.records)
	}
	record := recorder.records[0]
	if record.action != "mcp.server.registered" {
		t.Fatalf("action = %q, want mcp.server.registered", record.action)
	}
	if record.target != descriptor.GetServerId() {
		t.Fatalf("target = %q, want the server id", record.target)
	}
	if record.payload["name"] != "vendor" {
		t.Fatalf("payload = %+v, want name=vendor", record.payload)
	}
	for key, value := range record.payload {
		if key != "name" && key != "tier" && key != "url" {
			t.Fatalf("payload has unexpected key %q=%v", key, value)
		}
		if s, ok := value.(string); ok && strings.Contains(s, "vendor-secret-token") {
			t.Fatalf("payload carries the bearer token: %+v", record.payload)
		}
	}
}

func TestRotateMcpServerTokenIsAuditedAsRotatedOrCleared(t *testing.T) {
	service, repo := newRegistryTestService(t)
	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: server.ID, BearerToken: "vendor-secret-token",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: server.ID, BearerToken: "",
	}); err != nil {
		t.Fatal(err)
	}

	if len(recorder.records) != 2 {
		t.Fatalf("records = %+v, want two", recorder.records)
	}
	if recorder.records[0].action != "mcp.server.token_rotated" {
		t.Fatalf("first action = %q, want mcp.server.token_rotated", recorder.records[0].action)
	}
	if recorder.records[0].payload["tokenConfigured"] != true {
		t.Fatalf("first payload = %+v, want tokenConfigured=true", recorder.records[0].payload)
	}
	if recorder.records[1].action != "mcp.server.token_cleared" {
		t.Fatalf("second action = %q, want mcp.server.token_cleared", recorder.records[1].action)
	}
	if recorder.records[1].payload["tokenConfigured"] != false {
		t.Fatalf("second payload = %+v, want tokenConfigured=false", recorder.records[1].payload)
	}
	for _, record := range recorder.records {
		for key := range record.payload {
			if key != "name" && key != "tokenConfigured" {
				t.Fatalf("rotate payload has unexpected key %q", key)
			}
		}
	}
}

// An audit failure must not fail the mutation that already happened, and
// whatever gets logged about the failure must never contain the token —
// even when the dependency's own returned error is the thing carrying the
// token, which is the realistic way a token could leak through
// auditMCPEvent's error handling if it ever logged err.Error() instead of
// just the action/target. This exercises both a register and a
// rotate/clear so both auditMCPEvent call sites are covered.
func TestAuditFailureDoesNotFailMutationAndStaysTokenFree(t *testing.T) {
	service, _ := newRegistryTestService(t)

	const token = "vendor-secret-should-never-be-logged-0123456789ab"
	const rotatedToken = token + "-rotated"
	recorder := &recordingAuditRecorder{
		fail: errors.New("audit sink rejected token " + token + " and " + rotatedToken),
	}
	service.SetAuditRecorder(recorder)

	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	// Restored via Cleanup (run after every assertion below), not an
	// early defer/restore, so the buffer captured here still reflects
	// every log line by the time it is inspected.
	t.Cleanup(func() { log.SetOutput(previous) })

	descriptor, err := service.RegisterMcpServer(context.Background(), &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
		BearerToken: token,
	})
	if err != nil {
		t.Fatalf("an audit failure must not fail registration: %v", err)
	}
	if descriptor.GetName() != "vendor" {
		t.Fatalf("descriptor = %+v, want the registered server despite the audit failure", descriptor)
	}

	rotated, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: descriptor.GetServerId(), BearerToken: rotatedToken,
	})
	if err != nil {
		t.Fatalf("an audit failure must not fail rotation: %v", err)
	}
	if rotated.GetServerId() != descriptor.GetServerId() {
		t.Fatalf("rotated = %+v, want the same server despite the audit failure", rotated)
	}

	cleared, err := service.RotateMcpServerToken(context.Background(), &turingv1.RotateMcpServerTokenRequest{
		ServerId: descriptor.GetServerId(), BearerToken: "",
	})
	if err != nil {
		t.Fatalf("an audit failure must not fail clearing the token: %v", err)
	}
	if cleared.GetServerId() != descriptor.GetServerId() {
		t.Fatalf("cleared = %+v, want the same server despite the audit failure", cleared)
	}

	if len(recorder.records) != 3 {
		t.Fatalf("records = %+v, want three attempted audit calls (register, rotate, clear) despite each failing", recorder.records)
	}

	assertStringSentinelFree(t, "audit failure log", logged.String(), token, rotatedToken)
}
