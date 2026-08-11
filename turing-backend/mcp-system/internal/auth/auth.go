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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if expected == "" || !equalTokens(r.Header.Get("Authorization"), "Bearer "+expected) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// equalTokens compares in constant time. A plain != returns as soon as two bytes
// differ, so the time it takes leaks how much of a guess was correct — enough to
// recover a token byte by byte given enough attempts.
func equalTokens(got string, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
