package auth

import (
	"crypto/subtle"
	"errors"
	"net/http"
)

// AgentFromBearer resolves the calling agent from the shared MCP bearer token.
//
// An unset token rejects every request: a misconfigured deploy must fail closed
// rather than expose the sandboxed file tools unauthenticated.
func AgentFromBearer(r *http.Request, systemToken string) (string, error) {
	if systemToken == "" || !equalTokens(r.Header.Get("Authorization"), "Bearer "+systemToken) {
		return "", errors.New("unauthorized")
	}
	// v1.0 has one runtime/MCP token for the general assistant; v1.1 should
	// replace this with a token-to-agent map.
	return "general_assistant", nil
}

// equalTokens compares in time independent of the token's CONTENT. A plain !=
// returns as soon as two bytes differ, leaking how much of a guess was correct.
// Note subtle.ConstantTimeCompare still returns early when the lengths differ,
// so the length of the expected token is not hidden — the accepted tradeoff.
func equalTokens(got string, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
