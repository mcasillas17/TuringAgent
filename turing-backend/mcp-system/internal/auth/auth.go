package auth

import (
	"crypto/subtle"
	"net/http"
)

// RequireBearer guards a handler with the shared MCP bearer token.
//
// An unset expected token rejects every request: a misconfigured deploy must
// fail closed rather than serve the tools unauthenticated.
func RequireBearer(expected string, next http.Handler) http.Handler {
	// Built once, not per request: one fewer heap copy of the secret per call.
	want := "Bearer " + expected
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if expected == "" || !equalTokens(r.Header.Get("Authorization"), want) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// equalTokens compares in time independent of the token's CONTENT. A plain !=
// returns as soon as two bytes differ, leaking how much of a guess was correct.
// Note subtle.ConstantTimeCompare still returns early when the lengths differ,
// so the length of the expected token is not hidden — the accepted tradeoff.
func equalTokens(got string, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
