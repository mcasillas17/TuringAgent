package auth

import (
	"net/http"
	"net/http/httptest"
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

// An absent header and Header.Set(k, "") are indistinguishable to Header.Get,
// so the empty case models "no header sent".
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
// server open. "empty bearer" is the load-bearing case — the others are rejected
// by the comparison anyway, so only this one pins the short-circuit.
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
		// Same length as the real credential, differing only inside the token:
		// the case that actually exercises the comparison over the secret.
		"same length, wrong token": "Bearer x3cret",
		"same length, last byte":   "Bearer s3creT",
	} {
		t.Run(name, func(t *testing.T) {
			code, reached := serve(t, "s3cret", header)
			if code != http.StatusUnauthorized || reached {
				t.Fatalf("expected 401 and no passthrough: code=%d reached=%v", code, reached)
			}
		})
	}
}
