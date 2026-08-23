package approvals

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type approvalHarness struct {
	repo     *repository.Repository
	database *db.DB
	bus      *events.Bus
	service  *Server
	conn     *grpc.ClientConn
}

func newApprovalHarness(t *testing.T) *approvalHarness {
	t.Helper()
	database := openApprovalTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	service := New(repo, bus, "approval-secret")
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	turingv1.RegisterApprovalServiceServer(grpcServer, service)
	go func() {
		_ = grpcServer.Serve(lis)
	}()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	// NewClient starts the channel IDLE; DialContext connected eagerly in the
	// background. Connect() restores that so handshake latency does not land
	// inside a test's deadline.
	conn.Connect()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = conn.Close()
	})
	return &approvalHarness{repo: repo, database: database, bus: bus, service: service, conn: conn}
}

func openApprovalTestDB(t *testing.T) *db.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	sqlDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", name))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	database := &db.DB{DB: sqlDB}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func TestRecoverExpirationConflictReturnsCurrentReadError(t *testing.T) {
	expirationErr := fmt.Errorf("expiration conflict")
	currentErr := fmt.Errorf("current approval unavailable")

	_, err := recoverExpirationConflict(context.Background(), expirationErr, func(context.Context) (repository.ApprovalRecord, error) {
		return repository.ApprovalRecord{}, currentErr
	})

	if !errors.Is(err, currentErr) || errors.Is(err, expirationErr) {
		t.Fatalf("recoverExpirationConflict error = %v, want current read error", err)
	}
}

func (h *approvalHarness) createRunningToolCall(t *testing.T) repository.EnqueueUserMessageResult {
	t.Helper()
	session, err := h.repo.CreateSession(context.Background(), "Approvals")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.RecordToolCallBefore(context.Background(), repository.ToolCallRecord{ToolCallID: "call_1", RunID: enqueued.RunID}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:placeholder"); err != nil {
		t.Fatal(err)
	}
	return enqueued
}

func TestCreateApprovalForToolPersistsEventAndAudit(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)

	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if approvalID == "" {
		t.Fatal("approvalID is empty")
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "waiting_approval" {
		t.Fatalf("run status = %q, want waiting_approval", run.Status)
	}
	events, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var requested repository.Event
	for _, event := range events {
		if event.Type == "approval.requested" {
			requested = event
		}
	}
	if requested.EventID == "" {
		t.Fatal("approval.requested event was not persisted")
	}
	// map[string]any, not map[string]string: approval.requested is the run's
	// waiting-approval lifecycle event, so it now also carries the nested
	// canonical run state.
	var payload map[string]any
	if err := json.Unmarshal([]byte(requested.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	argsSummary, _ := payload["argsSummary"].(string)
	if payload["approvalId"] != approvalID || payload["toolName"] != "files.update" ||
		argsSummary != "Requested change to note.txt" ||
		strings.Contains(argsSummary, "hello") {
		t.Fatalf("approval.requested payload = %+v", payload)
	}
	var auditAction string
	if err := h.database.QueryRowContext(context.Background(), `SELECT action FROM audit_logs WHERE target = ?`, approvalID).Scan(&auditAction); err != nil {
		t.Fatal(err)
	}
	if auditAction != "approval.requested" {
		t.Fatalf("audit action = %q", auditAction)
	}
}

func TestDefaultApprovalTTLAlignsExpiryAndJWT(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	started := time.Now()
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, approval.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if ttl := expiresAt.Sub(started); ttl < 64*time.Second || ttl > 66*time.Second {
		t.Fatalf("approval TTL = %v, want configured default near 65s", ttl)
	}
	if expiresAt.Nanosecond() != 0 {
		t.Fatalf("approval expiry = %s, want whole-second precision shared with JWT", approval.ExpiresAt)
	}
	if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	approved, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(approved.ApprovalToken, ".")
	if len(parts) != 3 {
		t.Fatalf("approval token = %q, want JWT", approved.ApprovalToken)
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["exp"] != float64(expiresAt.Unix()) {
		t.Fatalf("JWT exp = %#v, want approval expiry %d", payload["exp"], expiresAt.Unix())
	}
}

func TestApprovalLifecycleEventsIncludePersistedCorrelationIDs(t *testing.T) {
	createApproval := func(t *testing.T, h *approvalHarness) (repository.EnqueueUserMessageResult, string) {
		t.Helper()
		session, err := h.repo.CreateSession(context.Background(), "Correlation")
		if err != nil {
			t.Fatal(err)
		}
		enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.repo.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
			t.Fatal(err)
		}
		if err := h.repo.RecordToolCallBefore(context.Background(), repository.ToolCallRecord{
			ToolCallID: "call_correlation", RunID: enqueued.RunID, ModelToolCallID: "provider_call_1",
		}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:correlation"); err != nil {
			t.Fatal(err)
		}
		approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_correlation", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
		if err != nil {
			t.Fatal(err)
		}
		return enqueued, approvalID
	}
	assertCorrelation := func(t *testing.T, h *approvalHarness, enqueued repository.EnqueueUserMessageResult, eventType, approvalID string) {
		t.Helper()
		events, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.Type != eventType {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				t.Fatal(err)
			}
			if payload["approvalId"] != approvalID || payload["toolCallId"] != "call_correlation" ||
				payload["runId"] != enqueued.RunID || payload["traceId"] != enqueued.TraceID ||
				payload["modelToolCallId"] != "provider_call_1" {
				t.Fatalf("%s payload = %+v", eventType, payload)
			}
			return
		}
		t.Fatalf("%s event was not persisted", eventType)
	}

	t.Run("requested", func(t *testing.T) {
		h := newApprovalHarness(t)
		enqueued, approvalID := createApproval(t, h)
		assertCorrelation(t, h, enqueued, "approval.requested", approvalID)
	})
	t.Run("approved", func(t *testing.T) {
		h := newApprovalHarness(t)
		enqueued, approvalID := createApproval(t, h)
		if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
			t.Fatal(err)
		}
		assertCorrelation(t, h, enqueued, "approval.approved", approvalID)
	})
	t.Run("denied", func(t *testing.T) {
		h := newApprovalHarness(t)
		enqueued, approvalID := createApproval(t, h)
		if _, err := h.service.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID}); err != nil {
			t.Fatal(err)
		}
		assertCorrelation(t, h, enqueued, "approval.denied", approvalID)
	})
	t.Run("expired", func(t *testing.T) {
		h := newApprovalHarness(t)
		enqueued, approvalID := createApproval(t, h)
		if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID}); err != nil {
			t.Fatal(err)
		}
		assertCorrelation(t, h, enqueued, "approval.expired", approvalID)
	})
	t.Run("consumed", func(t *testing.T) {
		h := newApprovalHarness(t)
		enqueued, approvalID := createApproval(t, h)
		if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.ConsumeApproval(context.Background(), h.consumeRequest(t, enqueued, approvalID, "note.txt")); err != nil {
			t.Fatal(err)
		}
		assertCorrelation(t, h, enqueued, "approval.consumed", approvalID)
	})
}

func TestCreateApprovalForToolReusesExistingToolCallApproval(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)

	first, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("second approval = %q, want %q", second, first)
	}
	var count int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM approvals WHERE tool_call_id = 'call_1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("approval count = %d, want 1", count)
	}
}

func TestCreateApprovalForToolRejectsExistingApprovalForDifferentRun(t *testing.T) {
	h := newApprovalHarness(t)
	first := h.createRunningToolCall(t)
	firstID, err := h.service.CreateApprovalForTool(context.Background(), first.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	secondSession, err := h.repo.CreateSession(context.Background(), "Second")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: secondSession.SessionID, Content: "second", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkRunRunning(context.Background(), second.RunID); err != nil {
		t.Fatal(err)
	}

	secondID, err := h.service.CreateApprovalForTool(context.Background(), second.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err == nil {
		t.Fatalf("CreateApprovalForTool reused cross-run approval %q as %q", firstID, secondID)
	}
}

func TestApproveApprovalReturnsStatusAndToken(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)

	comment := "Approved after checking the path"
	resp, err := client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{
		ApprovalId: approvalID,
		Comment:    comment,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED {
		t.Fatalf("ApproveApproval status = %s", resp.Status)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(approval.ApprovalToken, ".") {
		t.Fatalf("approval token was not signed: %q", approval.ApprovalToken)
	}
	if approval.ApprovalComment != (sql.NullString{String: comment, Valid: true}) || approval.DenialReason.Valid {
		t.Fatalf("approval rationale = comment %#v reason %#v", approval.ApprovalComment, approval.DenialReason)
	}
	var auditAction, auditPayloadJSON string
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT action, payload_json
		FROM audit_logs
		WHERE action = 'approval.approved' AND target = ?
	`, approvalID).Scan(&auditAction, &auditPayloadJSON); err != nil {
		t.Fatal(err)
	}
	var auditPayload map[string]any
	if err := json.Unmarshal([]byte(auditPayloadJSON), &auditPayload); err != nil {
		t.Fatal(err)
	}
	if auditPayload["comment"] != comment || auditPayload["toolName"] != "files.update" {
		t.Fatalf("approval audit payload = %#v", auditPayload)
	}
}

func TestApproveApprovalNotifiesRuntimeWithToken(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	notifier := &recordingApprovalNotifier{}
	h.service.SetNotifier(notifier)

	_, err = h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatal(err)
	}
	if got := notifier.snapshot(); got.runID != enqueued.RunID || got.approvalID != approvalID || got.status != "approved" || !strings.Contains(got.approvalToken, ".") {
		t.Fatalf("approval notification = %+v", got)
	}
}

func TestGetApprovalForRuntimeReturnsApprovedTokenAndConsumeConsumesOnce(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)
	if _, err := client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}

	runtimeState, err := client.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatal(err)
	}
	if runtimeState.Status != turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED || !strings.Contains(runtimeState.ApprovalToken, ".") {
		t.Fatalf("runtime approval state = %+v", runtimeState)
	}
	consumed, err := client.ConsumeApproval(context.Background(), h.consumeRequest(t, enqueued, approvalID, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if consumed.Status != turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED {
		t.Fatalf("consume status = %s", consumed.Status)
	}
	again, err := client.ConsumeApproval(context.Background(), h.consumeRequest(t, enqueued, approvalID, "note.txt"))
	if err != nil {
		t.Fatalf("second ConsumeApproval: %v", err)
	}
	if again.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED ||
		again.GetReservation().GetArtifactId() != consumed.GetReservation().GetArtifactId() {
		t.Fatalf("second ConsumeApproval response = %+v, want original reservation", again)
	}
}

func TestConsumeExpiredApprovalPublishesToolFailureBeforeRunFailure(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)
	if _, err := client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
		t.Fatal(err)
	}
	published, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()

	if response, err := client.ConsumeApproval(context.Background(), h.consumeRequest(t, enqueued, approvalID, "note.txt")); status.Code(err) != codes.FailedPrecondition || response != nil {
		t.Fatalf("ConsumeApproval response/error = %+v/%v, want nil/FailedPrecondition", response, err)
	}
	var publishedTypes []string
	for len(publishedTypes) < 3 {
		select {
		case event := <-published:
			if event.RunID == enqueued.RunID && (event.Type == "approval.expired" || event.Type == "tool.call.failed" || event.Type == "agent.run.failed") {
				publishedTypes = append(publishedTypes, event.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("published consume-expiry lifecycle = %v", publishedTypes)
		}
	}
	if want := []string{"approval.expired", "tool.call.failed", "agent.run.failed"}; !reflect.DeepEqual(publishedTypes, want) {
		t.Fatalf("published consume-expiry lifecycle = %v, want %v", publishedTypes, want)
	}
}

func TestGetApprovalForRuntimeLazilyExpiresOnceAndTerminalizesRunAndJob(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingApprovalNotifier{}
	h.service.SetNotifier(notifier)
	published, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()

	start := make(chan struct{})
	states := make(chan *turingv1.RuntimeApprovalState, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			state, callErr := h.service.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
			states <- state
			errs <- callErr
		}()
	}
	close(start)
	for range 2 {
		if callErr := <-errs; callErr != nil {
			t.Fatalf("GetApprovalForRuntime error: %v", callErr)
		}
		state := <-states
		if state.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED {
			t.Fatalf("runtime approval state = %+v, want expired", state)
		}
	}

	second, err := h.service.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatalf("second GetApprovalForRuntime error: %v", err)
	}
	if second.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED {
		t.Fatalf("second runtime approval state = %+v, want expired", second)
	}

	var runStatus, runCode string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, error_code FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus, &runCode); err != nil {
		t.Fatal(err)
	}
	if runStatus != "failed" || runCode != "approval_expired" {
		t.Fatalf("run status/code = %q/%q, want failed/approval_expired", runStatus, runCode)
	}
	var jobStatus, jobCode string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, error_code FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus, &jobCode); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "failed" || jobCode != "approval_expired" {
		t.Fatalf("job status/code = %q/%q, want failed/approval_expired", jobStatus, jobCode)
	}
	var toolCallStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = 'call_1'`).Scan(&toolCallStatus); err != nil {
		t.Fatal(err)
	}
	if toolCallStatus != "failed" {
		t.Fatalf("expired tool call status = %q, want failed", toolCallStatus)
	}
	var eventCount, terminalEventCount, auditCount int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'approval.expired'`, enqueued.RunID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, enqueued.RunID).Scan(&terminalEventCount); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE action = 'approval.expired' AND target = ?`, approvalID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || terminalEventCount != 1 || auditCount != 1 {
		t.Fatalf("expiration event/terminal-event/audit counts = %d/%d/%d, want 1/1/1", eventCount, terminalEventCount, auditCount)
	}
	if got := notifier.snapshot(); got.count != 1 || got.runID != enqueued.RunID || got.approvalID != approvalID || got.status != "expired" || got.approvalToken != "" {
		t.Fatalf("expiration notification = %+v, want one expired notification", got)
	}
	var publishedTypes []string
	for len(publishedTypes) < 3 {
		select {
		case event := <-published:
			if event.RunID == enqueued.RunID && (event.Type == "approval.expired" || event.Type == "tool.call.failed" || event.Type == "agent.run.failed") {
				publishedTypes = append(publishedTypes, event.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("published expiration lifecycle = %v", publishedTypes)
		}
	}
	if want := []string{"approval.expired", "tool.call.failed", "agent.run.failed"}; !reflect.DeepEqual(publishedTypes, want) {
		t.Fatalf("published expiration lifecycle = %v, want %v", publishedTypes, want)
	}
	select {
	case event := <-published:
		t.Fatalf("duplicate expiration event published: %+v", event)
	default:
	}
}

func TestGetApprovalForRuntimeExpiresApprovedTokenBeforeReturningIt(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)
	if _, err := client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
		t.Fatal(err)
	}

	state, err := h.service.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatal(err)
	}
	if state.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED || state.GetApprovalToken() != "" {
		t.Fatalf("runtime approval state = %+v, want expired with no token", state)
	}
	var statusValue string
	var token sql.NullString
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, approval_token FROM approvals WHERE id = ?`, approvalID).Scan(&statusValue, &token); err != nil {
		t.Fatal(err)
	}
	if statusValue != "expired" || token.Valid {
		t.Fatalf("stored approval status/token = %q/%q, want expired/NULL", statusValue, token.String)
	}
}

func TestGetApprovalForRuntimeKeepsExpirationAtomicAndAncillaryFailuresNonFatal(t *testing.T) {
	notifierErr := status.Error(codes.Unavailable, "notify expiration failed: not pending")
	tests := []struct {
		name      string
		setup     func(t *testing.T, h *approvalHarness) error
		wantError string
		wantState turingv1.ApprovalStatus
	}{
		{
			name: "append event",
			setup: func(t *testing.T, h *approvalHarness) error {
				_, err := h.database.ExecContext(context.Background(), `
					CREATE TRIGGER fail_expiration_event
					BEFORE INSERT ON events
					WHEN NEW.type = 'approval.expired'
					BEGIN
						SELECT RAISE(ABORT, 'append expiration event failed');
					END
				`)
				return err
			},
			wantError: "append expiration event failed",
		},
		{
			name: "audit",
			setup: func(t *testing.T, h *approvalHarness) error {
				_, err := h.database.ExecContext(context.Background(), `
					CREATE TRIGGER fail_expiration_audit
					BEFORE INSERT ON audit_logs
					WHEN NEW.action = 'approval.expired'
					BEGIN
						SELECT RAISE(ABORT, 'record expiration audit failed');
					END
				`)
				return err
			},
			wantState: turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED,
		},
		{
			name: "notifier",
			setup: func(t *testing.T, h *approvalHarness) error {
				h.service.SetNotifier(failingApprovalNotifier{err: notifierErr})
				return nil
			},
			wantState: turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newApprovalHarness(t)
			enqueued := h.createRunningToolCall(t)
			approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
				t.Fatal(err)
			}
			if err := tt.setup(t, h); err != nil {
				t.Fatal(err)
			}

			state, err := h.service.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("GetApprovalForRuntime state/error = %+v/%v, want error containing %q", state, err, tt.wantError)
				}
			} else if err != nil {
				t.Fatalf("GetApprovalForRuntime reported ancillary failure: %v", err)
			} else if state.GetStatus() != tt.wantState {
				t.Fatalf("GetApprovalForRuntime state = %+v, want %s", state, tt.wantState)
			}
			current, err := h.repo.GetApproval(context.Background(), approvalID)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := "expired"
			if tt.wantError != "" {
				wantStatus = "pending"
			}
			if current.Status != wantStatus {
				t.Fatalf("approval status = %q, want %s", current.Status, wantStatus)
			}
		})
	}
}

type failingApprovalNotifier struct {
	err error
}

func (n failingApprovalNotifier) NotifyApprovalUpdated(context.Context, string, string, string, string) error {
	return n.err
}

type recordingApprovalNotifier struct {
	mu            sync.Mutex
	count         int
	runID         string
	approvalID    string
	status        string
	approvalToken string
	contextErr    error
}

func (n *recordingApprovalNotifier) NotifyApprovalUpdated(ctx context.Context, runID string, approvalID string, status string, approvalToken string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.count++
	n.runID = runID
	n.approvalID = approvalID
	n.status = status
	n.approvalToken = approvalToken
	n.contextErr = ctx.Err()
	return nil
}

type approvalNotificationSnapshot struct {
	count         int
	runID         string
	approvalID    string
	status        string
	approvalToken string
	contextErr    error
}

func (n *recordingApprovalNotifier) snapshot() approvalNotificationSnapshot {
	n.mu.Lock()
	defer n.mu.Unlock()
	return approvalNotificationSnapshot{
		count:         n.count,
		runID:         n.runID,
		approvalID:    n.approvalID,
		status:        n.status,
		approvalToken: n.approvalToken,
		contextErr:    n.contextErr,
	}
}

func TestPostCommitApprovalWorkUsesIndependentContext(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	notifier := &recordingApprovalNotifier{}
	h.service.SetNotifier(notifier)

	h.service.finishPostCommit(approval, "client", "approval.approved", "approved", "token")

	got := notifier.snapshot()
	if got.count != 1 || got.contextErr != nil || got.status != "approved" {
		t.Fatalf("post-commit notification = %+v, want independent live context", got)
	}
}

func TestDenyApprovalAtExpiryBoundaryTakesExpirationPath(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `
		UPDATE approvals
		SET expires_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?
	`, approvalID); err != nil {
		t.Fatal(err)
	}

	response, err := h.service.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: "too late"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("DenyApproval boundary response/error = %+v/%v, want FailedPrecondition", response, err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "expired" || approval.DenialReason.Valid {
		t.Fatalf("expired approval retained a denial rationale: %+v", approval)
	}
	var expiredEvents, deniedEvents int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'approval.expired'`, enqueued.RunID).Scan(&expiredEvents); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'approval.denied'`, enqueued.RunID).Scan(&deniedEvents); err != nil {
		t.Fatal(err)
	}
	if expiredEvents != 1 || deniedEvents != 0 {
		t.Fatalf("expiry/denial event counts = %d/%d, want 1/0", expiredEvents, deniedEvents)
	}
}

func TestDenyApprovalReturnsDeniedStatus(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)

	reason := "Wrong destination"
	resp, err := client.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: reason})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != turingv1.ApprovalStatus_APPROVAL_STATUS_DENIED {
		t.Fatalf("DenyApproval status = %s", resp.Status)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.DenialReason != (sql.NullString{String: reason, Valid: true}) || approval.ApprovalComment.Valid {
		t.Fatalf("denial rationale = comment %#v reason %#v", approval.ApprovalComment, approval.DenialReason)
	}
	var payloadJSON string
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT payload_json
		FROM audit_logs
		WHERE action = 'approval.denied' AND target = ?
	`, approvalID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["reason"] != reason || payload["toolName"] != "files.update" {
		t.Fatalf("denial audit payload = %#v", payload)
	}
}

func TestHumanApprovalRationaleEmptyInputContract(t *testing.T) {
	tests := []struct {
		name   string
		decide func(context.Context, *Server, string) error
		field  func(repository.ApprovalRecord) sql.NullString
	}{
		{
			name: "approve omitted scalar",
			decide: func(ctx context.Context, service *Server, approvalID string) error {
				_, err := service.ApproveApproval(ctx, &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
				return err
			},
			field: func(record repository.ApprovalRecord) sql.NullString { return record.ApprovalComment },
		},
		{
			name: "approve explicit empty scalar",
			decide: func(ctx context.Context, service *Server, approvalID string) error {
				_, err := service.ApproveApproval(ctx, &turingv1.ApproveApprovalRequest{ApprovalId: approvalID, Comment: ""})
				return err
			},
			field: func(record repository.ApprovalRecord) sql.NullString { return record.ApprovalComment },
		},
		{
			name: "deny omitted scalar",
			decide: func(ctx context.Context, service *Server, approvalID string) error {
				_, err := service.DenyApproval(ctx, &turingv1.DenyApprovalRequest{ApprovalId: approvalID})
				return err
			},
			field: func(record repository.ApprovalRecord) sql.NullString { return record.DenialReason },
		},
		{
			name: "deny explicit empty scalar",
			decide: func(ctx context.Context, service *Server, approvalID string) error {
				_, err := service.DenyApproval(ctx, &turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: ""})
				return err
			},
			field: func(record repository.ApprovalRecord) sql.NullString { return record.DenialReason },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newApprovalHarness(t)
			enqueued := h.createRunningToolCall(t)
			approvalID, err := h.service.CreateApprovalForTool(
				context.Background(),
				enqueued.RunID,
				"call_1",
				"general_assistant",
				"files.update",
				map[string]any{"path": "note.txt"},
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.decide(context.Background(), h.service, approvalID); err != nil {
				t.Fatal(err)
			}
			record, err := h.repo.GetApproval(context.Background(), approvalID)
			if err != nil {
				t.Fatal(err)
			}
			if field := test.field(record); !field.Valid || field.String != "" {
				t.Fatalf("empty human rationale = %#v, want valid empty string", field)
			}
		})
	}
}

func TestApprovalRationaleRejectsInvalidOrOversizedInput(t *testing.T) {
	tests := []struct {
		name   string
		decide func(context.Context, *Server, string) error
	}{
		{
			name: "approve oversized",
			decide: func(ctx context.Context, service *Server, approvalID string) error {
				_, err := service.ApproveApproval(ctx, &turingv1.ApproveApprovalRequest{
					ApprovalId: approvalID,
					Comment:    strings.Repeat("x", maxDecisionRationaleBytes+1),
				})
				return err
			},
		},
		{
			name: "deny invalid UTF-8",
			decide: func(ctx context.Context, service *Server, approvalID string) error {
				_, err := service.DenyApproval(ctx, &turingv1.DenyApprovalRequest{
					ApprovalId: approvalID,
					Reason:     string([]byte{0xff}),
				})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newApprovalHarness(t)
			enqueued := h.createRunningToolCall(t)
			approvalID, err := h.service.CreateApprovalForTool(
				context.Background(),
				enqueued.RunID,
				"call_1",
				"general_assistant",
				"files.update",
				map[string]any{"path": "note.txt"},
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.decide(context.Background(), h.service, approvalID); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("decision error = %v, want InvalidArgument", err)
			}
			record, err := h.repo.GetApproval(context.Background(), approvalID)
			if err != nil {
				t.Fatal(err)
			}
			if record.Status != "pending" || record.ApprovalComment.Valid || record.DenialReason.Valid {
				t.Fatalf("invalid rationale mutated approval: %+v", record)
			}
		})
	}
}

func TestApprovalRationaleAuditIsBoundedAndAllowlisted(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	secretToolArgument := "tool-argument-must-not-enter-decision-audit"
	approvalID, err := h.service.CreateApprovalForTool(
		context.Background(),
		enqueued.RunID,
		"call_1",
		"general_assistant",
		"files.update",
		map[string]any{
			"path":    "note.txt",
			"content": strings.Repeat(secretToolArgument, 100),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	comment := strings.Repeat("🔥", maxAuditRationaleBytes)
	if len(comment) > maxDecisionRationaleBytes {
		comment = comment[:maxDecisionRationaleBytes]
		for !utf8.ValidString(comment) {
			comment = comment[:len(comment)-1]
		}
	}
	if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{
		ApprovalId: approvalID,
		Comment:    comment,
	}); err != nil {
		t.Fatal(err)
	}
	record, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ApprovalComment.String != comment {
		t.Fatalf("stored comment was truncated: got %d bytes, want %d", len(record.ApprovalComment.String), len(comment))
	}

	var payloadJSON string
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT payload_json
		FROM audit_logs
		WHERE action = 'approval.approved' AND target = ?
	`, approvalID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payloadJSON, secretToolArgument) {
		t.Fatalf("decision audit leaked tool arguments: %s", payloadJSON)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	auditComment, ok := payload["comment"].(string)
	if !ok || len(auditComment) > maxAuditRationaleBytes || !utf8.ValidString(auditComment) || payload["commentTruncated"] != true {
		t.Fatalf("bounded audit comment = %#v", payload)
	}
	for _, forbidden := range []string{"approvalToken", "approvalJti", "args", "argsHash", "toolArgs"} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("decision audit payload includes forbidden field %q: %#v", forbidden, payload)
		}
	}
}

func TestDeleteSessionScrubsApprovalRationaleAudit(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(
		context.Background(),
		enqueued.RunID,
		"call_1",
		"general_assistant",
		"files.update",
		map[string]any{"path": "note.txt"},
	)
	if err != nil {
		t.Fatal(err)
	}
	const reason = "withdrawn rationale 5a53118c"
	if _, err := h.service.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{
		ApprovalId: approvalID,
		Reason:     reason,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.DeleteSession(context.Background(), enqueued.SessionID); err != nil {
		t.Fatal(err)
	}
	var approvals int
	if err := h.database.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM approvals WHERE id = ?`,
		approvalID,
	).Scan(&approvals); err != nil {
		t.Fatal(err)
	}
	if approvals != 0 {
		t.Fatalf("approval count after session deletion = %d, want 0", approvals)
	}
	var payloadJSON string
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT payload_json
		FROM audit_logs
		WHERE action = 'approval.denied' AND target = ?
	`, approvalID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	if payloadJSON != `{"scrubbed":true}` || strings.Contains(payloadJSON, reason) {
		t.Fatalf("approval audit payload after deletion = %q", payloadJSON)
	}
}

func TestLateApprovalAuditAfterSessionDeletionDoesNotRestoreRationale(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(
		context.Background(),
		enqueued.RunID,
		"call_1",
		"general_assistant",
		"files.update",
		map[string]any{"path": "note.txt"},
	)
	if err != nil {
		t.Fatal(err)
	}
	const reason = "rationale that must remain withdrawn"
	if _, err := h.service.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{
		ApprovalId: approvalID,
		Reason:     reason,
	}); err != nil {
		t.Fatal(err)
	}
	denied, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.DeleteSession(context.Background(), enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	h.service.finishPostCommit(denied, "client", "approval.denied", "denied", "")

	rows, err := h.database.QueryContext(context.Background(), `
		SELECT payload_json
		FROM audit_logs
		WHERE action = 'approval.denied' AND target = ?
		ORDER BY rowid
	`, approvalID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var payloads []string
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, payload)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payloads, []string{`{"scrubbed":true}`}) {
		t.Fatalf("approval audit payloads after late write = %q", payloads)
	}
}

func TestApprovedUnchangedTransitionSkipsPostCommitEffects(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(
		context.Background(),
		enqueued.RunID,
		"call_1",
		"general_assistant",
		"files.update",
		map[string]any{"path": "note.txt"},
	)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := h.repo.ApproveApproval(
		context.Background(),
		approvalID,
		"already-committed-token",
		sql.NullString{String: "first committed comment", Valid: true},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	notifier := &recordingApprovalNotifier{}
	h.service.SetNotifier(notifier)

	h.service.runChangedApprovalPostCommitEffects(repository.ApprovalTerminalization{Approval: approved})

	if got := notifier.snapshot(); got.count != 0 {
		t.Fatalf("unchanged transition notified runtime %d time(s)", got.count)
	}
	var audits int
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM audit_logs
		WHERE action = 'approval.approved' AND target = ?
	`, approvalID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 0 {
		t.Fatalf("unchanged transition wrote %d approval audit row(s)", audits)
	}
}

func TestApprovalRationaleSurvivesDatabaseRestart(t *testing.T) {
	tests := []struct {
		name   string
		decide func(context.Context, *Server, string) error
		field  func(repository.ApprovalRecord) sql.NullString
		want   string
	}{
		{
			name: "approval comment",
			decide: func(ctx context.Context, service *Server, approvalID string) error {
				_, err := service.ApproveApproval(ctx, &turingv1.ApproveApprovalRequest{
					ApprovalId: approvalID,
					Comment:    "persisted approval comment",
				})
				return err
			},
			field: func(record repository.ApprovalRecord) sql.NullString { return record.ApprovalComment },
			want:  "persisted approval comment",
		},
		{
			name: "denial reason",
			decide: func(ctx context.Context, service *Server, approvalID string) error {
				_, err := service.DenyApproval(ctx, &turingv1.DenyApprovalRequest{
					ApprovalId: approvalID,
					Reason:     "persisted denial reason",
				})
				return err
			},
			field: func(record repository.ApprovalRecord) sql.NullString { return record.DenialReason },
			want:  "persisted denial reason",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "turing.db")
			database, err := db.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.ApplyMigrations(ctx, database); err != nil {
				t.Fatal(err)
			}
			repo := repository.New(database)
			service := New(repo, events.NewBus(8), "approval-secret")
			session, err := repo.CreateSession(ctx, "Restart rationale")
			if err != nil {
				t.Fatal(err)
			}
			enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
				SessionID:     session.SessionID,
				Content:       "needs approval",
				AgentID:       "general_assistant",
				ModelProvider: "ollama",
				Model:         "llama3.2",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
				t.Fatal(err)
			}
			if err := repo.RecordToolCallBefore(
				ctx,
				repository.ToolCallRecord{ToolCallID: "call_restart", RunID: enqueued.RunID},
				"general_assistant",
				"files",
				"files.update",
				`{"path":"note.txt"}`,
				"sha256:restart",
			); err != nil {
				t.Fatal(err)
			}
			approvalID, err := service.CreateApprovalForTool(
				ctx,
				enqueued.RunID,
				"call_restart",
				"general_assistant",
				"files.update",
				map[string]any{"path": "note.txt"},
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.decide(ctx, service, approvalID); err != nil {
				t.Fatal(err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := db.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			if err := db.ApplyMigrations(ctx, reopened); err != nil {
				t.Fatal(err)
			}
			record, err := repository.New(reopened).GetApproval(ctx, approvalID)
			if err != nil {
				t.Fatal(err)
			}
			if field := test.field(record); !field.Valid || field.String != test.want {
				t.Fatalf("rationale after restart = %#v, want %q", field, test.want)
			}
		})
	}
}

// TestEveryApprovalDecisionOnARunIsPublished covers a run that asked for two
// authorizations before either was answered. The first decision resumes the
// run; the second one arrives at a run that is already running, so it commits
// no lifecycle change — but the user did decide, and a decision nobody
// publishes leaves the request on screen forever.
func TestEveryApprovalDecisionOnARunIsPublished(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	if err := h.repo.RecordToolCallBefore(context.Background(), repository.ToolCallRecord{ToolCallID: "call_2", RunID: enqueued.RunID},
		"general_assistant", "files", "files.update", `{"path":"second.txt"}`, "sha256:second"); err != nil {
		t.Fatal(err)
	}
	first, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_2", "general_assistant", "files.update", map[string]any{"path": "second.txt"})
	if err != nil {
		t.Fatal(err)
	}
	published, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()
	client := turingv1.NewApprovalServiceClient(h.conn)

	for _, approvalID := range []string{first, second} {
		if _, err := client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
			t.Fatalf("ApproveApproval %s: %v", approvalID, err)
		}
	}
	var approved []string
	for len(approved) < 2 {
		select {
		case event := <-published:
			if event.Type != "approval.approved" {
				continue
			}
			var payload struct {
				ApprovalID string `json:"approvalId"`
			}
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				t.Fatal(err)
			}
			approved = append(approved, payload.ApprovalID)
		case <-time.After(time.Second):
			t.Fatalf("published approval decisions = %v, want both %s and %s", approved, first, second)
		}
	}
	if want := []string{first, second}; !reflect.DeepEqual(approved, want) {
		t.Fatalf("published approval decisions = %v, want %v", approved, want)
	}
}

func TestDenyApprovalPublishesCommittedTerminalRunEventOnlyOnce(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	published, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()
	client := turingv1.NewApprovalServiceClient(h.conn)

	if _, err := client.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	var publishedTypes []string
	for len(publishedTypes) < 3 {
		select {
		case event := <-published:
			if event.RunID == enqueued.RunID && (event.Type == "approval.denied" || event.Type == "tool.call.denied" || event.Type == "agent.run.failed") {
				publishedTypes = append(publishedTypes, event.Type)
				if event.Type == "tool.call.denied" {
					var payload map[string]any
					if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
						t.Fatal(err)
					}
					// A closed category, not the sentence the backend used to
					// write: a client localizes "policy denied", and nothing
					// a tool or provider said can ride along inside it.
					want := map[string]any{
						"toolCallId": "call_1",
						"toolName":   "files.update",
						"serverName": "files",
						"category":   "policy_denied",
					}
					if !reflect.DeepEqual(payload, want) {
						t.Fatalf("tool.call.denied payload = %#v, want %#v", payload, want)
					}
				}
			}
		case <-time.After(time.Second):
			t.Fatalf("published lifecycle events = %v", publishedTypes)
		}
	}
	if want := []string{"approval.denied", "tool.call.denied", "agent.run.failed"}; !reflect.DeepEqual(publishedTypes, want) {
		t.Fatalf("published lifecycle events = %v, want %v", publishedTypes, want)
	}

	if _, err := client.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatalf("repeated denial: %v", err)
	}
	select {
	case event := <-published:
		t.Fatalf("repeated denial published duplicate event: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestApprovalTerminalizationRollsBackWhenRequiredApprovalEventAppendFails(t *testing.T) {
	tests := []struct {
		name        string
		eventType   string
		terminalize func(context.Context, *approvalHarness, string) error
	}{
		{
			name:      "denial",
			eventType: "approval.denied",
			terminalize: func(ctx context.Context, h *approvalHarness, approvalID string) error {
				_, err := h.service.DenyApproval(ctx, &turingv1.DenyApprovalRequest{ApprovalId: approvalID})
				return err
			},
		},
		{
			name:      "expiry",
			eventType: "approval.expired",
			terminalize: func(ctx context.Context, h *approvalHarness, approvalID string) error {
				if _, err := h.database.ExecContext(ctx, `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
					return err
				}
				_, err := h.service.GetApprovalForRuntime(ctx, &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newApprovalHarness(t)
			enqueued := h.createRunningToolCall(t)
			approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.database.ExecContext(context.Background(), fmt.Sprintf(`
				CREATE TRIGGER fail_%s_event
				BEFORE INSERT ON events
				WHEN NEW.type = '%s'
				BEGIN
					SELECT RAISE(ABORT, 'append approval event failed');
				END
			`, test.name, test.eventType)); err != nil {
				t.Fatal(err)
			}
			err = test.terminalize(context.Background(), h, approvalID)
			if err == nil || !strings.Contains(err.Error(), "append approval event failed") {
				t.Fatalf("terminalization error = %v, want approval event append failure", err)
			}
			approval, err := h.repo.GetApproval(context.Background(), approvalID)
			if err != nil {
				t.Fatal(err)
			}
			run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if approval.Status != "pending" || run.Status != "waiting_approval" {
				t.Fatalf("required event failure leaked terminal state: approval=%q run=%q", approval.Status, run.Status)
			}
		})
	}
}

func TestApproveExpiredApprovalFailsPrecondition(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)

	_, err = client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ApproveApproval error = %v, want FailedPrecondition", err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "expired" {
		t.Fatalf("approval status = %q, want expired", approval.Status)
	}
}

func TestReapproveExpiredApprovedApprovalRevokesToken(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)
	if _, err := client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
		t.Fatal(err)
	}

	_, err = client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("reapprove expired approval error = %v, want FailedPrecondition", err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "expired" || approval.ApprovalToken != "" {
		t.Fatalf("approval after reapprove = %+v, want expired with revoked token", approval)
	}
}

// TestApprovalDenialRationaleStaysOutOfRunStateAndFailureEvents holds the line
// TUR-002 drew. A human's reason for refusing a tool call is governed audit
// input, not a generic machine diagnostic: it belongs in the approval row and
// the bounded audit projection, and it must not reach a public event payload,
// a run state, or a failure event on the way out.
func TestApprovalDenialRationaleStaysOutOfRunStateAndFailureEvents(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID,
		"call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	const rationale = "denied because this would email the whole company"
	if _, err := turingv1.NewApprovalServiceClient(h.conn).DenyApproval(context.Background(),
		&turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: rationale}); err != nil {
		t.Fatal(err)
	}

	// The rationale is still where governance put it.
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if !approval.DenialReason.Valid || approval.DenialReason.String != rationale {
		t.Fatalf("stored denial reason = %+v, want the operator's words preserved", approval.DenialReason)
	}
	var auditPayloads int
	rows, err := h.database.QueryContext(context.Background(),
		`SELECT payload_json FROM audit_logs WHERE payload_json LIKE ?`, "%"+rationale+"%")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		auditPayloads++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if auditPayloads == 0 {
		t.Fatal("the governed audit projection lost the denial rationale")
	}

	// And nowhere else. Every public event this session can serve is read
	// through the same boundary a client reads.
	server := events.NewServer(h.repo, h.bus)
	listed, err := server.ListEvents(context.Background(),
		&turingv1.ListEventsRequest{SessionId: enqueued.SessionID, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	var sawDenial, sawFailure bool
	for _, event := range listed.GetEvents() {
		switch event.GetType() {
		case turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED:
			sawDenial = true
		case turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED:
			sawFailure = true
		}
		for key, value := range event.GetPayload().AsMap() {
			text, isText := value.(string)
			if isText && strings.Contains(text, rationale) {
				t.Fatalf("public %v payload republished the denial rationale under %q", event.GetType(), key)
			}
		}
		state := event.GetRunState()
		if state == nil {
			continue
		}
		if strings.Contains(state.String(), rationale) {
			t.Fatalf("run state on %v carries the denial rationale", event.GetType())
		}
	}
	if !sawDenial || !sawFailure {
		t.Fatalf("seeded events denial=%v failure=%v, want both public paths covered", sawDenial, sawFailure)
	}
}
