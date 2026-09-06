package approval

import (
	"context"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
)

func TestWriteRejectsMissingOrForeignPreviewBindingBeforeConsume(t *testing.T) {
	args := map[string]any{"path": "notes/todo.txt", "content": "reviewed"}
	for _, binding := range []approvalpreview.Binding{
		{},
		{PreviewHash: "p", PhysicalPath: "wrong", AfterHash: approvalpreview.Hash("reviewed"), SessionID: "sess_1", RunID: "run_1", ToolCallID: "call_1"},
	} {
		client := &recordingApprovalClient{}
		c := Consumer{JWTSecret: "secret", ApprovalClient: client}
		token := signTestToken(t, "secret", Claims{Binding: binding, Iss: approvalTokenIssuer, Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix()})
		_, err := c.AuthorizeWrite(context.Background(), WriteAuthorization{ApprovalToken: token, ProvenanceToken: signProvenance(t, "secret", provenanceClaims(t, args)), Tool: "files.create", Args: args, AgentID: "general_assistant", PhysicalPath: "sessions/sess_1/runs/run_1/files/notes/todo.txt"})
		if err == nil || client.request != nil {
			t.Fatalf("unbound write reached consume: %v", err)
		}
	}
}
