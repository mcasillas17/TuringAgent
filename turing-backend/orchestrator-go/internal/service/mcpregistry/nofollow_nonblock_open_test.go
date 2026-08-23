package mcpregistry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"golang.org/x/sys/unix"
)

// TestReimportConfiguredJSONSwapToFIFORightBeforeOpenNeverBlocks is the
// deterministic swap-race proof for the NOFOLLOW/NONBLOCK finding: a
// prior os.Lstat-then-plain-os.Open pair leaves a TOCTOU gap between the
// two syscalls — a regular file at Lstat time could be replaced with a
// FIFO before os.Open (which has no O_NONBLOCK of its own) is reached, and
// a FIFO's read-side open blocks indefinitely until a writer connects.
// beforeConfigFileOpen (test-only, nil in production) fires at exactly
// the point ReimportConfiguredJSON commits to opening mcp.json, letting
// this test win that race deterministically instead of hoping a genuine
// timing race lands in the right window: it swaps a real, valid mcp.json
// for a FIFO at that exact moment, with no writer ever provided. If the
// implementation still opened the path with a plain, blocking os.Open,
// this goroutine would hang forever; using O_NONBLOCK at the raw open(2)
// syscall itself is what makes that structurally impossible rather than
// merely unlikely.
func TestReimportConfiguredJSONSwapToFIFORightBeforeOpenNeverBlocks(t *testing.T) {
	root := t.TempDir()
	// A pre-flight capability probe, synchronous on this (the test's own)
	// goroutine, so an environment where FIFOs are unavailable skips
	// cleanly here rather than the swap hook below (which runs on a
	// spawned goroutine, where t.Skip/t.Fatal would be unsafe to call)
	// silently swallowing the same failure.
	preflight := filepath.Join(root, "preflight-fifo")
	if err := unix.Mkfifo(preflight, 0o600); err != nil {
		t.Skipf("FIFOs are unavailable in this environment: %v", err)
	}
	if err := os.Remove(preflight); err != nil {
		t.Fatal(err)
	}

	service, repo := newRegistryTestService(t)
	path := filepath.Join(root, "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers": {"vendor": {"url": "https://vendor.example/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	var swapErr error
	service.beforeConfigFileOpen = func(p string) {
		if err := os.Remove(p); err != nil {
			swapErr = err
			return
		}
		if err := unix.Mkfifo(p, 0o600); err != nil {
			swapErr = err
		}
	}

	type result struct {
		report ImportReport
		err    error
	}
	done := make(chan result, 1)
	go func() {
		report, err := service.ReimportConfiguredJSON(context.Background())
		done <- result{report: report, err: err}
	}()

	select {
	case res := <-done:
		// swapErr is only ever written on the goroutine above, and only
		// ever read here, after that goroutine has already sent on done
		// — the channel receive establishes a happens-before edge, so
		// this read is race-free.
		if swapErr != nil {
			t.Fatalf("test setup failed to swap mcp.json for a FIFO: %v", swapErr)
		}
		if res.err == nil {
			t.Fatal("a FIFO swapped in immediately before open must be reported as a read failure, not silently succeed")
		}
		if res.err.Error() != "read mcp.json failed" {
			t.Fatalf("error = %q, want the fixed read-failure message", res.err.Error())
		}
		if len(res.report.Imported) != 0 || len(res.report.Skipped) != 0 || len(res.report.Unsupported) != 0 {
			t.Fatalf("report = %+v, want empty on a read failure", res.report)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReimportConfiguredJSON did not return within 3s: swapping mcp.json to a FIFO immediately before open must never block")
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Tier != repository.MCPServerTierBundled {
			t.Fatalf("servers = %+v, want none created from a swapped-to-FIFO file", servers)
		}
	}
}

// TestReimportConfiguredJSONSwapToSymlinkRightBeforeOpenIsRefused is the
// symlink counterpart: a regular file at decision time, replaced with a
// symlink (pointing at an otherwise perfectly valid mcp.json) at the exact
// moment ReimportConfiguredJSON opens it. O_NOFOLLOW makes the raw open(2)
// itself fail rather than silently resolving the link — no separate
// Lstat-then-Open ordering to race in the first place, so this proves the
// new implementation refuses the swap deterministically rather than
// happening to observe the pre-swap file.
func TestReimportConfiguredJSONSwapToSymlinkRightBeforeOpenIsRefused(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real-mcp.json")
	if err := os.WriteFile(target, []byte(`{"mcpServers": {"attacker": {"url": "https://attacker.example/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Pre-flight probe on this goroutine, mirroring the FIFO test above.
	preflightLink := filepath.Join(root, "preflight-link")
	if err := os.Symlink(target, preflightLink); err != nil {
		t.Skipf("symlinks are unavailable in this environment: %v", err)
	}
	if err := os.Remove(preflightLink); err != nil {
		t.Fatal(err)
	}

	service, repo := newRegistryTestService(t)
	path := filepath.Join(root, "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers": {"vendor": {"url": "https://vendor.example/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	service.beforeConfigFileOpen = func(p string) {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
	}

	report, err := service.ReimportConfiguredJSON(context.Background())
	if err == nil {
		t.Fatal("a symlink swapped in immediately before open must be reported as a read failure, not silently followed")
	}
	if err.Error() != "read mcp.json failed" {
		t.Fatalf("error = %q, want the fixed read-failure message", err.Error())
	}
	if len(report.Imported) != 0 || len(report.Skipped) != 0 || len(report.Unsupported) != 0 {
		t.Fatalf("report = %+v, want empty on a read failure", report)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Name == "attacker" {
			t.Fatal("a symlink swapped in immediately before open must never be followed/imported")
		}
	}
}

// TestOpenRegularMCPConfigFileRejectsDeviceNodeWithoutBlocking is a
// unit-level structural proof, independent of any swap timing: opening
// /dev/null (a character device that is always instantly readable, so
// this can never itself hang regardless of what the implementation does)
// through the same helper ReimportConfiguredJSON uses must still refuse
// it as non-regular, rather than treating "opened successfully" as
// synonymous with "is a regular file."
func TestOpenRegularMCPConfigFileRejectsDeviceNodeWithoutBlocking(t *testing.T) {
	if _, err := os.Stat("/dev/null"); err != nil {
		t.Skipf("/dev/null is unavailable in this environment: %v", err)
	}
	file, err := openRegularMCPConfigFile("/dev/null")
	if err == nil {
		_ = file.Close()
		t.Fatal("/dev/null must be refused as non-regular, not opened for reading")
	}
	if strings.Contains(err.Error(), "/dev/null") {
		t.Fatalf("err = %v, must not repeat the path", err)
	}
}
