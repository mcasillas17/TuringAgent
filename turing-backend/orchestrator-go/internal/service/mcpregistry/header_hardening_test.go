package mcpregistry

import (
	"context"
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
		headers map[string]string
	}{
		{
			name: "differing values",
			headers: map[string]string{
				"Authorization": "Bearer first-token-value",
				"authorization": "Bearer second-token-value",
			},
		},
		{
			name: "identical values",
			headers: map[string]string{
				"Authorization": "Bearer same-token-value",
				"authorization": "Bearer same-token-value",
			},
		},
		{
			name: "three case variants",
			headers: map[string]string{
				"Authorization": "Bearer a",
				"authorization": "Bearer b",
				"AUTHORIZATION": "Bearer c",
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
		token, err := bearerFromHeaders(map[string]string{key: "Bearer vendor-secret"})
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
