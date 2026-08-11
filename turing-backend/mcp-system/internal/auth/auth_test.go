package auth

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// next records whether the guarded handler was reached, so every rejection case
// can assert the request was stopped rather than merely re-labelled.
func next(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func serve(t *testing.T, expected string, header string) (int, bool) {
	t.Helper()
	var reached bool
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	RequireBearer(expected, next(&reached)).ServeHTTP(rec, req)
	return rec.Code, reached
}

func TestRequireBearerAllowsTheExactToken(t *testing.T) {
	code, reached := serve(t, "s3cret", "Bearer s3cret")
	if code != http.StatusOK || !reached {
		t.Fatalf("valid token rejected: code=%d reached=%v", code, reached)
	}
}

// The security invariant from the design docs: an unset token must fail closed.
// A misconfigured deploy (missing MCP_SYSTEM_TOKEN_GENERAL) must never leave the
// server open, including when the caller sends no header or an empty bearer.
func TestRequireBearerFailsClosedWhenNoTokenIsConfigured(t *testing.T) {
	for name, header := range map[string]string{
		"no header":        "",
		"empty bearer":     "Bearer ",
		"bare Bearer":      "Bearer",
		"any token at all": "Bearer anything",
	} {
		t.Run(name, func(t *testing.T) {
			code, reached := serve(t, "", header)
			if code != http.StatusUnauthorized || reached {
				t.Fatalf("empty expected token did not fail closed: code=%d reached=%v", code, reached)
			}
		})
	}
}

func TestRequireBearerRejects(t *testing.T) {
	for name, header := range map[string]string{
		"missing header":     "",
		"wrong token":        "Bearer wrong",
		"no scheme":          "s3cret",
		"lowercase scheme":   "bearer s3cret",
		"wrong scheme":       "Basic s3cret",
		"token is a prefix":  "Bearer s3cre",
		"token has a suffix": "Bearer s3cretX",
		"leading space":      "Bearer  s3cret",
		"trailing space":     "Bearer s3cret ",
	} {
		t.Run(name, func(t *testing.T) {
			code, reached := serve(t, "s3cret", header)
			if code != http.StatusUnauthorized || reached {
				t.Fatalf("expected 401 and no passthrough: code=%d reached=%v", code, reached)
			}
		})
	}
}

// Header names are case-insensitive per RFC 7230, and HTTP/2 requires them to be
// sent lowercase. Parse real wire bytes so Go's canonicalisation runs exactly as
// it does for a live request — constructing the header map directly would skip
// it and prove nothing about what a real client can send.
func TestRequireBearerAcceptsAnyHeaderNameCasingOnTheWire(t *testing.T) {
	for _, name := range []string{"authorization", "AUTHORIZATION", "Authorization"} {
		t.Run(name, func(t *testing.T) {
			raw := "POST /mcp HTTP/1.1\r\nHost: x\r\n" + name + ": Bearer s3cret\r\n\r\n"
			req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
			if err != nil {
				t.Fatal(err)
			}
			var reached bool
			rec := httptest.NewRecorder()
			RequireBearer("s3cret", next(&reached)).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || !reached {
				t.Fatalf("header name %q rejected: code=%d reached=%v", name, rec.Code, reached)
			}
		})
	}
}
