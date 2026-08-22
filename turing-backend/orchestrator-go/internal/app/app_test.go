package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/auth"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	cfg := config.Config{
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
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
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
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
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret:        "approval-secret",
		DatabasePath:             t.TempDir() + "/turing.db",
		OllamaModel:              "llama3.2",
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
		ClientAPIKey: "client", RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer", ApprovalJWTSecret: "approval-secret", DatabasePath: dbPath,
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

func TestMCPRegistryRPCsAreSeparatedAcrossPublicAndInternalServers(t *testing.T) {
	app := newTestApp(t)
	publicClient := turingv1.NewMcpRegistryServiceClient(newBufconnClient(t, app.PublicServer))
	publicContext := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+"client"))
	if _, err := publicClient.CallRegisteredMcpTool(publicContext, &turingv1.CallRegisteredMcpToolRequest{
		ServerId: "missing", RunId: "run", ToolName: "tool",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public CallRegisteredMcpTool error = %v, want PermissionDenied", err)
	}
	if _, err := publicClient.ListMcpServers(publicContext, &turingv1.ListMcpServersRequest{}); err != nil {
		t.Fatalf("public ListMcpServers: %v", err)
	}

	internalClient := turingv1.NewMcpRegistryServiceClient(newBufconnClient(t, app.InternalServer))
	internalContext := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+"internal"))
	if _, err := internalClient.SetMcpServerEnabled(internalContext, &turingv1.SetMcpServerEnabledRequest{
		ServerId: "missing", Enabled: true,
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("internal SetMcpServerEnabled error = %v, want PermissionDenied", err)
	}
	if _, err := internalClient.ListMcpServers(internalContext, &turingv1.ListMcpServersRequest{}); err != nil {
		t.Fatalf("internal ListMcpServers: %v", err)
	}
}

// This is the real wiring in app.New — the exact allowlists a compromised
// mcp-files would face — not the standalone fixtures in the auth package's
// own unit tests. It must independently prove the escalation TUR-006 removes:
// the approval-consumer identity reaches ConsumeApproval (past authorization,
// into the handler) but is denied PermissionDenied for every runtime-only
// method, on both the unary and streaming internal interceptors.
func TestApprovalConsumerIdentityCannotReachRuntimeOnlyMethods(t *testing.T) {
	app := newTestApp(t)
	conn := newBufconnClient(t, app.InternalServer)
	approvalConsumerCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+"internal-approval-consumer"))

	sessionClient := turingv1.NewSessionServiceClient(conn)
	if _, err := sessionClient.ListMessages(approvalConsumerCtx, &turingv1.ListMessagesRequest{SessionId: "missing"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("approval-consumer ListMessages error = %v, want PermissionDenied", err)
	}
	if _, err := sessionClient.SearchMessages(approvalConsumerCtx, &turingv1.SearchMessagesRequest{Query: "x"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("approval-consumer SearchMessages error = %v, want PermissionDenied", err)
	}

	approvalClient := turingv1.NewApprovalServiceClient(conn)
	if _, err := approvalClient.GetApprovalForRuntime(approvalConsumerCtx, &turingv1.GetApprovalForRuntimeRequest{ApprovalId: "missing"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("approval-consumer GetApprovalForRuntime error = %v, want PermissionDenied", err)
	}

	runtimeClient := turingv1.NewRuntimeServiceClient(conn)
	// Do not Send before checking: the stream interceptor denies the call
	// before any handler runs, so the client can observe that only through
	// Recv — a Send raced against the server already closing the stream can
	// return io.EOF instead of ever reaching this assertion.
	stream, err := runtimeClient.ConnectWorker(approvalConsumerCtx)
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("approval-consumer ConnectWorker error = %v, want PermissionDenied", err)
	}

	// ConsumeApproval must still be reachable: a NotFound for a nonexistent
	// approval id proves authorization passed and the handler itself ran,
	// distinguishing "denied before the handler" from "denied inside it".
	if _, err := approvalClient.ConsumeApproval(approvalConsumerCtx, &turingv1.ConsumeApprovalRequest{ApprovalId: "missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("approval-consumer ConsumeApproval error = %v, want NotFound (reached the handler)", err)
	}
}

// Concurrent ConsumeApproval calls over the real internal gRPC server, behind
// the actual identity interceptor, must still consume an approval exactly
// once. The interceptor is a pre-handler authorization check and must not
// introduce — or hide — a double-consumption race in the transaction beneath
// it.
func TestConcurrentConsumeApprovalOverInternalServerConsumesExactlyOnce(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	session, err := app.Repository.CreateSession(ctx, "concurrent consume")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := app.Repository.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Repository.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := app.Repository.RecordToolCallBefore(ctx, repository.ToolCallRecord{ToolCallID: "call_concurrent", RunID: enqueued.RunID}, "general_assistant", "custom", "custom.write", `{}`, "sha256:concurrent"); err != nil {
		t.Fatal(err)
	}
	approvalID, err := app.ApprovalService.CreateApprovalForTool(ctx, enqueued.RunID, "call_concurrent", "general_assistant", "custom.write", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApprovalService.ApproveApproval(ctx, &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}

	approvalClient := turingv1.NewApprovalServiceClient(newBufconnClient(t, app.InternalServer))
	approvalConsumerCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+"internal-approval-consumer"))

	const attempts = 10
	responses := make(chan *turingv1.ApprovalResponse, attempts)
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := approvalClient.ConsumeApproval(approvalConsumerCtx, &turingv1.ConsumeApprovalRequest{ApprovalId: approvalID})
			if err != nil {
				errs <- err
				return
			}
			responses <- resp
		}()
	}
	wg.Wait()
	close(responses)
	close(errs)

	consumed := 0
	for resp := range responses {
		if resp.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED {
			t.Fatalf("unexpected non-error status: %v", resp.GetStatus())
		}
		consumed++
	}
	failedPrecondition := 0
	for err := range errs {
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("unexpected consume error = %v, want FailedPrecondition", err)
		}
		failedPrecondition++
	}
	if consumed != 1 {
		t.Fatalf("consumed count = %d, want exactly 1", consumed)
	}
	if failedPrecondition != attempts-1 {
		t.Fatalf("FailedPrecondition count = %d, want %d", failedPrecondition, attempts-1)
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

// A wrong-service call and an unrecognized bearer are exactly the two events
// TUR-006 most needs preserved in the audit trail: proof that a specific
// identity over-reached, and proof that something presented no valid
// internal credential at all. Both must actually persist, not merely avoid
// crashing the request — a silently dropped audit write would defeat the
// point of recording it.
func TestInternalAuthorizationFailuresAreAudited(t *testing.T) {
	app := newTestApp(t)
	conn := newBufconnClient(t, app.InternalServer)

	queryActorType := func(t *testing.T, requestID string) string {
		t.Helper()
		var actorType string
		deadline := time.Now().Add(2 * time.Second)
		for {
			err := app.database.QueryRowContext(context.Background(), `
				SELECT actor_type FROM audit_logs WHERE action = 'auth.failed' AND correlation_id = ?
			`, requestID).Scan(&actorType)
			if err == nil {
				return actorType
			}
			if !errors.Is(err, sql.ErrNoRows) || !time.Now().Before(deadline) {
				t.Fatalf("query auth audit for request %q: %v", requestID, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	t.Run("wrong-service call from a real identity", func(t *testing.T) {
		const requestID = "request-approval-consumer-overreach"
		approvalConsumerCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
			"authorization", "Bearer "+"internal-approval-consumer",
			"x-request-id", requestID,
		))
		if _, err := turingv1.NewSessionServiceClient(conn).ListMessages(approvalConsumerCtx, &turingv1.ListMessagesRequest{SessionId: "missing"}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("ListMessages error = %v, want PermissionDenied", err)
		}
		actorType := queryActorType(t, requestID)
		if actorType != "approval-consumer" {
			t.Fatalf("actor_type = %q, want %q", actorType, "approval-consumer")
		}
	})

	t.Run("unrecognized bearer", func(t *testing.T) {
		const requestID = "request-internal-unrecognized-bearer"
		unknownCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
			"authorization", "Bearer "+"not-a-registered-token",
			"x-request-id", requestID,
		))
		if _, err := turingv1.NewSessionServiceClient(conn).ListMessages(unknownCtx, &turingv1.ListMessagesRequest{SessionId: "missing"}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("ListMessages error = %v, want Unauthenticated", err)
		}
		actorType := queryActorType(t, requestID)
		if actorType != auth.UnknownIdentityActorType {
			t.Fatalf("actor_type = %q, want %q", actorType, auth.UnknownIdentityActorType)
		}
	})
}

// Nothing statically couples the internal identity names app.New wires up to
// the audit_logs.actor_type CHECK constraint that accepts them — the gap
// TestInternalAuthorizationFailuresAreAudited was written to catch was found
// precisely because the two are independent. This test reads the real
// configured identity names (app.InternalIdentityNames, not a hardcoded
// copy) so adding a future identity — for example a connector credential —
// without widening the CHECK fails here immediately, rather than as a
// silently dropped audit write discovered later.
func TestInternalIdentityActorTypesAreAcceptedByAuditSchema(t *testing.T) {
	app := newTestApp(t)
	actorTypes := append([]string{auth.UnknownIdentityActorType}, app.InternalIdentityNames...)
	for _, actorType := range actorTypes {
		t.Run(actorType, func(t *testing.T) {
			if err := app.AuditService.Record(context.Background(), "", actorType, "", "schema.probe", "", nil); err != nil {
				t.Fatalf("actor_type %q rejected by audit_logs schema: %v", actorType, err)
			}
		})
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
		"turing.v1.TelemetryService",
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
	// IntegrationService is split: management is refused by its internal
	// facet while discovery and dispatch are refused by its public facet.
	// Nothing outside the orchestrator schedules a run, and the runtime has no
	// reason to read the automation library — including the tool allowlists
	// that decide what it may do unattended.
	if _, ok := internalServices["turing.v1.AutomationService"]; ok {
		t.Fatal("internal server should not expose the automation library to the runtime")
	}
	if _, ok := internalServices["turing.v1.IntegrationService"]; !ok {
		t.Fatal("internal server missing integration dispatch facet")
	}
	// A usage report is for the person, not for the machinery. Registering it
	// internally would put it behind per-identity authorization instead of
	// removing it entirely, and no internal identity's allowlist should ever
	// need to grow to include it.
	if _, ok := internalServices["turing.v1.TelemetryService"]; ok {
		t.Fatal("internal server should not expose telemetry to the runtime")
	}
}

func TestIntegrationServiceFacetAndRuntimeIdentityWiring(t *testing.T) {
	app := newTestApp(t)
	publicClient := turingv1.NewIntegrationServiceClient(newBufconnClient(t, app.PublicServer))
	internalClient := turingv1.NewIntegrationServiceClient(newBufconnClient(t, app.InternalServer))
	publicCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer client")
	internalCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer internal")
	if _, err := publicClient.CallIntegrationTool(publicCtx, &turingv1.CallIntegrationToolRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public dispatch error = %v, want PermissionDenied", err)
	}
	if _, err := publicClient.ListIntegrationTools(publicCtx, &turingv1.ListIntegrationToolsRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public discovery error = %v, want PermissionDenied", err)
	}
	managementCalls := map[string]func() error{
		"ListProviders": func() error {
			_, err := internalClient.ListProviders(internalCtx, &turingv1.ListProvidersRequest{})
			return err
		},
		"ConnectAccount": func() error {
			_, err := internalClient.ConnectAccount(internalCtx, &turingv1.ConnectAccountRequest{})
			return err
		},
		"ListConnections": func() error {
			_, err := internalClient.ListConnections(internalCtx, &turingv1.ListConnectionsRequest{})
			return err
		},
		"GetConnection": func() error {
			_, err := internalClient.GetConnection(internalCtx, &turingv1.GetConnectionRequest{})
			return err
		},
		"RevokeConnection": func() error {
			_, err := internalClient.RevokeConnection(internalCtx, &turingv1.RevokeConnectionRequest{})
			return err
		},
		"DeleteConnection": func() error {
			_, err := internalClient.DeleteConnection(internalCtx, &turingv1.DeleteConnectionRequest{})
			return err
		},
	}
	for name, call := range managementCalls {
		if err := call(); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("internal %s error = %v, want PermissionDenied", name, err)
		}
	}
	response, err := internalClient.ListIntegrationTools(internalCtx, &turingv1.ListIntegrationToolsRequest{})
	if err != nil || response == nil || len(response.GetTools()) != 0 {
		t.Fatalf("runtime discovery response = %+v err=%v, want keyless empty list", response, err)
	}
}

// AuditService is the one place a client can read what the orchestrator has
// recorded about it, so it must sit behind the same public bearer token as
// every other client-facing RPC and must never be reachable from the runtime
// side — neither the runtime nor the approval-consumer identity's allowlist
// includes it, and it is not even registered on the internal server.
func TestAuditServiceIsAuthenticatedAndPublicOnly(t *testing.T) {
	app := newTestApp(t)

	if _, ok := app.PublicServer.GetServiceInfo()["turing.v1.AuditService"]; !ok {
		t.Fatal("public server missing turing.v1.AuditService")
	}
	if _, ok := app.InternalServer.GetServiceInfo()["turing.v1.AuditService"]; ok {
		t.Fatal("internal server should not expose turing.v1.AuditService to the runtime")
	}

	publicClient := turingv1.NewAuditServiceClient(newBufconnClient(t, app.PublicServer))

	if _, err := publicClient.ListAuditEntries(context.Background(), &turingv1.ListAuditEntriesRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated ListAuditEntries error = %v, want Unauthenticated", err)
	}

	clientCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer client"))
	if _, err := publicClient.ListAuditEntries(clientCtx, &turingv1.ListAuditEntriesRequest{}); err != nil {
		t.Fatalf("authenticated client ListAuditEntries error = %v, want nil", err)
	}

	// The internal token is the runtime's, not the client's. It must not open
	// the public server's audit door either.
	internalOnPublicCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer internal"))
	if _, err := publicClient.ListAuditEntries(internalOnPublicCtx, &turingv1.ListAuditEntriesRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("internal token on public server error = %v, want Unauthenticated", err)
	}

	// The service is entirely absent from the internal server, so any call
	// there — whichever token accompanies it — must fail as Unimplemented
	// rather than ever reach a handler.
	internalClient := turingv1.NewAuditServiceClient(newBufconnClient(t, app.InternalServer))
	if _, err := internalClient.ListAuditEntries(internalOnPublicCtx, &turingv1.ListAuditEntriesRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("internal token on internal server error = %v, want Unimplemented (service must not be exposed)", err)
	}
	clientOnInternalCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer client"))
	if _, err := internalClient.ListAuditEntries(clientOnInternalCtx, &turingv1.ListAuditEntriesRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("client token on internal server error = %v, want Unimplemented (service must not be exposed)", err)
	}
}

// TestDeletedSessionAuditIsListableOnlyAsScrubbedEvidence proves the deletion
// scrub is visible end to end through the authenticated public API: a tool
// audit row that legitimately carried a sensitive tool name is fully readable
// before the session is deleted, and turns into bare SCRUBBED evidence — no
// payload fields, no sentinel, structural identity intact — the moment the
// session is gone. It also checks the companion session.deleted row, whose
// allowlisted counts are the only thing it may ever reveal.
func TestDeletedSessionAuditIsListableOnlyAsScrubbedEvidence(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()

	const sentinelToolName = "sentinel-tool-871f2c9d-do-not-leak"
	const sentinelMessageContent = "sentinel-message-6a2e9f-do-not-leak"
	const sentinelEndpointHost = "sentinel-endpoint-3d7b1a-do-not-leak.example.com"
	const sentinelAgentName = "sentinel-agent-3d7b1a-do-not-leak"
	const sentinelModel = "sentinel-model-3d7b1a-do-not-leak"

	session, err := app.Repository.CreateSession(ctx, "Delete me for audit")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := app.Repository.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: sentinelMessageContent, AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Repository.ClaimNextJob(ctx, "general_assistant", "worker-delete-audit"); err != nil {
		t.Fatal(err)
	}
	// toolName is part of tool.call.before's reviewed allowlist (service.go's
	// applyAuditActionPolicy), so it is genuinely visible through the public
	// API before deletion — the assertion after deletion is not just proving
	// the field allowlist works, it is proving the scrub does.
	toolTarget := "call_" + enqueued.RunID
	if err := app.Repository.RecordAudit(ctx, enqueued.RunID, "runtime", "", "tool.call.before", toolTarget,
		`{"toolName":"`+sentinelToolName+`"}`); err != nil {
		t.Fatal(err)
	}

	// Routing rows are the audit shape a run-id scrub cannot reach: they carry
	// a NULL correlation_id and the session id as target. Route and unroute so
	// the session leaves behind a real session.routed (carrying the third-party
	// endpoint, model, and agent name) and a real session.unrouted.
	routedAgent, err := app.Repository.CreateExternalAgent(ctx, repository.ExternalAgentInput{
		DisplayName:   sentinelAgentName,
		Provider:      "anthropic",
		BaseURL:       "https://" + sentinelEndpointHost + "/v1",
		Model:         sentinelModel,
		CredentialRef: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Repository.SetSessionAgent(ctx, session.SessionID, routedAgent.AgentID); err != nil {
		t.Fatal(err)
	}
	if err := app.Repository.ClearSessionAgent(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}

	conn := newBufconnClient(t, app.PublicServer)
	client := turingv1.NewAuditServiceClient(conn)
	authCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer client"))
	correlationID := enqueued.RunID

	beforeDelete, err := client.ListAuditEntries(authCtx, &turingv1.ListAuditEntriesRequest{CorrelationId: &correlationID})
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeDelete.Entries) != 1 {
		t.Fatalf("before delete: got %d entries, want 1", len(beforeDelete.Entries))
	}
	if beforeDelete.Entries[0].GetPayload().GetState() != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_PRESENT {
		t.Fatalf("before delete: payload state = %v, want PRESENT", beforeDelete.Entries[0].GetPayload().GetState())
	}
	if beforeDelete.Entries[0].GetPayload().GetToolName() != sentinelToolName {
		t.Fatalf("before delete: tool name = %q, want the recorded sentinel (precondition failed)", beforeDelete.Entries[0].GetPayload().GetToolName())
	}

	// The run must be terminal — not queued/running/waiting_approval and not
	// execution_active — or DeleteSession refuses it (see
	// repository.ErrSessionHasActiveRun).
	if _, err := app.database.ExecContext(ctx, `UPDATE agent_runs SET status = 'completed', execution_active = 0 WHERE id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := app.Repository.DeleteSession(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}

	afterDelete, err := client.ListAuditEntries(authCtx, &turingv1.ListAuditEntriesRequest{CorrelationId: &correlationID})
	if err != nil {
		t.Fatal(err)
	}
	if len(afterDelete.Entries) != 1 {
		t.Fatalf("after delete: got %d entries, want 1 (deletion scrubs content, it does not remove the row)", len(afterDelete.Entries))
	}
	entry := afterDelete.Entries[0]
	if entry.GetPayload().GetState() != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_SCRUBBED {
		t.Fatalf("after delete: payload state = %v, want SCRUBBED", entry.GetPayload().GetState())
	}
	// Structural identity survives the scrub — only payload content is gone.
	if entry.GetAction() != "tool.call.before" {
		t.Fatalf("after delete: action = %q, want %q", entry.GetAction(), "tool.call.before")
	}
	if entry.GetCorrelationId() != enqueued.RunID {
		t.Fatalf("after delete: correlation_id = %q, want %q", entry.GetCorrelationId(), enqueued.RunID)
	}
	if entry.GetTarget() != toolTarget {
		t.Fatalf("after delete: target = %q, want %q", entry.GetTarget(), toolTarget)
	}
	afterJSON, err := protojson.Marshal(afterDelete)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(afterJSON), sentinelToolName) {
		t.Fatalf("after delete: sentinel leaked through protojson: %s", afterJSON)
	}

	// The deletion's own audit row is never scrubbed — it is the evidence of
	// the deletion — but it is metadata-only: exact allowlisted counts and the
	// target session id, never the withdrawn message content.
	deletedAction := "session.deleted"
	deletionEntries, err := client.ListAuditEntries(authCtx, &turingv1.ListAuditEntriesRequest{Action: &deletedAction})
	if err != nil {
		t.Fatal(err)
	}
	var deletionEntry *turingv1.AuditEntry
	for _, candidate := range deletionEntries.Entries {
		if candidate.GetTarget() == session.SessionID {
			deletionEntry = candidate
			break
		}
	}
	if deletionEntry == nil {
		t.Fatalf("no session.deleted row targeting %q found in %+v", session.SessionID, deletionEntries.Entries)
	}
	if deletionEntry.GetPayload().GetState() != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_PRESENT {
		t.Fatalf("session.deleted payload state = %v, want PRESENT", deletionEntry.GetPayload().GetState())
	}
	if got := deletionEntry.GetPayload().GetDeletedRuns(); got != 1 {
		t.Fatalf("session.deleted deleted_runs = %d, want 1", got)
	}
	// EnqueueUserMessage inserts both the user message and its assistant
	// placeholder, so one enqueued turn leaves two rows.
	if got := deletionEntry.GetPayload().GetDeletedMessages(); got != 2 {
		t.Fatalf("session.deleted deleted_messages = %d, want 2", got)
	}
	deletionJSON, err := protojson.Marshal(deletionEntry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(deletionJSON), sentinelMessageContent) {
		t.Fatalf("session.deleted row leaked withdrawn message content: %s", deletionJSON)
	}

	// The routing rows are still listable — evidence that this conversation was
	// pointed at a third party must survive the deletion — but only as
	// SCRUBBED: the endpoint, model, and agent display name go with the
	// session, even though no run id ever pointed at these rows.
	for _, action := range []string{"session.routed", "session.unrouted"} {
		routingEntries, err := client.ListAuditEntries(authCtx, &turingv1.ListAuditEntriesRequest{Action: &action})
		if err != nil {
			t.Fatal(err)
		}
		var routingEntry *turingv1.AuditEntry
		for _, candidate := range routingEntries.Entries {
			if candidate.GetTarget() == session.SessionID {
				routingEntry = candidate
				break
			}
		}
		if routingEntry == nil {
			t.Fatalf("no %s row targeting %q survived the delete: %+v", action, session.SessionID, routingEntries.Entries)
		}
		if routingEntry.GetPayload().GetState() != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_SCRUBBED {
			t.Fatalf("%s payload state = %v, want SCRUBBED after the session was deleted", action, routingEntry.GetPayload().GetState())
		}
		routingJSON, err := protojson.Marshal(routingEntries)
		if err != nil {
			t.Fatal(err)
		}
		for _, sentinel := range []string{sentinelEndpointHost, sentinelAgentName, sentinelModel} {
			if strings.Contains(string(routingJSON), sentinel) {
				t.Fatalf("%s leaked routing sentinel %q after deletion: %s", action, sentinel, routingJSON)
			}
		}
	}
}

// TestAuditCursorIsBoundToApprovalSecretNotClientKey proves the cursor MAC key
// wired in app.New is the server-side approval signing secret
// (TURING_APPROVAL_JWT_SECRET), not the public client bearer token. A cursor
// minted by one instance is:
//
//   - accepted by a restart over the same database with the same client key and
//     the same approval secret (pagination is durable across a restart);
//   - rejected once the approval secret changes, even when the client key is
//     unchanged and the caller is still authenticated (changing the server-only
//     signing secret invalidates outstanding cursors by design);
//   - still accepted when only the client bearer key is rotated but the approval
//     secret is unchanged (a public bearer holder cannot forge a cursor MAC from
//     the client key, and rotating that key alone must not invalidate a cursor).
//
// The four instances share one database file, so each is stopped before the
// next starts: the DSN opens with SetMaxOpenConns(1) and WAL, and a clean
// handoff keeps the test from depending on two writers coexisting.
func TestAuditCursorIsBoundToApprovalSecretNotClientKey(t *testing.T) {
	dbPath := t.TempDir() + "/turing.db"
	const clientKey = "cursor-restart-client-key"
	const approvalSecret = "cursor-restart-approval-secret"
	const marker = "cursor.restart.marker"
	baseCfg := config.Config{ClientAPIKey: clientKey, RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer", ApprovalJWTSecret: approvalSecret, DatabasePath: dbPath}

	appA, err := New(baseCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(appA.Stop)
	ctx := context.Background()
	if err := appA.Repository.RecordAudit(ctx, "", "client", "", marker, "first", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := appA.Repository.RecordAudit(ctx, "", "client", "", marker, "second", `{}`); err != nil {
		t.Fatal(err)
	}

	listMarker := func(app *App, key string, cursor string) (*turingv1.ListAuditEntriesResponse, error) {
		t.Helper()
		conn := newBufconnClient(t, app.PublicServer)
		client := turingv1.NewAuditServiceClient(conn)
		authCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+key))
		action := marker
		return client.ListAuditEntries(authCtx, &turingv1.ListAuditEntriesRequest{Action: &action, Page: &turingv1.PageRequest{Limit: 1, Cursor: cursor}})
	}

	firstPage, err := listMarker(appA, clientKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Entries) != 1 {
		t.Fatalf("first page: got %d entries, want 1", len(firstPage.Entries))
	}
	if firstPage.GetPage().GetNextCursor() == "" {
		t.Fatal("first page: expected a next cursor for a second row still pending")
	}
	cursor := firstPage.GetPage().GetNextCursor()
	firstTarget := firstPage.Entries[0].GetTarget()

	// App A hands the database off to every later instance, so stop it first.
	appA.Stop()

	// App B: same database, same client key, same approval secret. The derived
	// cursor key matches, so the cursor survives the restart and advances to the
	// older, not-yet-seen row.
	t.Run("same client key and approval secret accepts the cursor", func(t *testing.T) {
		appB, err := New(baseCfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(appB.Stop)
		secondPage, err := listMarker(appB, clientKey, cursor)
		if err != nil {
			t.Fatalf("cursor rejected after restart with the same client key and approval secret: %v", err)
		}
		if len(secondPage.Entries) != 1 {
			t.Fatalf("second page: got %d entries, want 1", len(secondPage.Entries))
		}
		if secondPage.Entries[0].GetTarget() == firstTarget {
			t.Fatalf("second page returned the same row (%q); cursor did not advance", firstTarget)
		}
		if secondPage.Entries[0].GetTarget() != "first" {
			t.Fatalf("second page target = %q, want %q (the older, not-yet-seen row)", secondPage.Entries[0].GetTarget(), "first")
		}
		appB.Stop()
	})

	// App C: same database, same client key (so the caller still authenticates),
	// but a DIFFERENT approval secret. The cursor MAC key is derived from the
	// approval secret, so the cursor must fail to verify even though the caller
	// is authorized. If the key were derived from the client key, this cursor
	// would incorrectly still be accepted.
	t.Run("changed approval secret rejects the cursor even when authenticated", func(t *testing.T) {
		changedSecretCfg := baseCfg
		changedSecretCfg.ApprovalJWTSecret = "a-completely-different-approval-secret"
		appC, err := New(changedSecretCfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(appC.Stop)
		if _, err := listMarker(appC, clientKey, cursor); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("cursor accepted under a different approval secret: err = %v, want InvalidArgument", err)
		}
		appC.Stop()
	})

	// App D: same database, same approval secret, but a DIFFERENT client key.
	// The caller authenticates with the rotated client key, and the cursor —
	// bound to the unchanged approval secret — must remain cryptographically
	// valid and return the expected older row. If the key were derived from the
	// client key, rotating that key alone would incorrectly invalidate the
	// cursor a public bearer holder cannot forge in the first place.
	t.Run("rotated client key alone keeps the cursor valid", func(t *testing.T) {
		rotatedClientCfg := baseCfg
		rotatedClientCfg.ClientAPIKey = "a-completely-different-client-key"
		appD, err := New(rotatedClientCfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(appD.Stop)
		pageD, err := listMarker(appD, rotatedClientCfg.ClientAPIKey, cursor)
		if err != nil {
			t.Fatalf("cursor rejected after only the client key changed (approval secret unchanged): %v", err)
		}
		if len(pageD.Entries) != 1 {
			t.Fatalf("app D page: got %d entries, want 1", len(pageD.Entries))
		}
		if pageD.Entries[0].GetTarget() != "first" {
			t.Fatalf("app D page target = %q, want %q (the older, not-yet-seen row)", pageD.Entries[0].GetTarget(), "first")
		}
		appD.Stop()
	})
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
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
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
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret: "approval-secret",
		IntegrationKey:    "not-a-key",
		DatabasePath:      t.TempDir() + "/turing.db",
	}); err == nil {
		t.Fatal("started with a malformed integration key")
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
