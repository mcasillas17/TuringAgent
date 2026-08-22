package mcpregistry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// bearerFromHeaders must reject more than one case-insensitive
// Authorization key with one fixed, generic reason — never picking
// whichever one Go's randomized map iteration happens to visit last. This
// covers both a differing-value pair (the more obviously suspicious case)
// and an identical-value pair (which a randomized-winner implementation
// would happily accept, since both values agree): both must be refused
// the same way.
func TestBearerFromHeadersRejectsDuplicateCaseInsensitiveAuthorizationKeys(t *testing.T) {
	for _, test := range []struct {
		name    string
		headers []mcpHeaderEntry
	}{
		{
			name: "differing values",
			headers: []mcpHeaderEntry{
				{Name: "Authorization", Value: "Bearer first-token-value"},
				{Name: "authorization", Value: "Bearer second-token-value"},
			},
		},
		{
			name: "identical values",
			headers: []mcpHeaderEntry{
				{Name: "Authorization", Value: "Bearer same-token-value"},
				{Name: "authorization", Value: "Bearer same-token-value"},
			},
		},
		{
			name: "three case variants",
			headers: []mcpHeaderEntry{
				{Name: "Authorization", Value: "Bearer a"},
				{Name: "authorization", Value: "Bearer b"},
				{Name: "AUTHORIZATION", Value: "Bearer c"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := bearerFromHeaders(test.headers)
			if err == nil {
				t.Fatal("want an error for more than one case-insensitive authorization header")
			}
			if err != errMultipleAuthorizationHeaders {
				t.Fatalf("err = %v, want the one fixed errMultipleAuthorizationHeaders reason", err)
			}
			for _, sentinel := range []string{"first-token-value", "second-token-value", "same-token-value", "Bearer a", "Bearer b", "Bearer c"} {
				if strings.Contains(err.Error(), sentinel) {
					t.Fatalf("err = %q, must not leak a header value", err.Error())
				}
			}
		})
	}
}

// A single Authorization header (any case) still works exactly as before.
func TestBearerFromHeadersAcceptsExactlyOneCaseInsensitiveAuthorizationKey(t *testing.T) {
	for _, key := range []string{"Authorization", "authorization", "AUTHORIZATION"} {
		token, err := bearerFromHeaders([]mcpHeaderEntry{{Name: key, Value: "Bearer vendor-secret"}})
		if err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
		if token != "vendor-secret" {
			t.Fatalf("key %q: token = %q, want vendor-secret", key, token)
		}
	}
}

// No headers at all is not an error: it just means no token.
func TestBearerFromHeadersNoHeadersMeansNoToken(t *testing.T) {
	token, err := bearerFromHeaders(nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		t.Fatalf("token = %q, want empty", token)
	}
}

// When an mcp.json entry's headers carry both an unsupported header name
// and more than one case-insensitive Authorization key, the outcome must
// be the exact same fixed, deterministic result on every call — never a
// randomized pick between "which unsupported header name gets named" or
// "unsupported header vs. duplicate Authorization" that would depend on
// Go's randomized map iteration order. This runs many trials specifically
// because that randomization would otherwise only show up intermittently.
func TestBearerFromHeadersDeterministicWithUnsupportedAndDuplicateAuthorizationHeaders(t *testing.T) {
	headers := []mcpHeaderEntry{
		{Name: "Authorization", Value: "Bearer first-header-value"},
		{Name: "authorization", Value: "Bearer second-header-value"},
		{Name: "X-Api-Key", Value: "attacker-controlled-key-value"},
	}
	const trials = 200
	for i := 0; i < trials; i++ {
		token, err := bearerFromHeaders(headers)
		if token != "" {
			t.Fatalf("trial %d: token = %q, want empty on any refusal", i, token)
		}
		if err == nil {
			t.Fatalf("trial %d: want an error", i)
		}
		if err.Error() != `header "X-Api-Key" is unsupported; only Authorization: ****** accepted` {
			t.Fatalf("trial %d: err = %q, want the fixed, deterministic unsupported-header reason naming X-Api-Key every time", i, err.Error())
		}
		for _, sentinel := range []string{"first-header-value", "second-header-value", "attacker-controlled-key-value"} {
			if strings.Contains(err.Error(), sentinel) {
				t.Fatalf("trial %d: err = %q, must not leak a header value", i, err.Error())
			}
		}
	}
}

// With two distinct unsupported header names present (and no Authorization
// header at all), the one named in the error must always be the
// lexicographically first — sorted, not whichever one iteration happens to
// visit first — on every trial.
func TestBearerFromHeadersUnsupportedHeaderSelectionIsSortedNotRandom(t *testing.T) {
	headers := []mcpHeaderEntry{
		{Name: "X-Zebra-Header", Value: "1"},
		{Name: "X-Alpha-Header", Value: "2"},
		{Name: "X-Mid-Header", Value: "3"},
	}
	const trials = 200
	for i := 0; i < trials; i++ {
		_, err := bearerFromHeaders(headers)
		if err == nil {
			t.Fatalf("trial %d: want an error", i)
		}
		if err.Error() != `header "X-Alpha-Header" is unsupported; only Authorization: ****** accepted` {
			t.Fatalf("trial %d: err = %q, want the lexicographically first unsupported header named every time", i, err.Error())
		}
	}
}

// The duplicate-header refusal must also be reachable — and stay
// sentinel-free and generic — through the full ImportJSON path, not just
// the unit-level bearerFromHeaders call.
func TestImportJSONRefusesDuplicateAuthorizationHeaderCaseVariants(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer one-value", "authorization": "Bearer two-value"}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if reason != errMultipleAuthorizationHeaders.Error() {
		t.Fatalf("reason = %q, want the fixed duplicate-header reason", reason)
	}
	if _, err := repo.GetMCPServerByName(context.Background(), "vendor"); err == nil {
		t.Fatal("a duplicate-header refusal must not create a server row")
	}
}

// decodeMCPHeaderEntries must preserve two JSON object members that share
// the *exact* same key — not just two that differ only in case — as two
// separate entries: json.Unmarshal into a map[string]string collapses an
// exact duplicate key into one entry, last-value-wins, which would let a
// second "Authorization" header silently overwrite the first (rather than
// being refused) if headers were ever decoded that way. This is the one
// scenario the old map[string]string-based field type could never even
// express, let alone catch.
func TestDecodeMCPHeaderEntriesPreservesExactDuplicateKeys(t *testing.T) {
	entries, err := decodeMCPHeaderEntries(json.RawMessage(`{"Authorization": "first-value", "Authorization": "second-value"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want 2: an exact-duplicate JSON key must not collapse into one entry", entries)
	}
	for _, entry := range entries {
		if entry.Name != "Authorization" {
			t.Fatalf("entries = %+v, want both named exactly %q", entries, "Authorization")
		}
	}
}

// bearerFromHeaders must refuse two entries that share the *exact* same
// name ("Authorization" spelled identically twice) the same way it refuses
// two that only differ in case — this is now directly expressible as a
// []mcpHeaderEntry (unlike the old map[string]string parameter, which
// could never hold two entries under the identical key at all).
func TestBearerFromHeadersRejectsExactDuplicateSpellingNotJustCaseVariants(t *testing.T) {
	_, err := bearerFromHeaders([]mcpHeaderEntry{
		{Name: "Authorization", Value: "first-value"},
		{Name: "Authorization", Value: "second-value"},
	})
	if err != errMultipleAuthorizationHeaders {
		t.Fatalf("err = %v, want the one fixed errMultipleAuthorizationHeaders reason", err)
	}
}

// The full ImportJSON path must refuse an entry whose raw mcp.json
// "headers" object spells "Authorization" identically twice — the
// realistic shape of the vulnerability this finding closes: a
// map[string]string-based decode would have silently kept only the last
// of the two (whichever encoding/json's own duplicate-key handling
// happened to keep), accepting one of the two tokens as if it were the
// only one configured, rather than refusing the ambiguous entry outright.
func TestImportJSONRefusesExactDuplicateAuthorizationSpellingNotJustCaseVariants(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "first-configured-value", "Authorization": "second-configured-value"}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused: an exact-duplicate Authorization key must not silently pick one of the two values", report.Unsupported)
	}
	if reason != errMultipleAuthorizationHeaders.Error() {
		t.Fatalf("reason = %q, want the fixed duplicate-header reason", reason)
	}
	for _, sentinel := range []string{"first-configured-value", "second-configured-value"} {
		if strings.Contains(reason, sentinel) {
			t.Fatalf("reason = %q, must not leak a header value", reason)
		}
	}
	if _, err := repo.GetMCPServerByName(context.Background(), "vendor"); err == nil {
		t.Fatal("an exact-duplicate-header refusal must not create a server row")
	}
}

// Mixed unsupported-header-name and exact-duplicate-Authorization-spelling
// must resolve the same fixed, deterministic way the case-variant mix
// already does: the unsupported header name wins.
func TestImportJSONMixedUnsupportedAndExactDuplicateAuthorizationIsDeterministic(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "a", "Authorization": "b", "X-Api-Key": "attacker-controlled-key-value"}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if !strings.Contains(reason, "X-Api-Key") || !strings.Contains(reason, "unsupported") {
		t.Fatalf("reason = %q, want the fixed, deterministic unsupported-header reason naming X-Api-Key", reason)
	}
	if _, err := repo.GetMCPServerByName(context.Background(), "vendor"); err == nil {
		t.Fatal("a refused entry must not create a server row")
	}
}

// A non-object "headers" value must be refused deterministically too, now
// that it is captured as raw json.RawMessage rather than decoded straight
// into map[string]string (which would have failed at the outer struct
// decode instead) — decodeMCPHeaderEntries is the one place that now
// decides what "headers" may shape as.
func TestImportJSONRefusesNonObjectHeadersValue(t *testing.T) {
	service, _ := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": ["not", "an", "object"]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, refused := report.Unsupported["vendor"]; !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused for a non-object headers value", report.Unsupported)
	}
	if len(report.Imported) != 0 {
		t.Fatalf("Imported = %v, want none", report.Imported)
	}
}

// An absent "headers" key and an explicit JSON null must both mean "no
// headers" — neither is an error — matching what an absent or null
// map[string]string field decoded to previously.
func TestImportJSONHeadersAbsentOrNullMeansNoToken(t *testing.T) {
	service, repo := newRegistryTestService(t)
	for _, document := range []string{
		`{"mcpServers": {"vendor-absent": {"url": "https://vendor-absent.example/mcp"}}}`,
		`{"mcpServers": {"vendor-null": {"url": "https://vendor-null.example/mcp", "headers": null}}}`,
	} {
		report, err := service.ImportJSON(context.Background(), []byte(document))
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Imported) != 1 {
			t.Fatalf("document %q: Imported = %v, want exactly one server imported", document, report.Imported)
		}
	}
	for _, name := range []string{"vendor-absent", "vendor-null"} {
		server, err := repo.GetMCPServerByName(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		if len(server.SealedToken) != 0 {
			t.Fatalf("%s: SealedToken = %v, want empty: no headers means no token", name, server.SealedToken)
		}
	}
}
