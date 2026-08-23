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
)

// mcp.json's root object must name the "mcpServers" key exactly as spelled
// — encoding/json's built-in case-insensitive field-name fallback would
// otherwise accept "McpServers" as if it were correctly spelled, silently
// reading a case-variant key an operator's editor or a tampered file might
// have introduced instead of refusing the ambiguity outright.
func TestImportJSONRefusesCaseVariantRootKey(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	_, err := service.ImportJSON(ctx, []byte(`{
		"McpServers": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`))
	if err == nil {
		t.Fatal("want an error: the root object must name the key exactly \"mcpServers\"")
	}
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledServiceCount(servers) != 0 {
		t.Fatalf("non-bundled count = %d, want zero: a case-variant root key must import nothing", nonBundledServiceCount(servers))
	}
}

// A second, all-caps case variant must be refused the same way.
func TestImportJSONRefusesAllCapsRootKeyVariant(t *testing.T) {
	service, _ := newRegistryTestService(t)
	ctx := context.Background()

	_, err := service.ImportJSON(ctx, []byte(`{
		"MCPSERVERS": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`))
	if err == nil {
		t.Fatal("want an error: the root object must name the key exactly \"mcpServers\"")
	}
}

// Two root-level keys that both spell "mcpServers" identically must be
// refused deterministically: plain encoding/json decoding into a struct or
// map silently keeps only the last one, which would let a second,
// attacker- or tooling-introduced "mcpServers" object quietly win instead
// of the document being refused.
func TestImportJSONRefusesExactDuplicateRootKey(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	// Constructed by hand (not json.Marshal, which could never produce a
	// duplicate key from a Go map in the first place) specifically to
	// exercise the raw duplicate-key wire shape.
	document := []byte(`{
		"mcpServers": {"first-vendor": {"url": "https://first-vendor.example/mcp"}},
		"mcpServers": {"second-vendor": {"url": "https://second-vendor.example/mcp"}}
	}`)
	_, err := service.ImportJSON(ctx, document)
	if err == nil {
		t.Fatal("want an error: an exact-duplicate \"mcpServers\" root key must be refused, not silently resolved to the last one")
	}
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledServiceCount(servers) != 0 {
		t.Fatalf("non-bundled count = %d, want zero: a duplicate root key must import nothing from either object", nonBundledServiceCount(servers))
	}
}

// A case-insensitive duplicate ("mcpServers" plus "MCPSERVERS", spelled
// differently but referring to the same conceptual key) must be refused
// exactly like an exact duplicate — regardless of which order they appear
// in.
func TestImportJSONRefusesCaseInsensitiveDuplicateRootKey(t *testing.T) {
	for _, document := range []string{
		`{
			"mcpServers": {"first-vendor": {"url": "https://first-vendor.example/mcp"}},
			"MCPSERVERS": {"second-vendor": {"url": "https://second-vendor.example/mcp"}}
		}`,
		`{
			"MCPSERVERS": {"second-vendor": {"url": "https://second-vendor.example/mcp"}},
			"mcpServers": {"first-vendor": {"url": "https://first-vendor.example/mcp"}}
		}`,
	} {
		service, repo := newRegistryTestService(t)
		ctx := context.Background()
		_, err := service.ImportJSON(ctx, []byte(document))
		if err == nil {
			t.Fatalf("document %q: want an error for a case-insensitive duplicate root key", document)
		}
		servers, err := repo.ListMCPServers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if nonBundledServiceCount(servers) != 0 {
			t.Fatalf("document %q: non-bundled count = %d, want zero", document, nonBundledServiceCount(servers))
		}
	}
}

// An unrelated sibling top-level key (anything that is not a
// case-insensitive match for "mcpServers") is left alone: this package's
// strict-root-key handling is scoped to the one key it actually reads, not
// a general "no other keys allowed" rule.
func TestImportJSONIgnoresUnrelatedTopLevelKeys(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"$schema": "https://example.com/mcp.schema.json",
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 1 || report.Imported[0] != "vendor" {
		t.Fatalf("Imported = %v, want [vendor]: an unrelated sibling key must not refuse the document", report.Imported)
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != nil {
		t.Fatal(err)
	}
}

// A document naming the exact same server twice — whether both
// definitions are identical or (more realistically, the actual attack
// this closes) they disagree on url/token — must refuse the whole
// document deterministically rather than silently keeping whichever one
// plain encoding/json map decoding happened to visit last. This is a
// whole-document failure (like the entry-count and document-size caps),
// not a per-entry Unsupported row, so neither definition can ever partly
// register: "no partial writes" for this refusal means *zero* rows, not
// one of the two candidates.
func TestImportJSONRefusesExactDuplicateServerNameWithDifferentURLAndToken(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	const firstToken = "first-configured-bearer-value"
	const secondToken = "second-configured-bearer-value"
	document := []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor-one.example/mcp",
				"headers": {"Authorization": "Bearer ` + firstToken + `"}
			},
			"vendor": {
				"url": "https://vendor-two.example/mcp",
				"headers": {"Authorization": "Bearer ` + secondToken + `"}
			}
		}
	}`)
	report, err := service.ImportJSON(ctx, document)
	if err == nil {
		t.Fatalf("want an error for a duplicate server name; got a report instead: %+v", report)
	}
	if strings.Contains(err.Error(), firstToken) || strings.Contains(err.Error(), secondToken) {
		t.Fatalf("err = %q, must not leak either configured token", err.Error())
	}
	if strings.Contains(err.Error(), "vendor-one.example") || strings.Contains(err.Error(), "vendor-two.example") {
		t.Fatalf("err = %q, want a generic reason, not either candidate's own URL", err.Error())
	}
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledServiceCount(servers) != 0 {
		t.Fatalf("non-bundled count = %d, want zero: neither duplicate definition may register", nonBundledServiceCount(servers))
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound: a duplicate-name refusal must create no row", err)
	}
}

// The same duplicate-name refusal, exercised through ReimportConfiguredJSON,
// must collapse into exactly one bounded "_document" issue — never one row
// per (duplicated) name — and leave mcp_import_issues/the registry
// completely untouched otherwise, the same as any other whole-document
// refusal.
func TestReimportConfiguredJSONCollapsesDuplicateServerNameToOneDocumentRefusal(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	root := t.TempDir()
	document := []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor-one.example/mcp"},
			"vendor": {"url": "https://vendor-two.example/mcp"}
		}
	}`)
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), document, 0o600); err != nil {
		t.Fatal(err)
	}
	service.SetMCPConfigRoot(root)

	report, err := service.ReimportConfiguredJSON(ctx)
	if err != nil {
		t.Fatalf("a duplicate-name document must be reported, not returned as an error: %v", err)
	}
	if len(report.Imported) != 0 || len(report.Skipped) != 0 {
		t.Fatalf("report = %+v, want no imports or skips", report)
	}
	if len(report.Unsupported) != 1 {
		t.Fatalf("Unsupported = %+v, want exactly one collapsed _document entry", report.Unsupported)
	}
	if _, refused := report.Unsupported["_document"]; !refused {
		t.Fatalf("Unsupported = %+v, want the _document key", report.Unsupported)
	}
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledServiceCount(servers) != 0 {
		t.Fatalf("non-bundled count = %d, want zero", nonBundledServiceCount(servers))
	}
}

// A document naming the same server three times, all with identical
// bodies, must still be refused: the refusal is about the name repeating
// at all, not merely about the bodies disagreeing.
func TestImportJSONRefusesDuplicateServerNameEvenWithIdenticalBodies(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	document := []byte(`{
		"mcpServers": {
			"vendor": {"url": "https://vendor.example/mcp"},
			"vendor": {"url": "https://vendor.example/mcp"},
			"vendor": {"url": "https://vendor.example/mcp"}
		}
	}`)
	_, err := service.ImportJSON(ctx, document)
	if err == nil {
		t.Fatal("want an error: a repeated server name must be refused even when every definition is identical")
	}
	if _, err := repo.GetMCPServerByName(ctx, "vendor"); err != repository.ErrMCPServerNotFound {
		t.Fatalf("err = %v, want ErrMCPServerNotFound", err)
	}
	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledServiceCount(servers) != 0 {
		t.Fatalf("non-bundled count = %d, want zero", nonBundledServiceCount(servers))
	}
}

// Two DIFFERENT (non-duplicate) server names must still both import
// normally: the duplicate-name check must not be a false positive against
// an ordinary, valid multi-server document.
func TestImportJSONDistinctServerNamesStillImportNormally(t *testing.T) {
	service, _ := newRegistryTestService(t)
	ctx := context.Background()

	report, err := service.ImportJSON(ctx, []byte(`{
		"mcpServers": {
			"vendor-a": {"url": "https://vendor-a.example/mcp"},
			"vendor-b": {"url": "https://vendor-b.example/mcp"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Imported) != 2 {
		t.Fatalf("Imported = %v, want two distinct servers", report.Imported)
	}
}

// The mcpServers entry-count cap must be enforced the instant it would be
// exceeded during the streaming parse itself — not only after a
// full-sized map of every entry has already been built in memory. This
// constructs a document with far more (tiny) entries than
// maxMCPImportEntries, comfortably within maxMCPImportDocumentBytes, and
// requires the refusal to come back quickly: building (and then
// discarding) a map/slice sized to the full entry count first, only to
// refuse afterward, would still work correctness-wise but defeats the
// point of bailing out early, and would be far slower than the bound
// below allows for.
func TestImportJSONEnforcesEntryCapDuringStreamingParseWithoutBuildingHugeMap(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	var buf bytes.Buffer
	buf.WriteString(`{"mcpServers":{`)
	const hugeEntryCount = 200_000
	for i := 0; i < hugeEntryCount; i++ {
		if i > 0 {
			buf.WriteByte(',')
		}
		fmt.Fprintf(&buf, `"s%d":{}`, i)
	}
	buf.WriteString(`}}`)
	if buf.Len() >= maxMCPImportDocumentBytes {
		t.Fatalf("test setup is broken: document (%d bytes) already exceeds maxMCPImportDocumentBytes", buf.Len())
	}

	start := time.Now()
	_, err := service.ImportJSON(ctx, buf.Bytes())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want an error: entry count far exceeds maxMCPImportEntries")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", maxMCPImportEntries)) {
		t.Fatalf("err = %v, want it to name the maxMCPImportEntries limit", err)
	}
	// A generous bound: bailing out as soon as the cap is exceeded should
	// take a small fraction of a second even on a slow CI runner: this
	// only needs to distinguish "bailed out early" from "built a
	// 200,000-entry map/slice first."
	if elapsed > 2*time.Second {
		t.Fatalf("ImportJSON took %s parsing a %d-entry document; want it to bail as soon as the cap is exceeded", elapsed, hugeEntryCount)
	}

	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nonBundledServiceCount(servers) != 0 {
		t.Fatalf("non-bundled count = %d, want zero", nonBundledServiceCount(servers))
	}
}

// A completely non-object root value (e.g. an array) must be refused, the
// same as it always was — decodeMCPRootServers requires the root token to
// open with '{'.
func TestImportJSONRefusesNonObjectRootValue(t *testing.T) {
	service, _ := newRegistryTestService(t)
	ctx := context.Background()

	_, err := service.ImportJSON(ctx, []byte(`["not", "an", "object"]`))
	if err == nil {
		t.Fatal("want an error: the root value must be an object")
	}
}

// A "mcpServers" value that is not itself an object (e.g. an array) must
// be refused deterministically.
func TestImportJSONRefusesNonObjectMcpServersValue(t *testing.T) {
	service, _ := newRegistryTestService(t)
	ctx := context.Background()

	_, err := service.ImportJSON(ctx, []byte(`{"mcpServers": ["not", "an", "object"]}`))
	if err == nil {
		t.Fatal(`want an error: "mcpServers" must be an object`)
	}
}
