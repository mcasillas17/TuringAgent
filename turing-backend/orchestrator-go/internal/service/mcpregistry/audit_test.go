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
	// afterRecord, if set, runs synchronously once the row above has been
	// captured, before Record returns. It lets a test simulate a
	// side effect of the audit write becoming visible to whatever the
	// caller does next with its own context (e.g. cancelling it), without
	// that side effect being able to erase or race the already-recorded
	// row.
	afterRecord func()
}

type auditCall struct {
	action  string
	target  string
	payload map[string]any
}

func (r *recordingAuditRecorder) Record(_ context.Context, _ string, _ string, _ string, action string, target string, payload map[string]any) error {
	r.mu.Lock()
	r.records = append(r.records, auditCall{action: action, target: target, payload: payload})
	fail := r.fail
	after := r.afterRecord
	r.mu.Unlock()
	if after != nil {
		after()
	}
	return fail
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

// ctxLivenessAuditRecorder records, for every call, whether the context it
// was handed was still live (not yet Done) at the moment of the call. It
// exists to catch auditMCPEvent forwarding a context that a post-commit side
// effect (the registry-change notifier) already cancelled, which would make
// the audit write racy or outright fail depending on the backing store.
type ctxLivenessAuditRecorder struct {
	mu      sync.Mutex
	calls   int
	sawLive bool
}

func (r *ctxLivenessAuditRecorder) Record(ctx context.Context, _ string, _ string, _ string, _ string, _ string, _ map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if ctx.Err() == nil {
		r.sawLive = true
	}
	return nil
}

// cancelingRegistryChangeNotifier simulates a post-commit side effect (the
// runtime registry-change notification) that ends up cancelling the
// original client request context — the realistic way a long-lived gRPC
// request context can already be Done by the time auditMCPEvent runs, if
// auditMCPEvent naively reused it instead of deriving a detached one.
type cancelingRegistryChangeNotifier struct {
	cancel context.CancelFunc
}

func (n *cancelingRegistryChangeNotifier) NotifyMCPRegistryChanged(context.Context) error {
	n.cancel()
	return nil
}

// The post-commit audit write is best-effort but must not be sabotaged by
// whatever happens to the original request context after the repository
// mutation lands. This is the discriminating case: a registry-change
// notifier that cancels the very context RegisterMcpServer was called
// with, invoked (like the real runtime notifier) after the mutation is
// already committed. auditMCPEvent must still hand the recorder a live
// context and the row must still be recorded — client cancellation must
// never race the durable audit write of a change that already happened.
func TestAuditContextIsDetachedFromClientCancellationAfterCommit(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	recorder := &ctxLivenessAuditRecorder{}
	service.SetAuditRecorder(recorder)
	service.SetRegistryChangeNotifier(&cancelingRegistryChangeNotifier{cancel: cancel})

	// The RPC's own outcome is not the point of this test: once the
	// notifier cancels the client's context, any later use of that same
	// context (such as re-reading the server to build a descriptor) may
	// legitimately fail — that is the client's own cancellation catching
	// up with it, not a bug. What must hold regardless is that the
	// mutation already committed and the audit write already succeeded
	// with a live context before that happens.
	_, _ = service.RegisterMcpServer(ctx, &turingv1.RegisterMcpServerRequest{
		Name: "vendor", Url: "https://vendor.example/mcp", Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	})

	if ctx.Err() == nil {
		t.Fatal("test setup failed: the notifier never cancelled the original request context")
	}
	if recorder.calls != 1 {
		t.Fatalf("audit calls = %d, want exactly 1", recorder.calls)
	}
	if !recorder.sawLive {
		t.Fatal("auditMCPEvent handed the recorder an already-cancelled context; it must derive a detached, bounded context instead")
	}

	stored, err := repo.GetMCPServerByName(context.Background(), "vendor")
	if err != nil {
		t.Fatalf("the repository mutation must have committed regardless of client cancellation: %v", err)
	}
	if stored.Name != "vendor" {
		t.Fatalf("stored server = %+v, want name=vendor", stored)
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
