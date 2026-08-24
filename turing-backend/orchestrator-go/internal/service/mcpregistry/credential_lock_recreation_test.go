package mcpregistry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestForgetCredentialLockIfCurrentOnlyRemovesMatchingLockObject is the
// unit-level proof for the compare-and-delete helper itself, independent
// of any concurrency: it must remove serverID's entry only when the
// caller's own lock object is still the one installed in the map, never
// a different object some other goroutine has since installed for the
// same id, and it must be a safe no-op for a key with no entry at all.
func TestForgetCredentialLockIfCurrentOnlyRemovesMatchingLockObject(t *testing.T) {
	service, _ := newRegistryTestService(t)
	lockA := service.credentialLock("server-a")
	lockB := &sync.RWMutex{} // a distinct object, deliberately never installed

	service.forgetCredentialLockIfCurrent("server-a", lockB)
	if _, ok := service.credentialLocks["server-a"]; !ok {
		t.Fatal("forgetCredentialLockIfCurrent removed the entry despite a mismatched lock object")
	}

	service.forgetCredentialLockIfCurrent("server-a", lockA)
	if _, ok := service.credentialLocks["server-a"]; ok {
		t.Fatal("forgetCredentialLockIfCurrent did not remove the entry despite a matching lock object")
	}

	// A no-op for a key that was never present at all — must not panic.
	service.forgetCredentialLockIfCurrent("server-never-existed", lockA)
}

// TestRotateMcpServerTokenCleansUpCredentialLockWhenServerDeletedMidRotation
// proves rotateServerTokenLocked cleans up its own server's credentialLocks
// entry when its post-lock re-read discovers the row is gone — closing
// what would otherwise be a permanent leak: the server row here is
// deleted directly through the repository (bypassing the service's own
// DeleteMcpServer/forgetCredentialLock), simulating some other actor
// removing the row while a rotation is already mid-flight holding the
// lock. Nothing else will ever call forgetCredentialLock for this id
// again, so if rotateServerTokenLocked did not clean up after itself here,
// this entry would remain in credentialLocks forever.
func TestRotateMcpServerTokenCleansUpCredentialLockWhenServerDeletedMidRotation(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "https://vendor.example/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	baseline := len(service.credentialLocks)

	reached := make(chan struct{})
	proceed := make(chan struct{})
	var once sync.Once
	service.rotateBarrier = func() {
		once.Do(func() { close(reached) })
		<-proceed
	}

	rotateDone := make(chan error, 1)
	go func() {
		_, err := service.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
			ServerId: server.Server.ID, BearerToken: "irrelevant-token",
		})
		rotateDone <- err
	}()

	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("rotation never reached its barrier")
	}
	if _, ok := service.credentialLocks[server.Server.ID]; !ok {
		t.Fatal("test setup is broken: rotation must have created a lock entry by the time it reaches its barrier")
	}

	if _, err := repo.DeleteMCPServer(ctx, server.Server.ID); err != nil {
		t.Fatal(err)
	}
	close(proceed)

	err = <-rotateDone
	if status.Code(err) != codes.NotFound {
		t.Fatalf("rotate error code = %v, want NotFound (the row disappeared mid-rotation)", status.Code(err))
	}
	if got := len(service.credentialLocks); got != baseline {
		t.Fatalf("credentialLocks size = %d, want back to baseline %d: rotation must clean up its own server's entry once it discovers the row is gone", got, baseline)
	}
}

// TestCallToolCleansUpCredentialLockWhenServerDeletedBeforeLockAcquired is
// the precise "recreation leak" reproduction: CallTool's own credentialLock
// call is what (re)creates a server's lock entry after
// DeleteMcpServer has already forgotten it (there was no entry at all
// before this call started, so DeleteMcpServer's own forget is a no-op) —
// and CallTool must clean up the entry it just (re)created once its own
// post-lock re-read discovers the server no longer exists, or that entry
// would leak forever: DeleteMcpServer only ever forgets a server's lock
// once, on its own successful delete, and that has already happened by
// the time CallTool even reaches credentialLock here.
func TestCallToolCleansUpCredentialLockWhenServerDeletedBeforeLockAcquired(t *testing.T) {
	h := newRegistryCallHarness(t)
	ctx := context.Background()
	if err := h.repo.SetMCPToolPolicy(ctx, h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}
	runID := h.runningToolCall(t, "call_lock_recreate", map[string]any{"path": "x"})

	baseline := len(h.registry.credentialLocks)
	if _, ok := h.registry.credentialLocks[h.serverID]; ok {
		t.Fatal("test setup is broken: a lock entry already exists for this server")
	}

	reached := make(chan struct{})
	proceed := make(chan struct{})
	var once sync.Once
	h.registry.callCredentialLockBarrier = func() {
		once.Do(func() { close(reached) })
		<-proceed
	}

	callDone := make(chan error, 1)
	go func() {
		_, err := h.registry.CallTool(ctx, CallInput{
			ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: map[string]any{"path": "x"},
		})
		callDone <- err
	}()

	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("CallTool never reached its pre-credential-lock barrier")
	}
	if _, ok := h.registry.credentialLocks[h.serverID]; ok {
		t.Fatal("test setup is broken: CallTool must not have created a lock entry yet at this barrier")
	}

	if _, err := h.registry.DeleteMcpServer(ctx, &turingv1.DeleteMcpServerRequest{ServerId: h.serverID}); err != nil {
		t.Fatal(err)
	}
	if got := len(h.registry.credentialLocks); got != baseline {
		t.Fatalf("credentialLocks size right after delete = %d, want unchanged at %d (no entry existed for the delete to forget)", got, baseline)
	}

	close(proceed)

	if err := <-callDone; err == nil {
		t.Fatal("CallTool must fail: the server was deleted before it ever read the token")
	} else if !errors.Is(err, repository.ErrMCPServerNotFound) {
		t.Fatalf("CallTool error = %v, want ErrMCPServerNotFound", err)
	}

	if got := len(h.registry.credentialLocks); got != baseline {
		t.Fatalf("credentialLocks size after CallTool finished = %d, want back to baseline %d: "+
			"the entry CallTool's own credentialLock call (re)created after the delete must not leak", got, baseline)
	}
}

// TestDiscoverCleansUpCredentialLockWhenServerDeletedBeforeLockAcquired is
// discoverLocked's own counterpart to the CallTool test above: enabling a
// server triggers discoverLocked, whose first action is credentialLock —
// if the server is deleted between SetMcpServerEnabled's own precheck and
// that call, discoverLocked's credentialLock call is what (re)creates the
// entry, and discoverLocked must clean it back up once discover's own
// post-lock re-read finds the row gone.
func TestDiscoverCleansUpCredentialLockWhenServerDeletedBeforeLockAcquired(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	vendor := toolsListVendor(t)
	server, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "remote", URL: vendor.URL, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	baseline := len(service.credentialLocks)
	if _, ok := service.credentialLocks[server.Server.ID]; ok {
		t.Fatal("test setup is broken: a lock entry already exists for this server")
	}

	reached := make(chan struct{})
	proceed := make(chan struct{})
	var once sync.Once
	service.discoverCredentialLockBarrier = func() {
		once.Do(func() { close(reached) })
		<-proceed
	}

	enableDone := make(chan error, 1)
	go func() {
		_, err := service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
			ServerId: server.Server.ID, Enabled: true,
		})
		enableDone <- err
	}()

	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("discoverLocked never reached its pre-credential-lock barrier")
	}
	if _, ok := service.credentialLocks[server.Server.ID]; ok {
		t.Fatal("test setup is broken: discoverLocked must not have created a lock entry yet at this barrier")
	}

	if _, err := service.DeleteMcpServer(ctx, &turingv1.DeleteMcpServerRequest{ServerId: server.Server.ID}); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	<-enableDone

	if got := len(service.credentialLocks); got != baseline {
		t.Fatalf("credentialLocks size after enable finished = %d, want back to baseline %d: "+
			"the entry discoverLocked's own credentialLock call (re)created after the delete must not leak", got, baseline)
	}
}

// TestConcurrentCallToolRotateAndDeleteAcrossServersRace is a broader
// stress/race test, extending cross_server_fence_test.go's own
// TestConcurrentCallToolAndRotateAcrossTwoServersRace with a concurrent
// DeleteMcpServer thrown into the same mix: many concurrent CallTool and
// RotateMcpServerToken calls against a surviving server ("vendor"), and
// against a second server ("vendor2") that is simultaneously being
// rotated and deleted, must neither deadlock nor (under `go test -race`)
// reveal a data race, and once everything settles, vendor2's
// credentialLocks entry must be gone — regardless of how the many
// concurrent CallTool/RotateMcpServerToken calls against it happened to
// interleave with the delete that removed it.
func TestConcurrentCallToolRotateAndDeleteAcrossServersRace(t *testing.T) {
	h := newRegistryCallHarness(t)
	ctx := context.Background()
	if err := h.repo.SetMCPToolPolicy(ctx, h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}

	sealed2, err := h.registry.sealServerToken("vendor2", "vendor2-token")
	if err != nil {
		t.Fatal(err)
	}
	server2, err := h.repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor2", URL: "http://vendor2:9000/mcp", SealedToken: sealed2, Tier: repository.MCPServerTierLocalContainer,
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

	const callCount = 8
	runIDsA := make([]string, callCount)
	runIDsB := make([]string, 4)
	for i := range runIDsA {
		runIDsA[i] = h.runningToolCall(t, fmt.Sprintf("race_del_a_%d", i), map[string]any{"path": "x"})
	}
	for i := range runIDsB {
		runIDsB[i] = h.runningToolCall(t, fmt.Sprintf("race_del_b_%d", i), map[string]any{"path": "y"})
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
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = h.registry.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
				ServerId: h.serverID, BearerToken: fmt.Sprintf("race-token-a-%d", i),
			})
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = h.registry.RotateMcpServerToken(ctx, &turingv1.RotateMcpServerTokenRequest{
				ServerId: server2.Server.ID, BearerToken: fmt.Sprintf("race-token-b-%d", i),
			})
		}(i)
	}
	for _, runID := range runIDsB {
		wg.Add(1)
		go func(runID string) {
			defer wg.Done()
			_, _ = h.registry.CallTool(ctx, CallInput{
				ServerID: server2.Server.ID, RunID: runID,
				ToolName: "vendor2.write", Args: map[string]any{"path": "y"},
			})
		}(runID)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = h.registry.DeleteMcpServer(ctx, &turingv1.DeleteMcpServerRequest{ServerId: server2.Server.ID})
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("concurrent CallTool/RotateMcpServerToken/DeleteMcpServer calls deadlocked")
	}

	if _, err := h.repo.GetMCPServerByName(ctx, "vendor2"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: vendor2 must end up deleted", err)
	}
	// Every goroutine above has already finished (received from done, a
	// happens-before edge), so reading credentialLocks directly here,
	// with no concurrent writer left, is race-free.
	if _, ok := h.registry.credentialLocks[server2.Server.ID]; ok {
		t.Fatal("credentialLocks still has an entry for the deleted vendor2 after the race settled")
	}
	if _, ok := h.registry.credentialLocks[h.serverID]; !ok {
		t.Fatal("credentialLocks lost its entry for the surviving vendor server")
	}
}
