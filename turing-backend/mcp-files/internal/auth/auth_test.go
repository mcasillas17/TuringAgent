package auth

import (
	"net/http/httptest"
	"testing"
)

func TestAgentFromBearerRejectsWrongToken(t *testing.T) {
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	if _, err := AgentFromBearer(req, "expected"); err == nil {
		t.Fatalf("expected 401-equivalent auth error")
	}
}

func TestAgentFromBearerMapsTokenToGeneralAssistant(t *testing.T) {
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer expected")
	agent, err := AgentFromBearer(req, "expected")
	if err != nil {
		t.Fatalf("unexpected auth error: %v", err)
	}
	if agent != "general_assistant" {
		t.Fatalf("unexpected agent %q", agent)
	}
}

// The security invariant from the design docs: an unset token must fail closed.
// A misconfigured deploy (missing MCP_FILES_TOKEN_GENERAL) must never leave the
// sandboxed file tools open, whatever the caller sends.
func TestAgentFromBearerFailsClosedWhenNoTokenIsConfigured(t *testing.T) {
	for name, header := range map[string]string{
		"no header":        "",
		"empty bearer":     "Bearer ",
		"bare Bearer":      "Bearer",
		"any token at all": "Bearer anything",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/mcp", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			if _, err := AgentFromBearer(req, ""); err == nil {
				t.Fatal("empty configured token did not fail closed")
			}
		})
	}
}

func TestAgentFromBearerRejectsMalformedCredentials(t *testing.T) {
	for name, header := range map[string]string{
		"missing header":     "",
		"no scheme":          "expected",
		"lowercase scheme":   "bearer expected",
		"wrong scheme":       "Basic expected",
		"token is a prefix":  "Bearer expecte",
		"token has a suffix": "Bearer expectedX",
		"trailing space":     "Bearer expected ",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/mcp", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			if _, err := AgentFromBearer(req, "expected"); err == nil {
				t.Fatalf("accepted malformed credential %q", header)
			}
		})
	}
}
