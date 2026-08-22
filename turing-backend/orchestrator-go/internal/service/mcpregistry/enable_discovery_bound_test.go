package mcpregistry

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// cancelOnFirstRoundTripTransport cancels the given context the instant the
// first HTTP request for it starts, then blocks until that cancellation is
// observable on the request's own context — simulating a client that
// cancels while a discovery request is in flight, without any real network
// activity or real-time sleep.
type cancelOnFirstRoundTripTransport struct {
	cancel context.CancelFunc
	once   sync.Once
	calls  int32
}

func (t *cancelOnFirstRoundTripTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&t.calls, 1)
	t.once.Do(t.cancel)
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// respondThenCancelTransport lets a real request complete normally (so
// discover() gets a real, valid tools/list response) and only cancels the
// given context once that response has been returned — simulating a client
// that cancels after the network round trip finishes but before discover()
// finishes committing what it fetched (schema validation, RecordDiscovery,
// its own "up" status write).
type respondThenCancelTransport struct {
	transport http.RoundTripper
	cancel    context.CancelFunc
}

func (t *respondThenCancelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.transport.RoundTrip(req)
	t.cancel()
	return resp, err
}

// TestSetMcpServerEnabledCancellationDuringDiscoveryStillNotifiesAndAudits
// proves cancellation while a discovery HTTP request is in flight cannot
// suppress the post-commit notify/audit: the enable itself already
// committed before discovery ever ran, and the fallback "down" status write
// (using a context detached from the now-cancelled ctx) must still succeed.
func TestSetMcpServerEnabledCancellationDuringDiscoveryStillNotifiesAndAudits(t *testing.T) {
	service, repo := newRegistryTestService(t)
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "remote", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	transport := &cancelOnFirstRoundTripTransport{cancel: cancel}
	service.httpClient = &http.Client{Transport: transport}

	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	// The RPC's own response may legitimately fail: by the time the final
	// read-back for the response runs, ctx is already cancelled. That is
	// unrelated to what this test proves.
	_, _ = service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if ctx.Err() == nil {
		t.Fatal("test setup failed: the transport never cancelled the request context")
	}
	if atomic.LoadInt32(&transport.calls) == 0 {
		t.Fatal("test setup failed: discovery never made an HTTP request")
	}

	updated, err := repo.GetMCPServer(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled {
		t.Fatal("the enable must have committed despite cancellation during discovery")
	}

	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 despite cancellation during discovery", notifier.calls)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want exactly one despite cancellation during discovery", recorder.records)
	}
	record := recorder.records[0]
	if record.action != "mcp.server.enabled" {
		t.Fatalf("action = %q, want mcp.server.enabled", record.action)
	}
	onlyExpectedAuditKeys(t, record.payload, "name", "tier", "remoteDiscoveryAttempted", "discoverySucceeded")
	if record.payload["discoverySucceeded"] != false {
		t.Fatalf("payload discoverySucceeded = %v, want false: the request was cancelled", record.payload["discoverySucceeded"])
	}
}

// TestSetMcpServerEnabledCancellationAfterDiscoveryRoundTripStillNotifiesAndAudits
// proves the same invariant for cancellation that lands after the network
// round trip already returned a valid, real response but before discover()
// finishes committing it (RecordDiscovery, its own "up" status write): the
// enable must still show up as committed, and notify/audit must still run,
// with the fallback "down" status write again using a context detached
// from the now-cancelled ctx.
func TestSetMcpServerEnabledCancellationAfterDiscoveryRoundTripStillNotifiesAndAudits(t *testing.T) {
	service, repo := newRegistryTestService(t)
	vendor := toolsListVendor(t)
	ctx, cancel := context.WithCancel(context.Background())
	service.httpClient = &http.Client{Transport: &respondThenCancelTransport{
		transport: vendor.Client().Transport,
		cancel:    cancel,
	}}
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "remote", URL: vendor.URL, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	_, _ = service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if ctx.Err() == nil {
		t.Fatal("test setup failed: the transport never cancelled the request context")
	}

	updated, err := repo.GetMCPServer(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled {
		t.Fatal("the enable must have committed despite cancellation after the discovery round trip")
	}

	if notifier.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 despite cancellation after the discovery round trip", notifier.calls)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want exactly one despite cancellation after the discovery round trip", recorder.records)
	}
	if recorder.records[0].action != "mcp.server.enabled" {
		t.Fatalf("action = %q, want mcp.server.enabled", recorder.records[0].action)
	}
}

// blockUntilContextDoneTransport never responds on its own: it blocks every
// request until that request's own context is done, then reports that
// context's error. It stands in for a vendor that would otherwise hold a
// tools/list page open for a long time, without this test ever actually
// waiting that long.
type blockUntilContextDoneTransport struct {
	calls int32
}

func (t *blockUntilContextDoneTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&t.calls, 1)
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// TestSetMcpServerEnabledBoundsWholeDiscoveryByASingleContextTimeout proves
// enableDiscoveryTimeout bounds the *entire* enable-time discovery
// operation (every tools/list page), not a fresh budget re-armed per
// request/page: a transport that would otherwise block forever is only
// ever invoked once and observes cancellation almost immediately, because
// SetMcpServerEnabled derives discover()'s context from a single
// context.WithTimeout call wrapping the whole operation. The caller
// context here carries no deadline of its own — enableDiscoveryTimeout
// itself must be what bounds the blocking transport — and
// enableDiscoveryTimeoutOverride substitutes a short duration for the real
// 30s constant so this test proves the wrapper without ever sleeping in
// real time; the same mechanism is what stops a page-hungry vendor from
// being able to force up to maxMCPToolPages times the per-request HTTP
// timeout.
func TestSetMcpServerEnabledBoundsWholeDiscoveryByASingleContextTimeout(t *testing.T) {
	service, repo := newRegistryTestService(t)
	transport := &blockUntilContextDoneTransport{}
	service.httpClient = &http.Client{Transport: transport}
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "remote", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The caller context deliberately carries no deadline of its own
	// (context.Background()): what must bound this blocking transport is
	// SetMcpServerEnabled's own enableDiscoveryTimeout wrapper, not
	// anything the caller supplied. enableDiscoveryTimeoutOverride is a
	// test-only seam that stands in for the real 30s constant so this
	// test does not have to wait out the real value (or its absence) in
	// real time to prove the wrapper is what stops the operation.
	service.enableDiscoveryTimeoutOverride = 20 * time.Millisecond

	start := time.Now()
	_, _ = service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("elapsed = %v, want well under enableDiscoveryTimeout (30s): the whole "+
			"multi-page discovery operation must be bounded by a single context "+
			"timeout, not a budget re-armed per request/page", elapsed)
	}
	if atomic.LoadInt32(&transport.calls) != 1 {
		t.Fatalf("transport calls = %d, want exactly 1: the operation must stop at the "+
			"first blocked request rather than starting another page", transport.calls)
	}

	updated, err := repo.GetMCPServer(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled {
		t.Fatal("the enable must have committed even though discovery never got a response")
	}
	status, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range status {
		if candidate.ID != server.ID {
			continue
		}
		found = true
		if candidate.Status != "down" {
			t.Fatalf("status = %q, want down: the timed-out discovery attempt must still be recorded", candidate.Status)
		}
	}
	if !found {
		t.Fatal("server missing from ListMCPServers")
	}
}
