package sessions

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A child session is read-only to the client: every lifecycle RPC is refused,
// and deleting it is left to its parent's deletion lifecycle.
func TestLifecycleRPCsRefuseADelegationSession(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Child")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(ctx, `UPDATE sessions SET kind = 'delegation' WHERE id = ?`, session.SessionID); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"rename": func() error {
			_, err := client.RenameSession(ctx, &turingv1.RenameSessionRequest{SessionId: session.SessionID, Title: "Renamed"})
			return err
		},
		"archive": func() error {
			_, err := client.ArchiveSession(ctx, &turingv1.ArchiveSessionRequest{SessionId: session.SessionID})
			return err
		},
		"restore": func() error {
			_, err := client.RestoreSession(ctx, &turingv1.RestoreSessionRequest{SessionId: session.SessionID})
			return err
		},
		"delete": func() error {
			_, err := client.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
			return err
		},
	} {
		err := call()
		if status.Code(err) != codes.FailedPrecondition || status.Convert(err).Message() != "delegation sessions are read-only" {
			t.Fatalf("%s = %v, want FailedPrecondition read-only", name, err)
		}
	}
	got, err := client.GetSession(ctx, &turingv1.GetSessionRequest{SessionId: session.SessionID})
	if err != nil || got.GetTitle() != "Child" {
		t.Fatalf("GetSession = %v, %v; want the unchanged child", got, err)
	}
	listed, err := client.ListSessions(ctx, &turingv1.ListSessionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, listedSession := range listed.GetSessions() {
		if listedSession.GetSessionId() == session.SessionID {
			t.Fatal("ListSessions returned a delegation session")
		}
	}
}
