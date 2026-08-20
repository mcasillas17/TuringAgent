package approval

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func signProvenance(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()
	headerBytes, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	input := base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(payloadBytes)
	return input + "." + hmacSignature(input, secret)
}

func provenanceClaims(t *testing.T, args map[string]any) map[string]any {
	t.Helper()
	return map[string]any{
		"iss": "turing.orchestrator", "aud": "mcp-files", "sub": "general_assistant",
		"jti": "prov_1", "kind": "provenance", "sid": "sess_1", "rid": "run_1", "gen": 0,
		"tool": "files.create", "args_hash": hashArgs(t, args), "path": "notes/todo.txt",
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
	}
}

func TestVerifyProvenanceReturnsBoundScope(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "notes/todo.txt"}
	consumer := Consumer{JWTSecret: "secret"}

	claims, err := consumer.VerifyProvenance(signProvenance(t, "secret", provenanceClaims(t, args)), "files.create", args, "general_assistant")
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}

	if claims.SessionID != "sess_1" || claims.RunID != "run_1" || claims.LogicalPath != "notes/todo.txt" {
		t.Fatalf("claims = %+v, want the session, run and path scope", claims)
	}
	if claims.DeletionGeneration != 0 {
		t.Fatalf("deletion generation = %d, want 0", claims.DeletionGeneration)
	}
}

func TestVerifyProvenanceRejectsMismatchedBindings(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "notes/todo.txt"}
	cases := map[string]func(map[string]any){
		"audience":  func(c map[string]any) { c["aud"] = "someone-else" },
		"issuer":    func(c map[string]any) { c["iss"] = "someone-else" },
		"subject":   func(c map[string]any) { c["sub"] = "other_agent" },
		"tool":      func(c map[string]any) { c["tool"] = "files.update" },
		"args hash": func(c map[string]any) { c["args_hash"] = "sha256:other" },
		"kind":      func(c map[string]any) { c["kind"] = "" },
		"expiry":    func(c map[string]any) { c["exp"] = time.Now().Add(-time.Second).Unix() },
		"session":   func(c map[string]any) { c["sid"] = "" },
		"run":       func(c map[string]any) { c["rid"] = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			claims := provenanceClaims(t, args)
			mutate(claims)
			consumer := Consumer{JWTSecret: "secret"}

			if _, err := consumer.VerifyProvenance(signProvenance(t, "secret", claims), "files.create", args, "general_assistant"); err == nil {
				t.Fatal("VerifyProvenance accepted a capability that was not bound to this call")
			}
		})
	}
}

func TestVerifyProvenanceRejectsForeignSignature(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "notes/todo.txt"}
	consumer := Consumer{JWTSecret: "secret"}

	if _, err := consumer.VerifyProvenance(signProvenance(t, "other-secret", provenanceClaims(t, args)), "files.create", args, "general_assistant"); err == nil {
		t.Fatal("VerifyProvenance accepted a capability this server did not issue")
	}
}

func TestValidateRejectsProvenanceCapabilityPresentedAsApproval(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "notes/todo.txt"}
	consumer := Consumer{JWTSecret: "secret", ApprovalClient: &recordingApprovalClient{}}
	capability := signProvenance(t, "secret", provenanceClaims(t, args))

	err := consumer.Validate(capability, "files.create", args, "general_assistant")

	if err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("Validate error = %v, want a refusal naming the provenance capability", err)
	}
}

func TestAuthorizeWriteConsumesApprovalAndReturnsReservation(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "notes/todo.txt"}
	client := &recordingApprovalClient{
		response: &turingv1.ApprovalResponse{
			ApprovalId: "appr_1",
			Status:     turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED,
			Reservation: &turingv1.SandboxArtifactReservation{
				ArtifactId: "sbxa_1", PhysicalPath: "sessions/sess_1/runs/run_1/files/notes/todo.txt",
				Policy: "delete_on_session_delete",
			},
		},
	}
	consumer := Consumer{JWTSecret: "secret", InternalToken: "internal", ApprovalClient: client}
	approvalToken := signTestToken(t, "secret", Claims{
		Iss: "turing.orchestrator", Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1",
		Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix(),
	})
	provenanceToken := signProvenance(t, "secret", provenanceClaims(t, args))

	reservation, err := consumer.AuthorizeWrite(context.Background(), WriteAuthorization{
		ApprovalToken:   approvalToken,
		ProvenanceToken: provenanceToken,
		Tool:            "files.create",
		Args:            args,
		AgentID:         "general_assistant",
		PhysicalPath:    "sessions/sess_1/runs/run_1/files/notes/todo.txt",
	})
	if err != nil {
		t.Fatalf("AuthorizeWrite: %v", err)
	}

	if reservation.ArtifactID != "sbxa_1" || reservation.PhysicalPath != "sessions/sess_1/runs/run_1/files/notes/todo.txt" {
		t.Fatalf("reservation = %+v", reservation)
	}
	if client.request.GetApprovalId() != "appr_1" || client.request.GetProvenanceToken() != provenanceToken {
		t.Fatalf("consume request = %+v, want the approval and the capability", client.request)
	}
	if client.request.GetPhysicalPath() != "sessions/sess_1/runs/run_1/files/notes/todo.txt" {
		t.Fatalf("consume physical path = %q", client.request.GetPhysicalPath())
	}
}

func TestAuthorizeWriteRejectsReservationForAnotherPath(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "notes/todo.txt"}
	client := &recordingApprovalClient{
		response: &turingv1.ApprovalResponse{
			ApprovalId: "appr_1",
			Status:     turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED,
			Reservation: &turingv1.SandboxArtifactReservation{
				ArtifactId: "sbxa_1", PhysicalPath: "sessions/sess_1/runs/run_1/files/somewhere/else.txt",
			},
		},
	}
	consumer := Consumer{JWTSecret: "secret", ApprovalClient: client}
	approvalToken := signTestToken(t, "secret", Claims{
		Iss: "turing.orchestrator", Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1",
		Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix(),
	})

	_, err := consumer.AuthorizeWrite(context.Background(), WriteAuthorization{
		ApprovalToken:   approvalToken,
		ProvenanceToken: signProvenance(t, "secret", provenanceClaims(t, args)),
		Tool:            "files.create",
		Args:            args,
		AgentID:         "general_assistant",
		PhysicalPath:    "sessions/sess_1/runs/run_1/files/notes/todo.txt",
	})

	if err == nil {
		t.Fatal("AuthorizeWrite accepted a reservation for a path it is not about to write")
	}
}

func TestAuthorizeWriteRequiresAReservation(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "notes/todo.txt"}
	client := &recordingApprovalClient{
		response: &turingv1.ApprovalResponse{ApprovalId: "appr_1", Status: turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED},
	}
	consumer := Consumer{JWTSecret: "secret", ApprovalClient: client}
	approvalToken := signTestToken(t, "secret", Claims{
		Iss: "turing.orchestrator", Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1",
		Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix(),
	})

	_, err := consumer.AuthorizeWrite(context.Background(), WriteAuthorization{
		ApprovalToken:   approvalToken,
		ProvenanceToken: signProvenance(t, "secret", provenanceClaims(t, args)),
		Tool:            "files.create",
		Args:            args,
		AgentID:         "general_assistant",
		PhysicalPath:    "sessions/sess_1/runs/run_1/files/notes/todo.txt",
	})

	if err == nil {
		t.Fatal("AuthorizeWrite proceeded without a durable artifact reservation")
	}
}

func TestFinalizeWriteReportsOutcomeOverTheInternalChannel(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "notes/todo.txt"}
	client := &recordingApprovalClient{finalizeResponse: &turingv1.FinalizeSandboxArtifactResponse{ArtifactId: "sbxa_1", State: "ready"}}
	consumer := Consumer{JWTSecret: "secret", InternalToken: "internal", ApprovalClient: client}
	token := signProvenance(t, "secret", provenanceClaims(t, args))

	if err := consumer.FinalizeWrite(context.Background(), "sbxa_1", token, true); err != nil {
		t.Fatalf("FinalizeWrite: %v", err)
	}

	if client.finalizeRequest.GetArtifactId() != "sbxa_1" || !client.finalizeRequest.GetCommitted() {
		t.Fatalf("finalize request = %+v", client.finalizeRequest)
	}
	if client.finalizeRequest.GetProvenanceToken() != token {
		t.Fatal("finalize did not carry the capability the reservation was taken under")
	}
	if client.finalizeAuthorization != "Bearer internal" {
		t.Fatalf("finalize authorization = %q, want the internal bearer token", client.finalizeAuthorization)
	}
}

func TestFinalizeWriteSurfacesRefusal(t *testing.T) {
	client := &recordingApprovalClient{finalizeErr: status.Error(codes.FailedPrecondition, "not yours")}
	consumer := Consumer{JWTSecret: "secret", ApprovalClient: client}

	err := consumer.FinalizeWrite(context.Background(), "sbxa_1", "token", true)

	if err == nil {
		t.Fatal("FinalizeWrite swallowed a refusal from the orchestrator")
	}
}

func TestFinalizeWriteOverGRPCUsesTheApprovalChannel(t *testing.T) {
	service := &recordingApprovalService{status: turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED}
	addr, dialer := startApprovalServer(t, service)
	consumer := Consumer{
		OrchestratorGRPCAddr: addr,
		InternalToken:        "internal",
		JWTSecret:            "secret",
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(dialer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	}

	if err := consumer.FinalizeWrite(context.Background(), "sbxa_1", "token", true); err != nil {
		t.Fatalf("FinalizeWrite: %v", err)
	}

	if service.finalizedArtifactID != "sbxa_1" {
		t.Fatalf("finalized artifact = %q, want sbxa_1 over the existing internal channel", service.finalizedArtifactID)
	}
}

type recordingApprovalClient struct {
	request               *turingv1.ConsumeApprovalRequest
	response              *turingv1.ApprovalResponse
	err                   error
	finalizeRequest       *turingv1.FinalizeSandboxArtifactRequest
	finalizeResponse      *turingv1.FinalizeSandboxArtifactResponse
	finalizeErr           error
	finalizeAuthorization string

	capabilityRequest       *turingv1.CheckSessionCapabilityRequest
	capabilityState         *turingv1.SessionCapabilityState
	capabilityErr           error
	capabilityAuthorization string
}

func (c *recordingApprovalClient) CheckSessionCapability(ctx context.Context, req *turingv1.CheckSessionCapabilityRequest, _ ...grpc.CallOption) (*turingv1.SessionCapabilityState, error) {
	c.capabilityRequest = req
	c.capabilityAuthorization = outgoingAuthorization(ctx)
	if c.capabilityErr != nil {
		return nil, c.capabilityErr
	}
	return c.capabilityState, nil
}

func (c *recordingApprovalClient) ConsumeApproval(_ context.Context, req *turingv1.ConsumeApprovalRequest, _ ...grpc.CallOption) (*turingv1.ApprovalResponse, error) {
	c.request = req
	if c.err != nil {
		return nil, c.err
	}
	return c.response, nil
}

func (c *recordingApprovalClient) FinalizeSandboxArtifact(ctx context.Context, req *turingv1.FinalizeSandboxArtifactRequest, _ ...grpc.CallOption) (*turingv1.FinalizeSandboxArtifactResponse, error) {
	c.finalizeRequest = req
	c.finalizeAuthorization = outgoingAuthorization(ctx)
	if c.finalizeErr != nil {
		return nil, c.finalizeErr
	}
	return c.finalizeResponse, nil
}

func hmacSignature(input string, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func outgoingAuthorization(ctx context.Context) string {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func TestCheckSessionAcceptsAnActiveSession(t *testing.T) {
	client := &recordingApprovalClient{capabilityState: &turingv1.SessionCapabilityState{Active: true}}
	consumer := Consumer{JWTSecret: "secret", InternalToken: "internal", ApprovalClient: client}

	if err := consumer.CheckSession(context.Background(), "capability"); err != nil {
		t.Fatalf("CheckSession: %v", err)
	}
	if client.capabilityRequest.GetProvenanceToken() != "capability" {
		t.Fatalf("request = %+v, want the capability forwarded", client.capabilityRequest)
	}
	if client.capabilityAuthorization != "Bearer internal" {
		t.Fatalf("authorization = %q, want the internal bearer token", client.capabilityAuthorization)
	}
}

func TestCheckSessionRefusesAWithdrawingSession(t *testing.T) {
	client := &recordingApprovalClient{capabilityState: &turingv1.SessionCapabilityState{Active: false, DeletionGeneration: 1}}
	consumer := Consumer{JWTSecret: "secret", ApprovalClient: client}

	err := consumer.CheckSession(context.Background(), "capability")

	if err == nil {
		t.Fatal("CheckSession accepted a capability whose session is being withdrawn")
	}
	if !strings.Contains(err.Error(), "deletion") {
		t.Fatalf("error = %v, want it to name the deletion in progress", err)
	}
}

func TestCheckSessionSurfacesTransportFailures(t *testing.T) {
	client := &recordingApprovalClient{capabilityErr: status.Error(codes.Unavailable, "orchestrator down")}
	consumer := Consumer{JWTSecret: "secret", ApprovalClient: client}

	if err := consumer.CheckSession(context.Background(), "capability"); err == nil {
		t.Fatal("CheckSession treated an unreachable orchestrator as an active session")
	}
}

func TestCheckSessionRequiresACapability(t *testing.T) {
	consumer := Consumer{JWTSecret: "secret", ApprovalClient: &recordingApprovalClient{}}

	if err := consumer.CheckSession(context.Background(), ""); err == nil {
		t.Fatal("CheckSession accepted a call with no capability")
	}
}

func TestCheckSessionOverGRPCUsesTheApprovalChannel(t *testing.T) {
	service := &recordingApprovalService{status: turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED, capabilityActive: true}
	addr, dialer := startApprovalServer(t, service)
	consumer := Consumer{
		OrchestratorGRPCAddr: addr,
		InternalToken:        "internal",
		JWTSecret:            "secret",
		DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(dialer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	}

	if err := consumer.CheckSession(context.Background(), "capability"); err != nil {
		t.Fatalf("CheckSession: %v", err)
	}

	if service.checkedCapability != "capability" {
		t.Fatalf("checked capability = %q, want it sent over the existing internal channel", service.checkedCapability)
	}
}
