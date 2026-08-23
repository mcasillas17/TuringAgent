package mcpregistry

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"golang.org/x/sys/unix"
)

// This is the decisive proof that the file read itself — not merely
// ImportJSON's own in-memory size check running on whatever already got
// read — is bounded. mcp.json is opened as a FIFO whose writer supplies
// more than maxMCPImportDocumentBytes and then deliberately keeps the
// pipe open (no EOF, no error) for longer than any reasonable bound: an
// unbounded read (os.ReadFile's own semantics — call Read until a real
// io.EOF or error) would block waiting for the writer to either close or
// send more, exactly as it would for a slow or hostile/unbounded real
// source. A read bounded via io.LimitReader(file, cap+1) stops issuing
// further Read calls once it has consumed cap+1 bytes — synthesizing its
// own io.EOF at that point — regardless of whether the underlying FIFO
// itself has anything left to say, so ReimportConfiguredJSON must return
// promptly either way.
func TestReimportConfiguredJSONReadIsBoundedEvenWhenTheSourceNeverEnds(t *testing.T) {
	service, _ := newRegistryTestService(t)
	root := t.TempDir()
	path := filepath.Join(root, "mcp.json")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Skipf("FIFOs are unavailable in this environment: %v", err)
	}
	service.SetMCPConfigRoot(root)

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		writer, err := os.OpenFile(path, os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer func() { _ = writer.Close() }()
		payload := bytes.Repeat([]byte(" "), maxMCPImportDocumentBytes+1)
		_, _ = writer.Write(payload)
		// Keep the pipe open well past any reasonable bound on the read
		// side, without ever closing or writing a real terminator: from
		// an unbounded reader's perspective, the stream simply has not
		// ended yet.
		time.Sleep(5 * time.Second)
	}()
	t.Cleanup(func() { <-writerDone })

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
		if res.err != nil {
			t.Fatalf("an oversized document must be reported, not returned as an error: %v", res.err)
		}
		if _, refused := res.report.Unsupported["_document"]; !refused {
			t.Fatalf("Unsupported = %v, want a _document refusal", res.report.Unsupported)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReimportConfiguredJSON did not return within 3s: the file read is not bounded and is waiting for the source to end")
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
