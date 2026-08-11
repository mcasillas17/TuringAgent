package app

import (
	"context"
	"net"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	cfg := config.Config{
		ClientAPIKey:      "client",
		InternalToken:     "internal",
		ApprovalJWTSecret: "approval-secret",
		DatabasePath:      t.TempDir() + "/turing.db",
		OllamaModel:       "llama3.2",
		OpenAIModel:       "gpt-4o-mini",
	}
	app, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Stop)
	return app
}

func TestAppPassesConfiguredApprovalTTLToApprovalService(t *testing.T) {
	app, err := New(config.Config{
		ClientAPIKey:      "client",
		InternalToken:     "internal",
		ApprovalJWTSecret: "approval-secret",
		ApprovalTTLMS:     2000,
		DatabasePath:      t.TempDir() + "/turing.db",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Stop)
	session, err := app.Repository.CreateSession(context.Background(), "Approval TTL")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := app.Repository.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Repository.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := app.Repository.RecordToolCallBefore(context.Background(), repository.ToolCallRecord{ToolCallID: "call_ttl", RunID: enqueued.RunID}, "general_assistant", "files", "files.update", `{}`, "sha256:ttl"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	approvalID, err := app.ApprovalService.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_ttl", "general_assistant", "files.update", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := app.Repository.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, approval.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if ttl := expiresAt.Sub(started); ttl < time.Second || ttl > 3*time.Second {
		t.Fatalf("approval TTL = %v, want configured 2s", ttl)
	}
}

func newBufconnClient(t *testing.T, server *grpc.Server) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	go func() { _ = server.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestPublicServerRequiresClientToken(t *testing.T) {
	app := newTestApp(t)
	conn := newBufconnClient(t, app.PublicServer)
	client := turingv1.NewHealthServiceClient(conn)
	if _, err := client.Check(context.Background(), &turingv1.HealthCheckRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected unauthenticated error, got %v", err)
	}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer client"))
	res, err := client.Check(ctx, &turingv1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Ok {
		t.Fatal("health check was not ok")
	}
}

func TestPublicServerReportsVersion(t *testing.T) {
	app := newTestApp(t)
	var wantSchemaVersion string
	if err := app.database.QueryRow(`
		SELECT CASE WHEN instr(version, '_') > 0 THEN substr(version, 1, instr(version, '_') - 1) ELSE version END
		FROM schema_migrations
		ORDER BY version DESC
		LIMIT 1`).Scan(&wantSchemaVersion); err != nil {
		t.Fatalf("read latest applied schema version: %v", err)
	}
	conn := newBufconnClient(t, app.PublicServer)
	client := turingv1.NewHealthServiceClient(conn)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer client"))
	res, err := client.Version(ctx, &turingv1.VersionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "1.0.0-go" {
		t.Errorf("version = %q, want %q", res.Version, "1.0.0-go")
	}
	if res.SchemaVersion != wantSchemaVersion {
		t.Errorf("schema version = %q, want %q", res.SchemaVersion, wantSchemaVersion)
	}
}

func TestInternalServerRequiresInternalToken(t *testing.T) {
	app := newTestApp(t)
	conn := newBufconnClient(t, app.InternalServer)
	client := turingv1.NewRuntimeServiceClient(conn)

	stream, err := client.ConnectWorker(context.Background())
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected unauthenticated error, got %v", err)
	}

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer internal"))
	stream, err = client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{}); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected runtime service to reject invalid worker_ready, got %v", err)
	}
}

func TestAppRegistersPublicAndInternalServices(t *testing.T) {
	app := newTestApp(t)
	publicServices := app.PublicServer.GetServiceInfo()
	for _, name := range []string{
		"turing.v1.HealthService",
		"turing.v1.SessionService",
		"turing.v1.EventService",
		"turing.v1.ChatService",
		"turing.v1.ApprovalService",
	} {
		if _, ok := publicServices[name]; !ok {
			t.Fatalf("public server missing %s", name)
		}
	}
	internalServices := app.InternalServer.GetServiceInfo()
	if _, ok := internalServices["turing.v1.RuntimeService"]; !ok {
		t.Fatal("internal server missing turing.v1.RuntimeService")
	}
	if _, ok := internalServices["turing.v1.HealthService"]; ok {
		t.Fatal("internal server should not register public health service")
	}
}

func TestStopReturnsWhenPublicStreamIsActive(t *testing.T) {
	app := newTestApp(t)
	conn := newBufconnClient(t, app.PublicServer)
	session, err := app.Repository.CreateSession(context.Background(), "Stop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Repository.AppendEvent(context.Background(), repository.AppendEventInput{
		SessionID:   session.SessionID,
		Type:        "system",
		PayloadJSON: "{}",
	}); err != nil {
		t.Fatal(err)
	}

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer client"))
	stream, err := turingv1.NewEventServiceClient(conn).SubscribeSessionEvents(ctx, &turingv1.SubscribeSessionEventsRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		app.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		app.PublicServer.Stop()
		app.InternalServer.Stop()
		_ = conn.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("Stop did not return while a public stream was active")
	}
}
