package agents

import (
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A delegation session's destination is not the user's to change: both RPCs
// are refused with the read-only reason and leave the destination as it was,
// which can still be read.
func TestRoutingADelegationSessionIsRefused(t *testing.T) {
	server, repo, database, ctx := newAgentServer(t, "claude")
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := server.CreateExternalAgent(ctx, createRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.SetSessionAgent(ctx, &turingv1.SetSessionAgentRequest{
		SessionId: session.SessionID, AgentId: agent.GetAgentId(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE sessions SET kind = 'delegation' WHERE id = ?`, session.SessionID); err != nil {
		t.Fatal(err)
	}

	_, setErr := server.SetSessionAgent(ctx, &turingv1.SetSessionAgentRequest{
		SessionId: session.SessionID, AgentId: agent.GetAgentId(),
	})
	_, clearErr := server.ClearSessionAgent(ctx, &turingv1.ClearSessionAgentRequest{SessionId: session.SessionID})
	for name, err := range map[string]error{"SetSessionAgent": setErr, "ClearSessionAgent": clearErr} {
		if status.Code(err) != codes.FailedPrecondition || status.Convert(err).Message() != "delegation sessions are read-only" {
			t.Fatalf("%s on a delegation session = %v, want FailedPrecondition read-only", name, err)
		}
	}
	read, err := server.GetSessionAgent(ctx, &turingv1.GetSessionAgentRequest{SessionId: session.SessionID})
	if err != nil || read.GetAgent().GetAgentId() != agent.GetAgentId() {
		t.Fatalf("destination after the refusals = %v, %v; want the agent it already had", read.GetAgent(), err)
	}
}
