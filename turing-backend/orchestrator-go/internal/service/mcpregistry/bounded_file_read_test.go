package mcpregistry

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"golang.org/x/sys/unix"
)

// mcp.json must never hang open on a FIFO: a plain, blocking os.Open on a
// FIFO's read side waits until a writer connects, and this test
// deliberately never provides one — if ReimportConfiguredJSON's open call
// blocked the way a plain os.Open would, this goroutine would hang
// forever. openRegularMCPConfigFile instead calls unix.Open with
// O_NONBLOCK, so the open itself returns immediately regardless of
// whether a writer is connected (rather than a separate check refusing
// before any open is attempted); the resulting descriptor is then
// Fstat-ed, and only a confirmed regular file is accepted — a FIFO's
// S_IFIFO mode fails that check and is refused as
// errMCPConfigNotRegularFile.
func TestReimportConfiguredJSONRefusesFIFOWithoutBlockingOnAWriter(t *testing.T) {
	service, repo := newRegistryTestService(t)
	root := t.TempDir()
	path := filepath.Join(root, "mcp.json")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Skipf("FIFOs are unavailable in this environment: %v", err)
	}
	service.SetMCPConfigRoot(root)

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
		if res.err == nil {
			t.Fatal("a FIFO in place of mcp.json must be reported as a read failure, not silently succeed")
		}
		if res.err.Error() != "read mcp.json failed" {
			t.Fatalf("error = %q, want the fixed read-failure message", res.err.Error())
		}
		if len(res.report.Imported) != 0 || len(res.report.Skipped) != 0 || len(res.report.Unsupported) != 0 {
			t.Fatalf("report = %+v, want empty on a read failure", res.report)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReimportConfiguredJSON did not return within 3s: opening a FIFO with O_NONBLOCK must return immediately (never block waiting for a writer that will never come), so the fstat-confirmed non-regular refusal that follows must not be delayed either")
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Tier != repository.MCPServerTierBundled {
			t.Fatalf("servers = %+v, want none created from a refused FIFO", servers)
		}
	}
}

// A Unix domain socket at mcp.json's path must be refused the same fixed
// way as a FIFO or a directory: it is exactly as non-regular, and opening
// it for a read (rather than connecting to it) is meaningless.
func TestReimportConfiguredJSONRefusesUnixSocket(t *testing.T) {
	service, repo := newRegistryTestService(t)
	root := t.TempDir()
	path := filepath.Join(root, "mcp.json")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("Unix domain sockets are unavailable in this environment: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	service.SetMCPConfigRoot(root)

	report, err := service.ReimportConfiguredJSON(context.Background())
	if err == nil {
		t.Fatal("a socket in place of mcp.json must be reported as a read failure, not silently succeed")
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
		if server.Tier != repository.MCPServerTierBundled {
			t.Fatalf("servers = %+v, want none created from a refused socket", servers)
		}
	}
}

// A symlink at mcp.json's path is refused the same way, even when it
// points at an otherwise perfectly valid mcp.json elsewhere:
// openRegularMCPConfigFile opens the path with O_NOFOLLOW, which makes the
// raw open(2) syscall itself fail with ELOOP when the final path component
// is a symlink — the link is never resolved or followed, and no separate
// Lstat call is involved at all — the simplest and safest choice
// documented in openRegularMCPConfigFile's own comment, since resolving it
// would mean deciding how far to follow a chain of links (and defending
// against one that never terminates, or one that points back at a
// FIFO/socket/device) for a file this process has no deployed need to
// read through a symlink at all.
func TestReimportConfiguredJSONRefusesSymlinkEvenWhenTargetIsValid(t *testing.T) {
	service, repo := newRegistryTestService(t)
	root := t.TempDir()
	target := filepath.Join(root, "real-mcp.json")
	if err := os.WriteFile(target, []byte(`{"mcpServers": {"vendor": {"url": "https://vendor.example/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "mcp.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are unavailable in this environment: %v", err)
	}
	service.SetMCPConfigRoot(root)

	report, err := service.ReimportConfiguredJSON(context.Background())
	if err == nil {
		t.Fatal("a symlink in place of mcp.json must be reported as a read failure, not silently succeed")
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
		if server.Name == "vendor" {
			t.Fatal("a symlinked mcp.json must never be followed/imported")
		}
	}
}

// ReimportConfiguredJSON must never read the whole mcp.json file into
// memory before deciding whether it fits within maxMCPImportDocumentBytes:
// os.ReadFile has no size bound at all, so an arbitrarily large file on
// disk (whether genuinely huge or a sparse file that merely claims to be)
// would force this process to allocate an unbounded amount of memory
// before ImportJSON's own in-memory size check (which only runs on
// whatever bytes already got read) ever has a chance to refuse it. This
// creates a sparse file far larger than the cap — its logical size is set
// via Truncate rather than by writing real bytes, so creating it is fast
// regardless of how large it claims to be — and requires the whole
// operation to still complete quickly and refuse boundedly, which could
// only happen if the read itself is bounded rather than reading (or
// buffering) the entire file first.
func TestReimportConfiguredJSONRefusesSparseHugeFileWithoutReadingItAll(t *testing.T) {
	service, repo := newRegistryTestService(t)
	root := t.TempDir()
	path := filepath.Join(root, "mcp.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Far larger than maxMCPImportDocumentBytes (32 MiB): 4 GiB, entirely
	// sparse (no real disk blocks allocated for the zero-filled region).
	const hugeSize = 4 << 30
	if err := file.Truncate(hugeSize); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	start := time.Now()
	report, err := service.ReimportConfiguredJSON(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("an oversized document must be reported, not returned as an error: %v", err)
	}
	// A bounded read (at most maxMCPImportDocumentBytes+1 bytes) completes
	// in well under a second even on a slow CI runner; reading the whole
	// 4 GiB sparse file (or worse, allocating a same-sized buffer for it)
	// would take vastly longer or exhaust memory. This is a coarse,
	// generous bound specifically to avoid flaking on a loaded runner —
	// its purpose is only to distinguish "bounded" from "read everything."
	if elapsed > 10*time.Second {
		t.Fatalf("ReimportConfiguredJSON took %s, want it bounded by maxMCPImportDocumentBytes rather than reading the whole file", elapsed)
	}
	if len(report.Imported) != 0 || len(report.Skipped) != 0 {
		t.Fatalf("report = %+v, want no imports or skips for an oversized document", report)
	}
	reason, refused := report.Unsupported["_document"]
	if !refused {
		t.Fatalf("Unsupported = %v, want a _document refusal", report.Unsupported)
	}
	if strings.Contains(reason, root) || strings.Contains(reason, path) {
		t.Fatalf("reason = %q, must not leak the config root path", reason)
	}
	if len(reason) > maxMCPStatusMessageBytes {
		t.Fatalf("reason is %d bytes, want it bounded by maxMCPStatusMessageBytes (%d)", len(reason), maxMCPStatusMessageBytes)
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Tier != repository.MCPServerTierBundled {
			t.Fatalf("servers = %+v, want no non-bundled server created from an oversized document", servers)
		}
	}
	issues, err := repo.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Name != "_document" {
		t.Fatalf("issues = %+v, want exactly one bounded _document issue", issues)
	}
}

// The file-read bound itself must not be off-by-one: a file whose exact
// byte size equals maxMCPImportDocumentBytes must still be read in full
// and handed to ImportJSON, not refused at the file-read layer before
// ImportJSON's own (already-tested) at-cap boundary ever gets a chance to
// accept it.
func TestReimportConfiguredJSONFileAtExactCapStillImports(t *testing.T) {
	service, _ := newRegistryTestService(t)
	root := t.TempDir()

	base := []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`)
	padding := maxMCPImportDocumentBytes - len(base)
	if padding < 0 {
		t.Fatalf("test setup is broken: base document (%d bytes) already exceeds maxMCPImportDocumentBytes (%d)", len(base), maxMCPImportDocumentBytes)
	}
	document := append(bytes.Repeat([]byte(" "), padding), base...)
	if len(document) != maxMCPImportDocumentBytes {
		t.Fatalf("test setup is broken: document = %d bytes, want exactly maxMCPImportDocumentBytes (%d)", len(document), maxMCPImportDocumentBytes)
	}
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), document, 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	report, err := service.ReimportConfiguredJSON(context.Background())
	if err != nil {
		t.Fatalf("a file of exactly maxMCPImportDocumentBytes must not be refused: %v", err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]", report.Imported)
	}
}

// A file one byte over the cap must be refused the same bounded way the
// sparse-huge-file test above proves for a much larger file — this is the
// precise boundary case, written as real (non-sparse) bytes since it is
// small enough (32 MiB + 1) not to matter for test speed.
func TestReimportConfiguredJSONFileOneByteOverCapIsRefused(t *testing.T) {
	service, repo := newRegistryTestService(t)
	root := t.TempDir()

	oversized := bytes.Repeat([]byte(" "), maxMCPImportDocumentBytes+1)
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	report, err := service.ReimportConfiguredJSON(context.Background())
	if err != nil {
		t.Fatalf("an oversized document must be reported, not returned as an error: %v", err)
	}
	reason, refused := report.Unsupported["_document"]
	if !refused {
		t.Fatalf("Unsupported = %v, want a _document refusal", report.Unsupported)
	}
	if !strings.Contains(reason, fmt.Sprintf("%d", maxMCPImportDocumentBytes)) {
		t.Fatalf("reason = %q, want it to name the maxMCPImportDocumentBytes limit", reason)
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Tier != repository.MCPServerTierBundled {
			t.Fatalf("servers = %+v, want no non-bundled server created from an oversized document", servers)
		}
	}
}
