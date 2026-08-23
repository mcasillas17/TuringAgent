package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// perHostBlockTransport blocks only requests whose Host matches
// blockedHost until release is closed; every other request (including one
// to a different host entirely) proceeds immediately through inner
// without ever waiting on release. This is what lets a test prove that a
// per-server credential lock held by one server's in-flight call never
// blocks a completely different server's own call or rotation — unlike
// blockUntilReleasedTransport (used by the same-server fence tests in
// rotation_fence_test.go), which blocks every request regardless of
// destination and so cannot, by itself, distinguish "blocked because of
// this specific server" from "blocked because of the shared transport."
type perHostBlockTransport struct {
	inner       http.RoundTripper
	blockedHost string
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
}

func (t *perHostBlockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == t.blockedHost {
		t.startedOnce.Do(func() { close(t.started) })
		<-t.release
	}
	return t.inner.RoundTrip(req)
}

// A CallTool blocked mid-flight against one server must never block a
// concurrent RotateMcpServerToken or CallTool against a *different*
// server: the credential fence is keyed by server id (see
// (*Server).credentialLock), not a single process-global lock. Before
// that change, a single sync.RWMutex shared by every server meant a
// rotation or call for server B would queue up behind whatever server A
// happened to be doing, for no correctness reason at all.
func TestCredentialFenceDoesNotBlockRotationOrCallForADifferentServer(t *testing.T) {
	h := newRegistryCallHarness(t)
	ctx := context.Background()
	if err := h.repo.SetMCPToolPolicy(ctx, h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}

	// A second, independent server under the *same* registry instance,
	// reached over plain HTTP so the composite transport below needs no
	// special TLS trust for it (h.vendor is already TLS-backed, and the
	// blocking transport must still handle both).
	vendor2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"content": []any{}},
		})
	}))
	t.Cleanup(vendor2.Close)
	sealed2, err := h.registry.sealServerToken("vendor2", "vendor2-token")
	if err != nil {
		t.Fatal(err)
	}
	server2, err := h.repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor2", URL: vendor2.URL, SealedToken: sealed2, Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetMCPServerEnabled(ctx, server2.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.registry.RecordDiscovery(ctx, server2.Server.ID, []DiscoveredTool{{
		Name: "vendor2.write", SchemaJSON: `{"type":"object"}`,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetMCPToolPolicy(ctx, server2.Server.ID, "vendor2.write", "safe"); err != nil {
		t.Fatal(err)
	}

	blockedURL, err := url.Parse(h.vendor.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := &perHostBlockTransport{
		inner:       h.vendor.Client().Transport,
		blockedHost: blockedURL.Host,
		started:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	h.registry.httpClient = &http.Client{Transport: transport}

	runIDA := h.runningToolCall(t, "cross_server_call_a", map[string]any{"path": "x"})
	callADone := make(chan error, 1)
	go func() {
		_, err := h.registry.CallTool(ctx, CallInput{
			ServerID: h.serverID, RunID: runIDA, ToolName: "vendor.write", Args: map[string]any{"path": "x"},
		})
		callADone <- err
	}()

	select {
	case <-transport.started:
	case <-time.After(5 * time.Second):
		t.Fatal("CallTool against server A never reached the network call")
	}
	requireNotYetDone(t, callADone, 200*time.Millisecond,
		"CallTool against server A completed before being released; want it still blocked")

	// While A's call is still blocked, both a rotation and a call against
	// the completely different server B must complete promptly.
	rotateBDone := make(chan error, 1)
	go func() {
		_, err := h.registry.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
			ServerId: server2.Server.ID, BearerToken: "vendor2-token-rotated",
		})
		rotateBDone <- err
	}()
	if err := requireDoneSoon(t, rotateBDone, 3*time.Second,
		"RotateMcpServerToken for server B never finished while server A's call was blocked: "+
			"the credential fence must not be shared across servers"); err != nil {
		t.Fatalf("rotate B: %v", err)
	}

	runIDB := h.runningToolCall(t, "cross_server_call_b", map[string]any{"path": "y"})
	callBDone := make(chan error, 1)
	go func() {
		_, err := h.registry.CallTool(ctx, CallInput{
			ServerID: server2.Server.ID, RunID: runIDB, ToolName: "vendor2.write", Args: map[string]any{"path": "y"},
		})
		callBDone <- err
	}()
	if err := requireDoneSoon(t, callBDone, 3*time.Second,
		"CallTool for server B never finished while server A's call was blocked: "+
			"the credential fence must not be shared across servers"); err != nil {
		t.Fatalf("call B: %v", err)
	}

	// Now release A's call and confirm it still completes correctly.
	close(transport.release)
	if err := requireDoneSoon(t, callADone, 5*time.Second, "CallTool against server A never finished after being released"); err != nil {
		t.Fatalf("call A: %v", err)
	}
}

// A stress/race test across two distinct servers: many concurrent
// CallTool and RotateMcpServerToken calls against server A and server B
// simultaneously must neither deadlock (proving the two servers' locks
// are genuinely independent, not one masquerading as a no-op) nor reveal
// a data race under `go test -race`.
func TestConcurrentCallToolAndRotateAcrossTwoServersRace(t *testing.T) {
	h := newRegistryCallHarness(t)
	ctx := context.Background()
	if err := h.repo.SetMCPToolPolicy(ctx, h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}

	vendor2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"content": []any{}},
		})
	}))
	t.Cleanup(vendor2.Close)
	sealed2, err := h.registry.sealServerToken("vendor2", "vendor2-token")
	if err != nil {
		t.Fatal(err)
	}
	server2, err := h.repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor2", URL: vendor2.URL, SealedToken: sealed2, Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetMCPServerEnabled(ctx, server2.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.registry.RecordDiscovery(ctx, server2.Server.ID, []DiscoveredTool{{
		Name: "vendor2.write", SchemaJSON: `{"type":"object"}`,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetMCPToolPolicy(ctx, server2.Server.ID, "vendor2.write", "safe"); err != nil {
		t.Fatal(err)
	}
	// vendor2's requests must go through h.registry's shared httpClient
	// override too (set by newRegistryCallHarness to vendor.Client(), a
	// TLS client bound to h.vendor's own certificate); vendor2 is a
	// plain-HTTP httptest server, which the same *http.Transport can
	// still reach without any TLS handshake at all.

	const callCount = 8
	runIDsA := make([]string, callCount)
	runIDsB := make([]string, callCount)
	for i := range runIDsA {
		runIDsA[i] = h.runningToolCall(t, fmt.Sprintf("race_a_%d", i), map[string]any{"path": "x"})
		runIDsB[i] = h.runningToolCall(t, fmt.Sprintf("race_b_%d", i), map[string]any{"path": "y"})
	}

	var wg sync.WaitGroup
	for _, runID := range runIDsA {
		wg.Add(1)
		go func(runID string) {
			defer wg.Done()
			_, _ = h.registry.CallTool(ctx, CallInput{
				ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: map[string]any{"path": "x"},
			})
		}(runID)
	}
	for _, runID := range runIDsB {
		wg.Add(1)
		go func(runID string) {
			defer wg.Done()
			_, _ = h.registry.CallTool(ctx, CallInput{
				ServerID: server2.Server.ID, RunID: runID, ToolName: "vendor2.write", Args: map[string]any{"path": "y"},
			})
		}(runID)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = h.registry.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
				ServerId: h.serverID, BearerToken: fmt.Sprintf("race-token-a-%d", i),
			})
		}(i)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = h.registry.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
				ServerId: server2.Server.ID, BearerToken: fmt.Sprintf("race-token-b-%d", i),
			})
		}(i)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("concurrent CallTool/RotateMcpServerToken calls across two servers deadlocked")
	}
}

// RotateMcpServerToken must not grow the per-server credential lock map
// for a server id that never existed: a caller hammering the RPC with
// arbitrary, never-valid ids must not be able to leak lock entries
// unboundedly, unlike a real server's lock (removed only on that server's
// own successful delete).
func TestRotateMcpServerTokenForNonExistentServerDoesNotLeakCredentialLock(t *testing.T) {
	service, _ := newRegistryTestService(t)
	ctx := context.Background()

	before := len(service.credentialLocks)
	for i := 0; i < 5; i++ {
		_, err := service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
			ServerId: fmt.Sprintf("mcp_never_existed_%d", i), BearerToken: "irrelevant",
		})
		if err == nil {
			t.Fatal("want an error rotating a server id that was never registered")
		}
	}
	after := len(service.credentialLocks)
	if after != before {
		t.Fatalf("credentialLocks grew from %d to %d entries after rotating 5 never-valid ids; want unchanged", before, after)
	}
}

// Deleting a server removes its credential lock map entry, so the map's
// steady-state size tracks the registry's own row count rather than every
// distinct server id ever created over a long-running process's lifetime.
func TestDeleteMcpServerForgetsItsCredentialLock(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Force a lock entry to exist for this server, the same way a real
	// CallTool/discover/rotate would.
	_ = service.credentialLock(server.Server.ID)
	if _, ok := service.credentialLocks[server.Server.ID]; !ok {
		t.Fatal("test setup is broken: no lock entry was created")
	}

	if _, err := service.DeleteMcpServer(ctx, &turingv1.DeleteMcpServerRequest{
		ServerId: server.Server.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := service.credentialLocks[server.Server.ID]; ok {
		t.Fatal("credentialLocks still has an entry for a deleted server")
	}
}
