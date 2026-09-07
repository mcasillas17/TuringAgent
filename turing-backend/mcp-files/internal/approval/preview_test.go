package approval

import (
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
)

func TestPreviewCapabilityIsDomainSeparatedAndBoundToExactProvenance(t *testing.T) {
	args := map[string]any{"path": "notes/todo.txt", "content": "reviewed"}
	prov := signProvenance(t, "secret", provenanceClaims(t, args))
	req := approvalpreview.Request{Tool: "files.create", AgentID: "general_assistant", Args: args, ProvenanceToken: prov}
	claims := map[string]any{"iss": approvalTokenIssuer, "aud": "mcp-files", "sub": req.AgentID, "tool": req.Tool,
		"kind": approvalpreview.TokenKind, "args_hash": hashArgs(t, args), "provenance_hash": approvalpreview.Hash(prov), "exp": time.Now().Add(time.Minute).Unix()}
	preview := signProvenance(t, "secret", claims)
	c := Consumer{JWTSecret: "secret"}
	if err := c.VerifyPreview(preview, req); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyHS256(preview, "secret"); err == nil {
		t.Fatal("preview accepted as approval")
	}
	if _, err := c.VerifyProvenance(preview, req.Tool, req.Args, req.AgentID); err == nil {
		t.Fatal("preview accepted as provenance")
	}
	if err := c.VerifyPreview(prov, req); err == nil {
		t.Fatal("provenance accepted as preview")
	}
	for _, field := range []string{"aud", "kind", "sub", "tool", "args_hash", "provenance_hash"} {
		original := claims[field]
		claims[field] = "wrong"
		if err := c.VerifyPreview(signProvenance(t, "secret", claims), req); err == nil {
			t.Fatalf("preview accepted wrong %s", field)
		}
		claims[field] = original
	}
	claims["exp"] = time.Now().Add(-time.Minute).Unix()
	if err := c.VerifyPreview(signProvenance(t, "secret", claims), req); err == nil {
		t.Fatal("expired preview accepted")
	}
}
