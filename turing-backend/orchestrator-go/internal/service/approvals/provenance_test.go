package approvals

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *approvalHarness) approvedFileWrite(t *testing.T) (repository.EnqueueUserMessageResult, string) {
	t.Helper()
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	return enqueued, approvalID
}

func (h *approvalHarness) provenanceFor(t *testing.T, sessionID string, approvalID string, logicalPath string) string {
	t.Helper()
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := h.service.IssueToolProvenance(context.Background(), ProvenanceRequest{
		SessionID:   sessionID,
		RunID:       approval.RunID,
		AgentID:     approval.AgentID,
		ToolName:    approval.ToolName,
		ArgsHash:    approval.ArgsHash,
		LogicalPath: logicalPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestIssueToolProvenanceBindsCapabilityToSessionRunAndPath(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)

	token, err := h.service.IssueToolProvenance(context.Background(), ProvenanceRequest{
		SessionID:   enqueued.SessionID,
		RunID:       enqueued.RunID,
		AgentID:     "general_assistant",
		ToolName:    "files.create",
		ArgsHash:    "sha256:abc",
		LogicalPath: "notes/todo.txt",
	})
	if err != nil {
		t.Fatalf("IssueToolProvenance: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("provenance token = %q, want a JWT", token)
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["kind"] != "provenance" {
		t.Fatalf("kind = %#v, want provenance so it cannot stand in for an approval", payload["kind"])
	}
	if payload["aud"] != "mcp-files" || payload["iss"] != "turing.orchestrator" {
		t.Fatalf("payload = %+v, want the mcp-files audience from the orchestrator", payload)
	}
	if payload["sid"] != enqueued.SessionID || payload["rid"] != enqueued.RunID {
		t.Fatalf("payload = %+v, want session %q and run %q", payload, enqueued.SessionID, enqueued.RunID)
	}
	if payload["tool"] != "files.create" || payload["args_hash"] != "sha256:abc" || payload["path"] != "notes/todo.txt" {
		t.Fatalf("payload = %+v, want the tool, args hash and path scope bound", payload)
	}
	if payload["gen"] != float64(0) {
		t.Fatalf("gen = %#v, want the session's current deletion generation", payload["gen"])
	}
	expiry, ok := payload["exp"].(float64)
	if !ok || expiry <= float64(time.Now().Unix()) {
		t.Fatalf("exp = %#v, want a future expiry", payload["exp"])
	}
	if jti, ok := payload["jti"].(string); !ok || jti == "" {
		t.Fatalf("jti = %#v, want a unique capability id", payload["jti"])
	}
}

func TestIssueToolProvenanceReadsGenerationFromServerState(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	// The request type carries no generation at all, so the only place one can
	// come from is storage.
	token, err := h.service.IssueToolProvenance(context.Background(), ProvenanceRequest{
		SessionID:   enqueued.SessionID,
		RunID:       enqueued.RunID,
		AgentID:     "general_assistant",
		ToolName:    "files.read",
		ArgsHash:    "sha256:abc",
		LogicalPath: "note.txt",
	})
	if err != nil {
		t.Fatalf("IssueToolProvenance: %v", err)
	}
	claims, err := h.service.verifyProvenanceToken(token)
	if err != nil {
		t.Fatal(err)
	}
	current, err := h.repo.SessionDeletionGeneration(context.Background(), enqueued.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if claims.DeletionGeneration != current {
		t.Fatalf("generation = %d, want the session's current %d", claims.DeletionGeneration, current)
	}
}

func TestIssueToolProvenanceRejectsUnknownSession(t *testing.T) {
	h := newApprovalHarness(t)

	_, err := h.service.IssueToolProvenance(context.Background(), ProvenanceRequest{
		SessionID: "sess_missing", RunID: "run_missing", AgentID: "general_assistant",
		ToolName: "files.read", ArgsHash: "sha256:abc", LogicalPath: "note.txt",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("unknown session error = %v, want NotFound", err)
	}
}

func TestVerifyProvenanceTokenRejectsForeignSignature(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	token, err := h.service.IssueToolProvenance(context.Background(), ProvenanceRequest{
		SessionID: enqueued.SessionID, RunID: enqueued.RunID, AgentID: "general_assistant",
		ToolName: "files.create", ArgsHash: "sha256:abc", LogicalPath: "notes/todo.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	forged := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString([]byte("not-a-signature"))

	if _, err := h.service.verifyProvenanceToken(forged); err == nil {
		t.Fatal("verifyProvenanceToken accepted a forged signature")
	}
}

func TestConsumeApprovalReservesArtifactBeforeReportingSuccess(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued, approvalID := h.approvedFileWrite(t)
	token := h.provenanceFor(t, enqueued.SessionID, approvalID, "note.txt")
	owned := repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, "note.txt")

	response, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId: approvalID, ProvenanceToken: token, PhysicalPath: owned,
	})
	if err != nil {
		t.Fatalf("ConsumeApproval: %v", err)
	}
	if response.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED {
		t.Fatalf("status = %v, want consumed", response.GetStatus())
	}
	reservation := response.GetReservation()
	if reservation.GetArtifactId() == "" {
		t.Fatal("consume returned no artifact reservation")
	}
	if reservation.GetPhysicalPath() != owned {
		t.Fatalf("reserved path = %q, want %q", reservation.GetPhysicalPath(), owned)
	}
	if reservation.GetPolicy() != repository.SandboxArtifactPolicyDeleteOnSessionDelete {
		t.Fatalf("policy = %q, want %q", reservation.GetPolicy(), repository.SandboxArtifactPolicyDeleteOnSessionDelete)
	}
	artifacts, err := h.repo.SessionSandboxArtifacts(context.Background(), enqueued.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].State != repository.SandboxArtifactStateWriting {
		t.Fatalf("artifacts = %+v, want one durable writing reservation", artifacts)
	}
}

func TestConsumeApprovalRequiresProvenanceCapability(t *testing.T) {
	h := newApprovalHarness(t)
	_, approvalID := h.approvedFileWrite(t)

	_, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{ApprovalId: approvalID})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("consume without provenance error = %v, want FailedPrecondition", err)
	}
	approval, getErr := h.repo.GetApproval(context.Background(), approvalID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if approval.Status != "approved" {
		t.Fatalf("approval status = %q, want the approval left unspent", approval.Status)
	}
}

func TestConsumeApprovalRejectsProvenanceForAnotherToolCall(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued, approvalID := h.approvedFileWrite(t)
	foreign, err := h.service.IssueToolProvenance(context.Background(), ProvenanceRequest{
		SessionID: enqueued.SessionID, RunID: enqueued.RunID, AgentID: "general_assistant",
		ToolName: "files.update", ArgsHash: "sha256:someone-elses-arguments", LogicalPath: "note.txt",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId:      approvalID,
		ProvenanceToken: foreign,
		PhysicalPath:    repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, "note.txt"),
	})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("mismatched provenance error = %v, want FailedPrecondition", err)
	}
	if count := len(h.sessionArtifacts(t, enqueued.SessionID)); count != 0 {
		t.Fatalf("refused consume reserved %d artifacts", count)
	}
}

func TestConsumeApprovalRejectsPathOutsideCapabilityScope(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued, approvalID := h.approvedFileWrite(t)
	token := h.provenanceFor(t, enqueued.SessionID, approvalID, "note.txt")

	_, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId:      approvalID,
		ProvenanceToken: token,
		PhysicalPath:    "sessions/" + enqueued.SessionID + "/runs/" + enqueued.RunID + "/files/elsewhere.txt",
	})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("out-of-scope path error = %v, want FailedPrecondition", err)
	}
	if count := len(h.sessionArtifacts(t, enqueued.SessionID)); count != 0 {
		t.Fatalf("refused consume reserved %d artifacts", count)
	}
}

func TestConsumeApprovalReleasesReservationWhenApprovalCannotBeSpent(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued, approvalID := h.approvedFileWrite(t)
	token := h.provenanceFor(t, enqueued.SessionID, approvalID, "note.txt")
	owned := repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, "note.txt")
	if _, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId: approvalID, ProvenanceToken: token, PhysicalPath: owned,
	}); err != nil {
		t.Fatal(err)
	}
	first := h.sessionArtifacts(t, enqueued.SessionID)
	if len(first) != 1 {
		t.Fatalf("artifacts after first consume = %+v", first)
	}

	// A replayed consume must not leave a second reservation behind for a write
	// that will never be authorised.
	if _, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId: approvalID, ProvenanceToken: token, PhysicalPath: owned,
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("replayed consume error = %v, want FailedPrecondition", err)
	}
	second := h.sessionArtifacts(t, enqueued.SessionID)
	if len(second) != 1 || second[0].ArtifactID != first[0].ArtifactID {
		t.Fatalf("artifacts after replay = %+v, want the original reservation only", second)
	}
}

func TestConsumeApprovalRefusesWhileSessionIsBeingWithdrawn(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued, approvalID := h.approvedFileWrite(t)
	token := h.provenanceFor(t, enqueued.SessionID, approvalID, "note.txt")
	owned := repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, "note.txt")
	if _, err := h.repo.BeginSessionDeletion(context.Background(), enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	_, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId: approvalID, ProvenanceToken: token, PhysicalPath: owned,
	})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("consume during withdrawal error = %v, want FailedPrecondition", err)
	}
	if count := len(h.sessionArtifacts(t, enqueued.SessionID)); count != 0 {
		t.Fatalf("withdrawal-time consume reserved %d artifacts", count)
	}
}

func TestFinalizeSandboxArtifactMarksReservationReady(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued, approvalID := h.approvedFileWrite(t)
	token := h.provenanceFor(t, enqueued.SessionID, approvalID, "note.txt")
	owned := repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, "note.txt")
	consumed, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId: approvalID, ProvenanceToken: token, PhysicalPath: owned,
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := h.service.FinalizeSandboxArtifact(context.Background(), &turingv1.FinalizeSandboxArtifactRequest{
		ArtifactId: consumed.GetReservation().GetArtifactId(), ProvenanceToken: token, Committed: true,
	})
	if err != nil {
		t.Fatalf("FinalizeSandboxArtifact: %v", err)
	}
	if response.GetState() != repository.SandboxArtifactStateReady {
		t.Fatalf("state = %q, want ready", response.GetState())
	}
	artifacts := h.sessionArtifacts(t, enqueued.SessionID)
	if len(artifacts) != 1 || artifacts[0].State != repository.SandboxArtifactStateReady {
		t.Fatalf("artifacts = %+v, want one ready artifact", artifacts)
	}
}

func TestFinalizeSandboxArtifactReleasesUncommittedReservation(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued, approvalID := h.approvedFileWrite(t)
	token := h.provenanceFor(t, enqueued.SessionID, approvalID, "note.txt")
	owned := repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, "note.txt")
	consumed, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId: approvalID, ProvenanceToken: token, PhysicalPath: owned,
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := h.service.FinalizeSandboxArtifact(context.Background(), &turingv1.FinalizeSandboxArtifactRequest{
		ArtifactId: consumed.GetReservation().GetArtifactId(), ProvenanceToken: token, Committed: false,
	})
	if err != nil {
		t.Fatalf("FinalizeSandboxArtifact: %v", err)
	}
	if response.GetState() != "released" {
		t.Fatalf("state = %q, want released", response.GetState())
	}
	if count := len(h.sessionArtifacts(t, enqueued.SessionID)); count != 0 {
		t.Fatalf("released reservation left %d artifacts", count)
	}
}

func TestFinalizeSandboxArtifactRejectsCapabilityForAnotherArtifact(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued, approvalID := h.approvedFileWrite(t)
	token := h.provenanceFor(t, enqueued.SessionID, approvalID, "note.txt")
	owned := repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, "note.txt")
	consumed, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId: approvalID, ProvenanceToken: token, PhysicalPath: owned,
	})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := h.service.IssueToolProvenance(context.Background(), ProvenanceRequest{
		SessionID: enqueued.SessionID, RunID: enqueued.RunID, AgentID: "general_assistant",
		ToolName: "files.update", ArgsHash: "sha256:other", LogicalPath: "other.txt",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = h.service.FinalizeSandboxArtifact(context.Background(), &turingv1.FinalizeSandboxArtifactRequest{
		ArtifactId: consumed.GetReservation().GetArtifactId(), ProvenanceToken: foreign, Committed: true,
	})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("foreign finalization error = %v, want FailedPrecondition", err)
	}
	artifacts := h.sessionArtifacts(t, enqueued.SessionID)
	if len(artifacts) != 1 || artifacts[0].State != repository.SandboxArtifactStateWriting {
		t.Fatalf("artifacts = %+v, want the reservation untouched", artifacts)
	}
}

func TestFinalizeSandboxArtifactIsInternalOnly(t *testing.T) {
	h := newApprovalHarness(t)

	_, err := NewPublicServer(h.service).FinalizeSandboxArtifact(context.Background(), &turingv1.FinalizeSandboxArtifactRequest{ArtifactId: "sbxa_1"})

	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public FinalizeSandboxArtifact error = %v, want PermissionDenied", err)
	}
}

func (h *approvalHarness) sessionArtifacts(t *testing.T, sessionID string) []repository.SandboxArtifact {
	t.Helper()
	artifacts, err := h.repo.SessionSandboxArtifacts(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return artifacts
}

// consumeRequest builds the request mcp-files sends: the approval, the
// server-issued capability for the same tool call, and the location the write
// resolved to.
func (h *approvalHarness) consumeRequest(t *testing.T, enqueued repository.EnqueueUserMessageResult, approvalID string, logicalPath string) *turingv1.ConsumeApprovalRequest {
	t.Helper()
	return &turingv1.ConsumeApprovalRequest{
		ApprovalId:      approvalID,
		ProvenanceToken: h.provenanceFor(t, enqueued.SessionID, approvalID, logicalPath),
		PhysicalPath:    repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, logicalPath),
	}
}

func TestFinalizeSandboxArtifactAcceptsAnEarlierRunsLocation(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued, approvalID := h.approvedFileWrite(t)
	later, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: enqueued.SessionID, Content: "update it", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	// A capability for the later run, writing the file the earlier run made.
	token, err := h.service.IssueToolProvenance(context.Background(), ProvenanceRequest{
		SessionID: enqueued.SessionID, RunID: later.RunID, AgentID: approval.AgentID,
		ToolName: approval.ToolName, ArgsHash: approval.ArgsHash, LogicalPath: "note.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	reserved, _, err := h.repo.ReserveSandboxArtifact(context.Background(), repository.ReserveSandboxArtifactInput{
		SessionID:    enqueued.SessionID,
		RunID:        later.RunID,
		LogicalPath:  "note.txt",
		PhysicalPath: repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, "note.txt"),
	})
	if err != nil {
		t.Fatalf("cross-run reservation: %v", err)
	}

	response, err := h.service.FinalizeSandboxArtifact(context.Background(), &turingv1.FinalizeSandboxArtifactRequest{
		ArtifactId: reserved.ArtifactID, ProvenanceToken: token, Committed: true,
	})
	if err != nil {
		t.Fatalf("FinalizeSandboxArtifact for a cross-run update: %v", err)
	}
	if response.GetState() != repository.SandboxArtifactStateReady {
		t.Fatalf("state = %q, want ready", response.GetState())
	}
}

func TestIssueToolProvenanceRefusesAWithdrawingSession(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	if _, err := h.repo.BeginSessionDeletion(context.Background(), enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	_, err := h.service.IssueToolProvenance(context.Background(), ProvenanceRequest{
		SessionID: enqueued.SessionID, RunID: enqueued.RunID, AgentID: "general_assistant",
		ToolName: "files.read", ArgsHash: "sha256:abc", LogicalPath: "note.txt",
	})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("issue during withdrawal error = %v, want FailedPrecondition", err)
	}
}

func TestCheckSessionCapabilityReportsAnActiveSession(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	token := h.readCapability(t, enqueued, "note.txt")

	state, err := h.service.CheckSessionCapability(context.Background(), &turingv1.CheckSessionCapabilityRequest{ProvenanceToken: token})
	if err != nil {
		t.Fatalf("CheckSessionCapability: %v", err)
	}

	if !state.GetActive() || state.GetDeletionGeneration() != 0 {
		t.Fatalf("state = %+v, want an active session at generation 0", state)
	}
}

func TestCheckSessionCapabilityReportsAWithdrawalStartedAfterIssuance(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	// Issued while the session was still active, which is exactly the capability
	// a read holds when a withdrawal starts underneath it.
	token := h.readCapability(t, enqueued, "note.txt")
	if _, err := h.repo.BeginSessionDeletion(context.Background(), enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	state, err := h.service.CheckSessionCapability(context.Background(), &turingv1.CheckSessionCapabilityRequest{ProvenanceToken: token})
	if err != nil {
		t.Fatalf("CheckSessionCapability: %v", err)
	}

	if state.GetActive() {
		t.Fatalf("state = %+v, want the capability reported inactive during withdrawal", state)
	}
	if state.GetDeletionGeneration() != 1 {
		t.Fatalf("generation = %d, want the session's current generation", state.GetDeletionGeneration())
	}
}

func TestCheckSessionCapabilityRejectsAnInvalidCapability(t *testing.T) {
	h := newApprovalHarness(t)

	for name, token := range map[string]string{"empty": "", "garbage": "not.a.token"} {
		t.Run(name, func(t *testing.T) {
			if _, err := h.service.CheckSessionCapability(context.Background(), &turingv1.CheckSessionCapabilityRequest{ProvenanceToken: token}); err == nil {
				t.Fatal("CheckSessionCapability accepted a capability this server did not issue")
			}
		})
	}
}

func TestCheckSessionCapabilityIsInternalOnly(t *testing.T) {
	h := newApprovalHarness(t)

	_, err := NewPublicServer(h.service).CheckSessionCapability(context.Background(), &turingv1.CheckSessionCapabilityRequest{ProvenanceToken: "token"})

	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public CheckSessionCapability error = %v, want PermissionDenied", err)
	}
}

func TestFinalizeSandboxArtifactRetainsTheArtifactButFailsWhenWithdrawalStarted(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued, approvalID := h.approvedFileWrite(t)
	token := h.provenanceFor(t, enqueued.SessionID, approvalID, "note.txt")
	owned := repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, "note.txt")
	consumed, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId: approvalID, ProvenanceToken: token, PhysicalPath: owned,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The write lands, and only then does the withdrawal begin — the race the
	// post-write check exists for.
	if _, err := h.repo.BeginSessionDeletion(context.Background(), enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	_, err = h.service.FinalizeSandboxArtifact(context.Background(), &turingv1.FinalizeSandboxArtifactRequest{
		ArtifactId: consumed.GetReservation().GetArtifactId(), ProvenanceToken: token, Committed: true,
	})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("finalize during withdrawal error = %v, want FailedPrecondition", err)
	}
	if !strings.Contains(err.Error(), "deletion") {
		t.Fatalf("error = %v, want it to name the deletion in progress", err)
	}
	artifacts := h.sessionArtifacts(t, enqueued.SessionID)
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want the manifest row retained for cleanup", artifacts)
	}
	if artifacts[0].State != repository.SandboxArtifactStateReady {
		t.Fatalf("artifact state = %q, want %q so cleanup knows the file exists", artifacts[0].State, repository.SandboxArtifactStateReady)
	}
}

func TestFinalizeSandboxArtifactReleasesUncommittedReservationDuringWithdrawal(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued, approvalID := h.approvedFileWrite(t)
	token := h.provenanceFor(t, enqueued.SessionID, approvalID, "note.txt")
	owned := repository.OwnedSandboxPath(enqueued.SessionID, enqueued.RunID, "note.txt")
	consumed, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{
		ApprovalId: approvalID, ProvenanceToken: token, PhysicalPath: owned,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.BeginSessionDeletion(context.Background(), enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	// Nothing was written, so the reservation must go rather than leave the
	// withdrawal waiting on cleanup for a file that never existed.
	response, err := h.service.FinalizeSandboxArtifact(context.Background(), &turingv1.FinalizeSandboxArtifactRequest{
		ArtifactId: consumed.GetReservation().GetArtifactId(), ProvenanceToken: token, Committed: false,
	})
	if err != nil {
		t.Fatalf("release during withdrawal: %v", err)
	}
	if response.GetState() != "released" {
		t.Fatalf("state = %q, want released", response.GetState())
	}
	if count := len(h.sessionArtifacts(t, enqueued.SessionID)); count != 0 {
		t.Fatalf("released reservation left %d artifacts", count)
	}
}

func (h *approvalHarness) readCapability(t *testing.T, enqueued repository.EnqueueUserMessageResult, logicalPath string) string {
	t.Helper()
	token, err := h.service.IssueToolProvenance(context.Background(), ProvenanceRequest{
		SessionID: enqueued.SessionID, RunID: enqueued.RunID, AgentID: "general_assistant",
		ToolName: "files.read", ArgsHash: "sha256:abc", LogicalPath: logicalPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}
