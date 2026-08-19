package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strings"
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

func TestAppEnforcesConfiguredGlobalGeneralRunCapacity(t *testing.T) {
	application, err := New(config.Config{
		ClientAPIKey:             "client",
		InternalToken:            "internal",
		ApprovalJWTSecret:        "approval-secret",
		DatabasePath:             t.TempDir() + "/turing.db",
		MaxConcurrentRunsGeneral: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(application.Stop)
	for _, title := range []string{"first", "second"} {
		session, err := application.Repository.CreateSession(context.Background(), title)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := application.Repository.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: title, AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		}); err != nil {
			t.Fatal(err)
		}
	}
	conn := newBufconnClient(t, application.InternalServer)
	client := turingv1.NewRuntimeServiceClient(conn)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer internal"))
	first, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.CloseSend() }()
	if err := first.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-global-one", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		_, recvErr := first.Recv()
		t.Fatalf("first worker send = %v; stream result = %v", err, recvErr)
	}
	recvRuntimeCommand(t, first, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	})

	second, err := client.ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.CloseSend() }()
	if err := second.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-global-two", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvRuntimeCommand(t, second, func(command *turingv1.RuntimeCommand) bool {
		return command.GetWorkerAccepted() != nil
	})
	received := make(chan *turingv1.RuntimeCommand, 1)
	receiveErr := make(chan error, 1)
	go func() {
		command, err := second.Recv()
		if err != nil {
			receiveErr <- err
			return
		}
		received <- command
	}()
	select {
	case command := <-received:
		if command.GetRunAssigned() != nil {
			t.Fatalf("second worker received globally over-capacity assignment: %+v", command.GetRunAssigned())
		}
	case err := <-receiveErr:
		t.Fatalf("second worker receive error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAppRestartRecoversStaleRunningAssignment(t *testing.T) {
	dbPath := t.TempDir() + "/turing.db"
	cfg := config.Config{
		ClientAPIKey: "client", InternalToken: "internal", ApprovalJWTSecret: "approval-secret", DatabasePath: dbPath,
	}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session, err := first.Repository.CreateSession(context.Background(), "Restart recovery")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := first.Repository.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "recover me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Repository.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	first.Stop()

	restarted, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Stop)
	run, err := restarted.Repository.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" {
		t.Fatalf("restart left stale run %q, want queued for recovery", run.Status)
	}
	claimed, err := restarted.Repository.ClaimNextJob(context.Background(), "general_assistant", "worker-after-restart")
	if err != nil {
		t.Fatal(err)
	}
	if claimed.RunID != enqueued.RunID {
		t.Fatalf("recovered claim = %+v, want %q", claimed, enqueued.RunID)
	}
}

func recvRuntimeCommand(t *testing.T, stream turingv1.RuntimeService_ConnectWorkerClient, match func(*turingv1.RuntimeCommand) bool) *turingv1.RuntimeCommand {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		result := make(chan struct {
			command *turingv1.RuntimeCommand
			err     error
		}, 1)
		go func() {
			command, err := stream.Recv()
			result <- struct {
				command *turingv1.RuntimeCommand
				err     error
			}{command: command, err: err}
		}()
		select {
		case received := <-result:
			if received.err != nil {
				t.Fatal(received.err)
			}
			if match(received.command) {
				return received.command
			}
		case <-deadline:
			t.Fatal("timed out waiting for runtime command")
		}
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

func TestApprovalRPCsAreSeparatedAcrossPublicAndInternalServers(t *testing.T) {
	app := newTestApp(t)
	publicClient := turingv1.NewApprovalServiceClient(newBufconnClient(t, app.PublicServer))
	publicContext := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+"client"))
	if _, err := publicClient.GetApprovalForRuntime(publicContext, &turingv1.GetApprovalForRuntimeRequest{ApprovalId: "missing"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public GetApprovalForRuntime error = %v, want PermissionDenied", err)
	}
	if _, err := publicClient.ConsumeApproval(publicContext, &turingv1.ConsumeApprovalRequest{ApprovalId: "missing"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public ConsumeApproval error = %v, want PermissionDenied", err)
	}

	internalClient := turingv1.NewApprovalServiceClient(newBufconnClient(t, app.InternalServer))
	internalContext := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+"internal"))
	if _, err := internalClient.ApproveApproval(internalContext, &turingv1.ApproveApprovalRequest{ApprovalId: "missing"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("internal ApproveApproval error = %v, want PermissionDenied", err)
	}
	if _, err := internalClient.DenyApproval(internalContext, &turingv1.DenyApprovalRequest{ApprovalId: "missing"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("internal DenyApproval error = %v, want PermissionDenied", err)
	}
}

func TestAuthenticationFailuresAreAuditedWithoutCredentials(t *testing.T) {
	app := newTestApp(t)
	const requestID = "request-public-invalid"
	const secret = "invalid-public-secret"
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer "+secret,
		"x-request-id", requestID,
		"user-agent", "auth-audit-test",
	))
	_, err := turingv1.NewHealthServiceClient(newBufconnClient(t, app.PublicServer)).Check(ctx, &turingv1.HealthCheckRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Check error = %v, want Unauthenticated", err)
	}

	var actorType, correlationID, target, payloadJSON string
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = app.database.QueryRowContext(context.Background(), `
			SELECT actor_type, correlation_id, target, payload_json
			FROM audit_logs
			WHERE action = 'auth.failed' AND correlation_id = ?
		`, requestID).Scan(&actorType, &correlationID, &target, &payloadJSON)
		if err == nil {
			break
		}
		if !errors.Is(err, sql.ErrNoRows) || !time.Now().Before(deadline) {
			t.Fatalf("query auth audit: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if actorType != "client" || correlationID != requestID || target != turingv1.HealthService_Check_FullMethodName {
		t.Fatalf("auth audit identity = actor:%q correlation:%q target:%q", actorType, correlationID, target)
	}
	if len(payloadJSON) > 1024 || strings.Contains(payloadJSON, secret) || strings.Contains(strings.ToLower(payloadJSON), "authorization") {
		t.Fatalf("auth audit payload leaked or exceeded bounds: %q", payloadJSON)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["requestId"] != requestID || payload["userAgent"] == "" {
		t.Fatalf("auth audit payload = %#v", payload)
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
		"turing.v1.IntegrationService",
		"turing.v1.SkillService",
		"turing.v1.AutomationService",
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
	// Third-party connections are the user's business, not the runtime's.
	// Registering them internally would put them behind the runtime token,
	// which every tool server already holds.
	if _, ok := internalServices["turing.v1.IntegrationService"]; ok {
		t.Fatal("internal server should not expose the integration service to the runtime")
	}
}

// The whole feature is optional: an install that has never run the updated
// init.sh has no TURING_INTEGRATION_KEY and must still start, with the
// service registered so it can say what is missing rather than answering
// Unimplemented.
func TestAppStartsWithAndWithoutAnIntegrationKey(t *testing.T) {
	// newTestApp configures no key at all.
	if _, ok := newTestApp(t).PublicServer.GetServiceInfo()["turing.v1.IntegrationService"]; !ok {
		t.Fatal("the integration service is missing when no key is configured")
	}

	withKey, err := New(config.Config{
		ClientAPIKey:      "client",
		InternalToken:     "internal",
		ApprovalJWTSecret: "approval-secret",
		IntegrationKey:    strings.Repeat("ab", 32),
		DatabasePath:      t.TempDir() + "/turing.db",
	})
	if err != nil {
		t.Fatalf("start with an integration key: %v", err)
	}
	t.Cleanup(withKey.Stop)
	if _, ok := withKey.PublicServer.GetServiceInfo()["turing.v1.IntegrationService"]; !ok {
		t.Fatal("the integration service is missing when a key is configured")
	}

	// A key of the wrong shape is a misconfiguration, not something to
	// discover while somebody is pasting a token.
	if _, err := New(config.Config{
		ClientAPIKey:      "client",
		InternalToken:     "internal",
		ApprovalJWTSecret: "approval-secret",
		IntegrationKey:    "not-a-key",
		DatabasePath:      t.TempDir() + "/turing.db",
	}); err == nil {
		t.Fatal("started with a malformed integration key")
	// Nothing outside the orchestrator schedules a run, and the runtime has no
	// reason to read the automation library — including the tool allowlists
	// that decide what it may do unattended.
	if _, ok := internalServices["turing.v1.AutomationService"]; ok {
		t.Fatal("internal server should not expose the automation library to the runtime")
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
