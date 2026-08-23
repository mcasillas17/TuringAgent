package mcpregistry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// blockUntilReleasedTransport captures every request's Authorization
// header (in order) and blocks the very first RoundTrip call until
// release is closed, then delegates to inner. Every subsequent call sees
// release already closed and proceeds immediately. This is what lets a
// test observe "the network call has started" (via started) and hold it
// open for as long as needed to prove a concurrent RotateMcpServerToken
// blocks behind it.
type blockUntilReleasedTransport struct {
	inner   http.RoundTripper
	started chan struct{}
	release chan struct{}

	startedOnce sync.Once
	mu          sync.Mutex
	authHeaders []string
}

func (t *blockUntilReleasedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.authHeaders = append(t.authHeaders, req.Header.Get("authorization"))
	t.mu.Unlock()
	t.startedOnce.Do(func() { close(t.started) })
	<-t.release
	return t.inner.RoundTrip(req)
}

func (t *blockUntilReleasedTransport) authAt(index int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if index < 0 || index >= len(t.authHeaders) {
		return ""
	}
	return t.authHeaders[index]
}

// requireNotDone fails the test if channel ch is already readable —
// proving a goroutine waiting to signal completion on ch has not (yet)
// completed, rather than merely "completed within some window."
func requireNotYetDone(t *testing.T, ch <-chan error, wait time.Duration, message string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal(message)
	case <-time.After(wait):
	}
}

func requireDoneSoon(t *testing.T, ch <-chan error, wait time.Duration, message string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(wait):
		t.Fatal(message)
		return nil
	}
}

// A CallTool already using the server's current token to talk to the
// vendor must fully finish — network round trip and its own liveness
// status write — before a concurrent RotateMcpServerToken can proceed: no
// in-flight call may still be using a token that a rotation is in the
// process of replacing, and no rotation may reset liveness before an
// already-in-flight observation made under the old token has finished
// being recorded. Once the rotation *does* complete, the very next call
// must use the newly rotated token, never a stale one read before
// rotation.
func TestCallToolCredentialFenceBlocksRotationUntilInFlightCallFinishes(t *testing.T) {
	h := newRegistryCallHarness(t)
	ctx := context.Background()
	if err := h.repo.SetMCPToolPolicy(ctx, h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}
	transport := &blockUntilReleasedTransport{
		inner:   h.vendor.Client().Transport,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	h.registry.httpClient = &http.Client{Transport: transport}

	var logged bytes.Buffer
	previousLog := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previousLog) })

	runID := h.runningToolCall(t, "call_fence_old", map[string]any{"path": "x"})
	callDone := make(chan error, 1)
	go func() {
		_, err := h.registry.CallTool(ctx, CallInput{
			ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: map[string]any{"path": "x"},
		})
		callDone <- err
	}()

	select {
	case <-transport.started:
	case <-time.After(5 * time.Second):
		t.Fatal("CallTool never reached the network call")
	}

	rotateDone := make(chan error, 1)
	go func() {
		_, err := h.registry.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
			ServerId: h.serverID, BearerToken: "vendor-token-rotated",
		})
		rotateDone <- err
	}()
	requireNotYetDone(t, rotateDone, 200*time.Millisecond,
		"RotateMcpServerToken completed while CallTool was still using the old token; want it blocked")

	close(transport.release)

	if err := requireDoneSoon(t, callDone, 5*time.Second, "CallTool never finished after being unblocked"); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if err := requireDoneSoon(t, rotateDone, 5*time.Second,
		"RotateMcpServerToken never finished after CallTool released the credential lock"); err != nil {
		t.Fatalf("RotateMcpServerToken: %v", err)
	}

	if got := transport.authAt(0); got != "Bearer vendor-token" {
		t.Fatalf("in-flight call authorization = %q, want the old token", got)
	}

	server, err := h.repo.GetMCPServer(ctx, h.serverID)
	if err != nil {
		t.Fatal(err)
	}
	if server.Status != "unknown" {
		t.Fatalf("status = %q, want unknown: rotation's reset must be the last write, never overwritten "+
			"back by the old call's own status write after rotation returns", server.Status)
	}

	runID2 := h.runningToolCall(t, "call_fence_new", map[string]any{"path": "y"})
	if _, err := h.registry.CallTool(ctx, CallInput{
		ServerID: h.serverID, RunID: runID2, ToolName: "vendor.write", Args: map[string]any{"path": "y"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := transport.authAt(1); got != "Bearer vendor-token-rotated" {
		t.Fatalf("post-rotation call authorization = %q, want the newly rotated token", got)
	}

	if bytes.Contains(logged.Bytes(), []byte("vendor-token")) {
		t.Fatalf("process log carries a bearer token: %s", logged.String())
	}
}

// The same fence must hold for discover() (invoked by SetMcpServerEnabled),
// not just CallTool: an in-flight discovery using the old token must
// finish before a concurrent rotation proceeds, and a rediscovery
// triggered after rotation must use the new token.
func TestDiscoverCredentialFenceBlocksRotationUntilInFlightDiscoveryFinishes(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	vendor := toolsListVendor(t)
	transport := &blockUntilReleasedTransport{
		inner:   vendor.Client().Transport,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	service.httpClient = &http.Client{Transport: transport}

	sealed, err := service.sealServerToken("remote", "vendor-token")
	if err != nil {
		t.Fatal(err)
	}
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "remote", URL: vendor.URL, SealedToken: sealed, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	enableDone := make(chan error, 1)
	go func() {
		_, err := service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
			ServerId: server.Server.ID, Enabled: true,
		})
		enableDone <- err
	}()

	select {
	case <-transport.started:
	case <-time.After(5 * time.Second):
		t.Fatal("discovery never reached the network call")
	}

	rotateDone := make(chan error, 1)
	go func() {
		_, err := service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
			ServerId: server.Server.ID, BearerToken: "vendor-token-rotated",
		})
		rotateDone <- err
	}()
	requireNotYetDone(t, rotateDone, 200*time.Millisecond,
		"RotateMcpServerToken completed while discovery was still using the old token; want it blocked")

	close(transport.release)

	if err := requireDoneSoon(t, enableDone, 5*time.Second, "SetMcpServerEnabled never finished after being unblocked"); err != nil {
		t.Fatal(err)
	}
	if err := requireDoneSoon(t, rotateDone, 5*time.Second,
		"RotateMcpServerToken never finished after discovery released the credential lock"); err != nil {
		t.Fatal(err)
	}

	if got := transport.authAt(0); got != "Bearer vendor-token" {
		t.Fatalf("in-flight discovery authorization = %q, want the old token", got)
	}
	current, err := repo.GetMCPServer(ctx, server.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "unknown" {
		t.Fatalf("status = %q, want unknown: rotation's reset must be the final write", current.Status)
	}

	if _, err := service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.Server.ID, Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.Server.ID, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if got := transport.authAt(1); got != "Bearer vendor-token-rotated" {
		t.Fatalf("post-rotation discovery authorization = %q, want the newly rotated token", got)
	}
}

// The reverse direction: a rotation that is itself mid-flight (holding the
// credential write lock) must block a concurrent CallTool from proceeding
// until the rotation finishes — proven with a test-only barrier hook
// (mirroring reimportBarrier) since RotateMcpServerToken's own repository
// work is otherwise too fast to reliably race against.
func TestRotateMcpServerTokenBlocksConcurrentCallToolViaBarrier(t *testing.T) {
	h := newRegistryCallHarness(t)
	ctx := context.Background()
	if err := h.repo.SetMCPToolPolicy(ctx, h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}
	runID := h.runningToolCall(t, "call_fence_reverse", map[string]any{"path": "x"})

	reached := make(chan struct{})
	proceed := make(chan struct{})
	var barrierOnce sync.Once
	h.registry.rotateBarrier = func() {
		barrierOnce.Do(func() { close(reached) })
		<-proceed
	}

	rotateDone := make(chan error, 1)
	go func() {
		_, err := h.registry.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
			ServerId: h.serverID, BearerToken: "vendor-token-rotated",
		})
		rotateDone <- err
	}()

	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("rotation never reached its barrier")
	}

	callDone := make(chan error, 1)
	go func() {
		_, err := h.registry.CallTool(ctx, CallInput{
			ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: map[string]any{"path": "x"},
		})
		callDone <- err
	}()
	requireNotYetDone(t, callDone, 200*time.Millisecond,
		"CallTool completed while a rotation held the credential lock; want it blocked")

	close(proceed)

	if err := requireDoneSoon(t, rotateDone, 5*time.Second, "rotation never finished after its barrier was released"); err != nil {
		t.Fatal(err)
	}
	if err := requireDoneSoon(t, callDone, 5*time.Second,
		"CallTool never finished after the rotation released the credential lock"); err != nil {
		t.Fatal(err)
	}

	if got := h.authorization.Load(); got != "Bearer vendor-token-rotated" {
		t.Fatalf("call authorization = %v, want the newly rotated token", got)
	}
}

// blockingApprovalEnforcer signals entered the instant it is invoked and
// then blocks until proceed is closed, returning err. It stands in for
// caller-side approval enforcement that can legitimately take an
// unbounded amount of time (waiting on a human), used to prove CallTool
// does not acquire its credential lock until after this step, however
// long it takes.
type blockingApprovalEnforcer struct {
	entered chan struct{}
	proceed chan struct{}
	err     error
}

func (b *blockingApprovalEnforcer) ConsumeApprovalForThirdParty(
	context.Context, string, string, string, string, string, map[string]any,
) error {
	close(b.entered)
	<-b.proceed
	return b.err
}

// CallTool must not hold the credential lock while waiting on
// caller-side approval enforcement — only from when it actually reads the
// token. This proves it two ways: a concurrent RotateMcpServerToken
// completes promptly while CallTool is stuck waiting for approval (rather
// than deadlocking behind a lock CallTool has no reason to hold yet), and
// CallTool making no HTTP request at all here (it is refused before ever
// reaching the network call) shows the credential critical section was
// never entered in the first place.
func TestCallToolDoesNotHoldCredentialLockDuringApprovalEnforcement(t *testing.T) {
	h := newRegistryCallHarness(t)
	ctx := context.Background()
	// vendor.write keeps its default approval_required policy from
	// RecordDiscovery's DefaultPolicyFor.
	runID := h.runningToolCall(t, "call_fence_no_early_lock", map[string]any{"path": "x"})

	enforcer := &blockingApprovalEnforcer{
		entered: make(chan struct{}),
		proceed: make(chan struct{}),
		err:     errors.New("still not approved"),
	}
	h.registry.SetApprovalEnforcer(enforcer)

	callDone := make(chan error, 1)
	go func() {
		_, err := h.registry.CallTool(ctx, CallInput{
			ServerID: h.serverID, RunID: runID, ApprovalID: "irrelevant", ToolName: "vendor.write", Args: map[string]any{"path": "x"},
		})
		callDone <- err
	}()

	select {
	case <-enforcer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("CallTool never reached approval enforcement")
	}

	rotateDone := make(chan error, 1)
	go func() {
		_, err := h.registry.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
			ServerId: h.serverID, BearerToken: "vendor-token-rotated",
		})
		rotateDone <- err
	}()
	if err := requireDoneSoon(t, rotateDone, 2*time.Second,
		"RotateMcpServerToken blocked despite CallTool not having read the token yet: "+
			"the credential lock must not be held during approval enforcement"); err != nil {
		t.Fatal(err)
	}

	close(enforcer.proceed)
	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("want an error: the fake approval enforcer always refuses")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CallTool never finished")
	}
	if h.reached.Load() != 0 {
		t.Fatal("CallTool must not have reached the vendor: it was refused at the approval step")
	}
}

// A stress/race test: many concurrent CallTool and RotateMcpServerToken
// calls against the same server must neither deadlock nor (under
// `go test -race`) reveal a data race on the credential path.
func TestConcurrentCallToolAndRotateMcpServerTokenRace(t *testing.T) {
	h := newRegistryCallHarness(t)
	ctx := context.Background()
	if err := h.repo.SetMCPToolPolicy(ctx, h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}
	const callCount = 8
	runIDs := make([]string, callCount)
	for i := range runIDs {
		runIDs[i] = h.runningToolCall(t, fmt.Sprintf("call_race_%d", i), map[string]any{"path": "x"})
	}

	var wg sync.WaitGroup
	for _, runID := range runIDs {
		wg.Add(1)
		go func(runID string) {
			defer wg.Done()
			_, _ = h.registry.CallTool(ctx, CallInput{
				ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: map[string]any{"path": "x"},
			})
		}(runID)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = h.registry.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
				ServerId: h.serverID, BearerToken: fmt.Sprintf("race-token-%d", i),
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
		t.Fatal("concurrent CallTool/RotateMcpServerToken calls deadlocked")
	}
}
