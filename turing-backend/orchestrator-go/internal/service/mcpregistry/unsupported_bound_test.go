package mcpregistry

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// recordUnsupported must bound how many distinct names it ever records
// independently of ImportJSON's own upfront maxMCPImportEntries
// document-level gate (see TestImportJSONRefusesTooManyEntriesBeforeProcessing):
// should a future caller or refactor ever invoke it more times than that
// gate allows for — whether a new code path, a bug that calls it more than
// once per entry, or anything else — it must still collapse everything
// beyond the limit into one additional, fixed "_document" summary entry
// rather than growing the map without bound.
func TestRecordUnsupportedBoundsCountEvenIfCalledBeyondTheEntryCap(t *testing.T) {
	unsupported := make(map[string]string)
	for i := 0; i < maxMCPImportEntries+50; i++ {
		recordUnsupported(unsupported, fmt.Sprintf("name-%d", i), "some reason")
	}
	if len(unsupported) > maxMCPImportEntries+1 {
		t.Fatalf("len(unsupported) = %d, want at most maxMCPImportEntries+1 (%d)", len(unsupported), maxMCPImportEntries+1)
	}
	reason, present := unsupported["_document"]
	if !present {
		t.Fatalf("unsupported = %+v (len %d), want a _document overflow summary once the limit is exceeded", unsupported, len(unsupported))
	}
	if len(reason) > maxMCPStatusMessageBytes {
		t.Fatalf("_document reason is %d bytes, want it bounded by maxMCPStatusMessageBytes (%d)", len(reason), maxMCPStatusMessageBytes)
	}
	if !utf8.ValidString(reason) {
		t.Fatalf("_document reason is not valid UTF-8: %q", reason)
	}
}

// The first maxMCPImportEntries distinct names recorded before the
// overflow point must still be recorded individually and correctly — the
// defensive bound must not discard or corrupt anything within the limit,
// only collapse what comes after it.
func TestRecordUnsupportedKeepsEntriesWithinTheLimitIndividuallyRecorded(t *testing.T) {
	unsupported := make(map[string]string)
	for i := 0; i < maxMCPImportEntries; i++ {
		recordUnsupported(unsupported, fmt.Sprintf("name-%d", i), fmt.Sprintf("reason-%d", i))
	}
	if len(unsupported) != maxMCPImportEntries {
		t.Fatalf("len(unsupported) = %d, want exactly maxMCPImportEntries (%d): none should have overflowed yet", len(unsupported), maxMCPImportEntries)
	}
	if _, present := unsupported["_document"]; present {
		t.Fatal("a _document overflow entry must not appear before the limit is exceeded")
	}
	if unsupported["name-0"] != "reason-0" {
		t.Fatalf("name-0 reason = %q, want it individually preserved", unsupported["name-0"])
	}

	// The very next call (limit+1) must overflow into _document rather
	// than growing the map to limit+1 *named* entries.
	recordUnsupported(unsupported, "one-more-name", "one more reason")
	if len(unsupported) != maxMCPImportEntries+1 {
		t.Fatalf("len(unsupported) = %d, want maxMCPImportEntries+1 (%d) after exactly one overflow call", len(unsupported), maxMCPImportEntries+1)
	}
	if _, present := unsupported["one-more-name"]; present {
		t.Fatal("the (limit+1)th name must not be recorded individually; it must collapse into _document")
	}
	if _, present := unsupported["_document"]; !present {
		t.Fatal("want a _document overflow summary after exceeding the limit")
	}
}

// A long, attacker-controlled reason overflowing into _document must stay
// bounded and valid UTF-8, the same as every other recordUnsupported
// reason.
func TestRecordUnsupportedOverflowReasonStaysBoundedAndValidUTF8(t *testing.T) {
	unsupported := make(map[string]string)
	for i := 0; i < maxMCPImportEntries; i++ {
		recordUnsupported(unsupported, fmt.Sprintf("name-%d", i), "reason")
	}
	longName := strings.Repeat("é", 1000)
	recordUnsupported(unsupported, longName, strings.Repeat("x", 10_000))
	reason, present := unsupported["_document"]
	if !present {
		t.Fatal("want a _document overflow summary once the limit is exceeded")
	}
	if reason == "" {
		t.Fatal("_document reason must not be empty")
	}
	if len(reason) > maxMCPStatusMessageBytes {
		t.Fatalf("_document reason is %d bytes, want it bounded", len(reason))
	}
	if !utf8.ValidString(reason) {
		t.Fatalf("_document reason is not valid UTF-8: %q", reason)
	}
	if strings.Contains(reason, longName) {
		t.Fatal("the overflow summary must not echo the overflowing entry's own name")
	}
}
