package approval

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
)

// A deterministic JSON value exposes how often the complete argument object is
// canonicalized without adding production counters or replacing verification.
type countedJSONArgument struct{ calls *int }

func (v countedJSONArgument) MarshalJSON() ([]byte, error) {
	*v.calls++
	return []byte(`"fixture"`), nil
}

func TestWriteVerificationRunsOncePerDistinctBoundary(t *testing.T) {
	canonicalizations := 0
	args := map[string]any{"path": "notes/todo.txt", "content": "reviewed", "annotation": countedJSONArgument{calls: &canonicalizations}}
	physical := "sessions/sess_1/runs/run_1/files/notes/todo.txt"
	binding := approvalpreview.Binding{PreviewHash: "preview", SessionID: "sess_1", RunID: "run_1", ToolCallID: "call_1", PhysicalPath: physical, AfterHash: approvalpreview.Hash("reviewed")}
	approvalToken := signTestToken(t, "secret", Claims{Binding: binding, Iss: approvalTokenIssuer, Aud: "mcp-files", Sub: "general_assistant",
		JTI: "appr_verified", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix()})
	provenanceToken := signProvenance(t, "secret", provenanceClaims(t, args))
	client := &recordingApprovalClient{response: &turingv1.ApprovalResponse{Status: turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED,
		Reservation: &turingv1.SandboxArtifactReservation{ArtifactId: "artifact", PhysicalPath: physical}}}
	consumer := Consumer{JWTSecret: "secret", ApprovalClient: client}
	req := WriteAuthorization{ApprovalToken: approvalToken, ProvenanceToken: provenanceToken, Tool: "files.create", Args: args,
		AgentID: "general_assistant", PhysicalPath: physical}
	canonicalizations = 0
	verified, err := consumer.VerifyWrite(req)
	if err != nil || verified != binding {
		t.Fatalf("initial verification=%+v %v", verified, err)
	}
	if canonicalizations != 2 {
		t.Fatalf("initial boundary canonicalizations=%d, want one approval and one provenance check", canonicalizations)
	}
	if client.request != nil {
		t.Fatal("precondition verification consumed approval")
	}
	canonicalizations = 0
	if _, err := consumer.AuthorizeWrite(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if canonicalizations != 2 {
		t.Fatalf("consume boundary canonicalizations=%d, want one approval and one provenance check", canonicalizations)
	}
	if client.request.GetApprovalId() != "appr_verified" || client.request.GetPhysicalPath() != physical || client.request.GetProvenanceToken() != provenanceToken {
		t.Fatal("consume did not use the verified claims and original provenance")
	}
	client.request = nil
	req.Args["content"] = "changed after the earlier boundary"
	if _, err := consumer.AuthorizeWrite(context.Background(), req); err == nil || client.request != nil {
		t.Fatal("a later boundary reused verification for changed arguments")
	}
}
